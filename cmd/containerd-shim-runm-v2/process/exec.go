//go:build !windows

/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package process

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/containerd/console"
	"github.com/containerd/containerd/v2/pkg/stdio"
	"github.com/containerd/errdefs"
	"github.com/containerd/fifo"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/walteh/runm/core/runc/runtime"
)

type execProcess struct {
	wg sync.WaitGroup

	execState execState

	mu      sync.Mutex
	id      string
	console console.Console
	io      *processIO
	status  int
	exited  time.Time
	pid     safePid
	closers []io.Closer
	stdin   io.Closer
	stdio   stdio.Stdio
	path    string
	spec    specs.Process

	parent    *Init
	waitBlock chan struct{}
}

func (e *execProcess) CloseIO() {
	if e.io != nil {
		e.io.Close()
	}
}

func (e *execProcess) Runtime() runtime.Runtime {
	return e.parent.runtime
}

func (e *execProcess) CgroupAdapter() runtime.CgroupAdapter {
	return e.parent.cgroupAdapter
}

func (e *execProcess) Wait() {
	<-e.waitBlock
}

func (e *execProcess) ID() string {
	return e.id
}

func (e *execProcess) Pid() int {
	return e.pid.getLocked()
}

func (e *execProcess) ExitStatus() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *execProcess) ExitedAt() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exited
}

func (e *execProcess) SetExited(status int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.execState.SetExited(status)
}

func (e *execProcess) setExited(status int) {
	e.status = status
	e.exited = time.Now()
	e.parent.Platform.ShutdownConsole(context.Background(), e.console)
	close(e.waitBlock)
}

func (e *execProcess) Delete(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.execState.Delete(ctx)
}

func (e *execProcess) delete(ctx context.Context) error {
	waitTimeout(ctx, &e.wg, 2*time.Second)
	if e.io != nil {
		for _, c := range e.closers {
			c.Close()
		}
		e.io.Close()
	}
	pidfile := filepath.Join(e.path, fmt.Sprintf("%s.pid", e.id))
	// silently ignore error
	os.Remove(pidfile)
	return nil
}

func (e *execProcess) Resize(ws console.WinSize) error {
	start := time.Now()
	ctx := context.Background()
	slog.DebugContext(ctx, "SHIM:PROCESS:EXEC:START[RESIZE_PTY]", "id", e.id, "ws", ws, "e.console==nil", e.console == nil)
	defer func() {
		slog.DebugContext(ctx, "SHIM:PROCESS:EXEC:END  [RESIZE_PTY]", "id", e.id, "ws", ws, "e.console==nil", e.console == nil, "duration", time.Since(start))
	}()
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.execState.Resize(ws)
}

func (e *execProcess) resize(ws console.WinSize) error {
	slog.DebugContext(context.Background(), "SHIM:PROCESS:EXEC:RESIZE", "id", e.id, "ws", ws, "e.console==nil", e.console == nil)
	if e.console == nil {
		return nil
	}
	return e.console.Resize(ws)
}

func (e *execProcess) Kill(ctx context.Context, sig uint32, _ bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.execState.Kill(ctx, sig, false)
}

func (e *execProcess) kill(_ context.Context, sig uint32, _ bool) error {
	pid := e.pid.getLocked()
	switch {
	case pid == 0:
		return errors.Errorf("process not created: %w", errdefs.ErrFailedPrecondition)
	case !e.exited.IsZero():
		return errors.Errorf("process already finished: %w", errdefs.ErrNotFound)
	default:
		if err := unix.Kill(pid, syscall.Signal(sig)); err != nil {
			return errors.Errorf("exec kill error: %w", checkKillError(err))
		}
	}
	return nil
}

func (e *execProcess) Stdin() io.Closer {
	return e.stdin
}

func (e *execProcess) Stdio() stdio.Stdio {
	return e.stdio
}

func (e *execProcess) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	slog.DebugContext(ctx, "TMP0 start", "id", e.id, "pid", e.pid.getLocked())
	return e.execState.Start(ctx)
}

