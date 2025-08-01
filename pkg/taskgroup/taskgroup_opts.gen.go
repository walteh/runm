// Code generated by options-gen v0.52.1. DO NOT EDIT.

package taskgroup

import (
	"context"
	"log/slog"
	"time"
)

type TaskGroupOpt func(o *TaskGroupOpts)

func newTaskGroupOpts(
	options ...TaskGroupOpt,
) TaskGroupOpts {
	var o TaskGroupOpts

	// Setting defaults from field tag (if present)

	o.name = "taskgroup"
	o.logLevel = slog.Level(-4)
	o.logStart = true
	o.logEnd = true
	o.logTaskStart = true
	o.logTaskEnd = true
	o.logTaskPanic = true
	o.callerSkip = 1
	o.enableTicker = false
	o.tickerInterval, _ = time.ParseDuration("15s")
	o.tickerFrequency = 15
	o.tickerStartBurst = 5
	o.keepTaskHistory = true
	o.maxTaskHistory = 1000
	o.enablePprof = true

	for _, opt := range options {
		opt(&o)
	}
	return o
}

func WithName(opt string) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.name = opt }
}

func WithLogLevel(opt slog.Level) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.logLevel = opt }
}

func WithLogStart(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.logStart = opt }
}

func WithLogEnd(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.logEnd = opt }
}

func WithLogTaskStart(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.logTaskStart = opt }
}

func WithLogTaskEnd(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.logTaskEnd = opt }
}

func WithLogTaskPanic(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.logTaskPanic = opt }
}

func WithTimeout(opt time.Duration) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.timeout = opt }
}

func WithCallerSkip(opt int) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.callerSkip = opt }
}

func WithSlogBaseContext(opt context.Context) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.slogBaseContext = opt }
}

func WithAttrFunc(opt func() []slog.Attr) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.attrFunc = opt }
}

func WithMaxConcurrent(opt int) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.maxConcurrent = opt }
}

func WithEnableTicker(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.enableTicker = opt }
}

func WithTickerInterval(opt time.Duration) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.tickerInterval = opt }
}

func WithTickerFrequency(opt int) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.tickerFrequency = opt }
}

func WithTickerStartBurst(opt int) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.tickerStartBurst = opt }
}

func WithKeepTaskHistory(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.keepTaskHistory = opt }
}

func WithMaxTaskHistory(opt int) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.maxTaskHistory = opt }
}

func WithEnablePprof(opt bool) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.enablePprof = opt }
}

func WithPprofLabels(opt map[string]string) TaskGroupOpt {
	return func(o *TaskGroupOpts) { o.pprofLabels = opt }
}

func (o *TaskGroupOpts) Validate() error {
	return nil
}

// Public getter methods for private fields

func (o TaskGroupOpts) Name() string { return o.name }

func (o TaskGroupOpts) LogLevel() slog.Level { return o.logLevel }

func (o TaskGroupOpts) LogStart() bool { return o.logStart }

func (o TaskGroupOpts) LogEnd() bool { return o.logEnd }

func (o TaskGroupOpts) LogTaskStart() bool { return o.logTaskStart }

func (o TaskGroupOpts) LogTaskEnd() bool { return o.logTaskEnd }

func (o TaskGroupOpts) LogTaskPanic() bool { return o.logTaskPanic }

func (o TaskGroupOpts) Timeout() time.Duration { return o.timeout }

func (o TaskGroupOpts) CallerSkip() int { return o.callerSkip }

func (o TaskGroupOpts) SlogBaseContext() context.Context { return o.slogBaseContext }

func (o TaskGroupOpts) AttrFunc() func() []slog.Attr { return o.attrFunc }

func (o TaskGroupOpts) MaxConcurrent() int { return o.maxConcurrent }

func (o TaskGroupOpts) EnableTicker() bool { return o.enableTicker }

func (o TaskGroupOpts) TickerInterval() time.Duration { return o.tickerInterval }

func (o TaskGroupOpts) TickerFrequency() int { return o.tickerFrequency }

func (o TaskGroupOpts) TickerStartBurst() int { return o.tickerStartBurst }

func (o TaskGroupOpts) KeepTaskHistory() bool { return o.keepTaskHistory }

func (o TaskGroupOpts) MaxTaskHistory() int { return o.maxTaskHistory }

func (o TaskGroupOpts) EnablePprof() bool { return o.enablePprof }

func (o TaskGroupOpts) PprofLabels() map[string]string { return o.pprofLabels }
