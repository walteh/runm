//go:build !linux

package main

func setSubreaper() error {
	return nil
}

func waitByPidfd(pid int) error {
	return nil
}
