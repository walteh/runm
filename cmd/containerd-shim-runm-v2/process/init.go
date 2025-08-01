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
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/containerd/console"
	"github.com/containerd/containerd/v2/pkg/stdio"
	"github.com/containerd/fifo"
	"github.com/containerd/log"
	"gitlab.com/tozd/go/errors"

	google_protobuf "github.com/containerd/containerd/v2/pkg/protobuf/types"
	gorunc "github.com/containerd/go-runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/runc/process"
	"github.com/walteh/runm/core/runc/runtime"
)

// Init represents an initial process for a container
type Init struct {
	wg        sync.WaitGroup
	initState initState

	// mu is used to ensure that `Start()` and `Exited()` calls return in
	// the right order when invoked in separate goroutines.
	// This is the case within the shim implementation as it makes use of
	// the reaper interface.
	mu sync.Mutex

	waitBlock chan struct{}

	WorkDir string

	id       string
	Bundle   string
	console  console.Console
	Platform stdio.Platform
	io       *processIO

	// pausing preserves the pausing state.
	pausing      atomic.Bool
	status       int
	exited       time.Time
	pid          int
	closers      []io.Closer
	stdin        io.Closer
	stdio        stdio.Stdio
	Rootfs       string
	IoUID        int
	IoGID        int
	NoPivotRoot  bool
	NoNewKeyring bool
	CriuWorkPath string

	runtime       runtime.Runtime
	cgroupAdapter runtime.CgroupAdapter
}

func (p *Init) CloseIO() {
	if p.io != nil {
		p.io.Close()
	}
}

// New returns a new process
func New(id string, runtime runtime.Runtime, cgroupAdapter runtime.CgroupAdapter, stdio stdio.Stdio) *Init {
	p := &Init{
		id:            id,
		runtime:       runtime,
		cgroupAdapter: cgroupAdapter,
		stdio:         stdio,
		status:        0,
		waitBlock:     make(chan struct{}),
	}
	p.initState = &createdState{p: p}
	return p
}

// Create the process with the provided config
func (p *Init) Create(ctx context.Context, r *process.CreateConfig) error {

	defer func() {
		slog.InfoContext(ctx, "shim init process create is complete")
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic in Create", "error", r)
			panic(r)
		}
	}()

	var (
		err     error
		socket  runtime.ConsoleSocket
		pio     *processIO
		pidFile = newPidFile(p.Bundle)
	)

	ctx = slogctx.Append(ctx, "operation", "process.init.create")
	if r.Terminal {
		if socket, err = p.runtime.NewTempConsoleSocket(ctx); err != nil {
			return errors.Errorf("failed to create OCI runtime console socket: %w", err)
		}
		p.closers = append(p.closers, socket)
	} else {
		if pio, err = createIO(ctx, p.id, p.IoUID, p.IoGID, p.stdio, p.runtime); err != nil {
			return errors.Errorf("failed to create init process I/O: %w", err)
		}
		p.io = pio
	}

	if r.Checkpoint != "" {
		return p.createCheckpointedState(r, pidFile)
	}
	opts := &gorunc.CreateOpts{
		PidFile:      pidFile.Path(),
		NoPivot:      p.NoPivotRoot,
		NoNewKeyring: p.NoNewKeyring,
	}
	if p.io != nil {
		opts.IO = p.io.IO()
	}
	if socket != nil {
		opts.ConsoleSocket = socket
	}

	slog.InfoContext(ctx, "calling runtime.Create", "id", r.ID, "bundle", r.Bundle, "opts", opts)

	// gorunc:call Create
	if err := p.runtime.Create(ctx, r.ID, r.Bundle, opts); err != nil {
		// fmt.Fprintf(logging.GetDefaultLogWriter(), "runtime error (%T): %+v\n", err, err)
		// slog.ErrorContext(ctx, "runtime error", "error", err, "type", reflect.TypeOf(err))
		return errors.Errorf("OCI runtime create failed: %w", err)
	}
	slog.InfoContext(ctx, "done calling runtime.Create")
	if r.Stdin != "" {
		slog.InfoContext(ctx, "opening stdin", "path", r.Stdin)
		if err := p.openStdin(r.Stdin); err != nil {
			return err
		}
		slog.InfoContext(ctx, "done opening stdin")
	}

	slog.InfoContext(ctx, "starting console copy")

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if socket != nil {
		slog.InfoContext(ctx, "receiving console master")
		console, err := socket.ReceiveMaster()
		if err != nil {
			return errors.Errorf("failed to retrieve console master: %w", err)
		}
		slog.InfoContext(ctx, "starting console copy")
		console, err = p.Platform.CopyConsole(ctx, console, p.id, r.Stdin, r.Stdout, r.Stderr, &p.wg)
		if err != nil {
			return errors.Errorf("failed to start console copy: %w", err)
		}
		slog.InfoContext(ctx, "done starting console copy")
		p.console = console
	} else {
		slog.InfoContext(ctx, "starting io pipe copy", "pio_is_nil", pio == nil)
		if err := pio.Copy(ctx, &p.wg); err != nil {
			return errors.Errorf("failed to start io pipe copy: %w", err)
		}
		slog.InfoContext(ctx, "done starting io pipe copy")
	}

	slog.InfoContext(ctx, "reading pid file")
	pid, err := p.runtime.ReadPidFile(ctx, pidFile.Path())
	if err != nil {
		return errors.Errorf("failed to retrieve OCI runtime container pid: %w", err)
	}
	slog.InfoContext(ctx, "done reading pid file", "pid", pid)
	p.pid = pid
	return nil
}

