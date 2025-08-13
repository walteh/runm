//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	runc_main "github.com/opencontainers/runc"
	"github.com/urfave/cli"
)

//go:mainpkggen github.com/opencontainers/runc
func main() {

	app := runc_main.App()

	cleanup, err := configureLogging(false)
	if err != nil {
		fmt.Printf("problem setting up logging: %v\n", err)
		os.Exit(1)
	}

	defer cleanup()

	// log once a minute to the console to indicate that the command is running
	ticker := time.NewTicker(1 * time.Second)
	ticks := 0
	defer ticker.Stop()

	go func() {
		for tick := range ticker.C {

			ticks++
			if ticks < 10 || ticks%60 == 0 {
				slog.Info("still running in runc-test, waiting to be killed", "tick", tick)
			}
		}
	}()

	app.After = func(context *cli.Context) error {
		slog.Info("RUNC ENDED")
		return nil
	}

	cli.ErrWriter = runc_main.NewFatalWriter(cli.ErrWriter)
	if err := app.Run(os.Args); err != nil {
		fmt.Printf("problem running runc: %v\n", err)
		os.Exit(1)
	}
}

func guessRuncMode() string {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.Contains(arg, "/") {
			continue
		}
		return arg
	}
	return "unknown"
}
