# Domain Pitfalls — v1.0.6 Adoption-Path Correctness & Dispatch Safety

**Domain:** Corrective patch on an existing Go controller-runtime operator (TIDE v1.0.5)
**Defects addressed:** D1 (cost rollup under adoption), D2 (lifecycle stall at Initialized), D3 (dispatch concurrency cap), D4 (planner failure semantics — childless/exit-nonzero)
**Researched:** 2026-06-28
**Source:** Direct code inspection of internal/controller/\*, internal/pool/\*, internal/budget/\*, run-2b-FINDINGS.md, PROJECT.md

---

## Critical Pitfalls

### P-D1a: Double-counting `costSpentCents` under at-least-once reconcile

**What goes wrong:** `handleProjectJobCompletion` calls `budget.RollUpUsage` conditionally on `isFirstCompletion`. The `isFirstCompletion` signal was historically "reporter Job IsNotFound" — which reverts to `true` once the reporter Job TTL-GC's at 300 s. This exact bug was caught in Phase 27 (PlannerRolledUpUID marker added). The fix pattern is a durable `PlannerRolledUpUID` marker in `.status`. If the adoption-path fix introduces a new rollup call (e.g., rolling up per-planner-Job usage when a phase- or milestone-level planner fires under adoption) without a matching durable idempotency marker, the same double-count recurs every time the Job GC's.

**Why it happens:** The pool.Pool semaphore is in-memory; the rollup guard must live in CRD `.status` to survive controller restarts and Job TTL-GC. Any "first time?" check that relies on in-memory state or on the presence/absence of a K8s Job object fails when the Job disappears.

**Consequences:** `Project.Status.Budget.CostSpentCents` exceeds real spend; `absoluteCapCents` may fire spuriously; or conversely (if the guard overcounts the deduplication), real spend is never reported.

**Prevention:**
- Mirror the existing `PlannerRolledUpUID` pattern exactly for every new rollup call site: check the marker before calling `budget.RollUpUsage`, set it only on successful rollup, never clear it.
- The marker key must be unique per planner Job (e.g., `MilestoneRolledUpUID`, `PhaseRolledUpUID` paralleling the existing field), not a boolean flag.
- For the adoption path specifically: `project.Spec.ImportSource != nil` already suppresses rollup at the project-planner level (D-11 / `if project.Spec.ImportSource != nil { … skip … }`). Verify this guard is present at every reconciler level that calls `budget.RollUpUsage` — milestone, phase, and plan — not only the project level.

**Warning sign:** `Project.Status.Budget.PlannerRolledUpUID` is set but `costSpentCents` increments on every reconcile pass, or `costSpentCents` does not increase at all after 60 s of real API calls.

**Detection:** Envtest: call `budget.RollUpUsage` twice with identical usage; assert `CostSpentCents` changes only once. Integration test: inject a fake `isFirstCompletion=true` condition twice on the same project; assert the rollup is idempotent.

**Phase owner:** D1 phase.

---

### P-D1b: Rollup fires before or skips the lifecycle gate under adoption

**What goes wrong:** The rollup for child-level planners (milestone, phase, plan) is keyed off `handleJobCompletion` completion handlers, which are only reached when the corresponding level is `Running` and its planner Job reaches terminal state. Under adoption (D2), the project stays at `Initialized` — `reconcilePhase3Lifecycle`'s Step 0b `reconcileProjectPlannerDispatch` has an adoption guard at line ~1105 that returns early, so the project never transitions to `Running`, and the planner Job is never dispatched. Child-level planners (phase-, plan-) fire independently via their own reconcilers, which have no equivalent "adoption: do not dispatch" guard at the milestone or phase level.

This means usage from phase and plan planners under adoption reaches `handleJobCompletion`, calls `budget.RollUpUsage`, but the budget gate (`handleBudgetGate`) reads `project.Status.Budget.CostSpentCents` only when `reconcileProjectPhase2` runs — which is gated on `project.Status.Phase`. If D2 is not fixed first, the budget gate can never fire even after rollup lands.

**Why it happens:** D1 and D2 share the same lifecycle seam. The cost meter and the lifecycle phase are co-dependent: the phase gate reads the meter, the meter only fires from the Running state's code path.

