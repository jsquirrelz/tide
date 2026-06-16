# Phase 24: Global Wave Derivation Engine - Pattern Map

**Mapped:** 2026-06-16
**Files analyzed:** 4 new/modified files
**Analogs found:** 4 / 4

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/controller/project_controller.go` | controller | CRUD + event-driven | `internal/controller/project_controller.go` (self-extension) | exact |
| `internal/controller/plan_controller.go` | controller | CRUD | `internal/controller/plan_controller.go` (self-removal) | exact |
| `internal/controller/wave_controller.go` | controller | event-driven | `internal/controller/wave_controller.go` (self-fix, 4 TODOs) | exact |
| `test/integration/envtest/global_wave_derivation_test.go` | test | CRUD | `test/integration/envtest/indegree_test.go` | exact |

---

## Pattern Assignments

### `internal/controller/project_controller.go` (controller, CRUD + event-driven)

**Change scope:** Extend `assembleProjectDepGraph` with fan-out, add `deriveGlobalWaves`/`stampGlobalTaskLabels` functions, refactor `checkGlobalCycleGate` to share assembled (nodes, edges) with the new derivation step.

**Analog:** `internal/controller/project_controller.go` (existing functions at lines 1432-1577) + `internal/controller/plan_controller.go` (lines 1339-1455, ported pattern)

---

#### Imports pattern
The existing file already imports everything needed. No new imports are required beyond what the file already has (`pkg/dag`, `internal/owner`, `internal/metrics`, `apierrors`, `metav1`). Verify the file's import block before editing to confirm.

---

#### `assembleProjectDepGraph` — current conservative implementation (lines 1432-1479)

```go
// assembleProjectDepGraph builds the task-level dependency graph for the given
// Project by listing all v1alpha1.Tasks in the namespace that carry the project
// label. It returns (nodes, edges, error).
//
// Edge filtering — CONSERVATIVE by design (RESEARCH OQ#3):
// Only task-to-task edges are emitted. A DependsOn entry that names a Plan,
// Phase, or Milestone (coarse scope ref) is SKIPPED because Phase 24's
// fan-out assembler will expand those refs into task-level edges.
func (r *ProjectReconciler) assembleProjectDepGraph(
	ctx context.Context,
	project *tidev1alpha2.Project,
) (nodes []dag.NodeID, edges []dag.Edge, err error) {
	var taskList tidev1alpha2.TaskList
	if listErr := r.List(ctx, &taskList,
		client.InNamespace(project.Namespace),
		client.MatchingLabels{owner.LabelProject: project.Name},
	); listErr != nil {
		return nil, nil, fmt.Errorf("list tasks for project %s: %w", project.Name, listErr)
	}

	taskNames := make(map[string]struct{}, len(taskList.Items))
	for i := range taskList.Items {
		taskNames[taskList.Items[i].Name] = struct{}{}
	}

	nodes = make([]dag.NodeID, 0, len(taskList.Items))
	for i := range taskList.Items {
		nodes = append(nodes, taskList.Items[i].Name)
	}

	for i := range taskList.Items {
		t := &taskList.Items[i]
		for _, dep := range t.Spec.DependsOn {
			// Phase 23: only wire edges when dep names a known task.
			// Coarse scope refs skipped — Phase 24 fan-out will add those edges.
			if _, isTask := taskNames[dep]; isTask {
				edges = append(edges, dag.Edge{From: dep, To: t.Name})
			}
		}
	}
	return nodes, edges, nil
}
```

**Phase 24 change:** Add List calls for `PlanList`, `PhaseList`, `MilestoneList`; build in-memory resolution maps (`tasksByPlan`, `planToPhase`, `phaseToMS`); replace the conservative task-only edge loop with the full fan-out helper `tasksForScope`; add edge deduplication via `map[string]struct{}`; also iterate over `Plan.Spec.DependsOn`, `Phase.Spec.DependsOn`, `Milestone.Spec.DependsOn` and emit cross-task edges (all Tasks in the owning scope depend on all Tasks in the referenced scope).

---

#### `checkGlobalCycleGate` — current implementation (lines 1482-1528)

```go
func (r *ProjectReconciler) checkGlobalCycleGate(
	ctx context.Context,
	project *tidev1alpha2.Project,
) (blocked bool, result ctrl.Result, err error) {
	nodes, edges, asmErr := r.assembleProjectDepGraph(ctx, project)
	if asmErr != nil {
		return false, ctrl.Result{}, fmt.Errorf("assemble dep graph for project %s: %w", project.Name, asmErr)
	}

	// ComputeWaves validates the graph and returns *CycleError on a cycle.
	// The computed waves are deliberately DISCARDED — the gate only validates;
	// it does NOT store the schedule (PERSIST-03 / verify-no-aggregates).
	if _, computeErr := dag.ComputeWaves(nodes, edges); computeErr != nil {
		var cyc *dag.CycleError
		if goErrors.As(computeErr, &cyc) {
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               "CycleDetected",
				Status:             metav1.ConditionTrue,
				Reason:             tidev1alpha2.ReasonGlobalCycleDetected,
				Message:            fmt.Sprintf("cyclic global Execution DAG involving: %v", cyc.InvolvedNodes),
				LastTransitionTime: metav1.Now(),
			})
			if updateErr := r.Status().Update(ctx, project); updateErr != nil {
				logf.FromContext(ctx).Error(updateErr,
					"failed to update GlobalCycleDetected condition",
					"project", project.Name,
					"involved", cyc.InvolvedNodes)
			}
			// NOT a TerminalError — a plan edit can remove the cycle; allow requeue.
			return true, ctrl.Result{}, nil
		}
		// Non-cycle error (e.g., unknown node from edge assembler defect) — transient requeue.
		return false, ctrl.Result{}, fmt.Errorf("ComputeWaves error for project %s: %w", project.Name, computeErr)
	}
	return false, ctrl.Result{}, nil
}
```

**Phase 24 change (Pitfall 7 mitigation):** Refactor the callee so `assembleProjectDepGraph` is called ONCE in the reconcile and its `(nodes, edges)` result is passed into both `checkGlobalCycleGate` (now takes pre-assembled args instead of calling the assembler itself) and `deriveGlobalWaves`. This halves API server List calls per reconcile and shares the fan-out result.

---

#### `taskToProject` mapper — current implementation (lines 1530-1543)

```go
// taskToProject maps a Task to a reconcile.Request for its owning Project,
// read from the canonical tideproject.k8s/project label (owner.LabelProject).
func (r *ProjectReconciler) taskToProject(_ context.Context, obj client.Object) []reconcile.Request {
	projectName := obj.GetLabels()[owner.LabelProject]
	if projectName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: projectName},
	}}
}
```

**Phase 24 change:** No change. Already correctly wires Task add/update events to ProjectReconciler (D-10). The Watch is already declared at `SetupWithManager` line 1572 — `Watches(&tidev1alpha2.Task{}, handler.EnqueueRequestsFromMapFunc(r.taskToProject))`.

---

#### `SetupWithManager` — current (lines 1548-1577); Phase 24 adds `Owns(&tidev1alpha2.Wave{})`

```go
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// ...
	return ctrl.NewControllerManagedBy(mgr).
		For(&tidev1alpha2.Project{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Owns(&batchv1.Job{}).
		Owns(&tidev1alpha2.Milestone{}).
		Watches(&tidev1alpha2.Task{}, handler.EnqueueRequestsFromMapFunc(r.taskToProject)).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("project").
		Complete(r)
}
```

**Phase 24 change:** Add `Owns(&tidev1alpha2.Wave{})` so Project-owned Wave CRs trigger ProjectReconciler on change. Wave ownership moves here from PlanReconciler.

---

#### New function: `deriveGlobalWaves` — port from `materializeWaves` (plan_controller.go:1339-1412)

**Core idempotent Create + AlreadyExists + exactly-once metric pattern** (lines 1379-1411 of plan_controller.go):

```go
// Check if Wave already exists.
var existing tideprojectv1alpha2.Wave
if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: waveName}, &existing); err != nil {
    if client.IgnoreNotFound(err) != nil {
        return fmt.Errorf("get wave %s: %w", waveName, err)
    }
    // Wave does not exist — set owner ref and create.
    if err := owner.EnsureOwnerRef(wave, plan, r.Scheme); err != nil {
        return fmt.Errorf("ensure owner ref wave %s: %w", waveName, err)
    }
    if err := r.Create(ctx, wave); err != nil {
        if !apierrors.IsAlreadyExists(err) {
            return fmt.Errorf("create wave %s: %w", waveName, err)
        }
        // AlreadyExists: idempotent success — watch-lag race (CR-01).
        // The reconcile that successfully created this Wave already counted it;
        // do NOT increment WavesDispatchedTotal here.
    } else {
        // Create succeeded — this is the once-only dispatch commit point.
        tidemetrics.WavesDispatchedTotal.WithLabelValues(projectName, phaseName, plan.Name).Inc()
    }
    logger.Info("created wave", "wave", waveName, "index", i)
} else {
    // Wave exists — ensure owner ref is set (may be missing on first reconcile
    // after a restart where the Wave was created but the Plan was not owner-set).
    // Do NOT increment WavesDispatchedTotal — this is a reconcile replay.
    if err := owner.EnsureOwnerRef(&existing, plan, r.Scheme); err == nil {
        _ = r.Update(ctx, &existing)
    }
}
```

**Adapt for global derivation:** Replace `plan.UID` naming with `project.Name` naming (`tide-wave-<project.Name>-<i>`); replace `owner.EnsureOwnerRef(wave, plan, ...)` with `owner.EnsureOwnerRef(wave, project, ...)`; replace metric `WithLabelValues(projectName, phaseName, plan.Name)` with `WithLabelValues(project.Name, "global", "global")` sentinel (Pitfall 3 — never emit empty label values; `"global"` is the chosen sentinel for phase/plan when waves are Project-scoped).

Add a prune loop after the create loop:

```go
// Prune stale Wave CRs (wave count decreased after re-derivation).
var allWaves tidev1alpha2.WaveList
if listErr := r.List(ctx, &allWaves,
    client.InNamespace(project.Namespace),
    client.MatchingLabels{owner.LabelProject: project.Name},
); listErr != nil {
    return fmt.Errorf("list waves for prune: %w", listErr)
}
for i := range allWaves.Items {
    w := &allWaves.Items[i]
    if w.Spec.ProjectRef == project.Name && w.Spec.WaveIndex >= len(globalWaves) {
        if delErr := r.Delete(ctx, w); delErr != nil && !apierrors.IsNotFound(delErr) {
            return fmt.Errorf("prune wave %s: %w", w.Name, delErr)
        }
    }
}
```

---

#### New function: `stampGlobalTaskLabels` — port from `stampTaskLabels` (plan_controller.go:1421-1455)

```go
// stampTaskLabels patches each Task in layers[N] with:
//   - tideproject.k8s/wave-index=<N>
//   - tideproject.k8s/project=<projectName>
func (r *PlanReconciler) stampTaskLabels(ctx context.Context, tasks []tideprojectv1alpha2.Task, layers [][]dag.NodeID, projectName string) error {
    // Build a name → layer-index map.
    taskLayer := make(map[string]int, len(tasks))
    for i, layer := range layers {
        for _, name := range layer {
            taskLayer[name] = i
        }
    }

    for i := range tasks {
        t := &tasks[i]
        layerIdx, ok := taskLayer[t.Name]
        if !ok {
            continue
        }
        waveIndexStr := fmt.Sprintf("%d", layerIdx)
        // Skip if labels are already correct.
        if t.Labels["tideproject.k8s/wave-index"] == waveIndexStr &&
            (projectName == "" || t.Labels["tideproject.k8s/project"] == projectName) {
            continue
        }
        patch := client.MergeFrom(t.DeepCopy())
        if t.Labels == nil {
            t.Labels = map[string]string{}
        }
        t.Labels["tideproject.k8s/wave-index"] = waveIndexStr
        if projectName != "" {
            t.Labels["tideproject.k8s/project"] = projectName
        }
        if err := r.Patch(ctx, t, patch); err != nil {
            return fmt.Errorf("stamp task labels on %s: %w", t.Name, err)
        }
    }
    return nil
}
```

**Adapt for global path:** Signature becomes `stampGlobalTaskLabels(ctx, taskList []tidev1alpha2.Task, globalWaves [][]dag.NodeID, projectName string) error` on `ProjectReconciler`. The pattern is identical; pass the full Task objects from the assembler's `taskList.Items` to avoid redundant Gets. The `client.MergeFrom(t.DeepCopy()) + r.Patch(ctx, t, patch)` idiom is the load-bearing pattern — copy it verbatim.

---

### `internal/controller/plan_controller.go` (controller, CRUD — removal)

**Change scope:** Remove `materializeWaves` (lines 1339-1413), remove `stampTaskLabels` (lines 1415-1455), remove `Owns(&tidev1alpha2.Wave{})` from `SetupWithManager` (line 1498). The callers within `reconcileWaveMaterialization` (around line 1009-1120) that invoke these two functions must be removed or stubbed to a no-op return.

**Analog:** Self-removal pattern — no external analog. The key invariant is: after the change, `PlanReconciler` must not contain any call site that creates Wave CRs or stamps wave-index labels. Grep confirmation before merging: `grep -nE 'materializeWaves|stampTaskLabels|WavesDispatchedTotal|Wave{}' internal/controller/plan_controller.go` must return zero hits in the wave-creation paths.

**Critical removal: `SetupWithManager` line 1498**

```go
// REMOVE this line:
Owns(&tideprojectv1alpha2.Wave{}).
```

This must be removed in the same plan that removes `materializeWaves` (Pitfall 1). If left in place, `PlanReconciler` will spuriously re-reconcile on every Project-owned Wave CR create/update from `ProjectReconciler`, causing log noise and potential owner-ref confusion (Pitfall 6).

**Metric sentinel concern:** `WavesDispatchedTotal.WithLabelValues(projectName, phaseName, plan.Name)` at line 1399 of plan_controller.go is removed with `materializeWaves`. The equivalent increment in the new `ProjectReconciler.deriveGlobalWaves` uses `WithLabelValues(project.Name, "global", "global")` — the sentinel rule from line 1354 of plan_controller.go ("never emit an empty label value") must be honored in the new site.

---

### `internal/controller/wave_controller.go` (controller, event-driven — 4 TODO closures)

**Change scope:** Close four Phase-24 TODO comments by implementing the correct behavior at each site.

**Analog:** Self-modification — patterns are already partially implemented in the same file.

---

#### TODO at line 104 — owner ref under ProjectRef

```go
// TODO(phase-24): in v1alpha2 Wave carries ProjectRef not PlanRef; the per-plan
// materializeWaves stub (Plan 23-02) sets the owner-ref at create time so the
// reconciler no longer needs to look up the parent here. Phase 24 will re-own
// Wave under Project; this step will then resolve ProjectRef → Project and set
// the owner ref. For now, skip the owner-ref walk — materializeWaves stamps it.
```

**Fix:** Remove the TODO comment. `ProjectReconciler.deriveGlobalWaves` sets the owner ref at create time via `owner.EnsureOwnerRef(wave, project, r.Scheme)`. No action required in `WaveReconciler` for newly-created Waves. Optionally add a fallback: if `wave.OwnerReferences` is empty and `wave.Spec.ProjectRef != ""`, fetch the Project and call `owner.EnsureOwnerRef`. Pattern for this fallback is the same `owner.EnsureOwnerRef` + `r.Update` at plan_controller.go:1406-1408.

---

#### TODO at line 134 + `reconcileObservational` label query (lines 141-160) — already correct, remove TODO

```go
// TODO(phase-24): re-wire Wave→Task association off the global wave index;
// ProjectRef-scoped listing lands with the global scheduler (Phase 24).

