package env

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/tozd/go/errors"
)

func initializeFsEnv() {
	// list all the files in the fs env dir
	files, err := os.ReadDir(FsEnvDir())
	if err != nil {
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		dat, err := os.ReadFile(filepath.Join(FsEnvDir(), file.Name()))
		if err != nil {
			continue
		}

		os.Setenv(file.Name(), string(dat))
	}

}

func init() {
	initializeFsEnv()
}

var (
	globalWorkDir           = "/tmp/tcontainerd"
	globalPersistentWorkDir = "/tmp/tcontainerd-persistent"
	globalFsEnvDir          = "/tmp/tcontainerd-fs-env"

	namespace               = "default"
	shimRuntimeID           = "io.containerd.runc.v2"
	shimName                = "containerd-shim-runc-v2"
	timeout                 = 10 * time.Second
	pullPolicy              = "missing"
	snapshotter             = "native"
	defaultDebugPort        = 12346
	underDlvEnvVar          = "UNDER_DLV"
	enableStargzSnapshotter = false
)

func WorkDir() string                  { return globalWorkDir }
func PersistentWorkDir() string        { return globalPersistentWorkDir }
func FsEnvDir() string                 { return globalFsEnvDir }
func ContainerdConfigTomlPath() string { return filepath.Join(WorkDir(), "containerd.toml") }
func NerdctlConfigTomlPath() string    { return filepath.Join(WorkDir(), "nerdctl.toml") }
func BuildkitdConfigTomlPath() string  { return filepath.Join(WorkDir(), "buildkitd.toml") }
func NerdctlCNINetConfPath() string    { return filepath.Join(WorkDir(), "cni", "net.d") }
func NerdctlCNIPath() string           { return filepath.Join(WorkDir(), "cni", "bin") }
func Namespace() string                { return namespace }
func ContainerdAddress() string        { return filepath.Join(WorkDir(), "containerd.sock") }
func LockFile() string                 { return filepath.Join(PersistentWorkDir(), "lock.pid") }
func ShimSimlinkPath() string          { return filepath.Join(WorkDir(), "reexec", shimName) }
func ShimRuncSimlinkPath() string      { return filepath.Join(WorkDir(), "reexec", "runc") }
func CtrSimlinkPath() string           { return filepath.Join(WorkDir(), "reexec", "ctr") }
func ShimRawWriterSockPath() string    { return filepath.Join(WorkDir(), "reexec-raw-writer.sock") }

func ShimDelimitedWriterSockPath() string {
	return filepath.Join(WorkDir(), "reexec-delim-writer.sock")
}
func ShimOtelProxySockPath() string           { return filepath.Join(WorkDir(), "reexec-otel-proxy.sock") }
func ShimRuntimeID() string                   { return shimRuntimeID }
func ShimName() string                        { return shimName }
func Timeout() time.Duration                  { return timeout }
func ContainerdRootDir() string               { return filepath.Join(PersistentWorkDir(), "root") }
func ContainerdStateDir() string              { return filepath.Join(PersistentWorkDir(), "state") }
func ContainerdContentDir() string            { return filepath.Join(PersistentWorkDir(), "content") }
func ContainerdSnapshotsDir() string          { return filepath.Join(PersistentWorkDir(), "snapshots") }
func ContainerdNativeSnapshotsDir() string    { return filepath.Join(PersistentWorkDir(), "native") }
func ContainerdOverlayfsSnapshotsDir() string { return filepath.Join(PersistentWorkDir(), "overlayfs") }
func NerdctlDataRoot() string                 { return filepath.Join(PersistentWorkDir(), "nerdctl-data-root") }
func BuildkitdRootDir() string                { return filepath.Join(PersistentWorkDir(), "buildkitd-root") }
func BuildkitdAddress() string                { return filepath.Join(PersistentWorkDir(), "buildkitd.sock") }
func CDISpecDir() string                      { return filepath.Join(PersistentWorkDir(), "cdi-spec") }
func BuildkitdOtelSocketPath() string {
	return filepath.Join(PersistentWorkDir(), "buildkitd-otel.sock")
}
func PullPolicy() string { return pullPolicy }
func Snapshotter() string {
	if enableStargzSnapshotter {
		return "stargz"
	}
	return snapshotter
}

func EnableStargzSnapshotter() bool { return enableStargzSnapshotter }

func StargzSocketPath() string {
	return filepath.Join(PersistentWorkDir(), "containerd-stargz-grpc.sock")
}

func StargzExportsDirPath() string {
	return filepath.Join(PersistentWorkDir(), "containerd-stargz-grpc-exports")
}

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

func getEnvVar(name string) string {
	val := os.Getenv(name)
	if val == "" {
		val = getFsEnvVar(name)
	}
	return val
}

func ensureShimEnvVars(strs ...string) {
	for _, str := range strs {
		if val := getEnvVar(str); val != "" {
			os.Setenv(str, val)
		}
	}
}

func resolveDebugEnvVar() string {
	exe := getMyExecutableName()
	envVar := getDebugEnvVarName(exe)

	val := os.Getenv(envVar)

	if val == "" {
		val = getFsEnvVar(envVar)
	}

	slog.Info("resolveDebugEnvVar", "exe", exe, "envVar", envVar, "val", val)

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

func GetCurrentRunmProcs() []string {
	return currentRunmProcs
}

var currentRunmProcs = []string{
	"CAP_CHOWN",
	"CAP_DAC_OVERRIDE",
	"CAP_DAC_READ_SEARCH",
	"CAP_FOWNER",
	"CAP_FSETID",
	"CAP_KILL",
	"CAP_SETGID",
	"CAP_SETUID",
	"CAP_SETPCAP",
	"CAP_LINUX_IMMUTABLE",
	"CAP_NET_BIND_SERVICE",
	"CAP_NET_BROADCAST",
	"CAP_NET_ADMIN",
	"CAP_NET_RAW",
	"CAP_IPC_LOCK",
	"CAP_IPC_OWNER",
	"CAP_SYS_MODULE",
	"CAP_SYS_RAWIO",
	"CAP_SYS_CHROOT",
	"CAP_SYS_PTRACE",
	"CAP_SYS_PACCT",
	"CAP_SYS_ADMIN",
	"CAP_SYS_BOOT",
	"CAP_SYS_NICE",
	"CAP_SYS_RESOURCE",
	"CAP_SYS_TIME",
	"CAP_SYS_TTY_CONFIG",
	"CAP_MKNOD",
	"CAP_LEASE",
	"CAP_AUDIT_WRITE",
	"CAP_AUDIT_CONTROL",
	"CAP_SETFCAP",
	"CAP_MAC_OVERRIDE",
	"CAP_MAC_ADMIN",
	"CAP_SYSLOG",
	"CAP_WAKE_ALARM",
	"CAP_BLOCK_SUSPEND",
	"CAP_AUDIT_READ",
	"CAP_PERFMON",
	"CAP_BPF",
	"CAP_CHECKPOINT_RESTORE",
}
