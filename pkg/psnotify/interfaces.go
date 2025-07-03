package psnotify

import (
	"sync"
	"syscall"
	"time"

	gorunc "github.com/containerd/go-runc"
)

// ProcessWatcher defines the interface for a process event watcher
//
//go:mock
type ProcessWatcher interface {
	// Watch adds a process ID to the watched process set with specified event flags
	Watch(pid int, flags uint32) error

	// RemoveWatch removes a process ID from the watched process set
	RemoveWatch(pid int) error

	// Close shuts down the watcher and releases resources
	Close() error

	// GetForkChannel returns the channel for fork events
	GetForkChannel() <-chan *ProcEventFork

	// GetExecChannel returns the channel for exec events
	GetExecChannel() <-chan *ProcEventExec

	// GetExitChannel returns the channel for exit events
	GetExitChannel() <-chan *ProcEventExit

	// GetErrorChannel returns the channel for error events
	GetErrorChannel() <-chan error
}

// ProcessDetector defines the interface for an enhanced process event detector
//
//go:mock
type ProcessDetector interface {
	// Watch adds a process ID to the watched process set with specified event flags
	Watch(pid int, flags uint32) error

	// RemoveWatch removes a process ID from the watched process set
	RemoveWatch(pid int) error

	// Close shuts down the detector and releases resources
	Close() error

	// GetForkExecChannel returns the channel for combined fork+exec events
	GetForkExecChannel() <-chan *DetectedEventForkExec

	// GetDoneChannel returns the channel for process done events
	GetDoneChannel() <-chan *DetectedEventDone

	// GetErrorChannel returns the channel for error events
	GetErrorChannel() <-chan error
}

// PidInfoProvider defines the interface for a process info provider
type PidInfoProvider interface {
	// GetPidInfo retrieves enhanced information about a process
	GetPidInfo(pid int) *PidInfo
}

type ProcEventFork struct {
	ParentPid int       // Pid of the process that called fork()
	ChildPid  int       // Child process pid created by fork()
	Timestamp time.Time // Timestamp of the fork event
}

type ProcEventExec struct {
	Pid       int       // Pid of the process that called exec()
	Timestamp time.Time // Timestamp of the exec event
}

type ProcEventExit struct {
	Pid        int            // Pid of the process that called exit()
	ExitCode   int            // Exit code of the process that called exit()
	ExitSignal syscall.Signal // Exit signal of the process that called exit()
	Timestamp  time.Time      // Timestamp of the exit event
}

// DetectedEventForkExec represents a combined fork+exec event with enhanced information
type DetectedEventForkExec struct {
	Timestamp  time.Time // Timestamp of the fork event
	Fork       *ProcEventFork
	Exec       *ProcEventExec
	ParentInfo *PidInfo
	ChildInfo  *PidInfo
}

// DetectedEventDone represents a process completion event with enhanced information
type DetectedEventDone struct {
	Timestamp time.Time // Timestamp of the exit event
	Exit      *ProcEventExit
	Info      *PidInfo
}

// PidInfo contains enhanced information about a process
type PidInfo struct {
	Pid      int        // Process ID
	Cgroup   string     // Cgroup path for the process (Linux-only)
	Argv     []string   // Command line arguments
	Argc     string     // Path to executable
	Parent   *PidInfo   // Parent process information
	Children []*PidInfo // Child processes
	mu       sync.Mutex // Mutex for protecting Children slice
}

// DetectorConfig holds configuration for the detector
type DetectorConfig struct {
	// Optional function to provide exit channel for gorunc integration
	ExitChannelFunc func() chan gorunc.Exit
	// Optional watcher to use (for testing)
	Watcher ProcessWatcher
}
