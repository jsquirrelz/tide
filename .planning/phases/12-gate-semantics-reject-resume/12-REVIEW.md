---
phase: 12-gate-semantics-reject-resume
reviewed: 2026-06-11T16:16:17Z
depth: standard
files_reviewed: 17
files_reviewed_list:
  - api/v1alpha1/shared_types.go
  - cmd/tide/approve.go
  - cmd/tide/approve_test.go
  - cmd/tide/resume.go
  - cmd/tide/resume_test.go
  - docs/gates.md
  - internal/controller/boundary_push_test.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/milestone_controller.go
  - internal/controller/milestone_gates_test.go
  - internal/controller/phase_controller.go
  - internal/controller/phase_gates_test.go
  - internal/controller/plan_controller.go
  - internal/controller/plan_gates_test.go
  - internal/controller/task_controller.go
  - internal/controller/task_gates_test.go
  - test/integration/envtest/gates_test.go
findings:
  critical: 3
  warning: 10
  info: 4
  total: 17
status: issues_found
---

# Phase 12: Code Review Report

**Reviewed:** 2026-06-11T16:16:17Z
**Depth:** standard
**Files Reviewed:** 17
**Status:** issues_found

## Summary

Phase 12 rewired approve/reject/resume semantics across the four level reconcilers and the CLI. The Milestone and Phase controllers received the full D-01/D-04 treatment (AwaitingApproval early-return, approve-returns-to-Running, ChildCount-gated succession) and the reject paths correctly switched from `patch*Failed` to park-style `patch*Rejected` while preserving genuine-failure `patch*Failed` call sites (WaveIntegrationFailed, EnvelopeReadFailed, ExceededAttempts, cap/violation paths — verified intact).

However, the **PlanReconciler did not receive the same treatment**, and tracing the plan reconcile flow shows the plan-level approve gate is structurally bypassed for any plan that authors Tasks (CR-01) and oscillates for leaf plans (CR-02). Separately, the CLI's level discovery rests on a `tideproject.k8s/project` label that no production code path stamps onto Milestones, Phases, or Plans (CR-03) — the new D-07 guard and `--retry-failed` walker inherit that blind spot. Several smaller robustness gaps surround the one-shot annotation consume (approvals can be silently dropped on transient patch failures or rate-limit deferrals) and the descent hold fails open on parent NotFound.

## Critical Issues

### CR-01: `gates.plan=approve` never parks a Plan that authored Tasks — child Tasks dispatch with zero approval (gate bypass)

**File:** `internal/controller/plan_controller.go:211-218, 490-498, 500-519`
**Issue:** The plan-level approve gate hook lives only in `handlePlannerJobCompletion` (line 500-519), which is reachable only while the Plan has **zero** Tasks (`reconcilePlannerDispatch` lines 211-218 early-exit to the wave path the moment any Task with `spec.planRef` exists — and `reporter.stampParentRef` guarantees every materialized Task carries `planRef`). The ChildCount gate (lines 490-498) requeues while `observed < expected`, and the instant the first Task materializes the next reconcile takes the early exit. So for any real planner output (`ChildCount > 0`), the gate hook can never execute with `observed >= expected`: the Plan's `Status.Phase` stays `"Running"`, `patchPlanAwaitingApproval` never fires, and the wave-materialization path dispatches executor Tasks unconditionally. The Task-side descent hold added this phase (`checkParentApproval(..., task.Spec.PlanRef, "Plan")`, task_controller.go:326) is dead code for this scenario because it only checks `Status.Phase == "AwaitingApproval"`, which is never set. Result: with `gates.plan=approve`, executor Jobs spend money without operator approval — the exact run-1 finding-1 failure class this phase set out to close, one level down. `plan_gates_test.go` Test 6a passes only because its fixture has `ChildCount=0` and no reporter (leaf path).
**Fix:** Park the Plan **before** waiting for children, mirroring the milestone/phase hook position — move the gate-policy check in `handlePlannerJobCompletion` to before the ChildCount requeue (children may continue materializing while parked; dispatch is what must be held):

