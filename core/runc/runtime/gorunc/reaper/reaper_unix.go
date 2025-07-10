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

package reaper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	runc "github.com/containerd/go-runc"
	"github.com/walteh/runm/core/runc/runtime/gorunc/piddebug"
	"github.com/walteh/runm/pkg/ticker"
	"golang.org/x/sys/unix"
)

// ErrNoSuchProcess is returned when the process no longer exists
var ErrNoSuchProcess = errors.New("no such process")

const bufferSize = 32

type subscriber struct {
	sync.Mutex
	c chan runc.Exit
	// done   chan struct{}
	closed bool
	// cmd    *exec.Cmd
}

// func (s *subscriber) wait() {
// 	<-s.done
// }

func (s *subscriber) close() {
	s.Lock()
	if s.closed {
		s.Unlock()
		return
	}
	close(s.c)
	// close(s.done)
	s.closed = true
	s.Unlock()
}

func (s *subscriber) do(fn func()) {
	s.Lock()
	fn()
	s.Unlock()
}

// Reap should be called when the process receives an SIGCHLD.  Reap will reap
// all exited processes and close their wait channels
func Reap() error {
	now := time.Now()
	exits, err := reap(false)
	slog.InfoContext(context.Background(), "reaped", "exits", exits, "err", err)
	for _, e := range exits {
		done := Default.notify(runc.Exit{
			Timestamp: now,
			Pid:       e.Pid,
			Status:    e.Status,
		})

		select {
		case <-done:
		case <-time.After(1 * time.Second):
		}
	}
	return err
}

// Default is the default monitor initialized for the package
var Default = &Monitor{
	subscribers: make(map[chan runc.Exit]*subscriber),
}

// Monitor monitors the underlying system for process status changes
type Monitor struct {
	sync.Mutex

	subscribers map[chan runc.Exit]*subscriber
}

// Start starts the command and registers the process with the reaper
func (m *Monitor) Start(c *exec.Cmd) (chan runc.Exit, error) {
	ec := m.Subscribe()
	slog.Info(fmt.Sprintf("REAPER:START:STARTING[%d]", -1))

	if err := c.Start(); err != nil {
		slog.Info(fmt.Sprintf("REAPER:START:ERROR[%d]", -1), "error", err)
		m.Unsubscribe(ec)
		return nil, err
	}
	slog.Info(fmt.Sprintf("REAPER:START:STARTED[%d]", c.Process.Pid))
	return ec, nil
}

// StartLocked starts the command and registers the process with the reaper
func (m *Monitor) StartLocked(c *exec.Cmd) (chan runc.Exit, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return m.Start(c)
}

// Wait blocks until a process is signal as dead.
// User should rely on the value of the exit status to determine if the
// command was successful or not.
func (m *Monitor) Wait(c *exec.Cmd, ec chan runc.Exit) (int, error) {
	defer waitTicker(c).RunAsDefer()()

	slog.Info(fmt.Sprintf("REAPER:WAIT:STARTED[%d]", c.Process.Pid))

	for e := range ec {
		slog.Debug(fmt.Sprintf("REAPER:WAIT:RECEIVED[%d]", e.Pid))

		if e.Pid == c.Process.Pid {
			// make sure we flush all IO
			slog.Debug(fmt.Sprintf("REAPER:WAIT:FLUSHING[%d]", c.Process.Pid))
			os.Setenv("EXEC_DEBUG", "")
			c.Wait()
			slog.Debug(fmt.Sprintf("REAPER:WAIT:FLUSHED[%d]", c.Process.Pid))
			m.Unsubscribe(ec)
			return e.Status, nil
		}
	}
	// return no such process if the ec channel is closed and no more exit
	// events will be sent
	return -1, ErrNoSuchProcess
}

