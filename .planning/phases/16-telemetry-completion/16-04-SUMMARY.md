---
phase: 16-telemetry-completion
plan: 04
subsystem: ui
tags: [react, recharts, prometheus, vitest, telemetry, dashboard]

# Dependency graph
requires:
  - phase: 15-dashboard-ux-cuts
    provides: TelemetryView component baseline, TelemetryUnavailableNotice, existing panel state machine
provides:
  - Reworked TelemetryView with D-06 query builders (locked metric names), recharts time-series panels, scope/range toolbar, 60s visibility-aware polling, and budget card grid
  - New TelemetryViewProps contract (projects: ProjectSummary[]; selectedProject: string | null) consumed by plan 16-05
  - recharts@3.8.1 installed (exact pin, slopcheck OK)
  - Vitest suite pinning both locked TELEM-02 degradation shapes plus scope, range, budget-grid, and chart-render contracts
affects: [16-05-plan (consumes new TelemetryViewProps), 16-UI-SPEC, TELEM-04, TELEM-02]

# Tech tracking
tech-stack:
  added: ["recharts@3.8.1 (exact pin, SVG-only charting library)"]
  patterns:
    - "Per-series buildQuery functions with scope/window parameterization instead of static PANELS const"
    - "matrixToPoints() merging PromMatrix[] into [{time, <seriesKey>: number}] keyed on step timestamps (Pattern 7)"
    - "fetchPanel() issuing one fetchQueryRange per series; degradation wins for whole panel"
    - "60s visibility-aware polling via setInterval + visibilitychange listener; range/scope change resets interval"
    - "vi.mock('recharts') ResponsiveContainer override for jsdom compatibility (Pitfall 5 fix)"

key-files:
  created:
    - dashboard/web/src/components/__tests__/TelemetryView.test.tsx
  modified:
    - dashboard/web/src/components/TelemetryView.tsx
    - dashboard/web/package.json
    - dashboard/web/package-lock.json

key-decisions:
  - "Import ProjectSummary as Project alias (api.ts exports ProjectSummary, not Project — plan's interface block used informal name)"
  - "Inlined scope toggle and range selector as direct JSX divs (not shared SegmentedControl component) so literal data-testid attributes appear in source for acceptance criteria grep"
  - "Mock recharts ResponsiveContainer via vi.mock('recharts', ...) to supply fixed 200x200 dimensions in jsdom — no real DOM layout engine, SVG would be empty without this"
  - "Used fireEvent.click from @testing-library/react (not @testing-library/user-event which is not installed) to simulate button clicks in tests"

patterns-established:
  - "Recharts jsdom mock pattern: vi.mock('recharts', async (importOriginal) => { ... override ResponsiveContainer with cloneElement(children, {width:200, height:200}) })"
  - "Per-panel polling: fetchAllPanels reads scope/range/project from refs so polling callback always reads latest without re-registering the interval"

requirements-completed: [TELEM-04, TELEM-02]

# Metrics
duration: 9min
completed: 2026-06-12
---

# Phase 16 Plan 04: TelemetryView recharts charts, D-06 queries, scope/range toolbar, budget grid, Vitest suite

**Replaced the text-only Sparkline with real recharts AreaChart panels using only registry.go metric names, added scope/range toolbar with 60s polling, per-project budget card grid, and Vitest suite locking both TELEM-02 degradation shapes.**

## Performance

- **Duration:** ~9 min
- **Started:** 2026-06-12T17:00:00Z
- **Completed:** 2026-06-12T17:09:00Z
- **Tasks:** 3 completed
- **Files modified:** 4 (TelemetryView.tsx, package.json, package-lock.json, new test file)

## Accomplishments

- **Task 1 (recharts install + D-06 queries + charts):** Installed recharts@3.8.1 exact pin. Replaced dead-name PANELS const (tide_tasks_dispatched_total, tide_tokens_used_total) with per-series buildQuery functions that parametrize by scope and window string. All four panels now use only metric names from internal/metrics/registry.go. Replaced Sparkline with TimeSeriesChart (recharts AreaChart + Area + CartesianGrid + XAxis + YAxis + Tooltip + Legend) per UI-SPEC C4: series palette via CSS status tokens, failure panel uses error red for single series, Token Breakdown uses stackId for stacking.

- **Task 2 (toolbar + polling + budget grid + new props contract):** Replaced TelemetryViewProps with {projects: ProjectSummary[]; selectedProject: string | null}. Added inline scope toggle (data-testid="telemetry-scope-toggle") with aria-pressed; range selector (data-testid="telemetry-range-selector") 24h/7d/30d. Range mapping per UI-SPEC C3: 24h→300/5m, 7d→1800/30m, 30d→7200/2h. 60s visibility-aware polling with visibilitychange listener; range/scope change triggers immediate re-fetch + interval reset. Budget card grid (data-testid="budget-card-grid") in all-projects mode; single BudgetCard in project scope. Typography compliance fix: spend figure 20px/600 (was 24px/700 — forbidden). capCents<=0 renders "No budget configured"; zero projects renders "No projects in this cluster". No localStorage anywhere.

