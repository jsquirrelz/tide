---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
plan: 09
subsystem: refactor
tags: [go, controller-runtime, constants, label-vocabulary, pvc-config]

# Dependency graph
requires:
  - phase: 41-refactoring-review-non-breaking-cleanup-12-items
    provides: waves 1-7 (dead code removal, PlannerReconcilerDeps/TaskReconcilerDeps carriers, gate-chain extraction)
provides:
  - "owner.LabelWavePaused / LabelWaveIndex / LabelAttempt constants beside owner.LabelProject"
  - "repo-wide label-literal sweep across internal/controller, internal/dispatch/podjob, cmd/dashboard/api, cmd/tide"
  - "SharedPVCName field + sharedPVCName() accessor on Milestone/Phase/Plan/Task reconcilers, wired from main.go"
  - "fix: ProjectReconciler.SharedPVCName is now actually wired in main.go (was previously always empty)"
  - "credproxyEndpoint / defaultPlannerIterations constants in dispatch_helpers.go"
affects: [dispatch, dashboard-api, cli, controller]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Label-key constants centralized in internal/owner/label.go; all consumers (controller, dispatch, dashboard API, CLI) import owner.Label* directly instead of re-deriving private per-package consts"
    - "sharedPVCName() accessor pattern (mirrors ProjectReconciler) replicated onto Milestone/Phase/Plan/Task reconcilers"

key-files:
  created: []
  modified:
    - internal/owner/label.go
    - internal/controller/git_writer.go
    - internal/controller/task_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/wave_controller.go
    - internal/controller/push_helpers.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/dispatch_helpers.go
    - internal/dispatch/podjob/backend.go
    - internal/dispatch/podjob/jobspec.go
    - cmd/dashboard/api/execution_dag.go
    - cmd/dashboard/api/waves.go
    - cmd/dashboard/api/waves_test.go
    - cmd/dashboard/api/informer_bridge_test.go
    - cmd/tide/cancel.go
    - cmd/tide/approve.go
    - cmd/tide/resume.go
    - cmd/tide/inspect_wave_run.go
    - cmd/manager/main.go
    - internal/controller/git_writer_test.go
    - internal/controller/boundary_push_test.go
    - internal/controller/plan_wave_integration_test.go
    - internal/controller/push_helpers_test.go

key-decisions:
  - "SharedPVCName landed as a DIRECT field on Milestone/Phase/Plan/Task reconcilers (not inside PlannerReconcilerDeps), matching ProjectReconciler's own established precedent — ProjectReconciler already has both the Deps carrier AND a direct SharedPVCName field, confirming config fields stay direct even post-41-06 carrier consolidation."
  - "Test-file identifiers referencing deleted private consts (labelProject/labelWaveIndex, gitWriterProjectLabelKey) were repointed to raw string literals rather than owner.*, so tests keep pinning the wire format independently of the constants — consistent across cmd/dashboard/api and internal/controller test files."
  - "Fixed an additional, previously-undiscovered instance of the SharedPVCName bug: ProjectReconciler carried the field but main.go never set it at construction, so its own sharedPVCName() accessor always fell back to the default regardless of --workspaces-pvc-name."

requirements-completed: [REFAC-11]

# Metrics
duration: ~20min
completed: 2026-07-12
---

# Phase 41 Plan 09: Centralize Magic Literals (Item 11) Summary

**Label-key constants centralized in internal/owner with a repo-wide sweep (controller, podjob dispatch, dashboard API, CLI), SharedPVCName genuinely wired to every dispatch reconciler including a second latent ProjectReconciler wiring bug, and credproxy endpoint/iterations constants in dispatch_helpers.go.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-12
- **Tasks:** 3/3 completed
- **Files modified:** 25