```go
// after the reject short-circuit, BEFORE the expected/observed gate:
if project != nil {
    policy := gates.EvaluatePolicy(project.Spec.Gates, "plan")
    if (policy == gates.PolicyApprove || policy == gates.PolicyPause) && !planAlreadyApproved(plan) {
        if !gates.CheckApprove(plan, "plan") {
            return r.patchPlanAwaitingApproval(ctx, plan, policy)
        }
        // consume + record ApprovedByUser (D-04 two-step)
    }
}
```

and make `reconcileWaveMaterialization` (or the tasks-exist early-exit) honor `plan.Status.Phase == "AwaitingApproval"` so the wave path cannot run while parked. Add a regression spec with `ChildCount > 0` plus reporter-materialized Tasks.

### CR-02: PlanReconciler lacks the AwaitingApproval early-return — a parked leaf Plan is stomped back to Running without approval (D-01 oscillation, park bypass)

**File:** `internal/controller/plan_controller.go:222-242, 366-377`
**Issue:** Milestone (`milestone_controller.go:216-243`) and Phase (`phase_controller.go:201-228`) both gained the Phase-12 `AwaitingApproval` branch (hold without annotation; consume + Running+ApprovedByUser with it). The Plan dispatch body has no such branch: a leaf Plan parked at `AwaitingApproval` by `patchPlanAwaitingApproval` falls through the `Status.Phase == "Running"` check on the next reconcile, re-enters the dispatch body (Job Create → AlreadyExists → idempotent), and unconditionally patches `Status.Phase = "Running"` at line 366-377 — un-parking the level with **no annotation consumed and no operator action**. This is the exact run-1 finding-2 oscillation the phase fixed for Phase, reproduced at Plan level: the level flaps AwaitingApproval→Running→AwaitingApproval on every watch event, and `tide approve`'s `findAwaitingPlan` race-depends on catching it in the parked half of the cycle.
**Fix:** Add the same early-return branch at the top of `reconcilePlannerDispatch` (before the `jobName` / Running checks):

```go
if plan.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(plan, "plan") {
        // consume annotation, patch Running + ApprovedByUser, Requeue
    }
    return ctrl.Result{}, true, nil
}
```

### CR-03: `tide approve` discovery, the D-07 Failed guard, and `tide resume --retry-failed` all key off a `tideproject.k8s/project` label that nothing stamps on Milestones, Phases, or Plans

**File:** `cmd/tide/approve.go:194-252, 300-372`; `cmd/tide/resume.go:104-202`
**Issue:** `findAwaitingMilestone/Phase/Plan`, `findFailedLevel`, and `retryFailedLevels` filter (or `MatchingLabels`-select) on `Labels["tideproject.k8s/project"] == projectName`. The only writer of that label in the entire codebase is `PlanReconciler.stampTaskLabels` (plan_controller.go:1185), which stamps **Tasks only**. `reporter.MaterializeChildCRDs` creates Milestones/Phases/Plans with no labels at all (verified: no label writes in `internal/reporter/materialize.go`). Consequences in a production (reporter-materialized) run: (a) `tide approve <project>` against a parked Milestone/Phase/Plan returns "no level awaiting approval" — the primary approve flow documented in docs/gates.md step 4 does not work; (b) the new D-07 guard silently misses Failed Milestones/Phases/Plans (guard bypassed — the failure mode T-12-05 protects against); (c) `tide resume --retry-failed` resets only Failed Tasks and reports "no Failed levels found" for failed upper levels, while docs/gates.md frames the kubectl recipe as needed only for "legacy" pre-label CRs. Every test fixture in `approve_test.go` / `resume_test.go` / `gates_test.go` stamps the label manually, which is why this passes CI. (The `findAwaiting*` half pre-dates this phase, but D-07 and `--retry-failed` were built on it this phase.)
**Fix:** Stamp `tideproject.k8s/project` in `reporter.MaterializeChildCRDs` (the reporter already knows the project namespace/parent chain — mirror `stampParentRef`), or make the CLI fall back to an owner-ref/parent-ref chain walk when the label misses, mirroring `resolveProjectForPlan`. Add a CLI test whose fixtures deliberately omit the label.

## Warnings

### WR-01: `checkParentApproval` fails open on parent NotFound and on unknown parent kinds

