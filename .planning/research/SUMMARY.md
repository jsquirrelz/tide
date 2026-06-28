# Project Research Summary

**Project:** TIDE v1.0.6 — Adoption-Path Correctness & Dispatch Safety
**Domain:** Corrective patch — Go/Kubernetes controller-runtime operator; four code-level defects on the import/adoption path exposed by dogfood run #2b
**Researched:** 2026-06-28
**Confidence:** HIGH (all four defects grounded in direct code inspection; no external libraries; no WebSearch required)

---

## Executive Summary

TIDE v1.0.6 is a targeted corrective patch on the import/adoption path first shipped in v1.0.5. Dogfood run #2b validated that adoption works (zero re-paid planning cost on resume; 44 plans authored via real Anthropic API) but OOM'd the single-node kind node after dispatching ~60 concurrent planner pods, and surfaced three additional correctness failures: the project lifecycle never advanced past `Initialized` under adoption (D2), the budget meter was therefore never tallied (D1), and a phase whose planner exited nonzero with no children was falsely marked `Succeeded` (D4). All four defects are narrow seam fixes — no new go.mod entries, no new CRDs, no architectural additions — on existing controller code that shipped partially wired.

The recommended approach is to fix in dependency order: D2 and D1 share a single seam in `reconcileProjectPlannerDispatch` and must ship together in one phase; D3 and D4 are independent of each other and of D1/D2 and can be planned in parallel after D1+D2 land. The milestone's critical open design decision is the D3 fix shape: one researcher (STACK.md) found the pool fully wired and concluded a chart-default reduction from 16 to 4 is sufficient; three deeper code-reads (ARCHITECTURE.md, FEATURES.md, PITFALLS.md) found that `defer r.PlannerPool.Release()` fires on reconcile-function return (milliseconds after Job creation, not Job completion), meaning the channel semaphore caps simultaneous `r.Create` calls but not in-flight pod count. That divergence must be resolved at the D3 plan/discuss phase before implementation begins.

The milestone has no external risk surface. Every fix uses existing controller-runtime helpers (`client.MergeFrom`, `r.Status().Patch`, `patchPhaseFailed`/`patchMilestoneFailed`), existing TIDE types, and the existing pool and budget infrastructure. All four fixes are envtest-verifiable. The relaunch of dogfood run #2b needs a multi-node cluster or a host with at least 16 GiB RAM regardless of which D3 fix shape is chosen; single-node kind cannot hold the parallelism.

---

## Key Findings

### Recommended Stack

No new dependencies. The fix set uses only what is already in go.mod:

**Core technologies (unchanged):**
- **Go 1.26 + controller-runtime v0.24.x** — all four fixes are standard `ctrl.Result` + `r.Status().Patch` patterns already used throughout the codebase
- **`internal/pool/pool.go` (existing `chan struct{}` semaphore)** — D3 fix shape (see design fork below) operates on or alongside this struct; Pool itself may or may not change depending on which approach is chosen
- **`internal/budget/tally.go` (`RollUpUsage`)** — already correct at phase/plan level; D1 fix ensures the lifecycle state allows the budget gate to evaluate, not changing `RollUpUsage` itself
- **`charts/tide/values.yaml` (FIXED CONTRACT)** — D3 requires a default change here (`plannerConcurrency: 16` to 3 or 4); binary catches up to chart, never reverse

**Version coupling:** No version changes. All fixes target logic inside existing packages.

### Expected Features (Behavioral Specifications)

This milestone's "features" are the correct expected behaviors of already-shipped code paths. All four must ship; none are deferrable without leaving run #2b's failure modes in production.

**Must have (table stakes — all four):**

- **D2: Project Phase advances to `Running` after `ImportComplete=True`** — without this the adopted project is permanently parked at `Initialized`; D1's budget halt gate never evaluates; project lifecycle state misrepresents reality. Fix: one `r.Status().Patch` before the adoption-guard `return` in `reconcileProjectPlannerDispatch`. Must not dispatch a project-planner Job; must be idempotent; must not regress the non-import lifecycle path.

