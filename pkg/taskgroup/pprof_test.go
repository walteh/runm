package taskgroup_test

import (
	"context"
	"runtime/pprof"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPprofLabelsSimple(t *testing.T) {
	// Test how pprof labels work
	labels := pprof.Labels("test_key", "test_value")
	
	var capturedLabels map[string]string
	
	pprof.Do(context.Background(), labels, func(ctx context.Context) {
		capturedLabels = make(map[string]string)
		pprof.ForLabels(ctx, func(key, value string) bool {
			capturedLabels[key] = value
			return true
		})
	})
	
	assert.Equal(t, "test_value", capturedLabels["test_key"])
}

func TestPprofLabelsWithoutContext(t *testing.T) {
	// Test how pprof labels work without context
	labels := pprof.Labels("test_key", "test_value")
	
	var capturedLabels map[string]string
	
	pprof.Do(context.Background(), labels, func(ctx context.Context) {
		capturedLabels = make(map[string]string)
		// Try using different contexts
		pprof.ForLabels(context.Background(), func(key, value string) bool {
			capturedLabels[key] = value
			return true
		})
	})
	
	assert.Equal(t, "test_value", capturedLabels["test_key"])
}

func TestPprofLabelsGoroutineLocal(t *testing.T) {
	// Test if labels are goroutine-local
	labels := pprof.Labels("test_key", "test_value")
	
	var capturedLabels map[string]string
	
	pprof.Do(context.Background(), labels, func(ctx context.Context) {
		// Call a function that doesn't receive the context
		capturedLabels = getCapturedLabels()
	})
	
	assert.Equal(t, "test_value", capturedLabels["test_key"])
}

func getCapturedLabels() map[string]string {
	labels := make(map[string]string)
	// Try to get labels without the context
	pprof.ForLabels(context.Background(), func(key, value string) bool {
		labels[key] = value
		return true
	})
	return labels
}