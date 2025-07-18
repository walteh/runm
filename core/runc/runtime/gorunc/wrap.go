//go:build !windows

package goruncruntime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/pkg/ticker"
)

func tryReadPidFile(ctx context.Context, pidfile string) (int, error) {
	pid, err := os.ReadFile(pidfile)
	if err != nil {
		return -1, errors.Errorf("failed to read pid file: %w", err)
	}
	return strconv.Atoi(string(pid))
}

func waitForFileToExistChan(ctx context.Context, pidfile string) chan int {
	pidfilechan := make(chan int, 1)
	go func() {
		for {
			if ctx.Err() != nil {
				pidfilechan <- -1
				return
			}

			if pidint, err := tryReadPidFile(ctx, pidfile); err == nil {
				pidfilechan <- pidint
				return
			}
		}
	}()
	return pidfilechan
}

func (r *GoRuncRuntime) reaperLoggingError(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int, justWait bool) {
	errchan := make(chan error, 1)
	defer close(errchan)
	go func() {
		errchan <- r.reaper(ctx, parentDoneChan, pidfile, name, pidchan, justWait)
	}()
	err := <-errchan
	if err != nil {
		slog.ErrorContext(ctx, "reaper error", "name", name, "pidfile", pidfile, "error", err)
	}
}

func (r *GoRuncRuntime) reaper(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int, justWait bool) error {
	pidint := -1
	waitingOnPid := true
	waitingOnParent := true
	waitingOnProcess := true

	// since this is a background process, we don't want to die when the parent dies
	ctx = context.WithoutCancel(ctx)

	slog.InfoContext(ctx, "starting reaper", "name", name, "pidfile", pidfile)

	defer ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("GORUNC:REAPER:%s[RUNNING]", name)),
		ticker.WithDoneMessage(fmt.Sprintf("GORUNC:REAPER:%s[DONE]", name)),
		ticker.WithAttrFunc(func() []slog.Attr {
			// attrs := piddebug.BigPidAttrFunc(pidint)

			attrs := []slog.Attr{}

			attrs = append(attrs, slog.Bool("waiting_on_pid", waitingOnPid))
			attrs = append(attrs, slog.Bool("waiting_on_parent", waitingOnParent))
			attrs = append(attrs, slog.Bool("waiting_on_process", waitingOnProcess))
			attrs = append(attrs, slog.String("pidfile", pidfile))
			return attrs
		}),
		ticker.WithSlogBaseContext(ctx),
	).RunAsDefer()()

	var err error

	if justWait {
		<-parentDoneChan
		return nil
	}

	// this hacky logic probably broke the exit status code carryover with go tool task dev:2025-07-02:01
	if pidchan != nil {
		pidint = <-pidchan
	} else {
		pidint = <-waitForFileToExistChan(ctx, pidfile)
	}

	waitingOnPid = false

	sys, err := os.FindProcess(pidint)
	if err != nil {
		return errors.Errorf("failed to find process: %w", err)
	}

	retryCount := 1000

	var waiter *os.ProcessState
	for {
		waiter, err = sys.Wait()
		if err != nil {
			if errors.Is(err, syscall.ECHILD) {
				retryCount--
				if retryCount <= 0 {
					return errors.Errorf("failed to wait on pid %d: %w", pidint, err)
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return errors.Errorf("failed to wait on pid %d: %w", pidint, err)
		}
		break
	}

	// waiter, err := sys.Wait()
	// if err != nil {
	// 	// if errors.Is(err, syscall.ECHILD) {
	// 	// 	slog.InfoContext(ctx, "process already exited", "name", name, "pidfile", pidfile, "pid", pidint)
	// 	// 	waitingOnProcess = false
	// 	// 	return nil // TODO:this makes this reaper non-functional
	// 	// } else {
	// 	return errors.Errorf("failed to wait on pid %d: %w", pidint, err)
	// 	// }
	// }

	waitingOnProcess = false

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

	waitingOnParent = false

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

	// reaperChan := make(chan string, 1)
	// defer close(reaperChan)
	// // go r.reaperLoggingError(ctx, reaperChan, options.PidFile, "create", nil)

	// options.NoNewKeyring = true

	// // pidchan := make(chan int, 1)
	// go r.reaperLoggingError(ctx, reaperChan, options.PidFile, "create", nil, true)
	// options.Started = pidchan

	return WrapWithRuntimeError(ctx, r, func() error {
		slog.InfoContext(ctx, "creating container", "id", id, "bundle", bundle, "options", options)
		// defer options.IO.Close()
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
	// reaperChan := make(chan string, 1)
	// defer close(reaperChan)
	// pidchan := make(chan int, 1)
	// opts.Started = pidchan
	// go r.reaperLoggingError(ctx, reaperChan, opts.PidFile, "exec", pidchan, true)
	return WrapWithRuntimeError(ctx, r, func() error {
		// defer opts.IO.Close()
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

// debugNamespaces prints current process namespace information
func debugNamespaces(processName string, pid int) string {
	buf := bytes.NewBufferString("")
	fmt.Fprintf(buf, "=== NAMESPACE DEBUG for %s (PID: %d) ===\n", processName, pid)

	// Read all namespace links
	namespaces := []string{"mnt", "pid", "net", "ipc", "uts", "user", "cgroup"}

	for _, ns := range namespaces {
		nsPath := fmt.Sprintf("/proc/%d/ns/%s", pid, ns)
		if link, err := os.Readlink(nsPath); err == nil {
			fmt.Fprintf(buf, "  %s: %s\n", ns, link)
		} else {
			fmt.Fprintf(buf, "  %s: ERROR - %v\n", ns, err)
		}
	}

	// Show parent process for context
	if ppid := os.Getppid(); ppid > 0 {
		fmt.Fprintf(buf, "  Parent PID: %d\n", ppid)
	}

	fmt.Fprintf(buf, "=== END NAMESPACE DEBUG ===\n")
	return buf.String()
}

// debugMounts shows current mount namespace view
func debugMounts(processName string, pid int) string {
	buf := bytes.NewBufferString("")
	fmt.Fprintf(buf, "=== MOUNT DEBUG for (%s) (PID: %d) ===\n", processName, pid)

	// Read /proc/self/mounts
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/mounts", pid)); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			fmt.Fprintf(buf, "  %s\n", line)
		}
	} else {
		fmt.Fprintf(buf, "  ERROR reading mounts: %v\n", err)
	}

	fmt.Fprintf(buf, "=== END MOUNT DEBUG ===\n")
	return buf.String()
}
