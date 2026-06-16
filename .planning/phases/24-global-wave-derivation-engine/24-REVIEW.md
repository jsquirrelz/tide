---
phase: 24-global-wave-derivation-engine
reviewed: 2026-06-16T00:00:00Z
depth: deep
files_reviewed: 3
files_reviewed_list:
  - internal/controller/project_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/wave_controller.go
findings:
  critical: 1
  warning: 5
  info: 4
  total: 10
status: issues_found
---

# Phase 24: Code Review Report

**Reviewed:** 2026-06-16
**Depth:** deep
**Files Reviewed:** 3
**Status:** issues_found

## Summary

Phase 24 moves wave derivation from a per-plan path (`PlanReconciler.materializeWaves` / `stampTaskLabels`) to a GLOBAL engine in `ProjectReconciler` (`assembleProjectDepGraph` → `checkGlobalCycleGate` → `deriveGlobalWaves` → `stampGlobalTaskLabels`). The assemble-once refactor, the metric exactly-once shape, the `"global"` sentinel labels, the `taskToWaveMapper` O(1) name derivation, and the full removal of the per-plan Wave-creation path are all implemented as described. The single-writer migration is clean: `PlanReconciler.SetupWithManager` correctly drops `Owns(&Wave{})` while keeping `Owns(&Task{})`/`Owns(&Job{})` so Plan reconcile triggers survive, and no per-plan code path creates Waves or stamps `wave-index` anymore.

However the review surfaces one BLOCKER: the **stale-Wave prune is dead code** — it lists Waves by a project label that `deriveGlobalWaves` never stamps on the Wave CR, so the `List` always returns zero and orphaned Waves leak permanently after a dependency removal. A cluster of WARNINGs follow: the `CycleDetected` condition is never cleared (contradicting its own doc comment), an unconditional `Update` on every existing Wave every reconcile (with a swallowed error), `ComputeWaves` is run twice per reconcile, directly-applied unlabeled Tasks are invisible to global derivation, and a label-stamp staleness window can mis-route the `taskToWaveMapper`.

## Critical Issues

### CR-01: Stale-Wave prune lists by a label the Wave CR never carries — prune is dead code, Waves leak

**File:** `internal/controller/project_controller.go:1660-1719`
**Confidence:** High

`deriveGlobalWaves` creates Wave CRs with only `Spec{ProjectRef, WaveIndex}` and an owner ref — the `ObjectMeta` has **no `Labels`** (lines 1660-1669):

```go
wave := &tidev1alpha2.Wave{
    ObjectMeta: metav1.ObjectMeta{ Name: waveName, Namespace: project.Namespace },
    Spec: tidev1alpha2.WaveSpec{ ProjectRef: project.Name, WaveIndex: i },
}
```

There is no Wave defaulting webhook that stamps `tideproject.k8s/project` (confirmed: `internal/webhook/v1alpha2/wave_webhook.go` only validates). Yet the prune lists Waves with a label selector (lines 1704-1708):

```go
if listErr := r.List(ctx, &allWaves,
    client.InNamespace(project.Namespace),
    client.MatchingLabels{owner.LabelProject: project.Name},   // <- Waves have no such label
); listErr != nil { ... }
```

Because no Wave carries `owner.LabelProject`, `allWaves.Items` is always empty, so the `WaveIndex >= len(globalWaves)` prune at lines 1711-1719 **never deletes anything**. When re-derivation produces fewer waves (a `dependsOn` edge removed, Tasks deleted), the now-stale high-index Wave CRs (`tide-wave-<project>-<N>`) are never pruned. They linger, their owner ref keeps them alive while the Project exists, and `WaveReconciler` keeps reconciling them (rolling up zero members → "Running" forever, or "Succeeded" with `len(members)==0` guarded out → "Running"). This is an orphan/leak defect and defeats the entire prune block.

**Fix:** Stamp the project label on the Wave at create time AND make the prune use a selector the Waves actually carry (or list all Waves in-namespace and filter on `Spec.ProjectRef`, which the loop body already re-checks at line 1713):

```go
wave := &tidev1alpha2.Wave{
    ObjectMeta: metav1.ObjectMeta{
        Name:      waveName,
        Namespace: project.Namespace,
        Labels:    map[string]string{owner.LabelProject: project.Name},
    },
    Spec: tidev1alpha2.WaveSpec{ProjectRef: project.Name, WaveIndex: i},
}
```

and either keep the label selector (now satisfied) or drop it and rely on the existing `w.Spec.ProjectRef == project.Name` guard:

```go
var allWaves tidev1alpha2.WaveList
if listErr := r.List(ctx, &allWaves, client.InNamespace(project.Namespace)); listErr != nil { ... }
for i := range allWaves.Items {
    w := &allWaves.Items[i]
    if w.Spec.ProjectRef == project.Name && w.Spec.WaveIndex >= len(globalWaves) { /* delete */ }
}
```

