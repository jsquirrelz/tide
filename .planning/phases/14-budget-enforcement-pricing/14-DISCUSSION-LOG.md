# Phase 14: Budget Enforcement + Pricing - Discussion Log

> **Audit trail only.** Decisions are captured in CONTEXT.md.

**Date:** 2026-06-11
**Areas discussed:** Pricing source + overrides, Drift process, Overshoot bounding, Cap surfacing

---

## Pricing source + overrides

| Option | Selected |
|--------|----------|
| Update table + helm overrides | ✓ (with freeform addition: "we need a regular process that keeps the table updated") |
| Update table only | |
| Helm-only pricing | |

**Notes:** Discussion surfaced that the existing opus-4-7 entry is WRONG ($15/$75 vs actual $5/$25, verified against the claude-api reference), not just missing models.

## Drift process (follow-up to the freeform ask)

| Option | Selected |
|--------|----------|
| Scheduled CI drift-check + issue (weekly Action, fetch published pricing page, deduped issue; release-checklist line; no auto-PR) | ✓ |
| CI drift-check fails the build | |
| Release checklist only | |

## Overshoot bounding (BUDGET-03)

| Option | Selected |
|--------|----------|
| Reservation at dispatch (pre-charge estimate; spent + reserved ≥ cap blocks; settle on completion) | ✓ |
| Wave-boundary projection | |
| Kill in-flight at cap | |

## Cap surfacing (BUDGET-02)

| Option | Selected |
|--------|----------|
| Project BudgetBlocked condition + keep existing BudgetExceeded phase machinery | ✓ |
| Phase-only, fix the silent path | |
| You decide | |

## Claude's Discretion

- Root-cause of run-1's silent BudgetExceeded path
- Reservation estimate source + settle/expiry semantics (in-process, rederivable — never CRD-status aggregates)
- Whether tide resume interacts with BudgetBlocked (existing bypass/cap-raise paths preferred)

## Deferred Ideas

- COST-01 prompt-caching strategy, COST-02 provider-key budget on dashboard
- Per-level budget sub-caps
