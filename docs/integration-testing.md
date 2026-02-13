# Integration Testing Guide

## Overview

Integration tests validate system behavior with real dependencies (external API, Memcached). This project includes two types of integration testing:

1. **Service Integration Tests** (`test-service.sh`) - Tests the full running service binary with real HTTP requests
2. **Go Integration Tests** (`-tags=integration`) - Tests individual components with real dependencies

Both complement unit tests by verifying component integration and end-to-end request flows.

## Types of Integration Tests

### 1. Service Integration Tests (`test-service.sh`)

**Purpose:** Tests the full running service binary with real HTTP requests.

**What it tests:**
- Full service startup and shutdown
- All HTTP endpoints (`/health`, `/weather/{location}`, `/metrics`)
- Cache functionality (cache hits/misses)
- Synthetic lifecycle scenarios (load, degraded state, recovery)
- Real service behavior with actual dependencies

**Run service integration tests:**
```bash
export WEATHER_API_KEY=your_api_key
./test-service.sh all
```

**Advantages:**
- Tests the actual service binary (production-like)
- Validates full request/response cycle
- Tests service lifecycle (startup, shutdown)
- Easy to use for manual testing

**See:** `README.md` Testing section for detailed usage

### 2. Go Integration Tests (`-tags=integration`)

**Purpose:** Tests individual components with real dependencies (API, Memcached).

**What it tests:**
- Component integration (HTTP handlers → service → cache → API)
- Degraded state recovery with real API failures
- Rate limiting under concurrent load
- Error propagation through layers

**Prerequisites:**
- `WEATHER_API_KEY` environment variable set (valid OpenWeatherMap API key)
- Optional: Memcached running on `localhost:11211` (or set `MEMCACHED_ADDRS`)
- Optional: `INTEGRATION_CACHE_BACKEND` environment variable set to `"memcached"` to use Memcached (defaults to in-memory)

**Run Go integration tests:**
```bash
export WEATHER_API_KEY=your_api_key
go test -tags=integration ./...
```

### Run Specific Integration Tests

```bash
go test -tags=integration ./internal/http
go test -tags=integration ./internal/degraded
```

### Run with Verbose Output

```bash
go test -tags=integration -v ./...
```

### Run Specific Test

```bash
go test -tags=integration -v ./internal/http -run TestIntegration_GetWeather_CacheHit
```

## Test Organization

### Service Integration Tests

**Script:** `test-service.sh`
- Full service binary testing
- HTTP endpoint validation
- Cache functionality testing
- Synthetic lifecycle scenarios
- Service startup/shutdown

**Commands:**
- `./test-service.sh all` - Build, start, test, cleanup
- `./test-service.sh test` - Run tests against running service
- `./test-service.sh synthetic` - Run lifecycle tests

### Go Integration Tests

**End-to-End Tests:** `internal/http/integration_test.go`
- Full request flow from HTTP handler through service, cache, and external API
- Cache hit/miss scenarios
- Error propagation
- Health check and metrics endpoints
- Rate limiting behavior

**Degraded State Tests:** `internal/degraded/integration_test.go`
- Degraded state detection with real API failures
- Recovery sequence validation
- Recovery override testing
- Error tracking validation

**Test Helpers:** `internal/testhelpers/integration.go`
- Shared test infrastructure
- Service setup helpers
- Configuration management

## Test Patterns

### Build Tags

All integration tests use `//go:build integration` build tags:

```go
//go:build integration
// +build integration

package http
```

This allows running unit tests and integration tests separately:
- Unit tests: `go test ./...` (excludes integration tests)
- Integration tests: `go test -tags=integration ./...` (includes integration tests)

### Graceful Skipping

Tests skip gracefully if dependencies are unavailable:

```go
apiKey := os.Getenv("WEATHER_API_KEY")
if apiKey == "" {
    t.Skip("WEATHER_API_KEY not set, skipping integration test")
}
```

### Test Helpers

Use test helpers from `internal/testhelpers`:

```go
cfg := testhelpers.GetIntegrationConfig(t)
weatherService, cacheSvc, cleanup := testhelpers.SetupIntegrationService(t, cfg)
defer cleanup()
```

## Writing Integration Tests

### Example: End-to-End Test

