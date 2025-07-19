package main

import (
	_ "net/http/pprof"

	_ "github.com/containerd/containerd/v2/cmd/containerd/builtins"

	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/cmd/containerd/command"
	"github.com/containerd/containerd/v2/cmd/containerd/server"
	"github.com/containerd/log"
	"github.com/sirupsen/logrus"
	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/test/integration/env"
)

func init() {
	server.AddHackedServerOption(grpcerr.GetGrpcServerOptsCtx(context.Background()))
}

var ctrCommands = FlagArray[string]{}

type simpleDialer struct {
	network string
	address string
}

func (d simpleDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return net.Dial(d.network, d.address)
}

var exitCode = 0

func main() {
	defer func() {
		for pid := range pids {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				slog.WarnContext(context.Background(), "failed to kill process", "pid", pid, "error", err)
			} else {
				slog.InfoContext(context.Background(), "killed process", "pid", pid)
			}
		}
		time.Sleep(1 * time.Second)
		os.Exit(exitCode)
	}()

	background := false
	debug := true
	json := false
	flag.BoolVar(&background, "background", false, "Run in background (daemon mode)")
	flag.BoolVar(&debug, "debug", true, "Run in debug mode")
	flag.Var(&ctrCommands, "ctr-command", "Command to run in ctr")
	flag.BoolVar(&json, "json", false, "Run in JSON mode")
	flag.Parse()

	var ctx context.Context

	cleanup, err := otel.ConfigureOTelSDK(ctx, "containerd")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to configure otel", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	if json {
		logger := logging.NewDefaultJSONLogger("containerd", os.Stdout)
		ctx = slogctx.NewCtx(context.Background(), logger)
	} else {
		logger := logging.NewDefaultDevLogger("containerd", os.Stdout)
		ctx = slogctx.NewCtx(context.Background(), logger)
	}

	log.L = &logrus.Entry{
		Logger: logrus.StandardLogger(),
		Data:   make(log.Fields, 6),
	}

	ctx = log.WithLogger(ctx, log.L)

	ctx = slogctx.Append(ctx, slog.String("process", "containerd"), slog.String("pid", strconv.Itoa(os.Getpid())))

	// Start pprof server
	pprofPort := startPprofServer(ctx)

	slog.InfoContext(ctx, "Starting development containerd instance",
		"background", background,
		"debug", debug,
		"ctr-commands", ctrCommands.Get(),
		"pprof-port", pprofPort)

	server, err := env.NewDevContainerdServer(ctx, command.App(), debug)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create containerd server", "error", err)
		exitCode = 1
		return
	}

	if len(ctrCommands.values) > 0 {
		slog.InfoContext(ctx, "Running ctr command", "command", ctrCommands.values)
		if err := server.StartBackground(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to start containerd in background", "error", err)
			server.Stop(ctx)
			exitCode = 1
			return
		}

		for _, command := range ctrCommands.values {
			if err := env.RunCtrCommand(ctx, strings.Split(command, " ")...); err != nil {
				slog.ErrorContext(ctx, "Failed to run ctr command", "error", err)
				server.Stop(ctx)
				exitCode = 1
				return
			}
		}

		server.Stop(ctx)
		exitCode = 0
		return
	}

	defer env.EnableDebugging()()

	if len(flag.Args()) > 0 {
		err := server.RunWithArgs(ctx, flag.Args())
		if err != nil {
			slog.ErrorContext(ctx, "Failed to run containerd with args", "error", err)
			exitCode = 1
			return
		}
		exitCode = 0
		return
	} else if background {
		slog.InfoContext(ctx, "Starting containerd in background mode")
		if err := server.StartBackground(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to start containerd in background", "error", err)
			exitCode = 1
			return
		}

		// Print connection info and exit
		// server.PrintConnectionInfoBackground()

	} else {
		// Foreground mode - handle signals gracefully
		slog.InfoContext(ctx, "Starting containerd in foreground mode")

		// server.PrintConnectionInfoForground()

		// Set up signal handling
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Start containerd in goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- server.Start(ctx)
		}()

		if err := runBuildkitInBackground(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to start buildkitd in background", "error", err)
			return
		}

		// Wait for either signal or error
		select {
		case sig := <-sigChan:
			slog.InfoContext(ctx, "Received signal, shutting down", "signal", sig)
			server.Stop(ctx)
		case err := <-errChan:
			if err != nil {
				slog.ErrorContext(ctx, "Containerd failed - sleeping for 2 seconds for graceful shutdown", "error", err)
				time.Sleep(2 * time.Second)
				exitCode = 1
				return
			}
		}
	}
}

type FlagArray[T any] struct {
	values []T
}

func (f *FlagArray[T]) String() string {
	return fmt.Sprintf("%v", f.values)
}

func (f *FlagArray[T]) Set(value string) error {

	slog.InfoContext(context.Background(), "Setting flag", "value", value)

	var v T
	switch any(v).(type) {
	case string:
		f.values = append(f.values, any(value).(T))
	case int:
		i, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		f.values = append(f.values, any(i).(T))
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}

func (f *FlagArray[T]) Get() []T {
	return f.values
}

var pids = make(map[int]bool)

func runBuildkitInBackground(ctx context.Context) error {
	// look for a binary called buildkitd-test next to our own binary
	buildkitdTestPath, err := os.Executable()
	if err != nil {
		return errors.Errorf("failed to get executable path: %w", err)
	}
	buildkitdTestPath = filepath.Dir(buildkitdTestPath) + "/buildkitd-test"

	// run it in background
	cmd := exec.CommandContext(ctx, buildkitdTestPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "UNDER_CONTAINERD_TEST=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Setpgid: true,
		// Pgid: os.Getpid(),
	}
	if err := cmd.Start(); err != nil {
		return errors.Errorf("failed to start buildkitd-test: %w", err)
	}

	go func() {
		slog.InfoContext(ctx, "buildkitd-test started", "pid", cmd.Process.Pid)
		if err := cmd.Wait(); err != nil {
			slog.WarnContext(ctx, "failed to wait for buildkitd-test, storing pid "+strconv.Itoa(cmd.Process.Pid)+" for cleanup later", "error", err)
			pids[cmd.Process.Pid] = true
		}
	}()

	return nil
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
