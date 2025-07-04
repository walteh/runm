//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/mdlayher/vsock"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
)

// var binariesToCopy = []string{
// 	"/hbin/lshw",
// }

var (
	containerIdFlag string
	runmModeFlag    string
	bundleSource    string
	enableOtel      bool
	rawMbindsString string

	mbinds map[string]string
)

type runmLinuxMounter struct {
	rawWriter  io.WriteCloser
	logWriter  io.WriteCloser
	otelWriter io.WriteCloser

	logger *slog.Logger
}

// DO NOT USE SLOG IN THIS FUNCTION - LOG TO STDOUT
func (r *runmLinuxMounter) setupLogger(ctx context.Context) (context.Context, error) {
	var err error

	fmt.Printf("connecting to vsock for raw writer\n")

	rawWriterConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		return nil, errors.Errorf("problem dialing vsock for raw writer: %w", err)
	}

	fmt.Printf("connecting to vsock for delimited writer\n")

	delimitedLogProxyConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		return nil, errors.Errorf("problem dialing vsock for log proxy: %w", err)
	}

	opts := []logging.OptLoggerOptsSetter{
		logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
		logging.WithEnableDelimiter(true),
		logging.WithRawWriter(rawWriterConn),
	}

	var logger *slog.Logger
	if enableOtel {
		otelConn, err := vsock.Dial(2, uint32(constants.VsockOtelPort), nil)
		if err != nil {
			return nil, errors.Errorf("problem dialing vsock for otel: %w", err)
		}

		otelInstancez, err := logging.NewGRPCOtelInstances(ctx, otelConn, serviceName)
		if err != nil {
			return nil, errors.Errorf("failed to setup OTel SDK: %w", err)
		}

		logger = logging.NewDefaultDevLoggerWithOtel(ctx, serviceName, delimitedLogProxyConn, otelInstancez, opts...)

		r.otelWriter = otelConn

	} else {
		fmt.Printf("DEBUG: pid: %d - setting up logger without otel\n", os.Getpid())
		logger = logging.NewDefaultDevLogger(serviceName, delimitedLogProxyConn, opts...)
	}

	go func() {
		fmt.Fprintf(rawWriterConn, "test test 123 from raw writer\n")
	}()

	r.rawWriter = rawWriterConn
	r.logWriter = delimitedLogProxyConn
	r.logger = logger

	return slogctx.NewCtx(ctx, logger), nil
}

const (
	serviceName = "runm[mounter]"
)

func init() {
	flag.StringVar(&containerIdFlag, "container-id", "", "the container id")
	flag.StringVar(&runmModeFlag, "runm-mode", "", "the runm mode")
	flag.StringVar(&bundleSource, "bundle-source", "", "the bundle source")
	flag.StringVar(&rawMbindsString, "mbinds", "", "the mbinds")
	flag.BoolVar(&enableOtel, "enable-otlp", false, "enable otel")
	flag.Parse()

	mbinds = make(map[string]string)

	for _, mbind := range strings.Split(rawMbindsString, ",") {
		parts := strings.Split(mbind, ":")
		mbinds[parts[0]] = parts[1]
	}
}

func main() {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runmLinuxMounter := &runmLinuxMounter{}

	ctx, err := runmLinuxMounter.setupLogger(ctx)
	if err != nil {
		fmt.Printf("failed to setup logger: %v\n", err)
		os.Exit(1)
	}

	err = recoveryMain(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "error in main", "error", err)
		os.Exit(1)
	}
}

const (
	CGroupName = "runm"
)

func recoveryMain(ctx context.Context) (err error) {
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
		err = mount(ctx)
		errChan <- err
	}()

	return <-errChan
}

