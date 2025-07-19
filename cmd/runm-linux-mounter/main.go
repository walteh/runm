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

// var binariesToCopy = []string{
// 	"/hbin/lshw",
// }

var (
	containerIdFlag  string
	runmModeFlag     string
	bundleSource     string
	enableOtel       bool
	mfsBindsString   string
	msockBindsString string
	timezone         string
	time             string // unix timestamp in nanoseconds, not meant to be exact (that is what the timesync does)

	mfsBinds   map[string]string
	msockBinds map[string]string
)

type runmLinuxMounter struct {
	rawWriter  io.WriteCloser
	logWriter  io.WriteCloser
	otelWriter io.WriteCloser

	logger *slog.Logger
}

// DO NOT USE SLOG IN THIS FUNCTION - LOG TO STDOUT
func (r *runmLinuxMounter) setupLogger(ctx context.Context) (context.Context, func(), error) {
	var err error

	fmt.Println("linux-runm-mounter: setting up logging - all future logs will be sent to vsock (pid: ", os.Getpid(), ")")

	rawWriterConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		return nil, nil, errors.Errorf("problem dialing vsock for raw writer: %w", err)
	}

	delimitedLogProxyConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		return nil, nil, errors.Errorf("problem dialing vsock for log proxy: %w", err)
	}

	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return vsock.Dial(2, uint32(constants.VsockOtelPort), nil)
	}

	cleanup, err := otel.ConfigureOTelSDKWithDialer(ctx, serviceName, enableOtel, dialer)
	if err != nil {
		return nil, nil, errors.Errorf("failed to setup OTel SDK: %w", err)
	}

	opts := []logging.LoggerOpt{
		logging.WithRawWriter(rawWriterConn),
	}

	logger := logging.NewDefaultDevLoggerWithDelimiter(serviceName, delimitedLogProxyConn, opts...)

	r.rawWriter = rawWriterConn
	r.logWriter = delimitedLogProxyConn
	r.logger = logger

	return slogctx.NewCtx(ctx, logger), func() {
		cleanup()
	}, nil
}

const (
	serviceName = "runm[mounter]"
)

func init() {
	flag.StringVar(&containerIdFlag, "container-id", "", "the container id")
	flag.StringVar(&runmModeFlag, "runm-mode", "", "the runm mode")
	flag.StringVar(&bundleSource, "bundle-source", "", "the bundle source")
	flag.StringVar(&mfsBindsString, "mfs-binds", "", "the mfs binds")
	flag.StringVar(&msockBindsString, "msock-binds", "", "the msock binds")
	flag.BoolVar(&enableOtel, "enable-otlp", false, "enable otel")
	flag.StringVar(&timezone, "timezone", "UTC", "the timezone")
	flag.StringVar(&time, "time", "0", "the time in nanoseconds")
	flag.Parse()

	mfsBinds = make(map[string]string)
	msockBinds = make(map[string]string)

	if mfsBindsString != "" {
		for _, mbind := range strings.Split(mfsBindsString, ",") {
			parts := strings.Split(mbind, constants.MbindSeparator)
			mfsBinds[parts[0]] = parts[1]
		}
	}

	if msockBindsString != "" {
		for _, mbind := range strings.Split(msockBindsString, ",") {
			parts := strings.Split(mbind, constants.MbindSeparator)
			msockBinds[parts[0]] = parts[1]
		}
	}
}

