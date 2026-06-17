---
phase: 25-global-dispatch-failure-semantics-gates-resumption
plan: "02"
subsystem: controller/dispatch
tags: [global-dag, indegree, coarse-ref, task-reconciler, depgraph, DISP-01, DISP-03, RESUME-01]
dependency_graph:
  requires: [25-01]
  provides: [DISP-01, DISP-03, RESUME-01]
  affects: [task_controller.go, depgraph.go, project_controller.go]
tech_stack:
  added: []
  patterns:
    - "shared scopeResolver struct in depgraph.go reused by ProjectReconciler and TaskReconciler"
    - "coarse-ref fan-out: DependsOn=Plan/Phase/Milestone name expands to all member tasks"
    - "globalDependentsMapper: ancestor-scope-name matching for cross-plan re-enqueue"
    - "computeGlobalIndegree: conservative (unsatisfied if any coarse member not Succeeded)"
key_files:
  created:
    - internal/controller/depgraph.go
    - internal/controller/depgraph_test.go
    - internal/controller/failure_halt.go
    - internal/controller/task_global_dispatch_test.go
  modified:
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go
    - api/v1alpha2/task_types.go
    - api/v1alpha2/zz_generated.deepcopy.go
    - config/crd/bases/tideproject.k8s_tasks.yaml
decisions:
  - "scopeResolver lives in internal/controller (not pkg/dag) to keep dag k8s-free for verify-dag-imports guard"
  - "computeGlobalIndegree: unresolved refs count as unsatisfied (conservative) — prevents ghost dispatches"
  - "globalDependentsMapper builds resolver via ancestor scope names so coarse-ref dependents are re-enqueued"
  - "DISP-02 terminal short-circuit: setFailureHaltIfNeeded fires in gateChecks when Phase=Failed (covers tests that patch status directly)"
  - "TaskSpec.Gates field added to enable per-task gate override (DISP-03 test required it)"
metrics:
  duration: "~5.75 hours (across context window boundary)"
  completed: "2026-06-17"
  tasks_completed: 2
  files_created: 4
  files_modified: 5
---

# Phase 25 Plan 02: Global Dispatch via Shared DepGraph Resolver Summary

Shared coarse-ref fan-out resolver in `depgraph.go` wiring TaskReconciler to the same global Execution DAG that ProjectReconciler wave derivation uses, turning DISP-01, DISP-03, and RESUME-01 envtest specs GREEN.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Extract shared coarse-ref fan-out resolver to depgraph.go | `66d876e` | depgraph.go, depgraph_test.go, failure_halt.go, project_controller.go |
| 2 | Widen TaskReconciler to global dispatch | `a9e939b` | task_controller.go, task_global_dispatch_test.go, task_types.go, zz_generated.deepcopy.go, CRD manifest |

## What Was Built

### Task 1: Shared DepGraph Resolver (depgraph.go)

Extracted the inline `tasksForScope` closure from `assembleProjectDepGraph` into a reusable `scopeResolver` struct with four key functions:

- `buildScopeResolver(tasks, plans, phases, ms)` — builds lookup maps for direct task, plan→tasks, plan→phase, phase→milestone resolution
- `resolveScope(name)` — resolution order: direct task → plan → phase → milestone; returns empty for unresolved refs
- `ancestorScopeNames(taskPlanRef)` — returns (planName, phaseName, msName) for a task given its plan name
- `buildGlobalEdges(resolver, tasks, plans, phases, ms)` — implements the full 6a-6d fan-out with `from\x00to` de-dup

`assembleProjectDepGraph` refactored to call the shared resolver (pure extraction, byte-identical edge output). The D-01 invariant — "TaskReconciler dispatch and ProjectReconciler wave derivation resolve edges through the SAME shared resolver and can never disagree" — is now structurally enforced.

Nine table-driven unit tests in `depgraph_test.go` cover direct/plan/phase/milestone resolution and edge de-dup.

`failure_halt.go` created to resolve compile errors from 25-01 RED scaffold: `checkFailureHalt` reads ConditionFailureHalt; `setFailureHaltIfNeeded` stamps it (conservative profile only, idempotent).

### Task 2: Global TaskReconciler Dispatch

Three new functions in `task_controller.go`:

**`listProjectTasks`** — lists all Tasks project-wide using `owner.LabelProject` label selector. Returns error on empty project name.

**`computeGlobalIndegree`** — builds the shared `scopeResolver`, iterates DependsOn entries, resolves each through the resolver:
- Direct task ref: satisfied when that task is Succeeded
- Coarse ref (plan/phase/milestone): satisfied only when ALL resolved member tasks are Succeeded
- Unresolved ref: counted as unsatisfied (conservative)

**`globalDependentsMapper`** — replaces `siblingsToTaskMapper`. Gets the changed task's project label, lists all project tasks + plans + phases + milestones, builds resolver to obtain ancestor scope names, constructs `matchable = {task.Name, planName, phaseName, msName}`, re-enqueues any project task whose DependsOn intersects the matchable set. UID self-skip guard prevents loops.

