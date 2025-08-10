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
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/vishvananda/netlink"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/gvnet"
	"github.com/walteh/runm/core/virt/guest/managerserver"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/pkg/taskgroup"
	"github.com/walteh/runm/pkg/ticker"
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
	cancel     context.CancelFunc
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

func main() {
	var exitCode = 0
	defer func() {
		os.Exit(exitCode)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vmfuseInit := &vmfuseInit{}

	defer func() {
		if vmfuseInit.cancel != nil {
			vmfuseInit.cancel()
		}
	}()

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
		// return vsock.Dial(2, uint32(constants.VsockOtelPort), nil)
		return net.Dial("tcp", fmt.Sprintf("%s:4317", gvnet.VIRTUAL_GATEWAY_IP))
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

func handleExitSignals(ctx context.Context, cancel context.CancelFunc) {
	ch := make(chan os.Signal, 32)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case s := <-ch:
			slog.InfoContext(ctx, "Caught exit signal", "signal", s)
			cancel()
			return
		case <-ctx.Done():
			return
		}
	}
}

func (v *vmfuseInit) run(ctx context.Context) error {
	slog.InfoContext(ctx, "starting vmfuse-init",
		"mount_type", mountType,
		"mount_sources", mountSources,
		"mount_target", mountTarget,
		"export_path", exportPath,
		"ready_file", readyFile)

	defer ticker.NewTicker(
		ticker.WithMessage("VMFUSE:INIT[RUNNING]"),
		ticker.WithDoneMessage("VMFUSE:INIT[DONE]"),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
		ticker.WithFrequency(15),
		ticker.WithStartBurst(5),
		ticker.WithAttrFunc(func() []slog.Attr {
			return []slog.Attr{
				slog.Int("pid", os.Getpid()),
				slog.String("gomaxprocs", strconv.Itoa(runtime.GOMAXPROCS(0))),
			}
		}),
	).RunAsDefer()()

	ctx, cancel := context.WithCancel(ctx)
	go handleExitSignals(ctx, cancel)

	v.cancel = cancel
	// Create TaskGroup with pprof enabled and custom labels
	taskgroupz := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("vmfuse-init"),
		taskgroup.WithEnablePprof(true),
		taskgroup.WithPprofLabels(map[string]string{
			"service":   serviceName,
			"runm-mode": "vmfuse",
		}),
		taskgroup.WithSlogBaseContext(ctx),
	)

	if err := configureNetwork(ctx); err != nil {
		return errors.Errorf("setting up network: %w", err)
	}

	taskgroupz.GoWithName("grpc-vsock-server", func(ctx context.Context) error {

		server := grpc.NewServer(
			grpcerr.GetGrpcServerOptsCtx(ctx),
			otel.GetGrpcServerOpts(),
		)

		managerserver.Register(server)

		return v.runGrpcVsockServer(ctx, server)
	})

	// Setup basic mounts needed for Linux environment
	if err := v.setupBasicMounts(ctx); err != nil {
		return errors.Errorf("setting up basic mounts: %w", err)
	}

	// Redirect syslog to stdout for better visibility
	// Also redirect /dev/log to stdout if possible
	if err := v.redirectDevLogToStdout(ctx); err != nil {
		slog.WarnContext(ctx, "failed to redirect /dev/log", "error", err)
	}

	// Create mount target directory
	if err := os.MkdirAll(mountTarget, 0755); err != nil {
		return errors.Errorf("creating mount target directory: %w", err)
	}

	// Perform the requested mount
	if err := v.performMount(ctx); err != nil {
		return errors.Errorf("performing mount: %w", err)
	}

	// ls -la the mount target
	if err := ExecCmdForwardingStdio(ctx, "ls", "-lahrs", mountTarget); err != nil {
		return errors.Errorf("listing mount target: %w", err)
	}

	// Setup and start Ganesha NFS server
	if err := v.setupGaneshaNFS(ctx); err != nil {
		return errors.Errorf("setting up Ganesha NFS: %w", err)
	}

	// Keep running to serve NFS
	<-ctx.Done()
	return ctx.Err()
}