- **D1: Budget rollup accrues for adopted-project Jobs** — `absoluteCapCents` is the safety contract for expensive runs; a budget that does not count spend is not a budget. The existing `budget.RollUpUsage` path at phase/plan levels is correct; fix scope is: (a) verify no `if project.Spec.ImportSource != nil { skip }` guards exist at milestone/phase/plan call sites beyond the correct project-planner-level D-11 suppression, and (b) confirm the budget halt gate evaluates once D2 sets `PhaseRunning`. The D-11 suppression (no double-counting of prior-run project-planner spend) must be preserved exactly.

- **D3: Per-level max-in-flight planner cap enforced at steady state** — the single-node OOM is a safety failure, not a performance issue. The cap must apply to in-flight running Jobs, not just to concurrent `r.Create` calls. Fix shape is a design decision (see fork below). Default must be sane for single-node kind; cap must NEVER silently truncate a wave without logging or eventing deferred dispatches.

- **D4: Planner `exitCode != 0` with `childCount == 0` must fail the parent phase/milestone** — false-Succeeded levels corrupt the planning DAG; a phase that succeeded with no plans will never be re-planned. Truth table: `(exitCode==0, childCount==0)` is Succeeded (genuine leaf, must not regress); `(exitCode!=0, childCount==0)` is Failed; `(exitCode!=0, childCount>0)` is Failed. A shared `isPlannerFailure` helper should cover phase and milestone (plan controller is already structurally correct via Phase 30).

**Defer (v2+):**
- Dashboard "Adopted" badge distinguishing imported vs freshly-planned nodes
- Per-Project concurrency CRD field (chart-level cap is sufficient for v1.0.6)
- Prometheus pool saturation gauge (logging is sufficient initially; P-D3b notes it as a pitfall mitigation worth adding but not blocking)
- Automatic planner retry with backoff (operator-driven `tide resume --retry-failed` is the correct v1.0.6 posture)

### Architecture Approach

All four fixes are modifications to existing controller functions, with no new reconcilers, no new CRDs, and no new persistence surface.

**Components modified:**

1. **`internal/controller/project_controller.go` — `reconcileProjectPlannerDispatch`** — D2+D1: one status patch inserted before the adoption-guard `return ctrl.Result{}, nil`; sets `project.Status.Phase = PhaseRunning` when `ImportComplete=True` and phase is still `Initialized`.

2. **`internal/controller/phase_controller.go` — `handleJobCompletion`** — D4: add `if envReadOK && out.ExitCode != 0 && out.ChildCount == 0 { return r.patchPhaseFailed(...) }` before the existing `expected == 0 -> patchPhaseSucceeded` leaf-success arm. Add `patchPhaseFailed` helper if absent (mirror `patchPlanFailed`).

3. **`internal/controller/milestone_controller.go` — `handleJobCompletion`** — D4: same guard and helper pattern as phase controller.

