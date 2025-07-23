//go:build !windows

package vmm

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/containers/common/pkg/strongunits"
	"github.com/rs/xid"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/virt/host"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/units"
)

type EmptyVMConfig struct {
	StartingMemory strongunits.B
	VCPUs          uint64
	Platform       units.Platform
	RawWriter      io.Writer
	DelimWriter    io.Writer
}

// NewContainerizedVirtualMachineFromRootfs creates a VM using an already-prepared rootfs directory
// This is used by container runtimes like containerd that have already prepared the rootfs
func NewKernelCommandVM[VM VirtualMachine](
	ctx context.Context,
	hpv Hypervisor[VM],
	ctrconfig *EmptyVMConfig) (*RunningVM[VM], error) {

	if ctrconfig.RawWriter == nil {
		ctrconfig.RawWriter = logging.GetDefaultRawWriter()
	}
	if ctrconfig.DelimWriter == nil {
		ctrconfig.DelimWriter = logging.GetDefaultDelimWriter()
	}

	id := "vm-empty-" + xid.New().String()
	creationErrGroup, ctx := errgroup.WithContext(ctx)

	ctx = appendContext(ctx, id)

	startTime := time.Now()

	workingDir, err := host.EmphiricalVMCacheDir(ctx, id)
	if err != nil {
		return nil, err
	}

	err = os.MkdirAll(workingDir, 0755)
	if err != nil {
		return nil, errors.Errorf("creating working directory: %w", err)
	}

	ec1Dev, _, err := NewEc1BlockDevice(ctx, workingDir)
	if err != nil {
		return nil, errors.Errorf("creating ec1 block device: %w", err)
	}

	linuxRuntimeBuildDir := os.Getenv("LINUX_RUNTIME_BUILD_DIR")
	if linuxRuntimeBuildDir == "" {
		return nil, errors.New("LINUX_RUNTIME_BUILD_DIR is not set")
	}

	var bootloader virtio.Bootloader

	devices := []virtio.VirtioDevice{
		ec1Dev,
	}

	switch ctrconfig.Platform {
	case units.PlatformLinuxARM64:
		bootloader = &virtio.LinuxBootloader{
			InitrdPath:    filepath.Join(linuxRuntimeBuildDir, "initramfs.cpio.gz"),
			VmlinuzPath:   filepath.Join(linuxRuntimeBuildDir, "kernel"),
			KernelCmdLine: "console=hvc0 -- runm-mode=empty",
		}
	default:
		return nil, errors.Errorf("unsupported OS: %s", ctrconfig.Platform.OS())
	}
	// setup a log
	devices = append(devices, &virtio.VirtioSerialLogFile{
		Path:   filepath.Join(workingDir, "console.log"),
		Append: false,
	})

	// add vsock and memory devices

	netdev, hostIPPort, err := PrepareVirtualNetwork(ctx, nil)
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
		workingDir:   workingDir,
		netdev:       netdev,
		rawWriter:    ctrconfig.RawWriter,
		delimWriter:  ctrconfig.DelimWriter,
	}

	return runner, nil
}