Note the same dead-selector also affects the label-stamp re-List at lines 1724-1730 (`MatchingLabels{owner.LabelProject: project.Name}` on **Tasks**) — that one is fine because the reporter stamps the project label on Tasks at create time, but it shares the fragility (see WR-04). Add a focused envtest: create a Project with N Tasks producing M>1 waves, remove a `dependsOn`, re-reconcile, assert the high-index Wave CR is deleted — this would have caught the dead prune.

## Warnings

### WR-01: `CycleDetected` condition is never cleared — contradicts its own doc comment

**File:** `internal/controller/project_controller.go:1797-1828`, comment at `1834`
**Confidence:** High

The comment on `taskToProject` (line 1833-1835) states: "re-enqueues the Project on any Task DependsOn edit so checkGlobalCycleGate re-runs and the sticky CycleDetected condition clears once the cycle is broken (WR-02)." But `checkGlobalCycleGate` only ever **sets** `CycleDetected=True` (lines 1809-1815). On the no-cycle path it returns `false, ctrl.Result{}, nil` (line 1828) without flipping the existing condition to `False` or removing it. After an operator breaks a cycle, the Project re-reconciles, the gate passes, Waves derive correctly — but `CycleDetected=True` stays stamped forever, a misleading operator signal. The "verify before claiming" gap: the comment asserts behavior the code does not implement.

**Fix:** On the pass path, clear the condition before returning:

```go
// no cycle — clear any prior sticky CycleDetected
if meta.FindStatusCondition(project.Status.Conditions, "CycleDetected") != nil {
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type: "CycleDetected", Status: metav1.ConditionFalse,
        Reason: "NoCycle", Message: "global Execution DAG is acyclic",
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Update(ctx, project); err != nil { return false, ctrl.Result{}, err }
}
return false, ctrl.Result{}, nil
```

### WR-02: Existing-Wave owner-ref `Update` runs unconditionally every reconcile with a swallowed error

**File:** `internal/controller/project_controller.go:1692-1698`
**Confidence:** High

```go
} else {
    if ownerErr := owner.EnsureOwnerRef(&existing, project, r.Scheme); ownerErr == nil {
        _ = r.Update(ctx, &existing)
    }
}
```

`EnsureOwnerRef` → `SetControllerReference` is idempotent on data but returns `nil` whether or not it mutated `existing`, so `r.Update` fires on **every** reconcile for **every** already-existing Wave — and the global derivation runs on every Project reconcile and on every Task watch event (`taskToProject`). That is a Wave write per-Wave per-reconcile-of-the-Project regardless of whether the owner ref changed. The error is discarded (`_ =`), so a real conflict/permission failure is silently masked, and the churn bumps `resourceVersion` + re-triggers the `Owns(&Wave{})` watch → extra Project reconciles (a mild self-feedback loop). Gate the Update on an actual change.

**Fix:** Only update when the owner ref was absent:

```go
if !metav1.IsControlledBy(&existing, project) {
    if err := owner.EnsureOwnerRef(&existing, project, r.Scheme); err != nil {
        return fmt.Errorf("ensure owner ref on existing wave %s: %w", waveName, err)
    }
    if err := r.Update(ctx, &existing); err != nil {
        return fmt.Errorf("update owner ref on wave %s: %w", waveName, err)
    }
}
```

### WR-03: `ComputeWaves` runs twice per reconcile despite the assemble-once refactor

**File:** `internal/controller/project_controller.go:1806` (gate) and `1649` (derive)
**Confidence:** High

The Reconcile comment (lines 258-261) credits the refactor with avoiding double work, and it does dedupe the four `List` calls in `assembleProjectDepGraph`. But `checkGlobalCycleGate` calls `dag.ComputeWaves(nodes, edges)` (line 1806) and discards the result, then `deriveGlobalWaves` calls `dag.ComputeWaves(nodes, edges)` again (line 1649) on the identical `(nodes, edges)`. The whole point of "assemble once" is undercut: the layering is computed twice every reconcile. Since `ComputeWaves` is the only thing that can return `CycleError`, and `deriveGlobalWaves` already handles a `ComputeWaves` error defensively (lines 1650-1655), the cycle gate could consume the derive's single computation, or the two could be merged. (Performance per se is out of v1 scope, but this is a correctness-adjacent redundancy: two computations can in principle diverge if anything between them mutated state, and the gate's discard-the-result pattern is the smell.)

**Fix:** Compute waves once in Reconcile (or in the gate) and thread the `[][]NodeID` result into `deriveGlobalWaves`, removing the second `ComputeWaves` call. The gate would return `(blocked, waves, result, err)` or Reconcile would call `ComputeWaves` directly and pass `waves` to both consumers.

### WR-04: Global derivation lists Tasks by label only — directly-applied/unlabeled Tasks are silently excluded

**File:** `internal/controller/project_controller.go:1459-1465`, `1724-1730`
**Confidence:** Medium

