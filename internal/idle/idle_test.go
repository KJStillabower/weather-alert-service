package idle

import (
	"testing"
	"time"
)

func TestRequestCount_Empty(t *testing.T) {
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("RequestCount() = %d, want 0", n)
	}
}

func TestRecordRequest_AndCount(t *testing.T) {
	Reset()
	RecordRequest()
	RecordRequest()
	if n := RequestCount(1 * time.Minute); n != 2 {
		t.Errorf("RequestCount() = %d, want 2", n)
	}
}

func TestRequestCount_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	RecordRequest()
	if n := RequestCount(1 * time.Nanosecond); n != 0 {
		t.Errorf("RequestCount(1ns) = %d, want 0 (request outside window)", n)
	}
}

func TestReset(t *testing.T) {
	Reset()
	RecordRequest()
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("After Reset, RequestCount() = %d, want 0", n)
	}
}
