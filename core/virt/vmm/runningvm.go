package vmm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/sync/errgroup"

	"github.com/nxadm/tail"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/walteh/runm/core/gvnet"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/conn"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"

	grpcruntime "github.com/walteh/runm/core/runc/runtime/grpc"
	runmv1 "github.com/walteh/runm/proto/v1"
)

type RunningVM[VM VirtualMachine] struct {
	// streamExecReady bool
	// manager                *VSockManager
	runtime      *grpcruntime.GRPCClientRuntime
	hostOtlpPort uint32
	bootloader   virtio.Bootloader

	closeCancel context.CancelFunc

	// streamexec   *streamexec.Client
	portOnHostIP uint16
	wait         chan error
	vm           VM
	netdev       gvnet.Proxy
	workingDir   string
	// stdin        io.Reader
	// stdout       io.Writer
	// stderr       io.Writer
	// connStatus      <-chan VSockManagerState
	start time.Time

	rawWriter   io.Writer
	delimWriter io.Writer
}

func (r *RunningVM[VM]) GuestService(ctx context.Context) (*grpcruntime.GRPCClientRuntime, error) {
	slog.InfoContext(ctx, "getting guest service", "id", r.vm.ID())
	if r.runtime != nil {
		return r.runtime, nil
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	timeout := time.NewTimer(3 * time.Second)
	defer ticker.Stop()
	defer timeout.Stop()

	lastError := error(errors.Errorf("initial error"))

	for {
		select {
		case <-ticker.C:
			slog.InfoContext(ctx, "connecting to vsock", "port", constants.RunmGuestServerVsockPort)
			conn, err := r.vm.VSockConnect(ctx, uint32(constants.RunmGuestServerVsockPort))
			if err != nil {
				lastError = err
				continue
			}
			opts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
					slog.InfoContext(ctx, "dialing vsock", "port", constants.RunmGuestServerVsockPort, "ignored_addr", addr)
					return conn, nil
				}),
				grpc.WithUnaryInterceptor(grpcerr.UnaryClientInterceptor),
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024 * 1024 * 10)),
			}
			if logging.GetGlobalOtelInstances() != nil {
				opts = append(opts, logging.GetGlobalOtelInstances().GetGrpcClientOpts())
			}
			grpcConn, err := grpc.NewClient("passthrough:target", opts...)
			if err != nil {
				lastError = err
				continue
			}

			// test the connection
			grpcConn.Connect()

			r.runtime, err = grpcruntime.NewGRPCClientRuntimeFromConn(grpcConn)
			if err != nil {
				lastError = err
				continue
			}
			r.runtime.SetVsockProxier(r)
			return r.runtime, nil
		case <-timeout.C:
			slog.ErrorContext(ctx, "timeout waiting for guest service connection", "error", lastError)
			return nil, errors.Errorf("timeout waiting for guest service connection: %w", lastError)
		case <-ctx.Done():
			slog.ErrorContext(ctx, "context done waiting for guest service connection", "error", lastError)
			return nil, ctx.Err()
		}
	}
}

// open a new unix socket pair

// // attempt to get the raw connection, otherwise proxy it

// rc, err := hack.TryGetUnexportedFieldOf[net.Conn](conn, "rawConn")
// if err != nil {
// 	return nil, errors.Errorf("getting raw connection: %w", err)
// }

// unixConn, ok := rc.(*net.UnixConn)
// if !ok {
// 	return nil, errors.Errorf("raw connection is not a net.UnixConn")
// }

func (r *RunningVM[VM]) ListenAndAcceptSingleVsockConnection(ctx context.Context, port uint32, dialCallback func(ctx context.Context) error) (*net.UnixConn, error) {

	lfunc := func(ctx context.Context) (net.Listener, error) {
		return r.vm.VSockListen(ctx, uint32(port))
	}

	cz, err := conn.ListenAndAcceptSingleNetConn(ctx, lfunc, dialCallback)
	if err != nil {
		return nil, errors.Errorf("listening and accepting vsock connection: %w", err)
	}

	unixConn, err := conn.CreateUnixConnProxy(ctx, cz)
	if err != nil {
		return nil, errors.Errorf("creating unix conn proxy: %w", err)
	}

	// open a new unix socket pair

	return unixConn, nil
}

// type filer interface {
// 	File() (*os.File, error)
// }

// rawConn := hack.GetUnexportedFieldOf(conn, "rawConn")
// rawConnf, ok := rawConn.(filer)
// if !ok {
// 	return nil, errors.Errorf("connection does not support File()")
// }

