package virt

import (
	"context"
	"errors"
	"log/slog"

	"github.com/containers/common/pkg/strongunits"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/walteh/run"

	"github.com/walteh/runm/core/runc/oom"
	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/virt/vmm"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/units"
)

var (
	_ runtime.Runtime         = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ runtime.RuntimeExtras   = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ runtime.CgroupAdapter   = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ runtime.EventHandler    = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ runtime.GuestManagement = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ run.Runnable            = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
)

type RunmVMRuntime[VM vmm.VirtualMachine] struct {
	runtime.Runtime
	runtime.RuntimeExtras
	runtime.CgroupAdapter
	runtime.EventHandler
	runtime.GuestManagement

	spec       *specs.Spec
	vm         *vmm.RunningVM[VM]
	oomWatcher *oom.Watcher

	closers []func() error

	runGroup *run.Group
}

func NewRunmVMRuntime[VM vmm.VirtualMachine](
	ctx context.Context,
	hpv vmm.Hypervisor[VM],
	opts *runtime.RuntimeOptions,
	maxMemory strongunits.StorageUnits,
	vcpus int,
) (*RunmVMRuntime[VM], error) {

	runGroup := run.New()

	closers := []func() error{}

	cfg := vmm.OCIVMConfig{
		ID:             opts.ProcessCreateConfig.ID,
		Spec:           opts.OciSpec,
		RootfsMounts:   opts.Mounts,
		StartingMemory: maxMemory.ToBytes(),
		VCPUs:          1,
		Platform:       units.PlatformLinuxARM64,
		Bundle:         opts.Bundle,
		HostOtlpPort:   opts.HostOtlpPort,
		RawWriter:      logging.GetDefaultRawWriter(),
		DelimWriter:    logging.GetDefaultDelimWriter(),
	}

	vm, err := vmm.NewOCIVirtualMachine(ctx, hpv, cfg)
	if err != nil {
		return nil, err
	}

	closers = append(closers, func() error {
		return vm.Close(ctx)
	})

	slog.InfoContext(ctx, "created oci vm, starting it", "id", vm.VM().ID(), "rawWriter==nil", cfg.RawWriter == nil, "delimWriter==nil", cfg.DelimWriter == nil)

	if err := vm.Start(ctx); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "started vm, connecting to guest service", "id", vm.VM().ID())

	if ctx.Err() != nil {
		slog.ErrorContext(ctx, "context done before creating VM runtime")
		return nil, ctx.Err()
	}

	srv, err := vm.GuestService(ctx)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "connected to guest service", "id", vm.VM().ID())

	ep := oom.NewWatcher(opts.Publisher, srv)

	slog.InfoContext(ctx, "created oom watcher", "id", vm.VM().ID())

	runGroup.Always(ep)

	return &RunmVMRuntime[VM]{
		vm:              vm,
		oomWatcher:      ep,
		spec:            cfg.Spec,
		Runtime:         srv,
		RuntimeExtras:   srv,
		CgroupAdapter:   srv,
		EventHandler:    srv,
		closers:         closers,
		GuestManagement: srv,
		runGroup:        runGroup,
	}, nil
}

// Alive implements run.Runnable.
func (r *RunmVMRuntime[VM]) Alive() bool {
	return r.vm.VM().CurrentState() == vmm.VirtualMachineStateTypeRunning
}

// Close implements run.Runnable.
func (r *RunmVMRuntime[VM]) Close(ctx context.Context) error {
	errs := []error{}
	for _, closer := range r.closers {
		if err := closer(); err != nil {
			slog.ErrorContext(ctx, "error closing vm", "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Fields implements run.Runnable.
func (r *RunmVMRuntime[VM]) Fields() []slog.Attr {
	return []slog.Attr{
		slog.String("id", r.vm.VM().ID()),
	}
}

// Name implements run.Runnable.
func (r *RunmVMRuntime[VM]) Name() string {
	return r.vm.VM().ID()
}

// Run implements run.Runnable.
// Subtle: this method shadows the method (RuntimeExtras).Run of RunmVMRuntime.RuntimeExtras.
func (r *RunmVMRuntime[VM]) Run(ctx context.Context) error {
	slog.InfoContext(ctx, "running vm", "id", r.vm.VM().ID())

	return r.runGroup.RunContext(ctx)
}
