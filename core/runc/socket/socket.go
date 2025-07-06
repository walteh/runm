package socket

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"sync"

	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/pkg/conn"
)

func debugReader(ctx context.Context, name string, r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	tr := io.TeeReader(r, pw)
	// reaqd one byte at a time from r and print it to stdout
	go func() {
		slog.InfoContext(ctx, "starting debugReader", "name", name)
		for ctx.Err() == nil {

			buf := make([]byte, 1)
			_, err := pr.Read(buf)
			if err != nil {
				return
			}
			slog.InfoContext(ctx, "captured byte", "name", name, "byte", buf[0])
		}
	}()
	return tr
}

func simpleUnixProxyConn(ctx context.Context, sock runtime.AllocatedSocket) (*net.UnixConn, error) {
	if sockConn, ok := sock.Conn().(*net.UnixConn); ok {
		return sockConn, nil
	}
	// create a socketpair
	return conn.CreateUnixConnProxy(ctx, sock.Conn())
}

// runc.ConsoleSocket -> console.Console -> my.Socket
// BindConsoleToSocket implements runtime.SocketAllocator.
func BindRuncConsoleToSocket(ctx context.Context, cons runtime.ConsoleSocket, sock runtime.AllocatedSocket) error {

	// // open up the console socket path, and create a pipe to it

	consConn := cons.UnixConn()
	sockConn := sock.Conn()

	wg := sync.WaitGroup{}
	wg.Add(2)

	// create a goroutine to read from the pipe and write to the socket
	go func() {
		defer wg.Done()
		io.Copy(consConn, debugReader(ctx, "consConn", sockConn))
	}()

	// create a goroutine to read from the socket and write to the console
	go func() {
		defer wg.Done()
		io.Copy(sockConn, debugReader(ctx, "sockConn", consConn))
	}()

	go func() {
		wg.Wait()
		consConn.Close()
		sockConn.Close()
	}()

	return nil
}

func halfCloseWrite(conn net.Conn) {
	// Attempt a half-close if supported (e.g., Unix domain or TCP)
	// if uc, ok := conn.(*net.UnixConn); ok {
	// 	uc.CloseWrite() // sends FIN (EOF) to peer  [oai_citation:8‡github.com](https://github.com/golang/go/issues/67337?utm_source=chatgpt.com)
	// 	conn.Close()
	// } else {
	// 	conn.Close() // fallback: full close
	// }

	conn.Close()
}

type breakOnZeroReader struct {
	r io.Reader
}

func (b *breakOnZeroReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

func copyLoggingAllBytesSomehow(ctx context.Context, name string, w io.Writer, r io.Reader) (int64, error) {
	pr, pw := io.Pipe()
	wrapped := &breakOnZeroReader{r: r}

	nr := io.TeeReader(wrapped, pw)
	go func() {
		for {
			buf := bufio.NewScanner(pr)
			buf.Split(bufio.ScanLines)
			buf.Scan()
			err := buf.Err()
			if err != nil {
				slog.ErrorContext(ctx, "error reading byte", "name", name, "error", err)
				return
			}
			slog.InfoContext(ctx, "teed line for reader", "name", name, "data", buf.Text())
		}
	}()
	return io.Copy(w, nr)
}

func BindIOToSockets(ctx context.Context, ios runtime.IO, stdin, stdout, stderr runtime.AllocatedSocket) error {

	if ios == nil {
		return errors.Errorf("ios is nil")
	}

	var wg sync.WaitGroup

	// stdin: host socket → container Stdin()
	if stdin != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := stdin.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stdin", "error", err)
				return
			}
			slog.InfoContext(ctx, "copying stdin")
			_, err := copyLoggingAllBytesSomehow(ctx, "stdin", ios.Stdin(), stdin.Conn())
			if err != nil {
				slog.WarnContext(ctx, "closing io.Stdin with error", "error", err)
			} else {
				slog.InfoContext(ctx, "closing io.Stdin with NO error")
			}
			ios.Stdin().Close() // close FIFO writer to signal EOF
			halfCloseWrite(stdin.Conn())
		}()
	}

	// stdout: container Stdout() → host socket
	if stdout != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := stdout.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stdout", "error", err)
				return
			}
			slog.InfoContext(ctx, "copying stdout")
			_, err := copyLoggingAllBytesSomehow(ctx, "stdout", stdout.Conn(), ios.Stdout())
			if err != nil {
				slog.WarnContext(ctx, "closing io.Stdout with error", "error", err)
			} else {
				slog.InfoContext(ctx, "closing io.Stdout with NO error")
			}
			halfCloseWrite(stdout.Conn()) // signal EOF to guest reader
		}()
	}

	// stderr: container Stderr() → host socket
	if stderr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := stderr.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stderr", "error", err)
				return
			}
			slog.InfoContext(ctx, "copying stderr")
			_, err := copyLoggingAllBytesSomehow(ctx, "stderr", stderr.Conn(), ios.Stderr())
			if err != nil {
				slog.WarnContext(ctx, "closing io.Stderr with error", "error", err)
			} else {
				slog.InfoContext(ctx, "closing io.Stderr with NO error")
			}
			halfCloseWrite(stderr.Conn()) // signal EOF
		}()
	}

	return nil
}

// type allocatedSocketWithUnixConn struct {
// 	runtime.AllocatedSocket
// 	unixConn *net.UnixConn
// }

// func NewAllocatedSocketWithUnixConn(socket runtime.AllocatedSocket) runtime.AllocatedSocketWithUnixConn {
// 	if unixConn, ok := socket.(runtime.AllocatedSocketWithUnixConn); ok {
// 		return unixConn
// 	}
// 	if unixConn, ok := socket.(*SimpleVsockProxyConn); ok {
// 		return &allocatedSocketWithUnixConn{socket: socket, unixConn: unixConn.UnixConn()}
// 	}
// 	return nil
// }

// func (a *allocatedSocketWithUnixConn) forward() (*net.UnixConn, error) {
// 	// create a socketpair
// 	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
// 	if err != nil {
// 		return nil, errors.Errorf("creating socketpair: %w", err)
// 	}
// 	unixConn, err := net.FileConn(os.NewFile(uintptr(fds[0]), "external.socket"))
// 	if err != nil {
// 		return nil, errors.Errorf("converting file to UnixConn: %w", err)
// 	}
// 	proxyConn, err := net.FileConn(os.NewFile(uintptr(fds[1]), "internal.socket"))
// 	if err != nil {
// 		return nil, errors.Errorf("converting file to UnixConn: %w", err)
// 	}
// 	return proxyConn, nil
// }
