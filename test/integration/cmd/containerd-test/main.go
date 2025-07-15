package main

import (
	_ "github.com/containerd/containerd/v2/cmd/containerd/builtins"
	"github.com/containerd/containerd/v2/cmd/containerd/command"
	"github.com/containerd/containerd/v2/cmd/containerd/server"
	"google.golang.org/grpc"

	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/containerd/log"
	"github.com/sirupsen/logrus"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/test/integration/env"
)

func init() {
	server.AddHackedServerOption(grpc.ChainUnaryInterceptor(grpcerr.UnaryServerInterceptor))
	server.AddHackedServerOption(grpc.ChainStreamInterceptor(grpcerr.NewStreamServerInterceptor(context.Background())))
}

var ctrCommands = FlagArray[string]{}

type simpleDialer struct {
	network string
	address string
}

func (d simpleDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return net.Dial(d.network, d.address)
}

func main() {

	background := false
	debug := true
	json := false
	flag.BoolVar(&background, "background", false, "Run in background (daemon mode)")
	flag.BoolVar(&debug, "debug", true, "Run in debug mode")
	flag.Var(&ctrCommands, "ctr-command", "Command to run in ctr")
	flag.BoolVar(&json, "json", false, "Run in JSON mode")
	flag.Parse()

	var ctx context.Context

	// // dial otel grpc
	// conn, err := net.Dial("tcp", "localhost:4317")
	// if err != nil {
	// 	slog.ErrorContext(ctx, "Failed to dial otel grpc", "error", err)
	// 	os.Exit(1)
	// }
	// defer conn.Close()

	// otelInstances, err := logging.NewGRPCOtelInstances(ctx, simpleDialer{network: "tcp", address: "localhost:4317"}, "containerd")
	// if err != nil {
	// 	slog.ErrorContext(ctx, "Failed to create otel instances", "error", err)
	// 	os.Exit(1)
	// }
	// otelInstances.EnableGlobally()

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

	slog.InfoContext(ctx, "Starting development containerd instance",
		"background", background,
		"debug", debug,
		"ctr-commands", ctrCommands.Get())

	server, err := env.NewDevContainerdServer(ctx, command.App(), debug)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create containerd server", "error", err)
		os.Exit(1)
	}

	if len(ctrCommands.values) > 0 {
		slog.InfoContext(ctx, "Running ctr command", "command", ctrCommands.values)
		if err := server.StartBackground(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to start containerd in background", "error", err)
			server.Stop(ctx)
			os.Exit(1)
		}

		for _, command := range ctrCommands.values {
			if err := env.RunCtrCommand(ctx, strings.Split(command, " ")...); err != nil {
				slog.ErrorContext(ctx, "Failed to run ctr command", "error", err)
				server.Stop(ctx)
				os.Exit(1)
			}
		}

		server.Stop(ctx)
		os.Exit(0)
	}

	if err := env.EnableDebugging(); err != nil {
		slog.ErrorContext(ctx, "Failed to enable debugging", "error", err)
		os.Exit(1)
	}

	if background {
		slog.InfoContext(ctx, "Starting containerd in background mode")
		if err := server.StartBackground(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to start containerd in background", "error", err)
			os.Exit(1)
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

		// Wait for either signal or error
		select {
		case sig := <-sigChan:
			slog.InfoContext(ctx, "Received signal, shutting down", "signal", sig)
			server.Stop(ctx)
		case err := <-errChan:
			if err != nil {
				slog.ErrorContext(ctx, "Containerd failed", "error", err)
				os.Exit(1)
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
