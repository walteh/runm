package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/opencontainers/runc/libcontainer"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"github.com/walteh/runm/pkg/logging"
)

func init() {

	if len(os.Args) > 1 && os.Args[1] == "init" {
		opts := []logging.OptLoggerOptsSetter{}

		_ = logging.NewDefaultDevLogger("runc[init]", os.Stdout, opts...)

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
		// This is the golang entry point for runc init, executed
		// before main() but after libcontainer/nsenter's nsexec().
		libcontainer.Init()
	}
}
