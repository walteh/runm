package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	gorunc "github.com/containerd/go-runc"
	"github.com/walteh/runm/pkg/psnotify"
	"gitlab.com/tozd/go/errors"
)

func init() {
	err := setSubreaper()
	if err != nil {
		slog.Error("failed to set subreaper", "error", err)
	}
}

type pidInfo struct {
	pid      int
	cgroup   string
	argv     []string
	argc     string
	parent   *pidInfo
	children []*pidInfo
}

var pidToCgroup = struct {
	sync.RWMutex
	m map[int]*pidInfo
}{m: make(map[int]*pidInfo)}

func (r *runmLinuxInit) runPsnotify(ctx context.Context, exitChan chan gorunc.Exit) error {
	err := setSubreaper()
	if err != nil {
		slog.ErrorContext(ctx, "failed to set subreaper", "error", err)
	}
	watcher, err := psnotify.NewWatcher()
	if err != nil {
		return errors.Errorf("failed to create psnotify watcher: %w", err)
	}

	watcher.Watch(os.Getpid(), psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT)

	for {
		select {
		case forkEv := <-watcher.Fork:
			parent := int(forkEv.ParentPid)
			child := int(forkEv.ChildPid)

			pidToCgroup.RLock()
			parentInfo := pidToCgroup.m[parent]
			pidToCgroup.RUnlock()

			// if parent was in a tracked group, or child itself belongs there, watch it:
			// if grp := lookupCgroup(child); grp != "" {
			// 	pidToCgroup.Lock()
			// 	pidToCgroup.m[child] = grp
			// 	pidToCgroup.Unlock()
			// 	if err := watcher.Watch(child, psnotify.PROC_EVENT_EXIT|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_FORK); err != nil {
			// 		slog.ErrorContext(ctx, "failed to watch child", "error", err)
			// 	}
			// } else {
			info, err := r.getPidInfo(child)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get cgroup from proc", "error", err)
				continue
			}
			pidToCgroup.Lock()
			info.parent = parentInfo
			pidToCgroup.m[child] = info
			if parentInfo != nil {
				parentInfo.children = append(parentInfo.children, info)
			}
			pidToCgroup.Unlock()

			parentArgc := ""
			if parentInfo != nil {
				parentArgc = parentInfo.argc
			}

			slog.DebugContext(ctx, "PSNOTIFY[FORK]", "parent", parent, "child", child, "parent_argc", parentArgc, "child_argc", info.argc)

			if err := watcher.Watch(child, psnotify.PROC_EVENT_EXIT); err != nil {
				slog.ErrorContext(ctx, "failed to watch child", "error", err)
			}
			// }
		case status := <-watcher.Exit:
			pid := int(status.Pid)
			pidToCgroup.RLock()
			grp := pidToCgroup.m[pid]
			pidToCgroup.RUnlock()

			attrs := []slog.Attr{}

			if grp.parent != nil {
				attrs = append(attrs, slog.Group("parent",
					slog.Int("pid", grp.parent.pid),
					slog.String("cgroup", grp.parent.cgroup),
					// slog.String("argv", strings.Join(grp.parent.argv, " ")),
					slog.String("argc", grp.parent.argc),
				))
			}

			attrs = append(attrs, slog.Group("children",
				slog.Int("count", len(grp.children)),
			))

			attrs = append(attrs, slog.Int("pid", pid),
				slog.String("cgroup", grp.cgroup),
				// slog.String("argv", strings.Join(grp.argv, " ")),
				slog.String("argc", grp.argc),
				slog.Time("timestamp", status.Timestamp),
				slog.Int("exitCode", status.ExitCode),
				slog.Int("exitSignal", int(status.ExitSignal)),
			)

			slog.LogAttrs(ctx, slog.LevelDebug, "PSNOTIFY[EXIT]", attrs...)

			go func() {
				err := waitByPidfd(pid)
				if err != nil {
					attrs = append(attrs, slog.String("error", err.Error()))
					slog.LogAttrs(ctx, slog.LevelError, "failed to wait for process", attrs...)
					return
				}
				slog.LogAttrs(ctx, slog.LevelDebug, "PSNOTIFY[IO_CLOSED]", attrs...)

				exitChan <- gorunc.Exit{
					Pid:       pid,
					Status:    int(status.ExitCode),
					Timestamp: status.Timestamp,
				}
			}()

			// Clean up
			watcher.RemoveWatch(pid)
			pidToCgroup.Lock()
			delete(pidToCgroup.m, pid)
			pidToCgroup.Unlock()
		case execEv := <-watcher.Exec:
			pid := int(execEv.Pid)
			slog.DebugContext(ctx, "PSNOTIFY[EXEC]", "pid", pid)
			// TODO: your gRPC call here, e.g. reportExec(pid)
		case err := <-watcher.Error:
			slog.ErrorContext(ctx, "PSNOTIFY[ERROR]", "error", err)
		}
	}
}

// lookupCgroup checks our map in case we already recorded it on fork

func (r *runmLinuxInit) getPidInfo(pid int) (*pidInfo, error) {
	info := &pidInfo{pid: pid}
	info.cgroup, _ = getCgroupFromProc(pid)
	info.argv = getArgvFromProc(pid)
	info.argc, _ = getExePath(pid)
	return info, nil
}

func getArgvFromProc(pid int) []string {
	f, err := os.Open(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil
	}
	defer f.Close()

	out := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\000")
		out = append(out, parts...)
	}
	return out
}

// getExePath returns the absolute path of the binary for pid,
// by resolving the /proc/<pid>/exe symlink.
func getExePath(pid int) (string, error) {
	exeLink := fmt.Sprintf("/proc/%d/exe", pid)
	return os.Readlink(exeLink)
}

// getCgroupFromProc reads /proc/<pid>/cgroup and returns
// the first cgroup path it finds (e.g. "/services/web").
func getCgroupFromProc(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	// Each line is “hierarchy-ID:subsystems:cgroup-path”
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		// parts[2] is the cgroup path (with leading “/”).
		return parts[2], nil
	}
	return "", nil
}

// grpInList returns true if grp is one of our trackedGroups
// func grpInList(grp string) bool {
// 	for _, g := range trackedGroups {
// 		if g == grp {
// 			return true
// 		}
// 	}
// 	return false
// }

// parseInt is a tiny helper to convert a string PID to int (returns 0 on error)
func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
