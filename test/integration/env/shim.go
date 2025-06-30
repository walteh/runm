package env

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/plugin/registry"
	"github.com/moby/sys/reexec"
	"github.com/sirupsen/logrus"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/cmd/containerd-shim-runm-v2/manager"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/sloglogrus"

	taskplugin "github.com/walteh/runm/cmd/containerd-shim-runm-v2/task/plugin"
	vfruntimeplugin "github.com/walteh/runm/core/runc/runtime/virt/plugins/vf"
)

func ShimReexecInit() {
	reexec.Register(ShimSimlinkPath(), ShimMain)
}

type simpleOtelDialer struct {
	port    uint32
	network string
}

func (d *simpleOtelDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	return net.Dial(d.network, fmt.Sprintf(":%d", d.port))
}

var mode = guessShimMode()

func setupOtelForShim(ctx context.Context) (*slog.Logger, func() error, error) {

	shimName := fmt.Sprintf("shim[%s]", mode)

	opts := []logging.OptLoggerOptsSetter{
		logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
		logging.WithEnableDelimiter(true),
	}

	rawWriterSock, err := net.Dial("unix", ShimRawWriterSockPath())
	if err != nil {
		slog.Error("Failed to dial log proxy socket", "error", err, "path", ShimRawWriterSockPath())
		return nil, nil, err
	}

	opts = append(opts, logging.WithRawWriter(rawWriterSock))

	logProxySockDelim, err := net.Dial("unix", ShimDelimitedWriterSockPath())
	if err != nil {
		slog.Error("Failed to dial log proxy socket", "error", err, "path", ShimDelimitedWriterSockPath())
		return nil, nil, err
	}

	// opts = append(opts, logging.WithDelimitedLogWriter(logProxySockDelim))

	// attempt to listen on the port 5909
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", MagicHostOtlpGRPCPort()))
	if err == nil {
		listener.Close()
		l := logging.NewDefaultDevLogger(shimName, logProxySockDelim, opts...)
		l.Debug("logger created without otel, the host otel grpc port is free", "port", MagicHostOtlpGRPCPort())
		return l, func() error {
			rawWriterSock.Close()
			logProxySockDelim.Close()
			return nil
		}, nil
	}

	otlpConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", MagicHostOtlpGRPCPort()))
	if err != nil {
		return nil, nil, err
	}

	otelInstances, err := logging.NewGRPCOtelInstances(ctx, otlpConn, shimName)
	if err != nil {
		return nil, nil, err
	}

	return logging.NewDefaultDevLoggerWithOtel(ctx, shimName, logProxySockDelim, otelInstances, opts...), func() error {
		rawWriterSock.Close()
		logProxySockDelim.Close()
		return otelInstances.Shutdown(ctx)
	}, nil
}

func ShimMain() {

	ctx := context.Background()

	// slog.Info("dialed log proxy socket", "conn", logProxySock)

	// otelProxySock, err := net.Dial("unix", ShimOtelProxySockPath())
	// if err != nil {
	// 	slog.Error("Failed to dial otel proxy socket", "error", err, "path", ShimOtelProxySockPath())
	// 	return
	// }
	// defer otelProxySock.Close()

	// slog.Info("dialed otel proxy socket", "conn", otelProxySock)

	logger, sd, err := setupOtelForShim(ctx)
	if err != nil {
		slog.Error("Failed to setup otel", "error", err)
		os.Exit(1)
	}
	defer sd()

	ctx = slogctx.NewCtx(ctx, logger)

	ctx = slogctx.Append(ctx,
		slog.String("process", "shim"),
		slog.String("pid", strconv.Itoa(os.Getpid())),
		slog.String("ppid", strconv.Itoa(syscall.Getppid())),
		slog.String("mode", mode),
	)

	slog.InfoContext(ctx, "SHIM_STARTING", "args", os.Args[1:])

	// Set up panic and exit monitoring BEFORE anything else
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "SHIM_EXIT_PANIC", "panic", r, "stack", string(debug.Stack()))
			panic(r)
		}
		slog.InfoContext(ctx, "SHIM_EXIT_NORMAL")
	}()

	if syscall.Getppid() == 1 {

		var rusage syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err == nil {
			slog.InfoContext(ctx, "SHIM_INITIAL_RESOURCE_USAGE",
				"max_rss", rusage.Maxrss,
				"user_time", rusage.Utime,
				"sys_time", rusage.Stime)
		}

		// Start a goroutine to monitor resource usage
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					var rusage syscall.Rusage
					if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err == nil {
						slog.InfoContext(ctx, "SHIM_RESOURCE_USAGE_CHECK",
							"max_rss", rusage.Maxrss,
							"user_time", float64(rusage.Utime.Usec)/1000000,
							"sys_time", float64(rusage.Stime.Usec)/1000000,
							"num_goroutines", runtime.NumGoroutine())
					}
				}
			}
		}()

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

func guessShimMode() string {

	if slices.Contains(os.Args, "-info") {
		return "info"
	}

	if slices.Contains(os.Args, "delete") {
		return "delete"
	}

	if slices.Contains(os.Args, "start") {
		return "start"
	}

	return "primary"
}

func RunShim(ctx context.Context) error {

	sloglogrus.SetLogrusLevel(logrus.DebugLevel)

	registry.Reset()
	taskplugin.Reregister()
	vfruntimeplugin.Reregister()

	if logging.GetGlobalOtelInstances() != nil {
		os.Setenv("RUNM_SHIM_HOST_OTLP_PORT", strconv.Itoa(int(MagicHostOtlpGRPCPort())))
	}

	os.Setenv("LINUX_RUNTIME_BUILD_DIR", LinuxRuntimeBuildDir())

	shim.Run(ctx, manager.NewDebugManager(manager.NewShimManager("io.containerd.runc.v2")), func(c *shim.Config) {
		c.NoReaper = true
		c.NoSetupLogger = true
	})

	return nil
}
