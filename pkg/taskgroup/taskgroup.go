package taskgroup

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	slogctx "github.com/veqryn/slog-context"
	"github.com/walteh/runm/pkg/syncmap"
	"github.com/walteh/runm/pkg/ticker"
)

//go:opts
type TaskGroupOpts struct {
	name            string     `default:"taskgroup"`
	logLevel        slog.Level `default:"-4"`
	logStart        bool       `default:"true"`
	logEnd          bool       `default:"true"`
	logTaskStart    bool       `default:"false"`
	logTaskEnd      bool       `default:"false"`
	logTaskPanic    bool       `default:"true"`
	timeout         time.Duration
	callerSkip      int `default:"1"`
	slogBaseContext context.Context
	attrFunc        func() []slog.Attr
	maxConcurrent   int           // 0 means unlimited
	enableTicker    bool          `default:"false"`
	tickerInterval  time.Duration `default:"30s"`
	tickerFrequency int           `default:"5"`
	keepTaskHistory bool          `default:"true"`
	maxTaskHistory  int           `default:"1000"`
	enablePprof     bool          `default:"true"`
	pprofLabels     map[string]string
}

type TaskRegistry interface {
	GetRunningTasks() []*TaskState
	GetAllTasks() []*TaskState
	GetTask(id string) (*TaskState, bool)
	GetTasksByStatus(status TaskStatus) []*TaskState
	GetTasksByName(name string) []*TaskState
	GetTaskCount() int
	GetRunningTaskCount() int
}

type TaskGroup struct {
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex
	opts        TaskGroupOpts
	err         error
	errOnce     sync.Once
	groupError  *TaskGroupError
	caller      uintptr
	started     bool
	finished    bool
	taskCounter int64
	semaphore   chan struct{}

	// Task registry
	tasks       *syncmap.Map[string, *TaskState]
	taskHistory *syncmap.Map[string, *TaskState]

	// Ticker for periodic status logging
	statusTicker *ticker.Ticker

	// Cleanup functions
	cleanupFuncs []CleanupEntry
	cleanupOnce  sync.Once
}

func NewTaskGroup(ctx context.Context, opts ...TaskGroupOpt) *TaskGroup {
	options := newTaskGroupOpts(opts...)

	if options.slogBaseContext == nil {
		options.slogBaseContext = ctx
	}

	caller, _, _, _ := runtime.Caller(options.callerSkip)

	ctx, cancel := context.WithCancel(ctx)
	if options.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, options.timeout)
	}

	var semaphore chan struct{}
	if options.maxConcurrent > 0 {
		semaphore = make(chan struct{}, options.maxConcurrent)
	}

	tg := &TaskGroup{
		ctx:         ctx,
		cancel:      cancel,
		opts:        options,
		caller:      caller,
		semaphore:   semaphore,
		groupError:  NewTaskGroupError(),
		tasks:       syncmap.NewMap[string, *TaskState](),
		taskHistory: syncmap.NewMap[string, *TaskState](),
	}

	// Setup ticker for periodic status logging
	if options.enableTicker {
		tg.statusTicker = ticker.NewTicker(
			ticker.WithMessage(fmt.Sprintf("TASKGROUP[%s]:STATUS", options.name)),
			ticker.WithDoneMessage(fmt.Sprintf("TASKGROUP[%s]:DONE", options.name)),
			ticker.WithSlogBaseContext(options.slogBaseContext),
			ticker.WithLogLevel(options.logLevel),
			ticker.WithInterval(options.tickerInterval),
			ticker.WithFrequency(options.tickerFrequency),
			ticker.WithStartBurst(2),
			ticker.WithAttrFunc(func() []slog.Attr {
				return tg.getStatusAttrs()
			}),
		)
	}

	return tg
}

func (tg *TaskGroup) getStatusAttrs() []slog.Attr {
	runningTasks := tg.GetRunningTasks()
	allTasks := tg.GetAllTasks()

	attrs := []slog.Attr{
		slog.String("taskgroup", tg.opts.name),
		slog.Int("running_tasks", len(runningTasks)),
		slog.Int("total_tasks", len(allTasks)),
	}

	if tg.opts.attrFunc != nil {
		attrs = append(attrs, tg.opts.attrFunc()...)
	}

	// Add running task names
	if len(runningTasks) > 0 {
		runningNames := make([]string, len(runningTasks))
		for i, task := range runningTasks {
			runningNames[i] = task.Name
		}
		attrs = append(attrs, slog.Any("running_task_names", runningNames))
	}

	return attrs
}

func (tg *TaskGroup) Context() context.Context {
	return tg.ctx
}

