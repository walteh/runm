package server

import (
	"context"

	"google.golang.org/grpc"

	"github.com/walteh/runm/core/runc/conversion"
	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.RuncExtrasServiceServer = (*Server)(nil)

////////////////////////////////////////////////////////////
// RuntimeExtras
////////////////////////////////////////////////////////////

func (s *Server) Run(ctx context.Context, req *runmv1.RuncRunRequest) (*runmv1.RuncRunResponse, error) {
	resp := &runmv1.RuncRunResponse{}

	opts, err := conversion.ConvertCreateOptsFromProto(ctx, req.GetOptions(), s.state)
	if err != nil {
		return nil, err
	}

	status, err := s.runtimeExtras.Run(ctx, req.GetId(), req.GetBundle(), opts)
	if err != nil {
		resp.SetGoError(err.Error())
	}
	resp.SetStatus(int32(status))
	return resp, nil
}

// Events implements runmv1.RuncServiceServer.
func (s *Server) Events(*runmv1.RuncEventsRequest, grpc.ServerStreamingServer[runmv1.RuncEvent]) error {
	return runtime.ReflectNotImplementedError()
}

// Stats implements the RuncServiceServer Stats method.
func (s *Server) Stats(ctx context.Context, req *runmv1.RuncStatsRequest) (*runmv1.RuncStatsResponse, error) {
	resp := &runmv1.RuncStatsResponse{}

	stats, err := s.runtimeExtras.Stats(ctx, req.GetId())
	if err != nil {
		resp.SetGoError(err.Error())
		return resp, nil
	}

	runcStats, err := conversion.ConvertStatsToProto(stats)
	if err != nil {
		resp.SetGoError(err.Error())
		return resp, nil
	}
	resp.SetStats(runcStats)
	return resp, nil
}

// Top implements the RuncServiceServer Top method.
func (s *Server) Top(ctx context.Context, req *runmv1.RuncTopRequest) (*runmv1.RuncTopResponse, error) {
	resp := &runmv1.RuncTopResponse{}

	topResults, err := s.runtimeExtras.Top(ctx, req.GetId(), req.GetPsOptions())
	if err != nil {
		resp.SetGoError(err.Error())
		return resp, nil
	}

	resp.SetResults(conversion.ConvertTopResultsToProto(topResults))

	return resp, nil
}

// State implements the RuncServiceServer State method.
func (s *Server) State(ctx context.Context, req *runmv1.RuncStateRequest) (*runmv1.RuncStateResponse, error) {
	resp := &runmv1.RuncStateResponse{}

	container, err := s.runtimeExtras.State(ctx, req.GetId())
	if err != nil {
		resp.SetGoError(err.Error())
		return resp, nil
	}

	containerz, err := conversion.ConvertContainerToProto(container)
	if err != nil {
		resp.SetGoError(err.Error())
		return resp, nil
	}

	resp.SetContainer(containerz)
	return resp, nil
}

// List implements the RuncServiceServer List method.
func (s *Server) List(ctx context.Context, req *runmv1.RuncListRequest) (*runmv1.RuncListResponse, error) {
	resp := &runmv1.RuncListResponse{}

	containers, err := s.runtimeExtras.List(ctx)
	if err != nil {
		resp.SetGoError(err.Error())
		return resp, nil
	}

	runcContainers := make([]*runmv1.RuncContainer, len(containers))
	for i, container := range containers {
		c := &runmv1.RuncContainer_builder{
			Id:               container.ID,
			Pid:              int32(container.Pid),
			Status:           container.Status,
			Bundle:           container.Bundle,
			Rootfs:           container.Rootfs,
			CreatedTimestamp: container.Created.UnixNano(),
			Annotations:      container.Annotations,
		}
		runcContainers[i] = c.Build()
	}

	resp.SetContainers(runcContainers)
	return resp, nil
}

// Version implements the RuncServiceServer Version method.
func (s *Server) Version(ctx context.Context, req *runmv1.RuncVersionRequest) (*runmv1.RuncVersionResponse, error) {
	resp := &runmv1.RuncVersionResponse{}

	version, err := s.runtimeExtras.Version(ctx)
	if err != nil {
		resp.SetGoError(err.Error())
		return resp, nil
	}

	resp.SetRunc(version.Runc)
	resp.SetCommit(version.Commit)
	resp.SetSpec(version.Spec)

	return resp, nil
}
