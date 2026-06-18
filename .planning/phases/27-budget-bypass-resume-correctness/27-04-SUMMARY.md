---
phase: 27-budget-bypass-resume-correctness
plan: "04"
subsystem: controller
tags: [budget, bypass, halt, resume, cap, rolling-window]

requires:
  - phase: 27-01
    provides: BypassBaselineCents int64 field on BudgetStatus (the durable baseline field set at bypass time)
  - phase: 27-02
    provides: BYPASS-01 resume-at-Running fix in handleBudgetGate
  - phase: 27-03
    provides: BYPASS-03 PlannerRolledUpUID idempotency in handleProjectJobCompletion

provides:
  - D-04 acknowledged-spend baseline: bypass sets BypassBaselineCents=CostSpentCents; re-halt fires only on new post-bypass spend
  - Which-cap observability: ConditionBudgetExceeded Reason=AbsoluteCapReached|RollingWindowCapReached with current spend + both cap values
  - IsCapExceeded call-site audit: baseline scoped to handleBudgetGate only; TaskReconciler dispatch gate unaffected
  - Unit coverage: TestIsCapExceeded_NoBaselineAwareness confirms predicate has no baseline awareness
  - Envtest coverage: raise-absolute-cap-alone resume stays Running; re-halt on new rolling-window spend carries RollingWindowCapReached

affects: [phase-28, any future plan modifying handleBudgetGate or budget cap behavior]

tech-stack:
  added: []
  patterns:
    - "Acknowledged-spend baseline: set field == current spend at bypass time; re-halt guarded on field < current spend"
    - "Which-cap determination: default AbsoluteCapReached; switch to RollingWindowCapReached when absolute not the exceeded cap"
    - "Baseline scoped to handleBudgetGate only — shared IsCapExceeded predicate left unchanged per Pitfall 4"

key-files:
  created: []
  modified:
    - internal/controller/project_controller.go
    - internal/budget/cap.go
    - internal/budget/cap_test.go
    - internal/controller/project_controller_test.go

key-decisions:
  - "D-04 (BYPASS-04): bypass acknowledges current spend as baseline; re-halt fires only on CostSpentCents > BypassBaselineCents — raising absolute cap alone makes resume stick"
  - "Which-cap reason: default AbsoluteCapReached; switch to RollingWindowCapReached when AbsoluteCapCents<=0 or spend<=AbsoluteCapCents; message cites both cap values"
  - "IsCapExceeded body left UNCHANGED (only doc comment added) — call-site audit confirms TaskReconciler at budget_blocked.go:83 is unaffected"
  - "TTL bypass test updated spend to 201 (> baseline 200) to simulate genuine new spend after TTL expiry — Rule 1 auto-fix"

patterns-established:
  - "Acknowledged-spend baseline pattern: set field=current-spend in bypass-clear status patch; guard re-halt on field < current-spend"

requirements-completed: [BYPASS-04]

duration: 25min
completed: 2026-06-18
---

# Phase 27 Plan 04: Budget-Bypass Resume Correctness (D-04) Summary

**Budget bypass acknowledges prior spend as a durable baseline (BypassBaselineCents) so raising the absolute cap alone makes a resume stick, and re-halt now names which cap fired (AbsoluteCapReached vs RollingWindowCapReached) with current spend + both cap values**

## Performance

- **Duration:** 25 min
- **Started:** 2026-06-18T15:42:00Z
- **Completed:** 2026-06-18T16:07:09Z
- **Tasks:** 2 (TDD cycles)
- **Files modified:** 4

## Accomplishments

- D-04 baseline: `handleBudgetGate` bypass-clear branch now sets `BypassBaselineCents = CostSpentCents` in the same status patch that sets `PhaseRunning` — so the next reconcile's re-halt guard sees `newSpendSinceBypass = (200 > 200) = false` and suppresses the rolling-window re-halt on already-incurred spend
- Which-cap observability: re-halt condition uses dynamic reason (`AbsoluteCapReached` vs `RollingWindowCapReached`) and a message citing `CostSpentCents`, `AbsoluteCapCents`, and `RollingWindowCapCents`; same reason used for the K8s Event
- `IsCapExceeded` body byte-for-byte unchanged; doc comment added pointing to `handleBudgetGate` for bypass/baseline behavior; both non-test call-sites audited (see below)

## Task Commits

1. **Task 1 RED: failing BYPASS-04 tests** — `836d86d` (test)
2. **Task 1 GREEN: D-04 implementation + TTL test auto-fix** — `8b20e4c` (feat + Rule 1 bug fix)

