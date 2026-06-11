# Phase 12: Gate Semantics + Reject/Resume - Pattern Map

**Mapped:** 2026-06-11
**Files analyzed:** 10 modified files
**Analogs found:** 10 / 10

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/controller/milestone_controller.go` | controller | request-response | `internal/controller/phase_controller.go` | exact (same gate pattern, symmetric fix) |
| `internal/controller/phase_controller.go` | controller | request-response | `internal/controller/milestone_controller.go` | exact |
| `internal/controller/plan_controller.go` | controller | request-response | `internal/controller/milestone_controller.go` | exact |
| `internal/controller/task_controller.go` | controller | request-response | `internal/controller/phase_controller.go` | exact |
| `internal/controller/dispatch_helpers.go` | utility | request-response | `internal/controller/dispatch_helpers.go` (existing) | role-match (add shared helper) |
| `api/v1alpha1/shared_types.go` | model | — | `api/v1alpha1/shared_types.go` (existing constants block) | exact |
| `cmd/tide/approve.go` | utility | request-response | `cmd/tide/resume.go` | exact (same CLI seam pattern) |
| `cmd/tide/resume.go` | utility | request-response | `cmd/tide/approve.go` | exact (same CLI seam pattern) |
| `docs/gates.md` | config | — | `docs/gates.md` (existing) | exact (doc rewrite of step 5) |
| `internal/controller/milestone_gates_test.go` | test | — | `internal/controller/phase_gates_test.go` | exact |
| `internal/controller/phase_gates_test.go` | test | — | `internal/controller/milestone_gates_test.go` | exact |
| `test/integration/envtest/gates_test.go` | test | — | `test/integration/envtest/gates_test.go` (existing) | exact (add specs to existing file) |
| `cmd/tide/approve_test.go` | test | — | `cmd/tide/resume_test.go` | exact |
| `cmd/tide/resume_test.go` | test | — | `cmd/tide/approve_test.go` | exact |

---

## Pattern Assignments

### `internal/controller/milestone_controller.go` (controller, request-response)

**Analog:** `internal/controller/phase_controller.go` (symmetric reconciler one level down)

**D-04 fix: AwaitingApproval branch must NOT call patchMilestoneSucceeded directly.**

Current buggy pattern (milestone_controller.go lines 214–224):
```go
if ms.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(ms, "milestone") {
        jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
        var job batchv1.Job
        if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: jobName}, &job); err == nil {
            return r.handleJobCompletion(ctx, ms, &job)
        }
        // BUG: calls patchMilestoneSucceeded without ChildCount guard
        return r.patchMilestoneSucceeded(ctx, ms)
    }
    return ctrl.Result{}, nil
}
```

**Replacement pattern — consume annotation, return to Running, let children-gated succession decide:**
```go
// Copy: annotation-consume two-step (MergeFrom object patch + Status patch)
// Source: milestone_controller.go lines 473–480 (gate-policy hook in handleJobCompletion)
if ms.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(ms, "milestone") {
        // Consume annotation (T-04-G2 one-shot)
        newAnno := gates.ConsumeApprove(ms, "milestone")
        annoPatch := client.MergeFrom(ms.DeepCopy())
        ms.SetAnnotations(newAnno)
        if err := r.Patch(ctx, ms, annoPatch); err != nil {
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
        return ctrl.Result{Requeue: true}, nil
    }
    return ctrl.Result{}, nil
}
```

**D-05 fix: reject parks, does not fail-mark. In handleJobCompletion (lines 411–412):**

Existing pattern to replace:
```go
// milestone_controller.go:411-412 — BUG: writes Failed on reject
if project != nil && gates.CheckRejected(project) {
    return r.patchMilestoneFailed(ctx, ms, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
}
```

Copy park pattern from `patchMilestoneAwaitingApproval` (lines 601–621):
```go
// Source: milestone_controller.go:601-621 — patchMilestoneAwaitingApproval
func (r *MilestoneReconciler) patchMilestoneAwaitingApproval(...) (ctrl.Result, error) {
    patch := client.MergeFrom(ms.DeepCopy())
    ms.Status.Phase = "AwaitingApproval"
    meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
        Status:             metav1.ConditionTrue,
        Reason:             reason,
        Message:            message,
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, ms, patch); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

New `patchMilestoneRejected` helper to add follows the same structure, with `Reason=ReasonRejectedByUser` and no `Status.Phase=Failed`.

**Fallback path guard (Pitfall 1 from RESEARCH.md):**
The fallback block in `handleJobCompletion` (lines 525–542) calls `patchMilestoneSucceeded` unconditionally when `!envReadOK`. After D-04 lands the Running branch calls `handleJobCompletion` again, and a nil/erroring EnvReader triggers this fallback. The fallback must NOT call `patchMilestoneSucceeded` unless `expected==0` OR `BoundaryDetected` — copy the nil-EnvReader fallback detection from phase_controller.go lines 440–455 which already correctly gates on `hasChildPlans` before calling `patchPhaseSucceeded`.

---

### `internal/controller/phase_controller.go` (controller, request-response)

**Analog:** `internal/controller/milestone_controller.go`

**D-01 fix: phase_controller.go lacks the AwaitingApproval early-return.**

`reconcilePlannerDispatch` (line 187) currently starts with only `Succeeded || Failed` short-circuit. The AwaitingApproval branch from milestone_controller.go lines 214–225 must be added symmetrically, in the same position (after terminal short-circuit, before jobName construction):

```go
// Source: milestone_controller.go lines 206–225 — copy verbatim, change "milestone" to "phase"
// Step 1: Terminal short-circuit.
if ph.Status.Phase == "Succeeded" || ph.Status.Phase == "Failed" {
    return ctrl.Result{}, nil
}
// Step 1a: AwaitingApproval — same D-04 pattern as milestone
if ph.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(ph, "phase") {
        // consume annotation + patch Running + ApprovedByUser condition + Requeue
        ...  // identical structure to milestone D-04 fix above
    }
    return ctrl.Result{}, nil
}
```

**D-05 fix: phase_controller.go handleJobCompletion lines 378–379 — same reject-park replacement as milestone.**

Existing buggy pattern:
```go
// phase_controller.go:378-379
if project != nil && gates.CheckRejected(project) {
    return r.patchPhaseFailed(ctx, ph, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
}
```

Copy `patchPhaseAwaitingApproval` structure (lines 507–527) with `Reason=ReasonRejectedByUser`. New `patchPhaseRejected` helper: same MergeFrom + Status.Patch shape.

**Status patch pattern** (phase_controller.go lines 309–321):
```go
patch := client.MergeFrom(ph.DeepCopy())
ph.Status.Phase = "Running"
meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionAuthoringPlanner,
    Status:             metav1.ConditionTrue,
    Reason:             "PlannerDispatched",
    Message:            fmt.Sprintf("Planner Job %s dispatched", jobName),
    LastTransitionTime: metav1.Now(),
})
if err := r.Status().Patch(ctx, ph, patch); err != nil {
    return ctrl.Result{}, err
}
```

---

### `internal/controller/plan_controller.go` (controller, request-response)

**Analog:** `internal/controller/milestone_controller.go`

**D-05 fix: patchPlanFailed call on reject path.**

The buggy call is at line 478:
```go
// plan_controller.go:478 — BUG: reject writes Failed
if project != nil && gates.CheckRejected(project) {
    return r.patchPlanFailed(ctx, plan, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
}
```

Copy `patchPlanAwaitingApproval` structure (lines 582–600) to make a `patchPlanRejected` helper:
```go
// Source: plan_controller.go:582-600 — patchPlanAwaitingApproval
func (r *PlanReconciler) patchPlanAwaitingApproval(ctx context.Context, plan *tideprojectv1alpha1.Plan, policy tideprojectv1alpha1.GatePolicy) (ctrl.Result, error) {
    reason := tideprojectv1alpha1.ReasonAwaitingApproval
    message := "Plan awaiting operator approve annotation (tideproject.k8s/approve-plan=true)"
    if policy == gates.PolicyPause {
        reason = tideprojectv1alpha1.ReasonPausedAtBoundary
        message = "Plan paused at boundary; requires explicit resume"
    }
    patch := client.MergeFrom(plan.DeepCopy())
    plan.Status.Phase = "AwaitingApproval"
    meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
        Status:             metav1.ConditionTrue,
        Reason:             reason,
        Message:            message,
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, plan, patch); err != nil {
        return ctrl.Result{}, err
    }
```

New `patchPlanRejected`: same shape, `Status.Phase` NOT set to `Failed`, `Reason=ReasonRejectedByUser`, `Status: metav1.ConditionTrue` (paused=true).

**ResumedByUser condition pattern** (plan_controller.go lines 520–528 — template for ApprovedByUser):
```go
// Source: plan_controller.go:520-528
meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
    Status:             metav1.ConditionFalse,
    Reason:             tideprojectv1alpha1.ReasonResumedByUser,
    Message:            "Plan resumed from gate boundary",
    LastTransitionTime: metav1.Now(),
})
```

The new `ReasonApprovedByUser` constant uses `Status: metav1.ConditionFalse` (pause lifted) — identical shape.

**Plan has no AwaitingApproval early-return in reconcilePlannerDispatch**, but its terminal short-circuit at line 222 (`Succeeded || Failed`) covers it once `Status.Phase=""` is used for the child hold (D-02). The plan reconciler's gate hook is in `handlePlannerJobCompletion` (lines 478–494) — the approve-consume path there already calls `ConsumeApprove` + `Patch` (line 487–492) correctly; the only fix needed is the reject-park replacement at line 478.

---

### `internal/controller/task_controller.go` (controller, request-response)

**Analog:** `internal/controller/phase_controller.go` `gateChecks` function

**D-05 fix: patchTaskFailed call on reject path.**

The buggy call is in `gateChecks` (lines 313–315):
```go
// task_controller.go:313-315 — BUG: reject writes Failed
if gates.CheckRejected(project) {
    result, err := r.patchTaskFailed(ctx, task, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
    return taskGateResult{shouldHalt: true, result: result}, err
}
```

Copy `patchTaskAwaitingApproval` structure (lines 675–688) to make a park-only path:
```go
// Source: task_controller.go:675-688
func (r *TaskReconciler) patchTaskAwaitingApproval(ctx context.Context, task *tideprojectv1alpha1.Task, policy tideprojectv1alpha1.GatePolicy) (ctrl.Result, error) {
    reason := tideprojectv1alpha1.ReasonAwaitingApproval
    message := "Task awaiting operator approve annotation (tideproject.k8s/approve-task=true)"
    if policy == gates.PolicyPause {
        reason = tideprojectv1alpha1.ReasonPausedAtBoundary
        message = "Task paused at boundary; requires explicit resume"
    }
    patch := client.MergeFrom(task.DeepCopy())
    task.Status.Phase = "AwaitingApproval"
    meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
        Status:             metav1.ConditionTrue,
        Reason:             reason,
        Message:            message,
        LastTransitionTime: metav1.Now(),
```

New `patchTaskRejected`: same MergeFrom + Status.Patch shape, `Status.Phase` NOT set to `Failed`, `Reason=ReasonRejectedByUser`.

**D-02 parent-approval hold in gateChecks:** The check must be inserted after Step 3 (resolveProject, line 287) and before Step 4 (budget gate, line 319), calling the new `checkParentApproval` helper from `dispatch_helpers.go`. If the parent Plan is AwaitingApproval, return `taskGateResult{shouldHalt: true}` with no status write (child stays at its current empty phase, not at AwaitingApproval — see Pitfall 5 in RESEARCH.md).

---

### `internal/controller/dispatch_helpers.go` (utility, request-response)

**Analog:** `internal/controller/dispatch_helpers.go` (the file already exists; add a new helper function)

**Existing file structure** (lines 1–56): package declaration, imports `client`, `tideprojectv1alpha1`, etc. New helper appends to the bottom.

**Imports already present in file** (lines 39–56):
```go
import (
    "context"
    ...
    "sigs.k8s.io/controller-runtime/pkg/client"
    tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
    ...
)
```

**`checkParentApproval` helper pattern** — copy the parent-lookup style from `resolveProject` in phase_controller.go (lines 546–562) but check `Status.Phase` instead of returning the object:
```go
// Source: phase_controller.go:546-562 — resolveProject (parent-lookup shape)
func (r *PhaseReconciler) resolveProject(ctx context.Context, ph *tideprojectv1alpha1.Phase) *tideprojectv1alpha1.Project {
    if ph.Spec.MilestoneRef == "" {
        return nil
    }
    var ms tideprojectv1alpha1.Milestone
    if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
        return nil
    }
    ...
}
```

New function signature and pattern for `checkParentApproval`:
```go
// Follows the parent-lookup + status-check pattern from resolveProject above.
// Returns (true, nil) when the direct parent is parked at AwaitingApproval.
// Returns (false, nil) when the parent is not found (IgnoreNotFound — transient).
func checkParentApproval(ctx context.Context, c client.Client, ns, parentName, parentKind string) (bool, error) {
    switch parentKind {
    case "Milestone":
        var ms tideprojectv1alpha1.Milestone
        if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ms); err != nil {
            return false, client.IgnoreNotFound(err)
        }
        return ms.Status.Phase == "AwaitingApproval", nil
    case "Phase":
        var ph tideprojectv1alpha1.Phase
        if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ph); err != nil {
            return false, client.IgnoreNotFound(err)
        }
        return ph.Status.Phase == "AwaitingApproval", nil
    case "Plan":
        var plan tideprojectv1alpha1.Plan
        if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &plan); err != nil {
            return false, client.IgnoreNotFound(err)
        }
        return plan.Status.Phase == "AwaitingApproval", nil
    }
    return false, nil
}
```

Note: `client.IgnoreNotFound(err)` returns nil for NotFound — the helper returns `(false, nil)` so the caller continues dispatch rather than erroring (parent not yet cached is transient).

---

### `api/v1alpha1/shared_types.go` (model, —)

**Analog:** `api/v1alpha1/shared_types.go` existing Phase 4 constants block (lines 128–155)

**Add `ReasonApprovedByUser` constant** to the Phase 4 gate vocabulary block. Copy the existing block structure:
```go
// Source: api/v1alpha1/shared_types.go:128-155 — Phase 4 condition + reason vocabulary
const (
    ConditionWaveOrLevelPaused = "WaveOrLevelPaused"
    ReasonAwaitingApproval     = "AwaitingApproval"
    ReasonPausedAtBoundary     = "PausedAtBoundary"
    ReasonRejectedByUser       = "RejectedByUser"
    ReasonResumedByUser        = "ResumedByUser"
)
```

Add immediately after `ReasonResumedByUser`:
```go
// ReasonApprovedByUser — operator ran `tide approve`; the level's
// AwaitingApproval park is lifted (Status=ConditionFalse indicates pause cleared).
// Mirrors ReasonResumedByUser; no new Status.Phase enum — level returns to Running.
// Phase 12 D-04.
ReasonApprovedByUser = "ApprovedByUser"
```

---

### `cmd/tide/approve.go` (utility, request-response)

**Analog:** `cmd/tide/resume.go` (same CLI seam pattern)

**D-07 fix: check for Failed levels before searching AwaitingApproval.**

Insert a Failed-level scan at the start of `approveLevel` (after the Project Get, before the `findAwaiting*` loop), copying the `findAwaiting*` list-and-filter pattern from lines 171–238:
```go
// Source: approve.go:171-186 — findAwaitingMilestone list-and-filter pattern
func findAwaitingMilestone(ctx context.Context, c client.Client, ns, projectName string) (client.Object, string, error) {
    var list tidev1alpha1.MilestoneList
    if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
        return nil, "", fmt.Errorf("list milestones: %w", err)
    }
    for i := range list.Items {
        m := &list.Items[i]
        if m.Labels["tideproject.k8s/project"] != projectName {
            continue
        }
        if m.Status.Phase == "AwaitingApproval" {
            return m, "milestone", nil
        }
    }
    return nil, "", nil
}
```

New `findFailedLevel` follows identical structure but checks `Status.Phase == "Failed"`. `approveLevel` calls it first and returns the D-07 error if any failed level is found:
```go
// Pattern: copy findAwaitingMilestone shape; change Phase check to "Failed"
// Error message format: mirrors existing "tide: project %q not found" error style
return fmt.Errorf("tide: level %s (%s) has failed; use 'tide resume %s --retry-failed' to recover", name, kind, projectName)
```

**Note on MergeFrom + Patch pattern** (approve.go lines 244–258):
```go
// Source: approve.go:244-258 — patchApproveLevel — the canonical annotation-write pattern
func patchApproveLevel(ctx context.Context, c client.Client, obj client.Object, level string) error {
    original := obj.DeepCopyObject().(client.Object)
    patch := client.MergeFrom(original)
    anno := obj.GetAnnotations()
    if anno == nil {
        anno = map[string]string{}
    }
    anno[gates.AnnotationApprovePrefix+level] = "true"
    obj.SetAnnotations(anno)
    if err := c.Patch(ctx, obj, patch); err != nil {
        return fmt.Errorf("patch %s/%s: %w", level, obj.GetName(), err)
    }
    return nil
}
```

**Cobra command changes:** `newApproveCmd` (lines 262–289) needs no structural change — `approveRun` signature stays the same, only `approveLevel` changes internally.

---

### `cmd/tide/resume.go` (utility, request-response)

**Analog:** `cmd/tide/approve.go` (parallel CLI seam)

**D-06 fix: add `--retry-failed` flag.**

Current `resumeRun` (lines 46–61) only clears the reject annotation on the Project. Extend to:
1. Accept `retryFailed bool` parameter.
2. If `retryFailed`, list all levels (Milestone/Phase/Plan/Task) for the project, and for each with `Status.Phase=="Failed"` patch `Status.Phase=""` + clear conditions via `c.Status().Patch`.

**Status subresource patch pattern** — copy from `patchPlanFailed` in plan_controller.go (lines 562–578) but in CLI context using the standalone client:
```go
// Source: plan_controller.go:562-578 — patchPlanFailed status subresource pattern
func (r *PlanReconciler) patchPlanFailed(ctx context.Context, plan *tideprojectv1alpha1.Plan, reason, message string) (ctrl.Result, error) {
    patch := client.MergeFrom(plan.DeepCopy())
    plan.Status.Phase = "Failed"
    ...
    if err := r.Status().Patch(ctx, plan, patch); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

In the CLI the reconciler's `r.Status().Patch` becomes `c.Status().Patch(ctx, obj, patch)` — same MergeFrom shape, different receiver.

**List-and-filter pattern** — copy from `findAwaitingMilestone` shape in approve.go (lines 171–186) but filter on `Status.Phase == "Failed"`. Process all four kinds in sequence.

**ResumedByUser condition** to set on recovered levels — copy from plan_controller.go lines 520–528:
```go
// Source: plan_controller.go:520-528
meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
    Status:             metav1.ConditionFalse,
    Reason:             tideprojectv1alpha1.ReasonResumedByUser,
    Message:            "Plan resumed from gate boundary",
    LastTransitionTime: metav1.Now(),
})
```

**Cobra command changes:** `newResumeCmd` (lines 64–87) needs a `--retry-failed` flag added. Copy the `--wave` flag pattern from `newApproveCmd` (approve.go lines 262–289):
```go
// Source: approve.go:262-288 — newApproveCmd flag registration pattern
var waveFlag string
c := &cobra.Command{...}
c.Flags().StringVar(&waveFlag, "wave", "", "Approve a specific wave: <plan-name>/<integer>")
```

New: `var retryFailed bool` + `c.Flags().BoolVar(&retryFailed, "retry-failed", false, "Reset Status.Phase on Failed levels so reconcilers re-dispatch")`.

---

### `docs/gates.md` (doc, —)

**Analog:** `docs/gates.md` existing (rewrite step 5 of "End-to-end approve flow")

**GATE-02 fix:** Step 5 at line 82 currently reads:
```
5. Reconciler's next loop reads the annotation via `gates.ConsumeApprove`,
   clears the annotation in the same patch, advances the level to
   `Succeeded`. The Project condition flips off.
