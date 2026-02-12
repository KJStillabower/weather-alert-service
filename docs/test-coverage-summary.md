# Test Coverage Summary

Generated from `go test ./... -cover`.

## Testing Strategy

Tests exist to **prevent incidents**: catch regressions before production, document behavior, and give confidence to change code. Coverage is an outcome, not the objective.

### Priorities

1. **Critical paths** — Happy path for core flows (weather lookup, health, lifecycle). A broken success path affects every request.
2. **Failure modes** — Upstream errors, timeouts, invalid config, rate limiting. These cause outages when mishandled.
3. **Error propagation** — Verify that failures map correctly to HTTP status and client behavior (retry vs no retry).
4. **Configuration and lifecycle** — Config load, health status transitions (healthy, degraded, overloaded, idle). Misconfiguration or wrong health transitions confuse operators and load balancers.

### What We Test and Why

| Layer | Risk if Untested | What We Verify |
|-------|------------------|----------------|
| **Client** | Wrong retry behavior, bad error handling | Retries on 5xx/timeout; no retry on 4xx; timeouts and cancellation |
| **Config** | Bad startup, wrong env/secret resolution | Load, validation, duration parsing, defaults |
| **Handlers** | Wrong API contract, bad status codes | Success, validation, error mapping, health states |
| **Service** | Cache/upstream logic bugs | Cache hit/miss, upstream failure, cache error fallback |
| **Lifecycle** | Health lies to LB | Overload, degraded, idle, shutting-down transitions |
| **Degraded recovery** | Recovery never runs or loops | Fibonacci backoff, onExhausted, test overrides |

### What We Skip and Why

- **main()** — Wiring only; integration tests cover startup and routing.
- **Memcached (unit)** — Requires real server; integration tests when available.
- **Data structs** — No behavior to assert.
- **Trivial registration** — Low risk; exercised in integration.

### Rule

Do not add tests solely to raise coverage. Add tests when a failure would cause an incident operators must debug.

## Overall

**79.5%** total statement coverage

## By Package

| Package | Coverage | Notes |
|---------|----------|-------|
| cmd/service | 0.0% | main entrypoint; not unit tested |
| internal/cache | 20.4% | In-memory covered; memcached not unit tested |
| internal/client | 93.5% | Strong |
| internal/config | 87.3% | Strong |
| internal/degraded | 89.9% | Core + recovery paths |
| internal/http | 90.2% | Handlers including /test |
| internal/idle | 96.2% | High |
| internal/lifecycle | 100.0% | Fully covered |
| internal/models | N/A | No test files |
| internal/observability | 90.9% | Strong |
| internal/overload | 100.0% | Fully covered |
| internal/service | 100.0% | Fully covered |
| internal/traffic | 98.4% | High |

## What We Test

### internal/cache

- **In-memory:** Get/Set, cache miss, TTL expiry
- **Memcached (integration):** Get/Set, miss against live server

### internal/client

- Client creation with invalid API key
- GetCurrentWeather success path
- Error handling (4xx, 5xx, invalid JSON)
- Retry logic (retries on 5xx/429/timeout; no retry on 4xx)
- Context cancellation and timeout
- Correlation ID propagation
- Response mapping
- Backoff calculation
- ValidateAPIKey
- Error response parsing (503, 504)
- isRetryable for timeout errors

### internal/config

- Load without API key (fails)
- Load from secrets file
- Config file not found
- Duration parsing (empty, invalid, defaults)
- Validation (weather API timeout)
- Invalid YAML (secrets, config)
- Load from env override
- Lifecycle/overload config
- Testing mode defaults

### internal/degraded

- Error rate (empty, success+error mix, expiry, reset)
- **Recovery:** Fibonacci delays, cap at max
- RunRecovery recovers on validate success
- RunRecovery calls onExhausted when validate always fails
- SetRecoveryDisabled / IsRecoveryDisabled
- ClearRecoveryOverrides
- SetForceFailNextAttempt / SetForceSucceedNextAttempt (test overrides)
- RunRecovery skips when recoveryDisabled
- GetAndAdvanceNextRecoveryDelay (Fibonacci sequence, exhausted)
- NotifyDegraded (no-op when no listener)
- StartRecoveryListener triggers RunRecovery on NotifyDegraded
- StartRecoveryListener exits on context cancel

### internal/http

- **Handlers:** GetWeather (success, empty location, service error)
- **Health:** healthy, degraded (invalid API key), shutting-down, overloaded, idle, not-idle (recent start, above threshold), degraded error rate, below error threshold
- **Test endpoints:** GetTestStatus, PostTestReset, PostTestLoad, PostTestError, PostTestShutdown, PostTestPreventClear, PostTestFailClear, PostTestClear
- PostTestAction unknown action returns 404
- **Middleware:** Correlation ID, metrics (OK and non-OK), timeout, rate limit 429, nil limiter passthrough, route detection, subrouter chain

### internal/idle

- Request count (empty, recorded, expiry, reset)

### internal/lifecycle

- IsShuttingDown default false
- SetShuttingDown true/false

### internal/observability

- Metrics registry usable
- SetTrackedLocations and RecordWeatherQuery
- MetricsHandler serves Prometheus format
- ParseLogLevel, NewLogger

### internal/overload

- Request count (empty, after RecordDenial)
- Count expiry outside window
- Reset
- Denial count

### internal/service

- NormalizeLocation
- GetWeather cache hit
- GetWeather cache miss then upstream success
- GetWeather upstream failure
- GetWeather cache Get error (fallback)

### internal/traffic

- Request count (empty, after RecordSuccess)
- RecordDenied and counts
- Error rate (success+error, denied excluded)
- Reset

## Not Tested

| Component | Reason |
|-----------|--------|
| **cmd/service main()** | Entrypoint; wiring and startup exercised via integration (`test-service.sh`) or E2E. Unit testing main requires process exec or heavy mocking. |
| **internal/cache (MemcachedCache)** | Requires live memcached server. Integration tests exist (`memcached_integration_test.go`) when server is available; skipped in CI without `-tags=integration`. |
| **internal/models** | Data structs (e.g. `WeatherData`) with no logic. Tests would add little value. |
| **observability.RegisterRateLimitGauges** | Called from main during startup. Registering gauge funcs is trivial; behavior covered indirectly by metrics scrapes in integration. |
