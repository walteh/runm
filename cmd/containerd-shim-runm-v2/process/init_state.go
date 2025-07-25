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

package process

import (
	"context"
	"log/slog"

	"github.com/containerd/log"
	"gitlab.com/tozd/go/errors"

	google_protobuf "github.com/containerd/containerd/v2/pkg/protobuf/types"
	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/process"
	"github.com/walteh/runm/core/runc/runtime"
)

type initState interface {
	Start(context.Context) error
	Delete(context.Context) error
	Pause(context.Context) error
	Resume(context.Context) error
	Update(context.Context, *google_protobuf.Any) error
	Checkpoint(context.Context, *process.CheckpointConfig) error
	Exec(context.Context, string, *process.ExecConfig) (Process, error)
	Kill(context.Context, uint32, bool) error
	SetExited(int)
	Status(context.Context) (string, error)
}

type createdState struct {
	p *Init
}

func (s *createdState) transition(name string) error {
	switch name {
	case "running":
		s.p.initState = &runningState{p: s.p}
	case "stopped":
		s.p.initState = &stoppedState{p: s.p}
	case "deleted":
		s.p.initState = &deletedState{}
	default:
		return errors.Errorf("invalid state transition %q to %q", stateName(s), name)
	}
	return nil
}

func (s *createdState) Pause(ctx context.Context) error {
	return errors.New("cannot pause task in created state")
}

func (s *createdState) Resume(ctx context.Context) error {
	return errors.New("cannot resume task in created state")
}

func (s *createdState) Update(ctx context.Context, r *google_protobuf.Any) error {
	return s.p.update(ctx, r)
}

func (s *createdState) Checkpoint(ctx context.Context, r *process.CheckpointConfig) error {
	return errors.New("cannot checkpoint a task in created state")
}

func (s *createdState) Start(ctx context.Context) error {
	if err := s.p.start(ctx); err != nil {
		return errors.Errorf("start - %T: %w", err, err)
	}
	return s.transition("running")
}

func (s *createdState) Delete(ctx context.Context) error {
	if err := s.p.delete(ctx); err != nil {
		return err
	}
	return s.transition("deleted")
}

func (s *createdState) Kill(ctx context.Context, sig uint32, all bool) error {
	slog.InfoContext(ctx, "KILLING RUNM from created state", "id", s.p.id, "signal", sig, "all", all)
	return s.p.kill(ctx, sig, all)
}

func (s *createdState) SetExited(status int) {
	s.p.setExited(status)

	if err := s.transition("stopped"); err != nil {
		panic(err)
	}
}

func (s *createdState) Exec(ctx context.Context, path string, r *process.ExecConfig) (Process, error) {
	return s.p.exec(ctx, path, r)
}

func (s *createdState) Status(ctx context.Context) (string, error) {
	return "created", nil
}

type createdCheckpointState struct {
	p    *Init
	opts *gorunc.RestoreOpts
}

func (s *createdCheckpointState) transition(name string) error {
	switch name {
	case "running":
		s.p.initState = &runningState{p: s.p}
	case "stopped":
		s.p.initState = &stoppedState{p: s.p}
	case "deleted":
		s.p.initState = &deletedState{}
	default:
		return errors.Errorf("invalid state transition %q to %q", stateName(s), name)
	}
	return nil
}

func (s *createdCheckpointState) Pause(ctx context.Context) error {
	return errors.New("cannot pause task in created state")
}

func (s *createdCheckpointState) Resume(ctx context.Context) error {
	return errors.New("cannot resume task in created state")
}

func (s *createdCheckpointState) Update(ctx context.Context, r *google_protobuf.Any) error {
	return s.p.update(ctx, r)
}

func (s *createdCheckpointState) Checkpoint(ctx context.Context, r *process.CheckpointConfig) error {
	return errors.New("cannot checkpoint a task in created state")
}

