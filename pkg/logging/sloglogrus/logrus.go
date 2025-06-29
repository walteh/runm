package sloglogrus

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

var _ logrus.Hook = &SlogBridgeHook{}

var (
	logrusOnce = sync.Once{}
)

func InterceptLogrus(l *slog.Logger) {
	logrus.SetReportCaller(true)
	logrus.AddHook(&SlogBridgeHook{})
	logrus.SetOutput(io.Discard)
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: true,
	})
}

func ForwardLogrusToSlogGlobally() {
	logrusOnce.Do(func() {
		InterceptLogrus(nil)
	})
}

func SetLogrusLevel(level logrus.Level) {
	logrus.SetLevel(level)
}

type SlogBridgeHook struct {
	logger *slog.Logger
}

func (h *SlogBridgeHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *SlogBridgeHook) Fire(entry *logrus.Entry) error {
	// Map logrus levels to slog levels
	var level slog.Level
	switch entry.Level {
	case logrus.PanicLevel, logrus.FatalLevel, logrus.ErrorLevel:
		level = slog.LevelError
	case logrus.WarnLevel:
		level = slog.LevelWarn
	case logrus.InfoLevel:
		level = slog.LevelInfo
	case logrus.DebugLevel, logrus.TraceLevel:
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}

	// Prepare slog attributes
	attrs := make([]slog.Attr, 0, len(entry.Data))
	for k, v := range entry.Data {
		attrs = append(attrs, slog.Any(k, v))
	}

	slices.SortFunc(attrs, func(a, b slog.Attr) int {
		return strings.Compare(a.Key, b.Key)
	})

	var caller uintptr
	if entry.Caller != nil {
		caller = entry.Caller.PC
	} else {
		caller, _, _, _ = runtime.Caller(5)

	}

	record := slog.NewRecord(
		entry.Time,
		level,
		entry.Message,
		caller,
	)
	record.AddAttrs(attrs...)

	if entry.Caller != nil && strings.HasSuffix(entry.Caller.File, "panic.go") {
		record := slog.NewRecord(entry.Time, level, string(debug.Stack()), caller)
		slog.Default().Handler().Handle(context.Background(), record)
	}

	// Send to slog

	ctx := entry.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if h.logger == nil {
		return slog.Default().Handler().Handle(ctx, record)
	}

	return h.logger.Handler().Handle(ctx, record)
}
