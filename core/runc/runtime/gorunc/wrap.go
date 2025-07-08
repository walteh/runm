//go:build !windows

package goruncruntime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/walteh/runm/pkg/ticker"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"
)

func tryReadPidFile(ctx context.Context, pidfile string) (int, error) {
	pid, err := os.ReadFile(pidfile)
	if err != nil {
		return -1, errors.Errorf("failed to read pid file: %w", err)
	}
	return strconv.Atoi(string(pid))
}

func waitForFileToExistChan(ctx context.Context, pidfile string) chan int {
	pidfilechan := make(chan int, 1)
	go func() {
		for {
			if ctx.Err() != nil {
				pidfilechan <- -1
				return
			}

			if pidint, err := tryReadPidFile(ctx, pidfile); err == nil {
				pidfilechan <- pidint
				return
			}
		}
	}()
	return pidfilechan
}

func (r *GoRuncRuntime) reaperLoggingError(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int, justWait bool) {
	errchan := make(chan error, 1)
	defer close(errchan)
	go func() {
		errchan <- r.reaper(ctx, parentDoneChan, pidfile, name, pidchan, justWait)
	}()
	err := <-errchan
	if err != nil {
		slog.ErrorContext(ctx, "reaper error", "name", name, "pidfile", pidfile, "error", err)
	}
}

