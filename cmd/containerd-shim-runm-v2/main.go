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

package main

import (
	_ "github.com/walteh/runm/cmd/containerd-shim-runm-v2/task/plugin"

	"context"
	"log/slog"
	"os"
	"time"

	"github.com/containerd/containerd/v2/pkg/shim"

	"github.com/walteh/runm/cmd/containerd-shim-runm-v2/manager"
)

func main() {
	// Create a debug manager that wraps the regular manager
	mgr := manager.NewDebugManager(manager.NewShimManager("io.containerd.runc.v2"))

	// When in delete mode, we need to make sure the process doesn't
	// exit too quickly after Stop returns, as containerd expects
	// to be able to make additional calls (Delete/Shutdown)
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "delete" {
		slog.Info("running in delete mode", "args", os.Args)

		// Create a context that will keep the process alive for a short time
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Run the shim manager but keep the process alive until context is done
		go shim.Run(ctx, mgr)

		// Wait for enough time for containerd to make its calls
		<-time.After(1 * time.Second)
		slog.Info("delete mode delay complete, exiting")
		return
	}

	// Normal mode - let shim.Run handle the process lifecycle
	shim.Run(context.Background(), mgr)
}
