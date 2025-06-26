package env

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
)

func SetupReexec(ctx context.Context, justSymlinks bool) error {

	os.MkdirAll(filepath.Dir(ShimSimlinkPath()), 0755)
	os.MkdirAll(filepath.Dir(CtrSimlinkPath()), 0755)
	os.MkdirAll(filepath.Dir(ShimRuncSimlinkPath()), 0755)

	self, _ := os.Executable()
	os.Remove(ShimSimlinkPath())
	os.Remove(CtrSimlinkPath())
	os.Remove(ShimRuncSimlinkPath())

	if err := os.Symlink(self, ShimSimlinkPath()); err != nil {
		slog.Error("create shim link", "error", err)
		os.Exit(1)
	}

	if err := os.Symlink(self, CtrSimlinkPath()); err != nil {
		slog.Error("create ctr link", "error", err)
		os.Exit(1)
	}

	if err := os.Symlink(self, ShimRuncSimlinkPath()); err != nil {
		slog.Error("create runc link", "error", err)
		os.Exit(1)
	}

	if justSymlinks {
		return nil
	}

	otelProxySock, err := net.Listen("unix", ShimOtelProxySockPath())
	if err != nil {
		slog.Error("Failed to create otel proxy socket", "error", err, "path", ShimOtelProxySockPath())
		os.Exit(1)
	}

	proxySock, err := net.Listen("unix", ShimLogProxySockPath())
	if err != nil {
		slog.Error("Failed to create log proxy socket", "error", err, "path", ShimLogProxySockPath())
		os.Exit(1)
	}

	// fwd logs from the proxy socket to stdout
	go func() {
		defer proxySock.Close()
		for {
			conn, err := proxySock.Accept()
			if err != nil {
				slog.Error("Failed to accept log proxy connection", "error", err)
				return
			}
			go func() { _, _ = io.Copy(os.Stdout, conn) }()
		}
	}()

	tcpconn, err := net.Dial("tcp", ":4317")
	if err != nil {
		slog.Error("failed to dial otel proxy FAILED", "error", err)
	} else {
		slog.Info("dialed otel proxy SUCCESS", "conn", tcpconn)
	}

	go func() {
		defer otelProxySock.Close()
		defer tcpconn.Close()
		for {
			conn, err := otelProxySock.Accept()
			if err != nil {
				slog.Error("Failed to accept otel proxy connection", "error", err)
				return
			}
			slog.Info("accepted otel proxy connection", "conn", conn)
			if tcpconn != nil {
				go func() {
					_, _ = io.Copy(tcpconn, conn)
					_, _ = io.Copy(conn, tcpconn)
				}()
			} else {
				slog.Error("no otel proxy connection", "conn", conn)
			}
		}
	}()

	oldPath := os.Getenv("PATH")
	newPath := filepath.Dir(ShimSimlinkPath()) + string(os.PathListSeparator) + oldPath
	os.Setenv("PATH", newPath)

	return nil
}
