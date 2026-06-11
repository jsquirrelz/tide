---
phase: 14-budget-enforcement-pricing
plan: "02"
subsystem: budget
tags: [budget, conditions, controller-runtime, go, reservation]
completed_date: "2026-06-11"
duration_seconds: 314
tasks_completed: 3
tasks_total: 3
files_created: 4
files_modified: 1
dependency_graph:
  requires: []
  provides:
    - api/v1alpha1: ConditionBudgetBlocked, ReasonBudgetCapReached, ReasonBudgetCapCleared
    - internal/budget/reservation.go: ReservationStore, NewReservationStore, RederiveReservations
    - internal/controller/budget_blocked.go: checkBudgetBlocked, setBudgetBlockedIfNeeded
  affects:
    - Plans 14-03, 14-05 (dispatch gate wiring)
tech_stack:
  added: []
  patterns:
    - sync.Map-backed in-process store (same as budget.Store in bucket.go)
    - meta.SetStatusCondition + client.MergeFrom status patch (condition-stamp pattern)
    - client.HasLabels{label} for rederivation scan
    - Nil-receiver-safe store methods (guard: if s == nil)
    - Bidirectional condition setter (set on exceed, clear on cap-raise recovery)
key_files:
  created:
    - internal/budget/reservation.go
    - internal/budget/reservation_test.go
    - internal/controller/budget_blocked.go
    - internal/controller/budget_blocked_test.go
  modified:
    - api/v1alpha1/shared_types.go
decisions:
  - "setBudgetBlockedIfNeeded is bidirectional (set + clear) — without the clear path, a cap-raise would never recover dispatch because the gate parks on the stale True condition"
  - "All ReservationStore methods are nil-receiver-safe — existing test constructors across the suite do not set the new field and must not panic (pre-emptive Rule 2)"
  - "HasHeadroom blocks at >= cap (strict less-than) per D-05 inequality"
  - "RederiveReservations skips pre-Phase-14 Jobs missing the estimated-cost label conservatively (Pitfall 5)"
requirements:
  - BUDGET-02
  - BUDGET-03
---

# Phase 14 Plan 02: BudgetBlocked Condition Vocabulary + ReservationStore Summary

BudgetBlocked condition constants, nil-safe ReservationStore with restart rederivation, and bidirectional check/set/clear condition helpers — everything Plan 14-03 needs to wire the dispatch gate.

## Tasks Completed

| # | Task | Commit | Files |
|---|------|--------|-------|
| 1 | Phase 14 condition constants in shared_types.go | ecf6c70 | api/v1alpha1/shared_types.go |
| 2 | ReservationStore with nil-safe methods + restart rederivation | b5ccff2 | internal/budget/reservation.go, internal/budget/reservation_test.go |
| 3 | budget_blocked.go — check + bidirectional set/clear helpers | 34b243c | internal/controller/budget_blocked.go, internal/controller/budget_blocked_test.go |

## What Was Built

### Task 1: Condition vocabulary (api/v1alpha1/shared_types.go)

Appended a Phase 14 const block immediately after the Phase 13 BillingHalt block:

- `ConditionBudgetBlocked = "BudgetBlocked"` — operator cap exhausted; new dispatch halted project-wide
- `ReasonBudgetCapReached = "BudgetCapReached"` — stamped by setBudgetBlockedIfNeeded on cap breach
- `ReasonBudgetCapCleared = "BudgetCapCleared"` — stamped when cap is raised and condition flips to False

Block doc comment explicitly states BudgetBlocked is DISTINCT from BillingHalt (operator cap vs provider billing auth) and both may be True simultaneously. No new Status.Phase enum values (D-04: BudgetExceeded phase machinery unchanged).

### Task 2: ReservationStore (internal/budget/reservation.go + tests)

In-process sync.Map-backed store, taskUID → int64 cents:

- `Reserve(uid, cents)` — records estimated cost pre-dispatch
- `Settle(uid)` — removes on task completion (actual cost rolled up by RollUpUsage)
- `Release(uid)` — removes on terminal failure (headroom recovered)
- `TotalReserved()` — sums all in-flight reservations
- `HasHeadroom(project, estimate)` — returns `committed+estimate < cap` (strict less-than, D-05); permissive for nil project, nil store, zero/negative cap
- `RederiveReservations(ctx, c, store)` — scans active Jobs via `client.HasLabels{reservedCostLabel}` on restart; skips terminated, malformed label values, missing labels (pre-Phase-14 pitfall 5)

All 5 methods have nil-receiver guards. PERSIST-02 contract documented: reservations are in-process only, never CRD status, rederivable from Job labels.

11 tests: 3 basic CRUD, 7 HasHeadroom table cases (including nil-project, nil-store, zero-cap, at-cap, over-cap), 1 nil-receiver safety, 3 rederivation fake-client cases. Green under -race.

### Task 3: budget_blocked.go + tests (internal/controller/)

Two functions in package controller:

- `checkBudgetBlocked(project)` — returns true iff BudgetBlocked=True condition present; nil-safe (returns false for nil)
- `setBudgetBlockedIfNeeded(ctx, c, project, reservedCents)` — **bidirectional**:
  - When `IsCapExceeded` true and condition absent/False → patches BudgetBlocked=True with ReasonBudgetCapReached, message includes spent/reserved/cap cents
  - When `IsCapExceeded` true and condition already True → no-op (idempotent)
  - When `IsCapExceeded` false and condition currently True → patches to False with ReasonBudgetCapCleared (cap-raise recovery)
  - When `IsCapExceeded` false and condition absent → no-op
  - Nil project → no-op, nil error

9 tests via fake client (no envtest needed); all behaviors and recovery path covered.

## Deviations from Plan

### Deliberate Extension: Extra test case (Rule 2 — missing critical coverage)

The plan specified 8 test cases for setBudgetBlockedIfNeeded. Added a 9th:
`TestSetBudgetBlockedIfNeeded_CapRaised_ClearsCondition` — explicitly covers the bidirectional clear path (cap-raise recovery), since the plan marks this as a critical deliberate extension. File count is correct; the test mirrors the PATTERNS.md "Cases to cover" list with the cap-raise case added.

None — plan executed as written (deliberate skeleton extension for bidirectional clear path was the plan's explicit intent, not a deviation).

## Known Stubs

None — all exported symbols are fully implemented with real behavior.

## Threat Flags

No new threat surface beyond what the plan's threat model documents (T-14-04, T-14-05, T-14-06, T-14-SC). No new network endpoints, auth paths, or file access patterns introduced.

## Self-Check: PASSED