_Task 2 coverage (TestIsCapExceeded_NoBaselineAwareness + BYPASS-04 envtest specs) was added in the Task 1 RED commit and passes in the GREEN commit — no separate commits needed._

## Files Created/Modified

- `internal/controller/project_controller.go` — `handleBudgetGate`: BypassBaselineCents set at bypass; newSpendSinceBypass guard on re-halt; dynamic which-cap reason/message
- `internal/budget/cap.go` — `IsCapExceeded`: body unchanged; doc comment added (bypass/baseline behavior lives in handleBudgetGate, not here)
- `internal/budget/cap_test.go` — `TestIsCapExceeded_NoBaselineAwareness`: documents predicate has no baseline awareness; tests true/true/false/ignore-baseline cases
- `internal/controller/project_controller_test.go` — Two new BYPASS-04 envtest specs; TTL bypass test updated to use spend=201 (> baseline 200)

## IsCapExceeded Call-Site Audit (T-27-04 / Pitfall 4)

| Call-site | File | Line | Affected by D-04 baseline? |
|-----------|------|------|----------------------------|
| `TaskReconciler` dispatch gate | `internal/controller/budget_blocked.go` | 83 | No — evaluates caps globally; no baseline comparison here |
| `ProjectReconciler.handleBudgetGate` | `internal/controller/project_controller.go` | 1282 | No change to call; baseline guard applied in the re-halt branch AFTER this call |

The acknowledged-spend baseline comparison (`CostSpentCents > BypassBaselineCents`) is **scoped exclusively to `handleBudgetGate`'s re-halt branch**. `IsCapExceeded` evaluates both caps unconditionally — unchanged — so the TaskReconciler dispatch gate at `budget_blocked.go:83` is unaffected.

## Decisions Made

- Chose `BypassBaselineCents` set-in-same-status-patch pattern (minimal — single field, no separate patch round-trip; baseline recorded atomically with phase transition)
- Which-cap determination: absolute cap takes priority when both exceeded (reason = `AbsoluteCapReached` unless absolute not the one exceeded)
- `newSpendSinceBypass` variable name chosen for clarity (matches plan's variable naming)
- TTL bypass test updated: spend reset to 201 (not 200) to simulate genuine new spend after TTL expiry; old behavior with 200 == baseline was incorrect under D-04 semantics

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TTL bypass test used spend == baseline causing re-halt suppression**
- **Found during:** Task 1 GREEN phase (running Budget gate regression tests)
- **Issue:** `TestProjectReconciler_BypassUntilAnnotation_TTLHonored` reset `CostSpentCents = 200` after TTL expiry, which equals the baseline (200) set during the TTL bypass-clear. Under D-04, `newSpendSinceBypass = (200 > 200) = false` → re-halt suppressed → test asserted `BudgetExceeded` but got `Pending`
- **Fix:** Changed spend reset to `201` (> baseline 200), matching the D-04 semantic that re-halt fires only on new spend since the bypass
- **Files modified:** `internal/controller/project_controller_test.go`
- **Verification:** All 5 Budget gate envtest specs pass (3 original + 2 new BYPASS-04)
- **Committed in:** `8b20e4c` (Task 1 GREEN feat commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — the D-04 semantics correctly changed what "re-halt requires"; the test predated D-04 and needed updating)
**Impact on plan:** Auto-fix necessary for correctness; no scope change.

## Issues Encountered

- Envtest requires `KUBEBUILDER_ASSETS` when run from the worktree directory (worktree doesn't have `bin/k8s/`); used `KUBEBUILDER_ASSETS=/Users/justinsearles/Projects/tide/bin/k8s/1.34.0-darwin-amd64` throughout

## Known Stubs

None.

## Threat Flags

T-27-04-01 (mitigated): baseline only suppresses re-halt up to the already-incurred amount; re-halt still fires on any new spend that crosses a cap (`CostSpentCents > BypassBaselineCents`). Proven by the new-spend envtest spec.

T-27-04-02 (mitigated): `IsCapExceeded` body unchanged; call-site audit confirms TaskReconciler dispatch gate is unaffected. See audit table above.

T-27-04-03 (accepted): message cites spend + both cap values — operator-facing observability, no secrets.

## Next Phase Readiness

- BYPASS-04 closed; Phase 27 plans 01–04 all complete — budget-bypass resume correctness phase done
- `BypassBaselineCents` field durable in CRD status; will survive controller restarts
- Phase 28 (Plan-Import Core) can proceed independently

---
*Phase: 27-budget-bypass-resume-correctness*
*Completed: 2026-06-18*
