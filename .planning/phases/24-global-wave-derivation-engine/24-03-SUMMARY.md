---
phase: 24-global-wave-derivation-engine
plan: "03"
subsystem: controller
tags: [global-wave-engine, EXEC-02, EXEC-03, EXEC-04, dag, project-controller, wave-cr]
dependency_graph:
  requires: [24-01, 24-02]
  provides: [deriveGlobalWaves, stampGlobalTaskLabels, global-Wave-CRs, bidirectional-index]
  affects: [ProjectReconciler, WaveReconciler, PlanReconciler, SetupWithManager, envtest-suite]
tech_stack:
  added: []
  patterns:
    - Get/IsNotFound/EnsureOwnerRef/Create/IsAlreadyExists idempotent Wave CR pattern (ported from materializeWaves)
    - exactly-once WavesDispatchedTotal increment on Create with "global"/"global" sentinels
    - client.MergeFrom+Patch skip-if-correct label stamping (ported from stampTaskLabels)
    - O(1) taskToWaveMapper via wave-index label derivation (replaces list-all-Waves)
    - prune loop for stale Wave CRs (WaveIndex >= len(globalWaves))
key_files:
  modified:
    - internal/controller/project_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/wave_controller.go
    - test/integration/envtest/global_wave_derivation_test.go
    - test/integration/envtest/indegree_test.go
decisions:
  - deriveGlobalWaves wired after checkGlobalCycleGate and before dispatcher seam (D-02 ordering)
  - stampGlobalTaskLabels called from within deriveGlobalWaves after Wave CR reconciliation
  - taskToWaveMapper changed from "list all Waves" to O(1) tide-wave-<project>-<waveIndex> derivation
  - SUB-02/FAIL-02 tests updated to wait for ProjectReconciler-created Waves (no longer manually create)
  - makeGlobalWaveTask helper added to stamp globalWaveTestProject label for correct reconciler pickup
metrics:
  duration: ~33 minutes
  completed: 2026-06-16T20:04:55Z
  tasks_completed: 2
  tasks_total: 2
  files_modified: 5
---

# Phase 24 Plan 03: Global Wave Derivation Engine Summary

## One-liner

Added `deriveGlobalWaves` + `stampGlobalTaskLabels` to `ProjectReconciler`, wired after the cycle gate, producing `tide-wave-<project>-<N>` Wave CRs owned by the Project and stamping global `tideproject.k8s/wave-index` labels on Tasks; all 44/44 envtest specs GREEN including the Wave-0 `GlobalDag/GlobalWaveIndex/BidirectionalIndex/WaveRederivation` target tests.

## What Was Built

### Task 1: deriveGlobalWaves + Wave CR reconcile/prune + idempotent metric

**Commit:** `f27f5a0`
**Files:** `internal/controller/project_controller.go`, `internal/controller/plan_controller.go`, `internal/controller/wave_controller.go`

1. **`deriveGlobalWaves(ctx, project, nodes, edges)`** on `ProjectReconciler`:
   - Calls `pkg/dag.ComputeWaves(nodes, edges)` to produce the global wave schedule.
   - For each layer i: creates `tide-wave-<project.Name>-<i>` Wave CR with `WaveSpec{ProjectRef: project.Name, WaveIndex: i}`, owner-ref to Project (`EnsureOwnerRef`, BlockOwnerDeletion).
   - Create path increments `WavesDispatchedTotal.WithLabelValues(project.Name, "global", "global")` exactly once; AlreadyExists and Get-success (replay) paths do NOT increment.
   - Prune loop: lists Waves by `LabelProject` label, deletes those with `WaveIndex >= len(globalWaves)` (Phase 25 should gate prune on Wave.Status.Phase == "Succeeded" per RESEARCH OQ-3 — comment added in code).
   - After Wave CR reconciliation, calls `stampGlobalTaskLabels`.

2. **`stampGlobalTaskLabels(ctx, tasks, globalWaves, projectName)`** on `ProjectReconciler`:
   - Builds name→globalWaveIndex map from `globalWaves`.
   - For each Task: patches `tideproject.k8s/wave-index=<N>` and `tideproject.k8s/project=<projectName>` using `client.MergeFrom(t.DeepCopy()) + r.Patch`. Skip-if-already-correct prevents churn.

