#!/bin/bash

# test-service.sh - Build, run, and test the Weather Alert Service
#
# Usage:  ./test-service.sh [OPTIONS] COMMAND
#         ./test-service.sh              # default: all (build, start, run tests)
#
# Commands:
#   build       Build the service binary
#   start       Build and start the service
#   stop        Stop the service (alias: cleanup)
#   start_cache Start memcached (Docker or system daemon)
#   stop_cache  Stop memcached
#   test        Run all tests (health, weather, cache, metrics)
#   health      Test GET /health
#   weather     Test GET /weather/{location}
#   metrics     Test GET /metrics
#   cache       Test cache hit (two requests, same timestamp)
#   logs        Show recent service logs
#   synthetic   Run lifecycle tests via /test (load, reset, degraded, recovery)
#   all         Build, start, run all tests (default)
#
# Options:  -v, --verbose  Show raw API responses
#
# Env:      ENV_NAME=dev         Config file (default: dev)
#           WEATHER_API_KEY      Required for health/weather
#           SERVER_PORT=8080     Service port
#           MEMCACHE_PORT=11211  Memcached port for start_cache
#
# See:  docs/test-service-synthetic-plan.md, docs/cache-design-plan.md

set -euo pipefail

# Run from script directory so config/*.yaml are found
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SERVICE_BIN="./bin/service"
SERVICE_PORT="${SERVER_PORT:-8080}"
BASE_URL="http://localhost:${SERVICE_PORT}"
LOG_FILE="/tmp/weather-service-test.log"
PID_FILE="/tmp/weather-service.pid"
MEMCACHE_PORT="${MEMCACHE_PORT:-11211}"
CACHE_CONTAINER_NAME="weather-memcached"
MEMCACHE_PID_FILE="/tmp/weather-memcached.pid"
VERBOSE=false
CLEANUP_ON_EXIT=false

# Functions

# log_info msg
# Prints an info message to stdout (blue).
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# log_success msg
# Prints a success message to stdout (green).
log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# log_error msg
# Prints an error message to stdout (red).
log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# log_warn msg
# Prints a warning message to stdout (yellow).
log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# cleanup [--force]
# Stops the service process and any process on SERVICE_PORT. With --force (or CLEANUP_ON_EXIT=true),
# runs unconditionally; otherwise no-op. Called on EXIT/INT/TERM by trap.
cleanup() {
    local force=false
    [ "${1:-}" = "--force" ] && force=true

    if [ "$force" = false ] && [ "$CLEANUP_ON_EXIT" != "true" ]; then
        return 0
    fi

    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            log_info "Stopping service (PID: $PID)..."
            kill "$PID" 2>/dev/null || true
            wait "$PID" 2>/dev/null || true
            rm -f "$PID_FILE"
            log_success "Service stopped"
        else
            rm -f "$PID_FILE"
        fi
    fi

    # Kill any process still bound to SERVICE_PORT (e.g. from manual run or stale PID)
    local port_pid
    port_pid=$(lsof -ti ":$SERVICE_PORT" 2>/dev/null || true)
    if [ -n "$port_pid" ]; then
        log_info "Stopping process on port $SERVICE_PORT (PID: $port_pid)..."
        kill $port_pid 2>/dev/null || true
        wait $port_pid 2>/dev/null || true
    fi
}

trap cleanup EXIT INT TERM

