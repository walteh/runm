//go:build darwin

package socket

import (
	"github.com/containerd/console"
	"github.com/walteh/runm/pkg/kqueue"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
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
