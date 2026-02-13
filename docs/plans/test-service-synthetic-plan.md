# test-service.sh synthetic – Plan

## Goal

Add a `synthetic` command to `test-service.sh` that exercises five major functionalities via the `/test` endpoint:

1. **Load** – Trigger overload
2. **Reset** – Clear all simulated state
3. **Degraded** – Trigger degraded via error budget
4. **Recovery** – Test recovery from degraded (prevent, fail, succeed)
5. **Recovery Fail** – Fibonacci exhaustion, shutting-down, reset clears

**Prerequisites:** Service running with `testing_mode: true` (e.g. `ENV_NAME=dev`), config from `config/dev.yaml`.

**Config (dev.yaml):** `rate_limit_rps=2`, `overload_window=60s`, `overload_threshold_pct=80`, `degraded_window=60s`, `degraded_error_pct=5`.

---

## 1. Load (Trigger Overload)

**Purpose:** Verify that synthetic load accumulates in the traffic window and triggers overload when exceeding the threshold.

**Formula:** Overloaded when `total_requests_in_window > rate_limit_rps × overload_window × (overload_threshold_pct/100)` → `2 × 60 × 0.8 = 96`.

### Flow

| Step | Action | Expected |
|------|--------|----------|
| 1 | `GET /test` | 200, parse `total_requests_in_window`, `denied_requests_in_window`, `auto_clear`. Initial state typically 0. |
| 2 | `POST /test/load` `{"count": 30}` | `state: "healthy"` (30 < 96) |
| 3 | sleep 1 | — |
| 4 | `POST /test/load` `{"count": 30}` | `state: "healthy"` (60 < 96) |
| 5 | sleep 1 | — |
| 6 | `POST /test/load` `{"count": 30}` | `state: "healthy"` (90 < 96) |
| 7 | sleep 1 | — |
| 8 | `POST /test/load` `{"count": 30}` | `state: "overloaded"` (120 > 96) |
| 9 | `GET /test` | Confirm `total_requests_in_window` ≈ 120, display parsed output. |

**Success:** Overload triggered by step 8. Log confirmation.

---

## 2. Reset

**Purpose:** Verify that `POST /test/reset` clears all simulated state (traffic, idle, degraded, recovery overrides, shutting-down flag).

### Flow

| Step | Action | Expected |
|------|--------|----------|
| 1 | (After Load test) `POST /test/reset` | 200, `"message": "All simulated state cleared"` |
| 2 | `GET /test` | `total_requests_in_window=0`, `denied_requests_in_window=0`, `errors_in_window=0`, `auto_clear=true` |
| 3 | `GET /health` | `status: "healthy"` (or degraded if API key invalid; reset does not fix API key) |

**Note:** Reset clears in-memory state only. It does not restart the process. There is no `test/restart` endpoint.

**Success:** All counters zero, health no longer overloaded.

---

## 3. Degraded

**Purpose:** Verify that the error budget (errors / (successes + errors)) triggers degraded when exceeding `degraded_error_pct` (5%).

**Formula:** Degraded when `error_rate_pct >= 5`. With load 39 + error 1 → 1/40 = 2.5% (healthy). Load 39 + error 3 → 3/42 ≈ 7.1% (degraded).

### Flow

| Step | Action | Expected |
|------|--------|----------|
| 1 | `POST /test/reset` | Clean slate |
| 2 | `POST /test/prevent_clear` | `auto_clear: false` – disable auto-recovery so we control timing |
| 3 | `POST /test/load` `{"count": 39}` | 39 successes in window |
| 4 | `POST /test/error` `{"count": 1}` | `state: "healthy"`, `error_rate_pct: 2` (1/40 ≈ 2.5%) |
| 5 | `POST /test/error` `{"count": 2}` | `state: "degraded"`, `error_rate_pct: 7` (3/42 ≈ 7.1%) |
| 6 | `GET /health` | 503, `status: "degraded"` |

**Success:** Step 4 healthy, step 5 degraded. Health reflects degraded.

**Endpoint requirement:** `POST /test/prevent_clear` must call `degraded.SetRecoveryDisabled(true)`. If not implemented, wire it (and fail_clear, clear) in `PostTestAction`.

---

## 4. Recovery

**Purpose:** Verify recovery controls: simulated failure, then forced success, restoring healthy state.

### Flow

| Step | Action | Expected |
|------|--------|----------|
| 1 | (After Degraded test) `POST /test/fail_clear` | Simulate failed recovery attempt; service remains degraded |
| 2 | `GET /health` | Still 503, `status: "degraded"` |
| 3 | `POST /test/clear` | Force successful recovery; clears degraded tracker and overrides |
| 4 | `GET /health` | 200, `status: "healthy"` |
| 5 | `GET /test` | `auto_clear` restored per ClearRecoveryOverrides; errors cleared |

**Success:** fail_clear leaves degraded; clear restores healthy.

**Endpoint requirements:**
- `POST /test/fail_clear` → `degraded.SetForceFailNextAttempt(true)` then trigger recovery (or rely on NotifyDegraded if already running)
- `POST /test/clear` → `degraded.SetForceSucceedNextAttempt(true)` then trigger recovery; or directly clear tracker and overrides per testing-simulation-plan semantics

---

## 5. Recovery Fail (Fibonacci exhaustion)

**Purpose:** Verify fail_clear advances Fibonacci sequence (1m, 2m, 3m, 5m, 8m, 13m), exhausts to shutting-down, and reset clears.

### Flow

| Step | Action | Expected |
|------|--------|----------|
| 1 | `POST /test/reset` | Clean slate |
| 2 | `POST /test/prevent_clear` | Disable auto-recovery |
| 3 | `POST /test/load` `{"count": 39}` + `POST /test/error` `{"count": 3}` | Degraded |
| 4 | `POST /test/fail_clear` x6 | `next_recovery`: 1m0s, 2m0s, 3m0s, 5m0s, 8m0s, 13m0s |
| 5 | `POST /test/fail_clear` (7th) | `next_recovery`: shutting-down, `/health` status=shutting-down |
| 6 | `POST /test/reset` | Restore healthy for subsequent tests |

**Success:** Fibonacci sequence displayed; exhaustion triggers shutting-down; reset clears.

---

## Implementation Outline

1. Add `synthetic` command to `test-service.sh`. Done.
2. Implement steps in order: Load → Reset → Degraded → Recovery. Use `curl` for all HTTP calls. Done.
3. Parse JSON responses (python3/json) to assert expected values. Done.
4. Log pass/fail per functionality. Exit with error if any step fails. Done.
5. Prerequisite check: valid WEATHER_API_KEY (status=healthy) and testing_mode=true.

---

## Endpoint Checklist

| Endpoint | Implemented | Notes |
|----------|-------------|-------|
| `GET /test` | yes | Status |
| `POST /test/load` | yes | Records N successes to traffic |
| `POST /test/error` | yes | Records N errors to traffic |
| `POST /test/reset` | yes | Clears all state |
| `POST /test/shutdown` | yes | Sets shutting-down |
| `POST /test/prevent_clear` | yes | SetRecoveryDisabled(true) |
| `POST /test/fail_clear` | yes | SetForceFailNextAttempt; next_recovery in response |
| `POST /test/clear` | yes | Reset + ClearRecoveryOverrides |
