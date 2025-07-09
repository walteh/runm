package socket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/pkg/ticker"
)

type breakOnZeroReader struct {
	r io.Reader
}

func (b *breakOnZeroReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

type logWriter struct {
	ctx           context.Context
	name          string
	count         uint64
	buf           []byte
	hasBeenClosed bool
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		idx := bytes.IndexByte(l.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(l.buf[:idx])
		slog.DebugContext(l.ctx, fmt.Sprintf("COPY:LINE:%s[DATA]", l.name), "data", line, "count", l.count)
		l.count++
		l.buf = l.buf[idx+1:]
	}
	return len(p), nil
}

func (l *logWriter) Close() error {
	l.hasBeenClosed = true
	slog.InfoContext(l.ctx, fmt.Sprintf("COPY:LINE:%s[CLOSED]", l.name), "count", l.count)
	return nil
}

type allocatedSocketIO struct {
	allocatedSockets [3]runtime.AllocatedSocket

	extraClosers []io.Closer
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
	if a.allocatedSockets[0] != nil {
		pr, closers := pipeProxyReader("stdin", a.allocatedSockets[0].Conn())
		a.extraClosers = append(a.extraClosers, closers...)
		cmd.Stdin = pr
	}
	if a.allocatedSockets[1] != nil {
		pw, closers := pipeProxyWriter("stdout", a.allocatedSockets[1].Conn())
		a.extraClosers = append(a.extraClosers, closers...)
		cmd.Stdout = pw
	}
	if a.allocatedSockets[2] != nil {
		pw, closers := pipeProxyWriter("stderr", a.allocatedSockets[2].Conn())
		a.extraClosers = append(a.extraClosers, closers...)
		cmd.Stderr = pw
	}

}

func bind(name string, w io.WriteCloser, r io.ReadCloser) {

	go func() {
		defer w.Close()
		defer r.Close()
		defer ticker.NewTicker(
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithCallerSkip(2),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.String("name", name),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("COPY:BIND:LINE:%s[RUNNING]", name)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		mr := io.TeeReader(r, &logWriter{ctx: context.Background(), name: name})
		n, err := io.Copy(w, mr)
		slog.Debug(fmt.Sprintf("COPY:BIND:LINE:%s[DONE]", name), "name", name, "copy_err", err, "bytes", n)
	}()
}

func pipeProxyReader(name string, r runtime.FileConn) (io.ReadCloser, []io.Closer) {
	pr, pw, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("failed to create pipe: %v", err))
	}

	startTime := time.Now()

	go func() {
		defer pr.Close()
		defer r.Close()
		defer ticker.NewTicker(
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithCallerSkip(2),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.String("name", name),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("COPY:LINE:%s[RUNNING]", name)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		mr := io.TeeReader(r, &logWriter{ctx: context.Background(), name: name})
		n, err := io.Copy(pw, mr)
		slog.Debug(fmt.Sprintf("COPY:LINE:%s[DONE]", name), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n)
	}()

	return pr, []io.Closer{pw, r}
}

func pipeProxyWriter(name string, w runtime.FileConn) (io.WriteCloser, []io.Closer) {
	startTime := time.Now()

	pr, pw, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("failed to create pipe: %v", err))
	}

	go func() {
		defer pr.Close()
		defer w.Close()
		defer ticker.NewTicker(
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithCallerSkip(2),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.String("name", name),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("COPY:LINE:%s[RUNNING]", name)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		mw := io.MultiWriter(w, &logWriter{ctx: context.Background(), name: name})
		n, err := io.Copy(mw, pr)
		slog.Debug(fmt.Sprintf("COPY:LINE:%s[DONE]", name), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n)
	}()

	return pw, []io.Closer{pr, w, pw}

}

func NewAllocatedSocketIO(stdin, stdout, stderr runtime.AllocatedSocket) runtime.IO {
	return &allocatedSocketIO{
		allocatedSockets: [3]runtime.AllocatedSocket{stdin, stdout, stderr},
		extraClosers:     []io.Closer{},
	}
}

// func debugReader(ctx context.Context, name string, r io.Reader) io.Reader {
// 	pr, pw := io.Pipe()
// 	tr := io.TeeReader(r, pw)
// 	// reaqd one byte at a time from r and print it to stdout
// 	go func() {
// 		slog.InfoContext(ctx, "starting debugReader", "name", name)
// 		for ctx.Err() == nil {

