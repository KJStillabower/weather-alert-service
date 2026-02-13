# Alert Thresholds Guide

## Overview

This document provides comprehensive documentation for all alert thresholds in the Weather Alert Service. It explains the rationale for each threshold, provides tuning guidance, and recommends environment-specific values.

**Purpose:**
- Help operators understand alert thresholds
- Guide threshold tuning decisions
- Document rationale for threshold values
- Provide environment-specific recommendations

**How to Use:**
- Review thresholds before deploying alerts
- Use environment-specific recommendations for dev/staging/prod
- Follow tuning process when adjusting thresholds
- Reference this document when investigating alert behavior

## Tuning Process

### When to Tune Thresholds

**Tune thresholds when:**
- **Alert Fatigue:** Too many false positives, alerts firing frequently without incidents
- **Missing Incidents:** Alerts not firing when incidents occur (thresholds too lenient)
- **SLO Changes:** Service-level objectives change, requiring threshold adjustments
- **Environment Differences:** Different environments need different sensitivity levels

### How to Tune Thresholds

1. **Review Historical Data**
   - Query Prometheus for historical metrics (last 7-30 days)
   - Calculate actual error rates, latencies, etc.
   - Compare against current thresholds

2. **Identify Issues**
   - Count false positives (alerts without incidents)
   - Identify missed incidents (incidents without alerts)
   - Calculate alert frequency

3. **Adjust Thresholds**
   - Increase threshold if too many false positives
   - Decrease threshold if missing incidents
   - Consider error budgets and SLOs

4. **Validate Changes**
   - Test threshold changes in non-production first
   - Monitor alert behavior after changes
   - Iterate based on feedback

5. **Document Changes**
   - Update threshold values in alert rules
   - Update this documentation
   - Document rationale for changes

### Change Management

- Review threshold changes in code review
- Test in dev/staging before production
- Monitor alert behavior after changes
- Document changes and rationale

## Alert Thresholds

### Availability Alerts

#### WeatherServiceDown

**Current Threshold:** `up{job="weather-alert-service"} == 0` for 1 minute

**Rationale:**
- Service instance is completely unreachable (crashed, network issue, etc.)
- Immediate detection is critical for availability
- 1 minute window allows for brief network hiccups without false positives

**Tuning Guidance:**
- **Too Sensitive:** Reduce `for` duration if brief network issues cause false positives
- **Too Insensitive:** Increase `for` duration if service crashes aren't detected quickly enough
- **Production Recommendation:** Keep at 1 minute (critical alert)

**Environment-Specific:**
- **Dev:** 2 minutes (more lenient for development environments)
- **Prod:** 1 minute (immediate detection critical)

---

### Request Layer Alerts

#### HighHTTPErrorRate

**Current Threshold:** > 5% (0.05) 5xx errors for 5 minutes

**Rationale:**
- Typical SLO target is 99.9% availability (0.1% error rate)
- 5% threshold provides early warning before SLO breach
- 5 minute window balances detection speed with noise reduction

**Tuning Guidance:**
- **Too Sensitive:** Increase threshold if normal error spikes cause false positives
- **Too Insensitive:** Decrease threshold if incidents aren't caught early enough
- **Error Budget:** Calculate based on SLO error budget (e.g., 0.1% SLO → 1% threshold for early warning)

**Environment-Specific:**
- **Dev:** 10% (more lenient, reduce alert fatigue)
- **Prod:** 1% (stricter, catch issues early)

**SLO Alignment:**
- For 99.9% availability SLO (0.1% errors), threshold should be 1-5% for early warning
- For 99.95% availability SLO (0.05% errors), threshold should be 0.5-2%

---

#### HighHTTPLatency

**Current Threshold:** p95 > 5 seconds for 5 minutes

**Rationale:**
- p95 latency indicates most requests are slow
- 5 second threshold catches significant degradation
- 5 minute window prevents brief spikes from triggering alerts

**Tuning Guidance:**
- **Too Sensitive:** Increase threshold if normal latency spikes cause false positives
- **Too Insensitive:** Decrease threshold if slow requests aren't detected
- **SLO Alignment:** Set threshold below SLO target (e.g., if SLO is p95 < 1s, threshold might be 2s for early warning)

