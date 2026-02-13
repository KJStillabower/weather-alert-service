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
	staleCacheTTL   time.Duration // Maximum age for stale cache fallback (0 = disabled)
	stampedeTracker *stampedeTracker
	coalescer       *requestCoalescer // Optional request coalescing (nil if disabled)
}

// NewWeatherService creates a new WeatherService with the provided dependencies.
// TTL specifies the cache expiration duration for weather data.
// staleCacheTTL specifies maximum age for stale cache fallback (0 = disabled).
// coalesceEnabled and coalesceTimeout configure request coalescing (disabled if timeout 0).
func NewWeatherService(client client.WeatherClient, cache cache.Cache, ttl time.Duration, staleCacheTTL time.Duration, coalesceEnabled bool, coalesceTimeout time.Duration) *WeatherService {
	var coalescer *requestCoalescer
	if coalesceEnabled && coalesceTimeout > 0 {
		coalescer = newRequestCoalescer(coalesceTimeout)
	}
	return &WeatherService{
		client:          client,
		cache:           cache,
		ttl:             ttl,
		staleCacheTTL:   staleCacheTTL,
		stampedeTracker: newStampedeTracker(),
		coalescer:       coalescer,
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

	getStart := time.Now()
	cached, ok, err := s.cache.Get(ctx, key)
	getDuration := time.Since(getStart).Seconds()
	if err != nil {
		observability.CacheErrorsTotal.WithLabelValues("get", categorizeCacheError(err)).Inc()
		observability.CacheOperationDurationSeconds.WithLabelValues("get", "error").Observe(getDuration)
	} else if ok {
		observability.CacheOperationDurationSeconds.WithLabelValues("get", "success").Observe(getDuration)
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

	// Use coalescer if enabled to prevent concurrent upstream calls for same key
	var data models.WeatherData
	var upstreamErr error
	if s.coalescer != nil {
		coalesceStart := time.Now()
		data, upstreamErr = s.coalescer.GetOrDo(ctx, key, func() (models.WeatherData, error) {
			return s.client.GetCurrentWeather(ctx, key)
		})
		coalesceWait := time.Since(coalesceStart)
		if upstreamErr == nil {
			// Check if we waited (coalesced) vs initiated the request
			// If wait time > 0, we likely coalesced (approximate)
			if coalesceWait > 10*time.Millisecond {
				observability.RequestCoalescingHitsTotal.WithLabelValues(observability.MetricLocationLabel(key)).Inc()
			}
			observability.RequestCoalescingWaitSeconds.Observe(coalesceWait.Seconds())
		}
	} else {
		data, upstreamErr = s.client.GetCurrentWeather(ctx, key)
	}
	if upstreamErr != nil {
		// Upstream failed - try stale cache if enabled
		if s.staleCacheTTL > 0 {
			stale, ok, staleErr := s.cache.GetStale(ctx, key, s.staleCacheTTL)
			if staleErr == nil && ok {
				// Calculate age from timestamp
				staleAge := time.Since(stale.Timestamp)
				observability.StaleCacheServesTotal.WithLabelValues(observability.MetricLocationLabel(key)).Inc()
				observability.StaleCacheAgeSeconds.Observe(staleAge.Seconds())
				stale.Stale = true
				if logger != nil {
					logger.Info("serving stale cache", zap.String("location", key), zap.Duration("age", staleAge))
				}
				return stale, nil
			}
		}
		return models.WeatherData{}, fmt.Errorf("fetch weather for %s: %w", key, upstreamErr)
	}

	setStart := time.Now()
	if setErr := s.cache.Set(ctx, key, data, s.ttl); setErr != nil {
		observability.CacheErrorsTotal.WithLabelValues("set", categorizeCacheError(setErr)).Inc()
		observability.CacheOperationDurationSeconds.WithLabelValues("set", "error").Observe(time.Since(setStart).Seconds())
		if logger != nil {
			logger.Warn("cache set failed", zap.String("location", key), zap.Error(setErr))
		}
	} else {
		observability.CacheOperationDurationSeconds.WithLabelValues("set", "success").Observe(time.Since(setStart).Seconds())
	}
	if logger != nil {
		logger.Debug("weather served", zap.String("location", key), zap.Bool("cached", false), zap.Duration("duration", time.Since(start)))
	}
	return data, nil
}

// categorizeCacheError returns a stable label for cache error metrics (timeout, connection, unknown).
func categorizeCacheError(err error) string {
	if err == nil {
		return "unknown"
	}
	errStr := err.Error()
	if strings.Contains(errStr, "timeout") {
		return "timeout"
	}
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "network") {
		return "connection"
	}
	return "unknown"
}

// normalizeLocation normalizes location strings by trimming whitespace and converting to lowercase.
// Used to ensure consistent cache keys and API requests regardless of input format.
func normalizeLocation(location string) string {
	return strings.ToLower(strings.TrimSpace(location))
}
