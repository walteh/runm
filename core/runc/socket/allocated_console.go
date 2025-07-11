package socket

import (
	"context"
	"log/slog"
	"net"
	"os"
	"reflect"

	"github.com/containerd/console"
	"github.com/rs/xid"
	"github.com/walteh/runm/core/runc/runtime"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
)

var _ runtime.ConsoleSocket = &guestPathProviderConsoleSocket{}

type guestPathProviderConsoleSocket struct {
	path string
}

func NewGuestPathProviderConsoleSocket() runtime.ConsoleSocket {
	path := "/tmp/runm-console-proxy." + xid.New().String() + ".sock"

	return &guestPathProviderConsoleSocket{
		path: path,
	}
}

func (g *guestPathProviderConsoleSocket) Close() error {
	os.Remove(g.path)
	return nil
}

func (g *guestPathProviderConsoleSocket) Path() string {
	return g.path
}

func (g *guestPathProviderConsoleSocket) ReceiveMaster() (console.Console, error) {
	return nil, errors.New("not implemented")
}

func BindAllocatedSocketConsole(ctx context.Context, sock runtime.AllocatedSocket, pathz runtime.ConsoleSocket) error {

	var path string
	if pathzd, ok := pathz.(*guestPathProviderConsoleSocket); ok {
		path = pathzd.Path()
	} else {
		return errors.New("path is not a guestPathProviderConsoleSocket, got: " + reflect.TypeOf(pathz).String())
	}

	// path := "/tmp/runm-console-proxy." + xid.New().String() + ".sock"

	go func() {
		err := RecvttyProxy(ctx, path, sock.Conn())
		if err != nil {
			slog.Error("error handling single", "error", err)
		}
		os.Remove(path)
	}()

	return nil
}

// type allocatedSocketConsole struct {
// 	ctx             context.Context
// 	allocatedSocket runtime.AllocatedSocket
// 	path            string

// 	extraClosers []io.Closer
// }

// func NewAllocatedSocketConsole(ctx context.Context, sock runtime.AllocatedSocket) (*allocatedSocketConsole, error) {

// 	path := "/tmp/runm-console-proxy." + xid.New().String() + ".sock"

// 	return &allocatedSocketConsole{
// 		ctx:             ctx,
// 		allocatedSocket: sock,
// 		path:            path,
// 		extraClosers:    []io.Closer{},
// 	}, nil
// }

// func (a *allocatedSocketConsole) Path() string {
// 	return a.path
// }

// // Close implements runtime.ConsoleSocket.
// func (a *allocatedSocketConsole) Close() error {
// 	for _, closer := range a.extraClosers {
// 		closer.Close()
// 	}
// 	if a.allocatedSocket != nil {
// 		a.allocatedSocket.Close()
// 	}
// 	return nil
// }

// // Path implements runtime.ConsoleSocket.

// // ReceiveMaster implements runtime.ConsoleSocket.
// func (a *allocatedSocketConsole) ReceiveMaster() (console.Console, error) {
// 	// f, err := RecvFd(a.unixConn)
// 	// if err != nil {
// 	// 	return nil, errors.Errorf("receiving master: %w", err)
// 	// }
// 	// return console.ConsoleFromFile(f)
// 	return nil, errors.New("not implemented")
// }

// UnixConn implements runtime.ConsoleSocket.
// func (a *allocatedSocketConsole) UnixConn() *net.UnixConn {
// 	return a.unixConn
// }

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