// 			buf := make([]byte, 1)
// 			_, err := pr.Read(buf)
// 			if err != nil {
// 				return
// 			}
// 			slog.InfoContext(ctx, "captured byte", "name", name, "byte", buf[0])
// 		}
// 	}()
// 	return tr
// }

// func simpleUnixProxyConn(ctx context.Context, sock runtime.AllocatedSocket) (*net.UnixConn, error) {
// 	if sockConn, ok := sock.Conn().(*net.UnixConn); ok {
// 		return sockConn, nil
// 	}
// 	// create a socketpair
// 	return conn.CreateUnixConnProxy(ctx, sock.Conn())
// }

// runc.ConsoleSocket -> console.Console -> my.Socket
// BindConsoleToSocket implements runtime.SocketAllocator.

// func halfCloseWrite(conn net.Conn) {
// 	// Attempt a half-close if supported (e.g., Unix domain or TCP)
// 	if uc, ok := conn.(*net.UnixConn); ok {
// 		uc.CloseWrite() // sends FIN (EOF) to peer  [oai_citation:8‡github.com](https://github.com/golang/go/issues/67337?utm_source=chatgpt.com)
// 		conn.Close()
// 	} else {
// 		conn.Close() // fallback: full close
// 	}

// 	conn.Close()
// }
// }

// type pipeProxy struct {
// 	pw      *os.File
// 	closers []io.Closer
// }

// func (p *pipeProxy) Write(b []byte) (int, error) {
// 	return p.pw.Write(b)
// }

// func (p *pipeProxy) Close() error {
// 	err := p.pw.Close()
// 	for _, closer := range p.closers {
// 		go closer.Close()
// 	}
// 	return err
// }

// func pipeProxyWriterAlt(name string, w runtime.FileConn) io.WriteCloser {
// 	startTime := time.Now()

// 	pr, pw, err := os.Pipe()
// 	if err != nil {
// 		slog.Error("failed to create pipe", "error", err)
// 	}

// 	proxy := &pipeProxy{
// 		pw:      pw,
// 		closers: []io.Closer{pr, w},
// 	}

// 	go func() {
// 		defer pr.Close()
// 		defer w.Close()
// 		defer ticker.NewTicker(
// 			ticker.WithLogLevel(slog.LevelDebug),
// 			ticker.WithFrequency(15),
// 			ticker.WithCallerSkip(2),
// 			ticker.WithAttrFunc(func() []slog.Attr {
// 				return []slog.Attr{
// 					slog.String("name", name),
// 				}
// 			}),
// 			ticker.WithMessageFunc(func() string {
// 				return fmt.Sprintf("COPY:LINE:%s[RUNNING]", name)
// 			}),
// 			ticker.WithDoneMessage(fmt.Sprintf("COPY:LINE:%s[DONE]", name)),
// 			ticker.WithStartBurst(5),
// 		).RunAsDefer()()
// 		mw := io.MultiWriter(w, &logWriter{ctx: context.Background(), name: name})
// 		n, err := io.Copy(mw, pr)
// 		slog.Info("copy pipe done", "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n)
// 	}()

// 	return proxy
// }

// func copyAndLogLines(ctx context.Context, name string, w io.WriteCloser, r io.ReadCloser) {

// 	lw := &logWriter{ctx: ctx, name: name}
// 	mw := io.MultiWriter(w, lw)

// 	defer ticker.NewTicker(
// 		ticker.WithSlogBaseContext(ctx),
// 		ticker.WithLogLevel(slog.LevelDebug),
// 		ticker.WithFrequency(15),
// 		ticker.WithCallerSkip(2),
// 		ticker.WithAttrFunc(func() []slog.Attr {
// 			return []slog.Attr{
// 				slog.String("name", name),
// 				slog.Uint64("count", lw.count),
// 				slog.Bool("log_writer_closed", lw.hasBeenClosed),
// 				slog.Bool("original_writer_closed", ctw.hasBeenClosed),
// 				slog.Bool("multi_writer_closed", mwt.hasBeenClosed),
// 			}
// 		}),
// 		ticker.WithMessageFunc(func() string {
// 			return fmt.Sprintf("COPY:LINE:%s[RUNNING]", name)
// 		}),
// 		ticker.WithStartBurst(5),
// 	).RunAsDefer()()

