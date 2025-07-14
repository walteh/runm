package grpcerr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/containerd/ttrpc"

	"github.com/walteh/runm/pkg/stackerr"
	"github.com/walteh/runm/pkg/ticker"
)

// NewUnaryServerTTRPCInterceptor returns a ttrpc.UnaryServerInterceptor that
// converts any returned error into a TTRPC error carrying the full error chain.
func NewUnaryServerTTRPCInterceptor(ctx context.Context) ttrpc.UnaryServerInterceptor {
	return func(grpcctx context.Context, unmarshal ttrpc.Unmarshaler, info *ttrpc.UnaryServerInfo, method ttrpc.Method) (interface{}, error) {
		return UnaryServerTTRPCInterceptor(mergeContext(ctx, grpcctx), unmarshal, info, method)
	}
}

// NewStreamServerTTRPCInterceptor returns a ttrpc.StreamServerInterceptor that
// converts any returned error into a TTRPC error carrying the full error chain.
func NewStreamServerTTRPCInterceptor(ctx context.Context) ttrpc.StreamServerInterceptor {
	return func(grpcctx context.Context, stream ttrpc.StreamServer, info *ttrpc.StreamServerInfo, handler ttrpc.StreamHandler) (interface{}, error) {
		return StreamServerTTRPCInterceptor(mergeContext(ctx, grpcctx), stream, info, handler)
	}
}

// NewUnaryClientTTRPCInterceptor returns a ttrpc.UnaryClientInterceptor that
// enhances error handling for client calls.
func NewUnaryClientTTRPCInterceptor(ctx context.Context) ttrpc.UnaryClientInterceptor {
	return func(grpcctx context.Context, req *ttrpc.Request, resp *ttrpc.Response, info *ttrpc.UnaryClientInfo, invoker ttrpc.Invoker) error {
		return UnaryClientTTRPCInterceptor(mergeContext(ctx, grpcctx), req, resp, info, invoker)
	}
}

// UnaryServerTTRPCInterceptor provides error handling, logging, and timing for unary TTRPC calls
func UnaryServerTTRPCInterceptor(
	ctx context.Context,
	unmarshal ttrpc.Unmarshaler,
	info *ttrpc.UnaryServerInfo,
	method ttrpc.Method,
) (_ interface{}, err error) {
	start := time.Now()
	operation := filepath.Base(info.FullMethod)
	service := filepath.Base(filepath.Dir(info.FullMethod))
	id := fmt.Sprintf("TTRPC:SERVER:%s:%s", service, operation)
	slog.DebugContext(ctx, fmt.Sprintf("%s[START]", id), "service", service)

	var errz error
	var resp interface{}

	defer func() {
		slog.DebugContext(ctx, fmt.Sprintf("%s[END]", id), "service", service, "error", errz, "duration", time.Since(start))
	}()

	defer ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", id)),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
	).RunAsDefer()()

	resp, errz = method(ctx, unmarshal)
	if errz == nil {
		return resp, nil
	}

	// Create a TTRPC error with encoded stack information
	se := stackerr.NewStackedEncodableErrorFromError(errz)
	encoded, err := json.Marshal(se)
	if err != nil {
		err = errz
		return
	}

	// Create a TTRPC error with encoded stack information in the error message
	err = fmt.Errorf("ttrpc error with stack: %w [encoded_stack: %s]", errz, string(encoded))
	return
}

// StreamServerTTRPCInterceptor provides error handling, logging, and timing for streaming TTRPC calls
func StreamServerTTRPCInterceptor(
	ctx context.Context,
	stream ttrpc.StreamServer,
	info *ttrpc.StreamServerInfo,
	handler ttrpc.StreamHandler,
) (_ interface{}, err error) {
	start := time.Now()
	operation := filepath.Base(info.FullMethod)
	service := filepath.Base(filepath.Dir(info.FullMethod))
	id := fmt.Sprintf("TTRPC:STREAM:SERVER:%s:%s", service, operation)
	slog.DebugContext(ctx, fmt.Sprintf("%s[START]", id), "service", service, "streaming_client", info.StreamingClient, "streaming_server", info.StreamingServer)

	var errz error
	var resp interface{}

	defer func() {
		slog.DebugContext(ctx, fmt.Sprintf("%s[END]", id), "service", service, "error", errz, "duration", time.Since(start))
	}()

	defer ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", id)),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
	).RunAsDefer()()

	resp, errz = handler(ctx, stream)
	if errz == nil {
		return resp, nil
	}

	// Create a TTRPC error with encoded stack information
	se := stackerr.NewStackedEncodableErrorFromError(errz)
	encoded, err := json.Marshal(se)
	if err != nil {
		err = errz
		return
	}

	// Create a TTRPC error with encoded stack information in the error message
	err = fmt.Errorf("ttrpc stream error with stack: %w [encoded_stack: %s]", errz, string(encoded))
	return
}

// UnaryClientTTRPCInterceptor provides error handling, logging, and timing for unary TTRPC client calls
func UnaryClientTTRPCInterceptor(
	ctx context.Context,
	req *ttrpc.Request,
	resp *ttrpc.Response,
	info *ttrpc.UnaryClientInfo,
	invoker ttrpc.Invoker,
) (err error) {
	start := time.Now()
	service := filepath.Base(filepath.Dir(info.FullMethod))
	operation := filepath.Base(info.FullMethod)
	id := fmt.Sprintf("TTRPC:CLIENT:%s:%s", service, operation)
	event := fmt.Sprintf("%s[START]", id)
	slog.DebugContext(ctx, event, "service", service)

	defer ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", id)),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
	).RunAsDefer()()

	errd := invoker(ctx, req, resp)
	slog.DebugContext(ctx, fmt.Sprintf("%s[END]", id), "service", service, "error", errd, "duration", time.Since(start))
	return errd
}
