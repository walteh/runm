package goruncruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"

	gorunc "github.com/containerd/go-runc"
	"github.com/opencontainers/runtime-spec/specs-go/features"

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
	return r.Runc.Create(ctx, id, bundle, options)
}
