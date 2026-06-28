# Phase 31: D2+D1 — Adoption Lifecycle Seam - Context

**Gathered:** 2026-06-28
**Status:** Ready for planning

<domain>
## Phase Boundary

One shared seam in `reconcileProjectPlannerDispatch` (`internal/controller/project_controller.go`): an adopted/imported Project advances `Initialized → Running` once `ImportComplete=True` **without** dispatching a project-planner Job (D2), which unblocks the budget meter so spend accrues and `absoluteCapCents` enforces on the adoption path (D1). Guarded against (a) project-planner re-dispatch on a cold-cache restart and (b) double-counting rollup after reporter-Job TTL-GC.

**In scope:** ADOPT-01..05 — the D2 lifecycle advance, the D1 budget rollup/halt under adoption, durable suppression of the project-planner, exactly-once child-level rollup, and non-regression of the normal (non-import) lifecycle.

**Out of scope (other phases):** D3 dispatch concurrency cap (Phase 32 — owns all `values.yaml` / pool changes), D4 planner failure semantics (Phase 33). The research declares Phase 31's core fix shape **unambiguous** — no research-phase needed.

</domain>

<decisions>
## Implementation Decisions

### Durable project-planner suppression (P-D2a)
- **D-01:** Make suppression durable via a **dedicated `.status` condition** — a new `ConditionProjectPlannerSuppressed` (Reason=`AdoptionComplete`), checked as a short-circuit **before** the live `r.List` of owned Milestones. Rationale: this matches the load-bearing TIDE convention (`ConditionBillingHalt`, `ConditionBudgetBlocked`, `ConditionImportComplete` in `api/v1alpha1/shared_types.go` are all durable, operator-visible, dispatch-suppressing conditions). It survives manager restart because it lives in `.status`, and is self-documenting in `kubectl describe` — unlike a cache-dependent List.
- **D-02:** The existing live-`r.List` adoption check (`project_controller.go:1105-1133`) becomes the **trigger** that first stamps the condition; the condition then becomes the **authoritative** short-circuit. Do not delete the List logic outright — it's how the condition gets stamped the first time the tree is confirmed present.

### Child-level rollup idempotency (P-D1a / P-D1c)
- **D-03:** Extend the Phase-27 project-level `PlannerRolledUpUID` pattern downward with **per-level scalar markers**: `MilestoneRolledUpUID` and `PhaseRolledUpUID` on each level's `.status.budget`, each stamped after its own rollup and checked before re-counting. Rationale: convention-aligned with the existing marker, smallest conceptual leap, independently auditable per level. Rejected a shared/generic dedup abstraction (harder to map to a level when auditing, diverges from the established scalar pattern).
- **D-03a (planner check):** Before adding a third marker, grep whether the **plan level** already grew an equivalent marker in Phase 27/30 — reuse if present, add only what's missing.

### Lifecycle-advance scope
- **D-04:** Set `Phase = Running` **identically to the normal lifecycle** — no "adopted"/import distinction on the transition. Everything keyed off project phase (budget gate, rollup, dispatch) fires the same way; zero new branching. `ConditionImportComplete` already distinguishes imported projects for any consumer that needs to tell them apart. The "Adopted" dashboard badge (OBS-02) is explicitly **deferred to v2** — do not pull it forward.

### Verification gates (all mandatory — block the phase if red)
- **D-05:** All five success criteria are **blocking** envtest gates:
  1. ADOPT-01 — adopted Project advances `Initialized→Running` with **zero** `role=project-planner` Jobs.
  2. ADOPT-02/03 — `CostSpentCents`/`TokensSpent` accrue as child planners complete; `absoluteCapCents` fires `BudgetBlocked` and stops new dispatch.
  3. ADOPT-04 — a reconcile after the 300s reporter-Job GC window does **not** re-increment `CostSpentCents` for the same Job (exercises the per-level `RolledUpUID` markers).
  4. ADOPT-05 — manager restart on an adopted-but-Running Project does **not** re-dispatch the project-planner (exercises the durable suppression condition).
  5. ADOPT-05 — a normal **non-import** Project still dispatches its project-planner and advances normally (no regression).

### Anti-pattern guardrails (HARD constraints — verified against current code)
- **D-06 (no pool-slot leak):** Both new short-circuits (Phase-advance + suppression-condition stamp) **must `return` before `PlannerPool.Acquire` at `project_controller.go:1137`**. The code already documents this exact pitfall at line 1079 ("parking after acquire leaks a slot") — preserve the ordering.
- **D-07 (single status write):** Batch the `Phase=Running` advance **and** the suppression-condition stamp into **one** `r.Status().Patch(ctx, obj, client.MergeFrom(base))` — not two sequential patches that can conflict on the second resourceVersion.
- **D-08 (no retry storm):** Budget-halt and rollup-skip paths return `ctrl.Result{}, nil` (or `RequeueAfter`), **never** a Go `err`, for expected/permanent states — mirrors `failure_halt.go` / `billing_halt.go`.
- **D-09 (convention):** Use `Status().Patch` + `MergeFrom` (dominant pattern, ~17 sites in `project_controller.go`) and write all conditions through `meta.SetStatusCondition` / `meta.FindStatusCondition`. Do **not** add to the small legacy pile of `Status().Update` sites.
- **D-10 (preserve D-11 suppression):** The existing project-level `PlannerRolledUpUID`-gated suppression of prior-run project-planner spend must be preserved **exactly** — the child-level markers are additive, not a replacement.
- **D-11 (no chart churn):** Touch **no** `charts/tide/values.yaml` — concurrency/chart changes belong to Phase 32 (D3). FIXED contract.

