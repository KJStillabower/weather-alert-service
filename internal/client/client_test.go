package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

func TestNewOpenWeatherClient_InvalidAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantErr error
	}{
		{
			name:    "empty API key",
			apiKey:  "",
			wantErr: ErrInvalidAPIKey,
		},
		{
			name:    "too short API key",
			apiKey:  "short",
			wantErr: ErrInvalidAPIKey,
		},
		{
			name:    "valid API key",
			apiKey:  "valid-api-key-12345",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewOpenWeatherClient(tt.apiKey, "https://api.test.com", 2*time.Second)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("NewOpenWeatherClient() expected error, got nil")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("NewOpenWeatherClient() error = %v, want %v", err, tt.wantErr)
				}
				if client != nil {
					t.Errorf("NewOpenWeatherClient() expected nil client on error")
				}
			} else {
				if err != nil {
					t.Fatalf("NewOpenWeatherClient() unexpected error: %v", err)
				}
				if client == nil {
					t.Fatalf("NewOpenWeatherClient() expected client, got nil")
				}
			}
		})
	}
}

func TestOpenWeatherClient_GetCurrentWeather_Success(t *testing.T) {
	apiResp := map[string]interface{}{
		"name": "Seattle",
		"main": map[string]interface{}{
			"temp":     15.5,
			"humidity": 65,
		},
		"weather": []map[string]interface{}{
			{
				"main":        "Clouds",
				"description": "scattered clouds",
			},
		},
		"wind": map[string]interface{}{
			"speed": 3.2,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.Contains(r.URL.RawQuery, "q=seattle") {
			t.Errorf("expected location in query, got %s", r.URL.RawQuery)
		}
		if !strings.Contains(r.URL.RawQuery, "appid=") {
			t.Errorf("expected API key in query")
		}
		if !strings.Contains(r.URL.RawQuery, "units=metric") {
			t.Errorf("expected units=metric in query")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	client, err := NewOpenWeatherClient("test-api-key-12345", server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.Background()
	got, err := client.GetCurrentWeather(ctx, "seattle")
	if err != nil {
		t.Fatalf("GetCurrentWeather() error = %v", err)
	}

	if got.Location != "seattle" {
		t.Errorf("Location = %q, want %q", got.Location, "seattle")
	}
	if got.Temperature != 15.5 {
		t.Errorf("Temperature = %f, want %f", got.Temperature, 15.5)
	}
	if got.Conditions != "scattered clouds" {
		t.Errorf("Conditions = %q, want %q", got.Conditions, "scattered clouds")
	}
	if got.Humidity != 65 {
		t.Errorf("Humidity = %d, want %d", got.Humidity, 65)
	}
	if got.WindSpeed != 3.2 {
		t.Errorf("WindSpeed = %f, want %f", got.WindSpeed, 3.2)
	}
}

func TestOpenWeatherClient_GetCurrentWeather_ErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		wantErr        error
		retryable      bool
		setupHandler   func(*testing.T) http.HandlerFunc
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			wantErr:    ErrInvalidAPIKey,
			retryable:  false,
			setupHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}
			},
		},
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			wantErr:    ErrLocationNotFound,
			retryable:  false,
			setupHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name:       "429 rate limited",
			statusCode: http.StatusTooManyRequests,
			wantErr:    ErrRateLimited,
			retryable:  true,
			setupHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusTooManyRequests)
				}
			},
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    ErrUpstreamFailure,
			retryable:  true,
			setupHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
		},
		{
			name:       "502 bad gateway",
			statusCode: http.StatusBadGateway,
			wantErr:    ErrUpstreamFailure,
			retryable:  true,
			setupHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadGateway)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.setupHandler(t))
			defer server.Close()

			client, err := NewOpenWeatherClientWithRetry("test-api-key-12345", server.URL, 2*time.Second, 1, 10*time.Millisecond, 100*time.Millisecond)
			if err != nil {
				t.Fatalf("NewOpenWeatherClientWithRetry() error = %v", err)
			}

			ctx := context.Background()
			_, err = client.GetCurrentWeather(ctx, "test")
			if err == nil {
				t.Fatalf("GetCurrentWeather() expected error, got nil")
			}

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("GetCurrentWeather() error = %v, want %v", err, tt.wantErr)
			}

			if tt.retryable && !client.isRetryable(err) {
				t.Errorf("isRetryable() = false, want true for %v", err)
			}
			if !tt.retryable && client.isRetryable(err) {
				t.Errorf("isRetryable() = true, want false for %v", err)
			}
		})
	}
}

