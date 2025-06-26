//go:build !linux

package goruncruntime

import (
	"context"

	"github.com/containerd/cgroups/v3/cgroup2/stats"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.CgroupAdapter = (*CgroupV2Adapter)(nil)

type CgroupV2Adapter struct {
}

func NewCgroupV2Adapter(ctx context.Context, containerId string) (*CgroupV2Adapter, error) {
	return nil, errors.Errorf("not implemented")
}

// OpenEventChan implements runtime.CgroupAdapter.
func (me *CgroupV2Adapter) OpenEventChan(ctx context.Context) (<-chan runtime.CgroupEvent, <-chan error, error) {
	return nil, nil, errors.Errorf("not implemented")
}

func (me *CgroupV2Adapter) ToggleControllers(ctx context.Context) error {
	return errors.Errorf("not implemented")
}

func (a *CgroupV2Adapter) Stat(ctx context.Context) (*stats.Metrics, error) {
	return nil, errors.Errorf("not implemented")
}
