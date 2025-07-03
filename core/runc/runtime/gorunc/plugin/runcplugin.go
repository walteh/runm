//go:build !windows

package goruncruntimeplugin

import (
	"github.com/containerd/containerd/v2/plugins"
	"github.com/containerd/plugin"
	"github.com/containerd/plugin/registry"

	goruncruntime "github.com/walteh/runm/core/runc/runtime/gorunc"
)

func init() {
	registry.Register(&plugin.Registration{
		Type:     plugins.InternalPlugin,
		ID:       "runm-runtime-creator",
		Requires: []plugin.Type{},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			return &goruncruntime.GoRuncRuntimeCreator{}, nil
		},
	})

}
