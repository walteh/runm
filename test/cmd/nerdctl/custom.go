package main

import (
	_ "unsafe"

	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/log"
	"github.com/containerd/nerdctl/v2/cmd/nerdctl/container"
	"github.com/containerd/nerdctl/v2/pkg/errutil"
	"github.com/containerd/nerdctl/v2/pkg/logging"
	"github.com/spf13/cobra"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/test/env"
	"google.golang.org/grpc"
)

//go:linkname NewApp github.com/containerd/nerdctl/v2/cmd/nerdctl.NewApp
func NewApp() (*cobra.Command, error)

func init() {

	os.Setenv("NERDCTL_TOML", env.NerdctlConfigTomlPath())
	os.Setenv("BUILDKIT_HOST", "unix://"+env.BuildkitdAddress())

	// set buildctl binary to the test binary
	buildctlBinary, err := os.Executable()
	if err != nil {
		panic(err)
	}

	os.Setenv("BUILDKIT_BUILDCTL_BINARY", strings.Replace(buildctlBinary, "nerdctl", "buildctl", 1))

	clientopts := []grpc.DialOption{
		otel.GetGrpcClientOpts(),
		grpcerr.GetGrpcClientOptsCtx(context.Background()),
	}

	container.AddHackedClientOpts(client.WithExtraDialOpts(clientopts))

	client.AddHackedClientOpts(clientopts...)

}

func main() {
	if err := xmain(); err != nil {
		errutil.HandleExitCoder(err)
		log.L.Fatal(err)
	}
}

func xmain() error {
	if len(os.Args) == 3 && os.Args[1] == logging.MagicArgv1 {
		// containerd runtime v2 logging plugin mode.
		// "binary://BIN?KEY=VALUE" URI is parsed into Args {BIN, KEY, VALUE}.
		return logging.Main(os.Args[2])
	}

	ctx, cancel := initContext()
	defer cancel()

	// nerdctl CLI mode
	app, err := NewApp()
	if err != nil {
		return err
	}
	return app.ExecuteContext(ctx)
}

func initContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	mode := guessNerdctlMode()

	cleanupFuncs := []func() error{}

	_, ctx, cleanup, err := env.SetupLoggingForNerdctl(ctx, mode)
	if err != nil {
		panic(err)
	}
	cleanupFuncs = append(cleanupFuncs, cleanup)

	if debugCancel := env.EnableDebugging(); debugCancel != nil {
		cleanupFuncs = append(cleanupFuncs, func() error {
			debugCancel()
			return nil
		})
	}

	cleanupFuncs = append(cleanupFuncs, func() error {
		cancel()
		return nil
	})

	return ctx, func() {
		cancel()
		for _, cleanup := range cleanupFuncs {
			if err := cleanup(); err != nil {
				slog.Error("failed to cleanup", "error", err)
			}
		}
	}
}

func guessNerdctlMode() string {

	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.Contains(arg, "/") {
			continue
		}
		return arg
	}
	return "unknown"
}
