# Performance Benchmarks Implementation Plan

**Status:** Completed (Tasks 1-3, Documentation)  
**Priority:** Low-Medium  
**Focus:** Establishing performance baselines and detecting regressions  
**Completed:** 2026-02-12

## Overview

This plan establishes performance benchmarks for critical code paths to enable performance regression detection and optimization guidance. Benchmarks will cover cache operations, API client overhead, and end-to-end request handling.

## Objectives

1. Establish baseline performance metrics for cache operations
2. Benchmark API client overhead (request building, parsing, retry logic)
3. Create end-to-end request handling benchmarks
4. Enable performance regression detection
5. Provide data for optimization decisions

## Scope

**In Scope:**
- Cache performance benchmarks (in-memory and Memcached)
- API client performance benchmarks
- End-to-end request handler benchmarks
- Benchmark documentation and usage guide

**Out of Scope (Deferred):**
- CI integration for automated benchmark runs (Task 2.4 - see Future Enhancements)
- Performance profiling and flame graphs (separate initiative)
- Load testing (separate initiative)
- Performance monitoring dashboards (observability covers this)

## Implementation Tasks

### Task 1: Cache Performance Benchmarks

**Deliverable:** `internal/cache/cache_bench_test.go`

**Purpose:** Establish baseline performance for cache operations to detect regressions and compare backend implementations.

**Steps:**

1. **Create Benchmark File Structure**
   - Create `internal/cache/cache_bench_test.go`
   - Set up benchmark helper functions
   - Create test data fixtures

2. **Implement In-Memory Cache Benchmarks**
   ```go
   // BenchmarkInMemoryCache_Get_Hit benchmarks cache Get operation on cache hit
   func BenchmarkInMemoryCache_Get_Hit(b *testing.B) {
       cache := NewInMemoryCache()
       ctx := context.Background()
       testData := createTestWeatherData("seattle")
       
       // Pre-populate cache
       cache.Set(ctx, "seattle", testData, 5*time.Minute)
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           _, _, _ = cache.Get(ctx, "seattle")
       }
   }
   
   // BenchmarkInMemoryCache_Get_Miss benchmarks cache Get operation on cache miss
   func BenchmarkInMemoryCache_Get_Miss(b *testing.B) {
       cache := NewInMemoryCache()
       ctx := context.Background()
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           _, _, _ = cache.Get(ctx, "nonexistent")
       }
   }
   
   // BenchmarkInMemoryCache_Set benchmarks cache Set operation
   func BenchmarkInMemoryCache_Set(b *testing.B) {
       cache := NewInMemoryCache()
       ctx := context.Background()
       testData := createTestWeatherData("seattle")
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           _ = cache.Set(ctx, "seattle", testData, 5*time.Minute)
       }
   }
   
   // BenchmarkInMemoryCache_Concurrent benchmarks concurrent cache operations
   func BenchmarkInMemoryCache_Concurrent(b *testing.B) {
       cache := NewInMemoryCache()
       ctx := context.Background()
       testData := createTestWeatherData("seattle")
       cache.Set(ctx, "seattle", testData, 5*time.Minute)
       
       b.RunParallel(func(pb *testing.PB) {
           for pb.Next() {
               _, _, _ = cache.Get(ctx, "seattle")
           }
       })
   }
   ```

3. **Implement Memcached Benchmarks** (if Memcached available)
   ```go
   // BenchmarkMemcachedCache_Get_Hit benchmarks Memcached Get on cache hit
   // Requires: Memcached running (skip if unavailable)
   func BenchmarkMemcachedCache_Get_Hit(b *testing.B) {
       if testing.Short() {
           b.Skip("Skipping Memcached benchmark in short mode")
       }
       
       cache, err := NewMemcachedCache("localhost:11211", 500*time.Millisecond, 2)
       if err != nil {
           b.Skipf("Memcached not available: %v", err)
       }
       defer cache.Close()
       
       ctx := context.Background()
       testData := createTestWeatherData("seattle")
       cache.Set(ctx, "seattle", testData, 5*time.Minute)
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           _, _, _ = cache.Get(ctx, "seattle")
       }
   }
   ```

