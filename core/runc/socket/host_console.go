//go:build !windows

package socket

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/containerd/console"
	gorunc "github.com/containerd/go-runc"
	"github.com/walteh/runm/core/runc/file"
	"github.com/walteh/runm/core/runc/runtime"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
)

var _ runtime.ConsoleSocket = &HostConsoleSocket{}

type HostConsoleSocket struct {
	socket      runtime.AllocatedSocketWithUnixConn
	referenceId string
}

func (h *HostConsoleSocket) FileConn() file.FileConn {
	return h.socket.Conn()
}

func (h *HostConsoleSocket) Close() error {
	return h.socket.Conn().Close() // TODO: close the other end of the socket
}

func (h *HostConsoleSocket) UnixConn() *net.UnixConn {
	return h.socket.UnixConn()
}

func (h *HostConsoleSocket) GetReferenceId() string {
	return h.referenceId
}

func (h *HostConsoleSocket) Path() string {
	return h.socket.UnixConn().LocalAddr().String()
}

func (h *HostConsoleSocket) ReceiveMaster() (console.Console, error) {
	f, err := RecvFd(h.socket.UnixConn())
	if err != nil {
		return nil, err
	}
	return console.ConsoleFromFile(f)
}

func NewHostUnixConsoleSocket(ctx context.Context, referenceId string, socket runtime.AllocatedSocketWithUnixConn) (*HostConsoleSocket, error) {
	tmp, err := gorunc.NewTempConsoleSocket()
	if err != nil {
		return nil, err
	}

	// connect to the unix socket
	runcUnixConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: tmp.Path(), Net: "unix"})
	if err != nil {
		return nil, errors.Errorf("failed to dial unix socket: %w", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(runcUnixConn, socket.UnixConn())
	}()

	go func() {
		defer wg.Done()
		io.Copy(socket.UnixConn(), runcUnixConn)
	}()

	go func() {
		wg.Wait()
		runcUnixConn.Close()
		tmp.Close()
	}()

	return &HostConsoleSocket{
		socket:      socket,
		referenceId: referenceId,
	}, nil

	// return &HostConsoleSocket{socket: socket, path: tmp.Path(), conn: tmp.Conn().(*net.UnixConn)}, nil
}

// func NewHostVsockFdConsoleSocket(ctx context.Context, socket runtime.VsockAllocatedSocket, proxier runtime.VsockProxier) (*HostConsoleSocket, error) {
// 	if
// 	return &HostConsoleSocket{socket: socket, path: path, conn: conn}, nil
// }

// func NewHostConsoleSocket(ctx context.Context, socket ) (*HostConsoleSocket, error) {
// 	switch v := socket.(type) {
// 	case *SimpleVsockConn:
// 		proxy, err := CreateLocalVsockProxyConn(ctx, v)
// 		if err != nil {
// 			return nil, errors.Errorf("creating local vsock proxy: %w", err)
// 		}
// 		return NewHostUnixConsoleSocket(ctx, proxy)
// 	case *SimpleVsockProxyConn:
// 		return NewHostUnixConsoleSocket(ctx, v)
// 	default:
// 		return nil, errors.Errorf("invalid socket type: %T", socket)
// 	}
// }

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
