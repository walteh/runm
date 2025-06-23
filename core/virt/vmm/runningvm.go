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
	"github.com/walteh/runm/core/gvnet"
	grpcruntime "github.com/walteh/runm/core/runc/runtime/grpc"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
	runmv1 "github.com/walteh/runm/proto/v1"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RunningVM[VM VirtualMachine] struct {
	// streamExecReady bool
	// manager                *VSockManager
	runtime    *grpcruntime.GRPCClientRuntime
	bootloader virtio.Bootloader

	// streamexec   *streamexec.Client
	portOnHostIP uint16
	wait         chan error
	vm           VM
	netdev       gvnet.Proxy
	workingDir   string
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	// connStatus      <-chan VSockManagerState
	start time.Time
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
			slog.InfoContext(ctx, "connecting to vsock", "port", constants.RunmVsockPort)
			conn, err := r.vm.VSockConnect(ctx, uint32(constants.RunmVsockPort))
			if err != nil {
				lastError = err
				continue
			}
			grpcConn, err := grpc.NewClient("passthrough:target",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
					slog.InfoContext(ctx, "dialing vsock", "port", constants.RunmVsockPort, "ignored_addr", addr)
					return conn, nil
				}),
			)
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

func (r *RunningVM[VM]) ProxyVsock(ctx context.Context, port uint32) (*net.UnixConn, string, error) {
	conn, err := r.vm.VSockConnect(ctx, uint32(port))
	if err != nil {
		return nil, "", errors.Errorf("connecting to vsock: %w", err)
	}

	unixSocketPath := filepath.Join(r.workingDir, fmt.Sprintf("vsock-%d.sock", port))

	// open a unix socket to the connection in a working directory
	unixConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: unixSocketPath, Net: "unix"})
	if err != nil {
		return nil, "", err
	}

	// copy the connection to the unix socket
	go func() {
		_, err := io.Copy(unixConn, conn)
		if err != nil {
			slog.ErrorContext(ctx, "error copying connection to unix socket", "error", err)
		}
	}()

	go func() {
		_, err := io.Copy(conn, unixConn)
		if err != nil {
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

	errgrp, _ := errgroup.WithContext(ctx)

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
		err = rvm.VM().ServeBackgroundTasks(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "error serving background tasks", "error", err)
			return errors.Errorf("serving background tasks: %w", err)
		}
		return nil
	})

	// errgrp.Go(func() error {
	// 	err = rvm.ForwardStdio(ctx, rvm.stdin, rvm.stdout, rvm.stderr)
	// 	if err != nil {
	// 		slog.ErrorContext(ctx, "error forwarding stdio", "error", err)
	// 		return errors.Errorf("forwarding stdio: %w", err)
	// 	}
	// 	slog.WarnContext(ctx, "forwarding stdio done")
	// 	return nil
	// })

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

	slog.InfoContext(ctx, "time sync", "response", response)

	return nil
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

	writer := logging.GetDefaultLogWriter()

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

	writer := logging.GetDefaultLogWriter()

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
