package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.CgroupAdapterServiceServer = (*Server)(nil)

// GetCgroupStats implements runmv1.CgroupAdapterServiceServer.
func (s *Server) GetCgroupStats(ctx context.Context, _ *runmv1.GetCgroupStatsRequest) (*runmv1.GetCgroupStatsResponse, error) {
	statz, err := s.cgroupAdapter.Stat(ctx)
	if err != nil {
		return nil, err
	}

	a := &anypb.Any{}

	if err = anypb.MarshalFrom(a, statz, proto.MarshalOptions{
		AllowPartial: true,
	}); err != nil {
		return nil, err
	}

	resp := &runmv1.GetCgroupStatsResponse{}
	resp.SetStats(a)
	return resp, nil
}

// StreamCgroupEvents implements runmv1.GuestCgroupServiceServer.
func (s *Server) StreamCgroupEvents(_ *runmv1.StreamCgroupEventsRequest, srv grpc.ServerStreamingServer[runmv1.StreamCgroupEventsResponse]) error {
	eventCh, errCh, err := s.cgroupAdapter.OpenEventChan(srv.Context())
	if err != nil {
		return err
	}

	oerrch := make(chan error)

	for {
		select {
		case event := <-eventCh:
			go func() {
				rev := &runmv1.CgroupEvent{}
				rev.SetHigh(event.High)
				rev.SetMax(event.Max)
				rev.SetOom(event.OOM)
				rev.SetOomKill(event.OOMKill)
				rev.SetLow(event.Low)
				resp := &runmv1.StreamCgroupEventsResponse{}
				resp.SetEvent(rev)
				if err := srv.Send(resp); err != nil {
					oerrch <- err
				}
			}()
		case err := <-errCh:
			return err
		case err := <-oerrch:
			return err
		}
	}
}

// ToggleAllControllers implements runmv1.GuestCgroupServiceServer.
func (s *Server) ToggleAllControllers(ctx context.Context, _ *runmv1.ToggleAllControllersRequest) (*runmv1.ToggleAllControllersResponse, error) {
	err := s.cgroupAdapter.ToggleControllers(ctx)
	if err != nil {
		return nil, err
	}
	return &runmv1.ToggleAllControllersResponse{}, nil
}
