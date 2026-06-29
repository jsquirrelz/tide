---
phase: 33-d4-planner-failure-semantics
plan: 03
subsystem: controller
tags: [go, controller-runtime, kubernetes, envtest, ginkgo, planner-failure, phase-controller, milestone-controller, resume]

# Dependency graph
requires:
  - phase: 33-01
    provides: isPlannerFailure helper (planner_failure.go) + ReasonPlannerFailed constant (api/v1alpha2/shared_types.go)
  - phase: 30
    provides: patchPlanFailed template (plan_controller.go) used as the shape for patchPhaseFailed/patchMilestoneFailed
provides:
  - patchPhaseFailed helper in phase_controller.go mirroring patchPlanFailed
  - patchMilestoneFailed helper in milestone_controller.go mirroring patchPlanFailed
  - isPlannerFailure guard inserted at phase succession site (before expected==0 succeed branch)
  - isPlannerFailure guard inserted at milestone succession site (before expected==0 succeed branch)
  - Envtest specs PLANFAIL-01 (phase false-leaf) and PLANFAIL-03 (phase genuine-leaf) in phase_controller_test.go
  - Envtest specs PLANFAIL-02 (milestone false-leaf) and PLANFAIL-03 (milestone genuine-leaf) in milestone_controller_test.go
  - Resume recovery test PLANFAIL-04 in cmd/tide/resume_test.go
affects: [phase-controller, milestone-controller, resume-cli, adoption-path, dag-integrity]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "patchXFailed returns status patch result (ctrl.Result{}, nil), NOT a Go error — prevents controller retry storm on permanent failure condition"
    - "isPlannerFailure guard ordered BEFORE expected==0 succeed branch — fail-check before succeed-check is load-bearing for PLANFAIL-03 non-regression"
    - "//nolint:unparam annotation required on patchXFailed helpers (always-nil error return kept so callers can use return r.patchXFailed(...) at call site)"
    - "Gates{Phase/Milestone: auto} required in envtests so gate-policy check doesn't park at AwaitingApproval before reaching the succession guard"

key-files:
  created: []
  modified:
    - internal/controller/phase_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller_test.go
    - internal/controller/milestone_controller_test.go
    - cmd/tide/resume_test.go

key-decisions:
  - "Guard patches a permanent Failed status condition (status patch, not Go error) to prevent controller retry storm — matches T-33-03 threat mitigation"
  - "fail-check ordered BEFORE succeed-check inside if envReadOK { block — load-bearing for PLANFAIL-03 (genuine leaf still Succeeds)"
  - "//nolint:unparam annotation copied verbatim from patchPlanFailed to silence lint false-positive on always-nil error return"
  - "Gates{Phase: auto} added to PLANFAIL envtest project fixtures — without it the default gate policy parks the level at AwaitingApproval before reaching the isPlannerFailure guard"
  - "Plan and Project levels deliberately excluded from guard — they succeed only via gates.BoundaryDetected (matched>0, false on zero children); false-leaf condition cannot arise at those levels"
  - "No new resume.go code needed — the existing retryFailedLevels walker already resets Failed Phase/Milestone; PLANFAIL-04 proves the guard's output is recoverable"

patterns-established:
  - "Pattern: patchXFailed helpers mirror patchPlanFailed — status patch only, no Go error, //nolint:unparam, stamp only ConditionFailed (do NOT clear ConditionWaveOrLevelPaused)"
  - "Pattern: false-leaf guard ordered as first statement in if envReadOK { block so fail-check precedes succeed-check"

requirements-completed: [PLANFAIL-01, PLANFAIL-02, PLANFAIL-03, PLANFAIL-04]

# Metrics
duration: 75min
completed: 2026-06-29
---

# Phase 33 Plan 03: D4 False-Leaf Guard Summary

**patchPhaseFailed/patchMilestoneFailed helpers added and isPlannerFailure guard wired at both phase and milestone succession sites, closing the false-leaf DAG corruption bug — locked by PLANFAIL-01/02/03 envtests and PLANFAIL-04 resume recovery test**

## Performance

- **Duration:** ~75 min
- **Started:** 2026-06-29T11:59:55Z
- **Completed:** 2026-06-29T13:10:46Z
- **Tasks:** 3
- **Files modified:** 5

