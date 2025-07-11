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
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/mdlayher/vsock"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"

	gorunc "github.com/containerd/go-runc"
	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/runtime/gorunc/reaper"
	"github.com/walteh/runm/core/runc/server"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/taskgroup"
	"github.com/walteh/runm/pkg/ticker"

	goruncruntime "github.com/walteh/runm/core/runc/runtime/gorunc"
	runtimemock "github.com/walteh/runm/gen/mocks/core/runc/runtime"
)

var (
	containerId  string
	runmMode     string
	bundleSource string
	mbinds       string
	enableOtel   bool
	timezone     string
)

const (
	serviceName = "runm[init]"
)

func init() {
	goruntime.GOMAXPROCS(goruntime.NumCPU())

	fmt.Println("initializing runm-linux-init")
	fmt.Println("args", os.Args)
	flag.StringVar(&containerId, "container-id", "", "the container id")
	flag.StringVar(&runmMode, "runm-mode", "", "the runm mode")
	flag.StringVar(&bundleSource, "bundle-source", "", "the bundle source")
	flag.BoolVar(&enableOtel, "enable-otlp", false, "enable otlp")
	flag.StringVar(&mbinds, "mbinds", "", "the mbinds") // this errors for some reason
	flag.StringVar(&timezone, "timezone", "", "the timezone")
	flag.Parse()

}

type runmLinuxInit struct {
	rawWriter  io.WriteCloser
	logWriter  io.WriteCloser
	otelWriter io.WriteCloser

	exitChan chan gorunc.Exit

	logger    *slog.Logger
	taskgroup *taskgroup.TaskGroup
	cancel    context.CancelFunc
}

// DO NOT USE SLOG IN THIS FUNCTION - LOG TO STDOUT
func (r *runmLinuxInit) setupLogger(ctx context.Context) (context.Context, error) {
	var err error

	fmt.Println("linux-runm-init: setting up logging - all future logs will be sent to vsock (pid: ", os.Getpid(), ")")

	rawWriterConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		return nil, errors.Errorf("problem dialing vsock for raw writer: %w", err)
	}

	delimitedLogProxyConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		return nil, errors.Errorf("problem dialing vsock for log proxy: %w", err)
	}

	opts := []logging.LoggerOpt{
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
		logger = logging.NewDefaultDevLogger(serviceName, delimitedLogProxyConn, opts...)
	}

	r.rawWriter = rawWriterConn
	r.logWriter = delimitedLogProxyConn
	r.logger = logger

	return slogctx.NewCtx(ctx, logger), nil
}

func main() {

	pid := os.Getpid()

	// reap.SetSubreaper(os.Getpid())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runmLinuxInit := &runmLinuxInit{}

	ctx, err := runmLinuxInit.setupLogger(ctx)
	if err != nil {
		fmt.Printf("failed to setup logger: %v\n", err)
		os.Exit(1)
	}

	ctx = slogctx.Append(ctx, slog.Int("pid", pid))

	err = recoveryMain(ctx, runmLinuxInit)
	if err != nil {
		slog.ErrorContext(ctx, "error in main", "error", err)
		os.Exit(1)
	}
}

func recoveryMain(ctx context.Context, r *runmLinuxInit) (err error) {
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
		err := r.run(ctx)
		errChan <- err
	}()

	return <-errChan
}

func (r *runmLinuxInit) configureRuntimeServer(ctx context.Context) (*grpc.Server, *server.Server, error) {
	namespace := "default"
	runcRoot := "/run/containerd/runc"

	realRuntime := goruncruntime.WrapdGoRuncRuntime(&gorunc.Runc{
		Command:       "/mbin/runc-test",
		Log:           filepath.Join(constants.Ec1AbsPath, runtime.LogFileBase),
		LogFormat:     gorunc.JSON,
		PdeathSignal:  unix.SIGKILL,
		Debug:         true,
		Root:          filepath.Join(runcRoot, namespace), // 		Root:         filepath.Join(opts.ProcessCreateConfig.Options.Root, opts.Namespace),
		SystemdCgroup: false,
	})

	serveropts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(grpcerr.NewUnaryServerInterceptor(ctx)),
		grpc.ChainStreamInterceptor(grpcerr.NewStreamServerInterceptor(ctx)),
	}

	if enableOtel {
		if logging.GetGlobalOtelInstances() != nil {
			serveropts = append(serveropts, logging.GetGlobalOtelInstances().GetGrpcServerOpts())
			defer logging.GetGlobalOtelInstances().Shutdown(ctx)
		} else {
			slog.WarnContext(ctx, "no otel instances found, not enabling otel")
		}
	}

	grpcVsockServer := grpc.NewServer(serveropts...)

	cgroupAdapter, err := goruncruntime.NewCgroupV2Adapter(ctx, containerId)
	if err != nil {
		return nil, nil, errors.Errorf("failed to create cgroup adapter: %w", err)
	}

	var mockRuntimeExtras = &runtimemock.MockRuntimeExtras{}

	realEventHandler := goruncruntime.NewGoRuncEventHandler()

	serverz := server.NewServer(
		realRuntime,
		mockRuntimeExtras,
		realEventHandler,
		cgroupAdapter,
		server.WithBundleSource(bundleSource),
		server.WithCleanupFn(func() error {
			if r.cancel != nil {
				r.cancel()
			}
			return nil
		}),
		// server.WithCustomExitChan(r.exitChan),
	)

	serverz.RegisterGrpcServer(grpcVsockServer)

	return grpcVsockServer, serverz, nil
}

