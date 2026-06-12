---
phase: 14-budget-enforcement-pricing
plan: "07"
subsystem: dashboard-frontend
status: complete
completed_date: "2026-06-12"
duration_minutes: 8
tags: [dashboard, react, typescript, budget-enforcement, ux, vitest]
requirements: [BUDGET-02]

dependency_graph:
  requires:
    - 14-06  # Go backend: blockingConditions field on projectSummary + SSE publish
  provides:
    - ConditionBadge primitive (BudgetBlocked + BillingHalt vocabulary)
    - TideNodeShell blocking-conditions slot (purple border + badges)
    - ProjectNodeData.blockingConditions passthrough
    - PlanningDAGView blockingConditions wiring
    - api.ts ProjectSummary.blockingConditions wire-type mirror
  affects:
    - dashboard project node render (no change when conditions absent)
    - 14-VERIFICATION.md BUDGET-02 success criterion 2 (dashboard half)

tech_stack:
  added: []
  patterns:
    - ConditionBadge mirrors StatusBadge anatomy verbatim (pill, color-mix, inline style)
    - CONDITION_TABLE re-export pattern mirrors STATUS_TABLE
    - open string type + null guard for unknown condition types (mirrors coerce())
    - purple border-l-4 sibling to existing destructive border-l-4 in TideNodeShell
    - blockingConditions ?? [] default in buildPlanningGraph for legacy-payload safety

key_files:
  created:
    - dashboard/web/src/components/ConditionBadge.tsx
    - dashboard/web/src/components/ConditionBadge.test.tsx
  modified:
    - dashboard/web/src/components/TideNodeShell.tsx
    - dashboard/web/src/components/ProjectNode.tsx
    - dashboard/web/src/components/PlanningDAGView.tsx
    - dashboard/web/src/lib/api.ts
    - dashboard/web/src/components/__tests__/nodes.test.tsx
    - dashboard/web/src/components/__tests__/dag-views.test.tsx

decisions:
  - Task 2 test helper uses TideNodeShell directly rather than ProjectNode to avoid
    an inter-task dependency (ProjectNode passthrough is Task 3); this matches the
    spirit of testing TideNodeShell in isolation while keeping tests in nodes.test.tsx
    per the file convention.
  - node_modules symlinked from main repo into worktree (no install, same packages).
---

# Phase 14 Plan 07: BudgetBlocked Dashboard Condition Badge — Summary

Frontend half of the BUDGET-02 gap closure: a `BudgetBlocked` condition on the Project CR is now visible on the dashboard project node via the generalized `ConditionBadge` mechanism. Phase 13's `BillingHalt` condition rides the same badge for free with no special-casing.

## What Was Built

Three tasks, all TDD (RED → GREEN):

**Task 1 — ConditionBadge primitive** (`ConditionBadge.tsx` + `ConditionBadge.test.tsx`):
- New component mirroring StatusBadge anatomy exactly (pill, inline-flex, color-mix tint)
- `CONDITION_TABLE` with two locked entries: BudgetBlocked (Wallet icon) + BillingHalt (CreditCard)
- `ProjectBlockingCondition` wire type exported for downstream consumers
- Unknown condition types return null (whitelist guard, T-14-07-02)
- `title` carries controller `message` verbatim — React-escaped, no XSS (T-14-07-01)
- 8 Vitest specs covering icon/label/tooltip/aria-label/unknown-type/table-export

**Task 2 — TideNodeShell extension** (`TideNodeShell.tsx` + additions to `nodes.test.tsx`):
- Optional `blockingConditions?: ProjectBlockingCondition[]` prop (default `[]`)
- Purple `border-l-4 border-l-[var(--color-status-blocked)]` when `isBlocked && !isFailed`
- Destructive red takes precedence when both failed and blocked apply
- `data-blocked="true|false"` attribute alongside existing `data-failed`
- `aria-label` extends with `, blocked: <Label1>[, <Label2>]` when blocked
- ConditionBadge rendered per entry in summary row after StatusBadge (shrink-0 wrapped)
- 5 new specs in nodes.test.tsx; all 16 pre-existing specs pass unmodified

**Task 3 — Payload wiring** (api.ts + ProjectNode + PlanningDAGView + dag-views.test.tsx):
- `api.ts`: `ProjectSummary` gains `blockingConditions?: ProjectBlockingCondition[]`
- `ProjectNode.tsx`: `ProjectNodeData` gains `blockingConditions: ProjectBlockingCondition[]`, passes to TideNodeShell
- `PlanningDAGView.tsx`: `buildPlanningGraph` maps `detail.blockingConditions ?? []` into `projectData`; PLANNING_KINDS / REFETCH_DEBOUNCE_MS / SSE handler untouched
- 2 new dag-views specs (blocked payload + legacy degradation via `?? []`)

## Test Results

- All 157 tests pass across 22 test files
- `tsc -b` clean (no type errors)
- `git diff --name-only` confirms StatusBadge.tsx and package.json untouched (D-04 + zero-new-packages)

## Commits

| Hash | Task | Description |
|------|------|-------------|
| `0c8335d` | Task 1 | `feat(14-07)`: ConditionBadge primitive — two-entry blocking-condition vocabulary |
| `5c82dce` | Task 2 | `feat(14-07)`: TideNodeShell blocking-conditions slot — purple border + ConditionBadge summary row |
| `6d8883d` | Task 3 | `feat(14-07)`: wire blockingConditions — api.ts, ProjectNodeData, buildPlanningGraph |

## Deviations from Plan

### Auto-fixed Issues

None. Plan executed exactly as written, with one test-design note:

**Task 2 test helper** — The plan described testing through `ProjectNode` in `nodes.test.tsx`, but `ProjectNode.blockingConditions` passthrough is Task 3. Testing TideNodeShell directly from `nodes.test.tsx` (using a `renderShell` helper) keeps tests isolated, avoids inter-task dependencies, and preserves the test file's existing structure. The behavior tested is identical to what the plan specifies; the implementation point is TideNodeShell, not ProjectNode.

## Known Stubs

None. All data paths are wired end-to-end: `ProjectDetail.blockingConditions ?? []` → `projectData.blockingConditions` → `ProjectNode.data.blockingConditions` → `TideNodeShell.blockingConditions` → `ConditionBadge`. The `?? []` default ensures legacy payloads produce no badge (the correct empty state).

## Threat Flags

No new threat surface beyond what 14-PLAN.md's threat register covers:
- T-14-07-01 (XSS via condition message): mitigated — `title={condition.message}` is a JSX attribute binding, React-escaped. No `dangerouslySetInnerHTML` anywhere in the diff.
- T-14-07-02 (vocabulary drift): mitigated — `CONDITION_TABLE[condition.type]` miss returns null.
- T-14-SC (supply chain): no new packages. Wallet + CreditCard from already-pinned lucide-react.

## Self-Check: PASSED

All files created/modified verified to exist on disk. All three task commits found in git log.

| Item | Status |
|------|--------|
| `ConditionBadge.tsx` | FOUND |
| `ConditionBadge.test.tsx` | FOUND |
| `TideNodeShell.tsx` | FOUND |
| `ProjectNode.tsx` | FOUND |
| `PlanningDAGView.tsx` | FOUND |
| `api.ts` | FOUND |
| `nodes.test.tsx` | FOUND |
| `dag-views.test.tsx` | FOUND |
| commit `0c8335d` | FOUND |
| commit `5c82dce` | FOUND |
| commit `6d8883d` | FOUND |
