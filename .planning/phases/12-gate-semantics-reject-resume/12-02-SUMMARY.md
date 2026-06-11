---
phase: 12-gate-semantics-reject-resume
plan: "02"
subsystem: cli
tags: [go, cobra, controller-runtime, fake-client, tdd, resume, approve, gate-semantics]

# Dependency graph
requires:
  - phase: 04-tide-cli
    provides: resume.go/approve.go seam signatures and fake-client test pattern
provides:
  - tide resume --retry-failed verb implementing D-06 run-1 kubectl recovery recipe
  - tide approve D-07 guard refusing Failed levels with actionable error
affects:
  - 13-dispatch-halt (HALT-01 recovery story points at tide resume --retry-failed)
  - future phases using resume verb shape

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Status-subresource patch via c.Status().Patch(ctx, item, client.MergeFrom(orig)) — required when fake client has WithStatusSubresource configured"
    - "Four-kind walker pattern: list Milestone/Phase/Plan/Task with MatchingLabels{tideproject.k8s/project} + Status.Phase guard"
    - "findFailedLevel function shape matching findAwaitingMilestone shape — scan in hierarchy order, return first hit"
    - "buildFailureDetail: FindStatusCondition(ConditionWaveOrLevelPaused) fallback to conditions[0] for failure reason in errors"

key-files:
  created: []
  modified:
    - cmd/tide/resume.go
    - cmd/tide/resume_test.go
    - cmd/tide/approve.go
    - cmd/tide/approve_test.go

key-decisions:
  - "resumeRun signature extended with retryFailed bool + out io.Writer — mirrors approveRun's existing writer param"
  - "retryFailedLevels inlines the four-kind walk rather than using a generic helper — avoids DeepCopyObject() interface mismatch with runtime.Object"
  - "D-07 guard fires in approveLevel only, not approveWave — --wave targets a specific Plan annotation outside the Failed-level scope"
  - "buildFailureDetail looks for ConditionWaveOrLevelPaused first; falls back to conditions[0] — covers both the WaveOrLevelPaused reason and other failure conditions"

patterns-established:
  - "Status subresource resets in CLI verbs must use c.Status().Patch (not c.Patch) and WithStatusSubresource on the fake client builder"
  - "D-07 guard pattern: findFailedLevel before findAwaiting* in approveLevel — guards added BEFORE the positive-path discovery chain"

requirements-completed: [GATE-03, RESUME-01]

# Metrics
duration: 30min
completed: 2026-06-11
---

# Phase 12 Plan 02: Resume --retry-failed + Approve D-07 Guard Summary

**`tide resume --retry-failed` implements the run-1 kubectl recovery recipe as a sanctioned CLI verb resetting all Failed levels via the status subresource; `tide approve` now refuses Failed levels with an actionable error pointing at `--retry-failed`.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-11T14:25:00Z
- **Completed:** 2026-06-11T14:56:24Z
- **Tasks:** 2 (both TDD)
- **Files modified:** 4

## Accomplishments

- `tide resume --retry-failed` walks all four level kinds (Milestone/Phase/Plan/Task), resets `Status.Phase` and conditions on every Failed item via `c.Status().Patch`, stamps `ResumedByUser` condition, and prints per-level feedback — replacing the run-1 manual kubectl recipe
- Running levels are never touched (Pitfall 3 / T-12-04 guard); flag absence leaves Failed levels untouched (D-06 deliberate friction)
- `findFailedLevel` added to `approve.go`: scans hierarchy in order, returns first Failed level
- `tide approve` now returns an actionable error before the `findAwaiting*` chain when a Failed level exists, naming the level, its kind, its failure reason/message, and pointing at `tide resume <project> --retry-failed`
- No approve annotation is written when the D-07 guard fires — approval never doubles as a spend-retry (T-12-05)
- All 8 new resume tests + 3 new approve tests pass; all 30+ pre-existing `cmd/tide` tests unaffected

## Task Commits

Each task was committed atomically:

1. **Task 1: tide resume --retry-failed** — `cab8a9d` (feat)
2. **Task 2: tide approve D-07 guard** — `12a3afd` (feat)

**Plan metadata:** committed with SUMMARY (docs)

_TDD: both tasks wrote failing tests first (RED), then implementation (GREEN), then refactored dead code._

