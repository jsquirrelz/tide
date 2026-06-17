# Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption — Pattern Map

**Mapped:** 2026-06-16
**Files analyzed:** 7 new/modified files
**Analogs found:** 7 / 7

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/controller/failure_halt.go` | controller helper | request-response | `internal/controller/billing_halt.go` | exact |
| `internal/controller/failure_halt_test.go` | test | request-response | `internal/controller/billing_halt_test.go` | exact |
| `internal/controller/task_controller.go` (modify) | controller | CRUD + event-driven | itself (lines 1182–1414, 1504–1554) | self-analog |
| `api/v1alpha2/project_types.go` (modify) | api type | — | `api/v1alpha2/shared_types.go` Gates/GatePolicy enum block (lines 47–64) | exact |
| `api/v1alpha2/shared_types.go` (modify) | api type | — | Phase 13 BillingHalt block (lines 199–217) | exact |
| `cmd/tide/resume.go` (modify) | cmd/CLI | request-response | itself lines 85–131 (BillingHalt clear block) | self-analog |
| `test/integration/envtest/global_dispatch_test.go` | test | event-driven | `test/integration/envtest/indegree_test.go` | exact |

---

## Pattern Assignments

### `internal/controller/failure_halt.go` (controller helper, request-response)

**Analog:** `internal/controller/billing_halt.go`

The new file is a near-exact mirror. Copy the file structure verbatim, replacing billing-specific identifiers. Key differences: (a) no `isBillingFailureReason` classifier — FailureHalt fires on ANY task failure under conservative profile; (b) no time-fence / `AnnotationFailureResumedAt` check inside `setFailureHaltIfNeeded` — `--retry-failed` resets Task phases so re-halt is intentional; (c) signature of `setFailureHaltIfNeeded` drops the `reason string` and `jobStart time.Time` parameters (simpler); (d) `setFailureHaltIfNeeded` must check `FailureProfile == conservative` before stamping.

**Package declaration + imports pattern** (`billing_halt.go` lines 1–52):
```go
// failure_halt.go — FailureHalt condition helpers for DISP-02 (Phase 25).
//
// D-02b: When TaskReconciler observes a task execution failure under a
// conservative FailureProfile, it calls setFailureHaltIfNeeded to stamp
// FailureHalt=True on the Project. All five dispatch gates call
// checkFailureHalt before dispatching; if halted they park with a 30s requeue.
//
// Conservative halt is cleared by `tide resume --retry-failed` (same verb that
// resets Failed Task phases). Unlike BillingHalt, no time-fence is needed: the
// --retry-failed resets also wipe the Failed Tasks, so re-halt after resume is
// from a genuine fresh failure — not a pre-resume straggler.
package controller

import (
    "context"

    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)
```

**`checkFailureHalt` function pattern** (`billing_halt.go` lines 72–89):
```go
// checkFailureHalt returns true if the Project has a FailureHalt=True condition,
// indicating that a task failed under conservative FailureProfile and all new
// dispatch should be parked until the operator runs `tide resume --retry-failed`.
//
// Nil-safe: a nil project returns false.
func checkFailureHalt(project *tideprojectv1alpha2.Project) bool {
    if project == nil {
        return false
    }
    for _, c := range project.Status.Conditions {
        if c.Type == tideprojectv1alpha2.ConditionFailureHalt &&
            c.Status == metav1.ConditionTrue {
            return true
        }
    }
    return false
}
```

**`setFailureHaltIfNeeded` function pattern** (`billing_halt.go` lines 91–135, simplified — drop time-fence):
```go
// setFailureHaltIfNeeded stamps FailureHalt=True on project when the Project's
// FailureProfile is conservative and FailureHalt is not already set. Idempotent:
// a second call when halt is already True is a no-op (avoids patch churn on
// concurrent wave failures).
//
// Called from TaskReconciler.handleJobCompletion on task execution failure only
// (not on planning Job failures — FailureHalt is an execution-layer signal).
// Nil project is a safe no-op.
func setFailureHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha2.Project) error {
    if project == nil {
        return nil
    }
    if project.Spec.FailureProfile != tideprojectv1alpha2.FailureProfileConservative {
        return nil // strict profile: no-op
    }
    // Already halted: idempotent no-op.
    for _, cond := range project.Status.Conditions {
        if cond.Type == tideprojectv1alpha2.ConditionFailureHalt &&
            cond.Status == metav1.ConditionTrue {
            return nil
        }
    }
    patch := client.MergeFrom(project.DeepCopy())
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:    tideprojectv1alpha2.ConditionFailureHalt,
        Status:  metav1.ConditionTrue,
        Reason:  tideprojectv1alpha2.ReasonTaskFailedHalt,
        Message: "A task failed under conservative FailureProfile. New dispatch halted project-wide. " +
            "Run `tide resume --retry-failed` after addressing the failure.",
        LastTransitionTime: metav1.Now(),
    })
    return c.Status().Patch(ctx, project, patch)
}
```

---

### `internal/controller/failure_halt_test.go` (test, request-response)

**Analog:** `internal/controller/billing_halt_test.go` (all 401 lines)

Copy the test file structure directly. Replace billing-specific function names and constants. Key differences: no time-fence tests (`PreResumeStraggler`, `ZeroJobStart`, `UnparseableAnnotation`); add tests for strict-profile no-op and conservative-profile stamp.

**Package + imports pattern** (`billing_halt_test.go` lines 17–32):
```go
// Plan 25-XX Task X — RED tests for FailureHalt condition vocabulary + shared helpers.
// Tests: checkFailureHalt, setFailureHaltIfNeeded.
package controller

