package http

import (
	"context"
	"testing"
	"time"
)

func TestInFlightTracker_CountAndWait(t *testing.T) {
	tracker := &InFlightTracker{}

	if got := tracker.Count(); got != 0 {
		t.Errorf("Count() = %d, want 0", got)
	}

	tracker.Increment()
	tracker.Increment()
	if got := tracker.Count(); got != 2 {
		t.Errorf("Count() = %d, want 2", got)
	}

	tracker.Decrement()
	if got := tracker.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}

	tracker.Decrement()
	if got := tracker.Count(); got != 0 {
		t.Errorf("Count() = %d, want 0", got)
	}
}

func TestInFlightTracker_WaitForZero(t *testing.T) {
	tracker := &InFlightTracker{}
	tracker.Increment()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = tracker.WaitForZero(ctx, 5*time.Millisecond)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	tracker.Decrement()

	select {
	case <-done:
		// WaitForZero returned (count reached zero)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitForZero did not return after count reached zero")
	}
}

func TestInFlightTracker_WaitForZero_ContextCanceled(t *testing.T) {
	tracker := &InFlightTracker{}
	tracker.Increment()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := tracker.WaitForZero(ctx, 5*time.Millisecond)
	if err == nil {
		t.Error("WaitForZero expected context error, got nil")
	}
}
