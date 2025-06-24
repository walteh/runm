package runtime

import (
	"context"
	"io"
	"os/exec"

	gorunc "github.com/containerd/go-runc"
)

var _ IO = &HostAllocatedStdio{}

var _ ReferableByReferenceId = &HostAllocatedStdio{}

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

func (p *HostAllocatedStdio) Set(stdio *exec.Cmd) {
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
