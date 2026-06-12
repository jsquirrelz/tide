---
phase: 15-paper-cuts
plan: "05"
subsystem: dashboard/frontend
tags: [statusbadge, complete-status, coerce-guard, finding-9b, ui-spec-c1, ui-spec-c2]
dependency_graph:
  requires: []
  provides: [CUTS-05]
  affects: [dashboard/web/src/components/StatusBadge.tsx, dashboard/web/src/components/PlanningDAGView.tsx, dashboard/web/src/components/ProjectPicker.tsx]
tech_stack:
  added: []
  patterns: [KNOWN_STATUS_VALUES-single-source-of-truth, STATUS_TABLE-derived-keys]
key_files:
  created: []
  modified:
    - dashboard/web/src/components/StatusBadge.tsx
    - dashboard/web/src/components/PlanningDAGView.tsx
    - dashboard/web/src/components/ProjectPicker.tsx
    - dashboard/web/src/components/StatusBadge.test.tsx
    - dashboard/web/src/components/__tests__/dag-views.test.tsx
    - dashboard/web/src/components/ProjectPicker.test.tsx
decisions:
  - "CircleCheckBig (not CircleCheck) for Complete badge — color-blindness rule requires distinct glyph since both Complete and Succeeded use --color-status-success"
  - "KNOWN_STATUS_VALUES derived from Object.keys(STATUS_TABLE) — single source of truth eliminates silent-drift bug class (UI-SPEC C2)"
  - "finding-9b regression scoped to tide-node-project DOM node — other nodes legitimately carry Pending badges"
metrics:
  duration: "~15min"
  completed: "2026-06-12"
  tasks_completed: 2
  files_changed: 6
---

# Phase 15 Plan 05: Complete Status Chip + Coerce-Guard Consolidation (CUTS-05) Summary

**One-liner:** Complete status added as first-class 11th StatusValue with CircleCheckBig icon; both coerce guards consolidated onto a single KNOWN_STATUS_VALUES export derived from STATUS_TABLE keys, eliminating the silent-drift bug class.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Complete vocabulary row + coerce-guard consolidation (UI-SPEC C1, C2) | 3a934f0 | StatusBadge.tsx, PlanningDAGView.tsx, ProjectPicker.tsx |
| 2 | Vitest regressions — finding-9b symptom + vocabulary assertions | 58c7843 | StatusBadge.test.tsx, dag-views.test.tsx, ProjectPicker.test.tsx |

## What Was Built

**Task 1 — StatusBadge + coerce consolidation:**

- `StatusValue` union extended from 10 to 11 members: `"Complete"` inserted immediately after `"Succeeded"` (terminal-success family grouping, UI-SPEC C1)
- `CircleCheckBig` added to lucide-react import block; Complete STATUS_TABLE row: `icon: CircleCheckBig`, `colorVar: var(--color-status-success)`, `srDescription: "Complete — all milestones succeeded"`, no animation
- `KNOWN_STATUS_VALUES` exported from StatusBadge as `Object.keys(STATUS_TABLE) as readonly StatusValue[]` — single source of truth (UI-SPEC C2)
- `PlanningDAGView.coerce()`: local 10-element KNOWN array replaced with `KNOWN_STATUS_VALUES` import
- `ProjectPicker.coerceStatus()`: local KNOWN_STATUSES array replaced with `KNOWN_STATUS_VALUES` import

**Task 2 — Vitest regressions:**

- `StatusBadge.test.tsx`: EXPECTED map extended to 11 entries; Complete row asserts iconName `"CircleCheckBig"` + success color + label `"Complete"`; describe block updated to "11 variants"
- `dag-views.test.tsx`: finding-9b regression — project node with `phase: "Complete"` asserts `status-badge-Complete` present and `status-badge-Pending` absent *within the project node* (scoped to avoid false positives from other nodes with legitimate Pending badges); companion test asserts unknown phase `"Bogus"` still coerces to Pending
- `ProjectPicker.test.tsx`: Complete badge at second coerce site — project row with `phase: "Complete"` renders `status-badge-Complete`, not `status-badge-Pending`

## Test Results

- `npm run test -- --run`: **164 tests, 22 test files, all passed**
- `tsc --noEmit`: **exit 0, no type errors**

## Deviations from Plan

**[Rule 1 - Bug] Test assertion scoped to project node (not document root)**

- **Found during:** Task 2
- **Issue:** The initial finding-9b regression asserted `document.querySelector('[data-testid="status-badge-Pending"]')` to be null at the document level. The PROJECT_PAYLOAD includes milestones, phases, and plans with `phase: "Pending"` which legitimately render Pending badges for their own nodes — making the document-level null assertion always fail.
- **Fix:** Scoped the assertion to `projectNode!.querySelector(...)` — the test correctly validates only the project node's own badge, which is the actual finding-9b symptom.
- **Files modified:** `dashboard/web/src/components/__tests__/dag-views.test.tsx`
- **Commit:** 58c7843 (within same task commit)

## Known Stubs

None — all status vocabulary is fully wired; the Complete badge renders live from STATUS_TABLE.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced. The coerce guard consolidation maintains the exhaustive-by-construction invariant from T-15-14 — unknown strings continue to degrade to Pending, and React escapes all text content.

## Self-Check: PASSED

All 7 key files exist on disk. Both commits (`3a934f0`, `58c7843`) present in git log. 164 tests green. `tsc --noEmit` exit 0.