func (tg *TaskGroup) Start() {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	if tg.started {
		return
	}

	tg.started = true

	if tg.statusTicker != nil {
		go tg.statusTicker.RunWithWaitOnContext(tg.ctx)
	}

	if tg.opts.logStart {
		tg.log(slog.LevelInfo, "taskgroup started", slog.String("name", tg.opts.name))
	}
}

func (tg *TaskGroup) Go(fn TaskFunc) {
	tg.GoWithName("", fn)
}

func (tg *TaskGroup) GoWithName(name string, fn TaskFunc) {
	tg.GoWithNameAndMeta(name, nil, fn)
}

func (tg *TaskGroup) GoWithNameAndMeta(name string, metadata map[string]any, fn TaskFunc) {
	tg.executeTask(name, metadata, fn, false)
}

func (tg *TaskGroup) updateTaskStatus(taskID string, status TaskStatus) {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	if task, exists := tg.tasks.Load(taskID); exists {
		task.Status = status
		task.UpdatedAt = time.Now()

		if status == TaskStatusRunning {
			task.StartTime = time.Now()
			task.GoroutineID = getGoroutineID()
		}
	}
}

// executeTask is the common implementation for Go and TryGo methods
func (tg *TaskGroup) executeTask(name string, metadata map[string]any, fn TaskFunc, tryMode bool) bool {
	tg.Start()

	tg.mu.Lock()
	if tg.finished {
		tg.mu.Unlock()
		return !tryMode // Go() doesn't return, TryGo() returns false
	}

	taskID := fmt.Sprintf("%s-%d", tg.opts.name, atomic.AddInt64(&tg.taskCounter, 1))
	if name == "" {
		name = fmt.Sprintf("task-%d", tg.taskCounter)
	}

	taskState := &TaskState{
		ID:          taskID,
		Name:        name,
		Status:      TaskStatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		GoroutineID: getGoroutineID(),
		Metadata:    metadata,
	}

	tg.tasks.Store(taskID, taskState)
	tg.mu.Unlock()

	// Check if we can acquire semaphore
	if tg.semaphore != nil {
		if tryMode {
			// Non-blocking semaphore acquisition for TryGo
			select {
			case tg.semaphore <- struct{}{}:
				// Successfully acquired
			default:
				// Could not acquire semaphore, remove task and return false
				tg.tasks.Delete(taskID)
				atomic.AddInt64(&tg.taskCounter, -1)
				return false
			}
		} else {
			// Blocking semaphore acquisition for Go
			select {
			case tg.semaphore <- struct{}{}:
				// Successfully acquired
			case <-tg.ctx.Done():
				tg.updateTaskStatus(taskID, TaskStatusCanceled)
				tg.setFundamentalError(tg.ctx.Err())
				return true
			}
		}
	}

	tg.wg.Add(1)

	go func() {
		defer tg.wg.Done()
		defer func() {
			if tg.semaphore != nil {
				<-tg.semaphore
			}
		}()

		// Update task status to running
		tg.updateTaskStatus(taskID, TaskStatusRunning)

		start := time.Now()
		if tg.opts.logTaskStart {
			tg.log(slog.LevelDebug, "task started",
				slog.String("task_id", taskID),
				slog.String("task_name", name),
				slog.Uint64("goroutine_id", getGoroutineID()),
			)
		}

		var err error
		var panicInfo *PanicInfo

		// Execute with pprof labels and panic recovery
		if tg.opts.enablePprof {
			labels := tg.createPprofLabels(taskID, name, metadata)
			pprof.Do(tg.ctx, labels, func(labeledCtx context.Context) {
				tg.executeTaskWithRecovery(labeledCtx, taskState, fn, &err, &panicInfo)
			})
		} else {
			tg.executeTaskWithRecovery(tg.ctx, taskState, fn, &err, &panicInfo)
		}

		duration := time.Since(start)

		// Update task state
		tg.mu.Lock()
		if task, exists := tg.tasks.Load(taskID); exists {
			task.EndTime = time.Now()
			task.Duration = duration
			task.UpdatedAt = time.Now()
			task.Error = err
			task.PanicInfo = panicInfo

			if panicInfo != nil {
				task.Status = TaskStatusPanicked
			} else if err != nil {
				task.Status = TaskStatusFailed
			} else {
				task.Status = TaskStatusCompleted
			}

			// Move to history if enabled
			if tg.opts.keepTaskHistory {
				tg.taskHistory.Store(taskID, task)
			}
		}
		tg.mu.Unlock()

		if tg.opts.logTaskEnd {
			attrs := []slog.Attr{
				slog.String("task_id", taskID),
				slog.String("task_name", name),
				slog.Duration("duration", duration),
				slog.Uint64("goroutine_id", getGoroutineID()),
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}
			if panicInfo != nil {
				attrs = append(attrs, slog.String("panic", fmt.Sprintf("%v", panicInfo.Value)))
			}
			tg.log(slog.LevelDebug, "task finished", attrs...)
		}

		if err != nil {
			tg.addTaskError(name, err)
			// Only set the first error for backwards compatibility with existing tg.err
			// Don't cancel context immediately - let other tasks complete to collect all errors
			tg.setTaskFailureError(err)
		}
	}()

	return true
}

