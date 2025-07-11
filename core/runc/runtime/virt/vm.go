//go:build !windows

package virt

import (
	"context"
	"log/slog"
	"time"

	"github.com/containers/common/pkg/strongunits"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/walteh/run"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"

	"github.com/walteh/runm/core/runc/oom"
	"github.com/walteh/runm/core/runc/runtime"
	grpcruntime "github.com/walteh/runm/core/runc/runtime/grpc"
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

	grpcConn   *grpc.ClientConn
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
		VCPUs:          2,
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

	slog.InfoContext(ctx, "created oci vm, starting it", "id", vm.VM().ID(), "rawWriter==nil", cfg.RawWriter == nil, "delimWriter==nil", cfg.DelimWriter == nil)

	if err := vm.Start(ctx); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "started vm, connecting to guest service", "id", vm.VM().ID())

	if ctx.Err() != nil {
		slog.ErrorContext(ctx, "context done before creating VM runtime")
		return nil, ctx.Err()
	}

	srv, err := vm.GuestVsockConn(ctx)
	if err != nil {
		return nil, errors.Errorf("getting guest vsock conn: %w", err)
	}

	rsrv, err := grpcruntime.NewGRPCClientRuntimeFromConn(srv)
	if err != nil {
		return nil, errors.Errorf("creating grpc client runtime: %w", err)
	}

	rsrv.SetVsockProxier(vm)

	slog.InfoContext(ctx, "connected to guest service", "id", vm.VM().ID())

	ep := oom.NewWatcher(opts.Publisher, rsrv)

	slog.InfoContext(ctx, "created oom watcher", "id", vm.VM().ID())

	runGroup.Always(ep)

	return &RunmVMRuntime[VM]{
		vm:              vm,
		oomWatcher:      ep,
		spec:            cfg.Spec,
		Runtime:         rsrv,
		RuntimeExtras:   rsrv,
		CgroupAdapter:   rsrv,
		EventHandler:    rsrv,
		closers:         closers,
		GuestManagement: rsrv,
		runGroup:        runGroup,
	}, nil
}

// Alive implements run.Runnable.
func (r *RunmVMRuntime[VM]) Alive() bool {
	return r.vm.VM().CurrentState() == vmm.VirtualMachineStateTypeRunning
}

// Close implements run.Runnable.
func (r *RunmVMRuntime[VM]) Close(ctx context.Context) error {
	slog.InfoContext(ctx, "about to close vm")
	ticker := time.NewTicker(100 * time.Microsecond)

	tickstart := make(chan struct{})
	go func() {
		close(tickstart)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				slog.DebugContext(ctx, "closing vm")
			case <-ctx.Done():
				slog.DebugContext(ctx, "context done, stopping ticker")
				return
			}
		}
	}()

	<-tickstart

	err := r.Runtime.Close(ctx)
	slog.DebugContext(ctx, "closed runtime", "err", err)

	// err = r.grpcConn.Close()
	// slog.DebugContext(ctx, "closed grpc conn", "err", err)

	err = r.vm.Close(ctx)
	slog.InfoContext(ctx, "closed vm", "err", err)
	return nil
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
