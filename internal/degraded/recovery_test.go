package degraded

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

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
