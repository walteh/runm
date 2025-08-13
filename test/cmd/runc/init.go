//go:build linux

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/opencontainers/runc/libcontainer"

	"github.com/walteh/runm/core/gvnet"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/pkg/ticker"
)

func init() {

	if len(os.Args) > 1 && os.Args[1] == "init" {

		cleanup, err := configureLogging(true)
		if err != nil {
			fmt.Printf("problem setting up logging: %v\n", err)
			os.Exit(1)
		}
		defer cleanup()

		defer ticker.NewTicker(
			ticker.WithInterval(1*time.Second),
			ticker.WithStartBurst(5),
			ticker.WithFrequency(15),
			ticker.WithMessage("RUNC:INIT[RUNNING]"),
			ticker.WithDoneMessage("RUNC:INIT[DONE]"),
			ticker.WithLogLevel(slog.LevelDebug),
		).RunAsDefer()()

		// This is the golang entry point for runc init, executed
		// before main() but after libcontainer/nsenter's nsexec().
		libcontainer.Init()
	}
}

func configureLogging(interceptLogrus bool) (func(), error) {

	ctx := context.Background()

	cleanups := []func(){}

	data, err := os.ReadFile("/runc-config-flags/otel-enabled")
	if err != nil {
		// fmt.Fprintf(os.Stderr, "problem reading otel-enabled: %v\n", err)
		data = []byte("0")
	}
	enableOtel := string(data) == "1"

	loggerName := fmt.Sprintf("runc[%s]", guessRuncMode())

	cleanup, err := otel.ConfigureOTelSDKWithDialer(ctx, loggerName, enableOtel, func(ctx context.Context, network, addr string) (net.Conn, error) {
		// return vsock.Dial(2, uint32(constants.VsockOtelPort), nil)
		return net.Dial("tcp", fmt.Sprintf("%s:4317", gvnet.VIRTUAL_GATEWAY_IP))
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "problem configuring otel: %v\n", err)
	}

	cleanups = append(cleanups, cleanup)

	opts := []logging.LoggerOpt{
		logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
		logging.WithEnableDelimiter(true),
		logging.WithInterceptLogrus(interceptLogrus),
	}

	opts = append(opts, logging.WithValues(
		slog.String("run_id", fmt.Sprintf("%d", runId)),
		slog.String("ppid", fmt.Sprintf("%d", os.Getppid())),
		slog.String("pid", fmt.Sprintf("%d", os.Getpid())),
	))

	rawConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		fmt.Printf("problem dialing raw log proxy: %v\n", err)
		return nil, err
	} else {
		cleanups = append(cleanups, func() { rawConn.Close() })
		opts = append(opts, logging.WithRawWriter(rawConn))
	}

	delimConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		fmt.Printf("problem dialing log proxy: %v\n", err)
		return nil, err
	} else {
		// defer conn.Close()
		cleanups = append(cleanups, func() { delimConn.Close() })
		_ = logging.NewDefaultDevLogger(loggerName, delimConn, opts...)
	}

	return func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}, nil
}