## Accomplishments
- Added `patchPhaseFailed` helper to `phase_controller.go` mirroring `patchPlanFailed` (with `//nolint:unparam`, stamps only `ConditionFailed`, returns `ctrl.Result{}, nil` — no retry storm)
- Added `patchMilestoneFailed` helper to `milestone_controller.go` with identical shape
- Inserted `isPlannerFailure` guard as the FIRST statement in the `if envReadOK {` block at both phase and milestone succession sites — ordered BEFORE `expected := out.ChildCount` (fail-check before succeed-check is the load-bearing PLANFAIL-03 invariant)
- Locked the contract with four envtests: PLANFAIL-01 (phase false-leaf → Failed), PLANFAIL-02 (milestone false-leaf → Failed), two PLANFAIL-03 instances (genuine-leaf still Succeeds at both levels), PLANFAIL-04 (resume recovery — no new resume code needed)
- Full test gate: `internal/controller/...` 167/167 specs PASS; `make test-int` MAKE_EXIT=0 (Layer-A 55/55, Layer-B 8/8 passed, 2 flaked push_lease Tests 3+4 both eventually passed with 3 attempts — pre-existing environmental flake in files Phase 33 did not touch)

## Task Commits

Each task was committed atomically:

1. **Task 1: Add patchPhaseFailed/patchMilestoneFailed helpers and insert the guard** - `e20c18f` (feat)
2. **Task 2: Envtests PLANFAIL-01/02/03 at phase + milestone, and PLANFAIL-04 resume recovery** - `a3e3dc5` (test)
3. **Task 3: Full Layer-A regression + make test-int gate** - (no code changes — gate evidence recorded in SUMMARY)

**Plan metadata:** (docs commit follows)

## Files Created/Modified
- `internal/controller/phase_controller.go` - Added `patchPhaseFailed` helper; inserted `isPlannerFailure` guard before `expected := out.ChildCount` in `if envReadOK {` block
- `internal/controller/milestone_controller.go` - Added `patchMilestoneFailed` helper; inserted `isPlannerFailure` guard before `expected := out.ChildCount` in `if envReadOK {` block
- `internal/controller/phase_controller_test.go` - New Ginkgo Describe block with PLANFAIL-01 (phase false-leaf) and PLANFAIL-03 (phase genuine-leaf) envtest specs; uses `Gates{Phase: GatePolicy("auto")}` to bypass AwaitingApproval
- `internal/controller/milestone_controller_test.go` - New Ginkgo Describe block with PLANFAIL-02 (milestone false-leaf) and PLANFAIL-03 (milestone genuine-leaf) envtest specs; uses `Gates{Milestone: GatePolicy("auto")}`
- `cmd/tide/resume_test.go` - Added `TestResumeRetryFailedPlannerFailed` proving failed phase/milestone with `ReasonPlannerFailed` is reset by `resumeRun(retryFailed=true)` with no new resume code

## Decisions Made

1. Guard patches permanent `Failed` condition (status patch, `ctrl.Result{}, nil`) — not a Go error — so the controller does not retry-storm on a terminal planner failure. Mirrors the T-33-03 threat mitigation.
2. `isPlannerFailure` guard placed as the FIRST statement in `if envReadOK {` — before `expected := out.ChildCount` — so that exitCode!=0 fires the fail path before the succeed path, preserving PLANFAIL-03 (genuine leaf still Succeeds).
3. `//nolint:unparam` annotation copied verbatim from `patchPlanFailed`. Without it, the linter flags the always-`nil` error return and blocks the CI gate.
4. `Gates{Phase: GatePolicy("auto")}` added to PLANFAIL envtest project fixtures (and milestone analog). Root cause of PLANFAIL-01/02 parking at `AwaitingApproval`: the gate-policy check runs BEFORE the `if envReadOK {` block; without `auto`, the default `Approve` policy parks the level before the guard fires.
5. No new `resume.go` code needed for PLANFAIL-04 — `retryFailedLevels` walker already resets Failed Phase and Milestone by status patch and stamps `ReasonResumedByUser`. PLANFAIL-04 proves the guard's output is recoverable by asserting both resources' `Status.Phase != "Failed"` after `resumeRun(retryFailed=true)`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Added Gates{Phase: GatePolicy("auto")} to PLANFAIL-01/02 envtest project fixtures**
- **Found during:** Task 2 (PLANFAIL-01/02 envtest specs)
- **Issue:** Tests parked at `AwaitingApproval` instead of transitioning to `Failed`. The gate-policy check in `handleJobCompletion` runs before the `if envReadOK {` block. Without `Gates` configured, the default policy is `Approve`, which parks the level at `AwaitingApproval` before the `isPlannerFailure` guard fires.
- **Fix:** Added `Gates: tideprojectv1alpha2.Gates{Phase: tideprojectv1alpha2.GatePolicy("auto")}` to phase test projects and `Gates: tideprojectv1alpha2.Gates{Milestone: tideprojectv1alpha2.GatePolicy("auto")}` to milestone test projects, matching the pattern used in existing Test-5.
- **Files modified:** internal/controller/phase_controller_test.go, internal/controller/milestone_controller_test.go
- **Verification:** PLANFAIL-01 and PLANFAIL-02 assert `Status.Phase=="Failed"` — both pass after the fix.
- **Committed in:** `a3e3dc5` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - Bug)
**Impact on plan:** Necessary for test correctness — the gate-policy interaction was not anticipated in the plan but is fundamental to the test environment. No scope creep.

