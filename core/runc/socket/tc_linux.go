//go:build linux

package socket

import (
	"golang.org/x/sys/unix"

	"github.com/containerd/console"
	"gitlab.com/tozd/go/errors"
)

const (
	cmdTcGet = unix.TCGETS
	cmdTcSet = unix.TCSETS
)

func newConsoleCreator() (func(consoleInstance console.Console) (console.Console, error), error) {
	kqueueConsoleInstance, err := console.NewEpoller()
	if err != nil {
		return nil, errors.Errorf("failed to create kqueue: %w", err)
	}

	return func(consoleInstance console.Console) (console.Console, error) {
		return kqueueConsoleInstance.Add(consoleInstance)
	}, nil
}
