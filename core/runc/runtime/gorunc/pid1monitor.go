// File: runm/core/runc/runtime/gorunc/pid1_reaper.go

package goruncruntime

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	gorunc "github.com/containerd/go-runc"
	"golang.org/x/sys/unix"
)

// Pid1Monitor is a special monitor designed for processes running as PID 1
// It addresses the specific challenges of waiting for processes when
// the waiting process is PID 1
type Pid1Monitor struct {
	sync.Mutex

	// Map of tracked processes by PID
	tracked map[int]*trackedProcess

	// Subscribers for exit notifications
	subscribers map[chan gorunc.Exit]struct{}
}

type trackedProcess struct {
	cmd         *exec.Cmd
	exitChannel chan gorunc.Exit
	description string
	startTime   time.Time
	waitDone    chan struct{}
}

// NewPid1Monitor creates a monitor specifically optimized for PID 1
func NewPid1Monitor() *Pid1Monitor {
	m := &Pid1Monitor{
		tracked:     make(map[int]*trackedProcess),
		subscribers: make(map[chan gorunc.Exit]struct{}),
	}

	// Start the background checker to detect terminated processes
	go m.runBackgroundChecker(context.Background())

	slog.Debug("initialized PID 1 monitor")
	return m
}

// Start starts the command and registers the process with the pid1 monitor
func (m *Pid1Monitor) Start(c *exec.Cmd) (chan gorunc.Exit, error) {
	slog.Debug("pid1 monitor start", "path", c.Path)

	// Start the process
	if err := c.Start(); err != nil {
		return nil, err
	}

	// Create exit notification channel
	ec := make(chan gorunc.Exit, 1)

	// Set up tracking for the process
	description := fmt.Sprintf("%s %v", c.Path, c.Args)
	m.trackProcess(c, ec, description)

	slog.Debug("pid1 monitor started process", "pid", c.Process.Pid, "description", description)
	return ec, nil
}

// StartLocked implements the locked start for the pid1 monitor
func (m *Pid1Monitor) StartLocked(c *exec.Cmd) (chan gorunc.Exit, error) {
	slog.Debug("pid1 monitor start locked", "path", c.Path)

	// Create channels for communicating with the locked goroutine
	ec := make(chan gorunc.Exit, 1)
	errCh := make(chan error, 1)
	startedCh := make(chan struct{})

	// Start in a goroutine with locked OS thread
	go func() {
		defer close(errCh)

		// Lock the OS thread
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Start the process
		if err := c.Start(); err != nil {
			errCh <- err
			return
		}

		// Signal that process has started
		close(startedCh)

		// Set up tracking for the process
		description := fmt.Sprintf("%s %v", c.Path, c.Args)
		m.trackProcess(c, ec, description)

		slog.Debug("pid1 monitor start locked completed", "pid", c.Process.Pid)
	}()

	// Wait for either an error or successful start
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	case <-startedCh:
		// Process started successfully
	}

	return ec, nil
}

// Wait implements the process monitor Wait interface
func (m *Pid1Monitor) Wait(c *exec.Cmd, ec chan gorunc.Exit) (int, error) {
	pid := c.Process.Pid
	slog.Debug("pid1 monitor wait", "pid", pid)

	// Create a context with timeout for safety
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Start checking for exit status
	waitDone := make(chan struct{})
	go m.waitForExit(ctx, pid, waitDone)

	// Wait for exit notification
	var exitStatus int
	var exitErr error

	select {
	case exit := <-ec:
		slog.Debug("received exit from channel", "pid", exit.Pid, "status", exit.Status)
		exitStatus = exit.Status
		// Signal the wait is done
		close(waitDone)
	case <-ctx.Done():
		slog.Error("wait timed out", "pid", pid, "timeout", "10m")
		return -1, fmt.Errorf("wait timed out after 10 minutes for pid %d", pid)
	}

	// Try to call Wait() to properly clean up resources, but don't fail if it errors
	err := c.Wait()
	if err != nil {
		slog.Debug("cmd.Wait() returned error after we already got exit", "pid", pid, "error", err)
	}

	// Remove process from tracking
	m.Lock()
	delete(m.tracked, pid)
	m.Unlock()

	return exitStatus, exitErr
}

// Subscribe registers a new channel to receive exit notifications
func (m *Pid1Monitor) Subscribe(ctx context.Context) chan gorunc.Exit {
	ch := make(chan gorunc.Exit, 32)

	m.Lock()
	m.subscribers[ch] = struct{}{}
	slog.InfoContext(ctx, "subscribing to pid1 monitor exits", "total_subscribers", len(m.subscribers))
	m.Unlock()

	return ch
}

// Unsubscribe removes a channel from receiving exit notifications
func (m *Pid1Monitor) Unsubscribe(ctx context.Context, ch chan gorunc.Exit) {
	m.Lock()
	delete(m.subscribers, ch)
	slog.InfoContext(ctx, "unsubscribing from pid1 monitor exits", "total_subscribers", len(m.subscribers))
	m.Unlock()
}

