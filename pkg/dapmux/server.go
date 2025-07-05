package dapmux

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	dapmuxv1 "github.com/walteh/runm/proto/dapmux/v1"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	targets        map[string]*dapmuxv1.Target
	targetsMu      sync.RWMutex
	activeTargetID string
	connections    map[string]net.Conn
	eventStreams   []chan *dapmuxv1.TargetEvent
	eventsMu       sync.RWMutex
}

func NewServer() *Server {

	return &Server{
		targets:     make(map[string]*dapmuxv1.Target),
		connections: make(map[string]net.Conn),
	}
}

func (s *Server) RegisterTarget(ctx context.Context, req *dapmuxv1.RegisterTargetRequest) (*dapmuxv1.RegisterTargetResponse, error) {
	slog.Info("Registering target", "process_name", req.GetProcessName(), "delve_addr", req.GetDelveAddr(), "location", req.GetLocation())

	// Connect to Delve instance
	conn, err := net.Dial("tcp", req.GetDelveAddr())
	if err != nil {
		return nil, errors.Errorf("failed to connect to Delve at %s: %w", req.GetDelveAddr(), err)
	}

	targetID := uuid.New().String()

	// Build connection info
	connInfo := &dapmuxv1.ConnectionInfo{}
	connInfo.SetRemoteAddr(conn.RemoteAddr().String())
	connInfo.SetLocalAddr(conn.LocalAddr().String())
	connInfo.SetProtocol("tcp")
	connInfo.SetConnectedAt(timestamppb.Now())

	// Build target
	target := &dapmuxv1.Target{}
	target.SetId(targetID)
	target.SetName(req.GetProcessName())
	target.SetAddr(req.GetDelveAddr())
	target.SetProcess(req.GetProcessName())
	target.SetLocation(req.GetLocation())
	target.SetRegisteredAt(timestamppb.Now())
	target.SetStatus(dapmuxv1.TargetStatus_TARGET_STATUS_ACTIVE)
	target.SetProcessId(req.GetProcessId())
	target.SetArgs(req.GetArgs())
	target.SetMetadata(req.GetMetadata())
	target.SetConnectionInfo(connInfo)

	s.targetsMu.Lock()
	s.targets[targetID] = target
	s.connections[targetID] = conn
	if s.activeTargetID == "" {
		s.activeTargetID = targetID
	}
	s.targetsMu.Unlock()

	// Broadcast target added event
	event := &dapmuxv1.TargetEvent{}
	event.SetType(dapmuxv1.TargetEventType_TARGET_EVENT_TYPE_ADDED)
	event.SetTarget(target)
	event.SetTimestamp(timestamppb.Now())
	event.SetDetails("Target registered")
	s.broadcastEvent(event)

	slog.Info("Target registered successfully", "target_id", targetID, "name", req.GetProcessName())

	resp := &dapmuxv1.RegisterTargetResponse{}
	resp.SetTargetId(targetID)
	resp.SetTarget(target)
	return resp, nil
}

func (s *Server) UnregisterTarget(ctx context.Context, req *dapmuxv1.UnregisterTargetRequest) (*dapmuxv1.UnregisterTargetResponse, error) {
	s.targetsMu.Lock()
	defer s.targetsMu.Unlock()

	target, exists := s.targets[req.GetTargetId()]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "target %s not found", req.GetTargetId())
	}

	// Close connection
	if conn, ok := s.connections[req.GetTargetId()]; ok {
		conn.Close()
		delete(s.connections, req.GetTargetId())
	}

	delete(s.targets, req.GetTargetId())

	// Clear active target if it was this one
	if s.activeTargetID == req.GetTargetId() {
		s.activeTargetID = ""
		for id, t := range s.targets {
			if t.GetStatus() == dapmuxv1.TargetStatus_TARGET_STATUS_ACTIVE {
				s.activeTargetID = id
				break
			}
		}
	}

	// Broadcast target removed event
	event := &dapmuxv1.TargetEvent{}
	event.SetType(dapmuxv1.TargetEventType_TARGET_EVENT_TYPE_REMOVED)
	event.SetTarget(target)
	event.SetTimestamp(timestamppb.Now())
	event.SetDetails("Target unregistered")
	s.broadcastEvent(event)

	slog.Info("Target unregistered", "target_id", req.GetTargetId(), "name", target.GetName())

	return &dapmuxv1.UnregisterTargetResponse{}, nil
}

