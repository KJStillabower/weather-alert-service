package observability

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// FlushTelemetry flushes telemetry buffers before process exit.
// For pull-based Prometheus, metrics are already exposed; this mainly flushes logs.
// Call during graceful shutdown after in-flight requests have drained.
func FlushTelemetry(ctx context.Context, logger *zap.Logger) error {
	if logger != nil {
		if err := logger.Sync(); err != nil {
			return fmt.Errorf("flush logs: %w", err)
		}
	}
	return nil
}
