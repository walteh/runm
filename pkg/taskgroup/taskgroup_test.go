package taskgroup_test

import (
	"context"
	"errors"
	"log/slog"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/walteh/runm/pkg/taskgroup"
)

func TestTaskGroup_BasicUsage(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	var counter int64
	tg.Go(func(context.Context) error {
		atomic.AddInt64(&counter, 1)
		return nil
	})

	tg.Go(func(context.Context) error {
		atomic.AddInt64(&counter, 1)
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.Equal(t, int64(2), atomic.LoadInt64(&counter))
}

func TestTaskGroup_WithError(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	expectedErr := errors.New("test error")
	tg.Go(func(context.Context) error {
		return expectedErr
	})

	tg.Go(func(context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	err := tg.Wait()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "test error")
}

func TestTaskGroup_WithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tg := taskgroup.NewTaskGroup(ctx)

	var started bool
	tg.Go(func(context.Context) error {
		started = true
		<-ctx.Done()
		return ctx.Err()
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := tg.Wait()
	assert.Error(t, err)
	assert.True(t, started)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestTaskGroup_WithTimeout(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithTimeout(100*time.Millisecond))

	tg.Go(func(context.Context) error {
		// Use the taskgroup context to respect timeout
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-tg.Context().Done():
			return tg.Context().Err()
		}
	})

	start := time.Now()
	err := tg.Wait()
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
	assert.Less(t, elapsed, 150*time.Millisecond)
}

func TestTaskGroup_WithLimit(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithMaxConcurrent(2))

	var concurrent int64
	var maxConcurrent int64
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		tg.Go(func(context.Context) error {
			defer wg.Done()
			current := atomic.AddInt64(&concurrent, 1)

			// Update max if this is higher
			for {
				max := atomic.LoadInt64(&maxConcurrent)
				if current <= max || atomic.CompareAndSwapInt64(&maxConcurrent, max, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&concurrent, -1)
			return nil
		})
	}

	err := tg.Wait()
	wg.Wait()

	assert.NoError(t, err)
	assert.LessOrEqual(t, atomic.LoadInt64(&maxConcurrent), int64(2))
}

func TestTaskGroup_TryGo(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithMaxConcurrent(1))

	// First should succeed
	success1 := tg.TryGo(func(context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	assert.True(t, success1)

	// Second should fail immediately due to limit
	success2 := tg.TryGo(func(context.Context) error {
		return nil
	})
	assert.False(t, success2)

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTaskGroup_WithName(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithName("test-group"))

	tg.GoWithName("task1", func(context.Context) error {
		return nil
	})

	tg.GoWithName("task2", func(context.Context) error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)

	started, finished, taskCount, taskErr := tg.Status()
	assert.True(t, started)
	assert.True(t, finished)
	assert.Equal(t, 2, taskCount)
	assert.NoError(t, taskErr)
}

func TestTaskGroup_Status(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	// Initial status
	started, finished, taskCount, err := tg.Status()
	assert.False(t, started)
	assert.False(t, finished)
	assert.Equal(t, 0, taskCount)
	assert.NoError(t, err)

	// Start a task
	tg.Go(func(context.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	// Check status after starting
	started, finished, taskCount, err = tg.Status()
	assert.True(t, started)
	assert.False(t, finished)
	assert.Equal(t, 1, taskCount)
	assert.NoError(t, err)

	// Wait for completion
	waitErr := tg.Wait()
	assert.NoError(t, waitErr)

	// Check final status
	started, finished, taskCount, err = tg.Status()
	assert.True(t, started)
	assert.True(t, finished)
	assert.Equal(t, 1, taskCount)
	assert.NoError(t, err)
}

func TestTaskGroup_WithLogLevels(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithLogLevel(slog.LevelDebug),
		taskgroup.WithLogStart(true),
		taskgroup.WithLogEnd(true),
		taskgroup.WithLogTaskStart(true),
		taskgroup.WithLogTaskEnd(true),
	)

	tg.GoWithName("debug-task", func(context.Context) error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTaskGroup_MultipleErrors(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	err1 := errors.New("error 1")
	err2 := errors.New("error 2")

	tg.Go(func(context.Context) error {
		return err1
	})

	tg.Go(func(context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return err2
	})

	err := tg.Wait()
	assert.Error(t, err)
	// Should get the first error that occurred
	assert.Contains(t, err.Error(), "error 1")
}

func TestTaskGroup_WithContext_Function(t *testing.T) {
	ctx := context.Background()
	tg, groupCtx := taskgroup.WithContext(ctx, taskgroup.WithName("context-group"))

	assert.NotNil(t, tg)
	assert.NotNil(t, groupCtx)

	var ctxReceived context.Context
	tg.Go(func(context.Context) error {
		ctxReceived = groupCtx
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.Equal(t, groupCtx, ctxReceived)
}

func TestTaskGroup_SetLimit(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	// Set limit before starting
	tg.SetLimit(2)

	var concurrent int64
	var maxConcurrent int64

	for i := 0; i < 4; i++ {
		tg.Go(func(context.Context) error {
			current := atomic.AddInt64(&concurrent, 1)

			// Update max if this is higher
			for {
				max := atomic.LoadInt64(&maxConcurrent)
				if current <= max || atomic.CompareAndSwapInt64(&maxConcurrent, max, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&concurrent, -1)
			return nil
		})
	}

	err := tg.Wait()
	assert.NoError(t, err)
	assert.LessOrEqual(t, atomic.LoadInt64(&maxConcurrent), int64(2))
}

func TestTaskGroup_SetLimitAfterStart(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	// Start a task first
	tg.Go(func(context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Try to set limit after starting - should be ignored
	tg.SetLimit(1)

	// Add more tasks
	var concurrent int64
	for i := 0; i < 3; i++ {
		tg.Go(func(context.Context) error {
			atomic.AddInt64(&concurrent, 1)
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&concurrent, -1)
			return nil
		})
	}

	err := tg.Wait()
	assert.NoError(t, err)
	// Should have been able to run more than 1 concurrent since limit was ignored
}

func TestTaskGroup_WithAttrFunc(t *testing.T) {
	ctx := context.Background()

	attrFunc := func() []slog.Attr {
		return []slog.Attr{
			slog.String("custom", "value"),
			slog.Int("test", 42),
		}
	}

	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithAttrFunc(attrFunc))

	tg.Go(func(context.Context) error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTaskGroup_ZeroTasks(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	err := tg.Wait()
	assert.NoError(t, err)

	started, finished, taskCount, taskErr := tg.Status()
	assert.True(t, started)
	assert.True(t, finished)
	assert.Equal(t, 0, taskCount)
	assert.NoError(t, taskErr)
}

func TestTaskGroup_TryGoWithName(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithMaxConcurrent(1))

	// First should succeed
	success1 := tg.TryGoWithName("task1", func(context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	assert.True(t, success1)

	// Second should fail immediately due to limit
	success2 := tg.TryGoWithName("task2", func(context.Context) error {
		return nil
	})
	assert.False(t, success2)

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTaskGroup_TaskRegistry(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	// Initially no tasks
	assert.Equal(t, 0, tg.GetTaskCount())
	assert.Equal(t, 0, tg.GetRunningTaskCount())

	// Add tasks
	tg.GoWithName("task1", func(context.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	tg.GoWithName("task2", func(context.Context) error {
		time.Sleep(50 * time.Millisecond)
		return errors.New("task error")
	})

	// Brief wait to let tasks start
	time.Sleep(10 * time.Millisecond)

	// Check running tasks
	runningTasks := tg.GetRunningTasks()
	assert.Equal(t, 2, len(runningTasks))
	assert.Equal(t, 2, tg.GetRunningTaskCount())

	// Check task names
	taskNames := make([]string, len(runningTasks))
	for i, task := range runningTasks {
		taskNames[i] = task.Name
	}
	assert.Contains(t, taskNames, "task1")
	assert.Contains(t, taskNames, "task2")

	// Wait for completion
	err := tg.Wait()
	assert.Error(t, err)

	// Check final status
	assert.Equal(t, 0, tg.GetRunningTaskCount())

	completedTasks := tg.GetTasksByStatus(taskgroup.TaskStatusCompleted)
	failedTasks := tg.GetTasksByStatus(taskgroup.TaskStatusFailed)
	assert.Equal(t, 1, len(completedTasks))
	assert.Equal(t, 1, len(failedTasks))
}

func TestTaskGroup_WithMetadata(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	metadata := map[string]any{
		"priority": "high",
		"timeout":  30,
		"retries":  3,
	}

	tg.GoWithNameAndMeta("meta-task", metadata, func(context.Context) error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)

	// Check task metadata
	tasks := tg.GetTasksByName("meta-task")
	assert.Equal(t, 1, len(tasks))
	assert.Equal(t, metadata, tasks[0].Metadata)
}

func TestTaskGroup_PanicRecovery(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithLogTaskPanic(true))

	tg.GoWithName("panic-task", func(context.Context) error {
		panic("test panic")
	})

	err := tg.Wait()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic in task")

	// Check panic info
	panickedTasks := tg.GetTasksByStatus(taskgroup.TaskStatusPanicked)
	assert.Equal(t, 1, len(panickedTasks))
	assert.NotNil(t, panickedTasks[0].PanicInfo)
	assert.Equal(t, "test panic", panickedTasks[0].PanicInfo.Value)
	assert.NotEmpty(t, panickedTasks[0].PanicInfo.Stack)
}

func TestTaskGroup_WithTicker(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithEnableTicker(true),
		taskgroup.WithTickerInterval(100*time.Millisecond),
		taskgroup.WithTickerFrequency(1),
	)

	tg.GoWithName("ticker-task", func(context.Context) error {
		time.Sleep(250 * time.Millisecond)
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTaskGroup_TaskHistory(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithKeepTaskHistory(true))

	tg.GoWithName("history-task", func(context.Context) error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)

	// Check task history
	history := tg.GetTaskHistory()
	assert.Equal(t, 1, len(history))
	assert.Equal(t, "history-task", history[0].Name)
	assert.Equal(t, taskgroup.TaskStatusCompleted, history[0].Status)
}

func TestTaskGroup_TaskStateLogValue(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	metadata := map[string]any{"key": "value"}
	tg.GoWithNameAndMeta("log-task", metadata, func(context.Context) error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)

	// Test TaskState.LogValue()
	tasks := tg.GetTasksByName("log-task")
	assert.Equal(t, 1, len(tasks))

	logValue := tasks[0].LogValue()
	assert.NotNil(t, logValue)
}

func TestTaskGroup_TaskStateHelpers(t *testing.T) {
	task := &taskgroup.TaskState{
		Status: taskgroup.TaskStatusRunning,
	}

	assert.True(t, task.IsRunning())
	assert.False(t, task.IsCompleted())

	task.Status = taskgroup.TaskStatusCompleted
	assert.False(t, task.IsRunning())
	assert.True(t, task.IsCompleted())

	task.Status = taskgroup.TaskStatusFailed
	assert.False(t, task.IsRunning())
	assert.True(t, task.IsCompleted())

	task.Status = taskgroup.TaskStatusPanicked
	assert.False(t, task.IsRunning())
	assert.True(t, task.IsCompleted())
}

func TestTaskGroup_GetTasksByName(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	// Add multiple tasks with same name
	tg.GoWithName("duplicate-task", func(context.Context) error {
		return nil
	})

	tg.GoWithName("duplicate-task", func(context.Context) error {
		return nil
	})

	tg.GoWithName("unique-task", func(context.Context) error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)

	// Test GetTasksByName
	duplicateTasks := tg.GetTasksByName("duplicate-task")
	assert.Equal(t, 2, len(duplicateTasks))

	uniqueTasks := tg.GetTasksByName("unique-task")
	assert.Equal(t, 1, len(uniqueTasks))

	nonExistentTasks := tg.GetTasksByName("non-existent")
	assert.Equal(t, 0, len(nonExistentTasks))
}

func TestTaskGroup_GetTask(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx)

	var taskID string
	tg.GoWithName("get-task", func(context.Context) error {
		// Find the task ID
		tasks := tg.GetTasksByName("get-task")
		if len(tasks) > 0 {
			taskID = tasks[0].ID
		}
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)

	// Test GetTask
	task, exists := tg.GetTask(taskID)
	assert.True(t, exists)
	assert.NotNil(t, task)
	assert.Equal(t, "get-task", task.Name)

	// Test non-existent task
	_, exists = tg.GetTask("non-existent-id")
	assert.False(t, exists)
}

func BenchmarkTaskGroup_BasicUsage(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg := taskgroup.NewTaskGroup(ctx, taskgroup.WithLogStart(false), taskgroup.WithLogEnd(false))

		for j := 0; j < 10; j++ {
			tg.Go(func(context.Context) error {
				return nil
			})
		}

		_ = tg.Wait()
	}
}

func BenchmarkTaskGroup_WithLimit(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg := taskgroup.NewTaskGroup(ctx,
			taskgroup.WithMaxConcurrent(5),
			taskgroup.WithLogStart(false),
			taskgroup.WithLogEnd(false),
			taskgroup.WithLogTaskStart(false),
			taskgroup.WithLogTaskEnd(false),
		)

		for j := 0; j < 20; j++ {
			tg.Go(func(context.Context) error {
				return nil
			})
		}

		_ = tg.Wait()
	}
}

func BenchmarkTaskGroup_WithRegistry(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg := taskgroup.NewTaskGroup(ctx,
			taskgroup.WithLogStart(false),
			taskgroup.WithLogEnd(false),
			taskgroup.WithKeepTaskHistory(true),
		)

		for j := 0; j < 10; j++ {
			tg.GoWithName("bench-task", func(context.Context) error {
				return nil
			})
		}

		_ = tg.Wait()

		// Query the registry
		_ = tg.GetTaskCount()
		_ = tg.GetRunningTaskCount()
	}
}

func TestTaskGroup_PprofIntegration(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("pprof-test"),
		taskgroup.WithEnablePprof(true),
		taskgroup.WithPprofLabels(map[string]string{
			"environment": "test",
			"component":   "taskgroup",
		}),
	)

	var taskCompleted bool
	var capturedLabels map[string]string

	tg.GoWithNameAndMeta("pprof-task", map[string]any{
		"priority": "high",
		"timeout":  30,
	}, func(context.Context) error {
		// Capture labels from within the task
		helper := tg.GetPprofHelper()
		// Note: We need to get the labeled context from the goroutine,
		// but for now let's see if we can get the context from the task
		ctx := tg.Context()
		capturedLabels = helper.GetCurrentLabels(ctx)
		taskCompleted = true
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.True(t, taskCompleted)
	assert.NotNil(t, capturedLabels)

	// Verify expected labels are present
	assert.Equal(t, "pprof-test", capturedLabels["taskgroup"])
	assert.Equal(t, "pprof-task", capturedLabels["task_name"])
	assert.Equal(t, "running", capturedLabels["task_status"])
	assert.Equal(t, "test", capturedLabels["environment"])
	assert.Equal(t, "taskgroup", capturedLabels["component"])
	assert.Equal(t, "high", capturedLabels["meta_priority"])
	assert.Equal(t, "30", capturedLabels["meta_timeout"])
	assert.Contains(t, capturedLabels["task_id"], "pprof-test-")
}

func TestTaskGroup_PprofDisabled(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("no-pprof"),
		taskgroup.WithEnablePprof(false),
	)

	var taskCompleted bool
	tg.GoWithName("no-pprof-task", func(context.Context) error {
		taskCompleted = true
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.True(t, taskCompleted)
}

func TestTaskGroup_PprofHelper(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("helper-test"),
		taskgroup.WithEnablePprof(true),
	)

	helper := tg.GetPprofHelper()
	assert.NotNil(t, helper)

	var taskID, taskName string
	var foundLabels bool

	tg.GoWithName("helper-task", func(context.Context) error {
		// Test helper methods
		if id, ok := helper.GetTaskIDFromLabels(tg.Context()); ok {
			taskID = id
		}
		if name, ok := helper.GetTaskNameFromLabels(tg.Context()); ok {
			taskName = name
		}
		if _, ok := helper.GetTaskLabel(tg.Context(), "taskgroup"); ok {
			foundLabels = true
		}
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.True(t, foundLabels)
	assert.Equal(t, "helper-task", taskName)
	assert.Contains(t, taskID, "helper-test-")
}

func TestTaskGroup_PprofWithAdditionalLabels(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("additional-labels"),
		taskgroup.WithEnablePprof(true),
	)

	var stageLabels map[string]string
	helper := tg.GetPprofHelper()

	tg.GoWithName("staged-task", func(context.Context) error {
		// Add additional labels for a specific stage
		helper.WithAdditionalLabels(ctx, map[string]string{
			"stage": "processing",
			"phase": "validation",
		}, func(labeledCtx context.Context) {
			stageLabels = helper.GetCurrentLabels(labeledCtx)
		})
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.NotNil(t, stageLabels)
	assert.Equal(t, "processing", stageLabels["stage"])
	assert.Equal(t, "validation", stageLabels["phase"])
}

func TestTaskGroup_PprofChildTaskLabels(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("parent-child"),
		taskgroup.WithEnablePprof(true),
	)

	helper := tg.GetPprofHelper()

	tg.GoWithName("parent-task", func(context.Context) error {
		// Create child task labels
		childLabels := helper.CreateChildTaskLabels(ctx, "child-operation")

		// Simulate child task execution with labels
		pprof.Do(ctx, childLabels, func(childCtx context.Context) {
			labels := helper.GetCurrentLabels(childCtx)
			assert.Equal(t, "child", labels["task_type"])
			assert.Equal(t, "child-operation", labels["child_task_name"])
			assert.Contains(t, labels["parent_task_id"], "parent-child-")
		})

		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTaskGroup_PprofUpdateTaskStage(t *testing.T) {
	ctx := context.Background()
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("staged-task"),
		taskgroup.WithEnablePprof(true),
	)

	helper := tg.GetPprofHelper()
	var stages []string

	tg.GoWithName("multi-stage-task", func(context.Context) error {
		// Simulate different task stages
		for _, stage := range []string{"initialize", "process", "finalize"} {
			helper.UpdateTaskStage(ctx, stage)
			if currentStage, ok := helper.GetTaskLabel(ctx, "task_stage"); ok {
				stages = append(stages, currentStage)
			}
		}
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.Contains(t, stages, "initialize")
	assert.Contains(t, stages, "process")
	assert.Contains(t, stages, "finalize")
}

func BenchmarkTaskGroup_WithPprof(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg := taskgroup.NewTaskGroup(ctx,
			taskgroup.WithEnablePprof(true),
			taskgroup.WithLogStart(false),
			taskgroup.WithLogEnd(false),
		)

		for j := 0; j < 10; j++ {
			tg.GoWithName("bench-task", func(context.Context) error {
				return nil
			})
		}

		_ = tg.Wait()
	}
}

func BenchmarkTaskGroup_WithoutPprof(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg := taskgroup.NewTaskGroup(ctx,
			taskgroup.WithEnablePprof(false),
			taskgroup.WithLogStart(false),
			taskgroup.WithLogEnd(false),
		)

		for j := 0; j < 10; j++ {
			tg.GoWithName("bench-task", func(context.Context) error {
				return nil
			})
		}

		_ = tg.Wait()
	}
}
