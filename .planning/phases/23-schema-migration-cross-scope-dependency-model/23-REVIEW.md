---
phase: 23-schema-migration-cross-scope-dependency-model
reviewed: 2026-06-16T00:00:00Z
depth: standard
files_reviewed: 13
files_reviewed_list:
  - api/v1alpha2/wave_types.go
  - api/v1alpha2/task_types.go
  - api/v1alpha2/plan_types.go
  - api/v1alpha2/project_types.go
  - api/v1alpha2/shared_types.go
  - internal/controller/project_controller.go
  - internal/controller/wave_controller.go
  - internal/controller/plan_controller.go
  - internal/webhook/v1alpha2/plan_webhook.go
  - internal/webhook/v1alpha2/wave_webhook.go
  - internal/webhook/v1alpha2/project_webhook.go
  - internal/webhook/v1alpha2/file_touch_utils.go
  - internal/metrics/registry.go
findings:
  critical: 0
  warning: 4
  info: 5
  total: 9
status: issues_found
---

# Phase 23: Code Review Report

**Reviewed:** 2026-06-16
**Depth:** standard
**Files Reviewed:** 13
**Status:** issues_found

## Summary

Reviewed the genuinely-new/reshaped logic of the v1alpha1→v1alpha2 breaking CRD migration: the
two new Project-reconciler guards (`checkSchemaRevisionGuard`, `checkGlobalCycleGate` +
`assembleProjectDepGraph`), the Wave re-ownership stubs in `wave_controller.go`/`plan_controller.go`,
the cross-scope-filtering plan webhook, the `SchemaRevision` discriminator + broadened `dependsOn`
fields, and the metrics label set.

Core doctrine holds: no cached schedule anywhere (waves discarded after `ComputeWaves`, `WaveIndex`
is a spec field not a status aggregate), the metric label set stays `{project,phase,plan,wave}` with
no `task` label, `values.yaml` does not appear, and CEL is used for structural validation while cycle
detection stays controller/webhook-side. The schema-revision guard correctly fail-closes with
`reconcile.TerminalError` (no requeue storm), and the empty/single-task graph is handled without
false positives (`ComputeWaves` returns `([], nil)` for an empty graph).

No BLOCKER-class defects found. The findings center on two themes: (1) the Phase-24 Wave stubs are
NOT the "safe no-ops" the brief assumes — `reconcileObservational` actively mis-aggregates Wave status
across plans in a shared namespace; and (2) the global cycle gate's documented self-healing
("a plan edit can remove the cycle; the reconciler requeues") does not actually fire, because the
Project reconciler has no Task watch. Both are observability/UX defects, not correctness-of-dispatch
defects, hence WARNING. Self-reference rejection on `dependsOn` is NOT enforced by CEL as the brief
implied — it falls through to the cycle detector (acceptable but worth noting).

## Warnings

### WR-01: Wave status mis-aggregates across plans sharing a wave-index in one namespace

**File:** `internal/controller/wave_controller.go:142-193`
**Issue:** `reconcileObservational` lists member Tasks by the single label
`tideproject.k8s/wave-index=<N>` scoped only to the namespace — with NO project or plan filter.
`wave-index` is a small integer (`0, 1, 2, …`) reused by `stampTaskLabels` for every plan
(`plan_controller.go:1436`, `materializeWaves` keys Waves on `plan.UID` but indexes restart at 0
per plan). In any namespace running more than one Plan (the normal multi-milestone case this whole
phase exists to enable), a Wave for plan A index 0 will aggregate Tasks from plan B index 0, C
index 0, etc. The resulting `Wave.Status.Phase` / `TaskRefs` are wrong. The review brief characterizes
this stub as a "SAFE no-op"; it is not a no-op — it computes and persists an incorrect status.
Severity is WARNING (not BLOCKER) only because nothing gates dispatch on `Wave.Status` (verified:
no reader of Wave status in task/plan/milestone dispatch gates), so this is observability corruption,
not mis-dispatch.
**Fix:** Until the Phase-24 global index lands, scope the member listing by BOTH the project and the
owning plan so per-plan index reuse cannot collide. The Wave already carries the owner Plan ref via
its owner-reference; add it as a label match:
```go
// Resolve owning plan from the Wave's controller owner-ref (set by materializeWaves),
// then filter members by plan as well as wave-index.
ownerPlan := metav1.GetControllerOf(wave) // the Plan that created this Wave (per-plan stub)
sel := client.MatchingLabels{"tideproject.k8s/wave-index": waveIndexLabel}
if ownerPlan != nil {
    sel["tideproject.k8s/plan"] = ownerPlan.Name // requires stampTaskLabels to also stamp plan
}
if err := r.List(ctx, &taskList, client.InNamespace(wave.Namespace), sel); err != nil { ... }
```
(Or stamp a composite `tideproject.k8s/wave=<plan.UID>-<index>` label and match on that single key.)

### WR-02: Global cycle gate's documented recovery never fires — Project does not watch Tasks

