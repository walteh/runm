package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/opencontainers/runc/libcontainer"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
)

func init() {

	// fmt.Printf("DEBUG: dial /tmp/runm-log-proxy.sock\n")

	if len(os.Args) > 1 && os.Args[1] == "init" {

		closer := setupLogging()
		defer closer()

		// This is the golang entry point for runc init, executed
		// before main() but after libcontainer/nsenter's nsexec().
		libcontainer.Init()
	}
}

func setupLogging() func() {
	closers := []func(){}

	opts := []logging.OptLoggerOptsSetter{
		logging.WithInterceptLogrus(true),
		logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
		logging.WithEnableDelimiter(true),
	}

	opts = append(opts, logging.WithValues([]slog.Attr{
		slog.String("run_id", fmt.Sprintf("%d", runId)),
		slog.String("ppid", fmt.Sprintf("%d", os.Getppid())),
		slog.String("pid", fmt.Sprintf("%d", os.Getpid())),
	}))

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

	ticker := time.NewTicker(1 * time.Second)
	ticks := 0

	go func() {
		for tick := range ticker.C {

			ticks++
			if ticks < 10 || ticks%60 == 0 {
				slog.Info("still running in runc-test[init], waiting to be killed", "tick", tick)
			}
		}
	}()

	return func() {
		ticker.Stop()
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