// 	n, err := io.Copy(mw, r)

// 	<-ctw.done

// 	return n, err
// }

// func BindIOToSockets(ctx context.Context, ios runtime.IO, stdin, stdout, stderr runtime.AllocatedSocket) error {

// 	if ios == nil {
// 		return errors.Errorf("ios is nil")
// 	}

// 	var wg sync.WaitGroup

// 	// stdin: host socket → container Stdin()
// 	if stdin != nil {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			if err := stdin.Ready(); err != nil {
// 				slog.ErrorContext(ctx, "failed to ready stdin", "error", err)
// 				return
// 			}
// 			slog.InfoContext(ctx, "copying stdin")
// 			_ := copyAndLogLines(ctx, "stdin", ios.Stdin(), stdin.Conn())
// 			if err != nil {
// 				slog.WarnContext(ctx, "closing io.Stdin with error", "error", err)
// 			} else {
// 				slog.InfoContext(ctx, "closing io.Stdin with NO error")
// 			}
// 			ios.Stdin().Close() // close FIFO writer to signal EOF
// 			halfCloseWrite(stdin.Conn())
// 		}()
// 	}

// 	// stdout: container Stdout() → host socket
// 	if stdout != nil {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			if err := stdout.Ready(); err != nil {
// 				slog.ErrorContext(ctx, "failed to ready stdout", "error", err)
// 				return
// 			}
// 			slog.InfoContext(ctx, "copying stdout")
// 			_, err := copyAndLogLines(ctx, "stdout", stdout.Conn(), ios.Stdout())
// 			if err != nil {
// 				slog.WarnContext(ctx, "closing io.Stdout with error", "error", err)
// 			} else {
// 				slog.InfoContext(ctx, "closing io.Stdout with NO error")
// 			}
// 			halfCloseWrite(stdout.Conn()) // signal EOF to guest reader
// 		}()
// 	}

// 	// stderr: container Stderr() → host socket
// 	if stderr != nil {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			if err := stderr.Ready(); err != nil {
// 				slog.ErrorContext(ctx, "failed to ready stderr", "error", err)
// 				return
// 			}
// 			slog.InfoContext(ctx, "copying stderr")
// 			_, err := copyAndLogLines(ctx, "stderr", stderr.Conn(), ios.Stderr())
// 			if err != nil {
// 				slog.WarnContext(ctx, "closing io.Stderr with error", "error", err)
// 			} else {
// 				slog.InfoContext(ctx, "closing io.Stderr with NO error")
// 			}
// 			halfCloseWrite(stderr.Conn()) // signal EOF
// 		}()
// 	}

// 	return nil
// }

// type allocatedSocketWithUnixConn struct {
// 	runtime.AllocatedSocket
// 	unixConn *net.UnixConn
// }

// func NewAllocatedSocketWithUnixConn(socket runtime.AllocatedSocket) runtime.AllocatedSocketWithUnixConn {
// 	if unixConn, ok := socket.(runtime.AllocatedSocketWithUnixConn); ok {
// 		return unixConn
// 	}
// 	if unixConn, ok := socket.(*SimpleVsockProxyConn); ok {
// 		return &allocatedSocketWithUnixConn{socket: socket, unixConn: unixConn.UnixConn()}
// 	}
// 	return nil
// }

// func (a *allocatedSocketWithUnixConn) forward() (*net.UnixConn, error) {
// 	// create a socketpair
// 	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
// 	if err != nil {
// 		return nil, errors.Errorf("creating socketpair: %w", err)
// 	}
// 	unixConn, err := net.FileConn(os.NewFile(uintptr(fds[0]), "external.socket"))
// 	if err != nil {
// 		return nil, errors.Errorf("converting file to UnixConn: %w", err)
// 	}
// 	proxyConn, err := net.FileConn(os.NewFile(uintptr(fds[1]), "internal.socket"))
// 	if err != nil {
// 		return nil, errors.Errorf("converting file to UnixConn: %w", err)
// 	}
// 	return proxyConn, nil
// }
