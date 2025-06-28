package server

import (
	"context"
	"log/slog"
	"time"

	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/socket"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.SocketAllocatorServiceServer = (*Server)(nil)

func (s *Server) AllocateSocketStream(req *runmv1.AllocateSocketStreamRequest, stream runmv1.SocketAllocatorService_AllocateSocketStreamServer) error {
	as, err := s.socketAllocator.AllocateSocket(stream.Context())
	if err != nil {
		return err
	}

	referenceId := runtime.NewSocketReferenceId(as)

	res := &runmv1.AllocateSocketStreamResponse{}
	res.SetSocketReferenceId(referenceId)
	if err := stream.Send(res); err != nil {
		return err
	}

	ready := make(chan error)
	go func() {
		ready <- as.Ready()
	}()

	select {
	case <-stream.Context().Done():
		return errors.Errorf("context done before socket was ready: %w", stream.Context().Err())
	case <-time.After(10 * time.Second):
		return errors.Errorf("timeout waiting for socket to be ready")
	case err := <-ready:
		if err != nil {
			return errors.Errorf("socket not ready: %w", err)
		}
		s.state.StoreOpenSocket(referenceId, as)
		return nil
	}
}

func (s *Server) AllocateConsole(ctx context.Context, req *runmv1.AllocateConsoleRequest) (*runmv1.AllocateConsoleResponse, error) {
	referenceId := runtime.NewConsoleReferenceId()
	cs, err := s.runtime.NewTempConsoleSocket(ctx)
	if err != nil {
		return nil, errors.Errorf("failed to allocate console: %w", err)
	}
	s.state.StoreOpenConsole(referenceId, cs)
	res := &runmv1.AllocateConsoleResponse{}
	res.SetConsoleReferenceId(referenceId)
	return res, nil
}

func (s *Server) AllocateIO(ctx context.Context, req *runmv1.AllocateIORequest) (*runmv1.AllocateIOResponse, error) {
	ioref := runtime.NewIoReferenceId()
	pio, err := s.runtime.NewPipeIO(ctx, 0, 0)
	if err != nil {
		return nil, errors.Errorf("failed to allocate io: %w", err)
	}
	s.state.StoreOpenIO(ioref, pio)
	res := &runmv1.AllocateIOResponse{}
	res.SetIoReferenceId(ioref)

	return res, nil
}

// AllocateSocket implements runmv1.SocketAllocatorServiceServer.
func (s *Server) AllocateSocket(ctx context.Context, req *runmv1.AllocateSocketRequest) (*runmv1.AllocateSocketResponse, error) {
	as, err := s.socketAllocator.AllocateSocket(ctx)
	if err != nil {
		return nil, errors.Errorf("failed to allocate socket: %w", err)
	}

	referenceId := runtime.NewSocketReferenceId(as)
	s.state.StoreOpenSocket(referenceId, as)

	res := &runmv1.AllocateSocketResponse{}
	res.SetSocketReferenceId(referenceId)
	return res, nil
}

// AllocateSockets implements runmv1.SocketAllocatorServiceServer.
func (s *Server) AllocateSockets(ctx context.Context, req *runmv1.AllocateSocketsRequest) (*runmv1.AllocateSocketsResponse, error) {
	socksToClean := make([]runtime.AllocatedSocket, 0, req.GetCount())
	defer func() {
		if len(socksToClean) == 0 {
			return
		}
		for _, sock := range socksToClean {
			sock.Close()
			s.state.DeleteOpenSocket(runtime.NewSocketReferenceId(sock))
		}
	}()

	for i := 0; i < int(req.GetCount()); i++ {
		as, err := s.socketAllocator.AllocateSocket(ctx)
		if err != nil {
			return nil, errors.Errorf("failed to allocate socket: %w", err)
		}
		socksToClean = append(socksToClean, as)
	}

	res := &runmv1.AllocateSocketsResponse{}
	refs := make([]string, 0, req.GetCount())
	for _, sock := range socksToClean {
		referenceId := runtime.NewSocketReferenceId(sock)
		s.state.StoreOpenSocket(referenceId, sock)
		refs = append(refs, referenceId)
		slog.InfoContext(ctx, "allocated socketz", "reference_id", referenceId)
	}
	res.SetSocketReferenceIds(refs)

	socksToClean = nil

	return res, nil
}

// BindConsoleToSocket implements runmv1.SocketAllocatorServiceServer.
func (s *Server) BindConsoleToSocket(ctx context.Context, req *runmv1.BindConsoleToSocketRequest) (*runmv1.BindConsoleToSocketResponse, error) {
	cs, ok := s.state.GetOpenConsole(req.GetConsoleReferenceId())
	if !ok {
		return nil, errors.Errorf("cannot bind console to socket: console not found")
	}

	as, ok := s.state.GetOpenSocket(req.GetSocketReferenceId())
	if !ok {
		return nil, errors.Errorf("cannot bind console to socket: socket '%s' not found", req.GetSocketReferenceId())
	}

	err := socket.BindConsoleToSocket(ctx, cs, as)
	if err != nil {
		return nil, err
	}

	return &runmv1.BindConsoleToSocketResponse{}, nil
}

// BindIOToSockets implements runmv1.SocketAllocatorServiceServer.
func (s *Server) BindIOToSockets(ctx context.Context, req *runmv1.BindIOToSocketsRequest) (*runmv1.BindIOToSocketsResponse, error) {
	io, ok := s.state.GetOpenIO(req.GetIoReferenceId())
	if !ok {
		return nil, errors.Errorf("io not found")
	}

	iosocks := [3]runtime.AllocatedSocket{}

	if req.GetStdinSocketReferenceId() != "" {
		sock, ok := s.state.GetOpenSocket(req.GetStdinSocketReferenceId())
		if !ok {
			return nil, errors.Errorf("stdin socket not found")
		}
		iosocks[0] = sock
	}
	if req.GetStdoutSocketReferenceId() != "" {
		sock, ok := s.state.GetOpenSocket(req.GetStdoutSocketReferenceId())
		if !ok {
			return nil, errors.Errorf("stdout socket not found")
		}
		iosocks[1] = sock
	}
	if req.GetStderrSocketReferenceId() != "" {
		sock, ok := s.state.GetOpenSocket(req.GetStderrSocketReferenceId())
		if !ok {
			return nil, errors.Errorf("stderr socket not found")
		}
		iosocks[2] = sock
	}

	err := socket.BindIOToSockets(ctx, io, iosocks[0], iosocks[1], iosocks[2])
	if err != nil {
		return nil, err
	}

	return &runmv1.BindIOToSocketsResponse{}, nil
}

// CloseConsole implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseConsole(ctx context.Context, req *runmv1.CloseConsoleRequest) (*runmv1.CloseConsoleResponse, error) {
	val, ok := s.state.GetOpenConsole(req.GetConsoleReferenceId())
	if !ok {
		return nil, errors.Errorf("console not found")
	}
	val.Close()
	s.state.DeleteOpenConsole(req.GetConsoleReferenceId())
	return &runmv1.CloseConsoleResponse{}, nil
}