// Step 1: List Tasks by the tideproject.k8s/wave-index label stamped by PlanReconciler.
waveIndexLabel := fmt.Sprintf("%d", wave.Spec.WaveIndex)
var taskList tideprojectv1alpha2.TaskList
if err := r.List(ctx, &taskList,
    client.InNamespace(wave.Namespace),
    client.MatchingLabels{
        "tideproject.k8s/wave-index": waveIndexLabel,
        owner.LabelProject:           wave.Spec.ProjectRef,
    },
); err != nil {
    return ctrl.Result{}, fmt.Errorf("list tasks for wave %s: %w", wave.Name, err)
}
```

**Fix:** Remove the TODO comment at line 134. The label query at lines 152-160 is already the correct global implementation — it queries `tideproject.k8s/wave-index == waveIndexLabel` AND `tideproject.k8s/project == wave.Spec.ProjectRef`. Once `ProjectReconciler` stamps global wave-index labels (replacing per-plan indices), this query becomes correct at Project scope with no code change needed. The TODO is the only thing to remove.

---

#### TODOs at lines 236 and 248 — `taskToWaveMapper`

Current stub (lines 240-263):

```go
func (r *WaveReconciler) taskToWaveMapper(ctx context.Context, obj client.Object) []reconcile.Request {
    task, ok := obj.(*tideprojectv1alpha2.Task)
    if !ok {
        return nil
    }
    if task.Spec.PlanRef == "" {
        return nil
    }
    // TODO(phase-24): associate Wave→Task via global wave index (ProjectRef-scoped).
    // For now, list all v1alpha2 Waves in the same namespace and enqueue them all.
    var waveList tideprojectv1alpha2.WaveList
    if err := r.List(ctx, &waveList,
        client.InNamespace(task.Namespace),
    ); err != nil {
        return nil
    }
    reqs := make([]reconcile.Request, 0, len(waveList.Items))
    for _, w := range waveList.Items {
        reqs = append(reqs, reconcile.Request{
            NamespacedName: client.ObjectKey{Namespace: w.Namespace, Name: w.Name},
        })
    }
    return reqs
}
```

**Fix:** Replace the "list all Waves in namespace" approach with a targeted O(1) lookup using the Task's `tideproject.k8s/wave-index` label and `tideproject.k8s/project` label:

```go
func (r *WaveReconciler) taskToWaveMapper(_ context.Context, obj client.Object) []reconcile.Request {
    task, ok := obj.(*tideprojectv1alpha2.Task)
    if !ok {
        return nil
    }
    labels := task.GetLabels()
    projectName := labels[owner.LabelProject]
    waveIndexStr := labels["tideproject.k8s/wave-index"]
    if projectName == "" || waveIndexStr == "" {
        return nil
    }
    waveName := fmt.Sprintf("tide-wave-%s-%s", projectName, waveIndexStr)
    return []reconcile.Request{{
        NamespacedName: client.ObjectKey{Namespace: task.Namespace, Name: waveName},
    }}
}
```

This is an O(1) name derivation using the same naming convention as `deriveGlobalWaves` (`tide-wave-<project.Name>-<globalIndex>`). Remove both TODO comments at lines 236 and 248.

**Pattern source for mapper style:** `project_controller.go:taskToProject` (lines 1535-1542) — identical mapper shape: read one label from the object, construct a NamespacedName, return a single-element slice.

---

### `test/integration/envtest/global_wave_derivation_test.go` (test, CRUD — new file)

**Analog:** `test/integration/envtest/indegree_test.go` (entire file, 367 lines)

**Package, imports, and suite registration pattern** (indegree_test.go:1-42):

```go
package envtest_integration

