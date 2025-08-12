package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	pyroscope "github.com/grafana/pyroscope-go"
	"github.com/mdlayher/vsock"
	slogctx "github.com/veqryn/slog-context"
	"github.com/walteh/runm/core/gvnet"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	"gitlab.com/tozd/go/errors"
)

func (r *runmLinuxInit) setupLogger(ctx context.Context) (context.Context, func(), error) {
	var err error

	fmt.Println("linux-runm-init: setting up logging - all future logs will be sent to vsock (pid: ", os.Getpid(), ")")

	rawWriterConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		return nil, nil, errors.Errorf("problem dialing vsock for raw writer: %w", err)
	}

	delimitedLogProxyConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		return nil, nil, errors.Errorf("problem dialing vsock for log proxy: %w", err)
	}

	opts := []logging.LoggerOpt{
		logging.WithRawWriter(rawWriterConn),
	}

	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		// return vsock.Dial(2, uint32(constants.VsockOtelPort), nil)
		return net.Dial("tcp", fmt.Sprintf("%s:4317", gvnet.VIRTUAL_GATEWAY_IP))
	}

	cleanups := []func(){}

	if enableOtel {
		os.MkdirAll("/runc-config-flags", 0755)
		// write to /runc-config-flags/otel-enabled
		os.WriteFile("/runc-config-flags/otel-enabled", []byte("1"), 0644)

		ctxz, cleanup, err := r.setupPyroscope(ctx)
		cleanups = append(cleanups, cleanup)
		if err != nil {
			return nil, nil, errors.Errorf("failed to setup Pyroscope: %w", err)
		}
		ctx = ctxz

	}

	cleanupz, err := otel.ConfigureOTelSDKWithDialer(ctx, serviceName, enableOtel, dialer)
	if err != nil {
		return nil, nil, errors.Errorf("failed to setup OTel SDK: %w", err)
	}
	cleanups = append(cleanups, cleanupz)

	logger := logging.NewDefaultDevLoggerWithDelimiter(serviceName, delimitedLogProxyConn, opts...)

	r.rawWriter = rawWriterConn
	r.logWriter = delimitedLogProxyConn
	r.logger = logger

	return slogctx.NewCtx(ctx, logger), func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}, nil
}

func (r *runmLinuxInit) setupPyroscope(ctx context.Context) (context.Context, func(), error) {
	formattedName := strings.ReplaceAll(serviceName, "[", "-")
	formattedName = strings.ReplaceAll(formattedName, "]", "")
	p, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: formattedName + "{env=local,version=dev}",
		ServerAddress:   "http://" + gvnet.VIRTUAL_GATEWAY_IP + ":4040", // or http://pyroscope:4040 when inside compose
		Logger:          pyroscope.StandardLogger,
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileGoroutines,
			pyroscope.ProfileMutexCount,
			pyroscope.ProfileMutexDuration,
			pyroscope.ProfileBlockCount,
			pyroscope.ProfileBlockDuration,
		},
		UploadRate: time.Second,
	})

	if err != nil {
		return nil, nil, errors.Errorf("failed to setup Pyroscope: %w", err)
	}

	slog.InfoContext(ctx, "Pyroscope setup complete")

	return ctx, func() {
		err := p.Stop()
		if err != nil {
			slog.InfoContext(ctx, "prob with pyroscope", "err", err)
		}
	}, nil
}

func (r *runmLinuxInit) runCadvisor(ctx context.Context) error {
	slog.InfoContext(ctx, "setting up cadvisor", "port", constants.GuestCadvisorTCPPort)

	cmd := exec.CommandContext(ctx, "/mbin/cadvisor-test",
		"--port", strconv.Itoa(constants.GuestCadvisorTCPPort),
		"--logtostderr")

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return errors.Errorf("failed to start cadvisor: %w", err)
	}

	slog.InfoContext(ctx, "cadvisor started successfully", "pid", cmd.Process.Pid, "port", constants.GuestCadvisorTCPPort)

	return cmd.Wait()
}