func mount(ctx context.Context) error {

	if runmModeFlag == "" {
		return errors.Errorf("runm-mode flag is required")
	}

	if containerIdFlag == "" {
		return errors.Errorf("container-id flag is required")
	}

	if _, err := os.Stat(constants.Ec1AbsPath); os.IsNotExist(err) {
		os.MkdirAll(constants.Ec1AbsPath, 0755)
	}

	// mount devtmpfs

	os.MkdirAll("/dev", 0755)
	os.MkdirAll("/sys", 0755) //
	os.MkdirAll("/proc", 0755)
	os.MkdirAll(constants.NewRootAbsPath, 0755)
	var err error

	// mount newroot onto itself - mount -t tmpfs tmpfs /newroot
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "tmpfs", "tmpfs", constants.NewRootAbsPath); err != nil {
		return errors.Errorf("mounting newroot onto itself: %w", err)
	}

	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "devtmpfs", "devtmpfs", "/dev"); err != nil {
		return errors.Errorf("problem mounting devtmpfs: %w", err)
	}

	// mount sysfs
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "sysfs", "sysfs", "/sys", "-o", "nosuid,noexec,nodev"); err != nil {
		return errors.Errorf("problem mounting sysfs: %w", err)
	}

	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "proc", "proc", "/proc"); err != nil {
		return errors.Errorf("problem mounting proc: %w", err)
	}

	if err := ExecCmdForwardingStdio(ctx, "sysctl", "-w", "kernel.pid_max=100000"); err != nil {
		return errors.Errorf("problem setting pid_max: %w", err)
	}

	if err := ExecCmdForwardingStdio(ctx, "sysctl", "-w", "user.max_user_namespaces=15000"); err != nil {
		return errors.Errorf("problem setting user.max_user_namespaces: %w", err)
	}

	// Mount the unified cgroup v2 hierarchy
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "cgroup2", "none", "/sys/fs/cgroup", "-o", "nsdelegate"); err != nil {
		return errors.Errorf("problem mounting cgroup2: %w", err)
	}

	// Enable the memory controller in the root cgroup
	if err := ExecCmdForwardingStdio(ctx, "sh", "-c", "echo +memory > /sys/fs/cgroup/cgroup.subtree_control"); err != nil {
		return errors.Errorf("failed to enable memory controller: %w", err)
	}

	// Create a child cgroup so that per-controller files appear (including memory.events)
	if err := ExecCmdForwardingStdio(ctx, "mkdir", "-p", "/sys/fs/cgroup/"+containerIdFlag); err != nil {
		return errors.Errorf("failed to create child cgroup: %w", err)
	}

	// mkdir newroot
	if err := os.MkdirAll(constants.NewRootAbsPath, 0755); err != nil {
		return errors.Errorf("failed to create newroot: %w", err)
	}

	// mkdir ec1
	// if err := os.MkdirAll(filepath.Join(constants.NewRootAbsPath, constants.Ec1AbsPath), 0755); err != nil {
	// 	return errors.Errorf("failed to create ec1: %w", err)
	// }

	// mkdir mbin
	if err := os.MkdirAll(filepath.Join(constants.NewRootAbsPath, constants.MbinAbsPath), 0755); err != nil {
		return errors.Errorf("failed to create mbin: %w", err)
	}

	// // List the new cgroup directory to verify memory.events is present
	// if err := ExecCmdForwardingStdio(ctx, "ls", "-lah", "/sys/fs/cgroup/"+containerIdFlag); err != nil {
	// 	return errors.Errorf("problem listing child cgroup: %w", err)
	// }

	// cmds = append(cmds, []string{"mkdir", "-p", prefix + "/dev/pts"})
	// cmds = append(cmds, []string{"mount", "-t", "devpts", "devpts", prefix + "/dev/pts", "-o", "gid=5,mode=620,ptmxmode=666"})

	// mount the ec1 virtiofs
	// err = ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.Ec1VirtioTag, filepath.Join(constants.NewRootAbsPath, constants.Ec1AbsPath))
	// if err != nil {
	// 	return errors.Errorf("problem mounting ec1 virtiofs: %w", err)
	// }

	// Add debugging for ec1 mount
	// slog.InfoContext(ctx, "DEBUG: EC1 virtiofs mount completed", "tag", constants.Ec1VirtioTag, "path", filepath.Join(constants.NewRootAbsPath, constants.Ec1AbsPath))
	// ExecCmdForwardingStdio(ctx, "ls", "-la", filepath.Join(constants.NewRootAbsPath, constants.Ec1AbsPath))
	// ExecCmdForwardingStdio(ctx, "cat", "/proc/mounts")
	// ExecCmdForwardingStdio(ctx, "df", "-h")

	for tag, target := range mbinds {
		out := filepath.Join(constants.NewRootAbsPath, target)
		if _, err := os.Stat(out); os.IsNotExist(err) {
			os.MkdirAll(out, 0755)
		}

		if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", tag, out); err != nil {
			return errors.Errorf("problem mounting mbind: %w", err)
		}
	}

	// if bundleSource != "" {
	// 	if err = os.MkdirAll(filepath.Join(constants.NewRootAbsPath, bundleSource), 0755); err != nil {
	// 		return errors.Errorf("problem creating bundle source: %w", err)
	// 	}

	// 	err = ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.BundleVirtioTag, filepath.Join(constants.NewRootAbsPath, bundleSource))
	// 	if err != nil {
	// 		return errors.Errorf("problem mounting ec1 virtiofs: %w", err)
	// 	}

	// 	// Add debugging for bundle mount
	// 	slog.InfoContext(ctx, "DEBUG: Bundle virtiofs mount completed", "tag", constants.BundleVirtioTag, "path", filepath.Join(constants.NewRootAbsPath, bundleSource))
	// 	ExecCmdForwardingStdio(ctx, "ls", "-la", bundleSource)
	// }

	// disableNewRoot := true

	// if !disableNewRoot {

	// 	if _, err := os.Stat(constants.NewRootAbsPath); os.IsNotExist(err) {
	// 		os.MkdirAll(constants.NewRootAbsPath, 0755)
	// 	}

	// 	err = ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.RootfsVirtioTag, constants.NewRootAbsPath)
	// 	if err != nil {
	// 		return errors.Errorf("problem mounting rootfs virtiofs: %w", err)
	// 	}

	// 	// Add debugging for rootfs mount
	// 	slog.InfoContext(ctx, "DEBUG: Rootfs virtiofs mount completed", "tag", constants.RootfsVirtioTag, "path", constants.NewRootAbsPath)
	// 	ExecCmdForwardingStdio(ctx, "ls", "-la", constants.NewRootAbsPath)

	// }

	// err = ExecCmdForwardingStdio(ctx, "ls", "-lah", "/proc")
	// if err != nil {
	// 	return errors.Errorf("problem mounting mbin: %w", err)
	// }

	// err = ExecCmdForwardingStdio(ctx, "ls", "-lah", "/proc")
	// if err != nil {
	// 	return errors.Errorf("problem mounting mbin: %w", err)
	// }

	// err = ExecCmdForwardingStdio(ctx, "cat", "/proc/filesystems")
	// if err != nil {
	// 	return errors.Errorf("problem mounting mbin: %w", err)
	// }

	// err = ExecCmdForwardingStdio(ctx, "ls", "-lah", "/dev")
	// if err != nil {
	// 	return errors.Errorf("problem mounting mbin: %w", err)
	// }

	// ///bin/zcat /proc/config.gz | /bin/grep CONFIG_SQUASHFS
	// err = ExecCmdForwardingStdio(ctx, "sh", "-c", "/bin/zcat /proc/config.gz | /bin/grep CONFIG_SQUASHFS")
	// if err != nil {
	// 	return errors.Errorf("problem mounting mbin: %w", err)
	// }
	if _, err := os.Stat(filepath.Join(constants.NewRootAbsPath, constants.MbinAbsPath)); os.IsNotExist(err) {
		os.MkdirAll(filepath.Join(constants.NewRootAbsPath, constants.MbinAbsPath), 0755)
	}
	err = ExecCmdForwardingStdio(ctx, "mount", "-t", constants.MbinFSType, "-o", "ro", constants.MbinVirtioTag, filepath.Join(constants.NewRootAbsPath, constants.MbinAbsPath))
	if err != nil {
		return errors.Errorf("problem mounting mbin: %w", err)
	}

	// mount the rootfs virtiofs

	// bindMounts, exists, err := loadBindMounts(ctx)
	// if err != nil {
	// 	return errors.Errorf("problem loading bind mounts: %w", err)
	// }

	// if !exists {
	// 	return errors.Errorf("no bind mounts found")
	// }

	// spec, exists, err := loadSpec(ctx)
	// if err != nil {
	// 	return errors.Errorf("problem loading spec: %w", err)
	// }

	// if !exists {
	// 	return errors.Errorf("no spec found")
	// }

	// if err := mountRootfsSecondary(ctx, constants.NewRootAbsPath, bindMounts); err != nil {
	// 	return errors.Errorf("problem mounting rootfs secondary: %w", err)
	// }

	// err = mountRootfsPrimary(ctx)
	// if err != nil {
	// 	return errors.Errorf("problem mounting rootfs: %w", err)
	// }

	os.MkdirAll(filepath.Join(constants.NewRootAbsPath, bundleSource, "rootfs"), 0755)

	// mount bundle/rootfs onto itself
	if err := ExecCmdForwardingStdio(ctx, "mount", "--bind", filepath.Join(constants.NewRootAbsPath, bundleSource, "rootfs"), filepath.Join(constants.NewRootAbsPath, bundleSource, "rootfs")); err != nil {
		return errors.Errorf("problem mounting bundle/rootfs onto itself: %w", err)
	}

	// ls -larh /var/lib/runm/ec1
	procMounts, err := exec.CommandContext(ctx, "/bin/busybox", "cat", "/proc/mounts").CombinedOutput()
	if err != nil {
		return errors.Errorf("problem listing proc mounts: %w", err)
	}
	slog.InfoContext(ctx, "cat /proc/mounts: "+string(procMounts))

	err = switchRoot(ctx)
	if err != nil {
		return errors.Errorf("problem switching root: %w", err)
	}

	return nil

}

