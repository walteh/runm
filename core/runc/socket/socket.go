package socket

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/walteh/runm/core/runc/runtime"
	"gitlab.com/tozd/go/errors"
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

// runc.ConsoleSocket -> console.Console -> my.Socket
// BindConsoleToSocket implements runtime.SocketAllocator.
func BindConsoleToSocket(ctx context.Context, cons runtime.ConsoleSocket, sock runtime.AllocatedSocket) error {

	// // open up the console socket path, and create a pipe to it
	consConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: cons.Path(), Net: "unix"})
	if err != nil {
		return errors.Errorf("failed to dial console socket: %w", err)
	}
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

	// return the pipe
	return nil
}

func halfCloseWrite(conn net.Conn) {
	// Attempt a half-close if supported (e.g., Unix domain or TCP)
	if uc, ok := conn.(interface{ CloseWrite() error }); ok {
		uc.CloseWrite() // sends FIN (EOF) to peer  [oai_citation:8‡github.com](https://github.com/golang/go/issues/67337?utm_source=chatgpt.com)
	} else {
		conn.Close() // fallback: full close
	}
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
			buf := make([]byte, 1)
			_, err := pr.Read(buf)
			if err != nil {
				slog.ErrorContext(ctx, "error reading byte", "name", name, "error", err)
				return
			}
			slog.InfoContext(ctx, "copied byte", "name", name, "byte", buf[0], "string", string(buf))
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
			copyLoggingAllBytesSomehow(ctx, "stdin", ios.Stdin(), stdin.Conn())
			slog.InfoContext(ctx, "closing io.Stdin()")
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
			copyLoggingAllBytesSomehow(ctx, "stdout", stdout.Conn(), ios.Stdout())
			slog.InfoContext(ctx, "closing stdout")
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
			copyLoggingAllBytesSomehow(ctx, "stderr", stderr.Conn(), ios.Stderr())
			slog.InfoContext(ctx, "closing stderr")
			halfCloseWrite(stderr.Conn()) // signal EOF
		}()
	}

	return nil
}
