package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/kjstillabower/weather-alert-service/internal/degraded"
	"github.com/kjstillabower/weather-alert-service/internal/idle"
	"github.com/kjstillabower/weather-alert-service/internal/lifecycle"
	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/overload"
	"github.com/kjstillabower/weather-alert-service/internal/service"
	"go.uber.org/zap"
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
