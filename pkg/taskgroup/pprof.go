package taskgroup

import (
	"context"
	"runtime/pprof"
)

// PprofHelper provides utilities for working with pprof labels
type PprofHelper struct {
	tg *TaskGroup
}

// GetCurrentLabels returns the current goroutine's pprof labels from the passed context
func (ph *PprofHelper) GetCurrentLabels(ctx context.Context) map[string]string {
	labels := make(map[string]string)
	pprof.ForLabels(ctx, func(key, value string) bool {
		labels[key] = value
		return true
	})
	return labels
}

// GetTaskLabel returns a specific label value for the current task from the passed context
func (ph *PprofHelper) GetTaskLabel(ctx context.Context, key string) (string, bool) {
	return pprof.Label(ctx, key)
}

// GetTaskIDFromLabels returns the task ID from pprof labels in the passed context
func (ph *PprofHelper) GetTaskIDFromLabels(ctx context.Context) (string, bool) {
	return pprof.Label(ctx, "task_id")
}

// GetTaskNameFromLabels returns the task name from pprof labels in the passed context
func (ph *PprofHelper) GetTaskNameFromLabels(ctx context.Context) (string, bool) {
	return pprof.Label(ctx, "task_name")
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

// CreateChildTaskLabels creates labels for child tasks that inherit parent context
func (ph *PprofHelper) CreateChildTaskLabels(parentCtx context.Context, childTaskName string) pprof.LabelSet {
	parentTaskID := "unknown"
	if id, ok := pprof.Label(parentCtx, "task_id"); ok && id != "" {
		parentTaskID = id
	}

	return pprof.Labels(
		"task_type", "child",
		"child_task_name", childTaskName,
		"parent_task_id", parentTaskID,
	)
}
