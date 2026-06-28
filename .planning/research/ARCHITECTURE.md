# Architecture Research: v1.0.6 Adoption-Path Correctness & Dispatch Safety

**Domain:** TIDE controller corrective patch — four defects on the import/adoption path (D1–D4)
**Researched:** 2026-06-28
**Confidence:** HIGH (all findings from direct code reading at HEAD; verified line numbers cited)

---

## Context

This document maps each of D1–D4 from `run-2b-FINDINGS.md` to the exact files and functions
that must change. All existing infrastructure is already in place (global Execution DAG, layered
Kahn, shared dispatch helpers, reporter Jobs, CRD-`.status`-only). This is a corrective patch
on those seams, not new architecture.

---

## D2 + D1: The Single Lifecycle Seam

### What the defect is

**D2**: After `ImportComplete=True`, `project.Status.Phase` stays `Initialized`. The adoption guard
returns early without advancing the Project to `Running`. Anything keyed off `Running` — including
the budget gate's halt-on-cap logic — never observes the project as actively spending.

**D1**: `Project.Status.Budget.CostSpentCents` (and `TokensSpent`) stays at zero during the
adoption planning cascade. The metered `budget.absoluteCapCents` gate cannot halt because
spend is never tallied. The run spent blind; only the node OOM stopped it.

D1 and D2 share one root: the adoption guard in `reconcileProjectPlannerDispatch` returns before
the line that sets `PhaseRunning`.

### Exact seam

**File:** `internal/controller/project_controller.go`
**Function:** `reconcileProjectPlannerDispatch`

The normal non-adoption path (line 1203–1215) does:

```go
// Step 10: Patch Status.Phase=Running + Condition AuthoringPlanner=True.
patch := client.MergeFrom(project.DeepCopy())
project.Status.Phase = tidev1alpha2.PhaseRunning
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:   tidev1alpha2.ConditionAuthoringPlanner,
    ...
})
if err := r.Status().Patch(ctx, project, patch); err != nil { ... }
```

The adoption guard (lines 1105–1133, Phase 30 RESUME-PARTIAL-02) returns before that:

```go
if metav1.IsControlledBy(&msList.Items[i], project) {
    logf.FromContext(ctx).V(1).Info("import adopted; skipping project planner dispatch", ...)
    return ctrl.Result{}, nil   // <-- returns here; PhaseRunning never set
}
```

**Fix for D2**: Before the `return ctrl.Result{}, nil` in the adoption guard, add a status
patch that sets `project.Status.Phase = tidev1alpha2.PhaseRunning`. This is a narrow
one-line addition at a single site, idempotent (Initialized → Running is a no-op if already
Running), and does not affect any other adoption guard invariant.

**Fix for D1**: The `budget.RollUpUsage` calls at the phase and plan completion seams already
exist and already reference the correct project object. They were not firing during the run
because the node OOM killed jobs before they completed. However, D1's headline claim
("budget.absoluteCapCents cannot enforce") also requires that the budget gate halts on each
ProjectReconciler reconcile. The `handleBudgetGate` in `reconcileProjectPhase2` already does
this — but it checks `IsCapExceeded` against `CostSpentCents`, which is zero when jobs are
killed before completing. The fix for D1 is therefore primarily ensuring that:

1. The project phase advances to `Running` (D2 fix), which correctly represents the lifecycle
   state and ensures the `handleBudgetGate` logic sees the correct phase for bypass/halt
   decisions (phase=`Initialized` passes through the budget gate today — it only halts on
   `PhaseBudgetExceeded`).
