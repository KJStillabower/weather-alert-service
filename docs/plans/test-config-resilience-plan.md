# Test Config Resilience Plan (Issue #4)

## Goal

Make synthetic tests in `test-service.sh` derive batch sizes and thresholds from the running service instead of hardcoding. Expose config-derived values in `GET /test` only (JSON for the bash script).

## Rationale

- **Current:** Tests assume fixed config (e.g. 96 overload threshold, 2 rps / burst 5, 5% degraded). Changing `rate_limit_rps`, `overload_threshold_pct`, or `degraded_error_pct` breaks tests.
- **Target:** Service exposes thresholds; test script reads and uses them. Changing config no longer requires test script changes.

## Design: GET /test Only

These values are static (config at startup). Prometheus gauges for static config are not a good fit; the metrics endpoint is for values that vary over time. Config-derived thresholds belong in `GET /test` only.

## Values to Expose

| Name | Formula | Use |
|------|----------|-----|
| `overload_threshold` | `rate_limit_rps * lifecycle_window_seconds * (overload_threshold_pct / 100)` | synthetic_load: total requests must exceed this to trigger overload |
| `rate_limit_rps` | From config | synthetic_load/degraded: compute batches and spacing |
| `rate_limit_burst` | From config | synthetic_load/degraded: batch size |
| `degraded_error_pct` | From config | synthetic_degraded: error budget threshold |
| `overload_window_seconds` | From config (lifecycle_window; optional) | For transparency; script can derive from window_length |

Handler already has `HealthConfig` with `RateLimitRPS`, `OverloadWindow`, `OverloadThresholdPct`, `DegradedWindow`, `DegradedErrorPct`. Rate limit burst is in `rate.Limiter`; Handler does not hold it. We need to pass `RateLimitBurst` into `HealthConfig` or Handler.

## Implementation Steps

### 1. Add RateLimitBurst to HealthConfig

**File:** `internal/http/handlers.go`

- Add `RateLimitBurst int` to `HealthConfig`.
- Document that it is 0 when rate limiter is disabled.

**File:** `cmd/service/main.go`

- Set `healthConfig.RateLimitBurst = cfg.RateLimitBurst` when building `HealthConfig`.

### 2. Extend GET /test Response

**File:** `internal/http/handlers.go` — `GetTestStatus`

Add a nested `config` object to the response:

```go
cfg := map[string]interface{}{
    "rate_limit_rps":          h.healthConfig.RateLimitRPS,
    "rate_limit_burst":        h.healthConfig.RateLimitBurst,
    "overload_threshold":      overloadThreshold,
    "overload_window_seconds": h.healthConfig.OverloadWindow.Seconds(),
    "degraded_error_pct":      h.healthConfig.DegradedErrorPct,
}
resp := map[string]interface{}{
    // existing fields
    "config": cfg,
}
```

Compute `overloadThreshold`:

```go
overloadThreshold := 0
if h.healthConfig != nil && h.healthConfig.RateLimitRPS > 0 {
    overloadThreshold = int(float64(h.healthConfig.RateLimitRPS) *
        h.healthConfig.OverloadWindow.Seconds() *
        float64(h.healthConfig.OverloadThresholdPct) / 100)
}
```

When `healthConfig == nil` or rate limiter disabled, return 0 for `overload_threshold` and `rate_limit_rps`/`rate_limit_burst`; test script can detect and skip or fail gracefully.

### 3. Update test-service.sh

**File:** `test-service.sh`

Add helper to fetch config from GET /test:

```bash
# synthetic_get_config
# Fetches config from GET /test. Sets: OVERLOAD_THRESHOLD, RATE_LIMIT_RPS, RATE_LIMIT_BURST, DEGRADED_ERROR_PCT.
# Returns 1 if GET /test fails or config missing.
synthetic_get_config() {
    local response
    response=$(synthetic_curl GET /test) || true
    if ! echo "$response" | grep -q '"config"'; then
        log_error "GET /test missing config (upgrade service or testing_mode off)"
        return 1
    fi
    OVERLOAD_THRESHOLD=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('overload_threshold',''))" 2>/dev/null)
    RATE_LIMIT_RPS=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('rate_limit_rps',''))" 2>/dev/null)
    RATE_LIMIT_BURST=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('rate_limit_burst',''))" 2>/dev/null)
    DEGRADED_ERROR_PCT=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('degraded_error_pct',''))" 2>/dev/null)
    if [ -z "$OVERLOAD_THRESHOLD" ] || [ "$OVERLOAD_THRESHOLD" = "0" ]; then
        log_error "overload_threshold is 0 (rate limiter disabled?)"
        return 1
    fi
}
```

**synthetic_load:**

- Call `synthetic_get_config` at start.
- 4 passes. `batch_size = ceil((OVERLOAD_THRESHOLD + 20) / 4)`. Spacing 1s between passes.
- Passes 1–3 expect healthy; pass 4 expects overloaded.

**synthetic_degraded:**

- Call `synthetic_get_config`. Load until `accepted >= min_successes` where `min_successes = ceil(100 / DEGRADED_ERROR_PCT)`.
- Inject errors twice: (1) 1 error → expect healthy (under limit). (2) `second_err = ceil((pct*(accepted+1)-100)/(100-pct))` more errors → expect degraded (over limit).

**synthetic_recovery_fail:**

- No load batching; just error 3 to get degraded, then fail_clear x6. Config not needed for batching. Optional: use config for clarity. Leave as-is if no load.

### 4. Files Summary

| File | Change |
|------|--------|
| `internal/http/handlers.go` | Add `RateLimitBurst` to `HealthConfig`; extend `GetTestStatus` with config fields |
| `cmd/service/main.go` | Set `HealthConfig.RateLimitBurst` |
| `test-service.sh` | Add `synthetic_get_config`; update `synthetic_load`, `synthetic_degraded` to use it |

### 5. Testing

- **Unit:** `handlers_test.go`: extend `GetTestStatus` test to assert new JSON fields.
- **Integration:** Run `./test-service.sh synthetic` with `dev_localcache`; verify all pass. Change `rate_limit_rps` to 3, `overload_threshold_pct` to 90; run again; verify tests still pass.

### 6. Edge Cases

- **Rate limiter disabled** (`rate_limit_rps` 0): `overload_threshold` = 0. Test script should fail/skip synthetic_load (cannot trigger overload via rate limit). synthetic_degraded does not need rate limit; can still run.
- **healthConfig nil:** Return zeros; script handles.
- **GET /test 404** (testing_mode off): Script already fails; no change.

## References

- GitHub Issue #4
- `docs/test-service-synthetic-plan.md`
