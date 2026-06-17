# Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption — Research

**Researched:** 2026-06-16
**Domain:** Go + controller-runtime — extending TaskReconciler dispatch readiness to global scope; adding FailureProfile enum + ConditionFailureHalt; composing task-gate with global indegree; confirming restart resumption falls out of D-01; extending `tide resume --retry-failed` to clear the new halt.
**Confidence:** HIGH — all findings verified by direct codebase inspection.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** TaskReconciler re-derives its own global readiness each reconcile — nothing derived is persisted. Shared helper so dispatch and wave-derivation can never disagree.
- **D-02:** `Project.Spec.FailureProfile` enum `{strict|conservative}`, default `strict`.
- **D-02a:** Strict profile is free from the indegree model — falls out automatically.
- **D-02b:** Conservative profile = project-wide `ConditionFailureHalt` mirroring `BillingHalt`. First failed task stamps it; in-flight Jobs drain; cleared by `tide resume --retry-failed`.
- **D-03:** Milestone/Phase/Plan gates are planning-DAG holds only — execution cannot outrun approve-at-descent. Task gate is the sole execution hold, composes with global indegree (D-03a). Held task blocks dependents for free via indegree (D-03b).
- **D-04:** Resumption falls out of D-01 — Phase 25 adds a regression test, not new persistence.
- **Dispatch predicate:** `dispatch(task) iff globalIndegree(task)==0 AND task-gate approved AND NOT billingHalt AND NOT (conservative ∧ failureHalt)`.
- **Guards:** `verify-no-aggregates`, `verify-no-sqlite-dep`, `verify-dag-imports` must stay green.
- **Metrics:** locked `{project,phase,plan,wave}` label set unchanged; `task` label stays forbidden.
- `tide resume --retry-failed` is the one sanctioned recovery verb.

### Claude's Discretion

- **D-01 mechanic:** list-all-project-tasks-and-filter vs label-select-each-dep's-scope — pick the efficient one. Location of the shared fan-out resolver.
- **Watch/field-index wiring** so a completing/held Task re-enqueues its global dependents.
- **Condition/reason vocabulary** for `FailureHalt` — mirror `BillingHalt` vocab in `shared_types.go`.
- **FailureProfile** CEL enum markers/printer columns.
- Keeping all guards green.

### Deferred Ideas (OUT OF SCOPE)

- Multi-milestone drive via the Milestone DAG + cross-milestone shared waves + per-milestone gate policy + README conformance test (Phase 26, MS-01..03, SPEC-01).
- Per-scope (milestone/phase) conservative halt granularity (Phase 26 follow-up).
- Optional project-level "approve the fully-assembled global DAG before execution" boundary gate (declined by user for this phase).
- Direct-SDK cross-pod prompt caching (CACHE-F1) — Ebb Tide era, unrelated.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DISP-01 | A Task dispatches only when ALL its global dependencies are complete (global indegree 0 vs the completed-task set), regardless of authoring Plan/Phase/Milestone. | Replace `listSiblingTasks` (plan-local) with project-label list + `computeIndegree` widened to global. The `assembleProjectDepGraph` fan-out resolver in `project_controller.go` is the shared helper. |
| DISP-02 | Wave-boundary failure semantics hold EXACTLY at global scope — failed task → independent siblings continue; global dependents never dispatch; non-dependents dispatch in strict / halt in conservative. | Strict is free from indegree (failed dep never Succeeded → dependents never reach 0). Conservative adds `ConditionFailureHalt` mirroring `ConditionBillingHalt`. |
| DISP-03 | Gates compose with the global scheduler as holds — a gate withholds a globally-ready Task until approved; approval releases it without bypassing dependency readiness. | `checkReadinessGates` already composes task-gate with indegree; widening listSiblingTasks makes composition global. |
| RESUME-01 | An orchestrator restart re-derives the entire Project execution schedule from the global indegree map + completed-task set alone — no other persisted execution state. | Falls out of D-01 (re-derive each reconcile from scratch). `checkRunningState` re-adopts in-flight Jobs via deterministic `podjob.JobName`. Regression test is the only new artifact. |
</phase_requirements>

---

## Summary

Phase 25 retires the plan-local sibling watch in `TaskReconciler` and makes dispatch readiness, the wave-boundary failure contract, gate holds, and restart resumption operate over the global Execution DAG Phase 24 built. The implementation is narrow because the indegree model already does most of the heavy lifting.

**The critical surgical change** is `listSiblingTasks` (line 1182–1192, `task_controller.go`): it currently filters by `taskPlanRefIndexKey` (`spec.planRef`). Widening it to list all Tasks in the project namespace by `owner.LabelProject` (the same label `assembleProjectDepGraph` uses) makes `computeIndegree` (line 1198–1213) automatically produce global indegree with zero algorithmic change. The `computeIndegree` logic itself (`statusByName[dep] != "Succeeded"`) is correct for global scope — it already blocks on Failed and AwaitingApproval predecessors, so strict DISP-02 semantics and D-03b held-task blocking both fall out.

**The watch change is equally important**: `siblingsToTaskMapper` (line 1386–1414) today maps a changed Task to its plan-siblings. It must instead map to all Tasks in the same *project* that declare a `DependsOn` edge pointing at the changed Task — i.e., the global dependents. Without this change, a cross-plan task completing does not re-enqueue its dependents in other plans, and they stall until the next periodic resync.

**Conservative halt** is the only net-new mechanism: `ConditionFailureHalt` + `setFailureHaltIfNeeded` (new file or extension of `billing_halt.go`) + a gate in `gateChecks` after `checkBillingHalt`. `tide resume --retry-failed` already clears `BillingHalt` and resets Failed levels; it just needs to also clear `FailureHalt`.

**Resumption** falls out completely: `checkRunningState` already re-adopts Running Jobs via `podjob.JobName(task.UID, task.Status.Attempt)`. Halt conditions persist as Project `.status.conditions` (not cached schedule state). Phase 25 adds a regression test only.

