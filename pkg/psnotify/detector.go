// Copyright (c) 2012 VMware, Inc.

//go:build darwin || freebsd || netbsd || openbsd || linux
// +build darwin freebsd netbsd openbsd linux

package psnotify

import (
	"fmt"
	"sync"
	"time"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/pkg/syncmap"
)

// AddChild adds a child process to the parent's children list
func (p *PidInfo) AddChild(child *PidInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ensure children slice is initialized
	if p.Children == nil {
		p.Children = make([]*PidInfo, 0, 4)
	}

	// Add the child
	p.Children = append(p.Children, child)
}

// RemoveChild removes a child process from the parent's children list
func (p *PidInfo) RemoveChild(childPid int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Children == nil {
		return
	}

	// Find and remove the child
	for i, child := range p.Children {
		if child.Pid == childPid {
			// Remove by swapping with the last element and trimming
			p.Children[i] = p.Children[len(p.Children)-1]
			p.Children = p.Children[:len(p.Children)-1]
			return
		}
	}
}

// EventWatcher is an interface that abstracts the psnotify Watcher
// Implements the ProcessWatcher interface
type EventWatcher interface {
	ProcessWatcher
}

// Detector wraps a Watcher and provides enhanced events with additional process information
// Implements the ProcessDetector interface
type Detector struct {
	watcher         ProcessWatcher
	Error           chan error
	ForkExec        chan *DetectedEventForkExec
	Done            chan *DetectedEventDone
	done            chan bool
	isClosed        bool
	pidInfoMap      *syncmap.Map[int, *PidInfo]
	exitChannelFunc func() chan gorunc.Exit // Optional function to get exit channel
	mu              sync.RWMutex
}

// GetForkExecChannel returns the channel for combined fork+exec events
func (d *Detector) GetForkExecChannel() <-chan *DetectedEventForkExec {
	return d.ForkExec
}

// GetDoneChannel returns the channel for process done events
func (d *Detector) GetDoneChannel() <-chan *DetectedEventDone {
	return d.Done
}

// GetErrorChannel returns the channel for error events
func (d *Detector) GetErrorChannel() <-chan error {
	return d.Error
}

// watcherAdapter adapts the original Watcher to implement ProcessWatcher
type watcherAdapter struct {
	*Watcher
}

// GetForkChannel returns the fork event channel
func (w *watcherAdapter) GetForkChannel() <-chan *ProcEventFork {
	return w.Fork
}

// GetExecChannel returns the exec event channel
func (w *watcherAdapter) GetExecChannel() <-chan *ProcEventExec {
	return w.Exec
}

// GetExitChannel returns the exit event channel
func (w *watcherAdapter) GetExitChannel() <-chan *ProcEventExit {
	return w.Exit
}

// GetErrorChannel returns the error channel
func (w *watcherAdapter) GetErrorChannel() <-chan error {
	return w.Error
}

// NewDetector creates a new detector that wraps a watcher
func NewDetector(cfg DetectorConfig) (*Detector, error) {
	var watcher ProcessWatcher
	var err error

	if cfg.Watcher != nil {
		// Use the provided watcher (for testing)
		watcher = cfg.Watcher
	} else {
		// Create a real watcher and adapt it
		realWatcher, err := NewWatcher()
		if err != nil {
			return nil, err
		}
		watcher = &watcherAdapter{realWatcher}
	}

	detector := &Detector{
		watcher:         watcher,
		Error:           make(chan error, 10),
		ForkExec:        make(chan *DetectedEventForkExec, 10),
		Done:            make(chan *DetectedEventDone, 10),
		done:            make(chan bool, 1),
		pidInfoMap:      syncmap.NewMap[int, *PidInfo](),
		exitChannelFunc: cfg.ExitChannelFunc,
	}

	go detector.eventLoop()
	return detector, err
}

