---
phase: "11"
plan: "03"
subsystem: git-transport-lifecycle
tags: [tide-push, integration, per-wave, plan-controller, project-controller, D-02, D-04, SC-1, SC-3, SC-4, SC-5]
dependency_graph:
  requires:
    - "IntegrateTaskBranches (B4, 11-02/bbba42f)"
    - "EnsureRunBranch (B1, e880a5a)"
    - "AddWorktree (B2, f639340)"
  provides:
    - "runClone --run-branch: EnsureRunBranch + linked worktree provisioning (B5)"
    - "runPush --integrate-task-branches: IntegrateTaskBranches before staging (D-04)"
    - "PlanStatus.IntegratedThroughWave field (per-wave integration gate)"
    - "CloneOptions.RunBranch -> --run-branch arg in buildCloneJob (B6/SC-5)"
    - "PushOptions.IntegrateTaskBranches -> --integrate-task-branches CSV in buildPushJob"
    - "reconcileWaveMaterialization per-wave integration gate (D-02/SC-3)"
    - "triggerWaveIntegrationJob helper"
    - "ReasonWaveIntegrationFailed constant"
  affects:
    - cmd/tide-push/main.go
    - internal/controller/boundary_push.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/push_helpers.go
    - api/v1alpha1/plan_types.go
    - api/v1alpha1/shared_types.go
    - config/crd/bases/tideproject.k8s_plans.yaml
tech_stack:
  added: []
  patterns:
    - "git worktree add (linked worktrees for run branch provisioning)"
    - "gogit.PlainOpenWithOptions(EnableDotGitCommonDir:true) for linked worktree support"
    - "fake.NewClientBuilder with WithIndex for field-indexer-based reconciler tests"
    - "label fast-path on resolveProjectForPlan (mirrors resolveProjectName pattern)"
    - "TDD RED/GREEN: 4 test files across 2 packages"
key_files:
  created:
    - internal/controller/plan_wave_integration_test.go
  modified:
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go
    - api/v1alpha1/plan_types.go
    - api/v1alpha1/shared_types.go
    - config/crd/bases/tideproject.k8s_plans.yaml
    - internal/controller/push_helpers.go
    - internal/controller/push_helpers_test.go
    - internal/controller/boundary_push.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
decisions:
  - "Linked worktree via git worktree add (not full clone) for run branch provisioning — shares object store so task branches in bare repo are visible for integration"
  - "gogit.PlainOpenWithOptions(EnableDotGitCommonDir:true) required for go-git to resolve HEAD in linked worktrees via commondir mechanism"
  - "resolveProjectForPlan gains label fast-path (tideproject.k8s/project label) for test isolation without full Phase->Milestone->Project chain"
  - "triggerWaveIntegrationJob uses deterministic name tide-push-wave-<plan.UID>-<waveIndex> keyed on Plan UID (not Project UID) for idempotency"
  - "Integration Job is tide-push --mode=push with IntegrateTaskBranches set and empty ArtifactPaths (integration-only, no planner artifacts)"
  - "RESPONSIBILITY A checks Status.Failed > 0 BEFORE Status.Succeeded==0 to avoid misclassifying permanently-failed Jobs as running (no livelock)"
metrics:
  duration: "~90min"
  completed: "2026-06-09"
  tasks: 3
  files: 10
---

# Phase 11 Plan 03: Wire B5 + B6 + Per-Wave Integration Summary

**One-liner:** tide-push clone mode now provisions the run branch ref and linked worktree (B5); push mode integrates per-task branches before staging planner artifacts (D-04); PlanReconciler triggers per-wave integration Jobs and gates wave k+1 dispatch on successful completion (D-02/SC-3); project_controller wires CloneOptions.RunBranch from project.Status.Git.BranchName (B6/SC-5).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Failing tests for runClone/runPush integration | 77d6ef9 | cmd/tide-push/main_test.go |
| 1 (GREEN) | Implement --run-branch and --integrate-task-branches | 91c3089 | cmd/tide-push/main.go, main_test.go |
| 2 (RED) | Failing tests for CloneOptions.RunBranch + PushOptions.IntegrateTaskBranches | e4b3956 | internal/controller/push_helpers_test.go |
| 2 (GREEN) | Add fields + buildCloneJob/buildPushJob args + manifests | 5db4def | api/v1alpha1/plan_types.go, push_helpers.go, CRD YAML |
| 3 (RED) | Failing tests for per-wave integration gate | 450d620 | internal/controller/plan_wave_integration_test.go |
| 3 (GREEN) | Per-wave integration logic + boundary push wiring | 58ccdc1 | shared_types.go, boundary_push.go, plan_controller.go, project_controller.go |

## What Was Built

### Task 1: cmd/tide-push/main.go

**pushConfig struct gains:**
- `RunBranch string` — clone-mode: per-run branch for EnsureRunBranch + run worktree (B5)
- `IntegrateTaskBranches []string` — push-mode: task branch names to merge before staging (D-04)

