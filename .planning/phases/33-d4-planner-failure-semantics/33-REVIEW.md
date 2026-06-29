---
phase: 33-d4-planner-failure-semantics
reviewed: 2026-06-29T13:35:22Z
depth: deep
files_reviewed: 8
files_reviewed_list:
  - internal/controller/planner_failure.go
  - internal/controller/planner_failure_test.go
  - api/v1alpha2/shared_types.go
  - internal/controller/phase_controller.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller_test.go
  - internal/controller/milestone_controller_test.go
  - cmd/tide/resume_test.go
findings:
  critical: 1
  warning: 2
  info: 2
  total: 5
status: resolved
---

# Phase 33: D4 — Planner Failure Semantics — Code Review

**Reviewed:** 2026-06-29T13:35:22Z
**Depth:** deep (cross-file analysis, call-chain tracing)
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Phase 33 implements the D4 false-leaf guard: a `isPlannerFailure` shared predicate
(`planner_failure.go`) and new `patchPhaseFailed`/`patchMilestoneFailed` helpers
inserted before the `expected == 0 → patchXSucceeded` branch at both succession sites.
The core predicate logic is correct, PLANFAIL-03 ordering is load-bearing and
correctly placed, and PLANFAIL-04 recovery via `resumeRun` requires no new code.

**One blocker:** the guard is positioned AFTER the gate-policy hook in
`handleJobCompletion`. For the Milestone level, whose default gate policy is
`PolicyApprove`, a failed planner with zero children is silently parked at
`AwaitingApproval` instead of being marked `Failed`. The guard only fires after
the operator manually approves an already-failed planner — inverting expected UX.
The PLANFAIL-02 envtest sidesteps this by using `Gates{Milestone: "auto"}`, which
is NOT the production default; the default-gate-policy case is untested and the
contract is not met for new projects out of the box.

---

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: Milestone `isPlannerFailure` guard masked by default `PolicyApprove` gate

**File:** `internal/controller/milestone_controller.go:651-726`
**Confidence:** HIGH

The `isPlannerFailure` guard is inserted at line 723, inside `if envReadOK {` (line
715), which is reached only after the gate-policy hook at lines 651-700. The gate
runs:

```go
policy := gates.EvaluatePolicy(project.Spec.Gates, "milestone")
if policy == gates.PolicyApprove || policy == gates.PolicyPause {
    alreadyApproved := false
    // ...
    if !alreadyApproved {
        if !gates.CheckApprove(ms, "milestone") {
            return r.patchMilestoneAwaitingApproval(ctx, ms, policy)  // RETURNS HERE
        }
    }
}
```

The default gate for Milestone is `PolicyApprove` (`internal/gates/policy.go:55`).
For any project that does not explicitly set `Gates.Milestone="auto"`, a milestone
planner that exits nonzero with zero children will be **parked at `AwaitingApproval`
rather than marked `Failed`**. The guard at line 723 is never reached on the first
call to `handleJobCompletion`.

The full flow for default-gates projects is:
1. Planner Job exits exitCode=1, childCount=0.
2. `handleJobCompletion` called. Gate fires: `AwaitingApproval` park returned.
3. Operator sees "AwaitingApproval" — misleading; the planner crashed.
4. Operator approves; annotation consumed; `Status.Phase="Running"`, `ApprovedByUser` stamped; Requeue.
5. Next reconcile: Running branch → `handleJobCompletion` again. Now `alreadyApproved=true`. Guard fires. `Failed` set.

The guard does eventually fire (step 5), so the false-succeed is prevented for the
approval case. However:

- **For `PolicyPause`:** after pausing, the operator must `tide resume` (not annotate).
  When `tide resume` is called, it clears the `FailureHalt` hold (not the `AwaitingApproval`
  park). The reconciler then re-enters... but `Status.Phase` is still `AwaitingApproval`,
  which enters the AwaitingApproval branch of `reconcilePlannerDispatch`, not the Running
  branch. Unless `CheckApprove` is satisfied, the level stays parked indefinitely.
  For a failed planner under `PolicyPause`, the operator must explicitly `tide approve`,
  not just `tide resume`. This is a dead-end that leaves the failed milestone permanently
  parked if the operator uses `tide resume` (the natural recovery action for failures).

- **The PLANFAIL-02 envtest avoids this entirely** by using `Gates{Milestone: "auto"}`,
  the non-default. The test comment acknowledges this: "auto gate so planner Job
  completion flows directly to succession logic without parking at AwaitingApproval".
  This means the test does not cover the contract for the standard configuration.

