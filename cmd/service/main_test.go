package main

import "testing"

// TestCoverageGaps_IntentionallyUntested documents why cmd/service has no unit tests.
// Run with -v to see skip reason.
func TestCoverageGaps_IntentionallyUntested(t *testing.T) {
	t.Skip("main.go is wiring-only; all logic lives in internal packages with tests. Entrypoint coverage would require exec or heavy mocking")
}
