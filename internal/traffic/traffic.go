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

func (t *Tracker) RecordSuccess() {
	t.recordOutcome(&t.successTimes)
}

func (t *Tracker) RecordError() {
	t.recordOutcome(&t.errorTimes)
}

func (t *Tracker) RecordDenied() {
	t.recordOutcome(&t.deniedTimes)
}

func (t *Tracker) RecordSuccessN(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for i := 0; i < n; i++ {
		t.successTimes = append(t.successTimes, now)
	}
	t.pruneLocked(now)
}

func (t *Tracker) RecordErrorN(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for i := 0; i < n; i++ {
		t.errorTimes = append(t.errorTimes, now)
	}
	t.pruneLocked(now)
}

func (t *Tracker) recordOutcome(slice *[]time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	*slice = append(*slice, now)
	t.pruneLocked(now)
}

func (t *Tracker) RequestCount(window time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)
	return t.countInWindow(t.successTimes, cutoff) +
		t.countInWindow(t.errorTimes, cutoff) +
		t.countInWindow(t.deniedTimes, cutoff)
}

func (t *Tracker) DenialCount(window time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.countInWindow(t.deniedTimes, time.Now().Add(-window))
}

func (t *Tracker) ErrorRate(window time.Duration) (errors, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-window)
	errCount := t.countInWindow(t.errorTimes, cutoff)
	successCount := t.countInWindow(t.successTimes, cutoff)
	return errCount, errCount + successCount
}

func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.successTimes = nil
	t.errorTimes = nil
	t.deniedTimes = nil
}

func (t *Tracker) countInWindow(times []time.Time, cutoff time.Time) int {
	n := 0
	for _, ts := range times {
		if !ts.Before(cutoff) {
			n++
		}
	}
	return n
}

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