func (r *GoRuncRuntime) reaper(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int, justWait bool) error {
	pidint := -1
	waitingOnPid := true
	waitingOnParent := true
	waitingOnProcess := true

	// since this is a background process, we don't want to die when the parent dies
	ctx = context.WithoutCancel(ctx)

	slog.InfoContext(ctx, "starting reaper", "name", name, "pidfile", pidfile)

	tickd := ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("GORUNC:REAPER:%s[RUNNING]", name)),
		ticker.WithDoneMessage(fmt.Sprintf("GORUNC:REAPER:%s[DONE]", name)),
		ticker.WithAttrFunc(func() []slog.Attr {
			attrs := []slog.Attr{
				slog.Int("current_pid", pidint),
				slog.Bool("waiting_on_pid", waitingOnPid),
				slog.Bool("waiting_on_parent", waitingOnParent),
				slog.Bool("waiting_on_process", waitingOnProcess),
			}

			// Add process state information if we have a valid PID
			if pidint > 0 {
				// Check if process still exists
				if _, err := os.FindProcess(pidint); err == nil {
					attrs = append(attrs, slog.Bool("process_exists", true))

					// Get process status
					if statusData, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pidint)); err == nil {
						status := string(statusData)
						if strings.Contains(status, "State:") {
							lines := strings.Split(status, "\n")
							for _, line := range lines {
								if strings.HasPrefix(line, "State:") {
									attrs = append(attrs, slog.String("process_state", strings.TrimSpace(line)))
									break
								}
							}
						}
					}

					// Count open file descriptors
					if fdDir, err := os.Open(fmt.Sprintf("/proc/%d/fd", pidint)); err == nil {
						if fdEntries, err := fdDir.Readdir(-1); err == nil {
							attrs = append(attrs, slog.Int("open_fds", len(fdEntries)))

							// Get details about open FDs
							fdTypes := make(map[string]int)
							for _, entry := range fdEntries {
								fdPath := fmt.Sprintf("/proc/%d/fd/%s", pidint, entry.Name())
								if target, err := os.Readlink(fdPath); err == nil {
									if strings.Contains(target, "socket:") {
										fdTypes["socket"]++
									} else if strings.Contains(target, "pipe:") {
										fdTypes["pipe"]++
									} else if strings.HasPrefix(target, "/") {
										fdTypes["file"]++
									} else {
										fdTypes["other"]++
									}
								}
							}

							// Add FD type counts
							for fdType, count := range fdTypes {
								attrs = append(attrs, slog.Int(fmt.Sprintf("fd_%s", fdType), count))
							}
						}
						fdDir.Close()
					}

					// Check if process is a zombie
					if statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pidint)); err == nil {
						fields := strings.Fields(string(statData))
						if len(fields) > 2 {
							attrs = append(attrs, slog.String("process_stat_state", fields[2]))
						}
					}

					// Check what the process is waiting on (if sleeping)
					if wchanData, err := os.ReadFile(fmt.Sprintf("/proc/%d/wchan", pidint)); err == nil {
						wchan := strings.TrimSpace(string(wchanData))
						if wchan != "" && wchan != "0" {
							attrs = append(attrs, slog.String("waiting_on", wchan))

							// Detect if process is stuck in namespace cleanup
							if strings.Contains(wchan, "zap_pid_ns_processes") {
								attrs = append(attrs, slog.Bool("stuck_in_namespace_cleanup", true))
							}
						}
					}

					// Check process stack trace if available
					// if stackData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stack", pidint)); err == nil {
					// 	stack := strings.TrimSpace(string(stackData))
					// 	if stack != "" {
					// 		// Just show the first few lines to avoid spam
					// 		lines := strings.Split(stack, "\n")
					// 		if len(lines) > 3 {
					// 			lines = lines[:3]
					// 		}
					// 		attrs = append(attrs, slog.String("stack_trace", strings.Join(lines, " | ")))
					// 	}
					// }

					// Check for child processes that might be keeping the namespace alive
					if entries, err := os.ReadDir("/proc"); err == nil {
						childCount := 0
						zombieCount := 0
						var childInfo []string
						for _, entry := range entries {
							if !entry.IsDir() {
								continue
							}
							// Check if it's a PID directory
							if childPid, err := strconv.Atoi(entry.Name()); err == nil && childPid != pidint {
								// Check if this process has our target as parent
								if statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPid)); err == nil {
									fields := strings.Fields(string(statData))
									if len(fields) > 3 {
										if ppid, err := strconv.Atoi(fields[3]); err == nil && ppid == pidint {
											childCount++
											state := "unknown"
											cmd := "unknown"
											if len(fields) > 2 {
												state = fields[2]
												if state == "Z" {
													zombieCount++
												}
											}
											if len(fields) > 1 {
												cmd = fields[1]
											}
											childInfo = append(childInfo, fmt.Sprintf("%d:%s:%s", childPid, cmd, state))
										}
									}
								}
							}
						}
						if childCount > 0 {
							attrs = append(attrs, slog.Int("child_processes", childCount))
							attrs = append(attrs, slog.String("child_details", strings.Join(childInfo, ",")))
						}
						if zombieCount > 0 {
							attrs = append(attrs, slog.Int("zombie_children", zombieCount))
						}
					}

					// Check threads in the same thread group
					if taskEntries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pidint)); err == nil {
						threadCount := len(taskEntries)
						if threadCount > 1 {
							attrs = append(attrs, slog.Int("thread_count", threadCount))

							// Check what other threads are doing
							busyThreads := 0
							for _, taskEntry := range taskEntries {
								if taskPid, err := strconv.Atoi(taskEntry.Name()); err == nil && taskPid != pidint {
									if taskStatData, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/stat", pidint, taskPid)); err == nil {
										fields := strings.Fields(string(taskStatData))
										if len(fields) > 2 && fields[2] != "S" { // Not sleeping
											busyThreads++
										}
									}
								}
							}
							if busyThreads > 0 {
								attrs = append(attrs, slog.Int("active_threads", busyThreads))
							}
						}
					}

					// Check process group and session info
					if statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pidint)); err == nil {
						fields := strings.Fields(string(statData))
						if len(fields) > 7 {
							if pgid, err := strconv.Atoi(fields[4]); err == nil {
								attrs = append(attrs, slog.Int("process_group", pgid))
							}
							if sid, err := strconv.Atoi(fields[5]); err == nil {
								attrs = append(attrs, slog.Int("session_id", sid))
							}
						}
					}

					// Check mount namespace references
					if mountData, err := os.ReadFile(fmt.Sprintf("/proc/%d/mounts", pidint)); err == nil {
						mounts := strings.Split(string(mountData), "\n")
						mountCount := 0
						for _, mount := range mounts {
							if strings.TrimSpace(mount) != "" {
								mountCount++
							}
						}
						attrs = append(attrs, slog.Int("mount_count", mountCount))
					}
				} else {
					attrs = append(attrs, slog.Bool("process_exists", false))
				}
			}

			return attrs
		}),
	)

	var err error

	go tickd.Run(ctx)
	defer tickd.Stop(ctx)

	if justWait {
		<-parentDoneChan
		return nil
	}

	// this hacky logic probably broke the exit status code carryover with go tool task dev:2025-07-02:01
	if pidchan != nil {
		pidint = <-pidchan
	} else {
		pidint = <-waitForFileToExistChan(ctx, pidfile)
	}

	waitingOnPid = false

	sys, err := os.FindProcess(pidint)
	if err != nil {
		return errors.Errorf("failed to find process: %w", err)
	}

	// output := ""

	// output += debugNamespaces("vm-init", os.Getpid()) + "\n"
	// output += debugMounts("vm-init", os.Getpid()) + "\n"
	// output += debugRootfs("vm-init", os.Getpid()) + "\n"

	// output += debugNamespaces("container-init", pidint) + "\n"
	// output += debugMounts("container-init", pidint) + "\n"
	// output += debugRootfs("container-init", pidint) + "\n"

	// slog.InfoContext(ctx, "found process", "pid", pidint, "output", output)

	retryCount := 1000

	var waiter *os.ProcessState
	for {
		waiter, err = sys.Wait()
		if err != nil {
			if errors.Is(err, syscall.ECHILD) {
				retryCount--
				if retryCount <= 0 {
					return errors.Errorf("failed to wait on pid %d: %w", pidint, err)
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return errors.Errorf("failed to wait on pid %d: %w", pidint, err)
		}
		break
	}

	// waiter, err := sys.Wait()
	// if err != nil {
	// 	// if errors.Is(err, syscall.ECHILD) {
	// 	// 	slog.InfoContext(ctx, "process already exited", "name", name, "pidfile", pidfile, "pid", pidint)
	// 	// 	waitingOnProcess = false
	// 	// 	return nil // TODO:this makes this reaper non-functional
	// 	// } else {
	// 	return errors.Errorf("failed to wait on pid %d: %w", pidint, err)
	// 	// }
	// }

	waitingOnProcess = false

	slog.InfoContext(ctx, "waiter done", "waiter", waiter)

	go func() {
		r.reaperCh <- gorunc.Exit{
			Timestamp: time.Now(),
			Pid:       pidint,
			Status:    int(waiter.Sys().(syscall.WaitStatus).ExitStatus()),
		}
	}()

	slog.InfoContext(ctx, "reaper waiting for parent to exit", "name", name, "pidfile", pidfile, "pid", pidint)

	<-parentDoneChan

	waitingOnParent = false

	slog.InfoContext(ctx, "reaper parent exited, waiting for process to exit", "name", name, "pidfile", pidfile, "pid", pidint)

	return nil
}

func (r *GoRuncRuntime) Create(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) error {
	// slog.Info(godump.DumpStr(options), "id", id, "bundle", bundle, "options", options)

	// output, err := exec.CommandContext(ctx, "/bin/busybox", "ls", "-lah", bundle).CombinedOutput()
	// if err != nil {
	// 	slog.ErrorContext(ctx, "failed to list bundle", "error", err, "output", string(output))
	// 	return errors.Errorf("failed to list bundle: %w", err)
	// }
	// slog.InfoContext(ctx, "ls -lahr: "+string(output), "bundle", bundle)

	// output, err = exec.CommandContext(ctx, "/bin/busybox", "ls", "-lah", filepath.Join(bundle, "rootfs")).CombinedOutput()
	// if err != nil {
	// 	slog.ErrorContext(ctx, "failed to list bundle", "error", err, "output", string(output))
	// 	return errors.Errorf("failed to list bundle: %w", err)
	// }
	// slog.InfoContext(ctx, "ls -lah: "+string(output), "bundle", bundle)

	reaperChan := make(chan string, 1)
	defer close(reaperChan)
	// go r.reaperLoggingError(ctx, reaperChan, options.PidFile, "create", nil)

	options.NoNewKeyring = true

	// pidchan := make(chan int, 1)
	go r.reaperLoggingError(ctx, reaperChan, options.PidFile, "create", nil, false)
	// options.Started = pidchan

	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Create(ctx, id, bundle, options)
	})
}