**Primary recommendation:** Implement in three focused changes — (1) global `listSiblingTasks` + global sibling watch; (2) `FailureProfile` field + `ConditionFailureHalt` + `checkFailureHalt` gate; (3) `tide resume` extension + regression tests.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Global dispatch readiness (DISP-01) | TaskReconciler (controller) | ProjectReconciler (provides assembled fan-out resolver) | Per-task, per-reconcile; task is the sole authority for its own dispatch |
| Failure halt stamping (DISP-02 conservative) | TaskReconciler (on Job completion) | ProjectReconciler (reads halt in checkBillingHalt slot) | handleJobCompletion already classifies failure reason; same pattern as setBillingHaltIfNeeded |
| Failure halt check at dispatch (DISP-02) | All five dispatch sites (task/plan/phase/milestone/project reconcilers) | — | checkBillingHalt pattern: all five call it before dispatching |
| Gate-as-hold for task gate (DISP-03) | TaskReconciler.checkReadinessGates | — | Task gate already parks at AwaitingApproval in checkReadinessGates; indegree widening makes composition global |
| Restart resumption (RESUME-01) | TaskReconciler (re-derive) + checkRunningState | — | Re-derives from scratch each reconcile; no new persistence |
| Recovery verb | `tide resume --retry-failed` (CLI) | — | Clears BillingHalt today; extend to clear FailureHalt |
| Fan-out resolver (shared helper) | `ProjectReconciler.assembleProjectDepGraph` (existing) | — | Already exported/factored; TaskReconciler can call the same `tasksForScope` closure pattern OR the resolver logic moves to a shared package |

---

## Standard Stack

### Core (no new dependencies)

This phase adds no new external dependencies. All implementation is in existing packages.

| Package | Current Role | Phase 25 Extension |
|---------|-------------|---------------------|
| `internal/controller/task_controller.go` | Task dispatch gate ladder | Widen `listSiblingTasks` + `siblingsToTaskMapper` to global scope; add `checkFailureHalt` gate |
| `internal/controller/billing_halt.go` | BillingHalt pattern | Mirror with `failure_halt.go` (or extend file) for `ConditionFailureHalt` + `setFailureHaltIfNeeded` |
| `api/v1alpha2/project_types.go` | ProjectSpec | Add `FailureProfile FailureProfileType` field with CEL enum |
| `api/v1alpha2/shared_types.go` | Condition/reason vocabulary | Add `ConditionFailureHalt`, `ReasonTaskFailed`, `AnnotationFailureResumedAt` |
| `cmd/tide/resume.go` | `tide resume --retry-failed` | Clear `ConditionFailureHalt` in addition to `ConditionBillingHalt` |
| `internal/controller/project_controller.go` | assembleProjectDepGraph | Export or refactor `tasksForScope` into a shareable resolver so TaskReconciler and ProjectReconciler use identical fan-out logic |

### Package Legitimacy Audit

No new external packages are introduced. This section is not applicable.

---

## Architecture Patterns

### System Architecture Diagram

```
Task reconcile (per-task, per-event):
  gateChecks()
    ├─ terminal short-circuit (Succeeded/Failed → noop)
    ├─ Running → checkRunningState() → re-adopt Job via podjob.JobName(UID,Attempt)
    ├─ resolveProject()
    ├─ CheckRejected(project)
    ├─ checkParentApproval(PlanRef) [planning-DAG hold, D-03]
    ├─ checkBillingHalt(project) [existing gate, Phase 13]
    ├─ [NEW] checkFailureHalt(project) [conservative halt, D-02b]
    ├─ setBudgetBlockedIfNeeded / checkBudgetBlocked [Phase 14]
    └─ checkReadinessGates()
         ├─ listProjectTasks()           ← CHANGED: was listSiblingTasks(plan-local)
         ├─ computeIndegree(task, allProjectTasks)  ← unchanged algorithm, wider scope
         ├─ wave-paused label check
         └─ gates.EvaluatePolicy("task") → AwaitingApproval hold

Task completing (handleJobCompletion):
  → [NEW] setFailureHaltIfNeeded(project, reason)  [conservative profile only]

ProjectReconciler.assembleProjectDepGraph() [Phase 24, unchanged]
  → tasksForScope closure [SHARED: must produce same edges as TaskReconciler]

siblingsToTaskMapper() [CHANGED: was plan-local, must become global-dependent mapper]
  → list all Tasks in namespace with DependsOn pointing at changed Task
    OR list all project Tasks and filter (simpler, correct, same O(V) cost)
```

### Recommended Project Structure

No structural changes to directories. New file: `internal/controller/failure_halt.go` (mirrors `billing_halt.go`).

```
internal/controller/
├── task_controller.go          # listSiblingTasks → listProjectTasks; siblingsToTaskMapper → global; checkFailureHalt gate added
├── failure_halt.go             # NEW: checkFailureHalt, setFailureHaltIfNeeded, isFailureReason (mirrors billing_halt.go)
├── failure_halt_test.go        # NEW: unit tests mirroring billing_halt_test.go
├── billing_halt.go             # UNCHANGED
api/v1alpha2/
├── project_types.go            # FailureProfile enum + FailureProfileType added to ProjectSpec
├── shared_types.go             # ConditionFailureHalt, ReasonTaskFailed, AnnotationFailureResumedAt added
cmd/tide/
├── resume.go                   # clearFailureHalt block added alongside BillingHalt clear
test/integration/envtest/
├── global_dispatch_test.go     # NEW: DISP-01 global indegree, DISP-02 strict/conservative, DISP-03 task-gate, RESUME-01 restart
```

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Global dependency resolution | Custom edge scanner in TaskReconciler | Reuse `assembleProjectDepGraph`'s `tasksForScope` closure pattern | `tasksForScope` already handles Task/Plan/Phase/Milestone fan-out with de-dup; duplicating it introduces disagreement (D-01 forbids this) |
| Halt condition stamping | Inline condition set in reconcileDispatch | Extract `failure_halt.go` mirroring `billing_halt.go` exactly | BillingHalt pattern is already proven across five dispatch sites; mirrors stale-evidence fence (AnnotationFailureResumedAt) |
| Global dependent re-enqueue | New watcher/indexer from scratch | Extend existing `siblingsToTaskMapper` to project scope | The pattern (EnqueueRequestsFromMapFunc + label list) is already wired and working; widening scope is a 1-line filter change |
| Resumption state machine | Persisted schedule / restart handler | Nothing — falls out of idempotent per-reconcile re-derive (D-04) | `checkRunningState` + `podjob.JobName(UID, Attempt)` already re-adopt in-flight Jobs |

