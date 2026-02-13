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
	data      map[string]models.WeatherData
	staleData map[string]models.WeatherData // Data that's expired but available for stale retrieval
	err       error
}

func (m *mockCache) Get(ctx context.Context, key string) (models.WeatherData, bool, error) {
	if m.err != nil {
		return models.WeatherData{}, false, m.err
	}
	val, ok := m.data[key]
	return val, ok, nil
}

func (m *mockCache) GetStale(ctx context.Context, key string, maxStaleAge time.Duration) (models.WeatherData, bool, error) {
	if m.err != nil {
		return models.WeatherData{}, false, m.err
	}
	// Check stale data first (expired but within maxStaleAge)
	if m.staleData != nil {
		if stale, ok := m.staleData[key]; ok {
			age := time.Since(stale.Timestamp)
			if age <= maxStaleAge {
				return stale, true, nil
			}
		}
	}
	// Fallback to regular data
	return m.Get(ctx, key)
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

// TestNormalizeLocation verifies that normalizeLocation trims whitespace, converts to lowercase,
// and handles various input formats correctly.
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

// TestWeatherService_GetWeather_CacheHit verifies that GetWeather returns cached data
// when a cache entry exists for the requested location, avoiding an upstream API call.
func TestWeatherService_GetWeather_CacheHit(t *testing.T) {
	// Arrange: Set up a cache with pre-populated weather data for "seattle"
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

	svc := NewWeatherService(nil, mockCache, 5*time.Minute, 0, false, 0)

	// Act: Request weather for a location that exists in cache
	got, err := svc.GetWeather(context.Background(), "seattle")

	// Assert: Verify cache hit returns data without error
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

// TestWeatherService_GetWeather_CacheMiss_UpstreamSuccess verifies that GetWeather fetches
// from upstream when cache misses, populates the cache with the result, and returns the data.
func TestWeatherService_GetWeather_CacheMiss_UpstreamSuccess(t *testing.T) {
	// Arrange: Set up empty cache and mock client with upstream weather data
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

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)

	// Act: Request weather for a location not in cache
	got, err := svc.GetWeather(context.Background(), "portland")

	// Assert: Verify upstream fetch succeeds and cache is populated
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

// TestWeatherService_GetWeather_UpstreamFailure verifies that GetWeather propagates
// upstream errors when cache misses and upstream fetch fails.
func TestWeatherService_GetWeather_UpstreamFailure(t *testing.T) {
	// Arrange: Set up empty cache and mock client that returns an error
	mockClient := &mockWeatherClient{
		err: errors.New("upstream error"),
	}

	mockCache := &mockCache{
		data: make(map[string]models.WeatherData),
	}

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)

	// Act: Request weather when upstream fails
	_, err := svc.GetWeather(context.Background(), "seattle")

	// Assert: Verify error is propagated
	if err == nil {
		t.Fatal("GetWeather() error = nil, want error")
	}

	if !errors.Is(err, mockClient.err) && err.Error() == "" {
		t.Errorf("GetWeather() error = %v, want upstream error", err)
	}
}

// TestWeatherService_GetWeather_CacheGetError verifies that GetWeather falls back to upstream
// when cache read fails, ensuring cache errors are non-fatal.
func TestWeatherService_GetWeather_CacheGetError(t *testing.T) {
	// Arrange: Set up cache that returns error and mock client with valid data
	mockCache := &mockCache{
		err: errors.New("cache error"),
	}

	mockClient := &mockWeatherClient{
		weather: models.WeatherData{Location: "seattle"},
	}

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0)

	// Act: Request weather when cache read fails
	got, err := svc.GetWeather(context.Background(), "seattle")

	// Assert: Verify fallback to upstream succeeds despite cache error
	if err != nil {
		t.Fatalf("GetWeather() error = %v, want nil (should fallback to upstream)", err)
	}

	if got.Location != "seattle" {
		t.Errorf("GetWeather().Location = %q, want seattle", got.Location)
	}
}

// TestWeatherService_GetWeather_StaleCacheFallback verifies that stale cache is served when upstream fails.
func TestWeatherService_GetWeather_StaleCacheFallback(t *testing.T) {
	staleData := models.WeatherData{
		Location:    "seattle",
		Temperature: 10.0,
		Conditions:  "Clear",
		Humidity:    60,
		WindSpeed:   3.0,
		Timestamp:   time.Now().Add(-30 * time.Minute), // 30 min old
	}

	mockCache := &mockCache{
		staleData: map[string]models.WeatherData{
			"seattle": staleData,
		},
	}
	mockClient := &mockWeatherClient{
		err: errors.New("upstream failure"),
	}

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute, 1*time.Hour, false, 0) // stale TTL 1h

	got, err := svc.GetWeather(context.Background(), "seattle")
	if err != nil {
		t.Fatalf("GetWeather() error = %v, want nil (stale cache served)", err)
	}
	if !got.Stale {
		t.Error("GetWeather().Stale = false, want true")
	}
	if got.Location != staleData.Location {
		t.Errorf("GetWeather().Location = %q, want %q", got.Location, staleData.Location)
	}
}

// TestWeatherService_GetWeather_StaleCacheDisabled verifies that stale cache is not used when disabled.
func TestWeatherService_GetWeather_StaleCacheDisabled(t *testing.T) {
	staleData := models.WeatherData{
		Location:    "seattle",
		Temperature: 10.0,
		Timestamp:   time.Now().Add(-30 * time.Minute),
	}

	mockCache := &mockCache{
		staleData: map[string]models.WeatherData{
			"seattle": staleData,
		},
	}
	mockClient := &mockWeatherClient{
		err: errors.New("upstream failure"),
	}

	svc := NewWeatherService(mockClient, mockCache, 5*time.Minute, 0, false, 0) // stale disabled

	_, err := svc.GetWeather(context.Background(), "seattle")
	if err == nil {
		t.Fatal("GetWeather() error = nil, want error (stale cache disabled)")
	}
}
