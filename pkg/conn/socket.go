package conn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/walteh/runm/pkg/ticker"
)

func DebugCopy(ctx context.Context, name string, w io.WriteCloser, r io.ReadCloser) (done chan struct{}) {

	startTime := time.Now()

	id := "DEBUGCOPY:" + name

	lw := NewDebugWriter(ctx, id, w)
	lr := NewDebugReader(ctx, id, r)

	done = make(chan struct{})

	go func() {
		defer w.Close()
		defer r.Close()
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

func OsPipeProxyReader(ctx context.Context, name string, r io.ReadCloser) (*os.File, []io.Closer) {
	pr, pw, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("failed to create pipe: %v", err))
	}

	startTime := time.Now()

	id := "COPY:PIPEPROXY:READER:" + name

	lw := NewDebugWriter(ctx, id, pw)
	lr := NewDebugReader(ctx, id, r)

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
					slog.Any("write_stats", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		n, err := io.Copy(lw, lr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "read_stats", lr, "write_stats", lw)
	}()

	return pr, []io.Closer{pw, r}
}

func OsPipeProxyWriter(ctx context.Context, name string, w io.WriteCloser) (*os.File, []io.Closer) {
	startTime := time.Now()

	id := "COPY:PIPEPROXY:WRITER:" + name

	pr, pw, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("failed to create pipe: %v", err))
	}

	lw := NewDebugWriter(ctx, id, w)
	lr := NewDebugReader(ctx, id, pr)

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
					slog.Any("read_stats", lr),
					slog.Any("write_stats", lw),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		n, err := io.Copy(lw, lr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "read_stats", lr, "write_stats", lw)
	}()

	return pw, []io.Closer{pr, w, pw}

}