**Key insight:** Every mechanism in this phase already exists in the codebase — the work is widening scope (plan-local → project-global) and mirroring a proven pattern (BillingHalt → FailureHalt). No net-new algorithms.

---

## Code Locations — Verified

All line numbers confirmed by direct `grep` of the current tree. They may drift ±5–10 lines during editing but the function boundaries are stable.

### `internal/controller/task_controller.go`

| Symbol | Approx Line | Current Behavior | Phase 25 Change |
|--------|-------------|-----------------|-----------------|
| `taskPlanRefIndexKey` | 65 | `".spec.planRef"` — field index for plan-local sibling list | Keep: still useful for `checkParentApproval`; add `taskProjectLabelKey` for global list |
| `gateChecks()` | 303 | Steps 1–5: terminal, Running, project, reject, parent-approval, billing-halt, budget, indegree | Add `checkFailureHalt(project)` at Step 3.5 — AFTER `checkBillingHalt` (line 367), BEFORE `setBudgetBlockedIfNeeded` (line 379) |
| `checkReadinessGates()` | 423 | Calls `listSiblingTasks` (plan-local) → `computeIndegree` → wave-pause → gate-policy | Replace `listSiblingTasks` call with `listProjectTasks` (project-label list); `computeIndegree` algorithm unchanged |
| `checkRunningState()` | 483 | `podjob.JobName(task.UID, task.Status.Attempt)` re-adoption | Unchanged — RESUME-01 already works |
| `listSiblingTasks()` | 1182 | `MatchingFields{taskPlanRefIndexKey: task.Spec.PlanRef}` — plan-local | **Replace** with `listProjectTasks()` using `MatchingLabels{owner.LabelProject: projectName}`. Keep `listSiblingTasks` only if needed by other callers (check: it is also used by `siblingsToTaskMapper`) |
| `computeIndegree()` | 1198 | `statusByName[dep] != "Succeeded"` — algorithm correct for global scope | **Unchanged** — works correctly once `siblings` becomes all-project-tasks |
| `siblingsToTaskMapper()` | 1386 | `MatchingFields{taskPlanRefIndexKey: task.Spec.PlanRef}` — maps changed Task to plan-siblings | **Replace**: map changed Task to all Tasks in its project that have the changed Task's name in their `DependsOn`. Simplest implementation: list all project Tasks by label and filter for those with `changedTaskName` in `Spec.DependsOn` |
| `SetupWithManager()` | 1504 | Registers `taskPlanRefIndexKey` field indexer + `Owns(Job)` + `Watches(Task, siblingsToTaskMapper)` | No structural changes needed IF `siblingsToTaskMapper` is replaced by label-list pattern (no new field indexer required for global dependent re-enqueue) |

### `internal/controller/billing_halt.go` → mirror as `failure_halt.go`

| Symbol | Mirrors | Phase 25 New Symbol |
|--------|---------|---------------------|
| `checkBillingHalt(project)` | → | `checkFailureHalt(project *v1alpha2.Project) bool` — reads `ConditionFailureHalt==True` |
| `setBillingHaltIfNeeded(ctx, c, project, reason, jobStart)` | → | `setFailureHaltIfNeeded(ctx, c, project *v1alpha2.Project) error` — stamps `ConditionFailureHalt` on FIRST failure under conservative profile. Simpler than billing: no stale-evidence time fence needed (failure halt is sticky until retry-failed; no "post-resume failure" ambiguity exists) |
| `AnnotationBillingResumedAt` | → | `AnnotationFailureResumedAt = "tideproject.k8s/failure-resumed-at"` — stamped by `tide resume --retry-failed` when clearing FailureHalt (mirrors billing pattern; lets `setFailureHaltIfNeeded` fence against pre-resume failures if desired — but simpler approach: since `--retry-failed` also resets Task phases, re-triggering halt is intentional not accidental, so the time fence may be omitted) |
| `isBillingFailureReason(reason)` | → | Not needed — conservative halt fires on ANY task failure, not a specific reason |

### `api/v1alpha2/project_types.go`

`ProjectSpec` (line 300) currently ends at `Git *GitConfig`. Add:

```go
// FailureProfile controls how a task failure affects non-dependent work.
// strict (default): non-dependent tasks in later waves continue dispatching.
// conservative: first failure halts all new dispatch project-wide until
// `tide resume --retry-failed` is run.
// +kubebuilder:validation:Enum=strict;conservative
// +kubebuilder:default=strict
// +optional
FailureProfile FailureProfileType `json:"failureProfile,omitempty"`
```

And the type (in `shared_types.go` or `project_types.go`):

```go
// FailureProfileType is the failure-propagation policy for this Project.
// +kubebuilder:validation:Enum=strict;conservative
type FailureProfileType string

const (
    FailureProfileStrict       FailureProfileType = "strict"
    FailureProfileConservative FailureProfileType = "conservative"
)
```

The `+kubebuilder:default=strict` marker means omitted field → `strict` in the CRD schema (validated by CEL). This keeps existing Projects (which have no `failureProfile` field) on the strict path without migration.

**`verify-no-aggregates` impact:** None. `FailureProfile` is a plain string enum — not `Schedule`, `Waves[]`, `IndegreeMap`, or `CachedDag`. The guard grep (`grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag'`) does not match.

### `api/v1alpha2/shared_types.go`

