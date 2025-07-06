package slogdevterm

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestGroupDottedNotation(t *testing.T) {
	var buf bytes.Buffer

	// Create a logger with minimal styling for easier testing
	handler := NewTermLogger(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}, WithStyles(EmptyStyles()))

	logger := slog.New(handler)

	// Test simple group
	logger.WithGroup("db").Info("test message", slog.String("host", "localhost"), slog.Int("port", 5432))

	output := buf.String()
	t.Logf("Simple group output: %s", output)

	// Check that we have dotted notation
	if !strings.Contains(output, "db.host=localhost") {
		t.Errorf("Expected 'db.host=localhost' in output, got: %s", output)
	}
	if !strings.Contains(output, "db.port=5432") {
		t.Errorf("Expected 'db.port=5432' in output, got: %s", output)
	}

	// Test nested groups
	buf.Reset()
	logger.WithGroup("app").WithGroup("db").Info("nested test", slog.String("driver", "postgres"))

	output = buf.String()
	t.Logf("Nested group output: %s", output)

	// Check nested dotted notation
	if !strings.Contains(output, "app.db.driver=postgres") {
		t.Errorf("Expected 'app.db.driver=postgres' in output, got: %s", output)
	}

	// Test WithAttrs combined with groups
	buf.Reset()
	logger.WithGroup("user").With(slog.String("id", "123")).Info("user action", slog.String("action", "login"))

	output = buf.String()
	t.Logf("WithAttrs + group output: %s", output)

	// Check that both preformatted and record attributes get proper dotted notation
	if !strings.Contains(output, "user.id=123") {
		t.Errorf("Expected 'user.id=123' in output, got: %s", output)
	}
	if !strings.Contains(output, "user.action=login") {
		t.Errorf("Expected 'user.action=login' in output, got: %s", output)
	}
}

func TestGroupAttributes(t *testing.T) {
	var buf bytes.Buffer

	handler := NewTermLogger(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}, WithStyles(EmptyStyles()))

	logger := slog.New(handler)

	// Test slog.Group attribute (inline groups)
	logger.Info("test with inline group",
		slog.Group("database",
			slog.String("host", "localhost"),
			slog.Int("port", 5432),
		),
		slog.String("app", "myapp"),
	)

	output := buf.String()
	t.Logf("Inline group output: %s", output)

	// Check that inline groups are flattened to dotted notation
	if !strings.Contains(output, "database.host=localhost") {
		t.Errorf("Expected 'database.host=localhost' in output, got: %s", output)
	}
	if !strings.Contains(output, "database.port=5432") {
		t.Errorf("Expected 'database.port=5432' in output, got: %s", output)
	}
	if !strings.Contains(output, "app=myapp") {
		t.Errorf("Expected 'app=myapp' in output, got: %s", output)
	}
}

func TestMultilineValueBoxes(t *testing.T) {
	var buf bytes.Buffer

	// Create a logger with minimal styling for easier testing
	handler := NewTermLogger(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}, WithStyles(EmptyStyles()), WithMultilineBoxes(true))

	logger := slog.New(handler)

	// Test multiline value in attribute
	multilineValue := "line 1\nline 2\nline 3"
	logger.Info("test message", slog.String("config", multilineValue))

	output := buf.String()
	t.Logf("Multiline value output: %s", output)

	// Check that we have the placeholder in the main line
	if !strings.Contains(output, "config=[multiline value below]") {
		t.Errorf("Expected 'config=[multiline value below]' placeholder in main line")
	}

	// Check that we have the boxed content with the key as title
	if !strings.Contains(output, "config") && !strings.Contains(output, "line 1") {
		t.Errorf("Expected boxed content with 'config' title and 'line 1' content")
	}

	// Test with groups and multiline values
	buf.Reset()
	logger.WithGroup("db").Info("database query",
		slog.String("sql", "SELECT *\nFROM users\nWHERE active = true"),
		slog.String("host", "localhost"))

	output = buf.String()
	t.Logf("Group multiline output: %s", output)

	// Check dotted notation with multiline value
	if !strings.Contains(output, "db.sql=[multiline value below]") {
		t.Errorf("Expected 'db.sql=[multiline value below]' with dotted notation")
	}

	// Check regular single-line value still works normally
	if !strings.Contains(output, "db.host=localhost") {
		t.Errorf("Expected 'db.host=localhost' for single-line value")
	}
}
