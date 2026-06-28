# Feature Research — TIDE v1.0.6 Adoption-Path Correctness & Dispatch Safety

**Domain:** Corrective patch for a Kubernetes-native agentic orchestrator — adoption-path lifecycle,
budget rollup, dispatch concurrency, and planner failure semantics.
**Researched:** 2026-06-28
**Confidence:** HIGH for all four defects — direct codebase inspection of live controller code,
pool.go, config.go, budget/tally.go, and all planner controllers; no WebSearch required.
**Mode:** Ecosystem (corrective patch)

---

## Research Context

This is NOT a new-feature milestone. Each "feature" is the **correct expected behavior** of an
existing code path that is currently broken. The four defects come directly from dogfood run #2b
(run-2b-FINDINGS.md D1–D4). The audience for this document is the requirements author and
roadmap planner who will turn these behavioral specifications into testable envtest acceptance
criteria.

**TIDE invariants that constrain every fix in this document:**

1. Waves are derived, never declared — don't cache the schedule; re-derive from the completed-task
   set in O(V+E).
2. Persistence is CRD-`.status` only — no external DB; rollup/concurrency state is re-derivable.
3. Cycles are bugs, not runtime conditions — cycle detection happens at plan-validation time.
4. Resumption state is minimal: indegree map + completed-task set, nothing more.
5. Pool semantic: `pool.Pool` is a channel-based semaphore whose slot is held for the duration of
   the reconcile call, not the duration of the Job. Release is deferred to function return.
6. The adoption path suppresses the project-planner (correct) — the lifecycle advance must come
   from a distinct adoption-complete transition, not from `handleProjectJobCompletion`.

---

## Existing Features This Milestone Builds On (Do NOT re-propose)

These are already shipped and must not be duplicated or weakened:

- Global Execution DAG (Spring Tide, phases 22–26)
- Import/adoption flow: ImportController state machine, `tide-import` Job, `ImportComplete=True`
  condition, adoption guard that suppresses the project-planner post-import (project_controller.go:1105)
- Budget reserve/settle via ReservationStore + BudgetBlocked condition
- BillingHalt (provider billing-400 halts the entire project)
- Gates-as-holds at every planning-DAG level
- Reporter Jobs materializing child CRDs (MilestoneReconciler, PhaseReconciler, PlanReconciler all
  spawn tide-reporter)
- `tide resume --retry-failed` recovery verb
- Pool infrastructure: `internal/pool/pool.go` (chan-based semaphore), `PlannerPool` and
  `ExecutorPool` fields on every reconciler, `config.go` `PlannerConcurrency`/`ExecutorConcurrency`
  fields, PreCharge at manager startup
- Plan childCount guard in plan_controller.go:692 (Phase 30) — what D4 extends to phase + milestone

---

## D2 — Project Lifecycle Advances Under Adoption

### Root Cause (verified in project_controller.go)

After `ImportComplete=True`, `reconcileProjectPlannerDispatch` hits the adoption guard at line 1119
and returns `ctrl.Result{}` (no requeue, no error). This zero result passes the
`if result.Requeue || result.RequeueAfter > 0` gate at reconcilePhase3Lifecycle line 509 and
execution falls through. However `Project.Status.Phase` stays at `PhaseInitialized` because the
only places that advance Phase to `PhaseRunning` are:

- `handleProjectJobCompletion` line 1205/1402 — only called after the project-planner Job
  completes; the adoption path suppresses that Job entirely.
- `handleBudgetGate` line 1402 — only fires after bypass logic; irrelevant here.

So the adopted project is permanently parked at `Initialized` even as milestone/phase/plan planners
run and produce output. Anything downstream keyed on `project.Status.Phase == PhaseRunning` — most
critically D1's budget rollup — never fires.

### Table-Stakes Behavior

