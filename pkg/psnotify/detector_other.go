//go:build !linux && !darwin
// +build !linux,!darwin

package psnotify

import (
	"time"
)

// getCgroupFromProc is a stub for non-Linux/non-Darwin platforms
func getCgroupFromProc(pid int) (string, error) {
	return "", nil
}

// getArgvFromProc is a stub for non-Linux/non-Darwin platforms
func getArgvFromProc(pid int) []string {
	return []string{}
}

// getExePath is a stub for non-Linux/non-Darwin platforms
func getExePath(pid int) (string, error) {
	return "", nil
}

// waitByPidfd is a stub for non-Linux/non-Darwin platforms
// On these platforms, we simply wait a short time and return
func waitByPidfd(pid int) error {
	// Wait a short time to give the process time to exit fully
	time.Sleep(100 * time.Millisecond)
	return nil
}