func (p *Init) openStdin(path string) error {
	sc, err := fifo.OpenFifo(context.Background(), path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return errors.Errorf("failed to open stdin fifo %s: %w", path, err)
	}
	p.stdin = sc
	p.closers = append(p.closers, sc)
	return nil
}

func (p *Init) createCheckpointedState(r *process.CreateConfig, pidFile *pidFile) error {
	opts := &gorunc.RestoreOpts{
		CheckpointOpts: gorunc.CheckpointOpts{
			ImagePath:  r.Checkpoint,
			WorkDir:    p.CriuWorkPath,
			ParentPath: r.ParentCheckpoint,
		},
		PidFile:     pidFile.Path(),
		NoPivot:     p.NoPivotRoot,
		Detach:      true,
		NoSubreaper: true,
	}

	if p.io != nil {
		opts.IO = p.io.IO()
	}

	p.initState = &createdCheckpointState{
		p:    p,
		opts: opts,
	}
	return nil
}

// Wait for the process to exit
func (p *Init) Wait() {
	<-p.waitBlock
}

// ID of the process
func (p *Init) ID() string {
	return p.id
}

// Pid of the process
func (p *Init) Pid() int {
	return p.pid
}

// ExitStatus of the process
func (p *Init) ExitStatus() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.status
}

// ExitedAt at time when the process exited
func (p *Init) ExitedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.exited
}

// Status of the process
func (p *Init) Status(ctx context.Context) (string, error) {
	if p.pausing.Load() {
		return "pausing", nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.initState.Status(ctx)
}

// Start the init process
func (p *Init) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.initState.Start(ctx)
}

func (p *Init) start(ctx context.Context) error {
	err := p.runtime.Start(ctx, p.id)
	if err != nil {
		return errors.Errorf("starting init process: %w", err)
	}
	return nil
}

// SetExited of the init process with the next status
func (p *Init) SetExited(status int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.initState.SetExited(status)
}

func (p *Init) setExited(status int) {
	p.exited = time.Now()
	p.status = status
	p.Platform.ShutdownConsole(context.Background(), p.console)
	close(p.waitBlock)
}

// Delete the init process
func (p *Init) Delete(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic in Delete", "error", r)
			panic(r)
		}
	}()

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.initState.Delete(ctx); err != nil {
		return errors.Errorf("deleting init process: %w", err)
	}
	return nil
}

