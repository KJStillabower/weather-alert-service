package lifecycle

import "testing"

// TestIsShuttingDown_DefaultFalse verifies that IsShuttingDown returns false
// by default when shutdown flag has not been set.
func TestIsShuttingDown_DefaultFalse(t *testing.T) {
	SetShuttingDown(false)
	if IsShuttingDown() {
		t.Error("IsShuttingDown() = true, want false by default")
	}
}

// TestSetShuttingDown_True verifies that SetShuttingDown(true) sets the
// shutdown flag and IsShuttingDown returns true.
func TestSetShuttingDown_True(t *testing.T) {
	SetShuttingDown(true)
	defer SetShuttingDown(false)
	if !IsShuttingDown() {
		t.Error("IsShuttingDown() = false after SetShuttingDown(true), want true")
	}
}

// TestSetShuttingDown_False verifies that SetShuttingDown(false) clears the
// shutdown flag and IsShuttingDown returns false.
func TestSetShuttingDown_False(t *testing.T) {
	SetShuttingDown(true)
	SetShuttingDown(false)
	if IsShuttingDown() {
		t.Error("IsShuttingDown() = true after SetShuttingDown(false), want false")
	}
}
