package traffic

import (
	"sync"
	"time"
)

var defaultTracker Tracker

// RecordSuccess records a successful request outcome.
func RecordSuccess() {
	defaultTracker.RecordSuccess()
}

// RecordError records a failed request outcome (upstream error, timeout, etc.).
func RecordError() {
	defaultTracker.RecordError()
}

// RecordDenied records a rate-limit denial (429).
func RecordDenied() {
	defaultTracker.RecordDenied()
}

// RecordSuccessN records N successful outcomes. For synthetic load injection.
func RecordSuccessN(n int) {
	defaultTracker.RecordSuccessN(n)
}

// RecordErrorN records N error outcomes. For synthetic error injection.
func RecordErrorN(n int) {
	defaultTracker.RecordErrorN(n)
}

// RequestCount returns the number of outcomes (success + error + denied) within the window.
func RequestCount(window time.Duration) int {
	return defaultTracker.RequestCount(window)
}

// DenialCount returns the number of denials within the window.
func DenialCount(window time.Duration) int {
	return defaultTracker.DenialCount(window)
}

// ErrorRate returns (errorCount, totalCount) within the window. totalCount = successes + errors (denied excluded).
func ErrorRate(window time.Duration) (errors, total int) {
	return defaultTracker.ErrorRate(window)
}

// Reset clears all recorded outcomes. For tests only.
func Reset() {
	defaultTracker.Reset()
}

// Tracker maintains sliding windows of outcome timestamps.
// Single source of truth for overload (RequestCount, DenialCount) and degraded (ErrorRate).
type Tracker struct {
	mu           sync.Mutex
	successTimes []time.Time
	errorTimes   []time.Time
	deniedTimes  []time.Time
}

// RecordSuccess records a successful request outcome in the tracker.
func (t *Tracker) RecordSuccess() {
	t.recordOutcome(&t.successTimes)
}

// RecordError records a failed request outcome in the tracker.
func (t *Tracker) RecordError() {
	t.recordOutcome(&t.errorTimes)
}

// RecordDenied records a rate-limit denial (429) in the tracker.
func (t *Tracker) RecordDenied() {
	t.recordOutcome(&t.deniedTimes)
}

// RecordSuccessN records N successful outcomes atomically for synthetic load injection.
func (t *Tracker) RecordSuccessN(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for i := 0; i < n; i++ {
		t.successTimes = append(t.successTimes, now)
	}
	t.pruneLocked(now)
}

// RecordErrorN records N error outcomes atomically for synthetic error injection.
func (t *Tracker) RecordErrorN(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for i := 0; i < n; i++ {
		t.errorTimes = append(t.errorTimes, now)
	}
	t.pruneLocked(now)
}

// recordOutcome appends current timestamp to the specified slice and prunes old entries.
func (t *Tracker) recordOutcome(slice *[]time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	*slice = append(*slice, now)
	t.pruneLocked(now)
}

// RequestCount returns the total number of outcomes (success + error + denied) within the window.
func (t *Tracker) RequestCount(window time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)
	return t.countInWindow(t.successTimes, cutoff) +
		t.countInWindow(t.errorTimes, cutoff) +
		t.countInWindow(t.deniedTimes, cutoff)
}

// DenialCount returns the number of rate-limit denials within the window.
func (t *Tracker) DenialCount(window time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.countInWindow(t.deniedTimes, time.Now().Add(-window))
}

// ErrorRate returns (errorCount, totalCount) within the window.
// totalCount includes successes and errors only; denials are excluded from error rate calculation.
func (t *Tracker) ErrorRate(window time.Duration) (errors, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-window)
	errCount := t.countInWindow(t.errorTimes, cutoff)
	successCount := t.countInWindow(t.successTimes, cutoff)
	return errCount, errCount + successCount
}

// Reset clears all recorded outcomes from the tracker.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.successTimes = nil
	t.errorTimes = nil
	t.deniedTimes = nil
}

// countInWindow counts timestamps that are not before the cutoff time.
func (t *Tracker) countInWindow(times []time.Time, cutoff time.Time) int {
	n := 0
	for _, ts := range times {
		if !ts.Before(cutoff) {
			n++
		}
	}
	return n
}

// pruneLocked removes timestamps older than maxAge (5 minutes) from all outcome slices.
// Must be called with mutex held.
func (t *Tracker) pruneLocked(now time.Time) {
	maxAge := 5 * time.Minute
	cutoff := now.Add(-maxAge)
	prune := func(slice *[]time.Time) {
		times := *slice
		i := 0
		for ; i < len(times) && times[i].Before(cutoff); i++ {
		}
		if i > 0 {
			*slice = append(times[:0], times[i:]...)
		}
	}
	prune(&t.successTimes)
	prune(&t.errorTimes)
	prune(&t.deniedTimes)
}
