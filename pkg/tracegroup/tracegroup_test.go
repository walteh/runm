package tracegroup

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTraceGroup_BasicUsage(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx)

	var counter int64
	tg.Go(func() error {
		atomic.AddInt64(&counter, 1)
		return nil
	})

	tg.Go(func() error {
		atomic.AddInt64(&counter, 1)
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.Equal(t, int64(2), atomic.LoadInt64(&counter))
}

func TestTraceGroup_WithError(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx)

	expectedErr := errors.New("test error")
	tg.Go(func() error {
		return expectedErr
	})

	tg.Go(func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	err := tg.Wait()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "test error")
}

func TestTraceGroup_WithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tg := NewTraceGroup(ctx)

	var started bool
	tg.Go(func() error {
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

func TestTraceGroup_WithTimeout(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx, WithTimeout(100*time.Millisecond))

	tg.Go(func() error {
		// Use the tracegroup context to respect timeout
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

func TestTraceGroup_WithLimit(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx, WithMaxConcurrent(2))

	var concurrent int64
	var maxConcurrent int64
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		tg.Go(func() error {
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

func TestTraceGroup_TryGo(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx, WithMaxConcurrent(1))

	// First should succeed
	success1 := tg.TryGo(func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	assert.True(t, success1)

	// Second should fail immediately due to limit
	success2 := tg.TryGo(func() error {
		return nil
	})
	assert.False(t, success2)

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTraceGroup_WithName(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx, WithName("test-group"))

	tg.GoWithName("task1", func() error {
		return nil
	})

	tg.GoWithName("task2", func() error {
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

func TestTraceGroup_Status(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx)

	// Initial status
	started, finished, taskCount, err := tg.Status()
	assert.False(t, started)
	assert.False(t, finished)
	assert.Equal(t, 0, taskCount)
	assert.NoError(t, err)

	// Start a task
	tg.Go(func() error {
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

func TestTraceGroup_WithLogLevels(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx, 
		WithLogLevel(slog.LevelDebug),
		WithLogStart(true),
		WithLogEnd(true),
		WithLogTaskStart(true),
		WithLogTaskEnd(true),
	)

	tg.GoWithName("debug-task", func() error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTraceGroup_MultipleErrors(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx)

	err1 := errors.New("error 1")
	err2 := errors.New("error 2")

	tg.Go(func() error {
		return err1
	})

	tg.Go(func() error {
		time.Sleep(10 * time.Millisecond)
		return err2
	})

	err := tg.Wait()
	assert.Error(t, err)
	// Should get the first error that occurred
	assert.Contains(t, err.Error(), "error 1")
}

func TestTraceGroup_WithContext_Function(t *testing.T) {
	ctx := context.Background()
	tg, groupCtx := WithContext(ctx, WithName("context-group"))

	assert.NotNil(t, tg)
	assert.NotNil(t, groupCtx)

	var ctxReceived context.Context
	tg.Go(func() error {
		ctxReceived = groupCtx
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
	assert.Equal(t, groupCtx, ctxReceived)
}

func TestTraceGroup_SetLimit(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx)

	// Set limit before starting
	tg.SetLimit(2)

	var concurrent int64
	var maxConcurrent int64

	for i := 0; i < 4; i++ {
		tg.Go(func() error {
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

func TestTraceGroup_SetLimitAfterStart(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx)

	// Start a task first
	tg.Go(func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Try to set limit after starting - should be ignored
	tg.SetLimit(1)

	// Add more tasks
	var concurrent int64
	for i := 0; i < 3; i++ {
		tg.Go(func() error {
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

func TestTraceGroup_WithAttrFunc(t *testing.T) {
	ctx := context.Background()
	
	attrFunc := func() []slog.Attr {
		return []slog.Attr{
			slog.String("custom", "value"),
			slog.Int("test", 42),
		}
	}
	
	tg := NewTraceGroup(ctx, WithAttrFunc(attrFunc))

	tg.Go(func() error {
		return nil
	})

	err := tg.Wait()
	assert.NoError(t, err)
}

func TestTraceGroup_ZeroTasks(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx)

	err := tg.Wait()
	assert.NoError(t, err)

	started, finished, taskCount, taskErr := tg.Status()
	assert.True(t, started)
	assert.True(t, finished)
	assert.Equal(t, 0, taskCount)
	assert.NoError(t, taskErr)
}

func TestTraceGroup_TryGoWithName(t *testing.T) {
	ctx := context.Background()
	tg := NewTraceGroup(ctx, WithMaxConcurrent(1))

	// First should succeed
	success1 := tg.TryGoWithName("task1", func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	assert.True(t, success1)

	// Second should fail immediately due to limit
	success2 := tg.TryGoWithName("task2", func() error {
		return nil
	})
	assert.False(t, success2)

	err := tg.Wait()
	assert.NoError(t, err)
}

func BenchmarkTraceGroup_BasicUsage(b *testing.B) {
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg := NewTraceGroup(ctx, WithLogStart(false), WithLogEnd(false))
		
		for j := 0; j < 10; j++ {
			tg.Go(func() error {
				return nil
			})
		}
		
		_ = tg.Wait()
	}
}

func BenchmarkTraceGroup_WithLimit(b *testing.B) {
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg := NewTraceGroup(ctx, 
			WithMaxConcurrent(5),
			WithLogStart(false), 
			WithLogEnd(false),
			WithLogTaskStart(false),
			WithLogTaskEnd(false),
		)
		
		for j := 0; j < 20; j++ {
			tg.Go(func() error {
				return nil
			})
		}
		
		_ = tg.Wait()
	}
}