import (
    "context"
    "testing"

    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"

    tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)
```

**Fixture construction pattern** (`billing_halt_test.go` lines 124–141):
```go
func TestSetFailureHaltIfNeeded_ConservativeStampsHalt(t *testing.T) {
    s := fakeSchemeWithAll(t)
    project := &tideprojectv1alpha2.Project{
        ObjectMeta: metav1.ObjectMeta{Name: "my-project", Namespace: "default"},
        Spec: tideprojectv1alpha2.ProjectSpec{
            SchemaRevision:  "v1alpha2",
            TargetRepo:      "https://example.com/repo.git",
            FailureProfile:  tideprojectv1alpha2.FailureProfileConservative,
        },
    }
    c := fake.NewClientBuilder().WithScheme(s).
        WithObjects(project).
        WithStatusSubresource(project).
        Build()

    if err := setFailureHaltIfNeeded(context.Background(), c, project); err != nil {
        t.Fatalf("setFailureHaltIfNeeded: %v", err)
    }

    var got tideprojectv1alpha2.Project
    if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
        t.Fatalf("get project: %v", err)
    }
    cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionFailureHalt)
    if cond == nil || cond.Status != metav1.ConditionTrue {
        t.Errorf("expected FailureHalt=True; got %v", cond)
    }
}
```

**Additional test cases required** (beyond billing analogs):
- `TestCheckFailureHalt_TrueWhenConditionPresent` — mirror `TestCheckBillingHalt_TrueWhenConditionPresent`
- `TestCheckFailureHalt_FalseWhenConditionAbsent` — mirror billing equivalent
- `TestCheckFailureHalt_FalseForNilProject` — nil-safety
- `TestSetFailureHaltIfNeeded_StrictProfile_NoOp` — strict profile must not stamp
- `TestSetFailureHaltIfNeeded_ConservativeStampsHalt` — as above
- `TestSetFailureHaltIfNeeded_IdempotentSecondCall` — second call when already True is no-op
- `TestSetFailureHaltIfNeeded_NilProject_NoOp` — nil safety

---

### `internal/controller/task_controller.go` (modify — 4 surgical changes)

**Analog:** itself — changes are in-place modifications to existing functions.

#### Change 1: `listSiblingTasks` → `listProjectTasks` (lines 1182–1192)

**Current code** (`task_controller.go` lines 1182–1192):
```go
// listSiblingTasks returns all Tasks in the same Plan as task (same namespace, same PlanRef).
func (r *TaskReconciler) listSiblingTasks(ctx context.Context, task *tideprojectv1alpha2.Task) ([]tideprojectv1alpha2.Task, error) {
    var taskList tideprojectv1alpha2.TaskList
    if err := r.List(ctx, &taskList,
        client.InNamespace(task.Namespace),
        client.MatchingFields{taskPlanRefIndexKey: task.Spec.PlanRef},
    ); err != nil {
        return nil, fmt.Errorf("list sibling tasks: %w", err)
    }
    return taskList.Items, nil
}
```

**Replace with** (mirrors `assembleProjectDepGraph` label query at `project_controller.go` line 1476–1481):
```go
// listProjectTasks returns all Tasks in the same Project as task, identified
// by the owner.LabelProject label. This is the global sibling set consumed by
// computeIndegree to resolve DependsOn across plan/phase/milestone boundaries
// (DISP-01 D-01). projectName must be non-empty (assert before calling).
func (r *TaskReconciler) listProjectTasks(ctx context.Context, task *tideprojectv1alpha2.Task, projectName string) ([]tideprojectv1alpha2.Task, error) {
    var taskList tideprojectv1alpha2.TaskList
    if err := r.List(ctx, &taskList,
        client.InNamespace(task.Namespace),
        client.MatchingLabels{owner.LabelProject: projectName},
    ); err != nil {
        return nil, fmt.Errorf("list project tasks: %w", err)
    }
    return taskList.Items, nil
}
```

The old `listSiblingTasks` stays as a private helper **only if** it is still needed by `siblingsToTaskMapper` — check for callers before removing. After the sibling mapper is replaced (Change 3 below), `listSiblingTasks` can be deleted.

#### Change 2: `checkReadinessGates` call site (lines 423–430)

**Current code** (`task_controller.go` lines 423–430):
```go
func (r *TaskReconciler) checkReadinessGates(ctx context.Context, task *tideprojectv1alpha2.Task, project *tideprojectv1alpha2.Project) (taskGateResult, error) {
    // Indegree compute (D-B3). Re-computed every reconcile; never cached.
    siblings, err := r.listSiblingTasks(ctx, task)
    if err != nil {
        return taskGateResult{}, err
    }
    indegree := r.computeIndegree(task, siblings)
```

**Replace `listSiblingTasks` call**:
```go
    // DISP-01: list ALL project tasks (global scope) so computeIndegree resolves
    // DependsOn edges across plan/phase/milestone boundaries. project.Name is
    // guaranteed non-empty (resolveProject returned it without error above).
    siblings, err := r.listProjectTasks(ctx, task, project.Name)
    if err != nil {
        return taskGateResult{}, err
    }
    indegree := r.computeIndegree(task, siblings)
```

`computeIndegree` itself (`task_controller.go` lines 1198–1213) requires NO changes — the algorithm is correct for global scope. The `statusByName[dep] != "Succeeded"` check already: blocks dependents of Failed tasks (DISP-02 strict), blocks dependents of AwaitingApproval tasks (D-03b), and never dispatches a task whose predecessor is missing from the map (conservatively treats unknown deps as unsatisfied). The only prerequisite is that `siblings` now contains all project tasks.

#### Change 3: `siblingsToTaskMapper` → `globalDependentsMapper` (lines 1386–1414)

**Current code** (`task_controller.go` lines 1386–1414):
```go
// siblingsToTaskMapper returns reconcile requests for all sibling Tasks sharing
// the same PlanRef as the changed Task.
func (r *TaskReconciler) siblingsToTaskMapper(ctx context.Context, obj client.Object) []reconcile.Request {
    task, ok := obj.(*tideprojectv1alpha2.Task)
    if !ok {
        return nil
    }
    if task.Spec.PlanRef == "" {
        return nil
    }
    var siblingList tideprojectv1alpha2.TaskList
    if err := r.List(ctx, &siblingList,
        client.InNamespace(task.Namespace),
        client.MatchingFields{taskPlanRefIndexKey: task.Spec.PlanRef},
    ); err != nil {
        return nil
    }
    reqs := make([]reconcile.Request, 0, len(siblingList.Items))
    for _, s := range siblingList.Items {
        if s.UID == task.UID {
            continue
        }
        reqs = append(reqs, reconcile.Request{
            NamespacedName: client.ObjectKey{Namespace: s.Namespace, Name: s.Name},
        })
    }
    return reqs
}
```

**Replace with** (Option A from RESEARCH.md — O(V), project-label list + filter for forward edges):
```go
// globalDependentsMapper re-enqueues all Tasks in the same project whose
// DependsOn contains the name of the changed Task. Drives DISP-01: when a
// global predecessor completes, fails, or becomes AwaitingApproval, its
// dependents re-evaluate readiness. Uses owner.LabelProject label (same as
// assembleProjectDepGraph) so cross-plan/phase/milestone dependents are covered.
func (r *TaskReconciler) globalDependentsMapper(ctx context.Context, obj client.Object) []reconcile.Request {
    task, ok := obj.(*tideprojectv1alpha2.Task)
    if !ok {
        return nil
    }
    projectName := task.Labels[owner.LabelProject]
    if projectName == "" {
        return nil
    }
    var all tideprojectv1alpha2.TaskList
    if err := r.List(ctx, &all,
        client.InNamespace(task.Namespace),
        client.MatchingLabels{owner.LabelProject: projectName},
    ); err != nil {
        return nil
    }
    reqs := make([]reconcile.Request, 0)
    for _, t := range all.Items {
        if t.UID == task.UID { // skip self-enqueue (Pitfall 1 from RESEARCH.md)
            continue
        }
        for _, dep := range t.Spec.DependsOn {
            if dep == task.Name {
                reqs = append(reqs, reconcile.Request{
                    NamespacedName: client.ObjectKey{Namespace: t.Namespace, Name: t.Name},
                })
                break
            }
        }
    }
    return reqs
}
```

#### Change 4: `gateChecks` — add `checkFailureHalt` gate (lines 367–371)

**Insertion point** — after the existing `checkBillingHalt` block (lines 367–371), before `setBudgetBlockedIfNeeded` (line 379):

**Current** (`task_controller.go` lines 367–378):
```go
    if checkBillingHalt(project) {
        logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
            "task", task.Name, "project", project.Name)
        return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
    }

    // Phase 14 BUDGET-02 / D-04: fourth dispatch-entry hold ...
    if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, ...
