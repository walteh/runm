//go:build !windows

package vmm

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/common/pkg/strongunits"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/port"
	"github.com/walteh/runm/pkg/units"
)

type VMFuseVMConfig struct {
	ID             string
	MountType      string
	Sources        []string
	Target         string
	StartingMemory strongunits.B
	VCPUs          uint64
	Platform       units.Platform
	RawWriter      io.Writer
	DelimWriter    io.Writer
	InitArgs       []string
}

// NewVMFuseVirtualMachine creates a VM configured for vmfuse operations
// This follows the same pattern as NewOCIVirtualMachine but for vmfuse
func NewVMFuseVirtualMachine[VM VirtualMachine](
	ctx context.Context,
	hpv Hypervisor[VM],
	config VMFuseVMConfig,
	devices ...virtio.VirtioDevice,
) (*RunningVM[VM], uint16, error) {

	vmid := config.ID

	// ctx = context.WithValue(ctx, "vmid", vmid)

	startTime := time.Now()

	// Create default devices for vmfuse VM
	defaults, nfsPort, err := newDefaultLinuxVMForVMFuse(ctx, vmid, config)
	if err != nil {
		return nil, 0, errors.Errorf("getting VM defaults: %w", err)
	}

	slog.InfoContext(ctx, "about to set up vmfuse VM",
		"mount_type", config.MountType,
		"sources", config.Sources,
		"target", config.Target,
	)

	var bootloader virtio.Bootloader

	switch config.Platform {
	case units.PlatformLinuxARM64:
		cfgs := []string{
			"console=hvc0",
			"systemd.unified_cgroup_hierarchy=1",
			"vm.compact_memory=1",
			"user.max_user_namespaces=15000",
			"pid_max=100000",
			"--",
			"-runm-mode=vmfuse",
			// "-vmfuse-mount-type=" + config.MountType,
			// "-vmfuse-mount-sources=" + strings.Join(config.Sources, ","),
			// "-vmfuse-mount-target=" + config.Target,
			"-mshare-dir-binds=" + defaults.MshareDirsWithTags(),
			"-timezone=" + defaults.Timezone,
			"-time=" + defaults.StartTimeUnixNanoString(),
			"-init-mbin-name=" + "vmfuse-init",
		}

		// Add init args if provided
		cfgs = append(cfgs, config.InitArgs...)

		bootloader = &virtio.LinuxBootloader{
			InitrdPath:    filepath.Join(defaults.HostRuntimeBuildDir, "initramfs.cpio.gz"),
			VmlinuzPath:   filepath.Join(defaults.HostRuntimeBuildDir, "kernel"),
			KernelCmdLine: strings.Join(cfgs, " "),
		}
	default:
		return nil, 0, errors.Errorf("unsupported OS: %s", config.Platform.OS())
	}

	opts := NewVMOptions{
		Vcpus:         config.VCPUs,
		Memory:        config.StartingMemory,
		Devices:       append(defaults.Devices, devices...),
		GuestPlatform: config.Platform,
	}

	slog.InfoContext(ctx, "ready to create vmfuse vm")

	vm, err := hpv.NewVirtualMachine(ctx, vmid, &opts, bootloader)
	if err != nil {
		return nil, 0, errors.Errorf("creating virtual machine: %w", err)
	}

	runner := &RunningVM[VM]{
		bootloader:   bootloader,
		start:        startTime,
		vm:           vm,
		portOnHostIP: defaults.MagicHostPort,
		wait:         make(chan error, 1),
		workingDir:   defaults.WorkingDir,
		netdev:       defaults.GvProxy,
		rawWriter:    config.RawWriter,
		delimWriter:  config.DelimWriter,
		msockBinds:   defaults.MshareSockPorts,
	}

	slog.InfoContext(ctx, "created vmfuse vm", "id", config.ID)

	return runner, nfsPort, nil
}

// newDefaultLinuxVMForVMFuse creates default VM configuration for vmfuse
func newDefaultLinuxVMForVMFuse(ctx context.Context, vmid string, config VMFuseVMConfig) (*DefaultVMConfig, uint16, error) {
	// Create virtio devices for source directory sharing
	shareDevices := make([]virtio.VirtioDevice, 0, len(config.Sources))
	mshareDirs := make([]string, 0, len(config.Sources))

	for i, source := range config.Sources {
		shareTag := generateShareTag(source, i)
		shareDevice, err := virtio.VirtioFsNew(source, shareTag)
		if err != nil {
			return nil, 0, errors.Errorf("creating virtio-fs device for %s: %w", source, err)
		}
		shareDevices = append(shareDevices, shareDevice)
		mshareDirs = append(mshareDirs, source)
	}

	//reserve a new port for the vmfuse server
	port, err := port.ReservePort(ctx)
	if err != nil {
		return nil, 0, errors.Errorf("reserving port: %w", err)
	}

	// Use the existing newDefaultLinuxVM but customize for vmfuse
	defaults, err := newDefaultLinuxVM(ctx, vmid, constants.MbinVMFUSEFileName, map[string]any{}, mshareDirs, []string{}, map[uint16]uint16{port: 2049})
	if err != nil {
		return nil, 0, errors.Errorf("creating default linux VM: %w", err)
	}

	// Add our custom virtio-fs devices
	defaults.Devices = append(defaults.Devices, shareDevices...)

	return defaults, port, nil
}

// generateShareTag creates a consistent tag for virtio-fs sharing
func generateShareTag(source string, index int) string {
	// Simple implementation - could be more sophisticated
	return filepath.Base(source) + "-" + string(rune('0'+index))
}