func waitTicker(c *exec.Cmd) *ticker.Ticker {
	return ticker.NewTicker(
		ticker.WithSlogBaseContext(context.Background()),
		ticker.WithLogLevel(slog.LevelDebug),
		ticker.WithFrequency(15),
		ticker.WithCallerSkip(2),
		ticker.WithStartBurst(5),
		ticker.WithAttrFunc(func() []slog.Attr {
			attrs := []slog.Attr{}

			if c != nil {
				attrs = append(attrs, slog.String("cmd", c.String()))
				// check io readers
				if c.Stdout != nil {
					grpAttrs := []slog.Attr{}
					grpAttrs = append(grpAttrs, slog.String("type", fmt.Sprintf("%T", c.Stdout)))
					if file, ok := c.Stdout.(*os.File); ok {
						grpAttrs = append(grpAttrs, slog.String("path", file.Name()))
						grpAttrs = append(grpAttrs, slog.String("fd", fmt.Sprintf("%v", file.Fd())))
						stat, err := file.Stat()
						if err == nil {
							grpAttrs = append(grpAttrs, slog.String("mode", fmt.Sprintf("%v", stat.Mode())))
							grpAttrs = append(grpAttrs, slog.String("size", fmt.Sprintf("%v", stat.Size())))
							grpAttrs = append(grpAttrs, slog.String("modtime", fmt.Sprintf("%v", stat.ModTime())))
							grpAttrs = append(grpAttrs, slog.String("isdir", fmt.Sprintf("%v", stat.IsDir())))
						}
					}
					attrs = append(attrs, slog.GroupAttrs("stdout", grpAttrs...))
				}
				if c.Stderr != nil {
					grpAttrs := []slog.Attr{}
					grpAttrs = append(grpAttrs, slog.String("type", fmt.Sprintf("%T", c.Stderr)))
					if file, ok := c.Stderr.(*os.File); ok {
						grpAttrs = append(grpAttrs, slog.String("path", file.Name()))
						grpAttrs = append(grpAttrs, slog.String("fd", fmt.Sprintf("%v", file.Fd())))
						stat, err := file.Stat()
						if err == nil {
							grpAttrs = append(grpAttrs, slog.String("mode", fmt.Sprintf("%v", stat.Mode())))
							grpAttrs = append(grpAttrs, slog.String("size", fmt.Sprintf("%v", stat.Size())))
							grpAttrs = append(grpAttrs, slog.String("modtime", fmt.Sprintf("%v", stat.ModTime())))
							grpAttrs = append(grpAttrs, slog.String("isdir", fmt.Sprintf("%v", stat.IsDir())))
						}
					}
					attrs = append(attrs, slog.GroupAttrs("stderr", grpAttrs...))
				}
				if c.Stdin != nil {
					grpAttrs := []slog.Attr{}
					grpAttrs = append(grpAttrs, slog.String("type", fmt.Sprintf("%T", c.Stdin)))
					if file, ok := c.Stdin.(*os.File); ok {
						grpAttrs = append(grpAttrs, slog.String("path", file.Name()))
						grpAttrs = append(grpAttrs, slog.String("fd", fmt.Sprintf("%v", file.Fd())))
						stat, err := file.Stat()
						if err == nil {
							grpAttrs = append(grpAttrs, slog.String("mode", fmt.Sprintf("%v", stat.Mode())))
							grpAttrs = append(grpAttrs, slog.String("size", fmt.Sprintf("%v", stat.Size())))
							grpAttrs = append(grpAttrs, slog.String("modtime", fmt.Sprintf("%v", stat.ModTime())))
							grpAttrs = append(grpAttrs, slog.String("isdir", fmt.Sprintf("%v", stat.IsDir())))
						}
					}
					attrs = append(attrs, slog.GroupAttrs("stdin", grpAttrs...))
				}

				if c.Process != nil {
					attrs = append(attrs, slog.Int("pid", c.Process.Pid))
					attrs = append(attrs, slog.GroupAttrs("piddebug", piddebug.BigPidAttrFunc(c.Process.Pid)...))
				}
				if c.ProcessState != nil {
					attrs = append(attrs, slog.Int("status", c.ProcessState.ExitCode()))
					attrs = append(attrs, slog.Any("signal", c.ProcessState.Sys().(syscall.WaitStatus).Signal()))
				}
			}
			return attrs
		}),
		ticker.WithMessageFunc(func() string {
			return fmt.Sprintf("REAPER:WAIT:%d[RUNNING]", c.Process.Pid)
		}),
	)
}

