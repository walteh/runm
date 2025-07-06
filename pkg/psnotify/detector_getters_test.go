//go:build !windows

package psnotify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/walteh/runm/pkg/psnotify"

	mockpsnotify "github.com/walteh/runm/gen/mocks/pkg/psnotify"
)

// TestDetectorGetters tests the getter methods of the detector
func TestDetectorGetters(t *testing.T) {
	// Create mock watcher with functions
	mockWatcher := &mockpsnotify.MockProcessWatcher{
		GetForkChannelFunc: func() <-chan *psnotify.ProcEventFork {
			return make(chan *psnotify.ProcEventFork)
		},
		GetExecChannelFunc: func() <-chan *psnotify.ProcEventExec {
			return make(chan *psnotify.ProcEventExec)
		},
		GetExitChannelFunc: func() <-chan *psnotify.ProcEventExit {
			return make(chan *psnotify.ProcEventExit)
		},
		GetErrorChannelFunc: func() <-chan error {
			return make(chan error)
		},
		WatchFunc: func(pid int, flags uint32) error {
			return nil
		},
		RemoveWatchFunc: func(pid int) error {
			return nil
		},
		CloseFunc: func() error {
			return nil
		},
	}

	// Create detector with mock watcher
	detector, err := psnotify.NewDetector(psnotify.DetectorConfig{
		Watcher: mockWatcher,
	})

	// Assert detector created successfully
	assert.NoError(t, err)
	assert.NotNil(t, detector)
	defer detector.Close()

	// Test GetForkExecChannel
	forkExecChan := detector.GetForkExecChannel()
	assert.NotNil(t, forkExecChan, "GetForkExecChannel should return a non-nil channel")

	// Test GetDoneChannel
	doneChan := detector.GetDoneChannel()
	assert.NotNil(t, doneChan, "GetDoneChannel should return a non-nil channel")

	// Test GetErrorChannel
	errorChan := detector.GetErrorChannel()
	assert.NotNil(t, errorChan, "GetErrorChannel should return a non-nil channel")

	// Test RemoveWatch
	err = detector.RemoveWatch(1000)
	assert.NoError(t, err, "RemoveWatch should not return an error")

	// Verify RemoveWatch was called
	removeWatchCalls := mockWatcher.RemoveWatchCalls()
	assert.Len(t, removeWatchCalls, 1, "RemoveWatch should be called once")
	assert.Equal(t, 1000, removeWatchCalls[0].Pid, "RemoveWatch should be called with pid 1000")
}