**Consequences:** Budget cap never enforces during an adopted run — exactly the run-2b failure mode.

**Prevention:** Fix D2 (lifecycle advance after ImportComplete=True) before D1 can be verified. The envtest for D1 must assert that `BudgetExceeded` fires after enough mock usage accumulates — not just that `costSpentCents` > 0.

**Warning sign:** `costSpentCents` is non-zero in `.status.budget` but `Phase` is still `Initialized` and no `BudgetExceeded` condition appears.

**Detection:** Envtest: create an adopted project (ImportSource set, ImportComplete=True, milestone present), fire a plan-level planner completion with usage that exceeds `absoluteCapCents`, then assert `Project.Status.Phase == BudgetExceeded`.

**Phase owner:** D1+D2 phase (same fix).

---

### P-D2a: Re-firing the suppressed project-planner after lifecycle advance

**What goes wrong:** The adoption guard in `reconcileProjectPlannerDispatch` (lines ~1105–1132) returns early when `ImportComplete=True` AND an owned Milestone exists. If the D2 fix advances `project.Status.Phase` from `Initialized` to `Running` via a new code path, and that path does not call `reconcileProjectPlannerDispatch` at all (i.e., bypasses Step 0b entirely), the guard is never evaluated. But if D2's advance patches `Status.Phase=Running` and then on the next reconcile `reconcilePhase3Lifecycle` reaches Step 0b as normal, the adoption guard runs — and if the list of owned Milestones is momentarily empty (informer lag between milestone creation and the list cache syncing), the guard falls through and dispatches the project planner.

**Why it happens:** The WR-01 guard in the existing adoption guard comments on exactly this: a free-form Spec.ProjectRef check would collide with same-name Milestones from a prior project. The UID-bound owner reference check (`metav1.IsControlledBy`) is correct, but only if the informer cache is warm. Under a fresh controller restart with a stale cache, `r.List` may return 0 items transiently, letting the guard fall through.

**Consequences:** A paid project-planner Job fires post-import, re-generating the milestone tree from scratch and potentially overwriting the adopted status.

**Prevention:**
- Do not rely on `r.List` returning a non-empty result as a one-shot gate. Add a second sentinel: a durable `.status` field (e.g., `Status.Import.AdoptionComplete: true`) set once the adoption guard has permanently suppressed dispatch. Check the field first, before the List.
- Alternatively: after ImportComplete=True, set `Status.Phase=Running` via a Status patch but also stamp `ConditionAuthoringPlanner=False` with `Reason=AdoptionSuppressed`. The existing dispatch guard at reconcileProjectPlannerDispatch already short-circuits on `ConditionAuthoringPlanner.Reason==PlannerDispatched` — adding a parallel `AdoptionSuppressed` short-circuit prevents re-dispatch even on cache miss.
- The fix must not change the non-adoption path. The adoption code path is gated on `project.Spec.ImportSource != nil` throughout; keep all new conditions inside that gate.

**Warning sign:** After a controller restart mid-adoption, a `tide-project-<uid>-1` Job appears in the namespace even though `ImportComplete=True` is already set and owned Milestones exist.

**Detection:** Envtest: set ImportComplete=True + create owned Milestone, then reconcile the project multiple times (simulate cache-cold by using `fake.NewClientBuilder()` with no pre-populated Milestones on the first reconcile). Assert no planner Job is created.

**Phase owner:** D2 phase.

---

### P-D2b: Lifecycle advance breaks the normal (non-adoption) path

**What goes wrong:** Adding a new `if project.Spec.ImportSource != nil` arm to `reconcilePhase3Lifecycle` that advances `Status.Phase=Running` can accidentally alter the phase machine for non-import projects if the condition check is too broad or the new arm does not `return` before reaching the existing Step 0b.

**Why it happens:** `reconcilePhase3Lifecycle` is a flat fallthrough function with comments labeling each step. A new arm inserted at the wrong position (e.g., after the `complete` fast-path but before Step 0b) can execute for non-import projects if `project.Spec.ImportSource` is nil but the surrounding logic alters control flow.

**Consequences:** Regression in the normal project lifecycle — most immediately in the existing `TestProjectLifecycle` envtest suite.

