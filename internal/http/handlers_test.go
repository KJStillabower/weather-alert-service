package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/kjstillabower/weather-alert-service/internal/degraded"
	"github.com/kjstillabower/weather-alert-service/internal/idle"
	"github.com/kjstillabower/weather-alert-service/internal/lifecycle"
	"github.com/kjstillabower/weather-alert-service/internal/overload"
	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/service"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type mockWeatherClient struct {
	weather      models.WeatherData
	err          error
	validateErr  error
	block        chan struct{} // if set, GetCurrentWeather blocks until ctx.Done()
}

func (m *mockWeatherClient) GetCurrentWeather(ctx context.Context, location string) (models.WeatherData, error) {
	if m.block != nil {
		select {
		case <-ctx.Done():
			return models.WeatherData{}, ctx.Err()
		case <-m.block:
			return models.WeatherData{}, nil
		}
	}
	return m.weather, m.err
}

func (m *mockWeatherClient) ValidateAPIKey(ctx context.Context) error {
	return m.validateErr
}

type mockCache struct {
	data map[string]models.WeatherData
	err  error
}

func (m *mockCache) Get(ctx context.Context, key string) (models.WeatherData, bool, error) {
	if m.err != nil {
		return models.WeatherData{}, false, m.err
	}
	val, ok := m.data[key]
	return val, ok, nil
}

func (m *mockCache) Set(ctx context.Context, key string, value models.WeatherData, ttl time.Duration) error {
	if m.err != nil {
		return m.err
	}
	if m.data == nil {
		m.data = make(map[string]models.WeatherData)
	}
	m.data[key] = value
	return nil
}

// TestHandler_GetWeather_Success verifies that GetWeather returns weather data
// successfully with correct HTTP status and response schema when upstream fetch succeeds.
func TestHandler_GetWeather_Success(t *testing.T) {
	// Arrange: Set up mock client with weather data, empty cache, and handler
	expectedWeather := models.WeatherData{
		Location:    "seattle",
		Temperature: 15.5,
		Conditions:  "Cloudy",
		Humidity:    75,
		WindSpeed:   5.2,
		Timestamp:   time.Now(),
	}

	mockClient := &mockWeatherClient{weather: expectedWeather}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "logger", logger)
	ctx = context.WithValue(ctx, "correlation_id", "test-correlation-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	// Act: Execute GET request
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and correct response data
	if w.Code != http.StatusOK {
		t.Errorf("GetWeather() status = %d, want %d", w.Code, http.StatusOK)
	}

	var response models.WeatherData
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Location != expectedWeather.Location {
		t.Errorf("Response.Location = %q, want %q", response.Location, expectedWeather.Location)
	}
	if response.Temperature != expectedWeather.Temperature {
		t.Errorf("Response.Temperature = %v, want %v", response.Temperature, expectedWeather.Temperature)
	}
}

