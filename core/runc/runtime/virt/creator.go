//go:build !windows

package virt

import (
	"context"
	"log/slog"

	"github.com/containers/common/pkg/strongunits"
	"github.com/opencontainers/runtime-spec/specs-go/features"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/virt/vmm"
)

func ptr[T any](v T) *T {
	return &v
}

var _ runtime.RuntimeCreator = (*RunmVMRuntimeCreator[vmm.VirtualMachine])(nil)

type RunmVMRuntimeCreator[VM vmm.VirtualMachine] struct {
	// publisher events.Publisher
	hpv       vmm.Hypervisor[VM]
	maxMemory strongunits.StorageUnits
	vcpus     int
}

// Features implements runtime.RuntimeCreator.
func (me *RunmVMRuntimeCreator[VM]) Features(ctx context.Context) (*features.Features, error) {
	feats := &features.Features{
		OCIVersionMin: "1.0.0",
		OCIVersionMax: "1.1.0",
		MountOptions:  []string{"ro", "rw", "bind", "recursive"},
		Linux: &features.Linux{
			MountExtensions: &features.MountExtensions{
				IDMap: &features.IDMap{Enabled: ptr(true)},
			},
			Cgroup: &features.Cgroup{
				V1:          ptr(false),
				V2:          ptr(true),
				Systemd:     ptr(false),
				SystemdUser: ptr(false),
				Rdma:        ptr(false),
			},
			Namespaces: []string{
				"mount", "uts", "ipc",
				"pid", "net", "user", "cgroup",
			},
			IntelRdt: nil,
			Apparmor: nil,
			Selinux:  nil,
		},
	}
	return feats, nil
}

func (me *RunmVMRuntimeCreator[VM]) Create(ctx context.Context, opts *runtime.RuntimeOptions) (runtime.Runtime, error) {
	if ctx.Err() != nil {
		slog.ErrorContext(ctx, "context done before creating VM runtime")
		return nil, ctx.Err()
	}
	vm, err := NewRunmVMRuntime(ctx, me.hpv, opts, me.maxMemory, me.vcpus)
	if err != nil {
		return nil, errors.Errorf("failed to create VM: %w", err)
	}

	slog.InfoContext(ctx, "created VM", "id", opts.ProcessCreateConfig.ID)
	return vm, nil
}

func NewRunmVMRuntimeCreator[VM vmm.VirtualMachine](hpv vmm.Hypervisor[VM], maxMemory strongunits.StorageUnits, vcpus int) *RunmVMRuntimeCreator[VM] {
	return &RunmVMRuntimeCreator[VM]{
		hpv:       hpv,
		maxMemory: maxMemory,
		vcpus:     vcpus,
	}
}
