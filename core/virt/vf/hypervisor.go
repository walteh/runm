package vf

import (
	"context"
	"log/slog"
	"sync"

	"github.com/Code-Hex/vz/v3"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/core/virt/vmm"
)

func NewHypervisor() vmm.Hypervisor[*VirtualMachine] {
	return &Hypervisor{
		vms:    make(map[string]*VirtualMachine),
		notify: make(chan *VirtualMachine),
	}
}

var _ vmm.Hypervisor[*VirtualMachine] = &Hypervisor{}

type Hypervisor struct {
	vms    map[string]*VirtualMachine
	mu     sync.Mutex
	notify chan *VirtualMachine
}

func (hpv *Hypervisor) NewVirtualMachine(ctx context.Context, id string, opts *vmm.NewVMOptions, bl virtio.Bootloader) (*VirtualMachine, error) {
	// drop privileges on the virtual machine

	// Add extensive logging and validation before VZ calls
	slog.InfoContext(ctx, "NewVirtualMachine: Starting VM creation",
		"id", id,
		"vcpus", opts.Vcpus,
		"memory_bytes", opts.Memory.ToBytes(),
		"num_devices", len(opts.Devices))

	cfg, vzbl, err := hpv.buildConfig(ctx, opts, bl)
	if err != nil {
		slog.ErrorContext(ctx, "NewVirtualMachine: Failed to build config", "error", err)
		return nil, err
	}

	slog.InfoContext(ctx, "NewVirtualMachine: Config built successfully")

	// Validate configuration before applying devices
	if cfg == nil {
		return nil, errors.Errorf("VM configuration is nil")
	}

	slog.InfoContext(ctx, "validating vz virtual machine configuration")

	applier, err := NewVzVirtioDeviceApplier(ctx, cfg, bl)
	if err != nil {
		slog.ErrorContext(ctx, "NewVirtualMachine: Failed to create virtio device applier", "error", err)
		return nil, errors.Errorf("creating vz virtio device applier: %w", err)
	}

	slog.InfoContext(ctx, "NewVirtualMachine: Applying virtio devices", "num_devices", len(opts.Devices))

	if err := virtio.ApplyDevices(ctx, applier, opts.Devices); err != nil {
		slog.ErrorContext(ctx, "NewVirtualMachine: Failed to apply virtio devices", "error", err)
		return nil, errors.Errorf("applying virtio devices: %w", err)
	}

	// Defer a recovery function in case VZ crashes
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "FATAL: vz.NewVirtualMachine caused panic", "panic", r)
		}
	}()

	vzVM, err := vz.NewVirtualMachine(cfg)
	if err != nil {
		slog.ErrorContext(ctx, "NewVirtualMachine: vz.NewVirtualMachine failed", "error", err)
		return nil, errors.Errorf("creating vz virtual machine: %w", err)
	}

	slog.InfoContext(ctx, "NewVirtualMachine: vz.NewVirtualMachine completed successfully")

	vm := &VirtualMachine{
		id:            id,
		bootLoader:    vzbl,
		configuration: cfg,
		vzvm:          vzVM,
		opts:          opts,
	}

	hpv.mu.Lock()
	hpv.vms[id] = vm
	hpv.mu.Unlock()

	slog.DebugContext(ctx, "notifying hypervisor", "vm", vm)
	go func() {
		hpv.notify <- vm
	}()

	slog.DebugContext(ctx, "returning vm", "vm", vm)
	return vm, nil
}

func (hpv *Hypervisor) OnCreate() <-chan *VirtualMachine {
	return hpv.notify
}

// func (hpv *Hypervisor) ListenNetworkBlockDevices(ctx context.Context, vm vmm.VirtualMachine) error {
// 	panic("not implemented")
// }

func (hpv *Hypervisor) buildConfig(ctx context.Context, opts *vmm.NewVMOptions, bl virtio.Bootloader) (*vz.VirtualMachineConfiguration, vz.BootLoader, error) {
	slog.DebugContext(ctx, "Creating virtual machine configuration")

	// Validate inputs before calling VZ
	if opts == nil {
		return nil, nil, errors.Errorf("VM options are nil")
	}

	if opts.Vcpus == 0 {
		return nil, nil, errors.Errorf("VCPU count cannot be 0")
	}

	if opts.Vcpus > 64 {
		return nil, nil, errors.Errorf("VCPU count %d exceeds maximum of 64", opts.Vcpus)
	}

	memoryBytes := opts.Memory.ToBytes()
	if memoryBytes == 0 {
		return nil, nil, errors.Errorf("Memory cannot be 0")
	}

	// // Check for reasonable memory limits (VZ has specific requirements)
	// const minMemory = 512 * 1024 * 1024       // 512MB minimum
	// const maxMemory = 64 * 1024 * 1024 * 1024 // 64GB maximum
	// if memoryBytes < minMemory {
	// 	return nil, nil, errors.Errorf("Memory %d bytes is below minimum %d bytes (512MB)", memoryBytes, minMemory)
	// }
	// if memoryBytes > maxMemory {
	// 	return nil, nil, errors.Errorf("Memory %d bytes exceeds maximum %d bytes (64GB)", memoryBytes, maxMemory)
	// }

	slog.InfoContext(ctx, "buildConfig: VM parameters validated",
		"vcpus", opts.Vcpus,
		"memory_bytes", memoryBytes,
		"memory_mb", memoryBytes/(1024*1024))

	vzBootloader, err := toVzBootloader(bl)
	if err != nil {
		return nil, nil, errors.Errorf("converting bootloader to vz bootloader: %w", err)
	}

	if vzBootloader == nil {
		return nil, nil, errors.Errorf("VZ bootloader is nil after conversion")
	}

	slog.InfoContext(ctx, "buildConfig: Calling vz.NewVirtualMachineConfiguration",
		"vcpus", uint(opts.Vcpus),
		"memory_bytes", uint64(memoryBytes))

	vzVMConfig, err := vz.NewVirtualMachineConfiguration(vzBootloader, uint(opts.Vcpus), uint64(memoryBytes))
	if err != nil {
		slog.ErrorContext(ctx, "buildConfig: vz.NewVirtualMachineConfiguration failed",
			"error", err,
			"vcpus", uint(opts.Vcpus),
			"memory_bytes", uint64(memoryBytes))
		return nil, nil, errors.Errorf("creating vz virtual machine configuration: %w", err)
	}

	slog.InfoContext(ctx, "buildConfig: vz.NewVirtualMachineConfiguration succeeded")
	return vzVMConfig, vzBootloader, nil
}
