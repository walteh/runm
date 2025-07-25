//go:build linux

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runc/libcontainer"
	"github.com/urfave/cli"
)

var killCommand = cli.Command{
	Name:  "kill",
	Usage: "kill sends the specified signal (default: SIGTERM) to the container's init process",
	ArgsUsage: `<container-id> [signal]

Where "<container-id>" is the name for the instance of the container and
"[signal]" is the signal to be sent to the init process.

EXAMPLE:
For example, if the container id is "ubuntu01" the following will send a "KILL"
signal to the init process of the "ubuntu01" container:

       # runc kill ubuntu01 KILL`,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:   "all, a",
			Usage:  "(obsoleted, do not use)",
			Hidden: true,
		},
	},
	Action: func(context *cli.Context) error {
		slog.Info("KILLING RUNC - CHECKING ARGS", "all", context.Bool("all"), "args", context.Args())
		if err := checkArgs(context, 1, minArgs); err != nil {
			return err
		}
		if err := checkArgs(context, 2, maxArgs); err != nil {
			return err
		}
		slog.Info("KILLING RUNC - getting container")
		container, err := getContainer(context)
		if err != nil {
			return err
		}
		slog.Info("KILLING RUNC - getting signal")
		sigstr := context.Args().Get(1)
		if sigstr == "" {
			sigstr = "SIGTERM"
		}
		slog.Info("KILLING RUNC - parsing signal")

		signal, err := parseSignal(sigstr)
		if err != nil {
			return err
		}

		// debugging
		processes, err := container.Processes()
		if err != nil {
			slog.Error("KILLING RUNC - problem getting processes", "error", err)
		}

		status, err := container.Status()
		if err != nil {
			slog.Error("KILLING RUNC - problem getting status", "error", err)
		}

		slog.Info("KILLING RUNC - sending signal, sending signal to container", "signal", signal, "signal_str", sigstr, "all", context.Bool("all"), "container_id", container.ID(), "args", context.Args(), "container", container, "container_pid", status.String(), "processes", processes, "status", status)
		err = container.Signal(signal)
		if errors.Is(err, libcontainer.ErrNotRunning) && context.Bool("all") {
			err = nil
		}
		slog.Info("KILLING RUNC - done", "error", err)
		return err
	},
}

func parseSignal(rawSignal string) (unix.Signal, error) {
	s, err := strconv.Atoi(rawSignal)
	if err == nil {
		return unix.Signal(s), nil
	}
	sig := strings.ToUpper(rawSignal)
	if !strings.HasPrefix(sig, "SIG") {
		sig = "SIG" + sig
	}
	signal := unix.SignalNum(sig)
	if signal == 0 {
		return -1, fmt.Errorf("unknown signal %q", rawSignal)
	}
	return signal, nil
}
