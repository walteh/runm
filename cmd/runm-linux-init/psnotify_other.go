//go:build !linux

package main

func setSubreaper() error {
	return nil
}

func waitByPidfd(pid int) error {
	return nil
}

func getPidFd(pid int) (int, error) {
	return 0, nil
}

func pidfdWait(pidfd int) error {
	return nil
}