func (s *createdCheckpointState) Start(ctx context.Context) error {
	p := s.p
	sio := p.stdio

	var (
		err    error
		socket runtime.ConsoleSocket
	)
	if sio.Terminal {
		if socket, err = p.runtime.NewTempConsoleSocket(ctx); err != nil {
			return errors.Errorf("failed to create OCI runtime console socket: %w", err)
		}
		defer socket.Close()
		s.opts.ConsoleSocket = socket
	}

	if _, err := s.p.runtime.Restore(ctx, p.id, p.Bundle, s.opts); err != nil {
		return errors.Errorf("OCI runtime restore failed: %w", err)
	}
	if sio.Stdin != "" {
		if err := p.openStdin(sio.Stdin); err != nil {
			return errors.Errorf("failed to open stdin fifo %s: %w", sio.Stdin, err)
		}
	}
	if socket != nil {
		console, err := socket.ReceiveMaster()
		if err != nil {
			return errors.Errorf("failed to retrieve console master: %w", err)
		}
		console, err = p.Platform.CopyConsole(ctx, console, p.id, sio.Stdin, sio.Stdout, sio.Stderr, &p.wg)
		if err != nil {
			return errors.Errorf("failed to start console copy: %w", err)
		}
		p.console = console
	} else {
		if err := p.io.Copy(ctx, &p.wg); err != nil {
			return errors.Errorf("failed to start io pipe copy: %w", err)
		}
	}
	pid, err := gorunc.ReadPidFile(s.opts.PidFile)
	if err != nil {
		return errors.Errorf("failed to retrieve OCI runtime container pid: %w", err)
	}
	p.pid = pid
	return s.transition("running")
}

func (s *createdCheckpointState) Delete(ctx context.Context) error {
	if err := s.p.delete(ctx); err != nil {
		return err
	}
	return s.transition("deleted")
}

func (s *createdCheckpointState) Kill(ctx context.Context, sig uint32, all bool) error {
	slog.InfoContext(ctx, "KILLING RUNM from created checkpoint state", "id", s.p.id, "signal", sig, "all", all)
	return s.p.kill(ctx, sig, all)
}

func (s *createdCheckpointState) SetExited(status int) {
	s.p.setExited(status)

	if err := s.transition("stopped"); err != nil {
		panic(err)
	}
}

func (s *createdCheckpointState) Exec(ctx context.Context, path string, r *process.ExecConfig) (Process, error) {
	return nil, errors.New("cannot exec in a created state")
}

func (s *createdCheckpointState) Status(ctx context.Context) (string, error) {
	return "created", nil
}

type runningState struct {
	p *Init
}

func (s *runningState) transition(name string) error {
	switch name {
	case "stopped":
		s.p.initState = &stoppedState{p: s.p}
	case "paused":
		s.p.initState = &pausedState{p: s.p}
	default:
		return errors.Errorf("invalid state transition %q to %q", stateName(s), name)
	}
	return nil
}

func (s *runningState) Pause(ctx context.Context) error {
	s.p.pausing.Store(true)
	// NOTE "pausing" will be returned in the short window
	// after `transition("paused")`, before `pausing` is reset
	// to false. That doesn't break the state machine, just
	// delays the "paused" state a little bit.
	defer s.p.pausing.Store(false)

	if err := s.p.runtime.Pause(ctx, s.p.id); err != nil {
		return errors.Errorf("OCI runtime pause failed: %w", err)
	}

	return s.transition("paused")
}

func (s *runningState) Resume(ctx context.Context) error {
	return errors.New("cannot resume a running process")
}

func (s *runningState) Update(ctx context.Context, r *google_protobuf.Any) error {
	return s.p.update(ctx, r)
}

func (s *runningState) Checkpoint(ctx context.Context, r *process.CheckpointConfig) error {
	return s.p.checkpoint(ctx, r)
}

func (s *runningState) Start(ctx context.Context) error {
	return errors.New("cannot start a running process")
}

func (s *runningState) Delete(ctx context.Context) error {
	return errors.New("cannot delete a running process")
}

func (s *runningState) Kill(ctx context.Context, sig uint32, all bool) error {
	slog.InfoContext(ctx, "KILLING RUNM from running state", "id", s.p.id, "signal", sig, "all", all)
	return s.p.kill(ctx, sig, all)
}

