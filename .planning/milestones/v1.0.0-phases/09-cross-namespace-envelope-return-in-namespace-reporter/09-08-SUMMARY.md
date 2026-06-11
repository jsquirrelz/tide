---
phase: 09-cross-namespace-envelope-return-in-namespace-reporter
plan: 08
subsystem: controller
tags: [gap-closure, succession-gate, budget-rollup, defect-B, defect-C]
dependency_graph:
  requires: [09-06]
  provides: [race-free-succession, planner-cost-rollup]
  affects: [milestone_controller, phase_controller, plan_controller, project_controller, pkg/dispatch]
tech_stack:
  added: []
  patterns: [ChildCount-gated-succession, isFirstCompletion-once-guard]
key_files:
  created: []
  modified:
    - pkg/dispatch/envelope.go
    - pkg/dispatch/envelope_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/milestone_controller_test.go
    - internal/controller/phase_controller.go
    - internal/controller/phase_controller_test.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/boundary_push_test.go
decisions:
  - "ChildCount added to both TerminationStub and EnvelopeOut; the latter enables PodStatusEnvelopeReader to populate it from the termination message JSON without requiring a separate ReadStub method"
  - "isFirstCompletion guard (reporter-Job newly spawned = first observation) prevents double-count of planner Usage on ChildCount-gate requeueing"
  - "nil-EnvReader fallback preserved in all 4 controllers for non-Option-C/unit-test paths"
metrics:
  duration: "~25 minutes"
  completed: "2026-06-08T20:01:36Z"
  tasks_completed: 3
  files_modified: 9
---

# Phase 09 Plan 08: ChildCount-gated succession + planner cost rollup Summary

ChildCount field on TerminationStub/EnvelopeOut replaces four inconsistent succession guards with one uniform gate; planner-level budget rollup wired to Project.Status.Budget in all four planner controllers.

## Tasks Completed

| Task | Name | Commit | Status |
|------|------|--------|--------|
| 1 (RED) | TerminationStub.ChildCount — failing tests | c1fdcea | done |
| 1 (GREEN) | TerminationStub.ChildCount — implementation | fe25ad5 | done |
| 2 | Uniform ChildCount-gated succession (Defect B) | 5c5c3e5 | done |
| 3 | Planner-level Usage rollup to Project.Status.Budget (Defect C) | 13c62fd | done |

## What Was Built

### Task 1: TerminationStub.ChildCount

- Added `ChildCount int json:"childCount"` to `TerminationStub` in `pkg/dispatch/envelope.go`
- `NewTerminationStub` sets `ChildCount = len(out.ChildCRDs)` — count only, no payloads
- Added `ChildCount int json:"childCount,omitempty"` to `EnvelopeOut` so `PodStatusEnvelopeReader` populates it from the termination message JSON
- New tests: `TestNewTerminationStub_ChildCount`, `TestNewTerminationStub_ChildCountJSON`
- Updated `TestTerminationStub_NoForbiddenFields` compile-time literal to include `ChildCount`
- Stub stays < 4096 bytes (existing `TestNewTerminationStub_StaysSmall` passes)

### Task 2: Uniform ChildCount-gated succession (Defect B)

All four planner controllers now implement the same succession gate:

```
if envReadOK (EnvReader != nil and ReadOut succeeded):
    expected = out.ChildCount
    expected == 0             → succeed (genuine leaf)
    observed < expected       → requeue 5s
    observed >= expected      → BoundaryDetected? push+succeed : requeue 5s
else (nil EnvReader fallback):
    prior hasChild-based behavior preserved
```

- **milestone**: removed `justMaterialized`; added `countChildPhases` helper; `hasChildPhases` now delegates to `countChildPhases`
- **phase**: added the missing ChildCount gate (root cause of premature-succession race); added `countChildPlans` + `countChildTasks` helpers
- **plan**: replaced `reporterSpawned` early-return with ChildCount gate on Task children (clears Running only after expected Tasks materialized)
- **project**: added ChildCount gate in `handleProjectJobCompletion` + `countChildMilestones` helper

