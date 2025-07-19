package task

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/otelttrpc"
	"github.com/containerd/ttrpc"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	taskv3 "github.com/containerd/containerd/api/runtime/task/v3"
	cruntime "github.com/containerd/containerd/v2/core/runtime/v2"
	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/pkg/logging/otel"
	"github.com/walteh/runm/pkg/ticker"
)

var _ taskv3.TTRPCTaskService = &errTaskService{}
var _ cruntime.TaskServiceClient = &errTaskService{}

var _ shim.TTRPCServerUnaryOptioner = &errTaskService{}
var _ shim.TTRPCService = &errTaskService{}

type errTaskService struct {
	ref              cruntime.TaskServiceClient
	enableLogErrors  bool
	enableLogSuccess bool
}

// RegisterTTRPC implements shim.TTRPCService.
func (e *errTaskService) RegisterTTRPC(s *ttrpc.Server) error {
	// s.SetDebugging(true)
	taskv3.RegisterTTRPCTaskService(s, e)
	return nil
}

func (e *errTaskService) UnaryServerInterceptor() ttrpc.UnaryServerInterceptor {

	return otelttrpc.UnaryServerInterceptor(
		otelttrpc.WithTracerProvider(otel.GetTracerProvider()),
		otelttrpc.WithPropagators(otel.GetPropagators()),
		otelttrpc.WithMeterProvider(otel.GetMeterProvider()),
	)
}

func NewDebugTaskService(s cruntime.TaskServiceClient, enableLogErrors, enableLogSuccess bool) cruntime.TaskServiceClient {
	return &errTaskService{
		ref:              s,
		enableLogErrors:  true,
		enableLogSuccess: true,
	}
}

var counter atomic.Int64

func wrap[I, O any](e *errTaskService, f func(context.Context, I) (O, error)) func(context.Context, I) (O, error) {

	pc, _, _, _ := runtime.Caller(1)
	funcName := runtime.FuncForPC(pc).Name()
	realNameS := strings.Split(filepath.Base(funcName), ".")
	realName := realNameS[len(realNameS)-1]
	id := fmt.Sprintf("SHIM:TTRPC:SERVER:%s", realName)

	return func(ctx context.Context, req I) (resp O, retErr error) {
		start := time.Now()
		reqNum := counter.Add(1)

		startLogRecord := slog.NewRecord(start, slog.LevelDebug, fmt.Sprintf("%s[START]", id), pc)
		startLogRecord.AddAttrs(
			slog.String("method", realName),
			slog.Int64("req_num", reqNum),
		)
		slog.Default().Handler().Handle(ctx, startLogRecord)

		defer func() {
			end := time.Now()
			endLogRecord := slog.NewRecord(end, slog.LevelDebug, fmt.Sprintf("%s[END]", id), pc)
			endLogRecord.AddAttrs(
				slog.String("method", realName),
				slog.Duration("duration", end.Sub(start)),
				slog.Any("err", retErr),
				slog.Int64("req_num", reqNum),
			)
			slog.Default().Handler().Handle(ctx, endLogRecord)
			if err := recover(); err != nil {
				slog.ErrorContext(ctx, "panic in task service", "error", err, "stack", string(debug.Stack()))
				retErr = errors.Errorf("panic in task service in %s: %s", realName, err)
			}
		}()

		ctx = slogctx.Append(ctx, slog.String("ttrpc_method", realName))

		defer ticker.NewTicker(
			ticker.WithInterval(1*time.Second),
			ticker.WithStartBurst(5),
			ticker.WithFrequency(15),
			ticker.WithMessage(fmt.Sprintf("%s[RUNNING]", id)),
			ticker.WithSlogBaseContext(ctx),
			ticker.WithAttrFunc(func() []slog.Attr {
				return []slog.Attr{
					slog.String("duration", time.Since(start).String()),
					slog.Int64("req_num", reqNum),
				}
			}),
			// ticker.WithDoneMessage(fmt.Sprintf("TICK:SHIM:TTRPC:DONE  :[%s]", realName)),
		).RunAsDefer()()

		resp, retErr = f(ctx, req)

		end := time.Now()

		if retErr != nil && e.enableLogErrors {
			if trac, ok := retErr.(errors.E); ok {
				pc = trac.StackTrace()[0]
			}

			rec := slog.NewRecord(end, slog.LevelError, "error in task service", pc)
			rec.AddAttrs(
				// slog.String("NOTE", "the caller of this log has been adjusted for clarity"),
				slog.Any("error", retErr),
				slog.String("method", realName),
				slog.Duration("duration", end.Sub(start)),
			)
			if err := slog.Default().Handler().Handle(ctx, rec); err != nil {
				slog.ErrorContext(ctx, "error logging error", "error", err)
			}
		}
		if retErr == nil && e.enableLogSuccess {
			rec := slog.NewRecord(end, slog.LevelInfo, "success in task service", pc)
			rec.AddAttrs(
				slog.String("method", realName),
				slog.Duration("duration", end.Sub(start)),
			)
			if err := slog.Default().Handler().Handle(ctx, rec); err != nil {
				slog.ErrorContext(ctx, "error logging success", "error", err)
			}
		}

		// if retErr != nil {
		// 	return resp, errdefs.Resolve(retErr)
		// }

		return resp, retErr
	}
}