func (r *GoRuncRuntime) Checkpoint(ctx context.Context, id string, opts *gorunc.CheckpointOpts, actions ...gorunc.CheckpointAction) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Checkpoint(ctx, id, opts, actions...)
	})
}

// Delete implements runtime.Runtime.
func (r *GoRuncRuntime) Delete(ctx context.Context, id string, opts *gorunc.DeleteOpts) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Delete(ctx, id, opts)
	})
}

// Exec implements runtime.Runtime.
func (r *GoRuncRuntime) Exec(ctx context.Context, id string, spec specs.Process, opts *gorunc.ExecOpts) error {
	reaperChan := make(chan string, 1)
	defer close(reaperChan)
	pidchan := make(chan int, 1)
	opts.Started = pidchan
	go r.reaperLoggingError(ctx, reaperChan, opts.PidFile, "exec", pidchan, true)
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Exec(ctx, id, spec, opts)
	})
}

// Kill implements runtime.Runtime.
func (r *GoRuncRuntime) Kill(ctx context.Context, id string, signal int, opts *gorunc.KillOpts) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		slog.InfoContext(ctx, "KILLING RUNC", "id", id, "signal", signal, "opts.all", opts.All)
		return r.internal.Kill(ctx, id, signal, opts)
	})
}

// Pause implements runtime.Runtime.
func (r *GoRuncRuntime) Pause(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Pause(ctx, id)
	})
}

