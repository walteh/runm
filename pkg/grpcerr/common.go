package grpcerr

import (
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	slogctx "github.com/veqryn/slog-context"
	"google.golang.org/grpc"
)

var thisProjectModule string

func init() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	thisProjectModule = bi.Main.Path
}

func GetGrpcServerOptsCtx(ctx context.Context) []grpc.ServerOption {
	return []grpc.ServerOption{
		// the stats handler actually works perfect, except for the codePointer
		// which it cannot grab via the data provided. so we need to use the interceptor
		grpc.ChainUnaryInterceptor(UnaryServerInterceptor),
		// grpc.StatsHandler(NewServerHandler(ctx)),
	}
}

func GetGrpcClientOptsCtx(ctx context.Context) grpc.DialOption {
	return grpc.WithStatsHandler(NewClientHandler(ctx))
}

func GetGrpcClientOpts() grpc.DialOption {
	return GetGrpcClientOptsCtx(context.Background())
}

func GetGrpcServerOpts() []grpc.ServerOption {
	return GetGrpcServerOptsCtx(context.Background())
}

var ignoredMethods = map[string]bool{
	"/containerd.services.content.v1.Content/Status":       true,
	"/containerd.services.content.v1.Content/ListStatuses": true,
	"/grpc.health.v1.Health/Check":                         true,
	"/containerd.services.content.v1.Content/Info":         true,
	"/containerd.services.content.v1.Content/Read":         true,
	"/containerd.services.containers.v1.Containers/Get":    true,
	"/containerd.services.leases.v1.Leases/Delete":         true,
	// "/containerd.services.images.v1.Images/List":           true,
	"/containerd.services.snapshots.v1.Snapshots/Stat": true,
	"/containerd.services.tasks.v1.Tasks/Get":          true,
}

var ignoredServices = map[string]bool{
	"containerd.services.content.v1.Content":                  true,
	"grpc.health.v1.Health":                                   true,
	"containerd.services.leases.v1.Leases":                    true,
	"containerd.services.introspection.v1.Introspection":      true,
	"opentelemetry.proto.collector.logs.v1.LogsService":       true,
	"opentelemetry.proto.collector.metrics.v1.MetricsService": true,
	"opentelemetry.proto.collector.trace.v1.TraceService":     true,
}

var ignoredMethodsLock sync.Mutex

func isMethodIgnored(method string) bool {
	ignoredMethodsLock.Lock()
	defer ignoredMethodsLock.Unlock()
	return ignoredMethods[method] || ignoredServices[filepath.Base(filepath.Dir(method))]
}

func mergeContext(ctx context.Context, grpcctx context.Context) context.Context {
	dat := slogctx.ExtractAppended(ctx, time.Now(), 0, "")
	anyattrs := make([]any, len(dat))
	for i, attr := range dat {
		anyattrs[i] = attr
	}
	return slogctx.Append(grpcctx, anyattrs...)
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
