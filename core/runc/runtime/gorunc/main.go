//go:build !windows

package goruncruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go/features"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/runtime/gorunc/reaper"
	"github.com/walteh/runm/pkg/logging"
)

var _ runtime.Runtime = (*GoRuncRuntime)(nil)
var _ runtime.RuntimeExtras = (*GoRuncRuntime)(nil)

type GoRuncRuntime struct {
	internal          *gorunc.Runc
	reaperCh          chan gorunc.Exit
	reaperSubscribers map[chan gorunc.Exit]struct{}
	reaperChanMutex   sync.Mutex
	// sharedDirPathPrefix string
}

func WrapdGoRuncRuntime(rt *gorunc.Runc) *GoRuncRuntime {
	r := &GoRuncRuntime{
		internal:          rt,
		reaperCh:          make(chan gorunc.Exit, 32),
		reaperSubscribers: make(map[chan gorunc.Exit]struct{}),
		reaperChanMutex:   sync.Mutex{},
	}
	go func() {
		for exit := range r.reaperCh {
			r.reaperChanMutex.Lock()
			defer r.reaperChanMutex.Unlock()
			for subscriber := range r.reaperSubscribers {
				go func() {
					subscriber <- exit
				}()
			}
		}
	}()
	return r
}

var initOnce sync.Once

// In runm/core/runc/runtime/gorunc/main.go, add near the init function:

type MonitorWithSubscribers interface {
	gorunc.ProcessMonitor
	Subscribe(ctx context.Context) chan gorunc.Exit
	Unsubscribe(ctx context.Context, ch chan gorunc.Exit)
}

var customMonitor MonitorWithSubscribers

func (r *GoRuncRuntime) SubscribeToReaperExits2(ctx context.Context) (<-chan gorunc.Exit, error) {
	ch := make(chan gorunc.Exit, 1)
	r.reaperChanMutex.Lock()
	defer r.reaperChanMutex.Unlock()
	r.reaperSubscribers[ch] = struct{}{}
	return ch, nil
}

func init() {
	// gorunc.Monitor = defaultMonitorInstance
	gorunc.Monitor = reaper.Default

}

func (r *GoRuncRuntime) SubscribeToReaperExits(ctx context.Context) (<-chan gorunc.Exit, error) {
	slog.InfoContext(ctx, "subscribing to custom monitor exits")

	defaultMonitorInstance.forwardTo = append(defaultMonitorInstance.forwardTo, r.reaperCh)

	if gorunc.Monitor == reaper.Default {
		return reaper.Default.Subscribe(), nil
	}

	return r.reaperCh, nil
}

type wrappedConsoleSocket struct {
	*gorunc.Socket
	unixConn *net.UnixConn
}

func (w *wrappedConsoleSocket) UnixConn() *net.UnixConn {
	return w.unixConn
}

func NewWrappedConsoleSocket(ctx context.Context) (runtime.ConsoleSocket, error) {
	sock, err := gorunc.NewTempConsoleSocket()
	if err != nil {
		return nil, errors.Errorf("creating temp console socket: %w", err)
	}

	unixConn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock.Path(), Net: "unix"})
	if err != nil {
		return nil, errors.Errorf("dialing unix socket: %w", err)
	}

	return &wrappedConsoleSocket{
		Socket:   sock,
		unixConn: unixConn,
	}, nil
}

func (r *GoRuncRuntime) NewTempConsoleSocket(ctx context.Context) (runtime.ConsoleSocket, error) {
	return NewWrappedConsoleSocket(ctx)
}

func (r *GoRuncRuntime) NewNullIO() (runtime.IO, error) {
	return gorunc.NewNullIO()
}

func (r *GoRuncRuntime) NewPipeIO(ctx context.Context, cioUID, ioGID int, opts ...gorunc.IOOpt) (runtime.IO, error) {
	return gorunc.NewPipeIO(cioUID, ioGID, opts...)
}

func (r *GoRuncRuntime) ReadPidFile(ctx context.Context, path string) (int, error) {
	return gorunc.ReadPidFile(path)
}

func (r *GoRuncRuntime) RuncRun(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) (int, error) {
	return r.internal.Run(ctx, id, bundle, options)
}

