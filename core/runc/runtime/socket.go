package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/mdlayher/vsock"
	"gitlab.com/tozd/go/errors"
	"go.uber.org/atomic"
)

type FileConn interface {
	syscall.Conn
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
}

type HostAllocatedSocket struct {
	conn        *net.UnixConn
	path        string
	referenceId string
}

func (h *HostAllocatedSocket) isAllocatedSocket() {}

func (h *HostAllocatedSocket) Close() error {
	return h.conn.Close()
}

func (h *HostAllocatedSocket) Conn() FileConn {
	return h.conn
}

func (h *HostAllocatedSocket) Path() string {
	return h.path
}

func (h *HostAllocatedSocket) Ready() error {
	return nil
}

func (h *HostAllocatedSocket) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return h.conn, nil
}

func NewHostAllocatedVsockSocket(ctx context.Context, port uint32, refId string, proxier VsockProxier) (*HostAllocatedSocket, error) {
	conn, path, err := proxier.ProxyVsock(ctx, port)
	if err != nil {
		return nil, err
	}
	return &HostAllocatedSocket{conn: conn, path: path, referenceId: refId}, nil
}

func NewHostAllocatedUnixSocket(ctx context.Context, path string, refId string) (*HostAllocatedSocket, error) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "new host allocated unix socket", "path", path, "refId", refId)
	return &HostAllocatedSocket{conn: conn, path: path, referenceId: refId}, nil
}

func NewHostAllocatedSocketFromId(ctx context.Context, id string, proxier VsockProxier) (*HostAllocatedSocket, error) {
	switch {
	case strings.HasPrefix(id, "socket:vsock:"):
		port, err := strconv.Atoi(strings.TrimPrefix(id, "socket:vsock:"))
		if err != nil {
			return nil, errors.Errorf("allocating vsock socket: %w", err)
		}
		sock, err := NewHostAllocatedVsockSocket(ctx, uint32(port), id, proxier)
		if err != nil {
			return nil, errors.Errorf("allocating vsock socket: %w", err)
		}
		return sock, nil
	case strings.HasPrefix(id, "socket:unix:"):
		path := strings.TrimPrefix(id, "socket:unix:")
		sock, err := NewHostAllocatedUnixSocket(ctx, path, id)
		if err != nil {
			return nil, errors.Errorf("allocating unix socket: %w", err)
		}
		return sock, nil
	}
	return nil, errors.Errorf("invalid socket type: %s", id)
}

func debugReader(ctx context.Context, name string, r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	tr := io.TeeReader(r, pw)
	// reaqd one byte at a time from r and print it to stdout
	go func() {
		slog.InfoContext(ctx, "starting debugReader", "name", name)
		for ctx.Err() == nil {

			buf := make([]byte, 1)
			_, err := pr.Read(buf)
			if err != nil {
				return
			}
			slog.InfoContext(ctx, "captured byte", "name", name, "byte", buf[0])
		}
	}()
	return tr
}

// runc.ConsoleSocket -> console.Console -> my.Socket
// BindConsoleToSocket implements runtime.SocketAllocator.
func BindConsoleToSocket(ctx context.Context, cons ConsoleSocket, sock AllocatedSocket) error {

	// // open up the console socket path, and create a pipe to it
	consConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: cons.Path(), Net: "unix"})
	if err != nil {
		return errors.Errorf("failed to dial console socket: %w", err)
	}
	sockConn := sock.Conn()

	wg := sync.WaitGroup{}
	wg.Add(2)

	// create a goroutine to read from the pipe and write to the socket
	go func() {
		defer wg.Done()
		io.Copy(consConn, debugReader(ctx, "consConn", sockConn))
	}()

	// create a goroutine to read from the socket and write to the console
	go func() {
		defer wg.Done()
		io.Copy(sockConn, debugReader(ctx, "sockConn", consConn))
	}()

	go func() {
		wg.Wait()
		consConn.Close()
		sockConn.Close()
	}()

	// return the pipe
	return nil
}

// func BindConsoleToSocket(ctx context.Context, cons ConsoleSocket, sock AllocatedSocket) error {

// 	// 1) Create a listener on the path your ConsoleSocket exposes:
// 	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: cons.Path(), Net: "unix"})
// 	if err != nil {
// 		return errors.Errorf("failed to listen on console socket: %w", err)
// 	}
// 	defer l.Close()

// 	go func() {

// 		for {
// 			// 2) Accept each new incoming connection:
// 			clientConn, err := l.AcceptUnix()
// 			if err != nil {
// 				slog.ErrorContext(ctx, "failed to accept console connection", "error", err)
// 				return
// 			}

// 			// 3) Dial the other side (vsock, etc.):
// 			serverConn := sock.Conn()

// 			// 4) Proxy bi-directionally:
// 			go func() {
// 				io.Copy(serverConn, clientConn) // client → server
// 				serverConn.Close()
// 			}()
// 			go func() {
// 				io.Copy(clientConn, serverConn) // server → client
// 				clientConn.Close()
// 			}()
// 		}
// 	}()

// 	return nil
// }

