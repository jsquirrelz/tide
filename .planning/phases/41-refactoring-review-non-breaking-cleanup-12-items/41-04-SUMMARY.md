---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
plan: 04
subsystem: api
tags: [go, kubernetes, crd, status-phase, controller-runtime]

# Dependency graph
requires:
  - phase: 41-refactoring-review-non-breaking-cleanup-12-items
    provides: "Plans 41-01..41-03 landed (halt helpers via meta.IsStatusConditionTrue, reconcileWithRetry test driver, dead SubagentImage/WaveReconciler pool removal) — base HEAD for this plan"
provides:
  - "LevelPhase* Status.Phase constants (Pending/Running/Succeeded/Failed/AwaitingApproval/ZeroMembers) in api/v1alpha3/shared_types.go, shared by Milestone/Phase/Plan/Task/Wave"
  - "Full mechanical sweep of non-test Status.Phase literal sites in internal/controller and cmd (tide + dashboard) onto the new constants"
affects: [internal/controller, cmd/tide, cmd/dashboard, api/v1alpha3]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "LevelPhase* const block in api/v1alpha3/shared_types.go — mirrors the existing Project Phase* precedent (project_types.go:460-486) but kept in a separate namespace since the five level kinds share ONE vocabulary while Project has its own distinct one"

key-files:
  created: []
  modified:
    - api/v1alpha3/shared_types.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go
    - internal/controller/project_controller.go
    - internal/controller/wave_controller.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/artifact_push.go
    - internal/controller/git_writer.go
    - cmd/tide/approve.go
    - cmd/tide/watch.go
    - cmd/tide/resume.go
    - cmd/tide/inspect_wave_run.go
    - cmd/dashboard/api/projects.go
    - cmd/dashboard/api/plans.go
    - cmd/dashboard/api/waves.go
    - cmd/dashboard/api/tasks.go
    - cmd/dashboard/api/execution_dag.go

key-decisions:
  - "Constants live in a single LevelPhase* block in api/v1alpha3/shared_types.go (the cross-kind file), not per-kind duplicate blocks — avoids a PhasePhaseSucceeded collision for the Phase kind (D-03 placement decision, made at plan-authoring time)"
  - "project_controller.go, and the entire cmd tier, alias api/v1alpha3 as tidev1alpha3 (not tideprojectv1alpha3 like most internal/controller files) — used each file's own existing alias rather than introducing a second import"
  - "cmd/tide/watch.go's one Project.Status.Phase site uses the EXISTING tidev1alpha3.PhasePending (Project's own vocabulary), not the new LevelPhasePending — the variable there (p) is a *Project, a different vocabulary namespace entirely"
  - "import_controller.go's importStateFailed (importStatePhase = \"Failed\") is left untouched — it's a distinct internal state-machine type tracked via Project.Status.Conditions, not a Status.Phase site on any of the five level kinds; out of the plan's own vocabulary scope despite surfacing in the raw grep"
  - "\"Dispatching\" left as a literal in cmd/dashboard/api/plans.go and waves.go — never assigned by any controller, explicitly out of the minted vocabulary per plan instruction; counts unchanged (2 each: 1 comment + 1 code)"
  - "Extended Task 3's sweep beyond the plan's named cmd files (execution_dag.go, tasks.go, inspect_wave_run.go) — the plan's own read_first instruction treats the vocabulary grep as authoritative (mirroring Task 2's phrasing), and the must_haves truth requires every non-test site in cmd, not just the six named files"

requirements-completed: [REFAC-01]

# Metrics
duration: 18min
completed: 2026-07-12
---

# Phase 41 Plan 04: Typed Status.Phase Constants Summary

**Introduced `LevelPhase*` string constants for the five level kinds' `Status.Phase` field and mechanically swept ~100 non-test literal sites across `internal/controller` and `cmd` onto them, with zero CRD schema change.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-07-12T01:14:00Z (approx, worktree setup + plan read)
- **Completed:** 2026-07-12T01:32:00Z
- **Tasks:** 3
- **Files modified:** 19

