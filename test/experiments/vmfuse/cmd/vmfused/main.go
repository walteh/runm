//go:build darwin

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/virt/vf"
	"github.com/walteh/runm/core/vmfuse/vmfused"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/pkg/taskgroup"
	"github.com/walteh/runm/test/integration/env"
)

var (
	serverAddr        string
	enableTestLogging bool
)

func init() {
	flag.StringVar(&serverAddr, "address", "", "address to listen on")
	flag.BoolVar(&enableTestLogging, "enable-test-logging", false, "enable test logging")
	flag.Parse()

	if serverAddr == "" {
		serverAddr = os.Getenv("VMFUSED_ADDRESS")
	}
	if !enableTestLogging {
		enableTestLogging = os.Getenv("ENABLE_TEST_LOGGING") == "1"
	}
}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	ctx := context.Background()

	if enableTestLogging {

		logger, cleanup, err := env.SetupLogForwardingToContainerd(ctx, "vmfused", false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[vmfused] ERROR: setting up log forwarding: %v\n", err)
			exitCode = 1
			return
		}
		defer cleanup()

		ctx = slogctx.NewCtx(ctx, logger)
	} else {
		logger := logging.NewDefaultDevLogger("vmfused", os.Stdout)
		ctx = slogctx.NewCtx(ctx, logger)
	}

	if err := runMain(ctx); err != nil {
		slog.ErrorContext(ctx, "vmfused failed", "error", err)
		exitCode = 1
		return
	}
}

func runMain(ctx context.Context) error {

	if serverAddr == "" {
		return errors.Errorf("listen address is required")
	}

	// Create hypervisor
	hyper := vf.NewHypervisor()

	taskGroup := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithEnableTicker(true),
	)

	// defer taskGroup.TickerRunAsDefer()()

	// Setup gRPC server

	grpcServer := grpc.NewServer(
		// insecure
		append(
			append(
				grpcerr.GetGrpcServerOptsCtx(ctx),
				otel.GetGrpcServerOpts()...,
			),
			grpc.Creds(insecure.NewCredentials()),
		)...,
	)

	// listen on unix socket

	sockAddr := strings.TrimPrefix(serverAddr, "unix://")
	listener, err := net.Listen("unix", sockAddr)
	if err != nil {
		return errors.Errorf("listening on unix socket: %w", err)
	}
	defer listener.Close()

	server, err := vmfused.NewVmfusedServer(ctx, listener, hyper)
	if err != nil {
		return errors.Errorf("creating vmfused server: %w", err)
	}

	server.Register(grpcServer)

	taskGroup.GoWithName("vmfused-grpc-server", func(ctx context.Context) error {
		slog.InfoContext(ctx, "starting vmfused server", "address", serverAddr)
		return grpcServer.Serve(listener)
	})

	taskGroup.RegisterCleanup(func(ctx context.Context) error {
		slog.InfoContext(ctx, "cleaning up vmfused server", "address", serverAddr)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Cleanup mounts
		server.Cleanup(shutdownCtx)

		// Stop gRPC server
		grpcServer.GracefulStop()
		return nil
	})

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// select {
	// case sig := <-sigChan:
	// 	slog.InfoContext(ctx, "received shutdown signal", "signal", sig)

	// 	taskGroup.CancelWithReason("shutdown signal received")
	// 	return nil
	// }

	go func() {
		sig := <-sigChan
		slog.InfoContext(ctx, "received shutdown signal", "signal", sig)
		taskGroup.CancelWithReason("shutdown signal received: " + sig.String())
	}()

	return taskGroup.Wait()
}
