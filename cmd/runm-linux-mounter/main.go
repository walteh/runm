package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/containerd/containerd/v2/pkg/oci"
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
)

func init() {
	flag.StringVar(&containerIdFlag, "container-id", "", "the container id")
	flag.StringVar(&runmModeFlag, "runm-mode", "", "the runm mode")
	flag.StringVar(&bundleSource, "bundle-source", "", "the bundle source")
	flag.Parse()
}

func main() {

	pid := os.Getpid()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logging.NewDefaultDevLogger("runm-linux-mounter", os.Stdout)

	ctx = slogctx.NewCtx(ctx, logger)

	ctx = slogctx.Append(ctx, slog.Int("pid", pid))

	err := recoveryMain(ctx)
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

	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "devtmpfs", "devtmpfs", "/dev"); err != nil {
		return errors.Errorf("problem mounting devtmpfs: %w", err)
	}

	// {
	// 	"destination": "/sys/fs/cgroup",
	// 	"type": "cgroup",
	// 	"source": "cgroup",
	// 	"options": [
	// 		"nosuid",
	// 		"noexec",
	// 		"nodev",
	// 		"relatime",
	// 		"ro"
	// 	]
	// }

	// parse the args to get the container-id flag

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

	// List the new cgroup directory to verify memory.events is present
	if err := ExecCmdForwardingStdio(ctx, "ls", "-lah", "/sys/fs/cgroup/"+containerIdFlag); err != nil {
		return errors.Errorf("problem listing child cgroup: %w", err)
	}

	// cmds = append(cmds, []string{"mkdir", "-p", prefix + "/dev/pts"})
	// cmds = append(cmds, []string{"mount", "-t", "devpts", "devpts", prefix + "/dev/pts", "-o", "gid=5,mode=620,ptmxmode=666"})

	// mount the ec1 virtiofs
	err := ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.Ec1VirtioTag, constants.Ec1AbsPath)
	if err != nil {
		return errors.Errorf("problem mounting ec1 virtiofs: %w", err)
	}

	if bundleSource != "" {
		if err = os.MkdirAll(bundleSource, 0755); err != nil {
			return errors.Errorf("problem creating bundle source: %w", err)
		}

		err = ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.BundleVirtioTag, bundleSource)
		if err != nil {
			return errors.Errorf("problem mounting ec1 virtiofs: %w", err)
		}
	}

	if _, err := os.Stat(constants.NewRootAbsPath); os.IsNotExist(err) {
		os.MkdirAll(constants.NewRootAbsPath, 0755)
	}

	err = ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.RootfsVirtioTag, constants.NewRootAbsPath)
	if err != nil {
		return errors.Errorf("problem mounting rootfs virtiofs: %w", err)
	}

	if _, err := os.Stat(constants.MbinAbsPath); os.IsNotExist(err) {
		os.MkdirAll(constants.MbinAbsPath, 0755)
	}

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

	err = ExecCmdForwardingStdio(ctx, "mount", "-t", constants.MbinFSType, "-o", "ro", constants.MbinVirtioTag, constants.MbinAbsPath)
	if err != nil {
		return errors.Errorf("problem mounting mbin: %w", err)
	}

	// mount the rootfs virtiofs

	bindMounts, exists, err := loadBindMounts(ctx)
	if err != nil {
		return errors.Errorf("problem loading bind mounts: %w", err)
	}

	if !exists {
		return errors.Errorf("no bind mounts found")
	}

	// spec, exists, err := loadSpec(ctx)
	// if err != nil {
	// 	return errors.Errorf("problem loading spec: %w", err)
	// }

	// if !exists {
	// 	return errors.Errorf("no spec found")
	// }

	if err := mountRootfsSecondary(ctx, "", bindMounts); err != nil {
		return errors.Errorf("problem mounting rootfs secondary: %w", err)
	}

	err = mountRootfsPrimary(ctx)
	if err != nil {
		return errors.Errorf("problem mounting rootfs: %w", err)
	}

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

	_ = ExecCmdForwardingStdio(ctx, "ls", "-lah", "/newroot")

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

	for _, mount := range customMounts {

		dest := filepath.Join(prefix, mount.Destination)

		if mount.Source == constants.RootfsVirtioTag {
			continue
		}

		if mount.Destination == "/dev" {
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

		opts := []string{"-o", opd}
		if len(mount.Options) == 1 {
			opts = []string{}
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
		}
	}

	for _, cmd := range cmds {
		err := ExecCmdForwardingStdioChroot(ctx, constants.NewRootAbsPath, cmd...)
		if err != nil {
			return errors.Errorf("running command: %v: %w", cmd, err)
		}
	}

	ExecCmdForwardingStdio(ctx, "ls", "-lah", "/app/scripts")

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

	// entrypoint := []string{"/mbin/runm-linux-init"}

	env := os.Environ()
	argc := "/mbin/runm-linux-init"
	argv := append([]string{argc}, os.Args[1:]...)
	// env = append(env, "PATH=/usr/sbin:/usr/bin:/sbin:/bin:/hbin")

	// argc := "/bin/busybox"
	// argv := append([]string{"switch_root", constants.NewRootAbsPath}, entrypoint...)

	slog.InfoContext(ctx, "switching root - godspeed little process", "rootfs", constants.NewRootAbsPath, "argv", argv)

	if err := syscall.Exec(argc, argv, env); err != nil {
		return errors.Errorf("Failed to exec %v %v: %v", argc, argv, err)
	}

	panic("unreachable, we hand off to the entrypoint")

}

func loadBindMounts(ctx context.Context) (bindMounts []specs.Mount, exists bool, err error) {
	bindMountsBytes, err := os.ReadFile(filepath.Join(constants.Ec1AbsPath, constants.ContainerMountsFile))
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
	specd, err := os.ReadFile(filepath.Join(constants.Ec1AbsPath, constants.ContainerSpecFile))
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
