package taskgroup

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

//go:opts
type TaskGroupOpts struct {
	name            string     `default:"taskgroup"`
	logLevel        slog.Level `default:"-4"`
	logStart        bool       `default:"true"`
	logEnd          bool       `default:"true"`
	logTaskStart    bool       `default:"false"`
	logTaskEnd      bool       `default:"false"`
	timeout         time.Duration
	callerSkip      int `default:"1"`
	slogBaseContext context.Context
	attrFunc        func() []slog.Attr
	maxConcurrent   int // 0 means unlimited
}

type TaskGroup struct {
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.Mutex
	opts        TaskGroupOpts
	err         error
	errOnce     sync.Once
	caller      uintptr
	started     bool
	finished    bool
	taskCounter int
	semaphore   chan struct{}
}

type TaskInfo struct {
	ID       int
	Name     string
	Started  time.Time
	Finished time.Time
	Error    error
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
		ctx:       ctx,
		cancel:    cancel,
		opts:      options,
		caller:    caller,
		semaphore: semaphore,
	}

	return tg
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

	if tg.opts.logStart {
		tg.log(slog.LevelInfo, "taskgroup started", slog.String("name", tg.opts.name))
	}
}

func (tg *TaskGroup) Go(fn func() error) {
	tg.GoWithName("", fn)
}

func (tg *TaskGroup) GoWithName(name string, fn func() error) {
	tg.Start()

	tg.mu.Lock()
	if tg.finished {
		tg.mu.Unlock()
		return
	}

	tg.taskCounter++
	taskID := tg.taskCounter
	if name == "" {
		name = fmt.Sprintf("task-%d", taskID)
	}
	tg.mu.Unlock()

	tg.wg.Add(1)

	go func() {
		defer tg.wg.Done()

		// Acquire semaphore if limited concurrency
		if tg.semaphore != nil {
			select {
			case tg.semaphore <- struct{}{}:
				defer func() { <-tg.semaphore }()
			case <-tg.ctx.Done():
				tg.setError(tg.ctx.Err())
				return
			}
		}

		start := time.Now()
		if tg.opts.logTaskStart {
			tg.log(slog.LevelDebug, "task started",
				slog.Int("task_id", taskID),
				slog.String("task_name", name))
		}

		err := fn()

		if tg.opts.logTaskEnd {
			attrs := []slog.Attr{
				slog.Int("task_id", taskID),
				slog.String("task_name", name),
				slog.Duration("duration", time.Since(start)),
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}
			tg.log(slog.LevelDebug, "task finished", attrs...)
		}

		if err != nil {
			tg.setError(err)
		}
	}()
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

	if tg.opts.logEnd {
		attrs := []slog.Attr{
			slog.String("name", tg.opts.name),
			slog.Int("total_tasks", tg.taskCounter),
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
	tg.Start()

	tg.mu.Lock()
	if tg.finished {
		tg.mu.Unlock()
		return false
	}

	tg.taskCounter++
	taskID := tg.taskCounter
	if name == "" {
		name = fmt.Sprintf("task-%d", taskID)
	}
	tg.mu.Unlock()

	// Check if we can acquire semaphore without blocking
	if tg.semaphore != nil {
		select {
		case tg.semaphore <- struct{}{}:
			// Successfully acquired, will be released in the goroutine
		default:
			// Could not acquire semaphore, undo the task counter increment
			tg.mu.Lock()
			tg.taskCounter--
			tg.mu.Unlock()
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

		start := time.Now()
		if tg.opts.logTaskStart {
			tg.log(slog.LevelDebug, "task started",
				slog.Int("task_id", taskID),
				slog.String("task_name", name))
		}

		err := fn()

		if tg.opts.logTaskEnd {
			attrs := []slog.Attr{
				slog.Int("task_id", taskID),
				slog.String("task_name", name),
				slog.Duration("duration", time.Since(start)),
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
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
	tg.mu.Lock()
	defer tg.mu.Unlock()

	return tg.started, tg.finished, tg.taskCounter, tg.err
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
