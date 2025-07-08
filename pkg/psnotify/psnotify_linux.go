// Copyright (c) 2012 VMware, Inc.

// Go interface to the Linux netlink process connector.
// See Documentation/connector/connector.txt in the linux kernel source tree.
package psnotify

import (
	"bytes"
	"encoding/binary"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// internal flags (from <linux/connector.h>)
	_CN_IDX_PROC = 0x1
	_CN_VAL_PROC = 0x1

	// internal flags (from <linux/cn_proc.h>)
	_PROC_CN_MCAST_LISTEN = 1
	_PROC_CN_MCAST_IGNORE = 2

	// Flags (from <linux/cn_proc.h>)
	PROC_EVENT_FORK = 0x00000001 // fork() events
	PROC_EVENT_EXEC = 0x00000002 // exec() events
	PROC_EVENT_EXIT = 0x80000000 // exit() events

	// Watch for all process events
	PROC_EVENT_ALL = PROC_EVENT_FORK | PROC_EVENT_EXEC | PROC_EVENT_EXIT
)

var (
	byteOrder = GetEndian()
)

func GetEndian() binary.ByteOrder {
	var i int32 = 0x1
	v := (*[4]byte)(unsafe.Pointer(&i))
	if v[0] == 0 {
		return binary.BigEndian
	} else {
		return binary.LittleEndian
	}
}

// linux/connector.h: struct cb_id
type cbId struct {
	Idx uint32
	Val uint32
}

// linux/connector.h: struct cb_msg
type cnMsg struct {
	Id    cbId
	Seq   uint32
	Ack   uint32
	Len   uint16
	Flags uint16
}

// linux/cn_proc.h: struct proc_event.{what,cpu,timestamp_ns}
type procEventHeader struct {
	What      uint32
	Cpu       uint32
	Timestamp uint64
}

// linux/cn_proc.h: struct proc_event.fork
type forkProcEvent struct {
	ParentPid  uint32
	ParentTgid uint32
	ChildPid   uint32
	ChildTgid  uint32
}

// linux/cn_proc.h: struct proc_event.exec
type execProcEvent struct {
	ProcessPid  uint32
	ProcessTgid uint32
}

// linux/cn_proc.h: struct proc_event.exit
type exitProcEvent struct {
	ProcessPid  uint32
	ProcessTgid uint32
	ExitCode    uint32
	ExitSignal  uint32
}

// standard netlink header + connector header
type netlinkProcMessage struct {
	Header syscall.NlMsghdr
	Data   cnMsg
}

type netlinkListener struct {
	addr *syscall.SockaddrNetlink // Netlink socket address
	sock int                      // The syscall.Socket() file descriptor
	seq  uint32                   // struct cn_msg.seq
}

// Initialize linux implementation of the eventListener interface
func createListener() (eventListener, error) {
	listener := &netlinkListener{}
	err := listener.bind()
	return listener, err
}

// noop on linux
func (w *Watcher) unregister(pid int) error {
	return nil
}

// noop on linux
func (w *Watcher) register(pid int, flags uint32) error {
	return nil
}

// Read events from the netlink socket
func (w *Watcher) readEvents() {
	buf := make([]byte, syscall.Getpagesize())

	listener, _ := w.listener.(*netlinkListener)

	for {
		if w.isDone() {
			return
		}

		nr, _, err := syscall.Recvfrom(listener.sock, buf, 0)

		if err != nil {
			w.Error <- err
			continue
		}
		if nr < syscall.NLMSG_HDRLEN {
			w.Error <- syscall.EINVAL
			continue
		}

		msgs, _ := syscall.ParseNetlinkMessage(buf[:nr])

		for _, m := range msgs {
			if m.Header.Type == syscall.NLMSG_DONE {
				w.handleEvent(m.Data)
			}
		}
	}
}

// Internal helper to check if pid && event is being watched
func (w *Watcher) isWatching(pid int, event uint32) bool {
	if watch, ok := w.watches[pid]; ok {
		return (watch.flags & event) == event
	}
	return false
}

