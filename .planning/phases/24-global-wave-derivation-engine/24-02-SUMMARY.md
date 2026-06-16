---
phase: 24-global-wave-derivation-engine
plan: "02"
subsystem: controller
tags: [fan-out, assembler, cycle-gate, dag, EXEC-01]
dependency_graph:
  requires: [24-01]
  provides: [full-fan-out-assembler, assemble-once-refactor]
  affects: [ProjectReconciler, checkGlobalCycleGate, Reconcile]
tech_stack:
  added: []
  patterns:
    - edgeSet de-duplication map for cycle-safe fan-out
    - pre-assembled (nodes,edges) passed to consumers (Pitfall 7)
    - in-memory scope-resolution maps (tasksByPlan/planToPhase/phaseToMS)
key_files:
  modified:
    - internal/controller/project_controller.go
    - internal/controller/project_controller_cycle_test.go
decisions:
  - assembleProjectDepGraph extended to full D-04 fan-out with List calls for PlanList/PhaseList/MilestoneList
  - checkGlobalCycleGate refactored to accept pre-assembled (nodes,edges) — Pitfall 7 mitigation
  - Reconcile calls assembler once and passes result to cycle gate; _ placeholders for Plan 03 deriveGlobalWaves
  - edgeSet keyed by from+NUL+to for O(1) de-duplication (Pitfall 2)
  - tasksForScope closure resolves direct-task/plan/phase/milestone refs in-memory, returns empty on miss (D-06)
metrics:
  duration: ~25 minutes
  completed: 2026-06-16T19:28:00Z
  tasks_completed: 1
  tasks_total: 1
  files_modified: 2
---

# Phase 24 Plan 02: Full Fan-Out Assembler Summary

## One-liner

Extended `assembleProjectDepGraph` to full in-memory fan-out over all four DependsOn carriers (Task/Plan/Phase/Milestone) with edge de-duplication; refactored `checkGlobalCycleGate` to accept pre-assembled (nodes,edges) so the assembler runs once per reconcile (Pitfall 7).

## What Was Built

### Task 1: Full fan-out resolution in assembleProjectDepGraph (all four dependsOn carriers)

**Commit:** `a1c2153`
**Files:** `internal/controller/project_controller.go`, `internal/controller/project_controller_cycle_test.go`

Extended the conservative Phase-23 task-only assembler to the full D-04 fan-out:

1. **Three extra List calls** per reconcile: `PlanList`, `PhaseList`, `MilestoneList` in the project namespace.

2. **Three in-memory resolution maps** (OQ-2):
   - `tasksByPlan map[string][]string` — planRef → []taskName
   - `planToPhase map[string]string` — plan.Name → phase.Name
   - `phaseToMS map[string]string` — phase.Name → milestone.Name

3. **`tasksForScope(scopeName)` closure** — resolves a DependsOn entry to its task set:
   - Direct task match → `[name]`
   - Plan match → `tasksByPlan[name]`
   - Phase match → union of tasks in all plans whose planToPhase == scopeName
   - Milestone match → transitive union via phaseToMS → planToPhase → tasksByPlan
   - Unresolved → empty slice (conservative, never invents an edge — D-06)

4. **Four DependsOn carrier loops**:
   - `Task.Spec.DependsOn` — fan-out from scope to this task
   - `Plan.Spec.DependsOn` — all tasks in this plan depend on all tasks in referenced scope
   - `Phase.Spec.DependsOn` — all tasks in this phase depend on all tasks in referenced scope
   - `Milestone.Spec.DependsOn` — all tasks in this milestone depend on all tasks in referenced scope

5. **Edge de-duplication** via `edgeSet map[string]struct{}` keyed `from+"\x00"+to` (Pitfall 2 — prevents double-indegree when a coarse ref overlaps with a fine direct ref).

6. **Assemble-once refactor (Pitfall 7)**: `checkGlobalCycleGate` signature changed from `(ctx, project)` to `(ctx, project, nodes, edges)` — it no longer calls the assembler internally. `Reconcile` calls `assembleProjectDepGraph` once and passes the result to both the cycle gate and placeholder `_` assignments for Plan 03's `deriveGlobalWaves`.

7. **Test update**: `project_controller_cycle_test.go` call sites updated to the new two-step pattern (call assembler first, then cycle gate).

## Acceptance Criteria — All Passed

| Check | Result |
|-------|--------|
| `go build ./internal/... ./api/... ./pkg/...` exits 0 | PASS |
| `tasksByPlan\|planToPhase\|phaseToMS` count ≥ 3 | PASS (18 matches) |
| `planList\|phaseList\|msList\|MilestoneList` count ≥ 1 | PASS (21 matches) |
| `.Spec.DependsOn` count ≥ 4 (all four carriers) | PASS (4 matches) |
| `edgeSet` present | PASS (3 matches) |
| `checkGlobalCycleGate` does NOT call `assembleProjectDepGraph` | PASS (0 matches) |
| `make verify-no-aggregates verify-dag-imports verify-no-sqlite-dep` | PASS |
| `TestGlobalCycleDetection` (cycle gate unit tests) | PASS |
| All pure Go unit tests (`go test ./internal/controller/... -run ^Test`) | PASS |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Updated call sites in project_controller_cycle_test.go**
- **Found during:** Task 1 compilation
- **Issue:** `checkGlobalCycleGate` signature changed to accept pre-assembled `(nodes, edges)` but the existing test at `project_controller_cycle_test.go:111,183` still called it with the old 2-arg signature.
- **Fix:** Updated both call sites to call `assembleProjectDepGraph` first and pass the result to `checkGlobalCycleGate`.
- **Files modified:** `internal/controller/project_controller_cycle_test.go`
- **Commit:** `a1c2153` (same commit)

### Notes on Expected RED State

The Wave-0 envtest tests in `test/integration/envtest/global_wave_derivation_test.go` remain RED for this plan, as documented in the plan. These tests assert that Wave CRs (`tide-wave-<project>-0/1/2`) exist — but Wave CR derivation (creating those CRs, stamping labels) is the responsibility of Plan 03's `deriveGlobalWaves` function, which is not yet implemented. The fan-out assembler built here is a prerequisite; Plan 03 consumes `(nodes, edges)` that are now available at the Reconcile call site.

The pre-existing `cmd/tide-demo-init/main.go:112: pattern all:fixture: no matching files found` error causes `go build ./...` to fail. This is unrelated to this plan (confirmed pre-existing via `git stash` regression test). The plan verification uses `go build ./internal/... ./api/... ./pkg/...` which exits 0.

The envtest suite (`TestControllers` Ginkgo suite) in `internal/controller/` fails due to missing etcd at `/usr/local/kubebuilder/bin/etcd` in this worktree environment. This is a pre-existing environment constraint — confirmed by running the same test against the base commit without any changes.

## Known Stubs

**`_ = depNodes; _ = depEdges`** at `project_controller.go:270-271` — The assembled (nodes, edges) are held but not yet used for wave derivation. This is intentional: Plan 03 will replace these blank identifiers with the `deriveGlobalWaves` call. The stub is documented and structurally correct — it makes the Plan 03 integration point explicit.

## Threat Flags

None. The changes are in-memory-only fan-out resolution with no new network endpoints, auth paths, file access patterns, or schema changes. T-24-02-01 (adversarial cycle via coarse refs) is mitigated: `checkGlobalCycleGate` runs on the fan-out-expanded graph BEFORE any derivation, so a coarse-ref cycle is caught and surfaces `CycleDetected` — no dispatch occurs.

## Self-Check

PASSED.
