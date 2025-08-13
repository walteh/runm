package main

import (
	_ "net/http/pprof"

	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/containerd/containerd/v2/pkg/cap"
	"github.com/containerd/log"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/util/bklog"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"google.golang.org/grpc"

	containerdclient "github.com/containerd/containerd/v2/client"
	buildkitd_main "github.com/moby/buildkit/cmd/buildkitd"
	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/test/env"
)

func init() {
	cap.SetMissingProcOverrides(env.GetCurrentRunmProcs())
}

func main() {
	var exitCode = 0
	defer func() {
		os.Exit(exitCode)
	}()

	ctx := context.Background()

	var logger *slog.Logger

	logger, ctx, df, err := env.SetupLogForwardingToContainerd(ctx, "buildkitd", true)
	if err != nil {
		logger.Error("failed to setup logging", "error", err)
		exitCode = 1
		return
	}
	defer df()

	goodStdLogger := logrus.StandardLogger()

	bklog.L = log.L

	// Start pprof server
	pprofPort := startPprofServer(ctx)
	logger.Info("buildkitd pprof server started", "port", pprofPort)

	clientopts := []grpc.DialOption{
		otel.GetGrpcClientOpts(),
		grpcerr.GetGrpcClientOptsCtx(ctx),
	}

	grpcclient.AddHackedClientOpts(clientopts...)
	session.AddHackedClientOpts(clientopts...)
	client.AddHackedClientOpts(clientopts...)
	containerdclient.AddHackedClientOpts(clientopts...)

	app := buildkitd_main.App(
		append(
			otel.GetGrpcServerOpts(),
			grpcerr.GetGrpcServerOptsCtx(ctx)...,
		)...,
	)

	var debugDeferred func()

	defer func() {
		if debugDeferred != nil {
			debugDeferred()
		}
	}()

	preBefore := app.Before

	exitErrHandler := func(c *cli.Context, err error) {
		// doing nothing here lets our main app handler handle the error
	}

	app.ExitErrHandler = exitErrHandler

	app.Before = func(c *cli.Context) error {

		c.Set("config", env.BuildkitdConfigTomlPath())

		c.App.ExitErrHandler = exitErrHandler

		log.L = &logrus.Entry{
			Logger: goodStdLogger,
			Data:   make(log.Fields, 6),
		}

		bklog.L = log.L

		ctx = log.WithLogger(ctx, log.L)

		ctx = slogctx.NewCtx(ctx, logger)

		debugDeferred = env.EnableDebugging()

		if preBefore != nil {
			if err := preBefore(c); err != nil {
				return err
			}
		}

		return nil
	}

	if err := app.Run(os.Args); err != nil {
		logger.Error("buildkitd failed", "error", errors.Errorf("running buildkitd: %w", err))
		if debugDeferred != nil {
			debugDeferred()
		}
		exitCode = 1
		return
	}
}

func startPprofServer(ctx context.Context) int {
	// Find an available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find available port for pprof server", "error", err)
		return 0
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		slog.WarnContext(ctx, "Failed to close listener", "error", err)
	}

	// Start the pprof server in a goroutine
	go func() {
		slog.InfoContext(ctx, "Starting pprof server", "port", port)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
			slog.ErrorContext(ctx, "pprof server failed", "error", err)
		}
	}()

	return port
}
