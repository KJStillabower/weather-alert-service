package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// State represents the circuit breaker state.
const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// State is the circuit breaker state (Closed, Open, HalfOpen).
type State int

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreaker protects upstream calls by opening after repeated failures
// and allowing probe requests in half-open state.
type CircuitBreaker struct {
	mu                sync.RWMutex
	state             State
	failureCount      int
	successCount      int
	lastFailureTime   time.Time
	failureThreshold  int
	successThreshold  int
	timeout           time.Duration
	component         string
	onStateChange     func(from, to State) // optional, for metrics
}

// Config holds circuit breaker parameters.
type Config struct {
	FailureThreshold  int
	SuccessThreshold  int
	Timeout           time.Duration
	Component         string
	OnStateChange     func(from, to State)
}

// New creates a new CircuitBreaker with the given config.
func New(cfg Config) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		timeout:          cfg.Timeout,
		component:        cfg.Component,
		onStateChange:    cfg.OnStateChange,
	}
}

// Call runs fn when the circuit allows it. When open, returns an error unless
// timeout has elapsed (then transitions to half-open). Records failures and
// successes to open/close the circuit.
func (cb *CircuitBreaker) Call(ctx context.Context, fn func() error) error {
	cb.mu.Lock()
	state := cb.state
	if state == StateOpen {
		if time.Since(cb.lastFailureTime) < cb.timeout {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker open")
		}
		cb.state = StateHalfOpen
		cb.successCount = 0
		prev := state
		state = StateHalfOpen
		cb.mu.Unlock()
		if cb.onStateChange != nil {
			cb.onStateChange(prev, state)
		}
	} else {
		cb.mu.Unlock()
	}

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailureTime = time.Now()
		if cb.state == StateHalfOpen || cb.failureCount >= cb.failureThreshold {
			from := cb.state
			cb.state = StateOpen
			cb.failureCount = 0
			if cb.onStateChange != nil {
				cb.onStateChange(from, StateOpen)
			}
		}
		return err
	}

	cb.successCount++
	cb.failureCount = 0
	if cb.state == StateHalfOpen && cb.successCount >= cb.successThreshold {
		from := cb.state
		cb.state = StateClosed
		cb.successCount = 0
		if cb.onStateChange != nil {
			cb.onStateChange(from, StateClosed)
		}
	}
	return nil
}

// State returns the current state (for metrics).
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}