| Behavior | Acceptance Signal (envtest) |
|----------|-----------------------------|
| After `ImportComplete=True`, the Project must advance from `PhaseInitialized` to `PhaseRunning` without a project-planner Job dispatching | `kubectl wait --for=jsonpath='{.status.phase}'=Running` on the Project object after ImportComplete=True |
| The advance must happen on the same reconcile pass that detects `ImportComplete=True` with at least one owned Milestone — not on a subsequent requeue triggered by a child event | Envtest: create an adopted project with a seed Milestone; patch ImportComplete=True; reconcile; assert Project.Status.Phase == "Running" in a single test step, no additional triggers |
| The advance must be idempotent: a second reconcile after the advance must not revert Phase to `Initialized` | Envtest: reconcile twice; assert Phase stays "Running" on both passes |
| The advance must not fire the project-planner Job: Phase==Running on an imported project must not trigger the normal dispatch path at reconcilePhase3Lifecycle Step 0b | Envtest: assert zero planner Jobs for the imported project namespace after the Phase advance |
| The `ConditionReady` or an equivalent condition must reflect the post-import running state | Optional: assert a condition documenting "Adopted" or "Running" so operators can distinguish a freshly-initialized project from an adoption-completed one |

### Implementation Note

The fix is a narrow state-machine transition: at the point where `reconcileProjectPlannerDispatch`
would early-return on the adoption guard (project_controller.go line 1119), it should first
advance `Project.Status.Phase` to `PhaseRunning` if the current phase is still `PhaseInitialized`.
This is a single Status patch before the `return ctrl.Result{}, nil`. No new controller, no new
condition type, no pool acquire.

### Dependency

D1 depends on D2: budget rollup in milestone/phase/plan controllers calls
`budget.RollUpUsage(ctx, r.Client, project, out.Usage)` but the rollup calls
`budget.IsCapExceeded(project)` which reads `project.Status.Budget.CostSpentCents` — none of this
is gated on project Phase. However the D-11 suppression at project_controller.go line 1306 (`if
project.Spec.ImportSource != nil { skip rollup }`) is the actual blocker for D1. D2 is a
prerequisite for the correct budget gate check (the `BudgetBlocked` condition and halt logic are
gated on project Phase via `reconcilePhase3Lifecycle`), so D2 must ship before or with D1.

---

## D1 — Cost Rollup Under Adoption

### Root Cause (verified in project_controller.go and milestone/phase/plan controllers)

The milestone/phase/plan controllers already call `budget.RollUpUsage` on Job completion — these
paths are NOT gated on project Phase and DO fire for adopted trees. The actual blocker is the
D-11 suppression in `handleProjectJobCompletion` (project_controller.go line 1304–1307):

```go
// D-11/R-13: budget rollup is suppressed unconditionally for imported envelopes —
// the prior run already counted the planning cost; rolling up here would double-count.
if project.Spec.ImportSource != nil {
    logger.V(1).Info("skipping budget rollup: project has importSource (D-11)", "project", project.Name)
}
```

This suppression was correct and intentional for the project-level planner envelope (which is a
pre-paid artifact from a prior run). But it does NOT suppress rollup in the milestone, phase, and
plan controllers — those rollups fire from NEWLY-DISPATCHED Jobs that did incur real spend. The
run-2b finding that `costSpentCents` stayed zero is most likely explained by D2: if the Project
never advanced to `PhaseRunning`, the budget gate check never evaluated `IsCapExceeded`, and the
controller never stamped a `BudgetBlocked` condition that operators or tests would observe as proof
the rollup worked. The budget tally itself (the patch in `budget/tally.go`) is unconditional —
`RollUpUsage` patches `Project.Status.Budget.TokensSpent` and `CostSpentCents` regardless of
project phase.

**Critical gap to verify during implementation:** Confirm by adding an envtest that milestone/phase/plan
controllers DO call `RollUpUsage` for adopted projects (where `project.Spec.ImportSource != nil`).
If they are also guarded (grep all callsites for similar `if project.Spec.ImportSource != nil`
checks), those guards must be removed or scoped to the planner-level suppression only. The D-11
intent was to avoid double-counting the prior-run planning cost; newly-dispatched Jobs in the
adopted run are genuinely new spend and must be tallied.

### Table-Stakes Behavior

