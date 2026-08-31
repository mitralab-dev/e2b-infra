package logger

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestStacktraceOnlyFromError(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.DebugLevel)
	l, err := NewLogger(LoggerConfig{ServiceName: "test", Cores: []zapcore.Core{core}})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}

	l.innerLogger.Warn("noisy warn")
	l.innerLogger.Error("real error", zap.String("k", "v"))

	entries := logs.All()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if s := entries[0].Entry.Stack; s != "" {
		t.Errorf("warn carried a stacktrace:\n%s", s)
	}
	if s := entries[1].Entry.Stack; !strings.Contains(s, "logger") {
		t.Errorf("error lost its stacktrace, got %q", s)
	}
}
