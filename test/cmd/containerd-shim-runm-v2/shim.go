package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/plugin/registry"
	"github.com/sirupsen/logrus"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/cmd/containerd-shim-runm-v2/manager"
	"github.com/walteh/runm/pkg/logging/sloglogrus"
	"github.com/walteh/runm/pkg/ticker"
	"github.com/walteh/runm/test/env"

	taskplugin "github.com/walteh/runm/cmd/containerd-shim-runm-v2/task/plugin"
	vfruntimeplugin "github.com/walteh/runm/core/runc/runtime/virt/plugins/vf"
)

func main() {

	ShimMain()
}

func ShimMain() {

	ctx := context.Background()

	_, ctx, sd, err := env.SetupLoggingForShim(ctx)
	if err != nil {
		slog.Error("Failed to setup logging for shim", "error", err)
		os.Exit(1)
	}
	defer sd()

	mode := env.GuessCurrentShimMode(os.Args)
	if mode == "primary" {
		// If debugging is enabled, exec to debug script
		defer env.EnableDebugging()()
	}

	ctx = slogctx.Append(ctx,
		slog.String("process", "shim"),
		slog.String("pid", strconv.Itoa(os.Getpid())),
		slog.String("ppid", strconv.Itoa(syscall.Getppid())),
		slog.String("mode", mode),
	)

	slog.InfoContext(ctx, "SHIM_STARTING", "args", os.Args[1:])

	// Add extra logging for primary mode
	if mode == "primary" {
		slog.InfoContext(ctx, "PRIMARY_SHIM_DETECTED", "will_debug", true)
	}

	// Set up panic and exit monitoring BEFORE anything else
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "SHIM_EXIT_PANIC", "panic", r, "stack", string(debug.Stack()))
			panic(r)
		}
		slog.InfoContext(ctx, "SHIM_EXIT_NORMAL")
	}()

	if syscall.Getppid() == 1 {

		defer ticker.NewTicker(
			ticker.WithInterval(1*time.Second),
			ticker.WithStartBurst(5),
			ticker.WithFrequency(15),
			ticker.WithMessage("SHIM:PROCESS[RUNNING]"),
			ticker.WithDoneMessage("SHIM:PROCESS[DONE]"),
			ticker.WithLogLevel(slog.LevelDebug),
		).RunAsDefer()()

	}

	// Monitor for unexpected signals and OS-level events
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGABRT, syscall.SIGSEGV, syscall.SIGBUS)

	go func() {
		sig := <-signalChan
		slog.ErrorContext(ctx, "SHIM_EXIT_SIGNAL", "signal", sig, "args", os.Args[1:])
		// Log stack trace of all goroutines
		buf := make([]byte, 64*1024)
		n := runtime.Stack(buf, true)
		slog.ErrorContext(ctx, string(buf[:n]))

		// Don't exit immediately, let the signal handler work
		time.Sleep(1 * time.Second)
	}()

	// errc := make(chan error)

	err = RunShim(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "SHIM_MAIN_FAILED", "error", err)
	}

}

func RunShim(ctx context.Context) error {

	sloglogrus.SetLogrusLevel(logrus.DebugLevel)

	registry.Reset()
	taskplugin.Reregister()
	vfruntimeplugin.Reregister()

	os.Setenv("LINUX_RUNTIME_BUILD_DIR", env.LinuxRuntimeBuildDir())

	shim.Run(ctx, manager.NewDebugManager(manager.NewShimManager("io.containerd.runc.v2")), func(c *shim.Config) {
		c.NoReaper = true
		c.NoSubreaper = true
		c.NoSetupLogger = true
	})

	return nil
}
