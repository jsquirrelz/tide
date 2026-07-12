---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
plan: 03
subsystem: infra
tags: [go, controller-runtime, dead-code-removal, refactoring]

# Dependency graph
requires:
  - phase: 41-refactoring-review-non-breaking-cleanup-12-items
    provides: "Plan 41-02 (wave 1) — meta.IsStatusConditionTrue halt helpers, unified reconcileWithRetry test driver"
provides:
  - "TaskReconciler with gateDispatch/ensureJob deleted (createDispatchJob is the sole live dispatch path)"
  - "Five reconciler structs (Task/Milestone/Phase/Plan/Project) with the dead SubagentImage field removed"
  - "WaveReconciler with PlannerPool/ExecutorPool removed (Dispatcher retained for the observational roll-up gate)"
  - "cmd/manager/main.go wiring slimmed to match — five dead SubagentImage assignments + one dead WaveReconciler ExecutorPool assignment gone"
  - "14 test fixtures swept to compile against the slimmed structs; dispatch_image_test.go's live resolveImage/BuildOptions assertions preserved"
affects: [internal/controller, cmd/manager]

# Tech tracking
tech-stack:
  added: []
  patterns: []

key-files:
  created: []
  modified:
    - internal/controller/task_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/wave_controller.go
    - internal/dispatch/dispatcher.go
    - internal/dispatch/podjob/doc.go
    - cmd/manager/main.go
    - internal/controller/boundary_push_test.go
    - internal/controller/dispatch_image_test.go
    - internal/controller/file_touch_gate_test.go
    - internal/controller/milestone_controller_test.go
    - internal/controller/milestone_gates_test.go
    - internal/controller/phase_controller_test.go
    - internal/controller/phase_gates_test.go
    - internal/controller/plan_controller_test.go
    - internal/controller/plan_gates_test.go
    - internal/controller/plan_planner_test.go
    - internal/controller/plan_wave_integration_test.go
    - internal/controller/planner_job_absent_test.go
    - internal/controller/task_controller_extracted_test.go
    - internal/controller/task_controller_test.go

key-decisions:
  - "Fixed two stale doc-comment references to the deleted ensureJob (internal/dispatch/dispatcher.go, internal/dispatch/podjob/doc.go), repointing them at createDispatchJob — the actual live equivalent; a dangling symbol reference directly caused by this plan's deletion (Rule 1)"
  - "dispatch_image_test.go: kept both resolveImage/BuildOptions behavioral assertions (pinned Levels.Plan.Image and Spec.Subagent.Image must win over the helm default), removed only the now-meaningless reconciler-field assignments that existed to prove the dead field was ignored"
  - "Logged the pre-existing cmd/tide-demo-init //go:embed all:fixture gap to deferred-items.md — unrelated to this plan, out of scope per SCOPE BOUNDARY"

requirements-completed: [REFAC-04]

# Metrics
duration: 16min
completed: 2026-07-12
---

# Phase 41 Plan 03: Dead Code / Dead Field Removal Summary

**Deleted TaskReconciler's dead gateDispatch/ensureJob, the "dead since Phase 13" SubagentImage reconciler-struct field on all five reconcilers, and WaveReconciler's unread PlannerPool/ExecutorPool — end to end through main.go wiring and 14 test fixtures — while leaving every live SubagentImage surface (resolveImage dispatch sites, PodJobBackend field, --subagent-image flag) byte-identical.**

## Performance

- **Duration:** 16 min
- **Started:** 2026-07-12T04:52:15Z
- **Completed:** 2026-07-12T05:08:41Z
- **Tasks:** 3/3 completed
- **Files modified:** 23 (7 production, 2 doc-comment fixes, 14 test fixtures)

