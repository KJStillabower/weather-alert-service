# Weather Alert Service - About

## Project Overview

This is a take-home assessment project for a Principal Site Reliability Engineer position. The goal is to build a production-ready backend Golang service that integrates with a third-party weather API while demonstrating best practices in observability, reliability, and operational excellence.

This file is high-level about the design and repo.

Operational Instructions will be in the [README.md](README.md) file


## Project Structure

```
Weather/
├── .cursor/
│   └── rules/          # Cursor AI rules for consistent development (10 rule files)
├── .github/
│   └── workflows/      # CI/CD workflows
├── cmd/
│   └── service/        # Application entrypoint
├── config/
│   ├── dev.yaml        # Development config
│   ├── dev_localcache.yaml  # Development config (in-memory cache)
│   ├── prod.yaml       # Production config
│   └── secrets.yaml    # API key only (gitignored)
├── docs/               # Design and plan documentation
│   ├── About.md        # This file - project overview and design
│   ├── observability.md  # Comprehensive observability guide
│   ├── test-coverage-summary.md  # Test coverage overview
│   ├── rule-simplification-summary.md  # Rules simplification summary
│   ├── plans/         # Design plan documents (historical context)
│   └── issues/        # GitHub issue tracking documents
├── internal/
│   ├── cache/          # Cache implementations (in-memory, memcached)
│   ├── client/         # OpenWeatherMap API client
│   ├── config/         # Configuration loading
│   ├── http/           # Handlers, middleware (correlation ID, metrics, rate limit, timeout)
│   ├── models/         # Shared data models
│   ├── observability/  # Metrics (Prometheus), logging (zap)
│   └── service/        # Business logic layer
├── samples/
│   ├── alerting/       # Prometheus + Alertmanager config samples
│   └── containers/     # Docker/Kubernetes build scripts
├── test-service.sh     # Build, start, test, stop automation
├── prompt.pdf          # Original assessment requirements
└── README.md           # Setup, usage, API documentation
```

## Key Design Decisions

**Language:** Go (Golang) - chosen for production readiness and operational excellence focus

**Architecture:** Layered service pattern
- HTTP handlers (thin, validation/translation only)
- Service layer (orchestration, business logic)
- Client layer (external API integration)
- Cache layer (in-memory cache-aside pattern)

**Observability:** Prometheus metrics + structured logging (zap)
- Metrics: request rates, latencies, cache performance, API calls
- Logging: decisions, boundaries, failures only (not routine success)
- Correlation IDs for request tracing

**Reliability:**
- Timeouts on all external calls and per-request handler timeout (`request.timeout`)
- Retry logic with exponential backoff (transient errors only)
- Rate limiting (token bucket on `/weather/{location}`, 429 when exceeded)
- Graceful shutdown

**Configuration:** `config/[env].yaml` for port, timeouts, cache, retries, rate limit, metrics; `ENV_NAME` (default: dev) selects env; API key from `WEATHER_API_KEY` or `config/secrets.yaml`; `LOG_LEVEL` env for log verbosity

## Cursor Rules

This project uses Cursor rules (`.cursor/rules/*.mdc`) to guide AI-assisted development:

**Always-Apply Rules (7 files):**
- `000-goal.mdc` - Project scope and priorities
- `001-preserve-existing.mdc` - Critical safety rule (preserve existing work)
- `010-role.mdc` - AI assistant collaboration approach
- `020-security.mdc` - Security best practices
- `030-patterns.mdc` - Go language patterns and service architecture
- `060-reliability.mdc` - Reliability patterns (retries, timeouts, rate limits)
- `100-documentation-communication.mdc` - Communication, git commits, and documentation standards

**Context-Specific Rules (3 files, load only when editing relevant files):**
- `040-testing.mdc` - Testing standards (`globs: **/*_test.go`)
- `050-observability.mdc` - Metrics and logging patterns (`globs: **/observability/**`)
- `070-api-contract.mdc` - API endpoint contracts (`globs: **/http/**`)

**For AI assistants:** Read these rules to understand project standards, patterns, and priorities before making changes. Context-specific rules load automatically when editing relevant files.

## Current Implementation Status

**Completed:**
- Basic service structure and dependency injection
- HTTP handlers for `/weather/{location}`, `/health`, `/metrics`
- Correlation ID middleware; propagation to upstream API
- Prometheus metrics middleware (request rates, latencies, in-flight)
- Configuration loading from `config/[env].yaml`, `config/secrets.yaml`, `WEATHER_API_KEY`
- In-memory cache (cache-aside, TTL); cache hit metrics
- OpenWeatherMap API client with retry, exponential backoff, context propagation
- Service layer with cache-aside pattern
- Retry logic (transient errors only); retry metrics
- Rate limiting middleware (token bucket, 429, configurable)
- Request timeout middleware for `/weather` (configurable)
- API key validation at startup and in health check
- Structured logging (zap), correlation IDs, `LOG_LEVEL` env
- Graceful shutdown
- Unit tests (config, cache, client, handlers, middleware, observability, service)
- Integration tests for API client (`go test -tags=integration`)
- Alerting samples (Prometheus rules, Alertmanager, PagerDuty/FireHydrant)


## For Reviewers

- See `prompt.pdf` for original requirements
- See `.cursor/rules/` for development standards and patterns
- See `README.md` for setup and usage instructions
- All code follows Go best practices and project-specific patterns defined in rules
- Documentation organization:
  - **Root docs:** Active reference guides (`About.md`, `observability.md`, summaries)
  - **`docs/plans/`:** Historical design plan documents (preserved for context)
  - **`docs/issues/`:** GitHub issue tracking documents (preserved for context)

For installation, configuration, running, and testing instructions, see [README.md](README.md).
