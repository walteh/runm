package env

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
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

	proxySock, err := net.Listen("unix", ShimRawWriterSockPath())
	if err != nil {
		slog.Error("Failed to create log proxy socket", "error", err, "path", ShimRawWriterSockPath())
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

	proxySockDelim, err := net.Listen("unix", ShimDelimitedWriterSockPath())
	if err != nil {
		slog.Error("Failed to create log proxy socket", "error", err, "path", ShimDelimitedWriterSockPath())
		os.Exit(1)
	}

	go func() {
		defer proxySockDelim.Close()
		for {
			conn, err := proxySockDelim.Accept()
			if err != nil {
				slog.Error("Failed to accept log proxy connection", "error", err)
				return
			}
			go func() {
				err := logging.HandleDelimitedProxy(ctx, conn, os.Stdout, constants.VsockDelimitedLogProxyDelimiter)
				if err != nil {
					slog.Error("Failed to handle log proxy connection", "error", err)
				}
			}()
		}
	}()

	oldPath := os.Getenv("PATH")
	newPath := filepath.Dir(ShimSimlinkPath()) + string(os.PathListSeparator) + oldPath
	os.Setenv("PATH", newPath)

	return nil
}
