# Phase 41: Refactoring Review — Non-Breaking Cleanup - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-11
**Phase:** 41-refactoring-review-non-breaking-cleanup-12-items
**Mode:** `--auto` (chained from Phase 40 completion; every question auto-resolved to the recommended option; no user prompts)
**Areas discussed:** seed staleness, constants placement, condition polarity, log-style policy, review-warnings fold, sequencing/plan shape

---

## Seed staleness (Phase 40 landed after the seed)

| Option | Description | Selected |
|--------|-------------|----------|
| Re-verify per item against HEAD | Treat file:lines as hints; researcher re-locates by symbol; drop already-done items | ✓ |
| Trust the seed verbatim | Execute against cited lines | |

**Auto-selection rationale:** Phase 40 renamed api/v1alpha2→v1alpha3 and swept comments; orchestrator greps confirmed item 3 already done, items 1/4/5 still live at shifted line numbers.

## Item 1 — constants placement

| Option | Description | Selected |
|--------|-------------|----------|
| api/v1alpha3 per-kind, string field, no Enum | Follow existing Project pattern; strictly non-breaking | ✓ |
| Add kubebuilder Enum now | CRD schema change — violates the phase's non-breaking boundary | |

## Item 9 — ConditionParentUnresolved polarity

| Option | Description | Selected |
|--------|-------------|----------|
| True == unresolved | Matches type name + Task usage (seed recommendation) | ✓ |
| False == unresolved | Matches milestone/phase current behavior, contradicts the name | |

## Item 12 — log-style policy

| Option | Description | Selected |
|--------|-------------|----------|
| Amend AGENTS.md (lowercase-initial codified) | Zero code churn; load-bearing test/verification greps untouched (seed's own lean) | ✓ |
| Align 47 sites to capital-initial | Mechanical sweep + every grep consumer updated in lockstep | |

## Phase-40 review warnings (6 WR advisory)

| Option | Description | Selected |
|--------|-------------|----------|
| Keep phase scope = seed items; route WRs via /gsd:code-review 40 --fix | No silent scope expansion (scope guardrail) | ✓ |
| Fold all 6 WRs into Phase 41 | +50% scope without user sign-off | |

## Sequencing / plan shape

| Option | Description | Selected |
|--------|-------------|----------|
| Seed's sequencing, item 3 dropped; item 7 one-controller-per-plan | Quick wins 2→5→6→4→1, structural 7→8→10, fixes 9+11, 12 as doc amendment | ✓ |
| Re-derive ordering at plan time | Discards the seed's dependency reasoning | |

## Claude's Discretion

- Plan/wave grouping of the 11 items (respecting locked sequencing)
- Item 6 helper: interface param vs generics
- Item 11 constant placement details

## Deferred Ideas

- Enum validation on Status.Phase (future crank)
- 40-REVIEW.md WR findings (companion work)
- /gsd:secure-phase 40 (outstanding security gate)
- Todo close-out sweep for the five delivered-but-still-pending 2026-07-03 todos (audit, not re-execution)

## Todo cross-reference note

`todo.match-phase` returned 8 matches ≥ 0.4. Auto-fold was overridden by the scope guardrail for 7 of them: 5 were already delivered by phases 34–38/40 (folding = re-execution risk), 1 is the superseded Phase-40 seed, 1 is milestone-level CACHE-F1 work. Only the Phase-41 seed itself was folded. All are documented in CONTEXT.md `<deferred>`.