# start_cache
# Starts memcached on localhost:${MEMCACHE_PORT}. Prefers Docker (container weather-memcached);
# falls back to system memcached if Docker unavailable. Exits with 1 on failure.
start_cache() {
    log_info "Starting memcached (localhost:${MEMCACHE_PORT})..."
    if command -v docker >/dev/null 2>&1; then
        if docker ps -q -f "name=^${CACHE_CONTAINER_NAME}$" 2>/dev/null | grep -q .; then
            log_success "Memcached already running (container ${CACHE_CONTAINER_NAME})"
            return 0
        fi
        if docker ps -aq -f "name=^${CACHE_CONTAINER_NAME}$" 2>/dev/null | grep -q .; then
            docker start "$CACHE_CONTAINER_NAME" >/dev/null 2>&1
            log_success "Memcached started (existing container)"
        else
            log_info "Pulling memcached:latest..."
            local pull_out pull_err=0
            pull_out=$(docker pull memcached:latest 2>&1) || pull_err=$?
            if [ $pull_err -ne 0 ]; then
                echo "$pull_out" | grep -q "permission denied" && \
                    log_error "Docker permission denied. Add your user to the docker group: sudo usermod -aG docker \$USER (then log out and back in). Or run: sudo $0 start_cache" || \
                    log_error "Failed to pull memcached image: $pull_out"
                exit 1
            fi
            if docker run -d --name "$CACHE_CONTAINER_NAME" -p "${MEMCACHE_PORT}:11211" memcached:latest 2>&1; then
                log_success "Memcached started (container ${CACHE_CONTAINER_NAME})"
            else
                log_error "Failed to start memcached container"
                exit 1
            fi
        fi
    elif command -v memcached >/dev/null 2>&1; then
        if [ -f "$MEMCACHE_PID_FILE" ]; then
            local pid
            pid=$(cat "$MEMCACHE_PID_FILE" 2>/dev/null)
            if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                log_success "Memcached already running (PID $pid)"
                return 0
            fi
            rm -f "$MEMCACHE_PID_FILE"
        fi
        memcached -d -l 127.0.0.1 -p "$MEMCACHE_PORT" -P "$MEMCACHE_PID_FILE" 2>/dev/null || true
        sleep 1
        if [ -f "$MEMCACHE_PID_FILE" ] && kill -0 "$(cat "$MEMCACHE_PID_FILE")" 2>/dev/null; then
            log_success "Memcached started (system daemon)"
        else
            log_error "Failed to start memcached. Check: memcached -d -l 127.0.0.1 -p ${MEMCACHE_PORT}"
            exit 1
        fi
    else
        log_error "Docker or memcached required. Install Docker, or: apt install memcached / brew install memcached"
        exit 1
    fi
    sleep 1
}

# stop_cache
# Stops memcached (Docker container or system daemon). No-op if not running.
stop_cache() {
    log_info "Stopping memcached..."
    if command -v docker >/dev/null 2>&1; then
        if docker ps -q -f "name=^${CACHE_CONTAINER_NAME}$" 2>/dev/null | grep -q .; then
            docker stop "$CACHE_CONTAINER_NAME" >/dev/null 2>&1
            log_success "Memcached stopped"
        else
            log_info "Memcached not running (container ${CACHE_CONTAINER_NAME})"
        fi
    elif [ -f "$MEMCACHE_PID_FILE" ]; then
        local pid
        pid=$(cat "$MEMCACHE_PID_FILE" 2>/dev/null)
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            wait "$pid" 2>/dev/null || true
            log_success "Memcached stopped"
        fi
        rm -f "$MEMCACHE_PID_FILE"
    else
        log_info "Memcached not running"
    fi
}

# check_service_running [max_attempts]
# Polls GET /health until response contains "service". Default 30s (30 attempts); pass N for N-second check.
# Returns 0 when ready, 1 on timeout.
check_service_running() {
    local max_attempts="${1:-30}"
    local attempt=1
    
    log_info "Waiting for service to be ready..."
    while [ $attempt -le $max_attempts ]; do
        local response
        response=$(curl -s "${BASE_URL}/health" 2>/dev/null) || true
        if echo "$response" | grep -q '"service"'; then
            log_success "Service is ready"
            return 0
        fi
        sleep 1
        attempt=$((attempt + 1))
    done
    if [ $max_attempts != 1 ]; then
        log_error "Service failed to start within ${max_attempts} seconds"
    else
        log_info "Service not started."
    fi

    return 1
}

# build_service
# Builds the Go service binary to SERVICE_BIN (./bin/service). Exits with 1 on build failure.
build_service() {
    log_info "Building service..."
    if go build -o "$SERVICE_BIN" ./cmd/service; then
        log_success "Service built successfully"
    else
        log_error "Failed to build service"
        exit 1
    fi
}

