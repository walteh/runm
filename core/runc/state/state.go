package state

import (
	"log/slog"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/pkg/syncmap"
)

type State struct {
	openIOs              *syncmap.Map[string, runtime.IO]
	openConsoles         *syncmap.Map[string, runtime.ConsoleSocket]
	openVsockConnections *syncmap.Map[uint32, runtime.VsockAllocatedSocket]
	openUnixConnections  *syncmap.Map[string, runtime.UnixAllocatedSocket]

	closedIOs              *syncmap.Map[string, runtime.IO]
	closedConsoles         *syncmap.Map[string, runtime.ConsoleSocket]
	closedVsockConnections *syncmap.Map[uint32, runtime.VsockAllocatedSocket]
	closedUnixConnections  *syncmap.Map[string, runtime.UnixAllocatedSocket]
}

func NewState() *State {
	return &State{
		openIOs:                syncmap.NewMap[string, runtime.IO](),
		openConsoles:           syncmap.NewMap[string, runtime.ConsoleSocket](),
		openVsockConnections:   syncmap.NewMap[uint32, runtime.VsockAllocatedSocket](),
		openUnixConnections:    syncmap.NewMap[string, runtime.UnixAllocatedSocket](),
		closedIOs:              syncmap.NewMap[string, runtime.IO](),
		closedConsoles:         syncmap.NewMap[string, runtime.ConsoleSocket](),
		closedVsockConnections: syncmap.NewMap[uint32, runtime.VsockAllocatedSocket](),
		closedUnixConnections:  syncmap.NewMap[string, runtime.UnixAllocatedSocket](),
	}
}

func (s *State) OpenIOs() *syncmap.Map[string, runtime.IO] {
	return s.openIOs
}

func (s *State) OpenConsoles() *syncmap.Map[string, runtime.ConsoleSocket] {
	return s.openConsoles
}

func (s *State) OpenVsockConnections() *syncmap.Map[uint32, runtime.VsockAllocatedSocket] {
	return s.openVsockConnections
}

func (s *State) OpenUnixConnections() *syncmap.Map[string, runtime.UnixAllocatedSocket] {
	return s.openUnixConnections
}

func (s *State) GetOpenIO(referenceId string) (runtime.IO, bool) {
	return s.openIOs.Load(referenceId)
}

// func (s *State) GetOpenSocket(referenceId string) (runtime.AllocatedSocket, bool) {
// 	return s.openSockets.Load(referenceId)
// }

func (s *State) GetOpenConsole(referenceId string) (runtime.ConsoleSocket, bool) {
	return s.openConsoles.Load(referenceId)
}

func (s *State) StoreOpenIO(referenceId string, io runtime.IO) {
	s.openIOs.Store(referenceId, io)
}

// func (s *State) StoreOpenSocket(referenceId string, socket runtime.AllocatedSocket) {
// 	s.openSockets.Store(referenceId, socket)
// }

func (s *State) StoreOpenConsole(referenceId string, console runtime.ConsoleSocket) {
	s.openConsoles.Store(referenceId, console)
}

func (s *State) DeleteOpenIO(referenceId string) {
	slog.Info("deleting open io", "referenceId", referenceId)
	io, ok := s.openIOs.LoadAndDelete(referenceId)
	if ok {
		s.closedIOs.Store(referenceId, io)
	}
}

// func (s *State) DeleteOpenSocket(referenceId string) {
// 	s.openSockets.Delete(referenceId)
// }

func (s *State) DeleteOpenConsole(referenceId string) {
	slog.Info("deleting open console", "referenceId", referenceId)
	console, ok := s.openConsoles.LoadAndDelete(referenceId)
	if ok {
		s.closedConsoles.Store(referenceId, console)
	}
}

func (s *State) StoreOpenVsockConnection(port uint32, conn runtime.VsockAllocatedSocket) {
	s.openVsockConnections.Store(port, conn)
}

func (s *State) GetOpenVsockConnection(port uint32) (runtime.VsockAllocatedSocket, bool) {
	return s.openVsockConnections.Load(port)
}

func (s *State) DeleteOpenVsockConnection(port uint32) {
	conn, ok := s.openVsockConnections.LoadAndDelete(port)
	if ok {
		s.closedVsockConnections.Store(port, conn)
	}
}

func (s *State) StoreOpenUnixConnection(path string, conn runtime.UnixAllocatedSocket) {
	s.openUnixConnections.Store(path, conn)
}

func (s *State) GetOpenUnixConnection(path string) (runtime.UnixAllocatedSocket, bool) {
	return s.openUnixConnections.Load(path)
}

func (s *State) DeleteOpenUnixConnection(path string) {
	conn, ok := s.openUnixConnections.LoadAndDelete(path)
	if ok {
		s.closedUnixConnections.Store(path, conn)
	}
}
