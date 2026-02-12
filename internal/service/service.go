package service

import (
	"context"
	"fmt"
	"strings"
	"time"

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

func (s *WeatherService) GetWeather(ctx context.Context, location string) (models.WeatherData, error) {
	key := normalizeLocation(location)

	if cached, ok, err := s.cache.Get(ctx, key); err == nil && ok {
		observability.CacheHitsTotal.WithLabelValues("weather").Inc()
		return cached, nil
	}

	data, err := s.client.GetCurrentWeather(ctx, key)
	if err != nil {
		return models.WeatherData{}, fmt.Errorf("fetch weather for %s: %w", key, err)
	}

	_ = s.cache.Set(ctx, key, data, s.ttl)
	return data, nil
}

func normalizeLocation(location string) string {
	return strings.ToLower(strings.TrimSpace(location))
}
