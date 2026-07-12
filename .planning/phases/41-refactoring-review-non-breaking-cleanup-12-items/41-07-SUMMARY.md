---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
plan: 07
subsystem: infra
tags: [go, controller-runtime, kubebuilder, refactoring, dedup]

# Dependency graph
requires:
  - phase: 41-06
    provides: PlannerReconcilerDeps consolidation on Milestone/Phase/Plan/Project reconcilers
provides:
  - "internal/controller/level_status.go: patchLevelStatus, consumeApproveAndResume, countChildren leaf primitives"
  - "15 patch{Level}{Outcome} functions converted to thin wrappers (unchanged names/signatures)"
  - "6 approve-consume sites (Milestone/Phase/Plan x2) delegating to consumeApproveAndResume"
  - "4 countChild* wrappers unified on one Controller-ownerRef+UID predicate"
affects: [internal/controller]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Leaf status-mutation primitive (patchLevelStatus): MergeFrom/optimistic-lock -> optional field set -> SetStatusCondition(s) -> Status().Patch, parameterized by a *string field pointer so both Status.Phase and Status.ValidationState call sites share one body"
    - "consumeApproveAndResume built on top of patchLevelStatus (annotation-consume-first, then a status patch) -- one shared body, N one-line callers, mirroring spawnReporterIfNeeded/triggerBoundaryPush"

key-files:
  created:
    - internal/controller/level_status.go
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go
    - internal/controller/project_controller.go

key-decisions:
  - "countChildren unifies the three Kind+UID ownerRef loops and the one metav1.IsControlledBy check onto a single Controller+UID predicate -- verified behaviorally equivalent because every child CRD's ownerRef in this codebase is stamped via owner.EnsureOwnerRef -> controllerutil.SetControllerReference (always Controller=true with correct Kind), confirmed in both production code and test fixtures."
  - "task_controller.go's approve-consume site is NOT migrated to consumeApproveAndResume -- it never writes the Running/WaveOrLevelPaused status half (only consumes the annotation), so it is not byte-equivalent to the shared two-step. Left inline with a cross-reference comment per the plan's own discretion rule."

patterns-established:
  - "Leaf status-mutation primitive shape (patchLevelStatus) for any future patch{Level}{Outcome}-style function"

requirements-completed: [REFAC-10]

# Metrics
duration: 8min
completed: 2026-07-12
---

# Phase 41 Plan 07: Extract Leaf Status-Mutation Primitives Summary

**One shared `patchLevelStatus` leaf backs all 15 `patch{Milestone,Phase,Plan,Task}{Succeeded,Failed,Rejected,AwaitingApproval,FileTouchMismatch}` wrappers, one shared `consumeApproveAndResume` backs six approve-annotation-consume sites, and one shared `countChildren` backs the four `countChild*` wrappers — all in a new `internal/controller/level_status.go`.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-07-12T13:22:30-04:00 (first task commit)
- **Completed:** 2026-07-12T13:30:03-04:00 (second task commit)
- **Tasks:** 2
- **Files modified:** 6 (1 created, 5 modified)

## Accomplishments

- Extracted `patchLevelStatus` (the MergeFrom/optimistic-lock → optional field set → SetStatusCondition(s) → Status().Patch 12-line body) into `internal/controller/level_status.go`; all 15 `patch*` functions across the Milestone/Phase/Plan/Task reconcilers now delegate to it with unchanged names and signatures — zero call-site churn outside the wrapper bodies.
- Extracted `consumeApproveAndResume` (the approve-annotation consume-first-then-status-patch-to-Running two-step) and migrated all six planner-tier gate hook sites (Milestone/Phase/Plan, each at their AwaitingApproval early-return and their `handleJobCompletion` gate-policy hook) to delegate to it. The D-04 invariant ("approve never jumps a level to Succeeded past its children") is preserved structurally — the helper always returns to `Running`; ChildCount-gated succession stays exclusively the caller's job.
- Extracted `countChildren` (namespace-scoped List + controller-ownerRef+UID filter) backing all four `countChild{Phases,Plans,Tasks,Milestones}` wrappers, unifying two previously-divergent matching predicates.
- Full `internal/controller` package (204 Ginkgo specs + all Go tests) green after both tasks.

## Task Commits

Each task was committed atomically:

