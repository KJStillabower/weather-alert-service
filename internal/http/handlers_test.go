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

func TestHandler_GetWeather_Success(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_GetWeather_EmptyLocation(t *testing.T) {
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	// Test with whitespace-only location (mux will match but handler validates)
	req := httptest.NewRequest("GET", "/weather/%20%20%20", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "logger", logger)
	ctx = context.WithValue(ctx, "correlation_id", "test-correlation-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)
	router.ServeHTTP(w, req)

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

func TestHandler_GetWeather_ServiceError(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_GetHealth(t *testing.T) {
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_InvalidAPIKey_DegradedWithLogger(t *testing.T) {
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

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_ShuttingDown(t *testing.T) {
	lifecycle.SetShuttingDown(true)
	defer lifecycle.SetShuttingDown(false)

	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_Overloaded(t *testing.T) {
	overload.Reset()
	// Threshold = 2 * 1 * 0.4 = 0.8, so 1+ requests overload
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

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_Idle(t *testing.T) {
	idle.Reset()
	// No requests recorded; uptime > minimum_lifespan; request rate below threshold

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

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_HealthyNotIdle_RecentStart(t *testing.T) {
	idle.Reset()
	// StartTime is recent; uptime < minimum_lifespan -> should be healthy, not idle
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

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_HealthyNotIdle_AboveThreshold(t *testing.T) {
	idle.Reset()
	// Record enough requests to exceed threshold; should be healthy, not idle
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

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_DegradedErrorRate(t *testing.T) {
	degraded.Reset()
	degraded.RecordError()
	degraded.RecordError()
	degraded.RecordSuccess() // 2 errors, 1 success = 66% error rate

	healthConfig := &HealthConfig{
		DegradedWindow:   1 * time.Minute,
		DegradedErrorPct: 50, // 66% > 50%
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_NotDegraded_BelowErrorThreshold(t *testing.T) {
	degraded.Reset()
	degraded.RecordSuccess()
	degraded.RecordSuccess()
	degraded.RecordError() // 1 error, 3 total = 33%

	healthConfig := &HealthConfig{
		DegradedWindow:   1 * time.Minute,
		DegradedErrorPct: 50, // 33% < 50%
	}
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.GetHealth(w, req)

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

func TestHandler_GetHealth_LogsTransition(t *testing.T) {
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

	// First call: healthy (no errors yet). Establishes prev.
	degraded.RecordSuccess()
	degraded.RecordSuccess()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.GetHealth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first GetHealth status = %d, want 200", w.Code)
	}
	if logs.Len() != 0 {
		t.Fatalf("first call should not log transition; got %d logs", logs.Len())
	}

	// Inject errors to breach threshold (66% > 50%).
	degraded.RecordError()
	degraded.RecordError()

	// Second call: degraded. Triggers transition log.
	w2 := httptest.NewRecorder()
	handler.GetHealth(w2, req)
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

	// Third call: still degraded. No new transition log.
	w3 := httptest.NewRecorder()
	handler.GetHealth(w3, req)
	if w3.Code != http.StatusServiceUnavailable {
		t.Fatalf("third GetHealth status = %d, want 503", w3.Code)
	}
	if logs.Len() != 1 {
		t.Errorf("third call (status unchanged) should not log; total logs = %d, want 1", logs.Len())
	}
}

func TestHandler_GetTestStatus(t *testing.T) {
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

	handler.GetTestStatus(w, req)

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

func TestHandler_PostTestReset(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_PostTestLoad(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_PostTestError(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_PostTestShutdown(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_PostTestPreventClear(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_PostTestFailClear(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_PostTestClear(t *testing.T) {
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
	router.ServeHTTP(w, req)

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

func TestHandler_PostTestAction_Unknown(t *testing.T) {
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	req := httptest.NewRequest("POST", "/test/badaction", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/test/{action}", handler.PostTestAction)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("PostTestAction(unknown) status = %d, want 404", w.Code)
	}
}
