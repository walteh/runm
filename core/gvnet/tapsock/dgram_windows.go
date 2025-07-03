//go:build windows

package tapsock

import (
	"context"

	"github.com/walteh/runm/core/virt/virtio"
	"gitlab.com/tozd/go/errors"
)

func NewDgramVirtioNet(ctx context.Context, macstr string) (*virtio.VirtioNet, *VirtualNetworkRunner, error) {
	return nil, nil, errors.Errorf("not implemented")
}