3. **Wired in Reconcile** (ordering confirmed via grep):
   - Line 262: `assembleProjectDepGraph` (Plan 02)
   - Line 267: `checkGlobalCycleGate` (Plan 02)
   - Line 271: `deriveGlobalWaves` (Plan 03 — NEW)
   - Line 277: dispatcher seam (`if r.Dispatcher != nil`)

4. **`Owns(&tidev1alpha2.Wave{})` added** to `ProjectReconciler.SetupWithManager` so Project-owned Wave changes re-enqueue the Project for Phase 25 dispatch.

5. **`Owns(&tidev1alpha2.Wave{})` removed** from `PlanReconciler.SetupWithManager` (Pitfall 1 mitigation — prevents spurious Plan reconciles from Project-owned Wave creates/updates).

6. **Four Phase-24 TODOs in `wave_controller.go` closed**:
   - TODO at 104 (owner ref): removed — `deriveGlobalWaves` sets owner ref at create time.
   - TODO at 134 (Wave→Task listing): removed — label query at 152-160 is already correct at global scope.
   - TODOs at 236/248 (`taskToWaveMapper`): replaced "list all Waves in namespace" (O(n)) with O(1) name derivation `tide-wave-<project>-<waveIndex>` using the Task's labels.

### Task 2: stampGlobalTaskLabels GREEN + envtest alignment

**Commit:** `211f821`
**Files:** `test/integration/envtest/global_wave_derivation_test.go`, `test/integration/envtest/indegree_test.go`

**`global_wave_derivation_test.go`:**
- Added `makeGlobalWaveTask` helper that stamps `globalWaveTestProject` as `tideproject.k8s/project` label (vs. `makeTask` which hardcodes `indegreeTestProject`). This ensures `ProjectReconciler`'s `client.MatchingLabels{owner.LabelProject: project.Name}` filter picks up the right Tasks.
- Replaced all 14 `makeTask` calls in global wave tests with `makeGlobalWaveTask`.

**`indegree_test.go` (Rule 1 bug fix — caused by `taskToWaveMapper` O(1) refactor):**
- SUB-02/FAIL-02 tests were manually creating Wave CRs with arbitrary names (`wave-rollup-succ-wave`, `wave-rollup-fail-wave`). After the `taskToWaveMapper` change, the mapper computes `tide-wave-indegree-test-project-0` when a Task updates, which didn't match the manually-created Wave name → Wave reconciler never triggered → rollup stuck.
- Fixed both tests to wait for `ProjectReconciler` to auto-create `tide-wave-indegree-test-project-0`, then patch Task statuses to drive rollup. The manual `k8sClient.Create(wave)` is removed.

## Envtest Results

**Target specs (Wave-0 GREEN):**

```
Ran 44 of 44 Specs in 38.210 seconds
SUCCESS! -- 44 Passed | 0 Failed | 0 Pending | 0 Skipped
--- PASS: TestIntegrationEnvtest (38.21s)
```

No `^--- FAIL` or `^FAIL\s` lines in output.

**Specs that went GREEN** (were RED in Plan 02):
- `GlobalDag: multi-plan project produces global schedule (EXEC-01)` — Wave CRs tide-wave-*-0/1/2 created
- `GlobalWaveIndex: Wave CRs carry global project-scoped indices (EXEC-02)` — WaveIndex/ProjectRef correct
- `BidirectionalIndex: global wave index queryable both directions (EXEC-03)` — task→wave label and wave→tasks selector both pass
- `WaveRederivation: re-derives schedule on task add (EXEC-04)` — wave-2 created after task-z added
- `WaveRederivation: no cached aggregate (PERSIST-03)` — Project retrievable, no Schedule field
- `cross-phase cross-milestone coarse-ref fan-out` — plan-level DependsOn fan-out to wave-1

## Acceptance Criteria — All Passed