**File:** `internal/controller/dispatch_helpers.go:271-296`
**Issue:** On `Get` NotFound the helper returns `(false, nil)` and the caller proceeds to create the planner/executor Job. The comment calls NotFound "transient informer lag" — but that is precisely the window where the parent's `AwaitingApproval` park is not yet visible, so the child dispatches and spends while the operator believes the run is parked (the zero-spend-during-review guarantee, GATE-04). The Task path is concretely exposed: a Task with the project label resolves its Project without ever needing the Plan in cache, so a lagging Plan informer fail-opens the hold. A deleted parent likewise should not result in dispatch. The unknown-kind default is also permissive.
**Fix:** Fail closed: on NotFound return `(true, nil)` (or have callers requeue after 5s) — the next reconcile re-checks once the cache catches up. Holding dispatch a few seconds is cheap; an unauthorized planner dispatch is not reversible.

### WR-02: Approve handshake is non-atomic — annotation consumed before the Running+ApprovedByUser status patch; a failure between the two silently drops the approval

**File:** `internal/controller/milestone_controller.go:218-241`; `internal/controller/phase_controller.go:203-226` (same pattern at the gate hook, milestone_controller.go:531-548, phase_controller.go:459-476)
**Issue:** The two-step writes the metadata patch (annotation removed) and then the status patch (Running + ApprovedByUser). If the status patch fails (conflict with a concurrent status writer, transient apiserver error), the reconcile returns an error and requeues — but the annotation is already gone, so the next reconcile takes branch (a) "no annotation → keep paused". The level stays at AwaitingApproval with no record that an approval was ever issued; the operator's `tide approve` is silently swallowed and must be re-run, with nothing surfacing why.
**Fix:** Invert the order: write Running+ApprovedByUser first, consume the annotation second. With the `alreadyApproved` check (ApprovedByUser condition) already present in the gate hook, a leftover annotation after a crash between the two steps is consumed/ignored safely, whereas the current order loses operator intent. Alternatively, on status-patch failure re-add the annotation before returning the error.

### WR-03: Task-level approve annotation consumed before dispatch commit — a rate-limit deferral eats the approval and re-parks the Task

**File:** `internal/controller/task_controller.go:397-409`
**Issue:** `checkReadinessGates` consumes the approve-task annotation and returns to `reconcileDispatch`, which then runs `acquireDispatchSlots`. If the rate-limit bucket is exhausted (`rateLimitedError`) — or `prepareDispatch` fails transiently — the reconcile requeues without dispatching and without recording any ApprovedByUser state (unlike milestone/phase, no condition is written). The next reconcile re-evaluates `policy == approve`, finds no annotation, and calls `patchTaskAwaitingApproval` — the operator's approval was consumed by a requeue.
**Fix:** Record approval durably at consume time (set the `WaveOrLevelPaused=False/ApprovedByUser` condition and have the gate skip when present, mirroring milestone/phase `alreadyApproved`), or defer the consume until after `createDispatchJob` succeeds.

### WR-04: `tide resume` cannot release a `pause` park — the documented release verb for gate=pause is a silent no-op

**File:** `cmd/tide/resume.go:69-89`; `docs/gates.md:32, 41-42`
**Issue:** docs/gates.md states gate=`pause` "halts after the artifact is authored; operator runs `tide resume <project>` to release", and `patch*AwaitingApproval` parks pause-gated levels at `Status.Phase=AwaitingApproval` with `Reason=PausedAtBoundary`. But `resumeRun` only (a) clears the Project's reject annotation and (b) with `--retry-failed`, resets Failed levels. A PausedAtBoundary level is neither — resume exits successfully having changed nothing, and the level stays parked indefinitely. The only working release is `tide approve` (the reconcilers' `CheckApprove` path doesn't distinguish which policy parked the level), which contradicts the documented approve/pause distinction.
**Fix:** Either have `resumeRun` also discover PausedAtBoundary levels and write the approve annotation (or a dedicated resume annotation the reconcilers consume), or change docs/gates.md to state that `tide approve` releases pause parks. Pick one and add a test for the pause→release path — there is currently none.

### WR-05: `--retry-failed` immediately re-fails (or wedges) for several genuine-failure classes because the failure artifacts are not reset