// Ps implements runtime.Runtime.
func (r *GoRuncRuntime) Ps(ctx context.Context, id string) ([]int, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() ([]int, error) {
		return r.internal.Ps(ctx, id)
	})
}

// Restore implements runtime.Runtime.
func (r *GoRuncRuntime) Restore(ctx context.Context, id string, bundle string, opts *gorunc.RestoreOpts) (int, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (int, error) {
		return r.internal.Restore(ctx, id, bundle, opts)
	})
}

// Resume implements runtime.Runtime.
func (r *GoRuncRuntime) Resume(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Resume(ctx, id)
	})
}

// Update implements runtime.Runtime.
func (r *GoRuncRuntime) Update(ctx context.Context, id string, resources *specs.LinuxResources) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Update(ctx, id, resources)
	})
}

// start
func (r *GoRuncRuntime) Start(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Start(ctx, id)
	})
}

// Events implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Events(ctx context.Context, id string, timeout time.Duration) (chan *gorunc.Event, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (chan *gorunc.Event, error) {
		return r.internal.Events(ctx, id, timeout)
	})
}

// List implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) List(ctx context.Context) ([]*gorunc.Container, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() ([]*gorunc.Container, error) {
		return r.internal.List(ctx)
	})
}

// State implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) State(ctx context.Context, id string) (*gorunc.Container, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.Container, error) {
		return r.internal.State(ctx, id)
	})
}

// Stats implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Stats(ctx context.Context, id string) (*gorunc.Stats, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.Stats, error) {
		return r.internal.Stats(ctx, id)
	})
}

// Top implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Top(ctx context.Context, a string, b string) (*gorunc.TopResults, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.TopResults, error) {
		return r.internal.Top(ctx, a, b)
	})
}

// Version implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Version(ctx context.Context) (gorunc.Version, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (gorunc.Version, error) {
		return r.internal.Version(ctx)
	})
}

// debugNamespaces prints current process namespace information
func debugNamespaces(processName string, pid int) string {
	buf := bytes.NewBufferString("")
	fmt.Fprintf(buf, "=== NAMESPACE DEBUG for %s (PID: %d) ===\n", processName, pid)

	// Read all namespace links
	namespaces := []string{"mnt", "pid", "net", "ipc", "uts", "user", "cgroup"}

	for _, ns := range namespaces {
		nsPath := fmt.Sprintf("/proc/%d/ns/%s", pid, ns)
		if link, err := os.Readlink(nsPath); err == nil {
			fmt.Fprintf(buf, "  %s: %s\n", ns, link)
		} else {
			fmt.Fprintf(buf, "  %s: ERROR - %v\n", ns, err)
		}
	}

	// Show parent process for context
	if ppid := os.Getppid(); ppid > 0 {
		fmt.Fprintf(buf, "  Parent PID: %d\n", ppid)
	}

	fmt.Fprintf(buf, "=== END NAMESPACE DEBUG ===\n")
	return buf.String()
}

// debugMounts shows current mount namespace view
func debugMounts(processName string, pid int) string {
	buf := bytes.NewBufferString("")
	fmt.Fprintf(buf, "=== MOUNT DEBUG for (%s) (PID: %d) ===\n", processName, pid)

	// Read /proc/self/mounts
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/mounts", pid)); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			fmt.Fprintf(buf, "  %s\n", line)
		}
	} else {
		fmt.Fprintf(buf, "  ERROR reading mounts: %v\n", err)
	}

	fmt.Fprintf(buf, "=== END MOUNT DEBUG ===\n")
	return buf.String()
}

