---
phase: 2
plan: 7
subsystem: budget
tags: [rate-limiter, budget, token-bucket, prometheus, status-patch]
dependency_graph:
  requires: ["02-03"]
  provides: ["internal/budget", "budget.Store", "budget.PreCharge", "budget.IsCapExceeded", "budget.IsBypassed", "budget.ConsumeBypass", "budget.RollUpUsage", "budget.ProviderRateLimitHitsTotal"]
  affects: ["02-09", "02-10", "02-12"]
tech_stack:
  added:
    - "golang.org/x/time v0.14.0 (promoted from indirect to direct — rate.Limiter)"
  patterns:
    - "sync.Map lazy-create via LoadOrStore for per-Secret-UID rate limiter"
    - "client.HasLabels (existence-only matcher) not client.MatchingLabels for Job label filter"
    - "client.MergeFrom + c.Status().Patch for Status subresource update"
    - "prometheus.NewCounterVec in init() via metrics.Registry.MustRegister"
key_files:
  created:
    - internal/budget/doc.go
    - internal/budget/bucket.go
    - internal/budget/bucket_test.go
    - internal/budget/precharge.go
    - internal/budget/precharge_test.go
    - internal/budget/cap.go
    - internal/budget/cap_test.go
    - internal/budget/tally.go
    - internal/budget/tally_test.go
    - internal/budget/metrics.go
  modified:
    - go.mod (golang.org/x/time promoted to direct dependency)
    - go.sum (updated)
decisions:
  - "Limits.RequestsPerMinute=0 returns nil from ForSecret — caller treats nil as unlimited (no gating)"
  - "bypass-budget-until=RFC3339 (TTL form) is the recommended form per RESEARCH.md Pitfall 7; bypass-budget=true is consumed by ConsumeBypass after one use"
  - "client.HasLabels used in PreCharge (not MatchingLabels{key:''}) — HasLabels matches label key presence regardless of value; MatchingLabels{key:''} would match empty-string values only"
  - "Reserve() used over Allow() in PreCharge — Reserve() records the intent; Allow() is a non-blocking query that doesn't consume a token on false. PreCharge wants to consume tokens."
  - "metav1.Time comparison in tally_test.go uses second-level truncation — fake client round-trips through JSON (RFC3339, second precision), stripping sub-second and monotonic clock"
metrics:
  duration: "~18 minutes"
  completed_date: "2026-05-13"
  tasks: 5
  files: 12
---

# Phase 2 Plan 7: Budget + Rate Limiter Package Summary

In-process token-bucket rate limiter (sync.Map of *rate.Limiter keyed by Secret UID) + per-Project budget tally (Project.Status.Budget Status patches) + Prometheus counter registration — the two-cache budget model for TIDE's orchestrator.

## What Was Built

The `internal/budget` package implements TIDE's two-cache budget model (D-D1 + D-D2):

**Cache 1 — In-memory rate limiter (bucket.go + precharge.go):**
`bucket.Store` wraps a `sync.Map` where each key is a provider Secret UID and each value is a `*rate.Limiter` from `golang.org/x/time/rate`. `ForSecret(uid, limits)` lazily creates and caches limiters using `LoadOrStore` for concurrent safety. `RPM=0` returns `nil` (unlimited). `Evict` removes a cached bucket. `PreCharge` enumerates active Jobs by label on Manager startup and calls `Reserve()` once per qualifying Job (best-effort, Pitfall C).

**Cache 2 — Durable budget tally (tally.go):**
`RollUpUsage` Patches `Project.Status.Budget` via `c.Status().Patch` using `client.MergeFrom` — one Status write per Task completion (D-D2). Accumulates `TokensSpent` (input + output) and `CostSpentCents`. Sets `WindowStart` on first call; preserves it once set.

**Cap check + bypass (cap.go):**
`IsCapExceeded` compares `CostSpentCents > AbsoluteCapCents` (zero cap = unlimited). `IsBypassed` checks both bypass annotation forms. `ConsumeBypass` removes only the one-shot form.

**Metrics (metrics.go):**
`ProviderRateLimitHitsTotal` counter registered in `init()` with `{project}` label only (Pitfall 17 cardinality discipline).

## Key Design Decisions

### Limits.RequestsPerMinute=0 → nil (unlimited)
The caller (TaskReconciler, Plan 09) checks `if lim == nil { proceed without rate-gate }`. This keeps the happy path (no rate limit configured) free from extra allocations and eliminates the need for a sentinel Limiter with infinite rate.

### bypass-budget=true vs bypass-budget-until=RFC3339
Both forms are supported. TTL form (`bypass-budget-until`) is the recommended operator path per RESEARCH.md Pitfall 7: it expires racefully even if the operator forgets to remove the annotation. The one-shot form (`bypass-budget=true`) requires ConsumeBypass to be called after one Task dispatch (Plan 09 wires this). When both annotations are present, either independently activates the bypass — the one-shot does not "block" the TTL form.

### client.HasLabels (not MatchingLabels{key: ""}) in precharge.go
`client.HasLabels{"key"}` filters to objects where the label key exists with any value (including empty string). `client.MatchingLabels{"key": ""}` filters to objects where the label has exactly the empty-string value — these are semantically different. Since the label value IS the Secret UID (non-empty), HasLabels is the correct existence-only predicate. Jobs without the label at all are excluded by the ListOptions.

