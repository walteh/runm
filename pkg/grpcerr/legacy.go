package grpcerr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/stackerr"
	"github.com/walteh/runm/pkg/ticker"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func LegacyUnaryServerInterceptor(
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