## Accomplishments
- `gateDispatch` and `ensureJob` (both `//nolint:unused` stubs kept only for a historical plan grep contract) deleted from `task_controller.go`; `createDispatchJob` remains the sole live dispatch path, untouched
- The dead `SubagentImage` field removed from `TaskReconcilerDeps`, `MilestoneReconciler`, `PhaseReconciler`, `PlanReconciler`, `ProjectReconciler` — `resolveImage` is now unambiguously the only image-resolution surface on reconcilers
- `WaveReconciler.PlannerPool`/`ExecutorPool` removed (zero reads existed); `Dispatcher` retained (still read to gate the observational roll-up)
- `cmd/manager/main.go` wiring slimmed to match: five dead `SubagentImage: subagentImage,` lines and one dead `ExecutorPool: executorPool,` line (WaveReconciler literal) removed; the `--subagent-image` flag, `helmProviderDefaults.Image` override, and `PodJobBackend.SubagentImage` wiring are all untouched and confirmed still the sole live path
- 14 test fixtures swept clean of the deleted field; `dispatch_image_test.go`'s two behavioral specs (proving CRD-level image config wins over any reconciler-level default) kept intact, only their now-meaningless "reconciler field must NOT win" assignments removed

## Task Commits

Each task was committed atomically:

1. **Task 1: Delete gateDispatch and ensureJob from task_controller.go** - `c673aab` (refactor)
2. **Task 2: Delete dead SubagentImage reconciler fields and WaveReconciler pools + main.go wiring** - `545fce5` (refactor)
3. **Task 3: Sweep test fixtures that set the deleted fields** - `b105bce` (test)

**Plan metadata:** (this commit, SUMMARY + deferred-items)