`assembleProjectDepGraph` lists Tasks exclusively via `client.MatchingLabels{owner.LabelProject: project.Name}` (lines 1460-1462). The removed per-plan path listed Tasks via the field index `taskPlanRefIndexKey` (see `reconcileWaveMaterialization`, plan_controller.go:1024-1026), which does **not** depend on the project label. Reporter-materialized Tasks do carry the label (`internal/reporter/materialize.go:285-287`), so the normal flow is covered — but any Task applied directly without the label (test fixtures, manual `kubectl apply`, a chaos/resume fixture, or a Task whose label backfill has not yet run) is invisible to the global engine: it gets no wave-index, no edge contribution, and won't be rolled up by any Wave. The result is a partial/incorrect global schedule with no error surfaced. This is a behavior regression versus the per-plan path's index-based listing.

**Fix:** Either list Tasks by the existing field index (namespace-scoped) and filter by resolved project membership, or document and enforce (admission default) that every Task carries `owner.LabelProject` before it can participate. At minimum, log a warning when a namespace contains Tasks lacking the project label so the gap is observable.

### WR-05: `taskToWaveMapper` returns nil (drops the event) when labels are stale/absent — Wave roll-up can stall

**File:** `internal/controller/wave_controller.go:232-247`
**Confidence:** Medium

`taskToWaveMapper` derives the Wave name `tide-wave-<project>-<waveIndex>` purely from the Task's `owner.LabelProject` and `wave-index` labels, returning `nil` if either is empty (lines 240-242). The wave-index label is stamped asynchronously by `ProjectReconciler.stampGlobalTaskLabels` after global derivation, and re-derivation can re-number a Task's wave (a dependency edit shifts indices). Between a Task status change and the next successful stamp, this mapper either (a) drops the event entirely (no label yet) so the Wave never re-rolls-up for that status change, or (b) maps to the **old** wave name if the index changed but the label is stale, enqueueing the wrong Wave. Because this is the only Task→Wave trigger (no periodic resync besides the 10h default), a dropped event can leave a Wave's status lagging until the next unrelated reconcile. The O(1) optimization trades correctness for the assumption that labels are always present and current.

**Fix:** Make the mapper resilient: if the wave-index label is absent, fall back to enqueueing all Waves for the Task's project (List by project + Wave name prefix), or have `ProjectReconciler` own the Task→Wave fan-out where it already holds the authoritative wave assignment. Add a low-frequency `RequeueAfter` safety resync on Waves in `Running` to self-heal dropped events.

## Info

### IN-01: Reconcile runs the full global engine even before the dispatcher is wired

**File:** `internal/controller/project_controller.go:252-274`
**Confidence:** High

The global cycle gate + wave derivation block runs at step 4a, **before** the `if r.Dispatcher != nil` seam (line 277) and before the init/clone/budget lifecycle. In non-dispatcher (unit-test) mode the engine still lists Plans/Phases/Milestones/Tasks and (when Tasks exist) creates Wave CRs. Functionally harmless today (zero Tasks → empty `globalWaves` → no-op), but it couples Wave creation to a code path that predates dispatch readiness; a future change that lets Tasks exist pre-dispatch would create Waves out of lifecycle order. Consider gating the derivation behind the same readiness check, or document why it intentionally precedes it.

### IN-02: Re-List of Tasks in `deriveGlobalWaves` duplicates the assembler's listing

**File:** `internal/controller/project_controller.go:1724-1730`
**Confidence:** High

The comment at lines 1722-1723 says "The taskList is re-used from the assembler's listing" but the code immediately issues a fresh `r.List` for the same project-labeled Tasks. The assembler's `taskList` is not actually threaded through. Pass the assembler's Task slice into `deriveGlobalWaves` (alongside nodes/edges) to honor the comment and drop one List per reconcile.

### IN-03: `WaveReconciler.reconcileObservational` Step 2 secondary filter is now redundant

**File:** `internal/controller/wave_controller.go:154-162`
**Confidence:** Medium

The Step-2 loop re-filters `taskList.Items` by the exact `wave-index` label that the List selector (lines 144-149) already matched exactly. The comment hedges "in case the label index is not exact-match only," but `client.MatchingLabels` is always exact equality, so this pass can never drop a member. Dead-but-harmless defensive code; remove it or convert to an assertion to avoid implying the List is fuzzy.

### IN-04: Two stale doc comments reference an interim per-plan state that Phase 24 removed

**File:** `internal/controller/wave_controller.go:139-143`
**Confidence:** Medium

The `reconcileObservational` comment still says the project filter is an "interim WR-01 fix … until the Phase-24 global wave index lands (see the TODO above)" — but Phase 24 has now landed and there is no "TODO above." Similarly, `taskToWaveMapper`'s contract is correct but the surrounding narrative predates the global index. These stale comments will mislead the next reader about which scheme is authoritative. Refresh them to state the global index is now the live contract.

---

_Reviewed: 2026-06-16_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
