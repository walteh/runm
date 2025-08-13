package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/containerd/log"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/util/bklog"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"

	containerdclient "github.com/containerd/containerd/v2/client"
	buildctl_main "github.com/moby/buildkit/cmd/buildctl"
	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/test/env"
)

func main() {

	var exitCode = 0
	defer func() {
		os.Exit(exitCode)
	}()

	ctx := context.Background()

	var logger *slog.Logger

	logger, ctx, df, err := env.SetupLogForwardingToContainerd(ctx, "buildctl", false)
	if err != nil {
		logger.Error("failed to setup logging", "error", err)
		exitCode = 1
		return
	}
	defer df()

	goodStdLogger := logrus.StandardLogger()

	log.L = &logrus.Entry{
		Logger: goodStdLogger,
		Data:   make(log.Fields, 6),
	}

	bklog.L = log.L

	ctx = log.WithLogger(ctx, log.L)

	ctx = slogctx.NewCtx(ctx, logger)

	clientopts := []grpc.DialOption{
		otel.GetGrpcClientOpts(),
		grpcerr.GetGrpcClientOptsCtx(ctx),
	}

	grpcclient.AddHackedClientOpts(clientopts...)
	session.AddHackedClientOpts(clientopts...)
	client.AddHackedClientOpts(clientopts...)
	containerdclient.AddHackedClientOpts(clientopts...)

	app, err := buildctl_main.BuildctlApp()
	if err != nil {
		panic(err)
	}

	var debugDeferred func()

	defer func() {
		if debugDeferred != nil {
			debugDeferred()
		}
	}()

	preBefore := app.Before

	exitErrHandler := func(c *cli.Context, err error) {
		logger.Error("buildkitd failed", "error", errors.Errorf("buildkitd failed: %w", err))
		if debugDeferred != nil {
			debugDeferred()
		}
		os.Exit(1)
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

	defer func() {
		if debugDeferred != nil {
			debugDeferred()
		}
	}()

	if err := app.Run(os.Args); err != nil {
		logger.Error("buildkitd failed", "error", err)
		exitCode = 1
		return
	}
}