4. **Add Memory Usage Benchmarks**
   ```go
   // BenchmarkInMemoryCache_MemoryPerEntry estimates memory usage per cache entry
   func BenchmarkInMemoryCache_MemoryPerEntry(b *testing.B) {
       cache := NewInMemoryCache()
       ctx := context.Background()
       testData := createTestWeatherData("seattle")
       
       var m1, m2 runtime.MemStats
       runtime.GC()
       runtime.ReadMemStats(&m1)
       
       for i := 0; i < b.N; i++ {
           cache.Set(ctx, fmt.Sprintf("key%d", i), testData, 5*time.Minute)
       }
       
       runtime.GC()
       runtime.ReadMemStats(&m2)
       
       bytesPerEntry := float64(m2.Alloc-m1.Alloc) / float64(b.N)
       b.ReportMetric(bytesPerEntry, "bytes/entry")
   }
   ```

5. **Create Benchmark Helper Functions**
   ```go
   // createTestWeatherData creates test weather data for benchmarks
   func createTestWeatherData(location string) models.WeatherData {
       return models.WeatherData{
           Location:    location,
           Temperature: 15.5,
           Conditions:  "Clear",
           Humidity:    65,
           WindSpeed:   10.2,
           Timestamp:   time.Now(),
       }
   }
   ```

**Acceptance Criteria:**
- [x] `cache_bench_test.go` exists with all benchmark functions
- [x] Benchmarks cover Get (hit/miss), Set, and concurrent operations
- [x] Both in-memory and Memcached benchmarks implemented
- [x] Memory usage benchmark included
- [x] Benchmarks run successfully (`go test -bench=. ./internal/cache`)
- [x] Benchmark results are reasonable and documented

---

### Task 2: API Client Performance Benchmarks

**Deliverable:** `internal/client/client_bench_test.go`

**Purpose:** Measure overhead of API client operations (request building, response parsing, error handling) to identify optimization opportunities.

**Steps:**

1. **Create Benchmark File Structure**
   - Create `internal/client/client_bench_test.go`
   - Set up mock HTTP responses
   - Create benchmark helper functions

2. **Implement Request Building Benchmarks**
   ```go
   // BenchmarkClient_BuildRequest benchmarks HTTP request construction
   func BenchmarkClient_BuildRequest(b *testing.B) {
       client, _ := NewOpenWeatherClient("test-api-key", "https://api.openweathermap.org/data/2.5/weather", 2*time.Second)
       ctx := context.Background()
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           _, _ = client.buildRequest(ctx, "seattle")
       }
   }
   ```

3. **Implement Response Parsing Benchmarks**
   ```go
   // BenchmarkClient_ParseResponse benchmarks JSON response parsing
   func BenchmarkClient_ParseResponse(b *testing.B) {
       // Sample OpenWeatherMap API response
       responseJSON := `{
           "main": {"temp": 15.5, "humidity": 65},
           "weather": [{"main": "Clear", "description": "clear sky"}],
           "wind": {"speed": 10.2},
           "name": "Seattle"
       }`
       
       var apiResp openWeatherResponse
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           _ = json.Unmarshal([]byte(responseJSON), &apiResp)
       }
   }
   
   // BenchmarkClient_MapResponse benchmarks response mapping to domain model
   func BenchmarkClient_MapResponse(b *testing.B) {
       client, _ := NewOpenWeatherClient("key", "url", time.Second)
       apiResp := openWeatherResponse{
           Main: struct {
               Temp     float64 `json:"temp"`
               Humidity int     `json:"humidity"`
           }{Temp: 15.5, Humidity: 65},
           Weather: []struct {
               Main        string `json:"main"`
               Description string `json:"description"`
           }{{Main: "Clear", Description: "clear sky"}},
           Wind: struct {
               Speed float64 `json:"speed"`
           }{Speed: 10.2},
           Name: "Seattle",
       }
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           _ = client.mapResponse(apiResp, "seattle")
       }
   }
   ```

4. **Implement Error Handling Benchmarks**
   ```go
   // BenchmarkClient_HandleErrorResponse benchmarks error response handling
   func BenchmarkClient_HandleErrorResponse(b *testing.B) {
       client, _ := NewOpenWeatherClient("key", "url", time.Second)
       
       // Create mock HTTP response with 503 status
       resp := &http.Response{
           StatusCode: http.StatusServiceUnavailable,
           Body:       io.NopCloser(strings.NewReader("")),
       }
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           resp.Body = io.NopCloser(strings.NewReader(""))
           _ = client.handleErrorResponse(resp)
       }
   }
   ```

