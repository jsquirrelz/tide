---
phase: 12-gate-semantics-reject-resume
plan: "04"
subsystem: controller/gates
tags: [tdd, gates, reject, resume, d05, envtest]
dependency_graph:
  requires: [12-01, 12-03]
  provides: [reject-park-semantics, resume-01-re-dispatch]
  affects: [milestone_controller, phase_controller, plan_controller, task_controller, envtest]
tech_stack:
  added: []
  patterns:
    - patchXxxRejected park helper (no Status.Phase write, RequeueAfter:5s, ConditionWaveOrLevelPaused/RejectedByUser)
    - D-05 dispatch-entry reject hold before PlannerPool.Acquire
key_files:
  created: []
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go
    - internal/controller/milestone_gates_test.go
    - internal/controller/phase_gates_test.go
    - internal/controller/plan_gates_test.go
    - internal/controller/task_gates_test.go
    - internal/controller/boundary_push_test.go
    - test/integration/envtest/gates_test.go
decisions:
  - "D-05 dispatch-entry hold fires before pool acquire — no Job is ever created under reject; this affects tests that assumed a Job existed for `handleJobCompletion` to process"
  - "RESUME-01 terminal short-circuit bypass is proven by Status.Phase exiting 'Failed' (not by Job existence, which is vacuously true due to D-B5 dedup)"
  - "boundary_push Test 6 updated: planner Job never created under D-05 dispatch-entry hold, so makeFakeJobTerminal removed; push Job absence is still proven"
metrics:
  duration: "~3h (continuation session)"
  completed_date: "2026-06-11"
  tasks_completed: 3
  files_modified: 10
---

# Phase 12 Plan 04: Reject-Recoverable Park Semantics (D-05) Summary

`tide reject` now parks levels with a `RejectedByUser` condition instead of fail-marking them — four `patchXxxRejected` helpers, two dispatch-entry holds, and full envtest coverage including RESUME-01 retry-failed re-dispatch proof.

## Tasks Completed

| Task | Description | Commit | Files |
|------|-------------|--------|-------|
| 1 | `patchMilestoneRejected` + `patchPhaseRejected` helpers; dispatch-entry holds; unit tests | `be82c7e` | milestone_controller.go, phase_controller.go, milestone_gates_test.go, phase_gates_test.go |
| 2 | `patchPlanRejected` + `patchTaskRejected` helpers; dispatch-entry hold; unit tests | `2692630` | plan_controller.go, task_controller.go, plan_gates_test.go, task_gates_test.go |
| 3 | envtest: `TestRejectHalts` park semantics + `RESUME-01` retry-failed re-dispatch spec | `74b3b1d` | test/integration/envtest/gates_test.go, boundary_push_test.go, *_gates_test.go |

## What Was Built

### Four `patchXxxRejected` Park Helpers

Each controller (Milestone, Phase, Plan, Task) gained a `patchXxxRejected` method:
- Sets `ConditionWaveOrLevelPaused=True/RejectedByUser` with the reject reason as message
- Returns `ctrl.Result{RequeueAfter: 5*time.Second}` so the reconciler polls for annotation removal
- Does NOT write `Status.Phase` — park state is condition-only, preserving recovery path

### D-05 Dispatch-Entry Holds

Added reject checks BEFORE `PlannerPool.Acquire` in each reconciler's dispatch function:
- **Milestone**: inline Project lookup in `reconcilePlannerDispatch`
- **Phase**: `r.resolveProject(ctx, ph)` before pool acquire
- **Plan**: `r.resolveProjectForPlan(ctx, plan)` before pool acquire (returns `res, true, err` signature)

When reject annotation is present at dispatch time, the level is immediately parked — no Job is ever created, no pool slot consumed.

The pre-existing reject check in `handleJobCompletion` is retained for in-flight Jobs (draining semantics per resolved discretion call). `patchXxxFailed` is unchanged and still fires for genuine planner-job failures.

### RESUME-01 envtest Spec

New `Describe("RESUME-01 — retry-failed status reset re-dispatches Failed Plan")` in `test/integration/envtest/gates_test.go`:
1. Creates Project + Milestone + Phase + Plan
2. Status-patches Plan to `Failed` (simulates wedge state)
3. Confirms terminal short-circuit holds (`Consistently` — no Job suffix `-2` created)
4. Applies `retryFailedLevels` recipe: `Status.Phase=""` + `Conditions=nil` + `ResumedByUser`
5. Drives reconciler; asserts `Status.Phase` exits `"Failed"` (bypass proof)
6. Asserts `ResumedByUser` condition persists after re-dispatch

