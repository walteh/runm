//go:build !windows

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

	"github.com/nxadm/tail"
	"gitlab.com/tozd/go/errors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/walteh/runm/core/gvnet"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/conn"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/taskgroup"

	runmv1 "github.com/walteh/runm/proto/v1"
)

type RunningVM[VM VirtualMachine] struct {
	cachedGuestGrpcConn *grpc.ClientConn
	// hostGrpcListener net.Listener
	hostOtlpPort uint32
	bootloader   virtio.Bootloader
	closeCancel  context.CancelFunc
	portOnHostIP uint16
	wait         chan error
	vm           VM
	netdev       gvnet.Proxy
	workingDir   string
	start        time.Time
	rawWriter    io.Writer
	delimWriter  io.Writer
	taskGroup    *taskgroup.TaskGroup
	stderrUndoFn func() error
}

func (r *RunningVM[VM]) GuestService(ctx context.Context) (runmv1.GuestManagementServiceClient, error) {
	slog.InfoContext(ctx, "getting guest service", "id", r.vm.ID())

	conn, err := r.GuestVsockConn(ctx)
	if err != nil {
		return nil, errors.Errorf("getting guest vsock conn: %w", err)
	}

	return runmv1.NewGuestManagementServiceClient(conn), nil

}

func (r *RunningVM[VM]) GuestVsockConn(ctx context.Context) (*grpc.ClientConn, error) {

	if r.cachedGuestGrpcConn != nil {
		return r.cachedGuestGrpcConn, nil
	}

	return r.buildGuestGrpcConn(ctx)
}

// func (r *RunningVM[VM]) GuestVsockConnOLD(ctx context.Context) (*grpc.ClientConn, error) {

// 	ticker := time.NewTicker(100 * time.Millisecond)
// 	timeout := time.NewTimer(3 * time.Second)
// 	defer ticker.Stop()
// 	defer timeout.Stop()

// 	lastError := error(errors.Errorf("initial error"))

// 	for {
// 		select {
// 		case <-ticker.C:
// 			slog.InfoContext(ctx, "connecting to vsock", "port", constants.RunmGuestServerVsockPort)

// 			opts := []grpc.DialOption{
// 				grpc.WithTransportCredentials(insecure.NewCredentials()),
// 				grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
// 					slog.InfoContext(ctx, "dialing vsock", "port", constants.RunmGuestServerVsockPort, "ignored_addr", addr)
// 					conn, err := r.vm.VSockConnect(ctx, uint32(constants.RunmGuestServerVsockPort))
// 					if err != nil {
// 						return nil, err
// 					}
// 					return conn, nil
// 				}),
// 				grpc.WithUnaryInterceptor(grpcerr.NewUnaryClientInterceptor(ctx)),
// 				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024*1024*10), grpc.WaitForReady(true)),
// 			}
// 			if logging.GetGlobalOtelInstances() != nil {
// 				opts = append(opts, logging.GetGlobalOtelInstances().GetGrpcClientOpts())
// 			}
// 			grpcConn, err := grpc.NewClient("passthrough:target", opts...)
// 			if err != nil {
// 				lastError = err
// 				continue
// 			}

// 			// test the connection
// 			grpcConn.Connect()

// 			// r.runtime, err = grpcruntime.NewGRPCClientRuntimeFromConn(grpcConn)
// 			// if err != nil {
// 			// 	lastError = err
// 			// 	continue
// 			// }
// 			// r.runtime.SetVsockProxier(r)
// 			// return r.runtime, nil
// 			return grpcConn, nil
// 		case <-timeout.C:
// 			slog.ErrorContext(ctx, "timeout waiting for guest service connection", "error", lastError)
// 			return nil, errors.Errorf("timeout waiting for guest service connection: %w", lastError)
// 		case <-ctx.Done():
// 			slog.ErrorContext(ctx, "context done waiting for guest service connection", "error", lastError)
// 			return nil, ctx.Err()
// 		}
// 	}
// }

func (r *RunningVM[VM]) ListenAndAcceptSingleVsockConnection(ctx context.Context, port uint32, dialCallback func(ctx context.Context) error) (net.Conn, error) {

	lfunc := func(ctx context.Context) (net.Listener, error) {
		return r.vm.VSockListen(ctx, uint32(port))
	}

	cz, err := conn.ListenAndAcceptSingleNetConn(ctx, lfunc, dialCallback)
	if err != nil {
		return nil, errors.Errorf("listening and accepting vsock connection: %w", err)
	}

	// unixConn, err := conn.CreateUnixConnProxy(ctx, cz)
	// if err != nil {
	// 	return nil, errors.Errorf("creating unix conn proxy: %w", err)
	// }

	// open a new unix socket pair

	return cz, nil
}

