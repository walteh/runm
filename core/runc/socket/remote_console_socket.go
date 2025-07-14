//go:build !windows

package socket

import (
	"context"
	"log/slog"
	"net"
	"sync"

	"github.com/containerd/console"

	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.ConsoleSocket = &RemoteConsoleSocket{}

type RemoteConsoleSocket struct {
	socket         runtime.AllocatedSocket
	referenceId    string
	creationCtx    context.Context
	closeCallbacks []func(ctx context.Context) error
	resizeFunc     func(ctx context.Context, winSize console.WinSize) error
}

func NewHostUnixConsoleSocketV2(ctx context.Context, referenceId string, socket runtime.AllocatedSocket, resizeFunc func(ctx context.Context, winSize console.WinSize) error, closeCallbacks ...func(ctx context.Context) error) (*RemoteConsoleSocket, error) {
	return &RemoteConsoleSocket{
		socket:         socket,
		referenceId:    referenceId,
		creationCtx:    ctx,
		closeCallbacks: closeCallbacks,
		resizeFunc:     resizeFunc,
	}, nil
}

func (h *RemoteConsoleSocket) Conn() net.Conn {
	return h.socket.Conn()
}

func (h *RemoteConsoleSocket) Close() error {
	h.socket.Close()
	for _, closer := range h.closeCallbacks {
		wg := sync.WaitGroup{}
		wg.Go(func() {
			err := closer(h.creationCtx)
			slog.DebugContext(h.creationCtx, "closed console callback", "err", err)
		})
		wg.Wait()
	}
	return nil
}

// func (h *HostConsoleSocketV2) UnixConn() *net.UnixConn {
// 	return h.socket.UnixConn()
// }

func (h *RemoteConsoleSocket) GetReferenceId() string {
	return h.referenceId
}

func (h *RemoteConsoleSocket) Path() string {
	panic("unimplemented") // this is used by runc, but since this is remote it has no path
}

func (h *RemoteConsoleSocket) ReceiveMaster() (console.Console, error) {
	innerConsole, err := NewRemotePTYConsoleAdapter(h.creationCtx, h.socket.Conn(), h.resizeFunc)
	if err != nil {
		return nil, err
	}

	return innerConsole, nil
}
