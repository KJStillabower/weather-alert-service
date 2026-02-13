# Concurrency Limit Plan (Load Shedding)

## Goal

Add a maximum concurrent request limit to prevent queue buildup and cascading latency. When in-flight requests exceed the limit, reject new requests immediately with 503 instead of queuing them. This complements existing rate limiting (requests/second) with a limit on parallelism (requests in flight).

## Rationale

- **Rate limiting** controls throughput (requests per second) but does not cap concurrency. A burst of slow requests (e.g. upstream timeouts) can accumulate in the handler queue.
- **Queue buildup** increases latency for all in-flight requests and risks memory/connection pressure.
- **Fast rejection** (503) lets clients retry elsewhere or fail fast instead of waiting for a slot that may not arrive soon.
- **Metric exists:** `httpRequestsInFlight` already tracks in-flight count; we use it as the basis for the limit.

## Config

Add under `reliability` or a new `concurrency` section in `config/[env].yaml`:

```yaml
reliability:
  # ... existing rate_limit_rps, rate_limit_burst, retry_* ...
  max_concurrent_requests: 50   # 0 = disabled (no limit)
```

- **`max_concurrent_requests`:** Maximum number of requests allowed in flight at once. When exceeded, return 503.
- **`0` or omit:** Disable concurrency limiting (current behavior).
- **Tuning:** Set below typical saturation point; e.g. if latency degrades above 30 in-flight, use 25–30.

## Implementation

### Middleware

Insert `ConcurrencyLimitMiddleware` after `MetricsMiddleware` (which already increments/decrements `httpRequestsInFlight`) and before `RateLimitMiddleware` on the weather router. Order:

1. CorrelationID (all routes)
2. Metrics (all routes) — Inc at entry, Dec at exit
3. ConcurrencyLimit (weather only) — TryAcquire before handler; if denied, 503
4. RateLimit (weather only)
5. Timeout (weather only)
6. GetWeather handler

**Option A: Semaphore before Metrics**

- Use `semaphore.Weighted` or `errgroup`-style semaphore.
- Acquire at middleware entry; release in defer.
- If Acquire fails (context cancelled or no capacity), return 503 immediately, **before** incrementing `httpRequestsInFlight` so we do not count rejected requests as in-flight.

**Option B: Check in-flight gauge**

- Read `httpRequestsInFlight` and compare to `max_concurrent_requests`. Problem: gauge is not atomic for “check-then-increment” without race; MetricsMiddleware does Inc then Dec. We would need to either:
  - Move the concurrency check inside MetricsMiddleware, or
  - Use a separate semaphore/counter owned by the concurrency middleware.

**Recommended: dedicated semaphore**

- ConcurrencyLimitMiddleware maintains its own semaphore with `max_concurrent_requests` capacity.
- Acquire with `TryAcquire(1)` or `Acquire(ctx)` with immediate timeout; on failure return 503.
- Apply only to `/weather` (same place as RateLimit). `/health` and `/metrics` are excluded so probes and scrapers are not blocked.

### Response

On limit exceeded:

- **Status:** 503 Service Unavailable
- **Body:** JSON error similar to rate limit: `{"error": {"code": "CAPACITY_EXCEEDED", "message": "Too many concurrent requests", "requestId": "<correlation_id>"}}`
- **Header:** `Retry-After` optional (e.g. 1–5 seconds) as a hint for clients.

### Metrics

- **New counter:** `concurrencyLimitRejectedTotal` — incremented each time a request is rejected due to concurrency limit.
- **Existing:** `httpRequestsInFlight` — unchanged; rejected requests never enter the handler, so they are not counted as in-flight.
- **Alerting:** Add rule in `samples/alerting/alert-rules.yaml` for sustained high rejections (e.g. `rate(concurrencyLimitRejectedTotal[5m]) > 0.1` as warning that capacity is undersized or traffic spiky).

## Integration with Lifecycle

- **Overload:** Concurrency rejections can be folded into overload logic (e.g. count toward `overload.RecordDenial()` or a separate concurrency-denial counter) so that sustained concurrency shedding contributes to `status: overloaded` on `/health`. Alternatively, keep them separate: rate-limit denials vs. concurrency denials for different alerting.
- **Health probes:** `/health` and `/metrics` bypass concurrency limit (not on weather router) so load balancers and Prometheus can always reach the service.

## Paths

| Path       | Rate Limit | Concurrency Limit |
|------------|------------|--------------------|
| `/health`  | No         | No                 |
| `/metrics` | No         | No                 |
| `/weather/*` | Yes      | Yes                |

## References

