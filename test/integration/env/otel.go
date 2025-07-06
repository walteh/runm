package env

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/containerd/log"
	"github.com/sirupsen/logrus"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
)

func SetupOtelForNerdctl(ctx context.Context) (*slog.Logger, func() error, error) {
	return SetupLogForwardingToContainerd(ctx, "nerdctl")
}

func SetupLogForwardingToContainerd(ctx context.Context, shimName string) (*slog.Logger, func() error, error) {

	opts := []logging.LoggerOpt{
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

	log.L = &logrus.Entry{
		Logger: logrus.StandardLogger(),
		Data:   make(log.Fields, 6),
	}

	return logging.NewDefaultDevLoggerWithOtel(ctx, shimName, logProxySockDelim, otelInstances, opts...), func() error {
		rawWriterSock.Close()
		logProxySockDelim.Close()
		return otelInstances.Shutdown(ctx)
	}, nil
}