// BindIOToSockets implements SocketAllocator.
func BindIOToSockets(ctx context.Context, ios IO, stdin, stdout, stderr AllocatedSocket) error {

	if ios == nil {
		return errors.Errorf("ios is nil")
	}

	if stdin != nil {
		go func() {
			if err := stdin.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stdin", "error", err)
				return
			}
			io.Copy(ios.Stdin(), stdin.Conn())
		}()
	}
	if stdout != nil {
		go func() {
			if err := stdout.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stdout", "error", err)
				return
			}
			io.Copy(stdout.Conn(), ios.Stdout())
		}()
	}
	if stderr != nil {
		go func() {
			if err := stderr.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stderr", "error", err)
				return
			}
			io.Copy(stderr.Conn(), ios.Stderr())
		}()
	}

	return nil
}

type GuestAllocatedUnixSocket struct {
	listener    *net.UnixListener
	conn        *net.UnixConn
	path        string
	ready       chan struct{}
	readyErr    error
	referenceId string
}

func (g *GuestAllocatedUnixSocket) isAllocatedSocket() {}

func (g *GuestAllocatedUnixSocket) Close() error {
	return g.conn.Close()
}

func (g *GuestAllocatedUnixSocket) Conn() FileConn {
	return g.conn
}

func (g *GuestAllocatedUnixSocket) Path() string {
	return g.path
}

func (g *GuestAllocatedUnixSocket) Ready() error {
	<-g.ready
	return g.readyErr
}

func NewGuestAllocatedUnixSocket(ctx context.Context, path string) (*GuestAllocatedUnixSocket, error) {
	conn, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}

	refId := NewUnixSocketReferenceId(path)

	guestConn := &GuestAllocatedUnixSocket{
		listener:    conn,
		path:        path,
		referenceId: refId,
		ready:       make(chan struct{}),
	}

	go func() {
		defer close(guestConn.ready)
		conn, err := conn.AcceptUnix()
		if err != nil {
			return
		}
		guestConn.conn = conn
	}()

	return guestConn, nil
}

type GuestAllocatedVsockSocket struct {
	listener    *vsock.Listener
	conn        *vsock.Conn
	ready       chan struct{}
	readyErr    error
	path        string
	referenceId string
}

func (g *GuestAllocatedVsockSocket) isAllocatedSocket() {}

func (g *GuestAllocatedVsockSocket) Close() error {
	return g.conn.Close()
}

func (g *GuestAllocatedVsockSocket) Conn() FileConn {
	return g.conn
}

func (g *GuestAllocatedVsockSocket) Ready() error {
	<-g.ready
	return g.readyErr
}

func NewGuestAllocatedVsockSocket(ctx context.Context, cid uint32, port uint32) (*GuestAllocatedVsockSocket, error) {
	listener, err := vsock.ListenContextID(cid, port, nil)
	if err != nil {
		return nil, err
	}
	refId := NewVsockSocketReferenceId(port)
	guestConn := &GuestAllocatedVsockSocket{
		listener:    listener,
		path:        fmt.Sprintf("vsock:%d", port),
		referenceId: refId,
		ready:       make(chan struct{}),
	}

	go func() {
		defer close(guestConn.ready)
		conn, err := listener.Accept()
		if err != nil {
			guestConn.readyErr = err
			return
		}
		guestConn.conn = conn.(*vsock.Conn)
	}()

	return guestConn, nil
}

var _ SocketAllocator = (*GuestUnixSocketAllocator)(nil)

type GuestUnixSocketAllocator struct {
	socketDir string
}

func NewGuestUnixSocketAllocator(socketDir string) *GuestUnixSocketAllocator {
	return &GuestUnixSocketAllocator{socketDir: socketDir}
}

var guestUnixSocketCounter = atomic.NewInt64(0)

// AllocateSocket implements SocketAllocator.
func (g *GuestUnixSocketAllocator) AllocateSocket(ctx context.Context) (AllocatedSocket, error) {

	unixSockPath := filepath.Join(g.socketDir, fmt.Sprintf("runm-%02d.sock", guestUnixSocketCounter.Add(1)))

	os.MkdirAll(filepath.Dir(unixSockPath), 0755)

	unixSock, err := NewGuestAllocatedUnixSocket(ctx, unixSockPath)
	if err != nil {
		return nil, errors.Errorf("failed to allocate unix socket: %w", err)
	}
	return unixSock, nil
}

var _ SocketAllocator = (*GuestVsockSocketAllocator)(nil)

var guestVsockSocketCounter = atomic.NewInt64(0)

func NewGuestVsockSocketAllocator(cid uint32, basePort uint32) *GuestVsockSocketAllocator {
	return &GuestVsockSocketAllocator{cid: cid, basePort: basePort}
}

type GuestVsockSocketAllocator struct {
	basePort uint32
	cid      uint32
}

func (g *GuestVsockSocketAllocator) AllocateSocket(ctx context.Context) (AllocatedSocket, error) {
	port := uint32(g.basePort) + uint32(guestVsockSocketCounter.Add(1))
	sock, err := NewGuestAllocatedVsockSocket(ctx, g.cid, port)
	if err != nil {
		return nil, errors.Errorf("failed to allocate vsock socket: %w", err)
	}
	return sock, nil
}

func (g *GuestAllocatedVsockSocket) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	err := g.Ready()
	if err != nil {
		return nil, err
	}
	return g.conn, nil
}

func (g *GuestAllocatedUnixSocket) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	err := g.Ready()
	if err != nil {
		return nil, err
	}
	return g.conn, nil
}
