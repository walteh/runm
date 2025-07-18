package taskgroup

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
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

	// Cancellation tracking
	cancelReason     string
	cancelCaller     uintptr
	cancelTime       time.Time
	cancelStack      string
	manualCancelOnce sync.Once
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

	// Add blocking detection
	if tg.opts.enablePprof {
		blockedTasks := tg.detectBlockedTasks(runningTasks)
		if len(blockedTasks) > 0 {
			attrs = append(attrs, slog.Int("blocked_tasks", len(blockedTasks)))

			// Add detailed blocking information
			var blockingDetails []slog.Attr
			for _, blocked := range blockedTasks {
				blockingDetails = append(blockingDetails, slog.Group(blocked.TaskID,
					slog.String("task_name", blocked.TaskName),
					slog.Uint64("goroutine_id", blocked.GoroutineID),
					slog.String("block_reason", blocked.BlockReason),
					slog.Duration("blocked_duration", blocked.Duration),
					slog.Group("blocked_at",
						slog.String("function", blocked.BlockedAt.Function),
						slog.String("file", blocked.BlockedAt.File),
						slog.Int("line", blocked.BlockedAt.Line),
					),
				))
			}
			attrs = append(attrs, slog.Group("blocking_details", slog.Any("tasks", blockingDetails)))
		}
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

	taskIDX := atomic.AddInt64(&tg.taskCounter, 1)
	taskID := fmt.Sprintf("%s-%d", tg.opts.name, taskIDX)

	taskState := &TaskState{
		ID:          taskID,
		IDX:         int(taskIDX),
		Group:       tg,
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
				slog.String("tid", taskID),
				slog.String("tname", name),
				slog.Duration("dur", duration),
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

		// Log cancellation information
		reason, caller, when, _ := tg.GetCancellationInfo()
		if reason != "" {
			tg.log(slog.LevelInfo, "taskgroup cancelled",
				slog.String("reason", reason),
				slog.String("caller", runtime.FuncForPC(caller).Name()),
				slog.Time("cancelled_at", when),
			)
		} else {
			// Context was cancelled externally (parent context, timeout, etc.)
			tg.log(slog.LevelInfo, "taskgroup cancelled by external context",
				slog.String("context_error", tg.ctx.Err().Error()),
			)
		}
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

// detectBlockedTasks analyzes running tasks to find blocked goroutines
func (tg *TaskGroup) detectBlockedTasks(runningTasks []*TaskState) []BlockedTaskInfo {
	if !tg.opts.enablePprof || len(runningTasks) == 0 {
		return nil
	}

	var blockedTasks []BlockedTaskInfo

	// Get goroutine profile using pprof
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return nil
	}

	// Create a buffer to capture the profile data
	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, 2); err != nil {
		return nil
	}

	// Parse the profile output to find blocked goroutines
	profileData := buf.String()
	blockedTasks = tg.parseBlockedGoroutines(profileData, runningTasks)

	return blockedTasks
}

// parseBlockedGoroutines parses pprof goroutine output to identify blocked tasks
func (tg *TaskGroup) parseBlockedGoroutines(profileData string, runningTasks []*TaskState) []BlockedTaskInfo {
	var blockedTasks []BlockedTaskInfo

	// Create a map of goroutine IDs to task states for quick lookup
	goroutineToTask := make(map[uint64]*TaskState)
	for _, task := range runningTasks {
		if task.GoroutineID != 0 {
			goroutineToTask[task.GoroutineID] = task
		}
	}

	// Split profile data into individual goroutine entries
	lines := strings.Split(profileData, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Look for goroutine headers like "goroutine 123 [blocked]:"
		if strings.HasPrefix(line, "goroutine ") && strings.Contains(line, "[") {
			goroutineID, blockReason, blocked := tg.parseGoroutineHeader(line)
			if !blocked || goroutineID == 0 {
				continue
			}

			// Check if this goroutine belongs to one of our running tasks
			if task, exists := goroutineToTask[goroutineID]; exists {
				// Parse the stack trace to find the blocking location
				blockLocation := tg.parseBlockingLocation(lines, i+1)

				blockedTask := BlockedTaskInfo{
					TaskID:      task.ID,
					TaskName:    task.Name,
					GoroutineID: goroutineID,
					BlockReason: blockReason,
					BlockedAt:   blockLocation,
					Duration:    time.Since(task.StartTime),
				}

				blockedTasks = append(blockedTasks, blockedTask)
			}
		}
	}

	return blockedTasks
}

// parseGoroutineHeader parses a goroutine header line to extract ID and block reason
func (tg *TaskGroup) parseGoroutineHeader(line string) (uint64, string, bool) {
	// Example: "goroutine 123 [chan receive]:"
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return 0, "", false
	}

	// Extract goroutine ID
	goroutineID := uint64(0)
	if id, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
		goroutineID = id
	}

	// Extract block reason from brackets
	reasonPart := strings.Join(parts[2:], " ")
	if start := strings.Index(reasonPart, "["); start != -1 {
		if end := strings.Index(reasonPart[start:], "]"); end != -1 {
			blockReason := reasonPart[start+1 : start+end]

			// Check if this is a blocking state
			isBlocked := tg.isBlockingState(blockReason)
			return goroutineID, blockReason, isBlocked
		}
	}

	return goroutineID, "", false
}