```

**Insert after billing-halt block**:
```go
    // Phase 25 DISP-02 / D-02b: fifth dispatch-entry hold — conservative failure halt.
    // Fires only when Project.Spec.FailureProfile==conservative AND a task execution
    // failure has stamped ConditionFailureHalt=True. Park (never fail); cleared by
    // `tide resume --retry-failed`. No per-Task condition stamp (same as BillingHalt —
    // operator signal is the single Project FailureHalt condition).
    if checkFailureHalt(project) {
        logf.FromContext(ctx).V(1).Info("dispatch held: project failure halt (conservative profile)",
            "task", task.Name, "project", project.Name)
        return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
    }
```

#### Change 5: `SetupWithManager` — update watch to `globalDependentsMapper` (lines 1539–1542)

**Current** (`task_controller.go` lines 1539–1542):
```go
        Watches(
            &tideprojectv1alpha2.Task{},
            handler.EnqueueRequestsFromMapFunc(r.siblingsToTaskMapper),
        ).
```

**Replace**:
```go
        Watches(
            &tideprojectv1alpha2.Task{},
            handler.EnqueueRequestsFromMapFunc(r.globalDependentsMapper),
        ).
```

No new field indexer is required. The `taskPlanRefIndexKey` field indexer (line 1508–1518) stays registered because `checkParentApproval` (line 354) uses it.

#### Change 6: `handleJobCompletion` — call `setFailureHaltIfNeeded` on task execution failure

Find the existing `setBillingHaltIfNeeded` call in `handleJobCompletion` (near line 907–915 per RESEARCH.md) and add `setFailureHaltIfNeeded` immediately after:

**Pattern** (mirrors the `setBillingHaltIfNeeded` call shape):
```go
    // Phase 25 D-02b: conservative halt on task execution failure.
    if hErr := setFailureHaltIfNeeded(ctx, r.Client, project); hErr != nil {
        logf.FromContext(ctx).Error(hErr, "setFailureHaltIfNeeded failed (non-fatal)")
    }