// Checkpoint implements taskv3.TTRPCTaskService.
func (e *errTaskService) Checkpoint(ctx context.Context, req *taskv3.CheckpointTaskRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.Checkpoint)(ctx, req)
}

// CloseIO implements taskv3.TTRPCTaskService.
func (e *errTaskService) CloseIO(ctx context.Context, req *taskv3.CloseIORequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.CloseIO)(ctx, req)
}

// Connect implements taskv3.TTRPCTaskService.
func (e *errTaskService) Connect(ctx context.Context, req *taskv3.ConnectRequest) (*taskv3.ConnectResponse, error) {
	return wrap(e, e.ref.Connect)(ctx, req)
}

// Create implements taskv3.TTRPCTaskService.
func (e *errTaskService) Create(ctx context.Context, req *taskv3.CreateTaskRequest) (*taskv3.CreateTaskResponse, error) {
	return wrap(e, e.ref.Create)(ctx, req)
}

// Delete implements taskv3.TTRPCTaskService.
func (e *errTaskService) Delete(ctx context.Context, req *taskv3.DeleteRequest) (*taskv3.DeleteResponse, error) {
	return wrap(e, e.ref.Delete)(ctx, req)
}

// Exec implements taskv3.TTRPCTaskService.
func (e *errTaskService) Exec(ctx context.Context, req *taskv3.ExecProcessRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.Exec)(ctx, req)
}

// Kill implements taskv3.TTRPCTaskService.
func (e *errTaskService) Kill(ctx context.Context, req *taskv3.KillRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.Kill)(ctx, req)
}

// Pause implements taskv3.TTRPCTaskService.
func (e *errTaskService) Pause(ctx context.Context, req *taskv3.PauseRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.Pause)(ctx, req)
}

// Pids implements taskv3.TTRPCTaskService.
func (e *errTaskService) Pids(ctx context.Context, req *taskv3.PidsRequest) (*taskv3.PidsResponse, error) {
	return wrap(e, e.ref.Pids)(ctx, req)
}

// ResizePty implements taskv3.TTRPCTaskService.
func (e *errTaskService) ResizePty(ctx context.Context, req *taskv3.ResizePtyRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.ResizePty)(ctx, req)
}

// Resume implements taskv3.TTRPCTaskService.
func (e *errTaskService) Resume(ctx context.Context, req *taskv3.ResumeRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.Resume)(ctx, req)
}

// Shutdown implements taskv3.TTRPCTaskService.
func (e *errTaskService) Shutdown(ctx context.Context, req *taskv3.ShutdownRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.Shutdown)(ctx, req)
}

// Start implements taskv3.TTRPCTaskService.
func (e *errTaskService) Start(ctx context.Context, req *taskv3.StartRequest) (*taskv3.StartResponse, error) {
	return wrap(e, e.ref.Start)(ctx, req)
}

// State implements taskv3.TTRPCTaskService.
func (e *errTaskService) State(ctx context.Context, req *taskv3.StateRequest) (*taskv3.StateResponse, error) {
	return wrap(e, e.ref.State)(ctx, req)
}

// Stats implements taskv3.TTRPCTaskService.
func (e *errTaskService) Stats(ctx context.Context, req *taskv3.StatsRequest) (*taskv3.StatsResponse, error) {
	return wrap(e, e.ref.Stats)(ctx, req)
}

// Update implements taskv3.TTRPCTaskService.
func (e *errTaskService) Update(ctx context.Context, req *taskv3.UpdateTaskRequest) (*emptypb.Empty, error) {
	return wrap(e, e.ref.Update)(ctx, req)
}

// Wait implements taskv3.TTRPCTaskService.
func (e *errTaskService) Wait(ctx context.Context, req *taskv3.WaitRequest) (*taskv3.WaitResponse, error) {
	return wrap(e, e.ref.Wait)(ctx, req)
}
