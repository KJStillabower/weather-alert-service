//go:build integration
// +build integration

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/kjstillabower/weather-alert-service/internal/cache"
	"github.com/kjstillabower/weather-alert-service/internal/client"
	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
	"github.com/kjstillabower/weather-alert-service/internal/service"
	testhelpers "github.com/kjstillabower/weather-alert-service/internal/testhelpers"
)

var testLogger *zap.Logger

func init() {
	var err error
	testLogger, err = observability.NewLogger()
	if err != nil {
		panic(err)
	}
}

// setupIntegrationHandler creates a fully configured handler for integration testing.
// Returns handler, cache instance (for test setup), and cleanup function.
func setupIntegrationHandler(t *testing.T) (*Handler, cache.Cache, func()) {
	cfg := testhelpers.GetIntegrationConfig(t)

	weatherService, cacheSvc, cleanup := testhelpers.SetupIntegrationService(t, cfg)

	weatherClient := testhelpers.SetupIntegrationClient(t, cfg)

	handler := NewHandler(weatherService, weatherClient, nil, testLogger, nil)

	return handler, cacheSvc, cleanup
}

// setupRateLimitedHandler creates a handler with rate limiter for testing.
func setupRateLimitedHandler(t *testing.T, rps int, burst int) (*Handler, cache.Cache, func()) {
	cfg := testhelpers.GetIntegrationConfig(t)

	weatherService, cacheSvc, cleanup := testhelpers.SetupIntegrationService(t, cfg)
	weatherClient := testhelpers.SetupIntegrationClient(t, cfg)

	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	handler := NewHandler(weatherService, weatherClient, nil, testLogger, limiter)

	return handler, cacheSvc, cleanup
}

// makeIntegrationRequest makes an HTTP request through the full handler stack.
func makeIntegrationRequest(t *testing.T, handler *Handler, method, path string) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(testLogger))
	router.Use(MetricsMiddleware)
	router.HandleFunc("/weather/{location}", handler.GetWeather).Methods("GET")
	router.HandleFunc("/health", handler.GetHealth).Methods("GET")
	router.Handle("/metrics", observability.MetricsHandler()).Methods("GET")

	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(context.WithValue(req.Context(), "logger", testLogger))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestIntegration_GetWeather_CacheHit verifies end-to-end request flow
// when data exists in cache, avoiding upstream API call.
func TestIntegration_GetWeather_CacheHit(t *testing.T) {
	handler, cacheSvc, cleanup := setupIntegrationHandler(t)
	defer cleanup()

	ctx := context.Background()
	location := "seattle"

	// Arrange: Pre-populate cache
	testData := models.WeatherData{
		Location:    location,
		Temperature: 15.5,
		Conditions:  "Clear",
		Humidity:    65,
		WindSpeed:   10.2,
		Timestamp:   time.Now(),
	}
	if err := cacheSvc.Set(ctx, location, testData, 5*time.Minute); err != nil {
		t.Fatalf("Failed to populate cache: %v", err)
	}

	// Act: Make HTTP request
	w := makeIntegrationRequest(t, handler, "GET", "/weather/"+location)

	// Assert: Verify cache hit (should be fast, no API call)
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var response models.WeatherData
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Location != location {
		t.Errorf("Location = %q, want %q", response.Location, location)
	}
	if response.Temperature != testData.Temperature {
		t.Errorf("Temperature = %f, want %f", response.Temperature, testData.Temperature)
	}
}

