package telemetry_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/giantswarm/klaus/pkg/telemetry"
)

func TestSetup_NoEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	tp, shutdown, err := telemetry.Setup(t.Context(), "test-service", "0.0.0")
	require.NoError(t, err)
	require.NotNil(t, tp)
	require.NotNil(t, shutdown)

	// Without an endpoint the provider must be the no-op implementation.
	_, ok := tp.(noop.TracerProvider)
	require.True(t, ok, "expected no-op TracerProvider when OTEL_EXPORTER_OTLP_ENDPOINT is unset")

	require.NoError(t, shutdown(t.Context()))
}

func TestSetup_ShutdownIdempotent(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	_, shutdown, err := telemetry.Setup(t.Context(), "test-service", "0.0.0")
	require.NoError(t, err)

	require.NoError(t, shutdown(t.Context()))
	require.NoError(t, shutdown(t.Context()), "second shutdown call must not error")
}

func TestTracer(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	_, _, err := telemetry.Setup(t.Context(), "test-service", "0.0.0")
	require.NoError(t, err)

	tracer := telemetry.Tracer("test")
	require.NotNil(t, tracer)

	ctx, span := tracer.Start(t.Context(), "test-span")
	require.NotNil(t, ctx)
	span.End()
}