import (
    "context"
    "fmt"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)
```

**BeforeEach / AfterEach cleanup pattern** (indegree_test.go:46-91): The AfterEach deletes Tasks, Plans, Waves, Projects, and PVCs in the test namespace. New test file must follow the same cleanup shape — delete Waves explicitly in AfterEach so stale global Wave CRs from one It block do not contaminate the next.

**`createSimplePlan` helper** (indegree_test.go:308-323):

```go
func createSimplePlan(ctx context.Context, name string) {
    plan := &tideprojectv1alpha2.Plan{
        ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: indegreeNamespace},
        Spec:       tideprojectv1alpha2.PlanSpec{PhaseRef: "test-phase"},
    }
    Expect(k8sClient.Create(ctx, plan)).To(Succeed())
    Eventually(func() error {
        return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: indegreeNamespace}, &tideprojectv1alpha2.Plan{})
    }, "5s", "100ms").Should(Succeed())
}
```

**`makeTask` / `makeTaskWithWaveLabel` helpers** (indegree_test.go:325-366):

```go
func makeTask(ctx context.Context, name, planRef string, dependsOn, files []string) *tideprojectv1alpha2.Task {
    return makeTaskWithWaveLabel(ctx, name, planRef, dependsOn, files, -1)
}

func makeTaskWithWaveLabel(ctx context.Context, name, planRef string, dependsOn, files []string, waveIndex int) *tideprojectv1alpha2.Task {
    labels := map[string]string{"tideproject.k8s/project": indegreeTestProject}
    if waveIndex >= 0 {
        labels["tideproject.k8s/wave-index"] = fmt.Sprintf("%d", waveIndex)
    }
    task := &tideprojectv1alpha2.Task{
        ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: indegreeNamespace, Labels: labels},
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
        return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: indegreeNamespace}, &tideprojectv1alpha2.Task{})
    }, "5s", "100ms").Should(Succeed())
    time.Sleep(50 * time.Millisecond) // allow indexer to propagate
    return task
}
```

**New file must extend these helpers with:**
- `createSimplePhase(ctx, name, milestoneRef string)` — creates a Phase with `Spec.MilestoneRef = milestoneRef`; follows `createSimplePlan` shape.
- `createSimpleMilestone(ctx, name, projectRef string)` — creates a Milestone with `Spec.ProjectRef = projectRef`; same shape.

**Wave CR assertion pattern** (indegree_test.go:232-240, existing Wave assertion):

```go
Eventually(func() string {
    w := &tideprojectv1alpha2.Wave{}
    if err := k8sClient.Get(ctx, client.ObjectKey{Name: waveName, Namespace: ns}, w); err != nil {
        return ""
    }
    return w.Status.Phase
}, "60s", "500ms").Should(Equal("Succeeded"))
```

For EXEC-01/02/03/04 tests, assert:
1. Wave CRs named `tide-wave-<project>-0`, `...-1`, `...-2` exist with correct `Spec.WaveIndex`.
2. Each Task's `tideproject.k8s/wave-index` label equals its expected global wave index (not a per-plan index).
3. Label selector `client.MatchingLabels{"tideproject.k8s/wave-index": "0", "tideproject.k8s/project": projectName}` returns the correct task set.

**Wave CR name assertion helper (new, based on indegree_test.go Get pattern):**

```go
Eventually(func() error {
    wave := &tideprojectv1alpha2.Wave{}
    return k8sClient.Get(ctx, client.ObjectKey{
        Name:      fmt.Sprintf("tide-wave-%s-%d", projectName, waveIdx),
        Namespace: testNamespace,
    }, wave)
}, "30s", "500ms").Should(Succeed(), "Wave CR tide-wave-%s-%d should exist", projectName, waveIdx)
```

---

## Shared Patterns

### Owner-ref assignment on child CR create
**Source:** `internal/controller/plan_controller.go` lines 1386-1388
**Apply to:** `ProjectReconciler.deriveGlobalWaves` — every Wave CR create site

```go
if err := owner.EnsureOwnerRef(wave, project, r.Scheme); err != nil {
    return fmt.Errorf("ensure owner ref wave %s: %w", waveName, err)
}
```

### Task label patch (MergeFrom + Patch)
**Source:** `internal/controller/plan_controller.go` lines 1442-1453
**Apply to:** `ProjectReconciler.stampGlobalTaskLabels`

```go
patch := client.MergeFrom(t.DeepCopy())
if t.Labels == nil {
    t.Labels = map[string]string{}
}
t.Labels["tideproject.k8s/wave-index"] = waveIndexStr
if projectName != "" {
    t.Labels["tideproject.k8s/project"] = projectName
}
if err := r.Patch(ctx, t, patch); err != nil {
    return fmt.Errorf("stamp task labels on %s: %w", t.Name, err)
}
```

### Metric sentinel rule — never emit empty label values
**Source:** `internal/controller/plan_controller.go` lines 1351-1361
**Apply to:** `ProjectReconciler.deriveGlobalWaves` metric increment

```go
// resolveProjectName is non-fatal — on error use "unknown" (Metric Label Sentinel,
// Pitfall 4 — never emit an empty label value). Wave materialization proceeds
// regardless of label resolution success.
projectName, err := r.resolveProjectName(ctx, plan)
if err != nil {
    projectName = "unknown"
}
phaseName := plan.Spec.PhaseRef
if phaseName == "" {
    phaseName = "unknown"
}
```

For global waves: `phase` and `plan` must not be empty. Use `"global"` as the sentinel for both when emitting `WavesDispatchedTotal.WithLabelValues(project.Name, "global", "global")`.

### Status condition set + update error handling
**Source:** `internal/controller/project_controller.go` lines 1507-1520 (`checkGlobalCycleGate`)
**Apply to:** Any new status conditions added to Project (e.g., `GlobalScheduleReady`)

```go
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:               "CycleDetected",
    Status:             metav1.ConditionTrue,
    Reason:             tidev1alpha2.ReasonGlobalCycleDetected,
    Message:            fmt.Sprintf("cyclic global Execution DAG involving: %v", cyc.InvolvedNodes),
    LastTransitionTime: metav1.Now(),
})
if updateErr := r.Status().Update(ctx, project); updateErr != nil {
    logf.FromContext(ctx).Error(updateErr,
        "failed to update GlobalCycleDetected condition",
        "project", project.Name,
        "involved", cyc.InvolvedNodes)
}
```

### `pkg/dag.ComputeWaves` + `CycleError` usage
**Source:** `pkg/dag/kahn.go` lines 46-97, `pkg/dag/errors.go`
**Apply to:** `ProjectReconciler.deriveGlobalWaves` (D-11 — reused unchanged)

```go
// ComputeWaves returns the layered topological sort of (nodes, edges).
// Each returned wave is sorted lexicographically for determinism.
// Returns *CycleError if the graph contains a cycle.
// Complexity: O(V + E).
func ComputeWaves(nodes []NodeID, edges []Edge) ([][]NodeID, error)

