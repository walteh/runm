package logging

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"
)

var rawWriter io.Writer
var rawWriterMutex sync.Mutex

func SetDefaultRawWriter(w io.Writer) {
	rawWriterMutex.Lock()
	defer rawWriterMutex.Unlock()
	rawWriter = w
}

func GetDefaultRawWriter() io.Writer {
	rawWriterMutex.Lock()
	defer rawWriterMutex.Unlock()
	return rawWriter
}

var logWriter io.Writer
var logWriterMutex sync.Mutex

func GetDefaultDelimWriter() io.Writer {
	logWriterMutex.Lock()
	defer logWriterMutex.Unlock()
	if logWriter == nil {
		return os.Stdout
	}
	return logWriter
}

func SetDefaultDelimWriter(w io.Writer) {
	logWriterMutex.Lock()
	defer logWriterMutex.Unlock()
	logWriter = w
}

// same but with raw uintptr instead of offset
func LogCaller(ctx context.Context, level slog.Level, pc uintptr, msg string, args ...any) {
	rec := slog.Record{
		Level:   slog.LevelDebug,
		Message: msg,
		Time:    time.Now(),
		PC:      pc,
	}
	rec.Add(args...)
	slog.Default().Handler().Handle(ctx, rec)
}

func LogRecord(ctx context.Context, level slog.Level, callerOffset int, msg string, args ...any) {
	pc, _, _, ok := runtime.Caller(callerOffset + 1)
	if !ok {
		slog.Default().WarnContext(ctx, "failed to get caller", "caller_offset", callerOffset)
		slog.Default().Log(ctx, level, msg, args...)
		return
	}
	LogCaller(ctx, level, pc, msg, args...)
}

// func ForwardMyStdioWritersTo(ctx context.Context, connOut, connErr interface{ File() (*os.File, error) }) error {

// 	// 2) Extract *os.File (does a dup under the hood)
// 	fileOut, err := connOut.File()
// 	if err != nil {
// 		return errors.Errorf("get stdout file: %v", err)
// 	}
// 	defer fileOut.Close()

// 	fileErr, err := connErr.File()
// 	if err != nil {
// 		return errors.Errorf("get stderr file: %v", err)
// 	}
// 	defer fileErr.Close()

// 	// 3) Dup2 the socket FDs onto STDOUT_FILENO & STDERR_FILENO
// 	if err := syscall.Dup2(int(fileOut.Fd()), int(os.Stdout.Fd())); err != nil {
// 		return errors.Errorf("dup2 stdout: %v", err)
// 	}
// 	if err := syscall.Dup2(int(fileErr.Fd()), int(os.Stderr.Fd())); err != nil {
// 		return errors.Errorf("dup2 stderr: %v", err)
// 	}
// 	return nil
// }

// PrefixedWriter wraps an io.Writer and adds a prefix to each line.
type PrefixedWriter struct {
	getPrefix func(string) string
	w         io.Writer
}

// Write splits p into lines and writes each with the prefix.
func (pw *PrefixedWriter) Write(p []byte) (n int, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(p))
	total := 0
	for scanner.Scan() {
		line := scanner.Text()
		m, err := fmt.Fprintf(pw.w, "%s\n", pw.getPrefix(line))
		total += m
		if err != nil {
			return total, err
		}
	}
	return total, scanner.Err()
}
