# Health Status Lifecycle Plan

## Goal

Extend `/health` from static `healthy`/`degraded` to a lifecycle-aware status that load managers (K8s, LB, autoscalers) can use for routing and scaling decisions.

## Status States

| Status | Meaning | HTTP | Load Manager Action |
|--------|---------|------|---------------------|
| `starting-up` | Awaiting first API call, cache warm, dependencies online | 503 | Do not route traffic |
| `healthy` | Can serve traffic; dependencies healthy | 200 | Route traffic |
| `overloaded` | Hitting limits; should shed load | 503 | Scale up, reduce weight, or back off |
| `idle` | Low traffic; candidate for scale-down | 200 | Optional: scale down |
| `degraded` | Producing/receiving errors; dependency issues; do not route | 503 | Stop routing; alert |
| `shutting-down` | Do not send new requests; draining in-flight | 503 | Remove from pool, stop routing |

### Config Values

Thresholds for status mapping live in `config/[env].yaml` under `lifecycle`. They avoid premature state transitions—e.g. require overload to persist before declaring it.

```yaml
lifecycle:
  ready_delay: "3s"             # Min uptime before healthy (avoids starting-up flicker)
  overload_window: "60s"        # Sliding window for overload calculation
  overload_threshold_pct: 80    # % of rate_limit_rps * window → overload if exceeded
  idle_threshold_req_per_min: 5 # Below this, consider idle
  idle_window: "5m"             # Sliding window for idle calculation
  minimum_lifespan: "5m"        # Min uptime before idle can be declared
  degraded_window: "60s"        # Window for error-rate calculation
  degraded_error_pct: 5        # Error rate (errors/total in window) above this % → degraded; scales with traffic
  degraded_retry_initial: "1m"   # First delay; Fibonacci builds from here
  degraded_retry_max: "20m"     # Max delay; sequence caps here, then shutdown
```

All durations use Go `time.ParseDuration` (`"s"`, `"m"`); scale for testing by changing values.

## Implementation Notes

### starting-up

- **Signals:** From process start until `ready_delay` elapsed.
- **Config:** `lifecycle.ready_delay`
- **HTTP:** 503 with `status: starting-up`. Single-node: narrow window; multi-node: awaiting cache sync.

### healthy

- **Signals:** `ready_delay` elapsed; endpoint available; API key validated at startup; dependencies healthy.
- **Data sources:** Uptime, `checks.weatherApi`, `checks.cache`
- **Config:** `lifecycle.ready_delay`
- **HTTP:** 200 with `status: healthy`

### overloaded (implemented)

- **Signals:** Approaching capacity (proactive). Requests in window exceed threshold before we start denying.
- **Implementation:** `internal/overload` sliding window of request timestamps (lifecycle) and denial timestamps (observability); `RecordRequest()` and `RecordDenial()` from middleware; `RequestCount(window)` and `DenialCount(window)`.
- **Config:** `lifecycle.overload_window` (disable by setting 0)
- **HTTP:** 503 with `status: overloaded`

Overloaded when requests in window > rate_limit_rps × overload_window × (overload_threshold_pct/100).

**Overload: /health vs /weather**

Overloaded means "at capacity"—we can still serve requests that get through. It is a routing/capacity signal, not "broken."

| Endpoint | Overloaded behavior |
|----------|----------------------|
| `/health` | Returns 503 with `status: overloaded`. Tells LBs/autoscalers: do not route new traffic here; scale up or shed load. |
| `/weather` | Never returns 503 for overload. Requests within rate limit get 200; excess requests get 429. The service remains capable of serving traffic. |

The 503 on `/health` is a proactive signal to stop sending this instance more work. `/weather` continues to return 200 or 429 per request.

**Design intent:** Denials are errors. Overload is health. No duplication: `/metrics` owns operational metrics; `/test` is synthetic/temporary and does not duplicate metrics.

**Metrics that matter (on `/metrics`)**

1. **Load** – throughput, capacity planning, how loaded is the endpoint
2. **Rejects** – are we returning 429; rate of denied requests

---

## Gap: Current Implementation vs Design

| Aspect | Design | Current | Gap |
|--------|--------|---------|-----|
| **Lifecycle formula** | Requests in window > threshold (proactive) | Uses `RequestCount` and formula | Aligned |
| **Requests tracking** | Sliding window for lifecycle | `overload.RecordRequest()`, `RequestCount(window)` | Aligned |
| **Denial tracking** | `RecordDenial()`, `DenialCount(window)` in overload | `RecordDenial()` in middleware; `DenialCount(window)` in package | Aligned |
| **Load metric** | Throughput/capacity on `/metrics` | `rateLimitRequestsInWindow` gauge (GaugeFunc) | Aligned |
| **Rejects metric** | Denials in window on `/metrics` | `rateLimitRejectsInWindow` gauge (GaugeFunc) | Aligned |

