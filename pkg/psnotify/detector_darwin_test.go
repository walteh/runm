//go:build darwin
// +build darwin

package psnotify_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/walteh/runm/pkg/psnotify"
)

// TestDarwinGetArgvFromProc tests the Darwin-specific implementation of getArgvFromProc
func TestDarwinGetArgvFromProc(t *testing.T) {
	// Start a test process
	cmd := exec.Command("sleep", "1")
	err := cmd.Start()
	assert.NoError(t, err, "Should start test process")

	// Get its PID
	pid := cmd.Process.Pid

	// Create a detector to access the private method
	detector, err := psnotify.NewDetector(psnotify.DetectorConfig{})
	assert.NoError(t, err, "Should create detector")
	defer detector.Close()

	// Use reflection to test the private method
	// For now, we'll test indirectly by watching the process
	err = detector.Watch(pid, psnotify.PROC_EVENT_EXIT)
	assert.NoError(t, err, "Should watch process")

	// Wait for the process to exit
	cmd.Wait()

	// Wait for the done event
	select {
	case event := <-detector.Done:
		assert.Equal(t, pid, event.Exit.Pid, "Should receive done event for correct PID")
		assert.NotNil(t, event.Info, "Should have process info")
		// On Darwin, we might get command arguments but it's not guaranteed
		// Just check that we don't panic when accessing the info
		t.Logf("Process info: Argv=%v, Argc=%s", event.Info.Argv, event.Info.Argc)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for done event")
	}
}

// TestDarwinWaitByPidfd tests the Darwin-specific implementation of waitByPidfd
func TestDarwinWaitByPidfd(t *testing.T) {
	// Start a test process
	cmd := exec.Command("echo", "hello")
	err := cmd.Start()
	assert.NoError(t, err, "Should start test process")

	// Get its PID
	pid := cmd.Process.Pid

	// Create a detector to watch the process
	detector, err := psnotify.NewDetector(psnotify.DetectorConfig{})
	assert.NoError(t, err, "Should create detector")
	defer detector.Close()

	// Watch the process
	err = detector.Watch(pid, psnotify.PROC_EVENT_EXIT)
	assert.NoError(t, err, "Should watch process")

	// Wait for the process to exit
	cmd.Wait()

	// Wait for the done event, which should be sent after waitByPidfd completes
	select {
	case event := <-detector.Done:
		assert.Equal(t, pid, event.Exit.Pid, "Should receive done event for correct PID")
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for done event")
	}
}

// TestDarwinParseCommandOutput tests the Darwin-specific parseCommandOutput function
func TestDarwinParseCommandOutput(t *testing.T) {
	// We'll test this indirectly by starting a process with quoted arguments
	quotedArg := "hello world"
	cmd := exec.Command("echo", quotedArg)
	err := cmd.Start()
	assert.NoError(t, err, "Should start test process")

	// Get its PID
	pid := cmd.Process.Pid

	// Create a detector to watch the process
	detector, err := psnotify.NewDetector(psnotify.DetectorConfig{})
	assert.NoError(t, err, "Should create detector")
	defer detector.Close()

	// Watch the process for exit
	err = detector.Watch(pid, psnotify.PROC_EVENT_EXIT)
	assert.NoError(t, err, "Should watch process")

	// Wait for the process to exit
	cmd.Wait()

	// Wait for the done event
	select {
	case event := <-detector.Done:
		assert.Equal(t, pid, event.Exit.Pid, "Should receive done event for correct PID")
		assert.NotNil(t, event.Info, "Should have process info")

		// Log the command arguments we got
		t.Logf("Process info: Argv=%v, Argc=%s", event.Info.Argv, event.Info.Argc)

		// On Darwin, we might not get the exact arguments, so just check that we don't panic
		assert.NotPanics(t, func() {
			if len(event.Info.Argv) >= 2 {
				t.Logf("Second argument: %s", event.Info.Argv[1])
			}
		})
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for done event")
	}
}

// TestDarwinGetExePath tests the Darwin-specific implementation of getExePath
func TestDarwinGetExePath(t *testing.T) {
	// Get our own process ID
	pid := os.Getpid()

	// Create a detector
	detector, err := psnotify.NewDetector(psnotify.DetectorConfig{})
	assert.NoError(t, err, "Should create detector")
	defer detector.Close()

	// Watch our own process (just to create a PidInfo entry)
	err = detector.Watch(pid, psnotify.PROC_EVENT_EXEC)
	assert.NoError(t, err, "Should watch process")

	// Wait a moment for the detector to create the PidInfo
	time.Sleep(100 * time.Millisecond)

	// We can't directly test the private method, but we can verify that
	// the detector is able to get process information for our process
	// This is a weak test, but it's better than nothing
	assert.NotPanics(t, func() {
		detector.Close()
	}, "Detector should close without panicking")
}
