---
phase: 16-telemetry-completion
plan: "05"
subsystem: dashboard-ui
tags: [view-switcher, telemetry, header, app-wiring, D-01, TELEM-02]
dependency_graph:
  requires: ["16-04"]
  provides: ["TELEM-02-mount-half"]
  affects: ["dashboard/web/src/App.tsx", "dashboard/web/src/components/Header.tsx"]
tech_stack:
  added: []
  patterns: ["tablist/tab ARIA semantics", "roving focus (ArrowLeft/ArrowRight)", "conditional render body branch"]
key_files:
  created:
    - dashboard/web/src/components/__tests__/view-switcher.test.tsx
  modified:
    - dashboard/web/src/App.tsx
    - dashboard/web/src/components/Header.tsx
decisions:
  - "ViewSwitcher defined as a local function component above App (PaneHeader-style) — keeps state ownership in App and avoids a new file for a single-use component"
  - "No vi.useFakeTimers() in test beforeEach — waitFor() needs real setTimeout; TelemetryView's 60s polling kept inert by unavailable fetch stub instead"
  - "fetch stub routes /api/v1/query_range to {status: unavailable} so TelemetryView renders in degraded state without SVG assertions needed in switcher tests"
metrics:
  duration: "~20 minutes"
  completed_date: "2026-06-12T21:24:03Z"
  tasks_completed: 2
  files_changed: 3
---

# Phase 16 Plan 05: View Switcher + TelemetryView Wiring Summary

Header DAGs|Telemetry segmented view switcher with tablist semantics, ArrowLeft/ArrowRight roving focus, full-width TelemetryView conditional body branch, and App-level Vitest coverage — all DAGs view and Phase 15 right-pane logic byte-for-byte untouched.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Header viewSwitcher slot + App view state, ViewSwitcher control, telemetry body branch | 4bc69c0 | App.tsx, Header.tsx |
| 2 | App-level Vitest — default view, tab switching, DAGs passthrough | b79a136 | view-switcher.test.tsx |

## What Was Built

### Task 1: Header + App wiring (D-01)

**Header.tsx** gains an optional `viewSwitcher?: ReactNode` slot (same pattern as `projectPicker`) rendered in the left cluster immediately after `{projectPicker}`.

**App.tsx** additions (all additive — zero edits inside existing two-pane body JSX):
- `ActiveView` type and `const [activeView, setActiveView] = useState<ActiveView>("dags")` — transient, no localStorage
- `ViewSwitcher` local function component above `App` with:
  - Container `role="tablist"` `aria-label="Dashboard view"` `data-testid="view-switcher"`
  - Two `<button type="button" role="tab">` segments in order: `DAGs` (`data-testid="view-tab-dags"`) then `Telemetry` (`data-testid="view-tab-telemetry"`)
  - `aria-selected` reflects active tab; `onKeyDown` handles `ArrowRight`/`ArrowLeft` with `element.focus()` for roving focus
  - Visual contract: active = `var(--color-surface-overlay)` bg + `var(--color-text-primary)` text; inactive = transparent + `var(--color-text-muted)` (UI-SPEC §C1)
- `else if (activeView === "telemetry")` branch before the two-pane else: renders `<TelemetryView projects={projects} selectedProject={selectedProject} />` full-width, no grid
- `import TelemetryView from "./components/TelemetryView"` 
- `viewSwitcher={<ViewSwitcher activeView={activeView} onChange={setActiveView} />}` passed to `<Header>`

### Task 2: view-switcher.test.tsx (App-level)

334-line Vitest file with 4 passing tests:
1. Default view is DAGs: `view-tab-dags` aria-selected=true; `telemetry-view` absent; `running-waves-view` present
2. Switch to Telemetry: click `view-tab-telemetry` → `telemetry-view` present; `running-waves-view` absent; tab selected
3. Switch back: click `view-tab-dags` → `running-waves-view` restored; `telemetry-view` gone; DAGs tab selected
4. Keyboard: `ArrowRight` on `view-tab-dags` → `view-tab-telemetry` aria-selected + `telemetry-view` mounted

Harness: `vi.mock("@xyflow/react")` (useNodesInitialized stub), `vi.mock("recharts")` (ResponsiveContainer height shim), hook stubs for `useProjects`/`useTasks`/`useTaskDetail`, URL-routing fetch stub (`/api/v1/query_range` → unavailable; anything else → project detail), `FakeEventSource` for jsdom.

## Verification

- `dashboard/web/src/App.tsx` TypeScript: clean
- `dashboard/web/src/components/Header.tsx` TypeScript: clean
- `npm test` (full suite, 26 test files): 196/196 passing (192 pre-existing + 4 new)
- Phase 15 regression guard (RunningWavesView.test.tsx, App.test.tsx): all passing
- TELEM-02 suite (TelemetryView.test.tsx, 16 tests): all passing

## Deviations from Plan

None — plan executed exactly as written.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes. The ViewSwitcher writes React state only; no new fetch calls; no localStorage. Consistent with T-16-13 and T-16-14 threat dispositions (both `accept` / `mitigate` via state-only writes and conditional render).

## Known Stubs

None — all wiring is live. TelemetryView (16-04) is fully implemented and mounted; the DAGs body is the pre-existing two-pane implementation unchanged.

## Self-Check: PASSED

- `dashboard/web/src/components/__tests__/view-switcher.test.tsx` — FOUND
- `dashboard/web/src/App.tsx` (modified) — FOUND
- `dashboard/web/src/components/Header.tsx` (modified) — FOUND
- Commit 4bc69c0 — FOUND (`git log --oneline 674222ff..HEAD`)
- Commit b79a136 — FOUND (`git log --oneline 674222ff..HEAD`)
