package http

import (
	"context"
	"sync"
	"time"
)

// InFlightTracker tracks the number of requests currently being served.
// Used during graceful shutdown to wait for in-flight requests to complete.
type InFlightTracker struct {
	mu    sync.RWMutex
	count int64
}

// Increment adds one to the in-flight count. Call when a request starts.
func (t *InFlightTracker) Increment() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count++
}

// Decrement subtracts one from the in-flight count. Call when a request completes.
func (t *InFlightTracker) Decrement() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count--
}

// Count returns the current in-flight count.
func (t *InFlightTracker) Count() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.count
}

// WaitForZero blocks until the in-flight count reaches zero or ctx is cancelled.
// checkInterval is how often to re-check the count.
func (t *InFlightTracker) WaitForZero(ctx context.Context, checkInterval time.Duration) error {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		if t.Count() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// globalInFlightTracker is the process-wide in-flight request counter used by MetricsMiddleware.
var globalInFlightTracker = &InFlightTracker{}

// InFlightCount returns the current number of in-flight requests.
func InFlightCount() int64 {
	return globalInFlightTracker.Count()
}

// WaitForInFlight blocks until in-flight requests reach zero or ctx is done.
// checkInterval is the interval between count checks.
func WaitForInFlight(ctx context.Context, checkInterval time.Duration) error {
	return globalInFlightTracker.WaitForZero(ctx, checkInterval)
}