**Environment-Specific:**
- **Dev:** p95 > 10s (more lenient)
- **Prod:** p95 > 2s (stricter, catch degradation early)

**SLO Alignment:**
- For p95 < 1s SLO, threshold should be 2-3s for early warning
- For p95 < 500ms SLO, threshold should be 1-2s

---

#### HighRequestSaturation

**Current Threshold:** > 50 in-flight requests for 5 minutes

**Rationale:**
- Indicates service is approaching capacity limits
- Threshold should be tuned based on actual service capacity
- 5 minute window prevents brief traffic spikes from triggering

**Tuning Guidance:**
- **Tune Based on Capacity:** Set threshold at 70-80% of actual capacity
- **Too Sensitive:** Increase threshold if normal traffic causes alerts
- **Too Insensitive:** Decrease threshold if capacity issues aren't detected early

**Environment-Specific:**
- **Dev:** > 100 (higher capacity expected in dev)
- **Prod:** > 50 (tune based on actual production capacity)

**Capacity Planning:**
- Monitor `httpRequestsInFlight` over time to understand capacity
- Set threshold at 70-80% of observed maximum
- Adjust based on scaling decisions

---

#### HighRateLimitRejections

**Current Threshold:** > 10 rejections in window for 2 minutes

**Rationale:**
- Indicates service is actively rejecting requests (at capacity)
- 2 minute window provides quick detection
- Threshold should align with rate limit configuration

**Tuning Guidance:**
- **Tune Based on Rate Limits:** Set threshold based on `rate_limit_rps` and `lifecycle_window`
- **Too Sensitive:** Increase threshold if normal rate limiting causes alerts
- **Too Insensitive:** Decrease threshold if capacity issues aren't detected

**Environment-Specific:**
- **Dev:** > 20 (more lenient)
- **Prod:** > 10 (stricter, catch capacity issues early)

**Rate Limit Alignment:**
- Threshold should be percentage of rate limit capacity
- Example: If rate limit is 100 RPS, threshold might be 10-20 rejections in window

---

#### RateLimitRequestsHigh

**Current Threshold:** > 4800 requests in window for 5 minutes

**Rationale:**
- Indicates service is approaching rate limit capacity
- Threshold calculated as 80% of `rate_limit_rps * lifecycle_window`
- Example: 100 RPS * 60s * 0.8 = 4800

**Tuning Guidance:**
- **Calculate Based on Config:** `rate_limit_rps * lifecycle_window * 0.8`
- **Adjust Percentage:** Use 0.7-0.9 multiplier based on desired early warning
- **Too Sensitive:** Increase threshold or multiplier
- **Too Insensitive:** Decrease threshold or multiplier

**Environment-Specific:**
- **Dev:** Higher threshold (if dev has higher rate limits)
- **Prod:** Tune based on production `rate_limit_rps` and `lifecycle_window` config

**Configuration-Dependent:**
- Must be tuned per environment based on actual rate limit configuration
- Document calculation in alert rule comments

---

#### HighHTTP4xxErrorRate

**Current Threshold:** > 10% (0.1) 4xx errors for 5 minutes

**Rationale:**
- High 4xx rate indicates client issues (bad requests, not found, etc.)
- May indicate API contract issues or client misconfiguration
- 10% threshold catches significant client error patterns

**Tuning Guidance:**
- **Too Sensitive:** Increase threshold if normal client errors cause false positives
- **Too Insensitive:** Decrease threshold if client issues aren't detected
- **Consider Source:** High 4xx from specific clients may be expected

**Environment-Specific:**
- **Dev:** 15% (more lenient, dev clients may have more errors)
- **Prod:** 5% (stricter, catch client issues early)

---

#### ZeroHTTPRequests

**Current Threshold:** Zero requests for 10 minutes, but had traffic in previous 30 minutes

**Rationale:**
- Detects unexpected traffic drop (routing issues, service isolation)
- 10 minute window prevents brief traffic lulls from triggering
- Requires previous traffic to avoid false positives on startup

**Tuning Guidance:**
- **Too Sensitive:** Increase window if normal traffic patterns cause false positives
- **Too Insensitive:** Decrease window if routing issues aren't detected quickly
- **Consider Patterns:** Some services have natural traffic patterns (e.g., business hours)