```

---

### `api/v1alpha2/project_types.go` (modify — add `FailureProfile` field)

**Analog:** `api/v1alpha2/shared_types.go` lines 47–64 (GatePolicy + Gates enum pattern)

**Existing enum pattern to copy** (`shared_types.go` lines 47–64):
```go
// GatePolicy is one of "auto" | "approve" | "pause" — per-level human gate.
// Phase 1 ships the field shape; Phase 4 consumes.
// +kubebuilder:validation:Enum=auto;approve;pause
type GatePolicy string

// Gates declares per-level gate policy. Phase 1 ships field; Phase 4 wires.
type Gates struct {
    // +optional
    Milestone GatePolicy `json:"milestone,omitempty"`
    ...
}
```

**New type** — add in `shared_types.go` after the Phase 14 block (after line 235):
```go
// Phase 25 condition + reason vocabulary — task failure halt (DISP-02 conservative).
const (
    // ConditionFailureHalt — a task failed under conservative FailureProfile;
    // new dispatch is halted project-wide until the operator runs
    // `tide resume --retry-failed`. Set by TaskReconciler.handleJobCompletion;
    // read by all five dispatch gates; cleared by tide resume. Phase 25 DISP-02.
    ConditionFailureHalt = "FailureHalt"

    // ReasonTaskFailedHalt — a member task failed and the Project's
    // FailureProfile is conservative; halt is set project-wide.
    ReasonTaskFailedHalt = "TaskFailedHalt"

    // AnnotationFailureResumedAt — RFC3339 timestamp stamped by
    // `tide resume --retry-failed` when clearing the FailureHalt condition.
    // Mirrors AnnotationBillingResumedAt. Optional: only needed if the
    // reconciler gates re-stamping FailureHalt against this timestamp.
    AnnotationFailureResumedAt = "tideproject.k8s/failure-resumed-at"
)

// FailureProfileType is the failure-propagation policy for this Project.
// +kubebuilder:validation:Enum=strict;conservative
type FailureProfileType string

const (
    // FailureProfileStrict (default): non-dependent tasks in later waves
    // continue dispatching when an earlier task fails. The indegree model
    // enforces this automatically — only dependents are blocked.
    FailureProfileStrict FailureProfileType = "strict"

    // FailureProfileConservative: first task execution failure halts all
    // new dispatch project-wide (ConditionFailureHalt) until the operator
    // runs `tide resume --retry-failed`. In-flight Jobs complete naturally.
    FailureProfileConservative FailureProfileType = "conservative"
)
```

**New field in `ProjectSpec`** — add after the `Git *GitConfig` field (line 376 of `project_types.go`):
```go
    // FailureProfile controls how a task execution failure affects non-dependent
    // work in later waves. strict (default): non-dependents continue dispatching
    // (enforced automatically by the indegree model — a failed task never reaches
    // Succeeded, so only its dependents are blocked). conservative: first failure
    // stamps ConditionFailureHalt and halts all new dispatch project-wide until
    // `tide resume --retry-failed` is run.
    // +kubebuilder:validation:Enum=strict;conservative
    // +kubebuilder:default=strict
    // +optional
    FailureProfile FailureProfileType `json:"failureProfile,omitempty"`
