//go:build !windows

/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package plugin

import (
	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/containerd/v2/pkg/shutdown"
	"github.com/containerd/containerd/v2/plugins"
	"github.com/containerd/plugin"
	"github.com/containerd/plugin/registry"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/cmd/containerd-shim-runm-v2/task"
	"github.com/walteh/runm/core/runc/runtime"
)

func init() {
	register()
}

func Reregister() {
	register()
}

func register() {
	registry.Register(&plugin.Registration{
		Type: plugins.TTRPCPlugin,
		ID:   "task",
		Requires: []plugin.Type{
			plugins.EventPlugin,
			plugins.InternalPlugin,
		},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			pp, err := ic.GetByID(plugins.EventPlugin, "publisher")
			if err != nil {
				return nil, errors.Errorf("getting publisher: %w", err)
			}
			ss, err := ic.GetByID(plugins.InternalPlugin, "shutdown")
			if err != nil {
				return nil, errors.Errorf("getting shutdown: %w", err)
			}
			rtc, err := ic.GetByID(plugins.InternalPlugin, "runm-runtime-creator")
			if err != nil {
				return nil, errors.Errorf("getting runm-runtime-creator: %w", err)
			}
			ts, err := task.NewTaskService(ic.Context, pp.(shim.Publisher), ss.(shutdown.Service), rtc.(runtime.RuntimeCreator))
			if err != nil {
				return nil, errors.Errorf("creating task service: %w", err)
			}
			return task.NewDebugTaskService(ts, true, true), nil
		},
	})

}