**Summary:** Lifecycle, denial tracking, and load/rejects gauges done. `/test` stays synthetic-only; no duplication.

---

## Implementation Sketch: Rectify Gaps

### 1. ~~Add denial tracking to `internal/overload`~~ Done

- ~~Add `RecordDenial()`~~ Done. Call from middleware when returning 429.
- ~~Add `DenialCount(window)`~~ Done. Sliding window of denial timestamps.
- ~~Tracker: `requestTimes` + `denialTimes`; `Reset()` clears both~~ Done.
- ~~Middleware: `RecordRequest()` for every request; `RecordDenial()` when returning 429~~ Done.

### 2. ~~Expose on `/metrics` (Prometheus)~~ Done

- ~~**Load:** Gauge for requests in window~~ Done. `rateLimitRequestsInWindow` (GaugeFunc).
- ~~**Rejects:** Gauge for denials in window~~ Done. `rateLimitRejectsInWindow` (GaugeFunc). Complement to cumulative `rateLimitDeniedTotal`.

### degraded (implemented)

- **Signals:** API key validation fails on health probe; or error rate (errors/total in window) within `degraded_window` exceeds `degraded_error_pct`.
- **Implementation:** API key probe on each health check; `internal/degraded` sliding window of successes+errors for error-rate; Fibonacci recovery via `NotifyDegraded` and `StartRecoveryListener`.
- **Data sources:** Health probe `ValidateAPIKey`; `RecordSuccess`/`RecordError` from weather handler.
- **Config:** `lifecycle.degraded_window`, `lifecycle.degraded_error_pct`, `lifecycle.degraded_retry_initial`, `lifecycle.degraded_retry_max`.
- **HTTP:** 503 with `status: degraded`

**Recovery from degraded**

When degraded (API key or error-rate), retry the startup routine (API key validation, and a test weather call) with Fibonacci backoff. Build sequence from `degraded_retry_initial` (e.g. 1m → 1m, 2m, 3m, 5m, 8m, 13m); cap at `degraded_retry_max`. After max delay, one more attempt; if still degraded, shutdown.

### idle (implemented)

- **Signals:** Uptime &gt; `minimum_lifespan` and weather-request rate below `idle_threshold_req_per_min` for `idle_window`.
- **Implementation:** `internal/idle` sliding window of weather-request timestamps.
- **Config:** `lifecycle.idle_threshold_req_per_min`, `lifecycle.idle_window`, `lifecycle.minimum_lifespan` (disable by setting any to 0)
- **HTTP:** 200 with `status: idle`; LB can use for scale-down. Optional.

### shutting-down (implemented)

- **Signal:** Process received SIGTERM/SIGINT; `srv.Shutdown()` in progress.
- **Implementation:** `internal/lifecycle` atomic flag set when `<-ctx.Done()` in main. Health handler checks flag first and returns 503.
- **HTTP:** 503 with `status: shutting-down` so LB stops routing immediately.

## Priority Order

1. **shutting-down** — Done. Atomic flag in `internal/lifecycle`, wired in main, health checks it.
2. **overloaded** — Done. Sliding window of requests (lifecycle) and denials (observability). Config `overload_window`, `overload_threshold_pct`. Gauges `rateLimitRequestsInWindow` and `rateLimitRejectsInWindow` on `/metrics`.
3. **idle** — Done. Sliding window of weather requests, config `idle_window`, `minimum_lifespan`, `idle_threshold_req_per_min`.
4. **degraded (error-rate + recovery)** — Done. Error rate % in window; `NotifyDegraded` triggers Fibonacci backoff recovery; exhausted → shutdown.
5. **starting-up** — Defer; current startup validation makes it a narrow window.

## Cache / Redis / Memcached

- **In-memory today:** Cache is always "healthy" if process is up; no probe.
- **Future Redis/Memcached:** Add `cache` check that pings the store. `ready` requires cache reachable (or optionally degrade to read-through if cache is optional). `checks.cache` would reflect connection status.
- Context: Health handler already uses `context.WithTimeout`; cache probe would use the same pattern for bounded checks.

## HTTP Status Code Mapping

- `ready`, `idle` → 200 (accept traffic)
- `starting-up`, `overloaded`, `shutting-down`, `degraded` → 503 (do not route)

## Response Shape (unchanged structure, richer status)

```json
{
  "status": "ready",
  "service": "weather-alert-service",
  "version": "dev",
  "checks": {
    "cache": "healthy",
    "weatherApi": "healthy"
  }
}
```

`status` values: `starting-up` | `ready` | `overloaded` | `idle` | `shutting-down` | `degraded`