**File:** `internal/controller/project_controller.go:1492-1527` and `1532-1557`
**Issue:** `checkGlobalCycleGate` sets a sticky `CycleDetected` condition and returns `blocked=true`,
with the comment "NOT a TerminalError — a plan edit can remove the cycle; allow requeue"
(line 1520) and the doc comment "the operator can fix the cycle by editing DependsOn on the relevant
Tasks and the reconciler will requeue on changes" (line 1487). But `SetupWithManager`
(lines 1544-1556) wires only `For(&Project{})` + `Owns(&Job{})` + `Owns(&Milestone{})`. There is no
`Watches(&Task{})` and no `Owns(&Task{})`. Editing a Task's `DependsOn` to break the cycle therefore
does NOT re-enqueue the Project, so the stale `CycleDetected` condition is never re-evaluated until
something unrelated touches the Project (generation/annotation change) or owned Milestone/Job status
flips. The self-healing the comments promise is inoperative for the exact remediation they describe.
**Fix:** Add a Task→Project watch so a `DependsOn` edit re-triggers the gate:
```go
return ctrl.NewControllerManagedBy(mgr).
    For(&tidev1alpha2.Project{}, builder.WithPredicates(predicate.Or(
        predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}))).
    Owns(&batchv1.Job{}).
    Owns(&tidev1alpha2.Milestone{}).
    Watches(&tidev1alpha2.Task{}, handler.EnqueueRequestsFromMapFunc(r.taskToProjectMapper)).
    // taskToProjectMapper maps a Task → its Project via the tideproject.k8s/project label
    ...
```
Alternatively, if a Task watch is deemed too broad for Phase 23, correct the misleading comments to
state that recovery requires re-touching the Project (so operators are not told to edit a Task and
then watch a condition that will never clear).

### WR-03: Dual import alias maps two distinct-looking names to the same package (type-identity trap)

**File:** `internal/controller/wave_controller.go:36-37`
**Issue:**
```go
tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
```
Both aliases resolve to the *same* `v1alpha2` package. `wave_controller.go` then mixes them —
e.g. `tideprojectv1alpha1.TaskList` (line 147) and `tideprojectv1alpha2.WaveList` (line 243) are the
identical type. This compiles (verified `go vet` clean) but is a maintainability landmine: a future
reader will reasonably assume `tideprojectv1alpha1.Task` is a *different* type than
`tideprojectv1alpha2.Task` and may "fix" a perceived cross-version conversion that does not exist, or
re-introduce a real v1alpha1 import under the wrong alias. The comment at line 79 ("Fetch v1alpha2.Wave")
shows the author already knows the v1alpha1 alias is fictitious. This same fictitious-alias pattern
also appears in `plan_webhook.go` callers via the package and is worth a sweep.
**Fix:** Drop the `tideprojectv1alpha1` alias entirely and use one alias (`tidev1alpha2`) for all
`v1alpha2` references in the file:
```go
import tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
// ...then s/tideprojectv1alpha1\.\|tideprojectv1alpha2\./tidev1alpha2./g across the file.
```

### WR-04: `dependsOn` self-reference is not rejected at admission — relies on cycle detector

**File:** `api/v1alpha2/task_types.go:86-87`, `api/v1alpha2/plan_types.go:29-38`
**Issue:** The only CEL rule on `Task.Spec.DependsOn` is `!self.exists(d, d == '')`
(empty-string rejection). There is no self-reference rule. The review brief states the design intent
as "CEL validation on dependsOn (empty-string / self-reference rejection)" — the self-reference half
is absent. A Task whose `dependsOn` contains its own name produces a self-edge `{From: name, To: name}`
in `assembleProjectDepGraph`/`tasksToDAGWithinPlan`; `ComputeWaves` does not special-case a self-loop
(`kahn.go:55-64` only rejects *unknown* nodes), so `indegree[name]` becomes 1 and never resolves,
surfacing as a `CycleError` involving that single node. Functionally the bad input is caught, but at
the wrong layer and with a confusing diagnostic ("cyclic DAG involving [taskX]" for a self-dep) and
only after the Task reconciles far enough to enter cycle detection — not synchronously at admission.
`Plan.Spec.DependsOn` has NO CEL rule at all (not even the empty-string guard), so a Plan can be
admitted with `dependsOn: [""]`.
**Fix:** Add the self-reference and (for Plan) empty-string CEL rules so bad input is rejected
synchronously with a precise message. Self-reference needs the object name, which CEL on the field
cannot see directly; enforce it controller-side in `assembleProjectDepGraph`/`tasksToDAGWithinPlan`
with an explicit `dep == t.Name → reject with "task may not depend on itself"` check, and add the
empty-string rule to `PlanSpec.DependsOn`:
```go
// plan_types.go
// +kubebuilder:validation:XValidation:rule="!self.exists(d, d == '')",message="dependsOn entries must not be empty strings"
DependsOn []string `json:"dependsOn,omitempty"`
```
```go
// assembleProjectDepGraph / tasksToDAGWithinPlan
if dep == t.Name { /* surface a distinct self-dependency error, not a generic cycle */ }
```

## Info

