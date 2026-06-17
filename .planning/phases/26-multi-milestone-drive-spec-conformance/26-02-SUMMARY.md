---
phase: 26-multi-milestone-drive-spec-conformance
plan: "02"
subsystem: controller
tags: [kubernetes, controller-runtime, wave, predicate, prune]

requires:
  - phase: 26-multi-milestone-drive-spec-conformance/26-01
    provides: idempotency guard changes to project_controller.go (exclusive file ownership)

provides:
  - "OQ-3 closed: wave aggregator emits ZeroMembers phase for zero-member waves; prune guard uses TaskRefs+Phase to distinguish in-flight from stale"
  - "WR-02 closed: globalDependentsMapper watch fires only on Status.Phase or Spec.DependsOn changes; no-op resourceVersion bumps suppressed"
  - "CR-01 PruneShrink regression test stays green"
  - "Unit test proving WR-02 predicate firing matrix (7 cases)"

affects:
  - 26-03
  - 26-04

tech-stack:
  added: ["slices (stdlib Go 1.21+, for slices.Contains/slices.Equal)"]
  patterns:
    - "predicate.Funcs with conservative UpdateFunc type-assert fallthrough for watch filtering"
    - "Phase-gated CreationTimestamp fence: apply only when Phase==\"\" (pre-aggregation), not to already-aggregated ZeroMembers waves"
    - "Exported constructor (newStatusPhaseOrDepsChangedPredicate) for plain-Go unit testing of predicate logic without Ginkgo/envtest"

key-files:
  created:
    - internal/controller/task_controller_predicate_test.go
  modified:
    - internal/controller/wave_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go

key-decisions:
  - "ZeroMembers as a distinct aggregator phase (not Running) is the root fix for OQ-3 — the prune guard can now use len(TaskRefs)==0 OR Phase==Succeeded without ambiguity"
  - "CreationTimestamp fence scoped to Phase=='' only: once WaveReconciler stamps any phase (including ZeroMembers), the fence must not apply — applying it to ZeroMembers broke CR-01 PruneShrink (discovered during Task 2 integration)"
  - "newStatusPhaseOrDepsChangedPredicate exported as package-internal constructor so the plain-Go test (not Ginkgo) can instantiate and exercise it without spinning up envtest"
  - "slices.Contains replaces inner for-loop in globalDependentsMapper for the modernize linter fix triggered by adding the slices import"

patterns-established:
  - "Predicate.Funcs pattern: define via constructor function before SetupWithManager; wire via builder.WithPredicates; unit-test via plain Go TestXxx (not Ginkgo)"
  - "Wave Phase taxonomy: ZeroMembers (no tasks), Succeeded (all done), Failed (any failed), Running (in-flight, non-zero members)"

requirements-completed: [MS-02, SPEC-01]

duration: 35min
completed: 2026-06-17
---

# Phase 26 Plan 02: OQ-3 wave prune guard + WR-02 watch predicate Summary

**Wave aggregator adds ZeroMembers phase (OQ-3 root fix) + in-flight-safe prune guard; globalDependentsMapper fires only on phase/dependsOn transitions (WR-02), proven by 7-case unit test**

## Performance

- **Duration:** 35 min
- **Started:** 2026-06-17T14:40:00Z
- **Completed:** 2026-06-17T15:15:00Z
- **Tasks:** 2
- **Files modified:** 4 (3 source files + 1 new test file)

## Accomplishments

- Wave aggregator now emits `ZeroMembers` phase for empty-member waves, making zero-member and in-flight Running waves unambiguously distinguishable
- Prune guard checks `len(TaskRefs)==0 || Phase==Succeeded` with a Phase=="" CreationTimestamp fence (5s) to avoid deleting waves before the aggregator stamps them
- CR-01 PruneShrink envtest regression test stays green
- globalDependentsMapper watch predicate fires only on `Status.Phase` or `Spec.DependsOn` changes; no-op resourceVersion-only events suppressed
- 7-case plain-Go unit test covers: phase change → true, dependsOn change → true, no-op → false, type-assert failure → true (conservative), Create → true, Delete → true, Generic → false

## Task Commits

1. **Task 1: Add ZeroMembers phase + in-flight-safe prune guard** - `06528a8` (fix)
2. **Task 2 RED: predicate unit test (failing)** - `42aa161` (test)
3. **Task 2 GREEN: WR-02 predicate implementation + fence fix** - `bbdf8a1` (feat)

## Files Created/Modified

