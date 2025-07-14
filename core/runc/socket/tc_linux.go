//go:build linux

package socket

import (
	"github.com/containerd/console"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
)

const (
	cmdTcGet = unix.TCGETS
	cmdTcSet = unix.TCSETS
)

func newConsole(consoleInstance console.Console) (console.Console, error) {
	kqueueConsoleInstance, err := console.NewEpoller()
	if err != nil {
		return nil, errors.Errorf("failed to create kqueue: %w", err)
	}

	kqueueConsole, err := kqueueConsoleInstance.Add(consoleInstance)
	if err != nil {
		return nil, errors.Errorf("failed to add console to kqueue: %w", err)
	}

	return kqueueConsole, nil
}