func main() {
	var exitCode = 0

	defer func() {
		os.Exit(exitCode)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runmLinuxMounter := &runmLinuxMounter{}

	ctx, cleanup, err := runmLinuxMounter.setupLogger(ctx)
	if err != nil {
		fmt.Printf("failed to setup logger: %v\n", err)
		exitCode = 1
		return
	}

	defer cleanup()

	err = recoveryMain(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "error in main", "error", err)
		exitCode = 1
		return
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

	// Make newroot mount private to prevent propagation to container namespaces
	if err := ExecCmdForwardingStdio(ctx, "mount", "--make-private", constants.NewRootAbsPath); err != nil {
		return errors.Errorf("making newroot private: %w", err)
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

	for tag, target := range mfsBinds {
		out := filepath.Join(constants.NewRootAbsPath, target)
		if _, err := os.Stat(out); os.IsNotExist(err) {
			os.MkdirAll(out, 0755)
		}

		if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", tag, out); err != nil {
			return errors.Errorf("problem mounting mbind: %w", err)
		}
	}

	if _, err := os.Stat(filepath.Join(constants.NewRootAbsPath, constants.MbinAbsPath)); os.IsNotExist(err) {
		os.MkdirAll(filepath.Join(constants.NewRootAbsPath, constants.MbinAbsPath), 0755)
	}
	err = ExecCmdForwardingStdio(ctx, "mount", "-t", constants.MbinFSType, "-o", "ro", constants.MbinVirtioTag, filepath.Join(constants.NewRootAbsPath, constants.MbinAbsPath))
	if err != nil {
		return errors.Errorf("problem mounting mbin: %w", err)
	}

	////////////////////////////////////////////////////////////////////////////////////////////////////////todo

	// os.MkdirAll(filepath.Join(constants.NewRootAbsPath, bundleSource, "rootfs"), 0755)

	// // mount bundle/rootfs onto itself
	// if err := ExecCmdForwardingStdio(ctx, "mount", "--bind", filepath.Join(constants.NewRootAbsPath, bundleSource, "rootfs"), filepath.Join(constants.NewRootAbsPath, bundleSource, "rootfs")); err != nil {
	// 	return errors.Errorf("problem mounting bundle/rootfs onto itself: %w", err)
	// }

	////////////////////////////////////////////////////////////////////////////////////////////////////////

	// ls -larh /var/lib/runm/ec1
	procMounts, err := exec.CommandContext(ctx, "/bin/busybox", "cat", "/proc/mounts").CombinedOutput()
	if err != nil {
		return errors.Errorf("problem listing proc mounts: %w", err)
	}
	slog.InfoContext(ctx, "cat /proc/mounts: "+string(procMounts))

	os.MkdirAll(filepath.Join(constants.NewRootAbsPath, "etc"), 0755)

	if err := os.WriteFile(filepath.Join(constants.NewRootAbsPath, "etc", "resolv.conf"), []byte("nameserver 192.168.127.1"), 0644); err != nil {
		return errors.Errorf("problem updating resolve.conf: %w", err)
	}

	// os.MkdirAll(filepath.Join(constants.NewRootAbsPath, "/usr/share/udhcpc"), 0755)

	// if err := os.WriteFile(filepath.Join(constants.NewRootAbsPath, "/usr/share/udhcpc/default.script"), []byte(udhcpcScript), 0755); err != nil {
	// 	return errors.Errorf("problem updating udhcpc.script: %w", err)
	// }

	err = switchRoot(ctx)
	if err != nil {
		return errors.Errorf("problem switching root: %w", err)
	}

	// update the resolve.conf to nameserver 192.168.127.1

	return nil

}

// var udhcpcScript = `
// #!/bin/sh
// # Busybox udhcpc dispatcher script. Copyright (C) 2009 by Axel Beckert.
// #
// # Based on the busybox example scripts and the old udhcp source
// # package default.* scripts.
// RESOLV_CONF="/etc/resolv.conf"

// case $1 in
// 	bound | renew)
// 		[ -n "$broadcast" ] && BROADCAST="broadcast $broadcast"
// 		[ -n "$subnet" ] && NETMASK="netmask $subnet"
// 		/bin/busybox ifconfig $interface $ip $BROADCAST $NETMASK
// 		if [ -n "$router" ]; then
// 			echo "$0: Resetting default routes"
// 			while /bin/busybox route del default gw 0.0.0.0 dev $interface; do :; done
// 			metric=0
// 			for i in $router; do
// 				/bin/busybox route add default gw $i dev $interface metric $metric
// 				metric=$(($metric + 1))
// 			done
// 		fi
// 		# Update resolver configuration file
// 		R=""
// 		[ -n "$domain" ] && R="domain $domain
// "
// 		for i in $dns; do
// 			echo "$0: Adding DNS $i"
// 			R="${R}nameserver $i
// "
// 		done
// 		#if [ -x /bin/busybox resolvconf ]; then
// 			echo -n "$R" | /bin/busybox resolvconf -a "${interface}.udhcpc"
// 		#else
// 			# echo -n "$R" > "$RESOLV_CONF"
// 		#fi
// 		;;
// 	deconfig)
// 		#if [ -x /bin/busybox resolvconf ]; then
// 			/bin/busybox resolvconf -d "${interface}.udhcpc"
// 		#fi
// 		/bin/busybox ifconfig $interface 0.0.0.0
// 		;;
// 	leasefail)
// 		echo "$0: Lease failed: $message"
// 		;;
// 	nak)
// 		echo "$0: Received a NAK: $message"
// 		;;
// 	*)
// 		echo "$0: Unknown udhcpc command: $1"
// 		exit 1
// 		;;
// esac
// `

func switchRoot(ctx context.Context) error {

	zoneinfoPath := filepath.Join(constants.NewRootAbsPath, "/usr/share/zoneinfo")
	os.MkdirAll(zoneinfoPath, 0755)
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.ZoneInfoVirtioTag, zoneinfoPath, "-o", "ro"); err != nil {
		return errors.Errorf("mounting zoneinfo: %w", err)
	}

	// Mount CA certificates for TLS verification (mount whole /etc/ssl from macOS host)
	caCertsPath := filepath.Join(constants.NewRootAbsPath, "/etc/ssl")
	os.MkdirAll(caCertsPath, 0755)
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.CaCertsVirtioTag, caCertsPath, "-o", "ro"); err != nil {
		return errors.Errorf("mounting ca certs: %w", err)
	}

	// // Mount zoneinfo to VM-specific path to avoid conflicts with container /usr
	// vmZoneinfoPath := filepath.Join(constants.NewRootAbsPath, "/var/lib/runm/zoneinfo")
	// os.MkdirAll(vmZoneinfoPath, 0755)
	// if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "virtiofs", constants.ZoneInfoVirtioTag, vmZoneinfoPath, "-o", "ro"); err != nil {
	// 	return errors.Errorf("mounting zoneinfo: %w", err)
	// }

	// // Create symlink from expected location to VM path
	// os.MkdirAll(filepath.Join(constants.NewRootAbsPath, "usr/share"), 0755)
	// // zoneinfoPath := filepath.Join(constants.NewRootAbsPath, "/usr/share/zoneinfo")
	// if err := ExecCmdForwardingStdioChroot(ctx, constants.NewRootAbsPath, "ln", "-sf", "/var/lib/runm/zoneinfo", "/usr/share/zoneinfo"); err != nil {
	// 	return errors.Errorf("symlinking zoneinfo: %w", err)
	// }

	// timezone in newroot

	// if err := os.Symlink(filepath.Join(zoneinfoPath, timezone), filepath.Join(constants.NewRootAbsPath, "etc", "localtime")); err != nil {
	// 	return errors.Errorf("symlinking localtime: %w", err)
	// }

	os.MkdirAll(filepath.Join(constants.NewRootAbsPath, "bin"), 0755)
	if err := ExecCmdForwardingStdio(ctx, "cp", "/bin/busybox", "/newroot/bin/busybox"); err != nil {
		return errors.Errorf("copying busybox: %w", err)
	}

	// grep " /newroot " /proc/self/mountinfo
	if err := ExecCmdForwardingStdio(ctx, "grep", "/newroot", "/proc/self/mountinfo"); err != nil {
		return errors.Errorf("grepping mountinfo: %w", err)
	}

	os.MkdirAll(filepath.Join(constants.NewRootAbsPath, "etc"), 0755)
	if err := ExecCmdForwardingStdioChroot(ctx, constants.NewRootAbsPath, "ln", "-sf", filepath.Join("/usr/share/zoneinfo", timezone), "/etc/localtime"); err != nil {
		return errors.Errorf("copying localtime: %w", err)
	}

	entrypoint := append([]string{"/mbin/runm-linux-init"}, os.Args[1:]...)

	env := os.Environ()

	argc := "/bin/busybox"
	argv := append([]string{argc, "switch_root", constants.NewRootAbsPath}, entrypoint...)

	slog.InfoContext(ctx, "switching root - godspeed little process", "rootfs", constants.NewRootAbsPath, "argv", argv)

	if err := syscall.Exec(argc, argv, env); err != nil {
		return errors.Errorf("Failed to exec %v %v: %v", argc, argv, err)
	}

	panic("unreachable, we hand off to the entrypoint")

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
