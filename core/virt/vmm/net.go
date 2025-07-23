package vmm

import (
	"context"
	"fmt"

	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/gvnet"
	"github.com/walteh/runm/pkg/port"
)

func PrepareVirtualNetwork(ctx context.Context, extraPortMappings map[uint16]uint16) (gvnet.Proxy, uint16, error) {
	port, err := port.ReservePort(ctx)
	if err != nil {
		return nil, 0, errors.Errorf("reserving port: %w", err)
	}
	cfg := &gvnet.GvproxyConfig{
		MagicHostPort:                   fmt.Sprintf("tcp://127.0.0.1:%d", port),
		EnableDebug:                     false,
		EnableMagicNFSForwarding:        true,
		ExtraHostToGuestTCPPortMappings: extraPortMappings,
	}

	dev, err := gvnet.NewProxy(ctx, cfg)
	if err != nil {
		return nil, 0, errors.Errorf("creating gvproxy: %w", err)
	}

	return dev, port, nil

}