- `internal/controller/wave_controller.go` - Aggregator switch: `case len(members)==0: phase="ZeroMembers"` as first case; keeps allSucceeded, Failed, Running for non-empty waves
- `internal/controller/project_controller.go` - Prune guard: `len(TaskRefs)==0 || Phase==Succeeded`; CreationTimestamp fence scoped to `Phase==""` only
- `internal/controller/task_controller.go` - `newStatusPhaseOrDepsChangedPredicate()` constructor; `slices` + `event` imports; `builder.WithPredicates(statusPhaseOrDepsChanged)` on globalDependentsMapper watch; `slices.Contains` simplification of DependsOn loop
- `internal/controller/task_controller_predicate_test.go` - 7-case plain-Go unit test for the WR-02 predicate firing matrix

## Decisions Made

- **ZeroMembers as the root fix for OQ-3**: The aggregator `default: "Running"` case was reached even for empty-member waves (len(members)==0 is false for allSucceeded, no failedTask). Adding `case len(members)==0: "ZeroMembers"` as the first case makes the empty case explicit and prevents the prune guard from conflating display-only ZeroMembers with real in-flight Running work.
- **CreationTimestamp fence scoped to Phase==""**: Initially the fence applied to any wave with `len(TaskRefs)==0 && recentlyCreated`. This broke CR-01 PruneShrink because wave-1 transitions to ZeroMembers (TaskRefs empty, Phase="ZeroMembers") within 5 seconds of creation during the fast envtest cycle. The fix: apply the fence ONLY when Phase=="" (pre-aggregation), so once the WaveReconciler stamps any phase, the fence is inactive and TaskRefs is authoritative.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] CreationTimestamp fence too broad — broke CR-01 PruneShrink**
- **Found during:** Task 2 (integration test run after Task 2 implementation)
- **Issue:** The initial `recentlyCreated && len(TaskRefs)==0` fence blocked pruning of ZeroMembers-phased waves created within 5 seconds, which in the fast envtest cycle included the very wave being tested by PruneShrink
- **Fix:** Tightened fence to `Phase=="" && len(TaskRefs)==0 && recentlyCreated` — only apply when Phase is unset (pre-aggregation); ZeroMembers waves are prunable once stamped
- **Files modified:** internal/controller/project_controller.go
- **Verification:** CR-01 PruneShrink passes (`Ran 1 of 51 Specs in 8.52s SUCCESS`)
- **Committed in:** bbdf8a1 (Task 2 GREEN commit, inlined with WR-02 implementation)

---

**Total deviations:** 1 auto-fixed (Rule 1 bug — fence logic)
**Impact on plan:** Required to keep the CR-01 regression test green. No scope creep; fix was a one-line condition tightening within the prune guard already being modified.

## Issues Encountered

- PruneShrink passed with only Task 1 changes but failed after Task 2 — traced to the `recentlyCreated` fence applying to ZeroMembers-phased waves that the WR-02 predicate change surfaced (the predicate reduces TaskReconciler churn, which changed timing slightly and made the 5-second fence more likely to fire on wave-1 during the assertion window)

## Known Stubs

None — all changes are controller logic and a unit test. No UI stubs, no placeholder data.

## Threat Flags

None — changes are in-process controller logic only, no new external input or network endpoints.

## Self-Check

- [x] `internal/controller/wave_controller.go` contains `ZeroMembers`
- [x] `internal/controller/project_controller.go` contains `skipping prune of in-flight wave`
- [x] `internal/controller/task_controller.go` contains `statusPhaseOrDepsChanged` (2 occurrences) and `builder.WithPredicates(statusPhaseOrDepsChanged)` (1 occurrence)
- [x] `internal/controller/task_controller_predicate_test.go` exists with 7 test cases — all pass
- [x] CR-01 PruneShrink envtest: PASS (`Ran 1 of 51 Specs in 8.52s SUCCESS`)
- [x] `go build ./internal/... ./api/... ./cmd/manager/...`: PASS
- [x] `make verify-dag-imports`: PASS

## Self-Check: PASSED

## Next Phase Readiness

- OQ-3 and WR-02 Phase 25 carried-in debt fully closed
- Project controller prune logic is now in-flight-safe
- globalDependentsMapper churn reduced to O(actual-phase-transitions) from O(all-task-events)
- Phase 26 Wave 2 complete; Phase 26 Wave 3 (spec conformance test + multi-milestone) can proceed

---
*Phase: 26-multi-milestone-drive-spec-conformance*
*Completed: 2026-06-17*
