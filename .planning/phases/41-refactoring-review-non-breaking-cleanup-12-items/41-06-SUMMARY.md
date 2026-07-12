---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
plan: 06
subsystem: infra
tags: [go, controller-runtime, reconciler-wiring, refactoring, dependency-injection]

# Dependency graph
requires:
  - phase: 41-refactoring-review-non-breaking-cleanup-12-items (plan 03)
    provides: SubagentImage field already deleted from all reconcilers (8-field carrier, not 9)
  - phase: 41-refactoring-review-non-breaking-cleanup-12-items (plan 05)
    provides: checkDispatchHolds extraction on Milestone/Phase/Plan (unrelated field usage, unaffected by this plan)
provides:
  - PlannerReconcilerDeps carrier struct (8 dispatch-tier fields) in internal/controller/dispatch_helpers.go
  - Milestone/Phase/Plan/Project reconcilers all carry Deps PlannerReconcilerDeps instead of 8 direct fields
  - main.go single plannerDeps construction, assigned 4 times (was 4 hand-maintained per-reconciler blocks)
  - cmd/manager wiring-lock tests (TestReconcilerWiringComplete, TestMainWiresDispatcherOnGatedReconcilers) extended for the Deps indirection
  - all internal/controller and test/integration/envtest fixtures updated to construct via Deps
affects: [41-07, 41-08, 41-09, future planner-reconciler work touching Dispatcher/EnvReader/SigningKey/CredproxyImage/TidePushImage/ReporterImage/HelmProviderDefaults/PricingOverridesJSON]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Dispatch-tier deps carrier (mirrors TaskReconcilerDeps): planner reconcilers hold one `Deps PlannerReconcilerDeps` field instead of N individually-wired direct fields"
    - "Single wiring-construction site in main.go (plannerDeps built once, assigned via `Deps: plannerDeps` to all 4 reconcilers) instead of N hand-copied per-reconciler literals"

