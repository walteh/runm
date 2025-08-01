//go:build !windows

package virt

import (
	"context"
	"log/slog"

	"github.com/containerd/containerd/v2/core/events"
	"github.com/containers/common/pkg/strongunits"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"

	"github.com/walteh/runm/core/runc/oom"
	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/virt/vmm"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/units"

	grpcruntime "github.com/walteh/runm/core/runc/runtime/grpc"
)

var (
	_ runtime.Runtime       = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ runtime.RuntimeExtras = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ runtime.CgroupAdapter = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	_ runtime.EventHandler  = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
	// _ run.Runnable            = (*RunmVMRuntime[vmm.VirtualMachine])(nil)
)

type RunmVMRuntime[VM vmm.VirtualMachine] struct {
	runtime.Runtime
	runtime.RuntimeExtras
	runtime.CgroupAdapter
	runtime.EventHandler

	grpcConn       *grpc.ClientConn
	spec           *specs.Spec
	vm             *vmm.RunningVM[VM]
	eventPublisher events.Publisher

	closers []func() error
}

func NewRunmVMRuntime[VM vmm.VirtualMachine](
	ctx context.Context,
	hpv vmm.Hypervisor[VM],
	opts *runtime.RuntimeOptions,
	maxMemory strongunits.StorageUnits,
	vcpus int,
) (*RunmVMRuntime[VM], error) {

	closers := []func() error{}

	cfg := vmm.OCIVMConfig{
		ID:             opts.ProcessCreateConfig.ID,
		Spec:           opts.OciSpec,
		RootfsMounts:   opts.Mounts,
		StartingMemory: maxMemory.ToBytes(),
		// VCPUs:          uint64(opts.OciSpec.Linux.Resources.CPU.Cpus),
		VCPUs:       uint64(vcpus),
		Platform:    units.PlatformLinuxARM64,
		Bundle:      opts.Bundle,
		RawWriter:   logging.GetDefaultRawWriter(),
		DelimWriter: logging.GetDefaultDelimWriter(),
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

	vm.TaskGroup().RegisterCleanupWithName("vm-grpc-client-conn-closer", func(ctx context.Context) error {
		return srv.Close()
	})

	rsrv, err := grpcruntime.NewGRPCClientRuntimeFromConn(srv)
	if err != nil {
		return nil, errors.Errorf("creating grpc client runtime: %w", err)
	}

	vm.TaskGroup().RegisterCleanupWithName("vm-grpc-client-runtime-closer", func(ctx context.Context) error {
		return rsrv.Close(ctx)
	})

	rsrv.SetVsockProxier(vm)

	slog.InfoContext(ctx, "connected to guest service", "id", vm.VM().ID())

	vm.TaskGroup().GoWithName("oom-watcher", func(ctx context.Context) error {
		return oom.RunOOMWatcher(ctx, opts.Publisher, rsrv)
	})

	return &RunmVMRuntime[VM]{
		vm:             vm,
		spec:           cfg.Spec,
		Runtime:        rsrv,
		RuntimeExtras:  rsrv,
		CgroupAdapter:  rsrv,
		EventHandler:   rsrv,
		closers:        closers,
		eventPublisher: opts.Publisher,
	}, nil
}

// // Alive implements run.Runnable.
// func (r *RunmVMRuntime[VM]) Alive() bool {
// 	return r.vm.VM().CurrentState() == vmm.VirtualMachineStateTypeRunning
// }

// // Close implements run.Runnable.
// func (r *RunmVMRuntime[VM]) Close(ctx context.Context) error {

// 	err := r.Runtime.Close(ctx)
// 	slog.DebugContext(ctx, "closed runtime", "err", err)

// 	// err = r.grpcConn.Close()
// 	// slog.DebugContext(ctx, "closed grpc conn", "err", err)

// 	err = r.vm.Close(ctx)
// 	slog.InfoContext(ctx, "closed vm", "err", err)
// 	return nil
// }

// Fields implements run.Runnable.
// func (r *RunmVMRuntime[VM]) Fields() []slog.Attr {
// 	return []slog.Attr{
// 		slog.String("id", r.vm.VM().ID()),
// 	}
// }

// // Name implements run.Runnable.
// func (r *RunmVMRuntime[VM]) Name() string {
// 	return r.vm.VM().ID()
// }

// // Run implements run.Runnable.
// // Subtle: this method shadows the method (RuntimeExtras).Run of RunmVMRuntime.RuntimeExtras.
// func (r *RunmVMRuntime[VM]) Run(ctx context.Context) error {
// 	slog.InfoContext(ctx, "running vm", "id", r.vm.VM().ID())

// 	return r.runGroup.RunContext(ctx)
// }