func (s *runningState) SetExited(status int) {
	s.p.setExited(status)

	if err := s.transition("stopped"); err != nil {
		panic(err)
	}
}

func (s *runningState) Exec(ctx context.Context, path string, r *process.ExecConfig) (Process, error) {
	return s.p.exec(ctx, path, r)
}

func (s *runningState) Status(ctx context.Context) (string, error) {
	return "running", nil
}

type pausedState struct {
	p *Init
}

func (s *pausedState) transition(name string) error {
	switch name {
	case "running":
		s.p.initState = &runningState{p: s.p}
	case "stopped":
		s.p.initState = &stoppedState{p: s.p}
	default:
		return errors.Errorf("invalid state transition %q to %q", stateName(s), name)
	}
	return nil
}

func (s *pausedState) Pause(ctx context.Context) error {
	return errors.New("cannot pause a paused container")
}

func (s *pausedState) Resume(ctx context.Context) error {
	if err := s.p.runtime.Resume(ctx, s.p.id); err != nil {
		return errors.Errorf("OCI runtime resume failed: %w", err)
	}

	return s.transition("running")
}

func (s *pausedState) Update(ctx context.Context, r *google_protobuf.Any) error {
	return s.p.update(ctx, r)
}

func (s *pausedState) Checkpoint(ctx context.Context, r *process.CheckpointConfig) error {
	return s.p.checkpoint(ctx, r)
}

func (s *pausedState) Start(ctx context.Context) error {
	return errors.New("cannot start a paused process")
}

func (s *pausedState) Delete(ctx context.Context) error {
	return errors.New("cannot delete a paused process")
}

func (s *pausedState) Kill(ctx context.Context, sig uint32, all bool) error {
	slog.InfoContext(ctx, "KILLING RUNM from paused state", "id", s.p.id, "signal", sig, "all", all)
	return s.p.kill(ctx, sig, all)
}

func (s *pausedState) SetExited(status int) {
	s.p.setExited(status)

	if err := s.p.runtime.Resume(context.Background(), s.p.id); err != nil {
		log.L.WithError(err).Error("resuming exited container from paused state")
	}

	if err := s.transition("stopped"); err != nil {
		panic(err)
	}
}

func (s *pausedState) Exec(ctx context.Context, path string, r *process.ExecConfig) (Process, error) {
	return nil, errors.New("cannot exec in a paused state")
}

func (s *pausedState) Status(ctx context.Context) (string, error) {
	return "paused", nil
}

type stoppedState struct {
	p *Init
}

func (s *stoppedState) transition(name string) error {
	switch name {
	case "deleted":
		s.p.initState = &deletedState{}
	default:
		return errors.Errorf("invalid state transition %q to %q", stateName(s), name)
	}
	return nil
}

func (s *stoppedState) Pause(ctx context.Context) error {
	return errors.New("cannot pause a stopped container")
}

func (s *stoppedState) Resume(ctx context.Context) error {
	return errors.New("cannot resume a stopped container")
}

func (s *stoppedState) Update(ctx context.Context, r *google_protobuf.Any) error {
	return errors.New("cannot update a stopped container")
}

func (s *stoppedState) Checkpoint(ctx context.Context, r *process.CheckpointConfig) error {
	return errors.New("cannot checkpoint a stopped container")
}

func (s *stoppedState) Start(ctx context.Context) error {
	return errors.New("cannot start a stopped process")
}

func (s *stoppedState) Delete(ctx context.Context) error {
	if err := s.p.delete(ctx); err != nil {
		return err
	}
	return s.transition("deleted")
}

func (s *stoppedState) Kill(ctx context.Context, sig uint32, all bool) error {
	slog.InfoContext(ctx, "KILLING RUNM from stopped state", "id", s.p.id, "signal", sig, "all", all)
	return s.p.kill(ctx, sig, all)
}

func (s *stoppedState) SetExited(status int) {
	// no op
}

func (s *stoppedState) Exec(ctx context.Context, path string, r *process.ExecConfig) (Process, error) {
	return nil, errors.New("cannot exec in a stopped state")
}

func (s *stoppedState) Status(ctx context.Context) (string, error) {
	return "stopped", nil
}
