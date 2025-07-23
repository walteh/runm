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

// DO NOT USE SLOG IN THIS FUNCTION - LOG TO STDOUT
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
		taskgroup.WithLogStart(true),
		taskgroup.WithLogEnd(true),
		taskgroup.WithLogTaskStart(false),
		taskgroup.WithLogTaskEnd(false),
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
	if err := v.redirectSyslogToStdout(ctx); err != nil {
		slog.WarnContext(ctx, "failed to redirect syslog to stdout", "error", err)
		// Don't fail startup if syslog redirection fails
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

	// Signal readiness
	if err := v.signalReady(ctx); err != nil {
		return errors.Errorf("signaling ready: %w", err)
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

// redirectSyslogToStdout configures syslog to redirect to stdout for better log visibility
func (v *vmfuseInit) redirectSyslogToStdout(ctx context.Context) error {
	slog.InfoContext(ctx, "setting up syslog redirection to stdout")

	// Start rsyslog with a configuration that forwards everything to stdout
	// This creates a simple rsyslog.conf that sends all logs to stdout via a program
	rsyslogConf := `/etc/rsyslog.conf`
	confContent := `# Forward all syslog messages to stdout
$ModLoad imuxsock # provides support for local system logging
$ModLoad imklog   # provides kernel logging support

# Send all messages to stdout via logger command
*.* @@127.0.0.1:514

# Also create a rule to pipe to stdout directly
$template StdoutFormat,"%timegenerated% %HOSTNAME% %syslogtag%%msg:::sp-if-no-1st-sp%%msg:::drop-last-lf%\n"
*.* |/bin/sh -c 'cat >> /proc/1/fd/1'
`

	// Write rsyslog configuration
	if err := os.WriteFile(rsyslogConf, []byte(confContent), 0644); err != nil {
		return errors.Errorf("writing rsyslog config: %w", err)
	}

	slog.InfoContext(ctx, "created rsyslog configuration for stdout redirection")

	// Try to start rsyslog in the background - don't fail if it doesn't work
	go func() {
		cmd := exec.CommandContext(ctx, "rsyslogd", "-n", "-f", rsyslogConf)
		if err := cmd.Run(); err != nil {
			slog.WarnContext(ctx, "rsyslog failed to start", "error", err)
		}
	}()

	// Also redirect /dev/log to stdout if possible
	if err := v.redirectDevLogToStdout(ctx); err != nil {
		slog.WarnContext(ctx, "failed to redirect /dev/log", "error", err)
	}

	return nil
}

// redirectDevLogToStdout creates a simple log forwarder for /dev/log
func (v *vmfuseInit) redirectDevLogToStdout(ctx context.Context) error {
	// Create a simple forwarder that reads from /dev/log and writes to stdout
	// This is a best-effort approach
	logSocket := "/dev/log"

	// Remove existing socket if it exists
	_ = os.Remove(logSocket)

	// Create a Unix domain socket listener
	listener, err := net.Listen("unix", logSocket)
	if err != nil {
		return errors.Errorf("creating log socket listener: %w", err)
	}

	slog.InfoContext(ctx, "created /dev/log socket for stdout redirection")

	// Start accepting connections in the background
	go func() {
		defer listener.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					continue
				}

				// Handle each connection in a separate goroutine
				go v.handleLogConnection(ctx, conn)
			}
		}
	}()

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

