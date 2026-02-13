# Enhanced Integration Test Scenarios Implementation Plan

**Status:** Planning  
**Priority:** Medium  
**Focus:** End-to-end integration testing with real dependencies

## Overview

This plan extends the existing integration test suite with comprehensive end-to-end scenarios that test the full request flow through all layers (HTTP handlers → service → cache → external API). These tests validate system integration, degraded state recovery, and rate limiting behavior with real dependencies.

## Objectives

1. Test full request flow from HTTP handler through all layers
2. Validate degraded state detection and recovery with real API failures
3. Test rate limiting behavior with concurrent requests
4. Ensure integration between components works correctly
5. Catch integration bugs that unit tests might miss

## Scope

**In Scope:**
- End-to-end service integration tests (full request flow)
- Degraded state recovery integration tests
- Rate limiting integration tests
- Test helpers and infrastructure

**Out of Scope:**
- Load/stress testing (separate initiative)
- Performance testing (covered by benchmarks plan)
- Chaos engineering tests (separate initiative)
- Multi-service integration (beyond current scope)

## Current State

**Existing Integration Tests:**
- `internal/client/client_integration_test.go` - Tests OpenWeatherMap API client
- `internal/cache/memcached_integration_test.go` - Tests Memcached cache

**Patterns Used:**
- `//go:build integration` build tags
- Skip tests if dependencies unavailable (API key, Memcached)
- Test against real external services
- API key format validation

**Gaps:**
- No end-to-end tests (handler → service → cache → API)
- No degraded state recovery tests
- No rate limiting integration tests
- No tests for full request lifecycle

## Implementation Tasks

### Task 1: End-to-End Service Integration Tests

**Deliverable:** `internal/http/integration_test.go` or `cmd/service/integration_test.go`

**Purpose:** Test complete request flow from HTTP handler through service layer, cache, and external API to validate system integration.

**Steps:**

1. **Create Integration Test File**
   - Choose location: `internal/http/integration_test.go` (tests HTTP layer) or `cmd/service/integration_test.go` (tests full service)
   - Add `//go:build integration` build tag
   - Set up test package structure

2. **Create Test Infrastructure Helpers**
   ```go
   //go:build integration
   // +build integration
   
   package http
   
   import (
       "context"
       "net/http"
       "net/http/httptest"
       "os"
       "testing"
       "time"
       
       "github.com/gorilla/mux"
       "go.uber.org/zap"
       
       "github.com/kjstillabower/weather-alert-service/internal/cache"
       "github.com/kjstillabower/weather-alert-service/internal/client"
       "github.com/kjstillabower/weather-alert-service/internal/config"
       "github.com/kjstillabower/weather-alert-service/internal/observability"
       "github.com/kjstillabower/weather-alert-service/internal/service"
   )
   
   // setupIntegrationService creates a fully configured service for integration testing
   func setupIntegrationService(t *testing.T) (*Handler, func()) {
       apiKey := os.Getenv("WEATHER_API_KEY")
       if apiKey == "" {
           t.Skip("WEATHER_API_KEY not set, skipping integration test")
       }
       
       logger, err := observability.NewLogger()
       if err != nil {
           t.Fatalf("NewLogger() error = %v", err)
       }
       
       // Create real client
       weatherClient, err := client.NewOpenWeatherClient(
           apiKey,
           "https://api.openweathermap.org/data/2.5/weather",
           5*time.Second,
       )
       if err != nil {
           t.Fatalf("NewOpenWeatherClient() error = %v", err)
       }
       
       // Create cache (try Memcached, fallback to in-memory)
       var cacheSvc cache.Cache
       var cleanup func()
       memcachedCache, err := cache.NewMemcachedCache("localhost:11211", 500*time.Millisecond, 2)
       if err == nil {
           cacheSvc = memcachedCache
           cleanup = func() { memcachedCache.Close() }
       } else {
           t.Logf("Memcached not available, using in-memory cache: %v", err)
           cacheSvc = cache.NewInMemoryCache()
           cleanup = func() {}
       }
       
       // Create service
       weatherService := service.NewWeatherService(weatherClient, cacheSvc, 5*time.Minute)
       
       // Create handler
       handler := NewHandler(weatherService, weatherClient, nil, logger, nil)
       
       return handler, cleanup
   }
   
   // makeIntegrationRequest makes an HTTP request through the full handler stack
   func makeIntegrationRequest(t *testing.T, handler *Handler, method, path string) *httptest.ResponseRecorder {
       router := mux.NewRouter()
       router.Use(CorrelationIDMiddleware(logger))
       router.Use(MetricsMiddleware)
       router.HandleFunc("/weather/{location}", handler.GetWeather).Methods("GET")
       router.HandleFunc("/health", handler.GetHealth).Methods("GET")
       router.HandleFunc("/metrics", observability.MetricsHandler()).Methods("GET")
       
       req := httptest.NewRequest(method, path, nil)
       logger, _ := observability.NewLogger()
       req = req.WithContext(context.WithValue(req.Context(), "logger", logger))
       
       w := httptest.NewRecorder()
       router.ServeHTTP(w, req)
       return w
   }
   ```