// file, err := rawConnf.File()
// if err != nil {
// 	return nil, errors.Errorf("getting file from connection: %w", err)
// }
// defer file.Close()

// // Now use this fd with mdlayher/socket
// sockConn, err := socket.FileConn(
// 	file,
// 	"custom",
// )

// return sockConn, nil
// }

func (r *RunningVM[VM]) Close(ctx context.Context) error {
	r.closeCancel()
	if r.vm.CurrentState() == VirtualMachineStateTypeStopped {
		return nil
	}
	err := r.vm.HardStop(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "error hard stopping vm", "error", err)
		return errors.Errorf("hard stopping vm: %w", err)
	}
	return nil
}

func (r *RunningVM[VM]) ProxyVsock(ctx context.Context, port uint32) (*net.UnixConn, string, error) {
	conn, err := r.vm.VSockConnect(ctx, uint32(port))
	if err != nil {
		return nil, "", errors.Errorf("connecting to vsock: %w", err)
	}

	unixSocketPath := filepath.Join(r.workingDir, fmt.Sprintf("vsock-%d.sock", port))

	// open a unix socket to the connection in a working directory
	unixConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: unixSocketPath, Net: "unix"})
	if err != nil {
		return nil, "", errors.Errorf("dialing unix socket: %w", err)
	}

	// copy the connection to the unix socket
	go func() {
		_, err := io.Copy(unixConn, conn)
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			slog.ErrorContext(ctx, "error copying connection to unix socket", "error", err)
		}
	}()

	go func() {
		_, err := io.Copy(conn, unixConn)
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			slog.ErrorContext(ctx, "error copying connection to vsock", "error", err)
		}
	}()

	return unixConn, unixSocketPath, nil
}

func (r *RunningVM[VM]) ForwardStdio(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	return ForwardStdio(ctx, r.vm, stdin, stdout, stderr)
}

func (r *RunningVM[VM]) WaitOnVmStopped() error {
	return <-r.wait
}

func (r *RunningVM[VM]) VM() VM {
	return r.vm
}

func (r *RunningVM[VM]) PortOnHostIP() uint16 {
	return r.portOnHostIP
}

func (r *RunningVM[VM]) RunCommandSimple(ctx context.Context, command string) ([]byte, []byte, int64, error) {
	guestService, err := r.GuestService(ctx)
	if err != nil {
		return nil, nil, 0, errors.Errorf("getting guest service: %w", err)
	}

	fields := strings.Fields(command)

	argc := fields[0]
	argv := []string{}
	for _, field := range fields[1:] {
		argv = append(argv, field)
	}

	// req, err := harpoonv1.NewValidatedRunRequest(func(b *harpoonv1.RunRequest_builder) {
	// 	// b.Stdin = stdinData
	// })
	req, err := runmv1.NewGuestRunCommandRequestE(&runmv1.GuestRunCommandRequest_builder{
		Argc:    argc,
		Argv:    argv,
		EnvVars: map[string]string{},
		Stdin:   []byte{},
	})
	if err != nil {
		return nil, nil, 0, err
	}

	exec, err := guestService.Management().GuestRunCommand(ctx, req)
	if err != nil {
		return nil, nil, 0, err
	}

	return exec.GetStdout(), exec.GetStderr(), int64(exec.GetExitCode()), nil
}