// TestIntegration_GetWeather_CacheMiss verifies end-to-end request flow
// when cache miss triggers upstream API call and cache population.
func TestIntegration_GetWeather_CacheMiss(t *testing.T) {
	handler, _, cleanup := setupIntegrationHandler(t)
	defer cleanup()

	location := "london"

	// Arrange: Ensure cache is empty (use unique location or clear cache)
	// For this test, we'll use a location that's unlikely to be cached

	// Act: Make HTTP request (should trigger API call)
	w := makeIntegrationRequest(t, handler, "GET", "/weather/"+location)

	// Assert: Verify successful response from API
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		return
	}

	var response models.WeatherData
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Location == "" {
		t.Error("Response missing location")
	}
	if response.Temperature == 0 {
		t.Error("Response missing temperature")
	}

	// Verify cache was populated (second request should be cache hit)
	time.Sleep(100 * time.Millisecond) // Small delay to ensure cache write completes
	w2 := makeIntegrationRequest(t, handler, "GET", "/weather/"+location)
	if w2.Code != http.StatusOK {
		t.Errorf("Second request failed: %d. Body: %s", w2.Code, w2.Body.String())
		return
	}

	// Verify response is from cache (check timestamp is same or very close)
	var response2 models.WeatherData
	if err := json.NewDecoder(w2.Body).Decode(&response2); err != nil {
		t.Fatalf("Failed to decode second response: %v", err)
	}

	// Timestamps should be identical (from cache) or very close (within 1 second)
	timeDiff := response.Timestamp.Sub(response2.Timestamp)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	if timeDiff > time.Second {
		t.Errorf("Second request should return cached data. Time diff: %v", timeDiff)
	}
}

// TestIntegration_GetWeather_UpstreamError verifies error propagation
// from upstream API through service to HTTP handler.
func TestIntegration_GetWeather_UpstreamError(t *testing.T) {
	// Use invalid API key to trigger upstream error
	invalidKey := "invalid_key_for_testing_123456789012"
	if len(invalidKey) < 32 {
		invalidKey = invalidKey + strings.Repeat("0", 32-len(invalidKey))
	}

	logger, _ := observability.NewLogger()
	weatherClient, err := client.NewOpenWeatherClient(
		invalidKey,
		"https://api.openweathermap.org/data/2.5/weather",
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	cacheSvc := cache.NewInMemoryCache()
	weatherService := service.NewWeatherService(weatherClient, cacheSvc, 5*time.Minute)
	handler := NewHandler(weatherService, weatherClient, nil, logger, nil)

	// Act: Make request (should fail upstream)
	w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")

	// Assert: Verify error is properly mapped to 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d. Body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
		return
	}

	var errorResponse map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	errorObj, ok := errorResponse["error"].(map[string]interface{})
	if !ok {
		t.Fatal("Error response missing error object")
	}

	if errorObj["code"] != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("Error code = %v, want UPSTREAM_UNAVAILABLE", errorObj["code"])
	}
}

// TestIntegration_GetHealth_FullStack verifies health check endpoint
// with real dependencies (API key validation, cache ping).
func TestIntegration_GetHealth_FullStack(t *testing.T) {
	handler, _, cleanup := setupIntegrationHandler(t)
	defer cleanup()

	// Act: Make health check request
	w := makeIntegrationRequest(t, handler, "GET", "/health")

	// Assert: Verify health response
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want 200 or 503. Body: %s", w.Code, w.Body.String())
		return
	}

	var healthResponse map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&healthResponse); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	status, ok := healthResponse["status"].(string)
	if !ok {
		t.Fatal("Health response missing status")
	}

	validStatuses := []string{"healthy", "degraded", "idle", "overloaded", "shutting-down"}
	found := false
	for _, vs := range validStatuses {
		if status == vs {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Status = %q, want one of %v", status, validStatuses)
	}
}

// TestIntegration_GetMetrics_Format verifies metrics endpoint
// returns Prometheus-compatible format.
func TestIntegration_GetMetrics_Format(t *testing.T) {
	handler, _, cleanup := setupIntegrationHandler(t)
	defer cleanup()

	// Make a request to generate metrics
	makeIntegrationRequest(t, handler, "GET", "/weather/seattle")

	// Act: Request metrics
	w := makeIntegrationRequest(t, handler, "GET", "/metrics")

	// Assert: Verify Prometheus format
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		return
	}

	body := w.Body.String()

	// Check for Prometheus metric format (name{labels} value)
	if !strings.Contains(body, "httpRequestsTotal") {
		t.Error("Metrics missing httpRequestsTotal")
	}
	if !strings.Contains(body, "weatherApiCallsTotal") {
		t.Error("Metrics missing weatherApiCallsTotal")
	}
	if !strings.Contains(body, "cacheHitsTotal") {
		t.Error("Metrics missing cacheHitsTotal")
	}
}

