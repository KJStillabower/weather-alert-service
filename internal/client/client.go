package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/circuitbreaker"
	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
)

// WeatherClient defines the interface for weather data providers.
// Implementations must provide weather data retrieval and API key validation.
type WeatherClient interface {
	GetCurrentWeather(ctx context.Context, location string) (models.WeatherData, error)
	ValidateAPIKey(ctx context.Context) error
}

var (
	// ErrInvalidAPIKey indicates the API key is invalid, missing, or not activated.
	ErrInvalidAPIKey = errors.New("invalid API key")
	// ErrLocationNotFound indicates the requested location was not found by the upstream API.
	ErrLocationNotFound = errors.New("location not found")
	// ErrUpstreamFailure indicates a transient upstream API failure (5xx errors, timeouts).
	ErrUpstreamFailure = errors.New("upstream failure")
	// ErrRateLimited indicates the upstream API rate limit was exceeded (429).
	ErrRateLimited = errors.New("rate limited")
)

// OpenWeatherClient implements WeatherClient for OpenWeatherMap API.
// Provides retry logic with exponential backoff for transient failures.
// Optional circuitBreaker wraps upstream calls when set.
type OpenWeatherClient struct {
	apiKey         string
	apiURL         string
	timeout        time.Duration
	client         *http.Client
	retryAttempts  int
	retryBaseDelay time.Duration
	retryMaxDelay  time.Duration
	circuitBreaker *circuitbreaker.CircuitBreaker
}

// NewOpenWeatherClient creates a new OpenWeatherClient with default retry settings
// (3 attempts, 100ms base delay, 2s max delay).
func NewOpenWeatherClient(apiKey, apiURL string, timeout time.Duration) (*OpenWeatherClient, error) {
	return NewOpenWeatherClientWithRetry(apiKey, apiURL, timeout, 3, 100*time.Millisecond, 2*time.Second)
}

