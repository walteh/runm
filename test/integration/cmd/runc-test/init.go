//go:build linux

package main

import (
	"strings"

	_ "github.com/opencontainers/runc/libcontainer/nsenter"

	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/opencontainers/runc/libcontainer"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
)

func init() {

	// fmt.Printf("DEBUG: dial /tmp/runm-log-proxy.sock\n")

	if len(os.Args) > 1 && os.Args[1] == "init" {

		closer := setupLogging()
		defer closer()

		// debugNamespaces("runc-test[init]")
		// debugMounts("runc-test[init]")
		// debugRootfs("runc-test[init]")
		// containerDebug("runc-test[init]")

		// This is the golang entry point for runc init, executed
		// before main() but after libcontainer/nsenter's nsexec().
		libcontainer.Init()
	}
}

func setupLogging() func() {
	closers := []func(){}

	opts := []logging.LoggerOpt{
		logging.WithInterceptLogrus(true),
		logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
		logging.WithEnableDelimiter(true),
	}

	opts = append(opts, logging.WithValues(
		slog.String("run_id", fmt.Sprintf("%d", runId)),
		slog.String("ppid", fmt.Sprintf("%d", os.Getppid())),
		slog.String("pid", fmt.Sprintf("%d", os.Getpid())),
	))

	rawConn, err := vsock.Dial(2, uint32(constants.VsockRawWriterProxyPort), nil)
	if err != nil {
		fmt.Printf("problem dialing log proxy: %v\n", err)
	} else {
		closers = append(closers, func() { rawConn.Close() })

		opts = append(opts, logging.WithRawWriter(rawConn))
	}

	dialConn, err := vsock.Dial(2, uint32(constants.VsockDelimitedWriterProxyPort), nil)
	if err != nil {
		fmt.Printf("problem dialing log proxy: %v\n", err)
	} else {
		closers = append(closers, func() { dialConn.Close() })

		_ = logging.NewDefaultDevLogger("runc[init]", dialConn, opts...)

	}

	// if dialConn != nil && rawConn != nil {
	// 	dfd, err := hack.GetFdFromUnixConn(dialConn)
	// 	if err != nil {
	// 		slog.Error("DEBUG: problem getting dialConn file", "err", err)
	// 	}
	// 	rfd, err := hack.GetFdFromUnixConn(rawConn)
	// 	if err != nil {
	// 		slog.Error("DEBUG: problem getting rawConn file", "err", err)
	// 	}
	// 	slog.Info("DEBUG: connection file descriptors created", "dialConn", dialConn.RemoteAddr(), "rawConn", rawConn.RemoteAddr(), "dfd", dfd, "rfd", rfd)

	// }

	ticker := time.NewTicker(10 * time.Millisecond)
	ticks := 0
	closers = append(closers, func() {
		ticker.Stop()
	})

	dirs := []string{
		".",
		"/usr",
		"/usr/local",
		"/usr/local/bin",
	}

	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			slog.Error("problem reading directory", "error", err)
		}
		for _, file := range files {
			slog.Info("file", "dir", dir, "file", file.Name())
		}
	}

	// stat /usr/local/bin/docker-entrypoint.sh
	stat, err := os.Stat("/usr/local/bin/docker-entrypoint.sh")
	if err != nil {
		slog.Error("problem statting /usr/local/bin/docker-entrypoint.sh", "error", err)
	}
	slog.Info("stat /usr/local/bin/docker-entrypoint.sh", "stat", stat)

	go func() {
		for tick := range ticker.C {

			ticks++
			if ticks < 1000 || ticks%100 == 0 {
				slog.Info("still running in runc-test[init], waiting to be killed", "tick", tick)
			}
		}
	}()

	return func() {
		for _, closer := range closers {
			closer()
		}
	}
}

// func getFdFromUnixConn(conn *net.UnixConn) (uintptr, error) {
// 	scs, err := conn..SyscallConn()
// 	if err != nil {
// 		return 0, err
// 	}