```go
//go:build integration
// +build integration

package http

func TestIntegration_GetWeather_CacheHit(t *testing.T) {
    handler, cacheSvc, cleanup := setupIntegrationHandler(t)
    defer cleanup()
    
    // Arrange: Pre-populate cache
    ctx := context.Background()
    testData := models.WeatherData{Location: "seattle", Temperature: 15.5}
    cacheSvc.Set(ctx, "seattle", testData, 5*time.Minute)
    
    // Act: Make HTTP request
    w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
    
    // Assert: Verify response
    if w.Code != http.StatusOK {
        t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
    }
}
```

### Example: Rate Limiting Test

```go
func TestIntegration_RateLimiting_Enforcement(t *testing.T) {
    handler, _, cleanup := setupRateLimitedHandler(t, 10, 20) // 10 RPS, burst 20
    defer cleanup()
    
    // Send requests exceeding rate limit
    for i := 0; i < 30; i++ {
        w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
        // Verify some get 429
    }
}
```

## Test Scenarios

### End-to-End Tests

1. **Cache Hit:** Pre-populate cache, verify no API call
2. **Cache Miss:** Verify API call and cache population
3. **Error Propagation:** Invalid API key → 503 error
4. **Health Check:** Real API key validation
5. **Metrics:** Prometheus format validation

### Degraded State Tests

1. **Detection:** Invalid API key triggers degraded state
2. **Recovery Sequence:** Fibonacci backoff delays
3. **Recovery Overrides:** Test-only override mechanisms
4. **Error Tracking:** Error rate calculation

### Rate Limiting Tests

1. **Enforcement:** Requests exceeding limit get 429
2. **Concurrent:** Rate limiting under concurrent load
3. **Window:** Rate limit window behavior over time
4. **Metrics:** Rate limit denials recorded in metrics

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `WEATHER_API_KEY` | Yes | - | OpenWeatherMap API key (required for integration tests) |
| `WEATHER_API_URL` | No | `https://api.openweathermap.org/data/2.5/weather` | API endpoint URL |
| `MEMCACHED_ADDRS` | No | `localhost:11211` | Memcached server address |
| `INTEGRATION_CACHE_BACKEND` | No | `in_memory` | Cache backend (`in_memory` or `memcached`) |

## Best Practices

1. **Always Cleanup:** Use `defer cleanup()` for test resources
2. **Skip Gracefully:** Skip tests if dependencies unavailable
3. **Use Unique Keys:** Use unique cache keys or clear cache between tests
4. **Realistic Data:** Use realistic but simple test data
5. **Document Tests:** Document test purpose and behavior
6. **Isolate Tests:** Tests should be independent and not rely on each other

## Troubleshooting

### Tests Skip Unexpectedly

- Check `WEATHER_API_KEY` is set
- Verify API key is valid and activated (may take up to 2 hours)
- Check Memcached is running (if using Memcached backend)

### Tests Fail with Network Errors

- Verify internet connectivity
- Check API endpoint URL is correct
- Verify API key hasn't expired

### Rate Limiting Tests Flaky

- Increase delays between requests
- Use lower rate limits for testing
- Add small delays to account for timing

### Cache Tests Fail

- Verify cache backend is available
- Check cache keys are unique
- Clear cache between tests if needed

## CI/CD Integration

Integration tests can be run in CI/CD pipelines:

```yaml
# Example GitHub Actions step
- name: Run integration tests
  env:
    WEATHER_API_KEY: ${{ secrets.WEATHER_API_KEY }}
  run: go test -tags=integration ./...
```

**Note:** Integration tests may be slower and require external dependencies. Consider:
- Running in separate CI job
- Not blocking unit test runs
- Using test API keys
- Limiting test frequency to avoid rate limits

## Comparison: Service vs Go Integration Tests

| Aspect | `test-service.sh` | Go Integration Tests |
|--------|-------------------|---------------------|
| **Scope** | Full service binary | Individual components |
| **Dependencies** | Running service process | Component dependencies |
| **Test Type** | Black-box HTTP testing | White-box component testing |
| **Use Case** | Manual testing, CI/CD | Automated component validation |
| **Speed** | Slower (process startup) | Faster (no process startup) |
| **Coverage** | Endpoints, lifecycle | Component integration, edge cases |

**When to use each:**
- **`test-service.sh`**: Quick manual testing, validating service works end-to-end, CI/CD smoke tests
- **Go integration tests**: Validating component integration, testing edge cases, regression testing

## References

- Service Integration Tests: `test-service.sh` (see `README.md` Testing section)
- Existing Component Integration Tests: `internal/client/client_integration_test.go`, `internal/cache/memcached_integration_test.go`
- Testing Standards: `.cursor/rules/040-testing.mdc`
- Go Testing: https://pkg.go.dev/testing
