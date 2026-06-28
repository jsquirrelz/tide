# Stack Research — TIDE v1.0.6 Adoption-Path Correctness & Dispatch Safety

**Domain:** Corrective patch to a shipping Go/Kubernetes operator (controller-runtime) — four code-level defects on the import/adoption path
**Researched:** 2026-06-28
**Confidence:** HIGH — all findings derived from the live codebase (read directly), go.mod, the Go stdlib, and the existing pool/dispatch infrastructure. No new external libraries proposed.

This file covers ONLY the stack questions specific to v1.0.6. The full prior stack (Go 1.26, controller-runtime v0.24.x, kubebuilder v4.14.0, Ginkgo v2.28, Gomega, Prometheus client_golang, OTel, etc.) is validated and unchanged from prior milestones.

---

## Context — The Four Defects

Dogfood run #2b validated the v1.0.5 import-resume path but HALTED on single-node OOM after ~60 concurrent planner pods, while also exposing three additional correctness issues:

- **D1** — `Project.status.costSpentCents` and `usage` block stayed empty while 44 plans were authored. The metered `budget.absoluteCapCents` gate cannot enforce if spend is never tallied. Root: the planner→reporter→Project usage rollup is tied to the normal project lifecycle, which the adoption path bypasses (see D2).
- **D2** — After `ImportComplete=True` the Project stayed `phase: Initialized` even as the phase→plan cascade ran. The adoption path correctly suppresses the project-planner but never advances the Project to `Running`/planning, so anything keyed off project phase (including D1's cost rollup) never fires.
- **D3** — ~60 subagent pods dispatched at once (15 phase + 44 plan planners). The pool infrastructure exists and is fully wired, but the chart defaults (`plannerConcurrency: 16`, `executorConcurrency: 4`) are too high for a single-node kind cluster. No mechanism counted in-flight Jobs at the planner level before dispatch in the adoption-path cascade.
- **D4** — A phase was marked `Succeeded` when its planner exited 1 with `childCount 0` and produced no plans. The `expected == 0` arm in `PhaseReconciler.handleJobCompletion` and `MilestoneReconciler.handleJobCompletion` fires `patchPhaseSucceeded`/`patchMilestoneSucceeded` regardless of whether `out.ExitCode != 0`, treating a failed planner as a genuine leaf. Phase 30 added a childless-success guard for plans; phase and milestone lack the equivalent exit-code check before the leaf path.

---

## D1 — Cost Rollup Under Adoption: No New Dependency

### Root cause (verified in code)

`budget.RollUpUsage` is called inside `handleJobCompletion` in `milestone_controller.go:587` and `phase_controller.go:518`. That function is entered only when a planner Job completes. Under the adoption path the project-planner Job is suppressed (correct — Phase 30 guard at `project_controller.go:1088`), and the Project lifecycle stalls at `Initialized` (D2), so `reconcileProjectPlannerDispatch` never returns a `Running` project to which rollup accrues.

The fix is purely logical in the existing controller code:

1. **Unblock D2 first** (see below) — once the Project advances to `Running` after `ImportComplete=True`, all subsequent planner Jobs at phase/plan levels complete normally and roll up via the existing `handleJobCompletion → budget.RollUpUsage` path already in place.
2. **Verify the adoption guard** (`project_controller.go:1088`) does not also suppress project-level cost reporting when child planners succeed. It does not — it only suppresses the project-level Job dispatch; phase/plan planner completions are independent and already roll up.

**Stack additions required:** None. `budget.RollUpUsage` in `internal/budget/tally.go` exists and is correct. The path just needs to be reachable.

**Do NOT add:** A separate "adoption-mode rollup" path, a second `budget.RollUpUsage` callsite in the import controller, or any new budget package. The existing path is correct once D2 is fixed.

---

## D2 — Project Lifecycle Advances Under Adoption: No New Dependency

### Root cause (verified in code)

`reconcileProjectPlannerDispatch` at `project_controller.go:999` short-circuits correctly at `Initialized` when the adoption guard fires — but it returns `ctrl.Result{}` (nil, nil), so the project parks at `Initialized`. The phase/plan planners fire via their own reconcilers (they watch Phase/Plan CRs, not Project phase), but the Project itself never advances to `Running`.

The fix is a targeted lifecycle transition in `reconcileProjectPlannerDispatch` (or in the adoption guard arm): when `ImportComplete=True` and the adoption guard fires, patch `Project.Status.Phase = PhaseRunning` before returning. This is a single `r.Status().Patch` call using the same `client.MergeFrom` pattern used throughout the existing reconciler.

### Integration point

`project_controller.go` already imports `k8s.io/apimachinery/pkg/api/meta` for `meta.SetStatusCondition`. The phase transition is:

```go
// In the import adoption guard arm, before returning ctrl.Result{}:
if project.Status.Phase == tidev1alpha2.PhaseInitialized {
    patch := client.MergeFrom(project.DeepCopy())
    project.Status.Phase = tidev1alpha2.PhaseRunning
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:    tidev1alpha2.ConditionReady,
        Status:  metav1.ConditionTrue,
        Reason:  "ImportAdopted",
        Message: "Import tree adopted; advancing to Running for child planner cascade",
        ...
    })
    if err := r.Status().Patch(ctx, project, patch); err != nil {
        return ctrl.Result{}, err
    }
}
```

**Stack additions required:** None. All types and helpers already imported in `project_controller.go`.

**Do NOT add:** An `ImportReconciler` lifecycle hook that patches the Project phase, a separate reconciler for "adopted projects", or any new condition type. The ProjectReconciler already owns Project phase transitions.

---

## D3 — Dispatch Concurrency Cap: Existing Pool Infrastructure, Wrong Defaults

### The misdiagnosis to avoid

`MaxConcurrentReconciles` (set via `controller.Options{MaxConcurrentReconciles: N}` in each reconciler's `SetupWithManager`) controls how many **reconcile goroutines** can run concurrently for a given Kind. It does NOT control how many **Jobs** (pods) are in flight. A single reconcile goroutine can dispatch a Job, return, and the next goroutine starts a new reconcile and dispatches another Job. With `MaxConcurrentReconciles: 4` for Plan and 15 phases all reconciling simultaneously, 60 Jobs can and did dispatch.

**MaxConcurrentReconciles is the wrong lever for D3.** Do not change it to address pod count.

### The correct mechanism: the existing `internal/pool` semaphore

The pool infrastructure is complete and production-wired (verified in `internal/pool/pool.go`, `cmd/manager/main.go:343-353`):

```go
// cmd/manager/main.go:343-353 (verified live)
plannerPool := pool.New(cfg.PlannerConcurrency, "planner")
executorPool := pool.New(cfg.ExecutorConcurrency, "executor")

plannerPool.PreCharge(ctx, mgr.GetClient(), "tideproject.k8s/role=planner")
executorPool.PreCharge(ctx, mgr.GetClient(), "tideproject.k8s/role=executor")
```

`pool.Acquire` is a blocking `chan struct{}` send that waits until a slot is available:

```go
// pool.go:61-67 (verified live)
func (p *Pool) Acquire(ctx context.Context) error {
    select {
    case p.sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

`PlannerPool.Acquire` is called before every planner Job creation at all five dispatch sites (milestone, phase, plan, project, and wave level — verified via grep). The chart default `plannerConcurrency: 16` allows up to 16 simultaneous planner pods, which is the OOM cause: 44 plan + 15 phase = 59 planners can dispatch against a 16-slot pool.

### The fix: lower chart defaults for single-node targets

**`charts/tide/values.yaml` change (FIXED CONTRACT — binary catches up to chart, never reverse):**

```yaml
# Before (current):
plannerConcurrency: 16
executorConcurrency: 4

# After:
plannerConcurrency: 4   # sane for single-node kind; operator raises for multi-node
executorConcurrency: 8  # executor pods are smaller; more can coexist
```

A single-node kind cluster with 8 GiB RAM can sustain roughly 4 concurrent Claude CLI + credproxy sidecar pairs (each ~1-2 GiB footprint) before memory pressure degrades throughput. `plannerConcurrency: 4` prevents the 60-pod cascade. `executorConcurrency: 8` preserves task parallelism (executor pods are lighter than planner+credproxy pairs).

These defaults should ship with documentation in `values.yaml` and `docs/production.md` explaining how to size for multi-node clusters.

### Separately-sized pools preserved

The spec contract "size planner and executor pools separately" is preserved. The existing architecture already has two independent `Pool` structs, two independent `PreCharge` calls with distinct label selectors (`tideproject.k8s/role=planner` vs `tideproject.k8s/role=executor`), and two independent chart values. Nothing in this fix unifies them.

**Stack additions required:** Zero. The pool is wired, the Acquire/Release calls are in place at all five dispatch sites, and `PreCharge` corrects the count on controller restart. The fix is a chart default change plus a `values.yaml` comment.

**Do NOT add:**
- A new in-process queue, rate limiter, or work queue (`workqueue.RateLimitingInterface`) — the pool semaphore already is the rate limiter
- `MaxConcurrentReconciles` tuning as the OOM fix — wrong lever
- An external job scheduler (Argo Workflows, Tekton) — the pool IS the scheduler
- A Kubernetes `ResourceQuota` on the namespace — correct direction conceptually but adds operator overhead and doesn't replace the pool; complement only
- Any in-Job concurrency limiting — the issue is at dispatch, not execution

### Enhancements to consider (not blocking)

- **Prometheus metric for pool queue depth** — `pool.go` currently has no instrumentation. A `prometheus.Gauge` for `len(sem)` vs `cap(sem)` would surface saturation. Not needed for the OOM fix, but useful for tuning on production clusters. Implement with existing `prometheus/client_golang v1.23`, zero new dep.
- **Per-Project pool isolation** — today all projects share one global planner pool. This is correct for v1.0.6; per-project pools are a future concern (multi-tenant) and are explicitly out of scope.

---

## D4 — Planner Failure Semantics: No New Dependency

### Root cause (verified in code)

In both `MilestoneReconciler.handleJobCompletion` (`milestone_controller.go:673`) and `PhaseReconciler.handleJobCompletion` (`phase_controller.go:593`), the `expected == 0` branch fires `patchMilestoneSucceeded`/`patchPhaseSucceeded` unconditionally:

```go
// milestone_controller.go:672-676 (verified)
if expected == 0 {
    // Genuine leaf — planner authored no Phase children.
    logger.V(1).Info("boundary push skipped: planner authored no Phase children (leaf)", "milestone", ms.Name)
    return r.patchMilestoneSucceeded(ctx, ms)
}
```

The defect: when `out.ExitCode != 0` AND `out.ChildCount == 0`, this arm fires, marking the parent Succeeded. A planner that failed with zero children is NOT a genuine leaf — it is a failed planner. The `expected == 0` case must first check `out.ExitCode`.

The plan controller has the same structural issue at `plan_controller.go:691-700` (the `expected == 0` arm for the `handlePlannerJobCompletion` path), but plan planner failures manifest differently (they fail the reconcileWaveMaterialization path, not patchPlanSucceeded directly), and the D4 report observed it at the phase level.

### Fix shape

In `handleJobCompletion` for both `MilestoneReconciler` and `PhaseReconciler`, immediately before the `expected == 0 → patchSucceeded` arm, add:

```go
if envReadOK && out.ExitCode != 0 {
    // Planner failed — do NOT succeed the parent as a leaf.
    // Fail (or retry) instead. A failed planner with zero children is not
    // a genuine leaf; it is a broken planner that must be re-run.
    return r.patchMilestoneFailed(ctx, ms, "PlannerFailed",
        fmt.Sprintf("planner Job exited %d with 0 children (not a leaf)", out.ExitCode))
}
```

The existing `patchMilestoneFailed`/`patchPhaseFailed` functions already exist and set `Status.Phase = "Failed"` plus a `ConditionFailed` condition. The retry path is `tide resume --retry-failed`, which already handles Failed phases and milestones (validated in v1.0.1 Phase 12).

For the `!envReadOK` case (read error): the existing behavior (requeue, let the fallback BoundaryDetected path handle it) is already correct and should not change — we only know to fail when we can actually read the exit code.

**Stack additions required:** None. `patchMilestoneFailed`, `patchPhaseFailed` exist. The condition type `tidev1alpha2.ConditionFailed` exists. The retry verb `tide resume --retry-failed` is implemented.

**Do NOT add:**
- Automatic planner retry inside the controller (re-dispatch on failure is `tide resume --retry-failed`, not a controller backoff loop)
- A new condition type for "planner failed with zero children"
- Any change to `handleJobCompletion` for `ProjectReconciler` without separately verifying — project-level planner failures may need the same guard but it was not the observed defect and should be verified independently

---

## go.mod Impact

**Zero new `require` entries.**

All four fixes use only:
- Existing controller-runtime types: `client.MergeFrom`, `ctrl.Result`, `meta.SetStatusCondition`
- Existing TIDE types: `tidev1alpha2.PhaseRunning`, `tidev1alpha2.ConditionReady`, `tidev1alpha2.ConditionFailed`
- Existing TIDE functions: `budget.RollUpUsage`, `patchMilestoneFailed`, `patchPhaseFailed`, `r.Status().Patch`
- Chart values: `plannerConcurrency`, `executorConcurrency` in `charts/tide/values.yaml`

No new packages, no new `require` entries, no external queues or schedulers.

---

## Summary Table

| Defect | Fix mechanism | Integration point | New dep? |
|--------|--------------|-------------------|----------|
| D1 — cost rollup under adoption | Unblock D2 so `PhaseRunning` is reached; existing `budget.RollUpUsage` path already correct | `project_controller.go` adoption guard | None |
| D2 — lifecycle stalls at Initialized | Patch `Project.Status.Phase = PhaseRunning` when adoption guard fires and project is still Initialized | `project_controller.go:reconcileProjectPlannerDispatch` | None |
| D3 — no concurrency cap | Lower chart defaults: `plannerConcurrency: 4`, `executorConcurrency: 8`; existing `pool.Acquire` already enforces the cap | `charts/tide/values.yaml` | None |
| D4 — false Succeeded on failed planner | Add `if envReadOK && out.ExitCode != 0 { patchFailed }` before the `expected == 0 → patchSucceeded` arm | `milestone_controller.go`, `phase_controller.go` in `handleJobCompletion` | None |

---

## What NOT to Add (Explicit Prohibition)

| Avoid | Why |
|-------|-----|
| `MaxConcurrentReconciles` change to fix D3 | Bounds reconcile goroutines, not in-flight Jobs — wrong lever |
| External work queue (Redis, NATS, etc.) | Pool semaphore already IS the work queue; adding an external queue violates no-external-DB constraint and adds ops burden |
| Argo Workflows / Tekton for concurrency | TIDE owns the DAG; waves are derived, not declared as Workflow templates (spec anti-pattern, enforced) |
| Per-Project planner pools | Out of scope for v1.0.6; shared pool is correct for single-project dogfood run |
| Automatic planner retry in controller | Retry is `tide resume --retry-failed`; controller retry loops add complexity and re-spend risk |
| `ImportReconciler` lifecycle hook for D2 | ProjectReconciler owns Project phase; cross-reconciler status writes violate the ownership model |
| New budget package / second RollUpUsage callsite for D1 | Existing path is correct once D2 is unblocked; a second callsite creates double-counting risk |
| Kubernetes `ResourceQuota` as the OOM fix | Complementary but does not replace pool; adds operator onboarding burden |

---

## Sources

- `internal/pool/pool.go` — `Pool.Acquire`/`Release`/`PreCharge` via `chan struct{}` semaphore; `PreCharge` lists live Jobs by label selector (HIGH confidence, live code, read directly)
- `cmd/manager/main.go:343-353` — `plannerPool` and `executorPool` constructed with `pool.New(cfg.PlannerConcurrency, "planner")`; both `PreCharge`d at startup; `plannerPool` wired into all five planner reconcilers (HIGH confidence, live code, read directly)
- `charts/tide/values.yaml:78-79` — `plannerConcurrency: 16`, `executorConcurrency: 4` confirmed (HIGH confidence, live file, read directly)
- `internal/config/config.go` — `Config.PlannerConcurrency` default 16, `ExecutorConcurrency` default 4; config validated >= 1 (HIGH confidence, live code, read directly)
- `internal/controller/milestone_controller.go:673-676` — `expected == 0 → patchMilestoneSucceeded` without exit-code check; confirmed D4 gap (HIGH confidence, live code, read directly)
- `internal/controller/phase_controller.go:593-596` — same `expected == 0 → patchPhaseSucceeded` gap (HIGH confidence, live code, read directly)
- `internal/controller/phase_controller.go:525-533` — `setBillingHaltIfNeeded` already gated on `envReadOK && out.ExitCode != 0`; D4 fix mirrors this pattern (HIGH confidence, live code, read directly)
- `internal/controller/project_controller.go:1088-1133` — Phase 30 adoption guard returns `ctrl.Result{}` without advancing `Status.Phase` from Initialized; confirmed D2 root (HIGH confidence, live code, read directly)
- `internal/controller/milestone_controller.go:587-590` — `budget.RollUpUsage` gated on `isFirstCompletion && envReadOK && project != nil`; reachable only via `handleJobCompletion` (planner Job completion path, not import path); confirmed D1 root (HIGH confidence, live code, read directly)
- `internal/controller/import_controller.go` — `succeedImport` sets `ConditionImportComplete=True` but performs no Project phase transition; ImportReconciler has no Project.Status.Phase write; confirmed D2 root (HIGH confidence, live code, read directly)
- controller-runtime v0.24.x docs — `controller.Options{MaxConcurrentReconciles}` documents goroutine concurrency, not Job count (HIGH confidence, verified against existing code usage in all SetupWithManager calls)

---

*Stack research for: TIDE v1.0.6 — Adoption-Path Correctness & Dispatch Safety*
*Researched: 2026-06-28*