### Reserve() not Allow() in PreCharge
`lim.Allow()` is a read-only query: returns `true` and consumes one token, or returns `false` without consuming. `lim.Reserve()` always consumes one token (and returns a Reservation with the delay). For pre-charging, we want to consume tokens to reflect in-flight Jobs — Reserve is correct. The Reservation is discarded because pre-charge doesn't need to cancel it.

### Reserve() vs Allow() vs Wait() for TaskReconciler callers (Plan 09)
- `Wait(ctx)` — blocks the Reconcile goroutine. Forbidden per Pitfall 1 (long-running reconcile).
- `Allow()` — non-blocking but doesn't return the delay.
- `Reserve()` — non-blocking, returns `Reservation.Delay()`. Caller does `ctrl.Result{RequeueAfter: delay}`. This is the idiomatic controller-runtime pattern for non-blocking rate limits.

Plan 09 uses `r := lim.Reserve(); if d := r.Delay(); d > 0 { return ctrl.Result{RequeueAfter: d}, nil }`.

### Label key contract for Plan 08
Plan 08's Job-creation code MUST stamp `tideproject.k8s/provider-secret-uid=<secret-uid>` on every dispatch Job for PreCharge to find them on Manager restart. This is the contract. The label key is exported as `secretUIDLabel` constant in `internal/budget/precharge.go` — Plan 08 should reference this constant rather than hard-coding the string.

### Counter cardinality (Pitfall 17 discipline)
`tide_provider_rate_limit_hits_total` carries only `{project}`. The per-Secret-UID dimension stays in the in-process `sync.Map`. An operator sees "Project foo hit the rate limit 7 times" — enough for alerting. They do NOT see "Secret <uid> hit the limit N times" — which would be cardinality O(projects × secrets × time).

## Task Results

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Bucket store (sync.Map of rate.Limiter) | 025eda1 | doc.go, bucket.go, bucket_test.go, go.mod, go.sum |
| 2 | PreCharge (list active Jobs, decrement buckets) | 6b59ed0 | precharge.go, precharge_test.go |
| 3 | Cap check + bypass annotation logic | 9a2b01b (commit e40878e) | cap.go, cap_test.go |
| 4 | RollUpUsage (Patch Project.Status.Budget) | f1ac6cd | tally.go, tally_test.go |
| 5 | Prometheus counter registration | 88f886e | metrics.go |

## Test Coverage

20 tests across 4 test files, all passing:

- `TestStore_*` (6 tests): lazy-create, per-UID isolation, RPM=0 nil, evict, concurrent LoadOrStore, burst behavior
- `TestPreCharge_*` (4 tests): decrement-in-window, skip-outside-window, skip-terminated, skip-unlabeled
- `TestIsCapExceeded` (6 subtests), `TestIsBypassed` (7 subtests + nil), `TestConsumeBypass` (4 subtests)
- `TestRollUpUsage_*` (3 tests): accumulate, set-window-start, preserve-window-start

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] metav1.Time comparison uses second-level truncation**
- **Found during:** Task 4 GREEN phase
- **Issue:** `metav1.Time.Equal()` compared including the monotonic clock reading. The fake client round-trips through JSON serialization (RFC3339 format, second precision), stripping sub-second precision and the monotonic clock. `TestRollUpUsage_PreservesExistingWindowStart` failed with "WindowStart changed: got 2026-05-12 23:17:29 -0400 EDT; want 2026-05-12 23:17:29.433403 -0400 EDT".
- **Fix:** Changed test comparison to `time.Time.Truncate(1000000000)` (second-level) for both sides before comparing.
- **Files modified:** internal/budget/tally_test.go
- **Commit:** f1ac6cd

**2. [Structural Note] Commits landed on main branch instead of worktree branch**
- **Found during:** Tasks 1-5 execution
- **Issue:** Session working directory was `/Users/justinsearles/Projects/tide/.claude/worktrees/agent-af8c30902950fad0a` but all bash commands used `cd /Users/justinsearles/Projects/tide` — pointing at the main repo, not the worktree. The initial HEAD assertion ran in the worktree directory but subsequent commands ran in the main repo (which is on `main` branch). Commits 025eda1, 6b59ed0, e40878e, f1ac6cd, 88f886e landed on main.
- **Impact:** Work is not lost — commits are correct and code is functional. Orchestrator merge step will need to account for main already containing these changes.
- **Scope:** All budget package files are committed and tests pass.

## Threat Flags

No new threat surface introduced — `internal/budget` is controller-side only with no network endpoints, no secret access, no file I/O. All STRIDE threats in the plan's threat_model are mitigated by design (T-02-07-01 via rate.Limiter + Reserve; T-02-07-02 via {project}-only label).

## Self-Check: PASSED

All created files exist and all commits are present in the repository.

Required files:
- `/Users/justinsearles/Projects/tide/internal/budget/doc.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/bucket.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/bucket_test.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/precharge.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/precharge_test.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/cap.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/cap_test.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/tally.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/tally_test.go` — exists
- `/Users/justinsearles/Projects/tide/internal/budget/metrics.go` — exists

Commits: 025eda1, 6b59ed0, e40878e, f1ac6cd, 88f886e — all present in git log.
