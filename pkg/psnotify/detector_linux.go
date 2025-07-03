// Copyright (c) 2012 VMware, Inc.

//go:build linux
// +build linux

package psnotify

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// waitByPidfd waits for all file descriptors of a process to close
// This is a more reliable indicator that the process has fully completed
func waitByPidfd(pid int) error {
	// Check if the process exists
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	_, err := os.Stat(statPath)
	if os.IsNotExist(err) {
		// Process already gone
		return nil
	}

	// Maximum number of attempts to check for FD closure
	maxAttempts := 10

	// Check for open file descriptors
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// First check if process still exists
		_, err := os.Stat(statPath)
		if os.IsNotExist(err) {
			// Process is gone, we're done
			return nil
		}

		// Try to get FD count
		count, err := getProcFdCount(pid)
		if err != nil {
			// If we can't access FDs, process may be gone
			if os.IsNotExist(err) {
				return nil
			}
			// Some other error, keep waiting
			continue
		}

		// No more open FDs, we're done
		if count == 0 {
			return nil
		}

		// Sleep briefly before next check
		syscall.Nanosleep(&syscall.Timespec{Sec: 0, Nsec: 10000000}, nil) // 10ms
	}

	// We've reached max attempts but process still has open FDs
	// Not returning an error as this isn't fatal - just log it
	fmt.Printf("Process %d still has open FDs after %d attempts\n", pid, maxAttempts)
	return nil
}

// getArgvFromProc retrieves the command line arguments for a process from /proc/{pid}/cmdline
func getArgvFromProc(pid int) []string {
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
	data, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return []string{}
	}

	// cmdline is null-terminated, split by null bytes
	args := strings.Split(string(data), "\x00")

	// Remove empty entries (especially the last one)
	var filtered []string
	for _, arg := range args {
		if arg != "" {
			filtered = append(filtered, arg)
		}
	}

	return filtered
}

// getExePath retrieves the path to the executable for a process
func getExePath(pid int) (string, error) {
	exePath := fmt.Sprintf("/proc/%d/exe", pid)
	path, err := os.Readlink(exePath)
	if err != nil {
		return "", err
	}
	return path, nil
}

// getCgroupFromProc retrieves the cgroup path for a process from /proc/{pid}/cgroup
func getCgroupFromProc(pid int) (string, error) {
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return "", err
	}

	// Parse cgroup information
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) >= 3 {
			// Get the path from the last part
			return parts[2], nil
		}
	}

	return "", fmt.Errorf("no cgroup found for pid %d", pid)
}

// getProcFdCount gets the number of file descriptors for a process
func getProcFdCount(pid int) (int, error) {
	fdPath := fmt.Sprintf("/proc/%d/fd", pid)

	// Open the directory
	dir, err := os.Open(fdPath)
	if err != nil {
		return 0, err
	}
	defer dir.Close()

	// Read all entries
	entries, err := dir.Readdirnames(-1)
	if err != nil {
		return 0, err
	}

	return len(entries), nil
}

// findParentPid attempts to find the parent PID of the given process (Linux-specific implementation)
func (d *Detector) findParentPid(pid int) int {
	// Try to get parent from /proc/<pid>/stat
	if info, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		// Format is: pid (comm) state ppid ...
		fields := strings.Split(string(info), " ")
		if len(fields) >= 4 {
			// PPid is the 4th field
			if ppid, err := strconv.Atoi(fields[3]); err == nil {
				return ppid
			}
		}
	}
	return 0
}