// // Debug helper to dump current watch state
// func (w *Watcher) debugWatches() {
// 	ctx := context.Background()
// 	watchList := make([]string, 0, len(w.watches))
// 	for pid, watch := range w.watches {
// 		flags := []string{}
// 		if watch.flags&PROC_EVENT_FORK != 0 {
// 			flags = append(flags, "FORK")
// 		}
// 		if watch.flags&PROC_EVENT_EXEC != 0 {
// 			flags = append(flags, "EXEC")
// 		}
// 		if watch.flags&PROC_EVENT_EXIT != 0 {
// 			flags = append(flags, "EXIT")
// 		}
// 		watchList = append(watchList, fmt.Sprintf("%d:[%s]", pid, strings.Join(flags, ",")))
// 	}
// 	slog.DebugContext(ctx, "PSNOTIFY_LINUX:CURRENT_WATCHES", "total", len(w.watches), "watches", strings.Join(watchList, " "))
// }

// Dispatch events from the netlink socket to the Event channels.
// Unlike bsd kqueue, netlink receives events for all pids,
// so we apply filtering based on the watch table via isWatching()
func (w *Watcher) handleEvent(data []byte) {
	now := time.Now()
	buf := bytes.NewBuffer(data)
	msg := &cnMsg{}
	hdr := &procEventHeader{}

	binary.Read(buf, byteOrder, msg)
	binary.Read(buf, byteOrder, hdr)

	switch hdr.What {
	case PROC_EVENT_FORK:
		event := &forkProcEvent{}
		binary.Read(buf, byteOrder, event)
		ppid := int(event.ParentTgid)
		pid := int(event.ChildTgid)

		// Debug: log all fork events we receive for processes we might care about
		// if ppid > 1 && pid > 1 { // Skip kernel threads
		// 	isWatchingExec := w.isWatching(ppid, PROC_EVENT_EXEC)
		// 	isWatchingFork := w.isWatching(ppid, PROC_EVENT_FORK)

		// 	// Log fork events for processes we're tracking or that look like container processes
		// 	if isWatchingFork || isWatchingExec || (ppid > 80 && ppid < 200) {
		// 		ctx := context.Background()
		// 		if isWatchingFork {
		// 			slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY_LINUX:FORK[%d->%d]", ppid, pid),
		// 				"parent_pid", ppid, "child_pid", pid, "watching_fork", true, "will_send_event", true)
		// 		} else if isWatchingExec {
		// 			slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY_LINUX:FORK[%d->%d]", ppid, pid),
		// 				"parent_pid", ppid, "child_pid", pid, "watching_exec", true, "watching_fork", false, "will_send_event", false)
		// 		} else {
		// 			slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY_LINUX:FORK[%d->%d]", ppid, pid),
		// 				"parent_pid", ppid, "child_pid", pid, "watching", false, "will_send_event", false)
		// 			// For problematic forks, dump the current watch state
		// 			if ppid > 80 && ppid < 200 {
		// 				w.debugWatches()
		// 			}
		// 		}
		// 	}
		// }

		if w.isWatching(ppid, PROC_EVENT_EXEC) {
			// follow forks
			watch, _ := w.watches[ppid]
			w.Watch(pid, watch.flags)
		}

		if w.isWatching(ppid, PROC_EVENT_FORK) {
			w.Fork <- &ProcEventFork{ParentPid: ppid, ChildPid: pid, Timestamp: now}
		}
	case PROC_EVENT_EXEC:
		event := &execProcEvent{}
		binary.Read(buf, byteOrder, event)
		pid := int(event.ProcessTgid)

		// Debug: log exec events for processes we're watching
		// if w.isWatching(pid, PROC_EVENT_EXEC) || w.isWatching(pid, PROC_EVENT_FORK) || w.isWatching(pid, PROC_EVENT_EXIT) {
		// 	ctx := context.Background()
		// 	slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY_LINUX:EXEC[%d]", pid),
		// 		"pid", pid, "watching_exec", w.isWatching(pid, PROC_EVENT_EXEC),
		// 		"watching_fork", w.isWatching(pid, PROC_EVENT_FORK),
		// 		"will_send_event", w.isWatching(pid, PROC_EVENT_EXEC))
		// }

		if w.isWatching(pid, PROC_EVENT_EXEC) {
			w.Exec <- &ProcEventExec{Pid: pid, Timestamp: now}
		}
	case PROC_EVENT_EXIT:
		event := &exitProcEvent{}
		binary.Read(buf, byteOrder, event)
		pid := int(event.ProcessTgid)
		tid := int(event.ProcessPid)

		// Debug: Check for additional info that might indicate reaping
		// if pid > 80 && pid < 200 {
		// 	ctx := context.Background()
		// 	ws := unix.WaitStatus(event.ExitCode)

		// 	// Log detailed exit event info
		// 	slog.DebugContext(ctx, fmt.Sprintf("PSNOTIFY_LINUX:RAW_EXIT[%d]", pid),
		// 		"pid", pid, "tid", tid,
		// 		"raw_exit_code", event.ExitCode, "raw_exit_signal", event.ExitSignal,
		// 		"parsed_exit_code", ws.ExitStatus(), "parsed_signal", ws.Signal(),
		// 		"header_what", hdr.What, "header_cpu", hdr.Cpu, "header_timestamp", hdr.Timestamp,
		// 		"msg_seq", msg.Seq, "msg_ack", msg.Ack, "msg_len", msg.Len, "msg_flags", msg.Flags,
		// 		"is_watching", w.isWatching(pid, PROC_EVENT_EXIT),
		// 		"pid_vs_tid_same", pid == tid,
		// 		"ws_exited", ws.Exited(), "ws_signaled", ws.Signaled(), "ws_stopped", ws.Stopped())
		// }

		if w.isWatching(pid, PROC_EVENT_EXIT) {
			if pid == tid {
				w.RemoveWatch(pid)

				ws := unix.WaitStatus(event.ExitCode)

				w.Exit <- &ProcEventExit{Pid: int(event.ProcessTgid), ExitCode: ws.ExitStatus(), ExitSignal: ws.Signal(), RawStatus: ws, Timestamp: now}
			}
		}
	}
}

