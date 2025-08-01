//go:build linux || darwin

package runcexecutor

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/containerd/console"
	"github.com/moby/buildkit/executor"
	"github.com/moby/buildkit/util/bklog"
	"github.com/moby/sys/signal"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"

	runc "github.com/containerd/go-runc"
	gatewayapi "github.com/moby/buildkit/frontend/gateway/pb"

	"github.com/walteh/runm/core/runc/runtime"
)

func updateRuncFieldsForHostOS(runtime *runtime.RuntimeOptions) {
	// PdeathSignal only supported on unix platforms
	runtime.PdeathSignal = syscall.SIGKILL // this can still leak the process
}

func (w *runcExecutor) run(ctx context.Context, id, bundle string, process executor.ProcessInfo, started func(), keep bool) error {
	killer := newRunProcKiller(w.runc, id)
	return w.callWithIO(ctx, process, started, killer, func(ctx context.Context, started chan<- int, io runc.IO, pidfile string) error {
		extraArgs := []string{}
		if keep {
			extraArgs = append(extraArgs, "--keep")
		}
		_, err := w.runc.RuncRun(ctx, id, bundle, &runc.CreateOpts{
			NoPivot:   w.noPivot,
			Started:   started,
			IO:        io,
			ExtraArgs: extraArgs,
		})
		return err
	})
}

func (w *runcExecutor) exec(ctx context.Context, id string, specsProcess *specs.Process, process executor.ProcessInfo, started func()) error {
	killer, err := newExecProcKiller(w.runc, id)
	if err != nil {
		return errors.Wrap(err, "failed to initialize process killer")
	}
	defer killer.Cleanup()

	return w.callWithIO(ctx, process, started, killer, func(ctx context.Context, started chan<- int, io runc.IO, pidfile string) error {
		return w.runc.Exec(ctx, id, *specsProcess, &runc.ExecOpts{
			Started: started,
			IO:      io,
			PidFile: pidfile,
		})
	})
}

type runcCall func(ctx context.Context, started chan<- int, io runc.IO, pidfile string) error

func (w *runcExecutor) callWithIO(ctx context.Context, process executor.ProcessInfo, started func(), killer procKiller, call runcCall) error {
	runcProcess, ctx := runcProcessHandle(ctx, killer)
	defer runcProcess.Release()

	eg, ctx := errgroup.WithContext(ctx)
	defer func() {
		if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			bklog.G(ctx).Errorf("runc process monitoring error: %s", err)
		}
	}()
	defer runcProcess.Shutdown()

	startedCh := make(chan int, 1)
	eg.Go(func() error {
		return runcProcess.WaitForStart(ctx, startedCh, started)
	})

	eg.Go(func() error {
		return handleSignals(ctx, runcProcess, process.Signal)
	})

	if !process.Meta.Tty {
		return call(ctx, startedCh, &forwardIO{stdin: process.Stdin, stdout: process.Stdout, stderr: process.Stderr}, killer.pidfile)
	}

	ptm, ptsName, err := console.NewPty()
	if err != nil {
		return err
	}

	pts, err := os.OpenFile(ptsName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		ptm.Close()
		return err
	}

	defer func() {
		if process.Stdin != nil {
			process.Stdin.Close()
		}
		pts.Close()
		ptm.Close()
		runcProcess.Shutdown()
		err := eg.Wait()
		if err != nil {
			bklog.G(ctx).Warningf("error while shutting down tty io: %s", err)
		}
	}()

	if process.Stdin != nil {
		eg.Go(func() error {
			_, err := io.Copy(ptm, process.Stdin)
			// stdin might be a pipe, so this is like EOF
			if errors.Is(err, io.ErrClosedPipe) {
				return nil
			}
			return err
		})
	}

	if process.Stdout != nil {
		eg.Go(func() error {
			_, err := io.Copy(process.Stdout, ptm)
			// ignore `read /dev/ptmx: input/output error` when ptm is closed
			var ptmClosedError *os.PathError
			if errors.As(err, &ptmClosedError) {
				if ptmClosedError.Op == "read" &&
					ptmClosedError.Path == "/dev/ptmx" &&
					errors.Is(ptmClosedError.Err, syscall.EIO) {
					return nil
				}
			}
			return err
		})
	}

	eg.Go(func() error {
		err := runcProcess.WaitForReady(ctx)
		if err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return nil
			case resize := <-process.Resize:
				err = ptm.Resize(console.WinSize{
					Height: uint16(resize.Rows),
					Width:  uint16(resize.Cols),
				})
				if err != nil {
					bklog.G(ctx).Errorf("failed to resize ptm: %s", err)
				}
				// SIGWINCH must be sent to the runc monitor process, as
				// terminal resizing is done in runc.
				err = runcProcess.monitorProcess.Signal(signal.SIGWINCH)
				if err != nil {
					bklog.G(ctx).Errorf("failed to send SIGWINCH to process: %s", err)
				}
			}
		}
	})

	runcIO := &forwardIO{}
	if process.Stdin != nil {
		runcIO.stdin = pts
	}
	if process.Stdout != nil {
		runcIO.stdout = pts
	}
	if process.Stderr != nil {
		runcIO.stderr = pts
	}

	return call(ctx, startedCh, runcIO, killer.pidfile)
}

func detectOOM(ctx context.Context, ns string, gwErr *gatewayapi.ExitError) {
	const defaultCgroupMountpoint = "/sys/fs/cgroup"

	if ns == "" {
		return
	}

	count, err := readMemoryEvent(filepath.Join(defaultCgroupMountpoint, ns), "oom_kill")
	if err != nil {
		bklog.G(ctx).WithError(err).Warn("failed to read oom_kill event")
		return
	}
	if count > 0 {
		gwErr.Err = syscall.ENOMEM
	}
}

func readMemoryEvent(fp string, event string) (uint64, error) {
	f, err := os.Open(filepath.Join(fp, "memory.events"))
	if err != nil {
		return 0, err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		parts := strings.Fields(s.Text())
		if len(parts) != 2 {
			continue
		}
		if parts[0] != event {
			continue
		}
		v, err := strconv.ParseUint(parts[1], 10, 64)
		if err == nil {
			return v, nil
		}
	}
	return 0, s.Err()
}
