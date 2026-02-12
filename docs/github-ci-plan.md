# GitHub CI Plan

## Goal

Add GitHub Actions workflows so that every push and pull request triggers a build, unit tests, and static analysis. Optionally run integration tests when secrets are available. Ensure check-in does not merge broken code.

## Triggers

| Event | Branches | Purpose |
|-------|----------|---------|
| `push` | `main`, `master` | Verify main branch stays green |
| `pull_request` | `main`, `master` | Block merge if CI fails |

## Jobs Overview

| Job | Runs on | Purpose |
|-----|---------|---------|
| `build` | Every push/PR | `go build ./...` — compiles all packages |
| `test` | Every push/PR | `go test ./...` — unit tests only (no `-tags=integration`) |
| `lint` | Every push/PR | `golangci-lint run` or `go vet ./...` — static analysis |
| `integration` | Optional | Integration tests; requires secrets and/or services |

## Implementation Plan

### 1. Workflow File

**Path:** `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'

      - name: Cache Go modules
        uses: actions/cache@v4
        with:
          path: |
            ~/go/pkg/mod
            ~/.cache/go-build
          key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}

      - name: Build
        run: go build -v ./...

  test:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'

      - name: Cache Go modules
        uses: actions/cache@v4
        with:
          path: |
            ~/go/pkg/mod
            ~/.cache/go-build
          key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}

      - name: Run tests
        run: go test -v -race -count=1 ./...
        # Excludes integration tests (no -tags=integration)

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'

      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest
```

**Notes:**
- `go-version-file: 'go.mod'` picks up Go version from `go.mod` (1.24).
- `-race` enables race detector; can remove if tests are slow.
- `-count=1` disables test cache for deterministic results.

### 2. Lint Options

**Option A: golangci-lint (recommended)**
- Broad set of linters (vet, staticcheck, errcheck, etc.).
- Add `.golangci.yml` in repo root to configure; default config is fine to start.
- Action: `golangci/golangci-lint-action@v4`.

**Option B: Minimal (go vet only)**
- No extra dependency; fewer checks:
  ```yaml
  - run: go vet ./...
  ```

### 3. Integration Tests (Optional Job)

Integration tests require:
- `WEATHER_API_KEY` — GitHub secret (valid OpenWeatherMap API key).
- Memcached — for `memcached_integration_test.go`; use `services:` in workflow.

**Workflow addition:**

```yaml
  integration:
    runs-on: ubuntu-latest
    needs: [build, test]
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    services:
      memcached:
        image: memcached:latest
        ports:
          - 11211:11211

    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'

      - name: Run integration tests
        env:
          WEATHER_API_KEY: ${{ secrets.WEATHER_API_KEY }}
          MEMCACHED_ADDRS: localhost:11211
        run: go test -v -tags=integration -count=1 ./...
```

**Options for integration:**
- **Always skip:** Omit job; integration tests run locally only. Simplest.
- **Run on main only:** As above; requires `WEATHER_API_KEY` repo secret. Fails if secret not set (job fails or skips).
- **Run on schedule:** `schedule: - cron: '0 12 * * *'` for daily runs.
- **Manual dispatch:** `workflow_dispatch` so maintainers trigger when needed.

**Recommendation:** Start with build + test + lint only. Add integration job when `WEATHER_API_KEY` is configured; use `t.Skip()` gracefully if secret absent (integration tests already skip when `WEATHER_API_KEY` is empty).

### 4. test-service.sh in CI

`test-service.sh` runs full stack (build, start, synthetic lifecycle tests). It needs:
- Memcached (or dev_localcache config)
- `WEATHER_API_KEY`
- Network for OpenWeatherMap

**Options:**
- **Do not run in CI:** Keep for local/manual use. CI focuses on `go build` and `go test`.
- **Run as separate workflow:** `workflow_dispatch` or on release tags; full E2E validation.
- **Run with dev_localcache:** Use `ENV_NAME=dev_localcache` to skip memcached; still needs `WEATHER_API_KEY` for health/weather. Possible but adds complexity.

**Recommendation:** Do not run `test-service.sh` in default CI. Document in README that `./test-service.sh all` is the local full validation; CI covers build and unit tests.

### 5. Branch Naming

If default branch is `main`, ensure workflow uses `main`. If `master`, use `master`. Adjust `branches:` in `on:` accordingly.

### 6. Status Badge

Add to README after workflow exists:

```markdown
[![CI](https://github.com/Owner/weather-alert-service/actions/workflows/ci.yml/badge.svg)](https://github.com/Owner/weather-alert-service/actions/workflows/ci.yml)
```

Replace `Owner/weather-alert-service` with actual repo path.

### 7. Files to Create

| File | Purpose |
|------|---------|
| `.github/workflows/ci.yml` | Main workflow: build, test, lint |
| `.golangci.yml` | Optional; golangci-lint config |
| `README.md` | Add CI badge |

### 8. Minimal Workflow (No Lint)

If avoiding golangci-lint initially:

```yaml
name: CI

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]

jobs:
  build-and-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'
      - run: go build ./...
      - run: go test -v -count=1 ./...
```

### 9. Dependencies / Caching

`go mod download` is implicit in `go build` and `go test`. Caching `~/go/pkg/mod` and `~/.cache/go-build` speeds up runs; first run still downloads.

## Summary

| Phase | Scope | Effort |
|-------|-------|--------|
| 1 | Build + unit test | Minimal; copy workflow |
| 2 | Add lint (golangci-lint) | Low; add action + optional config |
| 3 | Add integration job | Medium; needs secret, memcached service |
| 4 | test-service.sh in CI | Optional; higher complexity |

Start with Phase 1; add Phase 2 when ready. Phase 3 when `WEATHER_API_KEY` is available.
