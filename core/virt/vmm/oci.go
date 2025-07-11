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
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containers/common/pkg/strongunits"
	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/runc/process"
	"github.com/walteh/runm/core/virt/host"
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
	HostOtlpPort   uint32
	RawWriter      io.Writer
	DelimWriter    io.Writer
}

func appendContext(ctx context.Context, id string) context.Context {
	return slogctx.Append(ctx,
		slog.String("vmid", id),
	)
}

// devices:
// - mbin (mbin.squashfs)
// - bundle virtio fs (bundle.squashfs)

// NewContainerizedVirtualMachineFromRootfs creates a VM using an already-prepared rootfs directory
// This is used by container runtimes like containerd that have already prepared the rootfs
func NewOCIVirtualMachine[VM VirtualMachine](
	ctx context.Context,
	hpv Hypervisor[VM],
	ctrconfig OCIVMConfig,
	devices ...virtio.VirtioDevice) (*RunningVM[VM], error) {

	id := "vm-oci-" + ctrconfig.ID[:8]
	creationErrGroup, ctx := errgroup.WithContext(ctx)

	ctx = appendContext(ctx, id)

	startTime := time.Now()

	linuxRuntimeBuildDir := os.Getenv("LINUX_RUNTIME_BUILD_DIR")
	if linuxRuntimeBuildDir == "" {
		return nil, errors.New("LINUX_RUNTIME_BUILD_DIR is not set")
	}

	workingDir, err := host.EmphiricalVMCacheDir(ctx, id)
	if err != nil {
		return nil, err
	}

	// copy the build dir to the working dir
	err = os.MkdirAll(filepath.Join(workingDir, "build"), 0755)
	if err != nil {
		return nil, errors.Errorf("creating build directory: %w", err)
	}

	if err = os.CopyFS(workingDir, os.DirFS(linuxRuntimeBuildDir)); err != nil {
		return nil, errors.Errorf("copying build directory: %w", err)
	}

	mbinDev, _, err := NewMbinBlockDevice(ctx, workingDir)
	if err != nil {
		return nil, errors.Errorf("creating mbin block device: %w", err)
	}

	devices = append(devices, mbinDev)

	bundleDev, err := virtio.VirtioFsNew(ctrconfig.Bundle, constants.BundleVirtioTag)
	if err != nil {
		return nil, errors.Errorf("creating bundle virtio device: %w", err)
	}

	tzDev, err := virtio.VirtioFsNew("/usr/share/zoneinfo", constants.ZoneInfoVirtioTag)
	if err != nil {
		return nil, errors.Errorf("creating zoneinfo virtio device: %w", err)
	}
	devices = append(devices, tzDev)

	mbindDevices, proxyDevices, err := findMbindDevices(ctx, ctrconfig.Spec, ctrconfig.RootfsMounts)
	if err != nil {
		return nil, errors.Errorf("finding mbind devices: %w", err)
	}

	ec1Dev, ec1Proxy, err := makeEc1BlockDevice(ctx, workingDir, ctrconfig.Spec, ctrconfig.RootfsMounts)
	if err != nil {
		return nil, errors.Errorf("creating ec1 block device: %w", err)
	}
	devices = append(devices, ec1Dev)
	proxyDevices = append(proxyDevices, *ec1Proxy)

	proxyDevices = append(proxyDevices, proxyVirtioFs{
		Target: ctrconfig.Bundle,
		Tag:    constants.BundleVirtioTag,
	})

	proxyMountString := ""
	for _, proxyDevice := range proxyDevices {
		slog.InfoContext(ctx, "proxy device", "tag", proxyDevice.Tag, "target", proxyDevice.Target)
		proxyMountString += proxyDevice.Tag + ":" + proxyDevice.Target + ","
	}

	proxyMountString = strings.TrimSuffix(proxyMountString, ",")

	devices = append(devices, bundleDev)
	devices = append(devices, mbindDevices...)
	// devices = append(devices, mountDevices...)

	slog.InfoContext(ctx, "about to set up rootfs",
		"ctrconfig.RootfsMounts", ctrconfig.RootfsMounts,
		"spec.Root.Path", ctrconfig.Spec.Root.Path,
		"spec.Root.Readonly", ctrconfig.Spec.Root.Readonly,
	)

	// ec1Devices, err := PrepareContainerVirtioDevicesFromRootfs(ctx, workingDir, ctrconfig.Spec, ctrconfig.RootfsMounts, bindMounts, creationErrGroup)
	// if err != nil {
	// 	return nil, errors.Errorf("creating ec1 block device from rootfs: %w", err)
	// }
	// devices = append(devices, ec1Devices...)

	var bootloader virtio.Bootloader

	var otelString string = ""

	if ctrconfig.HostOtlpPort != 0 {
		otelString = "-enable-otlp"
	}

	localTimeRef, err := os.Readlink("/etc/localtime")
	if err != nil {
		return nil, errors.Errorf("reading localtime: %w", err)
	}

	zisplit := strings.Split(localTimeRef, "zoneinfo/")
	if len(zisplit) != 2 {
		return nil, errors.Errorf("invalid localtime reference: %s", localTimeRef)
	}

	loc, err := time.LoadLocation(zisplit[1])
	if err != nil {
		return nil, errors.Errorf("loading location: %w", err)
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
			"-mbinds=" + proxyMountString,
			"-timezone=" + loc.String(),
		}

		bootloader = &virtio.LinuxBootloader{
			InitrdPath:    filepath.Join(linuxRuntimeBuildDir, "initramfs.cpio.gz"),
			VmlinuzPath:   filepath.Join(linuxRuntimeBuildDir, "kernel"),
			KernelCmdLine: strings.Join(cfgs, " "),
		}
	default:
		return nil, errors.Errorf("unsupported OS: %s", ctrconfig.Platform.OS())
	}

	if ctrconfig.Spec.Process.Terminal {
		return nil, errors.New("terminal support is not implemented yet")
	} else {
		// setup a log
		devices = append(devices, &virtio.VirtioSerialLogFile{
			Path:   filepath.Join(workingDir, "console.log"),
			Append: false,
		})

	}

	// add vsock and memory devices

	netdev, hostIPPort, err := PrepareVirtualNetwork(ctx)
	if err != nil {
		return nil, errors.Errorf("creating net device: %w", err)
	}
	devices = append(devices, netdev.VirtioNetDevice())
	devices = append(devices, &virtio.VirtioVsock{})
	devices = append(devices, &virtio.VirtioBalloon{})
	devices = append(devices, &virtio.VirtioRng{})

	opts := NewVMOptions{
		Vcpus:         ctrconfig.VCPUs,
		Memory:        ctrconfig.StartingMemory,
		Devices:       devices,
		GuestPlatform: ctrconfig.Platform,
	}

	waitStart := time.Now()

	err = creationErrGroup.Wait()
	if err != nil {
		return nil, errors.Errorf("error waiting for errgroup: %w", err)
	}

	slog.InfoContext(ctx, "ready to create vm", "async_wait_duration", time.Since(waitStart))

	vm, err := hpv.NewVirtualMachine(ctx, id, &opts, bootloader)
	if err != nil {
		return nil, errors.Errorf("creating virtual machine: %w", err)
	}

	runner := &RunningVM[VM]{
		bootloader:   bootloader,
		hostOtlpPort: ctrconfig.HostOtlpPort,
		start:        startTime,
		vm:           vm,
		portOnHostIP: hostIPPort,
		wait:         make(chan error, 1),
		workingDir:   workingDir,
		netdev:       netdev,
		rawWriter:    ctrconfig.RawWriter,
		delimWriter:  ctrconfig.DelimWriter,
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

type proxyVirtioFs struct {
	Target string
	Tag    string
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

func findMbindDevices(ctx context.Context, spec *oci.Spec, rootfsMounts []process.Mount) ([]virtio.VirtioDevice, []proxyVirtioFs, error) {
	devices := []virtio.VirtioDevice{}

	proxyDevices := []proxyVirtioFs{}

	seen := map[string]bool{}

	for _, mount := range spec.Mounts {
		if mount.Type != "bind" && mount.Type != "rbind" {
			continue
		}

		src := mount.Source
		// if its a dir, update the source to be the dir
		if fi, err := os.Stat(src); err == nil && !fi.IsDir() {
			src = filepath.Dir(src)
		}

		if seen[src] {
			continue
		}

		tag := "mbind-" + quickHash(src)

		proxyDevices = append(proxyDevices, proxyVirtioFs{
			Target: src,
			Tag:    tag,
		})

		shareDev, err := virtio.VirtioFsNew(src, tag)
		if err != nil {
			return nil, nil, errors.Errorf("creating share device: %w", err)
		}

		devices = append(devices, shareDev)

		seen[src] = true
	}

	for _, mount := range rootfsMounts {
		shareDev, err := virtio.VirtioFsNew(mount.Source, "rootfs-"+quickHash(mount.Source))
		if err != nil {
			return nil, nil, errors.Errorf("creating share device: %w", err)
		}

		devices = append(devices, shareDev)
		proxyDevices = append(proxyDevices, proxyVirtioFs{
			Target: mount.Source,
			Tag:    "rootfs-" + quickHash(mount.Source),
		})
	}

	slog.InfoContext(ctx, "found mbind devices", "proxyDevices", proxyDevices)

	return devices, proxyDevices, nil
}

// PrepareContainerVirtioDevicesFromRootfs creates virtio devices using an existing rootfs directory
func makeEc1BlockDevice(ctx context.Context, wrkdir string, ctrconfig *oci.Spec, rootfsMounts []process.Mount) (virtio.VirtioDevice, *proxyVirtioFs, error) {
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

	return device, &proxyVirtioFs{
		Target: constants.Ec1AbsPath,
		Tag:    constants.Ec1VirtioTag,
	}, nil
}