5. **Implement Retry Logic Benchmarks**
   ```go
   // BenchmarkClient_IsRetryable benchmarks retry decision logic
   func BenchmarkClient_IsRetryable(b *testing.B) {
       client, _ := NewOpenWeatherClient("key", "url", time.Second)
       
       testErrors := []error{
           ErrRateLimited,
           ErrUpstreamFailure,
           fmt.Errorf("timeout: context deadline exceeded"),
           fmt.Errorf("invalid request"),
       }
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           err := testErrors[i%len(testErrors)]
           _ = client.isRetryable(err)
       }
   }
   
   // BenchmarkClient_CalculateBackoff benchmarks backoff calculation
   func BenchmarkClient_CalculateBackoff(b *testing.B) {
       client, _ := NewOpenWeatherClient("key", "url", time.Second)
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           attempt := (i % 5) + 1
           _ = client.calculateBackoff(attempt)
       }
   }
   ```

6. **Add Status Label Benchmark**
   ```go
   // BenchmarkStatusLabel benchmarks HTTP status code to label conversion
   func BenchmarkStatusLabel(b *testing.B) {
       statusCodes := []int{200, 400, 429, 500, 503}
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           code := statusCodes[i%len(statusCodes)]
           _ = statusLabel(code)
       }
   }
   ```

**Acceptance Criteria:**
- [x] `client_bench_test.go` exists with all benchmark functions
- [x] Benchmarks cover request building, response parsing, error handling, retry logic
- [x] Benchmarks use realistic test data
- [x] Benchmarks run successfully (`go test -bench=. ./internal/client`)
- [x] Benchmark results are reasonable and documented

---

### Task 3: End-to-End Request Benchmarks

**Deliverable:** `internal/http/handlers_bench_test.go`

**Purpose:** Measure full request handling performance including handler overhead, service layer, and error paths.

**Steps:**

1. **Create Benchmark File Structure**
   - Create `internal/http/handlers_bench_test.go`
   - Set up mock dependencies (service, client, logger)
   - Create HTTP request helpers

2. **Implement Cache Hit Path Benchmark**
   ```go
   // BenchmarkHandler_GetWeather_CacheHit benchmarks handler with cache hit
   func BenchmarkHandler_GetWeather_CacheHit(b *testing.B) {
       // Setup mocks
       mockService := &mockWeatherService{
           weather: models.WeatherData{Location: "seattle", Temperature: 15.5},
           err:     nil,
       }
       mockClient := &mockWeatherClient{}
       logger, _ := observability.NewLogger()
       handler := NewHandler(mockService, mockClient, nil, logger, nil)
       
       router := mux.NewRouter()
       router.HandleFunc("/weather/{location}", handler.GetWeather)
       
       req := httptest.NewRequest("GET", "/weather/seattle", nil)
       req = req.WithContext(context.WithValue(req.Context(), "correlation_id", "test-id"))
       req = req.WithContext(context.WithValue(req.Context(), "logger", logger))
       
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           w := httptest.NewRecorder()
           router.ServeHTTP(w, req)
       }
   }
   ```

3. **Implement Cache Miss Path Benchmark**
   ```go
   // BenchmarkHandler_GetWeather_CacheMiss benchmarks handler with cache miss
   func BenchmarkHandler_GetWeather_CacheMiss(b *testing.B) {
       // Setup mocks that simulate cache miss → API call
       mockService := &mockWeatherService{
           weather: models.WeatherData{Location: "seattle", Temperature: 15.5},
           err:     nil,
           cacheMiss: true, // Simulate cache miss
       }
       // ... similar setup
   }
   ```

4. **Implement Error Path Benchmarks**
   ```go
   // BenchmarkHandler_GetWeather_Error benchmarks handler error handling
   func BenchmarkHandler_GetWeather_Error(b *testing.B) {
       mockService := &mockWeatherService{
           err: fmt.Errorf("upstream failure"),
       }
       // ... setup and benchmark error path
   }
   
   // BenchmarkHandler_GetWeather_ValidationError benchmarks validation error handling
   func BenchmarkHandler_GetWeather_ValidationError(b *testing.B) {
       // Test empty location, invalid characters, etc.
   }
   ```

5. **Implement Rate Limiting Benchmark**
   ```go
   // BenchmarkHandler_GetWeather_RateLimited benchmarks rate limiting overhead
   func BenchmarkHandler_GetWeather_RateLimited(b *testing.B) {
       limiter := rate.NewLimiter(rate.Limit(100), 250)
       // Setup handler with rate limiter
       // Benchmark requests that hit rate limit
   }
   ```

