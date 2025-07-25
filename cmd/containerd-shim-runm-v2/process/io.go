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
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/stdio"
	"github.com/containerd/fifo"
	"github.com/containerd/log"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/pkg/conn"
)

const binaryIOProcTermTimeout = 12 * time.Second // Give logger process solid 10 seconds for cleanup

var bufPool = sync.Pool{
	New: func() interface{} {
		// setting to 4096 to align with PIPE_BUF
		// http://man7.org/linux/man-pages/man7/pipe.7.html
		buffer := make([]byte, 4096)
		return &buffer
	},
}

type processIO struct {
	io runtime.IO

	uri   *url.URL
	copy  bool
	stdio stdio.Stdio
}

func (p *processIO) Close() error {
	if p.io != nil {
		return p.io.Close()
	}
	return nil
}

func (p *processIO) IO() runtime.IO {
	return p.io
}

func (p *processIO) Copy(ctx context.Context, wg *sync.WaitGroup) error {
	slog.InfoContext(ctx, "copying pipes", "pio_is_nil", p == nil, "stdin_is_null", p.IO().Stdin() == nil, "stdout_is_null", p.IO().Stdout() == nil, "stderr_is_null", p.IO().Stderr() == nil)
	if !p.copy {
		return nil
	}
	var cwg sync.WaitGroup
	if err := copyPipes(ctx, p.IO(), p.stdio.Stdin, p.stdio.Stdout, p.stdio.Stderr, wg, &cwg); err != nil {
		return errors.Errorf("unable to copy pipes: %w", err)
	}
	cwg.Wait()
	return nil
}

func createIO(ctx context.Context, id string, ioUID, ioGID int, stdio stdio.Stdio, runtime runtime.Runtime) (*processIO, error) {
	pio := &processIO{
		stdio: stdio,
	}
	if stdio.IsNull() {
		i, err := runtime.NewNullIO()
		if err != nil {
			return nil, errors.Errorf("failed to create null IO: %w", err)
		}
		pio.io = i
		return pio, nil
	}
	u, err := url.Parse(stdio.Stdout)
	if err != nil {
		return nil, errors.Errorf("unable to parse stdout uri: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "fifo"
	}
	pio.uri = u
	switch u.Scheme {
	case "fifo":
		pio.copy = true
		slog.InfoContext(ctx, "creating pipe IO", "ioUID", ioUID, "ioGID", ioGID, "stdin", stdio.Stdin, "stdout", stdio.Stdout, "stderr", stdio.Stderr)
		pio.io, err = runtime.NewPipeIO(ctx, ioUID, ioGID, withConditionalIO(stdio))
	case "binary":
		slog.InfoContext(ctx, "creating binary IO", "uri", u.String(), "ioUID", ioUID, "ioGID", ioGID, "stdin", stdio.Stdin, "stdout", stdio.Stdout, "stderr", stdio.Stderr)
		bi, err := NewBinaryIO(ctx, id, u)
		if err != nil {
			return nil, errors.Errorf("failed to create binary IO: %w", err)
		}
		pio.io, err = wrapBinaryIO(ctx, runtime, bi, ioUID, ioGID)
		if err != nil {
			return nil, errors.Errorf("failed to wrap binary IO: %w", err)
		}
	case "file":
		filePath := u.Path
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return nil, errors.Errorf("failed to create directory: %w", err)
		}
		var f *os.File
		f, err = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, errors.Errorf("failed to create null IO: %w", err)
		}
		f.Close()
		pio.stdio.Stdout = filePath
		pio.stdio.Stderr = filePath
		pio.copy = true
		slog.InfoContext(ctx, "creating pipe IO (via file)", "ioUID", ioUID, "ioGID", ioGID, "stdio", stdio)
		pio.io, err = runtime.NewPipeIO(ctx, ioUID, ioGID, withConditionalIO(stdio))
	default:
		return nil, errors.Errorf("unknown STDIO scheme %s", u.Scheme)
	}
	if err != nil {
		return nil, errors.Errorf("failed to create process IO [scheme: %s]: %w", u.Scheme, err)
	}
	return pio, nil
}

