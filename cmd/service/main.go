package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/kjstillabower/weather-alert-service/internal/cache"
	"github.com/kjstillabower/weather-alert-service/internal/circuitbreaker"
	"github.com/kjstillabower/weather-alert-service/internal/client"
	"github.com/kjstillabower/weather-alert-service/internal/config"
	httphandler "github.com/kjstillabower/weather-alert-service/internal/http"
	"github.com/kjstillabower/weather-alert-service/internal/lifecycle"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
	"github.com/kjstillabower/weather-alert-service/internal/service"
)

func main() {
	logger, err := observability.NewLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("config", zap.Error(err))
	}

	weatherClient, err := client.NewOpenWeatherClientWithRetry(
		cfg.WeatherAPIKey,
		cfg.WeatherAPIURL,
		cfg.WeatherAPITimeout,
		cfg.RetryAttempts,
		cfg.RetryBaseDelay,
		cfg.RetryMaxDelay,
	)
	if err != nil {
		logger.Fatal("weather client", zap.Error(err))
	}

	if cfg.CircuitBreakerEnabled {
		cb := circuitbreaker.New(circuitbreaker.Config{
			FailureThreshold:  cfg.CircuitBreakerFailureThreshold,
			SuccessThreshold:  cfg.CircuitBreakerSuccessThreshold,
			Timeout:           cfg.CircuitBreakerTimeout,
			Component:         "weather_api",
			OnStateChange: func(from, to circuitbreaker.State) {
				observability.RecordCircuitBreakerTransition("weather_api", from.String(), to.String())
				observability.SetCircuitBreakerStateGauge("weather_api", observability.CircuitBreakerStateValue(int(to)))
			},
		})
		weatherClient.SetCircuitBreaker(cb)
		observability.SetCircuitBreakerStateGauge("weather_api", 0)
		logger.Info("circuit breaker enabled", zap.Int("failure_threshold", cfg.CircuitBreakerFailureThreshold), zap.Duration("timeout", cfg.CircuitBreakerTimeout))
	}

	var cacheSvc cache.Cache
	var memcacheCloser *cache.MemcachedCache
	switch cfg.CacheBackend {
	case "memcached":
		mc, err := cache.NewMemcachedCache(cfg.MemcachedAddrs, cfg.MemcachedTimeout, cfg.MemcachedMaxIdleConns)
		if err != nil {
			logger.Fatal("memcached cache", zap.Error(err))
		}
		memcacheCloser = mc
		cacheSvc = mc
		logger.Info("cache backend: memcached", zap.String("addrs", cfg.MemcachedAddrs))
	default:
		cacheSvc = cache.NewInMemoryCache()
		logger.Info("cache backend: in_memory")
	}
	weatherService := service.NewWeatherService(weatherClient, cacheSvc, cfg.CacheTTL, cfg.StaleCacheTTL, cfg.CoalesceEnabled, cfg.CoalesceTimeout)

	healthConfig := &httphandler.HealthConfig{
		OverloadWindow:         cfg.OverloadWindow,
		OverloadThresholdPct:   cfg.OverloadThresholdPct,
		RateLimitRPS:           cfg.RateLimitRPS,
		RateLimitBurst:         cfg.RateLimitBurst,
		DegradedWindow:         cfg.DegradedWindow,
		DegradedErrorPct:       cfg.DegradedErrorPct,
		DegradedRetryInitial:   cfg.DegradedRetryInitial,
		DegradedRetryMax:       cfg.DegradedRetryMax,
		IdleWindow:             cfg.IdleWindow,
		IdleThresholdReqPerMin: cfg.IdleThresholdReqPerMin,
		MinimumLifespan:        cfg.MinimumLifespan,
		StartTime:              time.Now(),
	}
	if memcacheCloser != nil {
		healthConfig.CachePing = memcacheCloser.Ping
	}

	var limiter *rate.Limiter
	if cfg.RateLimitRPS > 0 {
		limiter = rate.NewLimiter(rate.Limit(cfg.RateLimitRPS), cfg.RateLimitBurst)
	}
	handler := httphandler.NewHandler(weatherService, weatherClient, healthConfig, logger, limiter, cfg.LocationMaxLength, cfg.LocationMinLength)

	observability.RegisterRateLimitGauges(cfg.OverloadWindow)
	if len(cfg.TrackedLocations) > 0 {
		observability.SetTrackedLocations(cfg.TrackedLocations)
	}

	if cfg.WarmCache && len(cfg.TrackedLocations) > 0 {
		warmer := cache.NewCacheWarmer(weatherService, logger)
		warmCtx, warmCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := warmer.Warm(warmCtx, cfg.TrackedLocations); err != nil {
			logger.Warn("cache warming failed", zap.Error(err))
		}
		warmCancel()
		if cfg.WarmInterval > 0 {
			go func() {
				if err := warmer.WarmPeriodic(context.Background(), cfg.TrackedLocations, cfg.WarmInterval); err != nil && err != context.Canceled {
					logger.Error("periodic cache warming stopped", zap.Error(err))
				}
			}()
		}
	}

	router := mux.NewRouter()
	router.Use(httphandler.CorrelationIDMiddleware(logger))
	router.Use(httphandler.MetricsMiddleware)
	router.Use(httphandler.SizeMetricsMiddleware)
	router.HandleFunc("/health", handler.GetHealth).Methods("GET")
	router.Handle("/metrics", observability.MetricsHandler())
	weatherRouter := router.PathPrefix("/weather").Subrouter()
	weatherRouter.Use(httphandler.RateLimitMiddleware(limiter))
	weatherRouter.Use(httphandler.TimeoutMiddleware(cfg.RequestTimeout))
	weatherRouter.HandleFunc("/{location}", handler.GetWeather).Methods("GET")

	if cfg.TestingMode {
		logger.Warn("Testing mode enabled; /test endpoint exposed")
		router.HandleFunc("/test", handler.GetTestStatus).Methods("GET")
		router.HandleFunc("/test/{action}", handler.PostTestAction).Methods("POST")
	}

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("server starting", zap.String("addr", ":"+cfg.ServerPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server", zap.Error(err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	<-ctx.Done()
	stop()

	logger.Info("graceful shutdown triggered")
	lifecycle.SetShuttingDown(true)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown", zap.Error(err))
	}

	inFlight := httphandler.InFlightCount()
	logger.Info("waiting for in-flight requests", zap.Int64("count", inFlight))
	observability.RecordShutdownInFlight(inFlight)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), cfg.ShutdownInFlightTimeout)
	defer waitCancel()
	if err := httphandler.WaitForInFlight(waitCtx, cfg.ShutdownInFlightCheckInterval); err != nil {
		logger.Warn("in-flight requests not completed", zap.Error(err), zap.Int64("remaining", httphandler.InFlightCount()))
	}

	if err := observability.FlushTelemetry(context.Background(), logger); err != nil {
		logger.Error("telemetry flush", zap.Error(err))
	}

	if memcacheCloser != nil {
		if err := memcacheCloser.Close(); err != nil {
			logger.Error("memcached close", zap.Error(err))
		}
	}
	logger.Info("shutdown complete")
}
