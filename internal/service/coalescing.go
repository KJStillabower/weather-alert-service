package service

import (
	"context"
	"sync"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

// inFlightRequest tracks a single upstream request that multiple callers may wait for.
type inFlightRequest struct {
	mu      sync.Mutex
	result  models.WeatherData
	err     error
	done    bool
	waiters []chan struct{} // Channels to notify waiters when result is ready
}

// requestCoalescer prevents cache stampede by coalescing concurrent requests for the same key.
type requestCoalescer struct {
	mu       sync.Mutex
	inFlight map[string]*inFlightRequest
	timeout  time.Duration
}

// newRequestCoalescer creates a new requestCoalescer with the specified timeout.
func newRequestCoalescer(timeout time.Duration) *requestCoalescer {
	return &requestCoalescer{
		inFlight: make(map[string]*inFlightRequest),
		timeout:  timeout,
	}
}

// GetOrDo checks if a request for key is already in-flight. If yes, waits for its result.
// If no, executes fn and registers the request. Returns the result or error.
// Respects context cancellation and timeout to prevent indefinite blocking.
func (rc *requestCoalescer) GetOrDo(ctx context.Context, key string, fn func() (models.WeatherData, error)) (models.WeatherData, error) {
	rc.mu.Lock()
	req, exists := rc.inFlight[key]
	if exists {
		// Request in-flight - wait for it
		notify := make(chan struct{})
		req.mu.Lock()
		if req.done {
			// Already completed
			result := req.result
			err := req.err
			req.mu.Unlock()
			rc.mu.Unlock()
			if err != nil {
				return models.WeatherData{}, err
			}
			return result, nil
		}
		req.waiters = append(req.waiters, notify)
		req.mu.Unlock()
		rc.mu.Unlock()

		// Wait for notification or timeout
		waitCtx, cancel := context.WithTimeout(ctx, rc.timeout)
		defer cancel()
		select {
		case <-notify:
			req.mu.Lock()
			result := req.result
			err := req.err
			req.mu.Unlock()
			if err != nil {
				return models.WeatherData{}, err
			}
			return result, nil
		case <-waitCtx.Done():
			return models.WeatherData{}, waitCtx.Err()
		}
	}

	// No existing request - create one
	req = &inFlightRequest{
		waiters: make([]chan struct{}, 0),
	}
	rc.inFlight[key] = req
	rc.mu.Unlock()

	// Execute the request in goroutine
	go func() {
		result, err := fn()

		req.mu.Lock()
		req.result = result
		req.err = err
		req.done = true
		waiters := req.waiters
		req.waiters = nil
		req.mu.Unlock()

		// Notify all waiters
		for _, notify := range waiters {
			close(notify)
		}

		rc.cleanup(key)
	}()

	// Wait for result with timeout
	waitCtx, cancel := context.WithTimeout(ctx, rc.timeout)
	defer cancel()
	notify := make(chan struct{})
	req.mu.Lock()
	if req.done {
		// Completed already
		result := req.result
		err := req.err
		req.mu.Unlock()
		cancel()
		if err != nil {
			return models.WeatherData{}, err
		}
		return result, nil
	}
	req.waiters = append(req.waiters, notify)
	req.mu.Unlock()

	select {
	case <-notify:
		req.mu.Lock()
		result := req.result
		err := req.err
		req.mu.Unlock()
		if err != nil {
			return models.WeatherData{}, err
		}
		return result, nil
	case <-waitCtx.Done():
		return models.WeatherData{}, waitCtx.Err()
	}
}

// cleanup removes the in-flight request for key. Must be called after request completes.
func (rc *requestCoalescer) cleanup(key string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	delete(rc.inFlight, key)
}
