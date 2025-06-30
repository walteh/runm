package runtime

import (
	"context"
	"io"
	"net"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/net/proxy"

	"github.com/containerd/cgroups/v3/cgroup2/stats"
	"github.com/containerd/console"
	"github.com/containerd/containerd/v2/core/events"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-spec/specs-go/features"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/process"
)

const (
	GuestSharedDir = "/runm/shared"
	LogFileBase    = "runm.log"
)

type RuntimeOptions struct {
	ProcessCreateConfig *process.CreateConfig
	Mounts              []process.Mount
	Rootfs              string
	Namespace           string
	Publisher           events.Publisher
	OciSpec             *oci.Spec
	Bundle              string
	HostOtlpPort        uint32
}

type RuntimeCreator interface {
	Create(ctx context.Context, opts *RuntimeOptions) (Runtime, error)
	Features(ctx context.Context) (*features.Features, error)
}

//go:mock
type SocketAllocator interface {
	AllocateSocket(ctx context.Context) (AllocatedSocket, error)
}

type CgroupEvent struct {
	Low     uint64
	High    uint64
	Max     uint64
	OOM     uint64
	OOMKill uint64
}

type CgroupAdapter interface {
	Stat(ctx context.Context) (*stats.Metrics, error)
	ToggleControllers(ctx context.Context) error
	OpenEventChan(ctx context.Context) (<-chan CgroupEvent, <-chan error, error)
}

type GuestManagement interface {
	TimeSync(ctx context.Context, unixTimeNs uint64, timezone string) error
	Readiness(ctx context.Context) error
	RunCommand(ctx context.Context, cmd *exec.Cmd) error
}

type PublishEvent struct {
	Topic string
	Data  []byte
}

type EventHandler interface {
	Publish(ctx context.Context, event *PublishEvent) error

	Receive(ctx context.Context) (<-chan *PublishEvent, error)
}

//go:mock
type Runtime interface {
	// SharedDir() string
	// io: yes
	// ✅
	NewPipeIO(ctx context.Context, cioUID, ioGID int, opts ...gorunc.IOOpt) (IO, error)
	// io: yes
	NewTempConsoleSocket(ctx context.Context) (ConsoleSocket, error)
	// io: yes
	// ✅
	NewNullIO() (IO, error)
	// io: yes
	// console: yes
	// channel: yes
	// fd: yes
	Create(ctx context.Context, id, bundle string, opts *gorunc.CreateOpts) error
	// io: yes
	// console: yes
	// channel: yes
	Exec(ctx context.Context, id string, spec specs.Process, opts *gorunc.ExecOpts) error
	// fd: yes
	Checkpoint(ctx context.Context, id string, opts *gorunc.CheckpointOpts, actions ...gorunc.CheckpointAction) error
	// io: yes
	Restore(ctx context.Context, id, bundle string, opts *gorunc.RestoreOpts) (int, error)
	// ✅
	Kill(ctx context.Context, id string, signal int, opts *gorunc.KillOpts) error
	Start(ctx context.Context, id string) error
	// ✅
	Delete(ctx context.Context, id string, opts *gorunc.DeleteOpts) error
	// ✅
	Update(ctx context.Context, id string, resources *specs.LinuxResources) error
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	Ps(ctx context.Context, id string) ([]int, error)
	ReadPidFile(ctx context.Context, path string) (int, error)
}

type ConsoleSocket interface {
	ReceiveMaster() (console.Console, error)
	Path() string
	Close() error
}

// Platform handles platform-specific behavior that may differs across
// // platform implementations
// type Platform interface {
// 	CopyConsole(ctx context.Context, console Console, id, stdin, stdout, stderr string, wg *sync.WaitGroup) (Console, error)
// 	ShutdownConsole(ctx context.Context, console Console) error
// 	Close() error
// }

type RuntimeConsole interface {
	console.File

	// Resize resizes the console to the provided window size
	Resize(console.WinSize) error
	// ResizeFrom resizes the calling console to the size of the
	// provided console
	ResizeFrom(RuntimeConsole) error
	// SetRaw sets the console in raw mode
	SetRaw() error
	// DisableEcho disables echo on the console
	DisableEcho() error
	// Reset restores the console to its original state
	Reset() error
	// Size returns the window size of the console
	Size() (console.WinSize, error)
}

type IO interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Close() error
	// unused
	Set(cmd *exec.Cmd)
}

// RuncLibrary defines an interface for interacting with runc containers.
// This interface mirrors the functionality provided by the go-runc package
// to allow for easy mocking and testing.
//
//go:mock
type RuntimeExtras interface {
	// State returns the state of the container for the given id.
	State(context.Context, string) (*gorunc.Container, error)

	// Run creates and starts a container and returns its pid.
	RuncRun(context.Context, string, string, *gorunc.CreateOpts) (int, error)

	// Stats returns runtime specific metrics for a container.
	Stats(context.Context, string) (*gorunc.Stats, error)

	// Events returns events for the container.
	Events(context.Context, string, time.Duration) (chan *gorunc.Event, error)

	// List lists all containers.
	List(context.Context) ([]*gorunc.Container, error)

	// Version returns the version of runc.
	Version(context.Context) (gorunc.Version, error)

	Top(context.Context, string, string) (*gorunc.TopResults, error)
}

type AllocatedSocketReference interface {
	ReferableByReferenceId
}

type FileConn interface {
	syscall.Conn
	net.Conn
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
}

type AllocatedSocket interface {
	// isAllocatedSocket()
	proxy.ContextDialer
	io.Closer
	Conn() FileConn
	Ready() error
}

type VsockAllocatedSocket interface {
	AllocatedSocket
	Port() uint32
}

type UnixAllocatedSocket interface {
	AllocatedSocket
	Path() string
}

type ServerStateGetter interface {
	GetOpenIO(referenceId string) (IO, bool)
	GetOpenVsockConnection(port uint32) (VsockAllocatedSocket, bool)
	GetOpenUnixConnection(path string) (UnixAllocatedSocket, bool)
	GetOpenConsole(referenceId string) (ConsoleSocket, bool)
}

type ServerStateSetter interface {
	StoreOpenIO(referenceId string, io IO)
	StoreOpenSocket(referenceId string, socket AllocatedSocket)
	StoreOpenConsole(referenceId string, console ConsoleSocket)
}

type ReferableByReferenceId interface {
	GetReferenceId() string
}

type VsockProxier interface {
	ProxyVsock(ctx context.Context, port uint32) (*net.UnixConn, string, error)
	ListenAndAcceptSingleVsockConnection(ctx context.Context, port uint32, dialCallback func(ctx context.Context) error) (*net.UnixConn, error)
}

type VsockFdProxier interface {
	ProxyFd(ctx context.Context, port uint32) (*net.Conn, uintptr, error)
}
