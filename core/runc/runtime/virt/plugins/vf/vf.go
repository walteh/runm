//go:build darwin && arm64

package vfruntimeplugin

import (
	"github.com/containerd/containerd/v2/plugins"
	"github.com/containerd/plugin"
	"github.com/containerd/plugin/registry"
	"github.com/containers/common/pkg/strongunits"

	"github.com/walteh/runm/core/runc/runtime/virt"
	"github.com/walteh/runm/core/virt/vf"
)

func init() {
	register()
}

func register() {
	registry.Register(&plugin.Registration{
		Type:     plugins.InternalPlugin,
		ID:       "runm-runtime-creator",
		Requires: []plugin.Type{},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			return virt.NewRunmVMRuntimeCreator(
				vf.NewHypervisor(),
				strongunits.MiB(128), // max memory
				1,                    // vcpu
			), nil
		},
	})

}

func Reregister() {
	register()
}
