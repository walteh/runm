//go:build !windows

package grpcruntime

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/conversion"
	"github.com/walteh/runm/core/runc/runtime"

	runmsocket "github.com/walteh/runm/core/runc/socket"
	runmv1 "github.com/walteh/runm/proto/v1"
)

var vsockPortCounter = atomic.Uint32{}
var _ runtime.Runtime = (*GRPCClientRuntime)(nil)

func (c *GRPCClientRuntime) SharedDir() string {
	return c.sharedDirPathPrefix
}

// Ping checks if the runc service is alive.
func (c *GRPCClientRuntime) Ping(ctx context.Context) error {
	_, err := c.runtimeGrpcService.Ping(ctx, &runmv1.PingRequest{})
	return err
}

func (c *GRPCClientRuntime) vsockDialer(ctx context.Context, port uint32) error {
	req := &runmv1.DialOpenListenerRequest{}
	req.SetListeningOn(newVsockSocketType(port))
	_, err := c.socketAllocatorGrpcService.DialOpenListener(ctx, req)
	return err
}

// NewTempConsoleSocket implements runtime.Runtime.
func (c *GRPCClientRuntime) NewTempConsoleSocket(ctx context.Context) (runtime.ConsoleSocket, error) {

	allocatedSock, sockTyp, err := c.allocateVsockSocket(ctx)
	if err != nil {
		return nil, errors.Errorf("allocating vsock socket: %w", err)
	}

	cons, err := c.socketAllocatorGrpcService.AllocateConsole(ctx, &runmv1.AllocateConsoleRequest{})
	if err != nil {
		return nil, errors.Errorf("creating temp console socket: %w", err)
	}

	slog.InfoContext(ctx, "creating console - A")

	req := &runmv1.BindConsoleToSocketRequest{}
	req.SetConsoleReferenceId(cons.GetConsoleReferenceId())
	req.SetSocketType(sockTyp)

	slog.InfoContext(ctx, "binding console to socket - A")

	// bind the two together

	_, err = c.socketAllocatorGrpcService.BindConsoleToSocket(ctx, req)
	if err != nil {
		return nil, errors.Errorf("binding console to socket: %w", err)
	}

	slog.InfoContext(ctx, "binding console to socket - B")

	closeCallbacks := []func(ctx context.Context) error{
		func(ctx context.Context) error {
			req := &runmv1.CloseConsoleRequest{}
			req.SetConsoleReferenceId(cons.GetConsoleReferenceId())
			_, err := c.socketAllocatorGrpcService.CloseConsole(ctx, req)
			if err != nil {
				slog.ErrorContext(ctx, "error closing console", "error", err)
			}
			return nil
		},
	}

	consock, err := runmsocket.NewHostUnixConsoleSocketV2(ctx, cons.GetConsoleReferenceId(), allocatedSock, closeCallbacks...)
	if err != nil {
		return nil, errors.Errorf("creating host console socket: %w", err)
	}

	c.state.StoreOpenConsole(cons.GetConsoleReferenceId(), consock)
	c.state.StoreOpenVsockConnection(sockTyp.GetVsockPort().GetPort(), allocatedSock)

	slog.InfoContext(ctx, "binding console to socket - C")

	// socket is allocated, we just have an id
	// so now we need to creater a new socket

	return consock, nil
}

