package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kjstillabower/weather-alert-service/internal/cache"
	"github.com/kjstillabower/weather-alert-service/internal/client"
	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
)

// WeatherService orchestrates weather data retrieval using cache-aside pattern
// with upstream API fallback. Implements the service layer business logic.
type WeatherService struct {
	client          client.WeatherClient
	cache           cache.Cache
	ttl             time.Duration
	stampedeTracker *stampedeTracker 
}

// NewWeatherService creates a new WeatherService with the provided dependencies.
// TTL specifies the cache expiration duration for weather data.
func NewWeatherService(client client.WeatherClient, cache cache.Cache, ttl time.Duration) *WeatherService {
	return &WeatherService{
		client:          client,
		cache:           cache,
		ttl:             ttl,
		stampedeTracker: newStampedeTracker(),
	}
}

// loggerFromContext extracts a zap.Logger from request context if present.
// Returns nil if logger is not found or context is invalid.
func loggerFromContext(ctx context.Context) *zap.Logger {
	if v := ctx.Value("logger"); v != nil {
		if l, ok := v.(*zap.Logger); ok && l != nil {
			return l
		}
	}
	return nil
}

// GetWeather retrieves weather data for the specified location using cache-aside pattern.
// Checks cache first, falls back to upstream API on cache miss, and populates cache on success.
// Returns cached data if available, otherwise fetches from upstream and caches the result.
func (s *WeatherService) GetWeather(ctx context.Context, location string) (models.WeatherData, error) {
	key := normalizeLocation(location)
	start := time.Now()
	logger := loggerFromContext(ctx)

	if cached, ok, err := s.cache.Get(ctx, key); err == nil && ok {
		observability.CacheHitsTotal.WithLabelValues("weather").Inc()
		if logger != nil {
			logger.Debug("cache hit", zap.String("location", key))
			logger.Debug("weather served", zap.String("location", key), zap.Bool("cached", true), zap.Duration("duration", time.Since(start)))
		}
		return cached, nil
	}

	concurrentMisses := s.stampedeTracker.RecordMiss(key)
	defer s.stampedeTracker.RecordHit(key)
	locLabel := observability.MetricLocationLabel(key)
	if concurrentMisses > 1 {
		observability.CacheStampedeDetectedTotal.WithLabelValues(locLabel).Inc()
		observability.CacheStampedeConcurrency.WithLabelValues(locLabel).Observe(float64(concurrentMisses))
	}

	if logger != nil {
		logger.Debug("cache miss, fetching upstream", zap.String("location", key))
	}
	data, err := s.client.GetCurrentWeather(ctx, key)
	if err != nil {
		return models.WeatherData{}, fmt.Errorf("fetch weather for %s: %w", key, err)
	}

	_ = s.cache.Set(ctx, key, data, s.ttl)
	if logger != nil {
		logger.Debug("weather served", zap.String("location", key), zap.Bool("cached", false), zap.Duration("duration", time.Since(start)))
	}
	return data, nil
}

// normalizeLocation normalizes location strings by trimming whitespace and converting to lowercase.
// Used to ensure consistent cache keys and API requests regardless of input format.
func normalizeLocation(location string) string {
	return strings.ToLower(strings.TrimSpace(location))
}
