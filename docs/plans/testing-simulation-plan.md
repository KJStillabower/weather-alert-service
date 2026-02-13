# Testing/Simulation Plan

## Goal

Enable load balancer and orchestration testing by exposing a `test/` endpoint that can simulate lifecycle states (overloaded, idle, degraded, shutting-down). When `testing_mode` is enabled at startup, the endpoint allows controlled injection of state for validation and integration testing.

## Scope

- **Use case:** Simulate health status transitions so load balancers, Kubernetes probes, autoscalers, and alerting can be tested without needing to induce real failure.
- **Not in scope:** Production use. This is for local development and CI only.

## Configuration

**`testing_mode`** (bool, default: false)

- **Source:** Config YAML under a top-level key. When excluded, assume false.
- **Example:** `testing_mode: true` in `config/dev.yaml`
- **Validation:** When `testing_mode` is true, log a startup warning: "Testing mode enabled; test/ endpoint exposed."

## Endpoint

**Base path:** `/test`

- **Exposed when:** `testing_mode` is true
- **Exposed when:** Not exposed (404) when `testing_mode` is false
- **Methods:** `POST /test/{action}` with JSON body for params. `GET /test` for status.
- **Authentication:** None. Relies on testing_mode being disabled in production and network isolation for test environments.  In a world where authentication were in scope we would implement authentication.

## Actions

| Action | Method | Params | Effect |
|--------|--------|--------|--------|
| `load` | POST | `count` (int) | Inject N synthetic requests through the system logic . High count causes denials → overload; low count stays healthy. |
| `error` | POST | `count` (int) | Record N errors into degraded tracker (e.g. N=10). Potentially triggers degraded status when error rate exceeds threshold. |
| `prevent_clear` | POST | — | Disable auto-recovery (degraded retry goroutine). Service will not attempt recovery until re-enabled or reset. |
| `fail_clear` | POST | — | Simulate a failed recovery attempt (for testing Fibonacci backoff behavior). |
| `clear` | POST | — | Force a successful recovery attempt so service exits degraded. |
| `shutdown` | POST | — | Set lifecycle shutting-down flag. `/health` returns 503 shutting-down. |
| `reset` | POST | — | Clear all simulated state (overload requests, degraded errors, idle data, shutting-down flag, recovery overrides). Restore to natural state for continued testing. Does not restart the process. |
| (none) | GET | — | Return current simulated state (injected counts, flags). `GET /test` |

**Design notes:**

- **load:** `POST /test/load` with `count` sends that many synthetic requests through the system logic (in-process; no API or cache calls). Exercises rate limiter, denial logic, etc when count exceeds rate limit. Overloaded when requests in window > rate_limit_rps × lifecycle_window × (overload_threshold_pct/100).
- **prevent_clear / fail_clear / clear:** Require the recovery goroutine to accept overrides (disable, force-fail, force-succeed) if in test mode.

## Request/Response Shape

**POST /test/load**
```json
// Request body (optional; default count varies)
{ "count": 10 }

// Response 200 - below overload threshold
{ "ok": true, "action": "load", "message": "Recorded 10 requests", "state": "healthy" }

---

// Request body
{ "count": 500 }

// Response 200 - overload triggered
{ "ok": true, "action": "load", "message": "Recorded 500 requests", "state": "overloaded" }
```

**POST /test/error**
```json
// Request body
{ "count": 3 }

// Response 200 - below degraded threshold
{ "ok": true, "action": "error", "message": "Recorded 3 errors", "state": "healthy", "error_rate_pct": 30 }

---

// Request body
{ "count": 25 }

// Response 200 - degraded triggered
{ "ok": true, "action": "error", "message": "Recorded 25 errors", "state": "degraded", "error_rate_pct": 100 }
```


**POST /test/prevent_clear**
```json
// Response 200
{ "ok": true, "action": "prevent_clear", "message": "Auto-recovery disabled" }
```

**POST /test/fail_clear**
```json
// Response 200
{ "ok": true, "action": "fail_clear", "message": "Simulated failed recovery attempt", "state": "degraded", "next_recovery": "2m0s" }
```

**POST /test/clear**
```json
// Response 200
{ "ok": true, "action": "clear", "message": "Recovery forced successful", "state": "healthy" }
```

**POST /test/shutdown**
```json
// Response 200
{ "ok": true, "action": "shutdown", "message": "Shutting-down flag set" }
```

**POST /test/reset**
```json
// Response 200
{ "ok": true, "action": "reset", "message": "All simulated state cleared" }
```

**GET /test**
```json
// Response 200
{
  "total_requests_in_window": 0,
  "denied_requests_in_window": 0,
  "errors_in_window": 0,
  "window_length": "60s",
  "auto_clear": true
}
```

## Security Considerations

- **Production:** `testing_mode` must be false. When `testing_mode` is true, expose a Prometheus metric or label (e.g. `testing_mode=1`) so alerting can fire if a dev instance is wired to prod monitoring.
- **Network:** In production-like environments, ensure `/test` is not exposed to public networks (e.g. bind to localhost only when testing_mode is true, or rely on test networks).
- **No auth:** Endpoint has no authentication. Acceptable because it is only enabled in testing_mode.

## Implementation Order

1. ~~Add `testing_mode` to config YAML schema and parsing~~ Done. `config/dev.yaml`: true; `config/prod.yaml`: false. Startup warning not yet added.
2. ~~Register `/test` routes only when testing_mode is true~~ Done. `main.go` registers GET /test and POST /test/{action} when `cfg.TestingMode`.
3. ~~Implement `GET /test` (status)~~ Done. Returns total_requests_in_window, denied_requests_in_window, errors_in_window, window_length, auto_clear.
4. ~~Implement `POST /test/load`, `POST /test/error`, `POST /test/reset`, `POST /test/shutdown`~~ Done. load sends synthetic requests through rate limiter; error injects via degraded; reset clears all; shutdown sets lifecycle flag.
5. ~~Implement `POST /test/prevent_clear`, `POST /test/fail_clear`, `POST /test/clear`~~ Done. prevent_clear sets recovery disabled; fail_clear sets force-fail next attempt; clear resets degraded tracker and clears overrides.
6. ~~Add test-only setters to packages if needed~~ Done. degraded: SetRecoveryDisabled, SetForceFailNextAttempt, SetForceSucceedNextAttempt, ClearRecoveryOverrides, IsRecoveryDisabled. RunRecovery respects all overrides.

## Open Questions

- **Starting-up:** Not included in first cut; narrow window and harder to simulate. Can add later if needed.

## Curl Examples

Requires service running with `testing_mode: true` (e.g. `ENV_NAME=dev`). Default port 8080.

```bash
# Status
curl -s http://localhost:8080/test | jq

# Load (sends count requests through rate limiter; real denials; no API calls)
curl -s -X POST http://localhost:8080/test/load -H "Content-Type: application/json" -d '{"count": 120}'
curl -s -X POST http://localhost:8080/test/error -H "Content-Type: application/json" -d '{"count": 25}'

# Recovery controls
curl -s -X POST http://localhost:8080/test/prevent_clear
curl -s -X POST http://localhost:8080/test/fail_clear
curl -s -X POST http://localhost:8080/test/clear

# Lifecycle
curl -s -X POST http://localhost:8080/test/shutdown

# Reset all
curl -s -X POST http://localhost:8080/test/reset
```
