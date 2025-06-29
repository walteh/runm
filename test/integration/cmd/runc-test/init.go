package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/opencontainers/runc/libcontainer"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"github.com/walteh/runm/pkg/logging"
)

func init() {

	// fmt.Printf("DEBUG: dial /tmp/runm-log-proxy.sock\n")

	if len(os.Args) > 1 && os.Args[1] == "init" {
		// fmt.Printf("DEBUG: ini1\n")
		// fmt.Printf("DEBUG: ppid: %d\n", os.Getppid())
		// fmt.Printf("DEBUG: pid: %d\n", os.Getpid())
		// fmt.Printf("DEBUG: run_id: %d\n", runId)
		// fmt.Printf("DEBUG: args: %v\n", os.Args)

		opts := []logging.OptLoggerOptsSetter{
			logging.WithInterceptLogrus(false),
		}

		opts = append(opts, logging.WithValues([]slog.Attr{
			slog.String("run_id", fmt.Sprintf("%d", runId)),
			slog.String("ppid", fmt.Sprintf("%d", os.Getppid())),
			slog.String("pid", fmt.Sprintf("%d", os.Getpid())),
		}))

		// fmt.Printf("DEBUG: dial /tmp/runm-log-proxy.sock\n")

		// dial "/tmp/runm-log-proxy.sock"
		conn, err := net.Dial("unix", "/tmp/runm-log-proxy.sock")
		if err != nil {
			fmt.Printf("problem dialing log proxy: %v\n", err)
		} else {
			defer conn.Close()

			_ = logging.NewDefaultDevLogger("runc[init]", conn, opts...)

			ticker := time.NewTicker(1 * time.Second)
			ticks := 0
			defer ticker.Stop()

			go func() {
				for tick := range ticker.C {

					ticks++
					if ticks < 10 || ticks%60 == 0 {
						slog.Info("still running in runc-test[init], waiting to be killed", "tick", tick)
					}
				}
			}()
		}

		// fmt.Printf("DEBUG: dial /tmp/runm-log-proxy.sock done\n")

		// fmt.Printf("DEBUG: ini2 w\n")
		// This is the golang entry point for runc init, executed
		// before main() but after libcontainer/nsenter's nsexec().
		libcontainer.Init()
	}
}
