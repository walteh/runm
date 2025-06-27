package goruncruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-spec/specs-go/features"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/runtime"
)

var _ runtime.Runtime = (*GoRuncRuntime)(nil)
var _ runtime.RuntimeExtras = (*GoRuncRuntime)(nil)

type GoRuncRuntime struct {
	*gorunc.Runc
	sharedDirPathPrefix string
}

func (r *GoRuncRuntime) SharedDir() string {
	return r.sharedDirPathPrefix
}

func WrapdGoRuncRuntime(rt *gorunc.Runc) *GoRuncRuntime {
	return &GoRuncRuntime{
		Runc: rt,
	}
}

func (r *GoRuncRuntime) NewTempConsoleSocket(ctx context.Context) (runtime.ConsoleSocket, error) {
	return gorunc.NewTempConsoleSocket()
}

func (r *GoRuncRuntime) NewNullIO() (runtime.IO, error) {
	return gorunc.NewNullIO()
}

func (r *GoRuncRuntime) NewPipeIO(ctx context.Context, cioUID, ioGID int, opts ...gorunc.IOOpt) (runtime.IO, error) {
	return gorunc.NewPipeIO(cioUID, ioGID, opts...)
}

func (r *GoRuncRuntime) ReadPidFile(ctx context.Context, path string) (int, error) {
	return gorunc.ReadPidFile(path)
}

func (r *GoRuncRuntime) RuncRun(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) (int, error) {
	return r.Runc.Run(ctx, id, bundle, options)
}

var _ runtime.RuntimeCreator = (*GoRuncRuntimeCreator)(nil)

type GoRuncRuntimeCreator struct {
}

// Features implements runtime.RuntimeCreator.
func (c *GoRuncRuntimeCreator) Features(ctx context.Context) (*features.Features, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, gorunc.DefaultCommand, "features")
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute %v: %w (stderr: %q)", cmd.Args, err, stderr.String())
	}
	var feat features.Features
	if err := json.Unmarshal(stdout, &feat); err != nil {
		return nil, err
	}
	return &feat, nil
}

// todo the move here might be to add the exitting
// Create ionterface to the above type (not creator)
// and so we can wrao stdio from sockets for logging purposes
// ie redirect a copy to stdiout

func (c *GoRuncRuntimeCreator) Create(ctx context.Context, opts *runtime.RuntimeOptions) (runtime.Runtime, error) {
	r := WrapdGoRuncRuntime(&gorunc.Runc{
		Command:      opts.ProcessCreateConfig.Runtime,
		Log:          filepath.Join(opts.ProcessCreateConfig.Bundle, runtime.LogFileBase),
		LogFormat:    gorunc.JSON,
		PdeathSignal: unix.SIGKILL,
		Root:         filepath.Join(opts.ProcessCreateConfig.Options.Root, opts.Namespace),
		// SystemdCgroup: opts.ProcessCreateConfig.Options.SystemdCgroup,
		SystemdCgroup: false,
	})
	return r, nil
}

func (r *GoRuncRuntime) Create(ctx context.Context, id, bundle string, options *gorunc.CreateOpts) error {
	// slog.Info(godump.DumpStr(options), "id", id, "bundle", bundle, "options", options)

	output, err := exec.CommandContext(ctx, "/bin/busybox", "ls", "-lah", bundle).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list bundle: %w (output: %q)", err, string(output))
	}
	slog.InfoContext(ctx, "ls -lahr: "+string(output), "bundle", bundle)

	// Handle the exec FIFO in a separate goroutine to ensure proper synchronization
	// This is critical for container initialization
	go func() {
		// The FIFO is created in the container's state directory
		stateDir := filepath.Join(r.Root, id)
		fifoPath := filepath.Join(stateDir, "exec.fifo")

		// Wait a bit to ensure the container has time to create the FIFO
		time.Sleep(100 * time.Millisecond)

		// Check if the FIFO exists
		if _, err := os.Stat(fifoPath); err != nil {
			slog.InfoContext(ctx, "Exec FIFO not found, skipping FIFO handling", "path", fifoPath, "error", err)
			return
		}

		slog.InfoContext(ctx, "Opening exec FIFO to synchronize with container", "path", fifoPath)

		// Open the FIFO with a timeout to avoid hanging indefinitely
		openDone := make(chan struct{})
		var openErr error

		go func() {
			// Open the FIFO for reading (O_RDONLY)
			// This will block until the container writes to it
			_, openErr = os.OpenFile(fifoPath, os.O_RDONLY, 0)
			close(openDone)
		}()

		// Wait for either the open to complete or a timeout
		select {
		case <-openDone:
			if openErr != nil {
				slog.InfoContext(ctx, "Failed to open exec FIFO", "path", fifoPath, "error", openErr)
			} else {
				slog.InfoContext(ctx, "Successfully opened exec FIFO", "path", fifoPath)
			}
		case <-time.After(2 * time.Second):
			slog.InfoContext(ctx, "Timeout waiting for exec FIFO", "path", fifoPath)
		}
	}()

	files := []string{
		"bootstrap.json",
		"config.json",
		"options.json",
	}

	for _, file := range files {
		output, err = exec.CommandContext(ctx, "/bin/busybox", "cat", filepath.Join(bundle, file)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to list bundle: %w (output: %q)", err, string(output))
		}

		// var out map[string]any
		// if err := json.Unmarshal(output, &out); err != nil {
		// 	return fmt.Errorf("failed to unmarshal %s: %w (output: %q)", file, err, string(output))
		// }

		slog.InfoContext(ctx, "cat: "+file, "bundle", bundle, "output", string(output))
	}

	// parse the config.json
	config, err := os.ReadFile(filepath.Join(bundle, "config.json"))
	if err != nil {
		return fmt.Errorf("failed to read config.json: %w", err)
	}

	var configSpec specs.Spec
	if err := json.Unmarshal(config, &configSpec); err != nil {
		return fmt.Errorf("failed to unmarshal config.json: %w", err)
	}

	hoooksToProxy := []specs.Hook{}

	hoooksToProxy = append(hoooksToProxy, configSpec.Hooks.Poststart...)
	hoooksToProxy = append(hoooksToProxy, configSpec.Hooks.Poststop...)
	hoooksToProxy = append(hoooksToProxy, configSpec.Hooks.CreateRuntime...)
	hoooksToProxy = append(hoooksToProxy, configSpec.Hooks.CreateContainer...)
	hoooksToProxy = append(hoooksToProxy, configSpec.Hooks.StartContainer...)

	createdSymlinks := make(map[string]bool)
	// for all the hooks, create symlinks to the host service
	for _, hook := range hoooksToProxy {
		if _, ok := createdSymlinks[hook.Path]; !ok {
			os.MkdirAll(filepath.Dir(hook.Path), 0755)
			os.Symlink("/mbin/runm-linux-host-fork-exec-proxy", hook.Path)
			createdSymlinks[hook.Path] = true
			slog.InfoContext(ctx, "created symlink", "path", hook.Path)
		}
	}

	done := false
	defer func() {
		done = true
		slog.InfoContext(ctx, "done runc create")
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for !done {
			select {
			case <-ticker.C:
				slog.InfoContext(ctx, "still running runc create")
			}
		}
	}()

	return r.Runc.Create(ctx, id, bundle, options)
}
