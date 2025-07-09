//go:build !linux

package reap

import (
	"fmt"
)

// SetSubreaper sets the value i as the subreaper setting for the calling process
func SetSubreaper(i int) error {
	return fmt.Errorf("subreaping not supported on darwin")
}

// GetSubreaper returns the subreaper setting for the calling process
func GetSubreaper() (int, error) {
	return 0, fmt.Errorf("subreaping not supported on darwin")
}