func (e *execProcess) start(ctx context.Context) (err error) {

	slog.DebugContext(ctx, "EXECPROCESS:START[OPENING]", "id", e.id)
	defer slog.DebugContext(ctx, "EXECPROCESS:START[DONE]", "id", e.id)
	// The reaper may receive exit signal right after
	// the container is started, before the e.pid is updated.
	// In that case, we want to block the signal handler to
	// access e.pid until it is updated.
	e.pid.Lock()
	defer e.pid.Unlock()

	slog.DebugContext(ctx, "EXECPROCESS:START[A]", "id", e.id, "pid", e.pid.pid)

	var (
		socket  runtime.ConsoleSocket
		pio     *processIO
		pidFile = newExecPidFile(e.path, e.id)
	)

	if e.stdio.Terminal {
		slog.DebugContext(ctx, "EXECPROCESS:START[B]", "id", e.id, "pid", e.pid.pid)
		if socket, err = e.parent.runtime.NewTempConsoleSocket(ctx); err != nil {
			return errors.Errorf("failed to create runc console socket: %w", err)
		}
		e.closers = append(e.closers, socket)
	} else {
		if pio, err = createIO(ctx, e.id, e.parent.IoUID, e.parent.IoGID, e.stdio, e.parent.runtime); err != nil {
			return errors.Errorf("failed to create init process I/O: %w", err)
		}
		e.io = pio
	}
	slog.DebugContext(ctx, "EXECPROCESS:START[C]", "id", e.id, "pid", e.pid.pid)
	opts := &gorunc.ExecOpts{
		PidFile: pidFile.Path(),
		Detach:  true,
	}
	if pio != nil {
		slog.DebugContext(ctx, "EXECPROCESS:START[D]", "id", e.id, "pid", e.pid.pid)
		opts.IO = pio.IO()
	}
	if socket != nil {
		slog.DebugContext(ctx, "EXECPROCESS:START[E]", "id", e.id, "pid", e.pid.pid)
		opts.ConsoleSocket = socket
	}
	// ========= should run before not after ==============
	slog.DebugContext(ctx, "EXECPROCESS:START[G]", "id", e.id, "pid", e.pid.pid)
	if e.stdio.Stdin != "" {
		if err := e.openStdin(e.stdio.Stdin); err != nil {
			return err
		}
	}
	slog.DebugContext(ctx, "EXECPROCESS:START[H]", "id", e.id, "pid", e.pid.pid)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if socket != nil {
		slog.DebugContext(ctx, "EXECPROCESS:START[I]", "id", e.id, "pid", e.pid.pid)
		console, err := socket.ReceiveMaster()
		if err != nil {
			return errors.Errorf("failed to retrieve console master: %w", err)
		}
		slog.DebugContext(ctx, "EXECPROCESS:START[J]", "id", e.id, "pid", e.pid.pid)
		if e.console, err = e.parent.Platform.CopyConsole(ctx, console, e.id, e.stdio.Stdin, e.stdio.Stdout, e.stdio.Stderr, &e.wg); err != nil {
			return errors.Errorf("failed to start console copy: %w", err)
		}
		slog.DebugContext(ctx, "EXECPROCESS:START[K]", "id", e.id, "pid", e.pid.pid)
	} else {
		slog.DebugContext(ctx, "EXECPROCESS:START[L]", "id", e.id, "pid", e.pid.pid)
		if err := pio.Copy(ctx, &e.wg); err != nil {
			return errors.Errorf("failed to start io pipe copy: %w", err)
		}
	}
	slog.DebugContext(ctx, "EXECPROCESS:START[M]", "id", e.id, "pid", e.pid.pid)
	// ========= should run before not after ==============
	slog.DebugContext(ctx, "EXECPROCESS:START[F]", "id", e.id, "pid", e.pid.pid)

	// todo: the prob is that this is never returinging
	// gorunc:call Exec
	if err := e.parent.runtime.Exec(ctx, e.parent.id, e.spec, opts); err != nil {
		close(e.waitBlock)
		return errors.Errorf("OCI runtime exec failed: %w", err)
	}

	pid, err := e.parent.runtime.ReadPidFile(ctx, pidFile.Path())
	if err != nil {
		return errors.Errorf("failed to retrieve OCI runtime exec pid: %w", err)
	}
	e.pid.pid = pid
	slog.DebugContext(ctx, "EXECPROCESS:START[N]", "id", e.id, "pid", e.pid.pid)
	return nil
}

func (e *execProcess) openStdin(path string) error {
	sc, err := fifo.OpenFifo(context.Background(), path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errors.Errorf("failed to open stdin fifo %s: %w", path, err)
	}
	e.stdin = sc
	e.closers = append(e.closers, sc)
	return nil
}

func (e *execProcess) Status(ctx context.Context) (string, error) {
	s, err := e.parent.Status(ctx)
	if err != nil {
		return "", err
	}
	// if the container as a whole is in the pausing/paused state, so are all
	// other processes inside the container, use container state here
	switch s {
	case "paused", "pausing":
		return s, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.execState.Status(ctx)
}