func (r *runmLinuxInit) runGrpcVsockServer(ctx context.Context) error {
	slog.InfoContext(ctx, "listening on vsock", "port", constants.RunmGuestServerVsockPort)
	listener, err := vsock.ListenContextID(3, uint32(constants.RunmGuestServerVsockPort), nil)
	if err != nil {
		slog.ErrorContext(ctx, "problem listening vsock", "error", err)
		return errors.Errorf("problem listening vsock: %w", err)
	}

	grpcVsockServer, _, err := r.configureRuntimeServer(ctx)
	if err != nil {
		return errors.Errorf("problem configuring runtime server: %w", err)
	}

	if err := grpcVsockServer.Serve(listener); err != nil {
		return errors.Errorf("problem serving grpc vsock server: %w", err)
	}

	return nil
}

func (r *runmLinuxInit) runPprofVsockServer(ctx context.Context) error {
	slog.InfoContext(ctx, "starting pprof server on vsock", "port", constants.VsockPprofPort)

	listener, err := vsock.ListenContextID(3, uint32(constants.VsockPprofPort), nil)
	if err != nil {
		slog.ErrorContext(ctx, "problem listening vsock for pprof", "error", err)
		return errors.Errorf("problem listening vsock for pprof: %w", err)
	}

	server := &http.Server{
		Handler: http.DefaultServeMux,
	}

	if err := server.Serve(listener); err != nil {
		return errors.Errorf("problem serving pprof vsock server: %w", err)
	}

	return nil
}

func setupSignals() (chan os.Signal, error) {
	signals := make(chan os.Signal, 32)
	smp := []os.Signal{unix.SIGTERM, unix.SIGINT, unix.SIGPIPE}
	// if !config.NoReaper {
	smp = append(smp, unix.SIGCHLD)
	// }
	signal.Notify(signals, smp...)
	return signals, nil
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

func reapOnSignals(ctx context.Context, signals chan os.Signal) error {
	slog.InfoContext(ctx, "starting signal loop")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case s := <-signals:
			slog.InfoContext(ctx, "received signal", "signal", s)
			// Exit signals are handled separately from this loop
			// They get registered with this channel so that we can ignore such signals for short-running actions (e.g. `delete`)
			switch s {
			case unix.SIGCHLD:
				if err := reaper.Reap(); err != nil {
					slog.ErrorContext(ctx, "reap exit status", "error", err)
				}
			case unix.SIGPIPE:
			}
		}
	}
}

