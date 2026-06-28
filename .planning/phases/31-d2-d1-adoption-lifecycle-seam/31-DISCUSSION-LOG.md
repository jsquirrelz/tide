# Phase 31: D2+D1 — Adoption Lifecycle Seam - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-28
**Phase:** 31-d2-d1-adoption-lifecycle-seam
**Areas discussed:** Adoption sentinel durability, Child-level rollup idempotency, Lifecycle-advance scope, Verification gate scope, Codebase soundness / anti-patterns

---

## Adoption sentinel durability (P-D2a)

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated `.status` condition | New `ConditionProjectPlannerSuppressed` (Reason=AdoptionComplete), short-circuit before the live List; convention-aligned, restart-proof, operator-visible | ✓ |
| Reuse `Phase==Running` state | No new field; treat ImportSource!=nil + advanced-past-Initialized as the signal; less self-documenting, couples suppression to lifecycle value | |
| Belt-and-suspenders (List OR sentinel) | Keep live-List guard AND add sentinel; most defensive but two code paths for one job | |

**User's choice:** "Other" → asked "What is the convention in this scenario?" Resolved by grounding in the codebase: every durable dispatch-suppressing signal in TIDE is a `.status` condition (`BillingHalt`, `BudgetBlocked`, `ImportComplete`), so the dedicated condition is the convention-aligned choice. Locked the dedicated condition; live-List becomes the trigger that stamps it.
**Notes:** `PlannerRolledUpUID` (scalar marker) is the other convention but is reserved for exactly-once idempotency, a different job than "should I ever dispatch."

---

## Child-level rollup idempotency (P-D1a / P-D1c)

| Option | Description | Selected |
|--------|-------------|----------|
| Per-level `RolledUpUID` fields | Mirror Phase-27 project pattern: `MilestoneRolledUpUID` + `PhaseRolledUpUID` scalar markers per level; convention-aligned, independently auditable | ✓ |
| Shared generic dedup marker | One generic mechanism / set of rolled-up UIDs; less duplication but a new abstraction, harder to audit per level | |

**User's choice:** Per-level RolledUpUID fields.
**Notes:** Planner must first grep whether plan-level already grew an equivalent marker in Phase 27/30 before adding a third.

---

## Lifecycle-advance scope

| Option | Description | Selected |
|--------|-------------|----------|
| Plain Running, identical | Set Phase=Running exactly as normal lifecycle; zero new branching; ConditionImportComplete already distinguishes imported projects; OBS-02 badge deferred to v2 | ✓ |
| Carry an adopted marker now | Add explicit adopted/import marker on the transition; pulls deferred OBS-02 work forward; extra surface | |

**User's choice:** Plain Running, identical.
**Notes:** —

---

## Verification gate scope

| Option | Description | Selected |
|--------|-------------|----------|
| Adoption advance + no project-planner (ADOPT-01) | Headline D2 behavior | ✓ |
| Budget accrues + cap halts (ADOPT-02/03) | Headline D1 behavior | ✓ |
| TTL-GC double-count guard (ADOPT-04) | Exercises per-level RolledUpUID markers; run #2b-class silent failure | ✓ |
| Restart no-re-dispatch + non-import no-regression (ADOPT-05) | Exercises durable suppression condition + normal-lifecycle non-regression | ✓ |

**User's choice:** All four selected — all five success criteria are mandatory blocking envtest gates.
**Notes:** —

---

## Codebase soundness / anti-patterns

**User's request:** Confirm the work is sound across K8s/Helm/CRDs/controllers/pods and check whether anti-patterns are manifesting — does this work fix or exacerbate them?

**Findings (verified against current code):**
- **Fixes 2 latent anti-patterns:** (1) correctness decision driven by a live `List`/informer cache → moved to durable `.status` condition; (2) non-idempotent reconcile (child-level rollup double-counts across TTL-GC) → per-level `RolledUpUID` markers make it exactly-once.
- **Guardrails to avoid introducing new ones** (now D-06..D-09 in CONTEXT): no pool-slot leak (short-circuit before `PlannerPool.Acquire` at L1137), single batched `Status().Patch` for Phase-advance + condition, no Go-`err` return for expected halts (avoid retry storm), use `Status().Patch`+`MergeFrom` and `meta.SetStatusCondition`.
- **Pre-existing wrinkle (not a blocker, outside seam):** a few legacy `Status().Update` sites coexist with the dominant `Status().Patch` convention; Phase 31 follows Patch.
- **No `values.yaml` churn** — chart is FIXED contract; concurrency belongs to Phase 32.

**Outcome:** Folded the four guardrails into CONTEXT.md as hard constraints (D-06..D-11). Net: a corrective phase that removes two anti-patterns and introduces none.

---

## Claude's Discretion

None — every fork was decided by the user (or resolved by convention at the user's request).

## Deferred Ideas

- OBS-02 dashboard "Adopted" badge → v2.
- D3 dispatch concurrency cap + `values.yaml` `plannerConcurrency` default → Phase 32 (design fork).
- D4 planner failure semantics → Phase 33.
- Per-Project concurrency CRD field / Prometheus pool-saturation gauge / auto-retry-with-backoff → v2+.