**Environment-Specific:**
- **Dev:** 15 minutes (more lenient, dev may have natural traffic lulls)
- **Prod:** 10 minutes (stricter, catch routing issues early)

---

### Upstream API Alerts

#### WeatherAPIHighErrorRate

**Current Threshold:** > 20% (0.2) error rate for 5 minutes

**Rationale:**
- High upstream error rate indicates external API issues
- 20% threshold catches significant upstream degradation
- 5 minute window balances detection speed with noise reduction

**Tuning Guidance:**
- **Too Sensitive:** Increase threshold if upstream has normal error spikes
- **Too Insensitive:** Decrease threshold if upstream issues aren't caught early
- **Consider Upstream SLO:** Align with upstream provider's reliability

**Environment-Specific:**
- **Dev:** 30% (more lenient, dev may use test API keys with more errors)
- **Prod:** 10% (stricter, catch upstream issues early)

---

#### WeatherAPISlow

**Current Threshold:** p95 > 2 seconds for 5 minutes

**Rationale:**
- Per observability plan, p95 > 2s indicates upstream degradation
- p99 > 5s would indicate timeout risk
- 2 second threshold provides early warning before timeouts

**Tuning Guidance:**
- **Too Sensitive:** Increase threshold if normal upstream latency causes false positives
- **Too Insensitive:** Decrease threshold if upstream slowness isn't detected
- **Consider Timeout:** Threshold should be well below request timeout (e.g., timeout is 5s, threshold is 2s)

**Environment-Specific:**
- **Dev:** p95 > 5s (more lenient)
- **Prod:** p95 > 2s (stricter, per observability plan)

**Timeout Alignment:**
- Threshold should be 40-50% of request timeout
- Example: If timeout is 5s, threshold might be 2-2.5s

---

#### WeatherAPIHighRetries

**Current Threshold:** > 1 retry per second for 5 minutes

**Rationale:**
- High retry rate indicates unstable upstream (transient failures)
- 1 retry/s threshold catches significant upstream instability
- 5 minute window prevents brief spikes from triggering

**Tuning Guidance:**
- **Too Sensitive:** Increase threshold if normal retry patterns cause false positives
- **Too Insensitive:** Decrease threshold if upstream instability isn't detected
- **Consider Retry Policy:** Align with retry configuration (max attempts, base delay)

**Environment-Specific:**
- **Dev:** > 2 retries/s (more lenient)
- **Prod:** > 1 retry/s (stricter)

---

### Cache Alerts

#### LowCacheHitRate

**Current Threshold:** < 50% hit rate for 10 minutes

**Rationale:**
- Low cache hit rate indicates cache effectiveness degradation
- 50% threshold catches significant cache issues
- 10 minute window prevents brief cache misses from triggering

**Tuning Guidance:**
- **Too Sensitive:** Decrease threshold if normal cache patterns cause false positives
- **Too Insensitive:** Increase threshold if cache issues aren't detected
- **Consider TTL:** Cache hit rate depends on TTL and traffic patterns

**Environment-Specific:**
- **Dev:** < 30% (more lenient, dev may have lower cache hit rates)
- **Prod:** < 50% (stricter, catch cache issues early)

**Cache TTL Alignment:**
- Hit rate depends on TTL and request patterns
- Longer TTL → higher expected hit rate
- Adjust threshold based on expected hit rate for your TTL

---

### Runtime Alerts

#### HighMemoryUsage

**Current Threshold:** > 500MB RSS for 10 minutes

**Rationale:**
- High memory usage may indicate memory leak
- 500MB threshold catches significant memory growth
- 10 minute window prevents brief memory spikes from triggering

**Tuning Guidance:**
- **Tune Based on Capacity:** Set threshold based on available memory
- **Too Sensitive:** Increase threshold if normal memory usage causes false positives
- **Too Insensitive:** Decrease threshold if memory leaks aren't detected early

**Environment-Specific:**
- **Dev:** > 1GB (more lenient, dev may have more memory)
- **Prod:** > 500MB (tune based on production memory limits)

**Memory Limit Alignment:**
- Threshold should be 70-80% of container/pod memory limit
- Example: If limit is 256Mi, threshold might be 200MB

