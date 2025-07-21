//go:build !windows

package vmm

import (
	"context"
	"encoding/hex"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containers/common/pkg/strongunits"
	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/runc/process"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/units"
)

type OCIVMConfig struct {
	ID             string
	Bundle         string
	RootfsMounts   []process.Mount
	Spec           *oci.Spec
	StartingMemory strongunits.B
	VCPUs          uint64
	Platform       units.Platform
	RawWriter      io.Writer
	DelimWriter    io.Writer
}

func appendContext(ctx context.Context, id string) context.Context {
	return slogctx.Append(ctx,
		slog.String("vmid", id),
	)
}

// m

// devices:
// - mbin (mbin.squashfs)
// - bundle virtio fs (bundle.squashfs)

// NewContainerizedVirtualMachineFromRootfs creates a VM using an already-prepared rootfs directory
// This is used by container runtimes like containerd that have already prepared the rootfs
func NewOCIVirtualMachine[VM VirtualMachine](
	ctx context.Context,
	hpv Hypervisor[VM],
	ctrconfig OCIVMConfig,
	devices ...virtio.VirtioDevice,
) (*RunningVM[VM], error) {

	vmid := "vm-oci-" + ctrconfig.ID[:8]

	ctx = appendContext(ctx, vmid)

	startTime := time.Now()

	mshareFiles := map[string]any{
		constants.ContainerSpecFile:         ctrconfig.Spec,
		constants.ContainerRootfsMountsFile: ctrconfig.RootfsMounts,
	}

	mshareDirs, mshareSocks, err := isolateMsharesFromOciSpec(ctx, ctrconfig.Spec)
	if err != nil {
		return nil, errors.Errorf("finding mbind devices: %w", err)
	}

	mshareDirs = append(mshareDirs, ctrconfig.RootfsMounts[0].Source)
	mshareDirs = append(mshareDirs, ctrconfig.Bundle)

	defaults, err := newDefaultLinuxVM(ctx, vmid, constants.MbinFileName, mshareFiles, mshareDirs, mshareSocks)
	if err != nil {
		return nil, errors.Errorf("getting VM defaults: %w", err)
	}

	slog.InfoContext(ctx, "about to set up rootfs",
		"ctrconfig.RootfsMounts", ctrconfig.RootfsMounts,
		"spec.Root.Path", ctrconfig.Spec.Root.Path,
		"spec.Root.Readonly", ctrconfig.Spec.Root.Readonly,
	)

	var bootloader virtio.Bootloader

	var otelString string = ""

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		otelString = "-enable-otlp"
	}

	switch ctrconfig.Platform {
	case units.PlatformLinuxARM64:
		cfgs := []string{
			"console=hvc0",
			"systemd.unified_cgroup_hierarchy=1",
			"vm.compact_memory=1",
			"user.max_user_namespaces=15000",
			"pid_max=100000",
			"--",
			"-bundle-source=" + ctrconfig.Bundle,
			"-runm-mode=oci",
			"-container-id=" + ctrconfig.ID,
			otelString,
			"-init-mbin-name=" + "runm-linux-init",
			"-mshare-dir-binds=" + defaults.MshareDirsWithTags(),
			"-mshare-sock-binds=" + defaults.MshareSocketsWithPorts(),
			// "-rootfs-bind-options=" + strings.Join(ctrconfig.RootfsMounts[0].Options, ","),
			// "-rootfs-bind-target=" + ctrconfig.RootfsMounts[0].Target,
			// "-rootfs-bind-source=" + ctrconfig.RootfsMounts[0].Source,
			// "-rootfs-bind-type=" + ctrconfig.RootfsMounts[0].Type,
			"-timezone=" + defaults.Timezone,
			"-time=" + defaults.StartTimeUnixNanoString(),
		}

		bootloader = &virtio.LinuxBootloader{
			InitrdPath:    filepath.Join(defaults.HostRuntimeBuildDir, "initramfs.cpio.gz"),
			VmlinuzPath:   filepath.Join(defaults.HostRuntimeBuildDir, "kernel"),
			KernelCmdLine: strings.Join(cfgs, " "),
		}
	default:
		return nil, errors.Errorf("unsupported OS: %s", ctrconfig.Platform.OS())
	}

	opts := NewVMOptions{
		Vcpus:         ctrconfig.VCPUs,
		Memory:        ctrconfig.StartingMemory,
		Devices:       append(defaults.Devices, devices...),
		GuestPlatform: ctrconfig.Platform,
	}

	waitStart := time.Now()

	slog.InfoContext(ctx, "ready to create vm", "async_wait_duration", time.Since(waitStart))

	vm, err := hpv.NewVirtualMachine(ctx, vmid, &opts, bootloader)
	if err != nil {
		return nil, errors.Errorf("creating virtual machine: %w", err)
	}

	runner := &RunningVM[VM]{
		bootloader:   bootloader,
		start:        startTime,
		vm:           vm,
		portOnHostIP: defaults.MagicHostPort,
		wait:         make(chan error, 1),
		workingDir:   defaults.WorkingDir,
		netdev:       defaults.GvProxy,
		rawWriter:    ctrconfig.RawWriter,
		delimWriter:  ctrconfig.DelimWriter,
		msockBinds:   defaults.MshareSockPorts,
	}

	slog.InfoContext(ctx, "created oci vm", "id", ctrconfig.ID, "rawWriter==nil", ctrconfig.RawWriter == nil, "delimWriter==nil", ctrconfig.DelimWriter == nil)

	return runner, nil
}