// 	var fdz uintptr
// 	scs.Control(func(fd uintptr) {
// 		fdz = fd
// 	})

// 	return fdz, nil
// }

// debugNamespaces prints current process namespace information
func debugNamespaces(processName string) {
	pid := os.Getpid()
	fmt.Printf("=== NAMESPACE DEBUG for %s (PID: %d) ===\n", processName, pid)

	// Read all namespace links
	namespaces := []string{"mnt", "pid", "net", "ipc", "uts", "user", "cgroup", "proc"}

	for _, ns := range namespaces {
		nsPath := fmt.Sprintf("/proc/%d/ns/%s", pid, ns)
		if link, err := os.Readlink(nsPath); err == nil {
			fmt.Printf("  %s: %s\n", ns, link)
		} else {
			fmt.Printf("  %s: ERROR - %v\n", ns, err)
		}
	}

	fmt.Printf("=== NAMESPACE DEBUG for %s (PID=self) ===\n", processName)

	// Read all namespace links

	for _, ns := range namespaces {
		nsPath := fmt.Sprintf("/proc/self/ns/%s", ns)
		if link, err := os.Readlink(nsPath); err == nil {
			fmt.Printf("  %s: %s\n", ns, link)
		} else {
			fmt.Printf("  %s: ERROR - %v\n", ns, err)
		}
	}

	// Show parent process for context
	if ppid := os.Getppid(); ppid > 0 {
		fmt.Printf("  Parent PID: %d\n", ppid)
	}

	fmt.Printf("=== END NAMESPACE DEBUG ===\n")
}

// debugMounts shows current mount namespace view
func debugMounts(processName string) {
	fmt.Printf("=== MOUNT DEBUG for %s ===\n", processName)

	// Read /proc/self/mounts
	if data, err := os.ReadFile("/proc/self/mounts"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.Contains(line, "/usr") || strings.Contains(line, "/dev") ||
				strings.Contains(line, "/sys") || strings.Contains(line, "/proc") {
				fmt.Printf("  %s\n", line)
			}
		}
	} else {
		fmt.Printf("  ERROR reading mounts: %v\n", err)
	}

	fmt.Printf("=== END MOUNT DEBUG ===\n")
}

// debugRootfs shows what the current rootfs looks like
func debugRootfs(processName string) {
	fmt.Printf("=== ROOTFS DEBUG for %s ===\n", processName)

	// Check key directories
	checkDirs := []string{"/", "/usr", "/usr/local", "/usr/local/bin", "/dev", "/sys", "/proc"}

	for _, dir := range checkDirs {
		if info, err := os.Stat(dir); err == nil {
			fmt.Printf("  %s: EXISTS (mode: %s)\n", dir, info.Mode())

			// If it's /usr/local/bin, list contents
			if dir == "/usr/local/bin" {
				if entries, err := os.ReadDir(dir); err == nil {
					fmt.Printf("    Contents:\n")
					for _, entry := range entries {
						fmt.Printf("      %s\n", entry.Name())
					}
				} else {
					fmt.Printf("    ERROR listing contents: %v\n", err)
				}
			}
		} else {
			fmt.Printf("  %s: MISSING - %v\n", dir, err)
		}
	}

	fmt.Printf("=== END ROOTFS DEBUG ===\n")
}

func containerDebug(processName string) {
	fmt.Printf("=== CONTAINER PROCESS NAMESPACE DEBUG ===\n")
	namespaces := []string{"mnt", "pid", "net", "ipc", "uts", "user", "cgroup"}

	for _, ns := range namespaces {
		nsPath := fmt.Sprintf("/proc/self/ns/%s", ns)
		if link, err := os.Readlink(nsPath); err == nil {
			fmt.Printf("%s: %s\n", ns, link)
		} else {
			fmt.Printf("%s: ERROR - %v\n", ns, err)
		}
	}
	fmt.Printf("=== END CONTAINER DEBUG ===\n")
}
