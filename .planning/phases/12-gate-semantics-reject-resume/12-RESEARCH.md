# Phase 12: Gate Semantics + Reject/Resume - Research

**Researched:** 2026-06-11
**Domain:** Kubernetes controller gate semantics, annotation-driven approval flow, reject/resume recovery
**Confidence:** HIGH — all findings drawn from live codebase inspection

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**D-01: Gate sits at descent.** `AwaitingApproval` parks the level after its artifact is authored and BLOCKS child dispatch. The operator reviews the authored artifact, approves, children dispatch, and the level auto-succeeds when all children complete — no second approval at completion. Closes run-1 finding 1 (GATE-04).

**D-02: Materialize children, hold dispatch.** Child CRs are created immediately by the reporter (operator sees the planned children in dashboard/kubectl while reviewing), but child reconcilers refuse to dispatch planner/executor Jobs while the parent is unapproved. Review with full DAG visibility, zero spend.

**D-03: Approval never jumps a level to Succeeded.** Succession is exclusively children-gated. The ChildCount-gated succession logic in handleJobCompletion is the right shape; the bug is the approval path that bypasses it. The run-1 finding-7 regression test must prove: approving a Milestone with N incomplete children leaves it un-Succeeded until all N complete.

**D-04: `Running` + `ApprovedByUser` condition.** After approval the level's Status.Phase returns to Running; a Condition (Reason=`ApprovedByUser`, mirroring the existing `ResumedByUser` pattern) permanently records the human approval. NO new Status.Phase enum value (gates.md:99's `Approved` sketch is superseded). No CRD enum/conversion churn.

