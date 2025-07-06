package slogdevterm

import (
	"log/slog"
	"os"
)

// ExampleDottedNotation demonstrates how groups are flattened into dotted keys
// instead of showing nested structures.
func ExampleDottedNotation() {
	// Create a logger with the terminal handler
	handler := NewTermLogger(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	// Simple group - shows as "db.host=localhost db.port=5432"
	// instead of nested {"db":{"host":"localhost","port":5432}}
	logger.WithGroup("db").Info("Database connected",
		slog.String("host", "localhost"),
		slog.Int("port", 5432),
	)

	// Nested groups - shows as "app.user.id=123 app.user.name=john"
	// instead of {"app":{"user":{"id":"123","name":"john"}}}
	logger.WithGroup("app").WithGroup("user").Info("User logged in",
		slog.String("id", "123"),
		slog.String("name", "john"),
	)

	// Mixed with preformatted attributes from WithAttrs
	userLogger := logger.WithGroup("session").With(
		slog.String("id", "sess_abc123"),
		slog.String("ip", "192.168.1.1"),
	)
	userLogger.Info("Session activity",
		slog.String("action", "page_view"),
		slog.String("page", "/dashboard"),
	)
	// Shows: session.id=sess_abc123 session.ip=192.168.1.1 session.action=page_view session.page=/dashboard

	// Inline groups also get flattened
	logger.Info("Service status",
		slog.Group("metrics",
			slog.Int("requests", 1234),
			slog.Float64("latency", 45.6),
		),
		slog.String("status", "healthy"),
	)
	// Shows: metrics.requests=1234 metrics.latency=45.6 status=healthy
}