// TestIntegration_RateLimiting_Enforcement verifies that rate limiter
// correctly denies requests exceeding the rate limit.
func TestIntegration_RateLimiting_Enforcement(t *testing.T) {
	// Setup handler with low rate limit for testing
	rps := 10
	burst := 20
	handler, _, cleanup := setupRateLimitedHandler(t, rps, burst)
	defer cleanup()

	// Act: Send burst of requests exceeding rate limit
	successCount := 0
	deniedCount := 0

	for i := 0; i < burst+10; i++ {
		w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")

		if w.Code == http.StatusOK {
			successCount++
		} else if w.Code == http.StatusTooManyRequests {
			deniedCount++

			// Verify error response format
			var errorResponse map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&errorResponse); err == nil {
				errorObj := errorResponse["error"].(map[string]interface{})
				if errorObj["code"] != "RATE_LIMITED" {
					t.Errorf("Error code = %v, want RATE_LIMITED", errorObj["code"])
				}
			}
		}
	}

	// Assert: Some requests should be denied
	if deniedCount == 0 {
		t.Error("No requests were rate limited, but some should be")
	}

	// Verify success count doesn't exceed burst significantly
	// Allow some tolerance for timing
	if successCount > burst+5 {
		t.Errorf("Success count = %d, should not significantly exceed burst %d", successCount, burst)
	}
}

// TestIntegration_RateLimiting_Concurrent verifies rate limiting
// behavior under concurrent load.
func TestIntegration_RateLimiting_Concurrent(t *testing.T) {
	rps := 50
	burst := 100
	handler, _, cleanup := setupRateLimitedHandler(t, rps, burst)
	defer cleanup()

	const numGoroutines = 10
	const requestsPerGoroutine = 20

	var wg sync.WaitGroup
	results := make(chan int, numGoroutines*requestsPerGoroutine)

	// Act: Send concurrent requests
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
				results <- w.Code
			}
		}()
	}

	wg.Wait()
	close(results)

	// Assert: Count results
	successCount := 0
	deniedCount := 0
	for code := range results {
		if code == http.StatusOK {
			successCount++
		} else if code == http.StatusTooManyRequests {
			deniedCount++
		}
	}

	// Verify rate limiting occurred
	if deniedCount == 0 {
		t.Error("No requests were rate limited under concurrent load")
	}

	// Verify total requests processed
	total := successCount + deniedCount
	expected := numGoroutines * requestsPerGoroutine
	if total != expected {
		t.Errorf("Total requests = %d, want %d", total, expected)
	}
}

// TestIntegration_RateLimiting_Window verifies rate limit window
// behavior over time (requests allowed after window expires).
func TestIntegration_RateLimiting_Window(t *testing.T) {
	rps := 2 // Very low for testing
	burst := 5
	handler, _, cleanup := setupRateLimitedHandler(t, rps, burst)
	defer cleanup()

	// Act: Exhaust burst
	for i := 0; i < burst; i++ {
		w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
		if w.Code != http.StatusOK {
			t.Errorf("Request %d denied unexpectedly: %d", i, w.Code)
		}
	}

	// Next request should be denied
	w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Request after burst should be denied, got %d", w.Code)
	}

	// Wait for rate limit window to allow more requests
	// Rate is 2 per second, so wait 1 second to allow 2 more requests
	time.Sleep(time.Second + 100*time.Millisecond)

	// Next request should be allowed
	w2 := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
	if w2.Code != http.StatusOK {
		t.Errorf("Request after window should be allowed, got %d", w2.Code)
	}
}

// TestIntegration_RateLimiting_Metrics verifies that rate limit
// denials are recorded in metrics.
func TestIntegration_RateLimiting_Metrics(t *testing.T) {
	rps := 5
	burst := 10
	handler, _, cleanup := setupRateLimitedHandler(t, rps, burst)
	defer cleanup()

	// Exhaust rate limit
	for i := 0; i < burst+5; i++ {
		makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
	}

	// Act: Check metrics
	w := makeIntegrationRequest(t, handler, "GET", "/metrics")
	body := w.Body.String()

	// Assert: Verify rate limit metrics
	if !strings.Contains(body, "rateLimitDeniedTotal") {
		t.Error("Metrics missing rateLimitDeniedTotal")
	}
}
