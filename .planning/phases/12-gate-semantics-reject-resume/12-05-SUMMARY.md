---
phase: 12-gate-semantics-reject-resume
plan: "05"
subsystem: controller/gates
tags: [tdd, gap-closure, gates, plan-level, cr-01, cr-02, gate-01, gate-04, envtest]
dependency_graph:
  requires: [12-01, 12-03]
  provides: [plan-awaiting-approval-park, plan-gate-hold-wave-path, cr-01-closed, cr-02-closed]
  affects: [plan_controller, plan_gates_test, task_controller checkParentApproval]
tech_stack:
  added: []
  patterns:
    - AwaitingApproval early-return in reconcilePlannerDispatch (before tasks-exist List) — milestone parity
    - alreadyApproved guard on gate-policy hook (ConditionWaveOrLevelPaused False + ReasonApprovedByUser/ResumedByUser)
    - Reject short-circuit hoisted to top of handlePlannerJobCompletion — milestone parity
    - Gate hook repositioned before ChildCount requeue to close gate-bypass path for ChildCount>0 Plans
key_files:
  created: []
  modified:
    - internal/controller/plan_controller.go
    - internal/controller/plan_gates_test.go
decisions:
  - AwaitingApproval early-return placed before tasks-exist List (deliberate divergence from milestone analog placement); reason: a parked Plan with reporter-materialized Tasks would take the tasks-exist exit to dispatched=false, routing to reconcileWaveMaterialization while parked
  - Test fixture correction: reconcileN returns (ctrl.Result, error); used _, err pattern rather than .To(Succeed()) on two-return-value call — committed in same GREEN commit per Rule 1
metrics:
  duration: ~17 minutes
  completed: "2026-06-11"
  tasks_completed: 2
  files_changed: 2
---

# Phase 12 Plan 05: Plan Gate Bypass Gap Closure (CR-01/CR-02) Summary

Gap closure plan for two Phase 12 verifier-confirmed defects: CR-01 (plan-level gate structurally unreachable for ChildCount>0 Plans) and CR-02 (parked Plan oscillates back to Running). Three surgical edits to plan_controller.go with TDD RED→GREEN cycle.

## What Was Built

**Plan-level approve gate parity with Milestone/Phase** — `Project.Spec.Gates.Plan=approve` now correctly holds executor spend until review for both leaf (ChildCount==0) and non-leaf (ChildCount>0) Plans. A parked Plan stays parked across all reconcile paths.

### Three Edits to plan_controller.go

**Edit 1 — AwaitingApproval early-return in `reconcilePlannerDispatch` (closes CR-02 + wave-path half of CR-01):**
- Inserted at the very top of the function, before the tasks-exist `r.List` call
- No annotation present → `return ctrl.Result{}, true, nil` (hold; dispatched=true suppresses `reconcileWaveMaterialization`)
- Annotation present → consume + metadata Patch → status Patch Phase="Running"+ApprovedByUser condition → `return ctrl.Result{Requeue: true}, true, nil`
- This placement is a deliberate divergence from the milestone analog (where Step 1a sits after the terminal short-circuit): a parked Plan with reporter-materialized Tasks would otherwise take the tasks-exist early exit to `dispatched=false`, routing the reconcile to `reconcileWaveMaterialization` while parked

**Edit 2 — Reject short-circuit hoisted to top of `handlePlannerJobCompletion` (milestone parity):**
- `CheckRejected` check moved from after the ChildCount requeue to immediately after project resolution, before `EnvReader.ReadOut`
- Mirrors `milestone_controller.go:442-449` ("operator stop should always halt")

**Edit 3 — Gate-policy hook moved before ChildCount requeue (closes hook-unreachable half of CR-01):**
- `EvaluatePolicy` block relocated from after the ChildCount requeue to between the ValidationState=Validated stamp and the `expected := out.ChildCount` gate
- Added `alreadyApproved` guard: checks `ConditionWaveOrLevelPaused` with Status=False and Reason=ApprovedByUser/ResumedByUser — prevents re-parking after the Edit-1 branch has already approved the level
- Annotation-present-at-hook path: consume + metadata Patch + status Patch Running+ApprovedByUser, then fall through to ChildCount gate
- Mirrors `milestone_controller.go:510-553`

### Regression Specs

**Test 6d (GATE-01/GATE-04 / CR-01):**
- Plan with gates.plan=approve, ChildCount=2 planner output: parks at AwaitingApproval
- 2 reporter-style Tasks materialized while parked: Plan stays AwaitingApproval (tasks-exist path blocked by Edit 1)
- TaskReconciler driven on both tasks: zero executor Jobs, zero Wave CRs (checkParentApproval(kind=Plan) hold live)
- Approve annotation consumed: Plan returns Running+ApprovedByUser; Task hold lifts; executor Job dispatched

**Test 6e (CR-02):**
- Leaf Plan (ChildCount=0) parks at AwaitingApproval
- 3 re-reconciles with no annotation: Plan stays AwaitingApproval (no Running stomp)

**`driveToJobCompletion` parameterized** with `childCount int`; existing Tests 6a/6b/6c call sites updated to pass 0 (behavior unchanged).

## Deviations from Plan

**1. [Rule 1 - Bug] Test fixture: reconcileN return-value usage**
- **Found during:** Task 2 GREEN run
- **Issue:** `Expect(reconcileN(...)).To(Succeed())` fails because `reconcileN` returns `(ctrl.Result, error)` — Gomega's `Succeed()` expects a single error value
- **Fix:** Changed to `_, err := reconcileN(...); Expect(err).NotTo(HaveOccurred())` matching the pattern in `task_gates_test.go:74`
- **Files modified:** `internal/controller/plan_gates_test.go`
- **Commit:** c021c3b (included in GREEN commit per plan spec)

No other deviations. Plan executed exactly as specified for the controller changes.

## Verification Results

`make test` MAKE_EXIT=0, zero `^--- FAIL|^FAIL\s` lines, 111 Ginkgo specs passed.

Acceptance criteria confirmed:
- AwaitingApproval branch at line 215 < taskPlanRefIndexKey List at line 251 (Edit 1 position)
- EvaluatePolicy at line 537 < `expected := out.ChildCount` at line 591 (Edit 3 position)
- CheckRejected at line 444 < EnvReader.ReadOut at line 464 (Edit 2 position)
- ReasonApprovedByUser appears 3 times in plan_controller.go (Edit-1 consume + Edit-3 guard + Edit-3 consume)

## Self-Check: PASSED

- `internal/controller/plan_controller.go` exists and modified
- `internal/controller/plan_gates_test.go` exists and modified
- RED commit `fb31aad` exists: `test(12-05): add failing regression specs for plan gate bypass (CR-01/CR-02)`
- GREEN commit `c021c3b` exists: `fix(12-05): park Plan at approve gate before ChildCount wait; hold wave path while parked (CR-01/CR-02)`
