package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.EventServiceServer = (*Server)(nil)

// PublishEvent implements runmv1.EventServiceServer.
func (s *Server) PublishEvent(ctx context.Context, req *runmv1.PublishEventRequest) (*runmv1.PublishEventResponse, error) {
	reqdef := &runtime.PublishEvent{}
	reqdef.Topic = req.GetTopic()
	reqdef.Data = req.GetRawJson()

	err := s.eventHandler.Publish(ctx, reqdef)
	if err != nil {
		return nil, errors.Errorf("failed to publish event: %w", err)
	}
	return &runmv1.PublishEventResponse{}, nil
}

// PublishEvents implements runmv1.EventServiceServer.
func (s *Server) ReceiveEvents(_ *emptypb.Empty, srv grpc.ServerStreamingServer[runmv1.PublishEventsResponse]) error {
	rec, err := s.eventHandler.Receive(srv.Context())
	if err != nil {
		return err
	}

	errch := make(chan error)

	defer func() {
		close(errch)
	}()

	for event := range rec {
		go func() {
			resp := &runmv1.PublishEventsResponse{}
			resp.SetTopic(event.Topic)
			by, err := json.Marshal(event.Data)
			if err != nil {
				errch <- errors.Errorf("failed to marshal json event data: %w", err)
				return
			}
			resp.SetRawJson(by)
			if err := srv.Send(resp); err != nil {
				errch <- err
			}
		}()
	}

	for {
		select {
		case <-srv.Context().Done():
			return srv.Context().Err()
		case err := <-errch:
			slog.Error("failed to send event", "error", err)
			return err
		}
	}
}
