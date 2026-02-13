package observability

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestParseLogLevel verifies that parseLogLevel correctly parses log level
// strings from environment variables, handling case-insensitivity and whitespace.
func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		env    string
		expect zapcore.Level
	}{
		{"", zap.InfoLevel},
		{"INFO", zap.InfoLevel},
		{"DEBUG", zap.DebugLevel},
		{"WARN", zap.WarnLevel},
		{"ERROR", zap.ErrorLevel},
		{"debug", zap.DebugLevel},
		{"  warn  ", zap.WarnLevel},
		{"invalid", zap.InfoLevel},
	}
	for _, tt := range tests {
		level := parseLogLevel(tt.env)
		if got := level.Level(); got != tt.expect {
			t.Errorf("parseLogLevel(%q) = %v, want %v", tt.env, got, tt.expect)
		}
	}
}

// TestNewLogger verifies that NewLogger creates a valid logger instance
// that can be used for logging operations.
func TestNewLogger(t *testing.T) {
	logger, err := NewLogger()
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	if logger == nil {
		t.Fatal("NewLogger() returned nil logger")
	}

	logger.Info("test message")
	_ = logger.Sync() // best-effort; can fail on /dev/stderr in test env
}
