//go:build !windows

package goruncruntime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/walteh/runm/pkg/ticker"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"
)

func tryReadPidFile(ctx context.Context, pidfile string) (int, error) {
	pid, err := os.ReadFile(pidfile)
	if err != nil {
		return -1, errors.Errorf("failed to read pid file: %w", err)
	}
	return strconv.Atoi(string(pid))
}

func waitForFileToExistChan(ctx context.Context, pidfile string) chan error {
	pidfilechan := make(chan error, 1)
	go func() {
		for {
			if _, err := os.Stat(pidfile); err == nil {
				pidfilechan <- nil
				return
			} else {
				if errors.Is(err, os.ErrNotExist) {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				pidfilechan <- err
			}
		}
	}()
	return pidfilechan
}

func (r *GoRuncRuntime) reaperLoggingError(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int) {
	errchan := make(chan error, 1)
	defer close(errchan)
	go func() {
		errchan <- r.reaper(ctx, parentDoneChan, pidfile, name, pidchan)
	}()
	err := <-errchan
	if err != nil {
		slog.ErrorContext(ctx, "reaper error", "name", name, "pidfile", pidfile, "error", err)
	}
}

func (r *GoRuncRuntime) reaper(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int) error {
	pidint := -1

	slog.InfoContext(ctx, "starting reaper", "name", name, "pidfile", pidfile)

	tickd := ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(60),
		ticker.WithMessage(fmt.Sprintf("TICK:START:REAPER[%s]", name)),
		ticker.WithDoneMessage(fmt.Sprintf("TICK:END  :REAPER[%s]", name)),
		ticker.WithAttrFunc(func() []slog.Attr {
			return []slog.Attr{
				slog.Int("current_pid", pidint),
			}
		}),
	)

	var err error

	go tickd.Run(ctx)
	defer tickd.Stop()
	// this hacky logic probably broke the exit status code carryover with go tool task dev:2025-07-02:01
	if pidchan != nil {
		pidint = <-pidchan
	} else {
		pidint, err = tryReadPidFile(ctx, pidfile)
		if err != nil {
			slog.DebugContext(ctx, "failed to read pid file, waiting for it to be created", "path", pidfile, "error", err)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-waitForFileToExistChan(ctx, pidfile):
				if err != nil {
					return errors.Errorf("failed to wait for pid file to exist: %w", err)
				}
			}

			slog.DebugContext(ctx, "pid file ready, reading it", "path", pidfile)

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
		}
	}

	sys, err := os.FindProcess(pidint)
	if err != nil {
		return errors.Errorf("failed to find process: %w", err)
	}
	slog.InfoContext(ctx, "found process", "pid", pidint)

	waiter, err := sys.Wait()
	if err != nil {
		if errors.Is(err, syscall.ECHILD) {
			slog.InfoContext(ctx, "process already exited", "name", name, "pidfile", pidfile, "pid", pidint)
			return nil // TODO:this makes this reaper non-functional
		} else {
			return errors.Errorf("failed to wait on pid: %w", err)
		}
	}
	slog.InfoContext(ctx, "waiter done", "waiter", waiter)

	go func() {
		r.reaperCh <- gorunc.Exit{
			Timestamp: time.Now(),
			Pid:       pidint,
			Status:    int(waiter.Sys().(syscall.WaitStatus).ExitStatus()),
		}
	}()

	slog.InfoContext(ctx, "reaper waiting for parent to exit", "name", name, "pidfile", pidfile, "pid", pidint)

	<-parentDoneChan

	slog.InfoContext(ctx, "reaper parent exited, waiting for process to exit", "name", name, "pidfile", pidfile, "pid", pidint)

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
	go r.reaperLoggingError(ctx, reaperChan, options.PidFile, "create", nil)

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
	pidchan := make(chan int, 1)
	opts.Started = pidchan
	go r.reaperLoggingError(ctx, reaperChan, opts.PidFile, "exec", pidchan)
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
