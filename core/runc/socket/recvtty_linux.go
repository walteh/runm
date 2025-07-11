//go:build linux

package socket

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/containerd/console"
	"github.com/opencontainers/runc/libcontainer/utils"
	"gitlab.com/tozd/go/errors"
)

func RecvttyProxy(ctx context.Context, path string, proxy net.Conn) error {
	slog.DebugContext(ctx, "RECVTTYPROXY[A] opening socket", "path", path)
	// Open a socket.
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer ln.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[B] accepting connection", "path", path)

	// We only accept a single connection, since we can only really have
	// one reader for os.Stdin. Plus this is all a PoC.
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[C] closing socket", "path", path)

	// Close ln, to allow for other instances to take over.
	ln.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[D] getting fd of connection", "path", path)

	// Get the fd of the connection.
	unixconn, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("failed to cast to unixconn")
	}

	slog.DebugContext(ctx, "RECVTTYPROXY[E] getting file from connection", "path", path)

	socket, err := unixconn.File()
	if err != nil {
		return err
	}
	defer socket.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[F] getting master file descriptor from runC", "path", path)

	// Get the master file descriptor from runC.
	master, err := utils.RecvFile(socket)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "RECVTTYPROXY[G] getting console from master file descriptor", "path", path)

	c, err := console.ConsoleFromFile(master)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "RECVTTYPROXY[H] clearing ONLCR", "path", path)

	if err := console.ClearONLCR(c.Fd()); err != nil {
		return err
	}

	slog.DebugContext(ctx, "RECVTTYPROXY[I] copying from our stdio to the master fd", "path", path)

	// Copy from our stdio to the master fd.
	var (
		wg            sync.WaitGroup
		inErr, outErr error
	)
	wg.Add(1)
	go func() {
		_, outErr = io.Copy(proxy, c)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		_, inErr = io.Copy(c, proxy)
		wg.Done()
	}()

	slog.DebugContext(ctx, "RECVTTYPROXY[J] waiting for copying to finish", "path", path)

	// Only close the master fd once we've stopped copying.
	wg.Wait()
	c.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[K] done", "path", path, "outErr", outErr, "inErr", inErr)

	if outErr != nil {
		return outErr
	}

	return inErr
}
