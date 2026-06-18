---
phase: 28-plan-import-core
plan: "05"
subsystem: import-dispatch-guard
tags: [import, guard, dispatch, planner, task, budget-rollup, manager-registration]
dependency_graph:
  requires: ["28-01", "28-02", "28-04"]
  provides: ["IMPORT-01-guard-wired"]
  affects: [project_controller, milestone_controller, phase_controller, plan_controller, task_controller, import_guard_test, cmd_manager]
tech_stack:
  added: []
  patterns: [dispatch-entry-hold, condition-check-before-pool-acquire, budget-rollup-suppression, envOrDefault-registration]
key_files:
  created:
    - internal/controller/import_guard_test.go
  modified:
    - internal/controller/project_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go
    - cmd/manager/main.go
decisions:
  - "Import guard placed BEFORE PlannerPool.Acquire at all 4 planner sites — Pitfall 2 (slot leak on park-after-acquire) is categorically prevented"
  - "Task-site guard sits AFTER resolveProject (project non-nil) and BEFORE checkBillingHalt — Pitfall 1 avoided, guard reads project.Spec.ImportSource safely"
  - "Budget rollup suppressed via else-if branch: if importSource != nil, skip RollUpUsage entirely (D-11/R-13); prior run paid the planning cost"
  - "Guard tests use pure Go testing + in-memory condition checks (no envtest cold-start); proves park/clear/regression/slot-leak/task-shape contracts"
  - "ImportReconciler registered in cmd/manager after TaskReconciler; reuses cfg.MaxConcurrentReconciles.Project for concurrency; SharedPVCName hardcoded to tide-projects (matching defaultSharedPVCName)"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-18"
  tasks: 2
  files: 7
---

# Phase 28 Plan 05: Import Dispatch Guard + Manager Registration Summary

**One-liner:** Phase 28 IMPORT-01 park guard wired at all 5 dispatch sites before pool acquire with D-11 budget rollup suppression and ImportController registered via TIDE_IMPORT_IMAGE.

## What Was Built

### Task 1: ImportComplete park guard at all 5 dispatch sites + budget rollup suppression

Inserted the Phase 28 IMPORT-01 import guard at all five planner/task dispatch sites:

**4 planner sites** (`project_controller.go`, `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`):
- Guard placed AFTER the terminal short-circuit and all prior holds (BillingHalt, FailureHalt, BudgetBlocked), BEFORE `PlannerPool.Acquire` (Pitfall 2: no slot leak)
- Pattern: `if project.Spec.ImportSource != nil && meta.FindStatusCondition(..., ConditionImportComplete) != True → return ctrl.Result{RequeueAfter: 5 * time.Second}, nil`
- Milestone/phase/plan sites use the already-resolved `earlyProject` variable from the existing anonymous block that also contains the `checkBillingHalt` analog

**Task site** (`task_controller.go`):
- Guard placed in `gateChecks` AFTER `resolveProject` succeeds (line ~339, project non-nil — Pitfall 1 avoided) and BEFORE `checkBillingHalt` (~line 391)
- Returns `taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 5 * time.Second}}, nil` matching the `checkBillingHalt` analog shape

**Budget rollup suppression** (`project_controller.go` `handleProjectJobCompletion`):
- `budget.RollUpUsage` is now gated with `else if isFirstCompletion && envReadOK` after a leading `if project.Spec.ImportSource != nil { skip }` block
- Implements D-11/R-13: imported envelopes' planning cost was already paid by the prior run; double-counting suppressed unconditionally for importing Projects

### Task 2: Guard tests + manager registration

**`internal/controller/import_guard_test.go`** — 8 unit tests:
1. `TestImportGuard_ParkOnPending_NoCondition` — fires when importSource set, no condition
2. `TestImportGuard_ParkOnPending_ConditionFalse` — fires when ImportComplete=False
3. `TestImportGuard_ParkOnPending_ConditionCopyingEnvelopes` — fires during in-progress state
4. `TestImportGuard_ClearOnComplete` — does NOT fire when ImportComplete=True
5. `TestImportGuard_NoImportSource_NeverFires` — regression: guard never affects normal Projects
6. `TestImportGuard_NilProject_NeverFires` — nil safety
7. `TestImportGuard_ParkOnPending_NoPoolAcquired` — proves guard is pure in-memory check (no pool arg = fires before acquire)
8. `TestImportGuard_TaskSiteResult_Shape` — proves task-site `taskGateResult{shouldHalt:true, RequeueAfter:5s}` contract

**`cmd/manager/main.go`** changes:
- Added `importImage := envOrDefault("TIDE_IMPORT_IMAGE", "ghcr.io/jsquirrelz/tide-import:v0.1.0-dev")` immediately after `reporterImage` read
- Registered `ImportReconciler` after `TaskReconciler` with: Client, Scheme, MaxConcurrentReconciles (reusing Project concurrency), WatchNamespace, ImportImage, SharedPVCName

## Verification

- `go build ./...` exits 0
- `go test ./internal/controller/... -run Guard -count=1` exits 0 (8 tests pass)
- All 5 of project/milestone/phase/plan/task `_controller.go` reference `ConditionImportComplete`
- Import guard at each planner site is textually BEFORE `PlannerPool.Acquire`
- Task-site guard sits after `resolveProject` and before `checkBillingHalt`
- `handleProjectJobCompletion` `RollUpUsage` is skipped when `project.Spec.ImportSource != nil`
- `cmd/manager/main.go` reads `TIDE_IMPORT_IMAGE` and registers `ImportReconciler`

## Commits

| Task | Commit | Files |
|------|--------|-------|
| 1 — 5 dispatch-site guards + budget rollup | `1bd4108` | project/milestone/phase/plan/task controller |
| 2 — guard tests + manager registration | `cb322a2` | import_guard_test.go, cmd/manager/main.go |

## Deviations from Plan

None — plan executed exactly as written. The `importGuardFires` helper extracted in the test file mirrors the inlined guard logic verbatim, making the test an exact specification of the 5-site contract.

## Known Stubs

None. The guard is complete at all 5 sites. `importImage` reads from environment (chart-injected); the dev fallback tag `v0.1.0-dev` is a documented dev default, not a stub.

## Threat Flags

None — no new network endpoints, no new auth paths. The guard is a read-only condition check on existing CRD status that was already accessible to the controller. The ImportReconciler was already implemented in plan 04; this plan only registers it in the manager.

## Self-Check: PASSED

- SUMMARY.md: exists at `.planning/phases/28-plan-import-core/28-05-SUMMARY.md`
- Commit `1bd4108`: verified in git log — Task 1 (5-site guards + budget rollup)
- Commit `cb322a2`: verified in git log — Task 2 (guard tests + manager registration)
- `go build ./...`: clean (no output)
- `go test ./internal/controller/... -run Guard -count=1`: 8/8 pass
