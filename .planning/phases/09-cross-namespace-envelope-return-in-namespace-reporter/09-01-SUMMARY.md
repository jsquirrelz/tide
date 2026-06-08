---
phase: 09-cross-namespace-envelope-return-in-namespace-reporter
plan: "01"
subsystem: subagent-pricing
tags: [pricing, cost, anthropic, subagent, tdd]
dependency_graph:
  requires: []
  provides: [REQ-09-05]
  affects:
    - internal/subagent/anthropic/subagent.go
    - internal/budget/tally.go
tech_stack:
  added: []
  patterns:
    - Per-model price table (unexported struct + map) keyed on exact resolved model string
    - Ceil-division integer arithmetic for sub-cent rounding
    - Conservative-default + loud-stderr on table miss (T-09-01 / Pitfall 4)
key_files:
  created:
    - internal/subagent/anthropic/pricing.go
    - internal/subagent/anthropic/pricing_test.go
  modified:
    - internal/subagent/anthropic/subagent.go
decisions:
  - "Conservative-tier fallback on price-table miss emits stderr so the operator sees it in Pod logs; never returns 0 (Pitfall 4)"
  - "Ceil division: (numerator + million - 1) / million — zero-token usage for a known model still returns 0 correctly"
  - "usage.EstimatedCostCents assigned immediately before the EnvelopeOut literal so both the success and failure paths (waitErr branch) inherit the populated value"
metrics:
  duration: "8 minutes"
  completed: "2026-06-08"
  tasks_completed: 2
  files_modified: 3
---

# Phase 09 Plan 01: Per-model price table + EstimatedCostCents Summary

Resolved defect #6: the real Anthropic runner never computed `EstimatedCostCents`, so `RollUpUsage` accumulated zero and `Project.Status.budget.costSpentCents` stayed 0 despite real token bills. Added `internal/subagent/anthropic/pricing.go` with a per-model price table and wired it at the `EnvelopeOut` assembly point in `subagent.go`.

## What Was Built

Per-model price table (`pricing.go`) keyed on exact resolved model strings from `examples/projects/medium/project.yaml` and `charts/tide/values.yaml`:

| Model | Input (cents/MTok) | Output (cents/MTok) | Cache Read | Cache Write |
|-------|--------------------|---------------------|------------|-------------|
| claude-haiku-4-5 | 100 | 500 | 10 | 125 |
| claude-sonnet-4-6 | 300 | 1500 | 30 | 375 |
| claude-opus-4-7 | 1500 | 7500 | 150 | 1875 |

`estimatedCostCents(model string, u pkgdispatch.Usage) int64` uses ceil-division so sub-cent token counts round up to 1 cent rather than truncating to 0. On a table miss the function logs loud stderr and falls back to the most-expensive known tier (opus) — never silent 0 (T-09-01 / Pitfall 4).

The wire in `subagent.go` sets `usage.EstimatedCostCents = estimatedCostCents(in.Provider.Model, usage)` immediately before the `EnvelopeOut` literal, so both the success path and the `waitErr` failure path carry the computed cost in `out.Usage`. `RollUpUsage` in `tally.go` is unchanged — it already accumulates `EstimatedCostCents` at `:51`.

## TDD Gate Compliance

- RED commit: `96448b9` — `test(09-01): add failing test for estimatedCostCents price table`
- GREEN commit: `dc9a56e` — `feat(09-01): implement per-model price table + estimatedCostCents`
- No refactor needed.

## Commits

| Task | Commit | Description |
|------|--------|-------------|
| Task 1 (TDD RED) | `96448b9` | test(09-01): add failing test for estimatedCostCents price table |
| Task 1 (TDD GREEN) | `dc9a56e` | feat(09-01): implement per-model price table + estimatedCostCents |
| Task 2 | `eb97faf` | feat(09-01): wire estimatedCostCents into EnvelopeOut assembly |

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None. The price table uses real Anthropic published prices for all three keyed models. The medium-sample DoD model (`claude-haiku-4-5`) is fully priced.

## Threat Flags

No new security-relevant surface introduced. `pricing.go` is a pure in-process computation with no network endpoints, auth paths, or schema changes.

## Self-Check: PASSED

- `internal/subagent/anthropic/pricing.go` exists — FOUND
- `internal/subagent/anthropic/pricing_test.go` exists — FOUND
- `grep -q 'EstimatedCostCents = estimatedCostCents' internal/subagent/anthropic/subagent.go` — FOUND at line 246
- All 10 `TestEstimatedCostCents` subtests pass
- `go test ./internal/subagent/anthropic/... ./internal/budget/... -short -count=1` — both packages PASS
- RED commit `96448b9`, GREEN commit `dc9a56e`, wire commit `eb97faf` — all verified in git log
