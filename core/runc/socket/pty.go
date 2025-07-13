package socket

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/containerd/console"
	"github.com/creack/pty"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/pkg/conn"
)

// var _ runtime.RuntimeConsole = &PTYConsoleAdapter{}

// PTYConsoleAdapter wraps a ReadWriteCloser and presents it as a containerd/console
type PTYConsoleAdapter struct {
	conn      io.ReadWriteCloser // Your network connection
	ptyMaster *os.File           // PTY master side
	ptySlave  *os.File           // PTY slave path
	console   console.Console    // containerd console
	wg        sync.WaitGroup
	done      chan struct{}
}

// NewPTYConsoleAdapter creates a new adapter that bridges your ReadWriteCloser to a containerd console
func NewRemotePTYConsoleAdapter(ctx context.Context, wrcon io.ReadWriteCloser) (console.Console, error) {
	// Create a PTY pair using creack/pty
	ptyMaster, ptySlave, err := pty.Open()
	if err != nil {
		return nil, errors.Errorf("failed to create pty: %w", err)
	}

	// if err := console.ClearONLCR(ptySlave.Fd()); err != nil {
	// 	slog.Error("failed to clear ONLCR", "error", err)
	// }

	// set raw mode

	// Create containerd console from the PTY master
	consoleInstance, err := console.ConsoleFromFile(ptyMaster)
	if err != nil {
		ptyMaster.Close()
		ptySlave.Close()
		return nil, errors.Errorf("failed to create console: %w", err)
	}

	// if err := consoleInstance.SetRaw(); err != nil {
	// 	slog.Error("failed to set raw mode", "error", err)
	// }

	adapter := &PTYConsoleAdapter{
		conn:      wrcon,
		ptyMaster: ptyMaster,
		ptySlave:  ptySlave,
		console:   consoleInstance,
		done:      make(chan struct{}),
	}

	adapter.wg.Add(2)

	// Network -> PTY (stdin from network to container)
	go func() {
		defer adapter.wg.Done()
		<-conn.DebugCopy(ctx, "network(read)->pty(write)", consoleInstance, adapter.conn)
		adapter.ptyMaster.Close() // Signal EOF to container
	}()

	// PTY -> Network (stdout/stderr from container to network)
	go func() {
		defer adapter.wg.Done()
		<-conn.DebugCopy(ctx, "pty(read)->network(write)", adapter.conn, consoleInstance)
	}()
	return consoleInstance, nil
}

// Console returns the containerd console interface
func (a *PTYConsoleAdapter) Console() console.Console {
	return a.console
}

// PTYSlavePath returns the path to the PTY slave (for runc --console-socket)
func (a *PTYConsoleAdapter) PTYSlavePath() string {
	return a.ptySlave.Name()
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