3. **Implement Cache Hit Integration Test**
   ```go
   // TestIntegration_GetWeather_CacheHit verifies end-to-end request flow
   // when data exists in cache, avoiding upstream API call.
   func TestIntegration_GetWeather_CacheHit(t *testing.T) {
       handler, cleanup := setupIntegrationService(t)
       defer cleanup()
       
       ctx := context.Background()
       location := "seattle"
       
       // Arrange: Pre-populate cache
       cacheSvc := handler.cache // Access cache through handler (may need getter)
       testData := models.WeatherData{
           Location:    location,
           Temperature: 15.5,
           Conditions:  "Clear",
           Humidity:    65,
           WindSpeed:   10.2,
           Timestamp:   time.Now(),
       }
       if err := cacheSvc.Set(ctx, location, testData, 5*time.Minute); err != nil {
           t.Fatalf("Failed to populate cache: %v", err)
       }
       
       // Act: Make HTTP request
       w := makeIntegrationRequest(t, handler, "GET", "/weather/"+location)
       
       // Assert: Verify cache hit (should be fast, no API call)
       if w.Code != http.StatusOK {
           t.Errorf("Status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
       }
       
       var response models.WeatherData
       if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
           t.Fatalf("Failed to decode response: %v", err)
       }
       
       if response.Location != location {
           t.Errorf("Location = %q, want %q", response.Location, location)
       }
       if response.Temperature != testData.Temperature {
           t.Errorf("Temperature = %f, want %f", response.Temperature, testData.Temperature)
       }
   }
   ```

4. **Implement Cache Miss Integration Test**
   ```go
   // TestIntegration_GetWeather_CacheMiss verifies end-to-end request flow
   // when cache miss triggers upstream API call and cache population.
   func TestIntegration_GetWeather_CacheMiss(t *testing.T) {
       handler, cleanup := setupIntegrationService(t)
       defer cleanup()
       
       location := "london"
       
       // Arrange: Ensure cache is empty
       // (cache miss scenario)
       
       // Act: Make HTTP request (should trigger API call)
       w := makeIntegrationRequest(t, handler, "GET", "/weather/"+location)
       
       // Assert: Verify successful response from API
       if w.Code != http.StatusOK {
           t.Errorf("Status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
       }
       
       var response models.WeatherData
       if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
           t.Fatalf("Failed to decode response: %v", err)
       }
       
       if response.Location == "" {
           t.Error("Response missing location")
       }
       if response.Temperature == 0 {
           t.Error("Response missing temperature")
       }
       
       // Verify cache was populated (second request should be cache hit)
       w2 := makeIntegrationRequest(t, handler, "GET", "/weather/"+location)
       if w2.Code != http.StatusOK {
           t.Errorf("Second request failed: %d", w2.Code)
       }
       
       // Verify response is identical (from cache)
       var response2 models.WeatherData
       json.NewDecoder(w2.Body).Decode(&response2)
       if response.Timestamp != response2.Timestamp {
           t.Error("Second request should return cached data with same timestamp")
       }
   }
   ```

