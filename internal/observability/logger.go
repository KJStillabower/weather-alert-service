package observability

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates a new zap logger with production configuration.
// Log level is controlled by LOG_LEVEL environment variable (DEBUG, INFO, WARN, ERROR).
// Defaults to INFO level if LOG_LEVEL is not set or invalid.
func NewLogger() (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.Level = parseLogLevel(os.Getenv("LOG_LEVEL"))

	return config.Build()
}

// parseLogLevel parses log level string from environment variable.
// Returns DEBUG, WARN, or ERROR if matched (case-insensitive), otherwise INFO.
func parseLogLevel(s string) zap.AtomicLevel {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return zap.NewAtomicLevelAt(zap.DebugLevel)
	case "WARN":
		return zap.NewAtomicLevelAt(zap.WarnLevel)
	case "ERROR":
		return zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		return zap.NewAtomicLevelAt(zap.InfoLevel)
	}
}
