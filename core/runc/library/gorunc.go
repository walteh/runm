//go:build !windows

package library

import (
	"context"
	"time"

	runc "github.com/containerd/go-runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

var _ RuncLibrary = (*GoRuncAdapter)(nil)

// GoRuncAdapter adapts the runc.Runc to the GoRuncLibrary interface.
type GoRuncAdapter struct {
	runcCmd *runc.Runc
}

// NewGoRuncAdapter creates a new GoRuncAdapter.
func NewGoRuncAdapter(runcCmd *runc.Runc) *GoRuncAdapter {
	if runcCmd == nil {
		runcCmd = &runc.Runc{
			Command: runc.DefaultCommand,
		}
	}
	return &GoRuncAdapter{
		runcCmd: runcCmd,
	}
}

// State returns the state of the container for the given id.
func (g *GoRuncAdapter) State(ctx context.Context, id string) (*runc.Container, error) {
	return g.runcCmd.State(ctx, id)
}

// Create creates a new container and returns its pid.
func (g *GoRuncAdapter) Create(ctx context.Context, id, bundle string, opts *runc.CreateOpts) error {
	return g.runcCmd.Create(ctx, id, bundle, opts)
}

// Start starts a created container.
func (g *GoRuncAdapter) Start(ctx context.Context, id string) error {
	return g.runcCmd.Start(ctx, id)
}

// Run creates and starts a container and returns its pid.
func (g *GoRuncAdapter) Run(ctx context.Context, id, bundle string, opts *runc.CreateOpts) (int, error) {
	return g.runcCmd.Run(ctx, id, bundle, opts)
}

// Delete deletes the container.
func (g *GoRuncAdapter) Delete(ctx context.Context, id string, opts *runc.DeleteOpts) error {
	return g.runcCmd.Delete(ctx, id, opts)
}

// Kill sends the specified signal to the container.
func (g *GoRuncAdapter) Kill(ctx context.Context, id string, signal int, opts *runc.KillOpts) error {
	return g.runcCmd.Kill(ctx, id, signal, opts)
}

// Stats returns runtime specific metrics for a container.
func (g *GoRuncAdapter) Stats(ctx context.Context, id string) (*runc.Stats, error) {
	return g.runcCmd.Stats(ctx, id)
}

// Events returns events for the container.
func (g *GoRuncAdapter) Events(ctx context.Context, id string, duration time.Duration) (chan *runc.Event, error) {
	return g.runcCmd.Events(ctx, id, duration)
}

// Pause pauses the container.
func (g *GoRuncAdapter) Pause(ctx context.Context, id string) error {
	return g.runcCmd.Pause(ctx, id)
}

// Resume resumes the container.
func (g *GoRuncAdapter) Resume(ctx context.Context, id string) error {
	return g.runcCmd.Resume(ctx, id)
}

// Ps lists processes in the container.
func (g *GoRuncAdapter) Ps(ctx context.Context, id string) ([]int, error) {
	return g.runcCmd.Ps(ctx, id)
}

// Checkpoint checkpoints the container.
func (g *GoRuncAdapter) Checkpoint(ctx context.Context, id string, opts *runc.CheckpointOpts, actions ...runc.CheckpointAction) error {
	return g.runcCmd.Checkpoint(ctx, id, opts, actions...)
}

// Restore restores the container from a checkpoint.
func (g *GoRuncAdapter) Restore(ctx context.Context, id, bundle string, opts *runc.RestoreOpts) (int, error) {
	return g.runcCmd.Restore(ctx, id, bundle, opts)
}

// Exec executes an additional process in the container.
func (g *GoRuncAdapter) Exec(ctx context.Context, id string, spec specs.Process, opts *runc.ExecOpts) error {
	return g.runcCmd.Exec(ctx, id, spec, opts)
}

// List lists all containers.
func (g *GoRuncAdapter) List(ctx context.Context) ([]*runc.Container, error) {
	return g.runcCmd.List(ctx)
}

// Version returns the version of runc.
func (g *GoRuncAdapter) Version(ctx context.Context) (runc.Version, error) {
	return g.runcCmd.Version(ctx)
}

func (g *GoRuncAdapter) Top(ctx context.Context, id, psOptions string) (*runc.TopResults, error) {
	return g.runcCmd.Top(ctx, id, psOptions)
}

func (g *GoRuncAdapter) Update(ctx context.Context, id string, resources *specs.LinuxResources) error {
	return g.runcCmd.Update(ctx, id, resources)
}

func (g *GoRuncAdapter) NewTempConsoleSocket() (*runc.Socket, error) {
	return runc.NewTempConsoleSocket()
}

func (g *GoRuncAdapter) LogFilePath() string {
	return g.runcCmd.Log
}
