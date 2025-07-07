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

func (r *GoRuncRuntime) reaperLoggingError(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int) {
	errchan := make(chan error, 1)
	defer close(errchan)
	go func() {
		errchan <- r.reaper(ctx, parentDoneChan, pidfile, name, pidchan)
	}()
	err := <-errchan
	if err != nil {
		slog.ErrorContext(ctx, "reaper error", "name", name, "pidfile", pidfile, "error", err)
	}
}

func (r *GoRuncRuntime) reaper(ctx context.Context, parentDoneChan chan string, pidfile string, name string, pidchan chan int) error {
	pidint := -1
	waitingOnPid := true
	waitingOnParent := true
	waitingOnProcess := true

	slog.InfoContext(ctx, "starting reaper", "name", name, "pidfile", pidfile)

	tickd := ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(60),
		ticker.WithMessage(fmt.Sprintf("TICK:START:REAPER[%s]", name)),
		ticker.WithDoneMessage(fmt.Sprintf("TICK:END  :REAPER[%s]", name)),
		ticker.WithAttrFunc(func() []slog.Attr {
			return []slog.Attr{
				slog.Int("current_pid", pidint),
				slog.Bool("waiting_on_pid", waitingOnPid),
				slog.Bool("waiting_on_parent", waitingOnParent),
				slog.Bool("waiting_on_process", waitingOnProcess),
			}
		}),
	)

	var err error

	go tickd.Run(ctx)
	defer tickd.Stop(ctx)
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

	retryCount := 100

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
	go r.reaperLoggingError(ctx, reaperChan, options.PidFile, "create", nil)
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
	go r.reaperLoggingError(ctx, reaperChan, opts.PidFile, "exec", pidchan)
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