func copyPipes(ctx context.Context, rio runtime.IO, stdin, stdout, stderr string, wg, cwg *sync.WaitGroup) error {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic in copyPipes main", "error", r)
			slog.ErrorContext(ctx, string(debug.Stack()))
			panic(r)
		}
	}()
	var sameFile *countingWriteCloser
	for _, i := range []struct {
		name string
		dest func(wc io.WriteCloser, rc io.Closer)
	}{
		{
			name: stdout,
			dest: func(wc io.WriteCloser, rc io.Closer) {
				wg.Add(1)
				cwg.Add(1)
				go func() {
					defer func() {
						if r := recover(); r != nil {
							slog.ErrorContext(ctx, "panic in copyPipes stdout", "error", r)
							slog.ErrorContext(ctx, string(debug.Stack()))
							panic(r)
						}
					}()
					cwg.Done()
					p := bufPool.Get().(*[]byte)
					defer bufPool.Put(p)
					if _, err := io.CopyBuffer(wc, rio.Stdout(), *p); err != nil {
						log.G(ctx).Warn("error copying stdout")
					}
					wg.Done()
					wc.Close()
					if rc != nil {
						rc.Close()
					}
				}()
			},
		}, {
			name: stderr,
			dest: func(wc io.WriteCloser, rc io.Closer) {
				wg.Add(1)
				cwg.Add(1)
				go func() {
					defer func() {
						if r := recover(); r != nil {
							slog.ErrorContext(ctx, "panic in copyPipes stderr", "error", r)
							slog.ErrorContext(ctx, string(debug.Stack()))
							panic(r)
						}
					}()
					cwg.Done()
					p := bufPool.Get().(*[]byte)
					defer bufPool.Put(p)
					if _, err := io.CopyBuffer(wc, rio.Stderr(), *p); err != nil {
						log.G(ctx).Warn("error copying stderr")
					}
					wg.Done()
					wc.Close()
					if rc != nil {
						rc.Close()
					}
				}()
			},
		},
	} {
		ok, err := fifo.IsFifo(i.name)
		if err != nil {
			return err
		}
		var (
			fw io.WriteCloser
			fr io.Closer
		)
		if ok {
			if fw, err = fifo.OpenFifo(ctx, i.name, syscall.O_WRONLY, 0); err != nil {
				return errors.Errorf("containerd-shim: opening w/o fifo %q failed: %w", i.name, err)
			}
			if fr, err = fifo.OpenFifo(ctx, i.name, syscall.O_RDONLY, 0); err != nil {
				return errors.Errorf("containerd-shim: opening r/o fifo %q failed: %w", i.name, err)
			}
		} else {
			if sameFile != nil {
				sameFile.bumpCount(1)
				i.dest(sameFile, nil)
				continue
			}
			if fw, err = os.OpenFile(i.name, syscall.O_WRONLY|syscall.O_APPEND, 0); err != nil {
				return errors.Errorf("containerd-shim: opening file %q failed: %w", i.name, err)
			}
			if stdout == stderr {
				sameFile = newCountingWriteCloser(fw, 1)
			}
		}
		i.dest(fw, fr)
	}
	if stdin == "" {
		return nil
	}
	f, err := fifo.OpenFifo(context.Background(), stdin, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errors.Errorf("containerd-shim: opening %s failed: %s", stdin, err)
	}
	cwg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "panic in copyPipes", "error", r)
				panic(r)
			}
		}()
		cwg.Done()
		p := bufPool.Get().(*[]byte)
		defer bufPool.Put(p)

		io.CopyBuffer(rio.Stdin(), f, *p)
		rio.Stdin().Close()
		f.Close()
	}()
	return nil
}

// countingWriteCloser masks io.Closer() until close has been invoked a certain number of times.
type countingWriteCloser struct {
	io.WriteCloser
	count atomic.Int64
}

func newCountingWriteCloser(c io.WriteCloser, count int64) *countingWriteCloser {
	cwc := &countingWriteCloser{
		c,
		atomic.Int64{},
	}
	cwc.bumpCount(count)
	return cwc
}

func (c *countingWriteCloser) bumpCount(delta int64) int64 {
	return c.count.Add(delta)
}

func (c *countingWriteCloser) Close() error {
	if c.bumpCount(-1) > 0 {
		return nil
	}
	return c.WriteCloser.Close()
}

