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

	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
)

type WeatherClient interface {
	GetCurrentWeather(ctx context.Context, location string) (models.WeatherData, error)
	ValidateAPIKey(ctx context.Context) error
}

var (
	ErrInvalidAPIKey   = errors.New("invalid API key")
	ErrLocationNotFound = errors.New("location not found")
	ErrUpstreamFailure = errors.New("upstream failure")
	ErrRateLimited     = errors.New("rate limited")
)

type OpenWeatherClient struct {
	apiKey        string
	apiURL        string
	timeout       time.Duration
	client        *http.Client
	retryAttempts int
	retryBaseDelay time.Duration
	retryMaxDelay  time.Duration
}

func NewOpenWeatherClient(apiKey, apiURL string, timeout time.Duration) (*OpenWeatherClient, error) {
	return NewOpenWeatherClientWithRetry(apiKey, apiURL, timeout, 3, 100*time.Millisecond, 2*time.Second)
}

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

func (c *OpenWeatherClient) GetCurrentWeather(ctx context.Context, location string) (models.WeatherData, error) {
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

		result, err := c.callAPI(ctx, location)
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

func (c *OpenWeatherClient) callAPI(ctx context.Context, location string) (models.WeatherData, error) {
	start := time.Now()
	
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := c.buildRequest(reqCtx, location)
	if err != nil {
		observability.WeatherAPICallsTotal.WithLabelValues("error").Inc()
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
		return models.WeatherData{}, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.WeatherData{}, fmt.Errorf("read response body: %w", err)
	}

	var apiResp openWeatherResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return models.WeatherData{}, fmt.Errorf("parse response: %w", err)
	}

	return c.mapResponse(apiResp, location), nil
}

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

func (c *OpenWeatherClient) calculateBackoff(attempt int) time.Duration {
	delay := float64(c.retryBaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(c.retryMaxDelay) {
		delay = float64(c.retryMaxDelay)
	}

	jitter := delay * 0.1 * rand.Float64()
	return time.Duration(delay + jitter)
}

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

func extractCorrelationID(ctx context.Context) string {
	if corrIDVal := ctx.Value("correlation_id"); corrIDVal != nil {
		if corrID, ok := corrIDVal.(string); ok {
			return corrID
		}
	}
	return ""
}

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