**Prevention:**
- Gate every new block with `if project.Spec.ImportSource != nil { … return … }` — never fall through from the adoption arm.
- Run the full envtest suite (`make test-int-fast`) as the D2 gate, not just the new adoption test. Any regression in `project_controller_test.go` or `project_phase3_test.go` is a no-ship blocker.
- Add a regression envtest: create a project WITHOUT `ImportSource` and assert the existing lifecycle (init→clone→running→complete) is unaffected.

**Warning sign:** `TestProjectLifecycle` or `TestProjectPhase3` fails after the D2 change.

**Detection:** `make test-int-fast`; also grep for all callers that reach `reconcilePhase3Lifecycle` and confirm none of the new ImportSource arms are reachable when `project.Spec.ImportSource == nil`.

**Phase owner:** D2 phase.

---

### P-D3a: Pool `Acquire` counts in-process goroutines, not in-flight Kubernetes Jobs

**What goes wrong:** `pool.Pool.Acquire` is a `chan struct{}` semaphore that counts reconcile-goroutine acquisitions. It correctly caps simultaneous `r.Create(job)` calls within one manager process. However:

1. After a manager restart, `PreCharge` re-inspects live Jobs via `client.List` and charges one slot per `Status.Active > 0` Job. If the D3 fix uses a new label selector for the PreCharge call, or uses the wrong label, the pre-charge misses running Jobs and the pool starts at 0 slots consumed — allowing a fresh wave to dispatch up to the cap again on top of already-running Jobs.
2. The pool tracks goroutines, not Jobs. A TTL-GC'd Job (gone from the API server) whose goroutine already released the slot means the pool has "free" capacity that corresponds to a Job still running (if the container runtime is alive but the Job object was GC'd). On a kind cluster this matters for the TTL=300s Jobs.
3. `PreCharge` panics (via the `default:` arm returning an error) if live Jobs exceed capacity. That turns a misconfiguration into a manager startup failure rather than a soft degraded-mode. This is intentional and correct, but must be documented for the chart values.

**Why it happens:** The pool is an in-process semaphore, not a remote count. The K8s source of truth for in-flight Jobs is `Status.Active`, not the pool's channel depth.

**Consequences:** After manager restart, the cap is effectively inoperative until `plannerCapsFloorSeconds` (1800 s) has elapsed and old Jobs complete, during which the effective concurrency is `cap + (pre-existing active jobs that were missed)`.

**Prevention:**
- Use `tideproject.k8s/role=planner` and `tideproject.k8s/role=executor` as the label selectors for PreCharge — verify these labels are stamped on every planner and executor Job in `podjob.BuildJobSpec`. Do not use a broader selector.
- Envtest: call `pool.PreCharge` with a fake client containing 3 active Jobs on a capacity-4 pool; assert 3 slots consumed; then Acquire the 4th; assert 5th blocks.
- Document in chart values that `plannerPool.capacity` and `executorPool.capacity` must be set to the expected peak parallelism — not the desired parallelism. A too-small cap fails manager startup via PreCharge overflow.

**Warning sign:** After a manager restart, the pool appears to have full capacity even though `kubectl get jobs -n <ns> -l tideproject.k8s/role=planner` shows N active Jobs.

**Phase owner:** D3 phase.

---

### P-D3b: Pool that silently truncates a wave — no queuing, no log

**What goes wrong:** When a pool slot is unavailable, `pool.Acquire` blocks the reconcile goroutine until the context is cancelled (if no slot frees). If the cap is 5 and wave 0 has 15 phases, the first 5 reconciles acquire slots and dispatch; the next 10 block in `Acquire` until context timeout. From the operator's perspective, the 10 stalled phases just do not dispatch. There is no condition on the Phase CR, no event on the Project, no metric count of deferred dispatches. The stall is invisible unless the operator reads manager logs.

The current pool.go has no `Waiting() int` or `Deferred() int` metric. The only signal is the context-timeout error that surfaces as a reconcile error (which controller-runtime logs at error level and requeues).

**Why it happens:** The pool is a simple `chan struct{}` without observability hooks.

**Consequences:** Operators cannot distinguish "cap hit, dispatch deferred" from "something is stuck". On a single-node kind cluster with cap=5, a 15-phase run will stall silently for up to 5 × plannerCapsFloorSeconds before all phases dispatch.

