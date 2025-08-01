//go:build !windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/pkg/psnotify"
)

type pidInfo struct {
	pid      int
	pidfd    int
	cgroup   string
	argv     []string
	argc     string
	parent   *pidInfo
	children []*pidInfo

	exitEvent *psnotify.ProcEventExit
	execEvent *psnotify.ProcEventExec
	forkEvent *psnotify.ProcEventFork
}

func formattedCgroup(cgroup string) string {
	return strings.ReplaceAll(cgroup, containerId, "<container-id>")
}

func (p *pidInfo) id() string {
	switch {
	case p.pid == 1:
		return "init[1]"
	case filepath.Base(p.argc) == "busybox" && len(p.argv) > 0:
		if p.argv[0] == p.argc {
			if len(p.argv) > 1 {
				return fmt.Sprintf("busybox:%s[%d]", p.argv[1], p.pid)
			}
			return fmt.Sprintf("busybox[%d]", p.pid)
		}
		if len(p.argv) > 0 {
			return fmt.Sprintf("busybox:%s[%d]", p.argv[0], p.pid)
		}
		return fmt.Sprintf("busybox[%d]", p.pid)
	case filepath.Base(p.argc) == "runc" || filepath.Base(p.argc) == "runc-test":
		knownRuncNames := []string{"start", "create", "run", "init", "exec", "kill"}
		for _, name := range knownRuncNames {
			for _, arg := range p.argv {
				if arg == name {
					return fmt.Sprintf("runc:%s[%d]", name, p.pid)
				}
			}
		}
		return fmt.Sprintf("runc:unknown[%d]", p.pid)
	default:
		return fmt.Sprintf("%s[%d]", p.argc, p.pid)
	}
}

func (p *pidInfo) ArgvNoFlags() []string {
	argv := []string{}
	for _, arg := range p.argv {
		if !strings.HasPrefix(arg, "-") {
			argv = append(argv, arg)
		}
	}
	return argv
}

var _ slog.LogValuer = (*pidInfo)(nil)

func (p *pidInfo) LogValue() slog.Value {
	attrs := []slog.Attr{}

	if p.parent != nil {
		attrs = append(attrs, slog.Group("parent",
			slog.Int("pid", p.parent.pid),
			slog.String("cgroup", formattedCgroup(p.parent.cgroup)),
			slog.String("argc", p.parent.argc),
			slog.String("id", p.parent.id()),
			// slog.String("argv", strings.Join(p.parent.argv, " ")),
		))
	}

	myid := p.id()

	parentChain := []string{"init[1]"}
	for pz := p.parent; pz != nil; pz = pz.parent {
		parentChain = append(parentChain, pz.id())
	}
	parentChain = append(parentChain, myid)

	attrs = append(attrs, slog.String("chain", strings.Join(parentChain, " -> ")), slog.String("id", myid))

	attrs = append(attrs, slog.Group("children",
		slog.Int("count", len(p.children)),
	))

	attrs = append(attrs, slog.Int("pid", p.pid),
		slog.String("cgroup", formattedCgroup(p.cgroup)),
		slog.String("argc", p.argc),
		// slog.String("argv", strings.Join(p.argv, " ")),
	)

	if p.exitEvent != nil {
		exitGroup := slog.Group("exit",
			slog.Duration("dur", p.exitEvent.Timestamp.Sub(p.forkEvent.Timestamp)),
			slog.Int("exitCode", p.exitEvent.ExitCode),
			slog.Int("exitSignal", int(p.exitEvent.ExitSignal)),
		)
		attrs = append(attrs, exitGroup)
	}

	if p.execEvent != nil {
		execGroup := slog.Group("exec",
			slog.Duration("dur", p.execEvent.Timestamp.Sub(p.forkEvent.Timestamp)),
		)
		attrs = append(attrs, execGroup)
	}

	if p.forkEvent != nil {
		forkGroup := slog.Group("fork",
			slog.Time("timestamp", p.forkEvent.Timestamp),
			slog.Int("parentPid", int(p.forkEvent.ParentPid)),
			slog.Int("childPid", int(p.forkEvent.ChildPid)),
		)
		attrs = append(attrs, forkGroup)
	}

	return slog.GroupValue(attrs...)
}

var pidToCgroup = struct {
	sync.RWMutex
	m map[int]*pidInfo
}{m: make(map[int]*pidInfo)}

