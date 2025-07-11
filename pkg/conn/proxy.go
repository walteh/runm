//go:build !windows

package conn

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"reflect"
	"syscall"
	"time"

	"gitlab.com/tozd/go/errors"

	"github.com/rs/xid"
	"github.com/walteh/runm/pkg/logging"
)

func CopyLoggingErrors(ctx context.Context, dst io.Writer, src io.Reader) error {
	n, err := io.Copy(dst, src)
	logging.LogRecord(ctx, slog.LevelDebug, 1, "copying completed", "observed_error", err, "error_type", reflect.TypeOf(err), "bytes_copied", n)
	return err
}

func CreateUnixConnProxy(ctx context.Context, conn net.Conn) (*net.UnixConn, error) {
	// Create a socketpair - two connected Unix domain sockets
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, errors.Errorf("creating socketpair: %w", err)
	}

	// Convert raw file descriptors to *os.File objects
	f1 := os.NewFile(uintptr(fds[0]), "external.socket")
	f2 := os.NewFile(uintptr(fds[1]), "internal.socket")

	// Close the original fds as NewFile duplicates them
	// syscall.Close(fds[0])
	// syscall.Close(fds[1])

	// Convert one end to a UnixConn (this will be returned)
	unixConn, err := net.FileConn(f1)
	if err != nil {
		f1.Close()
		f2.Close()
		return nil, errors.Errorf("converting file to UnixConn: %w", err)
	}

	// Convert the other end to a regular connection
	proxyConn, err := net.FileConn(f2)
	if err != nil {
		unixConn.Close()
		f1.Close()
		f2.Close()
		return nil, errors.Errorf("converting file to UnixConn: %w", err)
	}

	// Start proxying data in both directions
	go func() {
		defer conn.Close()
		defer proxyConn.Close()
		CopyLoggingErrors(ctx, proxyConn, conn)
	}()

	go func() {
		defer conn.Close()
		defer proxyConn.Close()
		CopyLoggingErrors(ctx, conn, proxyConn)
	}()

	return unixConn.(*net.UnixConn), nil
}

func CreateUnixFileProxy(ctx context.Context) (string, *net.UnixConn, error) {

	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return "", nil, errors.Errorf("creating socketpair: %w", err)
	}

	// Convert raw file descriptors to *os.File objects
	f1 := os.NewFile(uintptr(fd), "external.socket")

	// Convert one end to a UnixConn (this will be returned)
	unixConnF, err := net.FileConn(f1)
	if err != nil {
		f1.Close()
		return "", nil, errors.Errorf("converting file to UnixConn: %w", err)
	}

	unixConn, ok := unixConnF.(*net.UnixConn)
	if !ok {
		return "", nil, errors.Errorf("unixConn is not a *net.UnixConn")
	}

	file := "/tmp/runm-file-pipe-proxy." + xid.New().String() + ".sock"

	// create a unix socket that we will forward all reads and writes too
	listener, err := net.Listen("unix", file)
	if err != nil {
		return "", nil, errors.Errorf("creating listener: %w", err)
	}

	go func() {

		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func() {
				defer conn.Close()
				defer listener.Close()
				CopyLoggingErrors(ctx, conn, unixConn)
			}()

			go func() {
				defer conn.Close()
				defer listener.Close()
				CopyLoggingErrors(ctx, unixConn, conn)
			}()
		}
	}()

	return file, unixConn, nil
}

// func CreateUnixListenerProxy(ctx context.Context, establishedConn net.Conn) (net.Listener, error) {
// 	listener, err := net.Listen("unix", "")
// 	if err != nil {
// 		return nil, errors.Errorf("creating listener: %w", err)
// 	}

// 	go func() {
// 		defer establishedConn.Close()
// 		defer listener.Close()
// 		CopyLoggingErrors(ctx, listener, establishedConn)
// 	}()

// 	go func() {
// 		defer establishedConn.Close()
// 		defer listener.Close()
// 		CopyLoggingErrors(ctx, establishedConn, listener)
// 	}()

// 	return listener, nil
// }

func ListenAndAcceptSingleNetConn(ctx context.Context, listenerCallback func(ctx context.Context) (net.Listener, error), dialCallback func(ctx context.Context) error) (net.Conn, error) {
	listener, err := listenerCallback(ctx)
	if err != nil {
		return nil, errors.Errorf("creating listener: %w", err)
	}
	defer listener.Close()

	connch := make(chan any, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			connch <- errors.Errorf("accepting connection: %w", err)
		} else {
			connch <- conn
		}
	}()

	go func() {
		err := dialCallback(ctx)
		if err != nil {
			connch <- errors.Errorf("dialing connection: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		return nil, errors.Errorf("context done: %w", ctx.Err())
	case <-time.After(10 * time.Second):
		return nil, errors.New("timeout waiting for connection")
	case ch := <-connch:
		switch ch := ch.(type) {
		case error:
			return nil, errors.Errorf("waiting for connection: %w", ch)
		case net.Conn:
			return ch, nil
		default:
			return nil, errors.Errorf("unexpected type: %T", ch)
		}
	}

}
