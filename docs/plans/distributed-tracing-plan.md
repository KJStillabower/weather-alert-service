# Distributed Tracing Plan

## Goal

Add distributed tracing using OpenTelemetry so that requests can be traced across the weather service and its outbound calls (OpenWeatherMap API). Enables debugging slow requests, understanding where latency is spent (cache hit vs. API call), and correlating logs/metrics with trace IDs in a multi-service or gateway scenario.

## Rationale

- **Latency attribution:** Determine whether slowness is in handler logic, cache lookup, or OpenWeatherMap API.
- **Request flow visibility:** Cache hit vs. miss, retries, upstream failures appear as span hierarchy.
- **Correlation:** Trace ID can replace or augment X-Correlation-ID for cross-service correlation; backend systems (Jaeger, Tempo, etc.) understand W3C TraceContext.
- **Production readiness:** OpenTelemetry is vendor-neutral; exporters support Jaeger, Zipkin, OTLP (Grafana Tempo, Honeycomb, Datadog, etc.).
- **Incremental:** Can be disabled via config; no impact when `tracing.enabled: false`.

## Technology: OpenTelemetry

- **API:** `go.opentelemetry.io/otel`
- **HTTP server (gorilla/mux):** `go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux`
- **HTTP client:** `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`
- **Exporters:** OTLP HTTP/gRPC (default for prod), stdout (dev), Jaeger (optional)
- **Propagation:** W3C TraceContext (extract from incoming, inject into outbound)

## Span Hierarchy

```
GET /weather/seattle (server span - otelmux)
├── weather.GetWeather (service span - manual)
│   ├── cache.Get (cache span - manual, optional)
│   └── openweathermap.api (HTTP client span - otelhttp)
│       └── [retries as child spans if any]
```

- **Server span:** Created by `otelmux.Middleware`; one per HTTP request.
- **Service span:** Manual span in `WeatherService.GetWeather`; optional but clarifies service-layer timing.
- **Cache span:** Manual span in cache layer if we want cache hit/miss visibility; low value for in-memory, higher for memcached.
- **HTTP client span:** Created by `otelhttp.Transport` wrapping the client's `http.Client`; automatically propagates context and records HTTP attributes.

## Correlation with X-Correlation-ID

- **Keep both:** Trace ID in W3C headers; X-Correlation-ID in response for clients that expect it. Map: use `trace.SpanFromContext(ctx).SpanContext().TraceID().String()` as correlation ID when none provided, or keep existing UUID for correlation ID and add trace ID to response headers (`X-Trace-ID`) when tracing enabled.
- **Prefer trace ID when tracing on:** If incoming request has TraceContext, extract it; trace ID becomes the primary identifier. Correlation ID can be set from trace ID for backward compatibility in logs/headers.

## Config

Add a new `tracing` section in `config/[env].yaml`:

```yaml
tracing:
  enabled: true
  service_name: "weather-alert-service"   # default from config or "weather-alert-service"
  exporter: "otlp"                         # otlp | stdout | none
  endpoint: "http://localhost:4318/v1/traces"  # OTLP HTTP endpoint; omit for stdout
  sample_ratio: 1.0                        # 0.0-1.0; 1.0 = all, 0.1 = 10%
```

- **`enabled`:** Master switch; `false` means no tracer, no spans, passthrough.
- **`exporter`:** `otlp` (OTLP HTTP), `stdout` (log spans to stderr for dev), `none` (no exporter; traces created but dropped).
- **`endpoint`:** Required when `exporter: otlp`. OTLP HTTP default port 4318; gRPC 4317.
- **`sample_ratio`:** Fraction of requests to trace. 1.0 = all; 0.1 = 10% sampled. Supports head-based sampling only (implementation simplicity); tail-based would require collector.

## Implementation Overview

1. **TracerProvider:** Create in main; configure with exporter and resource (service name, version).
2. **Propagation:** Set global propagator to `propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})`.
3. **Server:** Wrap router handler with `otelmux.Middleware(serviceName)` or apply as first middleware. Must run before other middleware so trace context is in request context.
4. **Client:** Wrap `http.Client.Transport` with `otelhttp.NewTransport(baseTransport)`. The OpenWeatherMap client needs to accept or construct a transport that includes otelhttp.
5. **Service/Cache spans:** Optional manual spans via `tracer.Start(ctx, "GetWeather")` in service layer; defer `span.End()`.
6. **Shutdown:** Call `TracerProvider.Shutdown(ctx)` during graceful shutdown to flush spans.