# start_service
# Builds (via build_service), starts the binary in background, waits for ready via check_service_running.
# Kills any existing instance first. Exits with 1 if service fails to become ready.
start_service() {
    log_info "Starting service..."
    
    # Kill any existing instance
    if [ -f "$PID_FILE" ]; then
        OLD_PID=$(cat "$PID_FILE")
        if ps -p "$OLD_PID" > /dev/null 2>&1; then
            log_warn "Stopping existing service instance (PID: $OLD_PID)"
            kill "$OLD_PID" 2>/dev/null || true
            wait "$OLD_PID" 2>/dev/null || true
        fi
        rm -f "$PID_FILE"
    fi
    
    # Start service in background
    "$SERVICE_BIN" > "$LOG_FILE" 2>&1 &
    SERVICE_PID=$!
    echo "$SERVICE_PID" > "$PID_FILE"
    
    log_info "Service started (PID: $SERVICE_PID)"
    
    if ! check_service_running; then
        log_error "Service failed to start. Check logs: $LOG_FILE"
        tail -20 "$LOG_FILE"
        exit 1
    fi
}

# test_health
# Tests GET /health. Verifies status (healthy/degraded), reports checks.cache when present.
# Returns 1 if curl fails (e.g. non-2xx).
test_health() {
    log_info "Testing /health endpoint..."
    
    local response
    response=$(curl -sf "${BASE_URL}/health" 2>&1)
    
    if [ $? -eq 0 ]; then
        log_success "Health check passed"
        
        if [ "$VERBOSE" = true ]; then
            echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
        fi
        
        # Check if status is healthy
        if echo "$response" | grep -q '"status":\s*"healthy"'; then
            log_success "Service status: healthy"
        elif echo "$response" | grep -q '"status":\s*"degraded"'; then
            log_warn "Service status: degraded (check API key)"
        else
            log_error "Service status: unexpected (not healthy or degraded)"
        fi
        
        # Report cache check when present (memcached backend)
        local cache_status
        cache_status=$(echo "$response" | python3 -c "import sys, json; d=json.load(sys.stdin); c=d.get('checks',{}); print(c.get('cache',''))" 2>/dev/null || echo "")
        if [ -n "$cache_status" ]; then
            if [ "$cache_status" = "healthy" ]; then
                log_success "Cache: healthy"
            else
                log_warn "Cache: unhealthy (memcached may be unreachable)"
            fi
        fi
    else
        log_error "Health check failed: $response"
        return 1
    fi
}

# test_weather location
# Tests GET /weather/{location}. Validates response has location and temperature. Returns 1 on failure.
test_weather() {
    local location="$1"
    log_info "Testing /weather/${location} endpoint..."
    
    local response
    response=$(curl -sf "${BASE_URL}/weather/${location}" 2>&1)
    
    if [ $? -eq 0 ]; then
        log_success "Weather request for ${location} succeeded"
        
        if [ "$VERBOSE" = true ]; then
            echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
        fi
        
        # Validate response structure
        if echo "$response" | grep -q '"location"'; then
            local temp=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('temperature', 'N/A'))" 2>/dev/null || echo "N/A")
            log_success "Temperature: ${temp}°C"
        fi
    else
        log_error "Weather request failed: $response"
        return 1
    fi
}

# test_metrics
# Tests GET /metrics. Checks for weatherApiCallsTotal. Returns 1 on failure.
test_metrics() {
    log_info "Testing /metrics endpoint..."
    
    local response
    response=$(curl -sf "${BASE_URL}/metrics" 2>&1)
    
    if [ $? -eq 0 ]; then
        log_success "Metrics endpoint accessible"
        
        # Check for key metrics (metric names match internal/observability/metrics.go)
        if echo "$response" | grep -q "weatherApiCallsTotal"; then
            log_success "Weather API metrics found"
            if [ "$VERBOSE" = true ]; then
                echo "$response" | grep -E "(weatherApiCallsTotal|weatherApiDurationSeconds)"
            else
                echo "$response" | grep -E "(weatherApiCallsTotal|weatherApiDurationSeconds)" | head -10
            fi
        else
            log_warn "Weather API metrics not found"
        fi
    else
        log_error "Metrics request failed: $response"
        return 1
    fi
}

# synthetic_curl METHOD path [json_body]
# Low-level curl wrapper for /test and /health. Uses BASE_URL. Optional json_body for POST.
synthetic_curl() {
    curl -s -X "$1" "${BASE_URL}$2" ${3:+-H "Content-Type: application/json" -d "$3"}
}