key-files:
  created: []
  modified:
    - internal/controller/dispatch_helpers.go (new PlannerReconcilerDeps struct)
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/boundary_push.go
    - cmd/manager/main.go
    - cmd/manager/wiring_test.go
    - cmd/manager/wave_dispatcher_wiring_test.go
    - internal/controller/*_test.go (22 fixture files)
    - test/integration/envtest/*.go (5 fixture files)

key-decisions:
  - "Project is included in the carrier (not just Milestone/Phase/Plan) per RESEARCH Pitfall 2 — omitting it would repeat the exact forgotten-Dispatcher bug class (cascade-8) this refactor exists to close"
  - "Test-fixture sweep done via a scripted AST-based text-splice tool rather than hand-editing 32 files — preserves each fixture's original field order, leading comments, and single-line-vs-multi-line style; verified gofmt-clean with zero diffs on every touched file"
  - "3 fixtures using post-construction field assignment (r.TidePushImage = ..., rPhase.HelmProviderDefaults = ...) fixed by hand since they fall outside the composite-literal sweep's scope"

requirements-completed: [REFAC-08]

# Metrics
duration: 71min
completed: 2026-07-12
---

# Phase 41 Plan 06: Consolidate Planner-Reconciler Dispatch Deps Summary

**Collapsed the 8 dispatch-tier fields hand-copied across Milestone/Phase/Plan/Project reconcilers into one `PlannerReconcilerDeps` carrier, built once in main.go and assigned four times — mirrors the existing `TaskReconcilerDeps` pattern.**

## Performance

- **Duration:** 71 min
- **Started:** 2026-07-12T01:52:50-04:00
- **Completed:** 2026-07-12T13:04:59-04:00
- **Tasks:** 3
- **Files modified:** 39

## Accomplishments
- Defined `PlannerReconcilerDeps` in `dispatch_helpers.go` with exactly the 8 dispatch-tier fields (Dispatcher, EnvReader, SigningKey, CredproxyImage, TidePushImage, ReporterImage, HelmProviderDefaults, PricingOverridesJSON), doc-commented to mirror `TaskReconcilerDeps`'s scoping rule (pools/WatchNamespace/Recorder/SharedPVCName stay direct fields)
- Migrated all four planner reconciler structs (Milestone/Phase/Plan/Project) onto `Deps PlannerReconcilerDeps`, sweeping 63 in-body `r.<field>` references to `r.Deps.<field>` across the four controllers plus `boundary_push.go`'s three `maybeTriggerBoundaryPush` wrappers
- Rewired `cmd/manager/main.go` to build `plannerDeps` once and assign it via `Deps: plannerDeps` to all four reconcilers, replacing four hand-maintained CR-01 wiring comment blocks with one construction site
- Extended `wiring_test.go`'s `TestReconcilerWiringComplete` to lock `Deps.Dispatcher`/`Deps.EnvReader` non-nil for all four planner reconcilers (Project gains an `EnvReader` case it lacked before — Phase 7 D-06 already wired it in production, the lock just hadn't caught up)
- Updated `wave_dispatcher_wiring_test.go`'s AST-based regression guard (debug #16) to follow the new `Deps: plannerDeps` indirection back to the literal that actually sets `Dispatcher`, so it still fires correctly
- Swept 27 test-fixture files (22 in `internal/controller`, 5 in `test/integration/envtest`) onto the carrier via a purpose-built AST-based codemod; fixed 3 additional post-construction field-assignment sites by hand

## Task Commits

Each task was committed atomically:

1. **Task 1: Define PlannerReconcilerDeps and migrate the four reconciler structs** - `adf49d5` (feat)
2. **Task 2: Rewire main.go and extend the wiring-lock tests** - `8b9e07c` (feat)
3. **Task 3: Sweep internal/controller and envtest test fixtures to the Deps form** - `7e86417` (test)

_Note: no plan-metadata commit yet — this SUMMARY.md commit is the plan-metadata commit (see below)._

## Files Created/Modified
- `internal/controller/dispatch_helpers.go` - New `PlannerReconcilerDeps` struct (Task 1)
- `internal/controller/milestone_controller.go` - `Deps PlannerReconcilerDeps` field; 14 in-body reference sweeps (Task 1)
- `internal/controller/phase_controller.go` - `Deps PlannerReconcilerDeps` field; 13 in-body reference sweeps (Task 1)
- `internal/controller/plan_controller.go` - `Deps PlannerReconcilerDeps` field; 17 in-body reference sweeps (Task 1)
- `internal/controller/project_controller.go` - `Deps PlannerReconcilerDeps` field; 17 in-body reference sweeps incl. doc comment (Task 1)
- `internal/controller/boundary_push.go` - 3 `maybeTriggerBoundaryPush` wrappers moved to `r.Deps.*` (Task 1)
- `cmd/manager/main.go` - single `plannerDeps` construction; 4 `Deps: plannerDeps` assignments (Task 2)
- `cmd/manager/wiring_test.go` - `TestReconcilerWiringComplete` cases converted to `.Deps.X` accessors + Project.Deps.EnvReader case added (Task 2)
- `cmd/manager/wave_dispatcher_wiring_test.go` - AST guard follows the `Deps: plannerDeps` indirection (Task 2)
- `internal/controller/*_test.go` (22 files) - fixtures moved onto `Deps: PlannerReconcilerDeps{...}` (Task 3)
- `test/integration/envtest/*.go` (5 files) - fixtures moved onto `Deps: controller.PlannerReconcilerDeps{...}` (Task 3)
- `internal/controller/project_boundary_push_test.go`, `project_pushresult_test.go`, `test/integration/envtest/planner_dispatch_test.go` - 8 post-construction field assignments moved to `.Deps.<field>` (Task 3)
- `.planning/phases/41-refactoring-review-non-breaking-cleanup-12-items/deferred-items.md` - logged 2 out-of-scope pre-existing findings (Task 3)

## Decisions Made
- Project is in scope for the carrier (RESEARCH Pitfall 2 honored) — same rationale as the plan's own framing: leaving Project out would repeat the forgotten-Dispatcher bug class this item exists to prevent.
- Chose a scripted AST-based text-splice codemod over hand-editing 32 test fixtures. Rationale: at this file count, manual editing risks silent field-order/comment drift; the codemod extracts each field's exact original source text (including leading comments) and only regenerates the wrapping `Deps: PlannerReconcilerDeps{...}` shell, verified `gofmt -l` clean (zero diffs) on every rewritten file plus zero net change in comment-line counts across the sweep.
- `Deps` field insertion position: at the position of the first carrier field encountered in each literal (not always last), so non-carrier fields that originally followed the carrier block (e.g. `MaxConcurrentReconciles` in `project_controller_test.go`) stay in their original relative position after the carrier fields move out.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed 3 post-construction field assignments the composite-literal sweep couldn't reach**
- **Found during:** Task 3
- **Issue:** `internal/controller/project_boundary_push_test.go` (4 sites) and `project_pushresult_test.go` (3 sites) set `r.TidePushImage = "..."` after constructing `r` via `newTestProjectReconciler()`; `test/integration/envtest/planner_dispatch_test.go` set `rPhase.HelmProviderDefaults = ...` after `newPhaseReconcilerForGateIT()`. These are plain assignment statements, not composite literals, so Task 1's field removal would leave them referencing a field that no longer exists directly on the struct.
- **Fix:** Changed each to `r.Deps.TidePushImage = "..."` / `rPhase.Deps.HelmProviderDefaults = ...`.
- **Files modified:** `internal/controller/project_boundary_push_test.go`, `internal/controller/project_pushresult_test.go`, `test/integration/envtest/planner_dispatch_test.go`
- **Verification:** `go build ./...` (scoped, excluding the pre-existing unrelated `cmd/tide-demo-init` gap) and `go vet ./internal/controller/... ./test/integration/envtest/...` both clean; full test suites green.
- **Committed in:** `7e86417` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary to keep the build green after Task 1's field removal; no scope creep — same mechanical field-name substitution as the rest of Task 3, just via assignment statements instead of composite literals.

## Issues Encountered

- **Acceptance-criterion grep false positive (Task 3):** the plan's acceptance criterion `grep -rn 'Dispatcher:' ... | grep -v 'Deps' | grep -cv 'WaveReconciler\|TaskReconciler'` expects 0, assuming a single-line `Deps: PlannerReconcilerDeps{Dispatcher: ...}` style. The codemod (correctly, matching the codebase's own multi-line convention already used for `TaskReconcilerDeps`) produces multi-line `Deps: PlannerReconcilerDeps{\n\tDispatcher: ...,\n\t...\n}` blocks, so `Dispatcher:` lines don't contain the literal substring `Deps` and the grep returns 73 instead of 0. Verified by inspection that every one of these 73 lines is a correctly-nested `Deps.Dispatcher` field, not a carrier bypass — proven definitively by `go build`/`go vet`/`go test` all passing (a literal direct-field bypass on any of the four reconciler structs is now a hard compile error, since Task 1 deleted those direct fields). No fix needed; documented here as the authoritative resolution of this criterion's literal-vs-intent gap.
- **Missing envtest binaries in this worktree:** `go test ./internal/controller/...` initially failed with `fork/exec /usr/local/kubebuilder/bin/etcd: no such file or directory` — the worktree lacked the `bin -> /Users/justinsearles/Projects/tide/bin` symlink present in the main checkout (gitignored, per-checkout). Created the same symlink locally to run verification, confirmed all 204 specs pass, then removed the symlink before final commit (it was never tracked by git and left no residue).
- **`bin/` symlink cleanup:** the temporary symlink used for the two envtest runs (Task 3 verification + the plan's overall `<verification>` block) was removed after each use; `git status --short` confirmed clean before every commit.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Item 8 (seed) is closed: a future forgotten-Dispatcher-class bug on any planner reconciler now fails `TestReconcilerWiringComplete` at compile/test time instead of silently no-oping in production.
- `internal/controller/dispatch_helpers.go:551,559,566` carries 3 pre-existing `logcheck` findings from Plan 41-05's `checkDispatchHolds` (not touched by this plan) — logged to `deferred-items.md`, available for a future cleanup item.
- `cmd/tide-demo-init`'s missing embedded `fixture/` directory (pre-existing, unrelated to any Phase 41 plan) still blocks a blanket `go build ./...`; scoped builds/tests are the working verification pattern documented in `deferred-items.md`.

---
*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Completed: 2026-07-12*

## Self-Check: PASSED

All key files verified present; all 3 task commit hashes (`adf49d5`, `8b9e07c`, `7e86417`) verified in `git log --oneline --all`.
