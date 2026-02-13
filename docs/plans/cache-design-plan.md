# Cache Layer Design Plan

## Goal

Add a configurable cache layer that supports shared, context-managed backends. Memcached provides a simple distributed cache suitable for our complexity. The design fits within the existing lifecycle scope as a reliability pattern: reduce upstream load, improve latency, and fail gracefully when cache is unavailable.

## Scope

- **In scope:** Memcached backend implementing `cache.Cache`; config-driven backend selection (in-memory vs memcached); connection lifecycle tied to process context; configurable tuning via YAML.
- **Out of scope:** Redis, persistence, pub/sub, cache clustering, or cache-as-primary-storage semantics. Given the constraints of this exercise and relative simplicity, in-memory presents a reasonable default for this exercise, although redis may be a more common real-world solution.

## Configuration

Extend `config/[env].yaml` under `cache`:

```yaml
cache:
  backend: "in_memory"      # in_memory | memcached
  ttl: "5m"                 # Entry TTL; applies to both backends

  # Memcached (ignored when backend=in_memory)
  memcached:
    addrs: "localhost:11211"   # Single addr or comma-separated list
    timeout: "500ms"          # Dial/operation timeout
    max_idle_conns: 2         # Idle connections per addr
```

### Config Values

| Key | Default | Notes |
|-----|---------|-------|
| `cache.backend` | `in_memory` | `in_memory` or `memcached` |
| `cache.ttl` | `5m` | Entry TTL; existing behavior |
| `cache.memcached.addrs` | `localhost:11211` | Comma-separated for multiple servers |
| `cache.memcached.timeout` | `500ms` | Per-operation timeout; fail fast on unreachable |
| `cache.memcached.max_idle_conns` | `2` | Connection pool idle size per addr |

Env override: `CACHE_BACKEND`, `MEMCACHED_ADDRS` (if needed for 12-factor).

## Lifecycle Integration

### Health Check

- **`checks.cache`:** `healthy` when backend is reachable (or backend is in-memory); `unhealthy` when memcached is configured but unreachable.
- **Impact on status:** Cache unhealthy does not force `degraded`. Cache is a latency optimization; on failure we fall through to upstream. Health reports cache state for observability; routing decisions remain driven by overload/degraded logic.

### Shutdown

- Cache connection closed during graceful shutdown, after in-flight requests drain.
- In-memory cache requires no teardown. Memcached pool closes connections in shutdown handler.

## Implementation Outline

### 1. Existing Interface

`internal/cache/cache.go` already defines:

```go
type Cache interface {
    Get(ctx context.Context, key string) (models.WeatherData, bool, error)
    Set(ctx context.Context, key string, value models.WeatherData, ttl time.Duration) error
}
```

No service-layer changes required. New backend implements the same interface.

### 2. MemcachedCache

- **Package:** `internal/cache` or `internal/cache/memcached`
- **Behavior:** Marshal `WeatherData` to JSON (or binary) for storage; unmarshal on Get. Use `context.Context` for request-scoped cancellation and timeout.
- **Error handling:** Get/Set errors → return `false`/error; caller treats as cache miss and fetches from upstream. Cache failure is non-fatal.
- **Connection:** Use a client library (e.g. `github.com/bradfitz/gomemcache`) with connection pooling. Create pool at startup; close on shutdown.

### 3. Config Wiring

- Add `CacheBackend`, `MemcachedAddrs`, `MemcachedTimeout`, `MemcachedMaxIdleConns` to `internal/config/config.go`.
- In `cmd/service/main.go`: branch on `cfg.CacheBackend`; instantiate `InMemoryCache` or `MemcachedCache`; pass to `WeatherService`.

### 4. Key Format

- Current key: `normalizeLocation(location)` (lowercase, trimmed). Keep for compatibility.
- Optionally prefix: `weather:` to namespace keys if memcached is shared with other services. Config-driven prefix is acceptable if needed later.

### 5. Update Existing Testing Pattern tooling


## Reliability Patterns

| Pattern | Behavior |
|---------|----------|
| Cache miss | Fetch from upstream; populate cache on success. Non-fatal cache write. |
| Cache error | Treat as miss; proceed to upstream. No request failure. |
| Cache down | All requests bypass cache; upstream sees full load. Service remains operational. |
| Shutdown | Stop accepting new cache ops; drain in-flight; close pool. |

## Checklist

- [x] Extend `config` with cache backend and memcached tuning
- [x] Implement `MemcachedCache` satisfying `cache.Cache`
- [x] Wire backend selection in `main.go`
- [x] Add `checks.cache` to `/health` when memcached is configured
- [x] Close memcached pool in shutdown handler
- [x] Document `cache.backend` and memcached options in env YAML examples