// debugRootfs shows what the current rootfs looks like
func debugRootfs(processName string, pid int) string {
	buf := bytes.NewBufferString("")
	fmt.Fprintf(buf, "=== ROOTFS DEBUG for (%s) (PID: %d) ===\n", processName, pid)

	// For container processes, we want to check their actual rootfs
	// Read from /proc/{pid}/root which is the container's rootfs view
	var baseDir string
	if strings.Contains(processName, "container") {
		baseDir = fmt.Sprintf("/proc/%d/root", pid)
		fmt.Fprintf(buf, "  Checking container rootfs via %s\n", baseDir)
	} else {
		baseDir = ""
		fmt.Fprintf(buf, "  Checking VM filesystem directly\n")
	}

	// Check key directories
	checkDirs := []string{"/", "/usr", "/usr/local", "/usr/local/bin", "/usr/share", "/usr/share/zoneinfo", "/dev", "/sys", "/proc", "/bin", "/bin/sh"}

	for _, dir := range checkDirs {
		fullPath := baseDir + dir
		if info, err := os.Stat(fullPath); err == nil {
			fmt.Fprintf(buf, "  %s: EXISTS (mode: %s)\n", dir, info.Mode())

			// If it's /usr/local/bin, list contents
			if dir == "/usr/local/bin" {
				if entries, err := os.ReadDir(fullPath); err == nil {
					fmt.Fprintf(buf, "    Contents:\n")
					for _, entry := range entries {
						fmt.Fprintf(buf, "      %s\n", entry.Name())
					}
				} else {
					fmt.Fprintf(buf, "    ERROR listing contents: %v\n", err)
				}

			}
			// In debugRootfs, add this check specifically for docker-entrypoint.sh
			if dir == "/usr/local/bin" && strings.Contains(processName, "container") {
				entrypointPath := fullPath + "/docker-entrypoint.sh"

				// Check file details
				if data, err := os.ReadFile(entrypointPath); err == nil {
					lines := strings.Split(string(data), "\n")
					if len(lines) > 0 {
						fmt.Fprintf(buf, "      Shebang: %s\n", lines[0])

					}
				}

				// Check if it's executable
				if info, err := os.Stat(entrypointPath); err == nil {
					fmt.Fprintf(buf, "      Permissions: %s\n", info.Mode())
				}
			}
			if dir == "/bin/sh" && strings.Contains(processName, "container") {
				// Check what libraries sh needs
				fmt.Fprintf(buf, "      Checking /bin/sh dependencies...\n")

				// Check if /lib and /lib64 exist in container
				libDirs := []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64"}
				for _, libDir := range libDirs {
					if _, err := os.Stat(baseDir + libDir); err == nil {
						fmt.Fprintf(buf, "      %s: EXISTS\n", libDir)
						if entries, err := os.ReadDir(baseDir + libDir); err == nil && len(entries) > 0 {
							fmt.Fprintf(buf, "        Contains %d items: ", len(entries))
							// Show first few items to see what's there
							for i, entry := range entries {
								if i >= 3 { // Show first 3 items
									fmt.Fprintf(buf, "...")
									break
								}
								fmt.Fprintf(buf, "%s ", entry.Name())
							}
							fmt.Fprintf(buf, "\n")
						} else {
							fmt.Fprintf(buf, "        EMPTY or unreadable\n")
						}
					} else {
						fmt.Fprintf(buf, "      %s: MISSING\n", libDir)
					}
				}

				// Check for dynamic linker specifically
				linkers := []string{"/lib64/ld-linux-x86-64.so.2", "/lib/ld-linux-aarch64.so.1", "/lib/ld-musl-x86_64.so.1", "/lib/ld-musl-aarch64.so.1"}
				for _, linker := range linkers {
					if _, err := os.Stat(baseDir + linker); err == nil {
						fmt.Fprintf(buf, "      Dynamic linker found: %s\n", linker)
					}
				}

				// Try different possible locations for ls
				lsPaths := []string{"/bin/ls", "/usr/bin/ls", "/usr/local/bin/ls"}
				foundLs := false

				for _, lsPath := range lsPaths {
					if _, err := os.Stat(baseDir + lsPath); err == nil {
						fmt.Fprintf(buf, "      Found ls at %s\n", lsPath)
						// Try to run ls from container using syscall chroot
						cmd := exec.Command(lsPath, "-la", "/bin")
						cmd.SysProcAttr = &syscall.SysProcAttr{
							Chroot: baseDir,
						}
						if err := cmd.Run(); err != nil {
							fmt.Fprintf(buf, "      ERROR: Cannot execute %s in container: %v\n", lsPath, err)
						} else {
							fmt.Fprintf(buf, "      SUCCESS: %s works in container\n", lsPath)
							foundLs = true
							break
						}
					}
				}

				if !foundLs {
					fmt.Fprintf(buf, "      ERROR: No ls command found in container\n")
				}

				// Check if we can execute a simple command using syscall chroot
				cmd := exec.Command("/bin/sh", "-c", "echo test")
				cmd.SysProcAttr = &syscall.SysProcAttr{
					Chroot: baseDir,
				}
				if err := cmd.Run(); err != nil {
					fmt.Fprintf(buf, "      ERROR: Cannot execute /bin/sh in container: %v\n", err)
				} else {
					fmt.Fprintf(buf, "      SUCCESS: /bin/sh works in container\n")
				}
			}
		} else {
			fmt.Fprintf(buf, "  %s: MISSING - %v\n", dir, err)
		}
	}

	fmt.Fprintf(buf, "=== END ROOTFS DEBUG ===\n")
	return buf.String()
}