5. **Implement Error Propagation Test**
   ```go
   // TestIntegration_GetWeather_UpstreamError verifies error propagation
   // from upstream API through service to HTTP handler.
   func TestIntegration_GetWeather_UpstreamError(t *testing.T) {
       // Use invalid API key to trigger upstream error
       apiKey := "invalid_key_for_testing"
       
       logger, _ := observability.NewLogger()
       weatherClient, _ := client.NewOpenWeatherClient(
           apiKey,
           "https://api.openweathermap.org/data/2.5/weather",
           5*time.Second,
       )
       cacheSvc := cache.NewInMemoryCache()
       weatherService := service.NewWeatherService(weatherClient, cacheSvc, 5*time.Minute)
       handler := NewHandler(weatherService, weatherClient, nil, logger, nil)
       
       // Act: Make request (should fail upstream)
       w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
       
       // Assert: Verify error is properly mapped to 503
       if w.Code != http.StatusServiceUnavailable {
           t.Errorf("Status = %d, want %d", w.Code, http.StatusServiceUnavailable)
       }
       
       var errorResponse map[string]interface{}
       if err := json.NewDecoder(w.Body).Decode(&errorResponse); err != nil {
           t.Fatalf("Failed to decode error response: %v", err)
       }
       
       errorObj, ok := errorResponse["error"].(map[string]interface{})
       if !ok {
           t.Fatal("Error response missing error object")
       }
       
       if errorObj["code"] != "UPSTREAM_UNAVAILABLE" {
           t.Errorf("Error code = %v, want UPSTREAM_UNAVAILABLE", errorObj["code"])
       }
   }
   ```

6. **Implement Health Check Integration Test**
   ```go
   // TestIntegration_GetHealth_FullStack verifies health check endpoint
   // with real dependencies (API key validation, cache ping).
   func TestIntegration_GetHealth_FullStack(t *testing.T) {
       handler, cleanup := setupIntegrationService(t)
       defer cleanup()
       
       // Act: Make health check request
       w := makeIntegrationRequest(t, handler, "GET", "/health")
       
       // Assert: Verify health response
       if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
           t.Errorf("Status = %d, want 200 or 503", w.Code)
       }
       
       var healthResponse map[string]interface{}
       if err := json.NewDecoder(w.Body).Decode(&healthResponse); err != nil {
           t.Fatalf("Failed to decode health response: %v", err)
       }
       
       status, ok := healthResponse["status"].(string)
       if !ok {
           t.Fatal("Health response missing status")
       }
       
       validStatuses := []string{"healthy", "degraded", "idle", "overloaded", "shutting-down"}
       found := false
       for _, vs := range validStatuses {
           if status == vs {
               found = true
               break
           }
       }
       if !found {
           t.Errorf("Status = %q, want one of %v", status, validStatuses)
       }
   }
   ```

7. **Implement Metrics Endpoint Integration Test**
   ```go
   // TestIntegration_GetMetrics_Format verifies metrics endpoint
   // returns Prometheus-compatible format.
   func TestIntegration_GetMetrics_Format(t *testing.T) {
       handler, cleanup := setupIntegrationService(t)
       defer cleanup()
       
       // Make a request to generate metrics
       makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
       
       // Act: Request metrics
       w := makeIntegrationRequest(t, handler, "GET", "/metrics")
       
       // Assert: Verify Prometheus format
       if w.Code != http.StatusOK {
           t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
       }
       
       body := w.Body.String()
       
       // Check for Prometheus metric format (name{labels} value)
       if !strings.Contains(body, "httpRequestsTotal") {
           t.Error("Metrics missing httpRequestsTotal")
       }
       if !strings.Contains(body, "weatherApiCallsTotal") {
           t.Error("Metrics missing weatherApiCallsTotal")
       }
       if !strings.Contains(body, "cacheHitsTotal") {
           t.Error("Metrics missing cacheHitsTotal")
       }
   }
   ```

**Acceptance Criteria:**
- [ ] Integration test file exists with `//go:build integration` tag
- [ ] Test helpers created for service setup and request making
- [ ] Cache hit integration test passes
- [ ] Cache miss integration test passes (validates API call and cache population)
- [ ] Error propagation test validates error mapping
- [ ] Health check integration test passes
- [ ] Metrics endpoint integration test validates Prometheus format
- [ ] Tests skip gracefully if dependencies unavailable
- [ ] Tests are documented with purpose and behavior

---

### Task 2: Degraded State Recovery Integration Tests

**Deliverable:** `internal/degraded/integration_test.go`

**Purpose:** Test degraded state detection and recovery with real API failures to validate recovery logic works end-to-end.

**Steps:**

1. **Create Integration Test File**
   - Create `internal/degraded/integration_test.go`
   - Add `//go:build integration` build tag
   - Set up test infrastructure