## Accomplishments
- Added a documented `LevelPhase*` const block to `api/v1alpha3/shared_types.go` (`LevelPhasePending`, `LevelPhaseRunning`, `LevelPhaseSucceeded`, `LevelPhaseFailed`, `LevelPhaseAwaitingApproval`, `LevelPhaseZeroMembers`) with zero `make manifests` drift, proving the change is CRD-schema-neutral
- Swept every non-test `Status.Phase` literal in `internal/controller` (9 files) onto the new constants, including the bonus `task_controller.go` site that now uses the existing `tideprojectv1alpha3.PhaseBudgetExceeded`
- Swept every non-test `Status.Phase` literal in `cmd/tide` and `cmd/dashboard/api` (9 files) onto the new constants, correctly distinguishing the one `Project.Status.Phase` site (`watch.go`, uses Project's own `PhasePending`) from the Task/Milestone/Phase/Plan sites (use `LevelPhase*`)
- Full `internal/controller` envtest suite (204/204 specs) and the full `cmd` tier test suite stay green after both sweeps — behavior invariance proven, not assumed

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the LevelPhase\* constant block to api/v1alpha3/shared_types.go** - `8c1db6d` (feat)
2. **Task 2: Sweep internal/controller non-test literal sites** - `8ffd1f9` (feat)
3. **Task 3: Sweep cmd/tide and cmd/dashboard literal sites** - `81fac27` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified
- `api/v1alpha3/shared_types.go` - New `LevelPhase*` const block (six constants), zero CRD manifest drift
- `internal/controller/milestone_controller.go` - 9 Status.Phase sites → `LevelPhase*`
- `internal/controller/phase_controller.go` - 9 Status.Phase sites → `LevelPhase*`
- `internal/controller/plan_controller.go` - 11 Status.Phase sites → `LevelPhase*` (2 empty-reset sites left as `""`)
- `internal/controller/task_controller.go` - 15 Status.Phase sites → `LevelPhase*`, plus the bonus `PhaseBudgetExceeded` site
- `internal/controller/project_controller.go` - 1 site (`tidev1alpha3.LevelPhaseSucceeded`, using the file's own alias)
- `internal/controller/wave_controller.go` - 9 sites (aggregator `phase` variable + switch cases) → `LevelPhase*`
- `internal/controller/dispatch_helpers.go` - 3 sites in `checkParentApproval` → `LevelPhaseAwaitingApproval`
- `internal/controller/artifact_push.go` - 1 switch-case site in `plannerMaterialized`
- `internal/controller/git_writer.go` - 1 branch-collection filter site
- `cmd/tide/approve.go` - 11 sites (targetPhase, findFailedLevel×4, findAwaiting\*×4)
- `cmd/tide/resume.go` - 4 sites (`retryFailedLevels` Failed-check per level kind)
- `cmd/tide/watch.go` - 5 sites (1 Project `PhasePending`, 4 Milestone switch-case values)
- `cmd/tide/inspect_wave_run.go` - 1 site (`defaultStatus` display normalization)
- `cmd/dashboard/api/projects.go` - 2 sites (active-milestone-count predicates)
- `cmd/dashboard/api/plans.go` - 2 sites (display normalization + "Running" half of a Dispatching-or-Running check)
- `cmd/dashboard/api/waves.go` - 2 sites (display normalization + "Running" half of `isRunningPhase`)
- `cmd/dashboard/api/tasks.go` - 1 site (display normalization)
- `cmd/dashboard/api/execution_dag.go` - 1 site (display normalization)

## Decisions Made
- Kept `LevelPhase*` in one block in `shared_types.go` rather than per-kind blocks, per the plan's own D-03 placement decision (documented in the plan file itself, not re-litigated here)
- Used each file's existing `api/v1alpha3` import alias (`tideprojectv1alpha3` in most `internal/controller` files, `tidev1alpha3` in `project_controller.go` and the entire `cmd` tier) rather than introducing a second alias anywhere
- Left `import_controller.go`'s `importStateFailed` untouched — confirmed it's a distinct `importStatePhase` type (its own doc comment: "tracked via Project.Status.Conditions"), not a `Status.Phase` site, so it falls outside this plan's vocabulary even though the raw grep flags it
- `cmd/tide/watch.go`'s `p.Status.Phase` (a `*Project`) uses the pre-existing `tidev1alpha3.PhasePending`, not the new `LevelPhasePending` — these two constants share the string value `"Pending"` but belong to different semantic vocabularies (Project vs. the five level kinds); using the Project constant here is the semantically correct choice, not just the type-checking one

## Deviations from Plan

### Auto-fixed Issues

None — no bugs, missing functionality, or blocking issues were hit during the sweep; every literal site the plan or its grep instructions identified was either a straightforward one-to-one constant substitution or a pre-confirmed out-of-scope exception.

### Scope Extensions (not auto-fixes, but worth flagging)

**1. Task 3 swept 3 files beyond its `<files>` header list**
- **Found during:** Task 3 (cmd sweep)
- **What:** The plan's `<files>` frontmatter/header for Task 3 names 6 files (`approve.go`, `watch.go`, `resume.go`, `projects.go`, `plans.go`, `waves.go`), but the task's own `<read_first>` grep surfaced 3 more non-test files with in-scope literal sites: `cmd/dashboard/api/execution_dag.go`, `cmd/dashboard/api/tasks.go`, `cmd/tide/inspect_wave_run.go` — all doing the identical "normalize empty `Status.Phase` to Pending for display" pattern already present in `plans.go`/`waves.go`.
- **Why included:** The plan's `must_haves.truths` states "Every non-test Status.Phase-context literal ... in internal/controller and cmd is replaced" — a phase-wide claim, not file-scoped — and Task 2's read_first uses the identical "authoritative site list, may include files not named above" framing. Leaving these 3 files with unconverted literals would have left the acceptance-criteria grep non-clean and the truth claim false.
- **Files modified:** `cmd/dashboard/api/execution_dag.go`, `cmd/dashboard/api/tasks.go`, `cmd/tide/inspect_wave_run.go`
- **Verification:** Same mechanical substitution as the named files; `go build ./...` and the full cmd test suite green; comment-filtered vocabulary grep across all of `cmd` returns 0 code-position hits.
- **Committed in:** `81fac27` (Task 3 commit)

---

**Total deviations:** 0 auto-fixes; 1 scope extension (additive, same pattern, explicitly licensed by the plan's own truth statement).
**Impact on plan:** None negative — the extension makes the plan's own acceptance criteria and must_haves fully satisfied rather than partially satisfied. No architectural change, no new files, no schema change.

## Issues Encountered
- `go build ./...` and `go list ./cmd/...` both fail (pre-existing, unrelated) on `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found` — a missing `//go:embed` fixture directory. Confirmed via `.planning/phases/.../deferred-items.md` (already logged by Plan 41-03) that this predates this plan and touches no file this plan modifies. Worked around by scoping `go build`/`go test`/`go vet` to `./api/...`, `./internal/controller/...`, and the individual `cmd/*` packages excluding `tide-demo-init`.
- Local `go test ./internal/controller/...` initially failed with `fork/exec /usr/local/kubebuilder/bin/etcd: no such file or directory` — the default envtest asset path isn't populated in this worktree. Resolved by running `make setup-envtest` and passing the resolved `KUBEBUILDER_ASSETS` explicitly (same invocation `make test` uses), matching CLAUDE.md's "confirm the command exists" guidance rather than treating it as a code regression.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `api/v1alpha3.LevelPhase*` is now the canonical vocabulary for Status.Phase on Milestone/Phase/Plan/Task/Wave; future plans touching these reconcilers should extend this vocabulary rather than reintroducing string literals.
- Seed item 1 (of 12 non-breaking cleanup items) is closed. No blockers for subsequent phase-41 plans.

---
*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Completed: 2026-07-12*
