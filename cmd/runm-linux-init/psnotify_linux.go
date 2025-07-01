package main

import (
	"fmt"
	"os"
	"time"

	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
)

func setSubreaper() error {
	_, _, err := unix.Syscall(unix.SYS_PRCTL,
		unix.PR_SET_CHILD_SUBREAPER, 1, 0)
	if err != 0 {
		return errors.Errorf("failed to set subreaper: %w", err)
	}
	return nil
}

func waitByPidfd(pid int) error {
	// 1) Open a pidfd (requires Linux ≥5.3)
	pidfd, err := unix.PidfdOpen(pid, unix.PIDFD_NONBLOCK)
	if err != nil {
		return err
	}
	defer unix.Close(pidfd)

	// 2) Poll for readability (blocks until process exits)
	pfd := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	_, err = unix.Poll(pfd, -1)
	return err
}

func waitForFdsToClose(pid int) {
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Directory gone ⇒ process entry removed ⇒ FDs closed
			return
		}
		if len(entries) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