func logFile(ctx context.Context, path string) {
	fmt.Println()
	fmt.Println("---------------" + path + "-----------------")
	_ = ExecCmdForwardingStdio(ctx, "ls", "-lah", path)
	_ = ExecCmdForwardingStdio(ctx, "cat", path)

}

func logCommand(ctx context.Context, cmd string) {
	fmt.Println()
	fmt.Println("---------------" + cmd + "-----------------")
	_ = ExecCmdForwardingStdio(ctx, "sh", "-c", cmd)
}

func logDirContents(ctx context.Context, path string) {
	fmt.Println()
	fmt.Println("---------------" + path + "-----------------")
	_ = ExecCmdForwardingStdio(ctx, "ls", "-lah", path)
}

func mountRootfsPrimary(ctx context.Context) error {

	// mkdir and mount the rootfs
	// if err := os.MkdirAll(constants.NewRootAbsPath, 0755); err != nil {
	// 	return errors.Errorf("making directories: %w", err)
	// }

	// if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.RootfsVirtioTag, constants.NewRootAbsPath); err != nil {
	// 	return errors.Errorf("mounting rootfs: %w", err)
	// }

	// _ = ExecCmdForwardingStdio(ctx, "ls", "-lah", "/newroot")

	// if err := os.MkdirAll(filepath.Join(constants.NewRootAbsPath), 0755); err != nil {
	// 	return errors.Errorf("making directories: %w", err)
	// }

	// // mount the first block device as the mbin
	// if err := ExecCmdForwardingStdio(ctx, "mount", "-t", constants.MbinFSType, "/dev/sda1", constants.MbinAbsPath); err != nil {
	// 	return errors.Errorf("mounting mbin: %w", err)
	// }

	// if err := ExecCmdForwardingStdio(ctx, "mount", "--move", constants.Ec1AbsPath, filepath.Join(constants.NewRootAbsPath, constants.Ec1AbsPath)); err != nil {
	// 	return errors.Errorf("mounting ec1: %w", err)
	// }

	cmds := [][]string{}

	// copyMounts, err := getCopyMountCommands(ctx)
	// if err != nil {
	// 	return errors.Errorf("getting copy mounts: %w", err)
	// }

	// cmds = append(cmds, copyMounts...)

	// for _, binary := range binariesToCopy {
	// 	cmds = append(cmds, []string{"mkdir", "-p", filepath.Join(constants.NewRootAbsPath, filepath.Dir(binary))})
	// 	cmds = append(cmds, []string{"touch", filepath.Join(constants.NewRootAbsPath, binary)})
	// 	cmds = append(cmds, []string{"mount", "--bind", binary, filepath.Join(constants.NewRootAbsPath, binary)})
	// }

	for _, cmd := range cmds {
		err := ExecCmdForwardingStdio(ctx, cmd...)
		if err != nil {
			return errors.Errorf("running command: %v: %w", cmd, err)
		}
	}

	return nil
}

