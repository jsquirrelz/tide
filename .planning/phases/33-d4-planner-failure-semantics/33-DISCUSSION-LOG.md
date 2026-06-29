# Phase 33: D4 — Planner Failure Semantics - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-29
**Phase:** 33-d4-planner-failure-semantics
**Areas discussed:** Scope: plan/project parity

---

## Gray-area selection

Four gray areas were offered (multiSelect). The user selected **one**: "Scope: plan/project parity". The other three were de-selected and resolved as Claude's discretion (see below).

| Offered area | Description | Selected |
|--------------|-------------|----------|
| Sizing-policy debt | Carried-in D3 default (4) vs chart's "≥ widest wave (6)" comment — raise / soften / document | |
| Failure vocabulary | Failed condition Reason constant + message + new patchPhaseFailed/patchMilestoneFailed helpers | |
| Scope: plan/project parity | Whether the guard also belongs at plan/project, or strictly phase+milestone | ✓ |
| Shared helper shape | isPlannerFailure signature & home | |

---

## Scope: plan/project parity

Evidence presented: phase & milestone false-SUCCEED on a zero-child failed planner (direct `expected==0 → patchSucceeded` shortcut bypassing `BoundaryDetected`). Plan & project are structurally protected from false-succeed because `BoundaryDetected` returns `matched > 0` (false on zero children) — but a zero-child *failed* planner there would instead leave the level stuck `Running` (a latent hung-state, not a DAG corruption). Project also has `setBillingHaltIfNeeded` on `exitCode != 0`.

| Option | Description | Selected |
|--------|-------------|----------|
| Phase+milestone only | Scope strictly to the two exposed levels per roadmap; add a comment documenting why plan/project are excluded (the matched>0 protection) | ✓ |
| Add guard at all 4 levels | Apply isPlannerFailure at plan & project too, converting the latent hung-Running into explicit Failed; wider blast radius, beyond roadmap scope | |
| Phase+milestone + regression test | Fix phase+milestone, add an envtest proving plan/project don't false-succeed, without changing their code | |

**User's choice:** Phase+milestone only.
**Notes:** The fix targets planning-DAG corruption (a failed planner falsely advancing its parent). Project is the root (no parent to corrupt); plan's children are execution Tasks, not planning-DAG nodes — so neither corrupts the planning DAG. The latent plan/project hung-Running state is captured as a deferred idea, not Phase 33 scope.

---

## Claude's Discretion

The three de-selected gray areas, resolved in CONTEXT.md with recommendations the planner may refine:

- **Sizing-policy debt (D-04):** Recommend softening the chart's "≥ widest wave" comment to a per-workload tuning note and documenting that the single-node default (4) intentionally trades throughput for safety — do NOT raise the default (4 was a deliberate Phase-32 safety choice; the inconsistency is in the doc wording, not the value). Docs/comment-only.
- **Failure vocabulary (D-05):** Add a `ReasonPlannerFailed` constant + new `patchPhaseFailed`/`patchMilestoneFailed` helpers mirroring `patchPlanFailed`; concrete operator-facing message naming exitCode + zero-children; permanent Failed (recovery via `--retry-failed`, no auto-retry).
- **Shared helper shape (D-06):** Package-level `isPlannerFailure(out, envReadOK) bool` in a shared controller file, called identically at both sites; the fail-check ordered before the succeed branch (PLANFAIL-03).

## Deferred Ideas

- Converting the latent plan/project hung-Running on a zero-child failed planner into an explicit Failed (belt-and-suspenders, all 4 levels) — out of D4 scope; future hardening candidate if dogfood surfaces a real stall.
- Raising `plannerConcurrency` beyond single-node safety — tied to the vNext multi-node/OpenAI milestone.
