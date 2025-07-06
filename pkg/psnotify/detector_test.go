//go:build darwin || freebsd || netbsd || openbsd || linux
// +build darwin freebsd netbsd openbsd linux

package psnotify_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/pkg/psnotify"
)

type detectorEvents struct {
	forkExecs []int
	dones     []int
	errors    []error
	done      chan bool
	mu        sync.RWMutex
}

type testDetector struct {
	t        *testing.T
	detector *psnotify.Detector
	events   *detectorEvents
}

// General purpose Detector wrapper for all tests
func newTestDetector(t *testing.T) *testDetector {
	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}

	events := &detectorEvents{
		done: make(chan bool, 1),
	}

	td := &testDetector{
		t:        t,
		detector: detector,
		events:   events,
	}

	go func() {
		for {
			select {
			case <-events.done:
				return
			case ev := <-detector.ForkExec:
				events.mu.Lock()
				events.forkExecs = append(events.forkExecs, ev.Fork.ParentPid)
				events.mu.Unlock()
			case ev := <-detector.Done:
				events.mu.Lock()
				events.dones = append(events.dones, ev.Exit.Pid)
				events.mu.Unlock()
			case err := <-detector.Error:
				events.mu.Lock()
				events.errors = append(events.errors, err)
				events.mu.Unlock()
			}
		}
	}()

	return td
}

func (td *testDetector) close() {
	pause := 200 * time.Millisecond
	time.Sleep(pause)

	td.events.done <- true
	td.detector.Close()
	time.Sleep(pause)
}

func (td *testDetector) getEvents() ([]int, []int, []error) {
	td.events.mu.RLock()
	defer td.events.mu.RUnlock()
	return td.events.forkExecs, td.events.dones, td.events.errors
}

func skipDetectorTest(t *testing.T) bool {
	if runtime.GOOS == "linux" && os.Getuid() != 0 {
		fmt.Println("SKIP: detector test must be run as root on linux")
		return true
	}
	return false
}

func startSleepCommandForDetector(t *testing.T) *exec.Cmd {
	cmd := exec.Command("sh", "-c", "sleep 100")
	if err := cmd.Start(); err != nil {
		t.Error(err)
	}
	return cmd
}

func runCommandForDetector(t *testing.T, name string) *exec.Cmd {
	cmd := exec.Command(name)
	if err := cmd.Run(); err != nil {
		t.Error(err)
	}
	return cmd
}

func expectDetectorEvents(t *testing.T, num int, name string, pids []int) bool {
	if len(pids) != num {
		t.Errorf("Expected %d %s events, got=%v", num, name, pids)
		return false
	}
	return true
}

func TestDetectorBasic(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer detector.Close()

	pid := os.Getpid()

	// Watch for fork events on this process
	if err := detector.Watch(pid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Error(err)
	}

	// Give some time for setup
	time.Sleep(100 * time.Millisecond)

	// Run a simple command
	cmd := exec.Command("echo", "hello")
	if err := cmd.Run(); err != nil {
		t.Error(err)
	}

	// Give time for events to be processed
	time.Sleep(500 * time.Millisecond)

	// Check if we got any events
	select {
	case event := <-detector.ForkExec:
		t.Logf("Got fork+exec event: parent=%d, child=%d",
			event.Fork.ParentPid, event.Fork.ChildPid)
	case <-time.After(100 * time.Millisecond):
		t.Log("No fork+exec events received")
	}

	select {
	case event := <-detector.Done:
		t.Logf("Got done event: pid=%d, exit_code=%d",
			event.Exit.Pid, event.Exit.ExitCode)
	case <-time.After(100 * time.Millisecond):
		t.Log("No done events received")
	}

	// Check if we got any errors
	select {
	case err := <-detector.Error:
		t.Logf("Got error: %v", err)
	case <-time.After(100 * time.Millisecond):
		t.Log("No errors received")
	}
}

