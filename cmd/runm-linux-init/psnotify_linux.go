package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"gitlab.com/tozd/go/errors"
)

func setSubreaper() error {
	_, _, err := unix.Syscall(unix.SYS_PRCTL,
		unix.PR_SET_CHILD_SUBREAPER, 1, 0)
	if err != 0 {
		return errors.Errorf("failed to set subreaper: %w", err)
	}
	return nil
}

func getPidFd(pid int) (int, error) {
	pidfd, err := unix.PidfdOpen(pid, unix.PIDFD_NONBLOCK)
	if err != nil {
		return 0, err
	}
	return pidfd, nil
}

func pidfdWait(pidfd int) error {
	defer unix.Close(pidfd)

	// 2) Poll for readability (blocks until process exits)
	pfd := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	_, err := unix.Poll(pfd, -1)

	if err != nil {
		return errors.Errorf("failed to poll for process id '%d': %w", pidfd, err)
	}

	// var info unix.Siginfo
	// // note: WAITID will not reap the child if you pass WNOWAIT
	// _, _, errno := unix.Syscall6(
	// 	unix.SYS_WAITID,
	// 	uintptr(unix.P_PIDFD), // idtype
	// 	uintptr(pidfd),        // id (the pidfd)
	// 	uintptr(unsafe.Pointer(&info)),
	// 	uintptr(unix.WEXITED), // options; drop WNOWAIT to reap
	// 	0, 0,
	// )
	// if errno != 0 {
	// 	return errors.Errorf("failed to wait for process: %w", errno)
	// }
	return nil
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
