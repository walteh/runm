//go:build !windows

package grpcruntime

import (
	"context"

	"github.com/containerd/cgroups/v3/cgroup2/stats"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runtime.CgroupAdapter = (*GRPCClientRuntime)(nil)

// EventChan implements runtime.CgroupAdapter.
func (me *GRPCClientRuntime) OpenEventChan(ctx context.Context) (<-chan *runmv1.CgroupEvent, <-chan error, error) {

	stream, err := me.guestCgroupAdapterService.StreamCgroupEvents(ctx, &runmv1.StreamCgroupEventsRequest{})
	if err != nil {
		return nil, nil, errors.Errorf("failed to open event channel: %w", err)
	}

	errch := make(chan error)
	rch := make(chan *runmv1.CgroupEvent)

	go func() {
		for {
			refId, err := stream.Recv()
			if err != nil {
				errch <- err
			} else {
				rch <- refId.GetEvent()
			}
		}
	}()

	return rch, errch, nil
}

// Stat implements runtime.CgroupAdapter.
func (me *GRPCClientRuntime) Stat(ctx context.Context) (*stats.Metrics, error) {
	var stats *stats.Metrics

	res, err := me.guestCgroupAdapterService.GetCgroupStats(ctx, &runmv1.GetCgroupStatsRequest{})
	if err != nil {
		return nil, errors.Errorf("failed to get cgroup stats: %w", err)
	}

	if err := res.GetStats().UnmarshalTo(stats); err != nil {
		return nil, errors.Errorf("failed to unmarshal cgroup stats: %w", err)
	}

	return stats, nil
}

// ToggleControllers implements runtime.CgroupAdapter.
func (me *GRPCClientRuntime) ToggleControllers(ctx context.Context) error {
	_, err := me.guestCgroupAdapterService.ToggleAllControllers(ctx, &runmv1.ToggleAllControllersRequest{})
	if err != nil {
		return errors.Errorf("failed to toggle cgroup controllers: %w", err)
	}
	return nil
}