# synthetic_json json_string key
# Extracts value for key from JSON string via python3. Returns empty string if parse fails.
synthetic_json() {
    echo "$1" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('$2', ''))" 2>/dev/null || echo ""
}

# synthetic_get_config
# Fetches config from GET /test config object. Sets: OVERLOAD_THRESHOLD, RATE_LIMIT_RPS, RATE_LIMIT_BURST, DEGRADED_ERROR_PCT.
# Returns 1 if GET /test fails or config missing.
synthetic_get_config() {
    local response
    response=$(synthetic_curl GET /test) || true
    if ! echo "$response" | grep -q '"config"'; then
        log_error "GET /test missing config (upgrade service or testing_mode off)"
        return 1
    fi
    OVERLOAD_THRESHOLD=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('overload_threshold',''))" 2>/dev/null)
    RATE_LIMIT_RPS=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('rate_limit_rps',''))" 2>/dev/null)
    RATE_LIMIT_BURST=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('rate_limit_burst',''))" 2>/dev/null)
    DEGRADED_ERROR_PCT=$(echo "$response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('config',{}).get('degraded_error_pct',''))" 2>/dev/null)
    if [ -z "$OVERLOAD_THRESHOLD" ] || [ "$OVERLOAD_THRESHOLD" = "0" ]; then
        log_error "overload_threshold is 0 (rate limiter disabled?)"
        return 1
    fi
}

# synthetic_load
# Synthetic test: 4 load passes, config-driven batch size. Expects overload on 4th pass.
# Requires testing_mode, GET /test with config. Returns 1 on failure.
synthetic_load() {
    log_info "Synthetic: Load (trigger overload)..."
    synthetic_get_config || return 1
    local batch_size spacing i
    batch_size=$(( (OVERLOAD_THRESHOLD + 20 + 3) / 4 ))
    spacing=1
    for i in 1 2 3 4; do
        local response state message
        response=$(synthetic_curl POST /test/load "{\"count\": $batch_size}")
        state=$(synthetic_json "$response" "state")
        message=$(synthetic_json "$response" "message")
        echo "$message"
        if [ "$i" -lt 4 ]; then
            if [ "$state" != "healthy" ]; then
                log_error "Load pass $i: expected state=healthy, got $state"
                return 1
            fi
        else
            if [ "$state" != "overloaded" ]; then
                log_error "Load pass $i: expected state=overloaded, got $state"
                return 1
            fi
        fi
        [ "$i" -lt 4 ] && sleep "$spacing"
    done
    local response denied errors total window
    response=$(synthetic_curl GET /test)
    denied=$(synthetic_json "$response" "denied_requests_in_window")
    errors=$(synthetic_json "$response" "errors_in_window")
    total=$(synthetic_json "$response" "total_requests_in_window")
    window=$(synthetic_json "$response" "window_length")
    log_success "Load: overload triggered with denied=$denied errors=$errors total=$total in $window window"
}

# synthetic_reset
# Synthetic test: POST /test/reset, verifies all counters zero. Returns 1 on failure.
synthetic_reset() {
    log_info "Synthetic: Reset..."
    local response
    response=$(synthetic_curl POST /test/reset)
    if ! echo "$response" | grep -q '"All simulated state cleared"'; then
        log_error "Reset failed"
        return 1
    fi
    response=$(synthetic_curl GET /test)
    local total denied errors
    total=$(synthetic_json "$response" "total_requests_in_window")
    denied=$(synthetic_json "$response" "denied_requests_in_window")
    errors=$(synthetic_json "$response" "errors_in_window")
    if [ "$total" != "0" ] || [ "$denied" != "0" ] || [ "$errors" != "0" ]; then
        log_error "Reset: expected zeros, got total=$total denied=$denied errors=$errors"
        return 1
    fi
    log_success "Reset: all state cleared"
}

