package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/kjstillabower/weather-alert-service/internal/client"
	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
	"github.com/kjstillabower/weather-alert-service/internal/service"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// setupBenchmarkHandler creates a handler with mocks for benchmarking.
func setupBenchmarkHandler() *Handler {
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)
	logger, _ := zap.NewDevelopment()
	return NewHandler(weatherService, mockClient, nil, logger, nil, 100, 1)
}

// setupBenchmarkHandlerWithCacheHit creates a handler with cache pre-populated.
func setupBenchmarkHandlerWithCacheHit() *Handler {
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)
	
	// Pre-populate cache
	testData := models.WeatherData{
		Location:    "seattle",
		Temperature: 15.5,
		Conditions:  "Clear",
		Humidity:    65,
		WindSpeed:   10.2,
		Timestamp:   time.Now(),
	}
	mockCache.Set(context.Background(), "seattle", testData, 5*time.Minute)
	
	logger, _ := zap.NewDevelopment()
	return NewHandler(weatherService, mockClient, nil, logger, nil, 100, 1)
}

// createBenchmarkRequest creates an HTTP request for benchmarking.
func createBenchmarkRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	logger, _ := zap.NewDevelopment()
	req = req.WithContext(context.WithValue(req.Context(), "correlation_id", "bench-id"))
	req = req.WithContext(context.WithValue(req.Context(), "logger", logger))
	return req
}

// BenchmarkHandler_GetWeather_CacheHit benchmarks handler with cache hit.
func BenchmarkHandler_GetWeather_CacheHit(b *testing.B) {
	handler := setupBenchmarkHandlerWithCacheHit()
	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := createBenchmarkRequest("GET", "/weather/seattle")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkHandler_GetWeather_CacheMiss benchmarks handler with cache miss.
func BenchmarkHandler_GetWeather_CacheMiss(b *testing.B) {
	mockClient := &mockWeatherClient{
		weather: models.WeatherData{
			Location:    "seattle",
			Temperature: 15.5,
			Conditions:  "Clear",
			Humidity:    65,
			WindSpeed:   10.2,
			Timestamp:   time.Now(),
		},
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil, 100, 1)

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := createBenchmarkRequest("GET", "/weather/seattle")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkHandler_GetWeather_Error benchmarks handler error handling.
func BenchmarkHandler_GetWeather_Error(b *testing.B) {
	mockClient := &mockWeatherClient{
		err: client.ErrUpstreamFailure,
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil, 100, 1)

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := createBenchmarkRequest("GET", "/weather/seattle")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkHandler_GetWeather_ValidationError benchmarks validation error handling.
func BenchmarkHandler_GetWeather_ValidationError(b *testing.B) {
	handler := setupBenchmarkHandler()
	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := createBenchmarkRequest("GET", "/weather/")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkHandler_GetWeather_RateLimited benchmarks rate limiting overhead.
func BenchmarkHandler_GetWeather_RateLimited(b *testing.B) {
	limiter := rate.NewLimiter(rate.Limit(100), 250)
	handler := setupBenchmarkHandler()
	handler.rateLimiter = limiter

	router := mux.NewRouter()
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := createBenchmarkRequest("GET", "/weather/seattle")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkHandler_GetHealth benchmarks health check endpoint.
func BenchmarkHandler_GetHealth(b *testing.B) {
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)
	
	healthConfig := &HealthConfig{
		OverloadWindow:         60 * time.Second,
		OverloadThresholdPct:  80,
		RateLimitRPS:          100,
		RateLimitBurst:        250,
		DegradedWindow:        5 * time.Minute,
		DegradedErrorPct:      5,
		DegradedRetryInitial:  1 * time.Second,
		DegradedRetryMax:      30 * time.Second,
		IdleWindow:            10 * time.Minute,
		IdleThresholdReqPerMin: 1,
		MinimumLifespan:       5 * time.Minute,
		StartTime:              time.Now(),
	}
	
	logger, _ := observability.NewLogger()
	handler := NewHandler(weatherService, mockClient, healthConfig, logger, nil, 100, 1)

	router := mux.NewRouter()
	router.HandleFunc("/health", handler.GetHealth)

	req := createBenchmarkRequest("GET", "/health")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}