```

The `+kubebuilder:default=strict` marker ensures existing Projects without the field default to strict without migration. The `+optional` + `omitempty` ensures strict-profile Projects serialize cleanly (the field is omitted if unset). `verify-no-aggregates` is unaffected — `FailureProfile` is a plain string enum, not `Schedule`/`Waves[]`/`IndegreeMap`/`CachedDag`.

---

### `api/v1alpha2/shared_types.go` (modify — add Phase 25 constants)

**Analog:** Phase 13 BillingHalt block (`shared_types.go` lines 199–217)

**Existing pattern to mirror** (`shared_types.go` lines 199–217):
```go
// Phase 13 condition + reason vocabulary — provider billing halt (HALT-01).
const (
    // ConditionBillingHalt — provider returned a credit-exhaustion 400; ...
    ConditionBillingHalt = "BillingHalt"

    // ReasonCreditBalanceTooLow — ...
    ReasonCreditBalanceTooLow = "CreditBalanceTooLow"

    // AnnotationBillingResumedAt — RFC3339 timestamp stamped by `tide resume`
    // when clearing the BillingHalt condition. ...
    AnnotationBillingResumedAt = "tideproject.k8s/billing-resumed-at"
)
```

**Insert Phase 25 block** immediately after line 235 (after the Phase 14 block), before Phase 23. The `FailureProfileType` type declaration should go here (or in `project_types.go` — either is idiomatic, but `shared_types.go` is preferred since it holds all cross-controller vocabulary). The exact placement: new const block + type declaration added after line 235 of the current file.

---

### `cmd/tide/resume.go` (modify — add FailureHalt clear inside `retryFailed` branch)

**Analog:** BillingHalt clear block (`resume.go` lines 85–131)

**Existing BillingHalt clear pattern** (`resume.go` lines 85–131):
```go
    // Phase 13 D-06: clear BillingHalt unconditionally ...
    if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
        return fmt.Errorf("re-get project for BillingHalt clear: %w", err)
    }
    haltCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha2.ConditionBillingHalt)
    if haltCond != nil {
        hadBillingHalt := haltCond.Status == metav1.ConditionTrue
        patch2 := client.MergeFrom(proj.DeepCopy())
        meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha2.ConditionBillingHalt)
        if err := c.Status().Patch(ctx, &proj, patch2); err != nil {
            return fmt.Errorf("patch status (clear BillingHalt): %w", err)
        }
        if hadBillingHalt {
            // stamp AnnotationBillingResumedAt ...
        }
    }

    if !retryFailed {
        return nil
    }
    return retryFailedLevels(ctx, c, ns, projectName, out)
```

**Insert FailureHalt clear** INSIDE the `retryFailed` gate — meaning AFTER the `if !retryFailed { return nil }` line (line 127) and BEFORE the `retryFailedLevels` call (line 131). BillingHalt is cleared unconditionally on bare resume (intentional — billing recovery); FailureHalt requires `--retry-failed` because the task failures must also be reset.

```go
    if !retryFailed {
        return nil
    }

    // Phase 25 D-04: clear FailureHalt when --retry-failed (conservative halt
    // recovery). Re-fetch to get fresh resourceVersion after the BillingHalt
    // status patch above. FailureHalt is task-execution-failure-specific;
    // it is only meaningful to clear it together with the --retry-failed Task
    // phase resets that follow. (Contrast with BillingHalt, which is cleared
    // on bare resume regardless of --retry-failed.)
    if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
        return fmt.Errorf("re-get project for FailureHalt clear: %w", err)
    }
    fhCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha2.ConditionFailureHalt)
    if fhCond != nil && fhCond.Status == metav1.ConditionTrue {
        patch3 := client.MergeFrom(proj.DeepCopy())
        meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha2.ConditionFailureHalt)
        if err := c.Status().Patch(ctx, &proj, patch3); err != nil {
            return fmt.Errorf("patch status (clear FailureHalt): %w", err)
        }
        if out != nil {
            fmt.Fprintln(out, "tide: cleared FailureHalt; re-dispatch will resume after retry-failed reset")
        }
    }

    return retryFailedLevels(ctx, c, ns, projectName, out)
```

Key guards: the `c.Get` re-fetch gets a fresh `resourceVersion` after the BillingHalt status patch (same pattern as the `AnnotationBillingResumedAt` re-fetch at lines 107–119 of the current file). No `AnnotationFailureResumedAt` stamp needed (no time-fence in `setFailureHaltIfNeeded`).

---

### `test/integration/envtest/global_dispatch_test.go` (new integration test)

**Analog:** `test/integration/envtest/indegree_test.go` (all 347 lines)

**Package + imports pattern** (`indegree_test.go` lines 17–32):
```go
package envtest_integration

