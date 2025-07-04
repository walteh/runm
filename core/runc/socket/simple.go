package socket

import (
	"context"
	"net"

	"github.com/mdlayher/vsock"
	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.VsockAllocatedSocket = (*SimpleVsockConn)(nil)
var _ runtime.UnixAllocatedSocket = (*SimpleUnixConn)(nil)

type SimpleVsockConn struct {
	conn *vsock.Conn
	port uint32
}

func NewSimpleVsockConn(ctx context.Context, conn *vsock.Conn, port uint32) *SimpleVsockConn {
	return &SimpleVsockConn{conn: conn, port: port}
}

func (h *SimpleVsockConn) Port() uint32 {
	return h.port
}

func (h *SimpleVsockConn) Close() error {
	return h.conn.Close()
}

func (h *SimpleVsockConn) Conn() runtime.FileConn {
	return h.conn
}

func (h *SimpleVsockConn) Ready() error {
	return nil
}

func (h *SimpleVsockConn) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return h.conn, nil
}

type SimpleVsockProxyConn struct {
	conn *net.UnixConn
	port uint32
}

func NewSimpleVsockProxyConn(ctx context.Context, conn *net.UnixConn, port uint32) *SimpleVsockProxyConn {
	return &SimpleVsockProxyConn{conn: conn, port: port}
}

func (h *SimpleVsockProxyConn) Port() uint32 {
	return h.port
}

func (h *SimpleVsockProxyConn) Close() error {
	return h.conn.Close()
}

func (h *SimpleVsockProxyConn) Conn() runtime.FileConn {
	return h.conn
}

func (h *SimpleVsockProxyConn) Ready() error {
	return nil
}

func (h *SimpleVsockProxyConn) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return h.conn, nil
}

type SimpleUnixConn struct {
	conn *net.UnixConn
	path string
}

func NewSimpleUnixConn(ctx context.Context, conn *net.UnixConn, path string) *SimpleUnixConn {
	return &SimpleUnixConn{conn: conn, path: path}
}

func (h *SimpleUnixConn) Path() string {
	return h.path
}

func (h *SimpleUnixConn) Close() error {
	return h.conn.Close()
}

func (h *SimpleUnixConn) Conn() runtime.FileConn {
	return h.conn
}

func (h *SimpleUnixConn) Ready() error {
	return nil
}

func (h *SimpleUnixConn) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return h.conn, nil
}