var _ runtime.RuntimeCreator = (*GoRuncRuntimeCreator)(nil)

type GoRuncRuntimeCreator struct {
}

// Features implements runtime.RuntimeCreator.
func (c *GoRuncRuntimeCreator) Features(ctx context.Context) (*features.Features, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, gorunc.DefaultCommand, "features")
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute %v: %w (stderr: %q)", cmd.Args, err, stderr.String())
	}
	var feat features.Features
	if err := json.Unmarshal(stdout, &feat); err != nil {
		return nil, err
	}
	return &feat, nil
}

// todo the move here might be to add the exitting
// Create ionterface to the above type (not creator)
// and so we can wrao stdio from sockets for logging purposes
// ie redirect a copy to stdiout

func (c *GoRuncRuntimeCreator) Create(ctx context.Context, opts *runtime.RuntimeOptions) (runtime.Runtime, error) {
	r := WrapdGoRuncRuntime(&gorunc.Runc{
		Command:      opts.ProcessCreateConfig.Runtime,
		Log:          filepath.Join(opts.ProcessCreateConfig.Bundle, runtime.LogFileBase),
		LogFormat:    gorunc.JSON,
		PdeathSignal: unix.SIGKILL,
		Root:         filepath.Join(opts.ProcessCreateConfig.Options.Root, opts.Namespace),
		// SystemdCgroup: opts.ProcessCreateConfig.Options.SystemdCgroup,
		SystemdCgroup: false,
	})
	return r, nil
}

func getRawRuncError(ctx context.Context, r *GoRuncRuntime, err error) error {
	rawStr := strings.TrimSpace(err.Error())
	errMsg := r.TryGetLastRuntimeError(ctx)
	if errMsg == "" {
		return errors.ErrorfOffset(2, "[raw runc error]: %s: [no runc error found in logs]", rawStr)
	}
	if strings.HasSuffix(rawStr, errMsg) {
		return errors.ErrorfOffset(2, "[raw runc error]: %s", rawStr)
	}
	return errors.ErrorfOffset(2, "[parsed runc error]: %s: [from logs] %s", rawStr, errMsg)
}

func WrapWithRuntimeError(ctx context.Context, r *GoRuncRuntime, f func() error) error {
	callerFuncName := logging.GetCurrentCallerURIOffset(1)
	spl := strings.Split(filepath.Base(callerFuncName.Function), ".")
	funcName := spl[len(spl)-1]

	slog.Debug(fmt.Sprintf("GORUNC:START[%s]", funcName))
	defer slog.Debug(fmt.Sprintf("GORUNC:END[%s]", funcName))
	err := f()
	if err == nil {
		return nil
	}
	return getRawRuncError(ctx, r, err)
}

func WrapWithRuntimeErrorResult[T any](ctx context.Context, r *GoRuncRuntime, f func() (T, error)) (T, error) {
	var zero T
	callerFuncName := logging.GetCurrentCallerURIOffset(1)
	slog.Debug(fmt.Sprintf("GORUNC:START[%s]", filepath.Base(callerFuncName.Function)))
	defer slog.Debug(fmt.Sprintf("GORUNC:END[%s]", filepath.Base(callerFuncName.Function)))
	result, err := f()
	if err == nil {
		return result, nil
	}
	return zero, getRawRuncError(ctx, r, err)
}

func (r *GoRuncRuntime) TryGetLastRuntimeError(ctx context.Context) string {
	f, err := os.OpenFile(r.internal.Log, os.O_RDONLY, 0400)
	if err != nil {
		slog.ErrorContext(ctx, "failed to open log file", "error", err, "log", r.internal.Log)
		return ""
	}
	defer f.Close()

	var (
		errMsg string
		log    struct {
			Level string
			Msg   string
			Time  time.Time
		}
	)

	dec := json.NewDecoder(f)
	for err = nil; err == nil; {
		if err = dec.Decode(&log); err != nil && err != io.EOF {
			slog.ErrorContext(ctx, "failed to decode log", "error", err)
			return ""
		}
		if log.Level == "error" {
			errMsg = strings.TrimSpace(log.Msg)
		}
	}

	return errMsg
}