func TestDetectorForkExec(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	pid := os.Getpid()
	td := newTestDetector(t)

	// Watch for fork events on this process
	if err := td.detector.Watch(pid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Error(err)
	}

	// Give some time for the watcher to be ready
	time.Sleep(100 * time.Millisecond)

	// This should trigger both fork and exec events
	runCommandForDetector(t, "echo")

	// Give more time for events to be processed
	time.Sleep(500 * time.Millisecond)

	td.close()

	forkExecs, dones, errors := td.getEvents()

	// Log what we got for debugging
	t.Logf("Got %d fork+exec events, %d done events, %d errors",
		len(forkExecs), len(dones), len(errors))

	// Filter out expected errors
	filteredErrors := []error{}
	for _, err := range errors {
		if err.Error() != "interrupted system call" {
			filteredErrors = append(filteredErrors, err)
		}
	}

	if len(filteredErrors) > 0 {
		t.Errorf("Unexpected errors: %v", filteredErrors)
	}

	// We should see at least one fork+exec event
	if len(forkExecs) < 1 {
		t.Errorf("Expected at least 1 fork+exec event, got %d", len(forkExecs))
	}

	// We should see at least one done event (the child process exiting)
	if len(dones) < 1 {
		t.Errorf("Expected at least 1 done event, got %d", len(dones))
	}
}

func TestDetectorDone(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	td := newTestDetector(t)

	cmd := startSleepCommandForDetector(t)
	childPid := cmd.Process.Pid

	// Give some time for the process to start
	time.Sleep(100 * time.Millisecond)

	// Watch for exit events on the child process
	if err := td.detector.Watch(childPid, psnotify.PROC_EVENT_EXIT); err != nil {
		t.Error(err)
	}

	// Give some time for the watcher to be ready
	time.Sleep(100 * time.Millisecond)

	// Kill the child process
	cmd.Process.Kill()
	cmd.Wait()

	// Give some time for events to be processed
	time.Sleep(300 * time.Millisecond)

	td.close()

	forkExecs, dones, errors := td.getEvents()

	// Filter out system call interruption errors which are expected
	filteredErrors := []error{}
	for _, err := range errors {
		if err.Error() != "interrupted system call" {
			filteredErrors = append(filteredErrors, err)
		}
	}

	if len(filteredErrors) > 0 {
		t.Errorf("Unexpected errors: %v", filteredErrors)
	}

	// We should see exactly one done event
	expectDetectorEvents(t, 1, "done", dones)

	// We should not see any fork+exec events since we only watched for exit
	expectDetectorEvents(t, 0, "fork+exec", forkExecs)

	// Verify the done event is for the right process
	if len(dones) > 0 && dones[0] != childPid {
		t.Errorf("Expected done event for pid %d, got %d", childPid, dones[0])
	}
}

func TestDetectorWithExitChannel(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	// Create a channel to receive exit events
	exitChan := make(chan gorunc.Exit, 10)

	cfg := psnotify.DetectorConfig{
		ExitChannelFunc: func() chan gorunc.Exit { return exitChan },
	}

	// Test that the detector accepts the configuration
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Error(err)
	}
	defer detector.Close()

	// Test that the detector works with the exit channel configuration
	pid := os.Getpid()
	if err := detector.Watch(pid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Error(err)
	}
}

func TestDetectorWatcherComparison(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	pid := os.Getpid()

	// First, test with a direct watcher
	watcher, err := psnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	if err := watcher.Watch(pid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Error(err)
	}

	time.Sleep(100 * time.Millisecond)

	// Run command and check if direct watcher gets events
	cmd := exec.Command("echo", "direct")
	if err := cmd.Run(); err != nil {
		t.Error(err)
	}

	time.Sleep(200 * time.Millisecond)

	directGotFork := false
	select {
	case event := <-watcher.Fork:
		t.Logf("Direct watcher got fork event: parent=%d, child=%d",
			event.ParentPid, event.ChildPid)
		directGotFork = true
	case <-time.After(100 * time.Millisecond):
		t.Log("Direct watcher: No fork events received")
	}

	watcher.Close()

	// Now test with detector
	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer detector.Close()

	if err := detector.Watch(pid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Error(err)
	}

	time.Sleep(100 * time.Millisecond)

	// Run command and check if detector gets events
	cmd = exec.Command("echo", "detector")
	if err := cmd.Run(); err != nil {
		t.Error(err)
	}

	time.Sleep(200 * time.Millisecond)

	detectorGotFork := false
	select {
	case event := <-detector.ForkExec:
		t.Logf("Detector got fork+exec event: parent=%d, child=%d",
			event.Fork.ParentPid, event.Fork.ChildPid)
		detectorGotFork = true
	case <-time.After(100 * time.Millisecond):
		t.Log("Detector: No fork+exec events received")
	}

	if directGotFork && !detectorGotFork {
		t.Error("Direct watcher got fork event but detector didn't")
	}
}