## Files Created/Modified

- `cmd/tide/resume.go` — Extended `resumeRun` signature; added `retryFailedLevels` four-kind walker; `--retry-failed` bool flag on `newResumeCmd`; updated Long description
- `cmd/tide/resume_test.go` — Updated existing 4 tests for new signature; added `TestResumeRunRetryFailed`, `TestResumeRetryFailedSkipsRunning`, `TestResumeWithoutFlagLeavesFailed`, `TestResumeRetryFailedAllFourKinds`
- `cmd/tide/approve.go` — Added `findFailedLevel`, `buildFailureDetail`; inserted D-07 guard at top of `approveLevel`; added `k8s.io/apimachinery/pkg/api/meta` import
- `cmd/tide/approve_test.go` — Added `makeFailedMilestone` fixture; added `TestApproveRunFailedLevelError`, `TestApproveFailedLevelErrorIncludesReason`, `TestApproveFailedLevelNoAnnotationWritten`

## Decisions Made

- `retryFailedLevels` inlines the four-kind loop rather than using a generic Go helper — `DeepCopyObject()` returns `runtime.Object`, not `any`, so the generic type constraint `interface{ client.Object; DeepCopyObject() any }` causes a compile error. Inlined per-kind blocks are clear and match the `findAwaiting*` style already in the codebase.
- D-07 guard scoped to `approveLevel` only (not `approveWave`) — `--wave` targets a specific Plan's wave annotation and is out of scope per plan spec.
- `buildFailureDetail` prefers `ConditionWaveOrLevelPaused` and falls back to `conditions[0]` so the error includes context whether the failure is gate-related or a reconciler error.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Generic resetLevelStatus helper compile error**
- **Found during:** Task 1 (GREEN phase — first compile attempt)
- **Issue:** Generic constraint `interface{ client.Object; DeepCopyObject() any }` fails because `runtime.Object.DeepCopyObject()` returns `runtime.Object` not `any` — Go's type-checker cannot satisfy the constraint
- **Fix:** Inlined the four-kind status-reset loop using `item.DeepCopy()` + `client.MergeFrom(orig)` + `c.Status().Patch` per kind — matches the plan's specified pattern exactly
- **Files modified:** cmd/tide/resume.go
- **Verification:** `go build ./cmd/tide/...` exits 0; all 8 TestResume* tests pass
- **Committed in:** cab8a9d (Task 1 commit)

**2. [Rule 1 - Bug] Dead variable artifacts in buildFailureDetail and approveLevel**
- **Found during:** Task 2 (REFACTOR phase — self-review)
- **Issue:** Intermediate implementation left unused `conds` slice, unused `conditionsGetter` interface, and `findCond` func literal in `buildFailureDetail` / `approveLevel`
- **Fix:** Removed dead variables/types; final `buildFailureDetail` uses only the typed switch with `meta.FindStatusCondition`
- **Files modified:** cmd/tide/approve.go
- **Verification:** `go build ./cmd/tide/...` exits 0; all 9 TestApprove* tests pass
- **Committed in:** 12a3afd (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — compile errors cleaned up)
**Impact on plan:** Both fixes were in the implementation mechanics (generics constraint, dead code cleanup); plan semantics unchanged.

## Issues Encountered

None beyond the two auto-fixed deviations above.

## User Setup Required

None — CLI-only plan with unit tests; no external service configuration required.

## Next Phase Readiness

- `tide resume --retry-failed` is ready for Phase 13's HALT-01 to reference as the recovery verb
- D-07 error format (`tide resume <project> --retry-failed`) is stable — Phase 13 can point at it verbatim
- All existing CLI tests pass; no regressions

---
*Phase: 12-gate-semantics-reject-resume*
*Completed: 2026-06-11*

## Self-Check: PASSED

Files verified:
- `cmd/tide/resume.go` — FOUND
- `cmd/tide/resume_test.go` — FOUND
- `cmd/tide/approve.go` — FOUND
- `cmd/tide/approve_test.go` — FOUND

Commits verified:
- `cab8a9d` — FOUND (feat(12-02): tide resume --retry-failed...)
- `12a3afd` — FOUND (feat(12-02): tide approve refuses Failed levels...)
