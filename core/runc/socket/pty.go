package socket

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/containerd/console"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/pkg/conn"
)

// var _ runtime.RuntimeConsole = &PTYConsoleAdapter{}

// PTYConsoleAdapter wraps a ReadWriteCloser and presents it as a containerd/console
type PTYConsoleAdapter struct {
	conn    io.ReadWriteCloser // Your network connection
	ptyFile *os.File           // PTY master side
	ttyFile *os.File           // TTY slave side
	console console.Console    // containerd console
	wg      sync.WaitGroup
	done    chan struct{}
}

// NewPTYConsoleAdapter creates a new adapter that bridges your ReadWriteCloser to a containerd console
func NewRemotePTYConsoleAdapter(ctx context.Context, wrcon io.ReadWriteCloser) (console.Console, error) {

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

	kqueueConsole, err := newConsole(consoleInstance)
	if err != nil {
		return nil, errors.Errorf("failed to add console to kqueue: %w", err)
	}

	// if err := consoleInstance.DisableEcho(); err != nil {
	// 	slog.Error("failed to disable echo", "error", err)
	// }

	// Clear ONLCR on both master and slave
	// if err := console.ClearONLCR(ttyFile.Fd()); err != nil {
	// 	slog.Error("failed to clear ONLCR on slave", "error", err)
	// }

	if err := console.ClearONLCR(ptyFile.Fd()); err != nil {
		slog.Error("failed to clear ONLCR on master", "error", err)
	}

	adapter := &PTYConsoleAdapter{
		conn:    wrcon,
		ttyFile: ttyFile,
		ptyFile: ptyFile,
		console: consoleInstance,
		done:    make(chan struct{}),
		wg:      sync.WaitGroup{},
	}

	adapter.wg.Add(2)

	// Network -> PTY (stdin from network to container)
	go func() {
		defer adapter.wg.Done()
		<-conn.DebugCopy(ctx, "network(read)->kqueue(write)", kqueueConsole, adapter.conn)
		// adapter.ptyFile.Close()
		// adapter.ptyFile.Close() // Signal EOF to container
	}()

	// PTY -> Network (stdout/stderr from container to network)
	go func() {
		defer adapter.wg.Done()
		<-conn.DebugCopy(ctx, "kqueue(read)->network(write)", adapter.conn, kqueueConsole)
	}()
	return adapter.console, nil
}

// Console returns the containerd console interface
func (a *PTYConsoleAdapter) Console() console.Console {
	return a.console
}

// PTYSlavePath returns the path to the PTY slave (for runc --console-socket)
func (a *PTYConsoleAdapter) PTYSlavePath() string {
	return a.ttyFile.Name()
}

// Close shuts down the adapter
func (a *PTYConsoleAdapter) Close() error {
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

// disableEchoManually disables echo on a PTY file descriptor using direct unix syscalls
func disableEchoManually(fd uintptr) error {
	// Get current terminal attributes
	var termios unix.Termios
	err := tcget(fd, &termios)
	if err != nil {
		return err
	}

	// Disable echo flags: ECHO, ECHOE, ECHOK, ECHONL
	termios.Lflag &^= unix.ECHO | unix.ECHOE | unix.ECHOK | unix.ECHONL

	// Set the modified attributes
	err = tcset(fd, &termios)
	if err != nil {
		return err
	}

	return nil
}

func tcget(fd uintptr, p *unix.Termios) error {
	termios, err := unix.IoctlGetTermios(int(fd), cmdTcGet)
	if err != nil {
		return err
	}
	*p = *termios
	return nil
}

func tcset(fd uintptr, p *unix.Termios) error {
	return unix.IoctlSetTermios(int(fd), cmdTcSet, p)
}

// const (
// 	cmdTcGet = unix.TIOCGETA
// 	cmdTcSet = unix.TIOCSETA
// )

// // Example usage:
// func ExampleUsage(networkConn io.ReadWriteCloser) {
// 	// Create the adapter
// 	adapter, err := NewPTYConsoleAdapter(networkConn)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer adapter.Close()

// 	// Now you can use adapter.Console() with containerd
// 	consoleInstance := adapter.Console()

// 	// Use console with containerd APIs...
// 	// For example, resize the terminal:
// 	if err := consoleInstance.Resize(console.WinSize{Height: 24, Width: 80}); err != nil {
// 		log.Printf("Failed to resize: %v", err)
// 	}
// }