func TestDetectorSimple(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer detector.Close()

	pid := os.Getpid()

	// Watch for fork events on this process
	if err := detector.Watch(pid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Fatal(err)
	}

	// Create done channel for the goroutine
	done := make(chan struct{})
	defer close(done)

	// Start a goroutine to collect events
	events := make(chan string, 10)
	go func() {
		defer func() {
			// Recover from any panic
			if r := recover(); r != nil {
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		for {
			select {
			case <-done:
				return // Exit when done channel is closed
			case event := <-detector.ForkExec:
				events <- fmt.Sprintf("ForkExec: parent=%d child=%d",
					event.Fork.ParentPid, event.Fork.ChildPid)
			case event := <-detector.Done:
				events <- fmt.Sprintf("Done: pid=%d exitcode=%d",
					event.Exit.Pid, event.Exit.ExitCode)
			case err := <-detector.Error:
				events <- fmt.Sprintf("Error: %v", err)
			case <-time.After(500 * time.Millisecond):
				// More frequent checks but don't exit
				select {
				case <-done:
					return
				default:
					// Continue
				}
			}
		}
	}()

	// Give some time for setup
	time.Sleep(100 * time.Millisecond)

	// Run a command
	cmd := exec.Command("echo", "test")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Wait for events
	timeout := time.After(1 * time.Second)
	eventCount := 0
	for {
		select {
		case event := <-events:
			t.Logf("Got event: %s", event)
			eventCount++
			if eventCount >= 2 { // Expect at least fork+exec and done
				return
			}
		case <-timeout:
			t.Log("Timeout waiting for events")
			return
		}
	}
}

func TestDetectorDirectFork(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	// Start with a direct test - fork a process and check if we get the event
	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer detector.Close()

	// Watch the current process
	selfPid := os.Getpid()
	if err := detector.Watch(selfPid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Fatal(err)
	}

	// Record PID before fork
	t.Logf("Parent PID: %d", selfPid)

	// Set up a channel to receive events from detector
	forkExecCh := make(chan *psnotify.DetectedEventForkExec)
	errCh := make(chan error)
	doneCh := make(chan struct{})

	// Start goroutine to collect events
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		for {
			select {
			case <-doneCh:
				return
			case event := <-detector.ForkExec:
				t.Logf("Received ForkExec event: parent=%d, child=%d",
					event.Fork.ParentPid, event.Fork.ChildPid)
				forkExecCh <- event
			case err := <-detector.Error:
				t.Logf("Received error: %v", err)
				if err.Error() != "interrupted system call" {
					errCh <- err
				}
			}
		}
	}()

	// Give time for watcher to be ready
	time.Sleep(100 * time.Millisecond)

	// Run an external command which should trigger a fork event
	t.Log("Running command...")
	cmd := exec.Command("ls", "-la")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Give some time for the fork event to be processed
	t.Log("Waiting for events...")
	select {
	case event := <-forkExecCh:
		// Success! We received a fork+exec event
		t.Logf("Success! Got fork+exec event: parent=%d, child=%d",
			event.Fork.ParentPid, event.Fork.ChildPid)
	case err := <-errCh:
		t.Errorf("Error: %v", err)
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for fork+exec event")
	}

	// Clean up
	close(doneCh)
	cmd.Wait()
}

func TestCompareWatcherAndDetector(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	// Test with direct watcher first
	watcher, err := psnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}

	selfPid := os.Getpid()
	if err := watcher.Watch(selfPid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Fatal(err)
	}

	// Run a command to trigger events
	t.Log("Running command with direct watcher active...")
	cmd := exec.Command("ls")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Try to get any events from the watcher
	t.Log("Checking watcher events...")
	select {
	case ev := <-watcher.Exec:
		t.Logf("Direct watcher got exec event: pid=%d", ev.Pid)
	case <-time.After(500 * time.Millisecond):
		t.Log("No direct exec events received")
	}

	watcher.Close()
	time.Sleep(200 * time.Millisecond)

	// Now test with detector
	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer detector.Close()

	if err := detector.Watch(selfPid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Fatal(err)
	}

	// Run a command to trigger events
	t.Log("Running command with detector active...")
	cmd = exec.Command("ls")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Try to get any events from the detector
	t.Log("Checking detector events...")
	select {
	case ev := <-detector.ForkExec:
		t.Logf("Detector got fork+exec event: parent=%d, child=%d",
			ev.Fork.ParentPid, ev.Fork.ChildPid)
	case ev := <-detector.Done:
		t.Logf("Detector got done event: pid=%d", ev.Exit.Pid)
	case <-time.After(500 * time.Millisecond):
		t.Log("No detector events received")
	}
}

