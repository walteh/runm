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

	"github.com/walteh/runm/pkg/syncmap"
	"github.com/walteh/runm/pkg/ticker"
)

//go:opts
type TaskGroupOpts struct {
	name            string        `default:"taskgroup"`
	logLevel        slog.Level    `default:"-4"`
	logStart        bool          `default:"true"`
	logEnd          bool          `default:"true"`
	logTaskStart    bool          `default:"false"`
	logTaskEnd      bool          `default:"false"`
	logTaskPanic    bool          `default:"true"`
	timeout         time.Duration
	callerSkip      int             `default:"1"`
	slogBaseContext context.Context
	attrFunc        func() []slog.Attr
	maxConcurrent   int             // 0 means unlimited
	enableTicker    bool            `default:"false"`
	tickerInterval  time.Duration   `default:"30s"`
	tickerFrequency int             `default:"5"`
	keepTaskHistory bool            `default:"true"`
	maxTaskHistory  int             `default:"1000"`
	enablePprof     bool            `default:"true"`
	pprofLabels     map[string]string
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
	Value      any
	Stack      string
	Timestamp  time.Time
	GoroutineID uint64
}

func (t *TaskState) IsRunning() bool {
	return t.Status == TaskStatusRunning
}

func (t *TaskState) IsCompleted() bool {
	return t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed || t.Status == TaskStatusPanicked
}

func (t *TaskState) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("id", t.ID),
		slog.String("name", t.Name),
		slog.String("status", t.Status.String()),
		slog.Time("start_time", t.StartTime),
		slog.Duration("duration", t.Duration),
		slog.Uint64("goroutine_id", t.GoroutineID),
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
		attrs = append(attrs, slog.Group("metadata", metaAttrs...))
	}

	return slog.GroupValue(attrs...)
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

func (tg *TaskGroup) Go(fn func() error) {
	tg.GoWithName("", fn)
}

func (tg *TaskGroup) GoWithName(name string, fn func() error) {
	tg.GoWithNameAndMeta(name, nil, fn)
}

func (tg *TaskGroup) GoWithNameAndMeta(name string, metadata map[string]any, fn func() error) {
	tg.Start()
	
	tg.mu.Lock()
	if tg.finished {
		tg.mu.Unlock()
		return
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
		select {
		case tg.semaphore <- struct{}{}:
			// Successfully acquired
		case <-tg.ctx.Done():
			tg.updateTaskStatus(taskID, TaskStatusCanceled)
			tg.setError(tg.ctx.Err())
			return
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
				tg.executeTaskWithRecovery(taskID, name, fn, &err, &panicInfo)
			})
		} else {
			tg.executeTaskWithRecovery(taskID, name, fn, &err, &panicInfo)
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
				// TODO: Implement history cleanup based on maxTaskHistory
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
			tg.setError(err)
		}
	}()
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
		tg.setError(tg.ctx.Err())
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
		
		if tg.err != nil {
			attrs = append(attrs, slog.String("error", tg.err.Error()))
		}
		
		tg.log(slog.LevelInfo, "taskgroup finished", attrs...)
	}
	
	return tg.err
}

func (tg *TaskGroup) TryGo(fn func() error) bool {
	return tg.TryGoWithName("", fn)
}

func (tg *TaskGroup) TryGoWithName(name string, fn func() error) bool {
	return tg.TryGoWithNameAndMeta(name, nil, fn)
}

func (tg *TaskGroup) TryGoWithNameAndMeta(name string, metadata map[string]any, fn func() error) bool {
	tg.Start()
	
	tg.mu.Lock()
	if tg.finished {
		tg.mu.Unlock()
		return false
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
	
	// Check if we can acquire semaphore without blocking
	if tg.semaphore != nil {
		select {
		case tg.semaphore <- struct{}{}:
			// Successfully acquired
		default:
			// Could not acquire semaphore, remove task and return false
			tg.tasks.Delete(taskID)
			atomic.AddInt64(&tg.taskCounter, -1)
			return false
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
				tg.executeTaskWithRecovery(taskID, name, fn, &err, &panicInfo)
			})
		} else {
			tg.executeTaskWithRecovery(taskID, name, fn, &err, &panicInfo)
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
			tg.setError(err)
		}
	}()
	
	return true
}

