package taskgroup

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type TaskFunc func(ctx context.Context) error

// CleanupFunc represents a cleanup function that can be registered with a TaskGroup
type CleanupFunc func(ctx context.Context) error

// CleanupEntry represents a named cleanup function
type CleanupEntry struct {
	Name string
	Func CleanupFunc
}

type TaskStatus int

const (
	TaskStatusPending TaskStatus = iota
	TaskStatusRunning
	TaskStatusCompleted
	TaskStatusFailed
	TaskStatusPanicked
	TaskStatusCanceled
)

func (s TaskStatus) String() string {
	switch s {
	case TaskStatusPending:
		return "pending"
	case TaskStatusRunning:
		return "running"
	case TaskStatusCompleted:
		return "completed"
	case TaskStatusFailed:
		return "failed"
	case TaskStatusPanicked:
		return "panicked"
	case TaskStatusCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

type TaskState struct {
	ID          string
	IDX         int
	Group       *TaskGroup
	Name        string
	Status      TaskStatus
	StartTime   time.Time
	EndTime     time.Time
	Duration    time.Duration
	Error       error
	PanicInfo   *PanicInfo
	Metadata    map[string]any
	GoroutineID uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PanicInfo struct {
	Value       any
	Stack       string
	Timestamp   time.Time
	GoroutineID uint64
}

func (t *TaskState) IsRunning() bool {
	return t.Status == TaskStatusRunning
}

func (t *TaskState) IsCompleted() bool {
	return t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed || t.Status == TaskStatusPanicked
}

var _ slog.LogValuer = (*TaskState)(nil)

func (t *TaskState) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("name", t.Group.opts.name+":"+t.Name),
		slog.String("status", t.Status.String()),
		slog.Duration("dur", time.Since(t.StartTime)),
	}

	if t.IsCompleted() {
		attrs = append(attrs, slog.Time("end_time", t.EndTime))
	}

	if t.Error != nil {
		attrs = append(attrs, slog.String("error", t.Error.Error()))
	}

	if t.PanicInfo != nil {
		attrs = append(attrs, slog.Group("panic",
			slog.String("value", fmt.Sprintf("%v", t.PanicInfo.Value)),
			slog.Time("timestamp", t.PanicInfo.Timestamp),
			slog.String("stack", t.PanicInfo.Stack),
		))
	}

	if len(t.Metadata) > 0 {
		var metaAttrs []any
		for k, v := range t.Metadata {
			metaAttrs = append(metaAttrs, slog.Any(k, v))
		}
		attrs = append(attrs, slog.Group("_metadata", metaAttrs...))
	}

	return slog.GroupValue(attrs...)
}
