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

	"github.com/containerd/console"
	"gitlab.com/tozd/go/errors"
)

type execState interface {
	Resize(console.WinSize) error
	Start(context.Context) error
	Delete(context.Context) error
	Kill(context.Context, uint32, bool) error
	SetExited(int)
	Status(context.Context) (string, error)
}

type execCreatedState struct {
	p *execProcess
}

func (s *execCreatedState) transition(name string) error {
	switch name {
	case "running":
		s.p.execState = &execRunningState{p: s.p}
	case "stopped":
		s.p.execState = &execStoppedState{p: s.p}
	case "deleted":
		s.p.execState = &deletedState{}
	default:
		return errors.Errorf("transitioning state from %q to %q", stateName(s), name)
	}
	return nil
}

func (s *execCreatedState) Resize(ws console.WinSize) error {
	return s.p.resize(ws)
}

func (s *execCreatedState) Start(ctx context.Context) error {
	// recover
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "EXECPROCESS:START[PANIC]", "id", s.p.id, "pid", s.p.pid.getLocked(), "panic", r)
		}
	}()

	slog.DebugContext(ctx, "EXECPROCESS:START[START]", "id", s.p.id, "pid", s.p.pid.getLocked())
	defer slog.DebugContext(ctx, "EXECPROCESS:START[DONE]", "id", s.p.id, "pid", s.p.pid.getLocked())
	if err := s.p.start(ctx); err != nil {
		return err
	}
	return s.transition("running")
}

func (s *execCreatedState) Delete(ctx context.Context) error {
	if err := s.p.delete(ctx); err != nil {
		return err
	}

	return s.transition("deleted")
}

func (s *execCreatedState) Kill(ctx context.Context, sig uint32, all bool) error {
	return s.p.kill(ctx, sig, all)
}

func (s *execCreatedState) SetExited(status int) {
	s.p.setExited(status)

	if err := s.transition("stopped"); err != nil {
		panic(err)
	}
}

func (s *execCreatedState) Status(ctx context.Context) (string, error) {
	return "created", nil
}

type execRunningState struct {
	p *execProcess
}

func (s *execRunningState) transition(name string) error {
	switch name {
	case "stopped":
		s.p.execState = &execStoppedState{p: s.p}
	default:
		return errors.Errorf("transitioning state from %q to %q", stateName(s), name)
	}
	return nil
}

func (s *execRunningState) Resize(ws console.WinSize) error {
	return s.p.resize(ws)
}

func (s *execRunningState) Start(ctx context.Context) error {
	return errors.New("cannot start a running process")
}

func (s *execRunningState) Delete(ctx context.Context) error {
	return errors.New("cannot delete a running process")
}

func (s *execRunningState) Kill(ctx context.Context, sig uint32, all bool) error {
	return s.p.kill(ctx, sig, all)
}

func (s *execRunningState) SetExited(status int) {
	s.p.setExited(status)

	if err := s.transition("stopped"); err != nil {
		panic(err)
	}
}

func (s *execRunningState) Status(ctx context.Context) (string, error) {
	return "running", nil
}

type execStoppedState struct {
	p *execProcess
}

func (s *execStoppedState) transition(name string) error {
	switch name {
	case "deleted":
		s.p.execState = &deletedState{}
	default:
		return errors.Errorf("transitioning state from %q to %q", stateName(s), name)
	}
	return nil
}

func (s *execStoppedState) Resize(ws console.WinSize) error {
	return errors.New("cannot resize a stopped container")
}

func (s *execStoppedState) Start(ctx context.Context) error {
	return errors.New("cannot start a stopped process")
}

func (s *execStoppedState) Delete(ctx context.Context) error {
	if err := s.p.delete(ctx); err != nil {
		return err
	}

	return s.transition("deleted")
}

func (s *execStoppedState) Kill(ctx context.Context, sig uint32, all bool) error {
	return s.p.kill(ctx, sig, all)
}

func (s *execStoppedState) SetExited(status int) {
	// no op
}

func (s *execStoppedState) Status(ctx context.Context) (string, error) {
	return "stopped", nil
}
