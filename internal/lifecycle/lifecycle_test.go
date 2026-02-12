package lifecycle

import "testing"

func TestIsShuttingDown_DefaultFalse(t *testing.T) {
	SetShuttingDown(false)
	if IsShuttingDown() {
		t.Error("IsShuttingDown() = true, want false by default")
	}
}

func TestSetShuttingDown_True(t *testing.T) {
	SetShuttingDown(true)
	defer SetShuttingDown(false)
	if !IsShuttingDown() {
		t.Error("IsShuttingDown() = false after SetShuttingDown(true), want true")
	}
}

func TestSetShuttingDown_False(t *testing.T) {
	SetShuttingDown(true)
	SetShuttingDown(false)
	if IsShuttingDown() {
		t.Error("IsShuttingDown() = true after SetShuttingDown(false), want false")
	}
}
