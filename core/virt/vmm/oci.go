package vmm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containers/common/pkg/strongunits"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/runc/process"
	"github.com/walteh/runm/core/virt/host"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/units"
)

type OCIVMConfig struct {
	ID           string
	RootfsMounts []process.Mount
	// StderrWriter io.Writer
	// StdoutWriter io.Writer
	// StdinReader  io.Reader
	Spec           *oci.Spec
	StartingMemory strongunits.B
	VCPUs          uint64
	Platform       units.Platform
}

func appendContext(ctx context.Context, id string) context.Context {
	// var rlimit syscall.Rlimit
	// syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit)
	// groups, _ := syscall.Getgroups()

	return slogctx.Append(ctx,
		slog.String("vmid", id),
		// slog.String("pid", strconv.Itoa(syscall.Getpid())),
		// slog.String("ppid", strconv.Itoa(syscall.Getppid())),
		// slog.String("uid", strconv.Itoa(syscall.Getuid())),
		// slog.String("gid", strconv.Itoa(syscall.Getgid())),
		// slog.String("egid", strconv.Itoa(syscall.Getegid())),
		// slog.String("euid", strconv.Itoa(syscall.Geteuid())),
		// slog.String("page_size", strconv.Itoa(syscall.Getpagesize())),
		// slog.Any("groups", groups),
		// slog.String("pgrp", strconv.Itoa(syscall.Getpgrp())),
		// slog.String("id", id),
		// slog.String("pid", strconv.Itoa(syscall.Getpid())),
		// slog.String("ppid", strconv.Itoa(syscall.Getppid())),
		// slog.String("egid", strconv.Itoa(syscall.Getegid())),
		// slog.String("euid", strconv.Itoa(syscall.Geteuid())),
		// slog.String("page_size", strconv.Itoa(syscall.Getpagesize())),
		// slog.String("rlimit_cur", strconv.Itoa(int(rlimit.Cur))),
		// slog.String("rlimit_max", strconv.Itoa(int(rlimit.Max))),
		// slog.String("table_size", strconv.Itoa(syscall.Getdtablesize())),
	)
}

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

	bindMounts, mountDevices, err := PrepareContainerMounts(ctx, ctrconfig.Spec, ctrconfig.ID)
	if err != nil {
		return nil, errors.Errorf("preparing container mounts: %w", err)
	}

	mbinDev, _, err := NewMbinBlockDevice(ctx, workingDir)
	if err != nil {
		return nil, errors.Errorf("creating mbin block device: %w", err)
	}
	devices = append(devices, mbinDev)

	ec1Dev, _, err := NewEc1BlockDevice(ctx, workingDir)
	if err != nil {
		return nil, errors.Errorf("creating ec1 block device: %w", err)
	}

	devices = append(devices, ec1Dev)
	devices = append(devices, mountDevices...)

	slog.InfoContext(ctx, "about to set up rootfs",
		"ctrconfig.RootfsMounts", ctrconfig.RootfsMounts,
		"spec.Root.Path", ctrconfig.Spec.Root.Path,
		"spec.Root.Readonly", ctrconfig.Spec.Root.Readonly,
	)

	ec1Devices, err := PrepareContainerVirtioDevicesFromRootfs(ctx, workingDir, ctrconfig.Spec, ctrconfig.RootfsMounts, bindMounts, creationErrGroup)
	if err != nil {
		return nil, errors.Errorf("creating ec1 block device from rootfs: %w", err)
	}
	devices = append(devices, ec1Devices...)

	var bootloader virtio.Bootloader

	switch ctrconfig.Platform {
	case units.PlatformLinuxARM64:
		bootloader = &virtio.LinuxBootloader{
			InitrdPath:    filepath.Join(linuxRuntimeBuildDir, "initramfs.cpio.gz"),
			VmlinuzPath:   filepath.Join(linuxRuntimeBuildDir, "kernel"),
			KernelCmdLine: "console=hvc0 systemd.unified_cgroup_hierarchy=1 -- runm-mode=oci",
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
		start:        startTime,
		vm:           vm,
		portOnHostIP: hostIPPort,
		wait:         make(chan error, 1),
		runtime:      nil,
		workingDir:   workingDir,
		netdev:       netdev,
	}

	slog.InfoContext(ctx, "created oci vm", "id", ctrconfig.ID)

	return runner, nil
}

func bindMountToVirtioFs(ctx context.Context, mount specs.Mount, containerId string) (*specs.Mount, virtio.VirtioDevice, error) {
	fi, err := os.Stat(mount.Source)
	if err != nil {
		return nil, nil, errors.Errorf("statting source: %w", err)
	}

	if fi.IsDir() {
		hash := sha256.Sum256([]byte(mount.Source))
		tag := "bind-" + hex.EncodeToString(hash[:8])
		// create a new fs direcotry share
		shareDev, err := virtio.VirtioFsNew(mount.Source, tag)
		if err != nil {
			return nil, nil, errors.Errorf("creating share device: %w", err)
		}

		mount.Type = "virtiofs"
		mount.Source = tag
		mount.Options = []string{}

		return &mount, shareDev, nil

	} else {

		base := filepath.Base(mount.Source)
		dir := filepath.Base(filepath.Dir(mount.Source))

		if base == "resolv.conf" || base == "hosts" {
			if dir == containerId {
				slog.WarnContext(ctx, "skipping mount of file", "file", mount.Source)

				// mount.Type = "copy"
				return nil, nil, nil
			}
		}

		return nil, nil, errors.Errorf("unsupported bind mount file: %s", mount.Source)

		// this is probably a security issue, but for now d
	}

}