type CycleError struct {
    InvolvedNodes []NodeID  // sorted lexicographically
}
```

The global assembler feeds this with the full Project-scoped node/edge set. The function is k8s-free and must remain so (`verify-dag-imports` guard). Call it after fan-out assembly; handle `*CycleError` separately from transient errors.

### `owner.LabelProject` constant and `StampProjectLabel`
**Source:** `internal/owner/label.go` lines 33-55
**Apply to:** Any code that reads or stamps the project label

```go
const LabelProject = "tideproject.k8s/project"

func StampProjectLabel(obj metav1.Object, projectName string) {
    // stamps tideproject.k8s/project on obj; overwrites existing value
}
```

The label value for the wave-index label key is `"tideproject.k8s/wave-index"` (a string literal — no constant defined). Use `owner.LabelProject` for the project label; use the string literal for the wave-index label, matching the existing pattern in `stampTaskLabels` (plan_controller.go:1446).

### `WavesDispatchedTotal` metric label arity
**Source:** `internal/metrics/registry.go` lines 132-138
**Apply to:** `ProjectReconciler.deriveGlobalWaves` — the one call site that replaces the per-plan increment

```go
WavesDispatchedTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_waves_dispatched_total",
        Help: "Count of Waves dispatched to the executor pool, surfaced by Project, Phase, and Plan (Phase 4 D-O2).",
    },
    []string{"project", "phase", "plan"},
)
```

**Label arity is locked at 3: `{project, phase, plan}`.** For global waves emitted by ProjectReconciler, use `WithLabelValues(project.Name, "global", "global")`. The `"global"` sentinel satisfies the "never emit empty label value" constraint (Pitfall 3).

---

## No Analog Found

All four files have close analogs. No files require falling back to RESEARCH.md external patterns.

---

## Metadata

**Analog search scope:** `internal/controller/`, `pkg/dag/`, `internal/owner/`, `internal/metrics/`, `test/integration/envtest/`
**Files scanned:** 7 analog files read directly
**Pattern extraction date:** 2026-06-16
