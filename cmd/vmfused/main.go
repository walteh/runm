//go:build !windows

package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/virt/vf"
	"github.com/walteh/runm/core/vmfuse/vmfused"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/pkg/taskgroup"
)

var (
	serverAddr string
)

func init() {
	flag.StringVar(&serverAddr, "address", "", "address to listen on")
	flag.Parse()

	if serverAddr == "" {
		serverAddr = os.Getenv("VMFUSED_ADDRESS")
	}
}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	logger := logging.NewDefaultDevLogger("vmfused", os.Stdout)
	ctx := slogctx.NewCtx(context.Background(), logger)

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

	// Setup gRPC server

	grpcServer := grpc.NewServer(
		grpcerr.GetGrpcServerOptsCtx(ctx),
		otel.GetGrpcServerOpts(),
	)

	// Setup listener
	listener, err := vmfused.CreateListener(serverAddr)
	if err != nil {
		return errors.Errorf("creating listener: %w", err)
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