Updated tests:
- `boundary_push_test.go`: Tests 1, 2, 4 — added `ChildCount: 1` to `EnvelopeOut` so gate proceeds to BoundaryDetected
- `milestone_controller_test.go` Test 4: added `ChildCount: 1` so uniform gate expects 1 child Phase
- `phase_controller_test.go` Test 5 (new): proves Phase does NOT Succeed while `observed < expected`; three assertions: 0 Plans → requeue, 1 Plan pending → requeue, 1 Plan Succeeded → Phase Succeeded

### Task 3: Planner-level Usage rollup (Defect C)

- All four planner controllers now call `budget.RollUpUsage(ctx, r.Client, project, out.Usage)` after successful `ReadOut`
- Guard: `isFirstCompletion && envReadOK` — `isFirstCompletion` is true when the reporter Job is newly spawned (first observation of Job terminal state); prevents double-count on ChildCount-gate requeueing
- `budget` package added to imports of milestone, phase, plan controllers (project already had it)
- New test: `milestone_controller_test.go` Test 5 — asserts `Project.Status.Budget.CostSpentCents >= 7` after planner Job completes with `EstimatedCostCents: 7`

## Verification

```
go test ./internal/controller/ ./internal/gates/ ./internal/budget/ ./pkg/dispatch/ -count=1
```

All pass (97 specs in controller suite including 2 new specs, all suites green).

## Deviations from Plan

**1. [Rule 2 - Missing] EnvelopeOut.ChildCount field added**
- Found during: Task 2 implementation
- Issue: `PodStatusEnvelopeReader.ReadOut` deserializes the termination message (a `TerminationStub` JSON) into `EnvelopeOut`. Without `ChildCount` on `EnvelopeOut`, the `childCount` field from the stub JSON would be discarded, leaving `out.ChildCount == 0` in all controllers.
- Fix: Added `ChildCount int json:"childCount,omitempty"` to `EnvelopeOut` (omitempty so existing test fixtures don't require updates). Controllers use `out.ChildCount` as intended.
- Files modified: `pkg/dispatch/envelope.go`

**2. [Rule 1 - Bug] boundary_push_test.go needed ChildCount=1**
- Found during: Task 2 test run
- Issue: Tests 1, 2, 4 in boundary_push_test set `ChildCount=0` (implicitly). With the new gate, `expected=0` → leaf → succeed immediately without reaching BoundaryDetected → push job never created → test failure.
- Fix: Added `ChildCount: 1` to those SetOut calls so the gate proceeds to BoundaryDetected (which these tests already satisfy via makeSucceededChildPhase/makeSucceededChildPlan).
- Commit: 5c5c3e5

## Known Stubs

None — all data flows wired.

## Threat Flags

None — no new network endpoints, auth paths, or trust-boundary changes. Changes are internal to the controller reconcile path and pkg/dispatch type definitions.

## Self-Check: PASSED

Files exist:
- [x] pkg/dispatch/envelope.go (ChildCount field)
- [x] internal/controller/milestone_controller.go (ChildCount gate + budget rollup)
- [x] internal/controller/phase_controller.go (ChildCount gate + budget rollup)
- [x] internal/controller/plan_controller.go (ChildCount gate + budget rollup)
- [x] internal/controller/project_controller.go (ChildCount gate + budget rollup)
- [x] internal/controller/milestone_controller_test.go (Tests 4+5 updated)
- [x] internal/controller/phase_controller_test.go (Test 5 added)

Commits exist:
- [x] c1fdcea (RED tests)
- [x] fe25ad5 (Task 1 implementation)
- [x] 5c5c3e5 (Task 2 uniform succession)
- [x] 13c62fd (Task 3 budget rollup)
