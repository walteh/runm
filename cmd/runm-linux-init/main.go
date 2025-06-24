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
	"time"

	"github.com/mdlayher/vsock"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/runc/runtime"
	goruncruntime "github.com/walteh/runm/core/runc/runtime/gorunc"
	"github.com/walteh/runm/core/runc/server"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"

	"github.com/containerd/containerd/v2/pkg/oci"
	gorunc "github.com/containerd/go-runc"

	runtimemock "github.com/walteh/runm/gen/mocks/core/runc/runtime"
)

var (
	containerId string
	runmMode    string
)

func init() {
	flag.StringVar(&containerId, "container-id", "", "the container id")
	flag.StringVar(&runmMode, "runm-mode", "", "the runm mode")
	flag.Parse()
}

func main() {

	pid := os.Getpid()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logging.NewDefaultDevLogger("runm-linux-init", os.Stdout)

	ctx = slogctx.NewCtx(ctx, logger)

	ctx = slogctx.Append(ctx, slog.Int("pid", pid))

	err := recoveryMain(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "error in main", "error", err)
		os.Exit(1)
	}
}

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
		err := runGrpcVsockServer(ctx)
		errChan <- err
	}()

	return <-errChan
}

func runGrpcVsockServer(ctx context.Context) error {

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		for tick := range ticker.C {
			if ctx.Err() != nil {
				return
			}
			slog.InfoContext(ctx, "still running in rootfs, waiting to be killed", "tick", tick)
		}
	}()

	if containerId == "" {
		return errors.Errorf("container-id flag is required")
	}

	// wrkDir := constants.Ec1AbsPath

	// ls -la /mbin/runc

	// fmt.Println("YOOOOO ls -la /mbin")

	// ExecCmdForwardingStdio(ctx, "ls", "-la", "/mbin")

	realRuntime := goruncruntime.WrapdGoRuncRuntime(&gorunc.Runc{
		Command:      "/mbin/runc",
		Log:          filepath.Join(constants.Ec1AbsPath, runtime.LogFileBase),
		LogFormat:    gorunc.JSON,
		PdeathSignal: unix.SIGKILL,
		Debug:        true,

		// Root:          filepath.Join(opts.ProcessCreateConfig.Options.Root, opts.Namespace),
		Root:          constants.NewRootAbsPath,
		SystemdCgroup: false,
	})

	realSocketAllocator := runtime.NewGuestVsockSocketAllocator(3, 2300)

	cgroupAdapter, err := goruncruntime.NewCgroupV2Adapter(ctx, containerId)
	if err != nil {
		return errors.Errorf("failed to create cgroup adapter: %w", err)
	}

	var mockRuntimeExtras = &runtimemock.MockRuntimeExtras{}

	grpcVsockServer := grpc.NewServer(grpc.UnaryInterceptor(grpcerr.UnaryServerInterceptor()))

	realEventHandler := goruncruntime.NewGoRuncEventHandler()

	serverz := server.NewServer(
		realRuntime,
		mockRuntimeExtras,
		realSocketAllocator,
		realEventHandler,
		cgroupAdapter,
	)

	serverz.RegisterGrpcServer(grpcVsockServer)

	slog.InfoContext(ctx, "listening on vsock", "port", constants.RunmVsockPort)

	listener, err := vsock.ListenContextID(3, uint32(constants.RunmVsockPort), nil)
	if err != nil {
		slog.ErrorContext(ctx, "problem listening vsock", "error", err)
		return errors.Errorf("problem listening vsock: %w", err)
	}

	egroup := errgroup.Group{}

	egroup.Go(func() error {
		slog.InfoContext(ctx, "serving grpc vsock server", "port", constants.RunmVsockPort)
		if err := grpcVsockServer.Serve(listener); err != nil {
			return errors.Errorf("problem serving grpc vsock server: %w", err)
		}
		return nil
	})

	return egroup.Wait()
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

func ExecCmdForwardingStdio(ctx context.Context, cmds ...string) error {
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