func TestOpenWeatherClient_GetCurrentWeather_RetryLogic(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		apiResp := map[string]interface{}{
			"name": "Seattle",
			"main": map[string]interface{}{
				"temp":     15.5,
				"humidity": 65,
			},
			"weather": []map[string]interface{}{
				{
					"main":        "Clouds",
					"description": "scattered clouds",
				},
			},
			"wind": map[string]interface{}{
				"speed": 3.2,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	client, err := NewOpenWeatherClientWithRetry("test-api-key-12345", server.URL, 2*time.Second, 3, 10*time.Millisecond, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewOpenWeatherClientWithRetry() error = %v", err)
	}

	ctx := context.Background()
	got, err := client.GetCurrentWeather(ctx, "seattle")
	if err != nil {
		t.Fatalf("GetCurrentWeather() error = %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if got.Location != "seattle" {
		t.Errorf("Location = %q, want %q", got.Location, "seattle")
	}
}

func TestOpenWeatherClient_GetCurrentWeather_NoRetryOnNonRetryableError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := NewOpenWeatherClientWithRetry("test-api-key-12345", server.URL, 2*time.Second, 3, 10*time.Millisecond, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewOpenWeatherClientWithRetry() error = %v", err)
	}

	ctx := context.Background()
	_, err = client.GetCurrentWeather(ctx, "test")
	if err == nil {
		t.Fatalf("GetCurrentWeather() expected error, got nil")
	}

	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry), got %d", attempts)
	}
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Errorf("GetCurrentWeather() error = %v, want %v", err, ErrInvalidAPIKey)
	}
}

