package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kjstillabower/weather-alert-service/internal/models"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
)

// WeatherFetcher is implemented by the service layer to fetch weather for a location.
// Used by CacheWarmer to avoid a circular dependency on the service package.
type WeatherFetcher interface {
	GetWeather(ctx context.Context, location string) (models.WeatherData, error)
}

// CacheWarmer warms the cache by prefetching weather for a list of locations.
type CacheWarmer struct {
	fetcher WeatherFetcher
	logger  *zap.Logger
}

// NewCacheWarmer creates a CacheWarmer that uses the given fetcher and logger.
func NewCacheWarmer(fetcher WeatherFetcher, logger *zap.Logger) *CacheWarmer {
	return &CacheWarmer{fetcher: fetcher, logger: logger}
}

// Warm fetches weather for each location concurrently and populates the cache via the fetcher.
// Returns an error if any location failed (aggregated).
func (w *CacheWarmer) Warm(ctx context.Context, locations []string) error {
	start := time.Now()
	observability.CacheWarmingTotal.Inc()
	if w.logger != nil {
		w.logger.Info("warming cache", zap.Int("locations", len(locations)))
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(locations))
	for _, loc := range locations {
		loc := loc
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.fetcher.GetWeather(ctx, loc)
			if err != nil {
				errCh <- fmt.Errorf("warm %s: %w", loc, err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	duration := time.Since(start).Seconds()
	observability.CacheWarmingDurationSeconds.Observe(duration)
	if w.logger != nil {
		w.logger.Info("cache warming complete", zap.Int("locations", len(locations)), zap.Int("errors", len(errs)), zap.Float64("duration_seconds", duration))
	}
	if len(errs) > 0 {
		observability.CacheWarmingErrorsTotal.Inc()
		return fmt.Errorf("cache warming: %v", errs)
	}
	return nil
}

// WarmPeriodic runs an initial Warm, then refreshes at the given interval until ctx is done.
func (w *CacheWarmer) WarmPeriodic(ctx context.Context, locations []string, interval time.Duration) error {
	if err := w.Warm(ctx, locations); err != nil && w.logger != nil {
		w.logger.Warn("initial cache warm failed", zap.Error(err))
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.Warm(ctx, locations); err != nil && w.logger != nil {
				w.logger.Warn("periodic cache warm failed", zap.Error(err))
			}
		}
	}
}