func (rvm *RunningVM[VM]) Start(ctx context.Context) error {

	ctx, closeCancel := context.WithCancel(ctx)
	errgrp, _ := errgroup.WithContext(ctx)

	rvm.closeCancel = closeCancel

	errgrp.Go(func() error {
		err := rvm.netdev.Wait(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "error waiting for netdev", "error", err)
			return errors.Errorf("waiting for netdev: %w", err)
		}
		return nil
	})

	err := bootVM(ctx, rvm.VM())
	if err != nil {
		if err := TryAppendingConsoleLog(ctx, rvm.workingDir); err != nil {
			slog.ErrorContext(ctx, "error appending console log", "error", err)
		}
		return errors.Errorf("booting virtual machine: %w", err)
	}

	errgrp.Go(func() error {
		var writer io.Writer
		if rvm.rawWriter == nil {
			slog.WarnContext(ctx, "raw writer is nil, using default raw writer")
			writer = logging.GetDefaultRawWriter()
		} else {
			writer = rvm.rawWriter
		}
		slog.InfoContext(ctx, "running vsock raw writer proxy server", "port", constants.VsockRawWriterProxyPort)
		err = rvm.RunVsockProxyServer(ctx, uint32(constants.VsockRawWriterProxyPort), writer)
		if err != nil {
			slog.ErrorContext(ctx, "error running vsock raw writer proxy server", "error", err)
			return errors.Errorf("running vsock raw writer proxy server: %w", err)
		}
		return nil
	})

	errgrp.Go(func() error {
		var writer io.Writer
		if rvm.delimWriter == nil {
			slog.WarnContext(ctx, "delim writer is nil, using default delim writer")
			writer = logging.GetDefaultDelimWriter()
		} else {
			writer = rvm.delimWriter
		}
		slog.InfoContext(ctx, "running vsock delimited writer proxy server", "port", constants.VsockDelimitedWriterProxyPort)
		err = rvm.RunVsockProxyServer(ctx, uint32(constants.VsockDelimitedWriterProxyPort), writer)
		if err != nil {
			slog.ErrorContext(ctx, "error running vsock delimited writer proxy server", "error", err)
			return errors.Errorf("running vsock delimited writer proxy server: %w", err)
		}
		return nil
	})

	if rvm.hostOtlpPort != 0 {
		errgrp.Go(func() error {
			err = rvm.SetupOtelForwarder(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "error setting up otel forwarder", "error", err)
				return errors.Errorf("setting up otel forwarder: %w", err)
			}
			return nil
		})
	}

	errgrp.Go(func() error {
		err = rvm.VM().ServeBackgroundTasks(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "error serving background tasks", "error", err)
			return errors.Errorf("serving background tasks: %w", err)
		}
		return nil
	})

	errgrp.Go(func() error {
		err = rvm.SetupHostService(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "error setting up host service", "error", err)
			return errors.Errorf("setting up host service: %w", err)
		}
		return nil
	})

	err = TailConsoleLog(ctx, rvm.workingDir)
	if err != nil {
		slog.ErrorContext(ctx, "error tailing console log", "error", err)
	}

	// For container runtimes, we want the VM to stay running, not wait for it to stop
	slog.InfoContext(ctx, "VM is ready for container execution")

	// Create an error channel that will receive VM state changes

	go func() {

		// Wait for errgroup to finish (this handles cleanup when context is cancelled)
		if err := errgrp.Wait(); err != nil && err != context.Canceled {
			slog.ErrorContext(ctx, "error running gvproxy", "error", err)
		}

		// // Wait for runtime services to finish
		// if err := runErrGroup.Wait(); err != nil && err != context.Canceled {
		// 	slog.ErrorContext(ctx, "error running runtime services", "error", err)
		// 	errCh <- err
		// 	return
		// }

		// Only send error if VM actually encounters an error state
		stateNotify := rvm.VM().StateChangeNotify(ctx)
		for {
			select {
			case state := <-stateNotify:
				if state.StateType == VirtualMachineStateTypeError {
					rvm.wait <- errors.Errorf("VM entered error state")
					return
				}
				if state.StateType == VirtualMachineStateTypeStopped {
					slog.InfoContext(ctx, "VM stopped")
					rvm.wait <- nil
					return
				}
				slog.InfoContext(ctx, "VM state changed", "state", state.StateType, "metadata", state.Metadata)
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.InfoContext(ctx, "waiting for guest service")

	connection, err := rvm.GuestService(ctx)
	if err != nil {
		return errors.Errorf("failed to get guest service: %w", err)
	}

	slog.InfoContext(ctx, "got guest service - making time sync request to management service")

	tsreq := &runmv1.GuestTimeSyncRequest{}
	tsreq.SetUnixTimeNs(uint64(time.Now().UnixNano()))
	response, err := connection.Management().GuestTimeSync(ctx, tsreq)
	if err != nil {
		slog.ErrorContext(ctx, "failed to time sync", "error", err)
		return errors.Errorf("failed to time sync: %w", err)
	}
	slog.InfoContext(ctx, "time sync 1", "response", response)

	// this first request, will take a few extra milliseconds, so we make the same call again

	tsreq.SetUnixTimeNs(uint64(time.Now().UnixNano()))
	response, err = connection.Management().GuestTimeSync(ctx, tsreq)
	if err != nil {
		slog.ErrorContext(ctx, "failed to time sync", "error", err)
		return errors.Errorf("failed to time sync: %w", err)
	}
	slog.InfoContext(ctx, "time sync 2", "response", response)

	return nil
}

func (rvm *RunningVM[VM]) SetupHostService(ctx context.Context) error {

	vsockListener, err := rvm.vm.VSockListen(ctx, uint32(constants.RunmHostServerVsockPort))
	if err != nil {
		slog.ErrorContext(ctx, "vsock listen failed", "err", err)
		return errors.Errorf("listening on vsock: %w", err)
	}
	defer vsockListener.Close()

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(grpcerr.UnaryServerInterceptor),
		grpc.ChainStreamInterceptor(grpcerr.StreamServerInterceptor()),
	)

	runmv1.RegisterHostServiceServer(grpcServer, rvm)

	err = grpcServer.Serve(vsockListener)
	if err != nil {
		slog.ErrorContext(ctx, "error serving grpc server", "err", err)
		return errors.Errorf("serving grpc server: %w", err)
	}

	return nil
}

func (rvm *RunningVM[VM]) SetupOtelForwarder(ctx context.Context) error {
	// 1️⃣ Listen on the VM’s VSOCK port
	vsockListener, err := rvm.vm.VSockListen(ctx, uint32(constants.VsockOtelPort))
	if err != nil {
		slog.ErrorContext(ctx, "vsock listen failed", "err", err)
		return errors.Errorf("listening on vsock: %w", err)
	}
	defer vsockListener.Close()

	slog.InfoContext(ctx, "vsock listener started", "port", constants.VsockOtelPort)

	// dial tcp localhost:5909
	conn, err := net.Dial("tcp", fmt.Sprintf(":%d", rvm.hostOtlpPort))
	if err != nil {
		slog.ErrorContext(ctx, "unix dial failed", "err", err)
		return errors.Errorf("dialing unix socket: %w", err)
	}

	defer conn.Close()

	slog.InfoContext(ctx, "dialed otel unix socket", "conn", conn)

	// 3️⃣ Accept loop: for each VSOCK conn, dial the UNIX socket, then proxy both ways
	for {
		select {
		case <-ctx.Done():
			// Parent context canceled: shut down
			slog.InfoContext(ctx, "shutting down Otel forwarder")
			return ctx.Err()
		default:
		}

		vConn, err := vsockListener.Accept()
		if err != nil {
			slog.ErrorContext(ctx, "vsock accept failed", "err", err)
			return errors.Errorf("vsock accept: %w", err)
		}

		slog.InfoContext(ctx, "vsock accepted for otel forwarder")

		// Proxy both directions
		go proxy(ctx, vConn, conn)
		go proxy(ctx, conn, vConn)
	}
}

// proxy copies from src to dst, logs errors, and closes both ends when done.
func proxy(ctx context.Context, src net.Conn, dst net.Conn) {
	defer src.Close()
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil && !errors.Is(err, net.ErrClosed) {
		slog.ErrorContext(ctx, "proxy copy error", "err", err)
	}
}

func bootVM[VM VirtualMachine](ctx context.Context, vm VM) error {
	bootCtx, bootCancel := context.WithCancel(ctx)
	errGroup, ctx := errgroup.WithContext(bootCtx)
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic in bootContainerVM", "panic", r)
			panic(r)
		}
		// clean up the boot provisioners - this shouldn't throw an error because they prob are going to throw something
		bootCancel()
		if err := errGroup.Wait(); err != nil {
			slog.DebugContext(ctx, "error running boot provisioners", "error", err)
		}

	}()

	go func() {
		for {
			select {
			case <-bootCtx.Done():
				return
			case <-vm.StateChangeNotify(bootCtx):
				slog.InfoContext(bootCtx, "virtual machine state changed", "state", vm.CurrentState())
			}
		}
	}()

	slog.InfoContext(ctx, "starting virtual machine")

	if err := vm.Start(ctx); err != nil {
		return errors.Errorf("starting virtual machine: %w", err)
	}

	if err := WaitForVMState(ctx, vm, VirtualMachineStateTypeRunning, time.After(30*time.Second)); err != nil {
		return errors.Errorf("waiting for virtual machine to start: %w", err)
	}

	slog.InfoContext(ctx, "virtual machine is running")

	return nil
}