func (v *vmfuseInit) setupBasicMounts(ctx context.Context) error {
	// Create basic directories including those required by Ganesha
	dirs := []string{"/dev", "/sys", "/proc", "/tmp", "/var", "/var/run", "/var/log", "/etc"}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errors.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Create Ganesha-specific directories
	ganeshaDir := "/var/run/ganesha"
	if err := os.MkdirAll(ganeshaDir, 0755); err != nil {
		return errors.Errorf("creating ganesha directory %s: %w", ganeshaDir, err)
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

// handleLogConnection reads from a log connection and writes to stdout
func (v *vmfuseInit) handleLogConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, err := conn.Read(buf)
			if err != nil {
				return
			}

			if n > 0 {
				// Write to stdout with a prefix
				logMsg := fmt.Sprintf("SYSLOG: %s", string(buf[:n]))
				fmt.Print(logMsg)
			}
		}
	}
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

func (v *vmfuseInit) runGrpcVsockServer(ctx context.Context, server *grpc.Server) error {
	slog.InfoContext(ctx, "listening on vsock", "port", constants.RunmGuestServerVsockPort)
	listener, err := vsock.ListenContextID(3, uint32(constants.RunmGuestServerVsockPort), nil)
	if err != nil {
		slog.ErrorContext(ctx, "problem listening vsock", "error", err)
		return errors.Errorf("problem listening vsock: %w", err)
	}

	if err := server.Serve(listener); err != nil {
		return errors.Errorf("problem serving grpc vsock server: %w", err)
	}

	return nil
}

func (v *vmfuseInit) verifyNFSPorts(ctx context.Context) {
	// Check the specific ports that Ganesha should be using
	nfsPorts := []struct {
		port int
		name string
	}{
		{2049, "NFS"},
		{20048, "MountD"}, // if NFSv3 support is enabled
	}

	for _, p := range nfsPorts {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", p.port), 1*time.Second)
		if err != nil {
			slog.WarnContext(ctx, "NFS port not accessible",
				"port", p.port,
				"service", p.name,
				"error", err)
		} else {
			_ = conn.Close()
			slog.InfoContext(ctx, "NFS port is accessible",
				"port", p.port,
				"service", p.name)
		}
	}
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
	start := time.Now()

	err := cmd.Run()
	level := slog.LevelDebug

	if err != nil {
		level = slog.LevelError
	}

	slog.Log(ctx, level, "finished running command '"+strings.Join(cmds, " ")+"'", "stdout", stdoutBuf.String(), "stderr", stderrBuf.String(), "error", err, "duration", time.Since(start))

	return err
}

func configureNetwork(ctx context.Context) error {

	gatewayIp := gvnet.VIRTUAL_GATEWAY_IP
	guestIp := gvnet.VIRTUAL_GUEST_IP
	// Find eth0 interface
	link, err := netlink.LinkByName("eth0")
	if err != nil {
		return errors.Errorf("finding eth0 interface: %w", err)
	}

	slog.InfoContext(ctx, "found network interface", "name", link.Attrs().Name, "mac", link.Attrs().HardwareAddr)

	guestNet := guestIp + "/24"

	// Bring up the interface
	if err := netlink.LinkSetUp(link); err != nil {
		return errors.Errorf("bringing up eth0: %w", err)
	}

	// Parse IP address and network
	ipNet, err := netlink.ParseIPNet(guestNet)
	if err != nil {
		return errors.Errorf("parsing IP network: %w", err)
	}

	// Add IP address to interface
	addr := &netlink.Addr{IPNet: ipNet}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return errors.Errorf("adding IP address to eth0: %w", err)
	}

	slog.InfoContext(ctx, "configured IP address", "interface", "eth0", "ip", guestNet)

	// Add default route
	gateway := net.ParseIP(gatewayIp)
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        gateway,
	}

	if err := netlink.RouteAdd(route); err != nil {
		return errors.Errorf("adding default route: %w", err)
	}

	slog.InfoContext(ctx, "configured default route", "gateway", gatewayIp)

	return nil
}