func (p *Init) delete(ctx context.Context) error {
	waitTimeout(ctx, &p.wg, 2*time.Second)
	err := p.runtime.Delete(ctx, p.id, &gorunc.DeleteOpts{})
	// ignore errors if a runtime has already deleted the process
	// but we still hold metadata and pipes
	//
	// this is common during a checkpoint, runc will delete the container state
	// after a checkpoint and the container will no longer exist within runc
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			err = nil
		} else {

			err = errors.Errorf("failed to delete task: %w", err)
		}
	}
	if p.io != nil {
		for _, c := range p.closers {
			c.Close()
		}
		p.io.Close()
	}

	slog.DebugContext(ctx, "closing runtime", "runtime", p.runtime)
	err = p.runtime.Close(ctx)
	slog.DebugContext(ctx, "runtime closed", "error", err)

	// // TODO: MAKE SURE WE DON"T NEED THIS
	// if err2 := mount.UnmountRecursive(p.Rootfs, 0); err2 != nil {
	// 	log.G(ctx).WithError(err2).Warn("failed to cleanup rootfs mount")
	// 	if err == nil {
	// 		err = errors.Errorf("failed rootfs umount: %w", err2)
	// 	}
	// }

	return err
}

// Resize the init processes console
func (p *Init) Resize(ws console.WinSize) error {
	start := time.Now()
	ctx := context.Background()
	slog.DebugContext(ctx, "SHIM:PROCESS:INIT:START[RESIZE_PTY]", "id", p.ID, "ws", ws, "p.console==nil", p.console == nil)

	defer func() {
		slog.DebugContext(ctx, "SHIM:PROCESS:INIT:END  [RESIZE_PTY]", "id", p.ID, "ws", ws, "p.console==nil", p.console == nil, "duration", time.Since(start))
	}()
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.console == nil {
		return nil
	}
	return p.console.Resize(ws)
}

// Pause the init process and all its child processes
func (p *Init) Pause(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.initState.Pause(ctx); err != nil {
		return errors.Errorf("pausing init process: %w", err)
	}
	return nil
}

// Resume the init process and all its child processes
func (p *Init) Resume(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.initState.Resume(ctx); err != nil {
		return errors.Errorf("resuming init process: %w", err)
	}
	return nil
}

// Kill the init process
func (p *Init) Kill(ctx context.Context, signal uint32, all bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.initState.Kill(ctx, signal, all); err != nil {
		return errors.Errorf("killing init process: %w", err)
	}
	return nil
}

func (p *Init) kill(ctx context.Context, signal uint32, all bool) error {
	err := p.runtime.Kill(ctx, p.id, int(signal), &gorunc.KillOpts{
		All: all,
	})
	return checkKillError(err)
}

// KillAll processes belonging to the init process
func (p *Init) KillAll(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// gorunc:call Kill
	if err := p.runtime.Kill(ctx, p.id, int(unix.SIGKILL), &gorunc.KillOpts{
		All: true,
	}); err != nil {
		return errors.Errorf("killing all processes: %w", err)
	}
	return nil
}

// Stdin of the process
func (p *Init) Stdin() io.Closer {
	return p.stdin
}

// Runtime returns the OCI runtime configured for the init process
func (p *Init) Runtime() runtime.Runtime {
	return p.runtime
}

// CgroupAdapter returns the cgroup adapter for the init process
func (p *Init) CgroupAdapter() runtime.CgroupAdapter {
	return p.cgroupAdapter
}

// Exec returns a new child process
func (p *Init) Exec(ctx context.Context, path string, r *process.ExecConfig) (Process, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	process, err := p.initState.Exec(ctx, path, r)
	if err != nil {
		return nil, errors.Errorf("executing init process: %w", err)
	}
	return process, nil
}

// exec returns a new exec'd process
func (p *Init) exec(_ context.Context, path string, r *process.ExecConfig) (Process, error) {
	// process exec request
	// var spec specs.Process
	// if err := json.Unmarshal(r.Spec.Value, &spec); err != nil {
	// 	return nil, err
	// }

	if r.Spec == nil {
		return nil, errors.Errorf("spec is nil")
	}

	r.Spec.Terminal = r.Terminal

	e := &execProcess{
		id:     r.ID,
		path:   path,
		parent: p,
		spec:   *r.Spec,
		stdio: stdio.Stdio{
			Stdin:    r.Stdin,
			Stdout:   r.Stdout,
			Stderr:   r.Stderr,
			Terminal: r.Terminal,
		},
		waitBlock: make(chan struct{}),
	}
	e.execState = &execCreatedState{p: e}
	return e, nil
}