```

Replace with the D-04 two-step flow:
- Consume annotation → patch `Status.Phase=Running` + `ApprovedByUser` condition.
- On next loop, Running branch calls `handleJobCompletion` → ChildCount-gated succession → eventually `Succeeded`.

Also update the "Reject flow" section (lines 85–90) to reflect D-05 (park, not fail-mark) and D-06 (`resume --retry-failed` for Failed recovery). The existing operator vocabulary table (lines 40–44) gains a `--retry-failed` row for `tide resume`.

---

### `internal/controller/milestone_gates_test.go` (test, —)

**Analog:** `internal/controller/milestone_gates_test.go` (existing, add new spec)

**New spec: GATE-01 regression — approve with ChildCount>0 stays Running until children Succeeded.**

Copy the existing `driveToJobCompletion` helper (lines 79–94) and `Describe` block structure (lines 96+). The new test variant:
1. Sets `envReader.SetOut(uid, pkgdispatch.EnvelopeOut{ChildCount: 5})` — 5 children expected.
2. Drives to AwaitingApproval, injects approve annotation.
3. Reconciles.
4. Asserts `Status.Phase == "Running"` (NOT Succeeded) immediately after approval.
5. Asserts `ApprovedByUser` condition present with `Status: metav1.ConditionFalse`.
6. Asserts zero child Phase Jobs exist (no dispatch yet — GATE-04 regression).

Key assertion pattern from existing tests:
```go
// Source: milestone_gates_test.go — Eventually pattern for condition assertions
Eventually(func() string {
    _ = mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)
    return got.Status.Phase
}, 5*time.Second, 50*time.Millisecond).Should(Equal("AwaitingApproval"))
```

---

### `internal/controller/phase_gates_test.go` (test, —)

**Analog:** `internal/controller/milestone_gates_test.go`

**New spec: GATE-04 regression — Phase parked at AwaitingApproval holds child Plan dispatch.**

Add to existing file after existing specs. Copy `makeProjectAndMilestone` helper (phase_gates_test.go lines 36–57) and `cleanup` function (lines 59–80). The new test:
1. Creates a Milestone in `AwaitingApproval` state (using status patch on fake object).
2. Creates a Phase with `Spec.MilestoneRef` pointing at it.
3. Drives `PhaseReconciler.Reconcile`.
4. Asserts no planner Job exists for the Phase.
5. Approves the Milestone.
6. Drives reconciler again.
7. Asserts planner Job now exists.

---

### `test/integration/envtest/gates_test.go` (test, —)

**Analog:** `test/integration/envtest/gates_test.go` (existing, add specs to existing file)

**Existing pattern** (lines 83–120): `Describe("TestGateApproveFlow")` drives `MilestoneReconciler` via fake jobs and asserts `Succeeded`. This spec must be updated to assert `Running+ApprovedByUser` after approval, then drive child completion, then assert `Succeeded`.

**New specs to add:**
1. `TestNoChildJobsWhileParentAwaiting` — GATE-04: Milestone AwaitingApproval → create Phase → drive PhaseReconciler → assert zero planner Jobs for Phase.
2. `TestResumeRetryFailed` — RESUME-01: drive Plan to Failed → call `resumeRun(..., retryFailed=true)` → drive PlanReconciler → assert `Status.Phase` clears and planner re-dispatches.

**Suite-level helpers to copy** (lines 52–81): `makeFakeJobTerminalGates`, `driveMSReconcile`, `drivePlanReconcile` — new specs follow the same drive-and-assert loop pattern.

---

### `cmd/tide/approve_test.go` (test, —)

**Analog:** `cmd/tide/resume_test.go`

**New test: TestApproveRunFailedLevelError — D-07.**

Copy the fake-client + `makeProject`/`makeMilestoneAwaiting` fixture pattern (approve_test.go lines 27–58). New fixture `makeFailedMilestone` sets `Status.Phase="Failed"`. New test:
```go
// Pattern: copy TestApproveLevelDiscoversAwaitingMilestone structure
// Source: cmd/tide/approve_test.go:60-77
func TestApproveRunFailedLevelError(t *testing.T) {
    p := makeProject("my-project")
    ms := makeFailedMilestone("ms-alpha", "my-project")
    c := fake.NewClientBuilder().WithScheme(testScheme(t)).
        WithStatusSubresource(&tidev1alpha1.Milestone{}).
        WithObjects(p, ms).Build()

    err := approveRun(context.Background(), c, "default", "my-project", "", nil)
    if err == nil {
        t.Fatal("expected error for Failed level; got nil")
    }
    if !strings.Contains(err.Error(), "retry-failed") {
        t.Errorf("expected error to mention --retry-failed; got: %v", err)
    }
}
```

---

### `cmd/tide/resume_test.go` (test, —)

**Analog:** `cmd/tide/resume_test.go` (existing, add new test)

**New test: TestResumeRunRetryFailed — D-06.**

Copy `makeRejectedProject` fixture (lines 23–33) and `TestResumeClearsRejectAnnotation` structure (lines 35–49). New test:
```go
// Pattern: copy TestResumeClearsRejectAnnotation structure
// Source: cmd/tide/resume_test.go:35-49
func TestResumeRunRetryFailed(t *testing.T) {
    p := makeProject("my-project")
    // Plan with Status.Phase=Failed
    plan := &tidev1alpha1.Plan{
        ObjectMeta: metav1.ObjectMeta{
            Name: "plan-alpha", Namespace: "default",
            Labels: map[string]string{"tideproject.k8s/project": "my-project"},
        },
        Spec:   tidev1alpha1.PlanSpec{PhaseRef: "some-phase"},
        Status: tidev1alpha1.PlanStatus{Phase: "Failed"},
    }
    c := fake.NewClientBuilder().WithScheme(testScheme(t)).
        WithStatusSubresource(&tidev1alpha1.Plan{}).
        WithObjects(p, plan).Build()

    if err := resumeRun(context.Background(), c, "default", "my-project", true /*retryFailed*/); err != nil {
        t.Fatalf("resumeRun --retry-failed: %v", err)
    }
    var got tidev1alpha1.Plan
    _ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "plan-alpha"}, &got)
    if got.Status.Phase == "Failed" {
        t.Errorf("expected Status.Phase cleared; still Failed (status=%v)", got.Status)
    }
}
```

---

## Shared Patterns

### Status subresource patch (apply to all controller and CLI status writes)

**Source:** `internal/controller/plan_controller.go` lines 562–578 (patchPlanFailed) and lines 582–600 (patchPlanAwaitingApproval)

Every status mutation uses `client.MergeFrom(obj.DeepCopy())` then `r.Status().Patch(ctx, obj, patch)`. Annotation mutations use a separate `client.MergeFrom` + `r.Patch` (NOT `r.Status().Patch`). These are always separate calls — never combined.

```go
// Annotation patch (metadata)
annoPatch := client.MergeFrom(obj.DeepCopy())
obj.SetAnnotations(newAnno)
if err := r.Patch(ctx, obj, annoPatch); err != nil { return ctrl.Result{}, err }