6. **Implement Health Endpoint Benchmark**
   ```go
   // BenchmarkHandler_GetHealth benchmarks health check endpoint
   func BenchmarkHandler_GetHealth(b *testing.B) {
       // Setup handler with health config
       // Benchmark health check performance
   }
   ```

7. **Create Benchmark Helper Functions**
   ```go
   // setupBenchmarkHandler creates a handler with mocks for benchmarking
   func setupBenchmarkHandler() *Handler {
       mockService := &mockWeatherService{}
       mockClient := &mockWeatherClient{}
       logger, _ := observability.NewLogger()
       return NewHandler(mockService, mockClient, nil, logger, nil)
   }
   
   // createBenchmarkRequest creates an HTTP request for benchmarking
   func createBenchmarkRequest(method, path string) *http.Request {
       req := httptest.NewRequest(method, path, nil)
       logger, _ := observability.NewLogger()
       req = req.WithContext(context.WithValue(req.Context(), "correlation_id", "bench-id"))
       req = req.WithContext(context.WithValue(req.Context(), "logger", logger))
       return req
   }
   ```

**Acceptance Criteria:**
- [x] `handlers_bench_test.go` exists with all benchmark functions
- [x] Benchmarks cover cache hit, cache miss, error paths, rate limiting
- [x] Benchmarks use proper mocks (no real network calls)
- [x] Benchmarks run successfully (`go test -bench=. ./internal/http`)
- [x] Benchmark results are reasonable and documented

---

## Documentation

### Create `docs/performance-benchmarks.md`

**Content:**

```markdown
# Performance Benchmarks

## Overview

Performance benchmarks establish baseline metrics for critical code paths and enable regression detection.

## Running Benchmarks

### All Benchmarks
```bash
go test -bench=. -benchmem ./...
```

### Specific Package
```bash
go test -bench=. -benchmem ./internal/cache
go test -bench=. -benchmem ./internal/client
go test -bench=. -benchmem ./internal/http
```

### Benchmark Options
- `-bench=.` - Run all benchmarks
- `-benchmem` - Report memory allocations
- `-benchtime=5s` - Run each benchmark for 5 seconds
- `-cpu=1,2,4` - Run benchmarks with different CPU counts

## Benchmark Results

### Cache Performance

**In-Memory Cache:**
- Get (hit): ~X ns/op, Y B/op
- Get (miss): ~X ns/op, Y B/op
- Set: ~X ns/op, Y B/op
- Memory per entry: ~X bytes

**Memcached:**
- Get (hit): ~X ns/op (network overhead)
- Set: ~X ns/op (network overhead)

### API Client Performance

- Request building: ~X ns/op
- Response parsing: ~X ns/op
- Error handling: ~X ns/op
- Retry decision: ~X ns/op
- Backoff calculation: ~X ns/op

### Handler Performance

- GetWeather (cache hit): ~X ns/op
- GetWeather (cache miss): ~X ns/op
- GetWeather (error): ~X ns/op
- GetHealth: ~X ns/op

## Interpreting Results

- **ns/op:** Nanoseconds per operation (lower is better)
- **B/op:** Bytes allocated per operation (lower is better)
- **allocs/op:** Number of allocations per operation (lower is better)

## Regression Detection

Compare benchmark results over time to detect performance regressions:

```bash
# Run benchmarks and save results
go test -bench=. -benchmem ./... > baseline.txt

# Later, compare against baseline
go test -bench=. -benchmem ./... > current.txt
diff baseline.txt current.txt
```

## Optimization Guidance

Use benchmarks to identify optimization opportunities:
- High allocation counts → consider object pooling
- Slow operations → profile with `go test -cpuprofile`
- Compare cache backends → choose based on performance needs
```

### Update `README.md`

Add section:
```markdown
## Performance Benchmarks

See `docs/performance-benchmarks.md` for benchmark documentation.

Run benchmarks:
```bash
go test -bench=. -benchmem ./...
```
```

---

## Validation

**Validation Steps:**

1. **Run All Benchmarks**
   ```bash
   go test -bench=. -benchmem ./internal/cache ./internal/client ./internal/http
   ```

2. **Verify Results Are Reasonable**
   - Cache Get (hit) should be < 100ns for in-memory
   - Handler benchmarks should be < 10μs for cache hits
   - No unexpected allocations