## Paths

| Path       | Traced |
|------------|--------|
| `/health`  | Yes (server span) |
| `/metrics` | Yes (server span) |
| `/weather/*` | Yes (full hierarchy) |
| `/test/*`  | Yes (server span; when testing_mode) |

All HTTP requests get a server span. Only `/weather` triggers service + client spans.

## Dependencies

```bash
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
go get go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
```

(Plus `otel/sdk`, `otel/trace`, `otel/propagation` as transitive.)

## References

- `internal/client/client.go` — HTTP client for OpenWeatherMap
- `internal/service/service.go` — Service layer, cache + client calls
- `internal/http/middleware.go` — CorrelationID, Metrics
- `cmd/service/main.go` — Router setup, shutdown
- [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/)
- [otelmux](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux)
- [otelhttp](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp)

---

## Implementation Plan: OpenTelemetry

Step-by-step implementation using OpenTelemetry Go SDK and contrib instrumentation.

### 1. Config

**File:** `internal/config/config.go`

- Add `TracingEnabled bool`, `TracingServiceName string`, `TracingExporter string`, `TracingEndpoint string`, `TracingSampleRatio float64` to `Config`.
- Add `Tracing struct` to `fileConfig`:
  ```yaml
  Tracing struct {
      Enabled     *bool   `yaml:"enabled"`
      ServiceName string  `yaml:"service_name"`
      Exporter    string  `yaml:"exporter"`
      Endpoint    string  `yaml:"endpoint"`
      SampleRatio float64 `yaml:"sample_ratio"`
  } `yaml:"tracing"`
  ```
- In `Load()`: populate `cfg.TracingEnabled`, etc.; default `enabled: false`, `service_name: "weather-alert-service"`, `exporter: "stdout"` for dev, `sample_ratio: 1.0`.

**File:** `config/dev.yaml`, `config/prod.yaml`

- Add:
  ```yaml
  tracing:
    enabled: false          # dev: false by default; set true to test
    service_name: "weather-alert-service"
    exporter: "stdout"       # dev: stdout for visibility; prod: otlp
    endpoint: ""             # prod: "http://otel-collector:4318/v1/traces"
    sample_ratio: 1.0
  ```

### 2. TracerProvider Initialization

**File:** `cmd/service/main.go` or new `internal/observability/tracing.go`

- Create `InitTracer(cfg *config.Config) (func(context.Context) error, error)`:
  - If `!cfg.TracingEnabled`, return `nil, nil`.
  - Build `resource.New()` with `service.name`, `service.version`.
  - Create exporter from `cfg.TracingExporter`:
    - `stdout`: `stdouttrace.New(stdouttrace.WithPrettyPrint())`
    - `otlp`: `otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.TracingEndpoint), otlptracehttp.WithInsecure())`
  - Create `sdktrace.NewTracerProvider` with `WithBatcher(exporter)`, `WithResource(resource)`, `WithSampler(sdktrace.TraceIDRatioBased(cfg.TracingSampleRatio))`.
  - Set global: `otel.SetTracerProvider(tp)`, `otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))`.
  - Return `tp.Shutdown` as shutdown func.
- Call `InitTracer` after config load; store shutdown func.
- In shutdown block, if shutdown func non-nil, call it with timeout.

### 3. Server Instrumentation

**File:** `cmd/service/main.go`

- Import `otelmux`.
- If tracing enabled, wrap the root handler with otelmux:
  ```go
  var handler http.Handler = router
  if tracerProvider != nil {
      handler = otelmux.Middleware(cfg.TracingServiceName)(router)
  }
  srv := &http.Server{Handler: handler, ...}
  ```
- **Important:** `otelmux.Middleware` returns a middleware function. For `http.Handler`, use `otelmux.Middleware(serviceName)` as a wrapper around the router. Check otelmux API: it may be `router.Use(otelmux.Middleware(...))` for mux, or we wrap the whole mux. Per docs, `router.Use(otelmux.Middleware("service-name"))` is the pattern—add as first middleware so trace context is in context before others.

