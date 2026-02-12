package traffic

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

func TestRecordSuccess_AndRequestCount(t *testing.T) {
	Reset()
	RecordSuccess()
	RecordSuccess()
	if n := RequestCount(1 * time.Minute); n != 2 {
		t.Errorf("RequestCount() = %d, want 2", n)
	}
}

func TestRecordDenied_AndCounts(t *testing.T) {
	Reset()
	RecordDenied()
	RecordDenied()
	if n := DenialCount(1 * time.Minute); n != 2 {
		t.Errorf("DenialCount() = %d, want 2", n)
	}
	if n := RequestCount(1 * time.Minute); n != 2 {
		t.Errorf("RequestCount() = %d, want 2", n)
	}
}

func TestErrorRate_SuccessAndError(t *testing.T) {
	Reset()
	RecordSuccess()
	RecordSuccess()
	RecordError()
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 1 || total != 3 {
		t.Errorf("ErrorRate() = (%d, %d), want (1, 3)", errors, total)
	}
}

func TestErrorRate_DeniedExcluded(t *testing.T) {
	Reset()
	RecordSuccess()
	RecordDenied()
	RecordDenied()
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 0 || total != 1 {
		t.Errorf("ErrorRate() = (%d, %d), want (0, 1) - denied excluded from error rate", errors, total)
	}
}

func TestLoadAndError_UnifiedDenominator(t *testing.T) {
	Reset()
	RecordSuccessN(39)
	RecordErrorN(1)
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 1 || total != 40 {
		t.Errorf("ErrorRate() = (%d, %d), want (1, 40) - load 39 + error 1 = 2.5%%", errors, total)
	}
	if n := RequestCount(1 * time.Minute); n != 40 {
		t.Errorf("RequestCount() = %d, want 40", n)
	}
}

func TestReset(t *testing.T) {
	Reset()
	RecordSuccess()
	RecordError()
	RecordDenied()
	Reset()
	if n := RequestCount(1 * time.Minute); n != 0 {
		t.Errorf("RequestCount() = %d, want 0", n)
	}
	errors, total := ErrorRate(1 * time.Minute)
	if errors != 0 || total != 0 {
		t.Errorf("ErrorRate() = (%d, %d), want (0, 0)", errors, total)
	}
}