| Check | Result |
|-------|--------|
| `go build ./internal/... ./api/... ./pkg/...` exits 0 | PASS |
| `grep -c 'func (r \*ProjectReconciler) deriveGlobalWaves'` returns 1 | PASS |
| `tide-wave-%s-%d` present in non-comment code | PASS (1 match) |
| `WithLabelValues(project.Name, "global", "global")` count ≥ 1 | PASS (1 match) |
| No empty label: `WithLabelValues(project.Name, "", ` for WavesDispatchedTotal | PASS (0 matches) |
| `WaveIndex >= len(` present | PASS (3 matches) |
| `Owns(&tidev1alpha2.Wave{})` in ProjectReconciler | PASS (1 match) |
| `make verify-no-aggregates verify-dag-imports verify-no-sqlite-dep` | PASS |
| `grep -c 'func (r \*ProjectReconciler) stampGlobalTaskLabels'` returns 1 | PASS |
| `client.MergeFrom` in stamper path | PASS (17 matches in file) |
| Envtest GlobalDag/GlobalWaveIndex/BidirectionalIndex/WaveRederivation GREEN | PASS (44/44) |
| Reconcile ordering: assemble (262) → cycle gate (267) → derive (271) → dispatch (277) | PASS |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] makeTask stamps wrong project label in global wave tests**
- **Found during:** Task 2 — first envtest run showed 6 failures; logs showed Wave CRs not being created because Tasks had `indegreeTestProject` label, but `ProjectReconciler` for `globalWaveTestProject` listed by `globalWaveTestProject` label.
- **Issue:** `makeTask` in `indegree_test.go` hardcodes `indegreeTestProject`; the global wave tests called `makeTask` but need `globalWaveTestProject`.
- **Fix:** Added `makeGlobalWaveTask` helper in `global_wave_derivation_test.go` that stamps `globalWaveTestProject`; replaced all 14 `makeTask` calls in the global wave test file.
- **Files modified:** `test/integration/envtest/global_wave_derivation_test.go`
- **Commit:** `211f821`

**2. [Rule 1 - Bug] SUB-02/FAIL-02 broke after taskToWaveMapper O(1) refactor**
- **Found during:** Task 2 — full envtest run showed 1 failure in `indegree_test.go` after global wave tests passed.
- **Issue:** Old `taskToWaveMapper` listed all Waves in namespace; new O(1) mapper computes `tide-wave-<project>-<waveIndex>`. SUB-02/FAIL-02 manually created Waves with arbitrary names (`wave-rollup-succ-wave`), which the new mapper could not find. The WaveReconciler was never re-triggered when Tasks updated.
- **Fix:** Updated SUB-02 and FAIL-02 to wait for `ProjectReconciler` to auto-create the Wave CR (`tide-wave-indegree-test-project-0`), then proceed with Task status patching. Removed manual Wave CR creation.
- **Files modified:** `test/integration/envtest/indegree_test.go`
- **Commit:** `211f821`

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes. All changes are in-memory reconcile logic + CRD lifecycle management. The threat model items from the plan are fully mitigated:

| Threat | Mitigation | Status |
|--------|-----------|--------|
| T-24-03-01 Cached schedule | D-05/PERSIST-03 — no aggregate field; `verify-no-aggregates` green | MITIGATED |
| T-24-03-02 Metric double-count | Exactly-once-on-Create increment; AlreadyExists/Get-replay do NOT increment | MITIGATED |
| T-24-03-03 Wave prune flap | Idempotent reconcile + WorkQueue dedup; Phase 25 comment added | ACCEPTED |
| T-24-03-04 Adversarial cycle | `checkGlobalCycleGate` runs BEFORE `deriveGlobalWaves` | MITIGATED |

## Known Stubs

None. All acceptance criteria met. Wave CR derivation is live and fully connected.

## Self-Check: PASSED

- `internal/controller/project_controller.go` exists: FOUND
- `deriveGlobalWaves` function count = 1: FOUND
- `stampGlobalTaskLabels` function count = 1: FOUND
- Commit `f27f5a0` (Task 1): FOUND
- Commit `211f821` (Task 2): FOUND
- Envtest 44/44 GREEN: VERIFIED
- verify-no-aggregates: PASSED
- verify-dag-imports: PASSED
- verify-no-sqlite-dep: PASSED
