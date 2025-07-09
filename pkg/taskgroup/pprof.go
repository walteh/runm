package taskgroup

import (
	"context"
	"runtime/pprof"
)

// PprofHelper provides utilities for working with pprof labels
type PprofHelper struct {
	tg *TaskGroup
}

// GetCurrentLabels returns the current goroutine's pprof labels
// This works when called from within a task with pprof enabled
func (ph *PprofHelper) GetCurrentLabels(ctx context.Context) map[string]string {
	labels := make(map[string]string)

	// First try the passed context (it might be a labeled context)
	hasLabels := false
	pprof.ForLabels(ctx, func(key, value string) bool {
		labels[key] = value
		hasLabels = true
		return true
	})

	// If the passed context doesn't have labels, try the stored context
	if !hasLabels {
		goroutineID := getGoroutineID()
		if labeledCtx, ok := ph.tg.pprofContexts.Load(goroutineID); ok {
			pprof.ForLabels(labeledCtx, func(key, value string) bool {
				labels[key] = value
				return true
			})
		}
	}

	return labels
}

// GetTaskLabel returns a specific label value for the current task
// This must be called from within a pprof.Do context
func (ph *PprofHelper) GetTaskLabel(ctx context.Context, key string) (string, bool) {
	// First try the passed context (it might be a labeled context)
	if value, ok := pprof.Label(ctx, key); ok && value != "" {
		return value, true
	}

	// If the passed context doesn't have the label, try the stored context
	goroutineID := getGoroutineID()
	if labeledCtx, ok := ph.tg.pprofContexts.Load(goroutineID); ok {
		return pprof.Label(labeledCtx, key)
	}
	return "", false
}

// GetTaskIDFromLabels returns the task ID from pprof labels
// This must be called from within a pprof.Do context
func (ph *PprofHelper) GetTaskIDFromLabels(ctx context.Context) (string, bool) {
	// First try the passed context (it might be a labeled context)
	if value, ok := pprof.Label(ctx, "task_id"); ok && value != "" {
		return value, true
	}

	// If the passed context doesn't have the label, try the stored context
	goroutineID := getGoroutineID()
	if labeledCtx, ok := ph.tg.pprofContexts.Load(goroutineID); ok {
		return pprof.Label(labeledCtx, "task_id")
	}
	return "", false
}

// GetTaskNameFromLabels returns the task name from pprof labels
// This must be called from within a pprof.Do context
func (ph *PprofHelper) GetTaskNameFromLabels(ctx context.Context) (string, bool) {
	// First try the passed context (it might be a labeled context)
	if value, ok := pprof.Label(ctx, "task_name"); ok && value != "" {
		return value, true
	}

	// If the passed context doesn't have the label, try the stored context
	goroutineID := getGoroutineID()
	if labeledCtx, ok := ph.tg.pprofContexts.Load(goroutineID); ok {
		return pprof.Label(labeledCtx, "task_name")
	}
	return "", false
}

// WithAdditionalLabels executes a function with additional pprof labels
func (ph *PprofHelper) WithAdditionalLabels(ctx context.Context, additionalLabels map[string]string, fn func(context.Context)) {
	if len(additionalLabels) == 0 {
		fn(ctx)
		return
	}

	labelSlice := make([]string, 0, len(additionalLabels)*2)
	for k, v := range additionalLabels {
		labelSlice = append(labelSlice, k, v)
	}

	labels := pprof.Labels(labelSlice...)
	pprof.Do(ctx, labels, fn)
}

// UpdateTaskStage updates the task stage in pprof labels
func (ph *PprofHelper) UpdateTaskStage(ctx context.Context, stage string) {
	goroutineID := getGoroutineID()
	if labeledCtx, ok := ph.tg.pprofContexts.Load(goroutineID); ok {
		// Create new labels with the stage added to the existing labels
		labels := pprof.Labels("task_stage", stage)
		newCtx := pprof.WithLabels(labeledCtx, labels)
		ph.tg.pprofContexts.Store(goroutineID, newCtx)
	}
}

// CreateChildTaskLabels creates labels for child tasks that inherit parent context
func (ph *PprofHelper) CreateChildTaskLabels(parentCtx context.Context, childTaskName string) pprof.LabelSet {
	return pprof.Labels(
		"task_type", "child",
		"child_task_name", childTaskName,
		"parent_task_id", func() string {
			// First try the passed context
			if id, ok := pprof.Label(parentCtx, "task_id"); ok && id != "" {
				return id
			}

			// If the passed context doesn't have the label, try the stored context
			goroutineID := getGoroutineID()
			if labeledCtx, ok := ph.tg.pprofContexts.Load(goroutineID); ok {
				if id, ok := pprof.Label(labeledCtx, "task_id"); ok {
					return id
				}
			}
			return "unknown"
		}(),
	)
}
