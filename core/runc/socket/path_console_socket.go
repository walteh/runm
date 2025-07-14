package socket

import (
	"context"
	"log/slog"

	"github.com/containerd/console"
	"github.com/rs/xid"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/pkg/recvtty"
)

var _ runtime.ConsoleSocket = &PathConsoleSocket{}

// the point of this thing is to provide runc with a valid Path() and then connect a console to that path via
//
//	a unix socket
type PathConsoleSocket struct {
	console *recvtty.RecvttyConnProxy
	path    string
}

func NewGuestPathProviderConsoleSocket() *PathConsoleSocket {
	path := "/tmp/runm-console-proxy." + xid.New().String() + ".sock"

	return &PathConsoleSocket{
		path: path,
	}
}

func (g *PathConsoleSocket) Close() error {
	return nil
}

func (g *PathConsoleSocket) Path() string {
	return g.path
}

func (g *PathConsoleSocket) ReceiveMaster() (console.Console, error) {
	if g.console == nil {
		return nil, errors.New("console not bound")
	}
	return g.console.Console(), nil
}

func (g *PathConsoleSocket) BindToAllocatedSocket(ctx context.Context, sock runtime.AllocatedSocket) error {

	console, err := recvtty.NewRecvttyProxy(ctx, g.path, sock.Conn())
	if err != nil {
		return errors.Errorf("creating recvtty proxy: %w", err)
	}
	g.console = console

	go func() {
		err = console.Run(ctx)
		if err != nil {
			slog.Error("running recvtty proxy", "error", err)
		}
	}()

	return nil
}
