package recvtty

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/containerd/console"
	"github.com/walteh/runm/pkg/conn"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
)

type RecvttyConnProxy struct {
	console console.Console
	proxy   net.Conn
}

func (r *RecvttyConnProxy) Console() console.Console {
	return r.console
}

func NewRecvttyProxy(ctx context.Context, path string, proxy net.Conn) (*RecvttyConnProxy, error) {
	slog.DebugContext(ctx, "RECVTTYPROXY[A] opening socket", "path", path)
	// Open a socket.
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}

	defer os.Remove(path)
	defer ln.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[B] accepting connection", "path", path)

	// We only accept a single connection, since we can only really have
	// one reader for os.Stdin. Plus this is all a PoC.
	connz, err := ln.Accept()
	if err != nil {
		return nil, err
	}
	defer connz.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[C] closing socket", "path", path)

	// Close ln, to allow for other instances to take over.
	ln.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[D] getting fd of connection", "path", path)

	slog.DebugContext(ctx, "RECVTTYPROXY[E] getting file from connection", "path", path)

	socket, err := connz.(*net.UnixConn).File()
	if err != nil {
		return nil, err
	}
	defer socket.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[F] getting master file descriptor from runC", "path", path)

	// Get the master file descriptor from runC.
	master, err := recvFile(socket)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "RECVTTYPROXY[G] getting console from master file descriptor", "path", path)

	c, err := console.ConsoleFromFile(master)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "RECVTTYPROXY[H] clearing ONLCR", "path", path)

	if err := console.ClearONLCR(c.Fd()); err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "RECVTTYPROXY[I] copying from our stdio to the master fd", "path", path)

	return &RecvttyConnProxy{
		console: c,
		proxy:   connz,
	}, nil
}

func (r *RecvttyConnProxy) Run(ctx context.Context) error {
	// Copy from our stdio to the master fd.
	var (
		wg            sync.WaitGroup
		inErr, outErr error
	)
	wg.Add(1)
	go func() {
		outErr = <-conn.DebugCopy(ctx, "pty(read)->network(write)", r.proxy, r.console)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		inErr = <-conn.DebugCopy(ctx, "network(read)->pty(write)", r.console, r.proxy)
		wg.Done()
	}()

	slog.DebugContext(ctx, "RECVTTYPROXY[J] waiting for copying to finish")

	// Only close the master fd once we've stopped copying.
	wg.Wait()
	r.console.Close()

	slog.DebugContext(ctx, "RECVTTYPROXY[K] done", "outErr", outErr, "inErr", inErr)

	if outErr != nil {
		return outErr
	}

	return inErr
}

func RecvFd(socket *net.UnixConn) (*os.File, error) {
	const MaxNameLen = 4096
	oobSpace := unix.CmsgSpace(4)

	name := make([]byte, MaxNameLen)
	oob := make([]byte, oobSpace)

	slog.Info("recvfd - A")

	n, oobn, _, _, err := socket.ReadMsgUnix(name, oob)
	if err != nil {
		slog.Info("recvfd - A.1", "error", err)
		return nil, errors.Errorf("reading unix message from socket: %w", err)
	}

	slog.Info("recvfd - B")

	if n >= MaxNameLen || oobn != oobSpace {
		return nil, errors.Errorf("recvfd: incorrect number of bytes read (n=%d oobn=%d)", n, oobn)
	}

	// Truncate.
	name = name[:n]
	oob = oob[:oobn]

	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, err
	}
	if len(scms) != 1 {
		return nil, errors.Errorf("recvfd: number of SCMs is not 1: %d", len(scms))
	}
	scm := scms[0]

	fds, err := unix.ParseUnixRights(&scm)
	if err != nil {
		return nil, err
	}
	if len(fds) != 1 {
		return nil, errors.Errorf("recvfd: number of fds is not 1: %d", len(fds))
	}
	fd := uintptr(fds[0])

	return os.NewFile(fd, string(name)), nil
}
