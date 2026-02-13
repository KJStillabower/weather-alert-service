package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/kjstillabower/weather-alert-service/internal/observability"
	"github.com/kjstillabower/weather-alert-service/internal/overload"
)

// CorrelationIDMiddleware adds or generates a correlation ID for each request.
// Extracts X-Correlation-ID from request header, or generates a new UUID if missing.
// Adds correlation ID to request context and response header, and attaches logger with correlation_id field.
func CorrelationIDMiddleware(logger *zap.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corrID := r.Header.Get("X-Correlation-ID")
			if corrID == "" {
				corrID = uuid.New().String()
			}

			ctx := context.WithValue(r.Context(), "correlation_id", corrID)
			r = r.WithContext(ctx)

			w.Header().Set("X-Correlation-ID", corrID)

			logger := logger.With(zap.String("correlation_id", corrID))
			ctx = context.WithValue(ctx, "logger", logger)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// MetricsMiddleware instruments HTTP requests with Prometheus metrics.
// Records request count, duration, and in-flight requests. Uses route template
// (e.g., /weather/{location}) to avoid cardinality explosion.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		observability.HTTPRequestsInFlight.Inc()
		defer observability.HTTPRequestsInFlight.Dec()

		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(recorder, r)

		duration := time.Since(start).Seconds()
		route := getRoute(r)
		method := r.Method
		statusCode := statusCodeString(recorder.statusCode)

		observability.HTTPRequestsTotal.WithLabelValues(method, route, statusCode).Inc()
		observability.HTTPRequestDuration.WithLabelValues(method, route).Observe(duration)
	})
}

// getRoute returns the route template for the request path to avoid cardinality
// in metrics. Maps specific paths to templates (e.g., /weather/seattle -> /weather/{location}).
func getRoute(r *http.Request) string {
	path := r.URL.Path
	switch {
	case path == "/health":
		return "/health"
	case path == "/metrics":
		return "/metrics"
	case strings.HasPrefix(path, "/weather/"):
		return "/weather/{location}"
	default:
		return path
	}
}

// statusRecorder wraps http.ResponseWriter to capture the HTTP status code
// written by handlers for metrics recording.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// statusCodeString converts HTTP status code to status class string (e.g., 200 -> "2xx", 404 -> "4xx").
// Used for metrics labeling to group status codes by class.
func statusCodeString(code int) string {
	return fmt.Sprintf("%dxx", code/100)
}

// TimeoutMiddleware sets a deadline on the request context. When exceeded, downstream handlers
// receive context.DeadlineExceeded. Apply only to routes that need it (e.g. /weather).
func TimeoutMiddleware(timeout time.Duration) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RateLimitMiddleware returns 429 when the token bucket is exhausted. Disabled when limiter is nil.
func RateLimitMiddleware(limiter *rate.Limiter) mux.MiddlewareFunc {
	if limiter == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				if logger, ok := r.Context().Value("logger").(*zap.Logger); ok && logger != nil {
					logger.Debug("rate limit denied")
				}
				overload.RecordDenial()
				observability.RateLimitDeniedTotal.Inc()
				writeRateLimitError(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeRateLimitError writes a 429 Too Many Requests error response in the standard error format.
// Includes correlation ID from request context if available.
func writeRateLimitError(w http.ResponseWriter, r *http.Request) {
	corrID := ""
	if v := r.Context().Value("correlation_id"); v != nil {
		corrID = v.(string)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":      "RATE_LIMITED",
			"message":   "Too many requests",
			"requestId": corrID,
		},
	})
}
