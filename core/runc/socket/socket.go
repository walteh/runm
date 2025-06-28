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

func BindIOToSockets(ctx context.Context, ios runtime.IO, stdin, stdout, stderr runtime.AllocatedSocket) error {

	if ios == nil {
		return errors.Errorf("ios is nil")
	}

	if stdin != nil {
		go func() {
			if err := stdin.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stdin", "error", err)
				return
			}
			io.Copy(ios.Stdin(), stdin.Conn())
		}()
	}
	if stdout != nil {
		go func() {
			if err := stdout.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stdout", "error", err)
				return
			}
			io.Copy(stdout.Conn(), ios.Stdout())
		}()
	}
	if stderr != nil {
		go func() {
			if err := stderr.Ready(); err != nil {
				slog.ErrorContext(ctx, "failed to ready stderr", "error", err)
				return
			}
			io.Copy(stderr.Conn(), ios.Stderr())
		}()
	}

	return nil
}
