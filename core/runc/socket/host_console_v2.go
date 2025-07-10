//go:build !windows

package socket

import (
	"context"
	"net"

	"github.com/containerd/console"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/file"
	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.ConsoleSocket = &HostConsoleSocket{}

type HostConsoleSocketV2 struct {
	socket      runtime.AllocatedSocketWithUnixConn
	referenceId string
}

func (h *HostConsoleSocketV2) FileConn() file.FileConn {
	return h.socket.Conn()
}

func (h *HostConsoleSocketV2) Close() error {
	return h.socket.Conn().Close() // TODO: close the other end of the socket
}

func (h *HostConsoleSocketV2) UnixConn() *net.UnixConn {
	return h.socket.UnixConn()
}

func (h *HostConsoleSocketV2) GetReferenceId() string {
	return h.referenceId
}

func (h *HostConsoleSocketV2) Path() string {
	return h.socket.UnixConn().LocalAddr().String()
}

func (h *HostConsoleSocketV2) ReceiveMaster() (console.Console, error) {
	f, err := RecvFd(h.socket.UnixConn())
	if err != nil {
		return nil, errors.Errorf("receiving master: %w", err)
	}
	return console.ConsoleFromFile(f)
}

func NewHostUnixConsoleSocketV2(ctx context.Context, referenceId string, socket runtime.AllocatedSocketWithUnixConn) (*HostConsoleSocketV2, error) {
	return &HostConsoleSocketV2{
		socket:      socket,
		referenceId: referenceId,
	}, nil

	// return &HostConsoleSocket{socket: socket, path: tmp.Path(), conn: tmp.Conn().(*net.UnixConn)}, nil
}

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
