# Environment Config YAML Plan

## Overview

The service loads configuration from `config/{ENV_NAME}.yaml`. Set `ENV_NAME` to select the file (if not set, the default is `dev`).

```bash
ENV_NAME=dev ./bin/service    # loads config/dev.yaml
ENV_NAME=prod ./bin/service  # loads config/prod.yaml
```

**Not in config YAML:** The API key is loaded from `WEATHER_API_KEY` environment variable or `config/secrets.yaml`. This is blocked through a `.gitignore` entry.

---

## Schema

### `testing_mode`

| Field | Type | Description |
|-------|------|-------------|
| `testing_mode` | bool | When true, exposes `/test` endpoint for lifecycle simulation. For local dev and CI only; must be false in production. See `docs/testing-simulation-plan.md`. |

### `server`

| Field | Type | Description |
|-------|------|-------------|
| `port` | string | HTTP listen port (e.g. `"8080"`, `"80"`) |

### `weather_api`

| Field | Type | Description |
|-------|------|-------------|
| `url` | string | OpenWeatherMap API base URL |
| `timeout` | duration | Per-request timeout for API calls. Format: `"2s"`, `"5s"`, `"100ms"` |

### `request`

| Field | Type | Description |
|-------|------|-------------|
| `timeout` | duration | Overall HTTP handler timeout. Must exceed `weather_api.timeout` |

### `cache`

| Field | Type | Description |
|-------|------|-------------|
| `backend` | string | `in_memory` or `memcached`. Default `in_memory`. Use `memcached` for shared cache across instances. |
| `ttl` | duration | How long cached weather entries are valid (e.g. `"10s"`, `"5m"`). Applies to both backends. |

**Env override:** `CACHE_BACKEND` overrides `cache.backend`.

#### `cache.memcached` (when `backend: memcached`)

| Field | Type | Description |
|-------|------|-------------|
| `addrs` | string | Comma-separated server list (e.g. `"localhost:11211"` or `"host1:11211,host2:11211"`) |
| `timeout` | duration | Dial/operation timeout; fail fast on unreachable (e.g. `"500ms"`) |
| `max_idle_conns` | int | Idle connections per addr in pool (e.g. `2`) |

**Env override:** `MEMCACHED_ADDRS` overrides `cache.memcached.addrs`.

See `docs/cache-design-plan.md` for design and health check behavior.

### `reliability`

| Field | Type | Description |
|-------|------|-------------|
| `retry_max_attempts` | int | Max retries for weather API calls on transient failure |
| `retry_base_delay` | duration | Initial backoff between retries |
| `retry_max_delay` | duration | Maximum backoff cap |
| `rate_limit_rps` | int | Sustained requests per second (rate limiter) |
| `rate_limit_burst` | int | Burst capacity: max requests allowed in a short window before throttling. |

**Rate limit behavior:** Standard token bucket. Tokens refill at `rate_limit_rps` per second. Each request consumes one token. `rate_limit_burst` is the bucket size—the maximum tokens available. A client can send up to `burst` requests back-to-back; after that, requests are throttled until tokens refill. A burst larger than rps allows a spike (e.g. rps=5, burst=10: up to 10 requests at once, then refill at 5/sec)

### `shutdown`

| Field | Type | Description |
|-------|------|-------------|
| `timeout` | duration | Grace period for in-flight requests before process exit |

### `lifecycle`

| Field | Type | Description |
|-------|------|-------------|
| `ready_delay` | duration | Min uptime before declaring healthy (avoids starting-up flicker). `"3s"`, `"0s"` to skip. |
| `overload_window` | duration | Sliding window for overload calculation |
| `overload_threshold_pct` | int | Percent of `rate_limit_rps * overload_window`; exceeded → overloaded. Scales with `reliability.rate_limit_rps` for testing. |
| `idle_threshold_req_per_min` | int | Requests/min below this for `idle_window` → status idle |
| `idle_window` | duration | Sliding window for idle detection |
| `minimum_lifespan` | duration | Min uptime before idle can be declared |
| `degraded_window` | duration | Window for error-rate calculation |
| `degraded_error_pct` | int | Error rate (errors/total in window) above this % → degraded |
| `degraded_retry_initial` | duration | First delay; Fibonacci sequence builds from here |
| `degraded_retry_max` | duration | Max delay; sequence caps here, then shutdown |

### `metrics`

| Field | Type | Description |
|-------|------|-------------|
| `tracked_locations` | list of strings | City names that get per-location metrics in `weatherQueriesByLocationTotal`. All other locations increment `location="other"`. When omitted or empty, defaults to 100 cities (see `internal/config/config.go`). |

---

## Duration Format

All duration fields use Go's `time.ParseDuration` format:

- `"100ms"` — milliseconds
- `"2s"` — seconds
- `"5m"` — minutes

---

## Example (minimal)

```yaml
server:
  port: "8080"

weather_api:
  url: "https://api.openweathermap.org/data/2.5/weather"
  timeout: "2s"

request:
  timeout: "5s"

cache:
  backend: "in_memory"
  ttl: "10s"
  memcached:
    addrs: "localhost:11211"
    timeout: "500ms"
    max_idle_conns: 2

reliability:
  retry_max_attempts: 3
  retry_base_delay: "100ms"
  retry_max_delay: "2s"
  rate_limit_rps: 5
  rate_limit_burst: 10

shutdown:
  timeout: "10s"

lifecycle:
  ready_delay: "3s"
  overload_window: "60s"
  overload_threshold_pct: 80
  idle_threshold_req_per_min: 5
  idle_window: "5m"
  minimum_lifespan: "5m"
  degraded_window: "60s"
  degraded_error_pct: 5
  degraded_retry_initial: "1m"
  degraded_retry_max: "13m"

metrics:
  tracked_locations:
    - seattle
    - portland
    - london
```

---

## Validation

- `REQUEST_TIMEOUT` must be greater than `WEATHER_API_TIMEOUT`
- Duration fields: invalid values fall back to internal defaults
- Missing optional fields: use code defaults
