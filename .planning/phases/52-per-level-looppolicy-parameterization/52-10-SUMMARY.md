---
phase: 52-per-level-looppolicy-parameterization
plan: 10
subsystem: controller
tags: [level-verify, phase-controller, milestone-controller, project-controller, envtest, ginkgo, esc-03]

# Dependency graph
requires:
  - phase: 52-per-level-looppolicy-parameterization (plan 08)
    provides: the shared level_verify.go dispatch/consume/exhaustion state machine (maybeRunLevelVerify, levelVerifyTarget)
provides:
  - phase_controller.go/milestone_controller.go/project_controller.go wired to maybeRunLevelVerify at every pre-Succeeded seam
  - LevelPhaseVerifying re-entry routing added to Phase/Milestone's reconcilePlannerDispatch (Project needed no equivalent fix)
  - level_verify_dispatch_test.go — 7 envtest Ginkgo specs proving SC2 end-to-end at all three levels
  - verify_halt_test.go ESC-03 regression extension (Phase-level sibling/FailureHalt-absence proof)
affects: [52-11]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Verify-before-publish seam insertion: maybeRunLevelVerify call precedes every boundary/artifact push trigger and every patch{Level}Succeeded call, restructuring shared trailing returns into per-branch returns so the 1:1 maybeRunLevelVerify:patchSucceeded call-site count holds file-wide"
    - "LevelPhaseVerifying re-entry routing at the top of reconcilePlannerDispatch (mirrors Plan's existing pattern) so a level parked mid-verify keeps getting reconciled instead of silently no-opping behind the tasks-exist idempotency guard"
    - "checkProjectComplete's signature grew a leading handled bool + ctrl.Result (4 return values) so the caller can short-circuit into the level-verify result without touching Project's heavier init/branch-name lifecycle"

key-files:
  created:
    - internal/controller/level_verify_dispatch_test.go
  modified:
    - internal/controller/phase_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/project_controller.go
    - internal/controller/verify_halt_test.go

key-decisions:
  - "Goal (the level-appropriate observable-outcome goal text) reuses Project.Spec.OutcomePrompt for all three levels — PhaseSpec/MilestoneSpec carry no authored goal/prompt field of their own, and reading the phase-brief artifact off the run branch is out of this plan's file scope (files_modified carries no pkg/git change)"
  - "ParentSpanID resolves per level's own existing AGENT-span-parent lookup: Phase re-fetches its parent Milestone's MilestoneTraceSpanID (mirrors emitTaskSpanOnce's fetch), Milestone reuses the already-in-hand Project's ProjectTraceSpanID, Project stays the zero value (trace root) — so the EVALUATOR span lands as a sibling of the level's own AGENT span, never its child"
  - "checkProjectComplete's signature changed from (bool, error) to (handled bool, res ctrl.Result, complete bool, err error) — the only call site (reconcilePhase3Lifecycle) needed a way to short-circuit into the level-verify machinery's ctrl.Result without threading a second return path"
  - "Added LevelPhaseVerifying routing to Phase/Milestone's reconcilePlannerDispatch (not originally listed as a plan action) — Rule 2 auto-add: without it, a level transitioned to Verifying would wedge forever since the existing tasks-exist idempotency guard silently no-ops on every subsequent reconcile. Project needed no equivalent fix — its Status.Git.BranchName-gated passthrough already re-enters checkProjectComplete unconditionally on every reconcile"

patterns-established:
  - "Level-verify target-builder helper per reconciler ({phase,milestone,project}LevelVerifyTarget) — keeps the seam call sites to one line (`if handled, res, vErr := maybeRunLevelVerify(...); handled { return res, vErr }`) with zero gate-policy conditionals in the controller files"

requirements-completed: [ESC-01]

# Metrics
duration: 45min
completed: 2026-07-20
---

# Phase 52 Plan 10: Wire Level-Verify Into Phase/Milestone/Project Summary

**Every Phase/Milestone/Project pre-Succeeded seam now dispatches an independent verifier before publishing — SC2 (observable-outcome verification with escalate-only exhaustion) is live end-to-end, proven by 7 new envtest Ginkgo specs plus an ESC-03 sibling-isolation regression.**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-07-20T04:47Z (base commit 2618a524)
- **Completed:** 2026-07-20T05:31:33-04:00
- **Tasks:** 2
- **Files modified:** 5 (3 controllers + 1 new test file + 1 extended test file)

## Accomplishments
- Wired `maybeRunLevelVerify` (52-08's shared machinery) into every `patch{Level}Succeeded` call site in `phase_controller.go` (4 sites) and `milestone_controller.go` (4 sites), plus the sole `checkProjectComplete` seam in `project_controller.go` — verify always runs BEFORE the boundary/artifact push trigger (T-52-31), and zero `MaxIterations`/`EscalationPolicy` conditionals leaked into the controller files (SC3 preserved).
- Closed a functional gap the plan's task list didn't call out explicitly but that Rule 2 (auto-add missing critical functionality) required: added `LevelPhaseVerifying` re-entry routing to Phase and Milestone's `reconcilePlannerDispatch` (mirroring Plan's own existing routing) — without it, a level parked in `Verifying` would never be reconciled past that point, since the pre-existing tasks-exist idempotency guard silently no-ops. Project needed no equivalent fix, since its `Status.Git.BranchName`-gated passthrough already re-enters `checkProjectComplete` unconditionally on every subsequent reconcile.
- Proved the wiring end-to-end with 7 new envtest Ginkgo specs (`level_verify_dispatch_test.go`) covering all 6 plan-specified cases (a)-(f), including the full `requireApproval` park→approve→Succeeded→convergence-guard arc and the off-switch pin.
- Extended `verify_halt_test.go` (previously plain `testing.T`-only) with a new Ginkgo spec proving ESC-03 one level up from Task: a Phase-level `VerifyHalt` never touches a sibling Phase's status and never stamps `ConditionFailureHalt`.

