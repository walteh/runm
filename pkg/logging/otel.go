package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/net/proxy"

	"gitlab.com/tozd/go/errors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

type OTelInstances struct {
	Propagator     propagation.TextMapPropagator
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	ErrorHandler   otel.ErrorHandler
	LoggerProvider *log.LoggerProvider
	Resource       *resource.Resource
	Conn           *grpc.ClientConn
}

func (i *OTelInstances) EnableGlobally() {
	global.SetLoggerProvider(i.LoggerProvider)
	otel.SetTextMapPropagator(i.Propagator)
	otel.SetTracerProvider(i.TracerProvider)
	otel.SetMeterProvider(i.MeterProvider)
	otel.SetErrorHandler(i.ErrorHandler)
}

func (i *OTelInstances) GetGrpcServerOpts() grpc.ServerOption {
	handler := otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(i.TracerProvider),
		otelgrpc.WithMeterProvider(i.MeterProvider),
		otelgrpc.WithPropagators(i.Propagator),
	)
	return grpc.StatsHandler(handler)
}

func (i *OTelInstances) GetGrpcClientOpts() grpc.DialOption {
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(i.TracerProvider),
		otelgrpc.WithMeterProvider(i.MeterProvider),
		otelgrpc.WithPropagators(i.Propagator),
	))
}

func (i *OTelInstances) Shutdown(ctx context.Context) error {
	var err error
	for _, fn := range []func(context.Context) error{
		func(ctx context.Context) error {
			if i.TracerProvider != nil {
				return i.TracerProvider.Shutdown(ctx)
			}
			return nil
		},
		func(ctx context.Context) error {
			if i.MeterProvider != nil {
				return i.MeterProvider.Shutdown(ctx)
			}
			return nil
		},
		func(ctx context.Context) error {
			if i.LoggerProvider != nil {
				return i.LoggerProvider.Shutdown(ctx)
			}
			return nil
		},
	} {
		err = errors.Join(err, fn(ctx))
	}
	return err
}

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func NewGRPCOtelInstances(ctx context.Context, conn net.Conn, serviceName string) (*OTelInstances, error) {

	var err error
	instances := &OTelInstances{}
	shouldShutdown := true
	defer func() {
		if shouldShutdown {
			instances.Shutdown(ctx)
		}
	}()

	// resolve the link of os.Args[0]
	executable, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		return nil, errors.Errorf("resolving executable: %w", err)
	}

	instances.Conn, err = initConnFromConn(ctx, conn)
	if err != nil {
		return nil, errors.Errorf("initializing gRPC connection: %w", err)
	}

	arch := semconv.HostArchARM64
	if runtime.GOARCH == "amd64" {
		arch = semconv.HostArchAMD64
	}

	instances.Resource, err = resource.New(ctx, resource.WithAttributes(
		semconv.OSName(runtime.GOOS),
		semconv.ServiceName(serviceName),
		semconv.ProcessParentPID(os.Getppid()),
		arch,
		semconv.ProcessPID(os.Getpid()),
		semconv.ProcessExecutableName(filepath.Base(executable)),
		semconv.ProcessCommandArgs(strings.Join(os.Args, " ")),
		semconv.ProcessRuntimeDescription(runtime.Version()),
	))
	if err != nil {
		return nil, errors.Errorf("initializing resource: %w", err)
	}

	var filterFunc func(sdktrace.SpanExporter) sdktrace.SpanExporter
	if serviceName == "containerd" {
		filterFunc = func(exporter sdktrace.SpanExporter) sdktrace.SpanExporter {
			return &containerdTaskFilteringExporter{next: exporter}
		}
	} else {
		filterFunc = func(exporter sdktrace.SpanExporter) sdktrace.SpanExporter {
			return exporter
		}
	}

	// Set up propagator.
	instances.Propagator = newPropagator()
	// otel.SetTextMapPropagator(prop)

	// Set up trace provider.
	instances.TracerProvider, err = initTracerProvider(ctx, instances.Resource, instances.Conn, filterFunc)
	if err != nil {
		return nil, errors.Errorf("initializing tracer provider: %w", err)
	}

	// Set up meter provider.
	instances.MeterProvider, err = initMeterProvider(ctx, instances.Resource, instances.Conn)
	if err != nil {
		return nil, errors.Errorf("initializing meter provider: %w", err)
	}

	// Set up logger provider.
	instances.LoggerProvider, err = newLoggerProvider(ctx, instances.Resource, instances.Conn)
	if err != nil {
		return nil, errors.Errorf("initializing logger provider: %w", err)
	}

	instances.ErrorHandler = otel.ErrorHandlerFunc(func(err error) {
		slog.Warn("error in OpenTelemetry", "error", err)
	})

	shouldShutdown = false

	return instances, nil
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// Initialize a gRPC connection to be used by both the tracer and meter
// providers.
func initConnFromDialer(ctx context.Context, dialer proxy.ContextDialer) (*grpc.ClientConn, error) {

	conn, err := grpc.NewClient(
		"passthrough://",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", addr)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	return conn, err
}

// Initialize a gRPC connection to be used by both the tracer and meter
// providers.
func initConnFromConn(ctx context.Context, nconn net.Conn) (*grpc.ClientConn, error) {

	conn, err := grpc.NewClient(
		"passthrough://",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return nconn, nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	return conn, err
}

// Initializes an OTLP exporter, and configures the corresponding trace provider.
func initTracerProvider(ctx context.Context, res *resource.Resource, conn *grpc.ClientConn, filterFunc func(sdktrace.SpanExporter) sdktrace.SpanExporter) (*sdktrace.TracerProvider, error) {
	// Set up a trace exporter
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Register the trace exporter with a TracerProvider, using a batch
	// span processor to aggregate spans before export.
	bsp := sdktrace.NewBatchSpanProcessor(filterFunc(traceExporter))

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// otel.SetTracerProvider(tracerProvider)

	// Set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Shutdown will flush any remaining spans and shut down the exporter.
	return tracerProvider, nil
}

// Initializes an OTLP exporter, and configures the corresponding meter provider.
func initMeterProvider(ctx context.Context, res *resource.Resource, conn *grpc.ClientConn) (*sdkmetric.MeterProvider, error) {
	metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	// otel.SetMeterProvider(meterProvider)

	return meterProvider, nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource, conn *grpc.ClientConn) (*log.LoggerProvider, error) {
	logExporter, err := otlploggrpc.New(ctx, otlploggrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}

	loggerProvider := log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
		log.WithResource(res),
	)
	return loggerProvider, nil
}

// filteringExporter wraps a SpanExporter and drops non-ttrpc spans.
type containerdTaskFilteringExporter struct {
	next sdktrace.SpanExporter
}

func (fe *containerdTaskFilteringExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	var filtered []sdktrace.ReadOnlySpan
	for _, span := range spans {
		// Only export spans from the ttrpc instrumentation library:
		if strings.HasPrefix(span.Name(), "containerd.task.v3.Task") {
			filtered = append(filtered, span)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return fe.next.ExportSpans(ctx, filtered)
}

func (fe *containerdTaskFilteringExporter) Shutdown(ctx context.Context) error {
	return fe.next.Shutdown(ctx)
}
