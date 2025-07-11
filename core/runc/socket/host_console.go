//go:build !windows

package socket

// func BindGuestConsoleToSocket(ctx context.Context, cons runtime.ConsoleSocket, sock runtime.AllocatedSocket) error {

// 	// // open up the console socket path, and create a pipe to it

// 	consConn := cons.UnixConn()
// 	sockConn := sock.Conn()

// 	// create a goroutine to read from the pipe and write to the socket
// 	bind(ctx, "socket->console", consConn, sockConn)

// 	// create a goroutine to read from the socket and write to the console
// 	bind(ctx, "console->socket", sockConn, consConn)

// 	return nil
// }

// func NewHostVsockFdConsoleSocket(ctx context.Context, socket runtime.VsockAllocatedSocket, proxier runtime.VsockProxier) (*HostConsoleSocket, error) {
// 	if
// 	return &HostConsoleSocket{socket: socket, path: path, conn: conn}, nil
// }

// func NewHostConsoleSocket(ctx context.Context, socket ) (*HostConsoleSocket, error) {
// 	switch v := socket.(type) {
// 	case *SimpleVsockConn:
// 		proxy, err := CreateLocalVsockProxyConn(ctx, v)
// 		if err != nil {
// 			return nil, errors.Errorf("creating local vsock proxy: %w", err)
// 		}
// 		return NewHostUnixConsoleSocket(ctx, proxy)
// 	case *SimpleVsockProxyConn:
// 		return NewHostUnixConsoleSocket(ctx, v)
// 	default:
// 		return nil, errors.Errorf("invalid socket type: %T", socket)
// 	}
// }

// var _ runtime.ConsoleSocket = &HostConsoleSocket{}

// type HostConsoleSocket struct {
// 	socket       runtime.AllocatedSocketWithUnixConn
// 	runcUnixConn *net.UnixConn
// 	referenceId  string
// }

// func (h *HostConsoleSocket) FileConn() file.FileConn {
// 	return h.socket.Conn()
// }

// func (h *HostConsoleSocket) Close() error {
// 	return h.socket.Conn().Close() // TODO: close the other end of the socket
// }

// func (h *HostConsoleSocket) UnixConn() *net.UnixConn {
// 	return h.socket.UnixConn()
// }

// func (h *HostConsoleSocket) GetReferenceId() string {
// 	return h.referenceId
// }

// func (h *HostConsoleSocket) Path() string {
// 	return h.socket.UnixConn().LocalAddr().String()
// }

// func (h *HostConsoleSocket) ReceiveMaster() (console.Console, error) {
// 	f, err := RecvFd(h.socket.UnixConn())
// 	if err != nil {
// 		return nil, errors.Errorf("receiving master: %w", err)
// 	}
// 	return console.ConsoleFromFile(f)
// }

// func NewHostUnixConsoleSocket(ctx context.Context, referenceId string, socket runtime.AllocatedSocketWithUnixConn) (*HostConsoleSocket, error) {
// 	tmp, err := gorunc.NewTempConsoleSocket()
// 	if err != nil {
// 		return nil, errors.Errorf("creating temp console socket: %w", err)
// 	}

// 	// connect to the unix socket
// 	runcUnixConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: tmp.Path(), Net: "unix"})
// 	if err != nil {
// 		return nil, errors.Errorf("failed to dial unix socket: %w", err)
// 	}

// 	bind(ctx, "runcConsole->hostConsole", runcUnixConn, socket.Conn())

// 	bind(ctx, "hostConsole->runcConsole", socket.Conn(), runcUnixConn)

// 	return &HostConsoleSocket{
// 		socket:       socket,
// 		runcUnixConn: runcUnixConn,
// 		referenceId:  referenceId,
// 	}, nil

// 	// return &HostConsoleSocket{socket: socket, path: tmp.Path(), conn: tmp.Conn().(*net.UnixConn)}, nil
// }