2. **Create Test Helpers**
   ```go
   //go:build integration
   // +build integration
   
   package degraded
   
   import (
       "context"
       "os"
       "testing"
       "time"
       
       "github.com/kjstillabower/weather-alert-service/internal/client"
   )
   
   // setupDegradedTestClient creates a client for degraded state testing
   func setupDegradedTestClient(t *testing.T) client.WeatherClient {
       apiKey := os.Getenv("WEATHER_API_KEY")
       if apiKey == "" {
           t.Skip("WEATHER_API_KEY not set, skipping integration test")
       }
       
       client, err := client.NewOpenWeatherClient(
           apiKey,
           "https://api.openweathermap.org/data/2.5/weather",
           5*time.Second,
       )
       if err != nil {
           t.Fatalf("NewOpenWeatherClient() error = %v", err)
       }
       
       return client
   }
   ```

3. **Implement Degraded State Detection Test**
   ```go
   // TestIntegration_DegradedState_Detection verifies that degraded state
   // is detected when API key validation fails.
   func TestIntegration_DegradedState_Detection(t *testing.T) {
       // Use invalid API key to simulate degraded state
       invalidClient, _ := client.NewOpenWeatherClient(
           "invalid_key_for_degraded_test",
           "https://api.openweathermap.org/data/2.5/weather",
           5*time.Second,
       )
       
       ctx := context.Background()
       
       // Act: Attempt API key validation (should fail)
       err := invalidClient.ValidateAPIKey(ctx)
       
       // Assert: Verify error indicates degraded state
       if err == nil {
           t.Error("ValidateAPIKey() error = nil, want error (invalid key)")
       }
       
       // Verify degraded state would be detected
       // (This tests the validation that health check uses)
   }
   ```

4. **Implement Recovery Sequence Test**
   ```go
   // TestIntegration_DegradedState_RecoverySequence verifies recovery
   // sequence (Fibonacci backoff) when API becomes available.
   func TestIntegration_DegradedState_RecoverySequence(t *testing.T) {
       apiKey := os.Getenv("WEATHER_API_KEY")
       if apiKey == "" {
           t.Skip("WEATHER_API_KEY not set, skipping integration test")
       }
       
       client := setupDegradedTestClient(t)
       
       // Simulate recovery attempts
       validateFunc := func(ctx context.Context) error {
           return client.ValidateAPIKey(ctx)
       }
       
       initialDelay := 1 * time.Minute
       maxDelay := 20 * time.Minute
       
       // Test recovery delay sequence
       delays := fibDelays(initialDelay, maxDelay)
       if len(delays) == 0 {
           t.Fatal("No recovery delays generated")
       }
       
       // Verify delay sequence is Fibonacci-based
       expectedFirst := initialDelay
       if delays[0] != expectedFirst {
           t.Errorf("First delay = %v, want %v", delays[0], expectedFirst)
       }
   }
   ```

5. **Implement Recovery Override Tests**
   ```go
   // TestIntegration_DegradedState_RecoveryOverrides verifies test-only
   // recovery overrides work correctly in integration tests.
   func TestIntegration_DegradedState_RecoveryOverrides(t *testing.T) {
       // Test recovery disabled override
       SetRecoveryDisabled(true)
       defer ClearRecoveryOverrides()
       
       if !IsRecoveryDisabled() {
           t.Error("Recovery should be disabled")
       }
       
       // Test force succeed override
       ClearRecoveryOverrides()
       SetForceSucceedNextAttempt(true)
       
       // Verify override is set (would be consumed by RunRecovery)
       // (Full recovery test would require more complex setup)
   }
   ```

6. **Implement Degraded State with Real API Test**
   ```go
   // TestIntegration_DegradedState_RealAPI simulates degraded state
   // by using invalid API key, then recovers with valid key.
   func TestIntegration_DegradedState_RealAPI(t *testing.T) {
       // This test would require:
       // 1. Start with invalid API key (degraded)
       // 2. Verify degraded state detection
       // 3. Switch to valid API key
       // 4. Verify recovery
       
       // Note: This is complex and may require service restart simulation
       // Consider if this adds value vs. unit tests with mocks
       t.Skip("Complex test requiring service lifecycle - defer to unit tests")
   }
   ```