**File:** `cmd/tide/resume.go:94-210`
**Issue:** The walker resets `Status.Phase` and `Conditions` only. Three failure classes flip straight back to Failed or wedge:
1. **Task ExceededAttempts** — `Status.Attempt` and the attempt-labeled Jobs survive; `nextAttempt` (task_controller.go:971-1001) computes `maxAttempt+1 > maxAttempts` and `prepareDispatch` re-patches `Failed/ExceededAttempts` on the first reconcile.
2. **Plan WaveIntegrationFailed** — the `tide-push-wave-<uid>-<N>` Job with `Status.Failed > 0` persists; `reconcileWaveBoundary` (plan_controller.go:753-759) re-patches `Failed/WaveIntegrationFailed` immediately.
3. **Plan CycleDetected** — `Status.ValidationState` stays `"CycleDetected"`, so the wave path no-ops at the `!= "Validated"` gate and the Plan sits at `Phase=""` forever (arguably correct for a cyclic DAG, but silent).
The verb prints "reset ... for re-dispatch" in all three cases — the operator gets a false success signal for exactly the recovery scenarios the verb exists for (docs/gates.md "Recovering Failed levels").
**Fix:** Per-kind cleanup in `retryFailedLevels`: for Tasks also delete attempt Jobs (or reset whatever `nextAttempt` derives from) or document that retry honors the attempts budget; for Plans delete the failed wave-integration Job; warn (don't reset) on `ValidationState=CycleDetected`.

### WR-06: D-07 guard blocks all approvals project-wide on any Failed level, contradicting the strict wave-boundary profile; `--wave` path skips the guard entirely

**File:** `cmd/tide/approve.go:152-164, 83-129`
**Issue:** Under the spec's strict failure profile, a failed Task is a *normal continuing-run state* — siblings and non-dependents keep dispatching. With D-07 as written, one genuinely-failed Task anywhere in the project makes `tide approve` refuse every subsequent approval (e.g., an unrelated Milestone park), and the only unblock — `tide resume --retry-failed` — resurrects **all** Failed levels including legitimately-dead work, which is exactly the accidental-resurrection D-06's "deliberate friction" exists to prevent. Meanwhile `approveWave` performs no Failed check at all, so the guard is also trivially bypassable via `--wave`, and a pre-written `approve-wave-N` annotation persists as a stale auto-approval if the wave later becomes pending.
**Fix:** Scope the guard to the approval target's own subtree (e.g., refuse only when the Failed level is the AwaitingApproval level itself or its ancestor/descendant), and apply the same guard (scoped to the named Plan) in `approveWave`.

### WR-07: `approveWave` never verifies the Plan belongs to the named Project

**File:** `cmd/tide/approve.go:83-129`
**Issue:** The function verifies the Project exists and the Plan exists, but never checks `plan.Labels["tideproject.k8s/project"]` (or the parent chain) against `projectName`. In a multi-project namespace, `tide approve proj-a --wave proj-b-plan/3` silently writes the wave approval onto project B's plan — cross-project gate actuation, the same mis-routing class Phase 04.1 P1.4 closed controller-side.
**Fix:** After fetching the Plan, validate ownership (label match or chain walk) and error with "plan %q does not belong to project %q" on mismatch.

### WR-08: `buildFailureDetail` reads the wrong condition — D-07 errors will surface misleading "failure reasons"

**File:** `cmd/tide/approve.go:254-298`
**Issue:** Genuine failures are stamped on the `ConditionFailed` condition type (`patchMilestoneFailed`, `patchPlanFailed`, `patchTaskFailed`). `buildFailureDetail` looks for `ConditionWaveOrLevelPaused` first and falls back to `Conditions[0]` — for a really-failed level, `WaveOrLevelPaused` is usually absent and `Conditions[0]` is whatever was set first (typically `AuthoringPlanner/PlannerDispatched` or `Reconciling`). The operator-facing error then reads e.g. `(reason: PlannerDispatched: Planner Job tide-milestone-…-1 dispatched)` instead of the failure reason. The test passes only because its fixture artificially places the failure message on `WaveOrLevelPaused`.
**Fix:** Look up `ConditionFailed` first, then `WaveOrLevelPaused`, then fall back. Also collapse the four identical type-switch arms into a single conditions accessor.

### WR-09: Nil-Project dereference in milestone planner dispatch (panic path; phase/plan controllers guard, milestone does not)

**File:** `internal/controller/milestone_controller.go:360, 384`
**Issue:** `project` can be nil at line 360 (`if project.Spec.ProviderSecretRef != ""`) and line 384 (`string(project.UID)`): the Step-4 resolve swallows Get errors (`if err == nil { project = &p }`). The earlier owner-ref Get in `Reconcile` makes this unlikely, but the cache can change between the two Gets within one reconcile (Project deleted mid-reconcile) — and the sibling controllers explicitly guard the identical block (`phase_controller.go:329 if project != nil && …`, `plan_controller.go:323`). A reconciler panic takes down the work item (and depending on `RecoverPanic` config, the manager). Pre-existing (not introduced by this phase's diff) but in-scope and trivially hardened.
**Fix:** Guard both uses: `if project != nil && project.Spec.ProviderSecretRef != ""` and resolve `projectUID` via the existing nil-safe pattern; or early-return/requeue when `project == nil` as the Plan controller's cascade-7 gate does.

### WR-10: Phase/Plan reject short-circuit fires after reporter-Job spawn — a rejected Project still creates new Jobs

**File:** `internal/controller/phase_controller.go:424, 440`; `internal/controller/plan_controller.go:441-465, 503`
**Issue:** Milestone moved its reject check to the top of `handleJobCompletion` (line 447, with an explicit Phase-04.1 comment: "operator stop should always halt"). Phase checks reject at line 440 — *after* `spawnReporterIfNeeded` (line 424) — and Plan checks at line 503, after both the reporter spawn and the `ValidationState=Validated` stamp. So a Project rejected mid-run still gets new reporter Jobs created (and at Plan level, the Validated stamp arms the wave path for the moment the reject clears, before the gate hook ever ran). This contradicts the D-05 contract that reject "halts further Job dispatch" and is an unforced parity drift between three supposedly-mirrored bodies.
**Fix:** Hoist the `gates.CheckRejected(project)` short-circuit to the top of `handleJobCompletion` / `handlePlannerJobCompletion` in both controllers, matching the milestone ordering.

## Info

### IN-01: docs/gates.md annotation table documents keys that do not exist

**File:** `docs/gates.md:57-61`
**Issue:** The table lists `tideproject.k8s/approve` (actual key: `tideproject.k8s/approve-<level>`) and `tideproject.k8s/approve-wave/<plan>/<wave>` (actual: `tideproject.k8s/approve-wave-<N>` written on the named Plan). An operator hand-writing annotations from this table writes no-op keys (`CheckApprove` is strict). The doc also forward-references "Phase 15 CUTS-01" for the project label whose stamping does not exist for upper levels (see CR-03).
**Fix:** Correct the table to match `internal/gates/annotation.go` constants.

### IN-02: Heavy four-way duplication in the CLI walkers

**File:** `cmd/tide/approve.go:194-252, 258-298, 300-372`; `cmd/tide/resume.go:104-202`
**Issue:** `findFailedLevel`, `buildFailureDetail`, the four `findAwaiting*` functions, and the four `retryFailedLevels` loops are copy-pasted per Kind with only the list type varying. Each future status-shape change must be made in 4-12 places (WR-08's wrong-condition bug already lives in 4 copies).
**Fix:** Introduce a small per-kind descriptor (list constructor + conditions/phase accessor) and iterate, or use `client.ObjectList` + `meta.ExtractList`.

### IN-03: Dead double-List in the GATE-04 envtest

**File:** `test/integration/envtest/gates_test.go:132-137`
**Issue:** The first `List` with `MatchingLabels{"batch.kubernetes.io/job-name": ""}` is immediately overwritten by the unfiltered `List`; the filtered call is dead code that suggests an abandoned approach.
**Fix:** Delete the first `List` call.

### IN-04: `tide approve` / success paths print nothing

**File:** `cmd/tide/approve.go:62-78, 396-423`
**Issue:** `approveRun` carries an `out io.Writer` documented as a "future seam" (nolint:unparam) and the cobra layer prints no confirmation — a successful approve is indistinguishable from a no-op at the terminal, which matters more now that WR-02/WR-03 can silently swallow approvals. `resume.go` already prints per-level feedback; approve should match.
**Fix:** Print one line on success (e.g., `tide: approved milestone/ms-alpha on project my-project`) from `patchApproveLevel`/`approveWave`.

---

_Reviewed: 2026-06-11T16:16:17Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
