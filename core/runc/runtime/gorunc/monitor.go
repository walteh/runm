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

package goruncruntime

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	gorunc "github.com/containerd/go-runc"
	"github.com/walteh/runm/pkg/ticker"
)

// Monitor is the default ProcessMonitor for handling runc process exit
// var goRuncProcessMonitor ProcessMonitor = &defaultMonitor{}

// ProcessMonitor is an interface for process monitoring.
//
// It allows daemons using go-runc to have a SIGCHLD handler
// to handle exits without introducing races between the handler
// and go's exec.Cmd.
//
// ProcessMonitor also provides a StartLocked method which is similar to
// Start, but locks the goroutine used to start the process to an OS thread
// (for example: when Pdeathsig is set).
type ProcessMonitor interface {
	Start(*exec.Cmd) (chan gorunc.Exit, error)
	StartLocked(*exec.Cmd) (chan gorunc.Exit, error)
	Wait(*exec.Cmd, chan gorunc.Exit) (int, error)
}

type defaultMonitor struct {
	forwardTo []chan gorunc.Exit
}

var defaultMonitorInstance *defaultMonitor = &defaultMonitor{
	forwardTo: make([]chan gorunc.Exit, 0),
}

func (m *defaultMonitor) Start(c *exec.Cmd) (chan gorunc.Exit, error) {
	if err := c.Start(); err != nil {
		return nil, err
	}
	ec := make(chan gorunc.Exit, 1)
	go func() {

		stillGoing := true
		defer func() {
			stillGoing = false
		}()
		var status int
		slog.Debug("DEBUG: monitor.Start after Start")

		go func() {
			for stillGoing {
				time.Sleep(1 * time.Second)
				slog.Debug("DEBUG: monitor.Start still waiting", "pid", c.Process.Pid, "status", c.ProcessState)
			}
		}()

		if err := c.Wait(); err != nil {
			status = 255
			if exitErr, ok := err.(*exec.ExitError); ok {
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					status = ws.ExitStatus()
				}
			}
		}
		event := gorunc.Exit{
			Timestamp: time.Now(),
			Pid:       c.Process.Pid,
			Status:    status,
		}
		slog.Debug("DEBUG: monitor.Start after Wait", "event", event)
		if m.forwardTo != nil {
			for _, ch := range m.forwardTo {
				go func() {
					ch <- event
				}()
			}
		}
		ec <- event
		close(ec)
	}()
	return ec, nil
}

// StartLocked is like Start, but locks the goroutine used to start the process to
// the OS thread for use-cases where the parent thread matters to the child process
// (for example: when Pdeathsig is set).
func (m *defaultMonitor) StartLocked(c *exec.Cmd) (chan gorunc.Exit, error) {
	started := make(chan error)
	ec := make(chan gorunc.Exit, 1)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("DEBUG: monitor.StartLocked panic", "error", err)
			}
		}()

		defer ticker.NewTicker(
			ticker.WithMessage("MONITOR:STARTLOCKED[RUNNING]"),
			ticker.WithDoneMessage("MONITOR:STARTLOCKED[DONE]"),
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithStartBurst(5),
		).RunAsDefer()()

		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		slog.Debug("DEBUG: monitor.StartLocked after LockOSThread")
		if err := c.Start(); err != nil {
			started <- err
			return
		}
		close(started)
		var status int
		// the prob is that this process never returns for some reason
		slog.Debug("DEBUG: monitor.StartLocked process info before Wait", "pid", c.Process.Pid, "state", c.ProcessState)

		// Debug process relationships
		debugProcessInfo(c.Process.Pid)

		if err := c.Wait(); err != nil {
			slog.Debug("DEBUG: monitor.StartLocked after Wait", "error", err, "pid", c.Process.Pid)
			status = 255
			if exitErr, ok := err.(*exec.ExitError); ok {
				slog.Debug("DEBUG: monitor.StartLocked exit error details", "exitErr", exitErr, "type", exitErr.Sys(), "pid", c.Process.Pid)
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					status = ws.ExitStatus()
				}
			}
		}
		// }
		slog.Debug("DEBUG: monitor.StartLocked after Wait", "status", status, "pid", c.Process.Pid)
		event := gorunc.Exit{
			Timestamp: time.Now(),
			Pid:       c.Process.Pid,
			Status:    status,
		}
		if m.forwardTo != nil {
			for _, ch := range m.forwardTo {
				go func() {
					ch <- event
				}()
			}
		}
		ec <- event

		slog.Info("DEBUG: monitor.StartLocked after Wait channel send", "pid", c.Process.Pid, "status", status)
		close(ec)
	}()

	if err := <-started; err != nil {
		return nil, err
	}
	return ec, nil
}

func (m *defaultMonitor) Wait(c *exec.Cmd, ec chan gorunc.Exit) (int, error) {
	slog.Debug("DEBUG: monitor.Wait before")
	e := <-ec
	slog.Debug("DEBUG: monitor.Wait after", "exit", e)
	return e.Status, nil
}

// Debug helper to check process relationships
func debugProcessInfo(pid int) {
	// Get process group
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		slog.Debug("DEBUG: Failed to get pgid", "pid", pid, "error", err)
	} else {
		slog.Debug("DEBUG: Process group info", "pid", pid, "pgid", pgid)
	}

	// Try to get parent-child relationships from /proc
	ppidBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		slog.Debug("DEBUG: Failed to read /proc/pid/stat", "pid", pid, "error", err)
		return
	}

	parts := strings.Fields(string(ppidBytes))
	if len(parts) > 3 {
		// Field 4 in /proc/pid/stat is ppid
		ppid, _ := strconv.Atoi(parts[3])
		slog.Debug("DEBUG: Process parent info", "pid", pid, "ppid", ppid)
	}

	// Check for any children
	cmd := exec.Command("pgrep", "-P", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err == nil {
		children := strings.Split(strings.TrimSpace(string(output)), "\n")
		slog.Debug("DEBUG: Process children", "pid", pid, "children", children)
	}
}