# synthetic_degraded
# Synthetic test: load successes, inject errors 2x (under limit -> healthy, over limit -> degraded).
# Config-driven: min_successes and error counts from degraded_error_pct.
# Returns 1 on failure.
synthetic_degraded() {
    log_info "Synthetic: Error budget test (healthy -> degraded)"
    synthetic_get_config || return 1
    synthetic_curl POST /test/prevent_clear >/dev/null
    sleep 2
    local min_successes batch_size accepted response total denied
    min_successes=$(( (100 + DEGRADED_ERROR_PCT - 1) / DEGRADED_ERROR_PCT ))
    batch_size=${RATE_LIMIT_BURST:-10}
    accepted=0
    while [ "$accepted" -lt "$min_successes" ]; do
        synthetic_curl POST /test/load "{\"count\": $batch_size}" >/dev/null
        response=$(synthetic_curl GET /test)
        total=$(synthetic_json "$response" "total_requests_in_window")
        denied=$(synthetic_json "$response" "denied_requests_in_window")
        accepted=$((total - denied))
        sleep 1
    done
    echo "Accepted: $accepted requests, injecting errors (under then over limit)..."
    response=$(synthetic_curl POST /test/error '{"count": 1}')
    local state pct
    state=$(synthetic_json "$response" "state")
    pct=$(synthetic_json "$response" "error_rate_pct")
    if [ "$state" != "healthy" ]; then
        log_error "Degraded: after 1 error expected state=healthy, got $state (pct=$pct)"
        return 1
    fi
    local second_err num den
    num=$(( DEGRADED_ERROR_PCT * (accepted + 1) - 100 ))
    den=$(( 100 - DEGRADED_ERROR_PCT ))
    if [ "$num" -le 0 ]; then
        second_err=1
    else
        second_err=$(( (num + den - 1) / den ))
        [ "$second_err" -lt 1 ] && second_err=1
    fi
    response=$(synthetic_curl POST /test/error "{\"count\": $second_err}")
    state=$(synthetic_json "$response" "state")
    pct=$(synthetic_json "$response" "error_rate_pct")
    if [ "$state" != "degraded" ]; then
        log_error "Degraded: after $second_err more errors expected state=degraded, got $state (pct=$pct)"
        return 1
    fi
    response=$(synthetic_curl GET /health)
    if ! echo "$response" | grep -qE '"status"[[:space:]]*:[[:space:]]*"degraded"'; then
        log_error "Degraded: /health should return status=degraded"
        return 1
    fi
    local err_total=$((1 + second_err))
    log_success "Degraded: error rate exceeded threshold at $pct% ($err_total errors)"
}

# synthetic_recovery
# Synthetic test: fail_clear (stays degraded), clear (restores healthy). Returns 1 on failure.
synthetic_recovery() {
    log_info "Synthetic: Recovery..."
    local response
    response=$(synthetic_curl POST /test/fail_clear)
    if ! echo "$response" | grep -q '"next_recovery"'; then
        log_error "Recovery step 1: fail_clear missing next_recovery"
        return 1
    fi
    response=$(synthetic_curl GET /health)
    if ! echo "$response" | grep -qE '"status"[[:space:]]*:[[:space:]]*"degraded"'; then
        log_error "Recovery step 2: expected degraded after fail_clear"
        return 1
    fi
    response=$(synthetic_curl POST /test/clear)
    response=$(synthetic_curl GET /health)
    if ! echo "$response" | grep -qE '"status"[[:space:]]*:[[:space:]]*"healthy"'; then
        log_error "Recovery step 4: expected healthy after clear"
        return 1
    fi
    log_success "Recovery: clear restored healthy"
}