func TestOpenWeatherClient_GetCurrentWeather_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewOpenWeatherClient("test-api-key-12345", server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetCurrentWeather(ctx, "test")
	if err == nil {
		t.Fatalf("GetCurrentWeather() expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("GetCurrentWeather() error = %v, want context.Canceled", err)
	}
}

func TestOpenWeatherClient_GetCurrentWeather_CorrelationID(t *testing.T) {
	var capturedCorrID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCorrID = r.Header.Get("X-Correlation-ID")

		apiResp := map[string]interface{}{
			"name": "Seattle",
			"main": map[string]interface{}{
				"temp":     15.5,
				"humidity": 65,
			},
			"weather": []map[string]interface{}{
				{
					"main":        "Clouds",
					"description": "scattered clouds",
				},
			},
			"wind": map[string]interface{}{
				"speed": 3.2,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	client, err := NewOpenWeatherClient("test-api-key-12345", server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.WithValue(context.Background(), "correlation_id", "test-correlation-id-123")
	_, err = client.GetCurrentWeather(ctx, "seattle")
	if err != nil {
		t.Fatalf("GetCurrentWeather() error = %v", err)
	}

	if capturedCorrID != "test-correlation-id-123" {
		t.Errorf("X-Correlation-ID header = %q, want %q", capturedCorrID, "test-correlation-id-123")
	}
}

func TestOpenWeatherClient_mapResponse(t *testing.T) {
	tests := []struct {
		name     string
		apiResp  openWeatherResponse
		location string
		want     models.WeatherData
	}{
		{
			name: "full response",
			apiResp: openWeatherResponse{
				Name: "Seattle",
				Main: struct {
					Temp     float64 `json:"temp"`
					Humidity int     `json:"humidity"`
				}{
					Temp:     15.5,
					Humidity: 65,
				},
				Weather: []struct {
					Main        string `json:"main"`
					Description string `json:"description"`
				}{
					{
						Main:        "Clouds",
						Description: "scattered clouds",
					},
				},
				Wind: struct {
					Speed float64 `json:"speed"`
				}{
					Speed: 3.2,
				},
			},
			location: "seattle",
			want: models.WeatherData{
				Location:    "seattle",
				Temperature: 15.5,
				Conditions:  "scattered clouds",
				Humidity:    65,
				WindSpeed:   3.2,
			},
		},
		{
			name: "no description uses main",
			apiResp: openWeatherResponse{
				Name: "Portland",
				Main: struct {
					Temp     float64 `json:"temp"`
					Humidity int     `json:"humidity"`
				}{
					Temp:     20.0,
					Humidity: 50,
				},
				Weather: []struct {
					Main        string `json:"main"`
					Description string `json:"description"`
				}{
					{
						Main:        "Clear",
						Description: "",
					},
				},
				Wind: struct {
					Speed float64 `json:"speed"`
				}{
					Speed: 2.5,
				},
			},
			location: "portland",
			want: models.WeatherData{
				Location:    "portland",
				Temperature: 20.0,
				Conditions:  "Clear",
				Humidity:    50,
				WindSpeed:   2.5,
			},
		},
		{
			name: "empty name uses location",
			apiResp: openWeatherResponse{
				Name: "",
				Main: struct {
					Temp     float64 `json:"temp"`
					Humidity int     `json:"humidity"`
				}{
					Temp:     10.0,
					Humidity: 70,
				},
				Weather: []struct {
					Main        string `json:"main"`
					Description string `json:"description"`
				}{
					{
						Main:        "Rain",
						Description: "light rain",
					},
				},
				Wind: struct {
					Speed float64 `json:"speed"`
				}{
					Speed: 1.0,
				},
			},
			location: "unknown",
			want: models.WeatherData{
				Location:    "unknown",
				Temperature: 10.0,
				Conditions:  "light rain",
				Humidity:    70,
				WindSpeed:   1.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &OpenWeatherClient{}
			got := client.mapResponse(tt.apiResp, tt.location)

			if got.Location != tt.want.Location {
				t.Errorf("Location = %q, want %q", got.Location, tt.want.Location)
			}
			if got.Temperature != tt.want.Temperature {
				t.Errorf("Temperature = %f, want %f", got.Temperature, tt.want.Temperature)
			}
			if got.Conditions != tt.want.Conditions {
				t.Errorf("Conditions = %q, want %q", got.Conditions, tt.want.Conditions)
			}
			if got.Humidity != tt.want.Humidity {
				t.Errorf("Humidity = %d, want %d", got.Humidity, tt.want.Humidity)
			}
			if got.WindSpeed != tt.want.WindSpeed {
				t.Errorf("WindSpeed = %f, want %f", got.WindSpeed, tt.want.WindSpeed)
			}
		})
	}
}

func TestOpenWeatherClient_calculateBackoff(t *testing.T) {
	client := &OpenWeatherClient{
		retryBaseDelay: 100 * time.Millisecond,
		retryMaxDelay:  2 * time.Second,
	}

	tests := []struct {
		name    string
		attempt int
		wantMax time.Duration
	}{
		{
			name:    "first retry",
			attempt: 1,
			wantMax: 200 * time.Millisecond,
		},
		{
			name:    "second retry",
			attempt: 2,
			wantMax: 400 * time.Millisecond,
		},
		{
			name:    "third retry",
			attempt: 3,
			wantMax: 2 * time.Second,
		},
		{
			name:    "fourth retry capped",
			attempt: 4,
			wantMax: 2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.calculateBackoff(tt.attempt)
			if got > tt.wantMax {
				t.Errorf("calculateBackoff(%d) = %v, want <= %v", tt.attempt, got, tt.wantMax)
			}
			if got <= 0 {
				t.Errorf("calculateBackoff(%d) = %v, want > 0", tt.attempt, got)
			}
		})
	}
}

func TestOpenWeatherClient_GetCurrentWeather_ExhaustedRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewOpenWeatherClientWithRetry("test-api-key-12345", server.URL, 2*time.Second, 2, 10*time.Millisecond, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewOpenWeatherClientWithRetry() error = %v", err)
	}

	ctx := context.Background()
	_, err = client.GetCurrentWeather(ctx, "test")
	if err == nil {
		t.Fatalf("GetCurrentWeather() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "exhausted retries") {
		t.Errorf("GetCurrentWeather() error = %v, want 'exhausted retries'", err)
	}
	if !errors.Is(err, ErrUpstreamFailure) {
		t.Errorf("GetCurrentWeather() error = %v, want ErrUpstreamFailure", err)
	}
}

func TestOpenWeatherClient_GetCurrentWeather_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client, err := NewOpenWeatherClient("test-api-key-12345", server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.Background()
	_, err = client.GetCurrentWeather(ctx, "test")
	if err == nil {
		t.Fatalf("GetCurrentWeather() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("GetCurrentWeather() error = %v, want 'parse response'", err)
	}
}

func TestOpenWeatherClient_GetCurrentWeather_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewOpenWeatherClient("test-api-key-12345", server.URL, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.Background()
	_, err = client.GetCurrentWeather(ctx, "test")
	if err == nil {
		t.Fatalf("GetCurrentWeather() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("GetCurrentWeather() error = %v, want 'timeout'", err)
	}
}

func TestOpenWeatherClient_ValidateAPIKey(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "401 invalid key",
			statusCode: http.StatusUnauthorized,
			wantErr:    true,
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client, err := NewOpenWeatherClient("test-api-key-12345", server.URL, 2*time.Second)
			if err != nil {
				t.Fatalf("NewOpenWeatherClient() error = %v", err)
			}

			ctx := context.Background()
			err = client.ValidateAPIKey(ctx)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ValidateAPIKey() expected error, got nil")
				}
				if tt.statusCode == http.StatusUnauthorized && !errors.Is(err, ErrInvalidAPIKey) {
					t.Errorf("ValidateAPIKey() error = %v, want ErrInvalidAPIKey", err)
				}
			} else {
				if err != nil {
					t.Fatalf("ValidateAPIKey() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestOpenWeatherClient_GetCurrentWeather_InvalidURL(t *testing.T) {
	client, err := NewOpenWeatherClient("test-api-key-12345", "://invalid", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.Background()
	_, err = client.GetCurrentWeather(ctx, "test")
	if err == nil {
		t.Fatal("GetCurrentWeather() expected error for invalid URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid API URL") && !strings.Contains(err.Error(), "build request") {
		t.Errorf("GetCurrentWeather() error = %v, want 'invalid API URL' or 'build request'", err)
	}
}

func TestOpenWeatherClient_handleErrorResponse_503_504(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{"503", http.StatusServiceUnavailable, ErrUpstreamFailure},
		{"504", http.StatusGatewayTimeout, ErrUpstreamFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client, err := NewOpenWeatherClient("test-api-key-12345", server.URL, 2*time.Second)
			if err != nil {
				t.Fatalf("NewOpenWeatherClient() error = %v", err)
			}

			ctx := context.Background()
			_, err = client.GetCurrentWeather(ctx, "test")
			if err == nil {
				t.Fatal("GetCurrentWeather() expected error, got nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("GetCurrentWeather() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenWeatherClient_isRetryable_TimeoutErrors(t *testing.T) {
	client := &OpenWeatherClient{}
	tests := []struct {
		name  string
		err   error
		want  bool
	}{
		{"timeout in message", errors.New("request timeout: context deadline exceeded"), true},
		{"context canceled", errors.New("context canceled"), true},
		{"nil", nil, false},
		{"non-retryable", ErrInvalidAPIKey, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.isRetryable(tt.err)
			if got != tt.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestCoverageGaps_IntentionallyUntested documents paths we reviewed but chose not to test.
// Run with -v to see skip reasons.
func TestCoverageGaps_IntentionallyUntested(t *testing.T) {
	t.Run("callAPI_clientDo_non_timeout_error", func(t *testing.T) {
		t.Skip("http.Client.Do returning non-timeout error (e.g. connection refused) requires network isolation; covered by integration tests")
	})
	t.Run("calculateBackoff_delay_cap_and_jitter", func(t *testing.T) {
		t.Skip("delay > maxDelay cap and jitter are internal to retry loop; testing would require controlling rand or extracting for injection")
	})
	t.Run("buildRequest_NewRequestWithContext_error", func(t *testing.T) {
		t.Skip("http.NewRequestWithContext error is effectively unreachable with valid URL; would need exotic invalid URL")
	})
	t.Run("handleErrorResponse_401_404_429_branches", func(t *testing.T) {
		t.Skip("401, 404, 429 branches are tested via handleErrorResponse table; remaining 12.5% is edge-case status handling")
	})
	t.Run("statusLabel_fallback_error", func(t *testing.T) {
		t.Skip("statusLabel fallback for status < 200 or >= 600 is edge case; API returns 2xx/4xx/5xx")
	})
	t.Run("ValidateAPIKey_401_vs_non200", func(t *testing.T) {
		t.Skip("ValidateAPIKey 401 vs generic non-200 branches; integration test covers happy path")
	})
}
