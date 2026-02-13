package idle

import (
	"testing"
	"time"
)

// TestRequestCount_Empty verifies that RequestCount returns 0 when no
// requests have been recorded within the time window.
func TestRequestCount_Empty(t *testing.T) {
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("RequestCount() = %d, want 0", n)
	}
}

// TestRecordRequest_AndCount verifies that RecordRequest correctly increments
// request count tracked by RequestCount.
func TestRecordRequest_AndCount(t *testing.T) {
	Reset()
	RecordRequest()
	RecordRequest()
	if n := RequestCount(1 * time.Minute); n != 2 {
		t.Errorf("RequestCount() = %d, want 2", n)
	}
}

// TestRequestCount_ExpiresOutsideWindow verifies that RequestCount excludes
// requests that occurred outside the specified time window.
func TestRequestCount_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	RecordRequest()
	if n := RequestCount(1 * time.Nanosecond); n != 0 {
		t.Errorf("RequestCount(1ns) = %d, want 0 (request outside window)", n)
	}
}

// TestReset verifies that Reset clears all recorded request counts,
// resetting tracking to zero.
func TestReset(t *testing.T) {
	Reset()
	RecordRequest()
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("After Reset, RequestCount() = %d, want 0", n)
	}
}
