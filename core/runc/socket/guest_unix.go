package socket

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"

	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.SocketAllocator = (*GuestUnixSocketAllocator)(nil)

type GuestUnixSocketAllocator struct {
	socketDir string
}

func NewGuestUnixSocketAllocator(socketDir string) *GuestUnixSocketAllocator {
	return &GuestUnixSocketAllocator{socketDir: socketDir}
}

var guestUnixSocketCounter = atomic.Uint32{}

// AllocateSocket implements SocketAllocator.
func (g *GuestUnixSocketAllocator) AllocateSocket(ctx context.Context) (runtime.AllocatedSocket, error) {

	unixSockPath := filepath.Join(g.socketDir, fmt.Sprintf("runm-%02d.sock", guestUnixSocketCounter.Add(1)))

	os.MkdirAll(filepath.Dir(unixSockPath), 0755)

	unixSock, err := NewGuestAllocatedUnixSocket(ctx, unixSockPath)
	if err != nil {
		return nil, errors.Errorf("failed to allocate unix socket: %w", err)
	}
	return unixSock, nil
}

var _ runtime.UnixAllocatedSocket = (*GuestAllocatedUnixSocket)(nil)

type GuestAllocatedUnixSocket struct {
	listener    *net.UnixListener
	conn        *net.UnixConn
	path        string
	ready       chan struct{}
	readyErr    error
	referenceId string
}

func (g *GuestAllocatedUnixSocket) Path() string {
	return g.path
}

func (g *GuestAllocatedUnixSocket) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	err := g.Ready()
	if err != nil {
		return nil, err
	}
	return g.conn, nil
}

func (g *GuestAllocatedUnixSocket) Close() error {
	return g.conn.Close()
}

func (g *GuestAllocatedUnixSocket) Conn() net.Conn {
	return g.conn
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

	refId := runtime.NewUnixSocketReferenceId(path)

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
