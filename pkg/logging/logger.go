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
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/walteh/runm/linux/constants"
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
	rawWriter         io.Writer
	enableDelimiter   bool
	delimiter         rune
	interceptHclog    bool
	values            []slog.Attr
	globalLogWriter   io.Writer
	// otlpInstances     *OTelInstances

	delayedHandlerCreatorOpts []LoggerOpt `option:"-"`
}

func NewDefaultDevLogger(name string, writer io.Writer, opts ...LoggerOpt) *slog.Logger {

	defaults := []LoggerOpt{
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
		WithOtelHandler(),
	}
	opts = append(defaults, opts...)
	return NewLogger(opts...)
}

func NewDefaultDevLoggerWithDelimiter(name string, writer io.Writer, opts ...LoggerOpt) *slog.Logger {
	opts = append(opts, WithDelimiter(constants.VsockDelimitedLogProxyDelimiter), WithEnableDelimiter(true))
	return NewDefaultDevLogger(name, writer, opts...)
}

func NewDefaultJSONLogger(name string, writer io.Writer, opts ...LoggerOpt) *slog.Logger {
	defaults := []LoggerOpt{
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
	}
	opts = append(defaults, opts...)
	return NewLogger(opts...)
}

func NewLogger(opts ...LoggerOpt) *slog.Logger {
	copts := newLoggerOpts(opts...)

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

	if copts.rawWriter == nil {
		copts.rawWriter = copts.fallbackWriter
	}

	if copts.enableDelimiter {
		if copts.delimiter == 0 {
			copts.delimiter = '\n'
		}
	}

	for _, opt := range copts.delayedHandlerCreatorOpts {
		opt(&copts)
	}

	// copts.rawWriter = &PrefixedWriter{
	// 	getPrefix: func(str string) string {
	// 		if strings.HasPrefix(str, "[name=") {
	// 			return str
	// 		}
	// 		return fmt.Sprintf("[name=%s][pid=%04d][time=%s]: %s", copts.processName, os.Getpid(), time.Now().Format("04:05.0000"), str)
	// 	},
	// 	w: copts.rawWriter,
	// }

	if len(copts.handlers) == 0 {
		_, _ = fmt.Fprintln(copts.fallbackWriter, "WARNING: no handlers provided, using fallback handler (defaults to stderr)")
		copts.handlers = []slog.Handler{
			slog.NewTextHandler(copts.fallbackWriter, copts.handlerOptions),
		}
	}

	h := newMultiHandler(copts.handlers...)

	h = newContextHandler(h)

	// h = NewOTelSlogTraceInjectionHandler(h)

	l := slog.New(h)

	for _, v := range copts.values {
		l = l.With(v)
	}

	if copts.globalLogWriter != nil {
		SetDefaultDelimWriter(copts.globalLogWriter)
		SetDefaultRawWriter(copts.rawWriter)
	}

	// if copts.delimitedLogWriter != nil {
	// 	SetDefaultDelimitedLogWriter(copts.delimiter, copts.delimitedLogWriter)
	// }

	if copts.makeDefaultLogger {
		slog.SetDefault(l)
		// if copts.otlpInstances != nil {
		// 	copts.otlpInstances.EnableGlobally()
		// 	SetGlobalOtelInstances(copts.otlpInstances)
		// }
	}

	if copts.interceptLogrus {
		sloglogrus.InterceptLogrus(l)
	}

	if copts.interceptHclog {
		sloghclog.InterceptHclog(l)
	}

	return l
}

func WithValue(v slog.Attr) LoggerOpt {
	return func(o *LoggerOpts) {
		o.values = append(o.values, v)
	}
}

func WithDevTermHanlder(writer io.Writer) LoggerOpt {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {

			devtermOpts := []slogdevterm.TermLoggerOption{
				slogdevterm.WithLoggerName(o.processName),
				slogdevterm.WithColorProfile(termenv.TrueColor),
				slogdevterm.WithRenderOption(termenv.WithTTY(true)),
				slogdevterm.WithEnableLoggerNameColor(true),
				slogdevterm.WithOSIcon(true),
				slogdevterm.WithDebugPatternColoring(true),
				slogdevterm.WithMultilineBoxes(true),
			}

			if o.enableDelimiter {
				devtermOpts = append(devtermOpts, slogdevterm.WithDelimiter(o.delimiter))
			}

			o.handlers = append(o.handlers, slogdevterm.NewTermLogger(writer, o.handlerOptions, devtermOpts...))
		})
	}
}

func WithOtelHandler() LoggerOpt {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			opts := []otelslog.Option{
				otelslog.WithSource(true),
			}

			// if o.otlpInstances == nil {
			// 	opts = append(opts, otelslog.WithLoggerProvider(o.otlpInstances.LoggerProvider))
			// }
			o.handlers = append(o.handlers, otelslog.NewHandler(
				Domain(),
				opts...,
			))
		})
	}
}

func WithJSONHandler(writer io.Writer) LoggerOpt {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			o.handlers = append(o.handlers, slog.NewJSONHandler(writer, o.handlerOptions))
		})
	}
}

func WithFileHandler(filename string) LoggerOpt {
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

func WithDiscardHandler() LoggerOpt {
	return func(o *LoggerOpts) {
		o.delayedHandlerCreatorOpts = append(o.delayedHandlerCreatorOpts, func(o *LoggerOpts) {
			o.handlers = append(o.handlers, slog.NewTextHandler(io.Discard, o.handlerOptions))
		})
	}
}

func WithGlobalRedactor() LoggerOpt {
	return func(o *LoggerOpts) {
		o.replacers = append(o.replacers, SlogReplacerFunc(Redact))
	}
}

func WithErrorStackTracer() LoggerOpt {
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

var _ slog.Handler = &OTelSlogTraceInjectionHandler{}

type OTelSlogTraceInjectionHandler struct {
	Next slog.Handler
}

// Enabled implements slog.Handler.
func (h *OTelSlogTraceInjectionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Next.Enabled(ctx, level)
}

// WithAttrs implements slog.Handler.
func (h *OTelSlogTraceInjectionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &OTelSlogTraceInjectionHandler{Next: h.Next.WithAttrs(attrs)}
}

// WithGroup implements slog.Handler.
func (h *OTelSlogTraceInjectionHandler) WithGroup(name string) slog.Handler {
	return &OTelSlogTraceInjectionHandler{Next: h.Next.WithGroup(name)}
}

func NewOTelSlogTraceInjectionHandler(h slog.Handler) *OTelSlogTraceInjectionHandler {
	return &OTelSlogTraceInjectionHandler{Next: h}
}

// Handle extracts trace and span IDs and adds them to the log record.
func (h *OTelSlogTraceInjectionHandler) Handle(ctx context.Context, r slog.Record) error {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}
	return h.Next.Handle(ctx, r)
}
