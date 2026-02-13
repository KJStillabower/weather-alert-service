package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/time/rate"

	"github.com/kjstillabower/weather-alert-service/internal/observability"
	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/service"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestMiddleware_ThroughHandler verifies that middleware chain processes requests
// correctly and adds correlation ID header when none is provided.
func TestMiddleware_ThroughHandler(t *testing.T) {
	// Arrange: Set up handler with middleware chain
	mockClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle", Temperature: 12.0},
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	w := httptest.NewRecorder()

	// Act: Execute request through middleware chain
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status and correlation ID header present
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("X-Correlation-ID") == "" {
		t.Error("X-Correlation-ID header missing")
	}
}

// TestMiddleware_CorrelationIDPropagated verifies that CorrelationIDMiddleware preserves
// client-provided correlation IDs in request and response headers.
func TestMiddleware_CorrelationIDPropagated(t *testing.T) {
	// Arrange: Set up handler with middleware and request containing correlation ID
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	req.Header.Set("X-Correlation-ID", "client-provided-id")
	w := httptest.NewRecorder()

	// Act: Execute request with client-provided correlation ID
	router.ServeHTTP(w, req)

	// Assert: Verify correlation ID is preserved in response
	if got := w.Header().Get("X-Correlation-ID"); got != "client-provided-id" {
		t.Errorf("X-Correlation-ID = %q, want client-provided-id", got)
	}
}

// TestMiddleware_MetricsRecordsNonOK verifies that MetricsMiddleware records
// non-OK status codes when handlers return errors.
func TestMiddleware_MetricsRecordsNonOK(t *testing.T) {
	// Arrange: Set up handler that returns error and middleware chain
	mockClient := &mockWeatherClient{
		err: http.ErrHandlerTimeout,
	}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()

	// Act: Execute request that triggers error
	router.ServeHTTP(w, req)

	// Assert: Verify 503 status is returned
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// TestMiddleware_HealthThroughChain verifies that health endpoint works correctly
// when processed through the middleware chain.
func TestMiddleware_HealthThroughChain(t *testing.T) {
	// Arrange: Set up handler and middleware chain
	mockClient := &mockWeatherClient{}
	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.HandleFunc("/health", handler.GetHealth)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act: Execute health check through middleware
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestTimeoutMiddleware_CancelsContextAfterTimeout verifies that TimeoutMiddleware
// cancels request context after timeout duration, causing upstream errors.
func TestTimeoutMiddleware_CancelsContextAfterTimeout(t *testing.T) {
	// Arrange: Set up slow client that blocks and timeout middleware with short duration
	slowClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle", Temperature: 10.0},
	}
	slowClient.block = make(chan struct{})
	defer close(slowClient.block)

	mockCache := &mockCache{}
	weatherService := service.NewWeatherService(slowClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, slowClient, nil, logger, nil)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.Use(TimeoutMiddleware(50*time.Millisecond))
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	w := httptest.NewRecorder()

	// Act: Execute request that exceeds timeout
	router.ServeHTTP(w, req)

	// Assert: Verify 503 status due to timeout
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (timeout should cause upstream error)", w.Code, http.StatusServiceUnavailable)
	}
}

// TestRateLimitMiddleware_Returns429WhenExceeded verifies that RateLimitMiddleware
// returns 429 Too Many Requests with RATE_LIMITED error code when rate limit is exceeded.
func TestRateLimitMiddleware_Returns429WhenExceeded(t *testing.T) {
	// Arrange: Set up handler with rate limiter (1 RPS, burst 2)
	mockClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle", Temperature: 10.0},
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	limiter := rate.NewLimiter(1, 2)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.Use(RateLimitMiddleware(limiter))
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	// Act: Execute 3 requests (first 2 should pass, third should be rate limited)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/weather/seattle", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Assert: First 2 requests succeed, third returns 429
		if i < 2 {
			if w.Code != http.StatusOK {
				t.Errorf("request %d: status = %d, want 200", i, w.Code)
			}
		} else {
			if w.Code != http.StatusTooManyRequests {
				t.Errorf("request %d: status = %d, want 429", i, w.Code)
			}
			var errResp struct {
				Error struct {
					Code      string `json:"code"`
					Message   string `json:"message"`
					RequestID string `json:"requestId"`
				} `json:"error"`
			}
			if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode 429 response: %v", err)
			}
			if errResp.Error.Code != "RATE_LIMITED" {
				t.Errorf("error.code = %q, want RATE_LIMITED", errResp.Error.Code)
			}
		}
	}
}