**Acceptance Criteria:**
- [ ] Integration test file exists for degraded state
- [ ] Degraded state detection test passes
- [ ] Recovery sequence test validates Fibonacci delays
- [ ] Recovery override tests pass
- [ ] Tests skip gracefully if API key unavailable
- [ ] Tests are documented

---

### Task 3: Rate Limiting Integration Tests

**Deliverable:** `internal/http/integration_test.go` (add rate limiting section)

**Purpose:** Test rate limiting behavior with concurrent requests to validate rate limiting works correctly under load.

**Steps:**

1. **Add Rate Limiting Test Helpers**
   ```go
   // setupRateLimitedHandler creates a handler with rate limiter for testing
   func setupRateLimitedHandler(t *testing.T, rps int, burst int) (*Handler, func()) {
       handler, cleanup := setupIntegrationService(t)
       
       limiter := rate.NewLimiter(rate.Limit(rps), burst)
       handler.limiter = limiter // May need setter or constructor parameter
       
       return handler, cleanup
   }
   ```

2. **Implement Rate Limit Enforcement Test**
   ```go
   // TestIntegration_RateLimiting_Enforcement verifies that rate limiter
   // correctly denies requests exceeding the rate limit.
   func TestIntegration_RateLimiting_Enforcement(t *testing.T) {
       // Setup handler with low rate limit for testing
       rps := 10
       burst := 20
       handler, cleanup := setupRateLimitedHandler(t, rps, burst)
       defer cleanup()
       
       // Act: Send burst of requests exceeding rate limit
       successCount := 0
       deniedCount := 0
       
       for i := 0; i < burst+10; i++ {
           w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
           
           if w.Code == http.StatusOK {
               successCount++
           } else if w.Code == http.StatusTooManyRequests {
               deniedCount++
               
               // Verify error response format
               var errorResponse map[string]interface{}
               if err := json.NewDecoder(w.Body).Decode(&errorResponse); err == nil {
                   errorObj := errorResponse["error"].(map[string]interface{})
                   if errorObj["code"] != "RATE_LIMITED" {
                       t.Errorf("Error code = %v, want RATE_LIMITED", errorObj["code"])
                   }
               }
           }
       }
       
       // Assert: Some requests should be denied
       if deniedCount == 0 {
           t.Error("No requests were rate limited, but some should be")
       }
       
       // Verify success count doesn't exceed burst
       if successCount > burst {
           t.Errorf("Success count = %d, should not exceed burst %d", successCount, burst)
       }
   }
   ```

3. **Implement Concurrent Rate Limiting Test**
   ```go
   // TestIntegration_RateLimiting_Concurrent verifies rate limiting
   // behavior under concurrent load.
   func TestIntegration_RateLimiting_Concurrent(t *testing.T) {
       rps := 50
       burst := 100
       handler, cleanup := setupRateLimitedHandler(t, rps, burst)
       defer cleanup()
       
       const numGoroutines = 10
       const requestsPerGoroutine = 20
       
       var wg sync.WaitGroup
       results := make(chan int, numGoroutines*requestsPerGoroutine)
       
       // Act: Send concurrent requests
       for i := 0; i < numGoroutines; i++ {
           wg.Add(1)
           go func() {
               defer wg.Done()
               for j := 0; j < requestsPerGoroutine; j++ {
                   w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
                   results <- w.Code
               }
           }()
       }
       
       wg.Wait()
       close(results)
       
       // Assert: Count results
       successCount := 0
       deniedCount := 0
       for code := range results {
           if code == http.StatusOK {
               successCount++
           } else if code == http.StatusTooManyRequests {
               deniedCount++
           }
       }
       
       // Verify rate limiting occurred
       if deniedCount == 0 {
           t.Error("No requests were rate limited under concurrent load")
       }
       
       // Verify total requests processed
       total := successCount + deniedCount
       expected := numGoroutines * requestsPerGoroutine
       if total != expected {
           t.Errorf("Total requests = %d, want %d", total, expected)
       }
   }
   ```