func PrepareContainerMounts(ctx context.Context, spec *oci.Spec, containerId string) ([]specs.Mount, []virtio.VirtioDevice, error) {
	bindMounts := []specs.Mount{}
	devices := []virtio.VirtioDevice{}

	// log all the mounts

	for _, mount := range spec.Mounts {
		if mount.Type == "" && slices.Contains(mount.Options, "rbind") {
			mount.Type = "rbind"
		}

		switch mount.Type {
		case "bind", "rbind":
			mnt, dev, err := bindMountToVirtioFs(ctx, mount, containerId)
			if err != nil {
				return nil, nil, errors.Errorf("binding mount to virtio fs: %w", err)
			}
			if mnt != nil {
				bindMounts = append(bindMounts, *mnt)
			}
			if dev != nil {
				devices = append(devices, dev)
			}
		case "tmpfs":
			if mount.Destination == "/dev" {
				bindMounts = append(bindMounts, specs.Mount{
					Type:        "devtmpfs",
					Source:      "devtmpfs",
					Destination: mount.Destination,
					Options:     mount.Options,
					UIDMappings: mount.UIDMappings,
					GIDMappings: mount.GIDMappings,
				})
			} else {
				bindMounts = append(bindMounts, mount)
			}
		default:
			bindMounts = append(bindMounts, mount)
		}
	}

	return bindMounts, devices, nil
}

// PrepareContainerVirtioDevicesFromRootfs creates virtio devices using an existing rootfs directory
func PrepareContainerVirtioDevicesFromRootfs(ctx context.Context, wrkdir string, ctrconfig *oci.Spec, rootfsMounts []process.Mount, bindMounts []specs.Mount, wg *errgroup.Group) ([]virtio.VirtioDevice, error) {
	outMounts := []specs.Mount{}
	ec1DataPath := filepath.Join(wrkdir, "harpoon-runtime-fs-device")

	devices := []virtio.VirtioDevice{}

	err := os.MkdirAll(ec1DataPath, 0755)
	if err != nil {
		return nil, errors.Errorf("creating block device directory: %w", err)
	}

	if len(rootfsMounts) != 1 {
		return nil, errors.Errorf("expected 1 rootfs mount, got %d", len(rootfsMounts))
	}

	rootfsMount := rootfsMounts[0]

	// i think the prob is that ctrconfig.Root.Path is set to 'rootfs'
	// Create a VirtioFs device pointing to the existing rootfs directory
	blkDev, err := virtio.VirtioFsNew(rootfsMount.Source, constants.RootfsVirtioTag)
	if err != nil {
		return nil, errors.Errorf("creating rootfs virtio device: %w", err)
	}

	outMounts = append(outMounts, specs.Mount{
		Type:        "virtiofs",
		Source:      constants.RootfsVirtioTag,
		Destination: "", // the root
		Options: slices.DeleteFunc(rootfsMount.Options, func(opt string) bool {
			return opt == "rbind" || opt == "bind"
		}),
	})

	// consoleAttachment := virtio.NewFileHandleDeviceAttachment(os.NewFile(uintptr(ctrconfig.StdinFD), "ptymaster"), virtio.DeviceSerial)
	// consoleConfig.SetAttachment(consoleAttachment)

	devices = append(devices, blkDev)

	specBytes, err := json.Marshal(ctrconfig)
	if err != nil {
		return nil, errors.Errorf("marshalling spec: %w", err)
	}

	// outMounts = append(outMounts, specs.Mount{
	// 	Type:        "virtiofs",
	// 	Source:      constants.Ec1VirtioTag,
	// 	Destination: constants.Ec1AbsPath,
	// 	Options:     []string{},
	// })

	outMounts = append(outMounts, bindMounts...)

	mountsBytes, err := json.Marshal(outMounts)
	if err != nil {
		return nil, errors.Errorf("marshalling mounts: %w", err)
	}

	files := map[string][]byte{
		constants.ContainerSpecFile:   specBytes,
		constants.ContainerMountsFile: mountsBytes,
	}

	for name, file := range files {
		filePath := filepath.Join(ec1DataPath, name)
		err = os.WriteFile(filePath, file, 0644)
		if err != nil {
			return nil, errors.Errorf("writing file to block device: %w", err)
		}
	}

	timesyncFile := filepath.Join(ec1DataPath, "timesync")

	_, zoneoffset := time.Now().Zone()

	wg.Go(func() error {
		timez := strconv.Itoa(int(time.Now().UnixNano()))
		timez += ":" + strconv.Itoa(zoneoffset)
		// write once
		err := os.WriteFile(timesyncFile, []byte(timez), 0644)
		if err != nil {
			slog.ErrorContext(ctx, "error writing timesync file", "error", err)
		}
		return nil
	})

	timeout := time.NewTimer(1 * time.Second)

	// create a temporary timesync file
	go func() {

		for {
			select {
			case <-timeout.C:
				err := os.WriteFile(timesyncFile, []byte("done"), 0644)
				if err != nil {
					slog.ErrorContext(ctx, "error writing timesync file", "error", err)
				}
				return
			default:
				timez := strconv.Itoa(int(time.Now().UnixNano()))
				timez += ":" + strconv.Itoa(zoneoffset)
				// slog.InfoContext(ctx, "writing timesync file", "time", timez)
				err := os.WriteFile(timesyncFile, []byte(timez), 0644)
				if err != nil {
					slog.ErrorContext(ctx, "error writing timesync file", "error", err)
				}
			}
		}
	}()

	// ec1Dev, err := virtio.VirtioFsNew(ec1DataPath, constants.Ec1VirtioTag)
	// if err != nil {
	// 	return nil, errors.Errorf("creating ec1 virtio device: %w", err)
	// }

	// devices = append(devices, ec1Dev)

	return devices, nil
}
