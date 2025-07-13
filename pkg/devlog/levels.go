package devlog

import (
	"log/slog"

	devlogv1 "github.com/walteh/runm/proto/devlog/v1"
)

// SlogLevelToDevlogLevel converts slog.Level to devlog LogLevel
func SlogLevelToDevlogLevel(level slog.Level) devlogv1.LogLevel {
	switch {
	case level <= slog.LevelDebug-4: // -8 or lower
		return devlogv1.LogLevel_LOG_LEVEL_TRACE
	case level <= slog.LevelDebug: // -4
		return devlogv1.LogLevel_LOG_LEVEL_DEBUG
	case level <= slog.LevelInfo: // 0
		return devlogv1.LogLevel_LOG_LEVEL_INFO
	case level <= slog.LevelWarn: // 4
		return devlogv1.LogLevel_LOG_LEVEL_WARN
	case level <= slog.LevelError: // 8
		return devlogv1.LogLevel_LOG_LEVEL_ERROR
	default: // 12 or higher
		return devlogv1.LogLevel_LOG_LEVEL_FATAL
	}
}

// DevlogLevelToSlogLevel converts devlog LogLevel to slog.Level
func DevlogLevelToSlogLevel(level devlogv1.LogLevel) slog.Level {
	switch level {
	case devlogv1.LogLevel_LOG_LEVEL_TRACE:
		return slog.LevelDebug - 4 // -8
	case devlogv1.LogLevel_LOG_LEVEL_DEBUG:
		return slog.LevelDebug // -4
	case devlogv1.LogLevel_LOG_LEVEL_INFO:
		return slog.LevelInfo // 0
	case devlogv1.LogLevel_LOG_LEVEL_WARN:
		return slog.LevelWarn // 4
	case devlogv1.LogLevel_LOG_LEVEL_ERROR:
		return slog.LevelError // 8
	case devlogv1.LogLevel_LOG_LEVEL_FATAL:
		return slog.LevelError + 4 // 12
	default:
		return slog.LevelInfo
	}
}

// GetLevelName returns a human-readable name for the log level
func GetLevelName(level devlogv1.LogLevel) string {
	switch level {
	case devlogv1.LogLevel_LOG_LEVEL_TRACE:
		return "TRACE"
	case devlogv1.LogLevel_LOG_LEVEL_DEBUG:
		return "DEBUG"
	case devlogv1.LogLevel_LOG_LEVEL_INFO:
		return "INFO"
	case devlogv1.LogLevel_LOG_LEVEL_WARN:
		return "WARN"
	case devlogv1.LogLevel_LOG_LEVEL_ERROR:
		return "ERROR"
	case devlogv1.LogLevel_LOG_LEVEL_FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}
