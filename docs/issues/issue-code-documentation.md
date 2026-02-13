# Issue: Add Documentation to Non-Test Go Code

**Labels:** documentation, code-quality, technical-debt

## Summary

Non-test Go code has documentation gaps, particularly for complex internal functions and unexported helpers. While exported/public APIs are generally documented, internal implementation details like `computeHealthStatus` and test endpoint handlers lack documentation that would improve maintainability.

## Current State

**Documentation coverage:**
- Exported/public functions: Generally documented (e.g., `GetWeather`, `GetHealth`, `NewHandler`)
- Struct types: Documented (e.g., `HealthConfig`, `Handler`)
- Complex internal functions: Missing documentation (e.g., `computeHealthStatus`)
- Test endpoint handlers: Missing individual function documentation (e.g., `postTestLoad`, `postTestError`)
- Helper functions: Missing documentation (e.g., `writeJSON`, `writeError`, `writeServiceError`)

**Example of well-documented code:**
```go
// GetWeather handles GET /weather/{location}.
func (h *Handler) GetWeather(w http.ResponseWriter, r *http.Request) {
    // ... implementation
}
```

**Example of missing documentation:**
```go
func (h *Handler) computeHealthStatus(ctx context.Context) healthResult {
    // Complex decision tree with no explanation
    if lifecycle.IsShuttingDown() {
        return healthResult{"shutting-down", ...}
    }
    // ... multiple conditional branches
}
```

## Problem

Per Go documentation conventions and `080-go-patterns.mdc`, exported functions should have comments explaining their purpose. Additionally, complex internal functions benefit from documentation explaining their logic and decision points.

**Current gaps:**
- Complex internal functions lack explanation of decision logic
- Test endpoint handlers lack individual documentation
- Helper functions lack purpose documentation
- Some exported functions may have minimal documentation

**Impact:**
- Reviewers must parse complex logic to understand behavior
- Maintenance burden increases (what does this function do?)
- Decision points in complex functions are unclear
- Inconsistent documentation standards across codebase

## Proposed Solution

Add documentation to non-test Go code following Go conventions and project patterns:

1. **Complex internal functions**: Add comments explaining decision logic and behavior
2. **Test endpoint handlers**: Document purpose and behavior of each handler
3. **Helper functions**: Add brief comments explaining purpose and usage
4. **Review exported functions**: Ensure all exported functions have adequate documentation

## Scope

**Files to review and update:**

**HTTP Layer:**
- `internal/http/handlers.go` - Complex `computeHealthStatus`, test handlers, helpers
- `internal/http/middleware.go` - Middleware functions

**Service Layer:**
- `internal/service/service.go` - Service orchestration logic

**Client Layer:**
- `internal/client/client.go` - API client implementation, retry logic, error handling

**Cache Layer:**
- `internal/cache/cache.go` - Cache implementation

**Config Layer:**
- `internal/config/config.go` - Configuration loading and validation

**Lifecycle/Health:**
- `internal/degraded/degraded.go` - Degraded state tracking
- `internal/degraded/recovery.go` - Recovery logic
- `internal/idle/idle.go` - Idle state tracking
- `internal/overload/overload.go` - Overload tracking
- `internal/traffic/traffic.go` - Traffic metrics
- `internal/lifecycle/lifecycle.go` - Lifecycle state

**Observability:**
- `internal/observability/metrics.go` - Metrics setup
- `internal/observability/logger.go` - Logger setup

**Entry Point:**
- `cmd/service/main.go` - Application startup and wiring

## Priority Areas

**High Priority:**
1. `computeHealthStatus` in `handlers.go` - Complex decision tree needs explanation
2. Complex retry/backoff logic in `client.go`
3. Recovery logic in `degraded/recovery.go`

**Medium Priority:**
1. Test endpoint handlers (`postTest*` functions)
2. Middleware functions
3. Service orchestration logic

**Low Priority:**
1. Simple helper functions
2. Already well-documented exported functions (review for completeness)

## Documentation Standards

**For exported functions/types:**
- Follow Go documentation conventions (sentence starting with function/type name)
- Explain purpose and behavior
- Document parameters and return values when non-obvious
- Reference related functions/types

**For complex internal functions:**
- Explain decision logic and control flow
- Document key decision points
- Explain edge cases or special behavior

**For helper functions:**
- Brief comment explaining purpose
- Note any side effects or important behavior

**Example patterns:**

```go
// computeHealthStatus determines the current health status by evaluating
// shutdown state, API key validity, overload thresholds, idle conditions,
// and error rates. Returns healthResult with status, HTTP status code, and reason.
// Decision order: shutting-down > API key invalid > overloaded > idle > degraded > healthy.
func (h *Handler) computeHealthStatus(ctx context.Context) healthResult {
    // ... implementation
}

// postTestLoad simulates load by recording the specified number of requests,
// respecting rate limits if configured. Returns accepted/denied counts and current health state.
func (h *Handler) postTestLoad(w http.ResponseWriter, r *http.Request) {
    // ... implementation
}

// writeJSON writes a JSON response with the specified HTTP status code.
// Sets Content-Type header and encodes the provided value.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
    // ... implementation
}
```

## Acceptance Criteria

- [ ] All complex internal functions have documentation explaining decision logic
- [ ] All test endpoint handlers have function-level documentation
- [ ] Helper functions have brief purpose documentation
- [ ] All exported functions have adequate documentation per Go conventions
- [ ] Documentation explains behavior and purpose, not just implementation
- [ ] Documentation follows Go documentation conventions
- [ ] Complex decision trees are clearly explained

## Priority

**Medium** - Improves maintainability and code clarity, but code is functional. Less urgent than test documentation was, but valuable for long-term maintainability.

## References

- `080-go-patterns.mdc` - Go patterns and documentation standards
- `030-patterns.mdc` - Service patterns and architecture
- Go documentation conventions: https://go.dev/doc/comment
- Current codebase in `internal/` and `cmd/`