| Behavior | Acceptance Signal (envtest) |
|----------|-----------------------------|
| `Project.Status.Budget.CostSpentCents` accrues as phase/plan/milestone planner Jobs complete under an imported project | Envtest: create imported project (ImportSource set), confirm ImportComplete=True, allow one phase-planner Job to complete with Usage.EstimatedCostCents > 0; assert Project.Status.Budget.CostSpentCents > 0 |
| `Project.Status.Budget.TokensSpent` accrues similarly | Same envtest: assert TokensSpent > 0 after completion |
| A project with `absoluteCapCents` set to a low value halts once `CostSpentCents` exceeds the cap, even on the import path | Envtest: set budget.absoluteCapCents to 1 cent; confirm BudgetBlocked=True or BudgetExceeded phase after rollup |
| The project-level planner envelope rollup suppression (D-11) remains in place: the project-planner Job itself does NOT double-count the imported planning cost | Regression: assert that project_controller.go's D-11 suppression block is preserved; no project-level envelope rollup fires |
| Rollup is idempotent: re-running the same Job completion handler does not double-count | The existing `PlannerRolledUpUID` marker guards idempotency at the project level; for milestone/phase/plan, the isFirstCompletion flag gates rollup (preserved) |

### Edge Cases

**Budget cap crossed mid-adoption cascade:** The `BudgetBlocked` condition is evaluated at dispatch
time by `checkBudgetBlocked(project)`. If the cap is crossed mid-cascade, the NEXT dispatch
(whichever reconciler fires next) will observe `BudgetBlocked=True` and park. Already-dispatched
Jobs continue to completion — this is the correct bounded-overshoot behavior per the
`ReservationStore` design. The fix must not change this.

**Rollup with no project reference:** `milestone_controller.go` and `phase_controller.go` guard
rollup with `if isFirstCompletion && envReadOK && project != nil` — this nil guard is already
correct. The adoption path changes nothing about how `project` is resolved (it comes from
`ms.Spec.ProjectRef` / `ph.Spec.ProjectRef`).

**D-11 scope clarification:** D-11 was "suppress project-level envelope rollup for imported
projects." The correct re-scoping is: suppress ONLY the project-planner Job's usage rollup (because
that Job ran in a prior run and its cost was already counted then). Do not suppress milestone-,
phase-, or plan-level rollups for Jobs dispatched in the current run. The guard comment and
behavior must be updated to reflect this narrower scope.

### Dependency

D1 depends on D2 for the budget HALT gate to fire correctly (`reconcilePhase3Lifecycle` must be
running, which requires Phase==Running). The rollup itself (the tally increment) can fire
independently of Phase — but the halt enforcement that stops future dispatch depends on the
project phase being in a state where `handleBudgetGate` evaluates.

---

## D3 — Dispatch Concurrency Caps

### Root Cause (verified in pool.go and milestone_controller.go / phase_controller.go)

The `pool.Pool` infrastructure exists and is correctly wired at all planner dispatch sites:
- `milestone_controller.go:382` — `r.PlannerPool.Acquire(ctx)` + `defer r.PlannerPool.Release()`
- `phase_controller.go:380` — same pattern
- `plan_controller.go` — same pattern (not verified above but structurally identical)
- `project_controller.go:1137` — same pattern

**The problem is the semantics of `defer r.PlannerPool.Release()`.**

The reconcile function for milestone (and phase, plan) calls `Acquire`, creates the Job, then
returns. `defer Release()` fires when the function returns — which is moments after Job creation.
So the pool slot is held only for the duration of the `reconcileDispatch` function (milliseconds),
not for the duration of the Job (1800s+). This means:

- Pool capacity=16 (the default from config.go) allows 16 SIMULTANEOUS RECONCILE CALLS to create
  Jobs, each releasing their slot as soon as they return.
- The next batch of reconcile calls (triggered by child events, requeue, or status changes) can
  immediately acquire slots and create more Jobs.
- Because the cascading fan-out runs many reconcile goroutines in quick succession (15 phases × 44
  plans = 59 goroutines ready from the wave fan-out), all 59 can cycle through the pool in 59/16
  rounds over a few seconds, dispatching all 59 Jobs with no effective throttle.

