//go:build !windows

package server

import (
	"context"
	"log/slog"
	"net"

	"github.com/containerd/console"
	"github.com/mdlayher/vsock"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/socket"
	"github.com/walteh/runm/core/runc/state"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.SocketAllocatorServiceServer = (*Server)(nil)

func (s *Server) DialOpenListener(ctx context.Context, req *runmv1.DialOpenListenerRequest) (*runmv1.DialOpenListenerResponse, error) {
	switch req.GetListeningOn().WhichType() {
	case runmv1.SocketType_VsockPort_case:
		vsockPort := req.GetListeningOn().GetVsockPort()
		vsock, err := vsock.Dial(2, vsockPort.GetPort(), nil)
		if err != nil {
			return nil, errors.Errorf("failed to open vsock listener: %w", err)
		}

		conn := socket.NewSimpleVsockConn(ctx, vsock, vsockPort.GetPort())
		s.state.StoreOpenVsockConnection(vsockPort.GetPort(), conn)

		// refid := runtime.NewSocketReferenceId(conn)

		// s.state.StoreOpenSocket(refid, conn)

	case runmv1.SocketType_UnixSocketPath_case:
		unixSocketPath := req.GetListeningOn().GetUnixSocketPath()
		unixSocket, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: unixSocketPath.GetPath(), Net: "unix"})
		if err != nil {
			return nil, errors.Errorf("failed to open unix socket listener: %w", err)
		}
		conn := socket.NewSimpleUnixConn(ctx, unixSocket, unixSocketPath.GetPath())
		s.state.StoreOpenUnixConnection(unixSocketPath.GetPath(), conn)
		// refid := runtime.NewSocketReferenceId(conn)
		// s.state.StoreOpenSocket(refid, conn)
	default:
		return nil, errors.Errorf("invalid listening on")
	}

	return &runmv1.DialOpenListenerResponse{}, nil
}

func (s *Server) AllocateConsole(ctx context.Context, req *runmv1.AllocateConsoleRequest) (*runmv1.AllocateConsoleResponse, error) {
	referenceId := runtime.NewConsoleReferenceId()
	path := socket.NewGuestPathProviderConsoleSocket()
	res := &runmv1.AllocateConsoleResponse{}
	res.SetConsoleReferenceId(referenceId)
	s.state.StoreOpenConsole(referenceId, path)
	return res, nil
}

func (s *Server) AllocateIO(ctx context.Context, req *runmv1.AllocateIORequest) (*runmv1.AllocateIOResponse, error) {
	ioref := runtime.NewIoReferenceId()
	pio, err := s.runtime.NewPipeIO(ctx, int(req.GetIoUid()), int(req.GetIoGid()), func(opts *gorunc.IOOption) {
		opts.OpenStdin = req.GetOpenStdin()
		opts.OpenStdout = req.GetOpenStdout()
		opts.OpenStderr = req.GetOpenStderr()
	})
	if err != nil {
		return nil, errors.Errorf("allocating io: %w", err)
	}
	s.state.StoreOpenIO(ioref, pio)
	res := &runmv1.AllocateIOResponse{}
	res.SetIoReferenceId(ioref)

	return res, nil
}

// BindConsoleToSocket implements runmv1.SocketAllocatorServiceServer.
func (s *Server) BindConsoleToSocket(ctx context.Context, req *runmv1.BindConsoleToSocketRequest) (*runmv1.BindConsoleToSocketResponse, error) {
	cs, ok := s.state.GetOpenConsole(req.GetConsoleReferenceId())
	if !ok {
		return nil, errors.Errorf("cannot bind console to socket: console not found")
	}

	sbcs, ok := cs.(runtime.BindableConsoleSocket)
	if !ok {
		return nil, errors.Errorf("cannot bind console to socket: console is not a socket bindable console")
	}

	as, err := loadSocket(s.state, req.GetSocketType())
	if err != nil {
		return nil, errors.Errorf("cannot bind console to socket: socket not found: %w", err)
	}

	err = sbcs.BindToAllocatedSocket(ctx, as)
	if err != nil {
		return nil, errors.Errorf("cannot bind console to socket: %w", err)
	}

	return &runmv1.BindConsoleToSocketResponse{}, nil
}

func loadSocket(s *state.State, req *runmv1.SocketType) (runtime.AllocatedSocket, error) {
	switch req.WhichType() {
	case runmv1.SocketType_VsockPort_case:
		res, ok := s.GetOpenVsockConnection(req.GetVsockPort().GetPort())
		if !ok {
			return nil, errors.Errorf("vsock port not found")
		}
		return res, nil
	case runmv1.SocketType_UnixSocketPath_case:
		res, ok := s.GetOpenUnixConnection(req.GetUnixSocketPath().GetPath())
		if !ok {
			return nil, errors.Errorf("unix socket path not found")
		}
		return res, nil
	}
	return nil, errors.Errorf("invalid socket type")
}

func storeSocket(st *state.State, sock runtime.AllocatedSocket) (*runmv1.SocketType, error) {
	switch s := sock.(type) {
	case runtime.VsockAllocatedSocket:
		st.StoreOpenVsockConnection(s.Port(), s)
		t := &runmv1.SocketType{}
		vt := &runmv1.VsockPort{}
		vt.SetPort(s.Port())
		t.SetVsockPort(vt)
		return t, nil
	case runtime.UnixAllocatedSocket:
		st.StoreOpenUnixConnection(s.Path(), s)
		t := &runmv1.SocketType{}
		ut := &runmv1.UnixSocketPath{}
		ut.SetPath(s.Path())
		t.SetUnixSocketPath(ut)
		return t, nil
	default:
		return nil, errors.Errorf("unknown socket type: %T", sock)
	}
}