# synthetic_recovery_fail
# Synthetic test: fail_clear x6 (Fibonacci 1m..13m), 7th exhausts to shutting-down, reset restores.
# Load goes through rate limiter; 8 batches of 5 with 3s spacing yield ~40 successes.
# Returns 1 on failure.
synthetic_recovery_fail() {
    log_info "Synthetic: Failure recovery Fibonacci backoff..."
    synthetic_curl POST /test/reset >/dev/null
    synthetic_curl POST /test/prevent_clear >/dev/null
    
    synthetic_curl POST /test/error '{"count": 3}' >/dev/null
    local response state
    state=$(synthetic_json "$(synthetic_curl GET /health)" "status")
    if [ "$state" != "degraded" ]; then
        log_error "Recovery fail step 3: expected degraded, got $state"
        return 1
    fi
    local expected=( "1m0s" "2m0s" "3m0s" "5m0s" "8m0s" "13m0s" )
    local i
    for i in 0 1 2 3 4 5; do
        response=$(synthetic_curl POST /test/fail_clear)
        local next
        next=$(synthetic_json "$response" "next_recovery")
        echo "Next recovery: $next"
        if [ "$next" != "${expected[$i]}" ]; then
            log_error "Recovery fail step 4 ($((i+1))/6): expected next_recovery=${expected[$i]}, got $next"
            return 1
        fi
    done
    response=$(synthetic_curl POST /test/fail_clear)
    local next
    next=$(synthetic_json "$response" "next_recovery")
    if [ "$next" != "shutting-down" ]; then
        log_error "Recovery fail step 5: expected next_recovery=shutting-down, got $next"
        return 1
    fi
    state=$(synthetic_json "$(synthetic_curl GET /health)" "status")
    if [ "$state" != "shutting-down" ]; then
        log_error "Recovery fail step 5: expected status=shutting-down, got $state"
        return 1
    fi
    synthetic_curl POST /test/reset >/dev/null
    state=$(synthetic_json "$(synthetic_curl GET /health)" "status")
    if [ "$state" != "healthy" ]; then
        log_error "Recovery fail cleanup: reset should restore healthy, got $state"
        return 1
    fi
    log_success "Recovery fail: Fibonacci exhausted, shutdown triggered, reset restored"
}

# test_synthetic
# Runs all synthetic lifecycle tests (load, reset, degraded, recovery, recovery_fail). Requires
# service running, status=healthy, testing_mode=true. Exits with 1 on any failure.
test_synthetic() {
    log_info "Running synthetic lifecycle tests (/test endpoint)..."
    synthetic_reset || exit 1
    echo ""
    if ! check_service_running; then
        log_error "Service is not running. Start with: $0 start"
        exit 1
    fi
    local health status
    health=$(synthetic_curl GET /health)
    status=$(synthetic_json "$health" "status")
    if [ "$status" != "healthy" ] && [ "$status" != "idle" ]; then
        log_error "Synthetic requires status=healthy or idle (valid WEATHER_API_KEY). Got: $status"
        exit 1
    fi
    if [ "$status" = "idle" ]; then
        log_info "Service is idle; restarting to begin with fresh state..."
        cleanup --force
        start_service
        sleep 2
    fi
    if ! echo "$(synthetic_curl GET /test 2>/dev/null)" | grep -q '"total_requests_in_window"'; then
        log_error "Synthetic requires testing_mode=true (config/dev.yaml)"
        exit 1
    fi
    synthetic_load || exit 1
    echo ""
    synthetic_reset || exit 1
    echo ""
    synthetic_degraded || exit 1
    echo ""
    synthetic_recovery || exit 1
    echo ""
    synthetic_recovery_fail || exit 1
    echo ""
    log_info "Synthetic: clearing state after completion"
    synthetic_curl POST /test/reset >/dev/null
    log_success "Synthetic: all five functionalities passed"
}

# test_cache
# Tests cache: two GET /weather/{location} requests; expects same timestamp (cache hit).
# Skips if checks.cache=unhealthy. Warns (does not fail) on timestamp mismatch.
test_cache() {
    log_info "Testing cache functionality..."
    
    # When memcached is configured, skip if cache is unreachable
    local health
    health=$(curl -s "${BASE_URL}/health" 2>/dev/null) || true
    local cache_status
    cache_status=$(echo "$health" | python3 -c "import sys, json; d=json.load(sys.stdin); c=d.get('checks',{}); print(c.get('cache',''))" 2>/dev/null || echo "")
    if [ "$cache_status" = "unhealthy" ]; then
        log_warn "Cache unreachable (checks.cache=unhealthy); skipping cache test"
        return 0
    fi
    
    local location="seattle"
    local first_response
    local second_response
    
    log_info "First request (cache miss expected)..."
    first_response=$(curl -sf "${BASE_URL}/weather/${location}" 2>&1)
    local first_timestamp=$(echo "$first_response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('timestamp', ''))" 2>/dev/null || echo "")
    
    sleep 1
    
    log_info "Second request (cache hit expected)..."
    second_response=$(curl -sf "${BASE_URL}/weather/${location}" 2>&1)
    local second_timestamp=$(echo "$second_response" | python3 -c "import sys, json; print(json.load(sys.stdin).get('timestamp', ''))" 2>/dev/null || echo "")
    
    if [ "$first_timestamp" = "$second_timestamp" ] && [ -n "$first_timestamp" ]; then
        log_success "Cache is working (timestamps match)"
    else
        log_warn "Cache may not be working (timestamps differ; in_memory ok, memcached requires running instance)"
    fi
}