func (r *runmLinuxInit) run(ctx context.Context) error {

	if containerId == "" {
		return errors.Errorf("container-id flag is required")
	}

	signals, err := setupSignals()
	if err != nil {
		return errors.Errorf("problem setting up signals: %w", err)
	}

	defer ticker.NewTicker(
		ticker.WithMessage("RUNM:INIT[RUNNING]"),
		ticker.WithDoneMessage("RUNM:INIT[DONE]"),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
		ticker.WithFrequency(15),
		ticker.WithStartBurst(5),
		ticker.WithAttrFunc(func() []slog.Attr {
			return []slog.Attr{
				slog.Int("pid", os.Getpid()),
				slog.String("gomaxprocs", strconv.Itoa(goruntime.GOMAXPROCS(0))),
			}
		}),
	).RunAsDefer()()

	ctx, cancel := context.WithCancel(ctx)
	go handleExitSignals(ctx, cancel)

	r.cancel = cancel
	// Create TaskGroup with pprof enabled and custom labels
	taskgroupz := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("runm-linux-init"),
		taskgroup.WithEnablePprof(true),
		taskgroup.WithPprofLabels(map[string]string{
			"service":      serviceName,
			"container-id": containerId,
			"runm-mode":    runmMode,
		}),
		taskgroup.WithLogStart(true),
		taskgroup.WithLogEnd(true),
		taskgroup.WithLogTaskStart(false),
		taskgroup.WithLogTaskEnd(false),
		taskgroup.WithSlogBaseContext(ctx),
	)

	r.exitChan = make(chan gorunc.Exit, 32*100)

	taskgroupz.GoWithName("reaper", func(ctx context.Context) (err error) {
		return reapOnSignals(ctx, signals)
	})

	taskgroupz.GoWithName("psnotify", func(ctx context.Context) error {
		return r.runPsnotify(ctx, r.exitChan)
	})

	taskgroupz.GoWithName("delim-writer-unix-proxy", func(ctx context.Context) error {
		return r.runVsockUnixProxy(ctx, constants.DelimitedWriterProxyGuestUnixPath, r.logWriter)
	})

	taskgroupz.GoWithName("raw-writer-unix-proxy", func(ctx context.Context) error {
		return r.runVsockUnixProxy(ctx, constants.RawWriterProxyGuestUnixPath, r.rawWriter)
	})

	go func() {
		err := runProxyHooks(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "problem proxying hooks", "error", err)
		}
	}()

	if err := mount(ctx); err != nil {
		return errors.Errorf("problem mounting rootfs: %w", err)
	}

	if err := ExecCmdForwardingStdio(ctx, "ls", "-lahs", "/"); err != nil {
		return errors.Errorf("problem listing /: %w", err)
	}

	if err := ExecCmdForwardingStdio(ctx, "ls", "-lahs", filepath.Join(bundleSource, "rootfs", "/usr/local/bin/docker-entrypoint.sh")); err != nil {
		return errors.Errorf("problem listing bundleSource/rootfs: %w", err)
	}

	taskgroupz.GoWithName("grpc-vsock-server", func(ctx context.Context) error {
		return r.runGrpcVsockServer(ctx)
	})

	taskgroupz.GoWithName("pprof-vsock-server", func(ctx context.Context) error {
		return r.runPprofVsockServer(ctx)
	})

	r.taskgroup = taskgroupz

	// // Demonstrate pprof helper functionality
	// WrapTaskGroupGoWithLogging("pprof-demo", taskgroupz, func(ctx context.Context) error {
	// 	return r.demonstratePprofHelper(ctx, taskgroupz)
	// })

	return taskgroupz.Wait()
}

func runProxyHooks(ctx context.Context) error {
	spec, exists, err := loadSpec(ctx)
	if err != nil {
		return errors.Errorf("failed to load spec: %w", err)
	}

	if !exists {
		return errors.Errorf("spec does not exist")
	}

	hoooksToProxy := []specs.Hook{}

	hoooksToProxy = append(hoooksToProxy, spec.Hooks.Poststart...)
	hoooksToProxy = append(hoooksToProxy, spec.Hooks.Poststop...)
	hoooksToProxy = append(hoooksToProxy, spec.Hooks.CreateRuntime...)
	hoooksToProxy = append(hoooksToProxy, spec.Hooks.CreateContainer...)
	hoooksToProxy = append(hoooksToProxy, spec.Hooks.StartContainer...)

	createdSymlinks := make(map[string]bool)
	// for all the hooks, create symlinks to the host service
	for _, hook := range hoooksToProxy {
		if _, ok := createdSymlinks[hook.Path]; !ok {
			os.MkdirAll(filepath.Dir(hook.Path), 0755)
			os.Symlink("/mbin/runm-linux-host-fork-exec-proxy", hook.Path)
			createdSymlinks[hook.Path] = true
			slog.InfoContext(ctx, "created symlink", "path", hook.Path)
		}
	}

	// format and print out spec
	specBytes, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return errors.Errorf("failed to marshal spec: %w", err)
	}
	slog.InfoContext(ctx, "spec", "spec", string(specBytes))

	return nil
}

