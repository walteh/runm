package conn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type debugWriter struct {
	ctx            context.Context
	name           string
	writeCount     uint64
	callCount      uint64
	buf            []byte
	hasBeenClosed  bool
	w              io.Writer
	sessionManager *sessionManager
}

func NewDebugWriter(ctx context.Context, name string, w io.Writer) io.Writer {
	return &debugWriter{
		ctx:           ctx,
		name:          name,
		buf:           []byte{},
		hasBeenClosed: false,
		writeCount:    0,
		callCount:     0,
		w:             w,
		sessionManager: &sessionManager{
			sessions:            make(map[*time.Time]struct{}),
			deletedSessionCount: 0,
			deletedSessionTime:  0,
		},
	}
}

func (l *debugWriter) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("bytes_written", int64(l.writeCount)),
		slog.Int64("calls", int64(l.callCount)),
		slog.Any("sessions", l.sessionManager),
		slog.String("write_target", getNameFromReadWriter(l.w)),
	)
}

func (l *debugWriter) Write(p []byte) (int, error) {
	l.callCount++

	defer l.sessionManager.Start()()

	n, err := l.w.Write(p)
	l.writeCount += uint64(n)

	if err != nil {
		var failedWriteData, successfulWriteData string
		if len(p) > n {
			failedWriteData = escapeString(p[n:])
			successfulWriteData = escapeString(p[:n])
		} else {
			failedWriteData = ""
			successfulWriteData = escapeString(p)
		}
		slog.ErrorContext(l.ctx, fmt.Sprintf("%s[WRITE-ERROR]", l.name), "error", err, "stats", l, "current_write_data", successfulWriteData, "failed_write_data", failedWriteData)
	} else {
		slog.DebugContext(l.ctx, fmt.Sprintf("%s[WRITE]", l.name), "data", escapeString(p[:n]), "stats", l)
	}

	// for {
	// 	idx := bytes.IndexByte(l.buf, '\n')
	// 	if idx < 0 {
	// 		break
	// 	}
	// 	line := string(l.buf[:idx])
	// 	l.buf = l.buf[idx+1:]
	// 	// ttys are breaking, we need to esape the line data
	// 	slog.DebugContext(l.ctx, fmt.Sprintf("%s[DATA]", l.name), "data", escapeString(line), "stats", l.LogValue())
	// }

	return n, err
}

func (l *debugWriter) Close() error {
	l.hasBeenClosed = true
	slog.InfoContext(l.ctx, fmt.Sprintf("%s[WRITER-CLOSED]", l.name), "stats", l)
	if closer, ok := l.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type debugReader struct {
	ctx            context.Context
	name           string
	r              io.Reader
	readCount      uint64
	callCount      uint64
	hasBeenClosed  bool
	sessionManager *sessionManager
}

func NewDebugReader(ctx context.Context, name string, r io.Reader) io.Reader {
	if strings.Contains(name, "network(read)->pty(write)") {
		// log a stack trace
		slog.DebugContext(ctx, "stack trace", "stack", string(debug.Stack()))
	}
	return &debugReader{
		ctx:           ctx,
		name:          name,
		r:             r,
		readCount:     0,
		callCount:     0,
		hasBeenClosed: false,
		sessionManager: &sessionManager{
			sessions:            make(map[*time.Time]struct{}),
			deletedSessionCount: 0,
			deletedSessionTime:  0,
		},
	}
}

func chainString(w any) string {
	chain := []string{}
	for w != nil {
		chain = append(chain, getNameFromReadWriter(w))
		switch wtr := any(w).(type) {
		case *debugWriter:
			w = wtr.w
		case *debugReader:
			w = wtr.r
		default:
			w = nil
		}
	}
	return strings.Join(chain, " -> ")
}

func (l *debugReader) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("bytes_read", int64(l.readCount)),
		slog.Int64("calls", int64(l.callCount)),
		slog.Any("sessions", l.sessionManager),
		slog.String("read_target", getNameFromReadWriter(l.r)),
	)
}

func (l *debugReader) Read(p []byte) (int, error) {
	l.callCount++
	defer l.sessionManager.Start()()

	n, err := l.r.Read(p)
	l.readCount += uint64(n)

	if err != nil {
		var failedReadData, successfulReadData string
		if len(p) > n {
			failedReadData = escapeString(p[n:])
			successfulReadData = escapeString(p[:n])
		} else {
			failedReadData = ""
			successfulReadData = escapeString(p)
		}
		slog.ErrorContext(l.ctx, fmt.Sprintf("%s[READ-ERROR]", l.name), "error", err, "stats", l, "current_read_data", successfulReadData, "failed_read_data", failedReadData)
	} else {
		slog.DebugContext(l.ctx, fmt.Sprintf("%s[READ]", l.name), "data", escapeString(p[:n]), "stats", l)
	}

	return n, err
}

func (l *debugReader) Close() error {
	l.hasBeenClosed = true
	slog.InfoContext(l.ctx, fmt.Sprintf("%s[READER-CLOSED]", l.name), "stats", l)
	if closer, ok := l.r.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type sessionManager struct {
	sync.Mutex
	sessions            map[*time.Time]struct{}
	deletedSessionTime  time.Duration
	deletedSessionCount uint64
}

func (s *sessionManager) CurrentTotalSessionTime() time.Duration {
	s.Lock()
	defer s.Unlock()
	now := time.Now()
	tme := s.deletedSessionTime
	for startTime := range s.sessions {
		tme += now.Sub(*startTime)
	}
	return tme
}

func (s *sessionManager) Start() func() {
	s.Lock()
	defer s.Unlock()
	startTime := time.Now()
	s.sessions[&startTime] = struct{}{}
	return func() {
		s.Lock()
		defer s.Unlock()
		s.deletedSessionTime += time.Since(startTime)
		s.deletedSessionCount++
		delete(s.sessions, &startTime)
	}
}

func (s *sessionManager) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("active", int64(len(s.sessions))),
		slog.Int64("deleted", int64(s.deletedSessionCount)),
		slog.Duration("total_time", s.CurrentTotalSessionTime()),
	)
}
