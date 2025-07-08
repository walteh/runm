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
	interval        time.Duration `default:"1s"`
	startBurst      int           `default:"5"`
	logLevel        slog.Level    `default:"-4"`
	frequency       int           `default:"60"`
	message         string        `default:"ticker running"`
	attrFunc        func() []slog.Attr
	doneMessage     string
	callerSkip      int `default:"1"`
	slogBaseContext context.Context
	messageFunc     func() string
}

type Ticker struct {
	opts        TickerOpts
	ticker      *time.Ticker
	ticks       int
	started     bool
	stopped     bool
	caller      uintptr
	context     context.Context
	messageFunc func() string
}

func NewTicker(opts ...TickerOpt) *Ticker {
	t := newTickerOpts(opts...)
	caller, _, _, _ := runtime.Caller(t.callerSkip)
	if t.slogBaseContext == nil {
		t.slogBaseContext = context.Background()
	}

	messageFunc := t.messageFunc
	if messageFunc == nil {
		messageFunc = func() string {
			return t.message
		}
	}

	return &Ticker{
		ticker:      time.NewTicker(t.Interval()),
		opts:        t,
		caller:      caller,
		context:     context.WithoutCancel(t.slogBaseContext),
		messageFunc: messageFunc,
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

func (t *Ticker) RunWithWaiter(fn func()) error {
	if t.started {
		return errors.New("ticker already started")
	}
	t.started = true
	go t.run()
	fn()
	t.Stop()
	return nil
}

func (t *Ticker) RunWithWaitOnContext(ctx context.Context) {
	if t.started {
		panic("ticker already started")
	}
	t.started = true
	go t.run()
	<-ctx.Done()
	t.Stop()
}

func (t *Ticker) RunAsDefer() func() {
	if t.started {
		panic("ticker already started")
	}
	t.started = true
	go t.run()
	return t.Stop
}

func (t *Ticker) run() {

	for range t.ticker.C {
		// if ctx.Err() != nil {
		// 	return ctx.Err()
		// }
		if t.stopped {
			return
		}
		t.ticks++
		if t.ticks < t.opts.StartBurst() || t.ticks%t.opts.Frequency() == 0 {
			attrs := []slog.Attr{}
			if t.opts.attrFunc != nil {
				attrs = t.opts.attrFunc()
			}
			attrs = append(attrs, slog.Any("tick", t.ticks))
			attrs = append(attrs, slog.Any("ctx_err", t.context.Err()))
			logAttrs(t.context, t.opts.logLevel, t.messageFunc(), t.caller, attrs...)
		}
	}

}

func (t *Ticker) Stop() {
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
	logAttrs(t.context, t.opts.logLevel, t.opts.doneMessage, t.caller, attrs...)
}
