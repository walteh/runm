package grpcerr

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"

	"google.golang.org/grpc/stats"

	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/ticker"
)

// RPC tracking data stored in context
type rpcData struct {
	start          time.Time
	operation      string
	service        string
	id             string
	codePointer    uintptr
	tickerStop     func()
	isClient       bool
	fullMethodName string
}

const rpcDataKey = "grpcerr_rpc_data"

func getRPCData(ctx context.Context) *rpcData {
	if ctx == nil {
		return nil
	}
	data, ok := ctx.Value(rpcDataKey).(*rpcData)
	if !ok {
		return nil
	}
	return data
}

// ClientHandler implements stats.Handler for client-side RPC handling
var _ stats.Handler = &ClientHandler{}

type ClientHandler struct {
	ctx context.Context
}

// NewClientHandler creates a new client stats handler
func NewClientHandler(ctx context.Context) *ClientHandler {
	return &ClientHandler{ctx: ctx}
}

// HandleConn implements stats.Handler for client
func (c *ClientHandler) HandleConn(ctx context.Context, cs stats.ConnStats) {
	// Connection-level stats - we don't need to handle these for our use case
}

// HandleRPC implements stats.Handler for client
func (c *ClientHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	switch stat := rs.(type) {
	case *stats.Begin:
		c.handleBegin(ctx, stat, true)
	case *stats.End:
		c.handleEnd(ctx, stat)
	}
}

// TagConn implements stats.Handler for client
func (c *ClientHandler) TagConn(ctx context.Context, cti *stats.ConnTagInfo) context.Context {
	return ctx
}

// TagRPC implements stats.Handler for client
func (c *ClientHandler) TagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	mergedCtx := mergeContext(c.ctx, ctx)
	return context.WithValue(mergedCtx, rpcDataKey, &rpcData{
		fullMethodName: rti.FullMethodName,
	})
}

func (c *ClientHandler) handleBegin(ctx context.Context, _ *stats.Begin, isClient bool) {
	data := getRPCData(ctx)
	if data == nil {
		return
	}

	if isMethodIgnored(data.fullMethodName) {
		return
	}

	data.start = time.Now()
	data.operation = filepath.Base(data.fullMethodName)
	data.service = filepath.Base(filepath.Dir(data.fullMethodName))
	data.isClient = isClient

	var prefix string
	if isClient {
		prefix = "GRPC:CLIENT"
		data.codePointer = lastNonGrpcCaller(ctx)
	} else {
		prefix = "GRPC:SERVER"
		// no way to get the code pointer for the server, use the interceptor instead of the stats handler
	}

	data.id = fmt.Sprintf("%s:%s:%s", prefix, data.service, data.operation)

	if data.codePointer == 0 {
		data.codePointer, _, _, _ = runtime.Caller(0)
	}

	logging.LogCaller(ctx, slog.LevelDebug, data.codePointer, fmt.Sprintf("%s[START]", data.id))

	// Start ticker for long-running operations
	data.tickerStop = ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", data.id)),
		ticker.WithCallerUintptr(data.codePointer),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
	).RunAsDefer()
}

func (c *ClientHandler) handleEnd(ctx context.Context, stat *stats.End) {
	data := getRPCData(ctx)
	if data == nil {
		return
	}

	// Stop ticker
	if data.tickerStop != nil {
		data.tickerStop()
	}

	duration := time.Since(data.start)
	err := stat.Error

	logging.LogCaller(ctx, slog.LevelDebug, data.codePointer, fmt.Sprintf("%s[END]", data.id), "error", err, "duration", duration)
}

// ServerHandler implements stats.Handler for server-side RPC handling
var _ stats.Handler = &ServerHandler{}

type ServerHandler struct {
	ctx context.Context
}

// NewServerHandler creates a new server stats handler
func NewServerHandler(ctx context.Context) *ServerHandler {
	return &ServerHandler{ctx: ctx}
}

// HandleConn implements stats.Handler for server
func (s *ServerHandler) HandleConn(ctx context.Context, cs stats.ConnStats) {
	// Connection-level stats - we don't need to handle these for our use case
}

// HandleRPC implements stats.Handler for server
func (s *ServerHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	switch stat := rs.(type) {
	case *stats.Begin:
		s.handleBegin(ctx, stat, false)
	case *stats.End:
		s.handleEnd(ctx, stat)
	}
}

// TagConn implements stats.Handler for server
func (s *ServerHandler) TagConn(ctx context.Context, cti *stats.ConnTagInfo) context.Context {
	return ctx
}

// TagRPC implements stats.Handler for server
func (s *ServerHandler) TagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	mergedCtx := mergeContext(s.ctx, ctx)
	return context.WithValue(mergedCtx, rpcDataKey, &rpcData{
		fullMethodName: rti.FullMethodName,
	})
}

func (s *ServerHandler) handleBegin(ctx context.Context, stat *stats.Begin, isClient bool) {
	data := getRPCData(ctx)
	if data == nil {
		return
	}

	if isMethodIgnored(data.fullMethodName) {
		return
	}

	data.start = stat.BeginTime
	data.operation = filepath.Base(data.fullMethodName)
	data.service = filepath.Base(filepath.Dir(data.fullMethodName))
	data.isClient = isClient

	var prefix string
	if isClient {
		prefix = "GRPC:CLIENT"
		data.codePointer = lastNonGrpcCaller(ctx)
	} else {
		prefix = "GRPC:SERVER"
		// no way to get the code pointer for the server, use the interceptor instead of the stats handler
	}

	data.id = fmt.Sprintf("%s:%s:%s", prefix, data.service, data.operation)

	if data.codePointer == 0 {
		data.codePointer, _, _, _ = runtime.Caller(0)
	}

	logging.LogCaller(ctx, slog.LevelDebug, data.codePointer, fmt.Sprintf("%s[START]", data.id))

	// Start ticker for long-running operations
	data.tickerStop = ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", data.id)),
		ticker.WithCallerUintptr(data.codePointer),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
	).RunAsDefer()
}

func (s *ServerHandler) handleEnd(ctx context.Context, stat *stats.End) {
	data := getRPCData(ctx)
	if data == nil {
		return
	}

	// Stop ticker
	if data.tickerStop != nil {
		data.tickerStop()
	}

	duration := time.Since(data.start)
	var err error = stat.Error

	// For server-side, handle error encoding like the original interceptor
	if !data.isClient && err != nil {
		err = s.processServerError(err)
	}

	logging.LogCaller(ctx, slog.LevelDebug, data.codePointer, fmt.Sprintf("%s[END]", data.id), "error", err, "duration", duration)
}

func (s *ServerHandler) processServerError(err error) error {
	return err
}
