# Phase 52: Per-Level LoopPolicy Parameterization - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-20
**Phase:** 52-per-level-looppolicy-parameterization
**Areas discussed:** Per-level schema placement & resolution precedence, LoopPolicy loop-level field + gate-policy resolver, Plan-check loop mechanics, Level-verify escalation & onExhaustion differentiation, Per-level verifier prompt templates
**Mode:** `--auto` — all areas auto-selected, every question resolved to the
recommended option without interactive prompts. No Option-A-vs-B fork with "no
existing source" surfaced (the CLAUDE.md bar for an interactive checkpoint
under auto-advance) — every call extends a Phase-49/51-locked precedent.

---

## Per-level schema placement & resolution precedence

| Option | Description | Selected |
|--------|-------------|----------|
| `Plan.Spec.verification` + Project per-level defaults map (Gates-style) | The exact homes Phase 51's D-01 named; Phase/Milestone resolve from Project scope like `Gates` already does; Task > Plan > Project precedence | ✓ |
| `VerificationSpec` on every CRD (incl. `Phase.Spec`/`Milestone.Spec`) | Symmetric but contradicts D-01's named shape and duplicates what the Project map covers | |
| Single flat `Project.Spec` map only (no `Plan.Spec` field) | Loses the planner-authored per-Plan contract (plan-check's rubric needs plan-specific pass criteria) | |

**Choice:** Recommended option (auto). Grounded in Phase 51 D-01's literal wording and the `Gates`/`ResolveProvider` precedents.

---

## LoopPolicy loop-level field + gate-policy resolver

| Option | Description | Selected |
|--------|-------------|----------|
| Add `Level` enum to `LoopPolicy` + one `ResolveLoopPolicy` function | SC3's "loop-level field on LoopPolicy" is literal; one resolver maps level → defaults (task:N / plan:1 / phase\|milestone\|project:0) and merges explicit config; all five controllers call it | ✓ |
| Derive level from owning CRD kind at call sites | Violates SC3 ("not from CRD kind/hierarchy position"); keeps per-controller switches | |

**Choice:** Recommended option (auto). SC3 mandates it.

---

## Plan-check loop mechanics

| Option | Description | Selected |
|--------|-------------|----------|
| Verify after child materialization; gate Wave-1 dispatch on APPROVED; re-plan = same template + findings; severity-weighted stall halt off `LastEvaluation`; own `LoopStatus` counter | Rubric needs authored child specs; CRDs are free, dispatch spends; mirrors Task's Verifying + evidence-packet precedents | ✓ |
| Verify before child materialization | Rubric's dependency-correctness/verification-derivability checks can't see child specs; fights the reporter seam | |
| Reuse planner `Attempt` as the re-plan counter | Conflates infra-retry with quality-iteration (violates the Phase 51 D-05 distinction ESC-01 re-asserts: "its own counter") | |

**Choice:** Recommended option (auto). Research FEATURES.md re-plan pseudocode + Phase 51 precedents; `REJECT` vocabulary and `maxAttempts:2` recommendation in that research noted as superseded (Phase 49 verdict enum; scoping decision N=1).

---

## Level-verify escalation & onExhaustion differentiation

| Option | Description | Selected |
|--------|-------------|----------|
| Trigger pre-Succeeded/pre-push; any non-APPROVED escalates via resolver; `requireApproval` → existing AwaitingApproval/`consumeApproveAndResume` gate, `escalate` → `ConditionVerifyHalt`; uniform across all levels incl. Task | ESC-02 says requireApproval goes "through the existing gate machinery"; closes `task_types.go:145`'s explicitly-deferred per-value gap in one place | ✓ |
| Both values keep resolving to VerifyHalt (status quo) | Leaves the Phase-51 "declared-but-uniform" gap open — the differentiation IS this phase's deliverable | |
| Mint a new halt class for requireApproval | Violates the additive-halt-class pattern (Billing→Failure→Verify); approval already has machinery | |

**Choice:** Recommended option (auto).

---

## Per-level verifier prompt templates

| Option | Description | Selected |
|--------|-------------|----------|
| Four per-level `<level>_verifier.tmpl` files (plan carries the goal-backward rubric) | Zero loader changes (`LoadPromptTemplate(role, level)` convention); per-level content genuinely differs (rubric vs observable-outcome) | ✓ |
| One generic verifier template parameterized by level | Fights the established `<level>_<role>.tmpl` convention; rubric vs outcome prompts share little | |

**Choice:** Recommended option (auto). Coverage-not-conservatism carried to all four (EVAL-04 / Opus-4.8 tuning note).

---

## Claude's Discretion

- Exact field names/JSON tags for the Project per-level map and `LoopPolicy.Level`; resolver signature + package home.
- Plan's Verifying representation (reuse `LevelPhaseVerifying` vs sibling constant).
- Severity-weighting scheme for stall detection (strictly-decreasing requirement fixed).
- Child-Task reconciliation mechanics on re-plan (no-stale-dispatch invariant fixed).
- `LoopStatus` embedding extent for phase/milestone/project statuses; EVALUATOR-span attribute details.

## Deferred Ideas

- Chart config surface + default posture → Phase 53 (CFG-01/02).
- Dashboard provenance + VerifyHalt visual state → Phase 53 (OBS-04).
- Integration-check beyond a project-level contract → Phase 53+/future.
- Risk/confidence/history-based gate resolution → Oversight-loop arc.
- Composite evaluators → named future arc.
- Todos reviewed, not folded: `cache-f1-direct-sdk-cross-pod-caching` (0.4, keyword false-positive; vNext+ per STATE.md, third consecutive phase reviewing it), `2026-07-03-signed-commits-verified-badge` (0.2, below threshold).