The `PreCharge` at startup (pool.go:88) is the mechanism for RESTART scenarios — it pre-fills the
pool from live Jobs to prevent double-dispatch on leader failover. It is NOT a steady-state throttle
because `Release()` is called on function return, not Job completion.

**The fix requires changing `Release()` semantics:** either defer Release to the Job's terminal
state (Job watch → release on Complete/Failed), or replace the per-reconcile acquire/release with
a separate "slot lease" that is retained in the pool's state until the Job terminates.

### Table-Stakes Behavior

| Behavior | Acceptance Signal (envtest or integration) |
|----------|--------------------------------------------|
| At most `PlannerConcurrency` planner Jobs are Active at any moment across all reconcilers | Envtest: configure PlannerConcurrency=2; observe via periodic Job List that `.status.active` count for planner Jobs never exceeds 2 |
| At most `ExecutorConcurrency` executor (task) Jobs are Active at any moment | Envtest: configure ExecutorConcurrency=2; same observation |
| Excess dispatches queue (controller reconcile blocks on Acquire) rather than fail or leak | Envtest: with concurrency=2 and 5 ready-to-dispatch milestones, eventually all 5 milestone Jobs start and complete (not stuck); no Jobs are dropped |
| On manager restart with N live Jobs, PreCharge correctly accounts for all N slots | Existing PreCharge test coverage; no regression |
| Single-node kind default is safe: with the default PlannerConcurrency the cluster does not OOM | Integration test (kind tier): set PlannerConcurrency to 4 (or a tuned single-node default); drive a 15-phase adoption run; kind node does not exit 137 |
| Planner and executor pools remain independent: executor cap does not bleed into planner cap | Unit test on pool.go; crosspool analyzer already enforces this statically (tools/analyzers/crosspool/) |

### Hard vs Soft Queue Decision

**Hard queue (blocking Acquire) is the correct choice.** The existing `pool.Pool.Acquire(ctx)`
already blocks the reconcile goroutine until a slot is free. This is not an external queue; it is
in-process backpressure via a Go channel. When the reconcile context is cancelled (controller-
runtime workqueue timeout), the goroutine returns an error and is requeued by the workqueue for
retry. No Jobs are lost. This matches how batch/workflow operators (Argo, Tekton, Volcano) handle
admission: a submitted unit is held in a pending queue until a resource slot is available.

**No external queue.** An external queue (Redis, K8s Queue CRD, Kueue) is out of scope for this
milestone, out of spec (CRD-status-only), and would create an operational dependency. The channel
semaphore is sufficient.

**How Operators in This Space Expose a Concurrency Cap**

This is the question asked by the downstream consumer: chart value vs CRD field vs both?

**Precedents:**
- Argo Workflows: `controller.workflowWorkers` in the controller ConfigMap; no per-workflow cap
  at the CRD level for the concurrency of the workflow controller itself. Per-workflow parallelism
  is a separate field (`parallelism` on the Workflow spec).
- Tekton Pipelines: `config-feature-flags` ConfigMap with `max-running-tasks-per-taskrun` (not a
  CRD field). Per-pipeline concurrency via LimitRange.
- Kueue (K8s-native batch queue): `ClusterQueue` CRD defines `nominalQuota` for slots. This is
  a separate CRD, not a field on the workload CRD.
- Volcano: `Queue` CRD with `capability` field (both CRD and chart-configurable).

**TIDE recommendation:** Chart value (via `config.yaml` ConfigMap) for the global pool caps;
no per-Project CRD field in v1.0.6. Rationale:

1. Pool caps are cluster-resource policy, not per-project intent. They belong in the operator's
   deployment config, not in the workload CR.
2. The infrastructure already exists: `config.go` `PlannerConcurrency`/`ExecutorConcurrency` feed
   `pool.New(cfg.PlannerConcurrency, "planner")` in manager main.go. Only the default value and
   the Release semantics need to change.
3. A per-Project `concurrency` field would require the controller to select the minimum of
   project-level and cluster-level caps, adding complexity without a real use case in v1.0.6.
4. The chart is the FIXED contract (binary catches up to chart, never reverse per project rule).
   Adding a chart value for the pool default is safe and reversible.

