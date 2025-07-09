package taskgroup_test

import (
	"context"
	"runtime/pprof"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPprofLabelsInGoroutine(t *testing.T) {
	// Test how pprof labels work in a goroutine
	labels := pprof.Labels("test_key", "test_value")
	
	var capturedLabels map[string]string
	
	pprof.Do(context.Background(), labels, func(ctx context.Context) {
		// The context here contains the labels
		capturedLabels = make(map[string]string)
		pprof.ForLabels(ctx, func(key, value string) bool {
			capturedLabels[key] = value
			return true
		})
	})
	
	assert.Equal(t, "test_value", capturedLabels["test_key"])
}

func TestPprofLabelsInGoroutineWithGoroutineSpawn(t *testing.T) {
	// Test how pprof labels work when spawning a goroutine
	labels := pprof.Labels("test_key", "test_value")
	
	var capturedLabels map[string]string
	done := make(chan struct{})
	
	pprof.Do(context.Background(), labels, func(ctx context.Context) {
		// Spawn a goroutine
		go func() {
			defer close(done)
			capturedLabels = make(map[string]string)
			pprof.ForLabels(ctx, func(key, value string) bool {
				capturedLabels[key] = value
				return true
			})
		}()
	})
	
	<-done
	assert.Equal(t, "test_value", capturedLabels["test_key"])
}

func TestPprofLabelsInGoroutineWithGoroutineSpawnNoContext(t *testing.T) {
	// Test how pprof labels work when spawning a goroutine without context
	labels := pprof.Labels("test_key", "test_value")
	
	var capturedLabels map[string]string
	done := make(chan struct{})
	
	pprof.Do(context.Background(), labels, func(ctx context.Context) {
		// Spawn a goroutine
		go func() {
			defer close(done)
			capturedLabels = make(map[string]string)
			pprof.ForLabels(context.Background(), func(key, value string) bool {
				capturedLabels[key] = value
				return true
			})
		}()
	})
	
	<-done
	assert.Equal(t, "test_value", capturedLabels["test_key"])
}