func (v *vmfuseInit) createGaneshaDirectories(ctx context.Context) error {

	// untar the ganesha plugins
	if err := os.MkdirAll("/ganesha-plugins", 0755); err != nil {
		return errors.Errorf("creating ganesha plugins directory: %w", err)
	}
	if err := ExecCmdForwardingStdio(ctx, "tar", "-xzf", "/mbin/ganesha-plugins.tar.gz", "-C", "/ganesha-plugins"); err != nil {
		return errors.Errorf("untaring ganesha plugins: %w", err)
	}

	// ls the plugins
	if err := ExecCmdForwardingStdio(ctx, "ls", "-lahrs", "/ganesha-plugins/ganesha-plugins"); err != nil {
		return errors.Errorf("listing ganesha plugins: %w", err)
	}

	// make the /usr/lib/ganesha directory
	if err := os.MkdirAll("/usr/lib/ganesha", 0755); err != nil {
		return errors.Errorf("creating ganesha directory: %w", err)
	}

	// symlink the /usr/lib/ganesha to /ganesha-plugins
	if err := os.Symlink("/ganesha-plugins/ganesha-plugins/libfsalvfs.so", "/usr/lib/ganesha/libfsalvfs.so"); err != nil {
		return errors.Errorf("symlinking ganesha plugins: %w", err)
	}

	// untar the ganesha plugins
	// Create Ganesha configuration directory
	if err := os.MkdirAll("/etc/ganesha", 0755); err != nil {
		return errors.Errorf("creating ganesha config directory: %w", err)
	}

	// Create NFS recovery directories
	recoveryDirs := []string{
		"/var/lib/nfs/ganesha",
		"/var/lib/nfs/ganesha/v4recov",
		"/var/lib/nfs/ganesha/v4old",
		"/var/lib/nfs/ganesha/v4recov/node0",
		"/var/lib/nfs/ganesha/v4old/node0",
	}

	for _, dir := range recoveryDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errors.Errorf("creating recovery directory %s: %w", dir, err)
		}
	}

	// Create basic /etc/netconfig for UDP support
	netconfig := `udp        tpi_clts      v     inet     udp     -       -
tcp        tpi_cots_ord  v     inet     tcp     -       -
udp6       tpi_clts      v     inet6    udp     -       -
tcp6       tpi_cots_ord  v     inet6    tcp     -       -
rawip      tpi_raw       -     inet      -      -       -
local      tpi_cots_ord  -     loopback  -      -       -
unix       tpi_cots_ord  -     loopback  -      -       -
`
	if err := os.WriteFile("/etc/netconfig", []byte(netconfig), 0644); err != nil {
		return errors.Errorf("creating netconfig file: %w", err)
	}

	slog.InfoContext(ctx, "created Ganesha directories and files",
		"recovery_dirs", len(recoveryDirs),
		"netconfig", "/etc/netconfig")

	return nil
}

func (v *vmfuseInit) setupGaneshaNFS(ctx context.Context) error {
	// Create all necessary directories for Ganesha
	if err := v.createGaneshaDirectories(ctx); err != nil {
		return errors.Errorf("creating ganesha directories: %w", err)
	}

	// Create Ganesha configuration file with only supported parameters
	ganeshaConfig := fmt.Sprintf(`NFS_Core_Param {
	NFS_Protocols = 4;
	NFS_Port = 2049;
	MNT_Port = 20048;
	NLM_Port = 32803;
	RQUOTA_Port = 875;
	Enable_NLM = false;
	Enable_RQUOTA = false;
	NSM_Use_Caller_Name = true;
}

NFS_IP_Name {
	Index_Size = 17;
	Expiration_Time = 3600;
}



EXPORT {
	Export_Id = 1;
	Path = "%s";
	Pseudo = "/";
	Access_Type = RW;
	Squash = No_Root_Squash;
	Protocols = 4;
	Transports = TCP;
	FSAL {
		Name = VFS;
	}
}
`, mountTarget)

	if err := os.WriteFile("/etc/ganesha/ganesha.conf", []byte(ganeshaConfig), 0644); err != nil {
		return errors.Errorf("writing ganesha config: %w", err)
	}

	slog.InfoContext(ctx, "created Ganesha config", "path", "/etc/ganesha/ganesha.conf", "export_path", mountTarget)

	// Set up network interfaces
	if err := ExecCmdForwardingStdio(ctx, "ip", "link", "set", "lo", "up"); err != nil {
		return errors.Errorf("setting up loopback: %w", err)
	}

	// Log active ports for debugging
	if err := v.logActivePorts(ctx); err != nil {
		slog.WarnContext(ctx, "failed to log active ports", "error", err)
	}

	// Start Ganesha NFS server in background with extensive debugging
	go func() {
		// First, verify the Ganesha binary exists and is executable
		if err := v.verifyGaneshaBinary(ctx); err != nil {
			slog.ErrorContext(ctx, "Ganesha binary verification failed", "error", err)
			return
		}

		// Log configuration contents before starting
		if err := v.logGaneshaConfig(ctx); err != nil {
			slog.WarnContext(ctx, "failed to log Ganesha config", "error", err)
		}

		// First try to understand what arguments Ganesha accepts
		slog.InfoContext(ctx, "checking Ganesha usage before starting...")
		if err := ExecCmdForwardingStdio(ctx, "/mbin/ganesha", "-h"); err != nil {
			slog.WarnContext(ctx, "could not get Ganesha usage", "error", err)
		}

		// // Try to validate the configuration
		// slog.InfoContext(ctx, "validating Ganesha configuration...")
		// if err := ExecCmdForwardingStdio(ctx, "/mbin/ganesha", "-f", "/etc/ganesha/ganesha.conf", "-t"); err != nil {
		// 	slog.WarnContext(ctx, "Ganesha config validation failed (or -t flag not supported)", "error", err)
		// }

		// Try to start Ganesha with detailed error reporting
		slog.InfoContext(ctx, "starting Ganesha NFS server",
			"binary", "/mbin/ganesha",
			"config", "/etc/ganesha/ganesha.conf")

		if err := v.executeGaneshaWithDetails(ctx); err != nil {
			slog.ErrorContext(ctx, "Ganesha NFS server failed", "error", err)

			// Try alternative arguments
			slog.InfoContext(ctx, "trying alternative Ganesha arguments...")
			if err2 := v.executeGaneshaAlternative(ctx); err2 != nil {
				slog.ErrorContext(ctx, "Ganesha alternative execution also failed", "error", err2)
			}

			// Try to get more info about why it failed
			v.diagnoseGaneshaFailure(ctx)
		} else {
			slog.InfoContext(ctx, "Ganesha NFS server exited cleanly")
		}

		// Log active ports after Ganesha attempt
		if err := v.logActivePorts(ctx); err != nil {
			slog.WarnContext(ctx, "failed to log active ports after Ganesha exit", "error", err)
		}
	}()

	// Wait for Ganesha to be ready by checking if port 2049 is listening
	if err := v.waitForGaneshaReady(ctx); err != nil {
		return errors.Errorf("waiting for Ganesha readiness: %w", err)
	}

	// Log active ports for debugging
	if err := v.logActivePorts(ctx); err != nil {
		slog.WarnContext(ctx, "failed to log active ports", "error", err)
	}

	// Specifically verify NFS-related ports
	v.verifyNFSPorts(ctx)

	slog.InfoContext(ctx, "Ganesha NFS server started", "export_path", mountTarget, "config", "/etc/ganesha/ganesha.conf")

	return nil
}

