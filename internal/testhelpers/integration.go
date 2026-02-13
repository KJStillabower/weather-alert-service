//go:build integration
// +build integration

package testhelpers

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/cache"
	"github.com/kjstillabower/weather-alert-service/internal/client"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
	"github.com/kjstillabower/weather-alert-service/internal/service"
)

// IntegrationTestConfig holds configuration for integration tests.
type IntegrationTestConfig struct {
	APIKey        string
	APIURL        string
	CacheBackend  string // "in_memory" or "memcached"
	MemcachedAddr string
}

// GetIntegrationConfig loads integration test configuration from environment.
// Skips test if WEATHER_API_KEY is not set.
func GetIntegrationConfig(t *testing.T) IntegrationTestConfig {
	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		t.Skip("WEATHER_API_KEY not set, skipping integration test")
	}

	apiURL := os.Getenv("WEATHER_API_URL")
	if apiURL == "" {
		apiURL = "https://api.openweathermap.org/data/2.5/weather"
	}

	cacheBackend := os.Getenv("INTEGRATION_CACHE_BACKEND")
	memcachedAddr := os.Getenv("MEMCACHED_ADDRS")
	if memcachedAddr == "" {
		memcachedAddr = "localhost:11211"
	}

	return IntegrationTestConfig{
		APIKey:        apiKey,
		APIURL:        apiURL,
		CacheBackend:  cacheBackend,
		MemcachedAddr: memcachedAddr,
	}
}

// SetupIntegrationService creates a fully configured service for integration tests.
// Returns weather service, cache instance, and cleanup function.
func SetupIntegrationService(t *testing.T, cfg IntegrationTestConfig) (*service.WeatherService, cache.Cache, func()) {
	logger, err := observability.NewLogger()
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	_ = logger // May be used later

	weatherClient, err := client.NewOpenWeatherClient(cfg.APIKey, cfg.APIURL, 5*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}

	var cacheSvc cache.Cache
	var cleanup func()

	if cfg.CacheBackend == "memcached" {
		memcachedCache, err := cache.NewMemcachedCache(cfg.MemcachedAddr, 500*time.Millisecond, 2)
		if err == nil {
			cacheSvc = memcachedCache
			cleanup = func() { memcachedCache.Close() }
			t.Logf("Using Memcached cache at %s", cfg.MemcachedAddr)
		} else {
			t.Logf("Memcached not available (%v), using in-memory cache", err)
			cacheSvc = cache.NewInMemoryCache()
			cleanup = func() {}
		}
	} else {
		cacheSvc = cache.NewInMemoryCache()
		cleanup = func() {}
	}

	weatherService := service.NewWeatherService(weatherClient, cacheSvc, 5*time.Minute)

	return weatherService, cacheSvc, cleanup
}

// SetupIntegrationClient creates a weather client for integration tests.
func SetupIntegrationClient(t *testing.T, cfg IntegrationTestConfig) client.WeatherClient {
	client, err := client.NewOpenWeatherClient(cfg.APIKey, cfg.APIURL, 5*time.Second)
	if err != nil {
		t.Fatalf("NewOpenWeatherClient() error = %v", err)
	}
	return client
}

// ClearCache clears all entries from the cache for test cleanup.
func ClearCache(ctx context.Context, cacheSvc cache.Cache) {
	// For in-memory cache, we can't clear it directly
	// For Memcached, we'd need to implement a Clear method or use a test-specific cache
	// For now, tests should use unique keys or reset cache between tests
	_ = ctx
	_ = cacheSvc
}
