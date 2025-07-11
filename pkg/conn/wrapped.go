package conn

import (
	"bytes"
	"io"
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

func NewCountReader(r io.Reader) io.ReadCloser {
	return &countReader{
		r:         r,
		ncount:    0,
		callCount: 0,
	}
}

func NewCountWriter(w io.WriteCloser) io.WriteCloser {
	return &countWriter{
		w:         w,
		ncount:    0,
		callCount: 0,
	}
}

type countWriter struct {
	w         io.Writer
	ncount    uint64
	callCount uint64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.ncount += uint64(n)
	c.callCount++
	return n, err
}

func (c *countWriter) Close() error {
	if closer, ok := c.w.(io.Closer); ok {
		return closer.Close()
	}
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

func escapeString(s []byte) string {
	if isPrintableASCII(string(s)) {
		return string(s) // Fast path for normal strings
	}

	// remove null bytes from the end of the string
	s = bytes.TrimRight(s, "\x00")

	// Use Go's quote function which handles all escape sequences properly
	quoted := strconv.Quote(string(s))
	// Remove the surrounding quotes that strconv.Quote adds
	if len(quoted) >= 2 && quoted[0] == '"' && quoted[len(quoted)-1] == '"' {
		return quoted[1 : len(quoted)-1]
	}
	return quoted
}