func (v *vmfuseInit) waitForGaneshaReady(ctx context.Context) error {
	// Wait for Ganesha to be ready by checking if port 2049 is listening
	for i := range 30 { // Try for up to 30 seconds
		conn, err := net.DialTimeout("tcp", "localhost:2049", 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			slog.InfoContext(ctx, "Ganesha NFS server is ready", "attempts", i+1)
			return nil
		} else {
			slog.DebugContext(ctx, "Ganesha NFS server is not ready", "error", err, "attempts", i+1)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
			continue
		}
	}

	return errors.Errorf("Ganesha NFS server did not become ready within 30 seconds")
}

func (v *vmfuseInit) logActivePorts(ctx context.Context) error {
	// Read /proc/net/tcp to get active listening ports
	tcpData, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return errors.Errorf("reading /proc/net/tcp: %w", err)
	}

	// Read /proc/net/tcp6 for IPv6 ports
	tcp6Data, err := os.ReadFile("/proc/net/tcp6")
	if err != nil {
		slog.DebugContext(ctx, "could not read /proc/net/tcp6", "error", err)
		tcp6Data = nil
	}

	// Parse listening ports
	listeningPorts := v.parseListeningPorts(string(tcpData), string(tcp6Data))

	if len(listeningPorts) > 0 {
		slog.InfoContext(ctx, "active listening ports", "ports", listeningPorts)
	} else {
		slog.WarnContext(ctx, "no listening ports found")
	}

	// Also try using netstat if available for comparison
	if err := v.logNetstatInfo(ctx); err != nil {
		slog.DebugContext(ctx, "netstat not available or failed", "error", err)
	}

	return nil
}

func (v *vmfuseInit) parseListeningPorts(tcp, tcp6 string) []string {
	var ports []string

	// Parse IPv4 TCP ports
	ports = append(ports, v.parsePortsFromProcNet(tcp, "tcp4")...)

	// Parse IPv6 TCP ports if available
	if tcp6 != "" {
		ports = append(ports, v.parsePortsFromProcNet(tcp6, "tcp6")...)
	}

	return ports
}

