# Issue: Improved Observability Documentation

**Labels:** documentation, observability, priority-high

## Summary

Observability is the top operational objective. We need consolidated, operator-facing documentation that summarizes what we generate and how to interpret it. Current observability documentation is scattered across README, design plans, and rules; there is no single operational guide.

---

## What We Currently Generate

### Metrics (`GET /metrics`)

| Category | Metric | Type | Purpose |
|----------|--------|------|---------|
| **Request** | `httpRequestsTotal` | Counter | method, route, statusCode; rate() for QPS |
| | `httpRequestDurationSeconds` | Histogram | Request latency; p95/p99 for SLOs |
| | `httpRequestsInFlight` | Gauge | Concurrent requests; saturation signal |
| **External API** | `weatherApiCallsTotal` | Counter | status: success, error, rate_limited, client_error, server_error |
| | `weatherApiDurationSeconds` | Histogram | Upstream latency; p95 > 2s = degradation |
| | `weatherApiRetriesTotal` | Counter | Retry attempts; high = unstable upstream |
| **Cache** | `cacheHitsTotal` | Counter | cacheType label; hit rate = hits/(hits+misses) |
| **Business** | `weatherQueriesTotal` | Counter | Total lookups; rate() for QPS |
| | `weatherQueriesByLocationTotal` | Counter | location (allow-list; others = "other"); top locations |
| **Rate limit** | `rateLimitDeniedTotal` | Counter | Cumulative 429 denials |
| **Runtime** | Process/Go collectors | — | CPU, memory, goroutines, threads |

**Sources:** `internal/observability/metrics.go`, `050-observability.mdc`, `docs/observability-metrics-plan.md`

### Logging

- **Format:** Structured (zap) JSON, stderr, ISO8601 timestamps
- **Correlation IDs:** Per-request; `X-Correlation-ID` header; propagated to upstream
- **Philosophy:** Decisions, boundaries, failures only; no routine success (metrics cover that)
- **Config:** `LOG_LEVEL` env var (DEBUG, INFO, WARN, ERROR)

**Sources:** `internal/observability/logger.go`, `internal/http/middleware.go`, `050-observability.mdc`

### Health and Lifecycle (`GET /health`)

- **Status values:** healthy, idle, overloaded, degraded, shutting-down
- **Checks:** weatherApi, cache
- **Routing:** 503 = do not route (degraded, overloaded, shutting-down)

**Sources:** `docs/health-status-plan.md`, `docs/service-level-objective-plan.md`

### Alerting (samples)

- **Files:** `samples/alerting/prometheus.yaml`, `alert-rules.yaml`, `alertmanager.yaml`
- **Alerts:** WeatherServiceDown, HighHTTPErrorRate, HighHTTPLatency, HighRequestSaturation, WeatherAPIHighErrorRate, WeatherAPISlow, WeatherAPIHighRetries, HighMemoryUsage, HighGoroutineCount
- **Integrations:** PagerDuty (critical), FireHydrant (all)

**Sources:** `samples/alerting/README.md`, `docs/service-level-objective-plan.md`

---

## Current Documentation Gaps

1. **No single operational guide** — operators must piece together README, observability-metrics-plan, SLO plan, alerting README
2. **Logging expectations unclear** — what fields appear when, how to use correlation IDs for debugging
3. **PromQL examples scattered** — common queries (error rate, cache hit rate, top locations) not in one place
4. **Operational interpretation missing** — when to investigate, how metrics map to actions
5. **Config-to-SLO relationship** — lifecycle thresholds drive SLOs; not summarized for operators
6. **Runbook placeholders** — alert rules reference `https://example.com/runbooks/...`; no real runbooks
7. **Dashboard guidance absent** — no suggested Grafana panels or layouts
8. **Correlation ID troubleshooting** — how to trace a request through logs using X-Correlation-ID

---

## Proposed Improvements

### 1. Create `docs/observability.md` (or observability-guide.md)

**DONE:** `docs/observability.md` created. Single operator-facing document containing:
- Summary table of all metrics (copy-paste ready)
- PromQL cookbook: error rate, latency percentiles, cache hit rate, top locations, QPS
- Logging: what we log, when, and what fields to expect
- Correlation ID: how to trace a request (header, log field, troubleshooting flow)
- Lifecycle/health: status meanings and when each occurs
- Config-to-SLO quick reference (degraded_error_pct, overload formula, etc.)

### 2. Update README Observability Section

- Point to `docs/observability-guide.md` as canonical reference
- Keep brief metrics table but link to full guide
- Add "Troubleshooting with observability" subsection

### 3. Alerting and Runbooks

- Document runbook placeholders as intentional (example.com) or replace with in-repo runbook snippets
- Add "When this fires" and "What to check" for each alert to `samples/alerting/README.md` or new `docs/alert-runbooks.md`

### 4. Optional: Dashboard Spec

- Add `docs/observability-dashboard-spec.md` or JSON spec for a Grafana dashboard (panels, queries, layout) so operators can import

---

## Acceptance Criteria

- [x] `docs/observability.md` exists and covers metrics summary, PromQL cookbook, logging expectations, correlation ID troubleshooting, lifecycle/health, config-to-SLO
- [x] README observability section references the guide and has a troubleshooting pointer
- [x] Alert runbooks (or "When this fires" / "What to check") documented for each alert (in docs/observability.md)
- [x] No duplicated or conflicting metric descriptions across docs; README and guide stay in sync

---

## References

- `050-observability.mdc` — rules and conventions
- `docs/observability-metrics-plan.md` — metric design
- `docs/service-level-objective-plan.md` — SLO/config mapping
- `docs/health-status-plan.md` — lifecycle states
- `samples/alerting/` — Prometheus and Alertmanager config