1. **Task 1: Extract the patch-level-status leaf and the countChildren body** - `4fca07e` (refactor)
2. **Task 2: Extract consumeApproveAndResume and migrate the approve-consume copies** - `0f8f4b9` (refactor)

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/controller/level_status.go` - New file: `patchLevelStatus`, `consumeApproveAndResume`, `countChildren` leaf primitives
- `internal/controller/milestone_controller.go` - 4 `patch*` wrappers + `countChildPhases` + 2 approve-consume sites converted to thin delegates
- `internal/controller/phase_controller.go` - 4 `patch*` wrappers + `countChildPlans` + 2 approve-consume sites converted to thin delegates
- `internal/controller/plan_controller.go` - 5 `patch*` wrappers + `countChildTasks` + 2 approve-consume sites converted to thin delegates
- `internal/controller/task_controller.go` - 2 `patch*` wrappers converted to thin delegates; approve-consume site left inline with a cross-reference comment (not byte-equivalent)
- `internal/controller/project_controller.go` - `countChildMilestones` converted to a thin delegate
- `.planning/phases/41-refactoring-review-non-breaking-cleanup-12-items/deferred-items.md` - Recorded the pre-existing, unrelated `cmd/tide-demo-init` embed gap re-confirmed during this plan's verification

## Decisions Made

- **`countChildren` unification onto Controller+UID.** The three existing loops (`countChildPhases`/`Plans`/`Tasks`) matched `ref.Kind == "<Kind>" && ref.UID == owner.UID` without checking the `Controller` flag; `countChildMilestones` used `metav1.IsControlledBy` (`Controller==true && UID==owner.UID`, no Kind check). Rather than inventing a fifth predicate or keeping four separate bodies, verified via `internal/owner.EnsureOwnerRef` (calls `controllerutil.SetControllerReference`, always stamping `Controller=true` with the correct `Kind`) and confirmed the same in envtest fixtures (`Controller: &tru` literals) — the two predicates are behaviorally identical in this codebase's single-owner model, so one `Controller+UID` predicate safely backs all four wrappers.
- **`patchLevelStatus`'s `fieldPtr *string` generalizes beyond `Status.Phase`.** `patchPlanFileTouchMismatch` mutates `Status.ValidationState`, not `Status.Phase` — the leaf's field pointer is generic so that call site passes `&plan.Status.ValidationState` instead, with no special-casing needed in the leaf.
- **Task's approve-consume site stays inline.** Read `task_controller.go`'s `checkReadinessGates` approve branch: after `gates.ConsumeApprove` + annotation patch, it falls straight through to `return taskGateResult{project: project}, nil` — it never sets `Status.Phase=Running` or `ConditionWaveOrLevelPaused=False` the way the six planner-tier sites do. Not byte-equivalent to `consumeApproveAndResume`'s two-step, so per the plan's own discretion rule it was left inline with a comment explaining why (rather than force-fitting or silently dropping status parity).

## Deviations from Plan

None — plan executed as written. The one judgment call flagged by the plan itself (Task 2's "migrate if byte-equivalent... otherwise leave inline with a cross-reference comment" for the Task variant) resolved to "leave inline," documented above and inline in the diff.

## Issues Encountered

- **The plan's own Task 1 `<verify>` command (`go test ./internal/controller/... -run 'Gates|Approve|Boundary' -count=1`) does not actually exercise the Ginkgo specs.** The package's Ginkgo suite runs under a single top-level `TestControllers` function; `go test -run` only filters top-level Go function names, not Ginkgo `Describe`/`It` text, and `TestControllers` doesn't match that regex. Running the command literally reports `ok` after executing only 2 unrelated plain-Go tests that happen to match the pattern, silently skipping all 204 Ginkgo specs (a false green). Caught this before trusting it — set up `KUBEBUILDER_ASSETS` via `setup-envtest` and re-ran with `-ginkgo.focus="Gate|Approv|Boundary"`, confirming 25/204 specs actually ran and passed after Task 1, then the full 204/204 after Task 2.
- **`go build ./...` / `go list ./...` / `go vet ./...` at the repo root fail** on a pre-existing, unrelated `cmd/tide-demo-init` gitignored-fixture embed gap (`//go:embed all:fixture` with no `fixture/` dir present until `go generate` materializes it) — confirmed present before any Phase 41 commit and already tracked in `deferred-items.md` from Plans 41-03/41-06. A single failing embed pattern anywhere in the module aborts `go list ./...` entirely (returns zero packages), so verification used explicit scoped paths instead: `go build ./internal/controller/... ./api/...` and `go vet ./internal/... ./api/... ./cmd/manager/... ./cmd/dashboard/... ./pkg/...`, both clean.
- **`golangci-lint` was not run** — the binary is not installed in this environment and `make golangci-lint` would require a network fetch + custom-plugin build outside this plan's scope; `go vet` (clean) and `gofmt -l` (clean) were used as the available substitutes.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Seed item 10 is closed at the leaf level. Phase 41's remaining refactoring-review items (per `41-PATTERNS.md`) are otherwise unaffected by this plan; `internal/controller` package is fully green (204/204 Ginkgo specs, all Go tests, `go vet`/`gofmt` clean) and ready for the next wave or phase closeout.

---
*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Completed: 2026-07-12*

## Self-Check: PASSED

All created/modified files and both task commit hashes (`4fca07e`, `0f8f4b9`) verified present.
