package server

import (
	"context"
	"log/slog"
	"net"

	"github.com/walteh/runm/pkg/dapmux"
)

//go:generate mkdir -p mocks
//go:generate go run github.com/golang/mock/mockgen -source=interfaces.go -destination=mocks/mock_interfaces.go -package=mocks

// Server is the main interface for the DAP multiplexing server
//
//go:mock
type Server interface {
	dapmux.Server

	// AddEventListener adds a listener for server events
	AddEventListener(listener dapmux.EventListener)

	// RemoveEventListener removes an event listener
	RemoveEventListener(listener dapmux.EventListener)

	// GetConnectionInfo returns information about server connections
	GetConnectionInfo() ConnectionInfo
}

// ConnectionManager handles network connections
//
//go:mock
type ConnectionManager interface {
	// StartListening starts listening for connections
	StartListening(ctx context.Context, addr string) error

	// StopListening stops listening for new connections
	StopListening(ctx context.Context) error

	// AcceptConnection accepts a new connection
	AcceptConnection(ctx context.Context) (net.Conn, error)

	// GetActiveConnections returns all active connections
	GetActiveConnections() []net.Conn

	// CloseConnection closes a specific connection
	CloseConnection(conn net.Conn) error
}

// TargetManager manages debug targets
//
//go:mock
type TargetManager interface {
	// RegisterTarget registers a new debug target
	RegisterTarget(ctx context.Context, req *dapmux.RegistrationRequest) (*dapmux.RegistrationResponse, error)

	// UnregisterTarget removes a debug target
	UnregisterTarget(ctx context.Context, targetID string) error

	// GetTarget returns a specific target by ID
	GetTarget(targetID string) (*dapmux.Target, error)

	// GetAllTargets returns all registered targets
	GetAllTargets() []dapmux.Target

	// SetActiveTarget sets the active target for debugging
	SetActiveTarget(targetID string) error

	// GetActiveTarget returns the currently active target
	GetActiveTarget() (*dapmux.Target, error)

	// UpdateTargetStatus updates a target's status
	UpdateTargetStatus(targetID string, status dapmux.TargetStatus) error
}

// MessageRouter handles DAP message routing
//
//go:mock
type MessageRouter interface {
	// RouteMessage routes a DAP message to the appropriate target
	RouteMessage(ctx context.Context, message *DAPMessage) error

	// ForwardToTarget forwards a message to a specific target
	ForwardToTarget(ctx context.Context, targetID string, message []byte) error

	// ForwardFromTarget forwards a message from a target to VSCode
	ForwardFromTarget(ctx context.Context, targetID string, message []byte) error

	// BroadcastMessage broadcasts a message to all targets
	BroadcastMessage(ctx context.Context, message []byte) error
}

// EventBus handles event distribution
//
//go:mock
type EventBus interface {
	// Publish publishes an event to all listeners
	Publish(ctx context.Context, event *Event) error

	// Subscribe subscribes to events
	Subscribe(ctx context.Context, listener dapmux.EventListener) error

	// Unsubscribe removes a listener
	Unsubscribe(ctx context.Context, listener dapmux.EventListener) error
}

// Config represents server configuration
type Config struct {
	ListenAddr       string
	RegistrationAddr string
	Logger           *slog.Logger
	MaxTargets       int
	ConnectTimeout   int
	DebugMode        bool
}

// ConnectionInfo provides information about server connections
type ConnectionInfo struct {
	VSCodeConnections     int
	TargetConnections     int
	TotalBytesTransferred int64
	ActiveSince           int64
}

// DAPMessage represents a DAP protocol message
type DAPMessage struct {
	Type      string                 `json:"type"`
	Seq       int                    `json:"seq"`
	Command   string                 `json:"command,omitempty"`
	Event     string                 `json:"event,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Body      map[string]interface{} `json:"body,omitempty"`
	TargetID  string                 `json:"targetId,omitempty"`
}

// Event represents a server event
type Event struct {
	Type      EventType              `json:"type"`
	TargetID  string                 `json:"targetId,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// EventType represents the type of server event
type EventType string

const (
	EventTypeTargetAdded         EventType = "target_added"
	EventTypeTargetRemoved       EventType = "target_removed"
	EventTypeTargetStatusChanged EventType = "target_status_changed"
	EventTypeVSCodeConnected     EventType = "vscode_connected"
	EventTypeVSCodeDisconnected  EventType = "vscode_disconnected"
	EventTypeMessageRouted       EventType = "message_routed"
	EventTypeError               EventType = "error"
)
