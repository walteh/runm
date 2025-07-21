//go:build !windows

package vmm

import (
	"context"
	"encoding/hex"
	"encoding/json"
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

	defaults, err := newDefaultLinuxVM(ctx, vmid, mshareFiles, mshareDirs, mshareSocks)
	if err != nil {
		return nil, errors.Errorf("getting VM defaults: %w", err)
	}

	// bundleDev, err := virtio.VirtioFsNew(ctrconfig.Bundle, constants.BundleVirtioTag)
	// if err != nil {
	// 	return nil, errors.Errorf("creating bundle virtio device: %w", err)
	// }

	// mfsBindMounts = append(mfsBindMounts, mfsBindMount{
	// 	Target: ctrconfig.Bundle,
	// 	Tag:    constants.BundleVirtioTag,
	// })

	// if len(ctrconfig.RootfsMounts) != 1 {
	// 	return nil, errors.Errorf("%d rootfs mounts found, expected exactly one: %v", len(ctrconfig.RootfsMounts), ctrconfig.RootfsMounts)
	// }

	// rootfsDev, err := virtio.VirtioFsNew(ctrconfig.RootfsMounts[0].Source, constants.RootfsMbindVirtioTag)
	// if err != nil {
	// 	return nil, errors.Errorf("creating rootfs	 virtio device: %w", err)
	// }

	// mfsBindMounts = append(mfsBindMounts, mfsBindMount{
	// 	Target: ctrconfig.RootfsMounts[0].Source,
	// 	Tag:    constants.RootfsMbindVirtioTag,
	// })

	// mfsBindString := ""
	// for _, mfsBind := range mfsBindMounts {
	// 	slog.InfoContext(ctx, "mfs bind", "tag", mfsBind.Tag, "target", mfsBind.Target)
	// 	mfsBindString += mfsBind.Tag + constants.MbindSeparator + mfsBind.Target + ","
	// }
	// mfsBindString = strings.TrimSuffix(mfsBindString, ",")

	// msockBindString := ""
	// for _, msockBind := range msockBindMounts {
	// 	slog.InfoContext(ctx, "msock bind", "destination", msockBind.Source, "port", msockBind.Port)
	// 	msockBindString += msockBind.Source + constants.MbindSeparator + strconv.Itoa(int(msockBind.Port)) + ","
	// }
	// msockBindString = strings.TrimSuffix(msockBindString, ",")

	// mbindDevices = append(mbindDevices, rootfsDev)
	// devices = append(devices, bundleDev)
	// devices = append(devices, mbindDevices...)
	// devices = append(devices, mountDevices...)

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

// func findOverlays(ctx context.Context, spec *oci.Spec, rootfsMounts []process.Mount) ([]proxyVirtioFs, error) {
// 	proxyDevices := []proxyVirtioFs{}

// 	for _, mount := range rootfsMounts {
// 		if mount.Type == "bind" || mount.Type == "rbind" {
// 			proxyDevices = append(proxyDevices, proxyVirtioFs{
// 				Target: mount.Source,
// 				Tag:    "overlay-" + quickHash(mount.Source),
// 			})
// 		}
// 	}

// 	return proxyDevices, nil
// }

type msockBindMount struct {
	Destination string
	Port        uint32
	Source      string
}

type mfsBindMount struct {
	Target  string
	Tag     string
	Options []string
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

// PrepareContainerVirtioDevicesFromRootfs creates virtio devices using an existing rootfs directory
func makeMConfigDevice(ctx context.Context, wrkdir string, ctrconfig *oci.Spec, rootfsMounts []process.Mount) (virtio.VirtioDevice, *mfsBindMount, error) {
	ec1DataPath := filepath.Join(wrkdir, "harpoon-runtime-fs-device")

	err := os.MkdirAll(ec1DataPath, 0755)
	if err != nil {
		return nil, nil, errors.Errorf("creating block device directory: %w", err)
	}

	specBytes, err := json.Marshal(ctrconfig)
	if err != nil {
		return nil, nil, errors.Errorf("marshalling spec: %w", err)
	}

	mountBytes, err := json.Marshal(rootfsMounts)
	if err != nil {
		return nil, nil, errors.Errorf("marshalling rootfs mounts: %w", err)
	}

	files := map[string][]byte{
		constants.ContainerSpecFile:         specBytes,
		constants.ContainerRootfsMountsFile: mountBytes,
	}

	for name, file := range files {
		filePath := filepath.Join(ec1DataPath, name)
		err = os.WriteFile(filePath, file, 0644)
		if err != nil {
			return nil, nil, errors.Errorf("writing file to block device: %w", err)
		}
	}

	device, err := virtio.VirtioFsNew(ec1DataPath, constants.Ec1VirtioTag)
	if err != nil {
		return nil, nil, errors.Errorf("creating ec1 virtio device: %w", err)
	}

	return device, &mfsBindMount{
		Target: constants.Ec1AbsPath,
		Tag:    constants.Ec1VirtioTag,
	}, nil
}