**Fix:** Move the `isPlannerFailure` guard **before** the gate-policy hook in
`handleJobCompletion` at both levels, or add a special-case inside the gate-policy
block that checks `isPlannerFailure` and bypasses the approval park for a failed
planner. The cleaner fix is to move it before the gate hook:

```go
// Phase 33 PLANFAIL-01/02: guard BEFORE gate-policy hook.
// A failed planner (exitCode!=0, childCount==0) should never be parked at
// AwaitingApproval — it should be marked Failed immediately so the operator
// sees the real state, not a misleading approval request.
if envReadOK {
    if isPlannerFailure(out, envReadOK) {
        return r.patchMilestoneFailed(ctx, ms, tideprojectv1alpha2.ReasonPlannerFailed,
            fmt.Sprintf("planner exited nonzero (exitCode=%d) with zero children; marked Failed to prevent false succession", out.ExitCode))
    }
}

// THEN the gate-policy hook...
if project != nil {
    policy := gates.EvaluatePolicy(project.Spec.Gates, "milestone")
    // ...
}
```

The same fix applies to `phase_controller.go:handleJobCompletion`. For Phase the
default policy is `PolicyAuto`, so the bug only triggers when the operator explicitly
sets `Gates.Phase="approve"` or `"pause"`. For Milestone it triggers on every
default-configured project.

Add a test variant that uses default gates (no `Gates` spec) to cover PLANFAIL-02
with `PolicyApprove` active, confirming the guard fires before the approval park.

---

## Warnings

### WR-01: PLANFAIL-01/02 tests do not assert pre-condition `Status.Phase=="Running"` before triggering the guard

**File:** `internal/controller/phase_controller_test.go:519-537`,
`internal/controller/milestone_controller_test.go:722-738`
**Confidence:** HIGH

Both PLANFAIL-01 and PLANFAIL-02 tests drive a dispatch reconcile (`reconcileWithRetry`
5×), fetch the object, then immediately set the failing envelope and mark the Job
terminal — without asserting that `Status.Phase=="Running"` first. If the dispatch
reconcile failed silently (e.g. planner pool exhausted, Job creation error), the
Phase/Milestone would not be in `Running` state, `makeFakeJobTerminal` would return
an error (job not found), and the test would fail for the wrong reason — obscuring
the root cause. More critically: if the dispatch reached a non-Running terminal
state for another reason, the final `Eventually` assertion of `Status.Phase=="Failed"`
could pass vacuously.

**Fix:** Add an intermediate `Eventually` after the dispatch step to assert
`Status.Phase=="Running"` before marking the Job terminal:

```go
// Assert dispatch landed before triggering terminal.
Eventually(func(g Gomega) {
    var running tideprojectv1alpha2.Phase
    g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &running)).To(Succeed())
    g.Expect(running.Status.Phase).To(Equal("Running"),
        "PLANFAIL-01 precondition: Phase must reach Running before guard test")
}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
```

Apply the same pattern to the milestone PLANFAIL-02 test.

### WR-02: `patchPhaseFailed`/`patchMilestoneFailed` leave `ConditionAuthoringPlanner=True` alongside `ConditionFailed=True`

**File:** `internal/controller/phase_controller.go:733-747`,
`internal/controller/milestone_controller.go:815-829`
**Confidence:** MEDIUM

When the guard fires and marks a Phase/Milestone `Failed`, `ConditionAuthoringPlanner`
was set to `True` during Job dispatch. Neither `patchPhaseFailed` nor
`patchMilestoneFailed` clears it. The result: `kubectl describe` shows both
`AuthoringPlanner=True` and `Failed=True` simultaneously — potentially misleading
operators who might interpret `AuthoringPlanner=True` as "the planner is still
running."

This is consistent with `patchPlanFailed` (the mirrored helper at
`plan_controller.go:887`) which also does not clear `ConditionAuthoringPlanner`. The
behaviour is not a new regression introduced by Phase 33, but it is visible operator
confusion that should be addressed now that the condition pair can appear at Phase and
Milestone levels.

**Fix:** Add a `meta.SetStatusCondition` call that flips `ConditionAuthoringPlanner`
to `False` in `patchPhaseFailed` and `patchMilestoneFailed` (and optionally in the
existing `patchPlanFailed` for consistency):

```go
meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha2.ConditionAuthoringPlanner,
    Status:             metav1.ConditionFalse,
    Reason:             tideprojectv1alpha2.ReasonPlannerFailed,
    Message:            "Planner Job failed; see ConditionFailed for details",
    LastTransitionTime: metav1.Now(),
})
```