### IN-01: Redundant second Get of the same GVK with a misleading comment

**File:** `internal/controller/project_controller.go:251-261`
**Issue:** `project` is already fetched at step 1 (line 215) as `tidev1alpha2.Project`. Step 4a then
does a *second* `r.Get(ctx, req.NamespacedName, &v2project)` into another `tidev1alpha2.Project` and
runs the guards against `v2project`. The comment (lines 246-250) frames this as a v1alpha1-vs-v1alpha2
GVK distinction ("the object exists under v1alpha2 GVK … running against a pre-migration cluster or
envtest with v1alpha1-only scheme"), but both variables are the *same* Go type and same GVK — the
second Get cannot fetch a different version. It is a redundant API round-trip whose only effect is to
re-read the object the reconciler already holds. The guards could run directly against `project`.
**Fix:** Run the guards against the already-fetched `project` and delete the second Get + misleading
comment:
```go
if blocked, result, gErr := r.checkSchemaRevisionGuard(ctx, &project); blocked { return result, gErr }
if blocked, result, gErr := r.checkGlobalCycleGate(ctx, &project); blocked { return result, gErr }
```

### IN-02: Global cycle gate under-detects until Tasks are project-labeled

**File:** `internal/controller/project_controller.go:1442-1452`
**Issue:** `assembleProjectDepGraph` lists Tasks via `MatchingLabels{owner.LabelProject: project.Name}`.
The `tideproject.k8s/project` label is stamped on Tasks by `stampTaskLabels`
(`plan_controller.go:1447-1448`) only AFTER wave materialization. Tasks that exist but are not yet
project-labeled are excluded from the graph, so a cross-plan cycle can go undetected until labels land.
This is conservative (under-detection, never a false positive) and the per-plan webhook still catches
within-plan cycles, so it is Info-level — but worth a note since the gate's completeness depends on a
label that is applied asynchronously.
**Fix:** None required for correctness; optionally document that the global gate is eventually-complete
(re-runs as labels land) rather than synchronously-complete, or assemble the graph via owner-ref walk
instead of the label fast-path for completeness.

### IN-03: Typo'd `dependsOn` entries are silently dropped, indistinguishable from coarse refs

**File:** `internal/controller/project_controller.go:1464-1477`, `internal/webhook/v1alpha2/plan_webhook.go:191-204`
**Issue:** Both edge assemblers skip any `dependsOn` entry that does not match a known node
("coarse scope ref to be fanned out in Phase 24"). A genuine authoring typo (a dep that names neither
a task nor a real Plan/Phase/Milestone) is dropped identically to a legitimate coarse ref, with no
warning. The dropped dep silently removes an intended ordering constraint. This is the documented
conservative tradeoff (the code comments call it out), so Info — but it means a fat-fingered dep name
produces a silently-wrong execution order rather than an error.
**Fix:** Phase 24 fan-out will need a scope-name resolver; when it lands, emit a warning Event for any
`dependsOn` entry that matches neither a task nor a resolvable scope node so typos surface instead of
vanishing.

### IN-04: `materializeWaves` swallows the owner-ref update error on the existing-Wave branch

**File:** `internal/controller/plan_controller.go:1406-1409`
**Issue:**
```go
if err := owner.EnsureOwnerRef(&existing, plan, r.Scheme); err == nil {
    _ = r.Update(ctx, &existing) // error discarded
}
```
The `r.Update` error is discarded with `_ =`. If the owner-ref back-fill fails (conflict, transient API
error), the Wave is left without the Plan owner ref and `materializeWaves` reports success. Garbage
collection of the Wave on Plan deletion then silently does not happen for that Wave. Low impact (the
common path sets the owner ref at create time), hence Info.
**Fix:** Log or propagate the update error:
```go
if err := owner.EnsureOwnerRef(&existing, plan, r.Scheme); err == nil {
    if uErr := r.Update(ctx, &existing); uErr != nil {
        logger.V(1).Info("failed to back-fill wave owner ref", "wave", existing.Name, "err", uErr)
    }
}
```

### IN-05: `WaveSpec.WaveIndex` carries a stale per-plan value under the Phase-23 stub

**File:** `internal/controller/plan_controller.go:1373-1376`, `api/v1alpha2/wave_types.go:31-37`
**Issue:** `WaveSpec.WaveIndex` is documented as "the global monotonic 0-indexed wave position derived
by pkg/dag.ComputeWaves over the entire Project's task DAG." Under the Phase-23 stub, `materializeWaves`
writes the *per-plan* layer index `i` (restarting at 0 for every plan), not a global index. This is an
intentional, documented stub (`TODO(phase-24)` at lines 1365-1366), and Phase 24 owns the global
derivation — so Info, not a defect. But any code that reads `WaveIndex` as already-global before
Phase 24 lands would be wrong; the field's contract and its current value diverge.
**Fix:** None for Phase 23. When Phase 24 wires the global assembler, ensure no consumer treats
`WaveIndex` as global before that point (the current `wave-index` *label* is also per-plan and feeds
WR-01).

---

_Reviewed: 2026-06-16_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
