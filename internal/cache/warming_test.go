package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

type mockWeatherFetcher struct {
	weather models.WeatherData
	err     error
}

func (m *mockWeatherFetcher) GetWeather(ctx context.Context, location string) (models.WeatherData, error) {
	if m.err != nil {
		return models.WeatherData{}, m.err
	}
	out := m.weather
	out.Location = location
	return out, nil
}

func TestCacheWarmer_Warm_Success(t *testing.T) {
	fetcher := &mockWeatherFetcher{weather: models.WeatherData{Temperature: 10, Conditions: "clear"}}
	warmer := NewCacheWarmer(fetcher, nil)
	ctx := context.Background()

	err := warmer.Warm(ctx, []string{"seattle", "boston"})
	if err != nil {
		t.Fatalf("Warm() error = %v, want nil", err)
	}
}

func TestCacheWarmer_Warm_EmptyLocations(t *testing.T) {
	fetcher := &mockWeatherFetcher{}
	warmer := NewCacheWarmer(fetcher, nil)
	ctx := context.Background()

	err := warmer.Warm(ctx, nil)
	if err != nil {
		t.Fatalf("Warm() with nil locations error = %v, want nil", err)
	}
	err = warmer.Warm(ctx, []string{})
	if err != nil {
		t.Fatalf("Warm() with empty locations error = %v, want nil", err)
	}
}

func TestCacheWarmer_Warm_FetcherError(t *testing.T) {
	fetcher := &mockWeatherFetcher{err: errors.New("api down")}
	warmer := NewCacheWarmer(fetcher, nil)
	ctx := context.Background()

	err := warmer.Warm(ctx, []string{"seattle"})
	if err == nil {
		t.Fatal("Warm() error = nil, want non-nil")
	}
	if msg := err.Error(); msg == "" || (msg != "cache warming: [warm seattle: api down]" && len(msg) < 10) {
		t.Errorf("Warm() error = %q, want non-empty message containing failure", msg)
	}
}