func (r *runmLinuxInit) runPsnotify(ctx context.Context, exitChan chan gorunc.Exit) error {

	watcher, err := psnotify.NewWatcher()
	if err != nil {
		return errors.Errorf("failed to create psnotify watcher: %w", err)
	}

	if err := watcher.Watch(os.Getpid(), psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT); err != nil {
		return errors.Errorf("failed to watch self: %w", err)
	}

	for {
		select {
		case forkEv := <-watcher.Fork:
			parent := int(forkEv.ParentPid)
			child := int(forkEv.ChildPid)

			pidToCgroup.RLock()
			parentInfo := pidToCgroup.m[parent]
			pidToCgroup.RUnlock()

			info, err := r.getPidInfo(child)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get cgroup from proc", "error", err)
				continue
			}
			info.forkEvent = forkEv
			pidToCgroup.Lock()
			_, exists := pidToCgroup.m[child]
			info.parent = parentInfo
			pidToCgroup.m[child] = info
			if parentInfo != nil {
				parentInfo.children = append(parentInfo.children, info)
			}
			pidToCgroup.Unlock()

			// parentArgc := ""
			// if parentInfo != nil {
			// 	parentArgc = parentInfo.argc
			// }

			if !exists {
				slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY:FORK[%d]", child), "info", info)
			}

			// slog.DebugContext(ctx, "PSNOTIFY[FORK]", "parent", parent, "child", child, "parent_argc", parentArgc, "child_argc", info.argc)

			if err := watcher.Watch(child, psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT); err != nil {
				slog.ErrorContext(ctx, "failed to watch child", "error", err)
			}

		case status := <-watcher.Exit:
			pid := int(status.Pid)
			pidToCgroup.Lock()
			grp := pidToCgroup.m[pid]
			if grp != nil {
				grp.exitEvent = status
			}
			pidToCgroup.Unlock()

			if grp == nil {
				slog.WarnContext(ctx, fmt.Sprintf("PSNOTIFY:EXIT-OF-ALREADY-REAPED[%d]", pid), "pid", pid)
				continue
			} else {
				grp.resolvePidData()
				slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY:REAP[%d]", pid), "info", grp)
			}

			// slog.Log(ctx, slog.LevelDebug, "PSNOTIFY[EXIT]", "info", grp)

			// go func() {

			// 	exitFd, err := getPidFd(pid)
			// 	if err == nil {
			// 		err := pidfdWait(exitFd)
			// 		if err != nil {
			// 			slog.ErrorContext(ctx, "failed to wait for process", "error", err, "info", grp)
			// 		}
			// 	}

			// 	slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY:REAP[%d]", pid), "info", grp)

			// 	exitChan <- gorunc.Exit{
			// 		Pid:       pid,
			// 		Status:    int(status.ExitCode),
			// 		Timestamp: status.Timestamp,
			// 	}

			// 	watcher.RemoveWatch(pid)
			// 	pidToCgroup.Lock()
			// 	delete(pidToCgroup.m, pid)
			// 	pidToCgroup.Unlock()

			// }()
			watcher.RemoveWatch(pid)
			pidToCgroup.Lock()
			delete(pidToCgroup.m, pid)
			pidToCgroup.Unlock()

			// Clean up

		case execEv := <-watcher.Exec:
			pid := int(execEv.Pid)
			pidToCgroup.RLock()
			grp := pidToCgroup.m[pid]
			pidToCgroup.RUnlock()

			if grp != nil {
				grp.resolvePidData()
				grp.execEvent = execEv

				// If this looks like a container init process after exec, ensure we're watching for forks
				// isContainerInit := grp.cgroup != "/" && grp.cgroup != "" && !strings.Contains(grp.argc, "runc")
				// if isContainerInit {
				// 	slog.InfoContext(ctx, fmt.Sprintf("PSNOTIFY:EXEC-CONTAINER-INIT[%d]", pid), "info", grp)
				// 	// Re-establish fork watching for container init processes
				// 	if err := watcher.Watch(pid, psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT); err != nil {
				// 		slog.ErrorContext(ctx, "failed to re-watch container init after exec", "error", err, "pid", pid)
				// 	} else {
				// 		slog.DebugContext(ctx, "re-established fork watching for container init after exec", "pid", pid, "info", grp)
				// 	}
				// } else {
				// 	slog.Log(ctx, slog.LevelDebug, fmt.Sprintf("PSNOTIFY:FORK-EXEC[%d]", pid), "info", grp)
				// }
			} else {
				slog.InfoContext(ctx, fmt.Sprintf("PSNOTIFY:EXEC[%d]", pid), "info", grp)
			}

			// TODO: your gRPC call here, e.g. reportExec(pid)
		case err := <-watcher.Error:
			slog.ErrorContext(ctx, "PSNOTIFY[ERROR]", "error", err)
		}
	}
}

func (p *pidInfo) resolvePidData() {
	if p.cgroup == "" {
		p.cgroup, _ = getCgroupFromProc(p.pid)
	}
	if len(p.argv) == 0 {
		p.argv = getArgvFromProc(p.pid)
	}
	if p.argc == "" {
		p.argc, _ = getExePath(p.pid)
	}
}

// lookupCgroup checks our map in case we already recorded it on fork

func (r *runmLinuxInit) getPidInfo(pid int) (*pidInfo, error) {
	info := &pidInfo{pid: pid}
	// pidfd, err := getPidFd(pid)
	// if err != nil {
	// 	slog.Warn("failed to get pidfd in fork", "pid", pid, "error", err)
	// } else {
	// 	slog.Info("got pidfd in fork", "pid", pid)
	// }
	// info.pidfd = pidfd
	info.resolvePidData()
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