4. **Implement Rate Limit Window Test**
   ```go
   // TestIntegration_RateLimiting_Window verifies rate limit window
   // behavior over time (requests allowed after window expires).
   func TestIntegration_RateLimiting_Window(t *testing.T) {
       rps := 2 // Very low for testing
       burst := 5
       handler, cleanup := setupRateLimitedHandler(t, rps, burst)
       defer cleanup()
       
       // Act: Exhaust burst
       for i := 0; i < burst; i++ {
           w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
           if w.Code != http.StatusOK {
               t.Errorf("Request %d denied unexpectedly: %d", i, w.Code)
           }
       }
       
       // Next request should be denied
       w := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
       if w.Code != http.StatusTooManyRequests {
           t.Errorf("Request after burst should be denied, got %d", w.Code)
       }
       
       // Wait for rate limit window to allow more requests
       time.Sleep(time.Second / time.Duration(rps))
       
       // Next request should be allowed
       w2 := makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
       if w2.Code != http.StatusOK {
           t.Errorf("Request after window should be allowed, got %d", w2.Code)
       }
   }
   ```

5. **Implement Rate Limit Metrics Test**
   ```go
   // TestIntegration_RateLimiting_Metrics verifies that rate limit
   // denials are recorded in metrics.
   func TestIntegration_RateLimiting_Metrics(t *testing.T) {
       rps := 5
       burst := 10
       handler, cleanup := setupRateLimitedHandler(t, rps, burst)
       defer cleanup()
       
       // Exhaust rate limit
       for i := 0; i < burst+5; i++ {
           makeIntegrationRequest(t, handler, "GET", "/weather/seattle")
       }
       
       // Act: Check metrics
       w := makeIntegrationRequest(t, handler, "GET", "/metrics")
       body := w.Body.String()
       
       // Assert: Verify rate limit metrics
       if !strings.Contains(body, "rateLimitDeniedTotal") {
           t.Error("Metrics missing rateLimitDeniedTotal")
       }
       
       // Verify metric value increased (would need parsing)
       // This is a basic check; full metric validation is complex
   }
   ```

**Acceptance Criteria:**
- [ ] Rate limiting integration tests exist
- [ ] Rate limit enforcement test passes
- [ ] Concurrent rate limiting test passes
- [ ] Rate limit window test passes
- [ ] Rate limit metrics test validates metric recording
- [ ] Tests use realistic rate limits
- [ ] Tests are documented

---

## Test Infrastructure

### Shared Test Helpers

Create `internal/testhelpers/integration.go`:

```go
//go:build integration
// +build integration

package testhelpers

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"
    "time"
    
    "github.com/kjstillabower/weather-alert-service/internal/cache"
    "github.com/kjstillabower/weather-alert-service/internal/client"
    "github.com/kjstillabower/weather-alert-service/internal/observability"
    "github.com/kjstillabower/weather-alert-service/internal/service"
)

// IntegrationTestConfig holds configuration for integration tests
type IntegrationTestConfig struct {
    APIKey        string
    APIURL        string
    CacheBackend  string // "in_memory" or "memcached"
    MemcachedAddr string
}

// GetIntegrationConfig loads integration test configuration from environment
func GetIntegrationConfig(t *testing.T) IntegrationTestConfig {
    apiKey := os.Getenv("WEATHER_API_KEY")
    if apiKey == "" {
        t.Skip("WEATHER_API_KEY not set, skipping integration test")
    }
    
    return IntegrationTestConfig{
        APIKey:        apiKey,
        APIURL:        "https://api.openweathermap.org/data/2.5/weather",
        CacheBackend:  os.Getenv("INTEGRATION_CACHE_BACKEND"), // "memcached" or default to in-memory
        MemcachedAddr: os.Getenv("MEMCACHED_ADDRS"),
    }
}

// SetupIntegrationService creates a fully configured service for integration tests
func SetupIntegrationService(t *testing.T, cfg IntegrationTestConfig) (*service.WeatherService, cache.Cache, func()) {
    logger, err := observability.NewLogger()
    if err != nil {
        t.Fatalf("NewLogger() error = %v", err)
    }
    
    weatherClient, err := client.NewOpenWeatherClient(cfg.APIKey, cfg.APIURL, 5*time.Second)
    if err != nil {
        t.Fatalf("NewOpenWeatherClient() error = %v", err)
    }
    
    var cacheSvc cache.Cache
    var cleanup func()
    
    if cfg.CacheBackend == "memcached" && cfg.MemcachedAddr != "" {
        memcachedCache, err := cache.NewMemcachedCache(cfg.MemcachedAddr, 500*time.Millisecond, 2)
        if err == nil {
            cacheSvc = memcachedCache
            cleanup = func() { memcachedCache.Close() }
        } else {
            t.Logf("Memcached not available, using in-memory: %v", err)
            cacheSvc = cache.NewInMemoryCache()
            cleanup = func() {}
        }
    } else {
        cacheSvc = cache.NewInMemoryCache()
        cleanup = func() {}
    }
    
    weatherService := service.NewWeatherService(weatherClient, cacheSvc, 5*time.Minute)
    
    return weatherService, cacheSvc, cleanup
}
```