**Prevention:**
- Add a Prometheus counter `tide_pool_deferred_total{pool="planner"}` incremented before each `Acquire` call when the pool is at capacity (check `len(sem) == cap(sem)` before calling). This is a non-blocking check.
- Add a log line at `Info` level: "planner pool at capacity; dispatch will block" when `len(sem) == cap(sem)` before `Acquire`.
- Envtest: create more phases than pool capacity; assert `tide_pool_deferred_total > 0` after reconcile.

**Warning sign:** 15 phases exist, only 5 have planner Jobs in `Active` state, and the remaining 10 show no conditions or events.

**Phase owner:** D3 phase.

---

### P-D3c: Deadlock-adjacent: pool capacity must be strictly less than `MaxConcurrentReconciles`

**What goes wrong:** `pool.Pool.Acquire` blocks the reconcile goroutine. `Release` fires on `return` from `reconcilePlannerDispatch` — immediately after `r.Create(job)` — not when the Job finishes. So the pool slot is held only for the duration of the API call, and there is no true deadlock. However: if pool capacity equals `MaxConcurrentReconciles`, every reconcile goroutine can block in `Acquire` simultaneously, leaving zero goroutines available to process Owns-watch events (which would otherwise trigger completions). Controller-runtime's work queue does not distinguish goroutine states; with all goroutines in `Acquire`, no new events process until one goroutine's `Create` call completes.

**Prevention:** Set pool capacity strictly less than `MaxConcurrentReconciles`. The recommended configuration: `pool.capacity = N`, `MaxConcurrentReconciles = 2N` or more. Document this invariant in chart values.

**Warning sign:** The manager appears to freeze — no reconcile completions logged — while all goroutines are in `Acquire`.

**Phase owner:** D3 phase.

---

### P-D3d: Pool-unification — planner and executor pools unified into one

**What goes wrong:** If the chart values or manager startup code passes the same `*pool.Pool` instance to both `PlannerPool` and `ExecutorPool` fields across all reconcilers, the spec's "size planner and executor pools separately" constraint is violated. The immediate effect: planner Jobs compete with executor Jobs for slots; a full executor wave can exhaust the pool, blocking all planners.

The existing code has a `crosspool` static analyzer (`tools/analyzers/crosspool/`) that enforces this constraint. If D3 adds new pool wiring, the analyzer's known field set may need updating.

**Prevention:**
- Keep the two pools as distinct named variables in `cmd/manager/main.go`. Never assign `plannerPool` to an `ExecutorPool` field or vice versa.
- After any change to pool construction or wiring, run `make lint` (which invokes the crosspool analyzer via golangci-lint).

**Warning sign:** `crosspool` analyzer fires, or both pools have the same `name` field value in manager logs.

**Phase owner:** D3 phase.

---

### P-D3e: Cap interacting with wave-boundary failure semantics — undispatched tasks stall the wave

**What goes wrong:** The spec's wave-boundary contract requires that failed-task siblings continue and dependents never dispatch. But if the pool cap prevents some wave-N tasks from dispatching — they never acquire a slot — those tasks are not failed; they are simply pending with `Phase=""`. The wave-boundary completion check in `gates.BoundaryDetected` counts owned Tasks by Succeeded status. An undispatched task has `Phase=""`, making `observed Succeeded < total tasks` — the wave never completes.

**Why it happens:** `BoundaryDetected` counts Succeeded children vs total children. A task that never dispatches (pool blocked) stays at `Phase=""` forever, making the wave stall indefinitely.

**Consequences:** A wave that cannot fully dispatch because the pool is too small will stall indefinitely. The run neither advances nor fails.

**Prevention:**
- The default pool capacity must be at least as large as the widest wave in the target workload. For run-2b (15 phases in wave 0), a cap of 15 or larger is required. The "sane single-node default" from D3's scope must document: "set to a value smaller than the cluster's pod capacity, not smaller than your widest wave."
- The fallback for a too-small cap is not to silently truncate but to serialize: all N tasks dispatch one-at-a-time through the semaphore, each wave member completing before the next acquires a slot. Verify this serial behavior in envtest.