Add a Phase 25 block after the Phase 14 block (line ~235):

```go
// Phase 25 condition + reason vocabulary — task failure halt (DISP-02 conservative).
const (
    // ConditionFailureHalt — a task failed under conservative FailureProfile;
    // new dispatch is halted project-wide until the operator runs
    // `tide resume --retry-failed`. Set by TaskReconciler on Job failure under
    // conservative profile; read by all five dispatch sites; cleared by tide resume.
    ConditionFailureHalt = "FailureHalt"

    // ReasonTaskFailedHalt — a member task failed and the Project's
    // FailureProfile is conservative; halt is set project-wide.
    ReasonTaskFailedHalt = "TaskFailedHalt"

    // AnnotationFailureResumedAt — RFC3339 timestamp stamped by `tide resume --retry-failed`
    // when clearing the FailureHalt condition. Mirrors AnnotationBillingResumedAt.
    // Optional: only needed if the reconciler gates re-stamping FailureHalt against
    // this timestamp (simpler to omit if retry-failed also resets Task phases).
    AnnotationFailureResumedAt = "tideproject.k8s/failure-resumed-at"
)
```

### `cmd/tide/resume.go`

`resumeRun()` currently clears `BillingHalt` at line 94–125. Add a parallel block immediately after:

```go
// Phase 25 D-04: clear FailureHalt unconditionally when retry-failed (operator
// chose recovery by invoking resume --retry-failed; the subsequent retryFailedLevels
// call resets Failed Tasks so they re-dispatch, which may re-trip the halt only on
// genuine new failures).
if retryFailed {
    if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
        return fmt.Errorf("re-get project for FailureHalt clear: %w", err)
    }
    haltCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha2.ConditionFailureHalt)
    if haltCond != nil && haltCond.Status == metav1.ConditionTrue {
        patch3 := client.MergeFrom(proj.DeepCopy())
        meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha2.ConditionFailureHalt)
        if err := c.Status().Patch(ctx, &proj, patch3); err != nil {
            return fmt.Errorf("patch status (clear FailureHalt): %w", err)
        }
        if out != nil {
            fmt.Fprintln(out, "tide: cleared FailureHalt; re-dispatch will resume after retry-failed reset")
        }
    }
}
```

Note: `FailureHalt` is only cleared when `--retry-failed` is set (not on bare `tide resume`), because the halt only fires on task failures and is only meaningful in conjunction with resetting those failed tasks.

---

## Watch / Field-Index Wiring

### Current state (VERIFIED)

`SetupWithManager` (line 1504–1554) registers:
1. A **field indexer** on `.spec.planRef` (`taskPlanRefIndexKey`), used by:
   - `listSiblingTasks` — the plan-local list
   - `siblingsToTaskMapper` — the plan-local watch mapper
2. `Owns(&batchv1.Job{})` — re-enqueues Task when its Job changes.
3. `Watches(&Task{}, siblingsToTaskMapper)` — re-enqueues plan-siblings when a sibling changes (drives FAIL-02 today).
4. A second `Watches(&Task{}, annotationOnly)` — re-enqueues a Task on annotation changes (approvals).

### Required change for DISP-01

**Replace the `siblingsToTaskMapper` watch** (or replace the mapper body) so that when Task T transitions to `Succeeded`, `Failed`, or `AwaitingApproval`, all Tasks in the *same project* that have T's name in their `Spec.DependsOn` are re-enqueued.

**Two valid implementations:**

**Option A: List-all-project-tasks-and-filter (recommended — O(V), simple)**

```go
// globalDependentsMapper re-enqueues all Tasks in the same project whose
// DependsOn contains the name of the changed Task. This drives DISP-01:
// when a global predecessor completes or fails, its dependents re-evaluate readiness.
func (r *TaskReconciler) globalDependentsMapper(ctx context.Context, obj client.Object) []reconcile.Request {
    task, ok := obj.(*tideprojectv1alpha2.Task)
    if !ok { return nil }
    projectName := task.Labels[owner.LabelProject]
    if projectName == "" { return nil }

    var all tideprojectv1alpha2.TaskList
    if err := r.List(ctx, &all,
        client.InNamespace(task.Namespace),
        client.MatchingLabels{owner.LabelProject: projectName},
    ); err != nil { return nil }

    reqs := make([]reconcile.Request, 0)
    for _, t := range all.Items {
        if t.UID == task.UID { continue }
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

**Option B: Per-dependency-scoped resolution (O(deps×scope), complex)**

For each dep name in the completed task's `DependsOn`, resolve it to a scope and list Tasks in that scope. This is O(deps × scope_size) and requires knowing the edge direction *backwards* — but the `DependsOn` is declared on the *dependent*, not the *dependency*. To find dependents of a changed Task you'd need a reverse index. Option A already gives you the dependents by scanning forward edges on all Tasks. Option B is not simpler for this direction.

**Verdict: Option A.** Same O(V) cost as `assembleProjectDepGraph`'s initial task list, no new field indexer required, correct for global scope.

**The old `siblingsToTaskMapper` can be renamed `globalDependentsMapper` and its body replaced.** The field indexer on `.spec.planRef` (`taskPlanRefIndexKey`) is still needed by `checkParentApproval` (line 354), so it stays registered.

---

## Failure Profile Dispatch Gate Ladder

The current `gateChecks` order (verified at line 303–414) after Phase 25:

```
Step 1: terminal short-circuit (Succeeded/Failed → halt)
Step 2: Running → checkRunningState
Step 3: resolveProject
Step 3.1: CheckRejected(project)
Step 3.2: checkParentApproval(PlanRef, "Plan")  [planning-DAG hold]
Step 3.3: checkBillingHalt(project)             [Phase 13, UNCHANGED]
Step 3.4: [NEW] checkFailureHalt(project)       [Phase 25 conservative only]
Step 3.5: setBudgetBlockedIfNeeded / checkBudgetBlocked [Phase 14, UNCHANGED]
Step 3.6: reservation headroom check            [Phase 14, UNCHANGED]
Step 5: checkReadinessGates
   → listProjectTasks (CHANGED: was listSiblingTasks)
   → computeIndegree (UNCHANGED algorithm)
   → wave-pause label
   → gates.EvaluatePolicy("task") [DISP-03, UNCHANGED wiring]
