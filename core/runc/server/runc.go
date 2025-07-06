//go:build !windows

package server

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kr/pty"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/conversion"
	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.RuncServiceServer = (*Server)(nil)

// Ping implements the RuncServiceServer Ping method.
func (s *Server) Ping(ctx context.Context, req *runmv1.PingRequest) (*runmv1.PingResponse, error) {
	return &runmv1.PingResponse{}, nil
}

func simulatePty(ctx context.Context, sock string) error {

	time.Sleep(1 * time.Second)
	master, slave, err := pty.Open()
	if err != nil {
		slog.Error("open pty", "error", err)
		return err
	}
	defer master.Close()
	defer slave.Close()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		slog.Error("dial socket", "path", sock, "error", err)
		return err
	}
	defer conn.Close()

	// Build the control message carrying our master FD
	rights := unix.UnixRights(int(master.Fd()))
	n, oobn, err := conn.(*net.UnixConn).
		WriteMsgUnix(nil, rights, nil)
	slog.Info("sent FD", "socket", sock, "n", n, "oobn", oobn, "error", err)
	<-ctx.Done()
	return nil
}

func (s *Server) NewTempConsoleSocket(ctx context.Context, req *runmv1.RuncNewTempConsoleSocketRequest) (*runmv1.RuncNewTempConsoleSocketResponse, error) {

	slog.InfoContext(ctx, "new temp console socket - A")

	socket, err := s.runtime.NewTempConsoleSocket(ctx)
	if err != nil {
		return nil, errors.Errorf("failed to create temp console socket: %w", err)
	}

	referenceId := runtime.NewConsoleReferenceId()
	s.state.StoreOpenConsole(referenceId, socket)

	// go simulatePty(ctx, socket.Path())

	resp := &runmv1.RuncNewTempConsoleSocketResponse{}
	resp.SetConsoleReferenceId(referenceId)
	return resp, nil
}

func (s *Server) ReadPidFile(ctx context.Context, req *runmv1.RuncReadPidFileRequest) (*runmv1.RuncReadPidFileResponse, error) {
	resp := &runmv1.RuncReadPidFileResponse{}

	pid, err := s.runtime.ReadPidFile(ctx, req.GetPath())
	if err != nil {
		return nil, errors.Errorf("failed to read pid file: %w", err)
	}
	resp.SetPid(int32(pid))
	return resp, nil
}

// Create implements the RuncServiceServer Create method.
func (s *Server) Create(ctx context.Context, req *runmv1.RuncCreateRequest) (*runmv1.RuncCreateResponse, error) {
	resp := &runmv1.RuncCreateResponse{}

	opts, err := conversion.ConvertCreateOptsFromProto(ctx, req.GetOptions(), s.state)
	if err != nil {
		return nil, errors.Errorf("failed to convert create opts: %w", err)
	}

	// if opts.IO != nil {
	// 	opts.IO = runtime.NewIoLogProxy(opts.IO, logging.GetDefaultLogWriter())
	// }

	slog.Info("creating container",
		"id", req.GetId(),
		"bundle", req.GetBundle(),
		"detach", opts.Detach,
		"opts", opts,
	)

	err = s.runtime.Create(ctx, req.GetId(), req.GetBundle(), opts)
	if err != nil {
		return nil, errors.Errorf("failed to create container: %w", err)
	}
	return resp, nil
}

// Start implements the RuncServiceServer Start method.
func (s *Server) Start(ctx context.Context, req *runmv1.RuncStartRequest) (*runmv1.RuncStartResponse, error) {
	resp := &runmv1.RuncStartResponse{}

	err := s.runtime.Start(ctx, req.GetId())
	if err != nil {
		return nil, errors.Errorf("starting container: %w", err)
	}
	return resp, nil
}

// Run implements the RuncServiceServer Run method.

// Delete implements the RuncServiceServer Delete method.
func (s *Server) Delete(ctx context.Context, req *runmv1.RuncDeleteRequest) (*runmv1.RuncDeleteResponse, error) {
	resp := &runmv1.RuncDeleteResponse{}

	opts := conversion.ConvertDeleteOptsFromProto(req.GetOptions())

	err := s.runtime.Delete(ctx, req.GetId(), opts)
	if err != nil {
		return nil, errors.Errorf("deleting container: %w", err)
	}
	return resp, nil
}

