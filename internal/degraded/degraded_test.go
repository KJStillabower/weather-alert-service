package degraded

import (
	"testing"
	"time"
)

func TestErrorRate_Empty(t *testing.T) {
	Reset()
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 0 || total != 0 {
		t.Errorf("ErrorRate() = (%d, %d), want (0, 0)", errors, total)
	}
}

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

func TestErrorRate_ExpiresOutsideWindow(t *testing.T) {
	Reset()
	RecordError()
	RecordSuccess()
	errors, total := ErrorRate(1 * time.Nanosecond)
	if errors != 0 || total != 0 {
		t.Errorf("ErrorRate(1ns) = (%d, %d), want (0, 0)", errors, total)
	}
}

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
