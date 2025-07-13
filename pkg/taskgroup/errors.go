package taskgroup

import (
	"fmt"
	"sort"
	"strings"
)

// TaskGroupError represents multiple task errors collected during execution
type TaskGroupError struct {
	TaskErrors map[string]error // map of task name to error
	PanicCount int              // number of tasks that panicked
	FailCount  int              // number of tasks that failed
}

// Error implements the error interface
func (tge *TaskGroupError) Error() string {
	if len(tge.TaskErrors) == 0 {
		return "taskgroup completed with no errors"
	}

	var parts []string

	// Count different types of failures
	failedCount := 0
	panickedCount := 0
	for _, err := range tge.TaskErrors {
		if strings.Contains(err.Error(), "panic in task:") {
			panickedCount++
		} else {
			failedCount++
		}
	}

	// Create summary
	summary := fmt.Sprintf("taskgroup failed with %d error(s)", len(tge.TaskErrors))
	if failedCount > 0 && panickedCount > 0 {
		summary = fmt.Sprintf("taskgroup failed with %d error(s) (%d failed, %d panicked)",
			len(tge.TaskErrors), failedCount, panickedCount)
	} else if panickedCount > 0 {
		summary = fmt.Sprintf("taskgroup failed with %d panic(s)", panickedCount)
	}

	parts = append(parts, summary)

	// Sort task names for consistent output
	var taskNames []string
	for taskName := range tge.TaskErrors {
		taskNames = append(taskNames, taskName)
	}
	sort.Strings(taskNames)

	// Add detailed errors
	for _, taskName := range taskNames {
		err := tge.TaskErrors[taskName]
		parts = append(parts, fmt.Sprintf("  - %s: %s", taskName, err.Error()))
	}

	return strings.Join(parts, "\n")
}

// Unwrap returns the underlying errors for Go 1.20+ error handling
func (tge *TaskGroupError) Unwrap() []error {
	var errs []error
	for _, err := range tge.TaskErrors {
		errs = append(errs, err)
	}
	return errs
}

// HasErrors returns true if there are any task errors
func (tge *TaskGroupError) HasErrors() bool {
	return len(tge.TaskErrors) > 0
}

// GetTaskError returns the error for a specific task name
func (tge *TaskGroupError) GetTaskError(taskName string) (error, bool) {
	err, exists := tge.TaskErrors[taskName]
	return err, exists
}

// GetFailedTaskNames returns the names of all failed tasks
func (tge *TaskGroupError) GetFailedTaskNames() []string {
	var names []string
	for taskName := range tge.TaskErrors {
		names = append(names, taskName)
	}
	sort.Strings(names)
	return names
}

// NewTaskGroupError creates a new TaskGroupError
func NewTaskGroupError() *TaskGroupError {
	return &TaskGroupError{
		TaskErrors: make(map[string]error),
	}
}

// AddTaskError adds an error for a specific task
func (tge *TaskGroupError) AddTaskError(taskName string, err error) {
	if err != nil {
		tge.TaskErrors[taskName] = err
		if strings.Contains(err.Error(), "panic in task:") {
			tge.PanicCount++
		} else {
			tge.FailCount++
		}
	}
}