func (v *vmfuseInit) parsePortsFromProcNet(data, protocol string) []string {
	var ports []string
	lines := strings.Split(data, "\n")

	for i, line := range lines {
		if i == 0 { // Skip header
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		localAddr := fields[1]
		state := fields[3]

		// State 0A = LISTEN (listening)
		if state == "0A" {
			parts := strings.Split(localAddr, ":")
			if len(parts) == 2 {
				// Convert hex port to decimal
				if port, err := parseHexPort(parts[1]); err == nil {
					ports = append(ports, fmt.Sprintf("%s:%d", protocol, port))
				}
			}
		}
	}

	return ports
}

func parseHexPort(hexPort string) (int, error) {
	port := int64(0)
	for _, char := range hexPort {
		port *= 16
		if char >= '0' && char <= '9' {
			port += int64(char - '0')
		} else if char >= 'A' && char <= 'F' {
			port += int64(char - 'A' + 10)
		} else if char >= 'a' && char <= 'f' {
			port += int64(char - 'a' + 10)
		} else {
			return 0, errors.Errorf("invalid hex character: %c", char)
		}
	}
	return int(port), nil
}

func (v *vmfuseInit) logNetstatInfo(ctx context.Context) error {
	// Try to use ss first (more modern and commonly available)
	// if err := ExecCmdForwardingStdio(ctx, "ss", "-tlnp"); err == nil {
	// 	slog.DebugContext(ctx, "ss output logged via ExecCmdForwardingStdio")
	// 	return nil
	// }

	// Fallback to netstat if ss is not available
	if err := ExecCmdForwardingStdio(ctx, "netstat", "-tlnp"); err == nil {
		slog.DebugContext(ctx, "netstat output logged via ExecCmdForwardingStdio")
		return nil
	}

	return errors.Errorf("neither ss nor netstat available")
}

func (v *vmfuseInit) verifyGaneshaBinary(ctx context.Context) error {
	binaryPath := "/mbin/ganesha"

	// Check if file exists
	info, err := os.Stat(binaryPath)
	if err != nil {
		return errors.Errorf("Ganesha binary not found at %s: %w", binaryPath, err)
	}

	// Check if it's executable
	mode := info.Mode()
	if mode&0111 == 0 {
		return errors.Errorf("Ganesha binary is not executable: mode=%v", mode)
	}

	slog.InfoContext(ctx, "Ganesha binary verified",
		"path", binaryPath,
		"size", info.Size(),
		"mode", fmt.Sprintf("%o", mode))

	// Try to get help using the correct flag format
	if err := ExecCmdForwardingStdio(ctx, binaryPath, "-h"); err != nil {
		slog.WarnContext(ctx, "Ganesha binary may not be functional", "help_error", err)
	}

	// Try to get version information using -v
	if err := ExecCmdForwardingStdio(ctx, binaryPath, "-v"); err != nil {
		slog.WarnContext(ctx, "Ganesha version check failed", "version_error", err)
	}

	// Check for library dependencies using ldd if available
	// v.checkLibraryDependencies(ctx, binaryPath)

	return nil
}

func (v *vmfuseInit) logGaneshaConfig(ctx context.Context) error {
	configPath := "/etc/ganesha/ganesha.conf"

	config, err := os.ReadFile(configPath)
	if err != nil {
		return errors.Errorf("reading Ganesha config: %w", err)
	}

	slog.InfoContext(ctx, "Ganesha configuration",
		"path", configPath,
		"size", len(config),
		"content", string(config))

	return nil
}

func (v *vmfuseInit) diagnoseGaneshaFailure(ctx context.Context) {
	slog.InfoContext(ctx, "diagnosing Ganesha failure...")

	// Check if config file exists and is readable
	if _, err := os.ReadFile("/etc/ganesha/ganesha.conf"); err != nil {
		slog.ErrorContext(ctx, "config file issue", "error", err)
	}

	// Check if export path exists and is accessible
	if info, err := os.Stat("/mnt/target"); err != nil {
		slog.ErrorContext(ctx, "export path issue", "path", "/mnt/target", "error", err)
	} else {
		slog.InfoContext(ctx, "export path status", "path", "/mnt/target", "mode", info.Mode())
	}

	// Try running Ganesha with different flags to get more info
	slog.InfoContext(ctx, "trying Ganesha with debug flags...")
	if err := ExecCmdForwardingStdio(ctx, "/mbin/ganesha", "-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_DEBUG"); err != nil {
		slog.ErrorContext(ctx, "debug run also failed", "error", err)
	}

	// Check system capabilities that Ganesha might need
	v.checkSystemRequirements(ctx)
}

func (v *vmfuseInit) checkSystemRequirements(ctx context.Context) {
	// Check /proc/filesystems for NFS support
	if data, err := os.ReadFile("/proc/filesystems"); err == nil {
		if strings.Contains(string(data), "nfs") {
			slog.InfoContext(ctx, "NFS filesystem support detected")
		} else {
			slog.WarnContext(ctx, "NFS filesystem support not found in /proc/filesystems")
		}
	}

	// Check if we have the necessary directories
	dirs := []string{"/proc", "/sys", "/dev", "/tmp", "/var/run"}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil {
			slog.WarnContext(ctx, "required directory missing", "dir", dir, "error", err)
		} else if !info.IsDir() {
			slog.WarnContext(ctx, "required path is not a directory", "dir", dir)
		}
	}
}

