package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/walteh/go-errors"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
)

var delimSockPath string
var rawSockPath string
var force bool

func main() {

	ctx := context.Background()

	flag.StringVar(&delimSockPath, "delim-sock-path", "/tmp/devlog-delim.sock", "Path to the delimited log socket")
	flag.StringVar(&rawSockPath, "raw-sock-path", "/tmp/devlog-raw.sock", "Path to the raw log socket")
	flag.BoolVar(&force, "force", false, "Force removal of existing sockets")
	flag.Parse()

	err := run(ctx, force, delimSockPath, rawSockPath)
	if err != nil {
		slog.Error("problem running devlog", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, force bool, delimSockPath string, rawSockPath string) error {
	if _, err := os.Stat(rawSockPath); err == nil {
		if force {
			os.Remove(rawSockPath)
		} else {
			return errors.Errorf("raw socket already exists: %s", rawSockPath)
		}
	}
	if _, err := os.Stat(delimSockPath); err == nil {
		if force {
			os.Remove(delimSockPath)
		} else {
			return errors.Errorf("delim socket already exists: %s", delimSockPath)
		}
	}

	defer func() {
		os.Remove(rawSockPath)
		os.Remove(delimSockPath)
	}()

	proxySock, err := net.Listen("unix", rawSockPath)
	if err != nil {
		return errors.Errorf("failed to create log proxy socket: %w", err)
	}

	proxySockDelim, err := net.Listen("unix", delimSockPath)
	if err != nil {
		return errors.Errorf("failed to create log proxy socket: %w", err)
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

	// wait for signals
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	sig := <-ch

	slog.Info("shutting down devlog", "signal", sig)

	return nil
}