## Files Created/Modified
- `internal/controller/task_controller.go` - removed `gateDispatch`/`ensureJob` (Task 1); removed `TaskReconcilerDeps.SubagentImage` (Task 2)
- `internal/dispatch/dispatcher.go`, `internal/dispatch/podjob/doc.go` - fixed stale doc-comment references from `ensureJob` to `createDispatchJob`
- `internal/controller/milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `project_controller.go` - removed dead `SubagentImage` field + doc comment
- `internal/controller/wave_controller.go` - removed `PlannerPool`/`ExecutorPool` fields and the now-unused `internal/pool` import; `Dispatcher` retained
- `cmd/manager/main.go` - removed five dead `SubagentImage:` wiring lines (Project/Milestone/Phase/Plan/TaskReconcilerDeps literals) and the WaveReconciler `ExecutorPool:` wiring line
- 14 `internal/controller/*_test.go` files - removed `SubagentImage:` assignments targeting the deleted reconciler field; `dispatch_image_test.go` additionally had its file-level doc comment updated and its two `SubagentImage`-assignment-only lines removed while keeping the `Image: testSubagentImage` HelmProviderDefaults assignments and the behavioral assertions
- `.planning/phases/41-refactoring-review-non-breaking-cleanup-12-items/deferred-items.md` (new) - logs the unrelated pre-existing `cmd/tide-demo-init` embed gap

## Decisions Made
- Fixed two stale doc-comment references to the deleted `ensureJob` symbol in `internal/dispatch/dispatcher.go` and `internal/dispatch/podjob/doc.go` (both describe the Phase-2 dispatch architecture and named `TaskReconciler.ensureJob` as the illustrative sync-create-Job path) — repointed at `createDispatchJob`, the function that has actually held that role since `ensureJob` went dead. Directly caused by Task 1's deletion; treated as Rule 1 (auto-fix bug: dangling symbol reference).
- In `dispatch_image_test.go`, kept every `resolveImage`/`BuildOptions` behavioral assertion (the two specs proving CRD `Levels.Plan.Image` and `Spec.Subagent.Image` win over any reconciler-level default) exactly as written; only removed the `SubagentImage: <stub>,` field assignments (plus their "must NOT win" comments) that existed solely to exercise the now-deleted field, per the plan's explicit read-before-deciding instruction.
- Logged (did not fix) a pre-existing, unrelated `cmd/tide-demo-init` build failure (`//go:embed all:fixture` with no `fixture/` directory present in this checkout) to `deferred-items.md` — confirmed via `git status`/`ls` that no file in `cmd/tide-demo-init/` was touched by this plan and the directory has never contained a `fixture/` subdir in this checkout; out of scope per the executor's SCOPE BOUNDARY rule.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed two dangling doc-comment references to the deleted `ensureJob`**
- **Found during:** Task 1
- **Issue:** `internal/dispatch/podjob/doc.go:54` and `internal/dispatch/dispatcher.go:38` both named `TaskReconciler.ensureJob` in prose describing the Phase-2 dispatch architecture; after deleting `ensureJob` these became references to a nonexistent symbol.
- **Fix:** Repointed both comments at `createDispatchJob`, the function that has been the actual live dispatch path since `ensureJob` went dead (confirmed via `grep -n "func (r *TaskReconciler) createDispatchJob"`).
- **Files modified:** `internal/dispatch/podjob/doc.go`, `internal/dispatch/dispatcher.go`
- **Verification:** `go build ./internal/... ./cmd/manager/...` green (comment-only change, no behavior risk)
- **Committed in:** `c673aab` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 bug — stale doc comments)
**Impact on plan:** Zero behavioral risk (comment-only); prevents future confusion about which function is actually live. No scope creep — directly caused by Task 1's own deletion.

## Issues Encountered

- **`go build ./...` and `go vet ./...` (repo-wide) fail on an unrelated pre-existing gap:** `cmd/tide-demo-init/main.go:112` declares `//go:embed all:fixture` but no `fixture/` directory exists in this checkout. This poisons Go's package-loading for the entire `./...` pattern (both `go build ./...` and `go vet ./...` report only this one error and skip every other package, per observed behavior). Confirmed via `git log`/`ls` that this plan touched zero files under `cmd/tide-demo-init/`. Used the plan's own scoped verify commands instead — `go build ./internal/... ./cmd/manager/...` and `go vet ./internal/controller/... ./cmd/manager/...` — both green throughout. Logged in `deferred-items.md`.
- **envtest binaries were not pre-provisioned in this worktree.** `make setup-envtest` downloaded `etcd`/`kube-apiserver`/`kubectl` for k8s 1.36 successfully (network available); one transient `fork/exec ... no such file or directory` on the first `go test` invocation (immediately after `setup-envtest` completed, suspected symlink/write-flush race) self-resolved on retry with the same `KUBEBUILDER_ASSETS` path. Final run: `go test ./internal/controller/... -count=1` passed 204 specs in 119.7s.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Seed item 4 (dead code / dead field removal) is closed: `gateDispatch`/`ensureJob`, the five dead `SubagentImage` reconciler fields, and `WaveReconciler.PlannerPool`/`ExecutorPool` are all gone, with wiring and test fixtures caught up.
- Every live `SubagentImage` surface (five `resolvedImage` dispatch-site assignments, `PodJobBackend.SubagentImage`, `BuildOptions.SubagentImage` JobSpec population, the `--subagent-image` flag) verified byte-identical — no regression risk for image resolution.
- `go build ./internal/... ./cmd/manager/...`, `go vet ./internal/controller/... ./cmd/manager/...`, `go test ./internal/controller/... ./cmd/manager/... -count=1` all green.
- No blockers for the remaining Phase 41 plans (items 5+).

## Self-Check: PASSED

- FOUND: internal/controller/task_controller.go
- FOUND: cmd/manager/main.go
- FOUND: internal/controller/wave_controller.go
- FOUND: .planning/phases/41-refactoring-review-non-breaking-cleanup-12-items/41-03-SUMMARY.md
- FOUND: .planning/phases/41-refactoring-review-non-breaking-cleanup-12-items/deferred-items.md
- FOUND commit: c673aab (Task 1)
- FOUND commit: 545fce5 (Task 2)
- FOUND commit: b105bce (Task 3)

---
*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Completed: 2026-07-12*