import (
    "context"
    "fmt"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)
```

**Project + multi-Plan fixture construction pattern** (`indegree_test.go` lines 43–91 + `makeTask` at lines 306–346):

The new test file creates Tasks across TWO Plans to test cross-plan DependsOn. Follow `makeTask`/`makeTaskWithWaveLabel` exactly but stamp `owner.LabelProject` (which `makeTask` already does at line 320). The critical extension: create two distinct Plan objects and Tasks in each.

```go
// globalDispatchTestProject is the Project used by global dispatch tests.
// Two Plans (plan-alpha, plan-beta) with Tasks across them model the
// cross-plan DependsOn scenario DISP-01 requires.
const globalDispatchTestProject = "global-dispatch-test-project"
const globalDispatchNS = "default"

var _ = Describe("Phase 25 global dispatch, failure semantics, gates, resumption", Label("envtest", "phase25"), func() {
    ctx := context.Background()

    BeforeEach(func() {
        makeBoundPVC(ctx, "tide-projects", globalDispatchNS)
        project := &tideprojectv1alpha2.Project{
            ObjectMeta: metav1.ObjectMeta{
                Name:      globalDispatchTestProject,
                Namespace: globalDispatchNS,
            },
            Spec: tideprojectv1alpha2.ProjectSpec{
                SchemaRevision: "v1alpha2",
                TargetRepo:     "https://github.com/example/global-dispatch-test.git",
            },
        }
        if err := k8sClient.Create(ctx, project); err != nil {
            Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
        }
    })

    AfterEach(func() {
        // Mirror indegree_test.go AfterEach cleanup pattern (lines 65–91):
        // delete Tasks, Plans, Waves, Projects, PVCs in the namespace.
        ...
    })
```

**Cross-plan task fixture helper** (extend `makeTask` from `indegree_test.go` line 306):
```go
// makeGlobalTask creates a Task in the given plan (cross-plan variant of makeTask)
// with the globalDispatchTestProject label pre-stamped.
func makeGlobalTask(ctx context.Context, name, planRef string, dependsOn, files []string) *tideprojectv1alpha2.Task {
    labels := map[string]string{
        "tideproject.k8s/project": globalDispatchTestProject,
    }
    task := &tideprojectv1alpha2.Task{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name,
            Namespace: globalDispatchNS,
            Labels:    labels,
        },
        Spec: tideprojectv1alpha2.TaskSpec{
            PlanRef:             planRef,
            PromptPath:          "envelopes/test/children/" + name + ".json",
            DependsOn:           dependsOn,
            FilesTouched:        files,
            DeclaredOutputPaths: files,
        },
    }
    Expect(k8sClient.Create(ctx, task)).To(Succeed())
    Eventually(func() error {
        return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalDispatchNS}, &tideprojectv1alpha2.Task{})
    }, "5s", "100ms").Should(Succeed())
    time.Sleep(50 * time.Millisecond) // allow indexer to propagate
    return task
}
```

**DISP-01 cross-plan test pattern** (extend `indegree_test.go` lines 94–123):
```go
Describe("DISP-01: cross-plan DependsOn blocks dispatch until global predecessor succeeds", Label("DISP-01"), func() {
    It("task in plan-beta with DependsOn=[task-in-plan-alpha] stays Pending until alpha succeeds", func() {
        // Two Plans, Tasks across them.
        createSimplePlanInNS(ctx, "alpha-plan", globalDispatchNS)
        createSimplePlanInNS(ctx, "beta-plan", globalDispatchNS)

        taskA := makeGlobalTask(ctx, "cross-plan-task-a", "alpha-plan", nil, []string{"a.go"})
        taskB := makeGlobalTask(ctx, "cross-plan-task-b", "beta-plan", []string{taskA.Name}, []string{"b.go"})
        _ = taskB

        // taskA has no deps — reconciler will try to dispatch (Running or Pending/Succeeded via stub).
        Eventually(func() string {
            t := &tideprojectv1alpha2.Task{}
            if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskA.Name, Namespace: globalDispatchNS}, t); err != nil {
                return ""
            }
            return t.Status.Phase
        }, "45s", "200ms").Should(Or(Equal("Running"), Equal("Succeeded"), Equal("Pending")))

        // taskB must stay non-Running while taskA has not completed.
        Consistently(func() string {
            t := &tideprojectv1alpha2.Task{}
            if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskB.Name, Namespace: globalDispatchNS}, t); err != nil {
                return "error"
            }
            return t.Status.Phase
        }, "3s", "200ms").ShouldNot(Equal("Running"),
            "cross-plan taskB should not dispatch before cross-plan taskA completes")
    })
})
```

**DISP-02 conservative halt test pattern** (uses status patch to simulate task failure, then checks Project condition):
```go
Describe("DISP-02 conservative: task failure stamps ConditionFailureHalt on Project", Label("DISP-02"), func() {
    It("ConditionFailureHalt=True set on Project after task execution failure under conservative profile", func() {
        // Set conservative profile on the project.
        proj := &tideprojectv1alpha2.Project{}
        Expect(k8sClient.Get(ctx, client.ObjectKey{Name: globalDispatchTestProject, Namespace: globalDispatchNS}, proj)).To(Succeed())
        patch := client.MergeFrom(proj.DeepCopy())
        proj.Spec.FailureProfile = tideprojectv1alpha2.FailureProfileConservative
        Expect(k8sClient.Patch(ctx, proj, patch)).To(Succeed())

        // ... create tasks, simulate Job failure via suiteEnvReader.SetOut + Job status patch, ...
        // Eventually check Project has ConditionFailureHalt=True
    })
})
```

**RESUME-01 restart regression test pattern** (from RESEARCH.md §Resumption):
```go
Describe("RESUME-01: restart re-derives schedule from Task CRD status", Label("RESUME-01"), func() {
    It("after A and B Succeeded in etcd, task C dispatches without new persistence", func() {
        // A → B → C chain (three Tasks, cross-plan).
        // Status-patch A and B to Succeeded.
        // Assert C reaches Running/dispatched (indegree=0 re-derived on next reconcile).
        // Verify no IndegreeMap/Schedule field added to Project status.
    })
})
```

**`resume_test.go` extension** (add to `cmd/tide/resume_test.go`, package `main`):

**Existing test fixture pattern** (`resume_test.go` lines 29–39, 120–157):
```go
// TestResumeRunClearsFailureHalt asserts that resumeRun with retryFailed=true
// clears a ConditionFailureHalt=True condition on the Project.
func TestResumeRunClearsFailureHalt(t *testing.T) {
    p := makeProject("my-project")
    // stamp FailureHalt on the project
    p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
        Type:               tidev1alpha2.ConditionFailureHalt,
        Status:             metav1.ConditionTrue,
        Reason:             tidev1alpha2.ReasonTaskFailedHalt,
        LastTransitionTime: metav1.Now(),
    })
    c := fake.NewClientBuilder().
        WithScheme(testScheme(t)).
        WithObjects(p).
        WithStatusSubresource(&tidev1alpha2.Project{}, &tidev1alpha2.Milestone{}, &tidev1alpha2.Phase{}, &tidev1alpha2.Plan{}, &tidev1alpha2.Task{}).
        Build()

    var buf bytes.Buffer
    if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
        t.Fatalf("resumeRun(retryFailed=true): %v", err)
    }

    var got tidev1alpha2.Project
    if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
        t.Fatalf("get project: %v", err)
    }
    fhCond := meta.FindStatusCondition(got.Status.Conditions, tidev1alpha2.ConditionFailureHalt)
    if fhCond != nil && fhCond.Status == metav1.ConditionTrue {
        t.Errorf("expected ConditionFailureHalt cleared by retry-failed; still True")
    }
    if !strings.Contains(buf.String(), "FailureHalt") {
        t.Errorf("expected output to mention FailureHalt; got %q", buf.String())
    }
}

