# Phase 32: D3 — Dispatch Concurrency Cap - Pattern Map

**Mapped:** 2026-06-28
**Files analyzed:** 9 new/modified files
**Analogs found:** 9 / 9

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/controller/dispatch_helpers.go` | utility | request-response | `internal/controller/dispatch_helpers.go` (extend) | exact — same file, new function added |
| `internal/controller/dispatch_helpers_test.go` | test | request-response | `internal/pool/pool_test.go` + existing `dispatch_helpers_test.go` | exact |
| `internal/controller/milestone_controller.go` | controller | request-response | `internal/controller/phase_controller.go` (identical dispatch shape) | exact |
| `internal/controller/phase_controller.go` | controller | request-response | `internal/controller/milestone_controller.go` | exact |
| `internal/controller/plan_controller.go` | controller | request-response | `internal/controller/phase_controller.go` | exact |
| `internal/controller/project_controller.go` | controller | request-response | `internal/controller/milestone_controller.go` | exact |
| `internal/pool/pool.go` | utility | request-response | `internal/pool/pool.go` (extend — Capacity method) | exact |
| `internal/config/config.go` | config | transform | `internal/config/config.go` (change default value at line 117) | exact |
| `charts/tide/values.yaml` | config | transform | `charts/tide/values.yaml` (change plannerConcurrency at line 78) | exact |

---

## Pattern Assignments

### `internal/controller/dispatch_helpers.go` (utility — new function)

**Analog:** `internal/pool/pool.go` — `PreCharge` function (same list-by-label + count-non-terminal shape)

**Imports already present** (lines 39-56 of `dispatch_helpers.go`):
```go
import (
    "context"
    batchv1 "k8s.io/api/batch/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    logf "sigs.k8s.io/controller-runtime/pkg/log"
    // ... others already imported
)
```
Note: `batchv1` is already imported in `dispatch_helpers.go` (line 45). No new imports required.

**Core pattern — `plannerInFlightCount` helper** (new function, place after `resolveImage` at line 291):
```go
// plannerInFlightCount returns the count of non-terminal planner Jobs visible
// in the informer cache. Used by the D3 concurrency cap gate before PlannerPool.Acquire.
// An empty watchNamespace counts across all namespaces (cluster-scoped install).
func plannerInFlightCount(ctx context.Context, c client.Client, watchNamespace string) (int, error) {
    var jobs batchv1.JobList
    opts := []client.ListOption{
        client.MatchingLabels{"tideproject.k8s/role": "planner"},
    }
    if watchNamespace != "" {
        opts = append(opts, client.InNamespace(watchNamespace))
    }
    if err := c.List(ctx, &jobs, opts...); err != nil {
        return 0, err
    }
    n := 0
    for i := range jobs.Items {
        if !isJobTerminal(&jobs.Items[i]) {
            n++
        }
    }
    return n, nil
}
```

**`isJobTerminal` reuse:** The function `isJobTerminal` already exists in `internal/controller/task_controller.go:1706` within `package controller`. Since `dispatch_helpers.go` is in the same package, call it directly — do not create a third copy.

**Label source (confirmed):** `internal/dispatch/podjob/jobspec.go:217` stamps `labels["tideproject.k8s/role"] = "planner"` on every `JobKindPlanner` Job. The `pool.PreCharge` at `cmd/manager/main.go:350` uses the same selector (`"tideproject.k8s/role=planner"`), confirming the label key.

**MatchingLabels + InNamespace pattern — existing call site analog** (`project_controller.go:87-90`):
```go
r.List(ctx, &pods,
    client.InNamespace(namespace),
    client.MatchingLabels{"job-name": pushJobName},
)
```

---

### `internal/pool/pool.go` (utility — one-line Capacity method addition)

**Analog:** existing `Pool.Acquire`, `Pool.Release`, `countActive` methods in the same file.

**Core pattern — Capacity method** (add after `Release()` at line 78):
```go
// Capacity returns the maximum number of concurrent acquisitions this Pool permits.
// Used by the D3 concurrency cap gate to compare the live in-flight count against
// the configured cap without exposing the private sem channel.
func (p *Pool) Capacity() int {
    return cap(p.sem)
}
```
`cap()` is the Go built-in for buffered channel capacity; `p.sem` is `chan struct{}` (line 44). This does not alter crosspool analyzer behavior (the analyzer inspects `select` statements, not method calls).

---

### `internal/controller/milestone_controller.go` — D3 gate insertion (controller, request-response)

**Analog:** existing park-before-acquire pattern at `milestone_controller.go:368-377` (import-hold) and `milestone_controller.go:360-367` (budget-blocked). The new gate is the same shape — check condition, log V(1), return `RequeueAfter`.

**Existing park pattern to mirror** (lines 368-377 of `milestone_controller.go`):
```go
// Phase 28 IMPORT-01: park planner dispatch until import completes.
// Position: BEFORE pool acquire (Pitfall 2 — parking after acquire leaks a slot).
if earlyProject != nil && earlyProject.Spec.ImportSource != nil {
    c := meta.FindStatusCondition(earlyProject.Status.Conditions, tideprojectv1alpha2.ConditionImportComplete)
    if c == nil || c.Status != metav1.ConditionTrue {
        logf.FromContext(ctx).V(1).Info("import pending; holding planner dispatch",
            "milestone", ms.Name, "project", ms.Spec.ProjectRef)
        return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
    }
}
```

**Acquire block to insert before** (lines 380-386 of `milestone_controller.go`):
```go
// Step 3: Acquire plannerPool (POOL-01) before creating the Job (D-A4).
if r.PlannerPool != nil {
    if err := r.PlannerPool.Acquire(ctx); err != nil {
        return ctrl.Result{}, err
    }
    defer r.PlannerPool.Release()
}
```

**New D3 gate — insert BETWEEN the import-hold block (line 377) and the Acquire block (line 380):**
```go
// [D3] Concurrency cap: count non-terminal planner Jobs before acquiring a slot.
// Position: BEFORE Acquire (D-03 ordering invariant — parking after Acquire leaks a slot).
if r.PlannerPool != nil {
    inFlight, err := plannerInFlightCount(ctx, r.Client, r.WatchNamespace)
    if err != nil {
        return ctrl.Result{}, fmt.Errorf("planner in-flight count: %w", err)
    }
    if inFlight >= r.PlannerPool.Capacity() {
        logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
            "inFlight", inFlight, "cap", r.PlannerPool.Capacity(),
            "milestone", ms.Name)
        return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
    }
}
```

**`WatchNamespace` field:** Confirmed present on `MilestoneReconciler` struct (the pattern `r.WatchNamespace` is used at `milestone_controller.go:317` in the existing `client.List` for namespace-scope guard). Same field name applies to all four reconcilers.

---

### `internal/controller/phase_controller.go` — D3 gate insertion (identical shape)

**Analog:** `milestone_controller.go` gate (above). Existing park block is at `phase_controller.go:366-375`; Acquire block is at `phase_controller.go:378-384`. Insert gate between them.

**Existing Acquire block** (lines 378-384 of `phase_controller.go`):
```go
// Acquire plannerPool before creating Job (D-A4).
if r.PlannerPool != nil {
    if err := r.PlannerPool.Acquire(ctx); err != nil {
        return ctrl.Result{}, err
    }
    defer r.PlannerPool.Release()
}
```

Gate insertion is identical to the milestone pattern with `"phase", ph.Name` as the log key-value pair.

---

### `internal/controller/plan_controller.go` — D3 gate insertion (identical shape)

**Analog:** `milestone_controller.go` gate. Existing park block ends at `plan_controller.go:381`; Acquire block is at `plan_controller.go:384-390`.

Note: `plan_controller.go` dispatch path is a helper (`dispatchPlannerJob`) that returns `(ctrl.Result, bool, error)` rather than `(ctrl.Result, error)`. The gate return at this level returns `ctrl.Result{RequeueAfter: 10 * time.Second}, true, nil` (mirroring the existing `plan_controller.go:362` shape which returns the `true` bool).

---

### `internal/controller/project_controller.go` — D3 gate insertion (identical shape)

**Analog:** `milestone_controller.go` gate. Existing Acquire block is at `project_controller.go:1177-1183`. Insert gate immediately before it (after the adoption-suppression block that ends at line 1175).

Gate insertion is identical to the milestone pattern with `"project", project.Name` as the log key-value pair.

---

### `internal/config/config.go` — default value change (config, transform)

**Analog:** `internal/config/config.go:120` — `executorConcurrency` default `4` (the same `resolveField` call pattern).

**Target line 117** (current):
```go
if err := resolveField("plannerConcurrency", raw.PlannerConcurrency, 16, &out.PlannerConcurrency); err != nil {
```

**Change:** Replace `16` with `4`:
```go
if err := resolveField("plannerConcurrency", raw.PlannerConcurrency, 4, &out.PlannerConcurrency); err != nil {
```

**Validation pattern** (`config.go:138-143`) — `resolveField` already validates `>= 1`, so the new default `4` passes validation automatically. No other config.go changes needed.

---

### `charts/tide/values.yaml` — plannerConcurrency default change (config)

**Target line 78** (current):
```
plannerConcurrency: 16
```

**Change** (chart is the FIXED contract; binary catches up to chart):
```yaml
# Single-node-safe default. Caps concurrent in-flight planner Jobs across all
# planner levels (milestone/phase/plan/project) globally. Increase for multi-node
# clusters. Must be ≥ the widest expected planning wave for full throughput.
plannerConcurrency: 4
executorConcurrency: 4
```

The surrounding comment block (lines 71-79) documents the two budgets; add the cap comment inline above `plannerConcurrency: 4`.

---

### `internal/controller/dispatch_helpers_test.go` — new tests (test, request-response)

**Analog:** `internal/pool/pool_test.go` (fake client pattern) and existing `dispatch_helpers_test.go` (pure-function table tests).

**Fake client setup pattern** (`internal/pool/pool_test.go:38-45`):
```go
func newFakeClient(t *testing.T, objs ...client.Object) client.Client {
    t.Helper()
    s := runtime.NewScheme()
    if err := scheme.AddToScheme(s); err != nil {
        t.Fatalf("AddToScheme: %v", err)
    }
    return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}
```

**Standard testing pattern** (existing `dispatch_helpers_test.go:27-48`):
```go
func TestResolveProviderPerLevelWins(t *testing.T) {
    // ... construct minimal struct, call function, assert with t.Errorf
}
```

**New tests to add** (`TestPlannerInFlightCount` and `TestConcurrencyCapGate`):

`TestPlannerInFlightCount` — unit test with fake client:
- 3 non-terminal Jobs with `tideproject.k8s/role=planner` label, cap=3 → expect count=3
- 2 non-terminal + 1 terminal Job → expect count=2 (terminal not counted)
- 0 Jobs → expect count=0
- namespace-scoped: Jobs in ns "a" and ns "b", `watchNamespace="a"` → expect count only from "a"

`TestConcurrencyCapGate` — verifies reconcile returns `RequeueAfter > 0, nil` and does not Acquire: construct `MilestoneReconciler` with fake pool (capacity=1) and pre-create 1 non-terminal planner Job; call the reconcile path; assert `RequeueAfter > 0` and `err == nil`.

**Package:** `package controller` (same as `dispatch_helpers_test.go:12`), using `testing` not Ginkgo (matching the pure-function test file).

---

## Shared Patterns

### RequeueAfter Park (D-04 return shape)
**Source:** `internal/controller/milestone_controller.go:374-376` (import-hold) and `milestone_controller.go:365-367` (budget-blocked)
**Apply to:** All four D3 gate insertions (milestone, phase, plan, project controllers)
```go
return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
```
Use `10 * time.Second` for the D3 cap (longer than import-hold's `5s`; planner Jobs run minutes not seconds). Return `nil` error — cap-reached is not an error condition (D-04). List errors return a wrapped `fmt.Errorf(...)` to trigger standard requeue-on-error.

### V(1) Log Line (CONCUR-04 observability minimum)
**Source:** `internal/controller/milestone_controller.go:373`
**Apply to:** All four gate insertions
```go
logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
    "inFlight", inFlight, "cap", r.PlannerPool.Capacity(),
    "<level>", <obj>.Name)
```
V(1) matches the import-hold log at `milestone_controller.go:373` — verbose, not emitted at default log level, queryable with `-v=1`.

### RetryOnConflict + MergeFromWithOptimisticLock (WR-02/03 hardening)
**Source:** `internal/budget/tally.go:57-88` — `RollUpUsage`
**Apply to:** `MilestoneRolledUpUID` stamp in `milestone_controller.go:600-603`, `PhaseRolledUpUID` in `phase_controller.go:530-533`, `PlanRolledUpUID` in `plan_controller.go:605-608`
```go
if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
    latest := &tideprojectv1alpha2.Milestone{} // substitute Phase/Plan at each site
    if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
        return err
    }
    if latest.Status.MilestoneRolledUpUID == milestoneJobName {
        return nil // already set by a concurrent reconcile — idempotent
    }
    patch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
    latest.Status.MilestoneRolledUpUID = milestoneJobName
    return r.Status().Patch(ctx, latest, patch)
}); err != nil {
    // WR-03: exhausted retry budget — return err to requeue rather than swallow.
    return ctrl.Result{}, fmt.Errorf("patch MilestoneRolledUpUID: %w", err)
}
```
`retry` import: `"k8s.io/client-go/util/retry"` — already imported in `internal/budget/tally.go`; add to each controller's import block.

### Existing Marker Stamp (before WR-02 hardening)
**Source:** `internal/controller/milestone_controller.go:600-604`
```go
markerPatch := client.MergeFrom(ms.DeepCopy())
ms.Status.MilestoneRolledUpUID = milestoneJobName
if pErr := r.Status().Patch(ctx, ms, markerPatch); pErr != nil {
    logger.Error(pErr, "patch MilestoneRolledUpUID failed (non-fatal)", "milestone", ms.Name)
}
```
This is the existing code at each of the three sites — replace entirely with the `RetryOnConflict` + `MergeFromWithOptimisticLock` block shown above.

---

## No Analog Found

All files have strong existing analogs. No entries in this section.

---

## Metadata

**Analog search scope:** `internal/controller/`, `internal/pool/`, `internal/config/`, `internal/budget/`, `internal/metrics/`, `charts/tide/`
**Files scanned:** 11 source files read directly
**Pattern extraction date:** 2026-06-28
