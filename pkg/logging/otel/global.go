package otel

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"

	"gitlab.com/tozd/go/errors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"

	realotel "go.opentelemetry.io/otel"
	realotellog "go.opentelemetry.io/otel/log"
	realglobal "go.opentelemetry.io/otel/log/global"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var defaultOTelInstances *OTelInstances
var instancesLock sync.Mutex

type ProxyDialerFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func ConfigureOTelSDK(ctx context.Context, serviceName string) (cleanup func(), err error) {

	grpc.EnableTracing = true

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		return ConfigureOTelSDKWithDialer(ctx, serviceName, true, func(ctx context.Context, network, addr string) (net.Conn, error) {
			// dns:// is required: https://github.com/open-telemetry/opentelemetry-go/issues/5562
			endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
			endpoint = strings.TrimPrefix(endpoint, "dns://")
			endpoint = strings.TrimPrefix(endpoint, "http://")
			endpoint = strings.TrimPrefix(endpoint, "https://")
			return net.Dial("tcp", endpoint)
		})
	}

	return ConfigureOTelSDKWithDialer(ctx, serviceName, false, nil)
}

func ConfigureOTelSDKWithNoExporter(ctx context.Context, serviceName string) (cleanup func(), err error) {
	return ConfigureOTelSDKWithDialer(ctx, serviceName, false, nil)
}

func ConfigureOTelSDKWithDialer(ctx context.Context, serviceName string, enable bool, dialer ProxyDialerFunc) (cleanup func(), err error) {

	instancesLock.Lock()
	defer instancesLock.Unlock()

	if defaultOTelInstances != nil {
		return nil, errors.Errorf("OpenTelemetry instances already initialized")
	}

	ctxdialer := NewContextDialerFunc(dialer)
	if !enable {
		ctxdialer = nil
	}

	instances, err := setupOTelSDK(ctx, serviceName, ctxdialer)
	if err != nil {
		return nil, errors.Errorf("initializing OpenTelemetry instances: %w", err)
	}

	instances.EnableGlobally()

	cleanup = func() {
		if err := instances.Shutdown(ctx); err != nil {
			slog.Error("error shutting down OpenTelemetry", "error", err)
		}
	}
	return cleanup, nil
}

func GetTracerProvider() *sdktrace.TracerProvider {
	instancesLock.Lock()
	defer instancesLock.Unlock()
	if defaultOTelInstances == nil {
		return sdktrace.NewTracerProvider()
	}
	return defaultOTelInstances.TracerProvider
}

func GetPropagators() propagation.TextMapPropagator {
	instancesLock.Lock()
	defer instancesLock.Unlock()
	if defaultOTelInstances == nil {
		return realotel.GetTextMapPropagator()
	}
	return defaultOTelInstances.Propagator
}

func GetMeterProvider() metric.MeterProvider {
	instancesLock.Lock()
	defer instancesLock.Unlock()
	if defaultOTelInstances == nil {
		return realotel.GetMeterProvider()
	}
	return defaultOTelInstances.MeterProvider
}

func GetLoggerProvider() realotellog.LoggerProvider {
	instancesLock.Lock()
	defer instancesLock.Unlock()
	if defaultOTelInstances == nil {
		return realglobal.GetLoggerProvider()
	}
	return defaultOTelInstances.LoggerProvider
}

func GetGrpcServerOpts() grpc.ServerOption {
	instancesLock.Lock()
	defer instancesLock.Unlock()
	if defaultOTelInstances == nil {
		return grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithTracerProvider(realotel.GetTracerProvider()),
			otelgrpc.WithMeterProvider(realotel.GetMeterProvider()),
			otelgrpc.WithPropagators(realotel.GetTextMapPropagator()),
		))
	}
	return grpc.StatsHandler(otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(defaultOTelInstances.TracerProvider),
		otelgrpc.WithMeterProvider(defaultOTelInstances.MeterProvider),
		otelgrpc.WithPropagators(defaultOTelInstances.Propagator),
	))
}

func GetGrpcClientOpts() grpc.DialOption {
	instancesLock.Lock()
	defer instancesLock.Unlock()
	if defaultOTelInstances == nil {
		return grpc.WithStatsHandler(otelgrpc.NewClientHandler(
			otelgrpc.WithTracerProvider(realotel.GetTracerProvider()),
			otelgrpc.WithMeterProvider(realotel.GetMeterProvider()),
			otelgrpc.WithPropagators(realotel.GetTextMapPropagator()),
		))
	}
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(defaultOTelInstances.TracerProvider),
		otelgrpc.WithMeterProvider(defaultOTelInstances.MeterProvider),
		otelgrpc.WithPropagators(defaultOTelInstances.Propagator),
	))
}