---

#### HighGoroutineCount

**Current Threshold:** > 500 goroutines for 10 minutes

**Rationale:**
- High goroutine count may indicate goroutine leak
- 500 goroutines threshold catches significant leaks
- 10 minute window prevents brief spikes from triggering

**Tuning Guidance:**
- **Tune Based on Baseline:** Monitor normal goroutine count, set threshold at 2-3x baseline
- **Too Sensitive:** Increase threshold if normal goroutine patterns cause false positives
- **Too Insensitive:** Decrease threshold if goroutine leaks aren't detected

**Environment-Specific:**
- **Dev:** > 1000 (more lenient)
- **Prod:** > 500 (tune based on production baseline)

**Baseline Alignment:**
- Monitor `go_goroutines` over time to establish baseline
- Set threshold at 2-3x observed baseline
- Adjust based on actual goroutine usage patterns

---

## Environment-Specific Recommendations

### Development Environment

**Recommended Thresholds (More Lenient):**
- `HighHTTPErrorRate`: 10% (vs 5% base)
- `HighHTTPLatency`: p95 > 10s (vs 5s base)
- `WeatherAPIHighErrorRate`: 30% (vs 20% base)
- `WeatherAPISlow`: p95 > 5s (vs 2s base)
- `LowCacheHitRate`: < 30% (vs 50% base)
- `HighMemoryUsage`: > 1GB (vs 500MB base)
- `HighGoroutineCount`: > 1000 (vs 500 base)
- Longer `for` durations (reduce noise)

**Rationale:**
- Development environments have more variability
- Reduce alert fatigue for developers
- Focus on critical issues only
- Allow experimentation without false positives

### Production Environment

**Recommended Thresholds (Stricter):**
- `HighHTTPErrorRate`: 1% (vs 5% base)
- `HighHTTPLatency`: p95 > 2s (vs 5s base)
- `WeatherAPIHighErrorRate`: 10% (vs 20% base)
- `WeatherAPISlow`: p95 > 2s (same as base, per observability plan)
- `LowCacheHitRate`: < 50% (same as base)
- `HighMemoryUsage`: > 500MB (same as base, tune per capacity)
- `HighGoroutineCount`: > 500 (same as base, tune per baseline)
- Shorter `for` durations for critical alerts

**Rationale:**
- Production requires early issue detection
- Stricter thresholds catch problems before they impact users
- Align with SLOs and error budgets
- Faster detection for critical issues

### Staging Environment

**Recommended Thresholds (Between Dev and Prod):**
- Use production-like thresholds but slightly more lenient
- Good for validating alert behavior before production
- Helps identify threshold issues before production deployment

---

## Threshold Validation

### Review Historical Data

Before tuning thresholds, review historical Prometheus data:

```promql
# Review error rate over last 7 days
avg_over_time(
  sum(rate(httpRequestsTotal{statusCode=~"5.."}[5m]))
  /
  sum(rate(httpRequestsTotal[5m]))
  [7d:1h]
)

# Review latency p95 over last 7 days
histogram_quantile(0.95,
  sum(rate(httpRequestDurationSeconds_bucket[5m])) by (le)
)
```

### Identify Threshold Issues

**Alert Fatigue Indicators:**
- Alerts firing frequently without incidents
- High false positive rate
- Operators ignoring alerts

**Missed Incident Indicators:**
- Incidents occurring without alerts
- Thresholds consistently above actual values
- Post-incident reviews showing missed alerts

### Validation Checklist

Before deploying threshold changes:
- [ ] Reviewed historical data (7-30 days)
- [ ] Calculated actual error rates/latencies
- [ ] Compared against current thresholds
- [ ] Identified false positives and missed incidents
- [ ] Documented rationale for changes
- [ ] Tested in non-production environment
- [ ] Updated alert rules and documentation

---

## References

- Alert Rules: `samples/alerting/alert-rules.yaml`
- Environment-Specific Rules: `samples/alerting/alert-rules-dev.yaml`, `samples/alerting/alert-rules-prod.yaml`
- Observability Guide: `docs/observability.md`
- Phase 2 Plan: `docs/plans/alert-threshold-tuning-phase2.md` (SLO-based alerting)
