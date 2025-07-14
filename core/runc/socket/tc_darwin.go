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

func newConsole(consoleInstance console.Console) (console.Console, error) {
	kqueueConsoleInstance, err := kqueue.NewKqueuer()
	if err != nil {
		return nil, errors.Errorf("failed to create kqueue: %w", err)
	}

	kqueueConsole, err := kqueueConsoleInstance.Add(consoleInstance)
	if err != nil {
		return nil, errors.Errorf("failed to add console to kqueue: %w", err)
	}

	return kqueueConsole, nil
}
