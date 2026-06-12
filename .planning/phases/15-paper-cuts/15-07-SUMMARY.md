---
phase: 15-paper-cuts
plan: "07"
subsystem: dashboard/frontend
tags: [sse, waves, aggregate, running-waves, ui-spec-c3, ui-spec-c4, d13, d14, d16, cuts-06]
one-liner: "RunningWavesView aggregate wave-card view consuming waves.snapshot SSE events as the right-pane default; All waves return affordance; full Vitest coverage per UI-SPEC Validation Contract"
dependency-graph:
  requires: [waves.snapshot-sse-event, KNOWN_STATUS_VALUES-export]
  provides: [CUTS-06-frontend, running-waves-view, all-waves-return, app-pane-swap-d13]
  affects:
    - dashboard/web/src/components/RunningWavesView.tsx
    - dashboard/web/src/components/EmptyState.tsx
    - dashboard/web/src/lib/sse.ts
    - dashboard/web/src/App.tsx
    - dashboard/web/src/App.test.tsx
    - dashboard/web/src/components/__tests__/RunningWavesView.test.tsx
tech-stack:
  added: []
  patterns:
    - "RunningWavesView: own useSSEStream subscription (multi-subscriber pattern); snapshot-replace state; defensive JSON parse"
    - "coerceStatus via KNOWN_STATUS_VALUES (shared guard from 15-05)"
    - "PaneHeader optional action slot for return affordance"
    - "history.replaceState for hash-clear on All waves click"
    - "emitToAll test helper: broadcast to all FakeEventSource instances matching URL (multi-subscriber test pattern)"
key-files:
  created:
    - dashboard/web/src/components/RunningWavesView.tsx
    - dashboard/web/src/App.test.tsx
    - dashboard/web/src/components/__tests__/RunningWavesView.test.tsx
  modified:
    - dashboard/web/src/components/EmptyState.tsx
    - dashboard/web/src/lib/sse.ts
    - dashboard/web/src/App.tsx
decisions:
  - "initialSnapshot=undefined disables the SSE subscription (url='') — mirrors PlanningDAGView.initialData pattern; hasInitialRef prevents prop change from re-enabling SSE in test contexts"
  - "emitToAll test helper broadcasts to all matching FakeEventSource instances because multiple components hold separate subscriptions to the same project-events URL (multi-subscriber pattern)"
  - "history.replaceState(null,'',pathname+search) clears hash symmetrically with window.location.hash set by onPlanClick"
  - "waveIndex is 0-based in the wire contract; wave label renders waveIndex+1 to match WaveBackground WAVE N idiom"
  - "PaneHeader action slot uses ReactNode so the All waves button is a plain <button> element — no new component abstraction needed"
metrics:
  duration: "~25 minutes"
  completed: "2026-06-12"
  tasks_completed: 3
  files_created: 3
  files_modified: 3
requirements: [CUTS-06]
---

# Phase 15 Plan 07: RunningWavesView Frontend (CUTS-06 client half) Summary

RunningWavesView aggregate wave-card view consuming waves.snapshot SSE events as the right-pane default; All waves return affordance; full Vitest coverage per UI-SPEC Validation Contract.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | RunningWavesView + EmptyState variant + sse.ts registration | 1f1bf49 | RunningWavesView.tsx, EmptyState.tsx, sse.ts |
| 2 | App.tsx pane swap + All waves return affordance | 2810e29 | App.tsx |
| 3 | Vitest coverage per the UI-SPEC Validation Contract | f374404 | RunningWavesView.test.tsx, App.test.tsx |

## What Was Built

### Task 1 — RunningWavesView component + EmptyState variant + sse.ts registration

**sse.ts:** `waves.snapshot` appended to `SSE_PROJECT_EVENT_TYPES` via `.concat(["waves.snapshot"])` after the `<kind>.<action>` flatMap. This is the build-blocking integration pitfall noted in UI-SPEC C3 — named SSE events not in the list never reach `onMessage`. A comment marks it as plural-waves to distinguish from `wave.create/update/delete`.

**EmptyState.tsx:** `"no-running-waves"` variant added following the E2/E3 anatomy — 18px/600 heading `"No running waves"`, 14px muted body with the locked Copywriting Contract copy.

**RunningWavesView.tsx:** Implements UI-SPEC C3 verbatim:
- Exported types `RunningWaveTask{name, status}`, `RunningWave{planName, waveIndex, tasks}`, props `{projectName, onPlanClick, initialSnapshot?}`
- Subscribes to `projectEventsURL(projectName)` via `useSSEStream` (own subscription — established multi-subscriber pattern). When `initialSnapshot` is provided, URL is disabled (`""`) to bypass SSE in tests.
- Defensive JSON parse on `waves.snapshot` events; malformed events ignored keeping last good state (T-15-21)
- View states: `null` → `Loader2` spinner (L2 pane-loading); `waves: []` → `EmptyState no-running-waves`; `waves: [...]` → `ALL RUNNING WAVES` header + card list
- Wave card anatomy: `Waves` icon 14px muted `aria-hidden data-icon="Waves"`, plan name `min-w-0 flex-1 truncate` with `title` tooltip, wave label `shrink-0` with locked format `WAVE <N> · <running>/<total> running`
- Chip row `aria-hidden="true"` with `StatusBadge` per task coerced through `KNOWN_STATUS_VALUES` (T-15-22); wrapped in `<span title={task.name} data-testid="wave-card-chip">`
- Card interaction: `role="button"` `tabIndex={0}` click+Enter/Space fires `onPlanClick(planName)`; `data-testid="wave-card-<planName>-<waveIndex>"`, `data-plan`, `data-wave-index`, `aria-label`

