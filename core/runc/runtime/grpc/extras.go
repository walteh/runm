package grpcruntime

import (
	"context"
	"log/slog"
	"time"

	"gitlab.com/tozd/go/errors"
	"google.golang.org/protobuf/types/known/durationpb"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/conversion"
	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runtime.RuntimeExtras = (*GRPCClientRuntime)(nil)

// Stats returns the stats for a container like cpu, memory, and io.
func (c *GRPCClientRuntime) Stats(ctx context.Context, id string) (*gorunc.Stats, error) {
	req := &runmv1.RuncStatsRequest{}
	req.SetId(id)

	resp, err := c.runtimeExtrasGprcService.Stats(ctx, req)
	if err != nil {
		return nil, errors.Errorf("getting stats: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return nil, errors.New(resp.GetGoError())
	// }
	stats, err := conversion.ConvertStatsFromProto(resp.GetStats())
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// List returns all containers created inside the provided runc root directory.
func (c *GRPCClientRuntime) List(ctx context.Context) ([]*gorunc.Container, error) {
	panic("unimplemented")
}

// State returns the state for the container provided by id.
func (c *GRPCClientRuntime) State(ctx context.Context, id string) (*gorunc.Container, error) {
	panic("unimplemented")
}

// Run runs the create, start, delete lifecycle of the container.
func (c *GRPCClientRuntime) RuncRun(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) (int, error) {
	panic("unimplemented")
}

func (c *GRPCClientRuntime) Events(ctx context.Context, id string, duration time.Duration) (chan *gorunc.Event, error) {
	req := &runmv1.RuncEventsRequest{}
	req.SetId(id)
	req.SetDuration(durationpb.New(duration))

	stream, err := c.runtimeExtrasGprcService.Events(ctx, req)
	if err != nil {
		return nil, err
	}

	events := make(chan *gorunc.Event)

	go func() {
		defer stream.CloseSend()
		defer close(events)

		for {
			event, err := stream.Recv()
			if err != nil {
				slog.Error("failed to receive event", "error", err)
				return
			}

			eventz, err := conversion.ConvertEventFromProto(event)
			if err != nil {
				slog.Error("failed to convert event", "error", err)
				return
			}

			events <- eventz
		}
	}()

	return events, nil
}

// Top lists all the processes inside the container returning the full ps data.
func (c *GRPCClientRuntime) Top(ctx context.Context, id string, psOptions string) (*gorunc.TopResults, error) {
	req := &runmv1.RuncTopRequest{}
	req.SetId(id)
	req.SetPsOptions(psOptions)

	resp, err := c.runtimeExtrasGprcService.Top(ctx, req)
	if err != nil {
		return nil, errors.Errorf("getting top: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return nil, errors.New(resp.GetGoError())
	// }

	results := conversion.ConvertTopResultsFromProto(resp.GetResults())

	return results, nil
}

// Version returns the runc and runtime-spec versions.
func (c *GRPCClientRuntime) Version(ctx context.Context) (gorunc.Version, error) {
	resp, err := c.runtimeExtrasGprcService.Version(ctx, &runmv1.RuncVersionRequest{})
	if err != nil {
		return gorunc.Version{}, errors.Errorf("getting version: %w", err)
	}
	// if resp.GetGoError() != "" {
	// 	return gorunc.Version{}, errors.New(resp.GetGoError())
	// }
	return gorunc.Version{
		Runc:   resp.GetRunc(),
		Spec:   resp.GetSpec(),
		Commit: resp.GetCommit(),
	}, nil
}
