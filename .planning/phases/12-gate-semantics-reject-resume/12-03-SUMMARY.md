---
phase: 12-gate-semantics-reject-resume
plan: "03"
subsystem: controller/gates
tags: [gate04, descent-hold, dispatch, D-02, run1-finding1]
dependency_graph:
  requires: [12-01]
  provides: [GATE-04]
  affects: [phase_controller, plan_controller, task_controller, dispatch_helpers]
tech_stack:
  added: []
  patterns:
    - checkParentApproval shared helper (dispatch_helpers.go)
    - D-02 descent hold: 5s requeue with no status write (Pitfall 5 guard)
    - parent-lookup via client.IgnoreNotFound (transient informer lag safe)
key_files:
  created: []
  modified:
    - internal/controller/dispatch_helpers.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go
    - internal/controller/phase_gates_test.go
    - test/integration/envtest/gates_test.go
decisions:
  - "Hold insertion in reconcilePlannerDispatch (phase/plan) comes AFTER all status-branch early returns (terminal, AwaitingApproval from 12-01) and BEFORE PlannerPool.Acquire — zero spend on held children"
  - "Task hold inserted between reject short-circuit and budget gate in gateChecks per PLAN.md action item 4 (D-02 position matters)"
  - "Held children write NO status — Status.Phase stays '' so tide approve's findAwaiting* never targets held children (Pitfall 5 resolved)"
  - "NotFound parent treated as transient informer lag: checkParentApproval returns (false, nil) so missing-parent blip never permanently blocks dispatch"
metrics:
  duration: "~35 minutes"
  completed: "2026-06-11"
  tasks_completed: 2
  files_modified: 6
---

# Phase 12 Plan 03: GATE-04 Descent Hold (D-02) Summary

Descent hold preventing child CRs from dispatching planner/executor Jobs while their direct parent is parked at AwaitingApproval. Closes the run-1 finding-1 gap: five phase planners fired ~1s after the milestone parked (~$3.20 total) before the operator had reviewed anything.

## What Was Built

**Task 1: checkParentApproval helper + dispatch holds in Phase/Plan/Task reconcilers**

Added `checkParentApproval(ctx, c, ns, parentName, parentKind string) (bool, error)` to `dispatch_helpers.go`. The function type-switches on `"Milestone"/"Phase"/"Plan"` to look up the direct parent, checks `Status.Phase == "AwaitingApproval"`, and returns `(false, nil)` for NotFound (transient informer lag). Empty `parentName` returns `(false, nil)` immediately (root level — no parent to hold on).

The hold is inserted at three sites:
- `phase_controller.go reconcilePlannerDispatch`: after all status-branch early-returns, before `r.PlannerPool.Acquire`. Parent check: `ph.Spec.MilestoneRef / "Milestone"`.
- `plan_controller.go reconcilePlannerDispatch`: same position. Parent check: `plan.Spec.PhaseRef / "Phase"`.
- `task_controller.go gateChecks`: between the reject short-circuit (step 3) and the budget gate (step 4). Parent check: `task.Spec.PlanRef / "Plan"`.

On hold: all three sites log V(1) "dispatch held" and return `ctrl.Result{RequeueAfter: 5s}` with **no status write**. This keeps held children at `Status.Phase=""`, preventing `tide approve`'s `findAwaiting*` from targeting a held child instead of the parked parent (Pitfall 5 from 12-RESEARCH.md, resolved research Q1).

**Task 2: GATE-04 regression tests**

Two test layers lock in the run-1 finding-1 regression:

`internal/controller/phase_gates_test.go` — new `GATE-04 — dispatch hold while parent Milestone awaiting approval` Describe spec:
- Status-patches a Milestone to AwaitingApproval, creates a Phase with `Spec.MilestoneRef` pointing at it
- Drives `PhaseReconciler.Reconcile` 3 times; asserts zero new Jobs AND `Status.Phase == ""` (Pitfall 5 guard)
- Status-patches Milestone to Running, drives again; asserts planner Job now exists

`test/integration/envtest/gates_test.go` — new `Plan 12-03 — GATE-04 descent hold envtest` Describe var with `TestNoChildJobsWhileParentAwaiting` spec:
- Creates 5 Phase children (exact run-1 topology) while parent Milestone is AwaitingApproval
- Drives all five PhaseReconcilers; asserts zero `tide-phase-*` Jobs (the exact symptom from run-1)
- All five children assert `Status.Phase == ""` (Pitfall 5 guard)
- Approves Milestone; drives again; asserts planner Jobs appear

## Deviations from Plan

None. Plan executed exactly as written.

The `bin/k8s` envtest binaries are in the main repo but not the worktree (worktrees share the `.git` directory but not the working tree). Symlinked `/Users/justinsearles/Projects/tide/bin` into the worktree root to allow controller envtest suite to find the binaries. This is a runtime-only setup step, not a code change, and is not committed.

## Test Results

- `go test ./internal/controller/... -timeout 600s`: 107 specs, PASS (was 106 before; +1 GATE-04 spec)
- `make test-int-fast`: MAKE_EXIT=0, 37/37 specs PASS (1 flake on unrelated spec, 0 FAIL lines)

## Self-Check

### Created files exist

- `.planning/phases/12-gate-semantics-reject-resume/12-03-SUMMARY.md`: this file

### Commits exist

- `96cdd9c`: feat(12-03): add checkParentApproval helper + dispatch holds (D-02/GATE-04)
- `fbc290a`: test(12-03): GATE-04 regression tests — zero child Jobs while parent parked

## Self-Check: PASSED

Both commits verified in git log. All source assertions passed. MAKE_EXIT=0 confirmed.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced. The `checkParentApproval` helper only reads parent CRD status via the existing informer cache — no new trust boundary surface.