2. No additional skip path silently suppresses rollup for adopted plans/phases: confirmed
   absent. The `if project.Spec.ImportSource != nil { ... skip rollup ... }` exists only in
   `handleProjectJobCompletion` at line 1306 (project-level planner rollup — correct, because
   the project planner doesn't run under adoption). Phase and plan `handleJobCompletion`/
   `handlePlannerJobCompletion` have NO import-source skip — they call `budget.RollUpUsage`
   normally when `isFirstCompletion && envReadOK && project != nil`.

The D2 fix (advancing to `Running`) is the necessary condition for D1's budget gate to fire
correctly. No additional D1-specific code change is needed beyond D2's patch — the rollup
infrastructure is correct; the lifecycle state is wrong.

### Component boundary

| What changes | File | Function | Change type |
|---|---|---|---|
| Advance Phase=Running under adoption | `internal/controller/project_controller.go` | `reconcileProjectPlannerDispatch` | MODIFIED (add status patch before adoption-guard return) |
| Budget gate fires on Running | `internal/controller/project_controller.go` | `handleBudgetGate` (called from `reconcileProjectPhase2`) | UNCHANGED (already correct once Phase=Running) |
| Phase/plan rollup | `internal/controller/phase_controller.go`, `plan_controller.go` | `handleJobCompletion`, `handlePlannerJobCompletion` | UNCHANGED (already call `budget.RollUpUsage`) |

### Existing code that must NOT change

- The existing project-level rollup skip at `handleProjectJobCompletion` line 1306
  (`if project.Spec.ImportSource != nil { skip rollup }`) is **correct**: the project-level
  planner did not run under adoption, so there is no planner cost to roll up at the project
  level. Do not remove it.
- The Phase 30 adoption guard itself (the `metav1.IsControlledBy` check) stays intact. The
  fix is additive: a status patch BEFORE the `return`, not a removal of the guard.

---

## D3: Dispatch Concurrency Cap

### What the defect is

~60 subagent pods dispatched simultaneously (15 phase planners + 44 plan planners in parallel
from independent wave derivation). Single-node kind OOM'd. The pool semaphore exists but does
not limit in-flight running Jobs — it only serializes Job creation calls.

### Why the pool semaphore doesn't help today

**File:** `internal/pool/pool.go`

The `Pool.Acquire` / `Pool.Release` semaphore is held only during the `buildJobSpec + Create`
window — `defer r.PlannerPool.Release()` fires when `reconcilePlannerDispatch` (a helper)
returns, immediately after the Job is created. The pool is therefore a creation-rate limiter,
not an in-flight-Job limiter. After the `Create` call returns, the slot is freed and another
dispatch can proceed immediately, even though the just-created Job is still running.

**Evidence:** `values.yaml` has `plannerConcurrency: 16`. With 59 independent plan/phase
objects triggering dispatch within the same reconcile burst, 59 Job creation calls succeed
in rapid sequence (each holding a pool slot for ~milliseconds during Create), resulting in
59 simultaneously running pods.

**Confirmed pool wiring (`cmd/manager/main.go` lines 343–353, 445, 475, 501):**
- `plannerPool = pool.New(cfg.PlannerConcurrency, "planner")` — shared across Project, Milestone, Phase, Plan reconcilers
- `executorPool = pool.New(cfg.ExecutorConcurrency, "executor")` — shared across Wave, Task reconcilers
- `plannerPool.PreCharge(ctx, mgr.GetClient(), "tideproject.k8s/role=planner")` — counts in-flight at startup only

### Fix: in-flight Job count gate at each dispatch site

The fix is to count currently-running planner Jobs cluster-wide BEFORE acquiring the pool
slot (or instead of the pool slot), and park (requeue) if the in-flight count is at or above
the configured cap.

**Insertion points** (one per dispatch function, same position — BEFORE `PlannerPool.Acquire`):

| Level | File | Function | Lines (approx) |
|---|---|---|---|
| Milestone | `internal/controller/milestone_controller.go` | `reconcilePlannerDispatch` | before line 381 |
| Phase | `internal/controller/phase_controller.go` | `reconcilePlannerDispatch` | before line 379 |
| Plan | `internal/controller/plan_controller.go` | `reconcilePlannerDispatch` | before line 384 |
| Project | `internal/controller/project_controller.go` | `reconcileProjectPlannerDispatch` | before line 1135 |

The gate: count Jobs with label `tideproject.k8s/role=planner` (already stamped by
`podjob.BuildJobSpec`) and `Status.Active > 0`. If `count >= cfg.PlannerConcurrency`, return
`ctrl.Result{RequeueAfter: 5 * time.Second}, nil` (park, not fail). This reuses the label
selector already used by `pool.PreCharge` at startup.

**No change to the executor pool** — task executor dispatch is already gated by the Wave
controller's wave-boundary failure semantics and the ExecutorPool semaphore, which is
correctlysized. Wave-boundary failure contract is unaffected: failed tasks continue to let
siblings in the same wave proceed; the cap only throttles cross-wave dispatch entry.

**Why not change the pool semantics instead:** Changing `Pool` to hold the slot until Job
completion would require wiring a Job-completion signal back to the pool, which introduces
a goroutine leak risk and adds cross-reconciler state. The in-flight-count check is a simple
`client.List` with a label selector, consistent with the PreCharge pattern already in the
codebase. It also composites cleanly with the existing `PlannerPool.Acquire` — the count
check parks before Acquire if the cluster is saturated, so the pool slot is only acquired
when there is actually room.

**The per-level cap is shared (one plannerConcurrency applies to all levels combined),
preserving the spec's "size planner and executor pools separately" constraint**: planner levels
(project/milestone/phase/plan) share one cap; executor levels (wave/task) share another.
The pools are not unified. The new count check operates on the planner label selector, which
covers all planner-kind Jobs regardless of level.

**Default value change required:** `plannerConcurrency: 16` is too high for a single-node
kind cluster. A sane single-node default is `plannerConcurrency: 3` (one per level type
simultaneously). This is a `values.yaml` change only — no controller code change to defaults.
Operators who know they have a multi-node cluster can increase it via `--set plannerConcurrency=N`.

### Component boundary

| What changes | File | Change type |
|---|---|---|
| In-flight count gate | `milestone_controller.go` `phase_controller.go` `plan_controller.go` `project_controller.go` | MODIFIED (add count-check before pool acquire at 4 dispatch sites) |
| `values.yaml` default | `charts/tide/values.yaml` | MODIFIED (plannerConcurrency: 16 → 3) |
| `Pool` itself | `internal/pool/pool.go` | UNCHANGED |
| `ExecutorPool` wiring | `wave_controller.go`, `task_controller.go` | UNCHANGED |
| Wave-boundary failure semantics | `depgraph.go`, `task_controller.go` | UNCHANGED |

---

## D4: Planner Failure Semantics (Phase + Milestone)

### What the defect is

A phase was marked `Succeeded` ("Plan children materialized") when its planner exited 1 with
`childCount 0` and produced no plans. The `expected == 0` branch at
`phase_controller.go:handleJobCompletion` line 593 leads unconditionally to
`patchPhaseSucceeded` without checking whether the planner actually succeeded.

The same pattern exists in the milestone controller.

### Exact seam — Phase controller

**File:** `internal/controller/phase_controller.go`
**Function:** `handleJobCompletion` (line 467)

The buggy branch (lines 590–597):

```go
if envReadOK {
    expected := out.ChildCount
    if expected == 0 {
        // Genuine leaf — planner authored no Plan children.
        logger.V(1).Info("boundary push skipped: planner authored no Plan children (leaf)", "phase", ph.Name)
        return r.patchPhaseSucceeded(ctx, ph)   // <-- fires even on exitCode != 0
    }
    ...
```

**Fix:** Add an `out.ExitCode != 0` check before succeeding when `expected == 0`:

```go
if envReadOK {
    expected := out.ChildCount
    if expected == 0 {
        if out.ExitCode != 0 {
            // Planner failed (exitCode != 0) and produced no children.
            // Must not succeed the parent — fail it instead.
            return r.patchPhaseFailed(ctx, ph, "PlannerFailed",
                fmt.Sprintf("phase planner exited %d with childCount 0", out.ExitCode))
        }
        // Genuine leaf (planner succeeded, authored no Plan children).
        logger.V(1).Info("boundary push skipped: planner authored no Plan children (leaf)", "phase", ph.Name)
        return r.patchPhaseSucceeded(ctx, ph)
    }
    ...
```

`patchPhaseFailed` must be verified or added. Check `phase_controller.go` for an existing
`patchPhaseFailed` function — if absent, add it mirroring `patchPlanFailed` in
`plan_controller.go` line 842.

### Exact seam — Milestone controller

**File:** `internal/controller/milestone_controller.go`
**Function:** `handleJobCompletion` (line 517)

Same pattern at lines 670–677:

```go
if envReadOK {
    expected := out.ChildCount
    if expected == 0 {
        // Genuine leaf — planner authored no Phase children.
        logger.V(1).Info("boundary push skipped: planner authored no Phase children (leaf)", "milestone", ms.Name)
        return r.patchMilestoneSucceeded(ctx, ms)   // <-- same bug
    }
    ...
```

Same fix: add `out.ExitCode != 0` check before succeeding.

### Plan controller — already correct (Phase 30 guard)

**File:** `internal/controller/plan_controller.go`
**Function:** `handlePlannerJobCompletion` (line 508)

The plan controller does NOT call `patchPlanSucceeded` in `handlePlannerJobCompletion`
when `expected == 0`. Instead, when `expected == 0`, it falls through to the boundary-push
trigger (line 725) and then clears the Running phase (line 730–738) — the plan-level
`patchPlanSucceeded` is called from `reconcileWaveMaterialization` (line 1168) which is
gated on `gates.BoundaryDetected(ctx, r.Client, plan, "Task")`, which requires at least
one Task to have Succeeded. So a plan with `childCount 0` and `exitCode != 0` that clears
Running will not get `Succeeded` stamped (no Tasks → `BoundaryDetected` returns false →
the Succeeded path is unreachable). The Phase-30 guard the findings reference is this
structural separation, not an explicit `exitCode` check. **Plan controller does not need
this fix.**

### Component boundary

| What changes | File | Function | Change type |
|---|---|---|---|
| `exitCode != 0` guard on childless success | `internal/controller/phase_controller.go` | `handleJobCompletion` | MODIFIED (add guard before `patchPhaseSucceeded` on `expected == 0`) |
| `exitCode != 0` guard on childless success | `internal/controller/milestone_controller.go` | `handleJobCompletion` | MODIFIED (same guard) |
| `patchPhaseFailed` helper | `internal/controller/phase_controller.go` | new function or existing | NEW or VERIFIED (mirror `patchPlanFailed` if absent) |
| `patchMilestoneFailed` helper | `internal/controller/milestone_controller.go` | new function or existing | NEW or VERIFIED (mirror `patchPlanFailed` if absent) |
| Plan controller | `internal/controller/plan_controller.go` | `handlePlannerJobCompletion` | UNCHANGED (already structurally correct) |
| Project controller | `internal/controller/project_controller.go` | `handleProjectJobCompletion` | UNCHANGED (project succeeds via `checkProjectComplete` → BoundaryDetected, not childCount==0) |

---

## Build Order

Dependencies between fixes:

```
D2 (advance PhaseRunning under adoption)
    └── D1 (budget rollup fires once lifecycle is correct)
            ← these are a single commit / phase

D3 (in-flight count gate)
    └── independent of D1/D2; can ship in parallel or before

D4 (childless-failed-planner guard at phase + milestone)
    └── independent of all others; can ship in parallel
```

**Recommended sequencing:**

```
Phase 1: D2 + D1 together (same seam)
  Files: internal/controller/project_controller.go
  Change: add Status.Phase=Running patch before adoption-guard return in
          reconcileProjectPlannerDispatch
  Tests: envtest that adopted project accrues CostSpentCents; envtest that
         project Phase advances to Running after ImportComplete=True with
         owned Milestones

Phase 2: D3 (dispatch concurrency cap)
  Files: internal/controller/milestone_controller.go
         internal/controller/phase_controller.go
         internal/controller/plan_controller.go
         internal/controller/project_controller.go
         charts/tide/values.yaml
  Change: in-flight count gate before PlannerPool.Acquire at 4 dispatch
          sites; default plannerConcurrency: 16 → 3
  Tests: envtest that >N concurrent plans requeue (park) instead of
         creating Jobs when cap is reached; unit test for count helper

Phase 3: D4 (planner failure semantics)
  Files: internal/controller/phase_controller.go
         internal/controller/milestone_controller.go
  Change: exitCode != 0 guard before patchPhaseSucceeded / patchMilestoneSucceeded
          in the expected==0 branch; add patchPhaseFailed/patchMilestoneFailed
          helpers if absent
  Tests: envtest that a phase/milestone planner exiting 1 with childCount 0
         marks the parent Failed (not Succeeded); mirrors the plan-level
         test shape from Phase 30
```

Phase 2 and Phase 3 have no dependency on each other and can be developed in parallel.
Phase 1 (D2+D1) is the highest-priority fix (it is the root of "spent blind") and should
land first.

---

## What Is New vs. Modified

| Component | Status | Notes |
|---|---|---|
| `reconcileProjectPlannerDispatch` (adoption guard) | MODIFIED | One status patch added before early return |
| In-flight count gate at 4 dispatch sites | MODIFIED | Addition before existing PlannerPool.Acquire |
| `values.yaml` plannerConcurrency default | MODIFIED | 16 → 3 |
| `handleJobCompletion` exitCode guard (phase, milestone) | MODIFIED | One guard added per file |
| `patchPhaseFailed` / `patchMilestoneFailed` | NEW (if absent) | Mirror `patchPlanFailed`; verify first |
| `Pool` struct and `Acquire`/`Release` | UNCHANGED | Existing semaphore preserved |
| `budget.RollUpUsage` | UNCHANGED | Already correct at phase/plan level |
| `handleBudgetGate` | UNCHANGED | Already correct once PhaseRunning is set |
| Wave-boundary failure contract | UNCHANGED | No change to depgraph, failure_halt, task dispatch |
| Executor pool and wave dispatch | UNCHANGED | D3 cap is planner-only |
| Plan controller `handlePlannerJobCompletion` | UNCHANGED | Already structurally guards childless success |
| `import_controller.go` | UNCHANGED | D1/D2 fix is in project_controller, not import |

---

## Constraints Honored

- **CRD-`.status`-only**: the D2 fix is a `Status().Patch` call, consistent with all other
  lifecycle transitions. No new persistence surface.
- **Resumability**: the `Phase=Running` patch survives controller restart. On the next
  reconcile the adoption guard re-fires, and the status patch is idempotent (Running → Running
  is a no-op via the `handleInitJobCompletion` cascade-13 guard, which skips re-stomping
  forward phases).
- **Separately-sized pools**: D3 adds a count check keyed on `tideproject.k8s/role=planner`
  label — planner-only. The executor pool (`role=executor`) is untouched. Spec constraint
  honored.
- **Wave-boundary failure contract**: D3's park-on-cap is a requeue, not a failure. No
  wave-boundary semantics are involved. Failed tasks → siblings continue as before.
- **Cycle detection**: none of the four fixes touch `pkg/dag`, `checkGlobalCycleGate`, or
  `assembleProjectDepGraph`. Cycle semantics are unaffected.
- **Configurable policy (not baked in)**: D3's cap comes from `plannerConcurrency` in
  `values.yaml` → `config.yaml` → `cfg.PlannerConcurrency`. No hardcoded value in the
  controller body.

---

## Sources

All findings from direct code reading at HEAD of `internal/controller/`:

- `project_controller.go`: `reconcileProjectPlannerDispatch` (lines 999–1218), `handleProjectJobCompletion` (lines 1233–1360), `reconcileProjectPhase2` (lines 313–378), `handleBudgetGate` (lines 1367–1464)
- `phase_controller.go`: `handleJobCompletion` (lines 467–648), `reconcilePlannerDispatch` (lines 379–384 pool acquire)
- `milestone_controller.go`: `handleJobCompletion` (lines 517–728), `reconcilePlannerDispatch` (lines 381–386 pool acquire)
- `plan_controller.go`: `handlePlannerJobCompletion` (lines 508–740), `reconcileWaveMaterialization` (lines 1035–1176), `reconcilePlannerDispatch` (lines 385–389 pool acquire)
- `import_controller.go`: `succeedImport` (lines 701–706) — sets ConditionImportComplete=True only, does NOT advance Project.Status.Phase
- `internal/pool/pool.go`: `Pool.Acquire`, `Pool.Release`, `Pool.PreCharge` (full file)
- `cmd/manager/main.go`: pool wiring (lines 343–353, 445, 475, 501, 529, 547)
- `charts/tide/values.yaml`: `plannerConcurrency: 16`, `executorConcurrency: 4` (lines 78–79)
- `internal/budget/tally.go`: `RollUpUsage` (lines 56–89)
- `.planning/dogfood/run-2b-FINDINGS.md`: defect definitions D1–D4 (authoritative)

---

*Architecture research for: TIDE v1.0.6 Adoption-Path Correctness & Dispatch Safety*
*Researched: 2026-06-28*
