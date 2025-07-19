package otel

import (
	"context"
	"log/slog"
	"net"

	"github.com/containers/gvisor-tap-vsock/pkg/tcpproxy"
	"gitlab.com/tozd/go/errors"
)

func RunTCPForwarder(ctx context.Context, to string, from net.Listener) error {

	var proxy tcpproxy.Proxy
	proxy.ListenFunc = func(network, laddr string) (net.Listener, error) {
		return from, nil
	}

	proxy.AddRoute("", &tcpproxy.DialProxy{
		Addr: to,
		// DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		// 	return to.DialContext(ctx, network, address)
		// },
		OnDialError: func(src net.Conn, dstDialErr error) {
			slog.ErrorContext(ctx, "failed to dial", "error", dstDialErr)
			src.Close()
		},
	})

	// Start the proxy
	err := proxy.Start()
	if err != nil {
		return errors.Errorf("starting tcpproxy [%s -> %s]: %w", from.Addr(), to, err)
	}

	return nil
}
