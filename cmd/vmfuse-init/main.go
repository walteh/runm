//go:build !windows

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/mdlayher/vsock"
	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
)

var (
	mountType    string
	mountSources string // comma-separated paths
	mountTarget  string
	exportPath   string
	enableOtel   bool
	readyFile    string
)

const serviceName = "vmfuse[init]"

type vmfuseInit struct {
	rawWriter  io.WriteCloser
	logWriter  io.WriteCloser
	otelWriter io.WriteCloser
	logger     *slog.Logger
}

func init() {
	flag.StringVar(&mountType, "vmfuse-mount-type", "bind", "type of mount: bind or overlay")
	flag.StringVar(&mountSources, "vmfuse-mount-sources", "", "comma-separated source paths")
	flag.StringVar(&mountTarget, "vmfuse-mount-target", "/mnt/target", "target mount point")
	flag.StringVar(&exportPath, "vmfuse-export-path", "/export", "NFS export path")
	flag.StringVar(&readyFile, "vmfuse-ready-file", "/virtiofs/ready", "file to create when ready")
	flag.BoolVar(&enableOtel, "enable-otlp", false, "enable otlp")

	_ = flag.String("runm-mode", "vmfuse", "the runm mode")
	_ = flag.String("timezone", "UTC", "the timezone")
	_ = flag.String("time", "0", "the time")
	_ = flag.String("init-mbin-name", "vmfuse-init", "the init mbin name")
	_ = flag.String("mshare-dir-binds", "", "the mshare dir binds")
	// _ = flag.String("tim")

	flag.Parse()
}

// DO NOT USE SLOG IN THIS FUNCTION - LOG TO STDOUT
func main() {
	var exitCode = 0
	defer func() {
		os.Exit(exitCode)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vmfuseInit := &vmfuseInit{}

	ctx, err := vmfuseInit.setupLogger(ctx)
	if err != nil {
		fmt.Printf("failed to setup logger: %v\n", err)
		exitCode = 1
		return
	}

	err = recoveryMain(ctx, vmfuseInit)
	if err != nil {
		slog.ErrorContext(ctx, "error in main", "error", err)
		exitCode = 1
		return
	}
}

func (v *vmfuseInit) setupLogger(ctx context.Context) (context.Context, error) {
	fmt.Println("vmfuse-init: setting up logging - all future logs will be sent to vsock (pid: ", os.Getpid(), ")")

	rawWriterConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		return nil, errors.Errorf("problem dialing vsock for raw writer: %w", err)
	}

	delimitedLogProxyConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		return nil, errors.Errorf("problem dialing vsock for log proxy: %w", err)
	}

	opts := []logging.LoggerOpt{
		logging.WithRawWriter(rawWriterConn),
	}

	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return vsock.Dial(2, uint32(constants.VsockOtelPort), nil)
	}

	cleanup, err := otel.ConfigureOTelSDKWithDialer(ctx, serviceName, enableOtel, dialer)
	if err != nil {
		return nil, errors.Errorf("failed to setup OTel SDK: %w", err)
	}

	defer cleanup()

	logger := logging.NewDefaultDevLoggerWithDelimiter(serviceName, delimitedLogProxyConn, opts...)

	v.rawWriter = rawWriterConn
	v.logWriter = delimitedLogProxyConn
	v.logger = logger

	return slogctx.NewCtx(ctx, logger), nil
}

func recoveryMain(ctx context.Context, v *vmfuseInit) (err error) {
	errChan := make(chan error)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				debug.PrintStack()
				fmt.Println("panic in main", r)
				slog.ErrorContext(ctx, "panic in main", "error", r)
				err = errors.Errorf("panic in main: %v", r)
				errChan <- err
			}
		}()
		err := v.run(ctx)
		errChan <- err
	}()

	return <-errChan
}

func (v *vmfuseInit) run(ctx context.Context) error {
	slog.InfoContext(ctx, "starting vmfuse-init",
		"mount_type", mountType,
		"mount_sources", mountSources,
		"mount_target", mountTarget,
		"export_path", exportPath,
		"ready_file", readyFile)

	// Setup basic mounts needed for Linux environment
	if err := v.setupBasicMounts(ctx); err != nil {
		return errors.Errorf("setting up basic mounts: %w", err)
	}

	// Create mount target directory
	if err := os.MkdirAll(mountTarget, 0755); err != nil {
		return errors.Errorf("creating mount target directory: %w", err)
	}

	// Perform the requested mount
	if err := v.performMount(ctx); err != nil {
		return errors.Errorf("performing mount: %w", err)
	}

	// Setup and start NFS server
	if err := v.setupNFS(ctx); err != nil {
		return errors.Errorf("setting up NFS: %w", err)
	}

	// Signal readiness
	if err := v.signalReady(ctx); err != nil {
		return errors.Errorf("signaling ready: %w", err)
	}

	// Keep running to serve NFS
	select {
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (v *vmfuseInit) setupBasicMounts(ctx context.Context) error {
	// Create basic directories
	dirs := []string{"/dev", "/sys", "/proc", "/tmp", "/var", "/etc"}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errors.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Mount proc
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "proc", "proc", "/proc"); err != nil {
		return errors.Errorf("mounting proc: %w", err)
	}

	// Mount sysfs
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "sysfs", "sysfs", "/sys"); err != nil {
		return errors.Errorf("mounting sysfs: %w", err)
	}

	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "devtmpfs", "devtmpfs", "/dev"); err != nil {
		return errors.Errorf("mounting devtmpfs: %w", err)
	}

	// Mount devtmpfs

	return nil
}