**Default value recommendation:** Reduce `PlannerConcurrency` default from 16 to 4 for
single-node kind cluster safety. The dogfood run dispatched ~60 Jobs simultaneously; a cap of 4
active planner Jobs at a time would have serialized them safely. Document in chart values.yaml
and in `config.go` with a comment referencing the run-2b-FINDINGS.md OOM incident.

### Implementation Note: Release Semantics Change

The Release must move from "deferred to function return" to "deferred to Job terminal state."
Two viable approaches:

1. **Watch-triggered release:** Add a watch on `batchv1.Job` with a label selector for
   `tideproject.k8s/role=planner`. When a Job transitions to Complete or Failed, enqueue the
   owning reconciler which calls `r.PlannerPool.Release()`. This requires a separate "leased jobs"
   counter or a per-Job semaphore token keyed by Job UID.

2. **Count-based live-Jobs check at Acquire time:** Instead of holding a channel slot, at dispatch
   time count the number of `Active` Jobs with role=planner using a label selector list, and
   short-circuit if the count >= cap. Return `ctrl.Result{RequeueAfter: 5s}` if at cap. This is
   simpler but slightly less precise (label-indexed List has a short cache lag).

Approach 2 (count-based check) is simpler to implement, matches how Argo and Tekton compute
admission, and avoids the complexity of per-Job token tracking. The channel semaphore in pool.go
is replaced or supplemented with a live-Job count check. The pool.PreCharge mechanism remains
useful for accounting on restart.

### Anti-Features for D3

| Anti-Feature | Why Not |
|---|---|
| External queue (Redis, RabbitMQ, Kueue) | Out of spec (CRD-status-only); adds ops dependency |
| Per-Project concurrency CRD field in v1.0.6 | Over-engineered for the actual use case; chart value is sufficient |
| Wave-level concurrency cap (separate from pool) | Wave internal parallelism is already bounded by the pool; two caps would interact unpredictably |
| Per-level (milestone/phase/plan) separate caps | The spec says "size planner and executor pools separately" — two pools, not six |
| Cycle-recovery features | Cycles are bugs; refuse at validation time; no runtime recovery |

---

## D4 — Planner Failure Semantics

### Root Cause (verified in phase_controller.go:590–596)

The plan controller's ChildCount-gated succession (plan_controller.go:692) already has a
`if expected == 0 { return ctrl.Result{} }` fast-path that succeeds a plan with zero child tasks
— this was the Phase 30 guard. But this guard is for the case where the planner SUCCEEDED with
zero children (a genuine leaf: "I authored no tasks"). A planner that exits !=0 OR produces
childCount=0 on an error is a different failure mode.

In phase_controller.go, the exitCode check at line 525 (`if envReadOK && out.ExitCode != 0`)
calls `setBillingHaltIfNeeded` but does NOT call `patchPhaseFailed`. The function falls through
to the ChildCount-gated succession at line 590. If `out.ChildCount == 0` (planner failed and
wrote no children), the succession path at line 596 calls `patchPhaseSucceeded`. This is the bug:
a failed planner with zero children succeeds the Phase.

The milestone controller has the same structure (milestone_controller.go:596). The plan controller
does NOT directly call `patchPlanFailed` from `handlePlannerJobCompletion` on exitCode != 0 either.

### Table-Stakes Behavior

| Behavior | Acceptance Signal (envtest) |
|----------|-----------------------------|
| A phase whose planner Job exits 1 must result in Phase.Status.Phase == "Failed", not "Succeeded" | Envtest: configure EnvReader to return ExitCode=1, ChildCount=0; reconcile PhaseReconciler; assert Phase.Status.Phase == "Failed" |
| A phase whose planner produces ChildCount=0 AND ExitCode=0 (genuine leaf) must Succeed | Envtest: ExitCode=0, ChildCount=0; assert Phase.Status.Phase == "Succeeded" — this is the leaf success case and must not regress |
| A milestone whose planner exits !=0 must result in Milestone.Status.Phase == "Failed" | Envtest: same pattern at milestone level |
| A milestone whose planner produces ChildCount=0 AND ExitCode=0 must Succeed | Envtest: genuine leaf milestone — must not regress |
| The ExitCode check and the ChildCount=0+ExitCode!=0 check are evaluated BEFORE the ChildCount-gated succession fast-path | Code order: exitCode guard at line 525 must set Failed before the line 596 leaf-success fires |
| A failed planner does not trigger BudgetBlocked or BillingHalt unless the reason is billing-related | The existing `setBillingHaltIfNeeded` call is retained; exitCode != 0 with non-billing reason does NOT halt the project |

