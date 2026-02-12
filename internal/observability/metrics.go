package observability

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kjstillabower/weather-alert-service/internal/overload"
)

var (
	registry *prometheus.Registry

	// HTTP request rate. Watch for: sudden drops (service down) or spikes (traffic surge).
	HTTPRequestsTotal *prometheus.CounterVec
	
	// HTTP request latency per request. Watch for: p95/p99 latency increases, SLO breaches.
	HTTPRequestDuration *prometheus.HistogramVec
	
	// Concurrent requests in flight. Watch for: saturation, capacity limits.
	HTTPRequestsInFlight prometheus.Gauge
	
	// OpenWeatherMap API call rate. Watch for: error vs success ratio.
	WeatherAPICallsTotal *prometheus.CounterVec
	
	// External API latency per request. Watch for: p95 > 2s (upstream degradation), p99 > 5s (timeout risk).
	WeatherAPIDuration *prometheus.HistogramVec
	
	// Retry attempts for weather API. Watch for: high retries = unstable upstream.
	WeatherAPIRetriesTotal prometheus.Counter
	
	// Cache hits. Cache misses = weatherApiCallsTotal - weatherApiRetriesTotal. Hit rate = hits/(hits+misses).
	CacheHitsTotal *prometheus.CounterVec
	
	// Total weather lookups. Watch for: traffic volume, rate() for QPS.
	WeatherQueriesTotal prometheus.Counter
	
	// Per-location query count (allow-list; others go to "other"). Watch for: top locations, traffic distribution.
	WeatherQueriesByLocationTotal *prometheus.CounterVec

	// Rate limit denials. Watch for: overload, capacity exceeded.
	RateLimitDeniedTotal prometheus.Counter

	// trackedLocations is built from config; used to resolve location for metrics.
	trackedLocationsMu sync.RWMutex
	trackedLocations   map[string]struct{}

	rateLimitGaugesOnce sync.Once
)

func init() {
	registry = prometheus.NewRegistry()

	registry.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "httpRequestsTotal",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "route", "statusCode"},
	)
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "httpRequestDurationSeconds",
			Help:    "HTTP request latency in seconds (per request)",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)
	HTTPRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "httpRequestsInFlight",
			Help: "Number of HTTP requests currently being served",
		},
	)
	WeatherAPICallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weatherApiCallsTotal",
			Help: "Total number of OpenWeatherMap API calls",
		},
		[]string{"status"},
	)
	WeatherAPIDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weatherApiDurationSeconds",
			Help:    "OpenWeatherMap API latency in seconds (per request)",
			Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"status"},
	)
	WeatherAPIRetriesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "weatherApiRetriesTotal",
			Help: "Total number of retry attempts for weather API calls",
		},
	)
	CacheHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cacheHitsTotal",
			Help: "Total number of cache hits. Cache misses = weatherApiCallsTotal - weatherApiRetriesTotal.",
		},
		[]string{"cacheType"},
	)
	WeatherQueriesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "weatherQueriesTotal",
			Help: "Total number of weather lookups",
		},
	)
	WeatherQueriesByLocationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weatherQueriesByLocationTotal",
			Help: "Weather queries by location (allow-list; others use location=other)",
		},
		[]string{"location"},
	)
	RateLimitDeniedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "rateLimitDeniedTotal",
			Help: "Total number of requests denied by rate limiter (429)",
		},
	)

	registry.MustRegister(
		HTTPRequestsTotal, HTTPRequestDuration, HTTPRequestsInFlight,
		WeatherAPICallsTotal, WeatherAPIDuration, WeatherAPIRetriesTotal,
		CacheHitsTotal,
		WeatherQueriesTotal, WeatherQueriesByLocationTotal,
		RateLimitDeniedTotal,
	)
}

// RegisterRateLimitGauges registers load and rejects gauges for the rate-limited path.
// Call from main after config load with cfg.OverloadWindow. Uses same window as lifecycle.
func RegisterRateLimitGauges(window time.Duration) {
	rateLimitGaugesOnce.Do(func() {
		registry.MustRegister(
			prometheus.NewGaugeFunc(
				prometheus.GaugeOpts{
					Name: "rateLimitRequestsInWindow",
					Help: "Requests hitting rate-limited path in sliding window; load/capacity planning",
				},
				func() float64 { return float64(overload.RequestCount(window)) },
			),
			prometheus.NewGaugeFunc(
				prometheus.GaugeOpts{
					Name: "rateLimitRejectsInWindow",
					Help: "429 responses in sliding window; are we rejecting requests",
				},
				func() float64 { return float64(overload.DenialCount(window)) },
			),
		)
	})
}

// SetTrackedLocations sets the allow-list for location metrics. Non-tracked locations increment "other".
func SetTrackedLocations(locations []string) {
	trackedLocationsMu.Lock()
	defer trackedLocationsMu.Unlock()
	trackedLocations = make(map[string]struct{}, len(locations))
	for _, loc := range locations {
		trackedLocations[normalizeLocationForMetrics(loc)] = struct{}{}
	}
}

// RecordWeatherQuery records a weather query for the given location.
func RecordWeatherQuery(location string) {
	WeatherQueriesTotal.Inc()
	loc := normalizeLocationForMetrics(location)
	trackedLocationsMu.RLock()
	_, ok := trackedLocations[loc] // nil map read is safe in Go
	trackedLocationsMu.RUnlock()
	if ok {
		WeatherQueriesByLocationTotal.WithLabelValues(loc).Inc()
	} else {
		WeatherQueriesByLocationTotal.WithLabelValues("other").Inc()
	}
}

func normalizeLocationForMetrics(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	return s
}

// MetricsHandler returns an http.Handler that serves application and runtime metrics.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