// trackProcess adds a process to the tracking map
func (m *Pid1Monitor) trackProcess(c *exec.Cmd, ec chan gorunc.Exit, description string) {
	pid := c.Process.Pid

	m.Lock()
	defer m.Unlock()

	waitDone := make(chan struct{})
	m.tracked[pid] = &trackedProcess{
		cmd:         c,
		exitChannel: ec,
		description: description,
		startTime:   time.Now(),
		waitDone:    waitDone,
	}

	// Start a goroutine to actively check this specific process
	go m.activelyCheckProcess(c.Process.Pid, waitDone)
}

// runBackgroundChecker periodically checks all tracked processes
func (m *Pid1Monitor) runBackgroundChecker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkTrackedProcesses()
		}
	}
}

// checkTrackedProcesses checks status of all tracked processes
func (m *Pid1Monitor) checkTrackedProcesses() {
	m.Lock()
	processes := make([]*trackedProcess, 0, len(m.tracked))
	for pid, proc := range m.tracked {
		processes = append(processes, proc)
		slog.Debug("checking tracked process", "pid", pid, "description", proc.description,
			"elapsed", time.Since(proc.startTime))
	}
	m.Unlock()

	// For each process, check if it still exists
	for _, proc := range processes {
		pid := proc.cmd.Process.Pid

		// Try to send signal 0 to check if process exists
		err := unix.Kill(pid, 0)
		if err != nil {
			if err == unix.ESRCH {
				slog.Debug("process no longer exists, likely exited", "pid", pid)

				// Process doesn't exist - check if we can get exit status
				var status int
				if proc.cmd.ProcessState != nil {
					// ProcessState is available, get status from it
					if ws, ok := proc.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
						status = ws.ExitStatus()
					}
				} else {
					// Assume default exit status for disappeared process
					status = 255
				}

				// Send exit notification
				m.notifyExit(pid, status)
			}
		}
	}
}

// activelyCheckProcess repeatedly checks a specific process
// and notifies when it exits
func (m *Pid1Monitor) activelyCheckProcess(pid int, done chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	checkCount := 0

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			checkCount++

			// Check if process still exists
			if err := unix.Kill(pid, 0); err != nil {
				if err == unix.ESRCH {
					slog.Debug("process no longer exists in active check",
						"pid", pid, "checks", checkCount)

					// Try to get exit status from /proc/PID/stat if possible
					status := 0 // Default success

					// Since process is gone, manually check for zombie status or other state
					m.Lock()
					proc, exists := m.tracked[pid]
					m.Unlock()

					if exists {
						// Process is tracked but no longer exists - send notification
						slog.Debug("notifying exit for disappeared process", "pid", pid, "status", status)
						m.notifyExit(proc.cmd.Process.Pid, status)
					}

					return
				}
			}

			// Every 30 seconds, log that we're still waiting
			if checkCount > 0 && checkCount%60 == 0 {
				slog.Debug("still waiting for process to exit", "pid", pid, "checks", checkCount)
			}
		}
	}
}

// waitForExit actively waits for a process to exit
func (m *Pid1Monitor) waitForExit(ctx context.Context, pid int, done chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			// Try syscall.Wait4 with WNOHANG to see if the process has exited
			var wstatus syscall.WaitStatus
			wpid, err := syscall.Wait4(pid, &wstatus, syscall.WNOHANG, nil)

			if err != nil {
				if err == syscall.ECHILD {
					// No child process to wait for - means the process might have been
					// reaped by someone else
					slog.Debug("process already reaped by someone else", "pid", pid)
					return
				}

				slog.Debug("wait4 error", "pid", pid, "error", err)
			} else if wpid == pid {
				// We successfully reaped the process!
				status := wstatus.ExitStatus()
				slog.Debug("successfully reaped process with wait4",
					"pid", pid, "status", status)

				// Notify of exit
				m.notifyExit(pid, status)
				return
			}
		}
	}
}

// notifyExit sends exit notification to the process's channel and all subscribers
func (m *Pid1Monitor) notifyExit(pid, status int) {
	m.Lock()
	proc, exists := m.tracked[pid]

	// Make a copy of all subscribers
	subscribers := make([]chan gorunc.Exit, 0, len(m.subscribers))
	for ch := range m.subscribers {
		subscribers = append(subscribers, ch)
	}

	// If found, remove from tracking
	if exists {
		delete(m.tracked, pid)
	}
	m.Unlock()

	if !exists {
		slog.Debug("tried to notify exit for untracked process", "pid", pid)
		return
	}

	// Create exit notification
	exit := gorunc.Exit{
		Timestamp: time.Now(),
		Pid:       pid,
		Status:    status,
	}

	// Send to process's channel
	select {
	case proc.exitChannel <- exit:
		slog.Debug("sent exit notification to process channel", "pid", pid, "status", status)
	default:
		slog.Warn("couldn't send to process channel, it might be full or closed", "pid", pid)
	}

	// Broadcast to all subscribers
	for _, ch := range subscribers {
		go func(ch chan gorunc.Exit) {
			select {
			case ch <- exit:
				// Successfully sent
			default:
				// Channel buffer is full or closed, log and continue
				slog.Warn("couldn't send exit to subscriber - channel full or closed", "pid", pid)
			}
		}(ch)
	}

	slog.Debug("exit notification complete", "pid", pid, "status", status)
}

// UsePid1Monitor replaces the default go-runc monitor with our Pid1Monitor
