package grpcerr

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"

	"google.golang.org/grpc"

	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/ticker"
)

func UnaryClientInterceptor(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) (err error) {
	if isMethodIgnored(method) {
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	start := time.Now()
	service := filepath.Base(filepath.Dir(method))
	operation := filepath.Base(method)
	id := fmt.Sprintf("GRPC:CLIENT:%s:%s", service, operation)
	event := fmt.Sprintf("%s[START]", id)

	codePointer := lastNonGrpcCaller(ctx)

	logging.LogCaller(ctx, slog.LevelDebug, codePointer, event)
	// if event == "GRPC:CLIENT:runm.v1.SocketAllocatorService:CloseIO[START]" {
	// 	slog.InfoContext(ctx, string(debug.Stack()))
	// }

	defer ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithCallerSkip(4),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", id)),
		// ticker.WithDoneMessage(fmt.Sprintf("TICK:GRPC-CLIENT:%s:%s[DONE]", service, operation)),
	).RunAsDefer()()

	errd := invoker(ctx, method, req, reply, cc, opts...)
	logging.LogCaller(ctx, slog.LevelDebug, codePointer, fmt.Sprintf("%s[END]", id), "error", errd, "duration", time.Since(start))
	return errd
}

func UnaryServerInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (_ interface{}, err error) {
	if isMethodIgnored(info.FullMethod) {
		return handler(ctx, req)
	}

	start := time.Now()
	operation := filepath.Base(info.FullMethod)
	service := filepath.Base(filepath.Dir(info.FullMethod))
	id := fmt.Sprintf("GRPC:SERVER:%s:%s", service, operation)

	codePointer := implLocation(info)

	if codePointer == 0 {
		codePointer, _, _, _ = runtime.Caller(0)
	}

	logging.LogCaller(ctx, slog.LevelDebug, codePointer, fmt.Sprintf("%s[START]", id))

	var errz error
	// var resp interface{}

	defer func() {
		logging.LogCaller(ctx, slog.LevelDebug, codePointer, fmt.Sprintf("%s[END]", id), "error", errz, "duration", time.Since(start))
	}()

	defer ticker.NewTicker(
		ticker.WithInterval(1*time.Second),
		ticker.WithStartBurst(5),
		ticker.WithFrequency(15),
		ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", id)),
		ticker.WithCallerUintptr(codePointer),
		ticker.WithSlogBaseContext(ctx),
		ticker.WithLogLevel(slog.LevelDebug),
	).RunAsDefer()()

	// resp, errz = handler(ctx, req)
	// if errz == nil {
	// 	return resp, nil
	// }

	return handler(ctx, req)

}
