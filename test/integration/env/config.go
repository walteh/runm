package env

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/v2/client"
	"gitlab.com/tozd/go/errors"
)

func NewContainerdClient(ctx context.Context) (*client.Client, error) {
	return client.New(ContainerdAddress(), client.WithDefaultNamespace(Namespace()), client.WithTimeout(Timeout()))
}

func LoadCurrentServerConfig(ctx context.Context) ([]byte, error) {

	// read the lock file
	running, err := isServerRunning(ctx)
	if err != nil {
		return nil, errors.Errorf("checking if server is running: %w", err)
	}
	if !running {
		return nil, errors.Errorf("server is already running, please stop it first")
	}

	configFile := filepath.Join(WorkDir(), "containerd.toml")

	config, err := os.ReadFile(configFile)
	if err != nil {
		return nil, errors.Errorf("reading config file: %w", err)
	}

	return config, nil
}

func (s *DevContainerdServer) createBuildkitConfig(ctx context.Context) error {
	configContent := fmt.Sprintf(`
root = "%[3]s"

[grpc]
address = ["unix://%[4]s"]

[otel]
socketPath = "%[5]s"

[worker.oci]
enabled = false # maybe one day, not worth the effort right

[worker.containerd]
enabled = true
address = "%[6]s" # breaks if we use unix:// for some reason
platforms = ["linux/arm64"]

[worker.containerd.runtime]
path = "%[1]s"
name = "%[2]s"

[cdi]
specDirs = ["%[7]s"]

[frontend.dockerfile.v0]
enabled = true

[frontend.gateway.v0]
enabled = true


`, ShimSimlinkPath(), ShimRuntimeID(), BuildkitdRootDir(), BuildkitdAddress(), BuildkitdOtelSocketPath(), ContainerdAddress(), CDISpecDir())

	if err := os.WriteFile(BuildkitdConfigTomlPath(), []byte(configContent), 0644); err != nil {
		return errors.Errorf("writing buildkit config: %w", err)
	}

	os.MkdirAll(CDISpecDir(), 0755)

	return nil
}

func (s *DevContainerdServer) createNerdctlConfig(ctx context.Context) error {
	logLevel := "info"
	if s.debug {
		logLevel = "debug"
	}

	configContent := fmt.Sprintf(`
debug          = false
debug_full     = false
address        = "unix://%[2]s"
namespace      = "%[3]s"
snapshotter    = "%[4]s"
data_root      = "%[6]s"
cni_netconfpath = "%[7]s"
cni_path        = "%[8]s"
# pull_policy    = "%[5]s"
# cgroup_manager = "cgroupfs"
# hosts_dir      = ["/etc/containerd/certs.d", "/etc/docker/certs.d"]
experimental   = true
# userns_remap   = ""
	`, logLevel == "debug", ContainerdAddress(), Namespace(), Snapshotter(), PullPolicy(), NerdctlDataRoot(), NerdctlCNINetConfPath(), NerdctlCNIPath())

	if err := os.WriteFile(NerdctlConfigTomlPath(), []byte(configContent), 0644); err != nil {
		return errors.Errorf("writing nerdctl config: %w", err)
	}

	slog.InfoContext(ctx, "Created nerdctl config", "path", NerdctlConfigTomlPath())
	return nil
} //s////////////////////////////////

func (s *DevContainerdServer) createContainerdConfig(ctx context.Context) error {
	logLevel := "info"
	if s.debug {
		logLevel = "debug"
	}

	configContent := fmt.Sprintf(`
version = 3
root   = "%[1]s"
state  = "%[2]s"

[grpc]
address = "%[3]s"

[ttrpc]
address = "%[3]s.ttrpc"

[debug]
level = "%[4]s"

[plugins."io.containerd.runtime.v1.linux"]
shim_debug = true

[plugins."io.containerd.snapshotter.v1.overlayfs"]
root_path = "%[10]s"

[plugins."io.containerd.snapshotter.v1.native"]
root_path = "%[7]s"

#[plugins."io.containerd.content.v1.content"]
#path = "%[8]s"

[plugins."io.containerd.cri.v1.runtime"]
cdi_spec_dirs = ["%[9]s"]

[plugins."io.containerd.gc.v1.scheduler"]
pause_threshold = 0.02
deletion_threshold = 0
mutation_threshold = 100
schedule_delay = "0s"
startup_delay = "10s"

# Metadata settings for content sharing
[plugins."io.containerd.metadata.v1.bolt"]
content_sharing_policy = "shared"



## Register harpoon runtime for CRI
#[plugins."io.containerd.cri.v1.runtime".containerd]
#  default_runtime_name = "%[5]s"
#
#  [plugins."io.containerd.cri.v1.runtime".containerd.runtimes]
#    [plugins."io.containerd.cri.v1.runtime".containerd.runtimes."%[5]s"]
#      runtime_type = "%[5]s"
#      [plugins."io.containerd.cri.v1.runtime".containerd.runtimes."io.containerd.runc.v2".options]
#        binary_name = "%[6]s"
`,
		ContainerdRootDir(),               // %[1]s
		ContainerdStateDir(),              // %[2]s
		ContainerdAddress(),               // %[3]s
		logLevel,                          // %[4]s
		shimRuntimeID,                     // %[5]s
		ShimSimlinkPath(),                 // %[6]s
		ContainerdNativeSnapshotsDir(),    // %[7]s
		ContainerdContentDir(),            // %[8]s
		CDISpecDir(),                      // %[9]s
		ContainerdOverlayfsSnapshotsDir(), // %[10]s
	)

	if EnableStargzSnapshotter() {
		configContent += fmt.Sprintf(`
# Enable stargz snapshotter for CRI
[plugins."io.containerd.grpc.v1.cri".containerd]
  snapshotter = "stargz"
  disable_snapshot_annotations = false

# Plug stargz snapshotter into containerd
[proxy_plugins]
  [proxy_plugins.stargz]
    type = "snapshot"
    address = "%[1]s"
  [proxy_plugins.stargz.exports]
    root = "%[2]s"
`, StargzSocketPath(), StargzExportsDirPath())
	}

	if err := os.WriteFile(ContainerdConfigTomlPath(), []byte(configContent), 0644); err != nil {
		return errors.Errorf("writing containerd config: %w", err)
	}

	slog.InfoContext(ctx, "Created containerd config", "path", ContainerdConfigTomlPath())
	return nil
}
