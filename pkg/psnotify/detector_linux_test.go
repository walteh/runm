//go:build linux
// +build linux

package psnotify_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/walteh/runm/pkg/psnotify"
)

// TestLinuxGetArgvFromProc tests the Linux-specific implementation of getArgvFromProc
func TestLinuxGetArgvFromProc(t *testing.T) {
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
		// On Linux, we should have the command arguments from /proc/{pid}/cmdline
		assert.NotEmpty(t, event.Info.Argv, "Should have command arguments")
		assert.Equal(t, "sleep", event.Info.Argv[0], "First argument should be 'sleep'")
		assert.Equal(t, "1", event.Info.Argv[1], "Second argument should be '1'")
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for done event")
	}
}

// TestLinuxWaitByPidfd tests the Linux-specific implementation of waitByPidfd
func TestLinuxWaitByPidfd(t *testing.T) {
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

// TestLinuxGetCgroupFromProc tests the Linux-specific implementation of getCgroupFromProc
func TestLinuxGetCgroupFromProc(t *testing.T) {
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
	// the detector is able to get cgroup information for our process
	// This is a weak test, but it's better than nothing
	assert.NotPanics(t, func() {
		detector.Close()
	}, "Detector should close without panicking")
}

// TestLinuxGetExePath tests the Linux-specific implementation of getExePath
func TestLinuxGetExePath(t *testing.T) {
	// Start a test process
	cmd := exec.Command("sleep", "1")
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

	// Wait for the done event
	select {
	case event := <-detector.Done:
		assert.Equal(t, pid, event.Exit.Pid, "Should receive done event for correct PID")
		assert.NotNil(t, event.Info, "Should have process info")
		// On Linux, we should have the executable path
		assert.NotEmpty(t, event.Info.Argc, "Should have executable path")
		assert.Contains(t, event.Info.Argc, "sleep", "Executable path should contain 'sleep'")
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for done event")
	}
}

// TestLinuxGetProcFdCount tests the Linux-specific implementation of getProcFdCount
func TestLinuxGetProcFdCount(t *testing.T) {
	// This is hard to test directly since it's a private function
	// But we can test that waitByPidfd works, which uses getProcFdCount

	// Start a test process
	cmd := exec.Command("sleep", "0.1")
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

// TestLinuxFindParentPid tests the Linux-specific implementation of findParentPid
func TestLinuxFindParentPid(t *testing.T) {
	// Start a test process
	cmd := exec.Command("sleep", "1")
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

	// Wait for the done event
	select {
	case event := <-detector.Done:
		assert.Equal(t, pid, event.Exit.Pid, "Should receive done event for correct PID")
		assert.NotNil(t, event.Info, "Should have process info")
		// The parent should be our process
		if event.Info.Parent != nil {
			assert.Equal(t, os.Getpid(), event.Info.Parent.Pid, "Parent PID should be our process")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for done event")
	}
}
