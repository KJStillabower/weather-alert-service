# Observability Guide

Single operational guide for the Weather Alert Service. Use this document to interpret metrics, logs, health status, and alerts.

## Overview

Observability consists of:

- **[Health](#health-and-lifecycle)** (`GET /health`) — Lifecycle-aware status for routing and scaling
- **[Metrics](#metrics)** (`GET /metrics`) — Prometheus-format counters, histograms, gauges
- **[Logging](#logging)** — Structured JSON to stderr; decisions, boundaries, failures
- **[Alerting](#alerts-and-runbooks)** — Sample rules in `samples/alerting/`

Our observability strategy treats each pillar as a distinct signal with a single responsibility. Health exposes lifecycle state so load balancers and orchestrators know when to route traffic; metrics quantify performance for dashboards, SLOs, and alerting; logging captures context for the events that matter (decisions, boundaries, failures) without duplicating what metrics already convey; alerts escalate when thresholds are breached. Together they answer the questions operators need: Is the service ready? How is it performing? What went wrong? Why?

We avoid overlap and noise. Metrics cover routine success paths (request counts, latencies, cache hits), so we do not log routine requests or cache hits. Logging focuses on actionable context: important choices, boundary conditions (rate limits, timeouts, capacity), and failures. Health synthesizes internal state into a routing signal (200 vs 503) rather than raw telemetry. This keeps each channel high-signal and prevents logging from becoming a source of instability when the system is degraded.

## Metrics

### Summary Table

#### Request

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `httpRequestsTotal` | Counter | method, route, statusCode | Total requests; `rate()` for QPS | Sudden drops (outage), spikes (surge); error rate by statusCode |
| `httpRequestDurationSeconds` | Histogram | method, route | Request latency; p95/p99 for SLOs | p95/p99 increases; SLO breaches; approaching request timeout |
| `httpRequestsInFlight` | Gauge | — | Concurrent requests | Saturation; sustained high = capacity or slow downstream |
| `httpRequestSizeBytes` | Histogram | method, route | Request body size in bytes | DoS; unusually large payloads; capacity planning |
| `httpResponseSizeBytes` | Histogram | method, route, statusCode | Response body size in bytes | Large payloads; capacity planning |

#### External API

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `weatherApiCallsTotal` | Counter | status | OpenWeatherMap calls; status: success, error, rate_limited, client_error, server_error | Error vs success ratio; rate_limited = API quota |
| `weatherApiDurationSeconds` | Histogram | status | Upstream latency | p95 > 2s (degradation); p99 > 5s (timeout risk) |
| `weatherApiRetriesTotal` | Counter | — | Retry attempts | High rate = unstable upstream; transient failures |
| `weatherApiErrorsTotal` | Counter | category | Weather API errors by category (timeout, network, invalid_api_key, rate_limited, upstream_5xx, parsing, etc.) | Error mix; debugging; see client.CategorizeError |

#### Cache

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `cacheHitsTotal` | Counter | cacheType | Cache hits; hit rate = hits / weatherQueriesTotal | Low hit rate; diminishing freshness vs cost trade-off |
| `cacheStampedeDetectedTotal` | Counter | location | Times concurrent cache misses for same key exceeded 1 (stampede) | Thundering herd; consider request coalescing |
| `cacheStampedeConcurrency` | Histogram | location | Concurrent miss count when stampede detected | Severity of stampede; per-key load |
| `cacheErrorsTotal` | Counter | operation, type | Cache errors by operation (get, set) and type (timeout, connection, unknown) | High rate = cache backend issues; alert on connection type |
| `cacheOperationDurationSeconds` | Histogram | operation, status | Get/Set duration; status success or error | Slow or failing cache ops; p95 by status |

#### Business

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `weatherQueriesTotal` | Counter | — | Total lookups; `rate()` for QPS | Traffic volume; unexpected drop or spike |
| `weatherQueriesByLocationTotal` | Counter | location | Per-location (allow-list; others = "other") | Top locations; "other" dominating = add to allow-list |

#### Errors (HTTP)

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `httpErrorsTotal` | Counter | method, route, category | HTTP errors by category (timeout, upstream_5xx, validation, etc.) | Error mix; debugging; aligns with writeServiceError |

#### Rate limit

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `rateLimitDeniedTotal` | Counter | — | Cumulative 429 denials | Growing = at capacity; scale or increase limits |
| `rateLimitRequestsInWindow` | Gauge | — | Requests in sliding window | Approaching overload threshold; capacity planning |
| `rateLimitRejectsInWindow` | Gauge | — | 429s in window | Actively rejecting; overloaded right now |

#### Runtime

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| process_*, go_* | — | — | CPU, memory, goroutines, threads (Prometheus collectors) | High sustained CPU; memory growth (leak); goroutine spike (leak) |

#### Graceful shutdown and reliability

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `shutdownInFlightRequests` | Gauge | — | In-flight request count recorded once at shutdown (before waiting) | Non-zero after shutdown = requests were still in flight; tune shutdown timeout if needed |
| `circuitBreakerState` | Gauge | component | Circuit breaker state: 0=closed, 1=open, 2=half-open | 1 = failing fast; 2 = probing; sustained open = upstream unhealthy |
| `circuitBreakerTransitionsTotal` | Counter | component, from, to | State transitions (e.g. closed→open, open→half_open) | Flapping; recovery frequency |
| `requestTimeoutPropagatedTotal` | Counter | propagated | Count where upstream timeout was derived from request context (yes) or not (no) | Ratio yes/no shows how often request deadlines are propagated to upstream |

**Circuit breaker:** When enabled, the weather API client wraps calls in a circuit breaker. After a configurable failure threshold the circuit opens and requests fail immediately; after a timeout it goes half-open and a success threshold closes it. Use `circuitBreakerState` and `circuitBreakerTransitionsTotal` to see open/half-open events and recovery.

**Request timeout propagation:** If the incoming request has a deadline, the client uses up to 90% of the remaining time for the upstream call (capped by configured client timeout, minimum 100ms). This keeps upstream calls within the request timeout budget. `requestTimeoutPropagatedTotal{propagated="yes"}` increments when the timeout was taken from context.

**Cache error alerting:** Sample alerts in `samples/alerting/alert-rules.yaml` include `HighCacheErrorRate` (e.g. >10% cache error rate over 5m) and `CacheBackendDown` (connection-type cache errors). Use `cacheErrorsTotal` and `cacheOperationDurationSeconds` to investigate cache backend health.

**Route labels:** Use path templates (e.g. `/weather/{location}`) to avoid cardinality explosions.

**Location cardinality:** `weatherQueriesByLocationTotal` uses a fixed allow-list from `config/[env].yaml` under `metrics.tracked_locations`. Queries for locations not on the list increment `location="other"`. The allow-list limits label cardinality to a fixed set (e.g. 100 locations + "other"), preventing unbounded series growth: an attacker sending arbitrary location strings cannot exhaust Prometheus memory or poison the metrics store. If the "other" share grows and you need per-location visibility for new hotspots, add those locations to `metrics.tracked_locations` in config.

---

## PromQL Cookbook

Common queries for dashboards and ad-hoc investigation:

| Question | Query |
|----------|-------|
| **Request QPS (all routes)** | `sum(rate(httpRequestsTotal[5m]))` |
| **Request QPS (weather only)** | `sum(rate(httpRequestsTotal{route="/weather/{location}"}[5m]))` |
| **5xx error rate** | `sum(rate(httpRequestsTotal{statusCode=~"5.."}[5m])) / sum(rate(httpRequestsTotal[5m]))` |
| **p95 request latency** | `histogram_quantile(0.95, sum(rate(httpRequestDurationSeconds_bucket[5m])) by (le, route))` |
| **p99 request latency** | `histogram_quantile(0.99, sum(rate(httpRequestDurationSeconds_bucket[5m])) by (le, route))` |
| **Cache hit rate** | `sum(rate(cacheHitsTotal[5m])) / sum(rate(weatherQueriesTotal[5m]))` |
| **Upstream API error rate** | `sum(rate(weatherApiCallsTotal{status=~"error|server_error|rate_limited"}[5m])) / sum(rate(weatherApiCallsTotal[5m]))` |
| **Upstream p95 latency** | `histogram_quantile(0.95, sum(rate(weatherApiDurationSeconds_bucket{status="success"}[5m])) by (le))` |
| **Top locations (1h)** | `topk(10, sum by (location)(rate(weatherQueriesByLocationTotal[1h])))` |
| **"Other" location share** | `rate(weatherQueriesByLocationTotal{location="other"}[1h]) / rate(weatherQueriesTotal[1h])` |
| **CPU utilization** | `rate(process_cpu_seconds_total[1m])` |
| **Errors by category (API)** | `sum by (category)(rate(weatherApiErrorsTotal[5m]))` |
| **Errors by category (HTTP)** | `sum by (route, category)(rate(httpErrorsTotal[5m]))` |
| **SLO availability (after recording rules)** | `http:availability:ratio` |
| **SLO error budget remaining** | `http:error_budget_remaining:ratio` |

---

## Logging

### Format and Output

- **Format:** Structured JSON (zap)
- **Output:** stderr (suitable for container/process capture)
- **Timestamps:** ISO8601 UTC

### Philosophy

We log **decisions, boundaries, and failures**. Routine successful requests are not logged; metrics cover those.

| Log | When |
|-----|------|
| **Decisions** | Cache backend selection, eviction, fallback behavior |
| **Boundaries** | Rate limits hit, timeouts, capacity thresholds |
| **Failures** | Weather fetch errors, health degraded, shutdown errors |
| **State changes** | Startup, shutdown, config-related events |

### What We Do NOT Log

- Routine successful requests (metrics cover this)
- Cache hits (metrics cover this)
- Sensitive data: API keys, credentials (see 090-security.mdc)
- Speculative debugging detail

### Configuration

- **LOG_LEVEL:** Env var only (`DEBUG`, `INFO`, `WARN`, `ERROR`). Default `INFO`.

### Log Events by Code Path

This section lists log events that can be generated in each code path. No log event = that path does not produce logs (by design; metrics cover routine flows).

#### GET /weather/{location}

| Log Event | Level | When | Fields |
|-----------|-------|------|--------|
| `cache hit` | DEBUG | Cache returns data for location | `location` |
| `cache miss, fetching upstream` | DEBUG | Cache miss; about to call OpenWeatherMap | `location` |
| `weather served` | DEBUG | Success (cache or upstream) | `location`, `cached`, `duration` |
| `rate limit denied` | DEBUG | Rate limiter returns 429 | (correlation_id from context) |
| `upstream error` | DEBUG | Weather fetch fails (503 to client) | `error` |

At INFO (default), routine success, cache hits, and 429 responses produce no logs. Set `LOG_LEVEL=DEBUG` when troubleshooting to trace request flow.

#### GET /health

| Log Event | Level | When | Fields |
|-----------|-------|------|--------|
| `health status transition` | INFO | Status changes (e.g. healthy -> degraded, overloaded -> healthy) | `previous_status`, `current_status`, `reason` |

Reasons: `api_key_invalid`, `error_rate_breach`, `overload_threshold`, `signal` (shutting-down), `low_traffic` (idle). Routine probes when status is unchanged produce no logs.

#### GET /metrics

Metrics probes produce no application logs.

#### GET /test, POST /test/{action} (testing mode)

Test endpoints are not intended for production, and produce no logs directly.
When leveraged, they may trigger other code-paths but will also trigger a startup warning as they should not be run in production.

#### Startup (main)

| Log Event | Level | When | Fields |
|-----------|-------|------|--------|
| `cache backend: [type]` | INFO | Cache backend = `type` | `addrs` |
| `Testing mode enabled; /test endpoint exposed` | WARN | `testing_mode: true` in config | — |
| `server starting` | INFO | HTTP server begins listening | `addr` |
| `config` | FATAL | Config load fails | `error` |
| `weather client` | FATAL | Weather client init fails | `error` |
| `memcached cache` | FATAL | Memcached connection fails | `error` |
| `server` | FATAL | ListenAndServe fails (non-ErrServerClosed) | `error` |

#### Shutdown (main)

| Log Event | Level | When | Fields |
|-----------|-------|------|--------|
| `graceful shutdown triggered` | INFO | SIGTERM/SIGINT received; draining begins | — |
| `shutdown` | ERROR | Server shutdown exceeds timeout | `error` |
| `memcached close` | ERROR | Memcached Close() fails | `error` |

---

## Correlation IDs

Every request has a correlation ID for tracing.

| Where | How |
|-------|-----|
| **Request** | Header `X-Correlation-ID` (client may supply; otherwise generated UUID) |
| **Context** | Stored in request context; available to handlers |
| **Response** | Echoed in response header `X-Correlation-ID` |
| **Logs** | Structured field `correlation_id` in request-scoped log entries |
| **Upstream** | Propagated to OpenWeatherMap via `X-Correlation-ID` header |

### Troubleshooting with Correlation IDs

1. Client receives error response; note `X-Correlation-ID` from response header (or `error.requestId` in JSON body).
2. Search logs for `"correlation_id":"<uuid>"` to see all log entries for that request.
3. Use correlation ID to trace: handler entry, upstream call, cache behavior, error path.

---

## Health and Lifecycle

`GET /health` returns a lifecycle-aware status. Use it for load balancer health checks, Kubernetes readiness/liveness, and autoscaling.

### Status Values

| Status | Meaning | HTTP | Load Manager Action |
|--------|---------|------|---------------------|
| `healthy` | Can serve traffic; dependencies OK | 200 | Route traffic |
| `idle` | Low traffic; candidate for scale-down | 200 | Optional: scale down |
| `overloaded` | At capacity; shed load | 503 | Scale up, reduce weight, back off |
| `degraded` | Errors; dependency issues | 503 | Stop routing; alert |
| `shutting-down` | Draining; do not send new requests | 503 | Remove from pool |

**503 = do not route.** LBs and K8s should stop sending traffic when status is overloaded, degraded, or shutting-down.

**Overloaded vs degraded:** Overloaded = at capacity; service still serves requests that get through. Degraded = producing/receiving errors; do not route.

### Response Shape

```json
{
  "status": "healthy",
  "service": "weather-alert-service",
  "version": "dev",
  "checks": {
    "weatherApi": "healthy",
    "cache": "healthy"
  },
  "timestamp": "2026-02-12T10:20:30Z"
}
```

---

## Config-to-SLO Quick Reference

SLOs are driven by config. Changing `config/[env].yaml` changes effective targets.

| SLO | Config Keys | Target | Breach Signal |
|-----|-------------|--------|---------------|
| Availability | `lifecycle.degraded_window`, `degraded_error_pct` | Error rate < pct in window | status=degraded |
| Request latency | `request.timeout` | p99 ≤ timeout | 503 (handler timeout) |
| Upstream latency | `weather_api.timeout` | p95 < timeout | Upstream timeout/retry |
| Capacity | `lifecycle.overload_window`, `overload_threshold_pct`, `reliability.rate_limit_rps` | Requests ≤ threshold in window | status=overloaded |
| Rate limit | `rate_limit_rps`, `rate_limit_burst` | Minimize 429s | 429 responses |
| Cache | `cache.ttl` | Hit rate (observability only) | No automatic breach |

**Formulas:**

- **Overload threshold:** `requests in window > rate_limit_rps × overload_window × (overload_threshold_pct/100)` → overloaded
- **Degraded:** `error_rate ≥ degraded_error_pct` in `degraded_window` → degraded
- **Idle:** `requests/min < idle_threshold_req_per_min` for `idle_window` after `minimum_lifespan` → idle

---

## SLO Tracking (Recording Rules)

Prometheus recording rules in `samples/alerting/recording-rules-slo.yaml` compute SLO-related time series from raw metrics. Load them via `rule_files` in `prometheus.yaml` (see `samples/alerting/prometheus.yaml`).

### SLO Targets (defaults in recording rules)

| SLO | Target | Recording rule | Compliance rule |
|-----|--------|----------------|-----------------|
| Availability | 99.9% | `http:availability:ratio` (1 - error_rate) | `http:slo_availability:compliance` (1 = OK) |
| Error rate | ≤ 0.1% | `http:error_rate:ratio` | — |
| Error budget | Remaining ratio | `http:error_budget_remaining:ratio` (1 = full, 0 = exhausted) | — |
| Latency p95 | ≤ 1s | `http:latency_p95:seconds` (per route) | `http:slo_latency_p95:compliance` |
| Latency p99 | ≤ 2s | `http:latency_p99:seconds` (per route) | `http:slo_latency_p99:compliance` |

Targets are defined in the recording rule file header and in the compliance expressions. Tune them to match your policy (e.g. 0.999 for 99.9%, 0.001 for 0.1% error rate).

### PromQL examples (SLO)

| Question | Query |
|----------|-------|
| **Current availability** | `http:availability:ratio` |
| **Error budget remaining** | `http:error_budget_remaining:ratio` |
| **Availability SLO compliant?** | `http:slo_availability:compliance` (1 = yes, 0 = breach) |
| **p95 latency by route** | `http:latency_p95:seconds` |
| **Routes breaching p95 SLO** | `http:slo_latency_p95:compliance == 0` |
| **Burn rate (error rate over 1h)** | `sum(rate(httpRequestsTotal{statusCode=~"5.."}[1h])) / sum(rate(httpRequestsTotal[1h]))` |

Validate the recording rules file with `promtool check rules samples/alerting/recording-rules-slo.yaml` when Prometheus tooling is available.

---

## Alerts and Runbooks

Alerts are defined in `samples/alerting/alert-rules.yaml`. Environment-specific rules are available:
- `alert-rules-dev.yaml` - Development (more lenient)
- `alert-rules-prod.yaml` - Production (stricter)

For alert threshold rationale, tuning guidance, and environment-specific recommendations, see `docs/alerting-thresholds.md`.

Tune thresholds to match `config/[env].yaml` and your SLOs.

| Alert | When It Fires | What to Check |
|-------|---------------|---------------|
| **WeatherServiceDown** | Target unreachable 1m | Process crash, network, port; check logs and process list |
| **HighHTTPErrorRate** | > 5% 5xx over 5m | Upstream failures, timeouts; check `weatherApiCallsTotal` and logs |
| **HighHTTPLatency** | p95 > 5s for 5m | Slow handler or upstream; correlate with `weatherApiDurationSeconds` |
| **HighRequestSaturation** | In-flight > 50 for 5m | Capacity or slow downstream; consider scale-up or increase limits |
| **WeatherAPIHighErrorRate** | > 20% API errors over 5m | OpenWeatherMap issues; check API key, rate limits, upstream status |
| **WeatherAPISlow** | p95 > 2s for 5m | Upstream degradation; may trigger retries and latency |
| **WeatherAPIHighRetries** | > 1 retry/s over 5m | Unstable upstream; transient failures |
| **HighMemoryUsage** | RSS > 500MB for 10m | Possible leak; profile or restart |
| **HighGoroutineCount** | > 500 for 10m | Goroutine leak; profile or restart |

---

## When to Investigate

| Signal | Action |
|--------|--------|
| `/health` returns 503 | Check status value; degraded → upstream/API key; overloaded → scale or increase limits |
| High 5xx in metrics | Correlate with upstream API errors; check `weatherApiCallsTotal` and logs |
| High latency (p95/p99) | Check `weatherApiDurationSeconds`; slow upstream drives handler latency |
| High `rateLimitRejectsInWindow` | At capacity; scale or adjust `rate_limit_rps`/`rate_limit_burst` |
| High `weatherApiRetriesTotal` | Upstream instability; monitor OpenWeatherMap status |
| Low cache hit rate | Review `cache.ttl`; consider longer TTL if freshness allows |
| Rising memory or goroutines | Possible leak; capture profile, consider restart |

---

## Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: 'weather-alert-service'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /metrics
```

Adjust `targets` for your deployment (e.g. service discovery, multiple instances).

---

## References

| Document | Purpose |
|----------|---------|
| `docs/observability-metrics-plan.md` | Metric design, cardinality, allow-list |
| `docs/plans/observability-improvements-plan.md` | Observability improvements (size metrics, stampede, error categories, SLO recording rules) |
| `docs/service-level-objective-plan.md` | SLO definitions, config mapping |
| `docs/health-status-plan.md` | Lifecycle states, formulas |
| `samples/alerting/` | Prometheus, alert rules, Alertmanager, recording-rules-slo.yaml |
| `050-observability.mdc` | Rules and conventions |
| `docs/issue-improved-observability-documentation.md` | Issue tracking for further improvements |
