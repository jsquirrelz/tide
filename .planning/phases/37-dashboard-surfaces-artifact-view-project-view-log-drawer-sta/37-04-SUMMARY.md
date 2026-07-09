---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 04
subsystem: ui
tags: [react, typescript, vitest, a11y, localStorage, resize, focus-trap]

# Dependency graph
requires:
  - phase: 37 (plan 37-03, TaskDetailDrawer lineage)
    provides: TaskDetailDrawer a11y chrome (focus trap, Escape-close, focus-restore, role=dialog) copied wholesale as the NodeDetailPanel base
provides:
  - ResizeHandle — hand-rolled drag/keyboard resize primitive (vertical + horizontal), zero new deps
  - usePersistedSize — localStorage-seeded, clamped-on-read size hook with debounce-free commit()
  - NodeDetailPanel — generalized right-panel shell (kind/name header, left-edge resize, collapse-to-rail, full a11y kit)
  - PlanningNodeKind type export for 37-08 kind-aware click routing
affects: [37-05 (ArtifactViewer mounts as NodeDetailPanel children), 37-08 (kind-aware click routing + App log-area ResizeHandle instance)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Hand-rolled resize primitive reporting px via onChange/onCommit; caller owns the style write (no animation during drag)"
    - "usePersistedSize: try/catch localStorage read seeded into useState, clamped on read; commit() writes on release only (the debounce)"
    - "Leading-edge sign convention: dragging a left/top handle toward the origin GROWS the region"
    - "Stable window pointer handlers via useCallback([]) reading a live propsRef — survives mid-drag re-renders without listener leaks"

key-files:
  created:
    - dashboard/web/src/components/ResizeHandle.tsx
    - dashboard/web/src/components/ResizeHandle.test.tsx
    - dashboard/web/src/components/NodeDetailPanel.tsx
    - dashboard/web/src/components/NodeDetailPanel.test.tsx
  modified: []

key-decisions:
  - "Hand-rolled ResizeHandle instead of react-resizable-panels (SUS-flagged; would force a deferred IDE-layout refactor) — zero new dependency"
  - "70vw width ceiling recomputed at drag time via a window-resize listener, not frozen at mount (UI-SPEC §6 clamp)"
  - "Collapse is internal state that keeps the component mounted (node selection survives); only Escape/backdrop call onClose (D-06 collapse ≠ close)"
  - "TaskDetailDrawer left untouched — NodeDetailPanel is a new component copying its a11y kit, not a refactor of it"

patterns-established:
  - "Pattern: resize primitives report px, callers persist + apply — keeps ResizeHandle layout-agnostic across its two instances"
  - "Pattern: localStorage-backed UI prefs read through try/catch + numeric clamp (tampering mitigation T-37-04-01)"

requirements-completed: [DASH-01]

coverage:
  - id: D1
    description: "ResizeHandle a11y — role=separator, aria-orientation, aria-valuenow/min/max, aria-label, tabIndex=0 for both orientations"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/ResizeHandle.test.tsx#ResizeHandle — a11y"
        status: pass
    human_judgment: false
  - id: D2
    description: "ResizeHandle keyboard operation — arrows ±16px (Left/Right vertical, Up/Down horizontal), Home/End jump to clamp limits, values clamp to [min,max]"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/ResizeHandle.test.tsx#ResizeHandle — keyboard"
        status: pass
    human_judgment: false
  - id: D3
    description: "ResizeHandle pointer drag — onChange during move, onCommit exactly once on release, no-op after release; setPointerCapture guarded"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/ResizeHandle.test.tsx#ResizeHandle — pointer drag"
        status: pass
    human_judgment: false
  - id: D4
    description: "usePersistedSize — localStorage-seeded (clamped), default fallback for absent/non-numeric, commit() writes current value, setValue clamps"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/ResizeHandle.test.tsx#usePersistedSize"
        status: pass
    human_judgment: false
  - id: D5
    description: "NodeDetailPanel dialog chrome — role=dialog aria-modal, <kind>/<name> header, children, close button; Escape + backdrop both close"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/NodeDetailPanel.test.tsx#NodeDetailPanel — dialog chrome"
        status: pass
    human_judgment: false
  - id: D6
    description: "NodeDetailPanel collapse-to-rail — chevron collapses to 32px rail hiding content, rail expands back, collapse never calls onClose, collapsed state persists"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/NodeDetailPanel.test.tsx#NodeDetailPanel — collapse to rail"
        status: pass
    human_judgment: false
  - id: D7
    description: "NodeDetailPanel resize + persistence — left-edge ResizeHandle changes width style, persists to tide.dashboard.panel-width on commit, restores on mount"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/NodeDetailPanel.test.tsx#NodeDetailPanel — resize + persistence"
        status: pass
    human_judgment: false
  - id: D8
    description: "NodeDetailPanel focus management — focus captured into panel on open, restored to previously-focused element on close"
    requirement: "DASH-01"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/NodeDetailPanel.test.tsx#NodeDetailPanel — focus management"
        status: pass
    human_judgment: false
  - id: D9
    description: "Visual/motion fidelity — 6px token-styled track, 12px hit area, 180ms collapse slide, dev-tool palette tokens — rendered appearance not asserted by unit tests"
    requirement: "DASH-01"
    verification: []
    human_judgment: true
    rationale: "Visual chrome (track hover color, cursor, slide easing, rail affordance) is not exercised by jsdom unit tests; needs a human/visual pass in 37-08 integration when the panel is mounted with real content"

# Metrics
duration: 12min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 04: Dashboard Panel Primitives (ResizeHandle + NodeDetailPanel) Summary

**Hand-rolled keyboard/pointer ResizeHandle + usePersistedSize hook, and a generalized NodeDetailPanel right-panel shell (kind/name header, left-edge resize, collapse-to-rail, full focus-trap a11y kit) — zero new dependencies, TaskDetailDrawer untouched.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-07-08T05:24:00Z
- **Completed:** 2026-07-08T05:31:00Z
- **Tasks:** 2 (both TDD)
- **Files created:** 4

## Accomplishments
- `ResizeHandle` — `role="separator"` drag/keyboard resize primitive supporting both vertical (panel-width) and horizontal (log-area-height) orientations; arrow keys ±16px, Home/End jump to clamps, pointer drag via guarded `setPointerCapture` + window listeners, leading-edge sign convention.
- `usePersistedSize(storageKey, defaultPx, min, max)` — localStorage-seeded (clamped on read, try/catch for unavailable storage, numeric-fallback on garbage), returns `[value, setValue, commit]` where `commit()` writes on release only.
- `NodeDetailPanel` — copies the TaskDetailDrawer a11y kit (focus capture/restore, Escape-close, Tab focus-trap, backdrop, `role="dialog"`) into a NEW component, replaces the hardcoded 420px width with `usePersistedSize` (70vw ceiling recomputed at drag time), adds a collapse chevron → 32px rail that keeps the component mounted (collapse ≠ close), and exports `PlanningNodeKind`.
- 20 new unit tests (11 ResizeHandle + 9 NodeDetailPanel); full dashboard suite 232/232 green; `tsc -b` clean; bundle-size gate passes (no new dep).

## Task Commits

Each task was committed atomically (TDD RED → GREEN):

1. **Task 1: ResizeHandle + usePersistedSize** — `5148ceb` (test) → `1274d66` (feat)
2. **Task 2: NodeDetailPanel shell** — `dcdb6f2` (test) → `4fe4f91` (feat)

_TDD tasks carry a test commit then an implementation commit._

## Files Created/Modified
- `dashboard/web/src/components/ResizeHandle.tsx` - Hand-rolled resize primitive + `usePersistedSize` hook.
- `dashboard/web/src/components/ResizeHandle.test.tsx` - 11 tests (a11y, keyboard, pointer, persistence).
- `dashboard/web/src/components/NodeDetailPanel.tsx` - Generalized right-panel shell; exports `PlanningNodeKind`.
- `dashboard/web/src/components/NodeDetailPanel.test.tsx` - 9 tests (chrome, collapse, resize+persistence, focus).

## Decisions Made
- Hand-rolled the resize primitive rather than pulling `react-resizable-panels` (SUS-flagged; would force the deferred IDE-layout refactor). Zero new dependency — bundle-size gate stays green and threat T-37-SC (package installs) is `accept`/N-A.
- 70vw width ceiling is recomputed on window resize (state + listener), so the clamp is correct "at drag time" per UI-SPEC §6 rather than frozen at mount.
- Collapse is internal component state; the panel stays mounted while collapsed so node selection survives. Only Escape/backdrop invoke `onClose`, honoring D-06 "collapse ≠ close".
- Kept `TaskDetailDrawer.tsx` byte-for-byte unchanged (`git diff --stat` shows 0 lines) — NodeDetailPanel copies its a11y idiom rather than refactoring the drawer, per the plan's explicit scope guard.

## Deviations from Plan

None - plan executed exactly as written. Both tasks followed the specified TDD flow; all acceptance-criteria greps and automated verifications pass.

## Issues Encountered
- jsdom ships no `PointerEvent` and no `setPointerCapture`. Resolved by (a) guarding the `setPointerCapture` call with a `typeof === "function"` check in the component, and (b) driving the drag tests with native `MouseEvent`s typed as pointer events (MouseEvent carries `clientX`/`clientY`), so coordinate deltas actually propagate through the window listeners.

## Threat Model Notes
- T-37-04-01 (Tampering, localStorage layout values): mitigated — `usePersistedSize` parses stored values as numbers and clamps to `[min,max]` on read; non-numeric input falls back to the default. No layout-injection possible.
- T-37-SC (package installs): not applicable — no dependencies added.

## Known Stubs
None. Both primitives are fully wired; the caller-content mount points (ArtifactViewer / settings) are 37-05/37-08's scope by design, and `children` is a real render slot, not a placeholder.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- 37-05 can mount `<ArtifactViewer>` as `NodeDetailPanel` children immediately.
- 37-08 can import `PlanningNodeKind` for kind-aware click routing and reuse `ResizeHandle` (horizontal orientation) for the App log-area height.
- D9 (visual/motion fidelity) is flagged for a human/visual pass during 37-08 integration — jsdom unit tests do not exercise rendered chrome (track hover color, cursor, slide easing).

## Self-Check: PASSED

- All 4 created source/test files present on disk.
- All 4 task commits (`5148ceb`, `1274d66`, `dcdb6f2`, `4fe4f91`) present in git history.
- Full dashboard vitest suite 232/232 green; `tsc -b` exit 0; bundle-size + no-dangerous-html gates green.
- Acceptance greps verified: `role="separator"`=1, `setPointerCapture`>=1, `resizable-panels`=0, `role="dialog"`=1, `tide.dashboard.panel-width`/`-collapsed` present, TaskDetailDrawer diff = 0 lines.

---
*Phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta*
*Completed: 2026-07-08*
