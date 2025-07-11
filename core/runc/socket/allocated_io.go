package socket

import (
	"context"
	"io"
	"os/exec"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/pkg/conn"
)

type allocatedSocketIO struct {
	ctx              context.Context
	allocatedSockets [3]runtime.AllocatedSocket

	extraClosers []io.Closer
}

func NewAllocatedSocketIO(ctx context.Context, stdin, stdout, stderr runtime.AllocatedSocket) runtime.IO {
	return &allocatedSocketIO{
		ctx:              ctx,
		allocatedSockets: [3]runtime.AllocatedSocket{stdin, stdout, stderr},
		extraClosers:     []io.Closer{},
	}
}

func (a *allocatedSocketIO) Stdin() io.WriteCloser {
	if a.allocatedSockets[0] == nil {
		return nil
	}
	return a.allocatedSockets[0].Conn()
}

func (a *allocatedSocketIO) Stdout() io.ReadCloser {
	if a.allocatedSockets[1] == nil {
		return nil
	}
	return a.allocatedSockets[1].Conn()
}

func (a *allocatedSocketIO) Stderr() io.ReadCloser {
	if a.allocatedSockets[2] == nil {
		return nil
	}
	return a.allocatedSockets[2].Conn()
}

func (a *allocatedSocketIO) Close() error {
	for _, closer := range a.extraClosers {
		closer.Close()
	}
	if a.allocatedSockets[0] != nil {
		a.allocatedSockets[0].Close()
	}
	if a.allocatedSockets[1] != nil {
		a.allocatedSockets[1].Close()
	}
	if a.allocatedSockets[2] != nil {
		a.allocatedSockets[2].Close()
	}
	return nil
}

func (a *allocatedSocketIO) Set(cmd *exec.Cmd) {
	cmdName := cmd.Args[0]
	if a.allocatedSockets[0] != nil {
		pr, closers := conn.OsPipeProxyReader(a.ctx, cmdName+":stdin", a.allocatedSockets[0].Conn())
		a.extraClosers = append(a.extraClosers, closers...)
		cmd.Stdin = pr
	}
	if a.allocatedSockets[1] != nil {
		pw, closers := conn.OsPipeProxyWriter(a.ctx, cmdName+":stdout", a.allocatedSockets[1].Conn())
		a.extraClosers = append(a.extraClosers, closers...)
		cmd.Stdout = pw
	}
	if a.allocatedSockets[2] != nil {
		pw, closers := conn.OsPipeProxyWriter(a.ctx, cmdName+":stderr", a.allocatedSockets[2].Conn())
		a.extraClosers = append(a.extraClosers, closers...)
		cmd.Stderr = pw
	}

}