func (v *vmfuseInit) checkLibraryDependencies(ctx context.Context, binaryPath string) {
	// Try ldd to check shared library dependencies
	if err := ExecCmdForwardingStdio(ctx, "ldd", binaryPath); err != nil {
		slog.DebugContext(ctx, "ldd check failed - might be static binary", "error", err)

		// Try file command to get binary info
		if err := ExecCmdForwardingStdio(ctx, "file", binaryPath); err != nil {
			slog.DebugContext(ctx, "file command also failed", "error", err)
		}

		// Try readelf if available for ELF info
		if err := ExecCmdForwardingStdio(ctx, "readelf", "-h", binaryPath); err != nil {
			slog.DebugContext(ctx, "readelf not available or failed", "error", err)
		}
	}
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

func (v *vmfuseInit) executeGaneshaWithDetails(ctx context.Context) error {
	// Custom execution of Ganesha with detailed error tracking and logging
	binary := "/mbin/ganesha"
	// Try different argument combinations - the StaticX version might need different args
	args := []string{"-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_INFO", "-F"} // -F = foreground mode

	slog.InfoContext(ctx, "executing Ganesha with detailed monitoring",
		"binary", binary,
		"fullCommand", fmt.Sprintf("%s %s", binary, strings.Join(args, " ")))

	cmd := exec.CommandContext(ctx, binary, args...)

	// Create buffers to capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set up environment
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH") + ":/mbin"}, os.Environ()...)

	// Record start time
	startTime := time.Now()

	// Run the command
	err := cmd.Run()
	duration := time.Since(startTime)

	// Log results with more detail
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}

	slog.Log(ctx, level, "Ganesha execution completed",
		"binary", binary,
		"args", args,
		"duration", duration,
		"stdout_size", stdout.Len(),
		"stderr_size", stderr.Len(),
		"stdout_content", stdout.String(),
		"stderr_content", stderr.String(),
		"error", err)

	// If there's any output, log it separately for visibility
	if stdout.Len() > 0 {
		slog.InfoContext(ctx, "Ganesha stdout output", "output", stdout.String())
	}

	if stderr.Len() > 0 {
		slog.WarnContext(ctx, "Ganesha stderr output", "output", stderr.String())
	}

	return err
}

func (v *vmfuseInit) executeGaneshaAlternative(ctx context.Context) error {
	// Try different argument combinations that might work
	alternatives := [][]string{
		{"-f", "/etc/ganesha/ganesha.conf"},                                             // Minimal args
		{"-f", "/etc/ganesha/ganesha.conf", "-d"},                                       // Debug mode
		{"-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_DEBUG"},                          // Debug logging
		{"-f", "/etc/ganesha/ganesha.conf", "-L", "/dev/stdout"},                        // Log to stdout only
		{"-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_INFO", "-L", "/tmp/ganesha.log"}, // Log to file
	}

	binary := "/mbin/ganesha"

	for i, args := range alternatives {
		slog.InfoContext(ctx, "trying Ganesha alternative",
			"attempt", i+1,
			"args", args)

		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH") + ":/mbin"}, os.Environ()...)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		startTime := time.Now()
		err := cmd.Run()
		duration := time.Since(startTime)

		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}

		slog.Log(ctx, level, "Ganesha alternative attempt result",
			"attempt", i+1,
			"args", args,
			"duration", duration,
			"stdout", stdout.String(),
			"stderr", stderr.String(),
			"error", err)

		// If this one worked (no error), return success
		if err == nil {
			slog.InfoContext(ctx, "Ganesha alternative succeeded", "args", args)
			return nil
		}

		// Short delay between attempts
		time.Sleep(100 * time.Millisecond)
	}

	return errors.Errorf("all Ganesha alternative executions failed")
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