func (v *vmfuseInit) performMount(ctx context.Context) error {
	sources := strings.Split(mountSources, ",")

	switch mountType {
	case "bind":
		if len(sources) != 1 {
			return errors.Errorf("bind mount requires exactly one source, got %d", len(sources))
		}

		slog.InfoContext(ctx, "performing bind mount", "source", sources[0], "target", mountTarget)
		return ExecCmdForwardingStdio(ctx, "mount", "--bind", sources[0], mountTarget)

	case "overlay":
		if len(sources) < 2 {
			return errors.Errorf("overlay mount requires at least 2 sources (lower,upper), got %d", len(sources))
		}

		// Assume sources[0] is lower, sources[1] is upper, sources[2] is work (if provided)
		lower := sources[0]
		upper := sources[1]

		// Create work directory if not provided
		work := filepath.Join(filepath.Dir(upper), "work")
		if len(sources) >= 3 {
			work = sources[2]
		}

		// Ensure upper and work directories exist
		if err := os.MkdirAll(upper, 0755); err != nil {
			return errors.Errorf("creating upper directory: %w", err)
		}
		if err := os.MkdirAll(work, 0755); err != nil {
			return errors.Errorf("creating work directory: %w", err)
		}

		opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s,index=on,nfs_export=on", lower, upper, work)

		slog.InfoContext(ctx, "performing overlay mount",
			"lower", lower, "upper", upper, "work", work, "target", mountTarget)

		return ExecCmdForwardingStdio(ctx, "mount", "-t", "overlay", "overlay", "-o", opts, mountTarget)

	default:
		return errors.Errorf("unsupported mount type: %s", mountType)
	}
}

func (v *vmfuseInit) setupNFS(ctx context.Context) error {
	// Create NFS export directory
	// if err := os.MkdirAll(exportPath, 0755); err != nil {
	// 	return errors.Errorf("creating export path: %w", err)
	// }

	// // // Create /etc/exports
	// if err := os.MkdirAll("/etc", 0755); err != nil {
	// 	return errors.Errorf("creating /etc directory: %w", err)
	// }

	// exportsContent := fmt.Sprintf("%s *(rw,sync,no_subtree_check,no_root_squash,fsid=0)\n", mountTarget)
	// if err := os.WriteFile("/etc/exports", []byte(exportsContent), 0644); err != nil {
	// 	return errors.Errorf("writing /etc/exports: %w", err)
	// }

	// slog.InfoContext(ctx, "created NFS exports", "content", exportsContent)

	// // Start NFS kernel server
	// go func() {
	// 	// if err := ExecCmdForwardingStdio(ctx, "nfsd", "8"); err != nil {
	// 	// 	slog.ErrorContext(ctx, "failed to start NFS server", "error", err)
	// 	// }
	// 	if err := startNFS(ctx, 8); err != nil {
	// 		slog.ErrorContext(ctx, "failed to start NFS server", "error", err)
	// 	}
	// }()

	if err := exportNfsDir(ctx, mountTarget, 8); err != nil {
		return errors.Errorf("exporting path: %w", err)
	}

	// Export filesystems
	// if err := ExecCmdForwardingStdio(ctx, "exportfs", "-ra"); err != nil {
	// 	return errors.Errorf("running exportfs: %w", err)
	// }

	slog.InfoContext(ctx, "NFS server started", "export_path", mountTarget)

	return nil
}

func (v *vmfuseInit) signalReady(ctx context.Context) error {
	// Create the ready file directory if needed
	readyDir := filepath.Dir(readyFile)
	if err := os.MkdirAll(readyDir, 0755); err != nil {
		return errors.Errorf("creating ready file directory: %w", err)
	}

	// Write ready signal
	if err := os.WriteFile(readyFile, []byte("ready"), 0644); err != nil {
		return errors.Errorf("writing ready file: %w", err)
	}

	slog.InfoContext(ctx, "signaled ready", "ready_file", readyFile)

	return nil
}

func ExecCmdForwardingStdio(ctx context.Context, cmds ...string) error {
	return ExecCmdForwardingStdioChroot(ctx, "", cmds...)
}

func ExecCmdForwardingStdioChroot(ctx context.Context, chroot string, cmds ...string) error {
	if len(cmds) == 0 {
		return errors.Errorf("no command to execute")
	}

	argc := "/bin/busybox"
	if strings.HasPrefix(cmds[0], "/") {
		argc = cmds[0]
		cmds = cmds[1:]
	}
	argv := cmds

	cmd := exec.CommandContext(ctx, argc, argv...)

	slog.DebugContext(ctx, "executing command '"+strings.Join(cmds, " ")+"'", "argc", argc, "argv", argv)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Cloneflags: syscall.CLONE_NEWNS,
		Chroot: chroot,
	}

	path := os.Getenv("PATH")

	cmd.Env = append([]string{"PATH=" + path + ":/hbin"}, os.Environ()...)

	stdoutBuf := bytes.NewBuffer(nil)
	stderrBuf := bytes.NewBuffer(nil)

	cmd.Stdin = bytes.NewBuffer(nil) // set to avoid reading /dev/null since it may not be mounted
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	err := cmd.Run()
	level := slog.LevelDebug

	if err != nil {
		level = slog.LevelError
	}

	slog.Log(ctx, level, "finished running command '"+strings.Join(cmds, " ")+"'", "stdout", stdoutBuf.String(), "stderr", stderrBuf.String(), "error", err)

	return err
}
