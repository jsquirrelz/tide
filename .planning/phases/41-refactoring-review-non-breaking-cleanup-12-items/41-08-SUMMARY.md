---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
plan: 08
subsystem: controller
tags: [status-conditions, k8s-conditions, controller-runtime, meta.SetStatusCondition]

# Dependency graph
requires:
  - phase: 41-07
    provides: leaf status-mutation primitives (patchLevelStatus, consumeApproveAndResume) that surfaceParentRefUnresolved's surrounding code now calls
provides:
  - ConditionParentUnresolved carries ONE truth semantics across all four kinds (Task/Plan/Milestone/Phase): Status=True means the parent is unresolved
  - ReasonParentResolved reason constant — the clear-on-resolve counterpart to ReasonParentRefNotFound
  - Clear-on-resolve write at Milestone and Phase's parent-resolve success path, guarded write-free in steady state
affects: [dashboard status-condition rendering, any future operator tooling reading ParentUnresolved]

# Tech tracking
tech-stack:
  added: []
  patterns: ["meta.IsStatusConditionTrue guard before a corrective status write (avoids a steady-state hot-loop)"]

key-files:
  created: []
  modified:
    - api/v1alpha3/shared_types.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/parentref_surface_test.go

key-decisions:
  - "D-04 (locked in 41-CONTEXT.md): True == parent unresolved, matching the type name and Task's pre-existing usage; Milestone/Phase's inverted polarity was the bug"
  - "Kept each site's existing status-write mechanism (Status().Update on Milestone/Phase) rather than converging on Task's MergeFrom+Patch — that convergence is a separate, unscoped concern"
  - "Landed as two commits per the plan's two-task shape, but only after both tasks' code + tests were written and the full suite verified green — Task 1 alone intentionally fails go test -run Parent (parentref_surface_test.go still pinned the old polarity), so the commit boundary follows Task 2's verify, not Task 1's own verify line"

requirements-completed: [REFAC-09]

# Metrics
duration: 12min
completed: 2026-07-12
---

# Phase 41 Plan 08: Normalize ConditionParentUnresolved Polarity Summary

**Fixed the inverted `ConditionParentUnresolved` polarity on Milestone/Phase (was Status=False for "parent missing"), added the missing clear-on-resolve half, and swept both consumer tests to the new True-means-unresolved contract — zero dashboard fallout confirmed.**

## Performance

- **Duration:** 12 min
- **Started:** 2026-07-12T13:33:00-04:00 (approx, first read)
- **Completed:** 2026-07-12T13:45:37-04:00
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- `surfaceParentRefUnresolved` on both `MilestoneReconciler` and `PhaseReconciler` now sets `ConditionParentUnresolved` to `metav1.ConditionTrue` (previously `ConditionFalse`) — matching `TaskReconciler`'s pre-existing, correct polarity.
- Added the previously-missing clear-on-resolve half: once the parent (Project for Milestone, Milestone for Phase) is found on a subsequent reconcile, the condition is set to `False`/`ReasonParentResolved`, guarded by `meta.IsStatusConditionTrue` so the common steady-state path (parent already resolved) performs zero extra status writes.
- Added `ReasonParentResolved` to the shared reason vocabulary in `api/v1alpha3/shared_types.go`, alongside an updated doc comment on `ReasonParentRefNotFound` noting the new True polarity.
- Flipped both existing `TestPhaseReconciler_ParentRefNotFound_Surfaces` / `TestMilestoneReconciler_ParentRefNotFound_Surfaces` assertions from expecting `ConditionFalse` to `ConditionTrue`, and added two new symmetric tests (`TestPhaseReconciler_ParentRefResolves_ClearsCondition`, `TestMilestoneReconciler_ParentRefResolves_ClearsCondition`) proving the clear-to-False/ParentResolved transition.
- Full consumer sweep completed: `cmd/dashboard` confirmed clean (zero consumers); `task_controller_extracted_test.go` confirmed needs no change (already True-polarity, asserts no explicit Status value); remaining prose comments (`plan_controller.go:1428`, `task_controller.go:77/1114`, `internal/dispatch/podjob/backend.go:46`) reference the condition generically without stating a polarity, so none were stale.

## Task Commits

Each task was committed atomically, but per the plan's Task 1 acceptance-criteria note ("the commit lands only after Task 2's verify"), both tasks' code was implemented and the full suite verified green before either commit was created — the commits themselves still map 1:1 to the plan's two tasks:

