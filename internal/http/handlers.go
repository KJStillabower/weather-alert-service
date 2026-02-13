package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/kjstillabower/weather-alert-service/internal/client"
	"github.com/kjstillabower/weather-alert-service/internal/degraded"
	"github.com/kjstillabower/weather-alert-service/internal/idle"
	"github.com/kjstillabower/weather-alert-service/internal/lifecycle"
	"github.com/kjstillabower/weather-alert-service/internal/observability"
	"github.com/kjstillabower/weather-alert-service/internal/overload"
	"github.com/kjstillabower/weather-alert-service/internal/service"
	"github.com/kjstillabower/weather-alert-service/internal/traffic"
)

// HealthConfig holds lifecycle thresholds for the health handler.
type HealthConfig struct {
	OverloadWindow         time.Duration
	OverloadThresholdPct   int
	RateLimitRPS           int
	DegradedWindow         time.Duration
	DegradedErrorPct       int
	DegradedRetryInitial   time.Duration
	DegradedRetryMax       time.Duration
	IdleWindow             time.Duration
	IdleThresholdReqPerMin int
	MinimumLifespan        time.Duration
	StartTime              time.Time
	// CachePing, when set, is called to check cache reachability. Used when backend is memcached.
	CachePing func() error
}

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	weatherService   *service.WeatherService
	client           client.WeatherClient
	healthConfig     *HealthConfig
	logger           *zap.Logger
	rateLimiter      *rate.Limiter
	healthStatusMu   sync.Mutex
	healthStatusPrev string
}

// NewHandler returns a new Handler.
func NewHandler(
	weatherService *service.WeatherService,
	client client.WeatherClient,
	healthConfig *HealthConfig,
	logger *zap.Logger,
	rateLimiter *rate.Limiter,
) *Handler {
	return &Handler{
		weatherService: weatherService,
		client:         client,
		healthConfig:   healthConfig,
		logger:         logger,
		rateLimiter:    rateLimiter,
	}
}

// GetWeather handles GET /weather/{location}.
func (h *Handler) GetWeather(w http.ResponseWriter, r *http.Request) {
	location := strings.TrimSpace(mux.Vars(r)["location"])
	if location == "" || strings.TrimSpace(location) == "" {
		writeError(w, r, http.StatusBadRequest, "INVALID_LOCATION", "location is required")
		return
	}

	idle.RecordRequest()
	result, err := h.weatherService.GetWeather(r.Context(), location)
	if err != nil {
		degraded.RecordError()
		writeServiceError(w, r, err)
		return
	}
	degraded.RecordSuccess()
	writeJSON(w, http.StatusOK, result)
}

// healthResult holds the computed health status and metadata for logging.
type healthResult struct {
	status     string
	statusCode int
	reason     string
}

// GetHealth handles GET /health.
func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	result := h.computeHealthStatus(r.Context())

	h.healthStatusMu.Lock()
	prev := h.healthStatusPrev
	if prev != "" && prev != result.status {
		h.logger.Info("health status transition",
			zap.String("previous_status", prev),
			zap.String("current_status", result.status),
			zap.String("reason", result.reason))
	}
	h.healthStatusPrev = result.status
	h.healthStatusMu.Unlock()

	status := result.status
	statusCode := result.statusCode
	checks := make(map[string]string)
	if status == "degraded" {
		checks["weatherApi"] = "unhealthy"
	} else {
		checks["weatherApi"] = "healthy"
	}
	if h.healthConfig != nil && h.healthConfig.CachePing != nil {
		if h.healthConfig.CachePing() == nil {
			checks["cache"] = "healthy"
		} else {
			checks["cache"] = "unhealthy"
		}
	}
	resp := map[string]interface{}{
		"status":    status,
		"service":   "weather-alert-service",
		"version":   "dev",
		"checks":    checks,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) computeHealthStatus(ctx context.Context) healthResult {
	if lifecycle.IsShuttingDown() {
		return healthResult{"shutting-down", http.StatusServiceUnavailable, "signal"}
	}
	if h.healthConfig == nil {
		if err := h.client.ValidateAPIKey(ctx); err != nil {
			return healthResult{"degraded", http.StatusServiceUnavailable, "api_key_invalid"}
		}
		return healthResult{"healthy", http.StatusOK, ""}
	}
	if err := h.client.ValidateAPIKey(ctx); err != nil {
		return healthResult{"degraded", http.StatusServiceUnavailable, "api_key_invalid"}
	}
	threshold := float64(h.healthConfig.RateLimitRPS) * h.healthConfig.OverloadWindow.Seconds() * float64(h.healthConfig.OverloadThresholdPct) / 100
	if float64(overload.RequestCount(h.healthConfig.OverloadWindow)) > threshold {
		return healthResult{"overloaded", http.StatusServiceUnavailable, "overload_threshold"}
	}
	if h.healthConfig.IdleWindow > 0 && h.healthConfig.MinimumLifespan > 0 && time.Since(h.healthConfig.StartTime) >= h.healthConfig.MinimumLifespan {
		if idle.RequestCount(h.healthConfig.IdleWindow) < h.healthConfig.IdleThresholdReqPerMin {
			return healthResult{"idle", http.StatusOK, "low_traffic"}
		}
	}
	if h.healthConfig.DegradedWindow > 0 && h.healthConfig.DegradedErrorPct > 0 {
		errors, total := degraded.ErrorRate(h.healthConfig.DegradedWindow)
		if total > 0 {
			pct := float64(errors) * 100 / float64(total)
			if pct >= float64(h.healthConfig.DegradedErrorPct) {
				return healthResult{"degraded", http.StatusServiceUnavailable, "error_rate_breach"}
			}
		}
	}
	return healthResult{"healthy", http.StatusOK, ""}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	corrID := ""
	if v := r.Context().Value("correlation_id"); v != nil {
		corrID = v.(string)
	}
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]string{
			"code":      code,
			"message":   message,
			"requestId": corrID,
		},
	})
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	writeError(w, r, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "Unable to fetch weather data")
	if logger, ok := r.Context().Value("logger").(*zap.Logger); ok && logger != nil {
		logger.Debug("upstream error", zap.Error(err))
	}
}

