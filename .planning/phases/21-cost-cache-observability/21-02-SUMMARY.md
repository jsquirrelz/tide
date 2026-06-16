---
phase: "21"
plan: "02"
subsystem: dashboard
tags:
  - react
  - telemetry
  - cache-observability
  - tdd
  - obsv-03
  - d-06
dependency_graph:
  requires:
    - "21-01"          # tide_cache_savings_cents_total counter (OBSV-02)
  provides:
    - "CacheEfficiencyPanel in TelemetryView grid (OBSV-03)"
    - "BreakdownKind level selector in toolbar (D-06)"
  affects:
    - "dashboard/web/src/components/TelemetryView.tsx"
    - "dashboard/web/src/components/__tests__/TelemetryView.test.tsx"
    - "cmd/dashboard/embed/dist/"
tech_stack:
  added: []
  patterns:
    - "CacheEfficiencyPanel: stat trio (hit%, creation tokens, savings $) + 48px sparkline"
    - "BreakdownKind sum by(<dim>)(...) PromQL aggregation wired across all 5 panels"
    - "Pre-existing TypeScript errors in TimeSeriesChart Tooltip formatters fixed (Rule 1)"
key_files:
  created: []
  modified:
    - "dashboard/web/src/components/TelemetryView.tsx"
    - "dashboard/web/src/components/__tests__/TelemetryView.test.tsx"
    - "cmd/dashboard/embed/dist/assets/index-BEfeN1Kf.js"
    - "cmd/dashboard/embed/dist/assets/index-BJNoTuKK.css"
    - "cmd/dashboard/embed/dist/index.html"
decisions:
  - "CacheEfficiencyPanel fetches 3 range queries (hit-ratio, creation tokens, savings cents) via existing fetchQueryRange; no new fetch primitive needed"
  - "BreakdownKind extends buildQuery signature with optional 4th arg; default='none' preserves backward-compatible behavior"
  - "TimeSeriesChart gets optional height prop (default 180px) to support 48px sparkline without a new component"
  - "isSingleFailure narrowed to boolean via (panelDef.failureColor === true) to fix pre-existing TS error in scope"
metrics:
  duration: "~8 minutes"
  completed_date: "2026-06-16"
  tasks_completed: 2
  files_modified: 5
---

# Phase 21 Plan 02: Dashboard Cache-Efficiency Panel and Level Selector Summary

TDD implementation of the cache-efficiency panel (OBSV-03) and per-level breakdown selector (D-06) in TelemetryView — five panels now visible, all queries support `sum by(<dim>)(...)` aggregation.

## Tasks Completed

| # | Task | Commit | Files |
|---|------|--------|-------|
| RED | Add failing tests: cache-efficiency panel + level selector suites; toHaveLength(4)→5 | `9eb37c0` | TelemetryView.test.tsx |
| 1+2 | Implement CacheEfficiencyPanel, BreakdownKind selector, all panel buildQuery updates, toolbar wiring, TypeScript fixes | `3499011` | TelemetryView.tsx, dist/ |

## What Was Built

### Task 1 — CacheEfficiencyPanel (OBSV-03)

`CacheEfficiencyPanel` sub-component appended as the 5th element in the `data-testid="telemetry-panels"` grid:

- Fetches three PromQL range queries via existing `fetchQueryRange`:
  1. Hit-ratio: `sum(increase(cache_read[w])) / (sum(increase(cache_read[w])) + sum(increase(cache_creation[w])))`
  2. Creation tokens: `sum(increase(tide_tokens_cache_creation_total{project}[w]))`
  3. Savings $: `sum(increase(tide_cache_savings_cents_total{project}[w]))`
- Stat trio: 20px/600/mono figures (hit%, creation tokens abbrev, `formatCents(savings)`) with 12px captions "hit", "creation", "saved"
- Hit-ratio NaN (0/0 PromQL division) renders `"—"` per D-04/UI-SPEC
- 48px sparkline (`TimeSeriesChart` with new `height` prop) for hit-ratio over time using `SERIES_PALETTE[0]` (neutral)
- Degradation: `<TelemetryUnavailableNotice />` on unavailable/unreachable — bumps total notice count from 4 to 5
- Props: `scope`, `project`, `window`, `step`, `startSec`, `endSec`, `range`, `breakdown`