### Updated TestRejectHalts

Restructured to use dispatch-entry hold semantics: reject annotation is set BEFORE Milestone creation, so no planner Job is ever created. Test drives reconcile directly, asserts park condition, clears annotation, verifies re-drive creates/dispatches Job.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Unit gate tests used `driveToJobCompletion` under dispatch-entry hold**
- **Found during:** Task 3 — running `go test ./internal/controller/...`
- **Issue:** `milestone_gates_test.go` Test 3, `phase_gates_test.go` Test 5c, `plan_gates_test.go` Test 6c all called `driveToJobCompletion` (which does `makeFakeJobTerminal`) with the reject annotation already set. With the D-05 dispatch-entry hold, no planner Job is created, so `makeFakeJobTerminal` failed with `Job not found`.
- **Fix:** Replaced the initial `driveToJobCompletion(...)` call with `waitForCacheSync` + `reconcileWithRetry(r.Reconcile, ..., 3)` in all three tests. Recovery path (after annotation clear) still uses `driveToJobCompletion` correctly.
- **Files modified:** milestone_gates_test.go, phase_gates_test.go, plan_gates_test.go
- **Commit:** `74b3b1d`

**2. [Rule 1 - Bug] boundary_push_test.go Test 6 had stale `makeFakeJobTerminal` call**
- **Found during:** Task 3 — `go test ./internal/controller/...`
- **Issue:** "Test 6: reject short-circuit skips push" applied the reject annotation before Milestone creation, then tried to `makeFakeJobTerminal` on the planner Job. D-05 dispatch-entry hold means no planner Job is created, so the call failed.
- **Fix:** Removed the `envReader.SetOut` + `makeFakeJobTerminal` + second `reconcileWithRetry` sequence. The reconciler parks the Milestone at the dispatch-entry hold; the push Job absence assertion still proves the contract.
- **Files modified:** boundary_push_test.go
- **Commit:** `74b3b1d`

**3. [Rule 1 - Bug] RESUME-01 `Consistently` block failed because background PlanReconciler created Job `-1` before Failed patch**
- **Found during:** Task 3 — running `make test-int-fast`
- **Issue:** The RESUME-01 spec created a Plan with `Status.Phase=""`, and the background PlanReconciler from `BeforeSuite` immediately created Job `tide-plan-<uid>-1`. By the time the test's `Consistently` block ran (after the `Failed` patch), Job `-1` already existed, failing the "no Job must exist" assertion.
- **Fix:** Changed the `Consistently` assertion to allow Job `-1` (pre-existing from initial dispatch) and only reject a Job with suffix `-2` or higher (which would indicate a re-dispatch while in `Failed` state). The terminal short-circuit is also verified by the final `Status.Phase != "Failed"` assertion.
- **Files modified:** test/integration/envtest/gates_test.go
- **Commit:** `74b3b1d`

## Verification

All tests pass:

```
ok  github.com/jsquirrelz/tide/internal/controller     51.397s
ok  github.com/jsquirrelz/tide/test/integration/envtest  32.175s  [38 passed]
```

### Grep Evidence (NO_REJECT_FAILMARK)

The dispatch-entry holds and `handleJobCompletion` reject sites all call `patchXxxRejected`, not `patchXxxFailed`:
- `milestone_controller.go`: `grep -c 'patchMilestoneRejected' internal/controller/milestone_controller.go` → 3 (2 call sites + 1 def)
- `phase_controller.go`: `grep -c 'patchPhaseRejected'` → 3
- `plan_controller.go`: `grep -c 'patchPlanRejected'` → 3
- `task_controller.go`: `grep -c 'patchTaskRejected'` → 3

## Known Stubs

None — all reject/park paths are fully wired. `patchXxxFailed` still exists and fires for genuine planner-Job failures.

## Threat Flags

None — no new network endpoints, auth paths, or schema changes introduced.

## Self-Check: PASSED

- `be82c7e` commit exists: confirmed
- `2692630` commit exists: confirmed
- `74b3b1d` commit exists: confirmed
- All modified files exist in working tree: confirmed
- `go test ./internal/controller/...` passes: confirmed (ok, 51.397s)
- `make test-int-fast` passes: confirmed (38/38, 32.175s)