// TestResumeWithoutRetryFailedLeavesFailureHalt asserts that bare resume (no
// --retry-failed) does NOT clear ConditionFailureHalt — only --retry-failed clears it.
func TestResumeWithoutRetryFailedLeavesFailureHalt(t *testing.T) {
    // ... same fixture, retryFailed=false, assert ConditionFailureHalt still True
}
```

---

## Shared Patterns

### Pattern: Project-Wide Halt Condition (applies to the FOUR execution dispatch sites)

**Source:** `internal/controller/billing_halt.go` + the four EXECUTION dispatch site insertions

**Apply to:** `task_controller.go`, `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`

> `BillingHalt` gates all FIVE dispatch sites (it freezes spend everywhere, including planner authoring). `FailureHalt` gates only the FOUR EXECUTION sites — NOT the project-level planner-dispatch site (`project_controller.go`). Conservative failure halt is execution-only per locked D-03 / RESEARCH OQ-3; gating planning would wrongly freeze authoring of already-approved scopes.

The halt-gate pattern for `checkFailureHalt` must be added AFTER `checkBillingHalt` at the four EXECUTION dispatch sites. The insertion is a 4-line block identical in structure to the billing-halt block:

```go
if checkFailureHalt(project) {
    logf.FromContext(ctx).V(1).Info("dispatch held: project failure halt (conservative profile)",
        "<kind>", <kindObject>.Name, "project", project.Name)
    return <haltResult>, nil
}
```

The four EXECUTION-site `checkFailureHalt` insertions (from RESEARCH.md §Sources secondary):
- `task_controller.go` line ~371 (after billing halt at ~367)
- `plan_controller.go` line ~346 (after billing halt at ~342)
- `phase_controller.go` line ~349 (after billing halt at ~345)
- `milestone_controller.go` line ~351 (after billing halt at ~347)

The fifth `checkBillingHalt` site is intentionally EXCLUDED from `checkFailureHalt`:
- `project_controller.go` line ~1004 (after billing halt at ~1000) — this site dispatches the PROJECT-LEVEL PLANNER Job, NOT a task execution Job. Per locked D-03 (and RESEARCH OQ-3), conservative failure halt is execution-only — do NOT add `checkFailureHalt` here. Gating it would wrongly freeze authoring of already-approved scopes.

### Pattern: Status Condition via `meta.SetStatusCondition` + `client.MergeFrom` + `Status().Patch`

**Source:** `internal/controller/billing_halt.go` lines 125–134

```go
patch := client.MergeFrom(project.DeepCopy())
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:               <ConditionType>,
    Status:             metav1.ConditionTrue,
    Reason:             <Reason>,
    Message:            "<human-readable message>",
    LastTransitionTime: metav1.Now(),
})
return c.Status().Patch(ctx, project, patch)
```

**Apply to:** `setFailureHaltIfNeeded` in `failure_halt.go`

### Pattern: `meta.RemoveStatusCondition` + `Status().Patch` for condition clearing

**Source:** `cmd/tide/resume.go` lines 97–101

```go
patch2 := client.MergeFrom(proj.DeepCopy())
meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha2.ConditionBillingHalt)
if err := c.Status().Patch(ctx, &proj, patch2); err != nil {
    return fmt.Errorf("patch status (clear BillingHalt): %w", err)
}
```

**Apply to:** `cmd/tide/resume.go` FailureHalt clear block

### Pattern: `EnqueueRequestsFromMapFunc` global label-scoped mapper

**Source:** `internal/controller/task_controller.go` lines 1386–1414 (existing `siblingsToTaskMapper`); see also `project_controller.go` line 1896–1902 (`taskToProject` mapper)

The label-list-and-filter pattern for the mapper:
1. Extract `owner.LabelProject` from the changed object's labels — return nil if empty.
2. List all project Tasks by label in the same namespace.
3. Filter: only Tasks whose `Spec.DependsOn` contains the changed Task's name.
4. Skip self (`t.UID == task.UID`).
5. Append reconcile.Request for each match.

**Apply to:** `globalDependentsMapper` replacing `siblingsToTaskMapper`

### Pattern: Fake-client unit test with status subresource

**Source:** `internal/controller/billing_halt_test.go` lines 124–141

```go
s := fakeSchemeWithAll(t)
project := &tideprojectv1alpha2.Project{
    ObjectMeta: metav1.ObjectMeta{Name: "...", Namespace: "default"},
    Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2", TargetRepo: "https://example.com/repo.git"},
}
c := fake.NewClientBuilder().WithScheme(s).
    WithObjects(project).
    WithStatusSubresource(project).
    Build()
