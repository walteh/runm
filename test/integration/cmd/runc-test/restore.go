//go:build linux

package main

import (
	"os"

	"github.com/moby/sys/userns"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var restoreCommand = cli.Command{
	Name:  "restore",
	Usage: "restore a container from a previous checkpoint",
	ArgsUsage: `<container-id>

Where "<container-id>" is the name for the instance of the container to be
restored.`,
	Description: `Restores the saved state of the container instance that was previously saved
using the runc checkpoint command.`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "console-socket",
			Value: "",
			Usage: "path to an AF_UNIX socket which will receive a file descriptor referencing the master end of the console's pseudoterminal",
		},
		cli.StringFlag{
			Name:  "image-path",
			Value: "",
			Usage: "path to criu image files for restoring",
		},
		cli.StringFlag{
			Name:  "work-path",
			Value: "",
			Usage: "path for saving work files and logs",
		},
		cli.BoolFlag{
			Name:  "tcp-established",
			Usage: "allow open tcp connections",
		},
		cli.BoolFlag{
			Name:  "ext-unix-sk",
			Usage: "allow external unix sockets",
		},
		cli.BoolFlag{
			Name:  "shell-job",
			Usage: "allow shell jobs",
		},
		cli.BoolFlag{
			Name:  "file-locks",
			Usage: "handle file locks, for safety",
		},
		cli.StringFlag{
			Name:  "manage-cgroups-mode",
			Value: "",
			Usage: "cgroups mode: soft|full|strict|ignore (default: soft)",
		},
		cli.StringFlag{
			Name:  "bundle, b",
			Value: "",
			Usage: "path to the root of the bundle directory",
		},
		cli.BoolFlag{
			Name:  "detach,d",
			Usage: "detach from the container's process",
		},
		cli.StringFlag{
			Name:  "pid-file",
			Value: "",
			Usage: "specify the file to write the process id to",
		},
		cli.BoolFlag{
			Name:  "no-subreaper",
			Usage: "disable the use of the subreaper used to reap reparented processes",
		},
		cli.BoolFlag{
			Name:  "no-pivot",
			Usage: "do not use pivot root to jail process inside rootfs.  This should be used whenever the rootfs is on top of a ramdisk",
		},
		cli.StringSliceFlag{
			Name:  "empty-ns",
			Usage: "create a namespace, but don't restore its properties",
		},
		cli.BoolFlag{
			Name:  "auto-dedup",
			Usage: "enable auto deduplication of memory images",
		},
		cli.BoolFlag{
			Name:  "lazy-pages",
			Usage: "use userfaultfd to lazily restore memory pages",
		},
		cli.StringFlag{
			Name:  "lsm-profile",
			Value: "",
			Usage: "Specify an LSM profile to be used during restore in the form of TYPE:NAME.",
		},
		cli.StringFlag{
			Name:  "lsm-mount-context",
			Value: "",
			Usage: "Specify an LSM mount context to be used during restore.",
		},
	},
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, exactArgs); err != nil {
			return err
		}
		// XXX: Currently this is untested with rootless containers.
		if os.Geteuid() != 0 || userns.RunningInUserNS() {
			logrus.Warn("runc checkpoint is untested with rootless containers")
		}

		options, err := criuOptions(context)
		if err != nil {
			return err
		}
		status, err := startContainer(context, CT_ACT_RESTORE, options)
		if err != nil {
			return err
		}
		// exit with the container's exit status so any external supervisor is
		// notified of the exit with the correct exit status.
		os.Exit(status)
		return nil
	},
}
