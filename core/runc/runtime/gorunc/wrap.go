//go:build !windows

package goruncruntime

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"syscall"
	"time"

	gorunc "github.com/containerd/go-runc"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"
)

func (r *GoRuncRuntime) reaper(ctx context.Context, pidfilechan chan string, pidfile string, name string) error {
	pidint := -1
	done := false
	defer func() {
		done = true
		slog.InfoContext(ctx, "done runc - "+name, "pid", pidint)
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if done {
				return
			}
			slog.InfoContext(ctx, "still running runc - "+name, "pid", pidint)
		}
	}()

	slog.InfoContext(ctx, "watching for pid file changes", "path", pidfile)

	<-pidfilechan

	fle, err := os.Open(pidfile)
	if err != nil {
		return errors.Errorf("failed to open pid file: %w", err)
	}
	defer fle.Close()

	pid, err := io.ReadAll(fle)
	if err != nil {
		return errors.Errorf("failed to read pid file: %w", err)
	}

	pidint, err = strconv.Atoi(string(pid))
	if err != nil {
		return errors.Errorf("failed to convert pid to int: %w", err)
	}

	sys, err := os.FindProcess(pidint)
	if err != nil {
		return errors.Errorf("failed to find process: %w", err)
	}
	slog.InfoContext(ctx, "found process", "pid", pid)
	waiter, err := sys.Wait()
	if err != nil {
		return errors.Errorf("failed to wait on pid: %w", err)
	}
	slog.InfoContext(ctx, "waiter done", "waiter", waiter)

	r.reaperCh <- gorunc.Exit{
		Timestamp: time.Now(),
		Pid:       pidint,
		Status:    int(waiter.Sys().(syscall.WaitStatus).ExitStatus()),
	}

	return nil
}

func (r *GoRuncRuntime) Create(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) error {
	// slog.Info(godump.DumpStr(options), "id", id, "bundle", bundle, "options", options)

	// output, err := exec.CommandContext(ctx, "/bin/busybox", "ls", "-lah", bundle).CombinedOutput()
	// if err != nil {
	// 	slog.ErrorContext(ctx, "failed to list bundle", "error", err, "output", string(output))
	// 	return errors.Errorf("failed to list bundle: %w", err)
	// }
	// slog.InfoContext(ctx, "ls -lahr: "+string(output), "bundle", bundle)

	// output, err = exec.CommandContext(ctx, "/bin/busybox", "ls", "-lah", filepath.Join(bundle, "rootfs")).CombinedOutput()
	// if err != nil {
	// 	slog.ErrorContext(ctx, "failed to list bundle", "error", err, "output", string(output))
	// 	return errors.Errorf("failed to list bundle: %w", err)
	// }
	// slog.InfoContext(ctx, "ls -lah: "+string(output), "bundle", bundle)

	reaperChan := make(chan string, 1)
	defer close(reaperChan)
	go r.reaper(ctx, reaperChan, options.PidFile, "create")

	options.NoNewKeyring = true

	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Create(ctx, id, bundle, options)
	})
}

func (r *GoRuncRuntime) Checkpoint(ctx context.Context, id string, opts *gorunc.CheckpointOpts, actions ...gorunc.CheckpointAction) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Checkpoint(ctx, id, opts, actions...)
	})
}

// Delete implements runtime.Runtime.
func (r *GoRuncRuntime) Delete(ctx context.Context, id string, opts *gorunc.DeleteOpts) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Delete(ctx, id, opts)
	})
}

// Exec implements runtime.Runtime.
func (r *GoRuncRuntime) Exec(ctx context.Context, id string, spec specs.Process, opts *gorunc.ExecOpts) error {
	reaperChan := make(chan string, 1)
	defer close(reaperChan)
	go r.reaper(ctx, reaperChan, opts.PidFile, "exec")
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Exec(ctx, id, spec, opts)
	})
}

// Kill implements runtime.Runtime.
func (r *GoRuncRuntime) Kill(ctx context.Context, id string, signal int, opts *gorunc.KillOpts) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		slog.InfoContext(ctx, "KILLING RUNC", "id", id, "signal", signal, "opts.all", opts.All)
		return r.internal.Kill(ctx, id, signal, opts)
	})
}

// Pause implements runtime.Runtime.
func (r *GoRuncRuntime) Pause(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Pause(ctx, id)
	})
}

// Ps implements runtime.Runtime.
func (r *GoRuncRuntime) Ps(ctx context.Context, id string) ([]int, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() ([]int, error) {
		return r.internal.Ps(ctx, id)
	})
}

// Restore implements runtime.Runtime.
func (r *GoRuncRuntime) Restore(ctx context.Context, id string, bundle string, opts *gorunc.RestoreOpts) (int, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (int, error) {
		return r.internal.Restore(ctx, id, bundle, opts)
	})
}

// Resume implements runtime.Runtime.
func (r *GoRuncRuntime) Resume(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Resume(ctx, id)
	})
}

// Update implements runtime.Runtime.
func (r *GoRuncRuntime) Update(ctx context.Context, id string, resources *specs.LinuxResources) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Update(ctx, id, resources)
	})
}

// start
func (r *GoRuncRuntime) Start(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Start(ctx, id)
	})
}

// Events implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Events(ctx context.Context, id string, timeout time.Duration) (chan *gorunc.Event, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (chan *gorunc.Event, error) {
		return r.internal.Events(ctx, id, timeout)
	})
}

// List implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) List(ctx context.Context) ([]*gorunc.Container, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() ([]*gorunc.Container, error) {
		return r.internal.List(ctx)
	})
}

// State implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) State(ctx context.Context, id string) (*gorunc.Container, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.Container, error) {
		return r.internal.State(ctx, id)
	})
}

// Stats implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Stats(ctx context.Context, id string) (*gorunc.Stats, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.Stats, error) {
		return r.internal.Stats(ctx, id)
	})
}

// Top implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Top(ctx context.Context, a string, b string) (*gorunc.TopResults, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.TopResults, error) {
		return r.internal.Top(ctx, a, b)
	})
}

// Version implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Version(ctx context.Context) (gorunc.Version, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (gorunc.Version, error) {
		return r.internal.Version(ctx)
	})
}
