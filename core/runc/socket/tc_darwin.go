//go:build darwin

package socket

import (
	"golang.org/x/sys/unix"

	"github.com/containerd/console"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/pkg/kqueue"
)

const (
	cmdTcGet = unix.TIOCGETA
	cmdTcSet = unix.TIOCSETA
)

func newConsoleCreator() (func(consoleInstance console.Console) (console.Console, error), error) {
	kqueueConsoleInstance, err := kqueue.NewKqueuer()
	if err != nil {
		return nil, errors.Errorf("failed to create kqueue: %w", err)
	}

	return func(consoleInstance console.Console) (console.Console, error) {
		return kqueueConsoleInstance.Add(consoleInstance)
	}, nil
}
