package env

import (
	"path/filepath"
	"runtime"
	"time"
)

var (
	globalWorkDir           = "/tmp/tcontainerd"
	globalPersistentWorkDir = "/tmp/tcontainerd-persistent"
	namespace               = "default"
	shimRuntimeID           = "io.containerd.runc.v2"
	shimName                = "containerd-shim-runc-v2"
	timeout                 = 10 * time.Second
	pullPolicy              = "missing"
	snapshotter             = "native"
)

func WorkDir() string                  { return globalWorkDir }
func PersistentWorkDir() string        { return globalPersistentWorkDir }
func ContainerdConfigTomlPath() string { return filepath.Join(WorkDir(), "containerd.toml") }
func NerdctlConfigTomlPath() string    { return filepath.Join(WorkDir(), "nerdctl.toml") }
func Namespace() string                { return namespace }
func Address() string                  { return filepath.Join(WorkDir(), "containerd.sock") }
func LockFile() string                 { return filepath.Join(PersistentWorkDir(), "lock.pid") }
func ShimSimlinkPath() string          { return filepath.Join(WorkDir(), "reexec", shimName) }
func ShimRuncSimlinkPath() string      { return filepath.Join(WorkDir(), "reexec", "runc") }
func CtrSimlinkPath() string           { return filepath.Join(WorkDir(), "reexec", "ctr") }
func ShimRawWriterSockPath() string    { return filepath.Join(WorkDir(), "reexec-raw-writer.sock") }
func ShimDelimitedWriterSockPath() string {
	return filepath.Join(WorkDir(), "reexec-delim-writer.sock")
}

func ShimOtelProxySockPath() string  { return filepath.Join(WorkDir(), "reexec-otel-proxy.sock") }
func ShimRuntimeID() string          { return shimRuntimeID }
func ShimName() string               { return shimName }
func Timeout() time.Duration         { return timeout }
func ContainerdRootDir() string      { return filepath.Join(PersistentWorkDir(), "root") }
func ContainerdStateDir() string     { return filepath.Join(PersistentWorkDir(), "state") }
func ContainerdContentDir() string   { return filepath.Join(PersistentWorkDir(), "content") }
func ContainerdSnapshotsDir() string { return filepath.Join(PersistentWorkDir(), "snapshots") }
func PullPolicy() string             { return pullPolicy }
func Snapshotter() string            { return snapshotter }

func MagicHostOtlpGRPCPort() uint32 {
	return 4317
}

func LinuxRuntimeBuildDir() string {
	// get abs path of current go file (not working directory)
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		panic("failed to get caller")
	}

	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "gen", "build", "linux_vf_offline_arm64")

}