func (rvm *RunningVM[VM]) Wait(ctx context.Context) error {
	return <-rvm.wait
}

func ptr[T any](v T) *T { return &v }

func TryAppendingConsoleLog(ctx context.Context, workingDir string) error {
	// log file
	file, err := os.ReadFile(filepath.Join(workingDir, "console.log"))
	if err != nil {
		return errors.Errorf("opening console log file: %w", err)
	}

	writer := logging.GetDefaultRawWriter()
	if writer == nil {
		slog.WarnContext(ctx, "default raw writer is not set, skipping appending console log")
		return nil
	}

	buf := bytes.NewBuffer(nil)
	buf.Write([]byte("\n\n--------------------------------\n\n"))
	buf.Write([]byte(filepath.Join(workingDir, "console.log")))
	buf.Write([]byte("\n\n"))
	buf.Write(file)
	buf.Write([]byte("\n--------------------------------\n\n"))

	_, err = io.Copy(writer, buf)
	if err != nil {
		slog.ErrorContext(ctx, "error copying console log", "error", err)
		return errors.Errorf("copying console log: %w", err)
	}

	return nil
}

func TailConsoleLog(ctx context.Context, workingDir string) error {
	dat, err := os.ReadFile(filepath.Join(workingDir, "console.log"))
	if err != nil {
		slog.ErrorContext(ctx, "error reading console log file", "error", err)
		return errors.Errorf("reading console log file: %w", err)
	}

	writer := logging.GetDefaultRawWriter()

	if writer == nil {
		slog.WarnContext(ctx, "default raw writer is not set, skipping tailing console log")
		return nil
	}

	for _, line := range strings.Split(string(dat), "\n") {
		fmt.Fprintf(writer, "%s\n", line)
	}

	go func() {
		t, err := tail.TailFile(filepath.Join(workingDir, "console.log"), tail.Config{Follow: true, Location: &tail.SeekInfo{Offset: int64(len(dat)), Whence: io.SeekStart}})
		if err != nil {
			slog.ErrorContext(ctx, "error tailing log file", "error", err)
			return
		}
		for line := range t.Lines {
			fmt.Fprintf(writer, "%s\n", line.Text)
		}
	}()

	return nil
}