func (tg *TaskGroup) Wait() error {
	tg.Start()

	// Use a channel to signal when WaitGroup is done
	done := make(chan struct{})
	go func() {
		tg.wg.Wait()
		close(done)
	}()

	// Wait for either completion or context cancellation
	select {
	case <-done:
		// All tasks completed normally
	case <-tg.ctx.Done():
		// Context was cancelled (timeout or manual cancellation)
		tg.setFundamentalError(tg.ctx.Err())
	}

	tg.mu.Lock()
	defer tg.mu.Unlock()

	if tg.finished {
		return tg.err
	}

	tg.finished = true
	tg.cancel()

	// Stop ticker if enabled
	if tg.statusTicker != nil {
		tg.statusTicker.Stop()
	}

	// Execute cleanup functions
	tg.executeCleanup(tg.ctx)

	if tg.opts.logEnd {
		runningTasks := tg.GetRunningTasks()
		completedTasks := tg.GetTasksByStatus(TaskStatusCompleted)
		failedTasks := tg.GetTasksByStatus(TaskStatusFailed)
		panickedTasks := tg.GetTasksByStatus(TaskStatusPanicked)

		attrs := []slog.Attr{
			slog.String("name", tg.opts.name),
			slog.Int("total_tasks", int(tg.taskCounter)),
			slog.Int("running_tasks", len(runningTasks)),
			slog.Int("completed_tasks", len(completedTasks)),
			slog.Int("failed_tasks", len(failedTasks)),
			slog.Int("panicked_tasks", len(panickedTasks)),
		}

		if tg.groupError.HasErrors() {
			attrs = append(attrs, slog.String("error", tg.groupError.Error()))
		}

		tg.log(slog.LevelInfo, "taskgroup finished", attrs...)
	}

	// Return the comprehensive error if any tasks failed
	if tg.groupError.HasErrors() {
		return tg.groupError
	}
	return tg.err
}

func (tg *TaskGroup) TryGo(fn TaskFunc) bool {
	return tg.TryGoWithName("", fn)
}

func (tg *TaskGroup) TryGoWithName(name string, fn TaskFunc) bool {
	return tg.TryGoWithNameAndMeta(name, nil, fn)
}

func (tg *TaskGroup) TryGoWithNameAndMeta(name string, metadata map[string]any, fn TaskFunc) bool {
	return tg.executeTask(name, metadata, fn, true)
}

func (tg *TaskGroup) Status() (started, finished bool, taskCount int, err error) {
	tg.mu.RLock()
	defer tg.mu.RUnlock()

	return tg.started, tg.finished, int(tg.taskCounter), tg.err
}

// setFundamentalError sets an error that should abort the entire taskgroup
// This cancels the context to stop all other tasks immediately
func (tg *TaskGroup) setFundamentalError(err error) {
	tg.errOnce.Do(func() {
		tg.mu.Lock()
		tg.err = err
		tg.mu.Unlock()
		tg.cancel()
	})
}

// setTaskFailureError sets an error from a failed task but allows other tasks to continue
// This preserves the first error for backwards compatibility without cancelling the context
func (tg *TaskGroup) setTaskFailureError(err error) {
	tg.errOnce.Do(func() {
		tg.mu.Lock()
		tg.err = err
		tg.mu.Unlock()
		// Don't cancel context here - let all tasks complete to collect errors
	})
}

func (tg *TaskGroup) addTaskError(taskName string, err error) {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	tg.groupError.AddTaskError(taskName, err)
}

// GetTaskGroupError returns the comprehensive error containing all task errors
func (tg *TaskGroup) GetTaskGroupError() *TaskGroupError {
	tg.mu.RLock()
	defer tg.mu.RUnlock()
	return tg.groupError
}

