package sbxlogger

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log/noop"
)

//nolint:paralleltest // the console sink binds to os.Stdout when the logger is built, so the test swaps the process-wide stdout
func TestInternalLoggerConsoleRespectsIsDebug(t *testing.T) {
	tests := []struct {
		name    string
		isDebug bool
	}{
		{name: "debug disabled", isDebug: false},
		{name: "debug enabled", isDebug: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStdout(t, func() {
				lg := NewLogger(t.Context(), noop.NewLoggerProvider(), SandboxLoggerConfig{
					ServiceName: "test",
					IsInternal:  true,
					IsDebug:     tt.isDebug,
				})
				lg.Debug(t.Context(), "debug line")
				lg.Info(t.Context(), "info line")
				_ = lg.Sync()
			})

			assert.Contains(t, output, "info line")
			assert.Equal(t, tt.isDebug, strings.Contains(output, "debug line"), "console output: %q", output)
		})
	}
}

func TestExternalLoggerKeepsDebugWithoutIsDebug(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		bodies []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	lg := NewLogger(t.Context(), noop.NewLoggerProvider(), SandboxLoggerConfig{
		ServiceName:      "test",
		IsInternal:       false,
		CollectorAddress: server.URL,
	})
	lg.Debug(t.Context(), "debug line")
	_ = lg.Sync()

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, strings.Join(bodies, "\n"), "debug line")
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	fn()

	require.NoError(t, w.Close())
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	return string(output)
}
