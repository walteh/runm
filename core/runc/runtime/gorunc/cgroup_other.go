//go:build !linux

package goruncruntime

import (
	"context"

	goruntime "runtime"

	"github.com/containerd/cgroups/v3/cgroup2/stats"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runtime.CgroupAdapter = (*CgroupV2Adapter)(nil)

type CgroupV2Adapter struct {
}

func NewCgroupV2Adapter(ctx context.Context, containerId string) (*CgroupV2Adapter, error) {
	return nil, errors.Errorf("NewCgroupV2Adapter is only implemented on linux platforms, not on %s", goruntime.GOOS)
}

// OpenEventChan implements runtime.CgroupAdapter.
func (me *CgroupV2Adapter) OpenEventChan(ctx context.Context) (<-chan *runmv1.CgroupEvent, <-chan error, error) {
	return nil, nil, errors.Errorf("CgroupV2Adapter.OpenEventChan is only implemented on linux platforms, not on %s", goruntime.GOOS)
}

func (me *CgroupV2Adapter) ToggleControllers(ctx context.Context) error {
	return errors.Errorf("CgroupV2Adapter.ToggleControllers is only implemented on linux platforms, not on %s", goruntime.GOOS)
}

func (a *CgroupV2Adapter) Stat(ctx context.Context) (*stats.Metrics, error) {
	return nil, errors.Errorf("CgroupV2Adapter.Stat is only implemented on linux platforms, not on %s", goruntime.GOOS)
}