**Warning sign:** `gates.BoundaryDetected(ctx, r.Client, plan, "Task")` returns false even though some tasks Succeeded, and the undispatched tasks have `Phase=""` with no planner Job.

**Phase owner:** D3 phase.

---

### P-D4a: Retry storm from returning a Go error on transient planner failure

**What goes wrong:** If the D4 fix returns `ctrl.Result{}, fmt.Errorf("transient error")` from the planner completion handler for a non-zero exit code, controller-runtime will exponentially backoff-requeue the item — up to 500+ requeues per minute under the default rate limiter. This retry storm exhausts the pool and blocks all other reconciles.

The existing `setBillingHaltIfNeeded` already classifies `billing` vs other reasons. The D4 fix must follow the same classification: `out.Reason == "billing"` → BillingHalt (existing path); `exitCode != 0 && childCount == 0` → PlannerFailed (new path); transient read error (`!envReadOK`) → do not fail, requeue.

**Prevention:**
- Only fail (set Phase=Failed via condition patch) when the reason is definitively non-transient: `exitCode != 0 AND childCount == 0 AND envReadOK`. Never fail when `!envReadOK` (transient envelope read failure).
- Use `patchPhaseFailed` / `patchMilestoneFailed` (sets a condition permanently), not a Go error return. Returning an error re-queues; setting Failed patches the condition permanently.
- The billing-halt check (`setBillingHaltIfNeeded`) runs before the D4 childless check; keep this ordering.

**Warning sign:** Manager logs show the same milestone or phase item being requeued 100+ times in a minute after a planner failure.

**Detection:** Envtest: inject a planner Job with `exitCode=1, reason="timeout"` and `childCount=0`; assert `Phase.Status.Phase=Failed` is set (not a requeue loop). Then inject `exitCode=1, reason="billing"` and assert `BillingHalt=True` is set on the project (not `Phase.Status.Phase=Failed`).

**Phase owner:** D4 phase.

---

### P-D4b: Level-divergent semantics — Phase/Milestone guard diverges from the Plan guard Phase 30 added

**What goes wrong:** Phase 30 added the childless-plan guard only at the plan controller level. D4 must add the equivalent at the phase and milestone controllers. Current state as read from the code:

- `plan_controller.go`: `handlePlannerJobCompletion` reads `out.ExitCode` via `setBillingHaltIfNeeded`, but has no explicit `patchPlanFailed` on `exitCode!=0/childCount==0`. (The task-level cycle detection sets `Phase=Failed` separately, at line ~1099.)
- `phase_controller.go`: `handleJobCompletion` calls `setBillingHaltIfNeeded` if `out.ExitCode != 0` but has no `patchPhaseFailed` path.
- `milestone_controller.go`: same — `setBillingHaltIfNeeded` but no `patchMilestoneFailed` on `exitCode!=0/childCount==0`.

D4 must add `patchFailed` at all three levels with identical conditions, or the Phase false-Succeeded bug (run-2b D4) persists at milestone and phase level.

**Prevention:**
- Implement the guard identically at all three levels: `if envReadOK && out.ExitCode != 0 && out.ChildCount == 0 { return patchFailed(...) }`.
- Do NOT use `out.ExitCode != 0` alone (without `childCount == 0`) — a planner that successfully authors children but exits with a warning code should not fail the level.
- Do NOT use `out.ChildCount == 0` alone — `exitCode == 0` with `childCount == 0` is a valid leaf handled by the existing "genuine leaf" succeed path.
- Extract a shared helper: `func isPlannerFailure(out pkgdispatch.EnvelopeOut, envReadOK bool) bool { return envReadOK && out.ExitCode != 0 && out.ChildCount == 0 }`. Three call sites, one definition.

**Warning sign:** `Phase.Status.Phase=Succeeded` when `kubectl logs <planner-job-pod>` shows `exitCode: 1` and no Plans were created. Or the guard fires for `exitCode=0, childCount=0` (incorrectly failing a leaf).

**Detection:** Envtest at three levels:
- Milestone: inject planner Job with exitCode=1, childCount=0; assert `ms.Status.Phase=Failed`.
- Phase: inject planner Job with exitCode=1, childCount=0; assert `ph.Status.Phase=Failed`.
- Plan: verify existing Phase-30 test still passes with the same conditions.
- Regression: inject exitCode=0, childCount=0 at all three levels; assert `Status.Phase=Succeeded` (leaf path, NOT Failed).