---

## Documentation

### Create `docs/integration-testing.md`

```markdown
# Integration Testing Guide

## Overview

Integration tests validate system behavior with real dependencies (external API, Memcached). These tests complement unit tests by verifying component integration.

## Running Integration Tests

### Prerequisites

- `WEATHER_API_KEY` environment variable set (valid OpenWeatherMap API key)
- Optional: Memcached running on `localhost:11211` (or set `MEMCACHED_ADDRS`)

### Run All Integration Tests

```bash
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

## Test Organization

- **End-to-End Tests:** `internal/http/integration_test.go`
- **Degraded State Tests:** `internal/degraded/integration_test.go`
- **Rate Limiting Tests:** `internal/http/integration_test.go` (rate limiting section)

## Test Patterns

- Use `//go:build integration` build tags
- Skip tests if dependencies unavailable
- Test against real external services
- Validate API key format before running

## Writing Integration Tests

See existing integration tests for patterns:
- `internal/client/client_integration_test.go`
- `internal/cache/memcached_integration_test.go`
```

### Update `README.md`

Add section:
```markdown
## Integration Tests

Integration tests require external dependencies (API key, optional Memcached).

Run integration tests:
```bash
export WEATHER_API_KEY=your_api_key
go test -tags=integration ./...
```

See `docs/integration-testing.md` for details.
```

---

## Validation

**Validation Steps:**

1. **Run All Integration Tests**
   ```bash
   go test -tags=integration ./...
   ```

2. **Verify Tests Skip Gracefully**
   - Run without `WEATHER_API_KEY` (should skip)
   - Run without Memcached (should use in-memory cache)

3. **Verify Test Coverage**
   - End-to-end request flow tested
   - Degraded state recovery tested
   - Rate limiting tested

4. **Check Test Documentation**
   - All tests have function-level documentation
   - Test purpose is clear
   - Dependencies documented

---

## Dependencies

**Required:**
- `WEATHER_API_KEY` environment variable (valid OpenWeatherMap API key)

**Optional:**
- Memcached running (for cache integration tests)
- `MEMCACHED_ADDRS` environment variable (defaults to `localhost:11211`)

**Test Infrastructure:**
- Go testing framework
- `httptest` for HTTP testing
- `testcontainers-go` (optional, for containerized Memcached)

---

## Risks and Mitigations

**Risk:** Integration tests may be flaky due to external dependencies  
**Mitigation:** Skip gracefully if dependencies unavailable, use timeouts, retry logic in tests

**Risk:** Integration tests may be slow  
**Mitigation:** Use appropriate timeouts, run in separate CI job, don't block unit tests

**Risk:** API key may be rate-limited  
**Mitigation:** Use test API key, limit test frequency, cache results where possible

**Risk:** Tests may require complex setup  
**Mitigation:** Provide clear documentation, test helpers, skip gracefully

**Risk:** Degraded state tests may be complex  
**Mitigation:** Start simple, use mocks where appropriate, defer complex scenarios

---

## Success Criteria

Enhanced integration tests implementation is complete when:
- ✅ End-to-end integration tests exist and pass
- ✅ Degraded state recovery integration tests exist and pass
- ✅ Rate limiting integration tests exist and pass
- ✅ Test helpers and infrastructure are in place
- ✅ Documentation exists for running and writing integration tests
- ✅ Tests skip gracefully if dependencies unavailable
- ✅ All tests are documented with purpose and behavior

---

## Future Enhancements

Potential future improvements:
- Use `testcontainers-go` for containerized Memcached in tests
- Add integration tests for graceful shutdown
- Add integration tests for metrics endpoint with real data
- Performance integration tests (separate from benchmarks)
- Multi-service integration tests (if scope expands)

---

## References

- Existing Integration Tests: `internal/client/client_integration_test.go`, `internal/cache/memcached_integration_test.go`
- Testing Standards: `.cursor/rules/040-testing.mdc`
- Go Testing: https://pkg.go.dev/testing