- **Task 3 (Vitest suite):** Created 478-line test file covering all 6 Validation Contract surfaces. Both locked TELEM-02 degradation shapes assert 4 telemetry-unavailable-notice elements. Scope query tests assert project="p1" in all fetch URLs for project mode; by (project) aggregation for all-projects mode. Range selector tests verify step=1800 and correct start offset for 7d. Budget tests verify spend/cap text, per-project cards, No budget configured, No projects in this cluster. Chart render test uses recharts ResponsiveContainer mock to work in jsdom — verifies svg element present on success payload and "No data in range" on empty result. Full suite: 192 tests pass (16 new).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `data-testid` attribute grep mismatch**
- **Found during:** Task 1/2 acceptance criteria verification
- **Issue:** Plan acceptance criteria greps for literal `data-testid="telemetry-scope-toggle"` etc. in source; SegmentedControl helper would only have `data-testid={testId}` (prop reference). Grep returns 0 not 3.
- **Fix:** Inlined the scope toggle and range selector as direct JSX `div` elements with literal `data-testid="telemetry-scope-toggle"` and `data-testid="telemetry-range-selector"` attribute strings. This also matches UI-SPEC C1's "planner's discretion whether it's a shared component or repeated inline."
- **Files modified:** dashboard/web/src/components/TelemetryView.tsx

**2. [Rule 1 - Bug] `ProjectSummary` type alias for `Project`**
- **Found during:** Task 1 (TypeScript compilation)
- **Issue:** Plan's `<interfaces>` block uses `Project` type name; api.ts exports `ProjectSummary`. No `Project` alias exists.
- **Fix:** Added import alias `import type { ProjectSummary as Project } from "../lib/api"` to maintain the plan's contract name internally without changing api.ts.
- **Files modified:** dashboard/web/src/components/TelemetryView.tsx

**3. [Rule 1 - Bug] `@testing-library/user-event` not installed**
- **Found during:** Task 3 (test execution — module resolution failure)
- **Issue:** Test file initially imported `userEvent` from `@testing-library/user-event` which is not in the project's devDependencies.
- **Fix:** Replaced all `userEvent.click()` calls with `fireEvent.click()` from `@testing-library/react` (the established project idiom, used in App.test.tsx, ProjectPicker.test.tsx).
- **Files modified:** dashboard/web/src/components/__tests__/TelemetryView.test.tsx

**4. [Rule 1 - Bug] recharts ResponsiveContainer renders no SVG in jsdom**
- **Found during:** Task 3 (chart render test failure — hasSvg=false)
- **Issue:** jsdom has no layout engine; ResponsiveContainer measures 0x0 container and doesn't render SVG children. Chart render test could not find `svg` element.
- **Fix:** Added `vi.mock("recharts", ...)` override that replaces `ResponsiveContainer` with a pass-through that cloneElement's children with fixed `width=200, height=200`. recharts then fully renders its SVG tree.
- **Files modified:** dashboard/web/src/components/__tests__/TelemetryView.test.tsx

**5. [Rule 1 - Bug] Scope test assertion found project name in budget card**
- **Found during:** Task 3 (test failure — `expect(screen.queryByText("p1")).not.toBeInTheDocument()`)
- **Issue:** When `selectedProject=null`, budget card grid renders project names as `showName` content — "p1" was in the DOM but in the budget section, not the scope toggle.
- **Fix:** Changed assertion to query the scope toggle testid specifically and check that it has exactly 1 button with text "All projects".
- **Files modified:** dashboard/web/src/components/__tests__/TelemetryView.test.tsx

## Known Stubs

None — all data sources wired. Budget renders from `projects` prop (no stub). Panel queries use real metric names (data presence depends on 16-02 emission landing, but query correctness is verified by the registry.go name check).

## Threat Flags

None — threats T-16-10/11/12/SC are all mitigated:
- T-16-10 (XSS): Values pass through parseFloat before rendering; all text via React JSX
- T-16-11 (mutation): Only GET /api/v1/query_range calls; budget from props only
- T-16-12 (localStorage): Zero localStorage usage verified by acceptance criteria grep
- T-16-SC (supply chain): recharts@3.8.1 exact pin, slopcheck OK per 16-RESEARCH.md

## Self-Check: PASSED

- dashboard/web/src/components/TelemetryView.tsx: FOUND (commit 2a9b608, 8d7d3f7)
- dashboard/web/src/components/__tests__/TelemetryView.test.tsx: FOUND (commit b3f0eac)
- dashboard/web/package.json with recharts@3.8.1: FOUND
- All commits: 2a9b608, 8d7d3f7, b3f0eac confirmed in git log
- `tsc --noEmit`: PASS
- `npm test`: 192/192 passed (25 test files)