func (tg *TaskGroup) log(level slog.Level, msg string, attrs ...slog.Attr) {
	if tg.opts.attrFunc != nil {
		attrs = append(attrs, tg.opts.attrFunc()...)
	}

	rec := slog.NewRecord(time.Now(), level, msg, tg.caller)
	rec.AddAttrs(attrs...)

	ctx := tg.opts.slogBaseContext
	if ctx == nil {
		ctx = context.Background()
	}

	_ = slog.Default().Handler().Handle(ctx, rec)
}

// TaskRegistry implementation
func (tg *TaskGroup) GetRunningTasks() []*TaskState {
	return tg.GetTasksByStatus(TaskStatusRunning)
}

func (tg *TaskGroup) GetAllTasks() []*TaskState {
	var tasks []*TaskState
	tg.tasks.Range(func(key string, value *TaskState) bool {
		tasks = append(tasks, value)
		return true
	})
	return tasks
}

func (tg *TaskGroup) GetTask(id string) (*TaskState, bool) {
	return tg.tasks.Load(id)
}

func (tg *TaskGroup) GetTasksByStatus(status TaskStatus) []*TaskState {
	var tasks []*TaskState
	tg.tasks.Range(func(key string, value *TaskState) bool {
		if value.Status == status {
			tasks = append(tasks, value)
		}
		return true
	})
	return tasks
}

func (tg *TaskGroup) GetTasksByName(name string) []*TaskState {
	var tasks []*TaskState
	tg.tasks.Range(func(key string, value *TaskState) bool {
		if value.Name == name {
			tasks = append(tasks, value)
		}
		return true
	})
	return tasks
}

func (tg *TaskGroup) GetTaskCount() int {
	return tg.tasks.Len()
}

func (tg *TaskGroup) GetRunningTaskCount() int {
	return len(tg.GetRunningTasks())
}

func (tg *TaskGroup) GetTaskHistory() []*TaskState {
	var tasks []*TaskState
	tg.taskHistory.Range(func(key string, value *TaskState) bool {
		tasks = append(tasks, value)
		return true
	})
	return tasks
}

// WithContext creates a new TaskGroup with the given context
func WithContext(ctx context.Context, opts ...TaskGroupOpt) (*TaskGroup, context.Context) {
	tg := NewTaskGroup(ctx, opts...)
	return tg, tg.Context()
}

// SetLimit sets the maximum number of concurrent goroutines
func (tg *TaskGroup) SetLimit(n int) {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	if tg.started {
		return // Cannot change limit after starting
	}

	if n > 0 {
		tg.semaphore = make(chan struct{}, n)
	} else {
		tg.semaphore = nil
	}
}

// getGoroutineID returns the current goroutine ID
func getGoroutineID() uint64 {
	// Parse goroutine ID from runtime stack
	buf := make([]byte, 64)
	buf = buf[:runtime.Stack(buf, false)]
	// Stack trace format: "goroutine 123 [running]:\n..."
	// Extract the goroutine ID number
	for i := 10; i < len(buf); i++ {
		if buf[i] == ' ' {
			var id uint64
			for j := 10; j < i; j++ {
				if buf[j] >= '0' && buf[j] <= '9' {
					id = id*10 + uint64(buf[j]-'0')
				}
			}
			return id
		}
	}
	return 0
}

// createPprofLabels creates pprof labels for the task
func (tg *TaskGroup) createPprofLabels(taskID, name string, metadata map[string]any) pprof.LabelSet {
	labels := make([]string, 0, 16) // Pre-allocate for common labels

	// Core task labels
	labels = append(labels,
		"task_id", taskID,
		"task_name", name,
		"taskgroup", tg.opts.name,
		"task_status", "running",
	)

	// Add custom taskgroup labels
	if tg.opts.pprofLabels != nil {
		for k, v := range tg.opts.pprofLabels {
			labels = append(labels, k, v)
		}
	}

	// Add metadata as labels (convert to strings)
	for k, v := range metadata {
		if len(labels) < 32 { // Limit total labels to avoid overhead
			labels = append(labels, fmt.Sprintf("meta_%s", k), fmt.Sprintf("%v", v))
		}
	}

	return pprof.Labels(labels...)
}

// executeTaskWithRecovery executes the task function with panic recovery
func (tg *TaskGroup) executeTaskWithRecovery(ctx context.Context, taskState *TaskState, fn TaskFunc, err *error, panicInfo **PanicInfo) {
	defer func() {
		if r := recover(); r != nil {
			*panicInfo = &PanicInfo{
				Value:       r,
				Stack:       string(debug.Stack()),
				Timestamp:   time.Now(),
				GoroutineID: getGoroutineID(),
			}
			*err = fmt.Errorf("panic in task: %v", r)

			if tg.opts.logTaskPanic {
				tg.log(slog.LevelError, "task panicked",
					slog.String("task_id", taskState.ID),
					slog.String("task_name", taskState.Name),
					slog.Any("panic_value", r),
					slog.String("stack", (*panicInfo).Stack),
				)
			}
		}
	}()

	// Enhance slog context with task information using groups
	ctx = slogctx.Append(ctx, "task", taskState)
	ctx = slogctx.Append(ctx, slog.String("taskgroup", tg.opts.name))

	*err = fn(ctx)
}

