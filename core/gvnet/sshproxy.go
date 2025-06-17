package gvnet

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"

	"golang.org/x/sync/errgroup"

	"github.com/containers/gvisor-tap-vsock/pkg/services/forwarder"
	"github.com/containers/gvisor-tap-vsock/pkg/tcpproxy"
	"github.com/containers/gvisor-tap-vsock/pkg/transport"
	"github.com/soheilhy/cmux"
	"gitlab.com/tozd/go/errors"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

func buildForwards(ctx context.Context, globalHostPort string, groupErrs *errgroup.Group, forwards map[int]cmux.Matcher) (cmux.CMux, map[string]net.Listener, error) {
	l, err := transport.Listen(globalHostPort)
	if err != nil {
		return nil, nil, errors.Errorf("listen: %w", err)
	}

	virtualPortMap := make(map[string]net.Listener)

	m := cmux.New(l)

	for guestPortTarget, matcher := range forwards {

		listener := m.Match(matcher)

		// hostProxyPort, err := port.ReservePort(ctx)
		// if err != nil {
		// 	return nil, nil, errors.Errorf("reserving ssh port: %w", err)
		// }

		// hostProxyPortStr := fmt.Sprintf("%s:%d", LOCAL_HOST_IP, hostProxyPort)
		guestPortTargetStr := fmt.Sprintf("%s:%d", VIRTUAL_GUEST_IP, guestPortTarget)

		// groupErrs.Go(func() error {
		// 	return ForwardListenerToPort(ctx, listener, hostProxyPortStr)
		// })

		virtualPortMap[guestPortTargetStr] = listener

	}

	return m, virtualPortMap, nil
}

func buildForwardsWithSwitch(ctx context.Context, globalHostPort string, switc *stack.Stack, groupErrs *errgroup.Group, forwards map[int]cmux.Matcher) (cmux.CMux, error) {
	l, err := transport.Listen(globalHostPort)
	if err != nil {
		return nil, errors.Errorf("listen: %w", err)
	}

	m := cmux.New(l)

	for guestPortTarget, matcher := range forwards {

		listener := m.Match(matcher)

		addr := listener.Addr().String()

		guestPortTargetStr := fmt.Sprintf("%s:%d", VIRTUAL_GUEST_IP, guestPortTarget)

		var proxy tcpproxy.Proxy
		proxy.ListenFunc = func(network, laddr string) (net.Listener, error) {
			return listener, nil
		}
		proxy.AddRoute(addr, &tcpproxy.DialProxy{
			Addr: guestPortTargetStr,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				addr, err := forwarder.TCPIPAddress(1, address)
				if err != nil {
					return nil, errors.Errorf("failed to get tcpip address: %w", err)
				}
				return gonet.DialContextTCP(ctx, switc, addr, ipv4.ProtocolNumber)
			},
		})

		groupErrs.Go(func() error {
			return proxy.Run()
		})

		// virtualPortMap[hostProxyPortStr] = guestPortTargetStr

	}

	return m, nil
}

// func ForwardListenerToPort(ctx context.Context, listener net.Listener, port string, errgroup *errgroup.Group) error {
// 	var proxy tcpproxy.Proxy

// 	proxy.ListenFunc = func(network, laddr string) (net.Listener, error) {
// 		return listener, nil
// 	}

// 	proxy.AddRoute(":", &tcpproxy.DialProxy{
// 		Addr: fmt.Sprintf("tcp://127.0.0.1:%s", port),
// 		// when there's a connection to the vsock listener, connect to the provided unix socket
// 		DialContext: func(ctx context.Context, _, addr string) (conn net.Conn, e error) {
// 			slog.InfoContext(ctx, "accepting connection", "addr", addr)
// 			return listener.Accept()
// 		},
// 	})

// 	err := proxy.Start()
// 	if err != nil {
// 		return errors.Errorf("failed to start proxy: %w", err)
// 	}

// 	return nil
// }

func ForwardListenerToPort(ctx context.Context, listener net.Listener, port string) error {
	p := new(tcpproxy.Proxy)

	addr := listener.Addr().String()

	p.ListenFunc = func(network, laddr string) (net.Listener, error) {
		return listener, nil
	}

	var dp *tcpproxy.DialProxy

	dp = &tcpproxy.DialProxy{
		Addr: port,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			slog.InfoContext(ctx, "dialing backend", "address", address)
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
		OnDialError: func(src net.Conn, err error) {
			slog.Error("backend dial error", "err", err)
			_ = src.Close()
		},
	}

	// this is effectivly the same as
	// dp = tcpproxy.To(port)
	// but we get better logging

	p.AddRoute(addr, dp)

	// 4) Run and wait for shutdown (blocks until ctx is canceled or an error occurs)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		_ = p.Close() // stops all proxy listeners
	}()
	if err := p.Run(); err != nil {
		return fmt.Errorf("tcp proxy failed: %w", err)
	}
	return nil
}

func ForwardListenerToPortOld(ctx context.Context, listener net.Listener, port string, errgroup *errgroup.Group) error {
	for {
		// Accept connection with timeout
		clientConn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil // Normal shutdown
			}
			return errors.Errorf("failed to accept: %w", err)
		}

		// Handle each client in a separate goroutine
		errgroup.Go(func() error {
			defer clientConn.Close()

			// muxconn := clientConn.(*cmux.MuxConn)

			slog.InfoContext(ctx, "forwarding connection", "client", clientConn.RemoteAddr(), "backend", port, "conntype", fmt.Sprintf("%T", clientConn))
			// Connect to the backend FOR THIS CLIENT
			backend, err := net.Dial("tcp", port)
			if err != nil {
				return errors.Errorf("failed to connect to backend: %w", err)
			}

			defer backend.Close()

			slog.InfoContext(ctx, "connected to backend", "backend", backend.RemoteAddr())

			// Use proper copying with context cancellation
			done := make(chan struct{}, 2)
			go func() {
				// CopyWithLoggingData(ctx, "client", clientConn, backend)
				io.Copy(backend, clientConn)
				done <- struct{}{}
			}()
			go func() {
				// CopyWithLoggingData(ctx, "backend", backend, clientConn)
				io.Copy(clientConn, backend)
				done <- struct{}{}
			}()

			// Wait for either copy to finish or context to cancel
			select {
			case <-done:
				return nil
			case <-ctx.Done():

				return ctx.Err()
			}
		})
	}
}

func CopyWithLoggingData(ctx context.Context, name string, src io.Reader, dst io.Writer) error {

	done := make(chan struct{}, 1)

	pr, pw := io.Pipe()

	tee := io.TeeReader(src, pw)

	go func() {
		io.Copy(dst, tee)
		done <- struct{}{}
	}()

	go func() {
		bufreader := bufio.NewScanner(pr)
		bufreader.Split(bufio.ScanBytes)
		slog.InfoContext(ctx, "starting to read from pipe")
		// log all content from the pw
		for bufreader.Scan() {
			// read each datagram from the pipe
			datagram := bufreader.Text()
			slog.InfoContext(ctx, "data", "name", name, "data", datagram)
		}
	}()

	<-done
	return nil
}
