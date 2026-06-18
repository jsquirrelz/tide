# Phase 27: Budget-Bypass Resume Correctness - Pattern Map

**Mapped:** 2026-06-18
**Files analyzed:** 6 (2 new fields in 1 type file, 3 controller/budget modifications, 2 new/extended test files)
**Analogs found:** 6 / 6

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `api/v1alpha2/project_types.go` | model | CRUD | same file — `BoundaryPushStatus`, `LeaseFailureCount`, `WindowStart` fields | exact |
| `internal/controller/project_controller.go` | controller | request-response | same file — `handleBudgetGate`, `reconcilePhase3Lifecycle`, `handleProjectJobCompletion` | exact |
| `internal/budget/cap.go` | utility | request-response | same file — `IsCapExceeded`, `IsBypassed`; observability change only (no new functions) | exact |
| `internal/controller/project_controller_test.go` | test | request-response | same file — `TestProjectReconciler_BypassAnnotation_ClearsBudgetExceeded` at :505 | exact |
| `internal/controller/project_planner_completion_test.go` | test | request-response | same file — QQH-01 primary `Describe` block | exact |
| `internal/controller/project_clone_idempotency_test.go` (new) | test | request-response | `internal/controller/project_phase3_test.go` — clone Job dispatch tests + `ensurePVC` | role-match |

---

## Pattern Assignments

### `api/v1alpha2/project_types.go` — two new `+optional omitempty` status fields

**Analog:** same file, `GitStatus` (lines 234-250), `BudgetStatus` (lines 257-269), `BoundaryPushStatus` (lines 279-296)

**Existing `+optional omitempty` bool field pattern** (`GitStatus`, line 247 area — `LeaseFailureCount` shows the int32 shape; `BranchName` shows the string shape):
```go
// BranchName is the lifetime branch fixed at Project creation.
// Format: "tide/run-<project>-<unix-epoch>".
// +optional
BranchName string `json:"branchName,omitempty"`
```

**Existing `+optional omitempty` int32 field on same struct:**
```go
// LeaseFailureCount tallies consecutive push lease rejections; reset to 0
// on successful push. Reconciler halts (Phase=PushLeaseFailed) when this
// count exceeds the configured retry budget.
// +optional
LeaseFailureCount int32 `json:"leaseFailureCount,omitempty"`
```

**Copy this shape for `CloneComplete bool` (BYPASS-02) — add to `GitStatus` after `LeaseFailureCount`:**
```go
// CloneComplete is true when the clone Job completed successfully.
// This durable flag gates clone Job re-dispatch on resume, replacing the
// TTL-unreliable Job-existence check (BYPASS-02 / Phase 27).
// +optional
CloneComplete bool `json:"cloneComplete,omitempty"`
```

**Existing `+optional omitempty` string field on `BudgetStatus`** (follow `WindowStart` for placement):
```go
// WindowStart marks the beginning of the current rolling budget window.
// +optional
WindowStart *metav1.Time `json:"windowStart,omitempty"`
```

**Copy this shape for `PlannerRolledUpUID string` (BYPASS-03) — add to `BudgetStatus` after `WindowStart`:**
```go
// PlannerRolledUpUID is the name of the most recent planner Job whose Usage
// was successfully rolled up into CostSpentCents. Prevents double-counting
// when the reporter Job has TTL-GC'd during a halt→resume cycle (BYPASS-03 / Phase 27).
// +optional
PlannerRolledUpUID string `json:"plannerRolledUpUID,omitempty"`
```

**After editing `project_types.go`, always run both (in order):**
```bash
make manifests   # regenerates config/crd/bases/tideproject.k8s_projects.yaml
make generate    # regenerates api/v1alpha2/zz_generated.deepcopy.go
```

---

### `internal/controller/project_controller.go` — four targeted changes

#### Change A — BYPASS-01: `handleBudgetGate` bypass sets `PhaseRunning` (line 1257)

**Analog:** same function, lines 1240-1272 (the bypass branch).