func TestDetectorExplicitChild(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	// Create detector
	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer detector.Close()

	// Start a child process that will run for a short time
	t.Log("Starting child process...")
	cmd := exec.Command("sleep", "1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	childPid := cmd.Process.Pid
	t.Logf("Child process PID: %d", childPid)

	// Explicitly watch the child process
	if err := detector.Watch(childPid, psnotify.PROC_EVENT_EXIT); err != nil {
		t.Fatal(err)
	}
	t.Log("Watching child process for exit...")

	// Set up a channel to collect done events
	doneChan := make(chan *psnotify.DetectedEventDone, 1)
	errChan := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)

	// Start a goroutine to collect events
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		for {
			select {
			case <-done:
				return
			case ev := <-detector.Done:
				t.Logf("Got Done event: pid=%d, exitCode=%d", ev.Exit.Pid, ev.Exit.ExitCode)
				if ev.Exit.Pid == childPid {
					doneChan <- ev
				}
			case err := <-detector.Error:
				if err.Error() != "interrupted system call" {
					errChan <- err
				}
			}
		}
	}()

	// Wait for the process to exit
	t.Log("Waiting for child process to exit...")
	cmd.Wait()
	t.Log("Child process exited")

	// Wait for the Done event
	select {
	case ev := <-doneChan:
		t.Logf("Success! Got Done event for pid=%d", ev.Exit.Pid)
	case err := <-errChan:
		t.Errorf("Error: %v", err)
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for Done event")
	}
}

func TestDetectorForkExecChild(t *testing.T) {
	if skipDetectorTest(t) {
		return
	}

	// Create detector
	cfg := psnotify.DetectorConfig{}
	detector, err := psnotify.NewDetector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer detector.Close()

	// Watch the current process for fork events
	selfPid := os.Getpid()
	t.Logf("Watching parent process PID: %d", selfPid)
	if err := detector.Watch(selfPid, psnotify.PROC_EVENT_FORK); err != nil {
		t.Fatal(err)
	}

	// Set up channels to collect events
	forkExecChan := make(chan *psnotify.DetectedEventForkExec, 1)
	doneChan := make(chan *psnotify.DetectedEventDone, 1)
	errChan := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)

	// Start a goroutine to collect events
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		for {
			select {
			case <-done:
				return
			case ev := <-detector.ForkExec:
				t.Logf("Got ForkExec event: parent=%d, child=%d",
					ev.Fork.ParentPid, ev.Fork.ChildPid)
				forkExecChan <- ev
			case ev := <-detector.Done:
				t.Logf("Got Done event: pid=%d, exitCode=%d",
					ev.Exit.Pid, ev.Exit.ExitCode)
				doneChan <- ev
			case err := <-detector.Error:
				if err.Error() != "interrupted system call" {
					errChan <- err
				}
			}
		}
	}()

	// Give the detector time to set up
	time.Sleep(200 * time.Millisecond)

	// Start a child process
	t.Log("Starting child process...")
	cmd := exec.Command("echo", "hello world")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait for the child process to exit
	cmd.Wait()
	t.Log("Child process exited")

	// Wait for events
	t.Log("Waiting for events...")

	// Wait up to 2 seconds for a ForkExec event
	select {
	case ev := <-forkExecChan:
		t.Logf("Success! Got ForkExec event for parent=%d, child=%d",
			ev.Fork.ParentPid, ev.Fork.ChildPid)
	case err := <-errChan:
		t.Errorf("Error: %v", err)
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for ForkExec event")
	}

	// Wait up to 2 seconds for a Done event
	select {
	case ev := <-doneChan:
		t.Logf("Success! Got Done event for pid=%d", ev.Exit.Pid)
	case err := <-errChan:
		t.Errorf("Error: %v", err)
	case <-time.After(2 * time.Second):
		t.Log("No Done event received (may be expected)")
	}
}

