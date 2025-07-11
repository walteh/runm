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

var _ runtime.ConsoleSocket = &HostConsoleSocketV2{}

type HostConsoleSocketV2 struct {
	socket         runtime.AllocatedSocket
	referenceId    string
	creationCtx    context.Context
	closeCallbacks []func(ctx context.Context) error
}

func (h *HostConsoleSocketV2) Conn() net.Conn {
	return h.socket.Conn()
}

func (h *HostConsoleSocketV2) Close() error {
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

func (h *HostConsoleSocketV2) GetReferenceId() string {
	return h.referenceId
}

func (h *HostConsoleSocketV2) Path() string {
	panic("unimplemented")
}

func (h *HostConsoleSocketV2) ReceiveMaster() (console.Console, error) {
	return NewRemotePTYConsoleAdapter(h.creationCtx, h.socket.Conn())
}

func NewHostUnixConsoleSocketV2(ctx context.Context, referenceId string, socket runtime.AllocatedSocket, closeCallbacks ...func(ctx context.Context) error) (*HostConsoleSocketV2, error) {
	return &HostConsoleSocketV2{
		socket:         socket,
		referenceId:    referenceId,
		creationCtx:    ctx,
		closeCallbacks: closeCallbacks,
	}, nil

	// return &HostConsoleSocket{socket: socket, path: tmp.Path(), conn: tmp.Conn().(*net.UnixConn)}, nil
}

var _ runtime.RuntimeConsole = &RemoteConsole{}

type RemoteConsole struct {
	allocatedSocket runtime.AllocatedSocket
}

// Close implements runtime.RuntimeConsole.
func (r *RemoteConsole) Close() error {
	return r.allocatedSocket.Close()
}

// DisableEcho implements runtime.RuntimeConsole.
func (r *RemoteConsole) DisableEcho() error {
	return nil
}

// Fd implements runtime.RuntimeConsole.
func (r *RemoteConsole) Fd() uintptr {
	panic("unimplemented")
}

// Name implements runtime.RuntimeConsole.
func (r *RemoteConsole) Name() string {
	panic("unimplemented")
}

// Read implements runtime.RuntimeConsole.
func (r *RemoteConsole) Read(p []byte) (n int, err error) {
	panic("unimplemented")
}

// Reset implements runtime.RuntimeConsole.
func (r *RemoteConsole) Reset() error {
	panic("unimplemented")
}

// Resize implements runtime.RuntimeConsole.
func (r *RemoteConsole) Resize(console.WinSize) error {
	panic("unimplemented")
}

// ResizeFrom implements runtime.RuntimeConsole.
func (r *RemoteConsole) ResizeFrom(runtime.RuntimeConsole) error {
	panic("unimplemented")
}

// SetRaw implements runtime.RuntimeConsole.
func (r *RemoteConsole) SetRaw() error {
	panic("unimplemented")
}

// Size implements runtime.RuntimeConsole.
func (r *RemoteConsole) Size() (console.WinSize, error) {
	panic("unimplemented")
}

// Write implements runtime.RuntimeConsole.
func (r *RemoteConsole) Write(p []byte) (n int, err error) {
	panic("unimplemented")
}