// NewBinaryIO runs a custom binary process for pluggable shim logging
func NewBinaryIO(ctx context.Context, id string, uri *url.URL) (_ *binaryIO, err error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	var closers []func() error
	defer func() {
		if err == nil {
			return
		}
		result := []error{err}
		for _, fn := range closers {
			result = append(result, fn())
		}
		err = errors.Join(result...)
	}()

	out, err := newPipe()
	if err != nil {
		return nil, errors.Errorf("failed to create stdout pipes: %w", err)
	}
	closers = append(closers, out.Close)

	serr, err := newPipe()
	if err != nil {
		return nil, errors.Errorf("failed to create stderr pipes: %w", err)
	}
	closers = append(closers, serr.Close)

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	closers = append(closers, r.Close, w.Close)

	cmd := NewBinaryCmd(uri, id, ns)
	cmd.ExtraFiles = append(cmd.ExtraFiles, out.r, serr.r, w)
	// don't need to register this with the reaper or wait when
	// running inside a shim
	if err := cmd.Start(); err != nil {
		return nil, errors.Errorf("failed to start binary process: %w", err)
	}
	closers = append(closers, func() error { return cmd.Process.Kill() })

	// close our side of the pipe after start
	if err := w.Close(); err != nil {
		return nil, errors.Errorf("failed to close write pipe after start: %w", err)
	}

	// wait for the logging binary to be ready
	b := make([]byte, 1)
	if _, err := r.Read(b); err != nil && err != io.EOF {
		return nil, errors.Errorf("failed to read from logging binary: %w", err)
	}

	return &binaryIO{
		cmd: cmd,
		out: out,
		err: serr,
	}, nil
}

type binaryIO struct {
	cmd      *exec.Cmd
	out, err *pipe
}

func (b *binaryIO) CloseAfterStart() error {
	var result []error

	for _, v := range []*pipe{b.out, b.err} {
		if v != nil {
			if err := v.r.Close(); err != nil {
				result = append(result, err)
			}
		}
	}

	return errors.Join(result...)
}

func (b *binaryIO) Close() error {
	var result []error

	for _, v := range []*pipe{b.out, b.err} {
		if v != nil {
			if err := v.Close(); err != nil {
				result = append(result, err)
			}
		}
	}

	if err := b.cancel(); err != nil {
		result = append(result, err)
	}

	return errors.Join(result...)
}

func (b *binaryIO) cancel() error {
	if b.cmd == nil || b.cmd.Process == nil {
		return nil
	}

	// Send SIGTERM first, so logger process has a chance to flush and exit properly
	if err := b.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		result := []error{errors.Errorf("failed to send SIGTERM: %w", err)}

		log.L.WithError(err).Warn("failed to send SIGTERM signal, killing logging shim")

		if err := b.cmd.Process.Kill(); err != nil {
			result = append(result, errors.Errorf("failed to kill process after faulty SIGTERM: %w", err))
		}

		return errors.Join(result...)
	}

	done := make(chan error, 1)
	go func() {
		done <- b.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(binaryIOProcTermTimeout):
		log.L.Warn("failed to wait for shim logger process to exit, killing")

		err := b.cmd.Process.Kill()
		if err != nil {
			return errors.Errorf("failed to kill shim logger process: %w", err)
		}

		return nil
	}
}

func (b *binaryIO) Stdin() io.WriteCloser {
	return nil
}

func (b *binaryIO) Stdout() io.ReadCloser {
	return nil
}

func (b *binaryIO) Stderr() io.ReadCloser {
	return nil
}

func (b *binaryIO) Set(cmd *exec.Cmd) {
	if b.out != nil {
		cmd.Stdout = b.out.w
	}
	if b.err != nil {
		cmd.Stderr = b.err.w
	}
}

func newPipe() (*pipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &pipe{
		r: r,
		w: w,
	}, nil
}

type pipe struct {
	r *os.File
	w *os.File
}

func (p *pipe) Close() error {
	var result []error

	if err := p.w.Close(); err != nil {
		result = append(result, errors.Errorf("pipe: failed to close write pipe: %w", err))
	}

	if err := p.r.Close(); err != nil {
		result = append(result, errors.Errorf("pipe: failed to close read pipe: %w", err))
	}

	return errors.Join(result...)
}

func wrapBinaryIO(ctx context.Context, run runtime.Runtime, binIO *binaryIO, u, g int) (runtime.IO, error) {
	// create the pipeio from the runtime
	pipeIO, err := run.NewPipeIO(ctx, u, g, withRawIOOpt(func(o *gorunc.IOOption) {
		o.OpenStdin = false
		o.OpenStdout = true
		o.OpenStderr = true
	}))
	if err != nil {
		return nil, errors.Errorf("failed to create pipeIO: %w", err)
	}

	cmdname := filepath.Base(binIO.cmd.Args[0])

	go func() {
		<-conn.DebugCopy(ctx, cmdname+":stdout:pipe(read)->binary(write)", binIO.out.w, pipeIO.Stdout())
	}()
	go func() {
		<-conn.DebugCopy(ctx, cmdname+":stderr:pipe(read)->binary(write)", binIO.err.w, pipeIO.Stderr())
	}()

	return pipeIO, nil
}
