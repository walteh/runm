//go:build !linux

package reap

func WaitByPidfd(pid int) error {
	return nil
}

func GetPidFd(pid int) (int, error) {
	return 0, nil
}

func PidfdWait(pidfd int) error {
	return nil
}