```

`checkFailureHalt` placement rationale: AFTER `checkBillingHalt` (consistent ordering — both are project-wide safety holds); BEFORE budget/headroom (a halted project should not waste headroom checks). No per-Task condition stamp (same as BillingHalt — operator signal is the single Project condition).

**The same `checkFailureHalt` must be added to all four other dispatch sites:**
- `milestone_controller.go` line ~347 — after `checkBillingHalt`
- `phase_controller.go` line ~345 — after `checkBillingHalt`
- `plan_controller.go` line ~342 — after `checkBillingHalt`
- `project_controller.go` line ~1000 — after `checkBillingHalt`

---

## `setFailureHaltIfNeeded` — Where It Fires

`setBillingHaltIfNeeded` is called from `handleJobCompletion` in both `task_controller.go` (line 907–915) and `project_controller.go` (lines 1187–1188, for the project-level planner Jobs).

For `FailureHalt` (conservative profile):
- It fires from `handleJobCompletion` in `task_controller.go` — task execution failures.
- Project-level planner Job failures are planning failures, not execution failures; `FailureHalt` should only fire on **task execution** failures (a planning failure should set the planning object's `Failed` phase, not halt execution dispatch).
- Implementation: `setFailureHaltIfNeeded` checks `project.Spec.FailureProfile == FailureProfileConservative` AND `ConditionFailureHalt` is not already `True` before stamping (idempotent, avoids patch churn on multiple concurrent failures).

```go
func setFailureHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha2.Project) error {
    if project == nil { return nil }
    if project.Spec.FailureProfile != FailureProfileConservative { return nil } // strict: no-op
    // Already halted: no-op (idempotent).
    for _, cond := range project.Status.Conditions {
        if cond.Type == ConditionFailureHalt && cond.Status == metav1.ConditionTrue { return nil }
    }
    patch := client.MergeFrom(project.DeepCopy())
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:    ConditionFailureHalt,
        Status:  metav1.ConditionTrue,
        Reason:  ReasonTaskFailedHalt,
        Message: "A task failed under conservative FailureProfile. New dispatch halted. " +
                 "Run `tide resume --retry-failed` after addressing the failure.",
        LastTransitionTime: metav1.Now(),
    })
    return c.Status().Patch(ctx, project, patch)
}
```

No time fence needed (unlike `setBillingHaltIfNeeded`): `--retry-failed` resets Failed Tasks, which can re-fail post-resume — that is correct behavior (new failures after resume should re-halt). The fence would prevent this.

---

## Common Pitfalls

### Pitfall 1: Double-indegree from the sibling watch firing on the changed task itself
**What goes wrong:** `globalDependentsMapper` maps ALL project Tasks; if it includes the changed Task itself, it double-enqueues it.
**Why it happens:** The `if t.UID == task.UID { continue }` guard was present in the original `siblingsToTaskMapper` and must carry over.
**How to avoid:** Keep the `UID != task.UID` guard in `globalDependentsMapper`.
**Warning signs:** Tasks re-reconcile twice on every Job completion.

### Pitfall 2: listProjectTasks lists Tasks from other Projects in the namespace
**What goes wrong:** With `MatchingLabels{owner.LabelProject: projectName}`, Tasks not yet project-labeled (WR-04 from Phase 24 warning) are excluded — that is correct. But if `projectName` is empty (label not yet stamped), a label selector with an empty value may match all unlabeled Tasks.
**Why it happens:** `resolveProject()` is called before `listProjectTasks`; if project resolution succeeded, `projectName` is non-empty. But the project name must be threaded to `listProjectTasks`.
**How to avoid:** Pass `project.Name` explicitly to `listProjectTasks`; assert it is non-empty before issuing the List.

### Pitfall 3: FailureHalt fires for strict profile
**What goes wrong:** `setFailureHaltIfNeeded` is called unconditionally in `handleJobCompletion`, halting strict-profile projects.
**Why it happens:** Missing the `FailureProfile == conservative` guard.
**How to avoid:** First check `project.Spec.FailureProfile != FailureProfileConservative` and return nil early.

### Pitfall 4: verify-no-aggregates trips on `FailureProfileType`
**What goes wrong:** A type named `FailureProfileType` might look suspect to future guard changes.
**Why it happens:** The current guard greps for `Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag`. A plain string type does not match.
**How to avoid:** Confirmed safe — `FailureProfile` is a string-typed enum field, not an aggregate. No guard update needed.

### Pitfall 5: Wave prune deletes in-flight Waves (Phase 24 OQ-3, inherited debt)
**What goes wrong:** `deriveGlobalWaves` prunes stale Wave CRs (index >= len(globalWaves)); if a task fails, DAG re-derivation may reduce wave count, deleting a Wave that still has Running tasks.
**Why it happens:** The prune at line 1751 in `project_controller.go` is unconditional (a Phase 24 open question).
**How to avoid:** Phase 25 should gate prune on `Wave.Status.Phase == "Succeeded"`. The CONTEXT.md calls this out as inherited debt from Phase 24. Add a check: `if w.Status.Phase != "Succeeded" { skip delete }`.

### Pitfall 6: `tide resume --retry-failed` clears FailureHalt on base (non-`--retry-failed`) mode
**What goes wrong:** Bare `tide resume` (used to clear reject annotations) accidentally clears FailureHalt.
**Why it happens:** FailureHalt clear is added outside the `if !retryFailed { return nil }` guard.
**How to avoid:** FailureHalt clear MUST be inside the `retryFailed` branch. Billing halt is cleared on bare resume (intentional — it's a billing recovery). FailureHalt requires `--retry-failed` (the task failures need resetting too).

### Pitfall 7: `computeIndegree` uses task NAME as key but project-label list returns cross-plan tasks
**What goes wrong:** If two Tasks in different Plans have the same `Name`, `statusByName` collides.
**Why it happens:** Task names are namespace-unique (K8s enforces this) — two Tasks cannot share a name in the same namespace. This is not actually a pitfall, but worth confirming.
**Confirmation:** [VERIFIED: K8s metadata.name is unique per namespace per GVK]. Task `DependsOn` references task names which are namespace-unique. `computeIndegree`'s map-by-name is correct.

---

## Resumption (RESUME-01)

Resumption falls out of D-01 with no new code in the reconciler. Verified from code:

1. **Completed-task set:** `computeIndegree` checks `statusByName[dep] != "Succeeded"`. Task CRDs survive in etcd (controller-runtime does not delete them). On restart, every Task's `Status.Phase` is re-read from etcd — the completed set is always current. [VERIFIED: `computeIndegree` at line 1198 reads `.Status.Phase` from the in-memory `siblings` list]

2. **In-flight Jobs:** `checkRunningState` (line 483) calls `r.Get` for `podjob.JobName(task.UID, task.Status.Attempt)`. Job name is deterministic from `task.UID` and `task.Status.Attempt` — survives any restart. A Running Task on restart re-enters `checkRunningState`, finds the Job, and either re-adopts or completes. [VERIFIED: `checkRunningState` at line 483]

3. **Halt conditions:** `ConditionBillingHalt` and new `ConditionFailureHalt` live as `Project.Status.Conditions` entries in etcd — not derived schedule state. They survive restart and are read at the start of every `gateChecks`. [VERIFIED: `checkBillingHalt` reads `project.Status.Conditions` at line 82 of `billing_halt.go`]

4. **Global indegree map:** Re-derived from `listProjectTasks()` every reconcile — no cached form exists (PERSIST-03 / `verify-no-aggregates` green). [VERIFIED: no `IndegreeMap` type in `api/v1alpha2/`]

**RESUME-01 regression test shape:** Create a project with Tasks A → B → C (cross-plan). Run A to Succeeded, B to Succeeded. Simulate operator restart (controller-runtime envtest manager restart / new reconciler instance). After restart, assert C dispatches (global indegree 0 after A and B succeeded). Assert no new persistence mechanism was introduced.

---

## Shared Fan-Out Resolver — Package Boundary Decision

**Problem:** `TaskReconciler.computeIndegree` needs to know, for each `dep` name in `task.Spec.DependsOn`, the resolved set of Task names — because `dep` may be a coarse ref (a Plan, Phase, or Milestone name) that fan-outs to multiple Tasks (per Phase 23 D-02 + Phase 24 D-04). If `computeIndegree` treats every `dep` as a Task name, it misses cross-scope coarse refs and under-counts indegree.

**Current state in Phase 24:** `assembleProjectDepGraph` (project_controller.go line 1470) already resolves coarse refs via `tasksForScope` (an in-memory closure). `TaskReconciler.computeIndegree` (task_controller.go line 1198) checks `statusByName[dep] != "Succeeded"` — treating `dep` as a Task name directly. This was acceptable before Phase 24 (all deps were plan-local task names). After Phase 24, `DependsOn` may carry Plan/Phase/Milestone names.

**The gap:** If `task.Spec.DependsOn = ["plan-B"]`, `computeIndegree` looks for `statusByName["plan-B"]` — which does not exist in `statusByName` (a Task-name map). It returns `indegree++` unconditionally, which means the Task is permanently blocked (never dispatches). This is a correctness bug for coarse-ref dependsOn.

**Resolution options:**

| Option | Complexity | Risk |
|--------|-----------|------|
| A (recommended): Factor `tasksForScope` out of `assembleProjectDepGraph` into `internal/controller/depgraph.go` (package-private shared helper); call it from both ProjectReconciler and a new `computeGlobalIndegree` in TaskReconciler | Low: copy the closure body to a new helper, replace in-place | None — same logic, shared location |
| B: ProjectReconciler pre-stamps a resolved-dep label on each Task | Medium: derived persisted signal, violates PERSIST-03 | HIGH — exactly what D-01 rejects |
| C: TaskReconciler calls `assembleProjectDepGraph` itself | Low: duplicates the full graph assembly per reconcile | Inefficient but not incorrect; adds all the List calls to every task reconcile |

**Recommendation: Option A** — extract `tasksForScope` to a new `internal/controller/depgraph.go` file (or inline in `task_controller.go` as a helper that accepts the same resolution maps). The resolution maps (tasksByPlan, planToPhase, phaseToMS) must be built from the current Task/Plan/Phase/Milestone lists. `computeGlobalIndegree` builds those maps (same Lists as `assembleProjectDepGraph` step 2–3) and then resolves each dep through `tasksForScope`. This adds 3–4 List calls per Task reconcile (Plan, Phase, Milestone lists) — acceptable, same as ProjectReconciler does.

**Alternative simpler path:** If all current `DependsOn` entries in the codebase after Phase 23/24 are Task-level names only (coarse refs were used in Phase 23 schema but may not be authored in practice yet), the coarse-ref gap is latent. The planner can note this as a correctness gap that needs the `depgraph.go` helper, but prioritize the plan-local→global widening first and address coarse-ref resolution in a follow-on task within Phase 25.

**Complexity tradeoff (per CONTEXT.md discretion note):** list-all-and-filter is O(V+E) per task reconcile — same as the project reconciler's wave derivation. Per-dependency-scoped resolution is O(deps × scope_size) — better when deps are few and scopes are large, but requires building the scope→task maps per dep, which costs the same Lists. For the typical case (task has 1–5 deps), list-all-and-filter is simpler and correct.

---

## Guards — Confirmed Safe

| Guard | Grep Pattern | Phase 25 Impact | Safe? |
|-------|-------------|-----------------|-------|
| `verify-no-aggregates` | `Schedule\|Waves *\[\]\|IndegreeMap\|CachedDag\|DerivedDag` in `api/v1alpha*/*_types.go` | `FailureProfile FailureProfileType` and `ConditionFailureHalt` string const — neither matches | YES |
| `verify-no-sqlite-dep` | DB drivers in `go.mod` | No new dependencies | YES |
| `verify-dag-imports` | `k8s.io/\|sigs.k8s.io/\|anthropics/` in `pkg/dag/...` | `pkg/dag` unchanged | YES |
| `verify-dispatch-imports` | forbidden imports in `pkg/dispatch/...` | `pkg/dispatch` unchanged | YES |
| Metric cardinality analyzer | `task` label forbidden in `internal/metrics/registry.go` | No new metrics | YES |
| `+kubebuilder:default=strict` on `FailureProfile` | CRD generation | Default ensures no migration needed for existing Projects | YES |

[VERIFIED: `verify-no-aggregates` Makefile grep at line 529 confirmed]

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2 + Gomega (envtest) |
| Config file | `test/integration/envtest/suite_test.go` (existing `BeforeSuite`) |
| Quick run command | `go test ./internal/controller/... -run TestFailureHalt -v` (unit) |
| Full envtest command | `go test ./test/integration/envtest/... -v --label-filter=phase25` |
| Integration suite command | `make test-int` (full suite — runs envtest + kind) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DISP-01 | Task with cross-plan DependsOn dispatches only after global predecessor Succeeds | envtest integration | `go test ./test/integration/envtest/... -run "DISP-01"` | ❌ Wave 0 |
| DISP-01 | Task with plan-local DependsOn still works (regression) | envtest integration | `go test ./test/integration/envtest/... -run "DISP-01.*plan-local"` | ❌ Wave 0 |
| DISP-02 (strict) | Failed Task A → independent Task C in later wave dispatches | unit + envtest | `go test ./internal/controller/... -run "TestStrictProfile"` | ❌ Wave 0 |
| DISP-02 (strict) | Failed Task A → dependent Task B never dispatches | unit | `go test ./internal/controller/... -run "TestIndegreeBlocksDependent"` | ❌ Wave 0 |
| DISP-02 (conservative) | Failed Task A → ConditionFailureHalt stamped on Project | unit | `go test ./internal/controller/... -run "TestSetFailureHaltIfNeeded"` | ❌ Wave 0 |
| DISP-02 (conservative) | ConditionFailureHalt → all new dispatch blocked (including non-dependents) | envtest | `go test ./test/integration/envtest/... -run "DISP-02.*conservative"` | ❌ Wave 0 |
| DISP-02 | `checkFailureHalt` returns false for strict profile project | unit | `go test ./internal/controller/... -run "TestCheckFailureHalt"` | ❌ Wave 0 |
| DISP-03 | Task gate `approve` holds globally-ready task; non-dependents keep flowing | envtest | `go test ./test/integration/envtest/... -run "DISP-03"` | ❌ Wave 0 (existing `TestGateApproveFlow` covers plan-local case; global variant needed) |
| RESUME-01 | After simulated restart, Task C dispatches if A and B are Succeeded in etcd | envtest | `go test ./test/integration/envtest/... -run "RESUME-01.*global"` | ❌ Wave 0 |
| RESUME-01 | Running task re-adopted via `podjob.JobName(UID, Attempt)` on restart | unit | `go test ./internal/controller/... -run "TestCheckRunningStateReAdopt"` | Existing `checkRunningState` tests cover this; verify global variant |
| DISP-02 | `tide resume --retry-failed` clears FailureHalt | unit (`resume_test.go`) | `go test ./cmd/tide/... -run "TestResumeRunClearsFailureHalt"` | ❌ Wave 0 |

### Existing Tests That Must Not Regress

| Test | Location | Risk from Phase 25 change |
|------|----------|---------------------------|
| `RESUME-01 — retry-failed status reset re-dispatches Failed Plan` | `gates_test.go` line 462 | Low — tests `retryFailedLevels`; no interaction with new FailureHalt |
| `TestGateApproveFlow` | `gates_test.go` line 254 | Medium — tests task gate; verify it still works with globalDependentsMapper |
| `TestNoChildJobsWhileParentAwaiting` | `gates_test.go` line 60 | Low — tests plan-gate descent hold; unaffected |
| `TestWavePauseBetweenWaves` | `gates_test.go` line 605 | Medium — uses wave-paused label; unaffected by global indegree change but verify |
| All `billing_halt*` tests | `billing_halt_test.go` | Low — unchanged file; new `failure_halt.go` is separate |
| `planner_dispatch_test.go` | Full file | Low — tests planner Jobs not task dispatch |

### Sampling Rate

- **Per task commit:** `go test ./internal/controller/... -run "TestFailureHalt|TestCheckFailureHalt|TestSetFailureHalt|TestGlobalIndegree" -v`
- **Per wave merge:** `go test ./internal/controller/... ./cmd/tide/... -v`
- **Phase gate:** `make test-int` full suite green before `/gsd:verify-work`

### Wave 0 Gaps (test files to create before implementation)

- [ ] `internal/controller/failure_halt_test.go` — unit tests for `checkFailureHalt`, `setFailureHaltIfNeeded`, covering: strict profile no-op, conservative stamps halt, idempotent second call, nil project safe
- [ ] `test/integration/envtest/global_dispatch_test.go` — Ginkgo suite covering DISP-01 (cross-plan dep), DISP-02 strict, DISP-02 conservative, DISP-03 task-gate hold, RESUME-01 restart
- [ ] `cmd/tide/resume_failure_test.go` or extend `cmd/tide/resume_test.go` — TestResumeRunClearsFailureHalt

---

## Environment Availability

Phase 25 is code/config changes only — no new external tools or services required. All existing infrastructure (envtest, kind, Go toolchain) is unchanged.

Step 2.6: SKIPPED — no new external dependencies.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Coarse-ref DependsOn entries (Plan/Phase names) are not yet authored in production test fixtures after Phase 23/24 — task `DependsOn` entries are still task-name strings in all current tests. | Shared Fan-Out Resolver | If wrong: `computeIndegree` would silently under-count indegree for coarse refs, causing premature dispatch. Must be verified before closing Phase 25. |
| A2 | `AnnotationFailureResumedAt` time fence is optional (retry-failed resets Task phases, so re-halt is intentional). | `setFailureHaltIfNeeded` | If wrong: a stale pre-resume failed task could re-trip halt on the same evidence after resume. Conservative call: add the fence anyway (mirrors billing exactly). |

---

## Open Questions

1. **Coarse-ref resolution in `computeIndegree`**
   - What we know: `assembleProjectDepGraph` resolves coarse refs via `tasksForScope` closure; `computeIndegree` treats every `dep` string as a Task name.
   - What's unclear: Whether any current test fixtures or authored `DependsOn` entries use Plan/Phase/Milestone names (vs Task names). If not, the bug is latent but untriggered.
   - Recommendation: Planner should add a task in Wave 0 to grep all Task.Spec.DependsOn across the test fixtures and confirm whether coarse refs are present. If present, factor `tasksForScope` into `depgraph.go` in the same wave. If absent, add a TODO comment in `computeIndegree` and schedule as a follow-on task.

2. **Wave prune guard (inherited from Phase 24 OQ-3)**
   - What we know: `deriveGlobalWaves` prunes Waves with `WaveIndex >= len(globalWaves)` unconditionally (line 1751, `project_controller.go`). A task failure that reduces the DAG could delete a still-Running Wave.
   - What's unclear: Whether a task failure actually causes re-derivation to produce fewer waves (it doesn't change the authored DAG, only Status.Phase — so the indegree model doesn't change the wave count). The prune may be safe in practice.
   - Recommendation: Planner should add the `Wave.Status.Phase != "Succeeded"` guard to the prune block as a defensive measure; this is a ~3-line change noted in Phase 24 commentary.

3. **`ProjectReconciler` dispatch site check for `FailureHalt`**
   - What we know: `checkBillingHalt` is called in `ProjectReconciler` (line ~1000) before dispatching project-level planner Jobs. `FailureHalt` should also be checked there.
   - What's unclear: Whether a conservative halt should block *planning* (authoring milestones/phases/plans) or only *execution* (task dispatch). The spec says "non-dependents dispatch in strict / halt in conservative" — this refers to execution. Planning dispatch (ProjectReconciler dispatching the milestone planner) is a different DAG.
   - Recommendation: Conservative halt should gate *task* dispatch only. The ProjectReconciler's planner-dispatch site should NOT check `FailureHalt`. This keeps conservative halt scoped to the execution layer, matching the spec's description of "non-dependents dispatch/halt" (planning is not a wave-boundary concept).

---

## Sources

### Primary (HIGH confidence — verified by direct code inspection)

- `/Users/justinsearles/Projects/tide/internal/controller/task_controller.go` — `listSiblingTasks` (1182), `computeIndegree` (1198), `gateChecks` (303), `checkReadinessGates` (423), `checkRunningState` (483), `siblingsToTaskMapper` (1386), `SetupWithManager` (1504), `taskPlanRefIndexKey` (65) [VERIFIED: current tree]
- `/Users/justinsearles/Projects/tide/internal/controller/billing_halt.go` — full `BillingHalt` pattern (checkBillingHalt, setBillingHaltIfNeeded, isBillingFailureReason) [VERIFIED: current tree]
- `/Users/justinsearles/Projects/tide/api/v1alpha2/project_types.go` — `ProjectSpec` shape (line 300–377); `Gates` struct (line 52–64) [VERIFIED: current tree]
- `/Users/justinsearles/Projects/tide/api/v1alpha2/shared_types.go` — all condition/reason constants; `ConditionBillingHalt` (205), `AnnotationBillingResumedAt` (216) [VERIFIED: current tree]
- `/Users/justinsearles/Projects/tide/cmd/tide/resume.go` — `resumeRun` (70), BillingHalt clear block (94–125), `retryFailedLevels` (137–252) [VERIFIED: current tree]
- `/Users/justinsearles/Projects/tide/internal/controller/project_controller.go` — `assembleProjectDepGraph` (1470–1667), `deriveGlobalWaves` (1688–1774), `checkGlobalCycleGate` (1841–1889), `taskToProject` mapper (1896), `SetupWithManager` (1909–1942) [VERIFIED: current tree]
- `/Users/justinsearles/Projects/tide/internal/controller/wave_controller.go` — `reconcileObservational` (129), `taskToWaveMapper` (240), Phase-25 NOTE at line 220 [VERIFIED: current tree]
- `/Users/justinsearles/Projects/tide/Makefile` — `verify-no-aggregates` (526–535), `verify-dag-imports` (472–481) [VERIFIED: current tree]
- `README.md` §"Failure handling at wave boundaries" (line 241–246) — spec contract [VERIFIED: current tree]
- `.planning/phases/25-global-dispatch-failure-semantics-gates-resumption/25-CONTEXT.md` — locked D-01..D-04, discretion areas [VERIFIED: read directly]

### Secondary (MEDIUM confidence)

- Five `checkBillingHalt` dispatch sites confirmed: `task_controller.go:367`, `project_controller.go:1000`, `plan_controller.go:342`, `phase_controller.go:345`, `milestone_controller.go:347` [VERIFIED: grep]

---

## Metadata

**Confidence breakdown:**
- Code locations and signatures: HIGH — verified by direct file read and grep
- Implementation approach (D-01 widening): HIGH — pattern is established by assembleProjectDepGraph
- Vocabulary/constants: HIGH — mirrors BillingHalt pattern exactly
- Coarse-ref resolution gap (A1): MEDIUM — assumption about test fixture state; needs one grep to confirm
- Wave prune guard: MEDIUM — analysis says it may be safe in practice, but guard should be added defensively

**Research date:** 2026-06-16
**Valid until:** 2026-07-16 (stable Go/controller-runtime codebase; no fast-moving dependencies)
