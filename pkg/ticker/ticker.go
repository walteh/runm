package ticker

import (
	"context"
	"errors"
	"log/slog"
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
}

type Ticker struct {
	opts    TickerOpts
	ticker  *time.Ticker
	ticks   int
	started bool
	stopped bool
}

func NewTicker(opts ...TickerOpt) *Ticker {
	t := newTickerOpts(opts...)

	return &Ticker{
		ticker: time.NewTicker(t.Interval()),
		opts:   t,
	}
}

func (t *Ticker) Opts() TickerOpts {
	return t.opts
}

func (t *Ticker) Run(ctx context.Context) error {
	if t.started {
		return errors.New("ticker already started")
	}
	t.started = true

	defer func() {
		attrs := []slog.Attr{}
		if ctx.Err() != nil {
			attrs = append(attrs, slog.String("reason", "context done"))
		}
		if t.ticks > 0 {
			attrs = append(attrs, slog.Any("ticks", t.ticks))
		}
		if t.opts.attrFunc != nil {
			attrs = append(attrs, t.opts.attrFunc()...)
		}
		slog.LogAttrs(ctx, t.opts.logLevel, t.opts.doneMessage, attrs...)
	}()

	defer t.ticker.Stop()
	for range t.ticker.C {
		if ctx.Err() != nil {
			return ctx.Err()
		}
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
			slog.LogAttrs(ctx, t.opts.logLevel, t.opts.message, attrs...)
		}
	}
	return nil
}

func (t *Ticker) Stop() {
	if t.stopped {
		return
	}
	t.stopped = true
}