// CloseIO implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseIO(ctx context.Context, req *runmv1.CloseIORequest) (*runmv1.CloseIOResponse, error) {
	val, ok := s.state.GetOpenIO(req.GetIoReferenceId())
	if !ok {
		return nil, errors.Errorf("io not found")
	}
	val.Close()
	s.state.DeleteOpenIO(req.GetIoReferenceId())
	return &runmv1.CloseIOResponse{}, nil
}

// CloseSocket implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseSocket(ctx context.Context, req *runmv1.CloseSocketRequest) (*runmv1.CloseSocketResponse, error) {
	val, ok := s.state.GetOpenSocket(req.GetSocketReferenceId())
	if !ok {
		return nil, errors.Errorf("socket not found")
	}
	val.Close()
	s.state.DeleteOpenSocket(req.GetSocketReferenceId())
	return &runmv1.CloseSocketResponse{}, nil
}

// CloseSockets implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseSockets(ctx context.Context, req *runmv1.CloseSocketsRequest) (*runmv1.CloseSocketsResponse, error) {
	for _, ref := range req.GetSocketReferenceIds() {
		val, ok := s.state.GetOpenSocket(ref)
		if !ok {
			return nil, errors.Errorf("socket not found")
		}
		val.Close()
	}

	for _, ref := range req.GetSocketReferenceIds() {
		s.state.DeleteOpenSocket(ref)
	}
	return &runmv1.CloseSocketsResponse{}, nil
}
