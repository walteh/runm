package runm

import (
	"github.com/containerd/console"
	"github.com/containerd/containerd/v2/pkg/stdio"
	"gitlab.com/tozd/go/errors"
)

func NewPlatform() (stdio.Platform, error) {
	epoller, err := console.NewEpoller()
	if err != nil {
		return nil, errors.Errorf("initializing epoller: %w", err)
	}
	go epoller.Wait()
	return newPlatform(epoller)
}