3. **Document Baseline Results**
   - Record initial benchmark results
   - Document in `docs/performance-benchmarks.md`
   - Establish baseline for regression detection

4. **Test Benchmark Reliability**
   - Run benchmarks multiple times
   - Verify consistent results
   - Check for flakiness

---

## Dependencies

- Go benchmarking support (built-in)
- Mock implementations for external dependencies
- Test data fixtures
- Memcached (optional, for Memcached benchmarks)

---

## Risks and Mitigations

**Risk:** Benchmarks may be flaky or inconsistent  
**Mitigation:** Use `-benchtime` to run longer, run multiple times, document variance

**Risk:** Benchmarks may not reflect real-world performance  
**Mitigation:** Use realistic test data, note that benchmarks measure overhead, not end-to-end latency

**Risk:** Benchmark maintenance overhead  
**Mitigation:** Keep benchmarks simple, document purpose, review during code changes

**Risk:** Memcached benchmarks require external dependency  
**Mitigation:** Skip Memcached benchmarks if unavailable, use `testing.Short()` flag

---

## Success Criteria

Performance benchmarks implementation is complete when:
- ✅ Cache benchmarks exist and run successfully
- ✅ API client benchmarks exist and run successfully
- ✅ Handler benchmarks exist and run successfully
- ✅ Benchmark documentation exists
- ✅ Baseline results are documented
- ✅ Benchmarks can be used for regression detection

**Status:** All success criteria met. Implementation completed on 2026-02-12.

## Implementation Summary

**Completed Deliverables:**

1. **`internal/cache/cache_bench_test.go`** - Cache performance benchmarks
   - In-memory cache: Get (hit/miss), Set, Concurrent operations
   - Memcached cache: Get (hit/miss), Set (with skip if unavailable)
   - Memory usage benchmark (bytes per entry)
   - All benchmarks compile and run successfully

2. **`internal/client/client_bench_test.go`** - API client performance benchmarks
   - Request building (URL parsing, query encoding)
   - Response parsing (JSON unmarshaling)
   - Response mapping (struct transformation)
   - Error handling (status code mapping)
   - Retry logic (isRetryable, calculateBackoff)
   - Status label conversion
   - All benchmarks compile and run successfully

3. **`internal/http/handlers_bench_test.go`** - End-to-end handler benchmarks
   - GetWeather (cache hit path)
   - GetWeather (cache miss path)
   - GetWeather (error handling)
   - GetWeather (validation error)
   - GetWeather (rate limiting overhead)
   - GetHealth (health check endpoint)
   - All benchmarks use proper mocks (no network calls)
   - All benchmarks compile and run successfully

4. **`docs/performance-benchmarks.md`** - Comprehensive benchmark documentation
   - Running benchmarks guide
   - Benchmark results interpretation
   - Regression detection process
   - Optimization guidance
   - Best practices

5. **`README.md`** - Updated with benchmark section
   - Quick start commands
   - Link to detailed documentation

**Key Features Implemented:**
- Complete benchmark coverage for cache, client, and handler layers
- Realistic test data and mock implementations
- Memory allocation tracking (`-benchmem`)
- Concurrent operation benchmarks
- Automatic skipping of Memcached benchmarks when unavailable
- Comprehensive documentation with interpretation guide

---

## Future Enhancements (Deferred)

### Task 2.4: CI Integration for Performance Regression

**Status:** Deferred (out of scope for initial implementation)

**Purpose:** Automate benchmark runs in CI and detect performance regressions automatically.

**Implementation Approach:**
- Create `.github/workflows/benchmarks.yml`
- Run benchmarks on each PR
- Compare against baseline
- Fail CI on significant regressions (> X% slower)
- Store benchmark results as artifacts

**Why Deferred:**
- Requires CI infrastructure setup
- Needs baseline storage mechanism
- May require benchmark result analysis tooling
- Can be added after initial benchmarks are established

**When to Implement:**
- After baseline benchmarks are established
- When performance regression detection becomes critical
- When CI infrastructure supports artifact storage

**Estimated Complexity:** Medium
- Requires CI workflow configuration
- Needs baseline comparison logic
- May need result storage (artifacts, database)

---

## References

- Go Benchmarking: https://pkg.go.dev/testing#hdr-Benchmarks
- Benchmark Best Practices: https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go
- Performance Profiling: https://go.dev/doc/diagnostics#profiling