// Checkpoint the init process
func (p *Init) Checkpoint(ctx context.Context, r *process.CheckpointConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.initState.Checkpoint(ctx, r); err != nil {
		return errors.Errorf("checkpointing init process: %w", err)
	}
	return nil
}

func (p *Init) checkpoint(ctx context.Context, r *process.CheckpointConfig) error {
	var actions []gorunc.CheckpointAction
	if !r.Exit {
		actions = append(actions, gorunc.LeaveRunning)
	}
	// keep criu work directory if criu work dir is set
	work := r.WorkDir
	if work == "" {
		work = filepath.Join(p.WorkDir, "criu-work")
		defer os.RemoveAll(work)
	}
	// gorunc:call Checkpoint
	if err := p.runtime.Checkpoint(ctx, p.id, &gorunc.CheckpointOpts{
		WorkDir:                  work,
		ImagePath:                r.Path,
		AllowOpenTCP:             r.AllowOpenTCP,
		AllowExternalUnixSockets: r.AllowExternalUnixSockets,
		AllowTerminal:            r.AllowTerminal,
		FileLocks:                r.FileLocks,
		EmptyNamespaces:          r.EmptyNamespaces,
	}, actions...); err != nil {
		dumpLog := filepath.Join(p.Bundle, "criu-dump.log")
		if cerr := copyFile(dumpLog, filepath.Join(work, "dump.log")); cerr != nil {
			log.G(ctx).WithError(cerr).Error("failed to copy dump.log to criu-dump.log")
		}
		return errors.Errorf("%s path= %s", criuError(err), dumpLog)
	}
	return nil
}

// Update the processes resource configuration
func (p *Init) Update(ctx context.Context, r *google_protobuf.Any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.initState.Update(ctx, r); err != nil {
		return errors.Errorf("updating init process: %w", err)
	}
	return nil
}

func (p *Init) update(ctx context.Context, r *google_protobuf.Any) error {
	var resources specs.LinuxResources
	if err := json.Unmarshal(r.Value, &resources); err != nil {
		return err
	}
	if err := p.runtime.Update(ctx, p.id, &resources); err != nil {
		return errors.Errorf("updating init process: %w", err)
	}
	return nil
}

// Stdio of the process
func (p *Init) Stdio() stdio.Stdio {
	return p.stdio
}

// func (p *Init) runtimeError(ctx context.Context, rErr error, msg string) error {

// 	if rErr == nil {
// 		return nil
// 	}

// 	if enc, ok := rErr.(*stackerr.StackedEncodableError); ok {
// 		return errors.Errorf("%s: %w", msg, enc)
// 	}

// 	if _, ok := rErr.(errors.E); ok {
// 		return errors.Errorf("%s: %w", msg, rErr)
// 	}

// 	rMsg, err := getLastRuntimeError(ctx, p.runtime)
// 	switch {
// 	// case err != nil:
// 	// return errors.Errorf("%s: %s (%s): %w", msg, "unable to retrieve OCI runtime error", err.Error(), rErr)
// 	case err != nil, rMsg == "":
// 		return errors.Errorf("%s: %w", msg, rErr)
// 	default:
// 		return errors.Errorf("%s: %s", msg, rMsg)
// 	}
// }

func withConditionalIO(c stdio.Stdio) gorunc.IOOpt {
	return func(o *gorunc.IOOption) {
		o.OpenStdin = c.Stdin != ""
		o.OpenStdout = c.Stdout != ""
		o.OpenStderr = c.Stderr != ""
	}
}

func withRawIOOpt(c func(o *gorunc.IOOption)) gorunc.IOOpt {
	return func(o *gorunc.IOOption) {
		c(o)
	}
}
