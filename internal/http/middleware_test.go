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
)

func TestMiddleware_ThroughHandler(t *testing.T) {
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

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("X-Correlation-ID") == "" {
		t.Error("X-Correlation-ID header missing")
	}
}

func TestMiddleware_CorrelationIDPropagated(t *testing.T) {
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

	router.ServeHTTP(w, req)

	if got := w.Header().Get("X-Correlation-ID"); got != "client-provided-id" {
		t.Errorf("X-Correlation-ID = %q, want client-provided-id", got)
	}
}

func TestMiddleware_MetricsRecordsNonOK(t *testing.T) {
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

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestMiddleware_HealthThroughChain(t *testing.T) {
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

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestTimeoutMiddleware_CancelsContextAfterTimeout(t *testing.T) {
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

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (timeout should cause upstream error)", w.Code, http.StatusServiceUnavailable)
	}
}

func TestRateLimitMiddleware_Returns429WhenExceeded(t *testing.T) {
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

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/weather/seattle", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

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

func TestRateLimitMiddleware_NilLimiterPassesThrough(t *testing.T) {
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
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (nil limiter should allow)", w.Code)
	}
}

func TestMiddleware_GetRouteDefaultPath(t *testing.T) {
	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(zap.NewNop()))
	router.Use(MetricsMiddleware)
	router.HandleFunc("/foo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/foo", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestMiddleware_MetricsRoute(t *testing.T) {
	router := mux.NewRouter()
	router.Use(CorrelationIDMiddleware(zap.NewNop()))
	router.Use(MetricsMiddleware)
	router.Handle("/metrics", observability.MetricsHandler())

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestSubrouter_WeatherRouteWithTimeoutAndRateLimit(t *testing.T) {
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
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (subrouter should route /weather/{location})", w.Code)
	}
}