### Task 2 — BreakdownKind Level Selector (D-06)

- `type BreakdownKind = "none" | "phase" | "plan" | "wave"` — TypeScript literal union, never interpolates user strings (T-21-02-01)
- `levelOptions` const with Project/Phase/Plan/Wave options (Phase/Plan/Wave use mono font)
- `breakdown` state + `breakdownRef` (following scopeRef/rangeRef polling pattern)
- All 9 `buildQuery` functions in PANELS extended: when `breakdown !== "none"`, emit `sum by(${breakdown})(...)` aggregation
- `CacheEfficiencyPanel` queries also switch to `sum by(${bd})(...)` when breakdown active
- `fetchPanel` gains optional `breakdown` param; key-derivation reads `matrix.metric[breakdown]` when active
- Toolbar: scope toggle + level selector grouped in left `flex gap-2` cluster; `<SegmentedControl testId="telemetry-level-selector">` placed between scope and range
- Polling effect re-registers on breakdown changes (added to `[scope, range, breakdown]` deps)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed pre-existing TypeScript errors in TimeSeriesChart**
- **Found during:** Task 1 implementation — `npm run lint` was already failing on the original code
- **Issue:** Three pre-existing TS errors: (a) `Tooltip formatter` parameter typed as `number` vs recharts' `ValueType|undefined`; (b) `labelFormatter` typed as `(v: number)=>string` vs `ReactNode=>ReactNode`; (c) `isSingleFailure` type was `boolean|undefined` but `seriesColor` required `boolean`
- **Fix:** Narrowed formatter to `typeof value === "number" ? value : 0`; labelFormatter to `typeof label === "number" ? tickFmt(label) : String(label)`; `isSingleFailure` to `(panelDef.failureColor === true) && !hasMultipleSeries`
- **Files modified:** `dashboard/web/src/components/TelemetryView.tsx`
- **Commit:** `3499011`

**2. [Rule 2 - Missing] Added `height` prop to `TimeSeriesChart`**
- **Found during:** Task 1 — sparkline requires 48px height but `TimeSeriesChart` had hardcoded 180px
- **Fix:** Added optional `height?: number` prop with default `180`; used in both `ResponsiveContainer` and the empty-state div
- **Files modified:** `dashboard/web/src/components/TelemetryView.tsx`
- **Commit:** `3499011`

## Known Stubs

None — the panel fetches live PromQL data; `"—"` for NaN hit-ratio is intentional D-04 behavior, not a stub.

## Self-Check

### Files exist
- `/Users/justinsearles/Projects/tide/dashboard/web/src/components/TelemetryView.tsx` — FOUND
- `/Users/justinsearles/Projects/tide/dashboard/web/src/components/__tests__/TelemetryView.test.tsx` — FOUND

### Commits exist
- `9eb37c0` (RED tests) — FOUND
- `3499011` (GREEN implementation) — FOUND

### Acceptance criteria
- `panel-cache-efficiency` in TelemetryView.tsx: 1 (≥ 1) PASS
- `tide_cache_savings_cents_total` in TelemetryView.tsx: 2 (≥ 1) PASS
- `telemetry-level-selector` in TelemetryView.tsx: 1 (≥ 1) PASS
- `BreakdownKind` in TelemetryView.tsx: 6 (≥ 1) PASS
- `by(phase)` in TelemetryView.tsx: 1 (≥ 1) PASS
- `telemetry-level-selector` in test file: 2 (≥ 1) PASS
- `toHaveLength(4)` in test file: 0 (== 0) PASS
- `toHaveLength(5)` in test file: 3 (≥ 2) PASS
- vitest: 204 passed, 0 failed PASS
- tsc -b: 0 errors PASS
- `make dashboard-frontend`: exit 0 PASS

## TDD Gate Compliance

| Gate | Commit | Status |
|------|--------|--------|
| RED — test(21-02) | `9eb37c0` | PASS — 7 new tests failed before implementation |
| GREEN — feat(21-02) | `3499011` | PASS — all 204 tests pass after implementation |
| REFACTOR | none needed | N/A |

## Threat Flags

None. All new surfaces are within the existing trust boundary (read-only operator dashboard, same PromQL proxy). `BreakdownKind` is a TypeScript literal union — no free-form strings reach PromQL (T-21-02-01 mitigated by construction).

## Self-Check: PASSED
