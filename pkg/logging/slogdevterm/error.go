package slogdevterm

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/list"
	"github.com/walteh/runm/pkg/stackerr"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc/status"
)

// stackTracer interface matches the one from pkg/errors for extracting stack traces
type stackTracer interface {
	StackTrace() []uintptr
}

// frameProvider interface for getting individual error creation frames
type frameProvider interface {
	Frame() uintptr
}

func isolateBetterError(err error) error {
	// For non-errors.E types, fall back to simple display
	if errz, ok := err.(errors.E); ok {
		return errz
	}

	if errz, ok := err.(*stackerr.StackedEncodableError); ok {
		return errz
	}

	if st, ok := status.FromError(err); ok {
		return stackerr.FromGRPCStatusError(st)
	}

	return nil
}

// ErrorToTrace renders an error with a simple two-part structure:
// 1. Error traces list (individual errors with their locations)
// 2. Simple stack trace (just call stack, no context mapping)
func ErrorToTrace(err error, record slog.Record, styles *Styles, render renderFunc, hyperlink HyperlinkFunc) string {
	if err == nil {
		return ""
	}

	// For non-errors.E types, fall back to simple display
	betterErr := isolateBetterError(err)
	if betterErr == nil {
		return renderSimpleError(err, styles, render, record, hyperlink)
	}

	err = betterErr

	// Build the display
	var sections []string

	// Section 1: Error traces (individual errors)
	errorTraces := buildErrorTraces(err, styles, render, hyperlink)
	if len(errorTraces) > 0 {
		sections = append(sections, strings.Join(errorTraces, "\n"))
	}

	// Section 2: Simple stack trace (no context mapping)
	stackTrace := buildSimpleStackTrace(err, styles, render, hyperlink, errorTraces)
	if len(stackTrace) > 0 {
		sections = append(sections, stackTrace)
	}

	if len(sections) == 0 {
		return renderSimpleError(err, styles, render, record, hyperlink)
	}

	// Get the root error message for header (avoid repetition)
	rootError := stackerr.GetRootError(err)
	header := render(styles.Error.Main, rootError.Error())
	content := strings.Join(sections, "\n\n")

	return render(styles.Error.Container, header+"\n\n"+content)
}

// buildErrorTraces creates a list of individual errors with their creation locations
func buildErrorTraces(err error, styles *Styles, render renderFunc, hyperlink HyperlinkFunc) []string {
	var traces []string

	sources := stackerr.NewStackedEncodableErrorFromError(err)

	for {
		enhancedSource := sources.Source
		next := sources.Next
		currentMsg := sources.Message

		var nextMsg string
		if next == nil {
			nextMsg = ""
		} else {
			nextMsg = next.Message
		}

		var location string
		if enhancedSource != nil {
			location = RenderEnhancedSource(enhancedSource, styles, render, hyperlink)
		} else {
			break
		}

		// Extract wrapper message
		var wrapper string
		if idx := strings.Index(currentMsg, nextMsg); idx > 0 {
			wrapper = strings.TrimSuffix(strings.TrimSpace(currentMsg[:idx]), ":")
		} else {
			wrapper = currentMsg
		}

		if wrapper != "" {
			message := render(styles.Error.Main, wrapper)
			traces = append(traces, fmt.Sprintf("    %s: %s", location, message))
		}

		if next == nil {
			break
		}

		sources = next

	}

	slices.Reverse(traces)

	return traces
}

// buildSimpleStackTrace creates a simple call stack without context mapping
func buildSimpleStackTrace(err error, styles *Styles, render renderFunc, hyperlink HyperlinkFunc, errorTraces []string) string {
	sources := stackerr.GetEnhancedSourcesFromError(err)
	if len(sources) == 0 {
		return ""
	}

	var rows []string
	for _, source := range sources {
		rows = append(rows, RenderEnhancedSource(source, styles, render, hyperlink))
	}

	// Dedupe consecutive identical rows
	rows = dedupeConsecutive(rows)

	// Create the list
	l := list.New(convertToListItems(rows)...).
		Enumerator(errorEnumerator).
		EnumeratorStyleFunc(errorEnumeratorStyle)

	return l.String()

}

// dedupeConsecutive removes consecutive identical strings
func dedupeConsecutive(rows []string) []string {
	if len(rows) <= 1 {
		return rows
	}

	var result []string
	result = append(result, rows[0])

	for i := 1; i < len(rows); i++ {
		if rows[i] != rows[i-1] {
			result = append(result, rows[i])
		}
	}

	return result
}

// convertToListItems converts strings to list items
func convertToListItems(rows []string) []any {
	items := make([]any, len(rows))
	for i, row := range rows {
		items[i] = row
	}
	return items
}

// errorEnumerator returns rounded tree-style connectors like Rust errors
func errorEnumerator(items list.Items, i int) string {
	if i == items.Length()-1 {
		return "╰── "
	}
	return "├── "
}

// errorEnumeratorStyle returns the style for enumerators
func errorEnumeratorStyle(items list.Items, i int) lipgloss.Style {
	return lipgloss.NewStyle()
}

// renderSimpleError renders a simple error with optional stack trace from log record
func renderSimpleError(err error, styles *Styles, render renderFunc, record slog.Record, hyperlink HyperlinkFunc) string {
	// Start with the error message
	header := render(styles.Error.Main, err.Error())

	// If we have source information from the log record, add it as a simple stack trace
	if record.PC != 0 {
		enhancedSource := stackerr.NewEnhancedSource(record.PC)
		location := RenderEnhancedSource(enhancedSource, styles, render, hyperlink)

		// Create a simple single-item list with the location
		l := list.New(location).
			Enumerator(errorEnumerator).
			EnumeratorStyleFunc(errorEnumeratorStyle)

		content := l.String()
		return render(styles.Error.Container, header+"\n\n"+content)
	}

	// Fallback to just the error message if no source info
	return render(styles.Error.Main, err.Error())
}
