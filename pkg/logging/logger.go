package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/muesli/termenv"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/walteh/runm/pkg/logging/slogdevterm"
	"github.com/walteh/runm/pkg/logging/sloghclog"
	"github.com/walteh/runm/pkg/logging/sloglogrus"
)

//go:opts
type LoggerOpts struct {
	handlerOptions    *slog.HandlerOptions
	fallbackWriter    io.Writer
	processName       string
	replacers         []SlogReplacer
	handlers          []slog.Handler
	makeDefaultLogger bool
	interceptLogrus   bool
	interceptHclog    bool
	values            []slog.Attr
	globalLogWriter   io.Writer
	otlpInstances     *OTelInstances

	delayedHandlerCreatorOpts []OptLoggerOptsSetter `opts:"-"`
}

func NewDefaultDevLogger(name string, writer io.Writer, opts ...OptLoggerOptsSetter) *slog.Logger {

	opts = append(opts,
		WithDevTermHanlder(writer),
		WithProcessName(name),
		WithGlobalRedactor(),
		WithErrorStackTracer(),
		WithInterceptLogrus(true),
		WithInterceptHclog(true),
		WithMakeDefaultLogger(true),
		WithGlobalLogWriter(writer),
		WithHandlerOptions(&slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		}),
	)
	return NewLogger(opts...)
}

func NewDefaultDevLoggerWithOtel(ctx context.Context, name string, rawLogWriter io.Writer, instances *OTelInstances, opts ...OptLoggerOptsSetter) *slog.Logger {

	opts = append(opts,
		WithDevTermHanlder(rawLogWriter),
		WithProcessName(name),
		WithGlobalRedactor(),
		WithErrorStackTracer(),
		WithInterceptLogrus(true),
		WithInterceptHclog(true),
		WithMakeDefaultLogger(true),
		WithGlobalLogWriter(rawLogWriter),
		WithOtelInstances(instances),
		WithOtelHandler(),
		WithHandlerOptions(&slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		}),
	)
	return NewLogger(opts...)
}

func NewDefaultJSONLogger(name string, writer io.Writer, opts ...OptLoggerOptsSetter) *slog.Logger {
	opts = append(opts,
		WithProcessName(name),
		WithGlobalRedactor(),
		WithErrorStackTracer(),
		WithInterceptLogrus(true),
		WithInterceptHclog(true),
		WithMakeDefaultLogger(true),
		WithGlobalLogWriter(writer),
		WithJSONHandler(writer),
		WithHandlerOptions(&slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		}),
	)
	return NewLogger(opts...)
}

func NewLogger(opts ...OptLoggerOptsSetter) *slog.Logger {
	copts := NewLoggerOpts(opts...)

	if copts.handlerOptions == nil {
		copts.handlerOptions = &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: true,
		}
	}

	if copts.processName == "" {
		executable, err := os.Executable()
		if err != nil {
			copts.processName = "unknown"
		} else {
			copts.processName = filepath.Base(executable)
		}
	}

	if len(copts.replacers) != 0 {
		repAttrBefore := copts.handlerOptions.ReplaceAttr

		copts.handlerOptions.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
			if repAttrBefore != nil {
				a = repAttrBefore(groups, a)
			}
			for _, replacer := range copts.replacers {
				a = replacer.Replace(groups, a)
			}
			return a
		}
	}

	if copts.fallbackWriter == nil {
		copts.fallbackWriter = os.Stderr
	}

	for _, opt := range copts.delayedHandlerCreatorOpts {
		opt(&copts)
	}

	if len(copts.handlers) == 0 {
		_, _ = fmt.Fprintln(copts.fallbackWriter, "WARNING: no handlers provided, using fallback handler (defaults to stderr)")
		copts.handlers = []slog.Handler{slog.NewTextHandler(copts.fallbackWriter, copts.handlerOptions)}
	}

	fan := newMultiHandler(copts.handlers...)

	ctxHandler := newContextHandler(fan)

	l := slog.New(ctxHandler)

	for _, v := range copts.values {
		l = l.With(v)
	}

	if copts.globalLogWriter != nil {
		SetDefaultLogWriter(copts.globalLogWriter)
	}

	if copts.makeDefaultLogger {
		slog.SetDefault(l)
		if copts.otlpInstances != nil {
			copts.otlpInstances.EnableGlobally()
		}
	}

	if copts.interceptLogrus {
		sloglogrus.InterceptLogrus(l)
	}

	if copts.interceptHclog {
		sloghclog.InterceptHclog(l)
	}

	return l
}

func WithValue(v slog.Attr) OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.values = append(o.values, v)
	}
}

func WithDevTermHanlder(writer io.Writer) OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			o.handlers = append(o.handlers, slogdevterm.NewTermLogger(writer, o.handlerOptions,
				slogdevterm.WithLoggerName(o.processName),
				slogdevterm.WithProfile(termenv.ANSI256),
				slogdevterm.WithRenderOption(termenv.WithTTY(true)),
				slogdevterm.WithLoggerName(o.processName),
			))
		})
	}
}

func WithOtelHandler() OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			if o.otlpInstances == nil {
				panic("otlpInstances is nil, use WithOtelInstances to set it")
			}
			o.handlers = append(o.handlers, otelslog.NewHandler(
				Domain(),
				otelslog.WithSource(true),
				otelslog.WithLoggerProvider(o.otlpInstances.LoggerProvider),
			))
		})
	}
}

func WithJSONHandler(writer io.Writer) OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			o.handlers = append(o.handlers, slog.NewJSONHandler(writer, o.handlerOptions))
		})
	}
}

func WithFileHandler(filename string) OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			o.handlers = append(o.handlers, slog.NewJSONHandler(&lumberjack.Logger{
				Filename:   filename, // Path to your log file
				MaxSize:    10,       // Max size in megabytes before rotation
				MaxBackups: 5,        // Max number of old log files to retain
				MaxAge:     1,        // Max number of days to retain old log files
				Compress:   true,     // Compress old log files
			}, o.handlerOptions))
		})
	}
}

func WithDiscardHandler() OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			o.handlers = append(o.handlers, slog.NewTextHandler(io.Discard, o.handlerOptions))
		})
	}
}

func WithGlobalRedactor() OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.replacers = append(o.replacers, SlogReplacerFunc(Redact))
	}
}

func WithErrorStackTracer() OptLoggerOptsSetter {
	return func(o *LoggerOpts) {
		o.replacers = append(o.replacers, SlogReplacerFunc(formatErrorStacks))
	}
}

type SlogReplacer interface {
	Replace(groups []string, a slog.Attr) slog.Attr
}

type SlogReplacerFunc func(groups []string, a slog.Attr) slog.Attr

func (f SlogReplacerFunc) Replace(groups []string, a slog.Attr) slog.Attr {
	return f(groups, a)
}