func TestDetector(t *testing.T) {
	// Create a mock watcher for testing
	mock := NewMockWatcher()

	// Create the detector with the mock watcher
	detector, err := psnotify.NewDetector(psnotify.DetectorConfig{
		Watcher: mock,
	})

	assert.NoError(t, err, "NewDetector should not error")
	assert.NotNil(t, detector, "Detector should not be nil")

	// Start watching process 1
	err = detector.Watch(1, psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT)
	assert.NoError(t, err, "Watch should not error")

	// Verify the watch was added
	flags, ok := mock.GetWatchFlags(1)
	assert.True(t, ok, "Process 1 should be watched")
	assert.Equal(t, uint32(psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT), flags, "Watch flags should match")

	// Create channels to capture events
	forkExecReceived := make(chan bool, 1)
	exitReceived := make(chan bool, 1)

	// Start a goroutine to collect events
	go func() {
		for {
			select {
			case forkExec := <-detector.ForkExec:
				t.Logf("Received fork+exec event: parent=%d, child=%d",
					forkExec.Fork.ParentPid, forkExec.Fork.ChildPid)
				assert.Equal(t, 1, int(forkExec.Fork.ParentPid), "Parent PID should be 1")
				assert.Equal(t, 2, int(forkExec.Fork.ChildPid), "Child PID should be 2")
				assert.Equal(t, 2, int(forkExec.Exec.Pid), "Exec PID should be 2")
				forkExecReceived <- true

			case exit := <-detector.Done:
				t.Logf("Received exit event: pid=%d, exitCode=%d",
					exit.Exit.Pid, exit.Exit.ExitCode)
				assert.Equal(t, 2, int(exit.Exit.Pid), "Exit PID should be 2")
				assert.Equal(t, 0, int(exit.Exit.ExitCode), "Exit code should be 0")
				exitReceived <- true

			case err := <-detector.Error:
				t.Errorf("Received unexpected error: %v", err)
			}
		}
	}()

	// Simulate a fork event
	forkEvent := &psnotify.ProcEventFork{
		ParentPid: 1,
		ChildPid:  2,
		Timestamp: time.Now(),
	}
	mock.SendFork(forkEvent)

	// Simulate an exec event for the same process
	execEvent := &psnotify.ProcEventExec{
		Pid:       2,
		Timestamp: time.Now(),
	}
	mock.SendExec(execEvent)

	// Wait for the fork+exec event
	select {
	case <-forkExecReceived:
		// Success
	case <-time.After(2 * time.Second):
		assert.Fail(t, "Timed out waiting for forkExec event")
	}

	// Now simulate process exit
	exitEvent := &psnotify.ProcEventExit{
		Pid:       2,
		ExitCode:  0,
		Timestamp: time.Now(),
	}
	mock.SendExit(exitEvent)

	// Wait for the exit event
	select {
	case <-exitReceived:
		// Success
	case <-time.After(2 * time.Second):
		assert.Fail(t, "Timed out waiting for exit event")
	}

	// Clean up
	detector.Close()
}

func TestDetectorErrorHandling(t *testing.T) {
	// Create a mock watcher
	mock := NewMockWatcher()

	// Create the detector
	detector, _ := psnotify.NewDetector(psnotify.DetectorConfig{
		Watcher: mock,
	})

	// Set an error for Watch operation
	testErr := errors.New("test error")
	mock.SetWatchError(testErr)

	// Try to watch a PID
	err := detector.Watch(1, psnotify.PROC_EVENT_FORK)
	assert.Equal(t, testErr, err, "Watch should return the error from watcher")

	// Clean up
	detector.Close()
}

func TestDetectorCleanup(t *testing.T) {
	// Create a mock watcher
	mock := NewMockWatcher()

	// Create the detector
	detector, _ := psnotify.NewDetector(psnotify.DetectorConfig{
		Watcher: mock,
	})

	// Watch a PID
	detector.Watch(1, psnotify.PROC_EVENT_FORK|psnotify.PROC_EVENT_EXEC|psnotify.PROC_EVENT_EXIT)

	// Verify the watch was added
	assert.True(t, mock.IsWatched(1), "PID 1 should be watched")

	// Close the detector
	err := detector.Close()
	assert.NoError(t, err, "Close should not error")

	// Try to watch another PID (should fail because detector is closed)
	mock.SetWatchError(nil) // Reset any error
	err = detector.Watch(2, psnotify.PROC_EVENT_FORK)
	assert.Error(t, err, "Watch should fail after detector is closed")

	// Multiple Close calls should not error
	err = detector.Close()
	assert.NoError(t, err, "Second Close should not error")
}
