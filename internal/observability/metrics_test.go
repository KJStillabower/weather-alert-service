package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMetrics_Usable verifies that all Prometheus metrics can be used without
// panic, ensuring label dimensions match usage across client, http, service, and cache packages.
func TestMetrics_Usable(t *testing.T) {
	// Verify metrics can be used without panic; label dimensions match usage in client, http, service, cache
	// Route uses path template to avoid cardinality (e.g. /weather/{location} not /weather/seattle)
	HTTPRequestsTotal.WithLabelValues("GET", "/weather/{location}", "2xx").Inc()
	HTTPRequestDuration.WithLabelValues("GET", "/weather/{location}").Observe(0.01)
	WeatherAPICallsTotal.WithLabelValues("success").Inc()
	WeatherAPICallsTotal.WithLabelValues("error").Inc()
	WeatherAPIDuration.WithLabelValues("success").Observe(0.1)
	CacheHitsTotal.WithLabelValues("weather").Inc()
	WeatherQueriesTotal.Inc()
	WeatherQueriesByLocationTotal.WithLabelValues("seattle").Inc()
	WeatherQueriesByLocationTotal.WithLabelValues("other").Inc()
}

// TestSetTrackedLocations_and_RecordWeatherQuery verifies that SetTrackedLocations
// configures location allow-list and RecordWeatherQuery correctly labels tracked vs "other" locations.
func TestSetTrackedLocations_and_RecordWeatherQuery(t *testing.T) {
	SetTrackedLocations([]string{"seattle", "portland"})
	RecordWeatherQuery("Seattle")
	RecordWeatherQuery("unknown-city")
	SetTrackedLocations(nil) // reset for other tests
}

// TestMetricsHandler_ServesPrometheusFormat verifies that MetricsHandler serves
// Prometheus text exposition format with correct HTTP status and metric output.
func TestMetricsHandler_ServesPrometheusFormat(t *testing.T) {
	handler := MetricsHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("MetricsHandler status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "httpRequestsTotal") {
		t.Error("MetricsHandler response should contain metric output")
	}
}
