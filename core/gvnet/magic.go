package gvnet

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"

	"github.com/containers/gvisor-tap-vsock/pkg/services/forwarder"
	"github.com/containers/gvisor-tap-vsock/pkg/tcpproxy"
	"github.com/containers/gvisor-tap-vsock/pkg/transport"
	"github.com/soheilhy/cmux"
	"gitlab.com/tozd/go/errors"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/walteh/runm/pkg/taskgroup"
)

type MagicHostPort struct {
	mux       cmux.CMux
	addr      net.Addr
	toClose   []io.Closer
	alive     bool
	listener  net.Listener
	taskGroup *taskgroup.TaskGroup
}

func NewMagicHostPortStream(ctx context.Context, magicHostPort string, tg *taskgroup.TaskGroup) (*MagicHostPort, error) {
	l, err := transport.Listen(magicHostPort)
	if err != nil {
		return nil, errors.Errorf("listen: %w", err)
	}

	cmux := cmux.New(l)

	cmux.HandleError(func(err error) bool {
		slog.ErrorContext(ctx, "cmux error", "error", err)
		return true
	})

	return &MagicHostPort{mux: cmux, addr: l.Addr(), toClose: []io.Closer{l}, listener: l, taskGroup: tg}, nil
}

func (g *MagicHostPort) ApplyRestMux(name string, mux http.Handler) {
	g.taskGroup.GoWithName("globalhostport_"+name, func(ctx context.Context) error {
		server := NewHTTPServer("globalhostport_"+name, mux, g.mux.Match(cmux.Any()))
		return server.Run(ctx)
	})
}

func (g *MagicHostPort) Run(ctx context.Context) error {
	g.alive = true
	defer func() {
		g.alive = false
	}()

	// Start the cmux server task
	g.taskGroup.GoWithName("cmux-server", func(ctx context.Context) error {
		server := NewCmuxServer(fmt.Sprintf("globalhostport_cmux(%s)", g.addr.String()), g.mux)
		return server.Run(ctx)
	})

	// Block until context is done
	<-ctx.Done()
	return ctx.Err()
}

func (g *MagicHostPort) Alive() bool {
	return g.alive
}

func (g *MagicHostPort) Close(ctx context.Context) error {
	for _, c := range g.toClose {
		go c.Close()
	}
	return nil
}

func (g *MagicHostPort) Fields() []slog.Attr {
	return []slog.Attr{
		slog.Group("globalhostport",
			slog.String("addr", g.addr.String()),
			slog.Bool("alive", g.alive),
		),
	}
}

func (g *MagicHostPort) Name() string {
	return fmt.Sprintf("globalhostport(%s)", g.addr.String())
}

func (g *MagicHostPort) ForwardCMUXMatchToGuestPort(ctx context.Context, switc *stack.Stack, guestPortTarget uint16, matcher cmux.Matcher) error {
	listener := g.mux.Match(matcher)

	hostAddress := fmt.Sprintf("cmux_match:%d", guestPortTarget)

	guestPortTargetStr := fmt.Sprintf("%s:%d", VIRTUAL_GUEST_IP, guestPortTarget)

	guestAddress, err := forwarder.TCPIPAddress(1, guestPortTargetStr)
	if err != nil {
		listener.Close()
		return errors.Errorf("failed to get tcpip address: %w", err)
	}

	// honestly not sure if this is needed, just interested in playing more with switch
	nic := switc.CheckLocalAddress(1, ipv4.ProtocolNumber, guestAddress.Addr)
	if nic != 1 {
		guestAddress.NIC = nic
	}

	var proxy tcpproxy.Proxy
	proxy.ListenFunc = func(network, laddr string) (net.Listener, error) {
		return listener, nil
	}

	proxy.AddRoute(hostAddress, &tcpproxy.DialProxy{
		Addr: guestPortTargetStr,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return gonet.DialContextTCP(ctx, switc, guestAddress, ipv4.ProtocolNumber)
		},
		OnDialError: func(src net.Conn, dstDialErr error) {
			slog.ErrorContext(ctx, "failed to dial", "error", dstDialErr)
			src.Close()
		},
	})

	// Start the proxy
	err = proxy.Start()
	if err != nil {
		return errors.Errorf("starting tcpproxy [%s -> %s]: %w", hostAddress, guestPortTargetStr, err)
	}

	// Register proxy cleanup
	g.taskGroup.RegisterCloserWithName(fmt.Sprintf("tcpproxy-%d", guestPortTarget), &proxy)

	// Run the proxy in a task
	g.taskGroup.GoWithName(fmt.Sprintf("tcpproxy-%d", guestPortTarget), func(ctx context.Context) error {
		slog.InfoContext(ctx, "starting tcpproxy",
			"source", hostAddress,
			"target", guestPortTargetStr,
			"guest_port", guestPortTarget)

		err := proxy.Wait()
		if err != nil {
			return errors.Errorf("running tcpproxy [%s -> %s]: %w", hostAddress, guestPortTargetStr, err)
		}
		return nil
	})
	g.toClose = append(g.toClose, listener)

	return nil
}
