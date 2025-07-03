//go:build darwin || linux || freebsd || netbsd || openbsd
// +build darwin linux freebsd netbsd openbsd

package psnotify_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	mockpsnotify "github.com/walteh/runm/gen/mocks/pkg/psnotify"
	"github.com/walteh/runm/pkg/psnotify"
)

func TestDetectorWithMocks(t *testing.T) {
	// Create channels for the mock
	forkChan := make(chan *psnotify.ProcEventFork, 1)
	execChan := make(chan *psnotify.ProcEventExec, 1)
	exitChan := make(chan *psnotify.ProcEventExit, 1)
	errorChan := make(chan error, 1)

	// Create mock watcher with functions
	mockWatcher := &mockpsnotify.MockProcessWatcher{
		GetForkChannelFunc: func() <-chan *psnotify.ProcEventFork {
			return forkChan
		},
		GetExecChannelFunc: func() <-chan *psnotify.ProcEventExec {
			return execChan
		},
		GetExitChannelFunc: func() <-chan *psnotify.ProcEventExit {
			return exitChan
		},
		GetErrorChannelFunc: func() <-chan error {
			return errorChan
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

	// Watch a process
	err = detector.Watch(1000, psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT)
	assert.NoError(t, err)

	// Verify the Watch call was made correctly
	watchCalls := mockWatcher.WatchCalls()
	assert.Len(t, watchCalls, 1, "Watch should be called once")
	assert.Equal(t, 1000, watchCalls[0].Pid, "Watch should be called with pid 1000")
	assert.Equal(t, uint32(psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT),
		watchCalls[0].Flags, "Watch should be called with the right flags")

	// Create channel to collect events
	forkExecEvents := make(chan *psnotify.DetectedEventForkExec, 1)

	// Create a goroutine to collect events
	go func() {
		select {
		case event := <-detector.ForkExec:
			forkExecEvents <- event
		case <-time.After(2 * time.Second):
			t.Log("Timeout waiting for fork+exec event")
		}
	}()

	// Send the fork event first
	timestamp := time.Now()
	forkEvent := &psnotify.ProcEventFork{
		ParentPid: 1000,
		ChildPid:  1001,
		Timestamp: timestamp,
	}
	forkChan <- forkEvent

	// Small delay to ensure the fork event is processed
	time.Sleep(100 * time.Millisecond)

	// Now send the exec event for the same process
	execEvent := &psnotify.ProcEventExec{
		Pid:       1001,
		Timestamp: timestamp.Add(10 * time.Millisecond),
	}
	execChan <- execEvent

	// Wait for the event to be processed
	time.Sleep(500 * time.Millisecond)

	// Check that we received a fork+exec event
	assert.Eventually(t, func() bool {
		return len(forkExecEvents) > 0
	}, 2*time.Second, 100*time.Millisecond, "Should receive fork+exec event")

	if len(forkExecEvents) > 0 {
		event := <-forkExecEvents
		assert.Equal(t, 1000, int(event.Fork.ParentPid))
		assert.Equal(t, 1001, int(event.Fork.ChildPid))
		assert.Equal(t, 1001, int(event.Exec.Pid))
	}

	// Test exit/done event
	doneEvents := make(chan *psnotify.DetectedEventDone, 1)
	go func() {
		select {
		case event := <-detector.Done:
			doneEvents <- event
		case <-time.After(2 * time.Second):
			t.Log("Timeout waiting for done event")
		}
	}()

	// Send the exit event
	exitEvent := &psnotify.ProcEventExit{
		Pid:       1001,
		ExitCode:  0,
		Timestamp: timestamp.Add(50 * time.Millisecond),
	}
	exitChan <- exitEvent

	// Verify done event was received (may take longer due to waitByPidfd)
	assert.Eventually(t, func() bool {
		return len(doneEvents) > 0
	}, 2*time.Second, 100*time.Millisecond, "Should receive done event")

	if len(doneEvents) > 0 {
		event := <-doneEvents
		assert.Equal(t, 1001, int(event.Exit.Pid))
		assert.Equal(t, 0, int(event.Exit.ExitCode))
	}

	// Test error propagation
	errorEvents := make(chan error, 1)
	go func() {
		select {
		case err := <-detector.Error:
			errorEvents <- err
		case <-time.After(2 * time.Second):
			t.Log("Timeout waiting for error event")
		}
	}()

	// Simulate an error
	testError := assert.AnError
	errorChan <- testError

	// Verify error was received
	assert.Eventually(t, func() bool {
		return len(errorEvents) > 0
	}, 2*time.Second, 100*time.Millisecond, "Should receive error event")

	if len(errorEvents) > 0 {
		err := <-errorEvents
		assert.Equal(t, testError, err)
	}

	// Test clean shutdown
	err = detector.Close()
	assert.NoError(t, err)

	// Verify Close was called
	assert.Len(t, mockWatcher.CloseCalls(), 1, "Close should be called once")
}

func TestDetectorErrorPropagation(t *testing.T) {
	// Create a test error
	testError := assert.AnError

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
			return testError
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

	// Watch a process - should return error
	err = detector.Watch(1000, psnotify.PROC_EVENT_FORK)
	assert.Equal(t, testError, err)

	// Verify Watch was called
	watchCalls := mockWatcher.WatchCalls()
	assert.Len(t, watchCalls, 1, "Watch should be called once")
	assert.Equal(t, 1000, watchCalls[0].Pid)
	assert.Equal(t, uint32(psnotify.PROC_EVENT_FORK), watchCalls[0].Flags)
}

func TestMultipleEvents(t *testing.T) {
	// Create channels for the mock
	forkChan := make(chan *psnotify.ProcEventFork, 5)
	execChan := make(chan *psnotify.ProcEventExec, 5)
	exitChan := make(chan *psnotify.ProcEventExit, 5)
	errorChan := make(chan error, 1)

	// Create mock watcher with functions
	mockWatcher := &mockpsnotify.MockProcessWatcher{
		GetForkChannelFunc: func() <-chan *psnotify.ProcEventFork {
			return forkChan
		},
		GetExecChannelFunc: func() <-chan *psnotify.ProcEventExec {
			return execChan
		},
		GetExitChannelFunc: func() <-chan *psnotify.ProcEventExit {
			return exitChan
		},
		GetErrorChannelFunc: func() <-chan error {
			return errorChan
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

	// Watch multiple processes
	for pid := 1000; pid < 1003; pid++ {
		err = detector.Watch(pid, psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT)
		assert.NoError(t, err)
	}

	// Verify Watch was called for each PID
	watchCalls := mockWatcher.WatchCalls()
	assert.Len(t, watchCalls, 3, "Watch should be called three times")

	// Channel to collect events
	forkExecEvents := make(chan *psnotify.DetectedEventForkExec, 3)

	// Start collecting events in a separate goroutine
	go func() {
		for i := 0; i < 3; i++ {
			select {
			case event := <-detector.ForkExec:
				forkExecEvents <- event
			case <-time.After(2 * time.Second):
				t.Log("Timeout waiting for event", i)
				return
			}
		}
	}()

	// Simulate multiple fork events
	timestamp := time.Now()

	// Process 1 - fork first
	forkChan <- &psnotify.ProcEventFork{ParentPid: 1000, ChildPid: 1001, Timestamp: timestamp}
	time.Sleep(50 * time.Millisecond)

	// Process 1 - exec second
	execChan <- &psnotify.ProcEventExec{Pid: 1001, Timestamp: timestamp.Add(10 * time.Millisecond)}
	time.Sleep(50 * time.Millisecond)

	// Process 2 - fork first
	forkChan <- &psnotify.ProcEventFork{ParentPid: 1001, ChildPid: 1002, Timestamp: timestamp}
	time.Sleep(50 * time.Millisecond)

	// Process 2 - exec second
	execChan <- &psnotify.ProcEventExec{Pid: 1002, Timestamp: timestamp.Add(20 * time.Millisecond)}
	time.Sleep(50 * time.Millisecond)

	// Process 3 - fork first
	forkChan <- &psnotify.ProcEventFork{ParentPid: 1002, ChildPid: 1003, Timestamp: timestamp}
	time.Sleep(50 * time.Millisecond)

	// Process 3 - exec second
	execChan <- &psnotify.ProcEventExec{Pid: 1003, Timestamp: timestamp.Add(30 * time.Millisecond)}

	// Wait and check that we got all three events
	time.Sleep(1 * time.Second)

	// Assert we received three events
	assert.Equal(t, 3, len(forkExecEvents), "Should receive three fork+exec events")
}