func quickHash(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	out := h.Sum(nil)

	return hex.EncodeToString(out)
}

func isolateMsharesFromOciSpec(ctx context.Context, spec *oci.Spec) ([]string, []string, error) {

	proxyDevices := []string{}

	seen := map[string]bool{}

	// msockCounter := constants.MsockBasePort

	msockBindMounts := []string{}

	for _, mount := range spec.Mounts {
		if mount.Type != "bind" && mount.Type != "rbind" && !slices.Contains(mount.Options, "rbind") {
			continue
		}

		if strings.HasSuffix(mount.Source, ".sock") {
			msockBindMounts = append(msockBindMounts, mount.Source)

			// port := msockCounter
			// msockCounter++

			// msockBindMounts = append(msockBindMounts, msockBindMount{
			// 	Destination: mount.Destination,
			// 	Port:        uint32(port),
			// 	Source:      mount.Source,
			// })
			continue
		}

		slog.InfoContext(ctx, "found bind mount", "mount", mount.Destination, "type", mount.Type, "options", mount.Options, "source", mount.Source, "gid", mount.GIDMappings, "uid", mount.UIDMappings)

		src := mount.Source
		// if its a dir, update the source to be the dir
		if fi, err := os.Stat(src); err == nil && !fi.IsDir() {
			src = filepath.Dir(src)
		}

		if seen[src] {
			continue
		}

		proxyDevices = append(proxyDevices, src)

		seen[src] = true
	}

	// if len(rootfsMounts) > 1 {
	// 	return nil, nil, nil, errors.Errorf("multiple rootfs mounts found, expected only one: %v", rootfsMounts)
	// }

	// for _, mount := range rootfsMounts {
	// 	hashd := quickHash(mount.Source)
	// 	shareDev, err := virtio.VirtioFsNew(mount.Source, "rootfs-"+hashd)
	// 	if err != nil {
	// 		return nil, nil, nil, errors.Errorf("creating share device: %w", err)
	// 	}

	// 	devices = append(devices, shareDev)
	// 	proxyDevices = append(proxyDevices, mfsBindMount{
	// 		Target:  mount.Source,
	// 		Tag:     "rootfs-" + hashd,
	// 		Options: mount.Options,
	// 	})
	// }

	slog.InfoContext(ctx, "found mbind devices", "proxyDevices", proxyDevices)

	return proxyDevices, msockBindMounts, nil
}