**Phase owner:** D4 phase.

---

## Moderate Pitfalls

### P-Cross-1: Sequencing — D1+D2 must land before D3 can be validated end-to-end

**What goes wrong:** D3's real-world efficacy cannot be verified until D1+D2 are fixed. With D2 unfixed, `project.Status.Phase` stays at `Initialized` and plan-level planner dispatch is gated on the ImportComplete check (plan_controller.go:374). Before D1+D2, the pool cap prevents nothing because dispatches are held by D2's lifecycle stall.

**Prevention:** Fix D2 in the first phase of this milestone; fix D3 in a subsequent phase after D2 is envtest-verified. Do not attempt to integration-test D3 on run-2b infra before D2 ships.

**Phase owner:** Per FINDINGS.md recommended sequencing.

---

### P-Cross-2: Single-node kind cannot hold the parallelism — infra, not a code fix

**What goes wrong:** A single-node kind cluster has approximately 2–4 vCPU and 7–8 GiB RAM. Each claude-CLI pod (planner or executor) consumes ~300–800 MB RSS plus the credproxy sidecar. At 10 concurrent planner pods, RSS exceeds available memory and the node OOM-kills. This is the direct cause of run-2b's `Exited 137`.

**Prevention:** Do not validate D3's concurrency cap on single-node kind. Use a multi-node kind cluster or a VM with at least 16 GiB RAM for the relaunch run. Single-node kind is sufficient for envtest and unit tests of the pool logic, but not for verifying that 15 concurrent planner pods are stable.

**Phase owner:** Infrastructure concern for the relaunch run, not a code fix in this milestone.

---

### P-Cross-3: `MaxConcurrentReconciles` is NOT the dispatch cap

**What goes wrong:** `MaxConcurrentReconciles` limits how many reconcile goroutines run simultaneously for a given Kind. It does NOT limit how many planner Jobs are in flight — a reconcile goroutine creates a Job and returns immediately (holding the pool slot only for the duration of `r.Create`). Setting `MaxConcurrentReconciles=5` with `pool.capacity=20` means up to 5 goroutines create Jobs simultaneously, but 20 Jobs can be active at once.

**Prevention:** Document the distinction in the chart values and in the manager's flag help text. The D3 fix must wire `pool.Pool.capacity` as the knob, not `MaxConcurrentReconciles`. The chart should expose both independently.

**Phase owner:** D3 phase (documentation and chart).

---

### P-D1c: Reporter-Job TTL race at rollup — same class as Phase 27 PlannerRolledUpUID bug

**What goes wrong:** In `spawnReporterIfNeeded` (called from `handleJobCompletion` at the milestone and phase levels), `isFirstCompletion` is set to `true` when the reporter Job is NotFound. After TTL-GC at 300 s, the reporter Job disappears and the next reconcile sees IsNotFound again, setting `isFirstCompletion=true` and calling `budget.RollUpUsage` a second time.

Phase 27 fixed this at the project level with `PlannerRolledUpUID`. Check whether milestone and phase controllers already have an equivalent durable marker. If not, add `MilestoneRolledUpUID` / `PhaseRolledUpUID` fields to Project status (or an equivalent label on the child CR), set after successful rollup, checked before calling `budget.RollUpUsage`.

**Warning sign:** `costSpentCents` continues to increment approximately every 300 s after the planning cascade completes.

**Phase owner:** D1 phase.

---

## Minor Pitfalls

### P-D2c: ImportComplete idempotency and the retry annotation

**What goes wrong:** The `ImportReconciler` has a `ConditionImportComplete=True` idempotency guard that returns early. If D2's lifecycle-advance code path is triggered by `ConditionImportComplete` becoming True, and the operator sets `AnnotationRetryImport` to reset the import, the import controller clears `ConditionImportComplete` — but D2's advance may have already set `Status.Phase=Running`. On the next reconcile, D2 checks `ConditionImportComplete` and finds it `False` (because of the retry), but the project is already at `Running`. This creates an inconsistent state.