### Soundness summary
This is a **corrective** phase. It removes two genuine anti-patterns currently in the codebase — (1) a correctness decision driven by a live `List`/informer cache (cache-as-truth), and (2) a non-idempotent reconcile (child-level rollup double-counts across TTL-GC). With D-06..D-09 baked into the plan it introduces none.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Defect definitions & research
- `.planning/dogfood/run-2b-FINDINGS.md` — authoritative D1–D4 defect definitions and the run #2b outcome (the OOM + spent-blind failure this phase closes).
- `.planning/research/SUMMARY.md` §"Phase 1: D2 + D1" — fix-shape synthesis, pitfalls, and idempotency gaps to close.
- `.planning/research/PITFALLS.md` — P-D2a (re-dispatch on cache miss), P-D1a / P-D1c (TTL-GC double-count), P-D2b (normal-lifecycle regression).
- `.planning/research/FEATURES.md` — D1/D2 behavioral specs and envtest shapes.
- `.planning/research/ARCHITECTURE.md` — component-boundary notes (which functions change; helpers to mirror).

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §"Adoption Lifecycle & Cost Rollup (D1 + D2)" — ADOPT-01..05.
- `.planning/ROADMAP.md` §"Phase 31" — goal + 5 success criteria.

### Source files this phase edits / mirrors
- `internal/controller/project_controller.go` — `reconcileProjectPlannerDispatch` (adoption guard L1105-1133; pool acquire L1137; D-11 suppression / `PlannerRolledUpUID` L1294-1318); `handleBudgetGate` budget halt.
- `internal/controller/milestone_controller.go` — `budget.RollUpUsage` site (~L587); add `MilestoneRolledUpUID` dedup.
- `internal/controller/phase_controller.go` — rollup site; add `PhaseRolledUpUID` dedup.
- `internal/controller/plan_controller.go` — check for an existing plan-level rollup marker (Phase 27/30) before adding one.
- `api/v1alpha1/shared_types.go` — condition-name convention (`ConditionBillingHalt`/`ConditionBudgetBlocked`/`ConditionImportComplete`); add `ConditionProjectPlannerSuppressed`.
- `api/v1alpha1/*_types.go` (budget status structs) — add `MilestoneRolledUpUID` / `PhaseRolledUpUID` fields next to `PlannerRolledUpUID`.
- `internal/budget/tally.go` — `RollUpUsage` (unchanged; the markers gate its invocation).
- `internal/controller/billing_halt.go` / `failure_halt.go` / `budget_blocked.go` — reference patterns for condition-patch + `nil`-error halts (do not return `err`).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`PlannerRolledUpUID` (project budget status) + its stamping at `project_controller.go:1316`** — the exact prior-art pattern to mirror for `MilestoneRolledUpUID`/`PhaseRolledUpUID`.
- **`meta.SetStatusCondition` / `meta.FindStatusCondition`** — used at 20+ sites; the only way conditions are written/read here.
- **`setBudgetBlockedIfNeeded` + `ConditionBudgetBlocked`** (`budget_blocked.go`, `shared_types.go:259`) — the D1 halt enforcement reuses this **existing** mechanism; D1 does not invent a new halt, it just needs `Phase==Running` (D2) so the gate evaluates.
- **`ConditionImportComplete`** (`project_controller.go:1081,1106`) — the existing import-done gate the new suppression condition sits beside.

### Established Patterns
- **`Status().Patch(MergeFrom(base))`** is the dominant write pattern (conflict-resistant optimistic concurrency) — new writes follow it; legacy `Status().Update` sites are not extended.
- **Halt/gate as a durable `.status` condition checked at the top of reconcile** — `BillingHalt`, `BudgetBlocked`, `ImportComplete`; the new suppression condition joins this family.
- **Pitfall-documented ordering:** suppression/parking returns happen **before** `PlannerPool.Acquire` (comment at `project_controller.go:1079`).

### Integration Points
- D2's status patch (Phase advance + suppression condition) goes into the adoption-guard arm of `reconcileProjectPlannerDispatch`, before pool acquire.
- D1's rollup markers gate the `budget.RollUpUsage` invocation in the milestone/phase controllers' `handleJobCompletion`.
- The budget halt gate (`handleBudgetGate`) already exists and only needs `Phase==Running` to fire — no new dispatch-gate code.

</code_context>

<specifics>
## Specific Ideas

- Persistence stays **CRD-`.status`-only** and **minimal/re-derivable** (spec §resumption) — the new markers are scalar Job-UID strings + one condition, not aggregates; they do not violate the no-`Schedule`/no-`Waves[]` aggregate guard.
- Every new piece of state must be **operator-legible** (`kubectl describe` shows the suppression condition and the reason).

</specifics>

<deferred>
## Deferred Ideas

- **OBS-02 — dashboard "Adopted" badge** distinguishing imported vs freshly-planned nodes → v2 (explicitly deferred in REQUIREMENTS.md; do not pull forward in D-04).
- **D3 dispatch concurrency cap + `values.yaml` `plannerConcurrency` default** → Phase 32 (carries the mandatory Option A/B design fork).
- **D4 planner failure semantics** (`isPlannerFailure` at phase/milestone) → Phase 33.
- **Per-Project concurrency CRD field / Prometheus pool-saturation gauge / auto-retry-with-backoff** → research SUMMARY "Defer (v2+)".

None of these are in Phase 31 scope.

</deferred>

---

*Phase: 31-d2-d1-adoption-lifecycle-seam*
*Context gathered: 2026-06-28*