const noSignal = ^uint32(0) // 0xFFFFFFFF

func getSignal(exitSignal uint32) syscall.Signal {
	if exitSignal == noSignal {
		return 0
	}
	return syscall.Signal(exitSignal)
}

// Bind our netlink socket and
// send a listen control message to the connector driver.
func (listener *netlinkListener) bind() error {
	sock, err := syscall.Socket(
		syscall.AF_NETLINK,
		syscall.SOCK_DGRAM,
		syscall.NETLINK_CONNECTOR)

	if err != nil {
		return err
	}

	listener.sock = sock
	listener.addr = &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: _CN_IDX_PROC,
	}

	err = syscall.Bind(listener.sock, listener.addr)

	if err != nil {
		return err
	}

	return listener.send(_PROC_CN_MCAST_LISTEN)
}

// Send an ignore control message to the connector driver
// and close our netlink socket.
func (listener *netlinkListener) close() error {
	err := listener.send(_PROC_CN_MCAST_IGNORE)
	syscall.Close(listener.sock)
	return err
}

// Generic method for sending control messages to the connector
// driver; where op is one of PROC_CN_MCAST_{LISTEN,IGNORE}
func (listener *netlinkListener) send(op uint32) error {
	listener.seq++
	pr := &netlinkProcMessage{}
	plen := binary.Size(pr.Data) + binary.Size(op)
	pr.Header.Len = syscall.NLMSG_HDRLEN + uint32(plen)
	pr.Header.Type = uint16(syscall.NLMSG_DONE)
	pr.Header.Flags = 0
	pr.Header.Seq = listener.seq
	pr.Header.Pid = uint32(os.Getpid())

	pr.Data.Id.Idx = _CN_IDX_PROC
	pr.Data.Id.Val = _CN_VAL_PROC

	pr.Data.Len = uint16(binary.Size(op))

	buf := bytes.NewBuffer(make([]byte, 0, pr.Header.Len))
	binary.Write(buf, byteOrder, pr)
	binary.Write(buf, byteOrder, op)

	return syscall.Sendto(listener.sock, buf.Bytes(), 0, listener.addr)
}
