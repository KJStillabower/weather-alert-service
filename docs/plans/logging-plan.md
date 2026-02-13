# Logging Plan

## Goals (from guidelines)

1. Use structured logging
2. Include correlation IDs for minimal request tracing
3. Never log sensitive data (API keys)
4. Anything else warranted

---

## Guidelines

- **Log:** Decisions, boundaries, failures (not routine success; metrics cover that)
- **Levels:** Info (lifecycle), Warn (degraded/boundaries), Error (failures), Fatal (startup unrecoverable)
- **LOG_LEVEL env:** Optional override for dev (e.g. `DEBUG`); default `INFO`

---

## Logging

### Output Destination

- **zap** Service by default writes to stdout+stderr interfaces.
- **test-service.sh** sends stdout+stderr to /tmp/weather-service-test.log for convenience.

### Structured Logging

- **Library:** zap with JSON output, ISO8601 timestamps
- **Fields:** `zap.String`, `zap.Error`, `zap.Int`, etc. (never sprintf)
- **Format:** Production config, consistent field names

### Correlation IDs

- **Middleware:** `CorrelationIDMiddleware` reads `X-Correlation-ID` or generates UUID
- **Context:** Stores `correlation_id` and request-scoped `logger` in context
- **Response:** Sets `X-Correlation-ID` header for client propagation
- **Client:** `extractCorrelationID(ctx)` propagates to OpenWeatherMap requests
- **Logs:** Request-scoped logger includes `correlation_id` in all structured fields

### Sensitive Data

- API key never logged; our rules reinforce this
- Errors may contain "API key invalid" but never the key value
- Config load: log presence (`api_key_set`), not value

### Log Sites

| Location | Event | Level | Correlation ID |
|----------|-------|-------|----------------|
| main.go | Startup, server started | Info | N/A (process-level) |
| main.go | Shutdown, server stopped | Info | N/A |
| main.go | Client init failed, server start failed | Fatal | N/A |
| main.go | Shutdown error | Error | N/A |
| handlers.go | Weather fetch failed | Error | Yes (request-scoped logger) |
| handlers.go | Health degraded | Warn | Yes |

---

## Implementation Notes

- Health degraded: log when API key validation fails so ops can correlate with probes
- Logger: `LOG_LEVEL` env (INFO, DEBUG, WARN, ERROR) for dev tuning
