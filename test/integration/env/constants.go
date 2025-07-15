package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/tozd/go/errors"
)

var (
	globalWorkDir           = "/tmp/tcontainerd"
	globalPersistentWorkDir = "/tmp/tcontainerd-persistent"
	globalFsEnvDir          = "/tmp/tcontainerd-fs-env"

	namespace        = "default"
	shimRuntimeID    = "io.containerd.runc.v2"
	shimName         = "containerd-shim-runc-v2"
	timeout          = 10 * time.Second
	pullPolicy       = "missing"
	snapshotter      = "native"
	defaultDebugPort = 12346
	underDlvEnvVar   = "UNDER_DLV"
)

func WorkDir() string                  { return globalWorkDir }
func PersistentWorkDir() string        { return globalPersistentWorkDir }
func FsEnvDir() string                 { return globalFsEnvDir }
func ContainerdConfigTomlPath() string { return filepath.Join(WorkDir(), "containerd.toml") }
func NerdctlConfigTomlPath() string    { return filepath.Join(WorkDir(), "nerdctl.toml") }
func NerdctlCNINetConfPath() string    { return filepath.Join(WorkDir(), "cni", "net.d") }
func NerdctlCNIPath() string           { return filepath.Join(WorkDir(), "cni", "bin") }
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
func NerdctlDataRoot() string        { return filepath.Join(PersistentWorkDir(), "nerdctl-data-root") }
func PullPolicy() string             { return pullPolicy }
func Snapshotter() string            { return snapshotter }

func MagicHostOtlpGRPCPort() uint32 {
	return 4317
}

func LinuxRuntimeBuildDir() string {
	val := os.Getenv("LINUX_RUNTIME_DIR")
	if val == "" {
		panic("LINUX_RUNTIME_DIR is not set")
	}
	return val
}

func ShimBinaryPath() string {
	val := os.Getenv("SHIM_BINARY_PATH")
	if val == "" {
		panic("SHIM_BINARY_PATH is not set")
	}
	return val
}

func getMyExecutableName() string {
	executable, err := os.Executable()
	if err != nil {
		return "unknown"
	}

	// resolve the symlink
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "unknown"
	}

	return filepath.Base(executable)
}

func getDebugEnvVarName(exe string) string {

	// if the executable name contains "debug", return true
	exe = strings.ReplaceAll(exe, "-", "_")

	exe = strings.ToUpper(exe)
	return fmt.Sprintf("DEBUG_%s", exe)
}

func getFsEnvVar(exe string) string {
	if dat, err := os.ReadFile(filepath.Join(FsEnvDir(), exe)); err == nil {
		return string(dat)
	}
	return ""
}

func resolveDebugEnvVar() string {
	exe := getMyExecutableName()
	envVar := getDebugEnvVarName(exe)

	val := os.Getenv(envVar)
	if val == "" {
		val = getFsEnvVar(exe)
	}

	return val
}

// func DebugFlagEnabled() bool {

// 	return false
// }

func FindDlvPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", errors.Errorf("failed to get executable path for debugging: %w", err)
	}

	dlvPath, err := exec.LookPath("dlv")
	if err != nil {

		// if this does not work, look for one next to myself
		dlvPath, err = exec.LookPath(filepath.Join(filepath.Dir(execPath), "dlv"))
		if err != nil {
			return "", errors.Errorf("dlv (Delve debugger) not found in PATH or next to myself")
		}
	}
	return dlvPath, nil
}
