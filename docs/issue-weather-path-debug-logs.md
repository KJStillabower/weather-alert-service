# Issue: Add DEBUG-Level Logs for Weather Path Troubleshooting

**Labels:** observability, enhancement

## Summary

Add DEBUG-level log events at key checkpoints on `GET /weather/{location}` so operators can set `LOG_LEVEL=DEBUG` when troubleshooting and trace request flow (cache hit/miss, upstream call, success, rate limit denial).

## Rationale

Our logging philosophy avoids routine success at INFO; metrics cover that. DEBUG is opt-in for incident response—when something goes wrong, operators set DEBUG, reproduce, and need a trace. Metrics show aggregates; logs provide per-request context. Both support troubleshooting.

## Proposed DEBUG Log Points

| Point | Location | Log | Fields |
|-------|----------|-----|--------|
| Cache hit | `internal/service` | `cache hit` | `location` |
| Cache miss | `internal/service` | `cache miss, fetching upstream` | `location` |
| Success | `internal/http` or service | `weather served` | `location`, `cached`, `duration` |
| Upstream error | `internal/http` | Already present | `error` |
| Rate limit denied | `internal/http` middleware | `rate limit denied` | (correlation_id from context) |

All logs include `correlation_id` when available from request context. Keep payloads minimal; no raw responses or large payloads.

## Deferred: Client Information (IP)

Client IP or other client identifiers for log filtering are not in scope for this issue. Whether to log client IP, and how (e.g. `r.RemoteAddr`, `X-Forwarded-For`, `X-Real-IP`, or hashed), depends on deployment topology (direct vs behind proxy/load balancer) and privacy requirements. To be decided when implementation details are known.

## Constraints

- DEBUG only; no impact when `LOG_LEVEL=INFO` (default)
- Minimal fields; avoid unbounded cardinality
- Document in `docs/observability.md` Log Events by Code Path for GET /weather

## Acceptance Criteria

- [x] Cache hit path logs at DEBUG
- [x] Cache miss path logs at DEBUG before upstream call
- [x] Success path logs at DEBUG with cached flag and duration
- [x] Rate limit middleware logs at DEBUG when denying
- [x] `docs/observability.md` updated with DEBUG log events for GET /weather
- [x] Tests added: `TestHandler_GetWeather_DebugLogs_CacheHit`, `TestHandler_GetWeather_DebugLogs_CacheMiss`, `TestRateLimitMiddleware_DebugLogs_Denied`
