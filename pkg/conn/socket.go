package conn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"reflect"
	"time"

	"github.com/containerd/console"
	"github.com/mdlayher/vsock"

	"github.com/walteh/runm/pkg/ticker"
)

func DebugCopy(ctx context.Context, name string, w io.Writer, r io.Reader) (done chan error) {
	return DebugCopyWithBuffer(ctx, name, w, r, nil)
}

func DebugCopyWithBuffer(ctx context.Context, name string, w io.Writer, r io.Reader, buffer []byte) (done chan error) {

	startTime := time.Now()

	id := "DEBUGCOPY:" + name

	lw := NewDebugWriter(ctx, id, w).(*debugWriter)

	lr := NewDebugReader(ctx, id, r).(*debugReader)

	lr.copiedWriter = lw
	lw.copiedReader = lr

	done = make(chan error)

	go func() {
		// if w, ok := w.(io.WriteCloser); ok {
		// 	defer w.Close()
		// }
		// if r, ok := r.(io.ReadCloser); ok {
		// 	defer r.Close()
		// }
		var err error
		defer func() {
			done <- err
		}()
		defer ticker.NewTicker(
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithSlogBaseContext(ctx),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.String("name", name),
					slog.Any("r", lr),
					slog.Any("w", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		// mw := io.MultiWriter(w, lw)
		var n int64
		if buffer != nil {
			n, err = io.CopyBuffer(lw, lr, buffer)
		} else {
			n, err = io.Copy(lw, lr)
		}
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "copy_err", err, "bytes", n, "duration", time.Since(startTime), "r", lr, "w", lw)
	}()

	return done
}

type FileReader interface {
	Fd() uintptr
	io.ReadCloser
}

func OsPipeProxyReader(ctx context.Context, name string, r io.ReadCloser) (FileReader, []io.Closer) {
	pr, pw, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("failed to create pipe: %v", err))
	}

	startTime := time.Now()

	id := "OS-PIPE-PROXY:READER:" + name

	lw := pw
	lr := NewDebugReader(ctx, id, r)

	// knownNamedFiles.Store(pr, namedFile{
	// 	name: "os-pipe-proxy-reader(source=" + getNameFromReadWriter(r) + ")",
	// })

	// knownNamedFiles.Store(pr, namedFile{
	// 	name: "os-pipe-reader(name=" + name + ", reader-source=" + getNameFromReadWriter(r) + ")",
	// })

	// knownNamedFiles.Store(pw, namedFile{
	// 	name: "forwarding-os-pipe-writer(name=" + name + ", reader-source=" + getNameFromReadWriter(r) + ")",
	// })

	go func() {
		defer pr.Close()
		defer r.Close()
		defer ticker.NewTicker(
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithSlogBaseContext(ctx),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.Any("r", lr),
					// slog.Any("w", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		n, err := io.Copy(lw, lr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "r", lr)
	}()

	return pr, []io.Closer{pw, r}
}

type FileWriter interface {
	Fd() uintptr
	io.WriteCloser
}

func getNameFromReadWriter(r any) string {
	if reflect.TypeOf(r).String() == "*vz.VirtioSocketConnection" {
		return fmt.Sprintf("vsock:vf(port=%d)", r.(interface{ DestinationPort() uint32 }).DestinationPort())
	}

	switch wtr := any(r).(type) {
	case *debugWriter:
		return fmt.Sprintf("debugWriter(%s:%s)", wtr.name, getNameFromReadWriter(wtr.w))
	case *debugReader:
		return fmt.Sprintf("debugReader(%s:%s)", wtr.name, getNameFromReadWriter(wtr.r))
	case *os.File:
		return fmt.Sprintf("os.File(%s)", wtr.Name())
	case *vsock.Conn:
		return fmt.Sprintf("vsock:guest(port=%d)", wtr.RemoteAddr().(*vsock.Addr).Port)

	case console.Console:
		return fmt.Sprintf("console.Console(%s)", wtr.Name())
	case net.Conn:
		return fmt.Sprintf("%s(%s)", reflect.TypeOf(wtr).String(), wtr.RemoteAddr().String())
	default:
		return reflect.TypeOf(r).String()
	}

}

func OsPipeProxyWriter(ctx context.Context, name string, w io.WriteCloser) (FileWriter, []io.Closer) {
	startTime := time.Now()

	id := "OS-PIPE-PROXY-WRITER:" + name

	// w -> pr -> pw

	pr, pw, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("failed to create pipe: %v", err))
	}

	// knownNamedFiles.Store(pr, namedFile{
	// 	name: "forwarding-os-pipe-reader(name=" + name + ", writer-source=" + getNameFromReadWriter(w) + ")",
	// })

	// knownNamedFiles.Store(pw, namedFile{
	// 	name: "os-pipe-proxy-writer(source=" + getNameFromReadWriter(w) + ")",
	// })

	lw := NewDebugWriter(ctx, id, w)
	lr := pr

	go func() {
		defer pr.Close()
		defer w.Close()
		defer ticker.NewTicker(
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithSlogBaseContext(ctx),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.Any("w", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		n, err := io.Copy(lw, lr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "w", lw)
	}()

	return pw, []io.Closer{pr, w, pw}

}
