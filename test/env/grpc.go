package env

import (
	"os"
	_ "unsafe"

	"context"

	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//go:linkname globalDialOptions google.golang.org/grpc.globalDialOptions
var globalDialOptions []grpc.DialOption

func init() {
	if globalDialOptions == nil {
		globalDialOptions = []grpc.DialOption{}
	}
	clientopts := []grpc.DialOption{
		otel.GetGrpcClientOpts(),
		grpcerr.GetGrpcClientOptsCtx(context.Background()),
		// insecure
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	globalDialOptions = append(globalDialOptions, clientopts...)

	otlptracegrpc.NewClient()

	os.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	// os.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
	// os.Setenv("OTEL_EXPORTER_OTLP_METRICS_INSECURE", "true")

}