**runClone() new behavior (when --run-branch is set):**
1. Calls `pkggit.EnsureRunBranch(destDir, cfg.RunBranch)` to create the branch ref in the bare repo
2. Calls `git -C destDir worktree add <runWorktreeDir> <runBranch>` to provision a linked worktree
3. Idempotent: tolerates "already" error messages from `git worktree add`

**runPush() new behavior (when --integrate-task-branches is set):**
- Calls `pkggit.IntegrateTaskBranches(bareRepoPath, cfg.Branch, cfg.IntegrateTaskBranches)` BEFORE `gogit.PlainOpenWithOptions` opens the run worktree
- Changed `gogit.PlainOpen` to `gogit.PlainOpenWithOptions(EnableDotGitCommonDir:true)` to support linked worktrees (plain PlainOpen fails on linked worktrees because go-git doesn't resolve HEAD via commondir by default)

**Key deviation discovered (Rule 1 - Bug):** `gogit.PlainOpen` returns "reference not found" on linked worktrees provisioned by `git worktree add`. Fixed by using `PlainOpenWithOptions(EnableDotGitCommonDir:true)`. This is required in production too — existing `setupWorkspace` in tests used full clones, not linked worktrees.

### Task 2: push_helpers.go + api/v1alpha1/plan_types.go

**CloneOptions.RunBranch:** `buildCloneJob` appends `--run-branch=<value>` when non-empty.

**PushOptions.IntegrateTaskBranches:** `buildPushJob` appends `--integrate-task-branches=<CSV>` when non-empty.

**PlanStatus.IntegratedThroughWave int:** Zero-based wave number for idempotency gate. Stamped only after integration Job Status.Succeeded > 0.

**make manifests generate:** CRD YAML regenerated with IntegratedThroughWave field. Exit 0.

### Task 3: reconcileWaveMaterialization + boundary_push.go + project_controller.go

**api/v1alpha1/shared_types.go:** `ReasonWaveIntegrationFailed = "WaveIntegrationFailed"` added alongside existing Reason* constants.

**internal/controller/boundary_push.go:**
- `triggerBoundaryPush` gains `integrateBranches []string` parameter (last arg)
- `triggerWaveIntegrationJob` helper: dispatches a per-wave integration Job with name `tide-push-wave-<plan.UID>-<waveIndex>`, owned by the Plan (not Project), with `IntegrateTaskBranches` set and empty `ArtifactPaths`
- `MilestoneReconciler.maybeTriggerBoundaryPush`: passes nil integrateBranches
- `PhaseReconciler.maybeTriggerBoundaryPush`: passes nil integrateBranches
- `PlanReconciler.maybeTriggerBoundaryPush(ctx, parent, project, taskItems)`: collects `TaskBranchName(task.UID)` for all Succeeded tasks and passes them as integrateBranches (D-04)

**internal/controller/plan_controller.go:**
- Added `pkggit "github.com/jsquirrelz/tide/pkg/git"` import
- `resolveProjectForPlan` gains a label fast-path via `tideproject.k8s/project` label (mirrors `resolveProjectName`), enabling test isolation without full chain setup
- `reconcileWaveMaterialization` per-wave integration block (between materializeWaves and BoundaryDetected):
  - RESPONSIBILITY A: Job exists → check Failed>0 (terminal → patchPlanFailed(WaveIntegrationFailed), no requeue); Succeeded>0 (stamp IntegratedThroughWave, continue); else (running → return RequeueAfter:5s)
  - RESPONSIBILITY B: No Job + wave-k all-Succeeded + IntegratedThroughWave<k+1 → dispatch Job + return RequeueAfter:5s
  - Continues loop if IntegratedThroughWave already covers this wave boundary
- `maybeTriggerBoundaryPush` call in `handlePlannerJobCompletion` updated to pass `nil` (no tasks at planner completion time)

**internal/controller/project_controller.go:** `CloneOptions.RunBranch = project.Status.Git.BranchName` (guarded by `BranchName != ""`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] gogit.PlainOpen fails on linked worktrees (reference not found)**
- **Found during:** Task 1 GREEN, TestRunPushIntegrateBeforeStage
- **Issue:** `gogit.PlainOpen` opens the per-worktree `.git` file correctly but `repo.Head()` fails because go-git doesn't follow the `commondir` mechanism by default. The branch ref is stored in `repo.git/worktrees/<name>/HEAD` but the ref resolution fails without `EnableDotGitCommonDir:true`.
- **Fix:** Changed `gogit.PlainOpen(worktreeDir)` to `gogit.PlainOpenWithOptions(worktreeDir, &gogit.PlainOpenOptions{EnableDotGitCommonDir: true})`.
- **Files modified:** `cmd/tide-push/main.go`
- **Commit:** 91c3089

**2. [Rule 2 - Missing] resolveProjectForPlan label fast-path**
- **Found during:** Task 3 GREEN, TestPlanReconcilerPerWaveIntegration
- **Issue:** `resolveProjectForPlan` walks Phase→Milestone→Project chain, requiring a full fixture hierarchy in tests. `resolveProjectName` already has a `tideproject.k8s/project` label fast-path. Without the same fast-path, fake-client tests cannot resolve the Project without creating Phase + Milestone objects.
- **Fix:** Added label fast-path to `resolveProjectForPlan` matching `resolveProjectName` pattern.
- **Files modified:** `internal/controller/plan_controller.go`
- **Commit:** 58ccdc1

**3. [Rule 1 - Bug] TestRunPushIntegrateBeforeStage test used setupWorkspace (full clone) incompatible with IntegrateTaskBranches (linked worktree)**
- **Found during:** Task 1 GREEN test design
- **Issue:** `setupWorkspace` creates the run worktree as a full clone. `IntegrateTaskBranches` expects a linked worktree off the bare repo so task branches are visible. Redesigned test to use `runClone` (with --run-branch) to provision the workspace before pushing.
- **Files modified:** `cmd/tide-push/main_test.go`
- **Commit:** 91c3089

**4. [Rule 1 - Bug] TaskSpec has no Role field**
- **Found during:** Task 3 RED test writing
- **Issue:** Plan action said `t.Spec.Role == "executor"` but `TaskSpec` has no Role field. All Tasks in a Plan are executor tasks. Dropped the Role check, using only `Status.Phase == "Succeeded"`.
- **Files modified:** `internal/controller/plan_wave_integration_test.go`, `internal/controller/boundary_push.go`
- **Commit:** 58ccdc1

## TDD Gate Compliance

| Gate | Task | Commit | Status |
|------|------|--------|--------|
| RED (test/cmd) | Task 1 | 77d6ef9 | test(11-03): failing tests for runClone/runPush |
| GREEN (feat/cmd) | Task 1 | 91c3089 | feat(11-03): implement --run-branch and --integrate-task-branches |
| RED (test/ctrl) | Task 2 | e4b3956 | test(11-03): failing tests for push_helpers fields |
| GREEN (feat/ctrl) | Task 2 | 5db4def | feat(11-03): add fields + CRD regen |
| RED (test/ctrl) | Task 3 | 450d620 | test(11-03): failing tests for per-wave integration gate |
| GREEN (feat/ctrl) | Task 3 | 58ccdc1 | feat(11-03): per-wave integration gate wiring |

## Test Coverage

| Test | Behavior Covered |
|------|-----------------|
| TestRunCloneModeNoRunBranchIsNoOp | Backward-compat: no --run-branch → no worktree provisioned |
| TestRunCloneProvisions | --run-branch: EnsureRunBranch + run worktree directory created |
| TestRunPushIntegrateBeforeStage | --integrate-task-branches: both task branches visible in run branch after push |
| TestBuildCloneJobRunBranchArg | buildCloneJob: --run-branch=<value> in args when set |
| TestBuildCloneJobNoRunBranch | buildCloneJob: no --run-branch when RunBranch empty |
| TestBuildPushJobIntegrateTaskBranches | buildPushJob: --integrate-task-branches=<CSV> in args |
| TestPlanReconcilerPerWaveIntegration (a) | Job dispatched for wave-0 branches; IntegratedThroughWave still 0 |
| TestPlanReconcilerPerWaveIntegration (b) | Pending Job → requeue, wave-1 not dispatched |
| TestPlanReconcilerPerWaveIntegration (c) | Succeeded Job → stamp IntegratedThroughWave=1 |
| TestPlanReconcilerPerWaveIntegration (d) | Idempotency: no second integration job on re-reconcile |
| TestPlanReconcilerPerWaveIntegration (e) | Failed Job → Plan.Status.Phase="Failed", Reason="WaveIntegrationFailed", no livelock |

## Known Stubs

None — all code paths produce real behavior. The integration flow is fully wired:
- `buildCloneJob` passes `--run-branch` to `tide-push`
- `runClone` calls `EnsureRunBranch` + `git worktree add`
- `runPush` calls `IntegrateTaskBranches` before staging
- `reconcileWaveMaterialization` dispatches integration Jobs and gates on completion
- `project_controller.go` wires `CloneOptions.RunBranch` from `project.Status.Git.BranchName`

## Threat Surface Scan

T-11-03-01 through T-11-03-07 addressed as designed:
- `--run-branch` value is a K8s-validated name + unix timestamp; exec.Command uses positional args (no shell injection)
- `--integrate-task-branches` CSV uses K8s UUIDs as branch names; no shell invoked
- RESPONSIBILITY A checks Status.Failed > 0 BEFORE Status.Succeeded==0 (T-11-03-07: no livelock)
- IntegratedThroughWave stamped only after Job.Status.Succeeded > 0 (T-11-03-06: wave k+1 gated)
- No new network endpoints introduced

## Self-Check: PASSED

All 11 files found. All 6 commits found. Build exits 0. All non-envtest tests pass.
