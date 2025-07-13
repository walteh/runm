package devlog

import (
	"context"
	"log/slog"

	"google.golang.org/protobuf/types/known/timestamppb"

	devlogv1 "github.com/walteh/runm/proto/devlog/v1"
)

// Handler is the interface for outputting devlog entries.
// This abstracts the destination (console, file, network, otel, etc.)
type Handler interface {
	// Handle processes a log entry
	Handle(ctx context.Context, entry *Entry) error

	// WithAttrs returns a new Handler with additional attributes
	WithAttrs(attrs []slog.Attr) Handler

	// WithGroup returns a new Handler with a group name
	WithGroup(name string) Handler

	// Enabled returns whether logging is enabled for the given level
	Enabled(ctx context.Context, level slog.Level) bool
}

// Producer is the interface for creating devlog entries.
// This abstracts the source (slog, raw logs, etc.)
type Producer interface {
	// Produce converts source data to devlog entries
	Produce(ctx context.Context, data interface{}) (*Entry, error)

	// Close shuts down the producer
	Close() error
}

// Consumer is the interface for receiving devlog entries and routing them.
// This handles the middleware layer between producers and handlers.
type Consumer interface {
	// Consume processes entries from producers and routes to handlers
	Consume(ctx context.Context, entry *Entry) error

	// AddHandler adds a handler to receive entries
	AddHandler(handler Handler)

	// RemoveHandler removes a handler
	RemoveHandler(handler Handler)

	// Close shuts down the consumer
	Close() error
}

// Entry represents a unified log entry that can be either structured or raw
type Entry struct {
	// Type indicates whether this is a structured or raw log
	Type EntryType

	// Structured contains structured log data (if Type == EntryTypeStructured)
	Structured *devlogv1.StructuredLog

	// Raw contains raw log data (if Type == EntryTypeRaw)
	Raw *devlogv1.RawLog
}

// EntryType represents the type of log entry
type EntryType int

const (
	EntryTypeUnspecified EntryType = iota
	EntryTypeStructured
	EntryTypeRaw
)

// String returns the string representation of the entry type
func (t EntryType) String() string {
	switch t {
	case EntryTypeStructured:
		return "structured"
	case EntryTypeRaw:
		return "raw"
	default:
		return "unspecified"
	}
}

// IsStructured returns true if the entry contains structured log data
func (e *Entry) IsStructured() bool {
	return e.Type == EntryTypeStructured && e.Structured != nil
}

// IsRaw returns true if the entry contains raw log data
func (e *Entry) IsRaw() bool {
	return e.Type == EntryTypeRaw && e.Raw != nil
}

// GetTimestamp returns the timestamp of the entry regardless of type
func (e *Entry) GetTimestamp() *timestamppb.Timestamp {
	switch e.Type {
	case EntryTypeStructured:
		if e.Structured != nil {
			return e.Structured.GetTimestamp()
		}
	case EntryTypeRaw:
		if e.Raw != nil {
			return e.Raw.GetTimestamp()
		}
	}
	return nil
}

// GetLoggerName returns the logger name regardless of type
func (e *Entry) GetLoggerName() string {
	switch e.Type {
	case EntryTypeStructured:
		if e.Structured != nil {
			return e.Structured.GetLoggerName()
		}
	case EntryTypeRaw:
		if e.Raw != nil {
			return e.Raw.GetLoggerName()
		}
	}
	return ""
}

// GetTraceID returns the trace ID regardless of type
func (e *Entry) GetTraceID() string {
	switch e.Type {
	case EntryTypeStructured:
		if e.Structured != nil {
			return e.Structured.GetTraceId()
		}
	case EntryTypeRaw:
		if e.Raw != nil {
			return e.Raw.GetTraceId()
		}
	}
	return ""
}