**Revised:** Add `router.Use(otelmux.Middleware(cfg.TracingServiceName))` as the **first** Use() call, before CorrelationID. That ensures trace context is extracted from incoming W3C headers before any other middleware runs.

### 4. HTTP Client Instrumentation

**File:** `internal/client/client.go`

- Add constructor option or parameter to accept `http.RoundTripper`. If none provided, use `http.DefaultTransport`.
- Wrap transport with `otelhttp.NewTransport(baseTransport)` when tracer is available. Challenge: client doesn't have cfg; tracer is global. So we can always wrap: `otelhttp.NewTransport(base)` will no-op if no TracerProvider is set. Use `otelhttp.NewTransport` unconditionally; when tracing disabled, otel uses a no-op tracer.
- In `NewOpenWeatherClientWithRetry`, change:
  ```go
  baseTransport := &http.Transport{...}  // or http.DefaultTransport
  transport := otelhttp.NewTransport(baseTransport)
  client: &http.Client{Transport: transport, Timeout: timeout}
  ```
- Ensure `callAPI` passes `ctx` to `c.client.Do(req)` (it uses `req` created with `reqCtx`—ensure the request is built with `NewRequestWithContext(ctx, ...)` so the outgoing HTTP request carries the trace context). The `otelhttp.Transport` will inject trace context into headers from the request context.

**File:** `internal/client/client.go` — `buildRequest`

- Use `http.NewRequestWithContext(ctx, "GET", url, nil)` so context (with trace) is attached to the request.

### 5. Optional Service Span

**File:** `internal/service/service.go`

- Inject `trace.Tracer` via constructor or use `otel.Tracer("weather-alert-service")` from global.
- In `GetWeather`: `ctx, span := tracer.Start(ctx, "GetWeather")`; `defer span.End()`. Add span attributes: `location`, `cache_hit` (if from cache).
- Keeps service layer timing visible in trace.

### 6. Optional Cache Span

**File:** `internal/cache/` (Get/Set)

- Add span for `cache.Get` and `cache.Set` with attribute `cache.key`. Lower priority; in-memory is fast; memcached may benefit. Can defer to phase 2.

### 7. Correlation ID and Trace ID

**File:** `internal/http/middleware.go` — `CorrelationIDMiddleware`

- When tracing enabled: if no `X-Correlation-ID` in request, use `trace.SpanFromContext(r.Context()).SpanContext().TraceID().String()` as correlation ID. Ensures correlation ID and trace ID align when tracing is on.
- Response header: add `X-Trace-ID` when tracing enabled, using `SpanContext().TraceID().String()`.

### 8. Shutdown

**File:** `cmd/service/main.go`

- After `srv.Shutdown`, before exit: if `tracerShutdown != nil`, call `tracerShutdown(shutdownCtx)` to flush and close the tracer provider.

### 9. Testing

- **Unit:** With `exporter: stdout`, run a request; verify span output to stderr. Mock TracerProvider for tests if needed.
- **Integration:** `tracing.enabled: true`, `exporter: stdout`; run `./test-service.sh health`; inspect logs for span output.
- **Disabled:** `tracing.enabled: false`; no new imports in hot path when disabled (init skipped). Verify no panics and behavior unchanged.
- **OTLP:** Run a local collector (e.g. `otelcol-contrib` or Jaeger all-in-one); set `endpoint`; verify traces appear in Jaeger UI.

### 10. Files Changed Summary

| File | Changes |
|------|---------|
| `internal/config/config.go` | Tracing config struct and Load logic |
| `config/dev.yaml` | `tracing` section |
| `config/prod.yaml` | `tracing` section |
| `internal/observability/tracing.go` | New: InitTracer, TracerProvider setup |
| `cmd/service/main.go` | InitTracer, otelmux middleware, tracer shutdown |
| `internal/client/client.go` | otelhttp.Transport, NewRequestWithContext |
| `internal/service/service.go` | Optional: manual GetWeather span |
| `internal/http/middleware.go` | Optional: trace ID as correlation ID fallback |

### 11. Dependencies

```bash
go get go.opentelemetry.io/otel \
       go.opentelemetry.io/otel/exporters/stdout/stdouttrace \
       go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp \
       go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux \
       go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
```
