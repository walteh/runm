//go:build linux

package main

import (
	"context"
	"net"

	_ "github.com/opencontainers/runc/libcontainer/nsenter"

	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/opencontainers/runc/libcontainer"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/pkg/ticker"
)

func init() {

	// fmt.Printf("DEBUG: dial /tmp/runm-log-proxy.sock\n")

	if len(os.Args) > 1 && os.Args[1] == "init" {

		closer := setupLogging()
		defer closer()

		defer ticker.NewTicker(
			ticker.WithInterval(1*time.Second),
			ticker.WithStartBurst(5),
			ticker.WithFrequency(15),
			ticker.WithMessage("RUNC:INIT[RUNNING]"),
			ticker.WithDoneMessage("RUNC:INIT[DONE]"),
			ticker.WithLogLevel(slog.LevelDebug),
		).RunAsDefer()()

		// debugNamespaces("runc-test[init]")
		// debugMounts("runc-test[init]")
		// debugRootfs("runc-test[init]")
		// containerDebug("runc-test[init]")

		// This is the golang entry point for runc init, executed
		// before main() but after libcontainer/nsenter's nsexec().
		libcontainer.Init()
	}
}

func setupLogging() func() {
	closers := []func(){}

	enableOtel := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""

	cleanup, err := otel.ConfigureOTelSDKWithDialer(context.Background(), "runc[init]", enableOtel, func(ctx context.Context, network, addr string) (net.Conn, error) {
		return vsock.Dial(2, uint32(constants.VsockOtelPort), nil)
	})

	if err != nil {
		fmt.Printf("problem configuring otel: %v\n", err)
	}

	closers = append(closers, func() {
		cleanup()
	})

	opts := []logging.LoggerOpt{
		logging.WithInterceptLogrus(true),
		logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
		logging.WithEnableDelimiter(true),
	}

	opts = append(opts, logging.WithValues(
		slog.String("run_id", fmt.Sprintf("%d", runId)),
		slog.String("ppid", fmt.Sprintf("%d", os.Getppid())),
		slog.String("pid", fmt.Sprintf("%d", os.Getpid())),
	))

	rawConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		fmt.Printf("problem dialing log proxy: %v\n", err)
	} else {
		closers = append(closers, func() { rawConn.Close() })

		opts = append(opts, logging.WithRawWriter(rawConn))
	}

	dialConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		fmt.Printf("problem dialing log proxy: %v\n", err)
	} else {
		closers = append(closers, func() { dialConn.Close() })

		_ = logging.NewDefaultDevLogger("runc[init]", dialConn, opts...)

	}

	// if dialConn != nil && rawConn != nil {
	// 	dfd, err := hack.GetFdFromUnixConn(dialConn)
	// 	if err != nil {
	// 		slog.Error("DEBUG: problem getting dialConn file", "err", err)
	// 	}
	// 	rfd, err := hack.GetFdFromUnixConn(rawConn)
	// 	if err != nil {
	// 		slog.Error("DEBUG: problem getting rawConn file", "err", err)
	// 	}
	// 	slog.Info("DEBUG: connection file descriptors created", "dialConn", dialConn.RemoteAddr(), "rawConn", rawConn.RemoteAddr(), "dfd", dfd, "rfd", rfd)

	// }

	// ticker := time.NewTicker(10 * time.Millisecond)
	// ticks := 0
	// closers = append(closers, func() {
	// 	ticker.Stop()
	// })

	// go func() {
	// 	for tick := range ticker.C {

	// 		ticks++
	// 		if ticks < 1000 || ticks%100 == 0 {
	// 			slog.Info("still running in runc-test[init], waiting to be killed", "tick", tick)
	// 		}
	// 	}
	// }()

	return func() {
		for _, closer := range closers {
			closer()
		}
	}
}

// func getFdFromUnixConn(conn *net.UnixConn) (uintptr, error) {
// 	scs, err := conn..SyscallConn()
// 	if err != nil {
// 		return 0, err
// 	}

// 	var fdz uintptr
// 	scs.Control(func(fd uintptr) {
// 		fdz = fd
// 	})

// 	return fdz, nil
// }
