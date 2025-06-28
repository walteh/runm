package socket

import (
	"context"
	"log/slog"
	"net"
	"os"

	"github.com/containerd/console"
	gorunc "github.com/containerd/go-runc"
	"github.com/walteh/runm/core/runc/file"
	"github.com/walteh/runm/core/runc/runtime"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
)

var _ runtime.ConsoleSocket = &HostConsoleSocket{}

type HostConsoleSocket struct {
	socket runtime.AllocatedSocket
	path   string
	conn   *net.UnixConn
	// unusedfd uintptr
}

func (h *HostConsoleSocket) FileConn() file.FileConn {
	return h.conn
}

func (h *HostConsoleSocket) Close() error {
	return h.conn.Close()
}

func (h *HostConsoleSocket) Path() string {
	return h.path
}

func (h *HostConsoleSocket) ReceiveMaster() (console.Console, error) {

	f, err := RecvFd(h.conn)
	if err != nil {
		return nil, err
	}
	return console.ConsoleFromFile(f)
}

func NewHostUnixConsoleSocket(ctx context.Context, socket runtime.UnixAllocatedSocket) (runtime.ConsoleSocket, error) {
	tmp, err := gorunc.NewTempConsoleSocket()
	if err != nil {
		return nil, err
	}

	// bind the two together
	err = BindConsoleToSocket(ctx, tmp, socket)
	if err != nil {
		return nil, err
	}

	return tmp, nil

	// return &HostConsoleSocket{socket: socket, path: tmp.Path(), conn: tmp.Conn().(*net.UnixConn)}, nil
}

func NewHostVsockFdConsoleSocket(ctx context.Context, socket runtime.VsockAllocatedSocket, proxier runtime.VsockProxier) (*HostConsoleSocket, error) {
	conn, path, err := proxier.ProxyVsock(ctx, socket.Port())
	if err != nil {
		return nil, err
	}
	return &HostConsoleSocket{socket: socket, path: path, conn: conn}, nil
}

func NewHostConsoleSocket(ctx context.Context, socket runtime.AllocatedSocket, proxier runtime.VsockProxier) (runtime.ConsoleSocket, error) {
	switch v := socket.(type) {
	case runtime.UnixAllocatedSocket:
		return NewHostUnixConsoleSocket(ctx, v)
	case runtime.VsockAllocatedSocket:
		return NewHostVsockFdConsoleSocket(ctx, v, proxier)
	default:
		return nil, errors.Errorf("invalid socket type: %T", socket)
	}
}

func RecvFd(socket *net.UnixConn) (*os.File, error) {
	const MaxNameLen = 4096
	oobSpace := unix.CmsgSpace(4)

	name := make([]byte, MaxNameLen)
	oob := make([]byte, oobSpace)

	slog.Info("recvfd - A")

	n, oobn, _, _, err := socket.ReadMsgUnix(name, oob)
	if err != nil {
		return nil, err
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
