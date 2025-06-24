package stackerr

import (
	"runtime"
	"strings"

	"gitlab.com/tozd/go/errors"
)

// GetRootError finds the innermost (root) error in the chain
func GetRootError(err error) error {
	current := err
	for {
		next := errors.Unwrap(current)
		if next == nil {
			return current
		}
		if _, ok := next.(errors.E); !ok {
			return next
		}
		current = next
	}
}

func IsNotRelevantFrame(frame runtime.Frame) bool {
	return !IsRelevantFrame(frame)
}

// isRelevantFrame determines if a frame should be included in the stack trace
func IsRelevantFrame(frame runtime.Frame) bool {
	// Skip runtime internals
	if strings.HasPrefix(frame.Function, "runtime.") {
		return false
	}

	// Skip reflect internals
	if strings.HasPrefix(frame.Function, "reflect.") {
		return false
	}

	// Skip testing internals
	if strings.Contains(frame.Function, "testing.") {
		return false
	}

	// Skip empty frames
	if frame.Function == "" && frame.File == "" {
		return false
	}

	return true
}

// convertStackTrace converts a slice of PCs to runtime.Frames
func ConvertStackTraceToFrames(st []uintptr) []runtime.Frame {
	var frames []runtime.Frame

	callersFrames := runtime.CallersFrames(st)
	for {
		frame, more := callersFrames.Next()
		frames = append(frames, frame)
		if !more {
			break
		}
	}

	return frames
}

func Hyperlink(link, renderedText string) string {
	// OSC 8 sequence around styled text
	start := "\x1b]8;;" + link + "\x07"
	end := "\x1b]8;;\x07"

	return start + renderedText + end
}
