package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"
)

var logWriter io.Writer
var logWriterMutex sync.Mutex

func GetDefaultLogWriter() io.Writer {
	logWriterMutex.Lock()
	defer logWriterMutex.Unlock()
	if logWriter == nil {
		return os.Stdout
	}
	return logWriter
}

func SetDefaultLogWriter(w io.Writer) {
	logWriterMutex.Lock()
	defer logWriterMutex.Unlock()
	logWriter = w
}

func LogRecord(ctx context.Context, level slog.Level, callerOffset int, msg string, args ...any) {
	pc, _, _, ok := runtime.Caller(callerOffset + 1)
	if !ok {
		slog.Default().WarnContext(ctx, "failed to get caller", "caller_offset", callerOffset)
		slog.Default().Log(ctx, level, msg, args...)
		return
	}
	rec := slog.Record{
		Level:   slog.LevelDebug,
		Message: msg,
		Time:    time.Now(),
		PC:      pc,
	}
	for _, arg := range args {
		rec.AddAttrs(slog.Attr{Key: "arg", Value: slog.AnyValue(arg)})
	}
	slog.Default().Log(ctx, slog.LevelDebug, msg, args...)
}