// ReadPidFile implements runtime.Runtime.
func (c *GRPCClientRuntime) ReadPidFile(ctx context.Context, path string) (int, error) {
	req := &runmv1.RuncReadPidFileRequest{}
	req.SetPath(path)
	resp, err := c.runtimeGrpcService.ReadPidFile(ctx, req)
	if err != nil {
		return -1, errors.Errorf("reading pid file: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return -1, errors.New(resp.GetGoError())
	// }
	return int(resp.GetPid()), nil
}

// LogFilePath implements runtime.Runtime.
func (c *GRPCClientRuntime) LogFilePath(ctx context.Context) (string, error) {
	return filepath.Join(c.sharedDirPathPrefix, runtime.LogFileBase), nil
}

// Update implements runtime.Runtime.
func (c *GRPCClientRuntime) Update(ctx context.Context, id string, resources *specs.LinuxResources) error {
	panic("unimplemented")
}

// NewNullIO implements runtime.Runtime.
func (c *GRPCClientRuntime) NewNullIO() (runtime.IO, error) {
	return runtime.NewHostNullIo()
}

func (c *GRPCClientRuntime) listenToVsockWithDialerCallback(ctx context.Context, port uint32) (net.Conn, error) {

	dcb := func(ctx context.Context) error {
		return c.vsockDialer(ctx, port)
	}

	cz, err := c.vsockProxier.ListenAndAcceptSingleVsockConnection(ctx, port, dcb)
	if err != nil {
		return nil, errors.Errorf("listening and accepting vsock connection: %w", err)
	}
	return cz, nil
}

func newVsockSocketType(port uint32) *runmv1.SocketType {
	typ := &runmv1.SocketType{}
	vsockPort := &runmv1.VsockPort{}
	vsockPort.SetPort(port)
	typ.SetVsockPort(vsockPort)
	return typ
}

func (c *GRPCClientRuntime) allocateVsockSocket(ctx context.Context) (*runmsocket.SimpleVsockProxyConn, *runmv1.SocketType, error) {
	port := vsockPortCounter.Add(1) + 3300

	conn, err := c.listenToVsockWithDialerCallback(ctx, port)
	if err != nil {
		return nil, nil, errors.Errorf("listening to vsock: %w", err)
	}

	typ := newVsockSocketType(port)

	allocatedSock := runmsocket.NewSimpleVsockProxyConn(ctx, conn, port)

	return allocatedSock, typ, nil
}

// NewPipeIO implements runtime.Runtime.
func (c *GRPCClientRuntime) NewPipeIO(ctx context.Context, ioUID, ioGID int, opts ...gorunc.IOOpt) (runtime.IO, error) {

	ropts := gorunc.IOOption{}
	for _, opt := range opts {
		opt(&ropts)
	}

	count := 0
	if ropts.OpenStderr {
		count++
	}
	if ropts.OpenStdout {
		count++
	}
	if ropts.OpenStdin {
		count++
	}

	if count == 0 {
		return nil, errors.New("no sockets to allocate")
	}

	refs := make([]*runmv1.SocketType, count)
	allocatedSockets := make(map[uint32]runtime.AllocatedSocket, count)

	for i := 0; i < count; i++ {

		allocatedSock, typ, err := c.allocateVsockSocket(ctx)
		if err != nil {
			return nil, errors.Errorf("allocating vsock socket: %w", err)
		}

		refs[i] = typ
		allocatedSockets[typ.GetVsockPort().GetPort()] = allocatedSock
	}

	ioReq := &runmv1.AllocateIORequest{}
	ioReq.SetOpenStdin(ropts.OpenStdin)
	ioReq.SetOpenStdout(ropts.OpenStdout)
	ioReq.SetOpenStderr(ropts.OpenStderr)
	ioReq.SetIoUid(int32(ioUID))
	ioReq.SetIoGid(int32(ioGID))

	sock, err := c.socketAllocatorGrpcService.AllocateIO(ctx, ioReq)
	if err != nil {
		slog.ErrorContext(ctx, "error allocating IO", "error", err)
		return nil, errors.Errorf("allocating IO: %w", err)
	}

	count2 := 0

	bindReq := &runmv1.BindIOToSocketsRequest{}
	bindReq.SetIoReferenceId(sock.GetIoReferenceId())

	if ropts.OpenStdin {
		slog.InfoContext(ctx, "allocating stdin socket", "socket_id", refs[count2])
		bindReq.SetStdinSocket(refs[count2])
		count2++
	}
	if ropts.OpenStdout {
		slog.InfoContext(ctx, "allocating stdout socket", "socket_id", refs[count2])
		bindReq.SetStdoutSocket(refs[count2])
		count2++
	}
	if ropts.OpenStderr {
		slog.InfoContext(ctx, "allocating stderr socket", "socket_id", refs[count2])
		bindReq.SetStderrSocket(refs[count2])

	}

	slog.InfoContext(ctx, "binding IO to sockets", "io_id", sock.GetIoReferenceId(), "stdin_id", bindReq.GetStdinSocket(), "stdout_id", bindReq.GetStdoutSocket(), "stderr_id", bindReq.GetStderrSocket())

	slog.InfoContext(ctx, "binding IO to sockets - A")
	_, err = c.socketAllocatorGrpcService.BindIOToSockets(ctx, bindReq)
	if err != nil {
		slog.ErrorContext(ctx, "error binding IO to sockets", "error", err)
		return nil, errors.Errorf("binding IO to sockets: %w", err)
	}

	var stdinRef, stdoutRef, stderrRef *runmv1.SocketType
	if ropts.OpenStdin {
		slog.InfoContext(ctx, "allocating stdin socket", "socket_id", bindReq.GetStdinSocket())
		stdinRef = bindReq.GetStdinSocket()
	}
	if ropts.OpenStdout {
		slog.InfoContext(ctx, "allocating stdout socket", "socket_id", bindReq.GetStdoutSocket())
		stdoutRef = bindReq.GetStdoutSocket()
	}
	if ropts.OpenStderr {
		slog.InfoContext(ctx, "allocating stderr socket", "socket_id", bindReq.GetStderrSocket())
		stderrRef = bindReq.GetStderrSocket()
	}

	var stdinAllocated, stdoutAllocated, stderrAllocated runtime.AllocatedSocket

	if stdinRef != nil {
		stdinAllocated = allocatedSockets[stdinRef.GetVsockPort().GetPort()]
	}

	if stdoutRef != nil {
		stdoutAllocated = allocatedSockets[stdoutRef.GetVsockPort().GetPort()]
	}

	if stderrRef != nil {
		stderrAllocated = allocatedSockets[stderrRef.GetVsockPort().GetPort()]
	}

	extraClosers := []func(ctx context.Context) error{
		func(ctx context.Context) error {
			req := &runmv1.CloseIORequest{}
			req.SetIoReferenceId(sock.GetIoReferenceId())
			_, err := c.socketAllocatorGrpcService.CloseIO(ctx, req)
			if err != nil {
				slog.ErrorContext(ctx, "error closing IO", "error", err)
			}
			return err
		},
	}

	ioz := runtime.NewHostAllocatedStdio(ctx, sock.GetIoReferenceId(), stdinAllocated, stdoutAllocated, stderrAllocated, extraClosers...)

	return ioz, nil
}

// Create creates a new container.
func (c *GRPCClientRuntime) Create(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) error {
	conv, err := conversion.ConvertCreateOptsToProto(ctx, options)
	if err != nil {
		return errors.Errorf("converting create opts to proto: %w", err)
	}

	req := &runmv1.RuncCreateRequest{}
	req.SetId(id)
	req.SetBundle(bundle)
	req.SetOptions(conv)

	// cat the contents of the pid file
	// pid, err := os.ReadDir(filepath.Dir(conv.GetPidFile()))
	// if err != nil {
	// 	return errors.Errorf("reading pid file: %w", err)
	// }
	// for _, p := range pid {
	// 	fmt.Printf("pid file contents %s\n", p.Name())
	// }

	slog.InfoContext(ctx, "creating container", "id", id, "bundle", bundle)

	_, err = c.runtimeGrpcService.Create(ctx, req)
	if err != nil {
		return errors.Errorf("creating container: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return errors.Errorf("go error in create: %s", resp.GetGoError())
	// }
	return nil
}

// Start starts an already created container.
func (c *GRPCClientRuntime) Start(ctx context.Context, id string) error {
	req := &runmv1.RuncStartRequest{}
	req.SetId(id)

	_, err := c.runtimeGrpcService.Start(ctx, req)
	if err != nil {
		return errors.Errorf("starting container: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return errors.Errorf("go error in start: %s", resp.GetGoError())
	// }
	return nil
}

// Delete deletes a container.
func (c *GRPCClientRuntime) Delete(ctx context.Context, id string, opts *gorunc.DeleteOpts) error {
	req := &runmv1.RuncDeleteRequest{}
	req.SetId(id)
	req.SetOptions(conversion.ConvertDeleteOptsToProto(opts))

	_, err := c.runtimeGrpcService.Delete(ctx, req)
	if err != nil {
		return errors.Errorf("deleting container: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return errors.Errorf("go error in start: %s", resp.GetGoError())
	// }
	return nil
}

// Kill sends the specified signal to the container.
func (c *GRPCClientRuntime) Kill(ctx context.Context, id string, signal int, opts *gorunc.KillOpts) error {
	req := &runmv1.RuncKillRequest{}
	req.SetId(id)
	req.SetSignal(int32(signal))
	req.SetOptions(conversion.ConvertKillOptsToProto(opts))

	_, err := c.runtimeGrpcService.Kill(ctx, req)
	if err != nil {
		if strings.Contains(err.Error(), "failed to write client preface") {
			// this is expected if the container is already dead
			return nil
		}
		return errors.Errorf("killing container: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return errors.Errorf("go error in kill: %s", resp.GetGoError())
	// }
	return nil
}

// Pause pauses the container with the provided id.
func (c *GRPCClientRuntime) Pause(ctx context.Context, id string) error {
	req := &runmv1.RuncPauseRequest{}
	req.SetId(id)

	_, err := c.runtimeGrpcService.Pause(ctx, req)
	if err != nil {
		return errors.Errorf("pausing container: %w", err)
	}
	return nil
}

// Resume resumes the container with the provided id.
func (c *GRPCClientRuntime) Resume(ctx context.Context, id string) error {
	req := &runmv1.RuncResumeRequest{}
	req.SetId(id)

	_, err := c.runtimeGrpcService.Resume(ctx, req)
	if err != nil {
		return errors.Errorf("resuming container: %w", err)
	}
	return nil
}

// Ps lists all the processes inside the container returning their pids.
func (c *GRPCClientRuntime) Ps(ctx context.Context, id string) ([]int, error) {
	req := &runmv1.RuncPsRequest{}
	req.SetId(id)

	resp, err := c.runtimeGrpcService.Ps(ctx, req)
	if err != nil {
		return nil, errors.Errorf("ps: %w", err)
	}
	pids := make([]int, len(resp.GetPids()))
	for i, pid := range resp.GetPids() {
		pids[i] = int(pid)
	}
	return pids, nil
}

// Exec executes an additional process inside the container.
func (c *GRPCClientRuntime) Exec(ctx context.Context, id string, spec specs.Process, options *gorunc.ExecOpts) error {
	req := &runmv1.RuncExecRequest{}
	req.SetId(id)

	specOut, err := conversion.ConvertProcessSpecToProto(&spec)
	if err != nil {
		return errors.Errorf("converting process spec to proto: %w", err)
	}
	req.SetSpec(specOut)

	execOpts, err := conversion.ConvertExecOptsToProto(options)
	if err != nil {
		return errors.Errorf("converting exec opts to proto: %w", err)
	}
	req.SetOptions(execOpts)

	_, err = c.runtimeGrpcService.Exec(ctx, req)
	if err != nil {
		return errors.Errorf("exec: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return errors.Errorf("go error in exec: %s", resp.GetGoError())
	// }
	return nil
}

func (c *GRPCClientRuntime) Checkpoint(ctx context.Context, id string, options *gorunc.CheckpointOpts, actions ...gorunc.CheckpointAction) error {
	req := &runmv1.RuncCheckpointRequest{}
	req.SetId(id)
	req.SetOptions(conversion.ConvertCheckpointOptsToProto(options))
	req.SetActions(conversion.ConvertCheckpointActionsToProto(actions...))

	_, err := c.runtimeGrpcService.Checkpoint(ctx, req)
	if err != nil {
		return errors.Errorf("checkpoint: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return errors.Errorf("go error in checkpoint: %s", resp.GetGoError())
	// }
	return nil
}

func (c *GRPCClientRuntime) Restore(ctx context.Context, id, bundle string, options *gorunc.RestoreOpts) (int, error) {
	req := &runmv1.RuncRestoreRequest{}
	req.SetId(id)
	req.SetBundle(bundle)
	req.SetOptions(conversion.ConvertRestoreOptsToProto(options))

	resp, err := c.runtimeGrpcService.Restore(ctx, req)
	if err != nil {
		return -1, errors.Errorf("restore: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return -1, errors.Errorf("go error in restore: %s", resp.GetGoError())
	// }
	return int(resp.GetStatus()), nil
}

func (c *GRPCClientRuntime) SubscribeToReaperExits(ctx context.Context) (<-chan gorunc.Exit, error) {
	ch := make(chan gorunc.Exit, 32)
	srv, err := c.eventService.SubscribeToReaperExits(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, errors.Errorf("subscribing to reaper exits: %w", err)
	}
	go func() {
		for {
			exit, err := srv.Recv()
			if err != nil {
				grpcErr := status.Convert(err)
				if errors.As(err, &io.EOF) {
					slog.DebugContext(ctx, "reaper exit channel closed")
					return
				}

				slog.ErrorContext(ctx, "reaper exit channel closed with error", "err", grpcErr, "code", grpcErr.Code())
				return
			}
			slog.InfoContext(ctx, "reaper exit", "pid", exit.GetPid(), "status", exit.GetStatus())
			ch <- gorunc.Exit{
				Timestamp: exit.GetTimestamp().AsTime(),
				Pid:       int(exit.GetPid()),
				Status:    int(exit.GetStatus()),
			}

		}
	}()

	return ch, nil
}
