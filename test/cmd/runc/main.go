//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	_ "unsafe"

	"github.com/urfave/cli"
)

//go:linkname App github.com/opencontainers/runc.App
func App() *cli.App

func main() {

	app := App()

	cleanup, err := configureLogging(false)
	if err != nil {
		fmt.Printf("problem setting up logging: %v\n", err)
		os.Exit(1)
	}

	defer cleanup()

	app.After = func(context *cli.Context) error {
		slog.Info("RUNC ENDED")
		return nil
	}

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