func mountRootfsSecondary(ctx context.Context, prefix string, customMounts []specs.Mount) error {
	cmds := [][]string{}

	slices.SortFunc(customMounts, func(a, b specs.Mount) int {
		if a.Type == "virtiofs" {
			return 1
		}
		return -1
	})

	for _, mount := range customMounts {

		dest := filepath.Join(prefix, mount.Destination)

		if mount.Source == constants.RootfsVirtioTag {
			continue
		}

		if mount.Destination == "/dev" {
			continue
		}

		// Skip mounts that are already handled in initramfs
		if mount.Destination == "/proc" || mount.Destination == "/sys" || mount.Destination == "/run" ||
			mount.Destination == "/dev/pts" || mount.Destination == "/dev/shm" || mount.Destination == "/dev/mqueue" {
			continue
		}

		slog.InfoContext(ctx, "mounting", "dest", dest, "mount", mount)
		cmds = append(cmds, []string{"mkdir", "-p", dest})
		// if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		// 	return errors.Errorf("making directories: %w", err)
		// }

		if dest == prefix+constants.Ec1AbsPath {
			continue
		}

		opd := strings.Join(mount.Options, ",")
		opd = strings.TrimSuffix(opd, ",")

		opts := []string{}

		if opd != "" {
			opts = append(opts, "-o", opd)
		}

		// if mount.Destination == "/dev" {
		// 	mount.Type = "devtmpfs"
		// 	mount.Source = "devtmpfs"
		// }

		switch mount.Type {

		case "bind", "copy":
			continue
		default:
			allOpts := []string{"mount", "-t", mount.Type, mount.Source}
			allOpts = append(allOpts, opts...)
			allOpts = append(allOpts, dest)
			cmds = append(cmds, allOpts)

			cmds = append(cmds, []string{"ls", "-lah", dest})
		}
	}

	for _, cmd := range cmds {
		err := ExecCmdForwardingStdio(ctx, cmd...)
		if err != nil {
			return errors.Errorf("running command: %v: %w", cmd, err)
		}
	}

	// ExecCmdForwardingStdio(ctx, "ls", "-lah", "/app/scripts")

	return nil
}