// WaitTimeout is used to skip the blocked command and kill the left process.
func (m *Monitor) WaitTimeout(c *exec.Cmd, ec chan runc.Exit, timeout time.Duration) (int, error) {
	type exitStatusWrapper struct {
		status int
		err    error
	}

	// capacity can make sure that the following goroutine will not be
	// blocked if there is no receiver when timeout.
	waitCh := make(chan *exitStatusWrapper, 1)
	go func() {
		defer close(waitCh)

		status, err := m.Wait(c, ec)
		waitCh <- &exitStatusWrapper{
			status: status,
			err:    err,
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-timer.C:
		syscall.Kill(c.Process.Pid, syscall.SIGKILL)
		return 0, fmt.Errorf("timeout %v for cmd(pid=%d): %s, %s", timeout, c.Process.Pid, c.Path, c.Args)
	case res := <-waitCh:
		return res.status, res.err
	}
}

// Subscribe to process exit changes
func (m *Monitor) Subscribe() chan runc.Exit {
	c := make(chan runc.Exit, bufferSize)
	m.Lock()
	s := &subscriber{
		c: c,
		// cmd:  cmd,
		// done: make(chan struct{}),
	}
	m.subscribers[c] = s
	m.Unlock()

	// if cmd != nil {
	// 	go s.runTicker()
	// }
	return c
}

// Unsubscribe to process exit changes
func (m *Monitor) Unsubscribe(c chan runc.Exit) {
	m.Lock()
	s, ok := m.subscribers[c]
	if !ok {
		m.Unlock()
		return
	}
	s.close()
	delete(m.subscribers, c)
	m.Unlock()
}

func (m *Monitor) getSubscribers() map[chan runc.Exit]*subscriber {
	out := make(map[chan runc.Exit]*subscriber)
	m.Lock()
	for k, v := range m.subscribers {
		out[k] = v
	}
	m.Unlock()
	return out
}

func (m *Monitor) notify(e runc.Exit) chan struct{} {
	const timeout = 1 * time.Millisecond
	var (
		done    = make(chan struct{}, 1)
		timer   = time.NewTimer(timeout)
		success = make(map[chan runc.Exit]struct{})
	)
	stop(timer, true)

	go func() {
		defer close(done)

		for {
			var (
				failed      int
				subscribers = m.getSubscribers()
			)
			for _, s := range subscribers {
				s.do(func() {
					if s.closed {
						return
					}
					if _, ok := success[s.c]; ok {
						return
					}
					timer.Reset(timeout)
					recv := true
					select {
					case s.c <- e:
						success[s.c] = struct{}{}
					case <-timer.C:
						recv = false
						failed++
					}
					stop(timer, recv)
				})
			}
			// all subscribers received the message
			if failed == 0 {
				return
			}
		}
	}()
	return done
}

func stop(timer *time.Timer, recv bool) {
	if !timer.Stop() && recv {
		<-timer.C
	}
}

// exit is the wait4 information from an exited process
type exit struct {
	Pid    int
	Status int
}

// reap reaps all child processes for the calling process and returns their
// exit information
func reap(wait bool) (exits []exit, err error) {
	var (
		ws  unix.WaitStatus
		rus unix.Rusage
	)
	flag := unix.WNOHANG
	if wait {
		flag = 0
	}
	for {
		pid, err := unix.Wait4(-1, &ws, flag, &rus)
		if err != nil {
			if err == unix.ECHILD {
				return exits, nil
			}
			return exits, err
		}
		if pid <= 0 {
			return exits, nil
		}
		exits = append(exits, exit{
			Pid:    pid,
			Status: exitStatus(ws),
		})
	}
}

const exitSignalOffset = 128

// exitStatus returns the correct exit status for a process based on if it
// was signaled or exited cleanly
func exitStatus(status unix.WaitStatus) int {
	if status.Signaled() {
		return exitSignalOffset + int(status.Signal())
	}
	return status.ExitStatus()
}
