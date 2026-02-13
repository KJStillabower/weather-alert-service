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
	weatherService := service.NewWeatherService(weatherClient, cacheSvc, cfg.CacheTTL)

	healthConfig := &httphandler.HealthConfig{
		OverloadWindow:         cfg.OverloadWindow,
		OverloadThresholdPct:   cfg.OverloadThresholdPct,
		RateLimitRPS:           cfg.RateLimitRPS,
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
	handler := httphandler.NewHandler(weatherService, weatherClient, healthConfig, logger, limiter)

	observability.RegisterRateLimitGauges(cfg.OverloadWindow)
	if len(cfg.TrackedLocations) > 0 {
		observability.SetTrackedLocations(cfg.TrackedLocations)
	}

	router := mux.NewRouter()
	router.Use(httphandler.CorrelationIDMiddleware(logger))
	router.Use(httphandler.MetricsMiddleware)
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
		logger.Error("shutdown", zap.Error(err))
	}
	if memcacheCloser != nil {
		if err := memcacheCloser.Close(); err != nil {
			logger.Error("memcached close", zap.Error(err))
		}
	}
}