# run_all_tests
# Runs test_health, test_weather (seattle, portland), test_cache, test_metrics in sequence.
run_all_tests() {
    log_info "Running all tests..."
    echo ""
    
    test_health
    echo ""
    
    test_weather "seattle"
    echo ""
    
    test_weather "portland"
    echo ""
    
    test_cache
    echo ""
    
    test_metrics
    echo ""
    
    log_success "All tests completed"
}

# show_logs
# Prints last 20 lines of LOG_FILE if it exists. No-op otherwise.
show_logs() {
    if [ -f "$LOG_FILE" ]; then
        log_info "Recent service logs:"
        tail -20 "$LOG_FILE"
    fi
}

# main
# Parses -v/--verbose and command, dispatches to the appropriate function.
# Default command: all. Prints usage and exits 1 for unknown commands.
main() {
    local command=""
    local args=()
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            *)
                if [ -z "$command" ]; then
                    command="$1"
                else
                    args+=("$1")
                fi
                shift
                ;;
        esac
    done
    
    # Default command if none provided
    command="${command:-all}"
    
    case "$command" in
        build)
            build_service
            ;;
        start)
            build_service
            start_service
            ;;
        stop|cleanup)
            cleanup --force
            ;;
        start_cache)
            start_cache
            ;;
        stop_cache)
            stop_cache
            ;;
        test)
            if ! check_service_running; then
                log_error "Service is not running. Start it first with: $0 start"
                exit 1
            fi
            run_all_tests
            ;;
        health)
            test_health
            ;;
        weather)
            if [ ${#args[@]} -eq 0 ]; then
                log_error "Usage: $0 weather <location>"
                exit 1
            fi
            test_weather "${args[0]}"
            ;;
        metrics)
            test_metrics
            ;;
        cache)
            test_cache
            ;;
        logs)
            show_logs
            ;;
        synthetic)
            if ! check_service_running 1; then
                build_service
                CLEANUP_ON_EXIT=true
                start_service
                sleep 2
            fi
            test_synthetic
            ;;
        all)
            build_service
            CLEANUP_ON_EXIT=true
            start_service
            sleep 2
            run_all_tests
            ;;
        *)
            echo "Usage: $0 [OPTIONS] {build|start|stop|cleanup|start_cache|stop_cache|test|health|weather <location>|metrics|cache|logs|synthetic|all}"
            echo ""
            echo "Options:"
            echo "  -v, --verbose    Show raw API responses"
            echo ""
            echo "Commands:"
            echo "  build     - Build the service"
            echo "  start     - Build and start the service"
            echo "  stop      - Stop the running service (alias: cleanup)"
            echo "  start_cache - Start memcached via Docker (localhost:11211)"
            echo "  stop_cache  - Stop memcached container"
            echo "  test      - Run all tests (service must be running)"
            echo "  health    - Test health endpoint"
            echo "  weather   - Test weather endpoint (requires location)"
            echo "  metrics   - Test metrics endpoint"
            echo "  cache     - Test cache (in_memory or memcached; skips if memcached unreachable)"
            echo "  logs      - Show service logs"
            echo "  synthetic - Run lifecycle tests via /test (auto-starts service if needed)"
            echo "  all       - Build, start, and run all tests"
            echo ""
            echo "Examples:"
            echo "  $0 all                    # Run all normal tests (quiet mode)"
            echo "  $0 --verbose all          # Run all normal tests with verbose output"
            echo "  $0 -v weather seattle     # Test weather endpoint with verbose output"
            echo "  $0 synthetic              # Run lifecycle tests (auto-starts if needed, requires testing_mode)"
            echo "  $0 start_cache            # Start memcached for dev (ENV_NAME=dev with cache.backend=memcached)"
            exit 1
            ;;
    esac
}

main "$@"
