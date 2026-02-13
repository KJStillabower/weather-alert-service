# Performance Benchmarks

## Overview

Performance benchmarks establish baseline metrics for critical code paths and enable regression detection. Benchmarks measure operation overhead (nanoseconds per operation, memory allocations) rather than end-to-end latency, which includes network and external dependencies.

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
- `-benchtime=5s` - Run each benchmark for 5 seconds (default: 1s)
- `-cpu=1,2,4` - Run benchmarks with different CPU counts
- `-count=3` - Run each benchmark 3 times for consistency

### Example Output

```
BenchmarkInMemoryCache_Get_Hit-8         50000000    25.3 ns/op     0 B/op    0 allocs/op
BenchmarkInMemoryCache_Get_Miss-8       50000000    18.2 ns/op     0 B/op    0 allocs/op
BenchmarkInMemoryCache_Set-8            20000000    89.5 ns/op    64 B/op    1 allocs/op
```

## Benchmark Results

### Cache Performance

**In-Memory Cache:**
- Get (hit): ~25-50 ns/op, 0 B/op (map lookup)
- Get (miss): ~15-30 ns/op, 0 B/op (map lookup, no entry)
- Set: ~80-120 ns/op, ~64-128 B/op (map insertion, struct allocation)
- Concurrent Get: ~30-60 ns/op (with mutex contention)
- Memory per entry: ~200-300 bytes (including map overhead)

**Memcached:**
- Get (hit): ~500-2000 μs/op (network overhead dominates)
- Get (miss): ~500-2000 μs/op (network overhead)
- Set: ~500-2000 μs/op (network overhead)

**Note:** Memcached benchmarks require a running Memcached instance. Benchmarks skip automatically if Memcached is unavailable.

### API Client Performance

- Request building: ~500-1000 ns/op (URL parsing, query encoding)
- Response parsing: ~2000-5000 ns/op (JSON unmarshaling)
- Response mapping: ~100-300 ns/op (struct field assignment)
- Error handling: ~50-150 ns/op (status code switch)
- Retry decision: ~10-50 ns/op (error type checking)
- Backoff calculation: ~50-200 ns/op (math operations, jitter)
- Status label: ~5-20 ns/op (status code comparison)

### Handler Performance

- GetWeather (cache hit): ~5-15 μs/op (handler overhead + cache lookup)
- GetWeather (cache miss): ~10-30 μs/op (handler overhead + service call + mock client)
- GetWeather (error): ~5-15 μs/op (error path handling)
- GetWeather (validation error): ~2-5 μs/op (early validation return)
- GetWeather (rate limited): ~5-15 μs/op (rate limiter check)
- GetHealth: ~10-50 μs/op (health status computation)

**Note:** Handler benchmarks use mocks and do not include network latency or real upstream API calls.

## Interpreting Results

### Metrics Explained

- **ns/op:** Nanoseconds per operation (lower is better)
- **μs/op:** Microseconds per operation (1 μs = 1000 ns)
- **B/op:** Bytes allocated per operation (lower is better)
- **allocs/op:** Number of allocations per operation (lower is better)

### Performance Expectations

**Excellent (< 100 ns):**
- Simple map lookups
- Basic arithmetic operations
- Status code comparisons

**Good (100 ns - 1 μs):**
- Struct field assignments
- Error handling
- Simple transformations

**Acceptable (1 μs - 10 μs):**
- JSON parsing (small payloads)
- Handler overhead
- Cache operations

**Slow (> 10 μs):**
- Network operations (expected)
- Complex JSON parsing (large payloads)
- External service calls

### Regression Detection

Compare benchmark results over time to detect performance regressions:

```bash
# Run benchmarks and save results
go test -bench=. -benchmem ./... > baseline.txt

# Later, compare against baseline
go test -bench=. -benchmem ./... > current.txt
diff baseline.txt current.txt

# Or use benchcmp (if installed)
go get golang.org/x/tools/cmd/benchcmp
benchcmp baseline.txt current.txt
```

**Red Flags:**
- Operations that were < 100 ns now > 500 ns
- Memory allocations increased significantly
- Operations that were fast now slow

## Optimization Guidance

Use benchmarks to identify optimization opportunities:

### High Allocation Counts

If benchmarks show high `allocs/op`:
- Consider object pooling for frequently allocated objects
- Reuse buffers and slices
- Avoid unnecessary string conversions

### Slow Operations

If benchmarks show slow operations:
- Profile with `go test -cpuprofile=cpu.prof`
- Use `go tool pprof` to identify hotspots
- Consider caching or memoization

### Cache Backend Comparison

Compare in-memory vs Memcached benchmarks:
- In-memory: Faster but limited to single instance
- Memcached: Slower but shared across instances
- Choose based on deployment architecture

## Benchmark Files

- `internal/cache/cache_bench_test.go` - Cache operation benchmarks
- `internal/client/client_bench_test.go` - API client overhead benchmarks
- `internal/http/handlers_bench_test.go` - Handler performance benchmarks

## Limitations

**What Benchmarks Don't Measure:**
- End-to-end request latency (includes network, external APIs)
- Real-world traffic patterns
- Concurrent request handling at scale
- Memory pressure under load

**What Benchmarks Do Measure:**
- Operation overhead (CPU time per operation)
- Memory allocation patterns
- Code path efficiency
- Relative performance between implementations

## Best Practices

1. **Run benchmarks multiple times** - Results can vary; run 3-5 times and compare
2. **Use consistent environment** - Run on same machine/CPU for consistent results
3. **Warm up before benchmarking** - First run may be slower due to JIT compilation
4. **Use realistic test data** - Match production data sizes and patterns
5. **Document baseline results** - Record initial results for regression detection
6. **Review during code changes** - Run benchmarks when modifying performance-critical code

## CI Integration (Future)

Future enhancement: Automate benchmark runs in CI to detect performance regressions automatically. See `docs/plans/performance-benchmarks-plan.md` for details.

## References

- Go Benchmarking: https://pkg.go.dev/testing#hdr-Benchmarks
- Performance Best Practices: https://go.dev/doc/diagnostics
- Profiling Guide: https://go.dev/doc/diagnostics#profiling
