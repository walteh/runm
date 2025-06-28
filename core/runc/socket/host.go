package socket

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/walteh/runm/core/runc/runtime"
	"gitlab.com/tozd/go/errors"
)

type HostAllocatedSocket struct {
	conn        *net.UnixConn
	path        string
	referenceId string
}

func (h *HostAllocatedSocket) isAllocatedSocket() {}

func (h *HostAllocatedSocket) Close() error {
	return h.conn.Close()
}

func (h *HostAllocatedSocket) Conn() runtime.FileConn {
	return h.conn
}

func (h *HostAllocatedSocket) Path() string {
	return h.path
}

func (h *HostAllocatedSocket) Ready() error {
	return nil
}

func (h *HostAllocatedSocket) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return h.conn, nil
}

func NewHostAllocatedVsockSocket(ctx context.Context, port uint32, refId string, proxier runtime.VsockProxier) (*HostAllocatedSocket, error) {
	conn, path, err := proxier.ProxyVsock(ctx, port)
	if err != nil {
		return nil, errors.Errorf("proxying vsock: %w", err)
	}
	return &HostAllocatedSocket{conn: conn, path: path, referenceId: refId}, nil
}

func NewHostAllocatedUnixSocket(ctx context.Context, path string, refId string) (*HostAllocatedSocket, error) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "new host allocated unix socket", "path", path, "refId", refId)
	return &HostAllocatedSocket{conn: conn, path: path, referenceId: refId}, nil
}

func NewHostAllocatedSocketFromId(ctx context.Context, id string, proxier runtime.VsockProxier) (*HostAllocatedSocket, error) {
	switch {
	case strings.HasPrefix(id, "socket:vsock:"):
		port, err := strconv.Atoi(strings.TrimPrefix(id, "socket:vsock:"))
		if err != nil {
			return nil, errors.Errorf("allocating vsock socket: %w", err)
		}
		sock, err := NewHostAllocatedVsockSocket(ctx, uint32(port), id, proxier)
		if err != nil {
			return nil, errors.Errorf("allocating vsock socket: %w", err)
		}
		return sock, nil
	case strings.HasPrefix(id, "socket:unix:"):
		path := strings.TrimPrefix(id, "socket:unix:")
		sock, err := NewHostAllocatedUnixSocket(ctx, path, id)
		if err != nil {
			return nil, errors.Errorf("allocating unix socket: %w", err)
		}
		return sock, nil
	}
	return nil, errors.Errorf("invalid socket type: %s", id)
}
