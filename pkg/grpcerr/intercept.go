package grpcerr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/stackerr"
	"github.com/walteh/runm/pkg/ticker"
)

// UnaryServerInterceptor returns a grpc.UnaryServerInterceptor that
// converts any returned error into a gRPC status carrying the full Tozd error chain.

func mergeContext(ctx context.Context, grpcctx context.Context) context.Context {
	dat := slogctx.ExtractAppended(ctx, time.Now(), 0, "")
	anyattrs := make([]any, len(dat))
	for i, attr := range dat {
		anyattrs[i] = attr
	}
	return slogctx.Append(grpcctx, anyattrs...)
}

// func NewUnaryServerInterceptor(ctx context.Context) grpc.UnaryServerInterceptor {
// 	return func(grpcctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
// 		return UnaryServerInterceptor(mergeContext(ctx, grpcctx), req, info, handler)
// 	}
// }

// func NewStreamServerInterceptor(ctx context.Context) grpc.StreamServerInterceptor {
// 	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
// 		return handler(srv, stream)
// 	}
// }

// func NewUnaryClientInterceptor(ctx context.Context) grpc.UnaryClientInterceptor {
// 	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
// 		return UnaryClientInterceptor(mergeContext(ctx, ctx), method, req, reply, cc, invoker, opts...)
// 	}
// }

// func NewStreamClientInterceptor(ctx context.Context) grpc.StreamClientInterceptor {
// 	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
// 		return streamer(mergeContext(ctx, ctx), desc, cc, method, opts...)
// 	}
// }

var ignoredMethods = map[string]bool{
	"/containerd.services.content.v1.Content/Status": true,
	"/grpc.health.v1.Health/Check":                   true,
}

var ignoredMethodsLock sync.Mutex

func isMethodIgnored(method string) bool {
	ignoredMethodsLock.Lock()
	defer ignoredMethodsLock.Unlock()
	return ignoredMethods[method]
}

// implLocation returns the concrete service implementation’s PC, file and line.
// Call this *inside* your interceptor.
func implLocation(info *grpc.UnaryServerInfo) (pc uintptr) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in implLocation", "error", r)
			pc = 0
		}
	}()
	// Extract final element of /pkg.Service/Foo  → "Foo"
	parts := strings.Split(info.FullMethod, "/")
	meth := parts[len(parts)-1]

	// 1) Work with the *pointer type*; that’s where pointer-receiver methods live.
	t := reflect.TypeOf(info.Server)        // e.g. *myService
	m, ok := t.MethodByName(meth)           // try pointer type first
	if !ok && t.Kind() == reflect.Pointer { // fall back to value type just in case
		m, ok = t.Elem().MethodByName(meth)
	}
	if !ok {
		return 0
	}

	pc = m.Func.Pointer()
	return
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
	var resp interface{}

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

	resp, errz = handler(ctx, req)
	if errz == nil {
		return resp, nil
	}

	enableStackError := os.Getenv("ENABLE_STACK_ERROR") == "1"

	if !enableStackError {
		return resp, errz
	}

	st := status.New(codes.Internal, errz.Error())

	se := stackerr.NewStackedEncodableErrorFromError(errz)

	encoded, errd := json.Marshal(se)
	if errd != nil {
		err = errz // ignore errd
		return
	}

	ei := &errdetails.ErrorInfo{
		Reason: errz.Error(),
		Domain: stackerr.Domain(),
		Metadata: map[string]string{
			"encoded_stack_error": string(encoded),
		},
	}

	st2, serr := st.WithDetails(ei)
	if serr != nil {
		err = st.Err()
		// Fallback: return bare status if adding details fails  [oai_citation:3‡github.com](https://github.com/grpc/grpc-go/issues/1233?utm_source=chatgpt.com).

	} else {
		err = st2.Err()
	}

	return

}

var thisProjectModule string

func init() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	thisProjectModule = bi.Main.Path
}

func lastNonGrpcCaller(ctx context.Context) uintptr {

	fallback := uintptr(0)
	for i := 5; i < 20; i++ {
		pc, file, _, ok := runtime.Caller(i)
		if !ok {
			continue
		}
		if fallback == 0 {
			fallback = pc
		}
		if strings.HasSuffix(file, ".pb.go") {
			continue
		}

		funcd := runtime.FuncForPC(pc)
		fn := funcd.Name()
		if strings.HasPrefix(fn, "google.golang.org/grpc") {
			continue
		}
		return pc
	}

	return fallback
}

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

// func StreamServerInterceptor() grpc.StreamServerInterceptor {
// 	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
// 		return handler(srv, stream)
// 	}
// }

// func StreamClientInterceptor() grpc.StreamClientInterceptor {
// 	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
// 		return streamer(ctx, desc, cc, method, opts...)
// 	}
// }

// FromError inspects a gRPC error, extracts any DebugInfo detail,
// and returns a new Tozd error containing the full %+v stack trace.

// var _ stats.Handler = (*serverHandler)(nil)

// type serverHandler struct{}

// // HandleConn implements stats.Handler.
// func (s *serverHandler) HandleConn(context.Context, stats.ConnStats) {
// }

// // HandleRPC implements stats.Handler.
// func (s *serverHandler) HandleRPC(context.Context, stats.RPCStats) {
// }

// // TagConn implements stats.Handler.
// func (s *serverHandler) TagConn(context.Context, *stats.ConnTagInfo) context.Context {
// 	panic("unimplemented")
// }

// // TagRPC implements stats.Handler.
// func (s *serverHandler) TagRPC(context.Context, *stats.RPCTagInfo) context.Context {
// 	panic("unimplemented")
// }
