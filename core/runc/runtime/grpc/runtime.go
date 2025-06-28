package grpcruntime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/conversion"
	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/socket"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runtime.Runtime = (*GRPCClientRuntime)(nil)

func (c *GRPCClientRuntime) SharedDir() string {
	return c.sharedDirPathPrefix
}

// Ping checks if the runc service is alive.
func (c *GRPCClientRuntime) Ping(ctx context.Context) error {
	_, err := c.runtimeGrpcService.Ping(ctx, &runmv1.PingRequest{})
	return err
}

// NewTempConsoleSocket implements runtime.Runtime.
func (c *GRPCClientRuntime) NewTempConsoleSocket(ctx context.Context) (runtime.ConsoleSocket, error) {

	sock, err := c.socketAllocatorGrpcService.AllocateSocketStream(ctx, &runmv1.AllocateSocketStreamRequest{})
	if err != nil {
		return nil, errors.Errorf("allocating socket stream: %w", err)
	}

	refId, err := sock.Recv()
	if err != nil {
		return nil, errors.Errorf("receiving socket reference id: %w", err)
	}

	hsock, err := socket.NewHostAllocatedSocketFromId(ctx, refId.GetSocketReferenceId(), c.vsockProxier)
	if err != nil {
		return nil, errors.Errorf("allocating host allocated socket: %w", err)
	}

	ready := make(chan error)
	go func() {
		slog.InfoContext(ctx, "waiting for socket to be ready - A")
		if err := hsock.Ready(); err != nil {
			ready <- err
			return
		}
		slog.InfoContext(ctx, "socket is ready - B")
		if err := sock.CloseSend(); err != nil {
			ready <- err
			return
		}
		slog.InfoContext(ctx, "socket is ready - C")
		ready <- nil
	}()

	select {
	case <-sock.Context().Done():
		return nil, errors.Errorf("context done before socket was ready: %w", sock.Context().Err())
	case <-time.After(10 * time.Second):
		return nil, errors.Errorf("timeout waiting for socket to be ready")
	case err := <-ready:
		if err != nil {
			return nil, errors.Errorf("socket not ready: %w", err)
		}
	}
	slog.InfoContext(ctx, "socket is ready - D")

	cons, err := c.runtimeGrpcService.NewTempConsoleSocket(ctx, &runmv1.RuncNewTempConsoleSocketRequest{})
	if err != nil {
		return nil, err
	}
	if cons.GetGoError() != "" {
		return nil, errors.New(cons.GetGoError())
	}

	slog.InfoContext(ctx, "creating console - A")

	req := &runmv1.BindConsoleToSocketRequest{}
	req.SetConsoleReferenceId(cons.GetConsoleReferenceId())
	req.SetSocketReferenceId(refId.GetSocketReferenceId())

	slog.InfoContext(ctx, "binding console to socket - A")

	// bind the two together

	_, err = c.socketAllocatorGrpcService.BindConsoleToSocket(ctx, req)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "binding console to socket - B")

	consock, err := socket.NewHostConsoleSocket(ctx, hsock, c.vsockProxier)
	if err != nil {
		return nil, err
	}

	c.state.StoreOpenConsole(cons.GetConsoleReferenceId(), consock)
	c.state.StoreOpenSocket(refId.GetSocketReferenceId(), hsock)

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
		return -1, err
	}
	if resp.GetGoError() != "" {
		return -1, errors.New(resp.GetGoError())
	}
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

	req := &runmv1.AllocateSocketsRequest{}
	req.SetCount(uint32(count))

	slog.InfoContext(ctx, "allocating sockets", "count", count)

	iov, err := c.socketAllocatorGrpcService.AllocateSockets(ctx, req)
	if err != nil {
		return nil, errors.Errorf("allocating sockets: %w", err)
	}

	ioReq := &runmv1.AllocateIORequest{}
	ioReq.SetOpenStdin(ropts.OpenStdin)
	ioReq.SetOpenStdout(ropts.OpenStdout)
	ioReq.SetOpenStderr(ropts.OpenStderr)

	sock, err := c.socketAllocatorGrpcService.AllocateIO(ctx, ioReq)
	if err != nil {
		slog.ErrorContext(ctx, "error allocating IO", "error", err)
		return nil, errors.Errorf("allocating IO: %w", err)
	}

	slog.InfoContext(ctx, "all sockets allocated", "list", iov.GetSocketReferenceIds())

	count2 := 0

	bindReq := &runmv1.BindIOToSocketsRequest{}
	bindReq.SetIoReferenceId(sock.GetIoReferenceId())

	if ropts.OpenStdin {
		slog.InfoContext(ctx, "allocating stdin socket", "socket_id", iov.GetSocketReferenceIds()[count2])
		bindReq.SetStdinSocketReferenceId(iov.GetSocketReferenceIds()[count2])
		count2++
	}
	if ropts.OpenStdout {
		slog.InfoContext(ctx, "allocating stdout socket", "socket_id", iov.GetSocketReferenceIds()[count2])
		bindReq.SetStdoutSocketReferenceId(iov.GetSocketReferenceIds()[count2])
		count2++
	}
	if ropts.OpenStderr {
		slog.InfoContext(ctx, "allocating stderr socket", "socket_id", iov.GetSocketReferenceIds()[count2])
		bindReq.SetStderrSocketReferenceId(iov.GetSocketReferenceIds()[count2])

	}

	slog.InfoContext(ctx, "binding IO to sockets", "io_id", sock.GetIoReferenceId(), "stdin_id", bindReq.GetStdinSocketReferenceId(), "stdout_id", bindReq.GetStdoutSocketReferenceId(), "stderr_id", bindReq.GetStderrSocketReferenceId())

	slog.InfoContext(ctx, "binding IO to sockets - A")
	_, err = c.socketAllocatorGrpcService.BindIOToSockets(ctx, bindReq)
	if err != nil {
		slog.ErrorContext(ctx, "error binding IO to sockets", "error", err)
		return nil, errors.Errorf("binding IO to sockets: %w", err)
	}

	var stdinRef, stdoutRef, stderrRef string

	if ropts.OpenStdin {
		slog.InfoContext(ctx, "allocating stdin socket", "socket_id", bindReq.GetStdinSocketReferenceId())
		stdinRef = bindReq.GetStdinSocketReferenceId()
	}
	if ropts.OpenStdout {
		slog.InfoContext(ctx, "allocating stdout socket", "socket_id", bindReq.GetStdoutSocketReferenceId())
		stdoutRef = bindReq.GetStdoutSocketReferenceId()
	}
	if ropts.OpenStderr {
		slog.InfoContext(ctx, "allocating stderr socket", "socket_id", bindReq.GetStderrSocketReferenceId())
		stderrRef = bindReq.GetStderrSocketReferenceId()
	}

	var stdinAllocated, stdoutAllocated, stderrAllocated runtime.AllocatedSocket

	if stdinRef != "" {
		stdinAllocated, err = socket.NewHostAllocatedSocketFromId(ctx, stdinRef, c.vsockProxier)
		if err != nil {
			return nil, errors.Errorf("allocating stdin socket: %w", err)
		}
	}

	if stdoutRef != "" {
		stdoutAllocated, err = socket.NewHostAllocatedSocketFromId(ctx, stdoutRef, c.vsockProxier)
		if err != nil {
			return nil, errors.Errorf("allocating stdout socket: %w", err)
		}
	}

	if stderrRef != "" {
		stderrAllocated, err = socket.NewHostAllocatedSocketFromId(ctx, stderrRef, c.vsockProxier)
		if err != nil {
			return nil, errors.Errorf("allocating stderr socket: %w", err)
		}
	}

	ioz := runtime.NewHostAllocatedStdio(ctx, sock.GetIoReferenceId(), stdinAllocated, stdoutAllocated, stderrAllocated)

	return ioz, nil
}