### Edge Cases

**exitCode != 0 but envelope unreadable (envReadOK=false):** If the Job exited non-zero but the
envelope could not be read from the PVC (e.g., the process died before writing), `envReadOK=false`
and `out.ExitCode` is zero-valued. The fix must handle this case: if the Job's
`batchv1.Job.Status.Failed > 0` (the Job-level failure count), treat as a planner failure
regardless of envelope readability. This is the "Job failed with no readable envelope" shape.

**Retry semantics — retry-then-fail vs fail-fast:** Based on TIDE's existing patterns, the
correct behavior is:
- Phase/milestone planner failure → mark the level `Failed` immediately (fail-fast at this
  level). The `tide resume --retry-failed` verb is the operator's explicit recovery path.
- Do NOT auto-retry planner Jobs. Auto-retry without operator intent risks spending budget on
  broken prompts repeatedly. The existing plan_controller.go approach (patchPlanFailed, not
  auto-retry) is the correct pattern to replicate.
- Retry is operator-driven via `tide resume --retry-failed` which clears the Failed status and
  allows re-dispatch on the next reconcile.

**Interaction with gates:** A planner that fails while awaiting approval (`AwaitingApproval`
condition) should be classified as Failed regardless of gate state. The gate-on-failure path
must not park a Failed level as AwaitingApproval.

**Guard scope:** Phase 30 added the childless-success guard for PLANS. D4 extends the
exitCode!=0 + childCount=0 guard to PHASES and MILESTONES. Task executors have a separate
failure path (task_controller.go handles Job.Status.Failed via a different code path). D4 does
NOT touch the task controller or the executor pool.

### Differentiating "genuine leaf" from "failed with no children"

The critical invariant to preserve:

```
exitCode == 0 AND childCount == 0 → Succeeded (genuine leaf — planner decided no work)
exitCode != 0 AND childCount == 0 → Failed (planner failed, never authored children)
exitCode == 0 AND childCount > 0  → Wait for children (normal case)
exitCode != 0 AND childCount > 0  → Failed (planner failed but somehow wrote some children — rare; treat as failed)
```

The third row (exitCode != 0, childCount > 0) may indicate a planner that partially authored
then died. The safest treatment is Failed: a partial child set is unreliable. Any authored
children can be cleaned up via the normal finalizer cascade on level deletion/retry.

### Anti-Features for D4

| Anti-Feature | Why Not |
|---|---|
| Auto-retry on planner failure | Risks budget burn on broken prompts; operator must explicitly approve retry |
| Cycle recovery for cyclic child sets produced by a failed planner | Cycles are bugs; refuse at plan-validation time; a failed planner that produced a cycle is doubly failed — mark Failed, do not attempt recovery |
| Re-extending this guard to the plan controller | Phase 30 already added this for plans; do not re-implement |
| Treating exitCode != 0 as AwaitingApproval | Conflates gate semantics with failure semantics; gates hold successful completions; failures are Failed |

---

## Feature Map: Table Stakes, Differentiators, Anti-Features

