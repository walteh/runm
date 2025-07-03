//go:build darwin && arm64

package vf

import (
	"fmt"
	"runtime"

	"github.com/Code-Hex/vz/v3"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/virt/host"
	"github.com/walteh/runm/core/virt/virtio"
)

func toVzLinuxBootloader(bootloader *virtio.LinuxBootloader) (vz.BootLoader, error) {
	if runtime.GOARCH == "arm64" {
		uncompressed, err := host.IsKernelUncompressed(bootloader.VmlinuzPath)
		if err != nil {
			return nil, err
		}
		if !uncompressed {
			return nil, fmt.Errorf("kernel must be uncompressed, %s is a compressed file", bootloader.VmlinuzPath)
		}
	}

	opts := []vz.LinuxBootLoaderOption{}
	if bootloader.InitrdPath != "" {
		opts = append(opts, vz.WithInitrd(bootloader.InitrdPath))
	}
	if bootloader.KernelCmdLine != "" {
		opts = append(opts, vz.WithCommandLine(bootloader.KernelCmdLine))
	}

	return vz.NewLinuxBootLoader(
		bootloader.VmlinuzPath,
		opts...,
	)
}

func toVzEFIBootloader(bootloader *virtio.EFIBootloader) (vz.BootLoader, error) {
	var efiVariableStore *vz.EFIVariableStore
	var err error

	if bootloader.CreateVariableStore {
		efiVariableStore, err = vz.NewEFIVariableStore(bootloader.EFIVariableStorePath, vz.WithCreatingEFIVariableStore())
	} else {
		efiVariableStore, err = vz.NewEFIVariableStore(bootloader.EFIVariableStorePath)
	}
	if err != nil {
		return nil, err
	}

	return vz.NewEFIBootLoader(
		vz.WithEFIVariableStore(efiVariableStore),
	)
}

func toVzBootloader(bl virtio.Bootloader) (vz.BootLoader, error) {

	switch b := bl.(type) {
	case *virtio.LinuxBootloader:
		return toVzLinuxBootloader(b)
	case *virtio.EFIBootloader:
		return toVzEFIBootloader(b)
	case *virtio.MacOSBootloader:
		return toVzMacOSBootloader(b)
	default:
		return nil, fmt.Errorf("Unexpected bootloader type: %T", b)
	}
}

func toVzPlatformConfiguration(bl virtio.Bootloader) (vz.PlatformConfiguration, error) {

	switch b := bl.(type) {
	case *virtio.LinuxBootloader, *virtio.EFIBootloader:
		return vz.NewGenericPlatformConfiguration()
	case *virtio.MacOSBootloader:
		platformConfig, err := NewMacPlatformConfiguration(b.MachineIdentifierPath, b.HardwareModelPath, b.AuxImagePath)
		if err != nil {
			return nil, errors.Errorf("creating macos platform configuration: %w", err)
		}
		return platformConfig, nil
	default:
		return nil, fmt.Errorf("Unexpected bootloader type: %T", b)
	}
}
