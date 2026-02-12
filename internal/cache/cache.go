package cache

import (
	"context"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

type Cache interface {
	Get(ctx context.Context, key string) (models.WeatherData, bool, error)
	Set(ctx context.Context, key string, value models.WeatherData, ttl time.Duration) error
}

type InMemoryCache struct {
	data map[string]cacheEntry
}

type cacheEntry struct {
	value     models.WeatherData
	expiresAt time.Time
}

func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		data: make(map[string]cacheEntry),
	}
}

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

func (c *InMemoryCache) Set(ctx context.Context, key string, value models.WeatherData, ttl time.Duration) error {
	c.data[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}
