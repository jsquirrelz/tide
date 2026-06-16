---
phase: 23-schema-migration-cross-scope-dependency-model
plan: "03"
subsystem: controller
tags:
  - schema-migration
  - v1alpha2
  - cycle-detection
  - metrics
  - guards
dependency_graph:
  requires:
    - "23-01 (api/v1alpha2 types: SchemaRevision, ReasonRequiresReinstall, ReasonGlobalCycleDetected)"
  provides:
    - "SCHEMA-03: old-object fail-closed guard (RequiresReinstall + TerminalError)"
    - "DEPS-03: global cross-scope cycle gate (GlobalCycleDetected condition)"
    - "SCHEMA-02: wave metric label locked to global wave index (TestWaveLabel)"
  affects:
    - "23-02 (wave_controller; parallel — no overlap)"
    - "Phase 24 (global Kahn engine; cycle gate is validation-only, no schedule stored)"
tech_stack:
  added: []
  patterns:
    - "reconcile.TerminalError for permanent fail-closed rejection (no requeue storm)"
    - "errors.As(*dag.CycleError) for structured cycle error unwrapping"
    - "Conservative coarse-ref skipping in dep-graph assembly (RESEARCH OQ#3)"
    - "Opportunistic v1alpha2 Get in v1alpha1 Reconcile for guard-only path"
    - "Fake controller-runtime client + stdlib testing.T for fast unit tests"
key_files:
  modified:
    - "internal/controller/project_controller.go"
    - "internal/controller/task_controller.go"
  created:
    - "internal/controller/project_controller_v2_guard_test.go"
    - "internal/controller/project_controller_cycle_test.go"
    - "internal/metrics/wave_label_test.go"
decisions:
  - "Guard methods take *tidev1alpha2.Project; wired into Reconcile via opportunistic v1alpha2 Get that gracefully skips if v1alpha2 scheme not registered or object not found"
  - "Tests use fake client + direct method calls for speed; avoids shared suite_test.go dependency conflict with v1alpha1-only envtest"
  - "makeCycleTask renamed from makeTask to avoid collision with existing makeTask in task_controller_test.go"
metrics:
  duration: "~2h"
  completed: "2026-06-16"
  tasks_completed: 2
  tasks_total: 2
  files_created: 3
  files_modified: 2
---

# Phase 23 Plan 03: v1alpha2 Migration Guards + Wave Label Lock Summary

One-liner: Controller-side SCHEMA-03 fail-closed guard (RequiresReinstall + TerminalError) and DEPS-03 global cycle gate (GlobalCycleDetected condition, involved nodes surfaced) added to ProjectReconciler; SCHEMA-02 wave label confirmed global-sourced with TestWaveLabel arity lock.

## Tasks Completed

### Task 1: Old-object fail-closed guard (SCHEMA-03) + global cross-scope cycle gate (DEPS-03)

**Files:**
- `internal/controller/project_controller.go` — three new methods + guard wiring in Reconcile
- `internal/controller/project_controller_v2_guard_test.go` — TestOldShapeRejection (2 subtests)
- `internal/controller/project_controller_cycle_test.go` — TestGlobalCycleDetection (2 subtests)

**What was built:**

1. `checkSchemaRevisionGuard(ctx, *tidev1alpha2.Project) (blocked bool, result ctrl.Result, err error)`:
   - Checks `project.Spec.SchemaRevision != "v1alpha2"` (absent = v1alpha1-shape signal)
   - Sets `Ready=False/RequiresReinstall` condition, persists via `r.Status().Update`
   - Returns `reconcile.TerminalError(...)` — no requeue, no dispatch

2. `assembleProjectDepGraph(ctx, *tidev1alpha2.Project) ([]dag.NodeID, []dag.Edge, error)`:
   - Lists v1alpha1.Tasks in project namespace with project label
   - Builds task-level dep graph from DependsOn entries
   - Conservative coarse-ref skipping (RESEARCH OQ#3): entries naming unknown tasks are skipped; Phase-24 fan-out can only ADD edges

3. `checkGlobalCycleGate(ctx, *tidev1alpha2.Project) (blocked bool, result ctrl.Result, err error)`:
   - Calls `pkg/dag.ComputeWaves`; discards schedule (PERSIST-03 / verify-no-aggregates)
   - On `*dag.CycleError`: sets `CycleDetected=True/GlobalCycleDetected` with involved nodes in message; NOT TerminalError (cycle is fixable)
   - Wired into Reconcile step 4a via opportunistic v1alpha2 Get

**Commits:** `dc49fcd`

### Task 2: Metric wave label global-sourced confirmation (SCHEMA-02)

**Files:**
- `internal/controller/task_controller.go` — resolveWave comment updated with D-08/SCHEMA-02
- `internal/metrics/wave_label_test.go` — TestWaveLabel (3 subtests)

**What was built:**

- `resolveWave` logic unchanged (already correct)
- Comment documents Phase 23 SCHEMA-02/D-08 resemantics: Wave name = global wave identifier post-23-02
- `TestWaveLabel` asserts: 7 TELEM-03 metrics accept `{project,phase,plan,wave}` (4 labels); resolveWave documents global wave; no `"task"` literal in registry.go

**Commits:** `5339857`

## Verification Results

```
go test ./internal/controller/... -run 'TestOldShapeRejection|TestGlobalCycleDetection' -count=1
→ PASS

go test ./internal/metrics/... -run TestWaveLabel -count=1
→ PASS

go build ./internal/controller/...
→ OK

make verify-no-aggregates
→ OK: no aggregate schedule fields

make verify-dag-imports
→ OK: pkg/dag imports are clean
```

## Acceptance Criteria

- [x] Tests pass: `TestOldShapeRejection`, `TestGlobalCycleDetection`, `TestWaveLabel`
- [x] `RequiresReinstall` in project_controller.go (count=3)
- [x] `reconcile.TerminalError` in project_controller.go (count=2)
- [x] `dag.ComputeWaves` in project_controller.go (count=2)
- [x] `GlobalCycleDetected` in project_controller.go (count=2)
- [x] OQ#3/conservative/coarse/scope ref comment present (count=6)
- [x] `make verify-no-aggregates` exits 0
- [x] Both involved nodes named in cycle condition message
- [x] TestWaveLabel asserts `{project,phase,plan,wave}` for 7 TELEM-03 metrics
- [x] No `"task"` literal in registry.go
- [x] `go build ./internal/controller/...` exits 0
- [x] D-08/SCHEMA-02 documented in task_controller.go (count=5)

## Deviations from Plan

### Implementation Approach Adjustment

**Found during:** Task 1 design

**Issue:** Guard methods require `*tidev1alpha2.Project` but `ProjectReconciler.Reconcile` uses `tideprojectv1alpha1.Project` throughout its body (extensive v1alpha1 usage in reconcileProjectPhase2 and many other methods). Switching the whole Reconcile to v1alpha2 types would be architectural scope.

**Fix applied (Rule 3):** Guard methods take `*tidev1alpha2.Project` directly. Wired into Reconcile via step 4a: opportunistic secondary v1alpha2 Get; if v1alpha2 scheme unregistered or object not found (e.g., legacy envtest suite), guards skip gracefully. Tests call guard methods directly (not via full Reconcile) using a fake client that has both v1alpha1+v1alpha2 registered.

### Test Helper Rename

**Found during:** Task 1 test compilation

**Issue:** `makeTask` already declared in `task_controller_test.go` with incompatible signature. Compile-time collision.

**Fix applied (Rule 3):** Renamed cycle-test helper to `makeCycleTask`.

## Known Stubs

None.

## Self-Check: PASSED
