package overload

import (
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/traffic"
)

// RecordDenial records a rate-limit denial (429). Call from middleware when returning 429.
func RecordDenial() {
	traffic.RecordDenied()
}

// RequestCount returns the number of requests (success + error + denied) within the given window.
func RequestCount(window time.Duration) int {
	return traffic.RequestCount(window)
}

// DenialCount returns the number of denials within the given window.
func DenialCount(window time.Duration) int {
	return traffic.DenialCount(window)
}

// Reset clears all recorded data. For tests only.
func Reset() {
	traffic.Reset()
}
