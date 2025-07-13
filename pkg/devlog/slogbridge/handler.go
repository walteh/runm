package slogbridge

import (
	"context"
	"log/slog"

	"github.com/walteh/runm/pkg/devlog"
)

// SlogHandler implements slog.Handler and converts records to devlog entries
type SlogHandler struct {
	producer *SlogProducer
	consumer devlog.Consumer
	level    slog.Level
	groups   []string
	attrs    []slog.Attr
}

// NewSlogHandler creates a new SlogHandler
func NewSlogHandler(consumer devlog.Consumer, loggerName string, level slog.Level) *SlogHandler {
	producer := NewSlogProducer(consumer, loggerName)

	return &SlogHandler{
		producer: producer,
		consumer: consumer,
		level:    level,
		groups:   []string{},
		attrs:    []slog.Attr{},
	}
}

// Enabled implements slog.Handler.Enabled
func (h *SlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle implements slog.Handler.Handle
func (h *SlogHandler) Handle(ctx context.Context, record slog.Record) error {
	// Update producer with current groups and attrs
	h.producer.groups = h.groups
	h.producer.attrs = h.attrs

	// Convert record to devlog entry
	entry, err := h.producer.Produce(ctx, record)
	if err != nil {
		return err
	}

	// Send to consumer
	return h.consumer.Consume(ctx, entry)
}

// WithAttrs implements slog.Handler.WithAttrs
func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	// Create a copy with new attributes
	newHandler := *h
	newHandler.attrs = make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newHandler.attrs, h.attrs)
	copy(newHandler.attrs[len(h.attrs):], attrs)

	return &newHandler
}

// WithGroup implements slog.Handler.WithGroup
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	// Create a copy with new group
	newHandler := *h
	newHandler.groups = make([]string, len(h.groups)+1)
	copy(newHandler.groups, h.groups)
	newHandler.groups[len(h.groups)] = name

	return &newHandler
}
