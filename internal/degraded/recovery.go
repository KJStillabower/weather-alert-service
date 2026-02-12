package degraded

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

var (
	recoveryChan   chan struct{}
	recoveryChanMu sync.Mutex

	// Test-only overrides; used when testing_mode is true.
	recoveryDisabled   atomic.Bool
	forceFailNext      atomic.Bool
	forceSucceedNext  atomic.Bool
	recoveryAttemptIdx atomic.Uint32 // Tracks simulated fail_clear count for next_recovery display
)

// SetRecoveryDisabled disables auto-recovery when true. RunRecovery returns immediately.
// Only intended for testing_mode. Cleared by ClearRecoveryOverrides.
func SetRecoveryDisabled(v bool) {
	recoveryDisabled.Store(v)
}

// IsRecoveryDisabled returns true when auto-recovery is disabled.
func IsRecoveryDisabled() bool {
	return recoveryDisabled.Load()
}

// SetForceFailNextAttempt makes the next recovery validate call simulate failure.
// Only intended for testing_mode. Single-use; cleared after consumed.
func SetForceFailNextAttempt(v bool) {
	forceFailNext.Store(v)
}

// SetForceSucceedNextAttempt makes the next recovery attempt succeed immediately and resets the degraded tracker.
// Only intended for testing_mode. Single-use; cleared after consumed.
func SetForceSucceedNextAttempt(v bool) {
	forceSucceedNext.Store(v)
}

// ClearRecoveryOverrides clears all test-only recovery overrides.
func ClearRecoveryOverrides() {
	recoveryDisabled.Store(false)
	forceFailNext.Store(false)
	forceSucceedNext.Store(false)
	recoveryAttemptIdx.Store(0)
}

// NotifyDegraded signals that the service is degraded. Triggers recovery if not already running.
// Safe to call from handlers; non-blocking.
func NotifyDegraded() {
	recoveryChanMu.Lock()
	ch := recoveryChan
	recoveryChanMu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// StartRecoveryListener starts a goroutine that runs recovery when NotifyDegraded is called.
// Call from main with the app context. validate should run API key check and optionally a test weather call.
func StartRecoveryListener(ctx context.Context, validate ValidateFunc, initial, max time.Duration, onExhausted func()) {
	ch := make(chan struct{}, 1)
	recoveryChanMu.Lock()
	recoveryChan = ch
	recoveryChanMu.Unlock()

	var running atomic.Bool
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				if running.Swap(true) {
					continue
				}
				go func() {
					defer running.Store(false)
					RunRecovery(ctx, validate, initial, max, onExhausted)
				}()
			}
		}
	}()
}

// ValidateFunc runs the startup validation (API key, optional test call). Returns nil if recovered.
type ValidateFunc func(ctx context.Context) error

// RunRecovery runs the Fibonacci backoff recovery. Calls validate at each interval.
// Delays: 1m, 2m, 3m, 5m, 8m, 13m (Fibonacci from initial). Stops when validate returns nil (recovered).
// After the final attempt, if validate still fails, calls onExhausted.
// Respects test-only overrides: recoveryDisabled (skip entirely), forceSucceedNext (immediate success),
// forceFailNext (simulate validation failure).
func RunRecovery(ctx context.Context, validate ValidateFunc, initial, max time.Duration, onExhausted func()) {
	if recoveryDisabled.Load() {
		return
	}
	if initial <= 0 || max < initial {
		return
	}
	delays := fibDelays(initial, max)
	timeout := 10 * time.Second
	for i, d := range delays {
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
		if recoveryDisabled.Load() {
			return
		}
		if forceSucceedNext.Swap(false) {
			Reset()
			return
		}
		if forceFailNext.Swap(false) {
			if i == len(delays)-1 {
				onExhausted()
				return
			}
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		err := validate(attemptCtx)
		cancel()
		if err == nil {
			Reset()
			return
		}
		if i == len(delays)-1 {
			onExhausted()
			return
		}
	}
}

// GetAndAdvanceNextRecoveryDelay returns the delay for the current simulated failure attempt,
// then advances the attempt index for the next fail_clear. Used by fail_clear to display
// the Fibonacci clock (1m, 2m, 3m, 5m, 8m, 13m...). Returns (0, false) when sequence exhausted.
func GetAndAdvanceNextRecoveryDelay(initial, max time.Duration) (time.Duration, bool) {
	delays := fibDelays(initial, max)
	if len(delays) == 0 {
		return 0, false
	}
	idx := recoveryAttemptIdx.Add(1) - 1 // increment and get previous value for this call
	if int(idx) >= len(delays) {
		return 0, false
	}
	return delays[idx], true
}

func fibDelays(initial, max time.Duration) []time.Duration {
	const (
		f0 = 1
		f1 = 2
	)
	a, b := float64(f0), float64(f1)
	unit := initial.Seconds() / float64(f0)
	var out []time.Duration
	for {
		d := time.Duration(a*unit) * time.Second
		if d > max {
			break
		}
		out = append(out, d)
		a, b = b, a+b
	}
	return out
}
