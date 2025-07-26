package socket

import (
	"context"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"

	"github.com/containerd/console"
	"github.com/creack/pty"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/pkg/conn"
)

var _ console.Console = &RemoteConsole{}

// PTYConsoleAdapter wraps a ReadWriteCloser and presents it as a containerd/console
type RemoteConsole struct {
	conn        io.ReadWriteCloser // Your network connection
	ptyFile     *os.File           // PTY master side
	ttyFile     *os.File           // TTY slave side
	console     console.Console    // containerd console
	wg          sync.WaitGroup
	done        chan struct{}
	resizeFunc  func(ctx context.Context, winSize console.WinSize) error
	creationCtx context.Context
}

// NewPTYConsoleAdapter creates a new adapter that bridges your ReadWriteCloser to a containerd console
func NewRemotePTYConsoleAdapter(ctx context.Context, wrcon io.ReadWriteCloser, resizeFunc func(ctx context.Context, winSize console.WinSize) error) (console.Console, error) {

	ptyFile, ttyFile, err := pty.Open()
	if err != nil {
		return nil, errors.Errorf("failed to create pty: %w", err)
	}

	// Create containerd console from the PTY master FIRST
	consoleInstance, err := console.ConsoleFromFile(ptyFile)
	if err != nil {
		ptyFile.Close()
		ttyFile.Close()
		return nil, errors.Errorf("failed to create console: %w", err)
	}

	creator, err := newConsoleCreator()
	if err != nil {
		return nil, errors.Errorf("failed to create console creator: %w", err)
	}

	// localKqueueConsole, err := creator(consoleInstance)
	// if err != nil {
	// 	return nil, errors.Errorf("failed to add console to kqueue: %w", err)
	// }

	adapter := &RemoteConsole{
		conn:        wrcon,
		ttyFile:     ttyFile,
		ptyFile:     ptyFile,
		console:     consoleInstance,
		done:        make(chan struct{}),
		wg:          sync.WaitGroup{},
		resizeFunc:  resizeFunc,
		creationCtx: ctx,
	}

	networkKqueueConsole, err := creator(adapter)
	if err != nil {
		return nil, errors.Errorf("failed to add console to kqueue: %w", err)
	}

	// networkKqueueConsole.DisableEcho()

	// WARNING: leaving this here for debugging purposes, but when we enable it we break the
	// entire forward of the terminal output
	// if err := consoleInstance.DisableEcho(); err != nil {
	// 	slog.Error("failed to disable echo", "error", err)
	// }

	// WARNING: leaving this here for debugging purposes, but when we enable it we break the
	// structure of the terminal output
	// if err := console.ClearONLCR(ptyFile.Fd()); err != nil {
	// 	slog.Error("failed to clear ONLCR on master", "error", err)
	// }

	adapter.wg.Add(2)

	// Network -> PTY (guest pty stderr/stdout -> host pty)
	go func() {
		defer adapter.wg.Done()
		<-conn.DebugCopy(ctx, "network(read)->kqueue(write)", networkKqueueConsole, adapter.conn)
		adapter.conn.Close()
		// adapter.ptyFile.Close() // Signal EOF to container
	}()

	// PTY -> Network (stdout/stderr from container to network)
	go func() {
		defer adapter.wg.Done()
		<-conn.DebugCopy(ctx, "kqueue(read)->network(write)", adapter.conn, networkKqueueConsole)
		adapter.conn.Close()
		// adapter.ptyFile.Close() // Signal EOF to container
	}()

	// go func() {
	// 	adapter.wg.Wait()
	// 	adapter.conn.Close()
	// 	adapter.console.Close()
	// }()

	return adapter, nil
}

// Console returns the containerd console interface

// PTYSlavePath returns the path to the PTY slave (for runc --console-socket)
func (a *RemoteConsole) TTYPath() string {
	return a.ttyFile.Name()
}

// Close shuts down the adapter
func (a *RemoteConsole) Close() error {
	close(a.done)

	// Close in order: network conn, then PTY
	if err := a.conn.Close(); err != nil {
		slog.Error("error closing network connection", "error", err)
	}

	if err := a.console.Close(); err != nil {
		slog.Error("error closing console", "error", err)
	}

	// Wait for goroutines to finish
	a.wg.Wait()

	return nil
}

// DisableEcho implements runtime.RuntimeConsole.
func (r *RemoteConsole) DisableEcho() error {
	return r.console.DisableEcho()
}

// Fd implements runtime.RuntimeConsole.
func (r *RemoteConsole) Fd() uintptr {
	return r.console.Fd()
}

// Name implements runtime.RuntimeConsole.
func (r *RemoteConsole) Name() string {
	return r.console.Name()
}

// Read implements runtime.RuntimeConsole.
func (r *RemoteConsole) Read(p []byte) (n int, err error) {
	n, err = r.console.Read(p)
	slog.DebugContext(r.creationCtx, "REMOTE_CONSOLE_ADAPTER:READ", "data", string(p[:n]), "err", err, "console_type", reflect.TypeOf(r.console))
	return n, err
}

// Reset implements runtime.RuntimeConsole.
func (r *RemoteConsole) Reset() error {
	return r.console.Reset()
}

// Resize implements runtime.RuntimeConsole.
func (r *RemoteConsole) Resize(sz console.WinSize) error {
	slog.DebugContext(r.creationCtx, "SHIM:REMOTE_CONSOLE:RESIZE", "sz", sz)
	if err := r.console.Resize(sz); err != nil {
		return err
	}

	slog.DebugContext(r.creationCtx, "SHIM:REMOTE_CONSOLE:RESIZE:AFTER", "sz", sz, "r.resizeFunc", r.resizeFunc != nil)

	if r.resizeFunc != nil {
		slog.DebugContext(r.creationCtx, "SHIM:REMOTE_CONSOLE:RESIZE", "sz", sz)
		return r.resizeFunc(r.creationCtx, sz)
	}

	return nil
}

// ResizeFrom implements runtime.RuntimeConsole.
func (r *RemoteConsole) ResizeFrom(rc console.Console) error {
	return r.console.ResizeFrom(rc)
}

// SetRaw implements runtime.RuntimeConsole.
func (r *RemoteConsole) SetRaw() error {
	return r.console.SetRaw()
}

// Size implements runtime.RuntimeConsole.
func (r *RemoteConsole) Size() (console.WinSize, error) {
	return r.console.Size()
}

// Write implements runtime.RuntimeConsole.
func (r *RemoteConsole) Write(p []byte) (n int, err error) {
	n, err = r.console.Write(p)
	slog.DebugContext(r.creationCtx, "REMOTE_CONSOLE_ADAPTER:WRITE", "data", string(p[:n]), "err", err, "console_type", reflect.TypeOf(r.console))
	return n, err
}
