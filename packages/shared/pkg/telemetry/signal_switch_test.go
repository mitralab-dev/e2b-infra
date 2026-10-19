package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// NewWithOptions installs its providers as the otel globals, so the test
// restores them, along with the package switches it flips.
//
//nolint:paralleltest,tparallel // mutates package switches and otel globals
func TestNewWithOptionsSignalSwitches(t *testing.T) {
	tests := []struct {
		name          string
		logsEnabled   bool
		tracesEnabled bool
	}{
		{name: "logs off", logsEnabled: false, tracesEnabled: true},
		{name: "traces off", logsEnabled: true, tracesEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreSignalGlobals(t)
			// Nothing listens here; the exporters dial lazily, so creating them never connects.
			otelCollectorGRPCEndpoint = "127.0.0.1:1"
			otelLogsEnabled = tt.logsEnabled
			otelTracesEnabled = tt.tracesEnabled

			client, err := NewWithOptions(t.Context(), "node", "test", "commit", "dev", "instance")
			require.NoError(t, err)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 100*time.Millisecond)
				defer cancel()
				_ = client.Shutdown(ctx)
			})

			_, logsNoop := client.LogsProvider.(noopLogProvider)
			_, tracesNoop := client.SpanExporter.(*noopSpanExporter)
			assert.Equal(t, !tt.logsEnabled, logsNoop, "logs provider is noop")
			assert.Equal(t, !tt.tracesEnabled, tracesNoop, "span exporter is noop")
		})
	}
}

func restoreSignalGlobals(t *testing.T) {
	t.Helper()

	endpoint, logs, traces := otelCollectorGRPCEndpoint, otelLogsEnabled, otelTracesEnabled
	meterProvider, tracerProvider, propagator := otel.GetMeterProvider(), otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otelCollectorGRPCEndpoint, otelLogsEnabled, otelTracesEnabled = endpoint, logs, traces
		otel.SetMeterProvider(meterProvider)
		otel.SetTracerProvider(tracerProvider)
		otel.SetTextMapPropagator(propagator)
	})
}
