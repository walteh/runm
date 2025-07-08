package socket

import (
	"context"
	"net"
	"sync/atomic"

	"github.com/mdlayher/vsock"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.SocketAllocator = (*GuestVsockSocketAllocator)(nil)

var guestVsockSocketCounter = atomic.Uint32{}

func NewGuestVsockSocketAllocatorPrettySureUnused(cid uint32, basePort uint32) *GuestVsockSocketAllocator {
	return &GuestVsockSocketAllocator{cid: cid, basePort: basePort}
}

type GuestVsockSocketAllocator struct {
	basePort uint32
	cid      uint32
}

func (g *GuestVsockSocketAllocator) AllocateSocket(ctx context.Context) (runtime.AllocatedSocket, error) {
	// pretty sure this is unused, we only use the vsock allocator for the grpc server
	return nil, errors.Errorf("pretty sure this vsock allocator is not used")
	// port := uint32(g.basePort) + uint32(guestVsockSocketCounter.Add(1))
	// sock, err := NewGuestAllocatedVsockSocket(ctx, g.cid, port)
	// if err != nil {
	// 	return nil, errors.Errorf("failed to allocate vsock socket: %w", err)
	// }
	// return sock, nil
}

func (g *GuestAllocatedVsockSocket) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	err := g.Ready()
	if err != nil {
		return nil, err
	}
	return g.conn, nil
}

var _ runtime.VsockAllocatedSocket = (*GuestAllocatedVsockSocket)(nil)

type GuestAllocatedVsockSocket struct {
	listener    *vsock.Listener
	conn        *vsock.Conn
	ready       chan struct{}
	readyErr    error
	port        uint32
	referenceId string
}

func (g *GuestAllocatedVsockSocket) Port() uint32 {
	return g.port
}

func (g *GuestAllocatedVsockSocket) Close() error {

	return g.conn.Close()
}

func (g *GuestAllocatedVsockSocket) Conn() runtime.FileConn {
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
	refId := runtime.NewVsockSocketReferenceId(port)
	guestConn := &GuestAllocatedVsockSocket{
		listener:    listener,
		port:        port,
		referenceId: refId,
		ready:       make(chan struct{}),
	}

	go func() {
		defer close(guestConn.ready)
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			guestConn.readyErr = err
			return
		}
		guestConn.conn = conn.(*vsock.Conn)
		guestConn.listener.Close()
	}()

	return guestConn, nil
}