// GetTestStatus handles GET /test. Returns current simulated state.
func (h *Handler) GetTestStatus(w http.ResponseWriter, r *http.Request) {
	window := 60 * time.Second
	if h.healthConfig != nil && h.healthConfig.DegradedWindow > 0 {
		window = h.healthConfig.DegradedWindow
	}
	errors, _ := degraded.ErrorRate(window)
	resp := map[string]interface{}{
		"total_requests_in_window":  overload.RequestCount(window),
		"denied_requests_in_window": overload.DenialCount(window),
		"errors_in_window":          errors,
		"window_length":             window.String(),
		"auto_clear":                !degraded.IsRecoveryDisabled(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// PostTestAction handles POST /test/{action} for load, error, reset, shutdown, prevent_clear, fail_clear, clear.
func (h *Handler) PostTestAction(w http.ResponseWriter, r *http.Request) {
	action := mux.Vars(r)["action"]
	switch action {
	case "load":
		h.postTestLoad(w, r)
	case "error":
		h.postTestError(w, r)
	case "reset":
		h.postTestReset(w, r)
	case "shutdown":
		h.postTestShutdown(w, r)
	case "prevent_clear":
		h.postTestPreventClear(w, r)
	case "fail_clear":
		h.postTestFailClear(w, r)
	case "clear":
		h.postTestClear(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "UNKNOWN_ACTION", "unknown test action: "+action)
	}
}

func (h *Handler) postTestLoad(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Count <= 0 {
		body.Count = 10
	}
	var accepted, denied int
	if h.rateLimiter != nil {
		for i := 0; i < body.Count; i++ {
			if h.rateLimiter.Allow() {
				traffic.RecordSuccess()
				idle.RecordRequest()
				accepted++
			} else {
				overload.RecordDenial()
				observability.RateLimitDeniedTotal.Inc()
				denied++
			}
		}
	} else {
		traffic.RecordSuccessN(body.Count)
		for i := 0; i < body.Count; i++ {
			idle.RecordRequest()
		}
		accepted = body.Count
	}
	result := h.computeHealthStatus(r.Context())
	status := result.status
	msg := "Recorded " + strconv.Itoa(accepted) + " accepted"
	if denied > 0 {
		msg += ", " + strconv.Itoa(denied) + " denied"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"action":    "load",
		"message":   msg,
		"state":     status,
		"accepted":  accepted,
		"denied":    denied,
	})
}

func (h *Handler) postTestError(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Count <= 0 {
		body.Count = 1
	}
	traffic.RecordErrorN(body.Count)
	window := 60 * time.Second
	if h.healthConfig != nil && h.healthConfig.DegradedWindow > 0 {
		window = h.healthConfig.DegradedWindow
	}
	errors, total := degraded.ErrorRate(window)
	pct := 0
	if total > 0 {
		pct = errors * 100 / total
	}
	result := h.computeHealthStatus(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":            true,
		"action":        "error",
		"message":       "Recorded " + strconv.Itoa(body.Count) + " errors",
		"state":         result.status,
		"error_rate_pct": pct,
	})
}

func (h *Handler) postTestReset(w http.ResponseWriter, r *http.Request) {
	overload.Reset()
	degraded.Reset()
	idle.Reset()
	degraded.ClearRecoveryOverrides()
	lifecycle.SetShuttingDown(false)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"action":  "reset",
		"message": "All simulated state cleared",
	})
}

func (h *Handler) postTestShutdown(w http.ResponseWriter, r *http.Request) {
	lifecycle.SetShuttingDown(true)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"action":  "shutdown",
		"message": "Shutting-down flag set",
	})
}

func (h *Handler) postTestPreventClear(w http.ResponseWriter, r *http.Request) {
	degraded.SetRecoveryDisabled(true)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"action":  "prevent_clear",
		"message": "Auto-recovery disabled",
	})
}

func (h *Handler) postTestFailClear(w http.ResponseWriter, r *http.Request) {
	degraded.SetForceFailNextAttempt(true)
	resp := map[string]interface{}{
		"ok":      true,
		"action":  "fail_clear",
		"message": "Simulated failed recovery attempt",
	}
	if h.healthConfig != nil && h.healthConfig.DegradedRetryInitial > 0 && h.healthConfig.DegradedRetryMax >= h.healthConfig.DegradedRetryInitial {
		if d, ok := degraded.GetAndAdvanceNextRecoveryDelay(h.healthConfig.DegradedRetryInitial, h.healthConfig.DegradedRetryMax); ok {
			resp["next_recovery"] = d.String()
		} else {
			resp["next_recovery"] = "shutting-down"
			lifecycle.SetShuttingDown(true)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) postTestClear(w http.ResponseWriter, r *http.Request) {
	degraded.Reset()
	degraded.ClearRecoveryOverrides()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"action":  "clear",
		"message": "Recovery forced successful",
	})
}