### Task 2 — App.tsx pane swap + All waves return affordance

**PaneHeader:** Gains `action?: ReactNode` slot in its prop interface. Label wrapped in `<span>` for flex layout with right-aligned action. PLANNING pane call untouched.

**Right pane:** `selectedPlan === null` branch replaces the old `<div>Select a plan…</div>` with `<RunningWavesView projectName={selectedProject ?? ""} onPlanClick={onPlanClick} />`. The `selectedPlan !== null` branch (ExecutionDAGView) is unchanged.

**All waves button:** Rendered in the EXECUTION PaneHeader `action` slot when `selectedPlan !== null`. Mono 12px/600, `--color-text-muted`, hover `--color-text-primary`, no border. Click sets `selectedPlan = null` and calls `history.replaceState(null, "", pathname+search)` to clear the `#/plan/...` hash symmetrically with how `onPlanClick` writes it. `data-testid="execution-pane-all-waves"`, `aria-label="Show all running waves"`.

**Phase 16 seam:** AppShell, Header, and the grid skeleton untouched.

### Task 3 — Vitest coverage per the UI-SPEC Validation Contract

**RunningWavesView.test.tsx (8 behaviors):**
1. Card content: plan name + exact `WAVE N · x/y running` label format
2. One `wave-card-chip` per task (6 chips for 4+2 task fixture)
3. Chip row carries `aria-hidden="true"` on each card's chip container
4. Card click fires `onPlanClick(planName)`
5. Keyboard `Enter` fires `onPlanClick(planName)`
6. `waves: []` renders `No running waves` heading (no role=button cards)
7. No initialSnapshot renders `Loader2` spinner (aria-label `"Loading running waves"`)
8. Malformed JSON event ignored; last good state persists; invalid shape also ignored

**App.test.tsx (4 behaviors):**
1. `RunningWavesView` mounts as right-pane default when `selectedPlan === null`
2. Old `"Select a plan to view its execution DAG"` string is absent (regression guard)
3. Wave card click swaps to ExecutionDAGView + sets `#/plan/<name>` hash
4. `All waves` button returns to aggregate + clears hash

**Test infrastructure decisions:**
- `window.matchMedia` stubbed in App.test.tsx (jsdom doesn't implement it; Header.tsx uses it for theme detection)
- `emitToAll` helper broadcasts to all FakeEventSource instances matching the project URL — necessary because multiple components (PlanningDAGView, RunningWavesView, useTasks, useTaskDetail) each hold their own subscription to the same `project-events` URL

## Test Results

- `npm run test -- --run`: **176 tests, 24 test files, all passed** (164 pre-existing + 8 RunningWavesView + 4 App)
- `tsc --noEmit`: **exit 0, no type errors**

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] dangerouslySetInnerHTML literal in comment tripped XSS guard test**
- **Found during:** Task 1 initial test run
- **Issue:** `RunningWavesView.tsx` file docstring included the literal string `dangerouslySetInnerHTML` in a comment (`no dangerouslySetInnerHTML anywhere…`). The `no-dangerous-html.test.ts` invariant test scans for this literal string and flagged the new component.
- **Fix:** Reworded the comment to `"no raw HTML injection (T-15-20)"` — equivalent meaning, no literal match.
- **Files modified:** `dashboard/web/src/components/RunningWavesView.tsx`
- **Commit:** 1f1bf49 (within same task commit)

No other deviations — plan executed exactly as written.

## Known Stubs

None. The aggregate view is fully wired: `waves.snapshot` SSE events arrive from the server (plan 15-06) and are rendered as cards immediately.

## Threat Flags

No new threat surfaces beyond those in the plan's threat model:
- T-15-20 (XSS via plan/task names): all strings render as React text children or `title` attributes — auto-escaped
- T-15-21 (malformed snapshot DoS): defensive parse ignores malformed events; asserted by Test 8
- T-15-22 (unknown status strings): chip statuses coerce through `KNOWN_STATUS_VALUES`; asserted by design (unknown → Pending)
- T-15-SC (no new packages): `Waves` and `Loader2` icons from pinned `lucide-react` dependency

## Self-Check

| Artifact | Status |
|----------|--------|
| `dashboard/web/src/components/RunningWavesView.tsx` | FOUND |
| `dashboard/web/src/components/EmptyState.tsx` (no-running-waves) | FOUND |
| `dashboard/web/src/lib/sse.ts` (waves.snapshot registered) | FOUND |
| `dashboard/web/src/App.tsx` (pane swap + All waves button) | FOUND |
| `dashboard/web/src/components/__tests__/RunningWavesView.test.tsx` | FOUND |
| `dashboard/web/src/App.test.tsx` | FOUND |
| Commit 1f1bf49 (Task 1) | FOUND |
| Commit 2810e29 (Task 2) | FOUND |
| Commit f374404 (Task 3) | FOUND |
| `npm run test -- --run` | 176 tests, 24 files, all PASS |
| `tsc --noEmit` | exit 0 |

## Self-Check: PASSED