**Existing bypass-clear status patch pattern** (lines 1255-1267 — copy the patch idiom, change only the target phase):
```go
// Clear the phase.
statusPatch := client.MergeFrom(project.DeepCopy())
project.Status.Phase = tidev1alpha2.PhasePending          // <-- BUG: this line
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:               tidev1alpha2.ConditionBudgetExceeded,
    Status:             metav1.ConditionFalse,
    Reason:             tidev1alpha2.ReasonBypassApplied,
    Message:            "Budget exceeded bypass applied by operator",
    LastTransitionTime: metav1.Now(),
})
if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
    return ctrl.Result{}, err
}
```

**Fix shape (D-01):** replace the single assignment with a `BranchName`-conditional:
```go
if project.Status.Git.BranchName != "" {
    project.Status.Phase = tidev1alpha2.PhaseRunning
} else {
    project.Status.Phase = tidev1alpha2.PhasePending
}
```

**Belt-and-suspenders BranchName guard (D-01, reconcileProjectPhase2 Step 3, ~line 338):**
Analog — `reconcilePhase3Lifecycle` Step 1 already uses the same sentinel at line 502:
```go
// Step 1: Branch-name init (D-B6).
if project.Status.Git.BranchName == "" {
    patch := client.MergeFrom(project.DeepCopy())
    project.Status.Git.BranchName = fmt.Sprintf("tide/run-%s-%d", project.Name, time.Now().Unix())
    if err := r.Status().Patch(ctx, project, patch); err != nil {
        return ctrl.Result{}, fmt.Errorf("patch branch name: %w", err)
    }
    return ctrl.Result{Requeue: true}, nil
}
```
Add the mirror guard at the top of Step 3 (init-Job block, ~line 338):
```go
// Guard: workspace already initialized — skip init-Job dispatch.
if project.Status.Git.BranchName != "" {
    return r.reconcilePhase3Lifecycle(ctx, project)
}
```

#### Change B — BYPASS-02: Clone-Job dispatch guarded by `CloneComplete` (lines 555-571)

**Analog:** same block, lines 547-572. Existing guard pattern (single `IsNotFound` condition):
```go
// Step 3: Clone Job dispatch (D-B4 PVC layout init).
pvcName := r.sharedPVCName()
cloneJobName := fmt.Sprintf("tide-clone-%s", project.UID)
var existingClone batchv1.Job
cloneErr := r.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: project.Namespace}, &existingClone)
if cloneErr != nil && !apierrors.IsNotFound(cloneErr) {
    return ctrl.Result{}, cloneErr
}
if apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
    // ... build and Create clone Job
```

**Fix shape (D-02):** prepend `!project.Status.Git.CloneComplete &&` to the dispatch condition:
```go
if !project.Status.Git.CloneComplete && apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
```

**Setting `CloneComplete=true` after clone success — follow the `BranchName` patch pattern** (line 503-507):
```go
patch := client.MergeFrom(project.DeepCopy())
project.Status.Git.CloneComplete = true
if err := r.Status().Patch(ctx, project, patch); err != nil {
    return ctrl.Result{}, fmt.Errorf("patch CloneComplete: %w", err)
}
```
(Place inside the clone-success detection path; executor must first grep `existingClone.Status` after line 571 to locate the exact success detection site — RESEARCH open question A3.)

#### Change C — BYPASS-03: `PlannerRolledUpUID` idempotency marker in `handleProjectJobCompletion` (lines 1177-1182)

**Analog:** same function, lines 1144-1182. Existing `isFirstCompletion` + rollup pattern:
```go
// Plan 09-08 Defect C: roll up planner-level Usage once per planner Job completion.
if isFirstCompletion && envReadOK {
    if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
        logger.Error(rollErr, "project planner budget rollup failed (non-fatal)", "project", project.Name)
    }
}
```

`jobName` is set at line 955:
```go
jobName := fmt.Sprintf("tide-project-%s-1", project.UID)
```