// GetPprofHelper returns a helper for pprof operations
func (tg *TaskGroup) GetPprofHelper() *PprofHelper {
	return &PprofHelper{tg: tg}
}

// RegisterCleanup registers a cleanup function to be called when the TaskGroup shuts down
func (tg *TaskGroup) RegisterCleanup(cleanup CleanupFunc) {
	tg.RegisterCleanupWithName("", cleanup)
}

// RegisterCleanupWithName registers a named cleanup function to be called when the TaskGroup shuts down
func (tg *TaskGroup) RegisterCleanupWithName(name string, cleanup CleanupFunc) {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	
	if name == "" {
		name = fmt.Sprintf("cleanup-%d", len(tg.cleanupFuncs)+1)
	}
	
	tg.cleanupFuncs = append(tg.cleanupFuncs, CleanupEntry{
		Name: name,
		Func: cleanup,
	})
}

// RegisterCloser registers an io.Closer to be closed when the TaskGroup shuts down
func (tg *TaskGroup) RegisterCloser(closer interface{ Close() error }) {
	tg.RegisterCloserWithName("", closer)
}

// RegisterCloserWithName registers a named io.Closer to be closed when the TaskGroup shuts down
func (tg *TaskGroup) RegisterCloserWithName(name string, closer interface{ Close() error }) {
	if name == "" {
		name = fmt.Sprintf("closer-%d", len(tg.cleanupFuncs)+1)
	}
	
	tg.RegisterCleanupWithName(name, func(ctx context.Context) error {
		return closer.Close()
	})
}

// executeCleanup runs all registered cleanup functions
func (tg *TaskGroup) executeCleanup(ctx context.Context) {
	tg.cleanupOnce.Do(func() {
		tg.mu.RLock()
		cleanupEntries := make([]CleanupEntry, len(tg.cleanupFuncs))
		copy(cleanupEntries, tg.cleanupFuncs)
		tg.mu.RUnlock()

		if len(cleanupEntries) == 0 {
			return
		}

		tg.log(slog.LevelDebug, "executing cleanup functions", slog.Int("count", len(cleanupEntries)))

		// Execute cleanup functions in reverse order (LIFO)
		for i := len(cleanupEntries) - 1; i >= 0; i-- {
			entry := cleanupEntries[i]
			func(cleanupEntry CleanupEntry) {
				defer func() {
					if r := recover(); r != nil {
						tg.log(slog.LevelError, "cleanup function panicked",
							slog.String("cleanup_name", cleanupEntry.Name),
							slog.Any("panic_value", r),
						)
					}
				}()

				tg.log(slog.LevelDebug, "executing cleanup function", slog.String("name", cleanupEntry.Name))

				if err := cleanupEntry.Func(ctx); err != nil {
					tg.log(slog.LevelWarn, "cleanup function failed",
						slog.String("cleanup_name", cleanupEntry.Name),
						slog.String("error", err.Error()),
					)
				} else {
					tg.log(slog.LevelDebug, "cleanup function completed", slog.String("name", cleanupEntry.Name))
				}
			}(entry)
		}

		tg.log(slog.LevelDebug, "all cleanup functions completed")
	})
}

// GetCleanupNames returns the names of all registered cleanup functions
func (tg *TaskGroup) GetCleanupNames() []string {
	tg.mu.RLock()
	defer tg.mu.RUnlock()
	
	names := make([]string, len(tg.cleanupFuncs))
	for i, entry := range tg.cleanupFuncs {
		names[i] = entry.Name
	}
	return names
}

// GetCleanupCount returns the number of registered cleanup functions
func (tg *TaskGroup) GetCleanupCount() int {
	tg.mu.RLock()
	defer tg.mu.RUnlock()
	return len(tg.cleanupFuncs)
}

// Close manually triggers cleanup and cancels the TaskGroup context
func (tg *TaskGroup) Close() error {
	tg.mu.Lock()
	if tg.finished {
		tg.mu.Unlock()
		return nil
	}
	tg.mu.Unlock()

	// Cancel context to signal shutdown
	tg.cancel()

	// Execute cleanup functions
	tg.executeCleanup(context.Background())

	return nil
}