**Prevention:** D2's lifecycle-advance must check `ConditionImportComplete=True` and that `Status.Phase != Running` before acting. It must not re-advance if `Running` is already set, and must not conflict with the import retry path.

**Phase owner:** D2 phase.

---

### P-D4c: The childless guard for leaf levels — exitCode=0, childCount=0 is valid

**What goes wrong:** The D4 guard triggers on `exitCode != 0 AND childCount == 0`. The existing "genuine leaf" path for milestones (milestone_controller.go:673 `if expected == 0 { return r.patchMilestoneSucceeded }`) fires on `expected == 0` regardless of exit code. If the D4 guard fires before the leaf-check (wrong ordering), it fails a planner that legitimately authored no children with exit code 0.

**Prevention:** Ordering in `handleJobCompletion`:
1. BillingHalt check (existing).
2. D4 childless-failure check: `if envReadOK && out.ExitCode != 0 && out.ChildCount == 0 { patchFailed }`.
3. Leaf-success check: `if out.ChildCount == 0 { patchSucceeded }`.

The two checks are mutually exclusive when `envReadOK=true` because they branch on `ExitCode`. When `!envReadOK`, neither fires (fallback to children-based BoundaryDetected).

**Phase owner:** D4 phase.

---

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|-------------|---------------|------------|
| D1: rollup under adoption | Double-count via isFirstCompletion reporter TTL race | PlannerRolledUpUID-style durable marker at every rollup call site |
| D1: budget gate fires | Budget gate blocked by D2 lifecycle stall | Fix D2 first; envtest must assert BudgetExceeded not just costSpentCents > 0 |
| D2: lifecycle advance | Re-firing suppressed project-planner on informer cache miss | Durable adoption-complete sentinel in .status, not just List count |
| D2: normal path regression | adoption arm falls through to non-import code | Gate every new block on ImportSource != nil; run full envtest suite |
| D3: pool pre-charge | Wrong label selector misses live Jobs at manager restart | Verify labels stamped in BuildJobSpec match PreCharge selector |
| D3: pool vs reconcile concurrency | MaxConcurrentReconciles confused with dispatch cap | Document distinction; wire pool.capacity as the real cap |
| D3: silent wave truncation | Pool-blocked tasks have no observable signal | Add deferred-dispatch Prometheus counter and Info log line |
| D3: pool unification | Same Pool pointer in PlannerPool and ExecutorPool | crosspool analyzer; separate named vars in main.go |
| D4: retry storm | Returning Go error for transient planner failure | Use patchFailed (condition), not error return, for definitive failures |
| D4: level divergence | Guard added at phase but not milestone (or vice versa) | Shared isPlannerFailure helper; envtest at all three levels |
| D4: leaf regression | exitCode=0 childCount=0 wrongly fails level | D4 guard requires exitCode != 0; ordering: fail check before succeed check |
| Relaunch infra | Single-node kind OOM at peak parallelism | Multi-node cluster or 16 GiB VM for run-2b relaunch |

---

## Sources

- `.planning/dogfood/run-2b-FINDINGS.md` — authoritative defect descriptions (D1–D4)
- `.planning/PROJECT.md` — Key Decisions: PlannerRolledUpUID, BillingHalt, pool sizing, CRD-status-only persistence
- `internal/controller/project_controller.go` — `handleProjectJobCompletion`, `reconcileProjectPlannerDispatch`, adoption guard (~L1088–1132), `handleBudgetGate`
- `internal/controller/milestone_controller.go` — `handleJobCompletion`, `spawnReporterIfNeeded`, `patchMilestoneSucceeded`
- `internal/controller/phase_controller.go` — `handleJobCompletion`, `patchPhaseSucceeded`
- `internal/controller/plan_controller.go` — `handlePlannerJobCompletion`, childless-success/failure shape, Phase-30 leaf guard
- `internal/controller/import_controller.go` — `succeedImport`, `ConditionImportComplete`, `AnnotationRetryImport` reset path
- `internal/pool/pool.go` — `Pool.Acquire`/`Release`/`PreCharge` semantics, TTL-GC behavior
- `internal/dispatch/podjob/caps.go` — `DefaultCaps`, `JobKindPlanner` / `JobKindExecutor` discriminator