```

**Apply to:** `failure_halt_test.go` and `resume_test.go` extensions

---

## Resolved: Coarse-Ref Resolution in `computeIndegree` (was Open Question)

`computeIndegree` today treats every `dep` string in `task.Spec.DependsOn` as a Task name, so a coarse-ref dep (Plan/Phase/Milestone name) is mis-resolved. **Resolution (locked):** the shared fan-out resolver in `depgraph.go` (`buildScopeResolver`/`resolveScope`, 25-02 Task 1) is built UNCONDITIONALLY — it is the same resolver `assembleProjectDepGraph` uses, so the dispatch indegree and the wave map can never disagree about what an edge means (the D-01 "never disagree" clause). The global indegree compute (25-02 Task 2) resolves every dep through it; no TODO / follow-on fallback.

The Wave 0 grep (25-01 Task 2) is purely diagnostic — it records whether current fixtures already exercise coarse refs (A1), it does NOT gate whether the resolver is built. Likewise the `globalDependentsMapper` (25-02 Task 2) must re-enqueue dependents through the SAME resolver, so a coarse-ref dependent (e.g. `DependsOn=["plan-alpha"]`) is re-enqueued when any member task of `plan-alpha` completes/fails/holds — otherwise the dependent stalls until the next periodic resync (a DISP-01 liveness violation):

```bash
grep -rn "dependsOn" test/ internal/ --include="*.go" | grep -v "_test.go:" | head -20
```

---

## No Analog Found

No files lack a close match — every file in this phase is either a direct mirror of an existing file or a surgical modification to an existing file.

---

## Metadata

**Analog search scope:** `internal/controller/`, `api/v1alpha2/`, `cmd/tide/`, `test/integration/envtest/`
**Files scanned:** `billing_halt.go`, `billing_halt_test.go`, `task_controller.go` (key sections), `project_controller.go` (assembleProjectDepGraph), `shared_types.go`, `project_types.go`, `resume.go`, `resume_test.go`, `indegree_test.go`, `gates_test.go`, `suite_test.go`
**Pattern extraction date:** 2026-06-16
