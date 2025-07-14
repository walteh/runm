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

package runm

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"syscall"

	"github.com/containerd/console"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/stdio"
	"github.com/containerd/fifo"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/cmd/containerd-shim-runm-v2/process"
	"github.com/walteh/runm/pkg/conn"
)

var bufPool = sync.Pool{
	New: func() interface{} {
		// setting to 4096 to align with PIPE_BUF
		// http://man7.org/linux/man-pages/man7/pipe.7.html
		buffer := make([]byte, 4096)
		return &buffer
	},
}

func newPlatform[T shutdownConsole](queue queue[T]) (stdio.Platform, error) {
	return &platform[T]{
		queue: queue,
	}, nil
}

type queue[T shutdownConsole] interface {
	Add(console console.Console) (T, error)
	Close() error
	CloseConsole(int) error
}

type shutdownConsole interface {
	console.Console
	Shutdown(close func(int) error) error
}

type platform[T shutdownConsole] struct {
	queue queue[T]
}

func (p *platform[T]) CopyConsole(ctx context.Context, console console.Console, id, stdin, stdout, stderr string, wg *sync.WaitGroup) (cons console.Console, retErr error) {
	if p.queue == nil {
		return nil, errors.New("uninitialized queue")
	}
	var kqueueConsole T
	var err error
	// if already a kqueueConsole dont recreate it
	if _, ok := console.(T); ok {
		kqueueConsole = console.(T)
		// return console, nil
	} else {

		kqueueConsole, err = p.queue.Add(console)
		if err != nil {
			return nil, errors.Errorf("adding console to queue: %w", err)
		}
	}

	var cwg sync.WaitGroup
	if stdin != "" {
		in, err := fifo.OpenFifo(context.Background(), stdin, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		cwg.Add(1)
		go func() {
			cwg.Done()
			bp := bufPool.Get().(*[]byte)
			defer bufPool.Put(bp)
			<-conn.DebugCopyWithBuffer(ctx, "stdin(read)->kqueue(write)", kqueueConsole, in, *bp)
			// we need to shutdown kqueueConsole when pipe broken
			kqueueConsole.Shutdown(p.queue.CloseConsole)
			kqueueConsole.Close()
			in.Close()
		}()
	}

	uri, err := url.Parse(stdout)
	if err != nil {
		return nil, errors.Errorf("unable to parse stdout uri: %w", err)
	}

	// ctx = slogctx.Append(ctx, "id", id)

	slog.DebugContext(ctx, "COPYCONSOLE[A] copying console to binary", "id", id, "stdout", stdout)

	switch uri.Scheme {
	case "binary":

		slog.DebugContext(ctx, "COPYCONSOLE[A] copying console to binary")

		ns, err := namespaces.NamespaceRequired(ctx)
		if err != nil {
			return nil, errors.Errorf("getting namespace: %w", err)
		}

		slog.DebugContext(ctx, "COPYCONSOLE[B] creating binary cmd")

		cmd := process.NewBinaryCmd(uri, id, ns)

		slog.DebugContext(ctx, "COPYCONSOLE[C] created binary cmd")

		// In case of unexpected errors during logging binary start, close open pipes
		var filesToClose []*os.File

		defer func() {
			if retErr != nil {
				process.CloseFiles(filesToClose...)
			}
		}()

		slog.DebugContext(ctx, "COPYCONSOLE[D] creating pipes")

		// Create pipe to be used by logging binary for Stdout
		outR, outW, err := os.Pipe()
		if err != nil {
			return nil, errors.Errorf("failed to create stdout pipes: %w", err)
		}
		filesToClose = append(filesToClose, outR)

		// Stderr is created for logging binary but unused when terminal is true
		serrR, _, err := os.Pipe()
		if err != nil {
			return nil, errors.Errorf("failed to create stderr pipes: %w", err)
		}
		filesToClose = append(filesToClose, serrR)

		r, w, err := os.Pipe()
		if err != nil {
			return nil, errors.Errorf("creating pipe: %w", err)
		}
		filesToClose = append(filesToClose, r)

		cmd.ExtraFiles = append(cmd.ExtraFiles, outR, serrR, w)

		wg.Add(1)
		cwg.Add(1)
		go func() {
			cwg.Done()
			<-conn.DebugCopy(ctx, "binary(read)->fifo(write)", outW, kqueueConsole)
			outW.Close()
			wg.Done()
		}()

		slog.DebugContext(ctx, "COPYCONSOLE[E] starting binary")

		if err := cmd.Start(); err != nil {
			return nil, errors.Errorf("failed to start logging binary process: %w", err)
		}

		slog.DebugContext(ctx, "COPYCONSOLE[F] closed write pipe after start")

		// Close our side of the pipe after start
		if err := w.Close(); err != nil {
			return nil, errors.Errorf("failed to close write pipe after start: %w", err)
		}

		slog.DebugContext(ctx, "COPYCONSOLE[G] read from logging binary")

		// Wait for the logging binary to be ready
		b := make([]byte, 1)
		if _, err := r.Read(b); err != nil && err != io.EOF {
			return nil, errors.Errorf("failed to read from logging binary: %w", err)
		}
		cwg.Wait()

		slog.DebugContext(ctx, "COPYCONSOLE[H] done")

	default:
		slog.DebugContext(ctx, "COPYCONSOLE[I] copying console to fifo")

		outw, err := fifo.OpenFifo(ctx, stdout, syscall.O_WRONLY, 0)
		if err != nil {
			return nil, errors.Errorf("opening stdout fifo: %w", err)
		}

		slog.DebugContext(ctx, "COPYCONSOLE[J] opening stdout fifo")

		outr, err := fifo.OpenFifo(ctx, stdout, syscall.O_RDONLY, 0)
		if err != nil {
			return nil, errors.Errorf("opening stdout fifo: %w", err)
		}

		slog.DebugContext(ctx, "COPYCONSOLE[K] adding to waitgroup")

		wg.Add(1)
		cwg.Add(1)
		go func() {
			cwg.Done()
			buf := bufPool.Get().(*[]byte)
			defer bufPool.Put(buf)
			slog.DebugContext(ctx, "COPYCONSOLE[L] copying console to fifo")
			<-conn.DebugCopyWithBuffer(ctx, "kqueue(read)->stdout(write)", outw, kqueueConsole, *buf)
			slog.DebugContext(ctx, "COPYCONSOLE[N] copied console to fifo")

			outw.Close()
			outr.Close()
			slog.DebugContext(ctx, "COPYCONSOLE[O] done")
			wg.Done()
		}()
		cwg.Wait()
	}

	slog.DebugContext(ctx, "COPYCONSOLE[P] done")

	return kqueueConsole, nil
}

func (p *platform[T]) ShutdownConsole(ctx context.Context, cons console.Console) error {
	slog.DebugContext(ctx, "SHUTDOWNCONSOLE")
	defer slog.DebugContext(ctx, "SHUTDOWNCONSOLE[DONE]")
	if p.queue == nil {
		return errors.New("uninitialized kqueuer")
	}
	kqueueConsole, ok := cons.(shutdownConsole)
	if !ok {
		return errors.Errorf("expected kqueueConsole, got %#v", cons)
	}
	return kqueueConsole.Shutdown(p.queue.CloseConsole)
}

func (p *platform[T]) Close() error {
	return p.queue.Close()
}