func switchRoot(ctx context.Context) error {

	// if err := ExecCmdForwardingStdio(ctx, "touch", "/newroot/harpoond"); err != nil {
	// 	return errors.Errorf("touching harpoond: %w", err)
	// }

	// // bind hbin
	// if err := ExecCmdForwardingStdio(ctx, "ls", "-lah", "/newroot/hbin"); err != nil {
	// 	return errors.Errorf("binding hbin: %w", err)
	// }

	// rename ourself to new root
	// if err := ExecCmdForwardingStdio(ctx, "mount", "--bind", os.Args[0], "/newroot/harpoond"); err != nil {
	// 	return errors.Errorf("renaming self: %w", err)
	// }

	// mount newroot onto itself
	// if err := ExecCmdForwardingStdio(ctx, "mount", "--bind", constants.NewRootAbsPath, constants.NewRootAbsPath); err != nil {
	// 	return errors.Errorf("mounting newroot onto itself: %w", err)
	// }

	// mount /bin /sbin /usr/bin /usr/sbin /usr/local/bin /usr/local/sbin onto /newroot
	// copy the /bin/busybox to /newroot/bin/busybox
	os.MkdirAll(filepath.Join(constants.NewRootAbsPath, "bin"), 0755)
	if err := ExecCmdForwardingStdio(ctx, "cp", "/bin/busybox", "/newroot/bin/busybox"); err != nil {
		return errors.Errorf("copying busybox: %w", err)
	}

	// grep " /newroot " /proc/self/mountinfo
	if err := ExecCmdForwardingStdio(ctx, "grep", "/newroot", "/proc/self/mountinfo"); err != nil {
		return errors.Errorf("grepping mountinfo: %w", err)
	}

	// ensure the init thing xists
	ExecCmdForwardingStdio(ctx, "ls", "-lah", "/newroot/mbin")

	ExecCmdForwardingStdio(ctx, "ls", "-lah", "/mbin")

	entrypoint := append([]string{"/mbin/runm-linux-init"}, os.Args[1:]...)

	env := os.Environ()
	// argc := "/mbin/runm-linux-init"
	// argv := append([]string{argc}, os.Args[1:]...)
	// env = append(env, "PATH=/usr/sbin:/usr/bin:/sbin:/bin:/hbin")

	argc := "/bin/busybox"
	argv := append([]string{argc, "switch_root", constants.NewRootAbsPath}, entrypoint...)

	slog.InfoContext(ctx, "switching root - godspeed little process", "rootfs", constants.NewRootAbsPath, "argv", argv)

	if err := syscall.Exec(argc, argv, env); err != nil {
		return errors.Errorf("Failed to exec %v %v: %v", argc, argv, err)
	}

	panic("unreachable, we hand off to the entrypoint")

}

