package env

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/containerd/log"
	"github.com/sirupsen/logrus"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
)

func SetupLoggingForNerdctl(ctx context.Context, name string) (*slog.Logger, func() error, error) {
	return SetupLogForwardingToContainerd(ctx, fmt.Sprintf("nerdctl[%s]", name), false)
}

func SetupLoggingForShim(ctx context.Context) (*slog.Logger, func() error, error) {

	mode := GuessCurrentShimMode(os.Args)

	shimName := "shim[" + mode + "]"

	ensureShimEnvVars(
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
	)

	return SetupLogForwardingToContainerd(ctx, shimName, mode == "primary")
}

func SetupLogForwardingToContainerd(ctx context.Context, shimName string, redirectStderr bool) (*slog.Logger, func() error, error) {

	opts := []logging.LoggerOpt{}

	dynamicCleanup := NewDynamicCleanup()
	defer dynamicCleanup.Defer()

	rawWriterSock, err := net.Dial("unix", ShimRawWriterSockPath())
	if err != nil {
		return nil, nil, errors.Errorf("dialing raw writer socket: %w", err)
	}

	dynamicCleanup.AddCloserFunc(rawWriterSock.Close)
	opts = append(opts, logging.WithRawWriter(rawWriterSock))

	logProxySockDelim, err := net.Dial("unix", ShimDelimitedWriterSockPath())
	if err != nil {
		return nil, nil, errors.Errorf("dialing delimited writer socket: %w", err)
	}

	dynamicCleanup.AddCloserFunc(logProxySockDelim.Close)
	// note: we don't need this becasue the delimited writrer is the one passed directly to the logger
	// opts = append(opts, logging.WithDelimitedLogWriter(logProxySockDelim))
	opts = append(opts, logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter))
	opts = append(opts, logging.WithEnableDelimiter(true))

	// Duplicate stderr to the raw writer socket to capture panic stack traces

	if redirectStderr {
		redirStderrCloser, err := DuplicateFdToWriter(int(os.Stderr.Fd()), rawWriterSock)
		if err != nil {
			return nil, nil, errors.Errorf("duplicating stderr: %w", err)
		}
		dynamicCleanup.AddCloserFunc(redirStderrCloser)

		redirStdoutCloser, err := DuplicateFdToWriter(int(os.Stdout.Fd()), rawWriterSock)
		if err != nil {
			return nil, nil, errors.Errorf("duplicating stdout: %w", err)
		}
		dynamicCleanup.AddCloserFunc(redirStdoutCloser)
	}

	cleanup, err := otel.ConfigureOTelSDK(ctx, shimName)
	if err != nil {
		return nil, nil, errors.Errorf("configuring otel: %w", err)
	}

	dynamicCleanup.AddCloserFunc(func() error {
		cleanup()
		return nil
	})

	log.L = &logrus.Entry{
		Logger: logrus.StandardLogger(),
		Data:   make(log.Fields, 6),
	}

	loggerz := logging.NewDefaultDevLogger(shimName, logProxySockDelim, opts...)

	return loggerz, dynamicCleanup.DetachDeferAsCloser(), nil
}