func (tg *TaskGroup) Status() (started, finished bool, taskCount int, err error) {
	tg.mu.RLock()
	defer tg.mu.RUnlock()
	
	return tg.started, tg.finished, int(tg.taskCounter), tg.err
}

func (tg *TaskGroup) setError(err error) {
	tg.errOnce.Do(func() {
		tg.mu.Lock()
		tg.err = err
		tg.mu.Unlock()
		tg.cancel()
	})
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
	// This is a simplified implementation
	// In production, you might want to use a more efficient method
	return 0 // Placeholder - would need runtime.Goroutine() or similar
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
	if metadata != nil {
		for k, v := range metadata {
			if len(labels) < 32 { // Limit total labels to avoid overhead
				labels = append(labels, fmt.Sprintf("meta_%s", k), fmt.Sprintf("%v", v))
			}
		}
	}
	
	return pprof.Labels(labels...)
}

// executeTaskWithRecovery executes the task function with panic recovery
func (tg *TaskGroup) executeTaskWithRecovery(taskID, name string, fn func() error, err *error, panicInfo **PanicInfo) {
	defer func() {
		if r := recover(); r != nil {
			*panicInfo = &PanicInfo{
				Value:      r,
				Stack:      string(debug.Stack()),
				Timestamp:  time.Now(),
				GoroutineID: getGoroutineID(),
			}
			*err = fmt.Errorf("panic in task: %v", r)
			
			if tg.opts.logTaskPanic {
				tg.log(slog.LevelError, "task panicked",
					slog.String("task_id", taskID),
					slog.String("task_name", name),
					slog.Any("panic_value", r),
					slog.String("stack", (*panicInfo).Stack),
				)
			}
		}
	}()
	*err = fn()
}

// PprofHelper provides utilities for working with pprof labels
type PprofHelper struct {
	tg *TaskGroup
}

// GetPprofHelper returns a helper for pprof operations
func (tg *TaskGroup) GetPprofHelper() *PprofHelper {
	return &PprofHelper{tg: tg}
}

// GetCurrentLabels returns the current goroutine's pprof labels
// This must be called from within a pprof.Do context
func (ph *PprofHelper) GetCurrentLabels() map[string]string {
	labels := make(map[string]string)
	pprof.ForLabels(context.Background(), func(key, value string) bool {
		labels[key] = value
		return true
	})
	return labels
}

// GetTaskLabel returns a specific label value for the current task
// This must be called from within a pprof.Do context
func (ph *PprofHelper) GetTaskLabel(ctx context.Context, key string) (string, bool) {
	return pprof.Label(ctx, key)
}

// GetTaskIDFromLabels returns the task ID from pprof labels
// This must be called from within a pprof.Do context
func (ph *PprofHelper) GetTaskIDFromLabels(ctx context.Context) (string, bool) {
	return pprof.Label(ctx, "task_id")
}

// GetTaskNameFromLabels returns the task name from pprof labels
// This must be called from within a pprof.Do context
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

// UpdateTaskStage updates the task stage in pprof labels
func (ph *PprofHelper) UpdateTaskStage(ctx context.Context, stage string) {
	labels := pprof.Labels("task_stage", stage)
	pprof.SetGoroutineLabels(pprof.WithLabels(ctx, labels))
}

// CreateChildTaskLabels creates labels for child tasks that inherit parent context
func (ph *PprofHelper) CreateChildTaskLabels(parentCtx context.Context, childTaskName string) pprof.LabelSet {
	return pprof.Labels(
		"task_type", "child",
		"child_task_name", childTaskName,
		"parent_task_id", func() string {
			if id, ok := pprof.Label(parentCtx, "task_id"); ok {
				return id
			}
			return "unknown"
		}(),
	)
}