// demonstratePprofHelper shows how to use the taskgroup pprof helper functionality
func (r *runmLinuxInit) demonstratePprofHelper(ctx context.Context, tg *taskgroup.TaskGroup) error {
	helper := tg.GetPprofHelper()

	// Get current pprof labels
	currentLabels := helper.GetCurrentLabels(ctx)
	slog.InfoContext(ctx, "pprof demo - current labels", "labels", currentLabels)

	if taskID, ok := helper.GetTaskIDFromLabels(ctx); ok {
		slog.InfoContext(ctx, "pprof demo - task ID", "task_id", taskID)
	}

	if taskName, ok := helper.GetTaskNameFromLabels(ctx); ok {
		slog.InfoContext(ctx, "pprof demo - task name", "task_name", taskName)
	}

	// Demonstrate different task stages using WithAdditionalLabels
	helper.WithAdditionalLabels(ctx, map[string]string{
		"task_stage": "initializing",
	}, func(labeledCtx context.Context) {
		slog.InfoContext(ctx, "pprof demo - updated stage to initializing")
	})

	helper.WithAdditionalLabels(ctx, map[string]string{
		"task_stage": "processing",
	}, func(labeledCtx context.Context) {
		slog.InfoContext(ctx, "pprof demo - updated stage to processing")
	})

	// Use additional labels for a specific operation
	helper.WithAdditionalLabels(ctx, map[string]string{
		"operation":    "demo-operation",
		"demo-counter": "1",
	}, func(labeledCtx context.Context) {
		demoLabels := helper.GetCurrentLabels(labeledCtx)
		slog.InfoContext(ctx, "pprof demo - labels with additional context", "labels", demoLabels)
	})

	helper.WithAdditionalLabels(ctx, map[string]string{
		"task_stage": "completed",
	}, func(labeledCtx context.Context) {
		slog.InfoContext(ctx, "pprof demo - completed")
	})

	return nil
}

func (r *runmLinuxInit) runVsockUnixProxy(ctx context.Context, path string, writer io.WriteCloser) error {
	unixConnz, err := net.Listen("unix", path)
	if err != nil {
		return errors.Errorf("problem listening vsock for log proxy: %w", err)
	}

	defer unixConnz.Close()

	for {
		conn, err := unixConnz.Accept()
		if err != nil {
			slog.ErrorContext(ctx, "problem accepting log proxy connection", "error", err)
			return errors.Errorf("problem accepting log proxy connection: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		go func() {
			defer conn.Close()
			io.Copy(writer, conn)
		}()
	}

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
	if err != nil {
		return errors.Errorf("running busybox command (stdio was copied to the parent process): %v: %w", cmds, err)
	}

	slog.DebugContext(ctx, "finished running command '"+strings.Join(cmds, " ")+"'", "stdout", stdoutBuf.String(), "stderr", stderrBuf.String())

	return nil
}

func mount(ctx context.Context) error {
	if err := os.MkdirAll("/dev", 0755); err != nil {
		return errors.Errorf("problem creating /dev: %w", err)
	}

	if err := os.MkdirAll("/sys", 0755); err != nil {
		return errors.Errorf("problem creating /sys: %w", err)
	}

	if err := os.MkdirAll("/proc", 0755); err != nil {
		return errors.Errorf("problem creating /proc: %w", err)
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

	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "devtmpfs", "devtmpfs", "/dev"); err != nil {
		return errors.Errorf("problem mounting devtmpfs: %w", err)
	}

	// mount sysfs
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "sysfs", "sysfs", "/sys", "-o", "nosuid,noexec,nodev"); err != nil {
		return errors.Errorf("problem mounting sysfs: %w", err)
	}

	// Mount the unified cgroup v2 hierarchy
	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "cgroup2", "none", "/sys/fs/cgroup", "-o", "nsdelegate"); err != nil {
		return errors.Errorf("problem mounting cgroup2: %w", err)
	}

	// Enable the memory controller in the root cgroup
	if err := ExecCmdForwardingStdio(ctx, "sh", "-c", "echo +memory > /sys/fs/cgroup/cgroup.subtree_control"); err != nil {
		return errors.Errorf("failed to enable memory controller: %w", err)
	}

	// check that /etc/localtime is a symlink to /usr/share/zoneinfo/something
	localtime, err := os.Readlink("/etc/localtime")
	if err != nil {
		return errors.Errorf("problem reading localtime: %w", err)
	}
	if !strings.HasPrefix(localtime, "/usr/share/zoneinfo/") {
		return errors.Errorf("/etc/localtime is not a symlink to /usr/share/zoneinfo/[something]: %s", localtime)
	}

	// list mounnts
	procMounts, err := exec.CommandContext(ctx, "/bin/busybox", "cat", "/proc/mounts").CombinedOutput()
	if err != nil {
		return errors.Errorf("problem listing proc mounts: %w", err)
	}
	slog.InfoContext(ctx, "cat /proc/mounts: "+string(procMounts))

	return nil
}

type simpleVsockDialer struct {
	port uint32
}

func (d *simpleVsockDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	slog.InfoContext(ctx, "dialing vsock for otel", "port", d.port)
	c, err := vsock.Dial(2, d.port, nil)
	if err != nil {
		slog.ErrorContext(ctx, "problem dialing vsock for otel", "error", err)
		return nil, errors.Errorf("dialing vsock: %w", err)
	}
	slog.InfoContext(ctx, "dialed vsock for otel", "conn", c)
	return c, nil
}
