//go:build windows

package tapsock

import (
	"context"

	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/virt/virtio"
)

func NewDgramVirtioNet(ctx context.Context, macstr string) (*virtio.VirtioNet, *VirtualNetworkRunner, error) {
	return nil, nil, errors.Errorf("not implemented")
}