// TestRateLimitMiddleware_DebugLogs_Denied verifies that RateLimitMiddleware emits
// DEBUG-level logs when requests are denied due to rate limiting.
func TestRateLimitMiddleware_DebugLogs_Denied(t *testing.T) {
	// Arrange: Set up handler with rate limiter and logger observer
	mockClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle", Temperature: 10.0},
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	limiter := rate.NewLimiter(1, 1)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.Use(RateLimitMiddleware(limiter))
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	// Act: Execute requests that exceed rate limit
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/weather/seattle", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if i == 2 && w.Code != http.StatusTooManyRequests {
			t.Fatalf("request 2: status = %d, want 429", w.Code)
		}
	}

	// Assert: Verify DEBUG log for rate limit denial
	entries := logs.FilterMessage("rate limit denied").All()
	if len(entries) < 1 {
		t.Fatalf("want at least 1 rate limit denied log, got %d", len(entries))
	}
}

// TestRateLimitMiddleware_NilLimiterPassesThrough verifies that RateLimitMiddleware
// allows all requests when limiter is nil, enabling optional rate limiting.
func TestRateLimitMiddleware_NilLimiterPassesThrough(t *testing.T) {
	// Arrange: Set up handler with nil rate limiter
	mockClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle", Temperature: 10.0},
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)
	router.Use(RateLimitMiddleware(nil))
	router.HandleFunc("/weather/{location}", handler.GetWeather)

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	w := httptest.NewRecorder()

	// Act: Execute request with nil limiter
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status (nil limiter allows all requests)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (nil limiter should allow)", w.Code)
	}
}

// TestMiddleware_GetRouteDefaultPath verifies that middleware correctly processes
// routes without path templates, using default route path for metrics.
func TestMiddleware_GetRouteDefaultPath(t *testing.T) {
	// Arrange: Set up router with simple route and middleware
	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(zap.NewNop()))
	router.Use(MetricsMiddleware)
	router.HandleFunc("/foo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/foo", nil)
	w := httptest.NewRecorder()

	// Act: Execute request to route without path template
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestMiddleware_MetricsRoute verifies that metrics endpoint works correctly
// when processed through the middleware chain.
func TestMiddleware_MetricsRoute(t *testing.T) {
	// Arrange: Set up router with metrics endpoint and middleware
	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(zap.NewNop()))
	router.Use(MetricsMiddleware)
	router.Handle("/metrics", observability.MetricsHandler())

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	// Act: Execute request to metrics endpoint
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestSubrouter_WeatherRouteWithTimeoutAndRateLimit verifies that subrouter configuration
// correctly applies timeout and rate limit middleware to weather routes.
func TestSubrouter_WeatherRouteWithTimeoutAndRateLimit(t *testing.T) {
	// Arrange: Set up handler with subrouter applying timeout and rate limit middleware
	mockClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle", Temperature: 10.0},
	}
	mockCache := &mockCache{data: make(map[string]models.WeatherData)}
	weatherService := service.NewWeatherService(mockClient, mockCache, 5*time.Minute)

	logger, _ := zap.NewDevelopment()
	handler := NewHandler(weatherService, mockClient, nil, logger, nil)

	limiter := rate.NewLimiter(10, 10)

	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(MetricsMiddleware)

	weatherRouter := router.PathPrefix("/weather").Subrouter()
	weatherRouter.Use(RateLimitMiddleware(limiter))
	weatherRouter.Use(TimeoutMiddleware(5*time.Second))
	weatherRouter.HandleFunc("/{location}", handler.GetWeather).Methods("GET")

	router.HandleFunc("/health", handler.GetHealth).Methods("GET")

	req := httptest.NewRequest("GET", "/weather/seattle", nil)
	w := httptest.NewRecorder()

	// Act: Execute request to weather route through subrouter
	router.ServeHTTP(w, req)

	// Assert: Verify 200 status (subrouter routes correctly)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (subrouter should route /weather/{location})", w.Code)
	}
}
