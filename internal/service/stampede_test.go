package service

import (
	"sync"
	"testing"
)

// TestStampedeTracker_RecordMiss_RecordHit verifies that RecordMiss increments and returns
// the concurrent count per key and that RecordHit decrements correctly until the key is removed.
func TestStampedeTracker_RecordMiss_RecordHit(t *testing.T) {
	st := newStampedeTracker()
	key := "seattle"

	// First miss: count 1
	if got := st.RecordMiss(key); got != 1 {
		t.Errorf("RecordMiss first = %d, want 1", got)
	}
	// Second concurrent miss: count 2
	if got := st.RecordMiss(key); got != 2 {
		t.Errorf("RecordMiss second = %d, want 2", got)
	}

	// Complete one miss
	st.RecordHit(key)
	if got := st.RecordMiss(key); got != 2 {
		t.Errorf("after one hit, RecordMiss = %d, want 2", got)
	}
	st.RecordHit(key)
	st.RecordHit(key)
	// All cleared; next miss is 1
	if got := st.RecordMiss(key); got != 1 {
		t.Errorf("after all hit, RecordMiss = %d, want 1", got)
	}
	st.RecordHit(key)
}

// TestStampedeTracker_Concurrent verifies that concurrent RecordMiss/RecordHit calls
// do not race and leave the tracker in a consistent state.
func TestStampedeTracker_Concurrent(t *testing.T) {
	st := newStampedeTracker()
	key := "london"
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st.RecordMiss(key)
			st.RecordHit(key)
		}()
	}
	wg.Wait()
	// All hits should have run; count for key should be 0 (no active misses)
	st.RecordHit(key)
	if got := st.RecordMiss(key); got != 1 {
		t.Errorf("after concurrent ops RecordMiss = %d, want 1", got)
	}
	st.RecordHit(key)
}
