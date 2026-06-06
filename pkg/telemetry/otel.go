package telemetry

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	// shutdownTimeout is the maximum time to wait for the tracer provider to flush
	// and export pending spans on shutdown.
	shutdownTimeout = 5 * time.Second
)

// ShutdownFunc flushes any pending spans and releases provider resources.
// Safe to call multiple times; subsequent calls are no-ops.
type ShutdownFunc func(context.Context) error

// Setup configures a global OTel tracer provider backed by the OTLP HTTP
// exporter when OTEL_EXPORTER_OTLP_ENDPOINT is set. Returns a no-op provider
// and a no-op shutdown function when the env var is absent.
//
// The returned ShutdownFunc must be called on server exit to flush pending spans.
func Setup(ctx context.Context, serviceName, serviceVersion string) (trace.TracerProvider, ShutdownFunc, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		nop := noop.NewTracerProvider()
		otel.SetTracerProvider(nop)
		return nop, func(_ context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", serviceVersion),
		),
		resource.WithProcess(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("building OTel resource: %w", err)
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("creating OTLP HTTP trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		return tp.Shutdown(ctx)
	}

	return tp, shutdown, nil
}

// Tracer returns a named tracer from the global provider.
// All klaus packages should obtain their tracer via this function so the
// provider can be swapped in tests.
func Tracer(name string) trace.Tracer {
	return otel.GetTracerProvider().Tracer(name)
}