// Kill implements the RuncServiceServer Kill method.
func (s *Server) Kill(ctx context.Context, req *runmv1.RuncKillRequest) (*runmv1.RuncKillResponse, error) {
	resp := &runmv1.RuncKillResponse{}

	opts := conversion.ConvertKillOptsFromProto(req.GetOptions())

	err := s.runtime.Kill(ctx, req.GetId(), int(req.GetSignal()), opts)
	if err != nil {
		if strings.HasSuffix(err.Error(), "container not running") {
			return resp, nil
		}
		return nil, errors.Errorf("killing container: %w", err)
	}
	return resp, nil
}

// Pause implements the RuncServiceServer Pause method.
func (s *Server) Pause(ctx context.Context, req *runmv1.RuncPauseRequest) (*runmv1.RuncPauseResponse, error) {
	resp := &runmv1.RuncPauseResponse{}

	err := s.runtime.Pause(ctx, req.GetId())
	if err != nil {
		return nil, errors.Errorf("pausing container: %w", err)
	}
	return resp, nil
}

// Resume implements the RuncServiceServer Resume method.
func (s *Server) Resume(ctx context.Context, req *runmv1.RuncResumeRequest) (*runmv1.RuncResumeResponse, error) {
	resp := &runmv1.RuncResumeResponse{}

	err := s.runtime.Resume(ctx, req.GetId())
	if err != nil {
		return nil, errors.Errorf("resuming container: %w", err)
	}
	return resp, nil
}

// Ps implements the RuncServiceServer Ps method.
func (s *Server) Ps(ctx context.Context, req *runmv1.RuncPsRequest) (*runmv1.RuncPsResponse, error) {
	resp := &runmv1.RuncPsResponse{}

	pids, err := s.runtime.Ps(ctx, req.GetId())
	if err != nil {
		return nil, errors.Errorf("ps: %w", err)
	}

	pidsList := make([]int32, len(pids))
	for i, pid := range pids {
		pidsList[i] = int32(pid)
	}

	resp.SetPids(pidsList)
	return resp, nil
}

// Exec implements the RuncServiceServer Exec method.
func (s *Server) Exec(ctx context.Context, req *runmv1.RuncExecRequest) (*runmv1.RuncExecResponse, error) {
	resp := &runmv1.RuncExecResponse{}

	// d := false
	// defer func() {
	// 	d = true
	// }()
	// go func() {
	// 	for {
	// 		time.Sleep(10 * time.Second)
	// 		if d {
	// 			return
	// 		}
	// 		s.state.LogCurrentStateReport(ctx)
	// 	}
	// }()

	if req.GetSpec() == nil {
		return nil, errors.Errorf("spec is required")
	}

	processSpec, err := conversion.ConvertProcessSpecFromProto(req.GetSpec())
	if err != nil {
		return nil, err
	}

	opts, err := conversion.ConvertExecOptsFromProto(req.GetOptions(), s.state)
	if err != nil {
		return nil, err
	}

	err = s.runtime.Exec(ctx, req.GetId(), *processSpec, opts)
	if err != nil {
		return nil, errors.Errorf("exec: %w", err)
	}

	return resp, nil
}

// Checkpoint implements runmv1.RuncServiceServer.
func (s *Server) Checkpoint(context.Context, *runmv1.RuncCheckpointRequest) (*runmv1.RuncCheckpointResponse, error) {
	return nil, runtime.ReflectNotImplementedError()
}

// Restore implements runmv1.RuncServiceServer.
func (s *Server) Restore(context.Context, *runmv1.RuncRestoreRequest) (*runmv1.RuncRestoreResponse, error) {
	return nil, runtime.ReflectNotImplementedError()
}

// Update implements runmv1.RuncServiceServer.
func (s *Server) Update(context.Context, *runmv1.RuncUpdateRequest) (*runmv1.RuncUpdateResponse, error) {
	return nil, runtime.ReflectNotImplementedError()
}
