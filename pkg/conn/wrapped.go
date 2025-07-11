package conn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"unicode"
)

type breakOnZeroReader struct {
	r io.Reader
}

func (b *breakOnZeroReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

type logWriter struct {
	ctx           context.Context
	name          string
	ncount        uint64
	callCount     uint64
	buf           []byte
	hasBeenClosed bool
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	l.ncount += uint64(len(p))
	l.callCount++
	for {
		idx := bytes.IndexByte(l.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(l.buf[:idx])
		// ttys are breaking, we need to esape the line data
		slog.DebugContext(l.ctx, fmt.Sprintf("%s[DATA]", l.name), "data", escapeString(line), "ncount", l.ncount, "call_count", l.callCount)
		l.buf = l.buf[idx+1:]
	}
	return len(p), nil
}

func (l *logWriter) Close() error {
	l.hasBeenClosed = true
	slog.InfoContext(l.ctx, fmt.Sprintf("%s:LOGWRITER[CLOSED]", l.name), "ncount", l.ncount, "call_count", l.callCount)
	return nil
}

type countReader struct {
	r         io.Reader
	ncount    uint64
	callCount uint64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.ncount += uint64(n)
	c.callCount++
	return n, err
}

func (c *countReader) Close() error {
	if closer, ok := c.r.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func escapeString(s string) string {
	if isPrintableASCII(s) {
		return s // Fast path for normal strings
	}

	// Use Go's quote function which handles all escape sequences properly
	quoted := strconv.Quote(s)
	// Remove the surrounding quotes that strconv.Quote adds
	if len(quoted) >= 2 && quoted[0] == '"' && quoted[len(quoted)-1] == '"' {
		return quoted[1 : len(quoted)-1]
	}
	return quoted
}
