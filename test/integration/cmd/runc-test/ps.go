//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/urfave/cli"
)

var psCommand = cli.Command{
	Name:      "ps",
	Usage:     "ps displays the processes running inside a container",
	ArgsUsage: `<container-id> [ps options]`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "format, f",
			Value: "table",
			Usage: `select one of: ` + formatOptions,
		},
	},
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, minArgs); err != nil {
			return err
		}

		container, err := getContainer(context)
		if err != nil {
			return err
		}

		pids, err := container.Processes()
		if err != nil {
			maybeLogCgroupWarning("ps", err)
			return err
		}

		switch context.String("format") {
		case "table":
		case "json":
			return json.NewEncoder(os.Stdout).Encode(pids)
		default:
			return errors.New("invalid format option")
		}

		// [1:] is to remove command name, ex:
		// context.Args(): [container_id ps_arg1 ps_arg2 ...]
		// psArgs:         [ps_arg1 ps_arg2 ...]
		//
		psArgs := context.Args()[1:]
		if len(psArgs) == 0 {
			psArgs = []string{"-ef"}
		}

		cmd := exec.Command("ps", psArgs...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, output)
		}

		lines := strings.Split(string(output), "\n")
		pidIndex, err := getPidIndex(lines[0])
		if err != nil {
			return err
		}

		fmt.Println(lines[0])
		for _, line := range lines[1:] {
			if len(line) == 0 {
				continue
			}
			fields := strings.Fields(line)
			p, err := strconv.Atoi(fields[pidIndex])
			if err != nil {
				return fmt.Errorf("unable to parse pid: %w", err)
			}

			if slices.Contains(pids, p) {
				fmt.Println(line)
			}
		}
		return nil
	},
	SkipArgReorder: true,
}

func getPidIndex(title string) (int, error) {
	titles := strings.Fields(title)

	pidIndex := -1
	for i, name := range titles {
		if name == "PID" {
			return i, nil
		}
	}

	return pidIndex, errors.New("couldn't find PID field in ps output")
}