// isBlockingState determines if a goroutine state indicates blocking
func (tg *TaskGroup) isBlockingState(state string) bool {
	blockingStates := []string{
		"chan receive",
		"chan send",
		"select",
		"IO wait",
		"sync.Mutex",
		"sync.RWMutex",
		"sync.WaitGroup",
		"sync.Cond",
		"semacquire",
		"sleep",
		"timer goroutine",
		"chan receive (nil chan)",
		"chan send (nil chan)",
	}

	for _, blockingState := range blockingStates {
		if strings.Contains(state, blockingState) {
			return true
		}
	}

	return false
}

// parseBlockingLocation parses stack trace lines to find where the goroutine is blocked
func (tg *TaskGroup) parseBlockingLocation(lines []string, startIndex int) BlockLocation {
	// Look for the first non-runtime frame in the stack trace
	for i := startIndex; i < len(lines) && i < startIndex+10; i++ {
		line := strings.TrimSpace(lines[i])

		// Skip empty lines and runtime frames
		if line == "" || strings.HasPrefix(line, "runtime.") {
			continue
		}

		// Look for function name
		if strings.Contains(line, "(") {
			// Next line should contain file:line
			if i+1 < len(lines) {
				nextLine := strings.TrimSpace(lines[i+1])
				if strings.Contains(nextLine, ":") {
					return tg.parseFileLocation(line, nextLine)
				}
			}
		}
	}

	return BlockLocation{
		Function: "unknown",
		File:     "unknown",
		Line:     0,
	}
}

// parseFileLocation parses function name and file:line information
func (tg *TaskGroup) parseFileLocation(funcLine, fileLine string) BlockLocation {
	function := strings.Split(funcLine, "(")[0]

	// Parse file:line
	if colonIndex := strings.LastIndex(fileLine, ":"); colonIndex != -1 {
		file := fileLine[:colonIndex]
		lineStr := fileLine[colonIndex+1:]

		// Remove any additional info after the line number
		if spaceIndex := strings.Index(lineStr, " "); spaceIndex != -1 {
			lineStr = lineStr[:spaceIndex]
		}

		if line, err := strconv.Atoi(lineStr); err == nil {
			return BlockLocation{
				Function: function,
				File:     file,
				Line:     line,
			}
		}
	}

	return BlockLocation{
		Function: function,
		File:     "unknown",
		Line:     0,
	}
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
	ctx = slogctx.Append(ctx, "t", taskState)
	// ctx = slogctx.Append(ctx, slog.String("tg.name", tg.opts.name))

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

// CancelWithReason manually cancels the TaskGroup with a specific reason
func (tg *TaskGroup) CancelWithReason(reason string) {
	tg.manualCancelOnce.Do(func() {
		caller, _, _, _ := runtime.Caller(1)

		tg.mu.Lock()
		tg.cancelReason = reason
		tg.cancelCaller = caller
		tg.cancelTime = time.Now()
		tg.cancelStack = string(debug.Stack())
		tg.mu.Unlock()

		tg.log(slog.LevelInfo, "taskgroup cancelled manually",
			slog.String("reason", reason),
			slog.String("caller", runtime.FuncForPC(caller).Name()),
		)

		tg.cancel()
	})
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
	tg.CancelWithReason("Close() called")

	// Execute cleanup functions
	tg.executeCleanup(context.Background())

	return nil
}

// GetCancellationInfo returns information about why/when the context was cancelled
func (tg *TaskGroup) GetCancellationInfo() (reason string, caller uintptr, when time.Time, stack string) {
	tg.mu.RLock()
	defer tg.mu.RUnlock()
	return tg.cancelReason, tg.cancelCaller, tg.cancelTime, tg.cancelStack
}

// IsCancelled returns true if the taskgroup context has been cancelled
func (tg *TaskGroup) IsCancelled() bool {
	select {
	case <-tg.ctx.Done():
		return true
	default:
		return false
	}
}

// LogCancellationIfCancelled logs cancellation information if the context is cancelled
// This is useful for tasks to call when they detect cancellation
func (tg *TaskGroup) LogCancellationIfCancelled(taskName string) bool {
	if !tg.IsCancelled() {
		return false
	}

	reason, caller, when, _ := tg.GetCancellationInfo()
	if reason != "" {
		tg.log(slog.LevelInfo, "task detected taskgroup cancellation",
			slog.String("task_name", taskName),
			slog.String("cancel_reason", reason),
			slog.String("cancel_caller", runtime.FuncForPC(caller).Name()),
			slog.Time("cancelled_at", when),
		)
	} else {
		tg.log(slog.LevelInfo, "task detected external context cancellation",
			slog.String("task_name", taskName),
			slog.String("context_error", tg.ctx.Err().Error()),
		)
	}
	return true
}