func deleteSocket(st *state.State, sock runtime.AllocatedSocket) error {
	switch s := sock.(type) {
	case runtime.VsockAllocatedSocket:
		st.DeleteOpenVsockConnection(s.Port())
	case runtime.UnixAllocatedSocket:
		st.DeleteOpenUnixConnection(s.Path())
	default:
		return errors.Errorf("unknown socket type: %T", sock)
	}
	return nil
}

// BindIOToSockets implements runmv1.SocketAllocatorServiceServer.
func (s *Server) BindIOToSockets(ctx context.Context, req *runmv1.BindIOToSocketsRequest) (*runmv1.BindIOToSocketsResponse, error) {
	io, ok := s.state.GetOpenIO(req.GetIoReferenceId())
	if !ok {
		return nil, errors.Errorf("io not found")
	}
	var err error
	iosocks := [3]runtime.AllocatedSocket{}

	if req.GetStdinSocket() != nil {
		iosocks[0], err = loadSocket(s.state, req.GetStdinSocket())
		if err != nil {
			return nil, err
		}
	}
	if req.GetStdoutSocket() != nil {
		iosocks[1], err = loadSocket(s.state, req.GetStdoutSocket())
		if err != nil {
			return nil, err
		}
	}
	if req.GetStderrSocket() != nil {
		iosocks[2], err = loadSocket(s.state, req.GetStderrSocket())
		if err != nil {
			return nil, err
		}
	}

	io.Close()

	ios := socket.NewAllocatedSocketIO(ctx, iosocks[0], iosocks[1], iosocks[2])
	s.state.StoreOpenIO(req.GetIoReferenceId(), ios)

	// err = socket.BindIOToSockets(ctx, io, ios)
	// if err != nil {
	// 	return nil, err
	// }

	return &runmv1.BindIOToSocketsResponse{}, nil
}

// CloseConsole implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseConsole(ctx context.Context, req *runmv1.CloseConsoleRequest) (*runmv1.CloseConsoleResponse, error) {
	val, ok := s.state.GetOpenConsole(req.GetConsoleReferenceId())
	if !ok {
		return nil, errors.Errorf("console not found for reference id: %s", req.GetConsoleReferenceId())
	}
	val.Close()
	s.state.DeleteOpenConsole(req.GetConsoleReferenceId())
	return &runmv1.CloseConsoleResponse{}, nil
}

// CloseIO implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseIO(ctx context.Context, req *runmv1.CloseIORequest) (*runmv1.CloseIOResponse, error) {

	val, ok := s.state.GetOpenIO(req.GetIoReferenceId())
	if !ok {
		// Make CloseIO idempotent - don't error if IO is already closed
		slog.DebugContext(ctx, "CloseIO called for already closed IO", "io_reference_id", req.GetIoReferenceId())
		return &runmv1.CloseIOResponse{}, nil
	}
	val.Close()
	s.state.DeleteOpenIO(req.GetIoReferenceId())
	slog.DebugContext(ctx, "CloseIO successfully closed IO", "io_reference_id", req.GetIoReferenceId())
	return &runmv1.CloseIOResponse{}, nil
}

// CloseSocket implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseSocket(ctx context.Context, req *runmv1.CloseSocketRequest) (*runmv1.CloseSocketResponse, error) {
	sock, err := loadSocket(s.state, req.GetSocketType())
	if err != nil {
		return nil, err
	}
	sock.Close()
	if err := deleteSocket(s.state, sock); err != nil {
		return nil, err
	}
	return &runmv1.CloseSocketResponse{}, nil
}

// CloseSockets implements runmv1.SocketAllocatorServiceServer.
func (s *Server) CloseSockets(ctx context.Context, req *runmv1.CloseSocketsRequest) (*runmv1.CloseSocketsResponse, error) {
	for _, sock := range req.GetSocketTypes() {
		sock, err := loadSocket(s.state, sock)
		if err != nil {
			return nil, err
		}
		sock.Close()
		if err := deleteSocket(s.state, sock); err != nil {
			return nil, err
		}
	}

	// for _, ref := range req.GetSocketReferenceIds() {
	// 	if err := deleteSocket(s.state, ref); err != nil {
	// 		return nil, err
	// 	}
	// }
	return &runmv1.CloseSocketsResponse{}, nil
}

// ResizeConsole implements runmv1.SocketAllocatorServiceServer.
func (s *Server) ResizeConsole(ctx context.Context, req *runmv1.ResizeConsoleRequest) (*runmv1.ResizeConsoleResponse, error) {
	cs, ok := s.state.GetOpenConsole(req.GetConsoleReferenceId())
	if !ok {
		return nil, errors.Errorf("console not found: %s", req.GetConsoleReferenceId())
	}

	master, err := cs.ReceiveMaster()
	if err != nil {
		return nil, errors.Errorf("failed to receive master console: %w", err)
	}

	windowSize := req.GetWindowSize()
	winSize := console.WinSize{
		Width:  uint16(windowSize.GetWidth()),
		Height: uint16(windowSize.GetHeight()),
	}

	err = master.Resize(winSize)
	if err != nil {
		return nil, errors.Errorf("failed to resize console: %w", err)
	}

	slog.InfoContext(ctx, "console resized successfully",
		"console_reference_id", req.GetConsoleReferenceId(),
		"width", winSize.Width,
		"height", winSize.Height)

	return &runmv1.ResizeConsoleResponse{}, nil
}
