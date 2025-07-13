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

func DebugCopy(ctx context.Context, name string, w io.Writer, r io.Reader) (done chan struct{}) {

	startTime := time.Now()

	id := "DEBUGCOPY:" + name

	lw := NewDebugWriter(ctx, id, w)
	lr := NewDebugReader(ctx, id, r)

	done = make(chan struct{})

	go func() {
		if w, ok := w.(io.WriteCloser); ok {
			defer w.Close()
		}
		if r, ok := r.(io.ReadCloser); ok {
			defer r.Close()
		}
		defer close(done)
		defer ticker.NewTicker(
			ticker.WithLogLevel(slog.LevelDebug),
			ticker.WithFrequency(15),
			ticker.WithSlogBaseContext(ctx),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.String("name", name),
					slog.Any("read_stats", lr),
					slog.Any("write_stats", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		mw := io.MultiWriter(w, lw)
		n, err := io.Copy(mw, lr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "copy_err", err, "bytes", n, "duration", time.Since(startTime), "read_stats", lr, "write_stats", lw)
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
					slog.String("name", name),
					slog.Any("read_stats", lr),
					// slog.Any("write_stats", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		n, err := io.Copy(lw, lr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "read_stats", lr)
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
					slog.String("name", name),
					slog.Any("write_stats", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		n, err := io.Copy(lw, lr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "write_stats", lw)
	}()

	return pw, []io.Closer{pr, w, pw}

}