4. **D3 dispatch sites (4 files)** — `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `project_controller.go`: fix shape depends on the design fork resolution.

5. **`charts/tide/values.yaml`** — D3: `plannerConcurrency: 16` to 3 or 4; both values appear across research files.

**Components unchanged:** `internal/pool/pool.go` (unless D3 Option B is chosen), `internal/budget/tally.go`, `import_controller.go`, `wave_controller.go`, `task_controller.go`, `depgraph.go`, `failure_halt.go`, global Execution DAG, wave-boundary failure semantics.

### Critical Pitfalls

1. **D3 design fork: pool `Release` fires on reconcile-function return, not Job completion** — three of four researchers (ARCHITECTURE.md, FEATURES.md, PITFALLS.md) confirmed via direct code inspection that `defer r.PlannerPool.Release()` fires when `reconcilePlannerDispatch` returns, milliseconds after `r.Create(job)`, not when the Job finishes. This means the semaphore caps concurrent Job-creation calls, not in-flight running pods. A chart-default reduction alone (STACK.md's proposal) would lower the creation burst but would not prevent 60 separate creation calls from firing across multiple reconcile rounds and reaching 60 running pods. **This is the milestone's highest-priority design decision; see the fork section below.**

2. **Double-counting rollup via reporter-Job TTL-GC (P-D1a / P-D1c)** — `isFirstCompletion` is gated on reporter Job IsNotFound; after TTL-GC at 300s the Job disappears and the flag becomes true again, re-invoking `budget.RollUpUsage`. Phase 27 fixed this at the project level via `PlannerRolledUpUID`. The D1 phase must verify whether `MilestoneRolledUpUID` / `PhaseRolledUpUID` equivalent markers exist at child levels; if not, add them. Failing to do so causes `costSpentCents` to increment every 300s after planning completes.

3. **Re-dispatching the suppressed project-planner on informer cache miss (P-D2a)** — after the D2 fix advances `Status.Phase=Running`, a subsequent reconcile with a transiently empty informer cache (post-restart) may not see owned Milestones and let the adoption guard fall through, dispatching a paid project-planner Job. Prevention: stamp a durable adoption-complete sentinel in `.status` (not just rely on the List returning non-empty), or use a `ConditionAuthoringPlanner=False, Reason=AdoptionSuppressed` short-circuit.

4. **D4 retry storm if a Go error is returned for planner failure (P-D4a)** — returning `ctrl.Result{}, err` from `handleJobCompletion` on planner failure triggers controller-runtime exponential backoff, exhausting the pool. Use `patchPhaseFailed`/`patchMilestoneFailed` (permanent condition patch), not an error return, for definitive failures. Only fail permanently when `envReadOK && exitCode != 0`; let `!envReadOK` requeue.

5. **Silent wave truncation when pool cap is smaller than the widest wave (P-D3e / P-D3b)** — if `plannerConcurrency < widest wave width`, tasks that cannot acquire a slot park at `Phase=""` and `gates.BoundaryDetected` never returns true. The run stalls indefinitely without an observable signal. Document that the cap must be at least as large as the widest expected wave, and ensure parked dispatches emit a log line and (optionally) a Prometheus counter.

---

## Implications for Roadmap

Suggested phase structure: **3 phases**. D1+D2 are one lifecycle seam and must ship together. D3 and D4 are independent and can be developed in parallel; recommended serial order (D3 before D4) based on severity and the open design fork.

### Phase 1: D2 + D1 — Adoption Lifecycle Seam

**Rationale:** D2 and D1 share a single call site in `reconcileProjectPlannerDispatch`. D1 cannot be fully verified until D2 is fixed (budget halt gate requires `Phase==Running`). This is the "spent blind" defect that directly undermines the budget-safety guarantee of the v1.0.x line. Highest priority.

**Delivers:**
- Project advances from `Initialized` to `Running` on `ImportComplete=True` without dispatching a project-planner Job
- `Project.Status.Budget.CostSpentCents` and `TokensSpent` accrue as phase/plan/milestone planners complete
- `absoluteCapCents` halt enforcement fires correctly on adopted projects
- D-11 project-level rollup suppression preserved (no double-count of prior-run planning cost)

**Addresses:** D1, D2 from run-2b-FINDINGS.md

**Avoids:**
- P-D2a (project-planner re-dispatch on cache miss) — add durable adoption sentinel in `.status`
- P-D1a / P-D1c (double-count via reporter TTL-GC) — verify or add `MilestoneRolledUpUID`/`PhaseRolledUpUID` markers
- P-D2b (normal lifecycle regression) — gate all new code on `ImportSource != nil`; run full envtest suite as gate

**Research flag:** Standard patterns (no research-phase needed). Fix shape is unambiguous. Primary complexity is the idempotency pitfalls documented in PITFALLS.md; address these in planning via targeted envtest requirements.

---

### Phase 2: D3 — Dispatch Concurrency Cap

**Rationale:** D3 is the direct cause of the OOM that halted run #2b. Independent of D1/D2. Requires an explicit design decision on the D3 fork before planning can proceed.

**Delivers:**
- In-flight planner Job count bounded by `plannerConcurrency` at steady state (not just at Job creation time)
- Default `plannerConcurrency` reduced to a safe single-node value (3 or 4 — resolve in planning)
- Deferred dispatches observable (log line minimum; Prometheus counter recommended)
- Separately-sized planner and executor pools preserved; executor pool unchanged
- Pool capacity vs MaxConcurrentReconciles distinction documented in chart

**Addresses:** D3 from run-2b-FINDINGS.md

**Avoids:**
- P-D3a (PreCharge misses live Jobs on restart) — verify label selector matches `BuildJobSpec` labels
- P-D3b (silent wave truncation) — add deferred-dispatch log line
- P-D3c (pool cap must be < MaxConcurrentReconciles) — document invariant in chart values
- P-D3d (pool unification) — run `make lint` (crosspool analyzer)
- P-D3e (BoundaryDetected stalls on undispatched tasks) — document cap sizing floor

**Research flag: NEEDS DISCUSS-PHASE before implementation.** The D3 design fork must be resolved:

**Option A (STACK.md — 1 of 4 researchers):** `defer r.PlannerPool.Release()` fires on function return, working as designed. Fix is to lower `plannerConcurrency` from 16 to 4 in `values.yaml`. Pool is fully wired; chart default is the only problem.

**Option B (ARCHITECTURE.md, FEATURES.md, PITFALLS.md — 3 of 4 researchers, deeper code reads):** `defer Release()` fires milliseconds after `r.Create(job)`, not on Job terminal state. The semaphore caps simultaneous creation calls, not in-flight pod count. 59 goroutines can cycle through the 16-slot pool in rapid succession, creating 59 Jobs before any completes. Fix requires a live `client.List` count of Jobs with `tideproject.k8s/role=planner` label at each dispatch site BEFORE `PlannerPool.Acquire`, returning `ctrl.Result{RequeueAfter: 5s}` when `count >= plannerConcurrency`.

**Evidence weight:** Option B has 3 researchers with deeper code reads. Resolution: set `plannerConcurrency=2`, create 5 Milestone objects, observe whether `kubectl get jobs -l tideproject.k8s/role=planner` shows at most 2 active Jobs or all 5 — one observation closes the fork. Recommendation is Option B (count-based live-Job check) plus the chart default reduction regardless.

---

### Phase 3: D4 — Planner Failure Semantics

**Rationale:** D4 is a correctness guard that prevents false-Succeeded phases from corrupting the planning DAG. Independent of D1/D2/D3. Lower operational severity than D3 but must ship in v1.0.6 to prevent silent planning tree gaps.

**Delivers:**
- Phase/milestone planner `exitCode != 0` with `childCount == 0` marks parent Failed, not Succeeded
- `exitCode == 0` with `childCount == 0` Succeeds (genuine leaf, no regression)
- Shared `isPlannerFailure(out, envReadOK)` helper at phase and milestone controllers
- `patchPhaseFailed`/`patchMilestoneFailed` helpers added or verified present
- `tide resume --retry-failed` is the operator's explicit recovery path (no auto-retry in controller)

**Addresses:** D4 from run-2b-FINDINGS.md

**Avoids:**
- P-D4a (retry storm from Go error return) — use `patchFailed` condition, not error return
- P-D4b (level divergence — guard at phase but not milestone) — shared helper; envtest at all three levels
- P-D4c (leaf regression — `exitCode=0, childCount=0` wrongly fails) — guard requires `exitCode != 0`; ordering: fail check before succeed check

**Research flag:** Standard patterns. Fix shape is unambiguous. Envtest shape fully specified in FEATURES.md.

---

### Phase Ordering Rationale

- D1+D2 must ship first: D2 is a prerequisite for D1's budget halt to evaluate; both are the root of "spent blind" — the headline safety failure
- D3 before D4: D3 requires a design decision and is higher operational severity; D4 is a narrow correctness fix with no design ambiguity
- D3 and D4 are independent: if parallelizing, D4 can ship concurrently with D3's design discussion; D4 has no dependency on D3's resolution
- Relaunch (run #2c) should not start until D1+D2+D3 are shipped AND adequate infrastructure (multi-node or at least 16 GiB VM) is in place

### Research Flags

Needs discuss-phase before implementation:
- **Phase 2 (D3):** The D3 design fork (pool semantics vs count-based check) must be resolved explicitly. A single `kubectl get jobs` observation on the existing `kind-tide-dogfood` cluster with `plannerConcurrency=2` and 5 Milestones would close it definitively. Resolve in a Phase 2 discuss step before writing the implementation plan.

Standard patterns (no research-phase needed):
- **Phase 1 (D1+D2):** Fix shape fully specified. Primary risk is idempotency edge cases (P-D2a, P-D1a/c) documented in PITFALLS.md; address via targeted envtest requirements in planning.
- **Phase 3 (D4):** Fix shape fully specified. The `isPlannerFailure` helper and ordering constraints are clear. Envtest shapes enumerated in FEATURES.md.

---

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All findings from direct code inspection; go.mod confirmed; zero new dependencies |
| Features (D1, D2, D4) | HIGH | Root causes pinned to specific files and line numbers; fix shapes unambiguous |
| Features (D3) | MEDIUM | Mechanism is code-confirmed but fix shape has a genuine divergence across researchers |
| Architecture (D1, D2, D4) | HIGH | Component boundaries clear; existing patchFailed/patchSucceeded helpers verified |
| Architecture (D3) | MEDIUM | In-flight-count vs creation-rate dispute needs one empirical test or code-archaeology to close |
| Pitfalls | HIGH | All pitfalls are code-grounded; idempotency and TTL-GC patterns sourced from Phase 27 lessons already in this codebase |

**Overall confidence:** HIGH for D1, D2, D4. MEDIUM for D3 pending fork resolution.

### Gaps to Address

- **D3 fix shape (design fork):** Resolve before planning Phase 2. Either run a targeted cluster test (observe active Job count vs pool depth with `plannerConcurrency=2` and 5 Milestones), or resolve in the Phase 2 discuss step. Do not begin D3 implementation until resolved.

- **D-11 scope at child levels:** During D1 implementation, grep all `budget.RollUpUsage` call sites for `if project.Spec.ImportSource != nil { skip }` guards at the milestone, phase, and plan controllers. FEATURES.md expects these guards do not exist at child levels (rollup is unconditional); confirm by inspection. If found, remove guards that suppress child-level rollup — they are new spend, not prior-run spend.

- **`MilestoneRolledUpUID` / `PhaseRolledUpUID` markers:** During D1 implementation, check whether the Phase 27 `PlannerRolledUpUID` idempotency pattern was extended to child levels. If not, add it. This is a concrete gap with a documented prior-art fix shape in the codebase.

- **`patchPhaseFailed` / `patchMilestoneFailed` existence:** During D4 implementation, verify these helpers exist. If absent, add them by mirroring `patchPlanFailed` in `plan_controller.go:842`. ARCHITECTURE.md marks this as "NEW or VERIFIED."

- **Default `plannerConcurrency` value:** ARCHITECTURE.md recommends 3; STACK.md and FEATURES.md recommend 4. Resolve in the D3 planning step to one canonical value with documented rationale.

---

## Sources

### Primary (HIGH confidence — direct code inspection, line numbers verified)

- `internal/controller/project_controller.go` — adoption guard (`reconcileProjectPlannerDispatch` lines ~1088–1133); D-11 suppression (L1304–1307); lifecycle advance sites (L1203–1215); `handleBudgetGate` (L1367–1464)
- `internal/controller/phase_controller.go` — `handleJobCompletion` (L467–648); `setBillingHaltIfNeeded` pattern (L525); ChildCount succession / false-Succeeded arm (L590–597); pool acquire (L379–384)
- `internal/controller/milestone_controller.go` — `handleJobCompletion` (L517–728); `budget.RollUpUsage` (L587–590); false-Succeeded arm (L670–677); pool acquire (L381–386)
- `internal/controller/plan_controller.go` — `handlePlannerJobCompletion` (L508–740); Phase-30 childless guard (L692); `patchPlanFailed` (L842)
- `internal/controller/import_controller.go` — `succeedImport` (L701–706); `AnnotationRetryImport` reset path
- `internal/pool/pool.go` — `Pool.Acquire`/`Release`/`PreCharge` (full file)
- `internal/budget/tally.go` — `RollUpUsage` (L56–89)
- `internal/config/config.go` — `PlannerConcurrency` default 16; `ExecutorConcurrency` default 4
- `charts/tide/values.yaml` — `plannerConcurrency: 16`, `executorConcurrency: 4` (L78–79)
- `cmd/manager/main.go` — pool construction and wiring (L343–353, 445, 475, 501, 529, 547)
- `.planning/dogfood/run-2b-FINDINGS.md` — authoritative D1–D4 defect definitions and run outcome

### Secondary (MEDIUM confidence — operator-space precedents from training knowledge)

- Argo Workflows controller ConfigMap pattern for concurrency caps — supports chart-value surface for D3
- Tekton Pipelines `config-feature-flags` ConfigMap — same surface decision
- Kueue `ClusterQueue` CRD, Volcano `Queue` CRD — considered and rejected for D3 (adds external dependency)

---

*Research completed: 2026-06-28*
*Ready for roadmap: yes — with D3 design fork flagged for Phase 2 discuss step*
