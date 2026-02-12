package degraded

import (
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/traffic"
)

// RecordSuccess records a successful weather request.
func RecordSuccess() {
	traffic.RecordSuccess()
}

// RecordError records a failed weather request (upstream error, timeout, etc.).
func RecordError() {
	traffic.RecordError()
}

// ErrorRate returns (errorCount, totalCount) within the window. totalCount = successes + errors.
func ErrorRate(window time.Duration) (errors, total int) {
	return traffic.ErrorRate(window)
}

// Reset clears all recorded data. For tests only.
func Reset() {
	traffic.Reset()
}
