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
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/containerd/errdefs"
)

const (
	// RuncRoot is the path to the root runc state directory
	RuncRoot = "/run/containerd/runc"
	// InitPidFile name of the file that contains the init pid
	InitPidFile = "init.pid"
)

// safePid is a thread safe wrapper for pid.
type safePid struct {
	mu  sync.Mutex
	pid int
}

func (s *safePid) get() int {
	s.Lock()
	defer s.Unlock()
	return s.pid
}

func (s *safePid) Lock() {
	tm := time.Now()
	lockLog(context.Background(), "safePid.Lock ATTEMPT")
	s.mu.Lock()
	lockLog(context.Background(), "safePid.Lock TAKEN", slog.Duration("duration", time.Since(tm)))
}

func (s *safePid) Unlock() {
	s.mu.Unlock()
	lockLog(context.Background(), "safePid.Unlock")
}

func lockLog(ctx context.Context, msg string, args ...slog.Attr) {
	ptr, _, _, _ := runtime.Caller(2)
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, msg, ptr)
	rec.AddAttrs(args...)
	slog.Default().Handler().Handle(ctx, rec)
}

// TODO(mlaventure): move to runc package?
// func getLastRuntimeError(ctx context.Context, r runtime.Runtime) (string, error) {
// 	logPath := filepath.Join(r.SharedDir(), runtime.LogFileBase)
// 	f, err := os.OpenFile(logPath, os.O_RDONLY, 0400)
// 	if err != nil {
// 		return "", err
// 	}
// 	defer f.Close()

// 	var (
// 		errMsg string
// 		log    struct {
// 			Level string
// 			Msg   string
// 			Time  time.Time
// 		}
// 	)

// 	dec := json.NewDecoder(f)
// 	for err = nil; err == nil; {
// 		if err = dec.Decode(&log); err != nil && err != io.EOF {
// 			return "", err
// 		}
// 		if log.Level == "error" {
// 			errMsg = strings.TrimSpace(log.Msg)
// 		}
// 	}

// 	return errMsg, nil
// }

// criuError returns only the first line of the error message from criu
// it tries to add an invalid dump log location when returning the message
func criuError(err error) string {
	parts := strings.Split(err.Error(), "\n")
	return parts[0]
}

func copyFile(to, from string) error {
	ff, err := os.Open(from)
	if err != nil {
		return err
	}
	defer ff.Close()
	tt, err := os.Create(to)
	if err != nil {
		return err
	}
	defer tt.Close()

	p := bufPool.Get().(*[]byte)
	defer bufPool.Put(p)
	_, err = io.CopyBuffer(tt, ff, *p)
	return err
}

func checkKillError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "os: process already finished") ||
		strings.Contains(err.Error(), "container not running") ||
		strings.Contains(strings.ToLower(err.Error()), "no such process") ||
		err == unix.ESRCH {
		return fmt.Errorf("process already finished: %w", errdefs.ErrNotFound)
	} else if strings.Contains(err.Error(), "does not exist") {
		return fmt.Errorf("no such container: %w", errdefs.ErrNotFound)
	}
	return fmt.Errorf("unknown error after kill: %w", err)
}

func newPidFile(bundle string) *pidFile {
	return &pidFile{
		path: filepath.Join(bundle, InitPidFile),
	}
}

func newExecPidFile(bundle, id string) *pidFile {
	return &pidFile{
		path: filepath.Join(bundle, fmt.Sprintf("%s.pid", id)),
	}
}

type pidFile struct {
	path string
}

func (p *pidFile) Path() string {
	return p.path
}

// func (p *pidFile) Read() (int, error) {
// 	path, err := p.runtime.ResolvePidFilePath(context.Background(), p.path)
// 	if err != nil {
// 		return -1, err
// 	}
// 	return p.runtime.ReadPidFile(context.Background(), path)
// }

// waitTimeout handles waiting on a waitgroup with a specified timeout.
// this is commonly used for waiting on IO to finish after a process has exited
func waitTimeout(ctx context.Context, wg *sync.WaitGroup, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func stateName(v interface{}) string {
	switch v.(type) {
	case *runningState, *execRunningState:
		return "running"
	case *createdState, *execCreatedState, *createdCheckpointState:
		return "created"
	case *pausedState:
		return "paused"
	case *deletedState:
		return "deleted"
	case *stoppedState:
		return "stopped"
	}
	panic(fmt.Errorf("invalid state %v", v))
}