func (r *RunningVM[VM]) buildGuestGrpcConn(ctx context.Context) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			slog.InfoContext(ctx, "dialing vsock", "port", constants.RunmGuestServerVsockPort, "ignored_addr", addr)
			conn, err := r.vm.VSockConnect(ctx, uint32(constants.RunmGuestServerVsockPort))
			if err != nil {
				return nil, err
			}
			return conn, nil
		}),
		grpc.WithUnaryInterceptor(grpcerr.NewUnaryClientInterceptor(ctx)),
		grpc.WithStreamInterceptor(grpcerr.NewStreamClientInterceptor(ctx)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024*1024*10), grpc.WaitForReady(true)),
	}
	if logging.GetGlobalOtelInstances() != nil {
		opts = append(opts, logging.GetGlobalOtelInstances().GetGrpcClientOpts())
	}
	grpcConn, err := grpc.NewClient("passthrough:target", opts...)
	if err != nil {
		return nil, errors.Errorf("building guest grpc conn: %w", err)
	}
	return grpcConn, nil
}

func (r *RunningVM[VM]) Close(ctx context.Context) error {
	slog.InfoContext(ctx, "closing RunningVM")
	r.closeCancel()

	// Wait for all tasks to finish or timeout
	if r.taskGroup != nil {
		slog.InfoContext(ctx, "waiting for taskgroup to finish")
		if err := r.taskGroup.Wait(); err != nil {
			slog.ErrorContext(ctx, "error waiting for taskgroup", "error", err)
		}
	}

	// Restore stderr redirection
	if r.stderrUndoFn != nil {
		slog.InfoContext(ctx, "restoring stderr redirection")
		if err := r.stderrUndoFn(); err != nil {
			slog.ErrorContext(ctx, "error restoring stderr", "error", err)
		}
	}

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

func (r *RunningVM[VM]) ProxyVsock(ctx context.Context, port uint32) (net.Conn, error) {
	conn, err := r.vm.VSockConnect(ctx, uint32(port))
	if err != nil {
		return nil, errors.Errorf("connecting to vsock: %w", err)
	}

	// unixSocketPath := filepath.Join(r.workingDir, fmt.Sprintf("vsock-%d.sock", port))

	// // open a unix socket to the connection in a working directory
	// unixConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: unixSocketPath, Net: "unix"})
	// if err != nil {
	// 	return nil, "", errors.Errorf("dialing unix socket: %w", err)
	// }

	// // copy the connection to the unix socket
	// go func() {
	// 	_, err := io.Copy(unixConn, conn)
	// 	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
	// 		slog.ErrorContext(ctx, "error copying connection to unix socket", "error", err)
	// 	}
	// }()

	// go func() {
	// 	_, err := io.Copy(conn, unixConn)
	// 	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
	// 		slog.ErrorContext(ctx, "error copying connection to vsock", "error", err)
	// 	}
	// }()

	return conn, nil
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

	exec, err := guestService.GuestRunCommand(ctx, req)
	if err != nil {
		return nil, nil, 0, err
	}

	return exec.GetStdout(), exec.GetStderr(), int64(exec.GetExitCode()), nil
}

func (rvm *RunningVM[VM]) Start(ctx context.Context) error {

	ctx, closeCancel := context.WithCancel(ctx)
	rvm.closeCancel = closeCancel

	if rvm.rawWriter == nil {
		slog.WarnContext(ctx, "raw writer is nil, using default raw writer")
		rvm.rawWriter = logging.GetDefaultRawWriter()
	}

	if rvm.delimWriter == nil {
		slog.WarnContext(ctx, "delim writer is nil, using default delim writer")
		rvm.delimWriter = logging.GetDefaultDelimWriter()
	}
	// Initialize taskgroup
	rvm.taskGroup = taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("runningvm"),
		taskgroup.WithEnablePprof(true),
		taskgroup.WithLogStart(true),
		taskgroup.WithLogEnd(true),
		taskgroup.WithSlogBaseContext(ctx),
	)

	if rvm.cachedGuestGrpcConn == nil {
		conn, err := rvm.buildGuestGrpcConn(ctx)
		if err != nil {
			return errors.Errorf("building guest grpc conn: %w", err)
		}
		rvm.cachedGuestGrpcConn = conn
	}

	rvm.taskGroup.GoWithName("netdev-wait", func(ctx context.Context) error {
		err := rvm.netdev.Wait(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "error waiting for netdev", "error", err)
			return errors.Errorf("waiting for netdev: %w", err)
		}
		return nil
	})

	rvm.taskGroup.GoWithName("vm-state-monitor", func(ctx context.Context) error {
		// Only send error if VM actually encounters an error state
		stateNotify := rvm.VM().StateChangeNotify(ctx)
		for {
			select {
			case state := <-stateNotify:
				if state.StateType == VirtualMachineStateTypeError {
					rvm.wait <- errors.Errorf("VM entered error state")
					return nil
				}
				if state.StateType == VirtualMachineStateTypeStopped {
					slog.InfoContext(ctx, "VM stopped")
					rvm.wait <- nil
					return nil
				}
				slog.InfoContext(ctx, "VM state changed", "state", state.StateType, "metadata", state.Metadata)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	err := bootVM(ctx, rvm.VM(), rvm.taskGroup)
	if err != nil {
		if err := TryAppendingConsoleLog(ctx, rvm.workingDir); err != nil {
			slog.ErrorContext(ctx, "error appending console log", "error", err)
		}
		return errors.Errorf("booting virtual machine: %w", err)
	}

	go func() {
		rvm.cachedGuestGrpcConn.Connect()
	}()

	rvm.taskGroup.GoWithName("vsock-raw-writer-proxy", func(ctx context.Context) error {

		err = rvm.RunVsockProxyServer(ctx, uint32(constants.VsockRawWriterProxyPort), rvm.rawWriter)
		if err != nil {
			slog.ErrorContext(ctx, "error running vsock raw writer proxy server", "error", err)
			return errors.Errorf("running vsock raw writer proxy server: %w", err)
		}
		return nil
	})

	rvm.taskGroup.GoWithName("vsock-delimited-writer-proxy", func(ctx context.Context) error {
		err = rvm.RunVsockProxyServer(ctx, uint32(constants.VsockDelimitedWriterProxyPort), rvm.delimWriter)
		if err != nil {
			return errors.Errorf("running vsock delimited writer proxy server: %w", err)
		}
		return nil
	})

	rvm.taskGroup.GoWithName("vsock-debug-proxy", func(ctx context.Context) error {
		// open up a tcp port (lets just do 2017)
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", 2017))
		if err != nil {
			return errors.Errorf("listening on tcp port: %w", err)
		}
		defer listener.Close()
		err = rvm.RunVsockProxyClient(ctx, uint32(constants.VsockDebugPort), listener)
		if err != nil {
			return errors.Errorf("running vsock delimited writer proxy server: %w", err)
		}
		return nil
	})

	rvm.taskGroup.GoWithName("vsock-pprof-proxy", func(ctx context.Context) error {
		// open up a tcp port for pprof (port 6060)
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", 6060))
		if err != nil {
			return errors.Errorf("listening on tcp port for pprof: %w", err)
		}
		defer listener.Close()
		err = rvm.RunVsockProxyClient(ctx, uint32(constants.VsockPprofPort), listener)
		if err != nil {
			return errors.Errorf("running vsock pprof proxy server: %w", err)
		}
		return nil
	})

	if rvm.hostOtlpPort != 0 {
		rvm.taskGroup.GoWithName("otel-forwarder", func(ctx context.Context) error {
			err = rvm.SetupOtelForwarder(ctx)
			if err != nil {
				return errors.Errorf("setting up otel forwarder: %w", err)
			}
			return nil
		})
	}

	rvm.taskGroup.GoWithName("vm-background-tasks", func(ctx context.Context) error {
		err = rvm.VM().ServeBackgroundTasks(ctx)
		if err != nil {
			return errors.Errorf("serving background tasks: %w", err)
		}
		return nil
	})

	rvm.taskGroup.GoWithName("host-service", func(ctx context.Context) error {
		err = rvm.SetupHostService(ctx)
		if err != nil {
			return errors.Errorf("setting up host service: %w", err)
		}
		return nil
	})

	rvm.taskGroup.GoWithName("tail-console-log", func(ctx context.Context) error {
		err = TailConsoleLog(ctx, rvm.workingDir, rvm.rawWriter)
		if err != nil {
			return errors.Errorf("tailing console log: %w", err)
		}
		return nil
	})

	// For container runtimes, we want the VM to stay running, not wait for it to stop
	slog.InfoContext(ctx, "VM is ready for container execution")

	// Create an error channel that will receive VM state changes

	err = rvm.RunInitalTimesyncRequests(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "error running initial timesync requests", "error", err)
		return errors.Errorf("running initial timesync requests: %w", err)
	}

	// Note: Removed circular cleanup dependency - Close() already calls taskGroup.Wait()
	// so registering Close() as a cleanup would create infinite recursion

	return nil
}

func (rvm *RunningVM[VM]) TaskGroup() *taskgroup.TaskGroup {
	return rvm.taskGroup
}

func (rvm *RunningVM[VM]) RunInitalTimesyncRequests(ctx context.Context) error {
	slog.InfoContext(ctx, "waiting for guest service")

	connection, err := rvm.GuestService(ctx)
	if err != nil {
		return errors.Errorf("failed to get guest service: %w", err)
	}

	now := time.Now()

	slog.InfoContext(ctx, "got guest service - making time sync request to management service")

	tsreq := &runmv1.GuestTimeSyncRequest{}
	tsreq.SetUnixTimeNs(uint64(now.UnixNano()))
	// tsreq.SetTimezone(fmt.Sprintf("%s%d", zone, offset))
	response, err := connection.GuestTimeSync(ctx, tsreq)
	if err != nil {
		slog.ErrorContext(ctx, "failed to time sync", "error", err)
		return errors.Errorf("failed to time sync: %w", err)
	}
	slog.InfoContext(ctx, "time sync 1", "response", response)

	now = time.Now()
	// this first request, will take a few extra milliseconds, so we make the same call again

	tsreq.SetUnixTimeNs(uint64(now.UnixNano()))
	response, err = connection.GuestTimeSync(ctx, tsreq)
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
		grpc.ChainUnaryInterceptor(grpcerr.NewUnaryServerInterceptor(ctx)),
		grpc.ChainStreamInterceptor(grpcerr.NewStreamServerInterceptor(ctx)),
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

func bootVM[VM VirtualMachine](ctx context.Context, vm VM, tg *taskgroup.TaskGroup) error {
	bootCtx, bootCancel := context.WithCancel(ctx)
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic in bootContainerVM", "panic", r)
			panic(r)
		}
		// clean up the boot provisioners - this shouldn't throw an error because they prob are going to throw something
		bootCancel()

	}()

	go func() {
		// this is temporary, don't need to add it to the taskgroup
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

func TailConsoleLog(ctx context.Context, workingDir string, writer io.Writer) error {
	dat, err := os.ReadFile(filepath.Join(workingDir, "console.log"))
	if err != nil {
		return errors.Errorf("reading console log file: %w", err)
	}

	if writer == nil {
		return errors.Errorf("writer is nil")
	}

	for _, line := range strings.Split(string(dat), "\n") {
		fmt.Fprintf(writer, "%s\n", line)
	}

	t, err := tail.TailFile(filepath.Join(workingDir, "console.log"), tail.Config{Follow: true, Location: &tail.SeekInfo{Offset: int64(len(dat)), Whence: io.SeekStart}})
	if err != nil {
		return errors.Errorf("error tailing log file: %w", err)
	}
	for line := range t.Lines {
		fmt.Fprintf(writer, "%s\n", line.Text)
	}

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

func (rvm *RunningVM[VM]) RunVsockProxyClient(ctx context.Context, port uint32, listener net.Listener) error {

	for {
		myConn, err := listener.Accept()
		if err != nil {
			slog.ErrorContext(ctx, "vsock accept failed", "err", err)
			continue
		}

		defer myConn.Close()

		// open up
		vConn, err := rvm.vm.VSockConnect(ctx, port)
		if err != nil {
			slog.ErrorContext(ctx, "vsock listen failed", "err", err)
			return errors.Errorf("listening on vsock: %w", err)
		}
		defer vConn.Close()

		go func() {
			defer myConn.Close()
			tb, err := io.Copy(myConn, vConn)
			if err != nil {
				slog.ErrorContext(ctx, "error copying to myConn", "err", err, "port", port, "bytes", tb)
			} else {
				slog.InfoContext(ctx, "copied to myConn - closing connection", "port", port, "bytes", tb)
			}
		}()

		go func() {
			defer vConn.Close()
			tb, err := io.Copy(vConn, myConn)
			if err != nil {
				slog.ErrorContext(ctx, "error copying to vConn", "err", err, "port", port, "bytes", tb)
			} else {
				slog.InfoContext(ctx, "copied to vConn - closing connection", "port", port, "bytes", tb)
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
