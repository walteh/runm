package main

import (
	"context"
	"os"

	"github.com/containerd/log"
	buildkitd_main "github.com/moby/buildkit/cmd/buildkitd"
	"github.com/moby/buildkit/util/bklog"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	slogctx "github.com/veqryn/slog-context"
	"github.com/walteh/runm/test/integration/env"
	"gitlab.com/tozd/go/errors"
)

func main() {

	ctx := context.Background()

	l, df, err := env.SetupLogForwardingToContainerd(ctx, "buildkitd", true)
	if err != nil {
		panic(err)
	}

	defer df()

	goodStdLogger := logrus.StandardLogger()

	log.L = &logrus.Entry{
		Logger: goodStdLogger,
		Data:   make(log.Fields, 6),
	}

	bklog.L = log.L

	ctx = log.WithLogger(ctx, log.L)

	ctx = slogctx.NewCtx(ctx, l)

	app := buildkitd_main.App()

	var debugDeferred func()

	defer func() {
		if debugDeferred != nil {
			debugDeferred()
		}
	}()

	preBefore := app.Before

	exitErrHandler := func(c *cli.Context, err error) {
		l.Error("buildkitd failed", "error", errors.Errorf("buildkitd failed: %w", err))
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

		ctx = slogctx.NewCtx(ctx, l)

		debugDeferred = env.EnableDebugging()

		if preBefore != nil {
			if err := preBefore(c); err != nil {
				return err
			}
		}

		return nil
	}

	if err := app.Run(os.Args); err != nil {
		l.Error("buildkitd failed", "error", err)
		if debugDeferred != nil {
			debugDeferred()
		}
		os.Exit(1)
	}
}