// Status patch (status subresource)
statusPatch := client.MergeFrom(obj.DeepCopy())
obj.Status.Phase = "..."
meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{...})
if err := r.Status().Patch(ctx, obj, statusPatch); err != nil { return ctrl.Result{}, err }
```

### meta.SetStatusCondition (apply to all condition writes)

**Source:** `api/v1alpha1/shared_types.go` lines 128–155; used throughout all four controllers

Always call `meta.SetStatusCondition` (not direct slice append). Handles `LastTransitionTime` idempotency automatically. Import is `"k8s.io/apimachinery/pkg/api/meta"`.

```go
meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
    Status:             metav1.ConditionFalse,   // ConditionFalse = pause lifted
    Reason:             tideprojectv1alpha1.ReasonApprovedByUser,
    Message:            "...",
    LastTransitionTime: metav1.Now(),
})
```

### gates.ConsumeApprove one-shot pattern (apply to all annotation consume calls)

**Source:** `internal/gates/annotation.go` lines 101–116; `internal/controller/milestone_controller.go` lines 473–479

The consumer gets a NEW map (purity contract T-04-G2). Caller patches once. Never mutate `obj.GetAnnotations()` in place.

```go
newAnno := gates.ConsumeApprove(obj, level)
patch := client.MergeFrom(obj.DeepCopy())
obj.SetAnnotations(newAnno)
if err := r.Patch(ctx, obj, patch); err != nil {
    return ctrl.Result{}, err
}
```

### client.IgnoreNotFound for parent lookups (apply to checkParentApproval and resolveProject calls)

**Source:** `internal/controller/milestone_controller.go` lines 154–157; `internal/controller/phase_controller.go` lines 143–148

Parent not found = transient (informer cache lag), not an error. Return `(false, nil)` to let dispatch continue; the next reconcile will catch it.

```go
if err := c.Get(ctx, key, &obj); err != nil {
    return false, client.IgnoreNotFound(err)  // nil for NotFound, err for others
}
```

### Fake-client test pattern with WithStatusSubresource (apply to all CLI tests that patch status)

**Source:** `cmd/tide/approve_test.go` lines 62–66; `cmd/tide/resume_test.go` lines 36–40

```go
c := fake.NewClientBuilder().
    WithScheme(testScheme(t)).
    WithStatusSubresource(&tidev1alpha1.Plan{}).
    WithObjects(p, plan).
    Build()
```

`WithStatusSubresource` is required for fake client status patches to be observable; without it `c.Status().Patch` is a no-op.

### Ginkgo envtest Eventually pattern (apply to all controller test assertions)

**Source:** `internal/controller/milestone_gates_test.go` (driveToJobCompletion helper, lines 79–94)

```go
Eventually(func() string {
    _ = mgrClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)
    return got.Status.Phase
}, 5*time.Second, 50*time.Millisecond).Should(Equal("ExpectedPhase"))
```

---

## No Analog Found

All files have strong analogs in the codebase. No file requires falling back to RESEARCH.md patterns — every pattern was found in live source.

---

## Metadata

**Analog search scope:** `internal/controller/`, `internal/gates/`, `api/v1alpha1/`, `cmd/tide/`, `test/integration/envtest/`, `docs/`
**Files scanned:** 14 source files read directly
**Pattern extraction date:** 2026-06-11