**D-05: Reject parks, never fail-marks.** `tide reject` sets a Rejected condition and halts dispatch; children pause where they are. No `Status.Phase=Failed` writes (today's patchPlanFailed cascade at plan_controller.go:478 goes away for the reject path). `Failed` is reserved for real failures.

**D-06: Resume lifts parks AND retries Failed (flagged).** `tide resume` undoes a reject park. With `--retry-failed` it also implements the run-1 kubectl recovery recipe as a sanctioned verb: clear status.phase + conditions → reconciler re-dispatches → `ResumedByUser` condition set.

**D-07: Approve on a Failed level errors with a pointer to resume.** `tide approve` against a level whose planner Job failed prints the failure reason and directs to `tide resume --retry-failed`. Approval never doubles as a spend-retry.

### Claude's Discretion

- Regression-test vehicle split: envtest vs kind Layer B per scenario.
- Exact condition type/reason naming (follow existing condition conventions in api/v1alpha1 + controllers).
- Migration/cleanup handling for run-1 CRs already fail-marked in the live cluster.
- Whether reject cancels in-flight Jobs or lets them drain (pick the simpler-correct option).

### Deferred Ideas (OUT OF SCOPE)

- Approval UX in dashboard (approve button beyond copy-to-clipboard).
- Per-level gate timeout / auto-approve-after-N-hours.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| GATE-01 | Approving a gated level with incomplete children does not advance it to Succeeded | Root cause identified: approval branch in AwaitingApproval handler calls patchMilestoneSucceeded without ChildCount guard; fix is to re-enter handleJobCompletion's children-gated succession path |
| GATE-02 | gates.md step 5 documents approve-then-wait-for-children semantics | gates.md step 5 exact text "advances the level to Succeeded" confirmed; update is a doc rewrite of that section |
| GATE-03 | A level whose planner Job failed is recoverable via `tide resume --retry-failed`, never wedged; `tide approve` against it gives an actionable error | `approveLevel` currently writes annotation without checking Failed state; `resumeRun` only clears the reject annotation and has no `--retry-failed` flag today |
| GATE-04 | A level parked at AwaitingApproval blocks child dispatch | Child reconcilers (Phase, Plan, Task) have no parent-approval check at dispatch entry; spawnReporterIfNeeded runs before gate hook so children materialize but no dispatch hold exists |
| RESUME-01 | `tide resume` after `tide reject` recovers fail-marked children | patchPlanFailed cascade confirmed; Failed early-exit at plan_controller.go:222 confirmed; resume clears annotation but leaves Status.Phase=Failed; solution is D-06 --retry-failed + parent-propagated resume |
</phase_requirements>

---

## Summary

Phase 12 is a focused bug-fix phase across the four level reconcilers (Milestone/Phase/Plan/Task) and the `cmd/tide` CLI. All five requirements stem from two root-cause clusters discovered in dogfood run 1: (1) the approval path short-circuits to `Succeeded` without waiting for children (GATE-01/GATE-02/GATE-04), and (2) the reject path fatally fail-marks children that resume cannot lift (RESUME-01/GATE-03).

The annotation primitives in `internal/gates/annotation.go` are sound and need no changes. The bugs are entirely in WHERE the consume result routes inside each reconciler and in what the CLI verbs do after writing annotations. Every fix has a symmetric counterpart across all four level reconcilers, and every fix needs a regression test that reproduces the exact run-1 symptom.

The existing test infrastructure is complete: `internal/controller/*_gates_test.go` (envtest, Ginkgo), `test/integration/envtest/gates_test.go` (multi-reconciler envtest), and `test/integration/kind/` (Layer B kind). New tests for this phase should follow the existing envtest patterns for the controller-unit layer and can use the kind cluster `tide` (with run-1 CRs intact) for repro validation.

**Primary recommendation:** Fix the four reconcilers symmetrically, add `ReasonApprovedByUser` to `api/v1alpha1/shared_types.go`, add `--retry-failed` to `cmd/tide/resume.go`, guard `approveLevel` against Failed levels, and rewrite `docs/gates.md` step 5 — all in one or two tightly scoped plans.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Approval annotation write | CLI (cmd/tide) | — | `tide approve` is the only writer; annotation propagated to child CRDs by approveLevel |
| Approval annotation consume + route | Controller (reconciler) | — | One-shot consume inside handleJobCompletion; T-04-G2 replay mitigation |
| Child dispatch hold (D-02) | Controller (child reconciler) | — | Phase/Plan/Task each check their parent's approval state at dispatch entry |
| AwaitingApproval → Running + ApprovedByUser condition | Controller (reconciler) | — | Same reconciler that consumed the annotation patches the condition |
| Children-gated Succeeded succession | Controller (reconciler) | — | handleJobCompletion's ChildCount gate is authoritative; approval path must rejoin it |
| Reject annotation write | CLI (cmd/tide) | — | `tide reject` writes tideproject.k8s/reject on Project |
| Reject park (D-05) | Controller (all 4 reconcilers) | — | CheckRejected at dispatch entry; park instead of patchFailed |
| Resume annotation clear | CLI (cmd/tide) | — | `tide resume` clears the reject annotation; --retry-failed resets Status.Phase |
| Failed level recovery (D-06) | CLI (cmd/tide) + Controller | — | CLI clears annotation + resets status; reconciler re-dispatches on next loop |
| Documentation | docs/gates.md | — | GATE-02 rewrites the doc; gate-flow doc is the operator vocabulary |

---

## Standard Stack

No new external packages are introduced by this phase. All changes are to existing Go source files using the project's pinned dependency set.

| Library | Version | Purpose | Already Used |
|---------|---------|---------|--------------|
| sigs.k8s.io/controller-runtime | v0.24.x | Reconciler, Patch, MergeFrom | Yes |
| k8s.io/apimachinery/pkg/api/meta | pinned via controller-runtime | SetStatusCondition, FindStatusCondition | Yes |
| github.com/onsi/ginkgo/v2 | v2.28 | Test framework | Yes |
| github.com/onsi/gomega | current | Test matchers | Yes |

## Package Legitimacy Audit

No external packages are installed by this phase. Section not applicable.

---

## Root Cause Analysis

### GATE-01 / GATE-03 Root Cause: AwaitingApproval re-enter path

**[VERIFIED: codebase]** In `milestone_controller.go:reconcilePlannerDispatch` (lines 214–224):

```go
if ms.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(ms, "milestone") {
        jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
        var job batchv1.Job
        if err := r.Get(ctx, ..., &job); err == nil {
            return r.handleJobCompletion(ctx, ms, &job)
        }
        // Job missing — annotation-only finalization
        return r.patchMilestoneSucceeded(ctx, ms)  // <-- THE BUG
    }
    return ctrl.Result{}, nil
}
```

When the planner Job is gone (TTL/GC), approval takes the `patchMilestoneSucceeded` fast-path unconditionally. Even when the Job is present, `handleJobCompletion` contains the ChildCount-gated succession, BUT it only gates succession when `envReadOK=true`. If `EnvReader` returns an error or is nil, the fallback path at lines 525–542 calls `patchMilestoneSucceeded` without checking child count.

**The correct fix for D-03:** When the approve annotation is consumed, the reconciler should transition to `Running` + set `ApprovedByUser` condition (D-04), then let the normal succession machinery decide Succeeded. This means children complete → `BoundaryDetected` fires → `patchMilestoneSucceeded`. The approval path must never call `patchMilestoneSucceeded` directly.

**The phase controller does NOT have the same AwaitingApproval early-return.** `phase_controller.go:reconcilePlannerDispatch` starts with `if ph.Status.Phase == "Succeeded" || ph.Status.Phase == "Failed"` only; there is no AwaitingApproval branch. This means a Phase parked at AwaitingApproval re-enters the planner dispatch path on every reconcile — this is the root cause of run-1 finding 2 (Phase status flapping). **The fix for D-01/D-03 must also add the AwaitingApproval early-return to phase_controller.go (and ensure plan_controller.go's reconcilePlannerDispatch has parity).**

### GATE-04 Root Cause: No parent-approval hold in child reconcilers

**[VERIFIED: codebase]** The child dispatch entry points — `PhaseReconciler.reconcilePlannerDispatch`, `PlanReconciler.reconcilePlannerDispatch`, `TaskReconciler.gateChecks` — have no check that the parent level is approved. The current gate hooks only check the level's OWN policy (e.g., whether a Phase is awaiting its own approval). They do not check whether the parent Milestone/Phase/Plan is still parked at AwaitingApproval.

The mechanism for D-02:
- Phase reconciler must check: "Is my parent Milestone in AwaitingApproval?" → if yes, return early (park at AwaitingApproval with Reason=`PausedAtBoundary` or a new `ParentAwaitingApproval` reason).
- Plan reconciler must check: "Is my parent Phase in AwaitingApproval?" → same.
- Task reconciler's `gateChecks` must check: "Is my parent Plan in AwaitingApproval?" → same. The resolved Project is already available; the parent Plan is fetched via `task.Labels["tideproject.k8s/project"]` → owner-ref chain, or directly via `task.Spec.PlanRef`.

**Shared helper opportunity:** A `checkParentApproval(ctx, client, obj client.Object) (bool, error)` helper function in `internal/controller/dispatch_helpers.go` (or similar) can be called from all three child reconcilers. It looks up the parent by the spec ref (task.Spec.PlanRef / plan.Spec.PhaseRef / phase.Spec.MilestoneRef), checks `Status.Phase == "AwaitingApproval"`, and returns true to halt dispatch. This respects D-02 without duplicating logic.

**IMPORTANT:** The parent-approval check must fire BEFORE planner/executor Job creation but AFTER idempotency checks (so an already-dispatched level that parents are later approved doesn't re-dispatch). In milestone_controller.go, the right position is after the Running short-circuit but before the plannerPool Acquire. In phase_controller.go, same position. In plan_controller.go, same. In task_controller.go, after Step 3 (resolve Project) and before Step 4 (budget gate).

### RESUME-01 Root Cause: patchPlanFailed cascade + Failed early-exit

**[VERIFIED: codebase]** At `plan_controller.go:478`:
```go
if project != nil && gates.CheckRejected(project) {
    return r.patchPlanFailed(ctx, plan, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
}
```

This fires inside `handlePlannerJobCompletion` after the planner Job completes. `patchPlanFailed` writes `Status.Phase=Failed`. Then, the terminal short-circuit at `reconcilePlannerDispatch:222` (`if plan.Status.Phase == "Succeeded" || plan.Status.Phase == "Failed"`) means every future reconcile returns early without re-dispatching.

`resumeRun` (cmd/tide/resume.go) clears only the reject annotation on the Project. It does NOT reset `Status.Phase` on individual Plans/Phases/Milestones. So after resume, child Plans remain Failed and never re-dispatch.

**The D-05 fix** removes the `patchPlanFailed` call from the reject path (in all four reconcilers' gate hooks) and replaces it with a park: set a `Rejected` condition (Reason=`RejectedByUser`) and return without advancing. Do NOT write `Status.Phase=Failed`.

**The D-06 fix** adds `--retry-failed` to `resumeRun`:
1. CLI iterates all levels (Milestones/Phases/Plans/Tasks) belonging to the project.
2. For each level with `Status.Phase=Failed`, patches `Status.Phase=""` and clears conditions via `status --subresource` (mirrors the run-1 kubectl recovery recipe).
3. The reconcilers' terminal short-circuit gates on `"Succeeded" || "Failed"` — clearing phase to `""` lets the reconciler re-enter the dispatch path.
4. Set `ResumedByUser` condition on each recovered level (mirrors existing pattern at plan_controller.go:523/552).

**Milestone/Phase reconcilers also have `patchMilestoneFailed`/`patchPhaseFailed`.** The `CheckRejected` short-circuit inside `handleJobCompletion` calls these at milestone_controller.go:411 and phase_controller.go:378. Both must be converted to park behavior under D-05.

### GATE-03: approveLevel behavior on Failed levels

**[VERIFIED: codebase]** `approveLevel` (cmd/tide/approve.go:132–163) walks child CRDs looking for `Status.Phase == "AwaitingApproval"`. If a level has `Status.Phase == "Failed"` (planner Job failed), `approveLevel` skips it and falls through to `"tide: no level awaiting approval"`. The current behavior is "no level awaiting approval" — ambiguous and unhelpful.

**D-07 fix:** Before the search loop, `approveRun` checks each level type for any Failed level. If one is found: print `"error: level <name> (<kind>) has failed. Use 'tide resume <project> --retry-failed' to recover."` and return an error. This makes the error actionable rather than misleading.

---

## Architecture Patterns

### System Architecture Diagram

```
tide approve <project>
    │
    ▼
approveRun (cmd/tide/approve.go)
    │─── check for Failed levels → error + "use tide resume --retry-failed"  [NEW D-07]
    │
    ├─── approveLevel() → finds AwaitingApproval child → writes approve-<level>=true annotation
    └─── approveWave() → writes approve-wave-N=true annotation
         │
         ▼
    Reconciler (next loop)
         │
         ├── AwaitingApproval branch detects annotation
         │     ├── consume annotation (ConsumeApprove — one-shot)
         │     ├── patch Status.Phase = "Running"         [NEW D-04]
         │     ├── set ApprovedByUser condition            [NEW D-04]
         │     └── return (let children-gated succession decide Succeeded)
         │
         └── BoundaryDetected + all children Succeeded
               └── patchMilestoneSucceeded / patchPhaseSucceeded


tide reject <project>
    │
    ▼
rejectRun → writes tideproject.k8s/reject on Project
    │
    ▼
Each child reconciler (Phase/Plan/Task) at gate hook
    ├── CheckRejected(project) == true
    ├── park at Rejected condition (Reason=RejectedByUser)  [CHANGED D-05]
    └── return — NO patchPlanFailed / patchMilestoneFailed


tide resume <project> [--retry-failed]
    │
    ▼
resumeRun (cmd/tide/resume.go)
    ├── ConsumeReject → patch Project (clear annotation) [EXISTING]
    └── [NEW --retry-failed flag]
          ├── list all levels (Milestone/Phase/Plan/Task) for project
          ├── for each with Status.Phase=="Failed":
          │     patch Status.Phase="" + clear conditions
          │     set ResumedByUser condition
          └── reconcilers re-enter normal dispatch path on next loop


Child reconciler (Phase/Plan/Task) dispatch entry  [NEW D-02 parent-approval hold]
    │
    ▼
checkParentApproval(ctx, client, levelObj)
    ├── fetch parent level (Milestone for Phase, Phase for Plan, Plan for Task)
    ├── parent.Status.Phase == "AwaitingApproval" → park child, return early
    └── pass → proceed with dispatch
```

### Recommended Project Structure

No new packages or directories. Changes are in:
```
internal/
├── controller/
│   ├── milestone_controller.go     — fix AwaitingApproval branch, reject park, add AwaitingApproval return
│   ├── phase_controller.go         — add AwaitingApproval branch, fix reject park
│   ├── plan_controller.go          — fix reject park (patchPlanFailed → park)
│   ├── task_controller.go          — add parent-approval hold in gateChecks
│   └── dispatch_helpers.go         — add checkParentApproval helper
├── gates/
│   └── annotation.go               — no changes needed (primitives are correct)
api/
└── v1alpha1/
    └── shared_types.go             — add ReasonApprovedByUser constant
cmd/tide/
├── approve.go                      — add Failed-level detection + D-07 error
└── resume.go                       — add --retry-failed flag + status reset
docs/
└── gates.md                        — rewrite step 5 (GATE-02)
```

### Pattern 1: D-04 — Approval consumes annotation, returns to Running

The correct pattern after consuming the approval annotation is NOT to call `patchMilestoneSucceeded` directly. Instead:

```go
// Source: internal/controller/milestone_controller.go (PROPOSED)
if ms.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(ms, "milestone") {
        // Consume annotation (one-shot, T-04-G2)
        newAnno := gates.ConsumeApprove(ms, "milestone")
        patch := client.MergeFrom(ms.DeepCopy())
        ms.SetAnnotations(newAnno)
        if err := r.Patch(ctx, ms, patch); err != nil {
            return ctrl.Result{}, err
        }
        // Return to Running + record ApprovedByUser condition
        statusPatch := client.MergeFrom(ms.DeepCopy())
        ms.Status.Phase = "Running"
        meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
            Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
            Status:             metav1.ConditionFalse,
            Reason:             tideprojectv1alpha1.ReasonApprovedByUser,  // NEW constant
            Message:            "Milestone approved; children will dispatch",
            LastTransitionTime: metav1.Now(),
        })
        if err := r.Status().Patch(ctx, ms, statusPatch); err != nil {
            return ctrl.Result{}, err
        }
        // Requeue immediately — the Running branch will call handleJobCompletion
        // which contains the ChildCount-gated succession
        return ctrl.Result{Requeue: true}, nil
    }
    return ctrl.Result{}, nil
}
```

Then, in the `Running` branch, the existing `handleJobCompletion` call handles ChildCount-gated succession correctly when `envReadOK=true`.

**Key invariant (D-03):** The `patchMilestoneSucceeded` function is ONLY called from within `handleJobCompletion` after the ChildCount gate passes. The AwaitingApproval branch must never call it directly.

### Pattern 2: D-02 — Parent-approval hold (shared helper)

```go
// Source: internal/controller/dispatch_helpers.go (PROPOSED)
// checkParentApproval returns true if the direct parent level is parked at
// AwaitingApproval. The child reconciler should park and requeue without dispatching.
// parentRef is the spec-level reference name (e.g., phase.Spec.MilestoneRef).
func checkParentApproval(ctx context.Context, c client.Client, ns, parentRef string, parentKind string) (bool, error) {
    switch parentKind {
    case "Milestone":
        var ms tideprojectv1alpha1.Milestone
        if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentRef}, &ms); err != nil {
            return false, client.IgnoreNotFound(err)
        }
        return ms.Status.Phase == "AwaitingApproval", nil
    case "Phase":
        var ph tideprojectv1alpha1.Phase
        // ... etc
    case "Plan":
        var plan tideprojectv1alpha1.Plan
        // ... etc
    }
    return false, nil
}
```

Each child reconciler calls this at dispatch entry, before plannerPool.Acquire or Job creation.

### Pattern 3: D-05 — Reject parks, never fail-marks

```go
// Source: internal/controller/plan_controller.go (PROPOSED) — replaces patchPlanFailed call
if project != nil && gates.CheckRejected(project) {
    patch := client.MergeFrom(plan.DeepCopy())
    meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
        Status:             metav1.ConditionTrue,
        Reason:             tideprojectv1alpha1.ReasonRejectedByUser,
        Message:            fmt.Sprintf("Rejected: %s", gates.RejectedReason(project)),
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, plan, patch); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil  // No Status.Phase=Failed
}
```

The reconciler's terminal short-circuit (`Succeeded || Failed`) does NOT fire because `Status.Phase` is not set to `Failed`. The next reconcile after `tide resume` clears the annotation re-enters the normal dispatch path.

### Pattern 4: D-06 — resume --retry-failed status reset

The `--retry-failed` implementation in `resumeRun` must use the status subresource patch. The run-1 recovery recipe (`kubectl patch --subresource=status --type=merge -p '{"status":{"phase":"","conditions":[]}}'`) is the behavioral spec.

```go
// Source: cmd/tide/resume.go (PROPOSED)
func resetFailedLevel(ctx context.Context, c client.Client, obj client.Object, level string) error {
    patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
    // Use reflection or type switch to set Status.Phase = "" and clear conditions
    // Must use Status().Patch for status subresource
    ...
}
```

**Constraint:** The status subresource requires a separate Patch call from the metadata patch. The CLI must call `c.Status().Patch(ctx, obj, patch)` not `c.Patch(ctx, obj, patch)` for the status fields. This mirrors `resumeRun`'s existing metadata patch for the annotation.

### Anti-Patterns to Avoid

- **Direct patchMilestoneSucceeded in AwaitingApproval branch:** The current code calls this when the planner Job is TTL'd. This bypasses the ChildCount gate and is the root cause of GATE-01.
- **Approval path as a spend-retry:** `tide approve` must check for Failed state and refuse before writing the annotation (D-07). Writing an approve annotation on a Failed level would make the reconciler enter a weird state since the Failed early-exit fires before the gate hook.
- **Status.Phase=Failed for reject:** The reconcilers' existing terminal short-circuit permanently wedges rejected levels. D-05's change ensures reject never writes Failed (only ConditionWaveOrLevelPaused with RejectedByUser reason).
- **Resume without --retry-failed clearing Status.Phase:** Just clearing the reject annotation on the Project does nothing for already-Failed child levels. The `--retry-failed` flag is the deliberate opt-in for resetting status.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Annotation consume | custom map mutation | `gates.ConsumeApprove` / `gates.ConsumeReject` | T-04-G2 purity contract; one-shot semantics built in |
| Status condition write | direct struct assignment | `meta.SetStatusCondition` | Handles LastTransitionTime, idempotency, existing condition replacement |
| Parent level lookup | raw Get with hardcoded GVK | type-switch using existing spec ref fields | Reconcilers already have the parent name in spec; consistent with idempotency guard pattern |
| Status subresource patch | c.Update (puts full object) | `c.Status().Patch` with `client.MergeFrom` | Only patches status; avoids metadata write conflicts |

---

## Common Pitfalls

### Pitfall 1: ChildCount envelope read failure swallows the guard

**What goes wrong:** In `handleJobCompletion`'s fallback path (EnvReader nil or readErr != nil), the code falls through to `patchMilestoneSucceeded` / `patchPhaseSucceeded` without the ChildCount gate. After D-04 lands (approval returning to `Running` and requeueing), the Running branch calls `handleJobCompletion` again. If `EnvReader` errors, the fallback fires and Succeeded is written unconditionally.

**How to avoid:** The D-03 fix must make `patchMilestoneSucceeded` / `patchPhaseSucceeded` unreachable from `handleJobCompletion` UNLESS either (a) `expected == 0` (genuine leaf) OR (b) `BoundaryDetected` returns true. The fallback path must also guard on `hasChildPhases` / `BoundaryDetected` before calling patchSucceeded.

**Warning signs:** Unit test passes (envReader set, ChildCount > 0 but observed == 0, so requeues), but integration test fails (TTL'd Job + nil EnvReader fallback fires patchSucceeded).

### Pitfall 2: Phase reconciler lacks AwaitingApproval early-return

**What goes wrong:** `phase_controller.go:reconcilePlannerDispatch` starts with only the `Succeeded || Failed` short-circuit. A Phase parked at AwaitingApproval re-enters the body, checks the idempotency guard (child Plan already exists), returns early. But on the next reconcile it re-enters again, falls through to the idempotency guard... it is a constant requeue loop. This is finding-2 (Phase oscillation).

**How to avoid:** Add the same AwaitingApproval branch to `phase_controller.go:reconcilePlannerDispatch` as exists in `milestone_controller.go:reconcilePlannerDispatch`. The fix for D-04 (return Running + ApprovedByUser on approval) naturally stops the oscillation once the approve annotation arrives; the early-return before that prevents the requeue loop.

**Warning signs:** `kubectl get phase -w` shows Phase alternating AwaitingApproval ↔ Running every few seconds with no Jobs created.

### Pitfall 3: resume --retry-failed on in-flight levels

**What goes wrong:** If `--retry-failed` resets the status of a level that has an executor Job currently running (e.g., a Task in Running state), clearing `Status.Phase` causes the reconciler to re-enter the dispatch path and create a second Job.

**How to avoid:** `--retry-failed` should only reset levels with `Status.Phase == "Failed"`. The `Failed` state already means no Job is running (they exited). Running-state levels are not touched.

**Warning signs:** Two Jobs for the same Task UID appear in the cluster after resume --retry-failed.

### Pitfall 4: Reject park condition conflicts with AwaitingApproval condition

**What goes wrong:** Both reject-park (D-05) and AwaitingApproval use `ConditionWaveOrLevelPaused`. If a level is simultaneously rejected AND awaiting approval (e.g., reject fires after a level was already parked at AwaitingApproval), the condition type/reason must unambiguously signal which state the level is in.

**How to avoid:** `CheckRejected` fires BEFORE the gate-policy hook in all reconcilers. So a rejected level is always caught before it can enter AwaitingApproval. These two states cannot co-exist. The condition is fine to share; just ensure the CheckRejected gate fires first (it already does in the existing code).

**Warning signs:** A level shows `Reason=RejectedByUser` but still has an approve annotation that reconciler tries to consume.

### Pitfall 5: approveLevel finds child levels, not the project's immediate AwaitingApproval level

**What goes wrong:** When a Milestone is in AwaitingApproval AND its children are also parked (D-02's parent-approval hold), `approveLevel` might find a lower-level child (Phase or Plan) that is also in AwaitingApproval and approve the wrong level.

**How to avoid:** D-02's parent-approval hold should use a different parking mechanism than `Status.Phase=AwaitingApproval` for the child. Using `Status.Phase="" + ConditionWaveOrLevelPaused (Reason=ParentAwaitingApproval)` for the child's hold state ensures `findAwaitingPhase`/`findAwaitingPlan`/`findAwaitingTask` won't find it. The parent (Milestone) is the only level with `Status.Phase=AwaitingApproval` at a given time.

**Warning signs:** `tide approve` writes the annotation on a Phase when the operator intended to approve the Milestone.

---

## Code Examples

### Existing ResumedByUser condition pattern (template for ApprovedByUser)

```go
// Source: internal/controller/plan_controller.go:520-528
meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
    Status:             metav1.ConditionFalse,
    Reason:             tideprojectv1alpha1.ReasonResumedByUser,
    Message:            "Plan resumed from gate boundary",
    LastTransitionTime: metav1.Now(),
})
```

The new `ReasonApprovedByUser` constant follows the same pattern; `Status: metav1.ConditionFalse` indicates the pause is lifted.

### Existing ChildCount-gated succession guard (must be preserved)

```go
// Source: internal/controller/milestone_controller.go:495-522
if envReadOK {
    expected := out.ChildCount
    if expected == 0 {
        return r.patchMilestoneSucceeded(ctx, ms)
    }
    observed := r.countChildPhases(ctx, ms)
    if observed < expected {
        return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
    }
    detected, derr := gates.BoundaryDetected(ctx, r.Client, ms, "Phase")
    // ...
    if detected {
        // ...
        return r.patchMilestoneSucceeded(ctx, ms)
    }
    return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}
```

This is the authoritative succession logic. D-03's fix routes approval back through this path.

### Existing test harness patterns (for regression tests)

```go
// Source: internal/controller/milestone_gates_test.go — Ginkgo envtest pattern
// driveToJobCompletion simulates a planner Job completing via fake Job terminal state
envReader := newMapEnvReader()
r := &MilestoneReconciler{
    Client: mgrClient, Scheme: k8sClient.Scheme(),
    Dispatcher: &stubDispatcher{}, PlannerPool: newPlannerPoolForTest(),
    EnvReader: envReader, ...
}
driveToJobCompletion(msName, r, envReader)
// Then assert AwaitingApproval, inject approval annotation, reconcile, assert Running+ApprovedByUser
// Then drive child completion, assert Succeeded
```

The GATE-01 regression test needs `envReader.SetOut(uid, pkgdispatch.EnvelopeOut{ChildCount: 5})` to simulate 5 Phase children expected, verify no `patchMilestoneSucceeded` fires after approval.

### Run-1 recovery recipe (D-06 behavioral spec)

```bash
# The kubectl recipe tide resume --retry-failed must replicate:
kubectl patch milestone <name> --subresource=status --type=merge \
    -p '{"status":{"phase":"","conditions":[]}}'
# Reconciler then re-dispatches and sets ResumedByUser condition
```

---

## State of the Art

| Old Approach | Current Approach | Impact |
|--------------|------------------|--------|
| Approve → patchSucceeded directly | Approve → Running + ApprovedByUser → ChildCount-gated Succeeded | D-03: approval no longer bypasses children |
| Reject → patchPlanFailed (Failed) | Reject → park condition (no Failed write) | D-05: resume can recover |
| resume → clears annotation only | resume --retry-failed → clears annotation + resets Status.Phase | D-06: Failed levels recoverable |
| Child reconcilers dispatch on parent AwaitingApproval | Child reconcilers hold dispatch until parent approves | D-02/GATE-04: zero spend during review |
| tide approve on Failed → "no level awaiting approval" | tide approve on Failed → actionable error pointing to resume | D-07: operator not confused |
| gates.md step 5: "advances to Succeeded" | gates.md step 5: documents approve-then-wait-for-children | GATE-02: doc matches reality |

**Deprecated/outdated:**
- `patchMilestoneSucceeded` called from AwaitingApproval branch: removed by D-03/D-04 fix.
- `patchPlanFailed` (and its milestone/phase equivalents) called from `CheckRejected` gate hook: removed by D-05 fix.
- `resume.go`'s annotation-only approach: extended with `--retry-failed` for D-06.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (controller-unit), Go testing (plain tests in kind package) |
| Config file | none (suites use `RunSpecs` in `*_test.go` package files) |
| Quick run command | `make test` (unit tier, -short) |
| Full suite command | `make test-int` (Layer A envtest + Layer B kind) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| GATE-01 | Approve milestone with N>0 incomplete children → Status.Phase stays Running (not Succeeded) until children complete | envtest (Ginkgo) | `go test ./internal/controller/... -run TestGateMilestoneApproveDoesNotSucceedWithChildren` | ❌ Wave 0 (new test in milestone_gates_test.go) |
| GATE-01 | Existing TestGateApproveFlow in gates_test.go currently asserts Succeeded immediately — must be updated to assert Running+ApprovedByUser then wait for child completion | envtest (Ginkgo) | `go test ./test/integration/envtest/... --ginkgo.label-filter=envtest` | ✅ existing — needs modification |
| GATE-02 | gates.md step 5 does NOT contain "advances the level to Succeeded" | manual / grep | `grep -c "advances the level to Succeeded" docs/gates.md` returns 0 | ✅ existing file, doc change |
| GATE-03 | tide approve on Failed level returns error mentioning resume --retry-failed | unit test | `go test ./cmd/tide/... -run TestApproveRunFailedLevelError` | ❌ Wave 0 (new test in approve_test.go) |
| GATE-04 | Phase parked at AwaitingApproval → child Phase reconciler holds planner Job dispatch | envtest (Ginkgo) | `go test ./internal/controller/... -run TestPhaseDispatchHoldWhileParentAwaiting` | ❌ Wave 0 (new test in phase_gates_test.go) |
| GATE-04 | No planner Jobs exist on child Phases while Milestone is AwaitingApproval | envtest (Ginkgo) | `go test ./test/integration/envtest/... --ginkgo.label-filter=envtest` | ❌ Wave 0 (new spec in gates_test.go) |
| RESUME-01 | tide resume --retry-failed resets Status.Phase on Failed Plans | unit test | `go test ./cmd/tide/... -run TestResumeRunRetryFailed` | ❌ Wave 0 (new test in resume_test.go) |
| RESUME-01 | After resume --retry-failed, plan reconciler re-dispatches | envtest (Ginkgo) | `go test ./test/integration/envtest/... --ginkgo.label-filter=envtest` | ❌ Wave 0 (new spec in gates_test.go) |

### Sampling Rate

- **Per task commit:** `make test` (unit tier, ~30s)
- **Per wave merge:** `make test-int-fast` (Layer A envtest only, ~90s)
- **Phase gate:** `make test-int` (full Layer A + Layer B kind) before `/gsd:verify-work`

**NOTE on make test-int:** Per CLAUDE.md, check BOTH `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'` in the log — Ginkgo "Ran X specs SUCCESS" can coexist with a red go-test that fails the package. Do not claim green until `MAKE_EXIT=0` verified.

### Wave 0 Gaps

- [ ] `internal/controller/milestone_gates_test.go` — add spec: approve with ChildCount>0 stays Running until children Succeeded
- [ ] `internal/controller/phase_gates_test.go` — add spec: dispatch hold when parent Milestone is AwaitingApproval
- [ ] `test/integration/envtest/gates_test.go` — add specs: GATE-04 no-child-jobs-while-parent-awaiting, RESUME-01 retry-failed recovery
- [ ] `cmd/tide/approve_test.go` — add: TestApproveRunFailedLevelError
- [ ] `cmd/tide/resume_test.go` — add: TestResumeRunRetryFailed with status reset assertion
- [ ] Modify existing `TestGateApproveFlow` in `test/integration/envtest/gates_test.go` — assert Running+ApprovedByUser after approval (not Succeeded), then drive child completion, then assert Succeeded

---

## Security Domain

The phase makes no authentication, authorization, or cryptographic changes. Gate enforcement uses the existing annotation-based handshake. ASVS categories V2/V3/V4/V6 do not apply. V5 (input validation) is tangentially relevant: the `--retry-failed` flag has no new input surface beyond the existing project-name argument. The existing `approveWave` DNS-1123 validation pattern is not replicated here (project names are already validated by the existing argument path).

---

## Open Questions (RESOLVED — choices locked in plan tasks: Q1 → 12-03/T1 (`Status.Phase=""`, no new condition), Q2 → 12-04/T1 (in-flight Jobs drain), Q3 → 12-02/T1 (full-tree walk))

1. **D-02: What condition state should child levels show during parent-approval hold?**
   - What we know: Current options are `Status.Phase=AwaitingApproval` (simple, but `approveLevel` would then find them) or `Status.Phase="" + ConditionWaveOrLevelPaused (Reason=ParentAwaitingApproval)` (correct but requires a new Reason constant).
   - What's unclear: Whether a new `ReasonParentAwaitingApproval` constant is worth adding, or whether keeping children in their current empty-phase state (no new condition) is cleaner and sufficient.
   - Recommendation: Use `Status.Phase=""` with NO new condition for the child hold — this is the simplest approach and avoids `approveLevel` accidentally targeting held children. The parent's `AwaitingApproval` state is visible in the hierarchy. Planner's call.

2. **D-05: Whether reject cancels in-flight Jobs or lets them drain.**
   - What we know: Draining is simpler (no Job deletion logic), consistent with "preserves all state" promise.
   - What's unclear: Whether an operator who rejects mid-wave expects the running tasks to stop.
   - Recommendation: Let in-flight Jobs drain. Cancellation would require tracking which Jobs belong to the rejected run — complex. The park condition halts NEW dispatch, which is what matters. Planner's call.

3. **D-06: Should resume --retry-failed walk the full child tree from Project, or require specifying individual level names?**
   - What we know: Walking the full tree is more convenient (one command recovers everything); targeting individual levels is safer (avoid accidentally resurrecting legitimately dead work).
   - Recommendation: Walk the full tree — the `--retry-failed` flag is already the "deliberate friction" D-06 calls for. An additional `--level <name>` sub-flag can be added later if needed. This matches the run-1 kubectl recipe's "patch everything" approach.

---

## Environment Availability

Step 2.6: SKIPPED — this phase is purely code and doc changes (Go source, docs/gates.md). No external tools, CLIs, or databases beyond the existing Go + envtest + kind toolchain, all confirmed present from prior phases.

---

## Assumptions Log

No assumed claims. All findings in this research are verified directly from the live codebase.

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| — | None | — | — |

---

## Sources

### Primary (HIGH confidence — live codebase)

- `internal/controller/milestone_controller.go` (lines 200–542) — reconcilePlannerDispatch, handleJobCompletion, AwaitingApproval branch, ChildCount-gated succession, patchMilestoneFailed
- `internal/controller/phase_controller.go` (lines 183–460) — reconcilePlannerDispatch, handleJobCompletion, reject gate hook, patchPhaseFailed
- `internal/controller/plan_controller.go` (lines 200–600) — reconcilePlannerDispatch, handlePlannerJobCompletion, patchPlanFailed at :478, Failed early-exit at :222, ResumedByUser sites
- `internal/controller/task_controller.go` (lines 275–396) — gateChecks, checkReadinessGates, reject short-circuit, AwaitingApproval dispatch block
- `internal/gates/annotation.go` — ConsumeApprove/CheckApprove/CheckRejected/ConsumeReject primitives
- `cmd/tide/approve.go` — approveRun, approveLevel, approveWave, findAwaiting* functions
- `cmd/tide/reject.go` — rejectRun
- `cmd/tide/resume.go` — resumeRun (annotation-only behavior confirmed)
- `api/v1alpha1/shared_types.go` (lines 128–155) — existing condition/reason constants; ReasonApprovedByUser does NOT exist yet
- `docs/gates.md` — step 5 confirmed: "advances the level to Succeeded" text present; `Approved` phase-value sketch at line 99
- `internal/controller/milestone_gates_test.go` — existing gate test patterns
- `test/integration/envtest/gates_test.go` — TestGateApproveFlow (currently asserts Succeeded immediately — needs updating for D-04)
- Memory file `project_dogfood_run1_findings.md` — run-1 symptom descriptions for findings 1, 5, 7, 9a

---

## Metadata

**Confidence breakdown:**
- Root cause analysis: HIGH — directly verified in source code
- Fix shapes: HIGH — follow established patterns (ConsumeApprove, ChildCount gate, ResumedByUser)
- Test infrastructure: HIGH — existing Ginkgo envtest suite confirmed, patterns verified
- CLI behavior: HIGH — approve.go/reject.go/resume.go read in full

**Research date:** 2026-06-11
**Valid until:** This research is based on a specific commit state. Verify against HEAD before planning if any gate-related commits land on main.
