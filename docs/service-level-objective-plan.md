# Service Level Objective Plan

## Goal

Define Service Level Indicators (SLIs) and Service Level Objectives (SLOs) derived from middleware and configuration. SLOs are **parameterized by config**—changing `config/[env].yaml` changes the effective SLO targets. This document maps config keys to service-level semantics.

## Config-to-SLO Mapping

| Config Section | Key | SLO Semantics |
|----------------|-----|---------------|
| `request` | `timeout` | Max request duration; handler returns 503 if exceeded |
| `weather_api` | `timeout` | Max upstream call duration; retries use same limit |
| `reliability` | `rate_limit_rps`, `rate_limit_burst` | Capacity; requests above limit get 429 |
| `lifecycle` | `overload_window`, `overload_threshold_pct` | Overload threshold: requests in window > rps × window × (pct/100) → 503 on /health |
| `lifecycle` | `degraded_window`, `degraded_error_pct` | Error budget: error_rate ≥ pct in window → degraded (503 on /health) |
| `lifecycle` | `idle_threshold_req_per_min`, `idle_window`, `minimum_lifespan` | Idle signal: requests/min < threshold for window after min lifespan |
| `cache` | `ttl` | Cache entry freshness; stale entries evicted |
| `reliability` | `retry_max_attempts` | Resilience; transient errors retried up to N times |
| `shutdown` | `timeout` | Grace period for in-flight requests before exit |

---

## SLI Definitions

### 1. Availability (Success Rate)

**SLI:** Proportion of `/weather/{location}` requests that succeed (200) vs. total requests (excluding health/metrics probes).

**Source:** `httpRequestsTotal{route="/weather/{location}", statusCode="200"}` vs. `httpRequestsTotal{route="/weather/{location}"}`

**Config driver:** `lifecycle.degraded_error_pct`—when error rate exceeds this, we declare degraded. Implicit SLO: "error rate stays below degraded_error_pct."

```
Availability SLI = 1 - (5xx + 429 in window) / total in window
SLO target: Availability ≥ (100 - degraded_error_pct)%
```

### 2. Latency (Request Duration)

**SLI:** Request duration for `/weather/{location}` (p50, p95, p99).

**Source:** `httpRequestDurationSeconds{route="/weather/{location}"}` histogram

**Config driver:** `request.timeout` is the hard ceiling. Requests that exceed it are terminated with 503.

```
Latency SLO: p99 ≤ request.timeout (hard); p95 < request.timeout (target)
```

### 3. Upstream Latency

**SLI:** OpenWeatherMap API call duration.

**Source:** `weatherApiDurationSeconds{status="success"}`

**Config driver:** `weather_api.timeout`—calls exceeding this fail (and may retry).

```
Upstream SLO: p95 < weather_api.timeout; alerts typically at p95 > 2s (see samples/alerting)
```

### 4. Error Budget (Degraded State)

**SLI:** Error rate over a sliding window (errors / (successes + errors) for weather requests).

**Source:** `internal/traffic` via `degraded.ErrorRate(window)`; exposed indirectly via `/health` status and `internal/degraded`

**Config driver:** `lifecycle.degraded_window`, `lifecycle.degraded_error_pct`

```
Error budget SLO: error_rate < degraded_error_pct in degraded_window
Breach: status = degraded; /health returns 503
```

### 5. Capacity (Overload)

**SLI:** Requests hitting the rate-limited path in a sliding window vs. capacity threshold.

**Source:** `rateLimitRequestsInWindow` (gauge), `rateLimitRejectsInWindow` (gauge); `overload.RequestCount(window)`, `overload.DenialCount(window)`

**Config driver:** `lifecycle.overload_window`, `lifecycle.overload_threshold_pct`, `reliability.rate_limit_rps`

```
Capacity threshold = rate_limit_rps × overload_window × (overload_threshold_pct / 100)
Overload SLO: requests in window ≤ threshold
Breach: status = overloaded; /health returns 503 (routing signal)
```

### 6. Rate Limit Rejects

**SLI:** Proportion of requests denied (429) due to rate limiting.

**Source:** `rateLimitDeniedTotal`, `rateLimitRejectsInWindow`

**Config driver:** `reliability.rate_limit_rps`, `reliability.rate_limit_burst`

```
Rejects occur when: sustained load > rate_limit_rps, or burst exceeds rate_limit_burst
SLO: minimize 429s; high rejects → scale up or increase limits
```

### 7. Cache Performance

**SLI:** Cache hit rate = hits / (hits + misses).

**Source:** `cacheHitsTotal`; misses = lookups - hits (derived)

**Config driver:** `cache.ttl`—longer TTL can improve hit rate; shorter improves freshness

```
Cache SLO: target hit rate (e.g. > 80%) for cost/latency; config does not enforce, observability only
```

---

## SLO Summary Table

| SLO | Config Keys | Target | Breach Signal |
|-----|-------------|--------|---------------|
| Availability | `degraded_window`, `degraded_error_pct` | Error rate < degraded_error_pct | status=degraded |
| Request latency | `request.timeout` | p99 ≤ timeout | 503 (handler timeout) |
| Upstream latency | `weather_api.timeout` | p95 < timeout | Upstream timeout/retry |
| Error budget | `degraded_window`, `degraded_error_pct` | errors/total < pct | status=degraded |
| Capacity | `overload_window`, `overload_threshold_pct`, `rate_limit_rps` | requests ≤ threshold | status=overloaded |
| Rate limit | `rate_limit_rps`, `rate_limit_burst` | Minimize 429s | 429 responses |
| Cache | `cache.ttl` | Hit rate (observability) | No automatic breach |

---

## Middleware and Config

| Middleware / Component | Config | SLO Impact |
|------------------------|--------|------------|
| TimeoutMiddleware | `request.timeout` | Hard limit on request duration |
| RateLimitMiddleware | `rate_limit_rps`, `rate_limit_burst` | Capacity; 429 when exceeded |
| MetricsMiddleware | — | Emits `httpRequestDurationSeconds`, `httpRequestsTotal` for SLI computation |
| overload package | `overload_window` | `RequestCount(window)` for overload SLO |
| degraded package | `degraded_window`, `degraded_error_pct` | Error rate for degraded SLO |
| client (OpenWeather) | `weather_api.timeout`, `retry_max_attempts` | Upstream latency, resilience |

---

## Alert Alignment

The alert rules in `samples/alerting/alert-rules.yaml` align with these SLOs:

- **HighHTTPErrorRate** (> 5% 5xx): availability SLO
- **HighHTTPLatency** (p95 > 5s): latency SLO (tune threshold to match `request.timeout`)
- **WeatherAPISlow** (p95 > 2s): upstream latency (tune to `weather_api.timeout`)
- **WeatherAPIHighErrorRate** (> 20%): upstream availability

Adjust alert thresholds to match your `config/[env].yaml` values. The lifecycle thresholds (`degraded_error_pct`, overload formula) are the canonical error-budget and capacity SLOs.

---

## References

- `docs/health-status-plan.md` — Lifecycle states and formulas
- `docs/observability-metrics-plan.md` — Metric definitions
- `docs/env-yaml-plan.md` — Full config schema
- `samples/alerting/` — Prometheus and Alertmanager config
