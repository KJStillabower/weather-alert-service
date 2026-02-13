# Weather Alert Service - About

## Project Overview

This is a take-home assessment project for a Principal Site Reliability Engineer position. The goal is to build a production-ready backend Golang service that integrates with a third-party weather API while demonstrating best practices in observability, reliability, and operational excellence.

This file is high-level about the design and repo.

Operational Instructions will be in the [README.md](README.md) file


## Project Structure

```
Weather/
├── .cursor/
│   └── rules/          # Cursor AI rules for consistent development
├── cmd/
│   └── service/        # Application entrypoint
├── config/
│   ├── dev.yaml        # Development config
│   ├── prod.yaml       # Production config
│   └── secrets.yaml    # API key only (gitignored)
├── docs/               # Design and plan documentation
│   ├── env-yaml-plan.md
│   ├── logging-plan.md
│   └── observability-metrics-plan.md
├── internal/
│   ├── cache/          # In-memory cache (cache-aside)
│   ├── client/         # OpenWeatherMap API client
│   ├── config/         # Configuration loading
│   ├── http/           # Handlers, middleware (correlation ID, metrics, rate limit, timeout)
│   ├── models/         # Shared data models
│   ├── observability/  # Metrics (Prometheus), logging (zap)
│   └── service/        # Business logic layer
├── samples/
│   └── alerting/       # Prometheus + Alertmanager config samples (PagerDuty, FireHydrant)
├── test-service.sh     # Build, start, test, stop automation
├── prompt.pdf          # Original assessment requirements
├── README.md           # Setup, usage, API documentation
└── About.md            # This file
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

- `000-goal.mdc` - Project scope and priorities
- `010-role.mdc` - AI assistant collaboration approach
- `020-rule-standards.mdc` - Rule file standards
- `021-change-control.mdc` - Git commit standards
- `030-patterns.mdc` - Go language patterns and service architecture
- `040-testing.mdc` - Testing standards and patterns
- `050-observability.mdc` - Metrics and logging patterns
- `060-reliability.mdc` - Reliability patterns (retries, timeouts, rate limits)
- `070-api-contract.mdc` - API endpoint contracts
- `090-security.mdc` - Security best practices
- `100-communication.mdc` - Communication standards
- `101-documentation.mdc` - Documentation requirements

**For AI assistants:** Read these rules to understand project standards, patterns, and priorities before making changes.

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

For installation, configuration, running, and testing instructions, see [README.md](README.md).
