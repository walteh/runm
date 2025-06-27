package grpcerr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/walteh/runm/pkg/stackerr"
)

// UnaryServerInterceptor returns a grpc.UnaryServerInterceptor that
// converts any returned error into a gRPC status carrying the full Tozd error chain.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (_ interface{}, err error) {
		start := time.Now()
		slog.DebugContext(ctx, fmt.Sprintf("SERVER_START[%s]", info.FullMethod))
		var errz error
		var resp interface{}
		defer func() {
			slog.DebugContext(ctx, fmt.Sprintf("SERVER_END[%s]", info.FullMethod), "error", errz, "duration", time.Since(start))
		}()
		resp, errz = handler(ctx, req)
		if errz == nil {
			return resp, nil
		}
		// Create a gRPC status with code and top‐level message  [oai_citation:0‡grpc.io](https://grpc.io/docs/guides/error/?utm_source=chatgpt.com).
		st := status.New(codes.Internal, errz.Error())
		// Attach the full %+v formatted Tozd error chain as DebugInfo  [oai_citation:1‡pkg.go.dev](https://pkg.go.dev/google.golang.org/genproto/googleapis/rpc/errdetails?utm_source=chatgpt.com) [oai_citation:2‡medium.com](https://medium.com/utility-warehouse-technology/advanced-grpc-error-usage-1b37398f0ff4?utm_source=chatgpt.com).

		se := stackerr.NewStackedEncodableErrorFromError(errz)

		encoded, err := json.Marshal(se)
		if err != nil {
			err = errz
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
}

func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		start := time.Now()
		slog.DebugContext(ctx, fmt.Sprintf("CLIENT_START[%s]", method))
		errd := invoker(ctx, method, req, reply, cc, opts...)
		slog.DebugContext(ctx, fmt.Sprintf("CLIENT_END[%s]", method), "error", errd, "duration", time.Since(start))
		return errd
	}
}

func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, stream)
	}
}

func StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(ctx, desc, cc, method, opts...)
	}
}

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