// Create creates a new container.
func (c *GRPCClientRuntime) Create(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) error {
	conv, err := conversion.ConvertCreateOptsToProto(ctx, options)
	if err != nil {
		return errors.Errorf("creating container - %T: %w", err, err)
	}

	req := &runmv1.RuncCreateRequest{}
	req.SetId(id)
	req.SetBundle(bundle)
	req.SetOptions(conv)

	// cat the contents of the pid file
	pid, err := os.ReadDir(filepath.Dir(conv.GetPidFile()))
	if err != nil {
		return errors.Errorf("reading pid file: %w", err)
	}
	for _, p := range pid {
		fmt.Printf("pid file contents %s\n", p.Name())
	}

	slog.InfoContext(ctx, "creating container", "id", id, "bundle", bundle)

	resp, err := c.runtimeGrpcService.Create(ctx, req)
	if err != nil {
		return errors.Errorf("creating container - %T: %w", err, err)
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

// Start starts an already created container.
func (c *GRPCClientRuntime) Start(ctx context.Context, id string) error {
	req := &runmv1.RuncStartRequest{}
	req.SetId(id)

	resp, err := c.runtimeGrpcService.Start(ctx, req)
	if err != nil {
		return err
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

// Delete deletes a container.
func (c *GRPCClientRuntime) Delete(ctx context.Context, id string, opts *gorunc.DeleteOpts) error {
	req := &runmv1.RuncDeleteRequest{}
	req.SetId(id)
	req.SetOptions(conversion.ConvertDeleteOptsToProto(opts))

	resp, err := c.runtimeGrpcService.Delete(ctx, req)
	if err != nil {
		return err
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

// Kill sends the specified signal to the container.
func (c *GRPCClientRuntime) Kill(ctx context.Context, id string, signal int, opts *gorunc.KillOpts) error {
	req := &runmv1.RuncKillRequest{}
	req.SetId(id)
	req.SetSignal(int32(signal))
	req.SetOptions(conversion.ConvertKillOptsToProto(opts))

	resp, err := c.runtimeGrpcService.Kill(ctx, req)
	if err != nil {
		return err
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

// Pause pauses the container with the provided id.
func (c *GRPCClientRuntime) Pause(ctx context.Context, id string) error {
	req := &runmv1.RuncPauseRequest{}
	req.SetId(id)

	resp, err := c.runtimeGrpcService.Pause(ctx, req)
	if err != nil {
		return err
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

// Resume resumes the container with the provided id.
func (c *GRPCClientRuntime) Resume(ctx context.Context, id string) error {
	req := &runmv1.RuncResumeRequest{}
	req.SetId(id)

	resp, err := c.runtimeGrpcService.Resume(ctx, req)
	if err != nil {
		return err
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

// Ps lists all the processes inside the container returning their pids.
func (c *GRPCClientRuntime) Ps(ctx context.Context, id string) ([]int, error) {
	req := &runmv1.RuncPsRequest{}
	req.SetId(id)

	resp, err := c.runtimeGrpcService.Ps(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.GetGoError() != "" {
		return nil, errors.New(resp.GetGoError())
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
		return err
	}
	req.SetSpec(specOut)

	req.SetOptions(conversion.ConvertExecOptsToProto(options))

	resp, err := c.runtimeGrpcService.Exec(ctx, req)
	if err != nil {
		return err
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

func (c *GRPCClientRuntime) Checkpoint(ctx context.Context, id string, options *gorunc.CheckpointOpts, actions ...gorunc.CheckpointAction) error {
	req := &runmv1.RuncCheckpointRequest{}
	req.SetId(id)
	req.SetOptions(conversion.ConvertCheckpointOptsToProto(options))
	req.SetActions(conversion.ConvertCheckpointActionsToProto(actions...))

	resp, err := c.runtimeGrpcService.Checkpoint(ctx, req)
	if err != nil {
		return err
	}
	if resp.GetGoError() != "" {
		return errors.New(resp.GetGoError())
	}
	return nil
}

func (c *GRPCClientRuntime) Restore(ctx context.Context, id, bundle string, options *gorunc.RestoreOpts) (int, error) {
	req := &runmv1.RuncRestoreRequest{}
	req.SetId(id)
	req.SetBundle(bundle)
	req.SetOptions(conversion.ConvertRestoreOptsToProto(options))

	resp, err := c.runtimeGrpcService.Restore(ctx, req)
	if err != nil {
		return -1, err
	}
	if resp.GetGoError() != "" {
		return -1, errors.New(resp.GetGoError())
	}
	return int(resp.GetStatus()), nil
}
