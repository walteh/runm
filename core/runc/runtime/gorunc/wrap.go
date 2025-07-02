package goruncruntime

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"time"

	gorunc "github.com/containerd/go-runc"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"
)

func (r *GoRuncRuntime) Create(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) error {
	// slog.Info(godump.DumpStr(options), "id", id, "bundle", bundle, "options", options)

	output, err := exec.CommandContext(ctx, "/bin/busybox", "ls", "-lah", bundle).CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "failed to list bundle", "error", err, "output", string(output))
		return errors.Errorf("failed to list bundle: %w", err)
	}
	slog.InfoContext(ctx, "ls -lahr: "+string(output), "bundle", bundle)

	output, err = exec.CommandContext(ctx, "/bin/busybox", "ls", "-lah", filepath.Join(bundle, "rootfs")).CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "failed to list bundle", "error", err, "output", string(output))
		return errors.Errorf("failed to list bundle: %w", err)
	}
	slog.InfoContext(ctx, "ls -lah: "+string(output), "bundle", bundle)
	// // Handle the exec FIFO in a separate goroutine to ensure proper synchronization
	// // This is critical for container initialization
	// go func() {
	// 	// The FIFO is created in the container's state directory
	// 	stateDir := filepath.Join(r.internal.Root, id)
	// 	fifoPath := filepath.Join(stateDir, "exec.fifo")

	// 	// Wait a bit to ensure the container has time to create the FIFO
	// 	time.Sleep(100 * time.Millisecond)

	// 	// Check if the FIFO exists
	// 	if _, err := os.Stat(fifoPath); err != nil {
	// 		slog.InfoContext(ctx, "Exec FIFO not found, skipping FIFO handling", "path", fifoPath, "error", err)
	// 		return
	// 	}

	// 	slog.InfoContext(ctx, "Opening exec FIFO to synchronize with container", "path", fifoPath)

	// 	// Open the FIFO with a timeout to avoid hanging indefinitely
	// 	openDone := make(chan struct{})
	// 	var openErr error

	// 	go func() {
	// 		// Open the FIFO for reading (O_RDONLY)
	// 		// This will block until the container writes to it
	// 		_, openErr = os.OpenFile(fifoPath, os.O_RDONLY, 0)
	// 		close(openDone)
	// 	}()

	// 	// Wait for either the open to complete or a timeout
	// 	select {
	// 	case <-openDone:
	// 		if openErr != nil {
	// 			slog.InfoContext(ctx, "Failed to open exec FIFO", "path", fifoPath, "error", openErr)
	// 		} else {
	// 			slog.InfoContext(ctx, "Successfully opened exec FIFO", "path", fifoPath)
	// 		}
	// 	case <-time.After(2 * time.Second):
	// 		slog.InfoContext(ctx, "Timeout waiting for exec FIFO", "path", fifoPath)
	// 	}
	// }()

	done := false
	defer func() {
		done = true
		slog.InfoContext(ctx, "done runc create")
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if done {
				return
			}
			slog.InfoContext(ctx, "still running runc create")
		}
	}()

	options.NoNewKeyring = true

	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Create(ctx, id, bundle, options)
	})
}

func (r *GoRuncRuntime) Checkpoint(ctx context.Context, id string, opts *gorunc.CheckpointOpts, actions ...gorunc.CheckpointAction) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Checkpoint(ctx, id, opts, actions...)
	})
}

// Delete implements runtime.Runtime.
func (r *GoRuncRuntime) Delete(ctx context.Context, id string, opts *gorunc.DeleteOpts) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Delete(ctx, id, opts)
	})
}

// Exec implements runtime.Runtime.
func (r *GoRuncRuntime) Exec(ctx context.Context, id string, spec specs.Process, opts *gorunc.ExecOpts) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Exec(ctx, id, spec, opts)
	})
}

// Kill implements runtime.Runtime.
func (r *GoRuncRuntime) Kill(ctx context.Context, id string, signal int, opts *gorunc.KillOpts) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		slog.InfoContext(ctx, "KILLING RUNC", "id", id, "signal", signal, "opts.all", opts.All)
		return r.internal.Kill(ctx, id, signal, opts)
	})
}

// Pause implements runtime.Runtime.
func (r *GoRuncRuntime) Pause(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Pause(ctx, id)
	})
}

// Ps implements runtime.Runtime.
func (r *GoRuncRuntime) Ps(ctx context.Context, id string) ([]int, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() ([]int, error) {
		return r.internal.Ps(ctx, id)
	})
}

// Restore implements runtime.Runtime.
func (r *GoRuncRuntime) Restore(ctx context.Context, id string, bundle string, opts *gorunc.RestoreOpts) (int, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (int, error) {
		return r.internal.Restore(ctx, id, bundle, opts)
	})
}

// Resume implements runtime.Runtime.
func (r *GoRuncRuntime) Resume(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Resume(ctx, id)
	})
}

// Update implements runtime.Runtime.
func (r *GoRuncRuntime) Update(ctx context.Context, id string, resources *specs.LinuxResources) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Update(ctx, id, resources)
	})
}

// start
func (r *GoRuncRuntime) Start(ctx context.Context, id string) error {
	return WrapWithRuntimeError(ctx, r, func() error {
		return r.internal.Start(ctx, id)
	})
}

// Events implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Events(ctx context.Context, id string, timeout time.Duration) (chan *gorunc.Event, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (chan *gorunc.Event, error) {
		return r.internal.Events(ctx, id, timeout)
	})
}

// List implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) List(ctx context.Context) ([]*gorunc.Container, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() ([]*gorunc.Container, error) {
		return r.internal.List(ctx)
	})
}

// State implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) State(ctx context.Context, id string) (*gorunc.Container, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.Container, error) {
		return r.internal.State(ctx, id)
	})
}

// Stats implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Stats(ctx context.Context, id string) (*gorunc.Stats, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.Stats, error) {
		return r.internal.Stats(ctx, id)
	})
}

// Top implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Top(ctx context.Context, a string, b string) (*gorunc.TopResults, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (*gorunc.TopResults, error) {
		return r.internal.Top(ctx, a, b)
	})
}

// Version implements runtime.RuntimeExtras.
func (r *GoRuncRuntime) Version(ctx context.Context) (gorunc.Version, error) {
	return WrapWithRuntimeErrorResult(ctx, r, func() (gorunc.Version, error) {
		return r.internal.Version(ctx)
	})
}
