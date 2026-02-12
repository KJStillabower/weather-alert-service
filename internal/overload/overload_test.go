package overload

import (
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/traffic"
)

func TestRequestCount_Empty(t *testing.T) {
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("RequestCount() = %d, want 0", n)
	}
}

func TestRecordDenial_AndRequestCount(t *testing.T) {
	Reset()
	traffic.RecordSuccess()
	traffic.RecordSuccess()
	if n := RequestCount(1 * time.Minute); n != 2 {
		t.Errorf("RequestCount() = %d, want 2", n)
	}
}

func TestRequestCount_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	traffic.RecordSuccess()
	if n := RequestCount(1 * time.Nanosecond); n != 0 {
		t.Errorf("RequestCount(1ns) = %d, want 0 (request outside window)", n)
	}
}

func TestReset(t *testing.T) {
	Reset()
	traffic.RecordSuccess()
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("After Reset, RequestCount() = %d, want 0", n)
	}
}

func TestRecordDenial_AndCount(t *testing.T) {
	Reset()
	RecordDenial()
	RecordDenial()
	if n := DenialCount(1 * time.Minute); n != 2 {
		t.Errorf("DenialCount() = %d, want 2", n)
	}
}

func TestDenialCount_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	RecordDenial()
	if n := DenialCount(1 * time.Nanosecond); n != 0 {
		t.Errorf("DenialCount(1ns) = %d, want 0 (denial outside window)", n)
	}
}

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
