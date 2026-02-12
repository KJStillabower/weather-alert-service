package idle

import (
	"sync"
	"time"
)

var defaultTracker Tracker

// RecordRequest records a request (e.g. weather query). Call from handlers for traffic that counts toward idle detection.
func RecordRequest() {
	defaultTracker.RecordRequest()
}

// RequestCount returns the number of requests within the given window ending at now.
func RequestCount(window time.Duration) int {
	return defaultTracker.RequestCount(window)
}

// Reset clears all recorded requests. For tests only.
func Reset() {
	defaultTracker.Reset()
}

// Tracker maintains a sliding window of request timestamps.
type Tracker struct {
	mu    sync.Mutex
	times []time.Time
}

// RecordRequest records a request at the current time.
func (t *Tracker) RecordRequest() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.times = append(t.times, now)
	t.pruneLocked(now)
}

// RequestCount returns the number of requests within the given window ending at now.
func (t *Tracker) RequestCount(window time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)
	t.pruneLocked(now)
	n := 0
	for _, ts := range t.times {
		if !ts.Before(cutoff) {
			n++
		}
	}
	return n
}

// Reset clears all recorded requests.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.times = nil
}

func (t *Tracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-30 * time.Minute)
	i := 0
	for ; i < len(t.times) && t.times[i].Before(cutoff); i++ {
	}
	if i > 0 {
		t.times = append(t.times[:0], t.times[i:]...)
	}
}