// NewOpenWeatherClientWithRetry creates a new OpenWeatherClient with configurable retry settings.
// Validates API key format (non-empty, minimum 10 characters) before creating client.
func NewOpenWeatherClientWithRetry(apiKey, apiURL string, timeout time.Duration, retryAttempts int, retryBaseDelay, retryMaxDelay time.Duration) (*OpenWeatherClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("%w: API key is required", ErrInvalidAPIKey)
	}
	if len(apiKey) < 10 {
		return nil, fmt.Errorf("%w: API key appears invalid (too short)", ErrInvalidAPIKey)
	}

	return &OpenWeatherClient{
		apiKey:        apiKey,
		apiURL:        apiURL,
		timeout:       timeout,
		retryAttempts: retryAttempts,
		retryBaseDelay: retryBaseDelay,
		retryMaxDelay:  retryMaxDelay,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// openWeatherResponse is the JSON shape returned by the OpenWeatherMap API for current weather.
type openWeatherResponse struct {
	Main struct {
		Temp     float64 `json:"temp"`
		Humidity int     `json:"humidity"`
	} `json:"main"`
	Weather []struct {
		Main        string `json:"main"`
		Description string `json:"description"`
	} `json:"weather"`
	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`
	Name string `json:"name"`
}

// GetCurrentWeather retrieves weather data for the specified location with retry logic.
// Retries on transient failures (timeouts, rate limits, 5xx errors) using exponential backoff.
// Returns immediately on non-retryable errors (4xx except 429). Respects context cancellation.
// Propagates request context deadline to upstream calls so upstream timeout does not exceed remaining request budget.
func (c *OpenWeatherClient) GetCurrentWeather(ctx context.Context, location string) (models.WeatherData, error) {
	upstreamTimeout := c.upstreamTimeoutFromContext(ctx)
	observability.RequestTimeoutPropagatedTotal.WithLabelValues(propagatedLabel(upstreamTimeout != c.timeout)).Inc()

	if c.circuitBreaker != nil {
		var result models.WeatherData
		var err error
		cbErr := c.circuitBreaker.Call(ctx, func() error {
			result, err = c.getCurrentWeatherWithRetry(ctx, location, upstreamTimeout)
			return err
		})
		if cbErr != nil {
			return models.WeatherData{}, fmt.Errorf("circuit breaker: %w", cbErr)
		}
		return result, err
	}
	return c.getCurrentWeatherWithRetry(ctx, location, upstreamTimeout)
}

// getCurrentWeatherWithRetry runs the retry loop for fetching weather. Used by GetCurrentWeather with or without circuit breaker.
func (c *OpenWeatherClient) getCurrentWeatherWithRetry(ctx context.Context, location string, upstreamTimeout time.Duration) (models.WeatherData, error) {
	var lastErr error
	for attempt := 0; attempt < c.retryAttempts; attempt++ {
		if attempt > 0 {
			observability.WeatherAPIRetriesTotal.Inc()
			delay := c.calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return models.WeatherData{}, ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := c.callAPI(ctx, location, upstreamTimeout)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !c.isRetryable(err) {
			return models.WeatherData{}, err
		}
	}
	return models.WeatherData{}, fmt.Errorf("exhausted retries: %w", lastErr)
}

// SetCircuitBreaker attaches an optional circuit breaker to the client.
// When set, GetCurrentWeather runs the upstream call inside the breaker.
func (c *OpenWeatherClient) SetCircuitBreaker(cb *circuitbreaker.CircuitBreaker) {
	c.circuitBreaker = cb
}

// upstreamTimeoutFromContext returns the timeout to use for upstream API calls.
// If ctx has a deadline, uses 90% of remaining time, capped at c.timeout and min 100ms.
func (c *OpenWeatherClient) upstreamTimeoutFromContext(ctx context.Context) time.Duration {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		return c.timeout
	}
	remaining := time.Until(deadline)
	upstreamTimeout := time.Duration(float64(remaining) * 0.9)
	if upstreamTimeout > c.timeout {
		upstreamTimeout = c.timeout
	}
	if upstreamTimeout < 100*time.Millisecond {
		upstreamTimeout = 100 * time.Millisecond
	}
	return upstreamTimeout
}

func propagatedLabel(propagated bool) string {
	if propagated {
		return "yes"
	}
	return "no"
}

// callAPI executes a single API request to fetch weather data for the location.
// Propagates correlation ID from context, records metrics, and handles HTTP errors.
// timeout is the maximum duration for this single request (may be derived from request context).
func (c *OpenWeatherClient) callAPI(ctx context.Context, location string, timeout time.Duration) (models.WeatherData, error) {
	start := time.Now()

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := c.buildRequest(reqCtx, location)
	if err != nil {
		observability.WeatherAPICallsTotal.WithLabelValues("error").Inc()
		observability.WeatherAPIErrorsTotal.WithLabelValues(string(CategorizeError(err))).Inc()
		return models.WeatherData{}, fmt.Errorf("build request: %w", err)
	}

	corrID := extractCorrelationID(ctx)
	if corrID != "" {
		req.Header.Set("X-Correlation-ID", corrID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		duration := time.Since(start).Seconds()
		observability.WeatherAPICallsTotal.WithLabelValues("error").Inc()
		observability.WeatherAPIDuration.WithLabelValues("error").Observe(duration)
		observability.WeatherAPIErrorsTotal.WithLabelValues(string(CategorizeError(err))).Inc()
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return models.WeatherData{}, fmt.Errorf("request timeout: %w", err)
		}
		return models.WeatherData{}, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start).Seconds()
	status := statusLabel(resp.StatusCode)
	observability.WeatherAPICallsTotal.WithLabelValues(status).Inc()
	observability.WeatherAPIDuration.WithLabelValues(status).Observe(duration)

	if err := c.handleErrorResponse(resp); err != nil {
		observability.WeatherAPIErrorsTotal.WithLabelValues(string(CategorizeError(err))).Inc()
		return models.WeatherData{}, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		observability.WeatherAPICallsTotal.WithLabelValues("error").Inc()
		observability.WeatherAPIErrorsTotal.WithLabelValues(string(CategorizeError(err))).Inc()
		return models.WeatherData{}, fmt.Errorf("read response body: %w", err)
	}

	var apiResp openWeatherResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		observability.WeatherAPICallsTotal.WithLabelValues("error").Inc()
		observability.WeatherAPIErrorsTotal.WithLabelValues(string(CategorizeError(err))).Inc()
		return models.WeatherData{}, fmt.Errorf("parse response: %w", err)
	}

	return c.mapResponse(apiResp, location), nil
}

// isRetryable determines if an error should trigger a retry attempt.
// Returns true for transient failures: rate limits (429), upstream failures (5xx),
// timeouts, and context cancellations. Returns false for client errors (4xx except 429).
func (c *OpenWeatherClient) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, ErrRateLimited) {
		return true
	}
	if errors.Is(err, ErrUpstreamFailure) {
		return true
	}

	errStr := err.Error()
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "context canceled") {
		return true
	}

	return false
}

// calculateBackoff calculates exponential backoff delay with jitter for retry attempts.
// Delay doubles with each attempt (exponential), capped at retryMaxDelay, with 10% random jitter
// to prevent thundering herd problems.
func (c *OpenWeatherClient) calculateBackoff(attempt int) time.Duration {
	delay := float64(c.retryBaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(c.retryMaxDelay) {
		delay = float64(c.retryMaxDelay)
	}

	jitter := delay * 0.1 * rand.Float64()
	return time.Duration(delay + jitter)
}

// buildRequest constructs an HTTP GET request to the OpenWeatherMap API with location,
// API key, and units=metric query parameters. Sets Accept header for JSON response.
func (c *OpenWeatherClient) buildRequest(ctx context.Context, location string) (*http.Request, error) {
	baseURL, err := url.Parse(c.apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid API URL: %w", err)
	}

	params := url.Values{}
	params.Set("q", location)
	params.Set("appid", c.apiKey)
	params.Set("units", "metric")
	baseURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	return req, nil
}

// handleErrorResponse maps HTTP status codes to domain errors.
// 401 -> ErrInvalidAPIKey, 404 -> ErrLocationNotFound, 429 -> ErrRateLimited,
// 5xx -> ErrUpstreamFailure. Returns nil for 2xx status codes.
func (c *OpenWeatherClient) handleErrorResponse(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: invalid API key", ErrInvalidAPIKey)
	case http.StatusNotFound:
		return fmt.Errorf("%w", ErrLocationNotFound)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w", ErrRateLimited)
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: HTTP %d", ErrUpstreamFailure, resp.StatusCode)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: HTTP %d", ErrUpstreamFailure, resp.StatusCode)
	}

	return nil
}

// mapResponse transforms OpenWeatherMap API response format to WeatherData model.
// Uses description if available, otherwise falls back to main condition. Uses API name
// if provided, otherwise uses requested location. Normalizes location to lowercase.
func (c *OpenWeatherClient) mapResponse(apiResp openWeatherResponse, location string) models.WeatherData {
	conditions := ""
	if len(apiResp.Weather) > 0 {
		conditions = apiResp.Weather[0].Main
		if apiResp.Weather[0].Description != "" {
			conditions = apiResp.Weather[0].Description
		}
	}

	displayName := apiResp.Name
	if displayName == "" {
		displayName = location
	}

	return models.WeatherData{
		Location:    strings.ToLower(displayName),
		Temperature: apiResp.Main.Temp,
		Conditions:  conditions,
		Humidity:    apiResp.Main.Humidity,
		WindSpeed:   apiResp.Wind.Speed,
		Timestamp:   time.Now(),
	}
}

// extractCorrelationID extracts correlation ID from request context if present.
// Returns empty string if correlation ID is not found or context is invalid.
func extractCorrelationID(ctx context.Context) string {
	if corrIDVal := ctx.Value("correlation_id"); corrIDVal != nil {
		if corrID, ok := corrIDVal.(string); ok {
			return corrID
		}
	}
	return ""
}

// statusLabel converts HTTP status code to a label for metrics (success, rate_limited,
// client_error, server_error, error). Used for Prometheus metric labeling.
func statusLabel(statusCode int) string {
	if statusCode >= 200 && statusCode < 300 {
		return "success"
	}
	if statusCode == 429 {
		return "rate_limited"
	}
	if statusCode >= 400 && statusCode < 500 {
		return "client_error"
	}
	if statusCode >= 500 {
		return "server_error"
	}
	return "error"
}

// ValidateAPIKey validates the API key by making a test request to the upstream API.
// Returns ErrInvalidAPIKey if API key is invalid (401), or error for other failures.
// Uses a short timeout (5s) to avoid blocking startup for extended periods.
func (c *OpenWeatherClient) ValidateAPIKey(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := c.buildRequest(ctx, "London")
	if err != nil {
		return fmt.Errorf("build validation request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("validation request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: API key is invalid or not activated", ErrInvalidAPIKey)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("validation failed: HTTP %d", resp.StatusCode)
	}

	return nil
}
