package service

import (
	"sync"
)

// stampedeTracker tracks concurrent cache misses per key to detect cache stampede.
// RecordMiss increments and returns the count for the key; RecordHit decrements.
// When multiple requests miss the same key simultaneously, concurrent count exceeds 1.
type stampedeTracker struct {
	mu           sync.Mutex         // protects activeMisses
	activeMisses map[string]int     // key -> number of concurrent misses in progress
}

// newStampedeTracker returns a new stampedeTracker.
func newStampedeTracker() *stampedeTracker {
	return &stampedeTracker{
		activeMisses: make(map[string]int),
	}
}

// RecordMiss records a cache miss for key and returns the concurrent miss count after incrementing.
// Caller should defer RecordHit(key) when the miss is resolved (upstream fetch completed).
func (st *stampedeTracker) RecordMiss(key string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.activeMisses[key]++
	return st.activeMisses[key]
}

// RecordHit records completion of a miss for key (decrements concurrent count).
func (st *stampedeTracker) RecordHit(key string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if count, ok := st.activeMisses[key]; ok && count > 0 {
		st.activeMisses[key]--
		if st.activeMisses[key] == 0 {
			delete(st.activeMisses, key)
		}
	}
}
