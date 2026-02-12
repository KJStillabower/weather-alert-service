package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

type mockWeatherClient struct {
	weather     models.WeatherData
	err         error
	validateErr error
}

func (m *mockWeatherClient) GetCurrentWeather(ctx context.Context, location string) (models.WeatherData, error) {
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

func TestNormalizeLocation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "trim and lower",
			in:   " Seattle ",
			want: "seattle",
		},
		{
			name: "already normalized",
			in:   "seattle",
			want: "seattle",
		},
		{
			name: "mixed case",
			in:   "SeAtTlE",
			want: "seattle",
		},
		{
			name: "with spaces",
			in:   "  New York  ",
			want: "new york",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeLocation(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeLocation(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestWeatherService_GetWeather_CacheHit(t *testing.T) {
	cached := models.WeatherData{
		Location:    "seattle",
		Temperature: 15.5,
		Conditions:  "Cloudy",
		Humidity:    75,
		WindSpeed:   5.2,
		Timestamp:   time.Now(),
	}

	mockCache := &mockCache{
		data: map[string]models.WeatherData{
			"seattle": cached,
		},
	}

	svc := NewWeatherService(nil, mockCache, 5*time.Minute)

	got, err := svc.GetWeather(context.Background(), "seattle")
	if err != nil {
		t.Fatalf("GetWeather() error = %v, want nil", err)
	}

	if got.Location != cached.Location {
		t.Errorf("GetWeather().Location = %q, want %q", got.Location, cached.Location)
	}
	if got.Temperature != cached.Temperature {
		t.Errorf("GetWeather().Temperature = %v, want %v", got.Temperature, cached.Temperature)
	}
}

func TestWeatherService_GetWeather_CacheMiss_UpstreamSuccess(t *testing.T) {
	upstreamWeather := models.WeatherData{
		Location:    "portland",
		Temperature: 18.3,
		Conditions:  "Sunny",
		Humidity:    60,
		WindSpeed:   3.1,
		Timestamp:   time.Now(),
	}

	mockClient := &mockWeatherClient{
		weather: upstreamWeather,
	}

	mockCache := &mockCache{
		data: make(map[string]models.WeatherData),
	}

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute)

	got, err := svc.GetWeather(context.Background(), "portland")
	if err != nil {
		t.Fatalf("GetWeather() error = %v, want nil", err)
	}

	if got.Location != upstreamWeather.Location {
		t.Errorf("GetWeather().Location = %q, want %q", got.Location, upstreamWeather.Location)
	}

	// Verify cache was populated
	cached, ok, _ := mockCache.Get(context.Background(), "portland")
	if !ok {
		t.Error("Cache was not populated after upstream fetch")
	}
	if cached.Location != upstreamWeather.Location {
		t.Errorf("Cached location = %q, want %q", cached.Location, upstreamWeather.Location)
	}
}

func TestWeatherService_GetWeather_UpstreamFailure(t *testing.T) {
	mockClient := &mockWeatherClient{
		err: errors.New("upstream error"),
	}

	mockCache := &mockCache{
		data: make(map[string]models.WeatherData),
	}

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute)

	_, err := svc.GetWeather(context.Background(), "seattle")
	if err == nil {
		t.Fatal("GetWeather() error = nil, want error")
	}

	if !errors.Is(err, mockClient.err) && err.Error() == "" {
		t.Errorf("GetWeather() error = %v, want upstream error", err)
	}
}

func TestWeatherService_GetWeather_CacheGetError(t *testing.T) {
	mockCache := &mockCache{
		err: errors.New("cache error"),
	}

	mockClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle"},
	}

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute)

	// Should fall through to upstream despite cache error
	got, err := svc.GetWeather(context.Background(), "seattle")
	if err != nil {
		t.Fatalf("GetWeather() error = %v, want nil (should fallback to upstream)", err)
	}

	if got.Location != "seattle" {
		t.Errorf("GetWeather().Location = %q, want seattle", got.Location)
	}
}