## Task Commits

1. **Task 1: Wire the three pre-Succeeded seams** - `3f9f7096` (feat)
2. **Task 2: LevelVerify Ginkgo specs + ESC-03 regression extension** - `abe4eb4e` (test)

## Files Created/Modified
- `internal/controller/phase_controller.go` - 4 `maybeRunLevelVerify` seam calls (leaf, boundary-detected, fallback-detected, fallback-leaf) + `phaseLevelVerifyTarget` helper + `LevelPhaseVerifying` re-entry routing
- `internal/controller/milestone_controller.go` - same shape one level up, `milestoneLevelVerifyTarget` helper
- `internal/controller/project_controller.go` - `checkProjectComplete` signature extended to `(handled, res, complete, err)`, `projectLevelVerifyTarget` helper
- `internal/controller/level_verify_dispatch_test.go` (new) - 7 Ginkgo specs (a)-(f2) proving the wired seams end-to-end at all three levels
- `internal/controller/verify_halt_test.go` - new Ginkgo `Describe` block (ESC-03 sibling regression), file's Ginkgo/Gomega imports added

## Decisions Made
- **Goal text = `Project.Spec.OutcomePrompt`** for all three levels. Neither `PhaseSpec` nor `MilestoneSpec` carries an authored goal/prompt field, and reading the phase-brief artifact from the run branch would require git access this plan's file scope doesn't include — the Project's outcome prompt is the closest authored goal text reachable from every level's object graph.
- **`ParentSpanID` resolution mirrors the level's own existing AGENT-span-parent lookup** rather than inventing a new pattern: Phase does a fresh `Get` on its parent Milestone (same shape as `emitTaskSpanOnce`'s own fetch), Milestone reuses the already-in-hand `project.Status.ProjectTraceSpanID`, Project stays the zero value (it is the trace root).
- **`checkProjectComplete`'s signature grew from `(bool, error)` to `(handled bool, res ctrl.Result, complete bool, err error)`.** The single call site needed a clean way to short-circuit into the level-verify machinery's `ctrl.Result` (e.g. a cap-hit requeue) without threading it through the existing 2-value contract or duplicating `BoundaryDetected`'s check at the call site.
- **Restructured the two `if detected {...} else if ... else {...}` fallback chains** (in both `phase_controller.go` and `milestone_controller.go`) into flat early-return sequences so every `patch{Level}Succeeded` call site is textually paired 1:1 with its own preceding `maybeRunLevelVerify` call — satisfies the plan's `grep -c` count-matching acceptance criterion without changing any observable branch behavior.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical Functionality] Added `LevelPhaseVerifying` re-entry routing to Phase/Milestone's `reconcilePlannerDispatch`**
- **Found during:** Task 1, while tracing what happens on the reconcile AFTER `maybeRunLevelVerify` transitions a level to `Verifying`
- **Issue:** Neither `phase_controller.go` nor `milestone_controller.go`'s `reconcilePlannerDispatch` had a case for `Status.Phase == LevelPhaseVerifying`. Since the level already owns ≥1 child (that's how the boundary was reached), the pre-existing tasks-exist idempotency guard would match and return a silent no-op on every subsequent reconcile — the dispatched verifier Job's completion would never be consumed, and the level would wedge in `Verifying` forever. Plan's own `must_haves` truth ("Succeeded only fires after an APPROVED verdict") would be unsatisfiable without this routing.
- **Fix:** Added a `LevelPhaseVerifying` case at the top of each function (mirroring `plan_controller.go`'s existing, analogous routing for the Plan level, `plan_controller.go:201-204`) that re-invokes `maybeRunLevelVerify` and falls through to `patch{Level}Succeeded` on `handled=false`.
- **Files modified:** `internal/controller/phase_controller.go`, `internal/controller/milestone_controller.go`
- **Verification:** Ginkgo specs (b), (c), (d), (f1) in `level_verify_dispatch_test.go` each drive a SECOND reconcile after the verifier Job completes and assert the terminal state — none would pass without this routing.
- **Committed in:** `3f9f7096` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 missing critical functionality)
**Impact on plan:** Necessary for SC2's core truth to hold at runtime; no scope creep — same files, same seam, discovered while implementing the plan's own instruction.

## Issues Encountered
- Fresh worktree had no envtest binaries (`bin/k8s` absent) — ran `make setup-envtest` once to provision them before the full suite would run; unrelated to any code change in this plan.
- `golangci-lint` binary was not pre-built in the worktree — ran `make golangci-lint` once (custom-plugin build, ~2 min) before `./bin/golangci-lint run ./internal/controller/...` could execute; returned 0 issues.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- SC2 is live at all three newly-verified levels (Phase/Milestone/Project) through the one shared `level_verify.go` machine; ESC-03 (VerifyHalt is a distinct halt class) is now regression-pinned at the Phase level in addition to Task.
- Plan 52-11 (kind concurrency spec for the worktree-checkout init container, depends on 52-05/52-09/52-10) can proceed — this plan's wiring is what 52-11's live proof exercises.
- No blockers. Full envtest suite (274 specs across `internal/controller`) green with these changes; `go vet`/`gofmt`/`golangci-lint` clean.

---
*Phase: 52-per-level-looppolicy-parameterization*
*Completed: 2026-07-20*
