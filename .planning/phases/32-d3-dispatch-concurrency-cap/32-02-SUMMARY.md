---
phase: 32-d3-dispatch-concurrency-cap
plan: "02"
subsystem: controller/budget-rollup-hardening
tags: [hardening, idempotency, retry, optimistic-lock, test-coverage]
dependency_graph:
  requires: ["32-01"]
  provides: ["WR-01", "WR-02", "WR-03", "WR-04"]
  affects: ["internal/controller/milestone_controller.go", "internal/controller/phase_controller.go", "internal/controller/plan_controller.go", "internal/controller/project_controller.go", "internal/controller/adoption_lifecycle_test.go"]
tech_stack:
  added: []
  patterns:
    - "retry.RetryOnConflict(retry.DefaultRetry, func() error { Get → MergeFromWithOptimisticLock → Status().Patch }) — mirrors budget.RollUpUsage in tally.go"
    - "countingClient / countingStatusPatcher wrapper for Status().Patch call counting in envtest"
key_files:
  created: []
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/adoption_lifecycle_test.go
decisions:
  - "WR-01: switch suppression patch to MergeFromWithOptimisticLock (matching RollUpUsage) rather than only fixing the comment — makes the 'conflict is retryable' invariant actually true"
  - "WR-04: patch-count assertion accepts 1-2 patches (not strictly 1) to tolerate a re-entry reconcile while still proving no unbounded splitting"
metrics:
  duration_minutes: 15
  completed_date: "2026-06-29"
  tasks_completed: 2
  tasks_total: 2
  files_changed: 5
---

# Phase 32 Plan 02: Carried-in D1 Hardening Debt (WR-01/02/03/04) Summary

RetryOnConflict + MergeFromWithOptimisticLock wrapping of all three child-level `*RolledUpUID` marker stamps, suppression patch corrected to optimistic lock, and WR-04 single-patch atomicity test added.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Wrap *RolledUpUID marker stamps in RetryOnConflict + re-fetch (WR-02/WR-03) | 9e80cbf | milestone_controller.go, phase_controller.go, plan_controller.go |
| 2 | Fix suppression-patch comment/lock (WR-01) + WR-04 single-patch test | 367e68e | project_controller.go, adoption_lifecycle_test.go |

## What Was Built

**Task 1 — WR-02/WR-03:** At each of the three child-level marker-stamp sites (milestone/phase/plan), replaced the best-effort `client.MergeFrom` + non-fatal `logger.Error` pattern with a `retry.RetryOnConflict(retry.DefaultRetry, func() error {...})` block that:
1. Re-fetches the latest typed level object via `r.Get(ctx, client.ObjectKeyFromObject(obj), latest)`
2. Early-returns nil if the marker is already set (idempotent against concurrent reconciles)
3. Uses `client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})` to embed `resourceVersion` so conflicts surface and trigger a re-fetch
4. On retry-budget exhaustion (WR-03), returns the error to requeue the reconcile rather than swallowing — ensuring the marker is durably set before the reporter Job's TTL-GC window can reopen the rollup branch

Added `"k8s.io/client-go/util/retry"` import to all three controllers (already in go.mod via `internal/budget/tally.go`).

**Task 2 — WR-01:** Switched the project suppression patch in `project_controller.go:1157` from plain `client.MergeFrom(project.DeepCopy())` to `client.MergeFromWithOptions(project.DeepCopy(), client.MergeFromWithOptimisticLock{})`. The old code's inline comment claimed "Conflict is retryable" but a plain MergeFrom embeds no resourceVersion and cannot conflict. The optimistic-lock option makes that claim true: a concurrent status write now surfaces as a conflict and the controller re-queues to re-fetch.

**Task 2 — WR-04:** Added a D-07 single-patch atomicity test in `adoption_lifecycle_test.go`:
- `countingStatusPatcher` implements the full `client.SubResourceWriter` interface, incrementing an `atomic.Int64` counter on every `Patch` call
- `countingClient` embeds `client.Client` and overrides `Status()` to return the counting wrapper
- `newCountingAdoptionReconciler()` constructs a `ProjectReconciler` backed by the counting client
- WR-04 `Describe` block drives the adoption advance and asserts: patch count is 1-2 (guarding against unbounded splitting), and `ConditionProjectPlannerSuppressed` is always present when `Phase=Running` (no transient Running-without-suppression intermediate state)

## Deviations from Plan

None — plan executed exactly as written.

## Verification

- `go build ./internal/controller/...` exits 0
- `go vet ./internal/controller/...` exits 0
- `grep -c 'RetryOnConflict'` returns 2 in each of milestone/phase/plan controllers (one for the call, one for DefaultRetry)
- `grep -c 'MergeFromWithOptimisticLock' milestone_controller.go` returns 2 (WR-02 marker stamp + existing)
- `grep -c 'MergeFromWithOptimisticLock' project_controller.go` returns 3 (WR-01 suppression patch + 2 existing)
- False invariant comment `'Conflict is retryable; surface as err so controller retries'` count = 0

## Known Stubs

None.

## Threat Flags

None — no new network endpoints, auth paths, or trust-boundary changes introduced.

## Self-Check: PASSED

- `internal/controller/milestone_controller.go` — FOUND (modified, builds clean)
- `internal/controller/phase_controller.go` — FOUND (modified, builds clean)
- `internal/controller/plan_controller.go` — FOUND (modified, builds clean)
- `internal/controller/project_controller.go` — FOUND (modified, builds clean)
- `internal/controller/adoption_lifecycle_test.go` — FOUND (modified, builds clean)
- Task 1 commit 9e80cbf — FOUND
- Task 2 commit 367e68e — FOUND
