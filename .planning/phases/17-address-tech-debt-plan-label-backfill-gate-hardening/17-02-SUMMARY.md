---
phase: 17-address-tech-debt-plan-label-backfill-gate-hardening
plan: "02"
subsystem: controller
tags: [debt, gate-hardening, reject-short-circuit, phase-controller, milestone-controller, tdd]
dependency_graph:
  requires: []
  provides: [DEBT-02]
  affects:
    - internal/controller/phase_controller.go
    - internal/controller/phase_controller_test.go
    - internal/controller/milestone_controller_test.go
tech_stack:
  added: []
  patterns:
    - "Reject-first short-circuit before reporter spawn — mirrors plan_controller.go:470-471 template"
    - "Exact-name Job absence assertion via Get + not-found error check (scoped, avoids false positives from concurrent specs)"
key_files:
  created: []
  modified:
    - internal/controller/phase_controller.go
    - internal/controller/phase_controller_test.go
    - internal/controller/milestone_controller_test.go
decisions:
  - "Phase source fix: relocated gates.CheckRejected block from after spawnReporterIfNeeded to immediately after projectUID derivation, matching plan_controller.go:470-471 template"
  - "Milestone source fix: already landed in Phase 12 commit be82c7e; Task 2 is spec-only (regression guard), documented as deviation"
  - "Reporter Job absence assertion uses exact name lookup (tide-reporter-<uid>) instead of namespace-wide scan to prevent cross-spec contamination in the shared envtest namespace"
metrics:
  duration: "~30 minutes"
  completed: "2026-06-13"
  tasks_completed: 2
  tasks_total: 2
  files_changed: 3
---

# Phase 17 Plan 02: Reject Short-Circuit Before Reporter Spawn Summary

Relocated `gates.CheckRejected` in `PhaseReconciler.handleJobCompletion` to fire before `spawnReporterIfNeeded`, matching the plan_controller.go:470-471 template; added regression specs for both Phase and Milestone levels proving no reporter Job is created on reject.

## Tasks

| Task | Name | Status | Commit |
|------|------|--------|--------|
| 1 | Relocate the phase reject short-circuit ahead of the reporter spawn | DONE | ae3fc8b (RED), 24155e3 (GREEN) |
| 2 | Relocate the milestone reject short-circuit ahead of envelope read and reporter spawn | DONE (spec-only) | 240f4d0 |

## What Was Built

**Task 1 — Phase controller fix (DEBT-02 T-17-04):**

The `gates.CheckRejected` short-circuit was at line 510 in `PhaseReconciler.handleJobCompletion`, AFTER `spawnReporterIfNeeded` at line 483. This meant a rejected Project's completing planner Job still spawned a new reporter Job before the reject park fired — a spend control bypass.

Fix: moved the 3-line reject block to immediately after `projectUID` derivation (line 452), before both the envelope-read block and `spawnReporterIfNeeded`. Updated comment to reference the plan_controller.go template.

**Task 2 — Milestone controller regression spec:**

The milestone controller's reject check was ALREADY correctly positioned (BEFORE envelope read and spawn) from Phase 12 commit `be82c7e`. The Task 2 work was spec-only: added a regression guard `Describe` block to `milestone_controller_test.go` asserting:
- (a) Milestone parked with `RejectedByUser` condition
- (b) No `tide-reporter-<uid>` Job created (exact-name lookup)

## Verification Results

```
go test ./internal/controller/... -run 'TestControllers/(PhaseReconciler|MilestoneReconciler)' -count=1
ok  github.com/jsquirrelz/tide/internal/controller  60.00s
```

All 140 specs pass. New DEBT-02 specs in both PhaseReconciler and MilestoneReconciler Describe blocks pass.

Line-order grep confirms:
- Phase: `gates.CheckRejected` at line 458 < `spawnReporterIfNeeded` at line 491
- Milestone: `gates.CheckRejected` at line 515 < `ReadOut` at line 534 < `spawnReporterIfNeeded` at line 556
- No `Delete(` calls on the reject path (Pitfall 3 satisfied)

## Deviations from Plan

### Deviation 1 — Milestone source code already fixed (Phase 12)

**Found during:** Task 2 read-first phase

**Issue:** The PLAN described the milestone reject check as sitting AFTER the envelope read (`:515` after `:534`). However, the current code in `milestone_controller.go` already has the reject check at line 515, which is BEFORE the envelope read at line 529 and spawn at line 556.

**Root cause:** The Phase 12 commit `be82c7e` (`feat(12-04): reject parks milestone+phase with RejectedByUser condition`) already relocated the milestone reject check. The RESEARCH was conducted against the pre-12-04 code, and the PATTERNS doc described the historical bug, not the current state.

**Impact:** Task 2 became spec-only work. No source file change to `milestone_controller.go` was needed.

**Action:** Added the regression spec as planned (the spec still provides value as a gate that would catch any future regression). Documented as a deviation.

### Deviation 2 — TDD RED test for milestone passed immediately

**Found during:** Task 2 RED phase

**Issue:** The milestone DEBT-02 spec passed on first run (GREEN from the start), violating the TDD fail-fast rule (RED must fail before GREEN).

**Reason:** The code was already correct (Deviation 1), so the spec correctly passes without any source changes.

**Action:** Per TDD executor guidance, investigated and confirmed the code was already correct. Proceeded with the GREEN spec as a regression guard. Documented per the TDD fail-fast investigation protocol.

### Deviation 3 — Reporter Job absence assertion scoped to Phase/Milestone UID

**Found during:** Task 1 GREEN debugging

**Issue:** The first implementation of the `tide-reporter-` absence assertion used a namespace-wide scan, which picked up reporter Jobs created by other tests running concurrently in the shared envtest namespace. This caused false failures even after the source fix was applied.

**Fix:** Changed to exact-name lookup (`tide-reporter-<level-uid>`) using `mgrClient.Get()` + `not-found` error check, scoped to the specific Phase/Milestone under test.

**Files modified:** `internal/controller/phase_controller_test.go` (test assertion refactored)

## Known Stubs

None. All assertions are wired to real behavior.

## Threat Flags

No new security-relevant surface introduced. This plan is a pure reordering fix with no new network endpoints, auth paths, or schema changes.

## Self-Check

- [x] `internal/controller/phase_controller.go` exists and modified
- [x] `internal/controller/phase_controller_test.go` exists and modified
- [x] `internal/controller/milestone_controller_test.go` exists and modified
- [x] Commit `ae3fc8b` exists (RED test)
- [x] Commit `24155e3` exists (GREEN fix)
- [x] Commit `240f4d0` exists (milestone spec)