func loadBindMounts(ctx context.Context) (bindMounts []specs.Mount, exists bool, err error) {
	bindMountsBytes, err := os.ReadFile(filepath.Join(constants.NewRootAbsPath, constants.Ec1AbsPath, constants.ContainerMountsFile))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, false, errors.Errorf("reading bind mounts: %w", err)
		}
		return nil, false, nil
	}

	err = json.Unmarshal(bindMountsBytes, &bindMounts)
	if err != nil {
		return nil, false, errors.Errorf("unmarshalling bind mounts: %w", err)
	}

	return bindMounts, true, nil
}

func loadSpec(ctx context.Context) (spec *oci.Spec, exists bool, err error) {
	specd, err := os.ReadFile(filepath.Join(constants.NewRootAbsPath, constants.Ec1AbsPath, constants.ContainerSpecFile))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, errors.Errorf("reading spec: %w", err)
	}

	err = json.Unmarshal(specd, &spec)
	if err != nil {
		return nil, false, errors.Errorf("unmarshalling spec: %w", err)
	}

	return spec, true, nil
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

	slog.DebugContext(ctx, "executing command", "argc", argc, "argv", argv)
	cmd := exec.CommandContext(ctx, argc, argv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Cloneflags: syscall.CLONE_NEWNS,
		Chroot: chroot,
	}

	path := os.Getenv("PATH")

	cmd.Env = append([]string{"PATH=" + path + ":/hbin"}, os.Environ()...)

	cmd.Stdin = bytes.NewBuffer(nil) // set to avoid reading /dev/null since it may not be mounted
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return errors.Errorf("running busybox command (stdio was copied to the parent process): %v: %w", cmds, err)
	}

	return nil
}
