package piddebug

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func BigPidAttrFunc(pidint int) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int("current_pid", pidint),
	}
	//
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
					stdioInfo := make(map[string]string)

					for _, entry := range fdEntries {
						fdPath := fmt.Sprintf("/proc/%d/fd/%s", pidint, entry.Name())
						if target, err := os.Readlink(fdPath); err == nil {
							fdNum := entry.Name()

							// Check for stdio (0=stdin, 1=stdout, 2=stderr)
							switch fdNum {
							case "0":
								stdioInfo["stdin"] = target
							case "1":
								stdioInfo["stdout"] = target
							case "2":
								stdioInfo["stderr"] = target
							}

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

					// Add stdio information
					for stdio, target := range stdioInfo {
						attrs = append(attrs, slog.String(fmt.Sprintf("stdio_%s", stdio), target))
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
}

// debugRootfs shows what the current rootfs looks like
func DebugRootfs(processName string, pid int) string {
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