// TestHandler_GetWeather_EmptyLocation verifies that GetWeather returns 400 Bad Request
// with INVALID_LOCATION error code when location is empty or whitespace-only.
func TestHandler_GetWeather_EmptyLocation(t *testing.T) {
	// Arrange: Set up handler and request with whitespace-only location
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/weather/%20%20%20", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "logger", logger)
	ctx = context.WithValue(ctx, "correlation_id", "test-correlation-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	// Act: Execute GET request with invalid location
	router.ServeHTTP(w, req)

	// Assert: Verify 400 status and error response shape
	if w.Code != http.StatusBadRequest {
		t.Errorf("GetWeather() status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var errorResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errorResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	errorObj, ok := errorResp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("Error response missing 'error' field")
	}

	if errorObj["code"] != "INVALID_LOCATION" {
		t.Errorf("Error code = %q, want INVALID_LOCATION", errorObj["code"])
	}
}

// TestHandler_GetWeather_ServiceError verifies that GetWeather maps service errors
// to 503 Service Unavailable with UPSTREAM_UNAVAILABLE error code.
func TestHandler_GetWeather_ServiceError(t *testing.T) {
	// Arrange: Set up mock client that returns error and handler
	mockClient := &mockWeatherClient{
		err: errors.New("upstream unavailable"),
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "logger", logger)
	ctx = context.WithValue(ctx, "correlation_id", "test-correlation-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	// Act: Execute GET request when upstream fails
	router.ServeHTTP(w, req)

	// Assert: Verify 503 status and error response shape
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("GetWeather() status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var errorResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errorResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	errorObj, ok := errorResp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("Error response missing 'error' field")
	}

	if errorObj["code"] != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("Error code = %q, want UPSTREAM_UNAVAILABLE", errorObj["code"])
	}
}

// TestHandler_GetHealth verifies that GetHealth returns 200 OK with healthy status
// and correct health check structure when all dependencies are operational.
func TestHandler_GetHealth(t *testing.T) {
	// Arrange: Set up handler with healthy dependencies
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check
	handler.GetHealth(w, req)

	// Assert: Verify 200 status and health response schema
	if w.Code != http.StatusOK {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusOK)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "healthy" {
		t.Errorf("Health status = %q, want healthy", health["status"])
	}

	if health["service"] != "weather-alert-service" {
		t.Errorf("Health service = %q, want weather-alert-service", health["service"])
	}

	checks, ok := health["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("Health checks missing")
	}

	if checks["weatherApi"] != "healthy" {
		t.Errorf("WeatherApi check = %q, want healthy", checks["weatherApi"])
	}
}

// TestHandler_GetHealth_InvalidAPIKey_DegradedWithLogger verifies that GetHealth returns
// degraded status when API key validation fails, indicating upstream dependency failure.
func TestHandler_GetHealth_InvalidAPIKey_DegradedWithLogger(t *testing.T) {
	// Arrange: Set up mock client with invalid API key error
	mockClient := &mockWeatherClient{
		validateErr: errors.New("invalid API key: API key is invalid or not activated"),
	}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "logger", logger)
	ctx = context.WithValue(ctx, "correlation_id", "test-correlation-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	// Act: Execute health check with invalid API key
	handler.GetHealth(w, req)

	// Assert: Verify 503 status and degraded health with unhealthy API check
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "degraded" {
		t.Errorf("Health status = %q, want degraded", health["status"])
	}

	checks, ok := health["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("Health checks missing")
	}

	if checks["weatherApi"] != "unhealthy" {
		t.Errorf("WeatherApi check = %q, want unhealthy", checks["weatherApi"])
	}
}

// TestHandler_GetHealth_ShuttingDown verifies that GetHealth returns shutting-down status
// when the service is in shutdown state, indicating it should not receive new traffic.
func TestHandler_GetHealth_ShuttingDown(t *testing.T) {
	// Arrange: Set shutdown flag and handler
	lifecycle.SetShuttingDown(true)
	defer lifecycle.SetShuttingDown(false)

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check during shutdown
	handler.GetHealth(w, req)

	// Assert: Verify 503 status and shutting-down health status
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "shutting-down" {
		t.Errorf("Health status = %q, want shutting-down", health["status"])
	}
}

// TestHandler_GetHealth_Overloaded verifies that GetHealth returns overloaded status
// when request rate exceeds configured overload threshold.
func TestHandler_GetHealth_Overloaded(t *testing.T) {
	// Arrange: Reset state and configure overload threshold (threshold = 2 * 1 * 0.4 = 0.8, so 1+ requests overload)
	overload.Reset()
	degraded.RecordSuccess()

	healthConfig := &HealthConfig{
		OverloadWindow:       1 * time.Second,
		OverloadThresholdPct: 40,
		RateLimitRPS:        2,
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check when overloaded
	handler.GetHealth(w, req)

	// Assert: Verify 503 status and overloaded health status
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "overloaded" {
		t.Errorf("Health status = %q, want overloaded", health["status"])
	}
}

// TestHandler_GetHealth_Idle verifies that GetHealth returns idle status when
// service uptime exceeds minimum lifespan and request rate is below idle threshold.
func TestHandler_GetHealth_Idle(t *testing.T) {
	// Arrange: Reset idle state and configure with uptime > minimum_lifespan, no requests recorded
	idle.Reset()

	healthConfig := &HealthConfig{
		IdleWindow:             1 * time.Minute,
		IdleThresholdReqPerMin: 5,
		MinimumLifespan:        100 * time.Millisecond,
		StartTime:              time.Now().Add(-1 * time.Minute), // simulated 1min uptime
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check when idle conditions are met
	handler.GetHealth(w, req)

	// Assert: Verify 200 status and idle health status
	if w.Code != http.StatusOK {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusOK)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "idle" {
		t.Errorf("Health status = %q, want idle", health["status"])
	}
}

// TestHandler_GetHealth_HealthyNotIdle_RecentStart verifies that GetHealth returns healthy
// (not idle) when service uptime is less than minimum lifespan, even if request rate is low.
func TestHandler_GetHealth_HealthyNotIdle_RecentStart(t *testing.T) {
	// Arrange: Reset idle state and configure with recent start (uptime < minimum_lifespan)
	idle.Reset()
	healthConfig := &HealthConfig{
		IdleWindow:             1 * time.Minute,
		IdleThresholdReqPerMin:  5,
		MinimumLifespan:        10 * time.Minute,
		StartTime:              time.Now().Add(-1 * time.Minute),
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check when uptime < minimum_lifespan
	handler.GetHealth(w, req)

	// Assert: Verify 200 status and healthy (not idle) status
	if w.Code != http.StatusOK {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusOK)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "healthy" {
		t.Errorf("Health status = %q, want healthy (uptime < minimum_lifespan)", health["status"])
	}
}

// TestHandler_GetHealth_HealthyNotIdle_AboveThreshold verifies that GetHealth returns healthy
// (not idle) when request rate exceeds idle threshold, even if uptime > minimum_lifespan.
func TestHandler_GetHealth_HealthyNotIdle_AboveThreshold(t *testing.T) {
	// Arrange: Reset idle state and record requests exceeding threshold
	idle.Reset()
	for i := 0; i < 10; i++ {
		idle.RecordRequest()
	}
	healthConfig := &HealthConfig{
		IdleWindow:             1 * time.Minute,
		IdleThresholdReqPerMin:  5,
		MinimumLifespan:        100 * time.Millisecond,
		StartTime:              time.Now().Add(-1 * time.Minute),
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check when request rate exceeds idle threshold
	handler.GetHealth(w, req)

	// Assert: Verify 200 status and healthy (not idle) status
	if w.Code != http.StatusOK {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusOK)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "healthy" {
		t.Errorf("Health status = %q, want healthy (request count above idle threshold)", health["status"])
	}
}

// TestHandler_GetHealth_DegradedErrorRate verifies that GetHealth returns degraded status
// when error rate exceeds configured degraded threshold.
func TestHandler_GetHealth_DegradedErrorRate(t *testing.T) {
	// Arrange: Reset degraded state and record errors exceeding threshold (2 errors, 1 success = 66% > 50%)
	degraded.Reset()
	degraded.RecordError()
	degraded.RecordError()
	degraded.RecordSuccess()

	healthConfig := &HealthConfig{
		DegradedWindow:   1 * time.Minute,
		DegradedErrorPct: 50,
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check when error rate exceeds threshold
	handler.GetHealth(w, req)

	// Assert: Verify 503 status and degraded health status
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "degraded" {
		t.Errorf("Health status = %q, want degraded", health["status"])
	}
}

// TestHandler_GetHealth_NotDegraded_BelowErrorThreshold verifies that GetHealth returns healthy
// status when error rate is below degraded threshold.
func TestHandler_GetHealth_NotDegraded_BelowErrorThreshold(t *testing.T) {
	// Arrange: Reset degraded state and record errors below threshold (1 error, 3 total = 33% < 50%)
	degraded.Reset()
	degraded.RecordSuccess()
	degraded.RecordSuccess()
	degraded.RecordError()

	healthConfig := &HealthConfig{
		DegradedWindow:   1 * time.Minute,
		DegradedErrorPct: 50,
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check when error rate is below threshold
	handler.GetHealth(w, req)

	// Assert: Verify 200 status and healthy health status
	if w.Code != http.StatusOK {
		t.Errorf("GetHealth() status = %d, want %d", w.Code, http.StatusOK)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "healthy" {
		t.Errorf("Health status = %q, want healthy (error rate below threshold)", health["status"])
	}
}

// TestHandler_GetHealth_LogsTransition verifies that GetHealth logs health status transitions
// only when status changes, not on every health check call.
func TestHandler_GetHealth_LogsTransition(t *testing.T) {
	// Arrange: Set up logger with observer and handler
	degraded.Reset()
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	healthConfig := &HealthConfig{
		DegradedWindow:   1 * time.Minute,
		DegradedErrorPct: 50,
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	// Act: First call - healthy (no errors yet). Establishes previous status.
	degraded.RecordSuccess()
	degraded.RecordSuccess()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.GetHealth(w, req)

	// Assert: First call should not log transition
	if w.Code != http.StatusOK {
		t.Fatalf("first GetHealth status = %d, want 200", w.Code)
	}
	if logs.Len() != 0 {
		t.Fatalf("first call should not log transition; got %d logs", logs.Len())
	}

	// Act: Inject errors to breach threshold (66% > 50%) and call again
	degraded.RecordError()
	degraded.RecordError()

	w2 := httptest.NewRecorder()
	handler.GetHealth(w2, req)

	// Assert: Second call should log transition from healthy to degraded
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("second GetHealth status = %d, want 503", w2.Code)
	}

	entries := logs.FilterMessage("health status transition").All()
	if len(entries) != 1 {
		t.Fatalf("want 1 transition log, got %d", len(entries))
	}
	entry := entries[0]
	var prev, curr, reason string
	for _, f := range entry.Context {
		switch f.Key {
		case "previous_status":
			prev = f.String
		case "current_status":
			curr = f.String
		case "reason":
			reason = f.String
		}
	}
	if prev != "healthy" {
		t.Errorf("previous_status = %q, want healthy", prev)
	}
	if curr != "degraded" {
		t.Errorf("current_status = %q, want degraded", curr)
	}
	if reason != "error_rate_breach" {
		t.Errorf("reason = %q, want error_rate_breach", reason)
	}

	// Act: Third call - still degraded
	w3 := httptest.NewRecorder()
	handler.GetHealth(w3, req)

	// Assert: Third call should not log (status unchanged)
	if w3.Code != http.StatusServiceUnavailable {
		t.Fatalf("third GetHealth status = %d, want 503", w3.Code)
	}
	if logs.Len() != 1 {
		t.Errorf("third call (status unchanged) should not log; total logs = %d, want 1", logs.Len())
	}
}

// TestHandler_GetWeather_DebugLogs_CacheHit verifies that GetWeather emits DEBUG-level logs
// for cache hits and weather served events with correct metadata.
func TestHandler_GetWeather_DebugLogs_CacheHit(t *testing.T) {
	// Arrange: Set up cache with pre-populated data, logger with observer, and handler
	expectedWeather := models.WeatherData{
		Location:    "seattle",
		Temperature: 15.0,
		Conditions:  "clear",
		Humidity:    50,
		WindSpeed:   3.0,
		Timestamp:   time.Now(),
	}
	mockClient := &mockWeatherClient{weather: expectedWeather}
	mockCache := &mockCache{data: map[string]models.WeatherData{"seattle": expectedWeather}}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	ctx := context.WithValue(req.Context(), "logger", logger)
	ctx = context.WithValue(ctx, "correlation_id", "test-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	// Act: Execute GET request for cached location
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and DEBUG logs for cache hit and weather served
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	hitEntries := logs.FilterMessage("cache hit").All()
	if len(hitEntries) != 1 {
		t.Fatalf("want 1 cache hit log, got %d", len(hitEntries))
	}
	var loc string
	for _, f := range hitEntries[0].Context {
		if f.Key == "location" {
			loc = f.String
			break
		}
	}
	if loc != "seattle" {
		t.Errorf("cache hit location = %q, want seattle", loc)
	}

	servedEntries := logs.FilterMessage("weather served").All()
	if len(servedEntries) != 1 {
		t.Fatalf("want 1 weather served log, got %d", len(servedEntries))
	}
	var cached bool
	for _, f := range servedEntries[0].Context {
		if f.Key == "cached" && f.Type == zapcore.BoolType {
			cached = f.Integer == 1
			break
		}
	}
	if !cached {
		t.Error("weather served should have cached=true for cache hit")
	}
}

// TestHandler_GetWeather_DebugLogs_CacheMiss verifies that GetWeather emits DEBUG-level logs
// for cache misses and weather served events with cached=false metadata.
func TestHandler_GetWeather_DebugLogs_CacheMiss(t *testing.T) {
	// Arrange: Set up empty cache, logger with observer, and handler
	expectedWeather := models.WeatherData{
		Location:    "portland",
		Temperature: 12.0,
		Conditions:  "cloudy",
		Humidity:    70,
		WindSpeed:   4.0,
		Timestamp:   time.Now(),
	}
	mockClient := &mockWeatherClient{weather: expectedWeather}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/weather/portland", nil)
	ctx := context.WithValue(req.Context(), "logger", logger)
	ctx = context.WithValue(ctx, "correlation_id", "test-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	// Act: Execute GET request for uncached location
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and DEBUG logs for cache miss and weather served with cached=false
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	missEntries := logs.FilterMessage("cache miss, fetching upstream").All()
	if len(missEntries) != 1 {
		t.Fatalf("want 1 cache miss log, got %d", len(missEntries))
	}

	servedEntries := logs.FilterMessage("weather served").All()
	if len(servedEntries) != 1 {
		t.Fatalf("want 1 weather served log, got %d", len(servedEntries))
	}
	var cached bool
	for _, f := range servedEntries[0].Context {
		if f.Key == "cached" && f.Type == zapcore.BoolType {
			cached = f.Integer == 1
			break
		}
	}
	if cached {
		t.Error("weather served should have cached=false for cache miss")
	}
}

// TestHandler_GetTestStatus verifies that GetTestStatus returns test status information
// including request counts, error counts, and configuration details.
func TestHandler_GetTestStatus(t *testing.T) {
	// Arrange: Reset state and set up handler with health config
	overload.Reset()
	degraded.Reset()

	healthConfig := &HealthConfig{
		OverloadWindow:        60 * time.Second,
		OverloadThresholdPct:  80,
		RateLimitRPS:         5,
		DegradedWindow:       60 * time.Second,
		DegradedErrorPct:     5,
	}

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Act: Execute GET request for test status
	handler.GetTestStatus(w, req)

	// Assert: Verify 200 status and response contains all required fields
	if w.Code != http.StatusOK {
		t.Errorf("GetTestStatus() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := resp["total_requests_in_window"]; !ok {
		t.Error("Response missing total_requests_in_window")
	}
	if _, ok := resp["denied_requests_in_window"]; !ok {
		t.Error("Response missing denied_requests_in_window")
	}
	if _, ok := resp["errors_in_window"]; !ok {
		t.Error("Response missing errors_in_window")
	}
	if _, ok := resp["window_length"]; !ok {
		t.Error("Response missing window_length")
	}
	if _, ok := resp["auto_clear"]; !ok {
		t.Error("Response missing auto_clear")
	}
}

// TestHandler_PostTestReset verifies that PostTestAction with "reset" action clears
// all test state including overload and degraded tracking.
func TestHandler_PostTestReset(t *testing.T) {
	// Arrange: Set up state with recorded events and handler
	degraded.Reset()
	degraded.RecordSuccess()
	degraded.RecordError()

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("POST", "/test/reset", nil)
	req = req.WithContext(context.WithValue(req.Context(), "correlation_id", "test-id"))
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request to reset test state
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status, correct response, and state cleared
	if w.Code != http.StatusOK {
		t.Errorf("PostTestReset() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["action"] != "reset" {
		t.Errorf("action = %q, want reset", resp["action"])
	}
	if !resp["ok"].(bool) {
		t.Error("ok = false, want true")
	}

	if overload.RequestCount(1*time.Minute) != 0 {
		t.Error("Reset: overload state not cleared")
	}
}

// TestHandler_PostTestLoad verifies that PostTestAction with "load" action accepts
// the specified number of requests and returns the accepted count.
func TestHandler_PostTestLoad(t *testing.T) {
	// Arrange: Reset state and set up handler
	overload.Reset()
	degraded.Reset()

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	body := `{"count": 15}`
	req := httptest.NewRequest("POST", "/test/load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request to generate load
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and correct accepted count
	if w.Code != http.StatusOK {
		t.Errorf("PostTestLoad() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["action"] != "load" {
		t.Errorf("action = %q, want load", resp["action"])
	}
	if got := int(resp["accepted"].(float64)); got != 15 {
		t.Errorf("accepted = %d, want 15", got)
	}
}

// TestHandler_PostTestError verifies that PostTestAction with "error" action records
// the specified number of errors and returns the calculated error rate percentage.
func TestHandler_PostTestError(t *testing.T) {
	// Arrange: Reset degraded state with some successes and set up handler
	degraded.Reset()
	degraded.RecordSuccess()
	degraded.RecordSuccess()
	degraded.RecordSuccess()

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	body := `{"count": 2}`
	req := httptest.NewRequest("POST", "/test/error", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request to inject errors
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and correct error rate calculation
	if w.Code != http.StatusOK {
		t.Errorf("PostTestError() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["action"] != "error" {
		t.Errorf("action = %q, want error", resp["action"])
	}
	if got := int(resp["error_rate_pct"].(float64)); got != 40 {
		t.Errorf("error_rate_pct = %d, want 40 (2 errors / 5 total)", got)
	}
}

// TestHandler_PostTestShutdown verifies that PostTestAction with "shutdown" action
// sets the service shutdown flag, triggering graceful shutdown behavior.
func TestHandler_PostTestShutdown(t *testing.T) {
	// Arrange: Reset shutdown flag and set up handler
	lifecycle.SetShuttingDown(false)
	defer lifecycle.SetShuttingDown(false)

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("POST", "/test/shutdown", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request to trigger shutdown
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status, correct response, and shutdown flag set
	if w.Code != http.StatusOK {
		t.Errorf("PostTestShutdown() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["action"] != "shutdown" {
		t.Errorf("action = %q, want shutdown", resp["action"])
	}
	if !lifecycle.IsShuttingDown() {
		t.Error("Shutting-down flag not set")
	}
}

// TestHandler_PostTestPreventClear verifies that PostTestAction with "prevent_clear" action
// disables automatic recovery clearing for degraded state testing.
func TestHandler_PostTestPreventClear(t *testing.T) {
	// Arrange: Clear recovery overrides and set up handler
	degraded.ClearRecoveryOverrides()
	defer degraded.ClearRecoveryOverrides()

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("POST", "/test/prevent_clear", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request to prevent recovery clearing
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and correct action response
	if w.Code != http.StatusOK {
		t.Errorf("PostTestPreventClear() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["action"] != "prevent_clear" {
		t.Errorf("action = %q, want prevent_clear", resp["action"])
	}
}

// TestHandler_PostTestFailClear verifies that PostTestAction with "fail_clear" action
// simulates a failed recovery attempt and returns the next recovery time.
func TestHandler_PostTestFailClear(t *testing.T) {
	// Arrange: Clear recovery overrides, reset shutdown flag, and set up handler with retry config
	degraded.ClearRecoveryOverrides()
	defer degraded.ClearRecoveryOverrides()
	lifecycle.SetShuttingDown(false)
	defer lifecycle.SetShuttingDown(false)

	healthConfig := &HealthConfig{
		DegradedRetryInitial: 1 * time.Minute,
		DegradedRetryMax:    13 * time.Minute,
	}

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("POST", "/test/fail_clear", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request to simulate failed recovery
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status, correct action, and next_recovery field present
	if w.Code != http.StatusOK {
		t.Errorf("PostTestFailClear() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["action"] != "fail_clear" {
		t.Errorf("action = %q, want fail_clear", resp["action"])
	}
	if _, ok := resp["next_recovery"]; !ok {
		t.Error("Response missing next_recovery")
	}
}

// TestHandler_PostTestClear verifies that PostTestAction with "clear" action
// clears degraded state and re-enables recovery when recovery was disabled.
func TestHandler_PostTestClear(t *testing.T) {
	// Arrange: Reset degraded state with recovery disabled and set up handler
	degraded.Reset()
	degraded.SetRecoveryDisabled(true)

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("POST", "/test/clear", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request to clear degraded state
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and correct action response
	if w.Code != http.StatusOK {
		t.Errorf("PostTestClear() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["action"] != "clear" {
		t.Errorf("action = %q, want clear", resp["action"])
	}
}

// TestHandler_PostTestAction_Unknown verifies that PostTestAction returns 404 Not Found
// when an unknown action is requested.
func TestHandler_PostTestAction_Unknown(t *testing.T) {
	// Arrange: Set up handler
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("POST", "/test/badaction", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)

	// Act: Execute POST request with unknown action
	router.ServeHTTP(w, req)

	// Assert: Verify 404 status for unknown action
	if w.Code != http.StatusNotFound {
		t.Errorf("PostTestAction(unknown) status = %d, want 404", w.Code)
	}
}
