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
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sync"
	"syscall"

	"github.com/containerd/console"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/stdio"
	"github.com/containerd/fifo"

	"github.com/walteh/runm/cmd/containerd-shim-runm-v2/process"
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

	kqueueConsole, err := p.queue.Add(console)
	if err != nil {
		return nil, err
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
			io.CopyBuffer(kqueueConsole, in, *bp)
			// we need to shutdown kqueueConsole when pipe broken
			kqueueConsole.Shutdown(p.queue.CloseConsole)
			kqueueConsole.Close()
			in.Close()
		}()
	}

	uri, err := url.Parse(stdout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse stdout uri: %w", err)
	}

	switch uri.Scheme {
	case "binary":
		ns, err := namespaces.NamespaceRequired(ctx)
		if err != nil {
			return nil, err
		}

		cmd := process.NewBinaryCmd(uri, id, ns)

		// In case of unexpected errors during logging binary start, close open pipes
		var filesToClose []*os.File

		defer func() {
			if retErr != nil {
				process.CloseFiles(filesToClose...)
			}
		}()

		// Create pipe to be used by logging binary for Stdout
		outR, outW, err := os.Pipe()
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout pipes: %w", err)
		}
		filesToClose = append(filesToClose, outR)

		// Stderr is created for logging binary but unused when terminal is true
		serrR, _, err := os.Pipe()
		if err != nil {
			return nil, fmt.Errorf("failed to create stderr pipes: %w", err)
		}
		filesToClose = append(filesToClose, serrR)

		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		filesToClose = append(filesToClose, r)

		cmd.ExtraFiles = append(cmd.ExtraFiles, outR, serrR, w)

		wg.Add(1)
		cwg.Add(1)
		go func() {
			cwg.Done()
			io.Copy(outW, kqueueConsole)
			outW.Close()
			wg.Done()
		}()

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("failed to start logging binary process: %w", err)
		}

		// Close our side of the pipe after start
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("failed to close write pipe after start: %w", err)
		}

		// Wait for the logging binary to be ready
		b := make([]byte, 1)
		if _, err := r.Read(b); err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read from logging binary: %w", err)
		}
		cwg.Wait()

	default:
		outw, err := fifo.OpenFifo(ctx, stdout, syscall.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
		outr, err := fifo.OpenFifo(ctx, stdout, syscall.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		cwg.Add(1)
		go func() {
			cwg.Done()
			buf := bufPool.Get().(*[]byte)
			defer bufPool.Put(buf)
			io.CopyBuffer(outw, kqueueConsole, *buf)

			outw.Close()
			outr.Close()
			wg.Done()
		}()
		cwg.Wait()
	}

	return kqueueConsole, nil
}

func (p *platform[T]) ShutdownConsole(ctx context.Context, cons console.Console) error {
	if p.queue == nil {
		return errors.New("uninitialized kqueuer")
	}
	kqueueConsole, ok := cons.(shutdownConsole)
	if !ok {
		return fmt.Errorf("expected kqueueConsole, got %#v", cons)
	}
	return kqueueConsole.Shutdown(p.queue.CloseConsole)
}

func (p *platform[T]) Close() error {
	return p.queue.Close()
}