func (s *Server) ListTargets(ctx context.Context, req *dapmuxv1.ListTargetsRequest) (*dapmuxv1.ListTargetsResponse, error) {
	s.targetsMu.RLock()
	defer s.targetsMu.RUnlock()

	var targets []*dapmuxv1.Target
	for _, target := range s.targets {
		// Apply filters if specified
		if len(req.GetStatusFilter()) > 0 {
			found := false
			for _, status := range req.GetStatusFilter() {
				if target.GetStatus() == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if req.GetLocationFilter() != "" && target.GetLocation() != req.GetLocationFilter() {
			continue
		}

		targets = append(targets, target)
	}

	resp := &dapmuxv1.ListTargetsResponse{}
	resp.SetTargets(targets)
	resp.SetActiveTargetId(s.activeTargetID)
	return resp, nil
}

func (s *Server) SetActiveTarget(ctx context.Context, req *dapmuxv1.SetActiveTargetRequest) (*dapmuxv1.SetActiveTargetResponse, error) {
	s.targetsMu.Lock()
	defer s.targetsMu.Unlock()

	target, exists := s.targets[req.GetTargetId()]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "target %s not found", req.GetTargetId())
	}

	s.activeTargetID = req.GetTargetId()
	slog.Info("Active target changed", "target_id", req.GetTargetId(), "name", target.GetName())

	resp := &dapmuxv1.SetActiveTargetResponse{}
	resp.SetTarget(target)
	return resp, nil
}

func (s *Server) ForwardDAPMessage(ctx context.Context, req *dapmuxv1.ForwardDAPMessageRequest) (*dapmuxv1.ForwardDAPMessageResponse, error) {
	s.targetsMu.RLock()
	conn, exists := s.connections[req.GetTargetId()]
	s.targetsMu.RUnlock()

	if !exists {
		return nil, status.Errorf(codes.NotFound, "target %s not found", req.GetTargetId())
	}

	if _, err := conn.Write(req.GetDapMessage()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to forward message to target %s: %v", req.GetTargetId(), err)
	}

	slog.Debug("Forwarded DAP message", "target_id", req.GetTargetId(), "seq", req.GetSequenceNumber())

	return &dapmuxv1.ForwardDAPMessageResponse{}, nil
}

func (s *Server) StreamTargetEvents(req *dapmuxv1.StreamTargetEventsRequest, stream grpc.ServerStreamingServer[dapmuxv1.TargetEvent]) error {
	eventChan := make(chan *dapmuxv1.TargetEvent, 100)

	s.eventsMu.Lock()
	s.eventStreams = append(s.eventStreams, eventChan)
	s.eventsMu.Unlock()

	defer func() {
		s.eventsMu.Lock()
		for i, ch := range s.eventStreams {
			if ch == eventChan {
				s.eventStreams = append(s.eventStreams[:i], s.eventStreams[i+1:]...)
				break
			}
		}
		s.eventsMu.Unlock()
		close(eventChan)
	}()

	for {
		select {
		case event := <-eventChan:
			if event == nil {
				return nil
			}

			// Apply filters if specified
			if len(req.GetEventTypes()) > 0 {
				found := false
				for _, eventType := range req.GetEventTypes() {
					if event.GetType() == eventType {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			if err := stream.Send(event); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

func (s *Server) Check(ctx context.Context, req *dapmuxv1.HealthCheckRequest) (*dapmuxv1.HealthCheckResponse, error) {
	resp := &dapmuxv1.HealthCheckResponse{}
	resp.SetStatus(dapmuxv1.HealthStatus_HEALTH_STATUS_SERVING)
	resp.SetMessage("DAP multiplexer is running")
	resp.SetDetails(map[string]string{
		"targets": fmt.Sprintf("%d", len(s.targets)),
	})
	return resp, nil
}

func (s *Server) Watch(req *dapmuxv1.HealthCheckRequest, stream grpc.ServerStreamingServer[dapmuxv1.HealthCheckResponse]) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			resp := &dapmuxv1.HealthCheckResponse{}
			resp.SetStatus(dapmuxv1.HealthStatus_HEALTH_STATUS_SERVING)
			resp.SetMessage("DAP multiplexer is running")
			resp.SetDetails(map[string]string{
				"targets": fmt.Sprintf("%d", len(s.targets)),
			})
			if err := stream.Send(resp); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

func (s *Server) broadcastEvent(event *dapmuxv1.TargetEvent) {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()

	for _, ch := range s.eventStreams {
		select {
		case ch <- event:
		default:
			// Channel is full, skip this stream
		}
	}
}
