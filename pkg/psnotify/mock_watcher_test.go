//go:build darwin || freebsd || netbsd || openbsd || linux

package psnotify_test

import (
	"errors"
	"sync"

	"github.com/walteh/runm/pkg/psnotify"
)

// Error constants for the mock watcher
var (
	errMockWatcherClosed = errors.New("mock watcher is closed")
)

// MockWatcher is a mock implementation of the EventWatcher interface for testing
type MockWatcher struct {
	Fork  chan *psnotify.ProcEventFork
	Exec  chan *psnotify.ProcEventExec
	Exit  chan *psnotify.ProcEventExit
	Error chan error

	watchedPids      map[int]uint32
	mu               sync.Mutex
	closed           bool
	watchError       error
	removeWatchError error
	closeError       error
}

// NewMockWatcher creates a new MockWatcher for testing
func NewMockWatcher() *MockWatcher {
	return &MockWatcher{
		Fork:        make(chan *psnotify.ProcEventFork, 10),
		Exec:        make(chan *psnotify.ProcEventExec, 10),
		Exit:        make(chan *psnotify.ProcEventExit, 10),
		Error:       make(chan error, 10),
		watchedPids: make(map[int]uint32),
	}
}

// Watch implements EventWatcher.Watch
func (m *MockWatcher) Watch(pid int, flags uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errMockWatcherClosed
	}

	if m.watchError != nil {
		return m.watchError
	}

	m.watchedPids[pid] = flags
	return nil
}

// RemoveWatch implements EventWatcher.RemoveWatch
func (m *MockWatcher) RemoveWatch(pid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errMockWatcherClosed
	}

	if m.removeWatchError != nil {
		return m.removeWatchError
	}

	delete(m.watchedPids, pid)
	return nil
}

// Close implements EventWatcher.Close
func (m *MockWatcher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true

	if m.closeError != nil {
		return m.closeError
	}

	close(m.Fork)
	close(m.Exec)
	close(m.Exit)
	close(m.Error)
	return nil
}

// GetForkChannel implements EventWatcher.GetForkChannel
func (m *MockWatcher) GetForkChannel() <-chan *psnotify.ProcEventFork {
	return m.Fork
}

// GetExecChannel implements EventWatcher.GetExecChannel
func (m *MockWatcher) GetExecChannel() <-chan *psnotify.ProcEventExec {
	return m.Exec
}

// GetExitChannel implements EventWatcher.GetExitChannel
func (m *MockWatcher) GetExitChannel() <-chan *psnotify.ProcEventExit {
	return m.Exit
}

// GetErrorChannel implements EventWatcher.GetErrorChannel
func (m *MockWatcher) GetErrorChannel() <-chan error {
	return m.Error
}

// SetWatchError sets an error to be returned by Watch
func (m *MockWatcher) SetWatchError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchError = err
}

// SetRemoveWatchError sets an error to be returned by RemoveWatch
func (m *MockWatcher) SetRemoveWatchError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeWatchError = err
}

// SetCloseError sets an error to be returned by Close
func (m *MockWatcher) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeError = err
}

// IsWatched checks if a PID is being watched
func (m *MockWatcher) IsWatched(pid int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watchedPids[pid]
	return ok
}

// GetWatchFlags returns the flags for a watched PID
func (m *MockWatcher) GetWatchFlags(pid int) (uint32, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	flags, ok := m.watchedPids[pid]
	return flags, ok
}

// SendFork sends a fork event to the mock watcher
func (m *MockWatcher) SendFork(event *psnotify.ProcEventFork) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		m.Fork <- event
	}
}

// SendExec sends an exec event to the mock watcher
func (m *MockWatcher) SendExec(event *psnotify.ProcEventExec) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		m.Exec <- event
	}
}

// SendExit sends an exit event to the mock watcher
func (m *MockWatcher) SendExit(event *psnotify.ProcEventExit) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		m.Exit <- event
	}
}

// SendError sends an error to the mock watcher
func (m *MockWatcher) SendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		m.Error <- err
	}
}
