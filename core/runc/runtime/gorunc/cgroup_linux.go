//go:build linux

package goruncruntime

import (
	"context"
	"log/slog"

	"github.com/containerd/cgroups/v3/cgroup2"
	"github.com/containerd/cgroups/v3/cgroup2/stats"
	"github.com/moby/sys/userns"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.CgroupAdapter = (*CgroupV2Adapter)(nil)

type CgroupV2Adapter struct {
	cgroup *cgroup2.Manager
}

func NewCgroupV2Adapter(ctx context.Context, containerId string) (*CgroupV2Adapter, error) {

	// get the cgroup manager
	cg, err := cgroup2.Load("/" + containerId)
	if err != nil {
		return nil, errors.Errorf("failed to load cgroup2 for root: %w", err)
	}

	return &CgroupV2Adapter{cgroup: cg}, nil
}

// OpenEventChan implements runtime.CgroupAdapter.
func (me *CgroupV2Adapter) OpenEventChan(ctx context.Context) (<-chan runtime.CgroupEvent, <-chan error, error) {

	evch, errch := me.cgroup.EventChan()

	evch2 := make(chan runtime.CgroupEvent)

	go func() {
		for ev := range evch {
			go func() {
				evch2 <- runtime.CgroupEvent{
					Low:     ev.Low,
					High:    ev.High,
					Max:     ev.Max,
					OOM:     ev.OOM,
					OOMKill: ev.OOMKill,
				}
			}()
		}
	}()

	return evch2, errch, nil
}

func (me *CgroupV2Adapter) ToggleControllers(ctx context.Context) error {
	allControllers, err := me.cgroup.RootControllers()
	if err != nil {
		slog.ErrorContext(ctx, "failed to get root controllers", "error", err)
	} else {
		if err := me.cgroup.ToggleControllers(allControllers, cgroup2.Enable); err != nil {
			if userns.RunningInUserNS() {
				return errors.Errorf("failed to enable controllers in user namespace (%v): %w", allControllers, err)
			} else {
				return errors.Errorf("failed to enable controllers in os (%v): %w", allControllers, err)
			}
		}
	}

	return nil
}

func (a *CgroupV2Adapter) Stat(ctx context.Context) (*stats.Metrics, error) {
	return a.cgroup.Stat()
}

type item struct {
	id  string
	ev  cgroup2.Event
	err error
}
