package state

import (
	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/pkg/syncmap"
)

type State struct {
	openIOs *syncmap.Map[string, runtime.IO]
	// openSockets          *syncmap.Map[string, runtime.AllocatedSocket]
	openConsoles         *syncmap.Map[string, runtime.ConsoleSocket]
	openVsockConnections *syncmap.Map[uint32, runtime.VsockAllocatedSocket]
	openUnixConnections  *syncmap.Map[string, runtime.UnixAllocatedSocket]
}

func NewState() *State {
	return &State{
		openIOs: syncmap.NewMap[string, runtime.IO](),
		// openSockets:          syncmap.NewMap[string, runtime.AllocatedSocket](),
		openConsoles:         syncmap.NewMap[string, runtime.ConsoleSocket](),
		openVsockConnections: syncmap.NewMap[uint32, runtime.VsockAllocatedSocket](),
		openUnixConnections:  syncmap.NewMap[string, runtime.UnixAllocatedSocket](),
	}
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
	s.openIOs.Delete(referenceId)
}

// func (s *State) DeleteOpenSocket(referenceId string) {
// 	s.openSockets.Delete(referenceId)
// }

func (s *State) DeleteOpenConsole(referenceId string) {
	s.openConsoles.Delete(referenceId)
}

func (s *State) StoreOpenVsockConnection(port uint32, conn runtime.VsockAllocatedSocket) {

	s.openVsockConnections.Store(port, conn)
}

func (s *State) GetOpenVsockConnection(port uint32) (runtime.VsockAllocatedSocket, bool) {
	return s.openVsockConnections.Load(port)
}

func (s *State) DeleteOpenVsockConnection(port uint32) {
	s.openVsockConnections.Delete(port)
}

func (s *State) StoreOpenUnixConnection(path string, conn runtime.UnixAllocatedSocket) {

	s.openUnixConnections.Store(path, conn)
}

func (s *State) GetOpenUnixConnection(path string) (runtime.UnixAllocatedSocket, bool) {
	return s.openUnixConnections.Load(path)
}

func (s *State) DeleteOpenUnixConnection(path string) {
	s.openUnixConnections.Delete(path)
}
