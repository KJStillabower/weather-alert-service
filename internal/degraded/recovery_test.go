package degraded

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestFibDelays verifies that fibDelays generates Fibonacci sequence delays
// up to the maximum delay value.
func TestFibDelays(t *testing.T) {
	delays := fibDelays(1*time.Minute, 13*time.Minute)
	want := []time.Duration{1, 2, 3, 5, 8, 13}
	if len(delays) != len(want) {
		t.Fatalf("len(delays) = %d, want %d", len(delays), len(want))
	}
	for i, w := range want {
		expected := time.Duration(w) * time.Minute
		if delays[i] != expected {
			t.Errorf("delays[%d] = %v, want %v", i, delays[i], expected)
		}
	}
}

// TestFibDelays_CapsAtMax verifies that fibDelays caps the last delay
// at the maximum value rather than exceeding it.
func TestFibDelays_CapsAtMax(t *testing.T) {
	delays := fibDelays(1*time.Minute, 5*time.Minute)
	if len(delays) < 2 {
		t.Fatalf("expected at least 2 delays")
	}
	last := delays[len(delays)-1]
	if last != 5*time.Minute {
		t.Errorf("last delay = %v, want 5m", last)
	}
}

// TestRunRecovery_Recovers verifies that RunRecovery successfully recovers
// when validation eventually succeeds after retries.
func TestRunRecovery_Recovers(t *testing.T) {
	attempts := atomic.Int32{}
	validate := func(ctx context.Context) error {
		if attempts.Add(1) >= 2 {
			return nil
		}
		return errors.New("fail")
	}
	exhausted := atomic.Bool{}
	ctx := context.Background()
	RunRecovery(ctx, validate, 10*time.Millisecond, 100*time.Millisecond, func() {
		exhausted.Store(true)
	})
	if exhausted.Load() {
		t.Error("onExhausted should not have been called")
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

// TestRunRecovery_Exhausted verifies that RunRecovery calls onExhausted callback
// when all retry attempts fail within the max elapsed time.
func TestRunRecovery_Exhausted(t *testing.T) {
	validate := func(ctx context.Context) error {
		return errors.New("always fail")
	}
	exhausted := atomic.Bool{}
	ctx := context.Background()
	RunRecovery(ctx, validate, 10*time.Millisecond, 50*time.Millisecond, func() {
		exhausted.Store(true)
	})
	if !exhausted.Load() {
		t.Error("onExhausted should have been called")
	}
}

// TestSetRecoveryDisabled_IsRecoveryDisabled verifies that recovery disabled
// state can be set and queried correctly.
func TestSetRecoveryDisabled_IsRecoveryDisabled(t *testing.T) {
	defer ClearRecoveryOverrides()

	SetRecoveryDisabled(true)
	if !IsRecoveryDisabled() {
		t.Error("IsRecoveryDisabled() = false, want true")
	}

	SetRecoveryDisabled(false)
	if IsRecoveryDisabled() {
		t.Error("IsRecoveryDisabled() = true, want false")
	}
}

// TestClearRecoveryOverrides verifies that ClearRecoveryOverrides resets
// all recovery override flags to their default state.
func TestClearRecoveryOverrides(t *testing.T) {
	SetRecoveryDisabled(true)
	SetForceFailNextAttempt(true)
	SetForceSucceedNextAttempt(true)

	ClearRecoveryOverrides()

	if IsRecoveryDisabled() {
		t.Error("ClearRecoveryOverrides did not clear recoveryDisabled")
	}
}

// TestSetForceFailNextAttempt_SetForceSucceedNextAttempt verifies that force
// flags allow test control over recovery behavior without executing validation.
func TestSetForceFailNextAttempt_SetForceSucceedNextAttempt(t *testing.T) {
	defer ClearRecoveryOverrides()

	t.Run("forceSucceedNext", func(t *testing.T) {
		ClearRecoveryOverrides()
		validateCalled := atomic.Bool{}
		validate := func(ctx context.Context) error {
			validateCalled.Store(true)
			return errors.New("would fail")
		}
		exhausted := atomic.Bool{}
		SetForceSucceedNextAttempt(true)
		RunRecovery(context.Background(), validate, 1*time.Millisecond, 100*time.Millisecond, func() {
			exhausted.Store(true)
		})
		if validateCalled.Load() {
			t.Error("forceSucceedNext should skip validate")
		}
		if exhausted.Load() {
			t.Error("forceSucceedNext should not call onExhausted")
		}
	})

	t.Run("forceFailNext_callsOnExhausted", func(t *testing.T) {
		ClearRecoveryOverrides()
		validate := func(ctx context.Context) error {
			return errors.New("fail")
		}
		exhausted := atomic.Bool{}
		SetForceFailNextAttempt(true)
		RunRecovery(context.Background(), validate, 1*time.Millisecond, 5*time.Millisecond, func() {
			exhausted.Store(true)
		})
		if !exhausted.Load() {
			t.Error("forceFailNext should eventually exhaust and call onExhausted")
		}
	})
}

// TestRunRecovery_RecoveryDisabled verifies that RunRecovery returns immediately
// without calling validate when recovery is disabled.
func TestRunRecovery_RecoveryDisabled(t *testing.T) {
	defer ClearRecoveryOverrides()

	SetRecoveryDisabled(true)
	validateCalled := atomic.Bool{}
	validate := func(ctx context.Context) error {
		validateCalled.Store(true)
		return nil
	}
	RunRecovery(context.Background(), validate, 1*time.Millisecond, 100*time.Millisecond, func() {})

	if validateCalled.Load() {
		t.Error("RunRecovery should return immediately when recoveryDisabled, without calling validate")
	}
}

// TestGetAndAdvanceNextRecoveryDelay verifies that GetAndAdvanceNextRecoveryDelay
// returns Fibonacci sequence delays and signals exhaustion when sequence completes.
func TestGetAndAdvanceNextRecoveryDelay(t *testing.T) {
	defer ClearRecoveryOverrides()

	ClearRecoveryOverrides()
	initial := 1 * time.Minute
	max := 13 * time.Minute
	want := []time.Duration{1, 2, 3, 5, 8, 13}

	for i, w := range want {
		d, ok := GetAndAdvanceNextRecoveryDelay(initial, max)
		if !ok {
			t.Fatalf("call %d: got ok=false, want true", i+1)
		}
		expected := w * time.Minute
		if d != expected {
			t.Errorf("call %d: delay = %v, want %v", i+1, d, expected)
		}
	}

	d, ok := GetAndAdvanceNextRecoveryDelay(initial, max)
	if ok {
		t.Errorf("after exhausting sequence: got ok=true, delay=%v, want ok=false", d)
	}
}

// TestNotifyDegraded_NoListener verifies that NotifyDegraded does not panic
// when no recovery listener is registered.
func TestNotifyDegraded_NoListener(t *testing.T) {
	NotifyDegraded()
}

// TestStartRecoveryListener_NotifyDegraded verifies that StartRecoveryListener
// triggers recovery when NotifyDegraded is called.
func TestStartRecoveryListener_NotifyDegraded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	validateCalled := atomic.Bool{}
	validate := func(ctx context.Context) error {
		validateCalled.Store(true)
		return nil
	}
	exhaustedCalled := atomic.Bool{}
	StartRecoveryListener(ctx, validate, 1*time.Millisecond, 100*time.Millisecond, func() {
		exhaustedCalled.Store(true)
	})

	NotifyDegraded()
	time.Sleep(50 * time.Millisecond)

	if !validateCalled.Load() {
		t.Error("NotifyDegraded should trigger RunRecovery which calls validate")
	}
	if exhaustedCalled.Load() {
		t.Error("validate succeeded, onExhausted should not be called")
	}
}

// TestStartRecoveryListener_ContextCancel verifies that StartRecoveryListener
// stops listening when context is cancelled, preventing recovery execution.
func TestStartRecoveryListener_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	validateCalled := atomic.Bool{}
	validate := func(ctx context.Context) error {
		validateCalled.Store(true)
		return errors.New("fail")
	}
	StartRecoveryListener(ctx, validate, 1*time.Minute, 13*time.Minute, func() {})

	time.Sleep(20 * time.Millisecond)

	if validateCalled.Load() {
		t.Error("cancelled context should not run recovery")
	}
}
