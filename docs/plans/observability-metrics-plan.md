# Observability Metrics Plan

## Goals (from prompt)

- Comprehensive observability: rates and latencies
- Imply high-value based on experience
- Explain each metric in code and summarize what to watch for
- High-value metrics: CPU, memory, connections, total hits, unique/top locations

---

## Proposed Metric Set

### 1. Infrastructure / Runtime

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `processCpuSecondsTotal` | Counter | — | Total CPU time (user + system) | High sustained CPU; rate() gives current load |
| `processResidentMemoryBytes` | Gauge | — | RSS memory in bytes | Memory leaks; unbounded growth |
| `processOpenFds` | Gauge | — | Open file descriptors | Connection leaks; approaching ulimit |
| `goGoroutines` | Gauge | — | Active goroutine count | Goroutine leaks; sudden spikes |
| `goThreads` | Gauge | — | OS threads | Thread exhaustion (rare) |

**Source:** Prometheus `process` and `go` collectors. Re-add to our custom registry.

**Query patterns (sliding windows):** Counters are cumulative; windows are applied at query time. No window config in code.

- CPU utilization (1m, 5m, 15m, 1h, 24h, 7d): `rate(processCpuSecondsTotal[1m])`, `rate(processCpuSecondsTotal[5m])`, etc.
- Request QPS: `rate(httpRequestsTotal[5m])`
- Histogram `*DurationSeconds` records per-request observations; percentiles: `histogram_quantile(0.95, rate(httpRequestDurationSeconds_bucket[5m]))`

---

### 2. Request Layer

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `httpRequestsTotal` | Counter | method, route, statusCode | Total requests | rate() for QPS; error rate by statusCode |
| `httpRequestDurationSeconds` | Histogram | method, route | Request latency | p95/p99; SLO breaches |
| `httpRequestsInFlight` | Gauge | — | Concurrent requests | Saturation; capacity limits |

**Route cardinality fix:** Use `route.GetPathTemplate()` (e.g. `/weather/{location}`) instead of raw path (`/weather/seattle`). Prevents unbounded cardinality from location strings.

---

### 3. External API (Weather)

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `weatherApiRequestsTotal` | Counter | status | OpenWeatherMap calls | rate(); error vs success ratio |
| `weatherApiRequestDurationSeconds` | Histogram | status | External API latency | p95 > 2s; upstream degradation |
| `weatherApiRetriesTotal` | Counter | — | Retry attempts | High retries = unstable upstream |

---

### 4. Cache

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `cacheHitsTotal` | Counter | cacheType | Cache hits | Hit rate = hits/(hits+misses) |

**Cache misses (derived):** `weatherApiCallsTotal - weatherApiRetriesTotal`. Each cache miss triggers one initial API call; retries add to the API total but not to the miss count.

---

### 5. Rate Limit / Capacity

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `rateLimitRequestsInWindow` | Gauge | — | Requests hitting rate-limited path in sliding window | Load; throughput; capacity planning |
| `rateLimitRejectsInWindow` | Gauge | — | 429 responses in sliding window | Are we rejecting requests; alert on active breakage |

**Source:** `internal/overload` sliding windows. Same window as lifecycle `overload_window` (configurable).

**Design:** Load is health (proactive). Rejects are errors (reactive). These metrics support capacity planning and alerting. `rateLimitDeniedTotal` (counter) remains for cumulative denials; the gauge gives "rejecting right now" view.

---

### 6. Business / Application

| Metric | Type | Labels | Purpose | Watch for |
|--------|------|--------|---------|-----------|
| `weatherQueriesTotal` | Counter | — | Total weather lookups | Traffic volume; rate() for QPS |
| `weatherQueriesByLocationTotal` | Counter | location | Per-location query count | Top locations; traffic distribution |

**Cardinality: Allow-list (DDoS / poisoning resistant)**

Prometheus labels create new time series. Unbounded location strings allow memory exhaustion and cache-poisoning attacks (attacker fills series budget with junk). Use a fixed allow-list.

- Config defines tracked locations. All others increment `location="other"`.
- Exactly 101 series (100 locations + other). No attack surface.
- Add locations to config if untracked hotspots appear in "other".

**Default tracked locations** (100: 50 US + 50 global; configurable in `config/*.yaml`):

```yaml
metrics:
  tracked_locations:
    # US (50)
    - seattle
    - portland
    - san francisco
    - los angeles
    - san diego
    - sacramento
    - san jose
    - fresno
    - phoenix
    - tucson
    - denver
    - boulder
    - salt lake city
    - las vegas
    - albuquerque
    - el paso
    - dallas
    - fort worth
    - houston
    - san antonio
    - austin
    - new orleans
    - atlanta
    - miami
    - orlando
    - tampa
    - charlotte
    - raleigh
    - nashville
    - memphis
    - chicago
    - detroit
    - minneapolis
    - st louis
    - kansas city
    - omaha
    - milwaukee
    - cleveland
    - pittsburgh
    - columbus
    - indianapolis
    - cincinnati
    - boston
    - new york
    - philadelphia
    - baltimore
    - washington
    - richmond
    - virginia beach
    - jacksonville
    # Global (50)
    - toronto
    - vancouver
    - montreal
    - calgary
    - ottawa
    - london
    - manchester
    - edinburgh
    - dublin
    - paris
    - lyon
    - marseille
    - berlin
    - munich
    - hamburg
    - amsterdam
    - brussels
    - madrid
    - barcelona
    - rome
    - milan
    - lisbon
    - athens
    - istanbul
    - moscow
    - warsaw
    - prague
    - vienna
    - budapest
    - tokyo
    - osaka
    - seoul
    - busan
    - beijing
    - shanghai
    - hong kong
    - singapore
    - kuala lumpur
    - bangkok
    - mumbai
    - delhi
    - sydney
    - melbourne
    - auckland
    - mexico city
    - buenos aires
    - sao paulo
    - rio de janeiro
    - cape town
    - cairo
```

Locations are matched case-insensitive after normalization (trim, lowercase). Add or remove as needed per environment.

**Derived metrics (PromQL):**

- Top locations: `topk(10, sum by (location) (rate(weatherQueriesByLocationTotal[1h])))`
- "Other" share: `rate(weatherQueriesByLocationTotal{location="other"}[1h]) / rate(weatherQueriesTotal[1h])`

---

## Metrics We Are NOT Adding (and why)

| Potential metric | Reason |
|------------------|--------|
| Active TCP connections | Requires listener wrapper; `processOpenFds` + `httpRequestsInFlight` approximate load |
| Cache size / evictions | In-memory cache has no size limit; entries expire by TTL only |
| Cache hit ratio | Derivable: hits/(hits+misses) |

---



## Summary

| Category | Metrics |
|----------|---------|
| Infrastructure | processCpuSecondsTotal, processResidentMemoryBytes, processOpenFds, goGoroutines, goThreads |
| Request | httpRequestsTotal, httpRequestDurationSeconds, httpRequestsInFlight |
| External API | weatherApiRequestsTotal, weatherApiRequestDurationSeconds, weatherApiRetriesTotal |
| Cache | cacheHitsTotal (misses = weatherApiCallsTotal - weatherApiRetriesTotal) |
| Rate Limit / Capacity | rateLimitRequestsInWindow, rateLimitRejectsInWindow |
| Business | weatherQueriesTotal, weatherQueriesByLocationTotal |

**Total: 15 metrics** (5 infra + 3 request + 3 weather API + 1 cache + 2 rate-limit + 2 business)
