package cache

import (
	"context"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

// Cache defines the interface for weather data caching implementations.
// Get returns cached data if present and not expired, Set stores data with TTL.
type Cache interface {
	Get(ctx context.Context, key string) (models.WeatherData, bool, error)
	Set(ctx context.Context, key string, value models.WeatherData, ttl time.Duration) error
}

// InMemoryCache implements Cache using an in-memory map with TTL-based expiration.
// Expired entries are removed on access. Not thread-safe; use with single goroutine or external synchronization.
type InMemoryCache struct {
	data map[string]cacheEntry
}

// cacheEntry stores cached weather data with expiration timestamp.
type cacheEntry struct {
	value     models.WeatherData
	expiresAt time.Time
}

// NewInMemoryCache creates a new in-memory cache instance.
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		data: make(map[string]cacheEntry),
	}
}

// Get retrieves cached weather data for the key if present and not expired.
// Returns (data, true, nil) on cache hit, (zero, false, nil) on miss or expiration.
// Expired entries are automatically removed from cache.
func (c *InMemoryCache) Get(ctx context.Context, key string) (models.WeatherData, bool, error) {
	entry, ok := c.data[key]
	if !ok {
		return models.WeatherData{}, false, nil
	}

	if time.Now().After(entry.expiresAt) {
		delete(c.data, key)
		return models.WeatherData{}, false, nil
	}

	return entry.value, true, nil
}

// Set stores weather data in cache with the specified TTL duration.
// Entry expires after TTL elapses and will be removed on next Get access.
func (c *InMemoryCache) Set(ctx context.Context, key string, value models.WeatherData, ttl time.Duration) error {
	c.data[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}
