---
phase: 27-budget-bypass-resume-correctness
plan: "02"
subsystem: controller
tags: [bypass, resume, clone-idempotency, budget, envtest, tdd]
dependency_graph:
  requires: ["27-01"]
  provides: ["BYPASS-01-fix", "BYPASS-02-fix", "clone-idempotency-envtest", "bypass-phase-running-envtest"]
  affects: ["internal/controller/project_controller.go", "internal/controller/project_controller_test.go", "internal/controller/project_clone_idempotency_test.go"]
tech_stack:
  added: []
  patterns: ["client.MergeFrom + r.Status().Patch status patch idiom", "Ginkgo Label(envtest) + Consistently/Eventually assertions", "TDD RED/GREEN/REFACTOR"]
key_files:
  created:
    - internal/controller/project_clone_idempotency_test.go
  modified:
    - internal/controller/project_controller.go
    - internal/controller/project_controller_test.go
decisions:
  - "D-01: bypass of initialized project (BranchName != \"\") sets PhaseRunning not PhasePending"
  - "D-02: clone dispatch gated on !CloneComplete flag, set only at observed Succeeded>0"
  - "Belt-and-suspenders guard in reconcileProjectPhase2 Step 3: BranchName guard short-circuits to reconcilePhase3Lifecycle"
metrics:
  duration: "~9m"
  completed_date: "2026-06-18"
  tasks: 2
  files: 3
---

# Phase 27 Plan 02: BYPASS-01 + BYPASS-02 Controller Fixes Summary

BranchName-conditional bypass target phase and durable CloneComplete dispatch guard make budget-halted-then-resumed projects idempotent against TTL-GC'd Job existence checks.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | BYPASS-01 failing test | 49df5cf | project_controller_test.go |
| 1 (GREEN) | BYPASS-01 fix in controller | f9f2172 | project_controller.go |
| 2 (RED) | BYPASS-02 failing tests | e080d2c | project_clone_idempotency_test.go |
| 2 (GREEN) | BYPASS-02 fix in controller | 33fe47b | project_controller.go |

## What Was Built

### Task 1: BYPASS-01 (f9f2172)

Fixed `handleBudgetGate` at line ~1257 in `project_controller.go`:

**Before:** `project.Status.Phase = tidev1alpha2.PhasePending` (unconditional)

**After:** BranchName-conditional — `PhaseRunning` when `BranchName != ""`, `PhasePending` otherwise.

Added belt-and-suspenders guard in `reconcileProjectPhase2` Step 3 (~line 345): when `BranchName != ""`, skip init-Job dispatch entirely and call `reconcilePhase3Lifecycle` directly. This prevents a TTL-GC'd init Job from triggering a destructive workspace re-init on resume.

Extended `TestProjectReconciler_BypassAnnotation_ClearsBudgetExceeded` in `project_controller_test.go` to:
1. Patch `Status.Git.BranchName` to a non-empty value before reconcile
2. Assert `fetched.Status.Phase == PhaseRunning` (positive assertion alongside existing `NotEqual(BudgetExceeded)`)

### Task 2: BYPASS-02 (33fe47b)

Fixed `reconcilePhase3Lifecycle` Step 3 clone dispatch in `project_controller.go`:

**Before:** `if apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && ...`

**After:** `if !project.Status.Git.CloneComplete && apierrors.IsNotFound(cloneErr) && ...`

Added clone-success detection block after the existing dispatch block:
```go
if cloneErr == nil && existingClone.Status.Succeeded > 0 && !project.Status.Git.CloneComplete {
    patch := client.MergeFrom(project.DeepCopy())
    project.Status.Git.CloneComplete = true
    r.Status().Patch(ctx, project, patch)
}
```

Created `project_clone_idempotency_test.go` with two `Label("envtest")` specs:
- **Spec 1**: patches `CloneComplete=true` + `BranchName` before reconcile, asserts clone Job is never created (`Consistently` + `not found` matcher)
- **Spec 2**: advances project to clone-dispatch point, patches clone Job to succeeded, drives reconciles, then `Eventually` asserts `CloneComplete==true`

## Deviations from Plan

None — plan executed exactly as written. No architectural changes required.

## Verification Results

- `grep -c 'CloneComplete' internal/controller/project_controller.go` = 7 (≥ 2 required: guard + set-on-success)
- Bypass test asserts `PhaseRunning`: `project_controller_test.go:565`
- Clone idempotency envtest exists with `Label("envtest")`, asserts no re-clone when `CloneComplete=true` and asserts set-on-success
- Full Layer A envtest suite: `ok github.com/jsquirrelz/tide/internal/controller 73.367s`

## TDD Gate Compliance

Both tasks followed RED → GREEN:

1. Task 1 RED: `49df5cf` — test committed, confirmed FAIL (`Expected Pending to equal Running`)
2. Task 1 GREEN: `f9f2172` — implementation committed, confirmed PASS
3. Task 2 RED: `e080d2c` — tests committed, confirmed FAIL (both specs, as expected)
4. Task 2 GREEN: `33fe47b` — implementation committed, confirmed PASS

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries were introduced. Both changes are purely in-controller status patch logic using existing `client.MergeFrom + r.Status().Patch` pattern. The `CloneComplete` field was added to the CRD schema in Plan 27-01; this plan only adds the guard and set-on-success logic that uses it.

## Self-Check: PASSED

- FOUND: internal/controller/project_clone_idempotency_test.go
- FOUND: internal/controller/project_controller.go
- FOUND: internal/controller/project_controller_test.go
- FOUND: .planning/phases/27-budget-bypass-resume-correctness/27-02-SUMMARY.md
- FOUND commit: 49df5cf (test RED BYPASS-01)
- FOUND commit: f9f2172 (feat GREEN BYPASS-01)
- FOUND commit: e080d2c (test RED BYPASS-02)
- FOUND commit: 33fe47b (feat GREEN BYPASS-02)
