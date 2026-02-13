# Alerting Samples

Sample Prometheus and Alertmanager configuration for the Weather Alert Service. Aligned with `docs/observability-metrics-plan.md`.

These files are illustrative. They were generated according to the below rule-set and should form a basis for creating clear, intelligible alerts.  

---

## Files

| File | Purpose |
|------|---------|
| `prometheus.yaml` | Scrape config for the weather service; loads alert rules; sends alerts to Alertmanager |
| `alert-rules.yaml` | Base/default alert definitions using our metrics |
| `alert-rules-dev.yaml` | Development environment alert rules (more lenient thresholds) |
| `alert-rules-prod.yaml` | Production environment alert rules (stricter thresholds) |
| `alertmanager.yaml` | Routes alerts to PagerDuty (critical) and FireHydrant (all) |

---

## How Metrics Map to Alerts

| Metric | Alert | Threshold |
|--------|-------|-----------|
| `up` | WeatherServiceDown | Target unreachable for 1m |
| `httpRequestsTotal` | HighHTTPErrorRate | > 5% 5xx over 5m |
| `httpRequestDurationSeconds` | HighHTTPLatency | p95 > 5s for 5m |
| `httpRequestsInFlight` | HighRequestSaturation | > 50 for 5m |
| `weatherApiCallsTotal` | WeatherAPIHighErrorRate | > 20% error/rate_limited over 5m |
| `weatherApiDurationSeconds` | WeatherAPISlow | p95 > 2s for 5m |
| `weatherApiRetriesTotal` | WeatherAPIHighRetries | > 1 retry/s over 5m |
| `process_resident_memory_bytes` | HighMemoryUsage | > 500MB for 10m |
| `go_goroutines` | HighGoroutineCount | > 500 for 10m |

Thresholds are examples; tune for your SLOs and capacity.

---

## Environment-Specific Alert Rules

The repository includes environment-specific alert rule files:

- `alert-rules.yaml` - Base/default rules (moderate thresholds)
- `alert-rules-dev.yaml` - Development environment (more lenient thresholds)
- `alert-rules-prod.yaml` - Production environment (stricter thresholds)

### Usage

**Development Environment:**

```yaml
# prometheus-dev.yaml
rule_files:
  - alert-rules-dev.yaml
```

**Production Environment:**

```yaml
# prometheus-prod.yaml
rule_files:
  - alert-rules-prod.yaml
```

**Key Differences:**

| Alert | Base | Dev | Prod |
|-------|------|-----|------|
| `HighHTTPErrorRate` | 5% | 10% | 1% |
| `HighHTTPLatency` | p95 > 5s | p95 > 10s | p95 > 2s |
| `WeatherAPIHighErrorRate` | 20% | 30% | 10% |
| `WeatherAPISlow` | p95 > 2s | p95 > 5s | p95 > 2s |
| `LowCacheHitRate` | < 50% | < 30% | < 50% |
| `HighMemoryUsage` | > 500MB | > 1GB | > 500MB |
| `HighGoroutineCount` | > 500 | > 1000 | > 500 |

See `docs/alerting-thresholds.md` for comprehensive threshold rationale, tuning guidance, and environment-specific recommendations.

### Kubernetes ConfigMap Example

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-alert-rules
  namespace: monitoring
data:
  alert-rules-prod.yaml: |
    # Paste contents of alert-rules-prod.yaml here
```

Then reference in Prometheus config:

```yaml
rule_files:
  - /etc/prometheus/rules/alert-rules-prod.yaml
```

---

## Third-Party Integration

### PagerDuty

1. In PagerDuty: **Integrations** → **Add integration** → **Prometheus**
2. Copy the **Integration Key** (Events API v2: use as `routing_key`)
3. In `alertmanager.yaml`, replace `<PAGERDUTY_ROUTING_KEY>` with that key

Critical alerts (service down, high error rate) page on-call. Resolved alerts notify when the incident clears.

### FireHydrant

1. In FireHydrant: **Integrations** → **Alertmanager** (or **Signals** → **Event Sources**)
2. Create the integration and copy the webhook URL
3. In `alertmanager.yaml`, replace the FireHydrant `url` with your webhook URL
4. Use **Alert Routing** in FireHydrant to auto-open incidents from Signals

All alerts flow to FireHydrant for visibility; use routing rules to decide when to create incidents or notify teams.

---

## Running (Optional)

```bash
# Start the weather service
./bin/service

# Start Alertmanager (from samples/alerting/)
alertmanager --config.file=alertmanager.yaml

# Start Prometheus (from samples/alerting/)
prometheus --config.file=prometheus.yaml
```

Prometheus scrapes `localhost:8080/metrics` and evaluates rules every 15s. Firing alerts are sent to Alertmanager on port 9093.
