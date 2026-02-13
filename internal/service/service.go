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

type WeatherService struct {
	client client.WeatherClient
	cache  cache.Cache
	ttl    time.Duration
}

func NewWeatherService(client client.WeatherClient, cache cache.Cache, ttl time.Duration) *WeatherService {
	return &WeatherService{
		client: client,
		cache:  cache,
		ttl:    ttl,
	}
}

func loggerFromContext(ctx context.Context) *zap.Logger {
	if v := ctx.Value("logger"); v != nil {
		if l, ok := v.(*zap.Logger); ok && l != nil {
			return l
		}
	}
	return nil
}

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

func normalizeLocation(location string) string {
	return strings.ToLower(strings.TrimSpace(location))
}
