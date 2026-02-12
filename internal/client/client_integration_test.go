//go:build integration
// +build integration

package client

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"
)

func isValidAPIKeyFormat(key string) error {
	if len(key) != 32 {
		return fmt.Errorf("API key length is %d, expected 32", len(key))
	}

	hexPattern := regexp.MustCompile(`^[0-9a-fA-F]+$`)
	if !hexPattern.MatchString(key) {
		return fmt.Errorf("API key contains non-hexadecimal characters")
	}

	return nil
}

func TestOpenWeatherClient_ValidateAPIKey_Integration(t *testing.T) {
	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		t.Skip("WEATHER_API_KEY not set, skipping integration test")
	}

	if err := isValidAPIKeyFormat(apiKey); err != nil {
		t.Fatalf("API key format validation failed: %v", err)
	}

	client, err := NewOpenWeatherClient(apiKey, "https://api.openweathermap.org/data/2.5/weather", 5*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.Background()
	err = client.ValidateAPIKey(ctx)
	if err != nil {
		t.Errorf("ValidateAPIKey() error = %v, want nil (API key may not be activated yet)", err)
	}
}

func TestOpenWeatherClient_GetCurrentWeather_Integration(t *testing.T) {
	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		t.Skip("WEATHER_API_KEY not set, skipping integration test")
	}

	if err := isValidAPIKeyFormat(apiKey); err != nil {
		t.Fatalf("API key format validation failed: %v", err)
	}

	client, err := NewOpenWeatherClient(apiKey, "https://api.openweathermap.org/data/2.5/weather", 5*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	ctx := context.Background()
	weather, err := client.GetCurrentWeather(ctx, "London")
	if err != nil {
		t.Fatalf("GetCurrentWeather() error = %v (API key may not be activated yet)", err)
	}

	if weather.Location == "" {
		t.Error("GetCurrentWeather() returned empty location")
	}
	if weather.Temperature == 0 {
		t.Error("GetCurrentWeather() returned zero temperature")
	}
}
