package overload

import (
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/traffic"
)

// TestRequestCount_Empty verifies that RequestCount returns 0 when no
// requests have been recorded within the time window.
func TestRequestCount_Empty(t *testing.T) {
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("RequestCount() = %d, want 0", n)
	}
}

// TestRecordDenial_AndRequestCount verifies that traffic.RecordSuccess correctly
// increments request count tracked by RequestCount.
func TestRecordDenial_AndRequestCount(t *testing.T) {
	Reset()
	traffic.RecordSuccess()
	traffic.RecordSuccess()
	if n := RequestCount(1 * time.Minute); n != 2 {
		t.Errorf("RequestCount() = %d, want 2", n)
	}
}

// TestRequestCount_ExpiresOutsideWindow verifies that RequestCount excludes
// requests that occurred outside the specified time window.
func TestRequestCount_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	traffic.RecordSuccess()
	if n := RequestCount(1 * time.Nanosecond); n != 0 {
		t.Errorf("RequestCount(1ns) = %d, want 0 (request outside window)", n)
	}
}

// TestReset verifies that Reset clears all recorded request counts,
// resetting tracking to zero.
func TestReset(t *testing.T) {
	Reset()
	traffic.RecordSuccess()
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("After Reset, RequestCount() = %d, want 0", n)
	}
}

// TestRecordDenial_AndCount verifies that RecordDenial correctly increments
// denial count tracked by DenialCount.
func TestRecordDenial_AndCount(t *testing.T) {
	Reset()
	RecordDenial()
	RecordDenial()
	if n := DenialCount(1 * time.Minute); n != 2 {
		t.Errorf("DenialCount() = %d, want 2", n)
	}
}

// TestDenialCount_ExpiresOutsideWindow verifies that DenialCount excludes
// denials that occurred outside the specified time window.
func TestDenialCount_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	RecordDenial()
	if n := DenialCount(1 * time.Nanosecond); n != 0 {
		t.Errorf("DenialCount(1ns) = %d, want 0 (denial outside window)", n)
	}
}

// TestReset_ClearsBoth verifies that Reset clears both request counts
// and denial counts simultaneously.
func TestReset_ClearsBoth(t *testing.T) {
	Reset()
	traffic.RecordSuccess()
	RecordDenial()
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("After Reset, RequestCount() = %d, want 0", n)
	}
	if n := DenialCount(1 * time.Minute); n != 0 {
		t.Errorf("After Reset, DenialCount() = %d, want 0", n)
	}
}
