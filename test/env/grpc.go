package env

import (
	"os"
	_ "unsafe"

	"context"

	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//go:linkname globalDialOptions google.golang.org/grpc.globalDialOptions
var globalDialOptions []grpc.DialOption

//go:linkname globalServerOptions google.golang.org/grpc.globalServerOptions
var globalServerOptions []grpc.ServerOption

func init() {
	ctx := context.Background()

	if globalDialOptions == nil {
		globalDialOptions = []grpc.DialOption{}
	}
	clientopts := []grpc.DialOption{
		otel.GetGrpcClientOpts(),
		grpcerr.GetGrpcClientOptsCtx(ctx),
		// insecure
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	globalDialOptions = append(globalDialOptions, clientopts...)

	if globalServerOptions == nil {
		globalServerOptions = []grpc.ServerOption{}
	}

	globalServerOptions = append(globalServerOptions, otel.GetGrpcServerOpts()...)
	globalServerOptions = append(globalServerOptions, grpcerr.GetGrpcServerOptsCtx(ctx)...)

	os.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	os.Setenv("RUNM_USING_TEST_ENV", "1")
	// os.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
	// os.Setenv("OTEL_EXPORTER_OTLP_METRICS_INSECURE", "true")

}
