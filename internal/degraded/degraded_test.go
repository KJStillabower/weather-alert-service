package degraded

import (
	"testing"
	"time"
)

// TestErrorRate_Empty verifies that ErrorRate returns (0, 0) when no
// events have been recorded within the time window.
func TestErrorRate_Empty(t *testing.T) {
	Reset()
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 0 || total != 0 {
		t.Errorf("ErrorRate() = (%d, %d), want (0, 0)", errors, total)
	}
}

// TestRecordSuccess_AndError_ErrorRate verifies that RecordSuccess and RecordError
// correctly track events and ErrorRate returns accurate counts.
func TestRecordSuccess_AndError_ErrorRate(t *testing.T) {
	Reset()
	RecordSuccess()
	RecordSuccess()
	RecordError()
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 1 || total != 3 {
		t.Errorf("ErrorRate() = (%d, %d), want (1, 3)", errors, total)
	}
}

// TestErrorRate_ExpiresOutsideWindow verifies that ErrorRate excludes events
// that occurred outside the specified time window.
func TestErrorRate_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	RecordError()
	RecordSuccess()
	errors, total := ErrorRate(1 * time.Nanosecond)
	if errors != 0 || total != 0 {
		t.Errorf("ErrorRate(1ns) = (%d, %d), want (0, 0)", errors, total)
	}
}

// TestReset verifies that Reset clears all recorded error and success events,
// resetting error rate tracking to zero.
func TestReset(t *testing.T) {
	Reset()
	RecordError()
	RecordSuccess()
	Reset()
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 0 || total != 0 {
		t.Errorf("After Reset, ErrorRate() = (%d, %d), want (0, 0)", errors, total)
	}
}
