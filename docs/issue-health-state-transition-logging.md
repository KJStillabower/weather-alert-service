# Issue: Log Health State Transitions

**Labels:** observability, enhancement, documentation

## Summary

Health state transitions (healthy -> degraded, overloaded -> healthy, etc.) are boundary events that fit our observability philosophy but are not currently logged. We should add transition logging and update the observability documentation.

## Rationale

Per `050-observability.mdc`, we log **state changes**—service startup, shutdown, configuration reloads. Health status changes are operational signals: when a service goes from healthy to degraded, or recovers from overload, operators need that in logs for incident response and postmortems. Metrics and `/health` responses show current state; logs provide the transition history.

## Implementation

### 1. Add transition logging in health handler

- Track previous health status (e.g. in `internal/lifecycle` or handler-scoped state)
- On each `/health` call, compare current vs previous status
- When status changes, log: `logger.Info("health status transition", zap.String("from", prev), zap.String("to", current), zap.String("reason", ...))`
- Include structured fields: `previous_status`, `current_status`, optional `reason` (e.g. "error_rate_breach", "api_key_invalid", "overload_threshold", "recovery")

### 2. Transitions to log

| From | To | Typical reason |
|------|-----|----------------|
| healthy | degraded | Error rate breach; API key validation failed |
| healthy | overloaded | Requests exceeded overload threshold |
| overloaded | healthy | Load dropped below threshold |
| degraded | healthy | Error rate recovered; API key validated |
| * | shutting-down | SIGTERM/SIGINT received |

### 3. Update docs/observability.md

- Add log event(s) to the **GET /health** row in "Log Events by Code Path"
- Document the new event: message, level, when it fires, structured fields
- Note that routine health probes (no transition) remain unlogged

## Acceptance criteria

- [ ] Health handler logs on status transition with `previous_status`, `current_status`, and context
- [ ] No log on routine probes when status is unchanged
- [ ] `docs/observability.md` Log Events by Code Path section updated for GET /health
- [ ] Tests verify transition logging (or at least that handler can transition without regression)
