//go:build darwin || freebsd || netbsd || openbsd
// +build darwin freebsd netbsd openbsd

/*
   Copyright The runm Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package kqueue

import (
	"io"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/containerd/console"
	"gitlab.com/tozd/go/errors"
)

const (
	maxEvents = 128
)

// Kqueuer manages multiple kqueue consoles using edge-triggered kqueue api so we
// dont have to deal with repeated wake-up of KQUEUE.
// For more details, see:
// - https://github.com/systemd/systemd/pull/4262
// - https://github.com/moby/moby/issues/27202
//
// Example usage of Kqueuer and KqueueConsole can be as follow:
//
//	Kqueuer, _ := NewKqueuer()
//	KqueueConsole, _ := Kqueuer.Add(console)
//	go Kqueuer.Wait()
//	var (
//		b  bytes.Buffer
//		wg sync.WaitGroup
//	)
//	wg.Add(1)
//	go func() {
//		io.Copy(&b, KqueueConsole)
//		wg.Done()
//	}()
//	// perform I/O on the console
//	KqueueConsole.Shutdown(Kqueuer.CloseConsole)
//	wg.Wait()
//	KqueueConsole.Close()
type Kqueuer struct {
	kq        int
	mu        sync.Mutex
	fdMapping map[int]*KqueueConsole
	closeOnce sync.Once
}

// NewKqueuer returns an instance of Kqueuer with a valid kqueue fd.
func NewKqueuer() (*Kqueuer, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	return &Kqueuer{
		kq:        kq,
		fdMapping: make(map[int]*KqueueConsole),
	}, nil
}

// Add creates an epoll console based on the provided console. The console will
// be registered with kqueue (i.e. using edge-triggered notification) and its
// file descriptor will be set to non-blocking mode. After this, user should use
// the return console to perform I/O.
func (e *Kqueuer) Add(console console.Console) (*KqueueConsole, error) {
	sysfd := int(console.Fd())

	slog.Info("adding console to kqueue", "sysfd", sysfd)
	// Set sysfd to non-blocking mode
	if err := unix.SetNonblock(sysfd, true); err != nil {
		return nil, errors.Errorf("setting non-blocking mode: %w", err)
	}

	events := []unix.Kevent_t{
		{Ident: uint64(sysfd), Filter: unix.EVFILT_READ, Flags: unix.EV_ADD | unix.EV_CLEAR},
		{Ident: uint64(sysfd), Filter: unix.EVFILT_WRITE, Flags: unix.EV_ADD | unix.EV_CLEAR},
	}

	_, err := unix.Kevent(e.kq, events, nil, nil)
	if err != nil {
		return nil, errors.Errorf("adding console to kqueue: %w", err)
	}

	ef := &KqueueConsole{
		Console: console,
		sysfd:   sysfd,
		readc:   sync.NewCond(&sync.Mutex{}),
		writec:  sync.NewCond(&sync.Mutex{}),
	}
	e.mu.Lock()
	e.fdMapping[sysfd] = ef
	e.mu.Unlock()
	return ef, nil
}

// Wait starts the loop to wait for its consoles' notifications and signal
// appropriate console that it can perform I/O.
func (e *Kqueuer) Wait() error {
	events := make([]unix.Kevent_t, maxEvents)
	for {
		n, err := unix.Kevent(e.kq, nil, events, nil)
		if err != nil {
			// EINTR: The call was interrupted by a signal handler before either
			// any of the requested events occurred or the timeout expired
			if err == unix.EINTR {
				continue
			}
			return errors.Errorf("waiting for kqueue events: %w", err)
		}
		for i := 0; i < n; i++ {
			ev := &events[i]
			sysfd := int(ev.Ident)

			if epfile := e.getConsole(sysfd); epfile != nil {
				if ev.Filter == unix.EVFILT_READ {
					epfile.signalRead()
				}
				if ev.Filter == unix.EVFILT_WRITE {
					epfile.signalWrite()
				}
				if ev.Flags&unix.EV_EOF != 0 {
					epfile.signalRead()
					epfile.signalWrite()
				}
			}
		}
	}
}

// CloseConsole unregisters the console's file descriptor from kqueue interface
func (e *Kqueuer) CloseConsole(fd int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.fdMapping, fd)
	// Kqueue automatically removes FDs when they are closed
	return nil
}

func (e *Kqueuer) getConsole(sysfd int) *KqueueConsole {
	e.mu.Lock()
	f := e.fdMapping[sysfd]
	e.mu.Unlock()
	return f
}

// Close closes the kqueue fd
func (e *Kqueuer) Close() error {
	closeErr := os.ErrClosed // default to "file already closed"
	e.closeOnce.Do(func() {
		closeErr = unix.Close(e.kq)
	})
	return closeErr
}

// KqueueConsole acts like a console but registers its file descriptor with an
// kqueue fd and uses kqueue API to perform I/O.
type KqueueConsole struct {
	console.Console
	readc  *sync.Cond
	writec *sync.Cond
	sysfd  int
	closed bool
}

// Read reads up to len(p) bytes into p. It returns the number of bytes read
// (0 <= n <= len(p)) and any error encountered.
//
// If the console's read returns EAGAIN or EIO, we assume that it's a
// temporary error because the other side went away and wait for the signal
// generated by kqueue event to continue.
func (ec *KqueueConsole) Read(p []byte) (n int, err error) {
	var read int
	ec.readc.L.Lock()
	defer ec.readc.L.Unlock()
	for {
		read, err = ec.Console.Read(p[n:])
		n += read
		if err != nil {
			var hangup bool
			if perr, ok := err.(*os.PathError); ok {
				hangup = (perr.Err == unix.EAGAIN || perr.Err == unix.EIO)
			} else {
				hangup = (err == unix.EAGAIN || err == unix.EIO)
			}
			// if the other end disappear, assume this is temporary and wait for the
			// signal to continue again. Unless we didnt read anything and the
			// console is already marked as closed then we should exit
			if hangup && !(n == 0 && len(p) > 0 && ec.closed) {
				ec.readc.Wait()
				continue
			}
		}
		break
	}
	// if we didnt read anything then return io.EOF to end gracefully
	if n == 0 && len(p) > 0 && err == nil {
		err = io.EOF
	}
	// signal for others that we finished the read
	ec.readc.Signal()
	return n, err
}

// Writes len(p) bytes from p to the console. It returns the number of bytes
// written from p (0 <= n <= len(p)) and any error encountered that caused
// the write to stop early.
//
// If writes to the console returns EAGAIN or EIO, we assume that it's a
// temporary error because the other side went away and wait for the signal
// generated by kqueue event to continue.
func (ec *KqueueConsole) Write(p []byte) (n int, err error) {
	var written int
	ec.writec.L.Lock()
	defer ec.writec.L.Unlock()
	for {
		written, err = ec.Console.Write(p[n:])
		n += written
		if err != nil {
			var hangup bool
			if perr, ok := err.(*os.PathError); ok {
				hangup = (perr.Err == unix.EAGAIN || perr.Err == unix.EIO)
			} else {
				hangup = (err == unix.EAGAIN || err == unix.EIO)
			}
			// if the other end disappears, assume this is temporary and wait for the
			// signal to continue again.
			if hangup {
				ec.writec.Wait()
				continue
			}
		}
		// unrecoverable error, break the loop and return the error
		break
	}
	if n < len(p) && err == nil {
		err = io.ErrShortWrite
	}
	// signal for others that we finished the write
	ec.writec.Signal()
	return n, err
}

// Shutdown closes the file descriptor and signals call waiters for this fd.
// It accepts a callback which will be called with the console's fd. The
// callback typically will be used to do further cleanup such as unregister the
// console's fd from the kqueue interface.
// User should call Shutdown and wait for all I/O operation to be finished
// before closing the console.
func (ec *KqueueConsole) Shutdown(close func(int) error) error {
	ec.readc.L.Lock()
	defer ec.readc.L.Unlock()
	ec.writec.L.Lock()
	defer ec.writec.L.Unlock()

	ec.readc.Broadcast()
	ec.writec.Broadcast()
	ec.closed = true
	return close(ec.sysfd)
}

// signalRead signals that the console is readable.
func (ec *KqueueConsole) signalRead() {
	ec.readc.L.Lock()
	ec.readc.Signal()
	ec.readc.L.Unlock()
}

// signalWrite signals that the console is writable.
func (ec *KqueueConsole) signalWrite() {
	ec.writec.L.Lock()
	ec.writec.Signal()
	ec.writec.L.Unlock()
}
