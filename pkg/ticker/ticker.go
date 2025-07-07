package ticker

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"time"
)

//go:opts
type TickerOpts struct {
	interval    time.Duration `default:"1s"`
	startBurst  int           `default:"5"`
	logLevel    slog.Level    `default:"-4"`
	frequency   int           `default:"60"`
	message     string        `default:"ticker running"`
	attrFunc    func() []slog.Attr
	doneMessage string
	callerSkip  int `default:"1"`
}

type Ticker struct {
	opts    TickerOpts
	ticker  *time.Ticker
	ticks   int
	started bool
	stopped bool
	caller  uintptr
}

func NewTicker(opts ...TickerOpt) *Ticker {
	t := newTickerOpts(opts...)
	caller, _, _, _ := runtime.Caller(t.callerSkip)

	return &Ticker{
		ticker: time.NewTicker(t.Interval()),
		opts:   t,
		caller: caller,
	}
}

func (t *Ticker) Opts() TickerOpts {
	return t.opts
}

func logAttrs(ctx context.Context, logLevel slog.Level, message string, caller uintptr, attrs ...slog.Attr) {
	rec := slog.NewRecord(time.Now(), logLevel, message, caller)
	rec.AddAttrs(attrs...)
	slog.Default().Handler().Handle(ctx, rec)
}

func (t *Ticker) Run(ctx context.Context) error {
	if t.started {
		return errors.New("ticker already started")
	}
	t.started = true

	defer t.Stop(ctx)
	for range t.ticker.C {
		// if ctx.Err() != nil {
		// 	return ctx.Err()
		// }
		if t.stopped {
			return nil
		}
		t.ticks++
		if t.ticks < t.opts.StartBurst() || t.ticks%t.opts.Frequency() == 0 {
			attrs := []slog.Attr{}
			if t.opts.attrFunc != nil {
				attrs = t.opts.attrFunc()
			}
			attrs = append(attrs, slog.Any("tick", t.ticks))
			attrs = append(attrs, slog.Any("ctx_err", ctx.Err()))
			logAttrs(ctx, t.opts.logLevel, t.opts.message, t.caller, attrs...)
		}
	}
	return nil
}

func (t *Ticker) Stop(ctx context.Context) {
	if t.stopped {
		return
	}
	t.stopped = true
	t.ticker.Stop()

	if t.opts.doneMessage == "" {
		return
	}
	attrs := []slog.Attr{}
	// if ctx.Err() != nil {
	// 	attrs = append(attrs, slog.String("reason", "context done"))
	// }
	if t.ticks > 0 {
		attrs = append(attrs, slog.Any("ticks", t.ticks))
	}
	if t.opts.attrFunc != nil {
		attrs = append(attrs, t.opts.attrFunc()...)
	}
	logAttrs(ctx, t.opts.logLevel, t.opts.doneMessage, t.caller, attrs...)
}