- `docs/health-status-plan.md` — Overload state and lifecycle
- `internal/http/middleware.go` — Existing middleware, `httpRequestsInFlight`
- `internal/observability/metrics.go` — Metric registration
- `samples/alerting/alert-rules.yaml` — Alert rule examples

---

## Implementation Plan: Dedicated Semaphore

Step-by-step implementation using `golang.org/x/sync/semaphore.Weighted`. ConcurrencyLimit runs **before** Metrics so rejected requests are not counted in `httpRequestsInFlight`.

### 1. Config

**File:** `internal/config/config.go`

- Add `MaxConcurrentRequests int` to `Config` struct.
- In `fileConfig.Reliability`, add `MaxConcurrentRequests int yaml:"max_concurrent_requests"`.
- In `Load()`, set `cfg.MaxConcurrentRequests = fc.Reliability.MaxConcurrentRequests`. Default 0 = disabled.

**File:** `config/dev.yaml`, `config/prod.yaml`

- Add under `reliability`: `max_concurrent_requests: 50` (or 0 for dev if not testing).

### 2. Metrics

**File:** `internal/observability/metrics.go`

- Add `ConcurrencyLimitRejectedTotal prometheus.Counter` with Help: `"Total requests rejected by concurrency limit (503)"`.
- Register in `init()` with other counters.

### 3. Middleware

**File:** `internal/http/middleware.go`

- Import `"golang.org/x/sync/semaphore"`.
- Add `ConcurrencyLimitMiddleware(sem *semaphore.Weighted, pathPrefix string) mux.MiddlewareFunc`:
  - If `sem == nil`, return passthrough.
  - In handler: if `!strings.HasPrefix(r.URL.Path, pathPrefix)`, call `next.ServeHTTP(w, r)` and return.
  - For matching paths: `if !sem.TryAcquire(1)` then increment `observability.ConcurrencyLimitRejectedTotal`, call `writeConcurrencyLimitError(w, r)`, return.
  - Otherwise `defer sem.Release(1)` then `next.ServeHTTP(w, r)`.
- Add `writeConcurrencyLimitError(w, r)`: 503, `Content-Type: application/json`, body `{"error":{"code":"CAPACITY_EXCEEDED","message":"Too many concurrent requests","requestId":"<correlation_id>"}}`, header `Retry-After: 2`.
- Use same correlation ID extraction as `writeRateLimitError`.

### 4. Main Wiring

**File:** `cmd/service/main.go`

- Import `"golang.org/x/sync/semaphore"`.
- After config load: `var sem *semaphore.Weighted`; if `cfg.MaxConcurrentRequests > 0`, `sem = semaphore.NewWeighted(int64(cfg.MaxConcurrentRequests))`.
- Insert ConcurrencyLimit **before** Metrics on the main router:
  ```
  router := mux.NewRouter()
  router.Use(httphandler.CorrelationIDMiddleware(logger))
  router.Use(httphandler.ConcurrencyLimitMiddleware(sem, "/weather"))
  router.Use(httphandler.MetricsMiddleware)
  router.HandleFunc("/health", ...)
  ...
  ```
- Pass `nil` when disabled; middleware returns passthrough.

### 5. Router Order Summary

| Order | Middleware              | Scope  | Notes                                                    |
|-------|-------------------------|--------|----------------------------------------------------------|
| 1     | CorrelationIDMiddleware | All    |                                                          |
| 2     | ConcurrencyLimitMiddleware | All    | Path check; only `/weather` hits semaphore; others pass |
| 3     | MetricsMiddleware       | All    | Rejected requests never reach here                      |
| 4+    | RateLimit, Timeout      | Weather only | On weatherRouter subrouter                          |

### 6. Dependencies

- Add to `go.mod`: `golang.org/x/sync` (likely already present; `go get golang.org/x/sync/semaphore` if not).

### 7. Testing

- **Unit:** `middleware_test.go`: mock handler; run N concurrent requests with `max_concurrent_requests: 2`; expect 2 success, rest 503. Verify `ConcurrencyLimitRejectedTotal` increments.
- **Integration:** `test-service.sh` or manual: set low limit (e.g. 3), run `curl` in parallel (e.g. 10); expect mix of 200 and 503.
- **Disabled:** `max_concurrent_requests: 0` or omit; all requests pass.

### 8. Alert Rule (Optional)

**File:** `samples/alerting/alert-rules.yaml`

- Add to `weather-service-request-layer`:
  ```yaml
  - alert: ConcurrencyLimitRejecting
    expr: rate(concurrencyLimitRejectedTotal{job="weather-alert-service"}[5m]) > 0.1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Concurrency limit rejecting requests"
      description: "{{ $value | humanize }}/s rejections; consider increasing max_concurrent_requests."
  ```