// Watch adds a pid to the watched process set
func (d *Detector) Watch(pid int, flags uint32) error {
	d.mu.RLock()
	closed := d.isClosed
	d.mu.RUnlock()

	if closed {
		return fmt.Errorf("detector is closed")
	}

	return d.watcher.Watch(pid, flags)
}

// RemoveWatch removes a pid from the watched process set
func (d *Detector) RemoveWatch(pid int) error {
	d.mu.RLock()
	closed := d.isClosed
	d.mu.RUnlock()

	if closed {
		return fmt.Errorf("detector is closed")
	}

	return d.watcher.RemoveWatch(pid)
}

// Close closes the detector and all its channels
func (d *Detector) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.isClosed {
		return nil
	}
	d.isClosed = true

	d.watcher.Close()
	d.done <- true
	return nil
}

// finish closes all channels
func (d *Detector) finish() {
	close(d.Error)
	close(d.ForkExec)
	close(d.Done)
}

// isDone checks if the detector should stop
func (d *Detector) isDone() bool {
	select {
	case <-d.done:
		d.finish()
		return true
	default:
		return false
	}
}

// eventLoop processes events from the watcher and enhances them with additional information
func (d *Detector) eventLoop() {
	pendingExecs := make(map[int]*ProcEventFork) // Store fork events waiting for exec
	watchingParent := make(map[int]bool)         // Track which parent PIDs we're watching

	defer func() {
		// Recover from any panic to prevent crashing
		if r := recover(); r != nil {
			// Log the panic but don't crash
			fmt.Printf("Detector panic recovered: %v\n", r)
		}
	}()

	fmt.Printf("Detector eventLoop started\n")

	// Get the watcher's channels through the interface
	forkChan := d.watcher.GetForkChannel()
	execChan := d.watcher.GetExecChannel()
	exitChan := d.watcher.GetExitChannel()
	errorChan := d.watcher.GetErrorChannel()

	for {
		if d.isDone() {
			fmt.Printf("Detector eventLoop done\n")
			return
		}

		select {
		case <-d.done:
			fmt.Printf("Detector received done signal\n")
			d.finish()
			return

		case forkEv := <-forkChan:
			parent := int(forkEv.ParentPid)
			child := int(forkEv.ChildPid)

			fmt.Printf("Detector received fork event: parent=%d, child=%d\n", parent, child)

			// Track this parent as watched
			watchingParent[parent] = true

			// Special case: on some platforms/implementations child PID might be 0
			if child <= 0 {
				fmt.Printf("Detector received fork with child=0, watching parent=%d\n", parent)
				continue
			}

			// Get or create parent info
			parentInfo, parentExists := d.pidInfoMap.Load(parent)
			if !parentExists {
				parentInfo = d.getPidInfo(parent)
				d.pidInfoMap.Store(parent, parentInfo)
			}

			// Get or create child info
			childInfo, childExists := d.pidInfoMap.Load(child)
			if !childExists {
				childInfo = d.getPidInfo(child)
			}

			// Set up parent-child relationship
			childInfo.Parent = parentInfo
			if parentInfo != nil {
				parentInfo.AddChild(childInfo)
			}

			// Store the updated child info
			d.pidInfoMap.Store(child, childInfo)

			// Store fork event for potential combination with exec
			pendingExecs[child] = forkEv

			// Watch for exec and exit events on the child
			if err := d.watcher.Watch(child, PROC_EVENT_EXEC|PROC_EVENT_EXIT); err != nil {
				fmt.Printf("Failed to watch child pid=%d: %v\n", child, err)
				d.sendError(err)
			}

		case execEv := <-execChan:
			pid := int(execEv.Pid)
			fmt.Printf("Detector received exec event: pid=%d\n", pid)

			// Look for a matching fork event
			if forkEv, found := pendingExecs[pid]; found {
				// We found a matching fork event
				fmt.Printf("Found matching fork event for pid=%d\n", pid)
				delete(pendingExecs, pid)

				// Get updated info for the process
				parentPid := int(forkEv.ParentPid)
				parentInfo, _ := d.pidInfoMap.Load(parentPid)

				childInfo, exists := d.pidInfoMap.Load(pid)
				if !exists {
					// This shouldn't happen if we got the fork event first,
					// but handle it just in case
					childInfo = d.getPidInfo(pid)
					if parentInfo != nil {
						childInfo.Parent = parentInfo
						parentInfo.AddChild(childInfo)
					}
					d.pidInfoMap.Store(pid, childInfo)
				}

				// Send fork+exec event
				d.sendForkExec(&DetectedEventForkExec{
					Timestamp:  execEv.Timestamp,
					Fork:       forkEv,
					Exec:       execEv,
					ParentInfo: parentInfo,
					ChildInfo:  childInfo,
				})
			}

		case exitEv := <-exitChan:
			pid := int(exitEv.Pid)
			fmt.Printf("Detector received exit event: pid=%d, exitCode=%d\n",
				pid, exitEv.ExitCode)

			// Clean up pending exec and watched parent
			delete(pendingExecs, pid)
			delete(watchingParent, pid)

			// Get process info
			info, exists := d.pidInfoMap.Load(pid)
			if !exists {
				// This could happen if we start watching a process mid-lifecycle
				info = d.getPidInfo(pid)
			}

			// Remove this child from its parent's children list if parent exists
			if info != nil && info.Parent != nil {
				info.Parent.RemoveChild(pid)
			}

			// Wait for file descriptors to close in a separate goroutine
			go func(pid int, exitEv *ProcEventExit, info *PidInfo) {
				fmt.Printf("Detector waiting for pid=%d file descriptors to close\n", pid)
				err := waitByPidfd(pid)
				if err != nil {
					fmt.Printf("Detector error waiting for pid=%d: %v\n", pid, err)
				}

				// Send done event after FDs are closed
				fmt.Printf("Detector sending done event for pid=%d\n", pid)
				d.sendDone(&DetectedEventDone{
					Timestamp: time.Now(),
					Exit:      exitEv,
					Info:      info,
				})

				// Send to gorunc exit channel if available
				if d.exitChannelFunc != nil {
					if exitChan := d.exitChannelFunc(); exitChan != nil {
						exitChan <- gorunc.Exit{
							Pid:       pid,
							Status:    exitEv.ExitCode,
							Timestamp: exitEv.Timestamp,
						}
					}
				}
			}(pid, exitEv, info)

			// Clean up immediately
			d.watcher.RemoveWatch(pid)
			d.pidInfoMap.Delete(pid)

		case err := <-errorChan:
			fmt.Printf("Detector received error: %v\n", err)
			d.sendError(err)
		}
	}
}

// getPidInfo retrieves enhanced information about a process
func (d *Detector) getPidInfo(pid int) *PidInfo {
	info := &PidInfo{
		Pid:      pid,
		Children: make([]*PidInfo, 0, 4),
	}
	info.Cgroup, _ = getCgroupFromProc(pid)
	info.Argv = getArgvFromProc(pid)
	info.Argc, _ = getExePath(pid)
	return info
}

// sendError safely sends an error without panicking
func (d *Detector) sendError(err error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.isClosed {
		return
	}

	select {
	case d.Error <- err:
	default:
		// Channel is full, drop the error
	}
}

// sendForkExec safely sends a fork+exec event without panicking
func (d *Detector) sendForkExec(event *DetectedEventForkExec) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.isClosed {
		return
	}

	select {
	case d.ForkExec <- event:
	default:
		// Channel is full, drop the event
	}
}

// sendDone safely sends a done event without panicking
func (d *Detector) sendDone(event *DetectedEventDone) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.isClosed {
		return
	}

	select {
	case d.Done <- event:
	default:
		// Channel is full, drop the event
	}
}