1. **Task 1: Flip polarity, add ReasonParentResolved, add clear-on-resolve** - `831d231` (fix)
2. **Task 2: Sweep tests + stale prose; document the observable change** - `b3e7155` (test)

_Note: `go test ./internal/controller/... -run 'Parent'` genuinely fails after Task 1's code lands alone (the old test file still pins `ConditionFalse`) — this is the expected, plan-documented task-boundary failure, not a regression. It passes once Task 2's test commit lands._

## Files Created/Modified
- `api/v1alpha3/shared_types.go` - Added `ReasonParentResolved = "ParentResolved"`; updated `ReasonParentRefNotFound`'s doc comment to note the new True polarity
- `internal/controller/milestone_controller.go` - `surfaceParentRefUnresolved` sets `ConditionTrue`; added clear-on-resolve write at the Step 4 parent-resolve success path
- `internal/controller/phase_controller.go` - Same two changes, mirrored for Phase/Milestone-as-parent
- `internal/controller/parentref_surface_test.go` - Flipped both "surfaces" tests to assert True; added two new clear-on-resolve tests

## Decisions Made
- Preserved each reconciler's existing `Status().Update` mechanism for both the surface write and the new clear write — matching Task's separate `MergeFrom`+`Patch` mechanism was explicitly out of scope per the plan and CONTEXT D-04.
- The clear-on-resolve message names the resolved parent (`fmt.Sprintf("parent Project %q resolved", parent.Name)` / `"parent Milestone %q resolved"`) for operator readability, mirroring the surface function's existing message style.
- New tests added directly to the existing `parentref_surface_test.go` (a plain Go `testing` file using a fake client, not Ginkgo/envtest) rather than a new suite — satisfies the "extend existing specs" instruction without any envtest budget concern since this file was never envtest-backed.

## Deviations from Plan

None — plan executed exactly as written, including its explicitly-anticipated Task 1 test-boundary failure.

One clarification on an acceptance-criteria literal match: the plan's Task 1 criterion `grep -c 'ReasonParentResolved' api/v1alpha3/shared_types.go` returning exactly `1` does not hold literally — it returns `2` (the doc comment line and the const definition line both contain the string `ReasonParentResolved`). This matches the file's own pre-existing convention (e.g. `ReasonNoProjectLabel`'s doc comment also restates its own name, so the same grep against that constant would also return 2) — not a defect. Confirmed the definition itself is singular via `grep -c 'ReasonParentResolved = "ParentResolved"'` → `1`.

## Issues Encountered
- `go build ./...` fails on `cmd/tide-demo-init` (`pattern all:fixture: no matching files found`) — a pre-existing, unrelated environmental gap (the fixture directory isn't present in this worktree checkout) with zero Phase-41-08 files touching that package. Verified builds/tests instead via `go build ./api/... ./internal/... ./cmd/manager/... ./cmd/dashboard/...` and `go test ./internal/controller/...`, both fully green.
- The full `internal/controller` package (Ginkgo + plain Go tests, 204 specs) requires envtest binaries not present in a fresh worktree checkout; ran `make setup-envtest` once to fetch them, then re-ran with `KUBEBUILDER_ASSETS` set — full suite green (107s), no environmental workaround needed beyond the one-time binary fetch.
- `golangci-lint` binary not present in this worktree/PATH; substituted `gofmt -l` (clean) + `go vet ./api/... ./internal/controller/...` (clean) on all four touched files.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- REFAC-09 satisfied; `ConditionParentUnresolved` now carries one truth semantics (True == unresolved) across all four kinds that set it (Task, Plan, Milestone, Phase).
- No outstanding follow-up from this plan — the one sanctioned observable behavior change for Phase 41 is complete, documented in both commit bodies, and its consumer sweep is closed (dashboard clean, tests updated, prose confirmed non-stale).
- Ready for the next phase-41 plan (41-09) or milestone close, pending orchestrator's wave-completion bookkeeping.

---
*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Completed: 2026-07-12*

## Self-Check: PASSED

- FOUND: `.planning/phases/41-refactoring-review-non-breaking-cleanup-12-items/41-08-SUMMARY.md`
- FOUND: commit `831d231` (fix: normalize polarity + add ReasonParentResolved)
- FOUND: commit `b3e7155` (test: sweep assertions + clear-on-resolve coverage)
- FOUND: `ReasonParentResolved` constant in `api/v1alpha3/shared_types.go`
- FOUND: `ConditionTrue // True == parent unresolved` in both `milestone_controller.go` and `phase_controller.go`
