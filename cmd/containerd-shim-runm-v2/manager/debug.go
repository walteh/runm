package manager

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/v2/pkg/shim"
	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"
)

type debugManager struct {
	ref shim.Manager
}

func NewDebugManager(m shim.Manager) shim.Manager {
	return &debugManager{m}
}

func wrap[I, O any](f func(context.Context, I) (O, error)) func(context.Context, I) (O, error) {
	pc, _, _, _ := runtime.Caller(1)
	funcName := runtime.FuncForPC(pc).Name()
	realNameS := strings.Split(filepath.Base(funcName), ".")
	realName := realNameS[len(realNameS)-1]

	return func(ctx context.Context, req I) (resp O, retErr error) {
		start := time.Now()

		startLogRecord := slog.NewRecord(start, slog.LevelInfo, strings.ToUpper(realName)+"_START", pc)
		startLogRecord.AddAttrs(
			slog.String("method", realName),
		)
		slog.Default().Handler().Handle(ctx, startLogRecord)

		defer func() {
			end := time.Now()
			endLogRecord := slog.NewRecord(end, slog.LevelInfo, strings.ToUpper(realName)+"_END", pc)
			endLogRecord.AddAttrs(
				slog.String("method", realName),
				slog.Duration("duration", end.Sub(start)),
			)
			slog.Default().Handler().Handle(ctx, endLogRecord)
			if err := recover(); err != nil {
				slog.ErrorContext(ctx, "panic in manager service", "error", err)
				retErr = errors.Errorf("panic in manager service in %s: %s", realName, err)
			}
		}()

		ctx = slogctx.Append(ctx, slog.String("manager_method", realName))

		resp, retErr = f(ctx, req)

		end := time.Now()

		if retErr != nil {
			if trac, ok := retErr.(errors.E); ok {
				pc = trac.StackTrace()[0]
			}

			rec := slog.NewRecord(end, slog.LevelError, "error in manager service", pc)
			rec.AddAttrs(
				slog.Any("error", retErr),
				slog.String("method", realName),
				slog.Duration("duration", end.Sub(start)),
			)
			if err := slog.Default().Handler().Handle(ctx, rec); err != nil {
				slog.ErrorContext(ctx, "error logging error", "error", err)
			}
		} else {
			rec := slog.NewRecord(end, slog.LevelInfo, "success in manager service", pc)
			rec.AddAttrs(
				slog.String("method", realName),
				slog.Duration("duration", end.Sub(start)),
			)
			if err := slog.Default().Handler().Handle(ctx, rec); err != nil {
				slog.ErrorContext(ctx, "error logging success", "error", err)
			}
		}

		return resp, retErr
	}
}

// Name implements shim.Manager
func (d *debugManager) Name() string {
	return d.ref.Name()
}

// Start implements shim.Manager
func (d *debugManager) Start(ctx context.Context, id string, opts shim.StartOpts) (shim.BootstrapParams, error) {
	return wrap(func(ctx context.Context, id string) (shim.BootstrapParams, error) {
		return d.ref.Start(ctx, id, opts)
	})(ctx, id)
}

// Stop implements shim.Manager
func (d *debugManager) Stop(ctx context.Context, id string) (shim.StopStatus, error) {
	return wrap(func(ctx context.Context, id string) (shim.StopStatus, error) {
		return d.ref.Stop(ctx, id)
	})(ctx, id)
}

// Info implements shim.Manager
func (d *debugManager) Info(ctx context.Context, reader io.Reader) (*types.RuntimeInfo, error) {
	return wrap(func(ctx context.Context, r io.Reader) (*types.RuntimeInfo, error) {
		return d.ref.Info(ctx, r)
	})(ctx, reader)
}