---

## Info

### IN-01: `ReasonPlannerFailed` const block is placed out of numeric phase order in `shared_types.go`

**File:** `api/v1alpha2/shared_types.go:206-212`
**Confidence:** HIGH

The Phase 33 const block is placed between the Phase 11 block (`ReasonWaveIntegrationFailed`,
line 197) and the Phase 13 block (`ConditionBillingHalt`, line 214), producing the
ordering: Phase 11 → Phase 33 → Phase 13 → Phase 14 → Phase 25 → Phase 28 → Phase 31 →
Phase 23. The commit message confirms this was intentional ("after ReasonWaveIntegrationFailed"),
following the `33-CONTEXT.md D-05` directive. Non-breaking, but a future reader will
find the file's phase numbering inconsistent.

**Fix (cosmetic):** Either accept the ordering (it matches the CONTEXT.md directive) or
add a comment noting the blocks are ordered by addition, not phase number. No code change
required; the const value `"PlannerFailed"` and usage are correct.

### IN-02: PLANFAIL-04 proves `resumeRun` clears state but does not prove re-dispatch fires after reset

**File:** `cmd/tide/resume_test.go:418-497`
**Confidence:** MEDIUM

`TestResumeRetryFailedPlannerFailed` tests `resumeRun` with a fake client — it verifies
`Status.Phase` is cleared and `ResumedByUser` condition is stamped. It does NOT exercise
the reconciler after the reset to confirm the controller re-enters the dispatch path
(as opposed to the terminal short-circuit). The terminal short-circuit at
`reconcilePlannerDispatch:237` fires on `"Succeeded"|"Failed"` only; after the reset,
`Status.Phase=""` bypasses the short-circuit and reaches the dispatch arm. This is
correct by inspection, but the envtest gap means a future regression (e.g. a second
short-circuit condition check added to the dispatch path) would not be caught.

**Fix:** Add a small envtest that creates a Phase with `Status.Phase="Failed"` and
`ReasonPlannerFailed`, calls `resumeRun`, then drives a reconcile and asserts that
the planner Job is re-dispatched (or `Status.Phase` is at minimum no longer `"Failed"`
and the reconciler did not bail via the terminal short-circuit).

---

_Reviewed: 2026-06-29T13:35:22Z_
_Reviewer: Claude Sonnet 4.6 (gsd-code-reviewer)_
_Depth: deep_

**Finding counts: 1 BLOCKER (CR-01), 2 WARNING (WR-01, WR-02), 2 INFO (IN-01, IN-02)**

---

## Resolution (orchestrator, 2026-06-29)

CR-01 was independently confirmed against source before acting (not taken on the reviewer's word): `internal/gates/policy.go:47` makes the milestone default gate `approve`, and in both controllers the gate-policy hook ran *before* the `isPlannerFailure` guard — so a failed milestone planner parked at `AwaitingApproval` instead of `Failed`. The PLANFAIL-02 envtest had masked this by forcing `Gates{Milestone:"auto"}`.

- **CR-01 (BLOCKER) — FIXED.** Moved the `isPlannerFailure` guard to *before* the gate-policy hook in both `phase_controller.go` and `milestone_controller.go` (a planner that authored nothing is terminally Failed and cannot be gate-approved). A successful planner (exitCode 0) still falls through to the gate hook unchanged.
- **WR-01 (WARNING) — FIXED.** PLANFAIL-01/02 now assert `Status.Phase=="Running"` (planner actually dispatched) before injecting the failure envelope, and run under their **approve** gate (phase=approve, milestone=approve) so they exercise production config and would catch a CR-01 regression. Re-ran: 4/4 PLANFAIL specs pass under the approve gate.
- **WR-02 (WARNING) — ACCEPTED (no change).** `patchPhaseFailed`/`patchMilestoneFailed` leave `ConditionAuthoringPlanner=True` alongside `ConditionFailed=True`, deliberately mirroring the existing `patchPlanFailed` (CONTEXT.md D-06 = mirror exactly). Cosmetic-only (`kubectl describe`); diverging the three level helpers would be worse.
- **IN-01 (INFO) — ACCEPTED.** `ReasonPlannerFailed` const placement is cosmetic; value is correct.
- **IN-02 (INFO) — ACCEPTED.** Re-dispatch after `--retry-failed` reset is existing reconciler behavior, out of D4 scope; PLANFAIL-04's contract (clear Failed, no retry storm) is proven.

Full controller suite (134s) + cmd/tide + `make lint` (0 issues) re-run green after the fix.