### Table Stakes (All four must ship in v1.0.6)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| D2: Project Phase advances to Running after ImportComplete=True | Without this, the adopted project is permanently stuck at Initialized; D1's halt gate never evaluates | Low — targeted state-machine fix, one Status patch before the adoption guard return | Must not dispatch a project-planner Job |
| D1: Budget rollup accrues for adopted-project plan/phase/milestone Jobs | `absoluteCapCents` is the safety contract for expensive runs; a budget that doesn't count spend is not a budget | Low-Medium — verify D-11 suppression scope; narrow the guard to project-planner only; add envtest | Must preserve D-11 no-double-count for the project-planner envelope |
| D3: Per-level max-in-flight cap enforced at steady state, not just at startup | Single-node OOM is a safety failure, not a performance issue; any operator deploying on a real cluster expects the cap to work | Medium — Release semantics change from function-return to Job-terminal-state; default cap reduction | Config-only surface (chart values), not a CRD field |
| D4: Planner exitCode!=0 or childCount=0+exitCode!=0 → Phase/Milestone Failed | False-Succeeded levels corrupt the planning DAG; a phase that succeeded with no plans will never be re-planned, leaving a gap in the execution tree | Low-Medium — exitCode check before ChildCount succession in phase + milestone controllers; envtest |Genuine leaf (exitCode==0, childCount==0) must not regress |

### Differentiators (Not in v1.0.6 scope)

These are follow-on improvements that the defect fixes unlock but do not require:

| Feature | Value | Why Defer |
|---------|-------|-----------|
| Dashboard "Adopted" badge distinguishing imported vs freshly-planned nodes | Operator visibility into what was re-planned vs adopted | Read-only dashboard; low urgency vs correctness |
| Per-Project concurrency CRD field | Allows per-project tuning | Chart-level cap is sufficient for v1.0.6; adds schema complexity |
| Prometheus metrics for pool saturation (slots-in-use/capacity gauge) | Operators can observe when the pool is the bottleneck | Nice to have; logging is sufficient initially |
| Automatic retry on planner failure (with backoff and max-attempts) | Reduces manual intervention | Out of scope; operator-driven retry is the correct v1.0.6 posture |

### Anti-Features (Explicitly Not Building)

| Anti-Feature | Why Not | What to Do Instead |
|---|---|---|
| External queue (Kueue, Redis) for dispatch throttling | Out of spec (CRD-status-only); adds external dependency | In-process count-based or channel-based pool check |
| Schedule caching in `.status` | Direct invariant violation — waves are derived, never declared | Re-derive from task DAG + completed-task set in O(V+E) |
| Cycle recovery for cycles produced by a broken planner | Cycles are bugs — refuse at validation time | Mark parent Failed; operator fixes and retries |
| Auto-advance Project Phase without fixing the lifecycle seam | Side-effects on non-import paths | Scope the advance to ImportComplete=True + owned Milestone present |
| Double-counting prior-run planning cost in rollup | Ruins budget accuracy for adopted projects | Keep the D-11 project-level suppression; remove suppression only from milestone/phase/plan-level Jobs |
| Per-wave concurrency cap separate from per-pool cap | Two caps on the same resource interact unpredictably | One planner pool, one executor pool; wave-internal parallelism is a derived consequence |
| Collapse planner failure into BudgetBlocked or BillingHalt | Conflates provider errors with code errors | Keep three distinct conditions: Failed (planner bug), BillingHalt (provider 400), BudgetBlocked (cap) |

---

## Feature Dependencies

```
D2 (Project lifecycle advance)
    └──enables──> D1 (budget halt gate fires on adopted projects)
    └──independent──> D3 (pool cap; no lifecycle dependency)
    └──independent──> D4 (planner failure semantics; no lifecycle dependency)

D1 (budget rollup)
    └──requires──> D2 (Phase must be Running for halt enforcement to evaluate)
    └──independent──> D3 (rollup fires regardless of pool state)
    └──independent──> D4 (rollup fires from reporters, not affected by planner failure path)

D3 (concurrency cap)
    └──independent──> D1, D2, D4
    └──requires──> chart value change for new default (not a CRD change)

D4 (planner failure semantics)
    └──independent──> D1, D2, D3
    └──touches same controllers as D1/D2 but different code paths
```

### Ordering Rationale for Phases

Implement D2 and D1 together in one phase (they share the same lifecycle seam in project_controller.go
and the same envtest setup: an adopted project running real planner Jobs). D3 and D4 can be
separate phases since they touch different code paths. Recommended order:

1. Phase A: D2 + D1 (lifecycle advance + rollup wire-up; single adopted-project envtest)
2. Phase B: D3 (pool cap semantics; requires envtest with multiple concurrent dispatches)
3. Phase C: D4 (planner failure guards at phase + milestone; envtest with exiting-nonzero planner)

