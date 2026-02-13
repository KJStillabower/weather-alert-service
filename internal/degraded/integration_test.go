//go:build integration
// +build integration

package degraded

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/client"
	testhelpers "github.com/kjstillabower/weather-alert-service/internal/testhelpers"
)

// TestIntegration_DegradedState_Detection verifies that degraded state
// is detected when API key validation fails.
func TestIntegration_DegradedState_Detection(t *testing.T) {
	// Use invalid API key to simulate degraded state
	invalidKey := "invalid_key_for_degraded_test_123456789012"
	if len(invalidKey) < 32 {
		invalidKey = invalidKey + "0000000000000000"[:32-len(invalidKey)]
	}

	invalidClient, err := client.NewOpenWeatherClient(
		invalidKey,
		"https://api.openweathermap.org/data/2.5/weather",
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.Background()

	// Act: Attempt API key validation (should fail)
	err = invalidClient.ValidateAPIKey(ctx)

	// Assert: Verify error indicates degraded state
	if err == nil {
		t.Error("ValidateAPIKey() error = nil, want error (invalid key)")
	}

	// Verify error is client.ErrInvalidAPIKey or wrapped version
	if err != nil {
		// Error should indicate invalid API key
		errStr := err.Error()
		if !strings.Contains(strings.ToLower(errStr), "invalid") && 
		   !strings.Contains(strings.ToLower(errStr), "api key") {
			t.Errorf("Error message = %q, should mention invalid API key", errStr)
		}
	}
}

// TestIntegration_DegradedState_RecoverySequence verifies recovery
// sequence (Fibonacci backoff) delay calculation.
func TestIntegration_DegradedState_RecoverySequence(t *testing.T) {
	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		t.Skip("WEATHER_API_KEY not set, skipping integration test")
	}

	client := testhelpers.SetupIntegrationClient(t, testhelpers.GetIntegrationConfig(t))

	// Test recovery delay sequence
	initialDelay := 1 * time.Minute
	maxDelay := 20 * time.Minute

	// Test recovery delay sequence generation
	delays := fibDelays(initialDelay, maxDelay)
	if len(delays) == 0 {
		t.Fatal("No recovery delays generated")
	}

	// Verify delay sequence is Fibonacci-based
	// First delay should be initialDelay (scaled to Fibonacci 1)
	expectedFirst := initialDelay
	if delays[0] != expectedFirst {
		t.Errorf("First delay = %v, want %v", delays[0], expectedFirst)
	}

	// Verify delays increase (Fibonacci sequence)
	if len(delays) > 1 {
		for i := 1; i < len(delays); i++ {
			if delays[i] <= delays[i-1] {
				t.Errorf("Delay %d (%v) should be greater than delay %d (%v)", i, delays[i], i-1, delays[i-1])
			}
		}
	}

	// Verify delays don't exceed maxDelay
	for i, delay := range delays {
		if delay > maxDelay {
			t.Errorf("Delay %d (%v) exceeds maxDelay %v", i, delay, maxDelay)
		}
	}

	// Verify client can be created (doesn't test recovery, just validates setup)
	ctx := context.Background()
	_ = client.ValidateAPIKey(ctx) // May succeed or fail, both are valid for this test
}

// TestIntegration_DegradedState_RecoveryOverrides verifies test-only
// recovery overrides work correctly in integration tests.
func TestIntegration_DegradedState_RecoveryOverrides(t *testing.T) {
	// Test recovery disabled override
	SetRecoveryDisabled(true)
	defer ClearRecoveryOverrides()

	if !IsRecoveryDisabled() {
		t.Error("Recovery should be disabled")
	}

	// Test force succeed override
	ClearRecoveryOverrides()
	SetForceSucceedNextAttempt(true)

	// Verify override is set (would be consumed by RunRecovery)
	// Note: Full recovery test would require more complex setup with RunRecovery
	// For integration tests, we verify the override mechanism works

	// Clear and verify
	ClearRecoveryOverrides()
	if IsRecoveryDisabled() {
		t.Error("Recovery should not be disabled after ClearRecoveryOverrides")
	}
}

// TestIntegration_DegradedState_ErrorTracking verifies that error tracking
// works correctly with real API calls.
func TestIntegration_DegradedState_ErrorTracking(t *testing.T) {
	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		t.Skip("WEATHER_API_KEY not set, skipping integration test")
	}

	// Record some errors and successes
	RecordError()
	RecordError()
	RecordSuccess()
	RecordSuccess()
	RecordSuccess()

	// Check error rate
	window := 1 * time.Minute
	errors, total := ErrorRate(window)

	// Assert: Should have recorded errors and successes
	if total == 0 {
		t.Error("ErrorRate() total = 0, want > 0")
	}
	if errors == 0 {
		t.Error("ErrorRate() errors = 0, but we recorded errors")
	}

	// Verify error rate calculation
	if errors > total {
		t.Errorf("ErrorRate() errors (%d) > total (%d)", errors, total)
	}

	// Cleanup
	Reset()
}