**Fix shape (D-03):** wrap the rollup in a `PlannerRolledUpUID` check, then patch the marker only on rollup success:
```go
if isFirstCompletion && envReadOK {
    if project.Status.Budget.PlannerRolledUpUID != jobName {
        if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
            logger.Error(rollErr, "project planner budget rollup failed (non-fatal)", "project", project.Name)
        } else {
            // Record the marker only after a successful rollup.
            markerPatch := client.MergeFrom(project.DeepCopy())
            project.Status.Budget.PlannerRolledUpUID = jobName
            if pErr := r.Status().Patch(ctx, project, markerPatch); pErr != nil {
                logger.Error(pErr, "patch PlannerRolledUpUID failed (non-fatal)", "project", project.Name)
            }
        }
    }
}
```

**Status patch pattern** (copy from lines 1255-1267 — `client.MergeFrom(project.DeepCopy())` + `r.Status().Patch`):
```go
statusPatch := client.MergeFrom(project.DeepCopy())
project.Status.Budget.PlannerRolledUpUID = jobName
if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
    return ctrl.Result{}, err
}
```

> **⚠ CORRECTION (overrides this mapper's original framing):** CONTEXT.md **D-04** is the
> authoritative decision and supersedes RESEARCH.md Pattern 4 / assumption A2. BYPASS-04 is a
> **behavior fix** (a bypass acknowledges current spend as a baseline; re-halt fires only on
> NEW post-bypass spend), **not** an observability-only / TTL-bypass-docs change. The
> which-cap-fired condition message below is a *carry-along* improvement, not the core fix.
> The acknowledged-spend comparison lives in `handleBudgetGate`'s re-halt guard (scoped to the
> bypass/resume path) so global `IsCapExceeded` semantics — and the `TaskReconciler` call-site —
> stay unchanged; that call-site audit is part of D-04.

#### Change D — BYPASS-04: Acknowledged-spend baseline + which-cap observability (`handleBudgetGate`, lines 1275-1294)

**Analog:** same block, lines 1275-1295. Existing halt branch (only cites `AbsoluteCapCents`):
```go
if project.Status.Phase != tidev1alpha2.PhaseBudgetExceeded && capExceeded && !bypassed {
    logger.Info("budget cap exceeded; halting dispatch", "project", project.Name)
    statusPatch := client.MergeFrom(project.DeepCopy())
    project.Status.Phase = tidev1alpha2.PhaseBudgetExceeded
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:               tidev1alpha2.ConditionBudgetExceeded,
        Status:             metav1.ConditionTrue,
        Reason:             "AbsoluteCapReached",
        Message:            fmt.Sprintf("Cost spent %d cents exceeds cap %d cents", project.Status.Budget.CostSpentCents, project.Spec.Budget.AbsoluteCapCents),
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
        return ctrl.Result{}, err
    }
    if r.Recorder != nil {
        r.Recorder.Event(project, corev1.EventTypeWarning, "AbsoluteCapReached",
            fmt.Sprintf("Project budget cap reached: %d cents spent of %d cents allowed", project.Status.Budget.CostSpentCents, project.Spec.Budget.AbsoluteCapCents))
    }
    return ctrl.Result{}, nil
}
```

**Fix shape (D-04):** replace the hardcoded `"AbsoluteCapReached"` Reason/Message with a helper that detects which cap fired:
```go
// Determine which cap triggered the halt (both may be exceeded; report the first hit).
reason := "AbsoluteCapReached"
message := fmt.Sprintf("Cost spent %d cents exceeds absolute cap %d cents; rolling window cap %d cents",
    project.Status.Budget.CostSpentCents,
    project.Spec.Budget.AbsoluteCapCents,
    project.Spec.Budget.RollingWindowCapCents)
if project.Spec.Budget.AbsoluteCapCents <= 0 ||
    project.Status.Budget.CostSpentCents <= project.Spec.Budget.AbsoluteCapCents {
    reason = "RollingWindowCapReached"
}
```

**Acknowledged-spend baseline (the core D-04 fix):** when a bypass is applied/consumed, record
the current spend as a durable baseline (`Status.Budget.BypassBaselineCents`, a new `+optional`
`omitempty` field — declared with the same three-line shape as the other new status fields).
The re-halt guard then fires only when a cap is exceeded by **new** spend since the bypass:

```go
// Re-halt only if a cap is still exceeded AND new spend has occurred since the last
// bypass acknowledged the prior spend. A fresh bypass sets BypassBaselineCents ==
// CostSpentCents, so re-halt is suppressed until dispatch spends MORE.
newSpendSinceBypass := project.Status.Budget.CostSpentCents > project.Status.Budget.BypassBaselineCents
if project.Status.Phase != tidev1alpha2.PhaseBudgetExceeded && capExceeded && !bypassed && newSpendSinceBypass {
    // ... set PhaseBudgetExceeded with the which-cap reason/message above
}
```

`IsCapExceeded` (`internal/budget/cap.go`) is intentionally **left unchanged** — the baseline
comparison is added in `handleBudgetGate`, not inside the shared predicate, so the
`TaskReconciler` call-site is unaffected. (D-04's call-site audit must confirm this.) The
discretionary choice of baseline representation (a dedicated field vs reusing an existing spend
snapshot) is the executor's, per CONTEXT.md.

---

### `internal/budget/cap.go` — `IsCapExceeded` unchanged (BYPASS-04)

**Analog:** `IsBypassed` doc block (lines 59-88).

`IsCapExceeded` evaluates both caps unconditionally — that contract must NOT change (RESEARCH
Pitfall 4: changing it would affect `TaskReconciler`). The acknowledged-spend baseline logic for
D-04 lives in `handleBudgetGate` (above), not here. Optionally add a doc comment on `IsCapExceeded`
pointing to `handleBudgetGate` for the bypass/baseline behavior so future readers don't expect the
baseline logic in the predicate.

---

### `internal/controller/project_controller_test.go` — extend existing bypass test (BYPASS-01)

**Analog:** `TestProjectReconciler_BypassAnnotation_ClearsBudgetExceeded` (lines 505-560).

**Existing assertion shape** (lines 554-559 — the gap):
```go
Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
Expect(fetched.Status.Phase).NotTo(Equal("BudgetExceeded"),
    "Phase should be cleared from BudgetExceeded when one-shot bypass annotation is present")
// One-shot bypass annotation should be consumed.
Expect(fetched.Annotations).NotTo(HaveKey("tideproject.k8s/bypass-budget"),
    "one-shot bypass annotation should be consumed after bypass")
```

**Add positive phase assertion immediately after the existing `NotTo(Equal("BudgetExceeded"))` check:**
```go
// BYPASS-01: bypass of an initialized project must target PhaseRunning, not PhasePending.
Expect(fetched.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseRunning),
    "Phase must be Running (not Pending) after bypass clears BudgetExceeded on an initialized project")
```

To make the test exercising BYPASS-01's `BranchName` guard, patch `Status.Git.BranchName` before the reconcile:
```go
// Simulate initialized project: set BranchName so bypass targets PhaseRunning.
sp := client.MergeFrom(fetched.DeepCopy())
fetched.Status.Git.BranchName = "tide/run-test-bypass-oneshot-1000000000"
Expect(k8sClient.Status().Patch(ctx, fetched, sp)).To(Succeed())
```
(Add this after `statusPatch.Status.Budget.CostSpentCents = 200` at ~line 542, before the reconcile calls.)

---

### `internal/controller/project_planner_completion_test.go` — extend with BYPASS-03 + BYPASS-05 TTL-GC spec

**Analog:** same file — primary `Describe` block (lines 97-197) and control `Describe` block (lines 200-263).

**Existing helper calls to copy** — use exactly as-is in the new `Describe` blocks:
```go
proj := qqhCreateProject(ctx, <projName>)
envReader := newMapEnvReader()
r := qqhBuildReconciler(envReader)
```
```go
envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
    TaskUID:    string(proj.UID),
    ExitCode:   0,
    ChildCount: 0,
    Usage: pkgdispatch.Usage{
        InputTokens:        1000,
        OutputTokens:       200,
        EstimatedCostCents: plannerCostCents,
    },
})
```
```go
Expect(makeFakeJobTerminal(ctx, mgrClient, plannerJobName, "default", true)).To(Succeed())
```

**BYPASS-05 TTL-GC companion spec shape** — call `handleProjectJobCompletion` with `nil` (GC'd Job path):
```go
// TTL-GC path: planner Job gone (nil) — handleProjectJobCompletion called directly.
_, err = r.handleProjectJobCompletion(ctx, proj, nil)
Expect(err).NotTo(HaveOccurred())
```

**BYPASS-03 double-count spec shape** — simulate halt+GC+resume by calling `handleProjectJobCompletion` twice with the same `envReadOK` seed; assert `CostSpentCents` equals `plannerCostCents` (not `2 * plannerCostCents`):
```go
// Second call must not double-count (PlannerRolledUpUID guard).
_, err = r.handleProjectJobCompletion(ctx, proj, nil)
Expect(err).NotTo(HaveOccurred())

Eventually(func(g Gomega) {
    var refreshed tideprojectv1alpha2.Project
    g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: <projName>, Namespace: "default"}, &refreshed)).To(Succeed())
    g.Expect(refreshed.Status.Budget.CostSpentCents).To(
        BeNumerically("==", plannerCostCents),
        "CostSpentCents must NOT double-count after second completion call")
    g.Expect(refreshed.Status.Budget.PlannerRolledUpUID).To(
        Equal(fmt.Sprintf("tide-project-%s-1", proj.UID)),
        "PlannerRolledUpUID must be set to the planner Job name")
}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
```

**AfterEach cleanup shape** — copy from lines 107-119 (delete Jobs by prefix):
```go
AfterEach(func() {
    qqhCleanupProject(ctx, <projName>)
    var jobs batchv1.JobList
    _ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
    for i := range jobs.Items {
        j := &jobs.Items[i]
        if len(j.Name) > 13 && (j.Name[:13] == "tide-project-" || j.Name[:13] == "tide-reporter") {
            _ = k8sClient.Delete(ctx, j, client.PropagationPolicy(metav1.DeletePropagationBackground))
        }
    }
})
```

---

### `internal/controller/project_clone_idempotency_test.go` (new — BYPASS-02)

**Analog:** `internal/controller/project_phase3_test.go` — Test 1 (branch-name init, lines 76-133) and Test 2 (bypass annotation, lines 135-189).

**File header pattern** (copy from `project_phase3_test.go`, lines 1-31):
```go
// Copyright header + package controller + same import block
package controller

import (
    "context"
    "fmt"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    batchv1 "k8s.io/api/batch/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"

    tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)
```

**`ensurePVC` call** — already defined in `project_phase3_test.go:34`; use directly:
```go
BeforeEach(func() {
    ensurePVC(ctx, pvcName, "default")
})
```

**Reconciler construction** (copy from `project_phase3_test.go` lines 90-97 — use `qqhBuildReconciler` if `SigningKey` is needed for clone dispatch; otherwise use the plain reconciler):
```go
r := &ProjectReconciler{
    Client:                  k8sClient,
    Scheme:                  k8sClient.Scheme(),
    Dispatcher:              &stubDispatcher{},
    MaxConcurrentReconciles: 1,
    SharedPVCName:           pvcName,
    TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
}
```

**Status patch to set `CloneComplete=true`** — copy from `project_phase3_test.go` lines 151-156 (status patch shape):
```go
var p tideprojectv1alpha2.Project
Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
statusPatch := client.MergeFrom(p.DeepCopy())
p.Status.Git.CloneComplete = true
p.Status.Git.BranchName = "tide/run-test-clone-idempotency-1000000000"
Expect(k8sClient.Status().Patch(ctx, &p, statusPatch)).To(Succeed())
```

**Clone-Job absence assertion** (BYPASS-02 guard: no new clone Job when `CloneComplete=true`):
```go
cloneJobName := fmt.Sprintf("tide-clone-%s", p.UID)
Consistently(func() error {
    return k8sClient.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: "default"}, &batchv1.Job{})
}, 1*time.Second, 100*time.Millisecond).Should(MatchError(ContainSubstring("not found")),
    "clone Job must NOT be re-dispatched when CloneComplete=true")
```

**`stampBudgetSpend` helper** — already defined in `budget_blocked_regression_test.go:51`; use directly for any bypass-halt setup needed in clone idempotency tests.

---

## Shared Patterns

### Status Patch — `client.MergeFrom` + `r.Status().Patch`
**Source:** `project_controller.go:1247-1267` (annotation patch) and `project_controller.go:1255-1267` (status patch)
**Apply to:** All four controller changes (BYPASS-01/02/03/04)
```go
// Annotation patch (metadata):
annotPatch := client.MergeFrom(project.DeepCopy())
project.Annotations = newAnnotations
if err := r.Patch(ctx, project, annotPatch); err != nil {
    return ctrl.Result{}, fmt.Errorf("consume bypass annotation: %w", err)
}

// Status patch:
statusPatch := client.MergeFrom(project.DeepCopy())
project.Status.Phase = tidev1alpha2.PhaseRunning
if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
    return ctrl.Result{}, err
}
```

### Non-fatal error logging pattern
**Source:** `project_controller.go:1179-1181`
**Apply to:** BYPASS-03 rollup + patch (both non-fatal)
```go
if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
    logger.Error(rollErr, "project planner budget rollup failed (non-fatal)", "project", project.Name)
}
```

### `meta.SetStatusCondition` with structured Reason
**Source:** `project_controller.go:1258-1264` (bypass branch) and lines 1280-1286 (halt branch)
**Apply to:** BYPASS-04 halt observability update
```go
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:               tidev1alpha2.ConditionBudgetExceeded,
    Status:             metav1.ConditionTrue,
    Reason:             "AbsoluteCapReached",   // <-- replace with dynamic reason
    Message:            fmt.Sprintf("..."),
    LastTransitionTime: metav1.Now(),
})
```

### Ginkgo `Label("envtest")` + `Eventually` assertion
**Source:** `project_planner_completion_test.go:97` (`Label`), lines 183-195 (`Eventually`)
**Apply to:** All new Ginkgo `Describe` blocks in BYPASS-01/02/03/05
```go
var _ = Describe("...", Label("envtest"), func() {
    ...
    Eventually(func(g Gomega) {
        var refreshed tideprojectv1alpha2.Project
        g.Expect(mgrClient.Get(ctx, ..., &refreshed)).To(Succeed())
        g.Expect(refreshed.Status.Budget.CostSpentCents).To(BeNumerically(">=", plannerCostCents))
    }, 5*time.Second, 100*time.Millisecond).Should(Succeed())
})
```

---

## No Analog Found

None. All files have direct analogs in the existing codebase.

---

## Metadata

**Analog search scope:** `api/v1alpha2/`, `internal/budget/`, `internal/controller/`
**Files scanned:** 8 source files + 5 test files
**Pattern extraction date:** 2026-06-18

### Key Patterns Identified

1. All status mutations use `client.MergeFrom(project.DeepCopy())` + `r.Status().Patch` — never `Update`. The `RollUpUsage` helper uses `client.MergeFromWithOptions(..., client.MergeFromWithOptimisticLock{})` with internal retry for concurrent-safe tally increments.
2. All new CRD status fields follow `// comment\n// +optional\nField Type \`json:"fieldName,omitempty"\`` — three-line shape matching `BranchName`, `LeaseFailureCount`, `WindowStart`.
3. New Ginkgo specs use `Label("envtest")` + distinct per-spec project names (never reuse `projectName` constants across `Describe` blocks) to avoid cross-spec state leakage.
4. Non-fatal errors (budget rollup failures, patch failures on tally markers) are `logger.Error(err, "... (non-fatal)", ...)` — never returned as reconcile errors.
5. `makeFakeJobTerminal` + `waitForCacheSync` / `Eventually` cache-wait are the two mandatory idioms before asserting on Job-driven state changes in envtest.
