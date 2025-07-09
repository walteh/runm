package reap

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"gitlab.com/tozd/go/errors"
)

func GetPidFd(pid int) (int, error) {
	pidfd, err := unix.PidfdOpen(pid, unix.PIDFD_NONBLOCK)
	if err != nil {
		return 0, err
	}
	return pidfd, nil
}

func PidfdWait(pidfd int) error {
	defer unix.Close(pidfd)

	// 2) Poll for readability (blocks until process exits)
	pfd := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	_, err := unix.Poll(pfd, -1)

	if err != nil {
		return errors.Errorf("failed to poll for process id '%d': %w", pidfd, err)
	}

	return nil
}

func WaitForFdsToClose(pid int) {
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
