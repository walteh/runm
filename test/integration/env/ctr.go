package env

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/containerd/containerd/v2/cmd/ctr/app"
	"github.com/moby/sys/reexec"
	"github.com/urfave/cli/v2"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/pkg/logging"
)

func CtrReexecInit() {
	reexec.Register(CtrSimlinkPath(), CtrMain)
}

var pluginCmds = []*cli.Command{}

var ctrArgs = []string{
	"ctr",
	"--address", Address(),
	"--namespace", Namespace(),
	// "--debug",
}

func CtrMain() {

	apd := app.New()

	ctx := context.Background()

	logger := logging.NewDefaultDevLogger("ctr", os.Stdout)

	ctx = slogctx.NewCtx(ctx, logger)

	args := append(ctrArgs, os.Args[1:]...)

	if err := apd.RunContext(ctx, args); err != nil {
		fmt.Fprintf(os.Stderr, "ctr: %s\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func RunCtrCommand(ctx context.Context, args ...string) error {

	apd := app.New()

	args = append(ctrArgs, args...)

	slog.InfoContext(ctx, "Running ctr command", "args", args)

	return apd.RunContext(ctx, args)
}
