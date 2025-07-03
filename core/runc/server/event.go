//go:build !windows

package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.EventServiceServer = (*Server)(nil)

func (s *Server) SubscribeToReaperExits(_ *emptypb.Empty, srv grpc.ServerStreamingServer[runmv1.ReaperExit]) error {

	// if s.customExitChan != nil {
	// 	go func() {
	// 		for exit := range s.customExitChan {
	// 			go func() {
	// 				payload := &runmv1.ReaperExit{}
	// 				payload.SetStatus(int32(exit.Status))
	// 				payload.SetPid(int32(exit.Pid))
	// 				payload.SetTimestamp(timestamppb.New(exit.Timestamp))
	// 				if err := srv.Send(payload); err != nil {
	// 					slog.ErrorContext(srv.Context(), "failed to send reaper exit", "error", err)
	// 				}
	// 			}()
	// 		}
	// 	}()
	// }

	slog.InfoContext(srv.Context(), "subscribing to reaper exits")

	defer func() {
		slog.InfoContext(srv.Context(), "unsubscribing from reaper exits")
	}()

	exits, err := s.runtime.SubscribeToReaperExits(srv.Context())
	if err != nil {
		slog.ErrorContext(srv.Context(), "failed to subscribe to reaper exits", "error", err)
		return errors.Errorf("failed to subscribe to reaper exits: %w", err)
	}

	for exit := range exits {
		slog.InfoContext(srv.Context(), "reaper exit", "pid", exit.Pid, "status", exit.Status)
		payload := &runmv1.ReaperExit{}
		payload.SetStatus(int32(exit.Status))
		payload.SetPid(int32(exit.Pid))
		payload.SetTimestamp(timestamppb.New(exit.Timestamp))
		if err := srv.Send(payload); err != nil {
			return err
		}
	}

	return nil
}

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
