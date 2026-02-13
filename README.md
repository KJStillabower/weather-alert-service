# Weather Alert Service

[![CI](https://github.com/KJStillabower/weather-alert-service/actions/workflows/ci.yml/badge.svg)](https://github.com/KJStillabower/weather-alert-service/actions/workflows/ci.yml)

A production-ready backend service that provides weather data via REST API, integrating with OpenWeatherMap.  This is a hypothetical service created for as an exercise to demonstrate how to run Site Reliability Design.  This was a bounded exercise to leverage AI, Scalability, DevOps processes and overall understanding of the exercise.  

![Architecture](docs/architecture.png)

**NOTES**
- Design Documentation is elaborated in the [docs/](docs) folder.  Here there are a number of overviews detailing decision and implementation proceses to elaborate futher edits and expansions.
- Overall [docs/about.md](docs/about.md) contains details on how this repo is setup and how to interact with design.


## API Endpoints
**Endpoints:**
- `GET /weather/{location}` - Get weather data for location
- `GET /health` - Health check (validates API key connectivity)
- `GET /metrics` - Prometheus metrics

**API Key Activation:**
OpenWeatherMap API keys can take up to 2 hours to activate after account creation. The service validates the API key at startup and exits with an error if invalid; the `/health` endpoint also validates on each probe.


## Quick Start

*TLDR;*
```bash
export ENV_NAME=dev_localcache
./test-service.sh all verbose
./test-service.sh synthetic
```

### Prerequisites

- **Go 1.21+** - [Download](https://golang.org/dl/)
- **OpenWeatherMap API Key** - [Get free key](https://openweathermap.org/api)
- **Memcached** for caching layer
- **bash** (for test script)

If you don't know how to install memcached, you can use a local cache to facilitate quicker start. Just reference the ```backend: "in_memory"``` in the config (See: `config/dev_localcache.yaml`)

If you have a docker or kubernettes environment, there are build scripts in the [samples/containers]() location


### Installation

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Adjust the config file to reference your appropriate memcached locations.

### Configuration

**Environment selection:** The service loads config from `config/[env].yaml`. Set `ENV_NAME` to choose the file (default: `dev`).
```bash
export ENV_NAME=dev            # loads config/dev.yaml (default)
export ENV_NAME=dev_localcache # loads config/dev_localcache (local, no memcache)
export ENV_NAME=prod           # loads config/prod.yaml
```

**API key only** (the only secret): Configure using one of these methods:

Option 1. **Environment variable (recommended):**
   ```bash
   export WEATHER_API_KEY=[your_api_key_here]
   ```

Option 2. **Secrets file:** Create or edit `config/secrets.yaml`:
   ```yaml
   weather_api_key: "[your_api_key_here]"
   ```

All other settings (port, timeouts, cache, retries, etc.) are in `config/[env].yaml`.

**Note:** API keys can take up to 2 hours to activate after account creation.

### Building
```bash
go build -o bin/service ./cmd/service
```

There is also test-service.sh for easy command-line automation.  See below for more on it's usage.

## Running the Service

### Start the Service

```bash
./bin/service
```

The default dev service starts on port `8080` as defined in the yaml. Configure via `server.port` in `config/[env].yaml`:

```bash
ENV_NAME=prod ./bin/service
```

### Verify Service is Running

Check the health endpoint:
```bash
curl -s "http://localhost:8080/health"
```

Expected response:
```json
{
   "checks":
   {
      "cache":"healthy",
      "weatherApi":"healthy"
   },
   "service":"weather-alert-service",
   "status":"healthy",
   "timestamp":"2026-02-12T18:02:39Z",
   "version":"dev"
}
```

## Testing

### Automated Testing Script

The project includes a comprehensive test script (`test-service.sh`) that automates building, starting, testing, and cleanup.

#### Run All Tests

```bash
./test-service.sh [all]
```

This command will:
1. Build the service
2. Start the service
3. Run all endpoint tests
4. Verify cache functionality
5. Check metrics
6. Stop the service 

#### Individual Test Commands

```bash
# Build the service
./test-service.sh build

# Build and start the service (service stays running after script exits)
./test-service.sh start

# Run all tests (service must be running)
./test-service.sh test

# Test specific endpoints (service must be running)
./test-service.sh health
./test-service.sh weather [location]
./test-service.sh metrics
./test-service.sh cache

# Lifecycle tests via /test (requires testing_mode, auto-starts service if needed)
./test-service.sh synthetic

# Memcached (when using cache.backend=memcached)
./test-service.sh start_cache
./test-service.sh stop_cache

# Stop the running service explicitly
./test-service.sh stop
# Or: ./test-service.sh cleanup

# View service logs
./test-service.sh logs
```

#### Verbose Mode

Use `-v` or `--verbose` to show raw API responses instead of summary output:

```bash
./test-service.sh --verbose all
./test-service.sh -v weather {location}
```

#### Cleanup Behavior

The script manages service lifecycle based on how it was invoked:

| Command | Cleanup on exit? |
|---------|------------------|
| `start` | No - service keeps running |
| `weather`, `test`, `health`, `metrics`, `cache` | No - service was already running |
| `synthetic` | Yes - auto-starts service, cleans up after tests |
| `start_cache`, `stop_cache` | N/A - memcached only, no service lifecycle |
| `stop` / `cleanup` | Yes - explicit stop (not a trap) |
| `all` | Yes - service started by this run, cleaned up automatically |

**Typical workflow:**
```bash
./test-service.sh start             # Start service (no cleanup)
./test-service.sh weather portland  # Test with service already running (no cleanup)
./test-service.sh stop              # Explicitly stop when done
```

The PID is stored in `/tmp/weather-service.pid`, so you can stop the service later even from a different terminal.

#### Test Script Features

- **Colored output** for easy reading
- **Selective cleanup** - only cleans up when `all` is used; `start` and test commands leave the service running
- **Verbose mode** - `-v` flag for raw API output
- **Health validation** - verifies API key connectivity
- **Cache verification** - confirms caching is working
- **Error handling** - clear error messages
- **PID tracking** - prevents multiple instances, enables stop via `stop`/`cleanup`

### Integration Tests

The project includes two types of integration testing:

#### 1. Service Integration Tests (`test-service.sh`)

Tests the full running service binary with real HTTP requests. This is the primary integration testing approach for manual testing and validation.

**Run service integration tests:**
```bash
export WEATHER_API_KEY=your_api_key
./test-service.sh all
```

**What it tests:**
- Full service startup and operation
- All HTTP endpoints (`/health`, `/weather/{location}`, `/metrics`)
- Cache functionality (hits/misses)
- Synthetic lifecycle scenarios (degraded state, recovery)

See the [Testing](#testing) section above for detailed `test-service.sh` usage.

#### 2. Go Integration Tests (`-tags=integration`)

Tests individual components with real dependencies (API, Memcached). Useful for automated testing and component-level validation.

**Prerequisites:**
- `WEATHER_API_KEY` environment variable set (valid OpenWeatherMap API key)
- Optional: Memcached running (for cache integration tests)

**Run Go integration tests:**
```bash
export WEATHER_API_KEY=your_api_key
go test -tags=integration ./...
```

**Run specific integration tests:**
```bash
go test -tags=integration ./internal/http
go test -tags=integration ./internal/degraded
```

**What it tests:**
- Component integration (handlers → service → cache → API)
- Degraded state recovery with real API failures
- Rate limiting under concurrent load
- Error propagation through layers

See `docs/integration-testing.md` for detailed integration testing guide and comparison of both approaches.

### Performance Benchmarks

Performance benchmarks establish baseline metrics for critical code paths and enable regression detection.

**Run all benchmarks:**
```bash
go test -bench=. -benchmem ./...
```

**Run benchmarks for specific packages:**
```bash
go test -bench=. -benchmem ./internal/cache
go test -bench=. -benchmem ./internal/client
go test -bench=. -benchmem ./internal/http
```

**Benchmark options:**
- `-benchmem` - Report memory allocations
- `-benchtime=5s` - Run each benchmark for 5 seconds
- `-cpu=1,2,4` - Run with different CPU counts

See `docs/performance-benchmarks.md` for comprehensive benchmark documentation, interpretation guide, and optimization recommendations.

### Manual Testing

#### Get Weather Data

```bash
curl http://localhost:8080/weather/seattle
```

Response:
```json
{
  "location": "seattle",
  "temperature": 7.15,
  "conditions": "few clouds",
  "humidity": 83,
  "windSpeed": 4.63,
  "timestamp": "2026-02-11T12:58:17.49200584-05:00"
}
```

#### Check Metrics

```bash
curl http://localhost:8080/metrics
```

Returns Prometheus-formatted metrics including:
- `httpRequestsTotal`, `httpRequestDurationSeconds`, `httpRequestSizeBytes`, `httpResponseSizeBytes` - Request counts, latencies, and payload sizes
- `weatherAPICallsTotal`, `weatherApiDurationSeconds`, `weatherApiErrorsTotal` - External API calls and errors by category
- `cacheHitsTotal`, `cacheStampedeDetectedTotal`, `cacheStampedeConcurrency` - Cache hits and stampede detection
- `weatherQueriesTotal`, `weatherQueriesByLocationTotal`, `httpErrorsTotal` - Queries and HTTP errors by category
- Rate limit and runtime metrics. Full list and PromQL: [docs/observability.md](docs/observability.md)

#### Health Check

```bash
curl http://localhost:8080/health
```

The health endpoint validates:
- Service is running
- API key is valid and activated
- When memcached is configured: `checks.cache` reports healthy or unhealthy

**Status values:**
- `healthy` - All systems operational
- `degraded` - API key invalid, or downstream errors/timeouts; do not route
- `overloaded` - At capacity (rate-limit denials in window); LB should shed load or scale up
- `idle` - Low traffic; candidate for scale-down (200, LB may reduce weight)
- `shutting-down` - Draining; do not route new traffic

**Overload: /health vs /weather**

When overloaded, the service can still serve traffic. 503 on `/health` is a routing signal—"do not send me more traffic." `/weather` never returns 503 for overload; it returns 200 for accepted requests and 429 for rate-limited ones. See `docs/health-status-plan.md`.

## API Endpoints

### GET /weather/{location}

Returns current weather data for the specified location.

**Parameters:**
- `location` (path) - City name (e.g., "seattle", "new york")

**Response:** `200 OK`
```json
{
  "location": "seattle",
  "temperature": 7.15,
  "conditions": "few clouds",
  "humidity": 83,
  "windSpeed": 4.63,
  "timestamp": "2026-02-11T12:58:17.49200584-05:00"
}
```

**Error Responses:**
- `400 Bad Request` - Invalid location
- `429 Too Many Requests` - Rate limit exceeded (config: `rate_limit_rps`, `rate_limit_burst`)
- `503 Service Unavailable` - Upstream API unavailable or request timeout

### GET /health

Service health and readiness check.

**Response:** `200 OK` (healthy, idle) or `503 Service Unavailable` (degraded, overloaded, shutting-down)
```json
{
  "status": "healthy",
  "service": "weather-alert-service",
  "version": "dev",
  "checks": {
    "weatherApi": "healthy",
    "cache": "healthy"
  }
}
```

### GET /metrics

**Full observability guide:** [docs/observability.md](docs/observability.md) — metrics, PromQL cookbook, logging, correlation IDs, health lifecycle, alerts, runbooks.

Prometheus metrics endpoint for scraping. Returns application metrics plus process/runtime (CPU, memory, goroutines) for observability.

**Response:** `200 OK` (Content-Type: `text/plain; version=0.0.4`)

**Application metrics:**

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `httpRequestsTotal` | Counter | `method`, `route`, `statusCode` | Total HTTP requests. `route` is path template (e.g. `/weather/{location}`). |
| `httpRequestDurationSeconds` | Histogram | `method`, `route` | Request latency per request. p95/p99: `histogram_quantile(0.95, rate(..._bucket[5m]))`. |
| `httpRequestsInFlight` | Gauge | — | Concurrent requests. Watch for saturation. |
| `weatherApiCallsTotal` | Counter | `status` | OpenWeatherMap calls. `status`: `success`, `error`, `rate_limited`, `client_error`, `server_error`. |
| `weatherApiDurationSeconds` | Histogram | `status` | External API latency per request. Buckets: 0.1, 0.25, 0.5, 1, 2.5, 5, 10s. |
| `weatherApiRetriesTotal` | Counter | — | Retry attempts. High value indicates unstable upstream. |
| `cacheHitsTotal` | Counter | `cacheType` | Cache hits. Misses = lookups - hits. Hit rate = hits/(hits+misses). |
| `weatherQueriesTotal` | Counter | — | Total weather lookups. rate() for QPS. |
| `weatherQueriesByLocationTotal` | Counter | `location` | Per-location queries (allow-list; others use `other`). Top: `topk(10, sum by (location)(rate(...[1h])))`. |
| `httpRequestSizeBytes` | Histogram | `method`, `route` | Request body size. Capacity planning; DoS awareness. |
| `httpResponseSizeBytes` | Histogram | `method`, `route`, `statusCode` | Response body size. |
| `cacheStampedeDetectedTotal` | Counter | `location` | Concurrent cache misses > 1 for same key (stampede). |
| `cacheStampedeConcurrency` | Histogram | `location` | Concurrent miss count when stampede detected. |
| `weatherApiErrorsTotal` | Counter | `category` | API errors by category (timeout, rate_limited, upstream_5xx, etc.). |
| `httpErrorsTotal` | Counter | `method`, `route`, `category` | HTTP errors by category. |
| `shutdownInFlightRequests` | Gauge | — | In-flight request count recorded at shutdown (before wait). |
| `circuitBreakerState` | Gauge | `component` | Circuit breaker state (0=closed, 1=open, 2=half-open). |
| `circuitBreakerTransitionsTotal` | Counter | `component`, `from`, `to` | State transitions. |
| `requestTimeoutPropagatedTotal` | Counter | `propagated` | Requests where upstream timeout was derived from request context (`yes`/`no`). |
| `cacheErrorsTotal` | Counter | `operation`, `type` | Cache errors by operation (get/set) and type (timeout, connection, unknown). |
| `cacheOperationDurationSeconds` | Histogram | `operation`, `status` | Cache Get/Set duration; status success/error. |

**Runtime metrics** (process_cpu_seconds_total, process_resident_memory_bytes, go_goroutines, etc.): standard Prometheus process and Go collectors. CPU utilization: `rate(process_cpu_seconds_total[1m])`.

**Prometheus scrape config:**
```yaml
scrape_configs:
  - job_name: 'weather-alert-service'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /metrics
```

**Alerting samples:** `samples/alerting/` contains example `prometheus.yaml`, `alert-rules.yaml`, `recording-rules-slo.yaml` (SLO calculations), and `alertmanager.yaml` with PagerDuty and FireHydrant integration. Cache error alerts (e.g. `HighCacheErrorRate`, `CacheBackendDown`) are in the same alert rules file. See [docs/observability.md](docs/observability.md) for SLO tracking and recording rules.

**Circuit breaker (optional):** When enabled via config (`circuit_breaker.enabled` or `CIRCUIT_BREAKER_ENABLED`), upstream weather API calls go through a circuit breaker. After a configurable number of failures the circuit opens and requests fail fast; after a timeout the circuit goes half-open and a success threshold closes it. Metrics: `circuitBreakerState`, `circuitBreakerTransitionsTotal`.

**Request timeout propagation:** When a request has a deadline (e.g. from an upstream gateway), the weather client uses up to 90% of the remaining time for the upstream API call (capped by the configured client timeout, minimum 100ms). This keeps upstream calls within the request timeout budget. `requestTimeoutPropagatedTotal{propagated="yes"|"no"}` tracks whether the timeout was derived from context.

### Logging

Structured logging (zap) with JSON output and ISO8601 timestamps. Logs are written to **stderr** (default; suitable for container/process capture).

- **Correlation IDs:** Every request gets a correlation ID (from `X-Correlation-ID` header or generated). Stored in context and included in all request-scoped log fields; propagated to upstream API via `X-Correlation-ID`.
- **No sensitive data:** API keys and credentials are never logged.
- **What we log:** Lifecycle (startup/shutdown), failures (weather fetch errors, health degraded, shutdown errors), and boundaries (e.g. health degraded when API key validation fails). Routine success paths are not logged; metrics cover those.
- **LOG_LEVEL:** Optional env var (`DEBUG`, `INFO`, `WARN`, `ERROR`). Default `INFO`; use `DEBUG` for development. Set via environment only—not in config YAML.

## Deployment

### Configuration Files

| File | Purpose |
|------|---------|
| `config/dev.yaml` | Development (memcached cache, testing_mode). Requires `./test-service.sh start_cache`. |
| `config/dev_localcache.yaml` | Development (in-memory cache). No memcached; use when memcached unavailable. |
| `config/prod.yaml` | Production config |
| `config/secrets.yaml` | API key only (gitignored) |

The service loads `config/{ENV_NAME}.yaml`. Set `ENV_NAME=dev_localcache` for in-memory dev. Add files (e.g. `config/staging.yaml`) as needed.

**Optional:** Override `metrics.tracked_locations` in env YAML to customize which locations get per-location metrics (default: 100 cities; others increment `other`).

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ENV_NAME` | Which config file to load (`config/{ENV_NAME}.yaml`) | `dev` |
| `WEATHER_API_KEY` | OpenWeatherMap API key (or set in `config/secrets.yaml`) | Required |
| `LOG_LEVEL` | Log level (`DEBUG`, `INFO`, `WARN`, `ERROR`). Env var only; not in `config/*.yaml`. | `INFO` |
| `SHUTDOWN_INFLIGHT_TIMEOUT` | Max time to wait for in-flight requests during shutdown | `5s` |
| `SHUTDOWN_INFLIGHT_CHECK_INTERVAL` | Interval between in-flight count checks during shutdown | `100ms` |
| `CIRCUIT_BREAKER_ENABLED` | Enable circuit breaker for upstream API calls | `false` |
| `CIRCUIT_BREAKER_FAILURE_THRESHOLD` | Failures before opening circuit | `5` |
| `CIRCUIT_BREAKER_SUCCESS_THRESHOLD` | Successes in half-open before closing | `2` |
| `CIRCUIT_BREAKER_TIMEOUT` | Time before half-open after open | `30s` |

### Production Considerations

1. **API Key Security**
   - Never commit API keys to version control
   - Use environment variables or secret management system built into devOps (why we default to env vars)
   - Rotate keys periodically

2. **Monitoring**
   - Expose `/metrics` endpoint for Prometheus scraping
   - Monitor `/health` endpoint for load balancer health checks
   - Set up alerts on error rates and latency

3. **Scaling**
   - Service is stateless; cache is configurable (`in_memory` or `memcached`)
   - Multiple instances can run behind a load balancer
   - Use `cache.backend: memcached` for shared cache across instances (see `docs/cache-design-plan.md`)

4. **Graceful Shutdown**
   - Service handles SIGTERM/SIGINT signals
   - Configurable shutdown timeout; server stops accepting new requests first
   - In-flight request tracker: middleware counts active requests; shutdown waits for in-flight to reach zero (configurable timeout and check interval) before proceeding
   - Telemetry flush: logger synced after in-flight wait so logs are not lost
   - Memcached (if configured) closed after telemetry flush
   - Config: `shutdown.in_flight_timeout`, `shutdown.in_flight_check_interval` (or env `SHUTDOWN_INFLIGHT_TIMEOUT`, `SHUTDOWN_INFLIGHT_CHECK_INTERVAL`)

## Lifecycle

The `/health` endpoint returns a lifecycle-aware status for load balancers, Kubernetes, and autoscalers. Thresholds are configurable in `config/[env].yaml` under `lifecycle`.

| Status | Meaning | HTTP | Load manager action |
|--------|---------|------|---------------------|
| `healthy` | Can serve traffic; dependencies OK | 200 | Route traffic |
| `idle` | Low traffic; candidate for scale-down | 200 | Optional: scale down |
| `overloaded` | At capacity; shed load | 503 | Scale up, reduce weight, or back off |
| `degraded` | API errors; dependency issues | 503 | Stop routing; alert |
| `shutting-down` | Draining; do not send new requests | 503 | Remove from pool |

**Overload vs degraded:** Overloaded means at capacity; the service still serves requests that get through. Degraded means producing or receiving errors; do not route. Cache unhealthy does not force degraded; it is reported in `checks.cache` only.

**Formulas:** Overloaded when `requests in window > rate_limit_rps × overload_window × (overload_threshold_pct/100)`. Degraded when error rate in window exceeds `degraded_error_pct`. Idle when requests/min below `idle_threshold_req_per_min` for `idle_window` after `minimum_lifespan`.

**Degraded recovery:** Fibonacci backoff from `degraded_retry_initial` to `degraded_retry_max`; exhaustion triggers `shutting-down`. 

*Notes:*
- See `docs/health-status-plan.md` for design notes 
- See `docs/testing-simulation-plan.md` for synthetic testing simulation.




## Troubleshooting

**Troubleshooting with observability:** Use correlation IDs (`X-Correlation-ID` in response) to trace requests through logs. See [docs/observability.md](docs/observability.md) for PromQL queries, when to investigate, and alert runbooks.

### Service Won't Start

**API key validation at startup:** The service validates the API key before listening. If you see "API key validation failed at startup", the key is invalid or not yet activated.

**Check API key:**
```bash
echo $WEATHER_API_KEY
# Or check config/secrets.yaml
```

**Check logs:**
```bash
./test-service.sh logs
# Or if running manually, check stderr
```

### Health Check Shows "degraded"

- API key may not be activated yet (wait up to 2 hours)
- Verify API key format (32 hex characters)
- Check API key in OpenWeatherMap account dashboard
- If MemCache configured, but unreachable, may show cache health issue

### 401 Unauthorized Errors

- API key is invalid or not activated
- Wait up to 2 hours after account creation
- Verify key matches exactly from OpenWeatherMap dashboard

### Cache Not Working

- Check cache TTL in config (`cache.ttl`)
- Verify cache metrics in `/metrics` endpoint (`cacheHitsTotal`)
- **in_memory:** Data lost on restart; single-instance only
- **memcached:** Run memcached (e.g. `./test-service.sh start_cache`), ensure `checks.cache=healthy` in `/health`

## Development

For development and contribution guidelines, see `docs/about.md`.

### Upcoming Features

#### Load Shedding
There is a plan document that wire-frames the load shedding principles that could be extended to reduce demand.  The lifecycle rate limits in place are designed to mitigate massive spikes and the implementation of a shared memcache would handle massive demand in our API.  However, it is conceivable that the design may benefit from expanded load shedding capabilities to reduce in-flight failures and prevent cascading demand failures through the stack.  Overall these are outlined in the docs, but were not addressed in this exercise due to time constraints.

#### Distributed Tracing
This was considered, but ultimately not pursued in this exercise.  It was prioritized to invest time in the testing and documentaiton tooling instead.  Much of distributed tracing require fact specific patterns of failure modes, architectures both up-stream and down stream, as well as broader observability infrastructure.  While that hasn't been attempted here, we are able to preserve some analysis in the plan docs for future iterations.