func (rvm *RunningVM[VM]) RunVsockProxyServer(ctx context.Context, port uint32, writer io.Writer) error {
	vsockListener, err := rvm.vm.VSockListen(ctx, port)
	if err != nil {
		slog.ErrorContext(ctx, "vsock listen failed", "err", err)
		return errors.Errorf("listening on vsock: %w", err)
	}
	defer vsockListener.Close()

	if writer == nil {
		slog.ErrorContext(ctx, "writer is nil", "port", port)
		return errors.Errorf("writer is nil: %w", err)
	}

	for {
		conn, err := vsockListener.Accept()
		if err != nil {
			slog.ErrorContext(ctx, "vsock accept failed", "err", err)
			continue
		}

		slog.InfoContext(ctx, "vsock accepted for proxy server", "port", port)

		go func() {
			defer conn.Close()
			tb, err := io.Copy(writer, conn)
			if err != nil {
				slog.ErrorContext(ctx, "error copying to writer", "err", err, "port", port)
			} else {
				slog.InfoContext(ctx, "copied to writer - closing connection", "port", port, "bytes", tb)
			}
		}()
	}
}

// func (rvm *RunningVM[VM]) RunVsockJSONLogProxyServer(ctx context.Context) error {
// 	vsockListener, err := rvm.vm.VSockListen(ctx, uint32(constants.VsockJSONLogProxyPort))
// 	if err != nil {
// 		slog.ErrorContext(ctx, "vsock listen failed", "err", err)
// 		return errors.Errorf("listening on vsock: %w", err)
// 	}
// 	defer vsockListener.Close()

// 	writer := logging.GetDefaultLogWriter()

// 	for {
// 		conn, err := vsockListener.Accept()
// 		if err != nil {
// 			slog.ErrorContext(ctx, "vsock accept failed", "err", err)
// 			continue
// 		}

// 		go func(c net.Conn) {
// 			defer c.Close()

// 			// Option A: JSON Decoder for NDJSON
// 			dec := json.NewDecoder(c)
// 			for dec.More() {
// 				var entry map[string]interface{}
// 				if err := dec.Decode(&entry); err != nil {
// 					if err == io.EOF {
// 						break
// 					}
// 					slog.Error("json decode error", "err", err)
// 					break
// 				}
// 				fmt.Fprintf(writer, "%v\n", entry)
// 			}

// 			// Option B: Scanner for line-delimited JSON
// 			// scanner := bufio.NewScanner(c)
// 			// for scanner.Scan() {
// 			//     fmt.Fprintf(writer, "%s\n", scanner.Text())
// 			// }
// 			// if err := scanner.Err(); err != nil {
// 			//     slog.Error("scanner error", "err", err)
// 			// }
// 		}(conn)
// 	}
// }