Additional changes:
- `checkReadinessGates` updated to call `listProjectTasks` + lists all Plan/Phase/Milestone resources + calls `computeGlobalIndegree`
- DISP-03: task-level gate (`task.Spec.Gates.Task`) takes precedence over project-level gate when set
- `setFailureHaltIfNeeded` wired at Failed terminal short-circuit in `gateChecks` (fires even when task Phase=Failed is set via direct status patch, not just real Job completion)
- `SetupWithManager` rewired to use `globalDependentsMapper`
- `TaskSpec.Gates` field added + deepcopy regenerated + CRD manifest updated

Twelve RED→GREEN unit tests in `task_global_dispatch_test.go` covering listProjectTasks label filtering, computeGlobalIndegree direct/coarse/unresolved paths, and globalDependentsMapper including the critical coarse-ref re-enqueue scenario.

## Verification Results

**Envtest:** 51/51 specs PASSED including:
- DISP-01: cross-plan DependsOn blocks dispatch until global predecessor succeeds
- DISP-02 strict: independent tasks continue when a task fails (both sub-cases)
- DISP-02 conservative: first failure stamps ConditionFailureHalt on Project
- DISP-03: task gate approve holds a globally-ready task; non-dependent flows
- RESUME-01: restart re-derives schedule from Task CRD status (cross-plan chain)

**Guard targets:** `make verify-no-aggregates` and `make verify-dag-imports` both exit 0.

**Kind tests:** `FAIL github.com/jsquirrelz/tide/test/integration/kind` — pre-existing infrastructure flake. The failing test is "initializes the git-http server via demo-remote-init Job" in `medium_http_test.go`, which fails because `ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` is not available locally (requires a prior nightly CI build). The log includes "# Debug session nightly-int-flake-timeout" confirming this is a known intermittent failure predating this plan. All 15 kind specs that could run passed; only this 1 fixture-dependent spec failed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] failure_halt.go created to resolve compile errors from 25-01 scaffold**
- **Found during:** Task 1 (package compilation)
- **Issue:** `failure_halt_test.go` from 25-01 RED scaffold referenced `checkFailureHalt` and `setFailureHaltIfNeeded` which didn't exist, blocking compilation
- **Fix:** Created `failure_halt.go` implementing both functions
- **Files modified:** internal/controller/failure_halt.go (created)
- **Commit:** `66d876e`

**2. [Rule 2 - Missing critical functionality] TaskSpec.Gates field added**
- **Found during:** Task 2 (go test -c compilation)
- **Issue:** `global_dispatch_test.go` from 25-01 used `Gates: tideprojectv1alpha2.Gates{Task: "approve"}` in TaskSpec, but the field didn't exist — compilation failed with "unknown field Gates"
- **Fix:** Added `Gates Gates` field to `TaskSpec` in `task_types.go`; ran `make generate` to regenerate deepcopy and CRD manifest
- **Files modified:** api/v1alpha2/task_types.go, api/v1alpha2/zz_generated.deepcopy.go, config/crd/bases/tideproject.k8s_tasks.yaml
- **Commit:** `a9e939b`

**3. [Rule 1 - Bug] DISP-02 conservative test gate: setFailureHaltIfNeeded at Failed terminal short-circuit**
- **Found during:** Task 2 (envtest DISP-02 conservative spec failing)
- **Issue:** The DISP-02 conservative envtest spec patches task status directly (Phase=Failed), bypassing the real `handleJobCompletion` path where `setFailureHaltIfNeeded` was originally placed. The halt was never stamped in tests.
- **Fix:** Added `setFailureHaltIfNeeded` call at the Failed terminal short-circuit in `gateChecks` — fires whenever the reconciler observes `Phase=Failed`, regardless of whether it came from real Job completion or direct status patch
- **Files modified:** internal/controller/task_controller.go
- **Commit:** `a9e939b`

### Intentional RED tests not fixed

**`resume_failure_test.go`** (`TestResumeRunClearsFailureHalt`, `TestResumeWithoutRetryFailedLeavesFailureHalt`) remains RED. The test file's comment explicitly marks it "RED until 25-03 Task 2" and the 25-03 PLAN.md owns this feature (`tide resume --retry-failed` FailureHalt clear). These tests are intentional scaffolding for the next plan.

## Deferred Items

- `resume_failure_test.go` FailureHalt clear in `cmd/tide/resume.go` — scope of 25-03
- Kind fixture image `ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` not pre-built locally — infrastructure issue tracked separately

## Known Stubs

None — all new functions in depgraph.go and task_controller.go are fully implemented.

## Self-Check: PASSED

- depgraph.go: FOUND
- depgraph_test.go: FOUND
- failure_halt.go: FOUND
- task_global_dispatch_test.go: FOUND
- Commit 66d876e: FOUND (git log)
- Commit a9e939b: FOUND (git log)
- 51/51 envtest specs PASSED: verified from /tmp/25-02-clean-run2.log line 4285
