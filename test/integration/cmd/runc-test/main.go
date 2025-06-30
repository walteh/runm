package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	//nolint:revive // Enable cgroup manager to manage devices
	"github.com/mdlayher/vsock"
	_ "github.com/opencontainers/cgroups/devices"
	"github.com/opencontainers/runc/libcontainer/seccomp"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// version is set from the contents of VERSION file.
//
//go:embed VERSION
var version string

// extraVersion is an optional suffix appended to runc version.
// It can be set via Makefile ("make EXTRA_VERSION=xxx") or by
// adding -X main.extraVersion=xxx option to the go build command.
var extraVersion = ""

// gitCommit will be the hash that the binary was built from
// and will be populated by the Makefile.
var gitCommit = ""

func printVersion(c *cli.Context) {
	w := c.App.Writer

	fmt.Fprintln(w, "runc version", c.App.Version)
	if gitCommit != "" {
		fmt.Fprintln(w, "commit:", gitCommit)
	}
	fmt.Fprintln(w, "spec:", specs.Version)
	fmt.Fprintln(w, "go:", runtime.Version())

	major, minor, micro := seccomp.Version()
	if major+minor+micro > 0 {
		fmt.Fprintf(w, "libseccomp: %d.%d.%d\n", major, minor, micro)
	}
}

const (
	specConfig = "config.json"
	usage      = `Open Container Initiative runtime

runc is a command line client for running applications packaged according to
the Open Container Initiative (OCI) format and is a compliant implementation of the
Open Container Initiative specification.

runc integrates well with existing process supervisors to provide a production
container runtime environment for applications. It can be used with your
existing process monitoring tools and the container will be spawned as a
direct child of the process supervisor.

Containers are configured using bundles. A bundle for a container is a directory
that includes a specification file named "` + specConfig + `" and a root filesystem.
The root filesystem contains the contents of the container.

To start a new instance of a container:

    # runc run [ -b bundle ] <container-id>

Where "<container-id>" is your name for the instance of the container that you
are starting. The name you provide for the container instance must be unique on
your host. Providing the bundle directory using "-b" is optional. The default
value for "bundle" is the current directory.`
)

func main() {

	app := cli.NewApp()
	app.Name = "runc"
	app.Version = strings.TrimSpace(version) + extraVersion
	app.Usage = usage

	cli.VersionPrinter = printVersion

	root := "/run/runc"
	xdgDirUsed := false
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir != "" && shouldHonorXDGRuntimeDir() {
		root = xdgRuntimeDir + "/runc"
		xdgDirUsed = true
	}

	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug logging",
		},
		cli.StringFlag{
			Name:  "log",
			Value: "",
			Usage: "set the log file to write runc logs to (default is '/dev/stderr')",
		},
		cli.StringFlag{
			Name:  "log-format",
			Value: "text",
			Usage: "set the log format ('text' (default), or 'json')",
		},
		cli.StringFlag{
			Name:  "root",
			Value: root,
			Usage: "root directory for storage of container state (this should be located in tmpfs)",
		},
		cli.BoolFlag{
			Name:  "systemd-cgroup",
			Usage: "enable systemd cgroup support, expects cgroupsPath to be of form \"slice:prefix:name\" for e.g. \"system.slice:runc:434234\"",
		},
		cli.StringFlag{
			Name:  "rootless",
			Value: "auto",
			Usage: "ignore cgroup permission errors ('true', 'false', or 'auto')",
		},
	}
	app.Commands = []cli.Command{
		checkpointCommand,
		createCommand,
		deleteCommand,
		eventsCommand,
		execCommand,
		killCommand,
		listCommand,
		pauseCommand,
		psCommand,
		restoreCommand,
		resumeCommand,
		runCommand,
		specCommand,
		startCommand,
		stateCommand,
		updateCommand,
		featuresCommand,
	}

	var logProxyConn, rawProxyConn net.Conn
	app.Before = func(context *cli.Context) error {
		if !context.IsSet("root") && xdgDirUsed {
			// According to the XDG specification, we need to set anything in
			// XDG_RUNTIME_DIR to have a sticky bit if we don't want it to get
			// auto-pruned.
			if err := os.MkdirAll(root, 0o700); err != nil {
				fmt.Fprintln(os.Stderr, "the path in $XDG_RUNTIME_DIR must be writable by the user")
				fatal(err)
			}
			if err := os.Chmod(root, os.FileMode(0o700)|os.ModeSticky); err != nil {
				fmt.Fprintln(os.Stderr, "you should check permission of the path in $XDG_RUNTIME_DIR")
				fatal(err)
			}
		}
		if err := reviseRootDir(context); err != nil {
			return err
		}

		opts := []logging.OptLoggerOptsSetter{
			logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
			logging.WithEnableDelimiter(true),
			logging.WithInterceptLogrus(false),
		}

		loggerName := fmt.Sprintf("runc[%s]", context.Args().First())

		opts = append(opts, logging.WithValues([]slog.Attr{
			slog.String("run_id", fmt.Sprintf("%d", runId)),
			slog.String("ppid", fmt.Sprintf("%d", os.Getppid())),
			slog.String("pid", fmt.Sprintf("%d", os.Getpid())),
		}))

		rawConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
		if err != nil {
			fmt.Printf("problem dialing raw log proxy: %v\n", err)
		} else {
			rawProxyConn = rawConn
			opts = append(opts, logging.WithRawWriter(rawConn))
		}

		delimConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
		if err != nil {
			fmt.Printf("problem dialing log proxy: %v\n", err)
		} else {
			// defer conn.Close()
			logProxyConn = delimConn
			_ = logging.NewDefaultDevLogger(loggerName, delimConn, opts...)
		}

		return configLogrus(context)
	}

	defer func() {
		if logProxyConn != nil {
			logProxyConn.Close()
		}
		if rawProxyConn != nil {
			rawProxyConn.Close()
		}
	}()

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

	defer func() {
		slog.Debug("DEBUG: RUNC IS DONE")
	}()

	// If the command returns an error, cli takes upon itself to print
	// the error on cli.ErrWriter and exit.
	// Use our own writer here to ensure the log gets sent to the right location.
	cli.ErrWriter = &FatalWriter{cli.ErrWriter}
	if err := app.Run(os.Args); err != nil {
		fatal(err)
	}
}

type FatalWriter struct {
	cliErrWriter io.Writer
}

func (f *FatalWriter) Write(p []byte) (n int, err error) {
	logrus.Error(string(p))
	if !logrusToStderr() {
		return f.cliErrWriter.Write(p)
	}
	return len(p), nil
}

func configLogrus(context *cli.Context) error {
	if context.GlobalBool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetReportCaller(true)
		// Shorten function and file names reported by the logger, by
		// trimming common "github.com/opencontainers/runc" prefix.
		// This is only done for text formatter.
		_, file, _, _ := runtime.Caller(0)
		prefix := filepath.Dir(file) + "/"
		logrus.SetFormatter(&logrus.TextFormatter{
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				function := strings.TrimPrefix(f.Function, prefix) + "()"
				fileLine := strings.TrimPrefix(f.File, prefix) + ":" + strconv.Itoa(f.Line)
				return function, fileLine
			},
		})
	}

	switch f := context.GlobalString("log-format"); f {
	case "":
		// do nothing
	case "text":
		// do nothing
	case "json":
		logrus.SetFormatter(new(logrus.JSONFormatter))
	default:
		return errors.New("invalid log-format: " + f)
	}

	if file := context.GlobalString("log"); file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_SYNC, 0o644)
		if err != nil {
			return err
		}
		logrus.SetOutput(f)
	}

	return nil
}
