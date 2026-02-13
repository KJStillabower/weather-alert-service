package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// BenchmarkClient_BuildRequest benchmarks HTTP request construction.
func BenchmarkClient_BuildRequest(b *testing.B) {
	client, _ := NewOpenWeatherClient("test-api-key", "https://api.openweathermap.org/data/2.5/weather", 2*time.Second)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.buildRequest(ctx, "seattle")
	}
}

// BenchmarkClient_ParseResponse benchmarks JSON response parsing.
func BenchmarkClient_ParseResponse(b *testing.B) {
	// Sample OpenWeatherMap API response
	responseJSON := `{
		"main": {"temp": 15.5, "humidity": 65},
		"weather": [{"main": "Clear", "description": "clear sky"}],
		"wind": {"speed": 10.2},
		"name": "Seattle"
	}`

	var apiResp openWeatherResponse

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = json.Unmarshal([]byte(responseJSON), &apiResp)
	}
}

// BenchmarkClient_MapResponse benchmarks response mapping to domain model.
func BenchmarkClient_MapResponse(b *testing.B) {
	client, _ := NewOpenWeatherClient("key", "url", time.Second)
	apiResp := openWeatherResponse{
		Main: struct {
			Temp     float64 `json:"temp"`
			Humidity int     `json:"humidity"`
		}{Temp: 15.5, Humidity: 65},
		Weather: []struct {
			Main        string `json:"main"`
			Description string `json:"description"`
		}{{Main: "Clear", Description: "clear sky"}},
		Wind: struct {
			Speed float64 `json:"speed"`
		}{Speed: 10.2},
		Name: "Seattle",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.mapResponse(apiResp, "seattle")
	}
}

// BenchmarkClient_HandleErrorResponse benchmarks error response handling.
func BenchmarkClient_HandleErrorResponse(b *testing.B) {
	client, _ := NewOpenWeatherClient("key", "url", time.Second)

	// Create mock HTTP response with 503 status
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.Body = io.NopCloser(strings.NewReader(""))
		_ = client.handleErrorResponse(resp)
	}
}

// BenchmarkClient_IsRetryable benchmarks retry decision logic.
func BenchmarkClient_IsRetryable(b *testing.B) {
	client, _ := NewOpenWeatherClient("key", "url", time.Second)

	testErrors := []error{
		ErrRateLimited,
		ErrUpstreamFailure,
		fmt.Errorf("timeout: context deadline exceeded"),
		fmt.Errorf("invalid request"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := testErrors[i%len(testErrors)]
		_ = client.isRetryable(err)
	}
}

// BenchmarkClient_CalculateBackoff benchmarks backoff calculation.
func BenchmarkClient_CalculateBackoff(b *testing.B) {
	client, err := NewOpenWeatherClientWithRetry("test-api-key-1234567890", "https://api.openweathermap.org/data/2.5/weather", time.Second, 3, 100*time.Millisecond, 2*time.Second)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		attempt := (i % 5) + 1
		_ = client.calculateBackoff(attempt)
	}
}

// BenchmarkStatusLabel benchmarks HTTP status code to label conversion.
func BenchmarkStatusLabel(b *testing.B) {
	statusCodes := []int{200, 400, 429, 500, 503}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		code := statusCodes[i%len(statusCodes)]
		_ = statusLabel(code)
	}
}