## Accomplishments
- Added `LabelWavePaused`, `LabelWaveIndex`, `LabelAttempt` constants beside `LabelProject` in `internal/owner/label.go`, then swept every non-test production literal spelling of these four label keys across `internal/controller`, `internal/dispatch/podjob`, `cmd/dashboard/api`, and `cmd/tide` to the `owner.*` constants.
- Deleted all three private duplicate-const anti-patterns (`git_writer.go`'s `gitWriterProjectLabelKey`, `cmd/dashboard/api/waves.go`'s and `cmd/tide/inspect_wave_run.go`'s `labelProject`/`labelWaveIndex` pairs) and repointed every consumer — production code to `owner.*`, test-file identifier consumers to raw string literals (tests keep pinning the wire format independently).
- Corrected `execution_dag.go`'s factually-wrong "avoids an import cycle with internal/owner" comment.
- Added `SharedPVCName` field + `sharedPVCName()` accessor to `MilestoneReconciler`, `PhaseReconciler`, `PlanReconciler`, `TaskReconciler` (mirroring `ProjectReconciler`'s existing pattern) and fixed the 5 remaining hardcoded `PVCName: "tide-projects"` dispatch-site literals.
- Wired `SharedPVCName: sharedPVCName` in `cmd/manager/main.go` for all five planner/executor reconcilers — discovering and fixing that `ProjectReconciler` itself was never wired despite carrying the field, so `--workspaces-pvc-name` was silently dead for it too.
- Added `credproxyEndpoint` and `defaultPlannerIterations` constants to `dispatch_helpers.go`; replaced the 5 hardcoded endpoint literals and 4 hardcoded iteration-default assignments across Milestone/Phase/Plan/Project/Task reconcilers.

## Task Commits

Each task was committed atomically:

1. **Task 1: Label-key constants in internal/owner + repo-wide non-test replacement** - `9ba1120` (refactor)
2. **Task 2: Plumb SharedPVCName to the 4 reconcilers and fix the 6 hardcoded dispatch sites** - `0dc6dfd` (fix)
3. **Task 3: Constants for the credproxy endpoint and planner-iterations default** - `e9a8fca` (refactor)

## Files Created/Modified
- `internal/owner/label.go` - Added LabelWavePaused, LabelWaveIndex, LabelAttempt
- `internal/controller/git_writer.go` - Removed gitWriterProjectLabelKey, uses owner.Label*
- `internal/controller/task_controller.go` - Label literal swap, SharedPVCName field, credproxyEndpoint/defaultPlannerIterations
- `internal/controller/plan_controller.go` - Label literal swap, SharedPVCName field, credproxyEndpoint/defaultPlannerIterations
- `internal/controller/project_controller.go` - Label literal swap, fixed own sharedPVCName() bypass, credproxyEndpoint/defaultPlannerIterations
- `internal/controller/wave_controller.go` - Label literal swap (owner.LabelWaveIndex)
- `internal/controller/push_helpers.go` - gitWriterProjectLabelKey → owner.LabelProject
- `internal/controller/milestone_controller.go` - SharedPVCName field, credproxyEndpoint/defaultPlannerIterations
- `internal/controller/phase_controller.go` - SharedPVCName field, credproxyEndpoint/defaultPlannerIterations
- `internal/controller/dispatch_helpers.go` - New credproxyEndpoint/defaultPlannerIterations constants
- `internal/dispatch/podjob/backend.go` - Label literal swap (owner.LabelProject)
- `internal/dispatch/podjob/jobspec.go` - Label literal swap (owner.LabelAttempt)
- `cmd/dashboard/api/execution_dag.go` - Label literal swap + corrected import-cycle comment
- `cmd/dashboard/api/waves.go` - Deleted private const pair, uses owner.Label*
- `cmd/tide/{cancel,approve,resume,inspect_wave_run}.go` - Label literal swap, deleted second private const pair
- `cmd/manager/main.go` - SharedPVCName wired to Milestone/Phase/Plan/Task/Project reconcilers
- Test files (`waves_test.go`, `informer_bridge_test.go`, `git_writer_test.go`, `boundary_push_test.go`, `plan_wave_integration_test.go`, `push_helpers_test.go`) - identifiers repointed to raw string literals

## Decisions Made
- SharedPVCName is a direct field on each reconciler, not folded into `PlannerReconcilerDeps` — matches the observed `ProjectReconciler` precedent (it already has both the Deps carrier and a direct SharedPVCName field).
- Test files that referenced the deleted private consts by identifier were repointed to raw string literals (not `owner.*`), consistent with the plan's stated philosophy that tests should pin the wire format independently of the constants.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] ProjectReconciler.SharedPVCName was never wired in main.go**
- **Found during:** Task 2
- **Issue:** `ProjectReconciler` already carried a `SharedPVCName` field and `sharedPVCName()` accessor, but `cmd/manager/main.go`'s `ProjectReconciler{...}` construction never set the field — so even after fixing the accessor-bypass at the planner-dispatch call site, `r.sharedPVCName()` always fell back to `defaultSharedPVCName` regardless of `--workspaces-pvc-name` / `TIDE_WORKSPACES_PVC_NAME`. This is the same latent-config-bug class the plan's `truths` explicitly targets ("genuinely honored by EVERY dispatch Job"), just at a wiring site the plan's read_first didn't call out.
- **Fix:** Added `SharedPVCName: sharedPVCName,` to the `ProjectReconciler{...}` literal in `cmd/manager/main.go`.
- **Files modified:** cmd/manager/main.go
- **Verification:** `go build`, `go test ./internal/controller/... ./cmd/manager/...` green; `grep -c 'SharedPVCName:\s*sharedPVCName' cmd/manager/main.go` returns 6 (Project + Milestone + Phase + Plan + Task + Import).
- **Committed in:** 0dc6dfd (Task 2 commit)

**2. [Deviation from inventory, not a bug] task_controller.go had only 1 PVCName dispatch site, not 2**
- **Found during:** Task 2
- **Issue:** The plan's verified inventory listed 6 hardcoded `PVCName: "tide-projects"` sites total (including two in task_controller.go at lines 802 and 1477). At current HEAD, `task_controller.go` has only 1 such site (`820`) — the second was already removed by an earlier wave's dead-code cleanup (item 4: dead `gateDispatch`/`ensureJob` removal).
- **Fix:** No fix needed — fixed all 5 sites that actually exist. `grep -c 'PVCName:\s*r.sharedPVCName()' internal/controller/*.go` totals 5, not 6.
- **Files modified:** N/A (inventory correction only)
- **Verification:** `grep -rn '"tide-projects"' internal/controller --include='*.go' | grep -v _test | grep -vE ':[0-9]+:\s*//'` returns exactly the 2 expected lines (defaultSharedPVCName + importWorkspaceVolume).
- **Committed in:** 0dc6dfd (Task 2 commit)

---

**Total deviations:** 2 (1 auto-fixed bug, 1 inventory-count correction)
**Impact on plan:** The auto-fixed bug closes the same latent-config-bug class the plan targets, at a site not named in the plan's read_first — necessary for the plan's own stated truth ("genuinely honored by EVERY dispatch Job") to actually hold. No scope creep beyond the plan's declared intent.

## Issues Encountered
- `internal/dispatch/podjob/backend.go`'s `PodJobBackend.Run` (explicitly documented as "fixture-only... NOT for the Phase 2 executor path") has its own local `pvcName == "" → "tide-projects"` fallback, independent of `defaultSharedPVCName` (podjob cannot import `internal/controller` — cycle). Surfaced by the wider `rg '"tide-projects"' internal` sweep but out of scope: not one of the plan's named dispatch sites, and the Task 2 acceptance gate is explicitly scoped to `internal/controller`. Left as-is; noted here for future phase consideration if podjob's fixture backend is ever consolidated.
- `go build ./...` / `go vet ./...` at the repo root fail on `cmd/tide-demo-init/main.go:112` (`pattern all:fixture: no matching files found`) — a pre-existing, environment-local condition (the `cmd/tide-demo-init/fixture/` directory is gitignored and materialized via `go:generate`, not present in this worktree). Confirmed unrelated to this plan's changes (file untouched, `git log` shows last change from Phase 5/15). Verified via scoped builds/tests against the plan's actual file set instead: `go build ./internal/... ./cmd/dashboard/... ./cmd/tide/... ./cmd/manager/...` and `go test ./internal/controller/... ./internal/dispatch/... ./cmd/tide/... ./cmd/dashboard/... ./cmd/manager/... -count=1`, both green.
- `make setup-envtest` was required to populate `KUBEBUILDER_ASSETS` (etcd/kube-apiserver binaries) before the `internal/controller` envtest suite could run in this worktree; ran once, then reused for all three tasks' verification.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Item 11 (REFAC-11) is closed: label-key vocabulary is single-sourced in `internal/owner`, `--workspaces-pvc-name` is genuinely honored by every planner and executor dispatch Job (including the newly-fixed ProjectReconciler wiring gap), and the credproxy endpoint / planner-iterations default are single named constants.
- All three commits verified independently with `go build`/`go vet`/`go test` against `internal/controller`, `internal/dispatch/podjob`, `cmd/tide`, `cmd/dashboard/api`, and `cmd/manager` — all green.
- No blockers for the next plan in this phase.

---
*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Completed: 2026-07-12*

## Self-Check: PASSED
- FOUND: internal/owner/label.go
- FOUND: internal/controller/dispatch_helpers.go
- FOUND: cmd/manager/main.go
- FOUND commit: 9ba1120 (Task 1)
- FOUND commit: 0dc6dfd (Task 2)
- FOUND commit: e9a8fca (Task 3)
