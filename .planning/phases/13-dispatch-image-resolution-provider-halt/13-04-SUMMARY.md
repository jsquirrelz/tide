---
phase: 13-dispatch-image-resolution-provider-halt
plan: "04"
subsystem: controller
tags: [billing-halt, dispatch-gate, backstop, regression, tdd]
dependency_graph:
  requires: [13-02]
  provides: [HALT-01-dispatch-entry-hold, HALT-01-backstop-classification]
  affects:
    - internal/controller/task_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/billing_halt_regression_test.go
    - internal/controller/task_gates_test.go
tech_stack:
  added: []
  patterns:
    - "Park-not-fail pattern: log V(1) + requeue 30s, no Status.Phase change, no per-level condition"
    - "Backstop non-fatal: setBillingHaltIfNeeded called after terminal patch, errors logged only"
    - "K8s 1.33 Job status: FailureTarget=True + startTime required before JobFailed=True"
key_files:
  created: []
  modified:
    - internal/controller/task_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/billing_halt_regression_test.go
    - internal/controller/task_gates_test.go
decisions:
  - "Planner controllers (milestone/phase/plan) use earlyProject variable for checkBillingHalt call, project variable for setBillingHaltIfNeeded; both are correct — the call-site grep counts 5 invocations of checkBillingHalt( and 5 of setBillingHaltIfNeeded(ctx,"
  - "markTaskJobFailed updated to set startTime + FailureTarget=True per K8s 1.33 Job status validation (deviation: API constraint discovery during test implementation)"
  - "backstop specificity test (non-billing failure must not set BillingHalt) requires markTaskJobFailed to fire handleJobCompletion — added call"
metrics:
  duration: "~90 minutes (continuation from previous session)"
  completed_date: "2026-06-11"
  tasks_completed: 2
  files_changed: 7
---

# Phase 13 Plan 04: BillingHalt Dispatch-Entry Hold + Backstop Summary

BillingHalt (HALT-01) wired as the third dispatch-entry hold at all five reconciler levels (task, milestone, phase, plan, project) with park-not-fail semantics, plus the envelope-failure backstop classifying billing reasons at all five job-completion sites.

## What Was Built

### Task 1: BillingHalt dispatch-entry hold at all five levels (RED + GREEN)

Inserted `checkBillingHalt` at each reconciler's dispatch-entry, positioned BEFORE pool/slot acquisition and BEFORE Job creation (Pitfall 2):

- **task_controller.go** `gateChecks`: after `checkParentApproval` hold, before `BudgetExceeded` gate
- **milestone_controller.go** planner dispatch: after `earlyProject` CheckRejected block
- **phase_controller.go** planner dispatch: after `earlyProject` CheckRejected + parent-approval block
- **plan_controller.go** planner dispatch: after `earlyProject` CheckRejected + parent-approval block
- **project_controller.go** `reconcileProjectPlannerDispatch`: before Step 8/9 pool/BuildOptions

Park semantics at every site: `ctrl.Result{RequeueAfter: 30 * time.Second}`, no `Status.Phase` change, no per-level condition written (avoids status flapping — operator signal is the single Project condition).

RED specs (task_gates_test.go tests 13a+13b, billing_halt_regression_test.go per-level holds) committed before implementation.

### Task 2: Reconciler backstop classification + run-1 regression (RED + GREEN)

Inserted `setBillingHaltIfNeeded` at each controller's envelope-failure interpretation site:

- **task_controller.go** `handleJobCompletion`: in `out.ExitCode != 0 || cap-hit` failure branch, after status patch, before budget rollup
- **milestone_controller.go** `handleJobCompletion`: when `envReadOK && out.ExitCode != 0`, after budget rollup
- **phase_controller.go** `handleJobCompletion`: same pattern
- **plan_controller.go** `handlePlannerJobCompletion`: when `out.ExitCode != 0` (no envReadOK sentinel — read error returns early)
- **project_controller.go** `handleProjectJobCompletion`: when `envReadOK && out.ExitCode != 0`, NOT the push-Job path

All calls are non-fatal (log error, continue) — the task/planner terminal patch always proceeds.

Run-1 regression in `billing_halt_regression_test.go`:
- **Leg 1**: billing failure stamps BillingHalt=True on Project
- **Leg 2**: sibling task B creates no Job while BillingHalt present
- **Leg 3**: dispatch resumes after `tide resume` (meta.RemoveStatusCondition) clears the condition
- **Specificity**: forced-failure Reason does NOT set BillingHalt

All 123 specs pass (119 pre-existing + 4 Task 1 RED + regression + specificity).

## Acceptance Criteria Results

```
$ grep -c 'checkBillingHalt(' ...five files... | awk '{s+=$2} END {print s}'
5
$ grep -c 'setBillingHaltIfNeeded(ctx,' ...five files... | awk '{s+=$2} END {print s}'
5
$ go test ./internal/controller/... -count=1
ok  (123 passed, 0 failed)
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] K8s 1.33 Job status validation requires FailureTarget=True + startTime**
- **Found during:** Task 2 GREEN test run
- **Issue:** `k8sClient.Status().Patch` for `batchv1.Job` with `JobFailed=True` condition rejected by K8s 1.33 API validation: "cannot set Failed=True condition without the FailureTarget=true condition" + "startTime is required for finished job"
- **Fix:** Updated `markTaskJobFailed` to set `j.Status.StartTime = &now` and include `batchv1.JobFailureTarget` condition (Status=True, Reason="PodFailed") before `batchv1.JobFailed`
- **Files modified:** `internal/controller/billing_halt_regression_test.go`
- **Commit:** 3231178

**2. [Rule 1 - Bug] markTaskJobFailed used silent error swallowing on Status.Patch**
- **Found during:** Task 2 first test run — all 4 regression specs failing with "timed out waiting for Failed condition"
- **Issue:** `_ = k8sClient.Status().Patch(...)` silenced the K8s 1.33 validation error; the patch never applied
- **Fix:** Added `Eventually` wait for the Job to exist before patching (race safety), changed `_ =` to `Expect(...).To(Succeed())` so API errors surface
- **Files modified:** `internal/controller/billing_halt_regression_test.go`
- **Commit:** 3231178

**3. [Rule 2 - Missing] Backstop specificity test missing markTaskJobFailed call**
- **Found during:** Task 2 implementation — the backstop specificity test never triggered handleJobCompletion because the Job wasn't terminal
- **Fix:** Added `markTaskJobFailed(t.UID)` call before `envReader.SetOut` in the specificity test's `It` block
- **Files modified:** `internal/controller/billing_halt_regression_test.go`
- **Commit:** 3231178

## Known Stubs

None.

## Threat Flags

None — no new network endpoints or trust boundaries introduced.

## Deferred Items

- `make test-int-fast` (Layer A envtest + chart tests): deferred — parallel agent 13-03 runs the kind harness on this host; running a second kind-layer would risk OOM (constrained VM). Layer A envtest (`go test ./internal/controller/...`) is green with 123/123 specs. Full `make test-int` should run once 13-03 completes and the kind cluster is available.

## Self-Check

Checking file existence:
- `internal/controller/task_controller.go` — modified, exists (checkBillingHalt + setBillingHaltIfNeeded)
- `internal/controller/billing_halt_regression_test.go` — modified, exists (all regression specs)
- `.planning/phases/13-dispatch-image-resolution-provider-halt/13-04-SUMMARY.md` — this file

Checking commits exist:
- `09aec93` — test(13-04): RED specs
- `972823d` — feat(13-04): dispatch-entry hold
- `3231178` — feat(13-04): backstop + regression GREEN

## Self-Check: PASSED
