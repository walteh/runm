package runtime

import (
	"context"
	"io"
	"log/slog"
	"os/exec"

	gorunc "github.com/containerd/go-runc"
)

var _ IO = &HostAllocatedStdio{}

var _ ReferableByReferenceId = &HostAllocatedStdio{}

var _ IO = &IoLogProxy{}

type HostAllocatedStdio struct {
	StdinSocket  AllocatedSocket
	StdoutSocket AllocatedSocket
	StderrSocket AllocatedSocket
	ReferenceId  string
}

func (p *HostAllocatedStdio) GetReferenceId() string {
	return p.ReferenceId
}

func NewHostAllocatedStdio(ctx context.Context, referenceId string, stdinRef, stdoutRef, stderrRef AllocatedSocket) *HostAllocatedStdio {
	return &HostAllocatedStdio{
		StdinSocket:  stdinRef,
		StdoutSocket: stdoutRef,
		StderrSocket: stderrRef,
		ReferenceId:  referenceId,
	}
}

func (p *HostAllocatedStdio) Stdin() io.WriteCloser {
	if p.StdinSocket == nil {
		return nil
	}
	return p.StdinSocket.Conn()
}

func (p *HostAllocatedStdio) Stdout() io.ReadCloser {
	if p.StdoutSocket == nil {
		return nil
	}
	return p.StdoutSocket.Conn()
}

func (p *HostAllocatedStdio) Stderr() io.ReadCloser {
	if p.StderrSocket == nil {
		return nil
	}
	return p.StderrSocket.Conn()
}

func (p *HostAllocatedStdio) Set(cmd *exec.Cmd) {
	if p.StdinSocket != nil {
		cmd.Stdin = p.StdinSocket.Conn()
	}
	if p.StdoutSocket != nil {
		cmd.Stdout = p.StdoutSocket.Conn()
	}
	if p.StderrSocket != nil {
		cmd.Stderr = p.StderrSocket.Conn()
	}
}

func (p *HostAllocatedStdio) Close() error {
	if p.StdinSocket != nil {
		p.StdinSocket.Close()
	}
	if p.StdoutSocket != nil {
		p.StdoutSocket.Close()
	}
	if p.StderrSocket != nil {
		p.StderrSocket.Close()
	}
	return nil
}

type HostNullIo struct {
	IO
}

func NewHostNullIo() (*HostNullIo, error) {
	io, err := gorunc.NewNullIO()
	if err != nil {
		return nil, err
	}
	return &HostNullIo{
		IO: io,
	}, nil
}

type IoLogProxy struct {
	original IO
	output   io.Writer
}

func NewIoLogProxy(original IO, output io.Writer) *IoLogProxy {
	return &IoLogProxy{
		original: original,
		output:   output,
	}
}

// Forward original IO methods
func (p *IoLogProxy) Stdin() io.WriteCloser {
	return p.original.Stdin()
}

func (p *IoLogProxy) Stdout() io.ReadCloser {
	return p.original.Stdout()
}

func (p *IoLogProxy) Stderr() io.ReadCloser {
	return p.original.Stderr()
}

// The key method where we set up stdout/stderr logging
func (p *IoLogProxy) Set(cmd *exec.Cmd) {
	// First let the original IO set up stdin/stdout/stderr
	p.original.Set(cmd)

	// Now set up pipes for stdout and stderr
	if cmd.Stdout != nil {
		originalStdout := cmd.Stdout
		stdoutR, stdoutW := io.Pipe()

		// Replace the command's stdout with our pipe writer
		cmd.Stdout = stdoutW

		// Start a goroutine to copy data from the pipe to both destinations
		go func() {
			defer stdoutW.Close()

			// Copy data from pipe reader to both original stdout and our log output
			_, err := io.Copy(io.MultiWriter(originalStdout, p.output), stdoutR)
			if err != nil {
				// Just log the error and continue
				slog.Error("error copying stdout", "error", err)
			}
		}()
	}

	if cmd.Stderr != nil {
		originalStderr := cmd.Stderr
		stderrR, stderrW := io.Pipe()

		// Replace the command's stderr with our pipe writer
		cmd.Stderr = stderrW

		// Start a goroutine to copy data from the pipe to both destinations
		go func() {
			defer stderrW.Close()

			// Copy data from pipe reader to both original stderr and our log output
			_, err := io.Copy(io.MultiWriter(originalStderr, p.output), stderrR)
			if err != nil {
				// Just log the error and continue
				slog.Error("error copying stderr", "error", err)
			}
		}()
	}
}

// Pass through to the original IO's Close
func (p *IoLogProxy) Close() error {
	return p.original.Close()
}

// Write implements the io.Writer interface
func (p *IoLogProxy) Write(b []byte) (n int, err error) {
	return p.output.Write(b)
}
