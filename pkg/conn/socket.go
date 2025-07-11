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

	lw := &logWriter{ctx: ctx, name: id}
	cr := &countReader{r: r, ncount: 0, callCount: 0}

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
					slog.Uint64("read_ncount", cr.ncount),
					slog.Uint64("write_ncount", lw.ncount),
					slog.Uint64("read_call_count", cr.callCount),
					slog.Uint64("write_call_count", lw.callCount),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		mw := io.MultiWriter(w, lw)
		n, err := io.Copy(mw, cr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "copy_err", err, "bytes", n, "duration", time.Since(startTime), "rncount", cr.ncount, "rcall_count", cr.callCount, "wncount", lw.ncount, "wcall_count", lw.callCount)
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

	lw := &logWriter{ctx: ctx, name: id}
	cr := &countReader{r: r, ncount: 0, callCount: 0}

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
					slog.Uint64("read_ncount", cr.ncount),
					slog.Uint64("write_ncount", lw.ncount),
					slog.Uint64("read_call_count", cr.callCount),
					slog.Uint64("write_call_count", lw.callCount),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		mw := io.MultiWriter(pw, lw)
		n, err := io.Copy(mw, cr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "rncount", cr.ncount, "rcall_count", cr.callCount, "wncount", lw.ncount, "wcall_count", lw.callCount)
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

	lw := &logWriter{ctx: ctx, name: id}
	cr := &countReader{r: pr, ncount: 0, callCount: 0}

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
					slog.Uint64("read_ncount", cr.ncount),
					slog.Uint64("write_ncount", lw.ncount),
					slog.Uint64("read_call_count", cr.callCount),
					slog.Uint64("write_call_count", lw.callCount),
				}
			}),
			ticker.WithMessageFunc(func() string {
				return fmt.Sprintf("%s[RUNNING]", id)
			}),
			ticker.WithStartBurst(5),
		).RunAsDefer()()
		mw := io.MultiWriter(w, lw)
		n, err := io.Copy(mw, cr)
		slog.Debug(fmt.Sprintf("%s[DONE]", id), "name", name, "duration", time.Since(startTime), "copy_err", err, "bytes", n, "rncount", cr.ncount, "rcall_count", cr.callCount, "wncount", lw.ncount, "wcall_count", lw.callCount)
	}()

	return pw, []io.Closer{pr, w, pw}

}