## Test Gate Evidence

**Layer-A (`internal/controller/...`):** `Ran 167 of 167 Specs SUCCESS! 167 Passed | 0 Failed` — includes all four PLANFAIL specs (PLANFAIL-01, PLANFAIL-02, two PLANFAIL-03 instances) plus `TestResumeRetryFailedPlannerFailed` in `cmd/tide/...`

**Layer-A envtest (`test/integration/envtest/...`):** `Ran 55 of 55 Specs in 71.680 seconds SUCCESS! 55 Passed | 0 Failed`

**Layer-B kind:** `Ran 8 of 22 Specs in 1195.873 seconds SUCCESS! 8 Passed | 0 Failed | 2 Flaked | 14 Skipped`
- 2 Flaked: push_lease Tests 3 and 4 — PVC prewarm race in `suite_test.go:771` and `push_lease_test.go:139`. Both eventually passed with 3 retry attempts. Pre-existing environmental flake: Phase 33 touched zero files under `test/integration/kind/`. MEMORY.md: "make test-int MAKE_EXIT=2 is the pre-existing kind medium_http fixture flake (zero kind files touched)."
- `grep -nE "^--- FAIL|^FAIL\s"` in log: zero matches (no permanent failures in any file)

**MAKE_EXIT=0**

**`go build ./... && go vet ./internal/controller/...`:** Both exit 0, no new `unparam` offenses on the two helpers.

## Issues Encountered
- Gate-policy interaction in PLANFAIL-01/02 tests (see Deviations above). Diagnosed from the `AwaitingApproval` assertion failure; fixed by adding `Gates{Phase: auto}` to test project fixtures.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
Phase 33 is complete: all four PLANFAIL requirements (PLANFAIL-01/02/03/04) are locked by green tests. The D4 false-leaf guard closes the planning DAG corruption bug where a failed planner with zero children could falsely advance its parent.

v1.0.6 milestone scope (Phases 31–33) is now fully implemented. Phase 31 (D2+D1 adoption lifecycle seam) and Phase 32 (D3 dispatch concurrency cap) are complete; Phase 33 (D4 planner failure semantics) is complete.

---
*Phase: 33-d4-planner-failure-semantics*
*Completed: 2026-06-29*

## Self-Check: PASSED

- `internal/controller/phase_controller.go`: FOUND (contains `patchPhaseFailed` at `e20c18f`)
- `internal/controller/milestone_controller.go`: FOUND (contains `patchMilestoneFailed` at `e20c18f`)
- `internal/controller/phase_controller_test.go`: FOUND (PLANFAIL-01/03 specs at `a3e3dc5`)
- `internal/controller/milestone_controller_test.go`: FOUND (PLANFAIL-02/03 specs at `a3e3dc5`)
- `cmd/tide/resume_test.go`: FOUND (TestResumeRetryFailedPlannerFailed at `a3e3dc5`)
- Commit `e20c18f`: FOUND in git log
- Commit `a3e3dc5`: FOUND in git log
- MAKE_EXIT=0: VERIFIED (background task exit code 0, log shows `PASS` + `ok`)