---

## Envtest Shape Reference

These are envtest-shaped acceptance criteria for the roadmapper. All in `internal/controller/`:

### D2 + D1 Combined Test

```go
// Create a Project with Spec.ImportSource set
// Create an owned Milestone (simulating ImportComplete)
// Patch Project.Status.Conditions: ImportComplete=True
// Reconcile ProjectReconciler
// Assert: Project.Status.Phase == "Running"
// Assert: No planner Job created in the namespace
//
// Then: Allow one Phase-level planner Job to complete
// (mock EnvReader to return ExitCode=0, ChildCount=1, Usage{EstimatedCostCents: 10})
// Reconcile PhaseReconciler
// Assert: Project.Status.Budget.CostSpentCents > 0
// Assert: Project.Status.Budget.TokensSpent > 0
//
// Then: Set budget.absoluteCapCents = 1
// Reconcile ProjectReconciler
// Assert: BudgetBlocked condition = True OR Project.Status.Phase == "BudgetExceeded"
```

### D3 Test

```go
// Configure PlannerConcurrency=2 in the Pool
// Create 5 Milestone objects all ready to dispatch
// Reconcile all 5 MilestoneReconcilers concurrently
// At any point-in-time inspection: count Active planner Jobs <= 2
// Eventually: all 5 Jobs complete (no deadlock)
```

### D4 Tests

```go
// Phase failure test:
// Create a Phase; mock EnvReader to return ExitCode=1, ChildCount=0
// Reconcile PhaseReconciler handleJobCompletion
// Assert: Phase.Status.Phase == "Failed"
// Assert: No child Plans created

// Phase genuine-leaf regression:
// Create a Phase; mock EnvReader to return ExitCode=0, ChildCount=0
// Reconcile PhaseReconciler handleJobCompletion
// Assert: Phase.Status.Phase == "Succeeded"

// Milestone failure test (same pattern at milestone level)
// Milestone genuine-leaf regression
```

---

## Sources

All findings are HIGH confidence — direct source-code inspection, no inference:

- `/Users/justinsearles/Projects/tide/internal/controller/project_controller.go` — D-11 suppression at line 1304; adoption guard at 1105; lifecycle advance gap at 1119; Phase advance sites at 1205/1402
- `/Users/justinsearles/Projects/tide/internal/controller/milestone_controller.go` — rollup at 588; exitCode check at 596; ChildCount succession at line 593+
- `/Users/justinsearles/Projects/tide/internal/controller/phase_controller.go` — exitCode check at 525; ChildCount succession at 593–596 (false-success bug); rollup at 519
- `/Users/justinsearles/Projects/tide/internal/controller/plan_controller.go` — Phase 30 childCount guard at 692; exitCode check at 600; rollup at 593
- `/Users/justinsearles/Projects/tide/internal/pool/pool.go` — Acquire/Release semantics (function-return); PreCharge for restart accounting
- `/Users/justinsearles/Projects/tide/internal/budget/tally.go` — RollUpUsage unconditional (no Phase gate)
- `/Users/justinsearles/Projects/tide/internal/config/config.go` — PlannerConcurrency default=16; ExecutorConcurrency default=4
- `/Users/justinsearles/Projects/tide/.planning/dogfood/run-2b-FINDINGS.md` — D1–D4 authoritative descriptions; "60 pods dispatched at once"
- `/Users/justinsearles/Projects/tide/.planning/PROJECT.md` — constraints, key decisions, failure semantics contract
- Operator-space precedents for concurrency cap surface (chart vs CRD vs both): Argo Workflows
  controller ConfigMap pattern; Tekton `config-feature-flags` ConfigMap; Kueue `ClusterQueue` CRD;
  Volcano `Queue` CRD — all sourced from training knowledge, MEDIUM confidence; sufficient for the
  surface-decision question (chart value is correct for v1.0.6)

---

*Feature research for: TIDE v1.0.6 — Adoption-Path Correctness & Dispatch Safety*
*Researched: 2026-06-28*
*Scope: Corrective patch — D1 through D4 from dogfood run #2b-FINDINGS.md only*
