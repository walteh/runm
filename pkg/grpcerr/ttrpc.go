package grpcerr

import (
	"context"
	"log/slog"

	"github.com/containerd/errdefs/pkg/errgrpc"
	"gitlab.com/tozd/go/errors"
)

func ToContainerdTTRPC(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	slog.ErrorContext(ctx, "bubbling up shim response error", "error", err)

	return errgrpc.ToGRPC(err)
}

func ToContainerdTTRPCf(ctx context.Context, err error, format string, args ...any) error {
	if err == nil {
		return nil
	}

	finalErr := errors.Errorf(format+": %w", append(args, err)...)

	slog.ErrorContext(ctx, "bubbling up shim response error", "final_error_message", finalErr)

	return errgrpc.ToGRPCf(err, format, args...)
}
