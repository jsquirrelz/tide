---
phase: 28-plan-import-core
plan: "04"
subsystem: controller
tags: [import, cycle-detection, seed-dag, envtest, phase28]
dependency_graph:
  requires: ["28-02"]
  provides: [ImportReconciler, BuildImportJob]
  affects:
    - internal/controller/import_controller.go
    - internal/controller/import_jobspec.go
    - internal/controller/import_controller_test.go
tech_stack:
  added: []
  patterns:
    - seed-derived planning DAG cycle detection (ComputeWaves on Milestone/Phase/Plan nodes)
    - two-subPath PVC mount (old read-only + new read-write, no root mount)
    - AlreadyExists-is-success idempotent CR materialization
    - FQ-name rekey ConfigMap (tide-import-rekey-<projectUID>)
    - AnnotationRetryImport reset pattern
key_files:
  created:
    - internal/controller/import_controller.go
    - internal/controller/import_jobspec.go
    - internal/controller/import_controller_test.go
  modified:
    - .gitignore (added /tide-import root binary)
decisions:
  - Seed-derived DAG (Milestone/Phase/Plan nodes + DependsOn edges) used for cycle detection,
    NOT buildGlobalEdges (empty under Task-less D-04 seed — cannot catch Plan-level cycles)
  - ImportImage="" dev mode skips Job and marks ImportComplete=True immediately
  - Rekey table stored in ConfigMap (tide-import-rekey-<projectUID>) per CRD-status-only persistence
  - currentImportState reads from ConditionImportComplete reason+message to avoid extra annotations
  - Owner ref falls back to Project when parent Milestone/Phase not found (reduces LiSTCall fanout)
metrics:
  duration: 10m
  completed_date: "2026-06-18"
  tasks_completed: 2
  files_created: 3
  files_modified: 1
  lines_of_code: 1458
  tests_passing: 4
---

# Phase 28 Plan 04: ImportController State Machine + Envtest Summary

One-liner: ImportReconciler drives Pending→CreatingCRs→CopyingEnvelopes→Complete via seed-derived planning-DAG cycle detection (dag.ComputeWaves on Milestone/Phase/Plan CR nodes) before any client.Create, with two-subPath PVC mount containment and FQ-name rekey ConfigMap.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | ImportReconciler state machine + seed-DAG cycle detection + jobspec | 02c09df | import_controller.go, import_jobspec.go |
| 2 | ImportController envtest — adoption, Plan-level cycle-reject, empty-seed, idempotency | 4fd3453 | import_controller_test.go |

## What Was Built

### Task 1: ImportReconciler + BuildImportJob

**`internal/controller/import_controller.go`** (698 lines)

The ImportReconciler drives a three-phase state machine on Projects with `Spec.ImportSource`:

1. **Pending → CreatingCRs**: reads seed ConfigMap; runs `dag.ComputeWaves` on the seed-derived planning DAG (nodes = all Milestone/Phase/Plan CR names; edges = `{From: dep, To: crName}` from each CR's `Spec.DependsOn`) **before any `client.Create`**. A `*CycleError` → `ReasonCyclicPlanDetected`; an unknown-node (unresolved ref) error → `ReasonImportFailed`; ZERO CRs created in both cases (D-10 per-milestone atomicity). After cycle check passes: materializes Milestone → Phase → Plan CRs in parent-first order with `EnsureOwnerRef` + `AlreadyExists`-is-success semantics; patches `Status.Phase` from seed entry (Anti-Pattern 4); writes rekey ConfigMap `tide-import-rekey-<projectUID>` (D-07 FQ-name keyed).

2. **CreatingCRs → CopyingEnvelopes**: if `ImportImage == ""` skips Job and marks `ImportComplete=True` immediately (dev mode). Otherwise dispatches `BuildImportJob` (deterministic name `tide-import-<projectUID>`).

3. **CopyingEnvelopes → Complete**: polls the import Job; on `JobComplete=True` → `ConditionImportComplete=True/ReasonImportSucceeded`; on `JobFailed=True` → `False/ReasonImportFailed`.

Idempotency guard (D-12): `ConditionImportComplete=True` → immediate no-op return before any work. `AnnotationRetryImport` resets the condition to trigger a re-run.

**`internal/controller/import_jobspec.go`** (220 lines)

`BuildImportJob` constructs the tide-import Job with:
- Two PVC `VolumeMount` entries on the shared `tide-projects` PVC:
  - `/old-workspace` subPath `OldSubPath` `ReadOnly=true` (salvaged envelopes — IMPORT-05)
  - `/new-workspace` subPath `NewSubPath` `ReadOnly=false` (new-UID destination)
  - Never mounts the PVC root (Pitfall 7 containment invariant)
- Rekey ConfigMap mounted as `/rekey/rekey.json`; binary reads via `cat /rekey/rekey.json | tide-import`
- Hardened `SecurityContext`: `RunAsNonRoot=true`, `AllowPrivilegeEscalation=false`, `Capabilities.Drop=["ALL"]` (mirrors `reporter_jobspec.go:156-162`)
- Deterministic name `tide-import-<project.UID>` (AlreadyExists idempotency)
- `tideproject.k8s/role=importer` label (discriminates from planner/reporter/dispatch Jobs)

**CRITICAL DESIGN NOTE — why the seed-derived DAG, not buildGlobalEdges:**

`buildGlobalEdges` (depgraph.go §6a–6c) projects coarse `DependsOn` references onto member Tasks via `resolver.tasksByPlan`. Under the D-04 seed (no Tasks), `tasksByPlan` is empty everywhere, so every Plan/Phase/Milestone `DependsOn` produces zero edges. A `plan-A ↔ plan-B` dependsOn cycle would pass `ComputeWaves` on that edgeless graph silently — bypassing the admission webhook (which `client.Create` skips). The ImportController builds a **new directed graph** with nodes = seed CR names and edges = their `DependsOn` refs, catching cycles at the Milestone/Phase/Plan level before any CR is created.

### Task 2: Envtest (4 specs, all green)

**`internal/controller/import_controller_test.go`** (540 lines)

| Spec | Requirement | What it proves |
|------|-------------|----------------|
| Test 1 (IMPORT-01 adoption) | new-UID CRs + rekey ConfigMap + ImportComplete=True | Happy path: 1MS/1PH/1PL seed materializes with fresh UIDs; rekey table has correct oldUID→newUID mapping |
| Test 2 (IMPORT-04 cycle-reject) | ReasonCyclicPlanDetected + `Consistently` ZERO CRs | Plan-A↔Plan-B cycle (no Tasks — D-04); `Consistently` window proves cycle check fires BEFORE any `client.Create` (D-10 atomicity). This is the spec that would have caught the edgeless-projection bug. |
| Test 3 (IMPORT-05 empty seed) | ImportComplete=True on empty seed | An import with zero entries succeeds cleanly |
| Test 4 (IMPORT-01 idempotency D-12) | Second reconcile after ImportComplete=True is a no-op | UID unchanged; condition stays True; no duplicate creates |

## Verification Results

```
go build ./internal/controller/...                           OK (0 errors)
go test ./internal/controller/... --ginkgo.label-filter='phase28'  4/4 PASS (13.8s)
make verify-dispatch-imports                                 OK
grep -c 'Wave{' internal/controller/import_controller.go    0 (D-09 confirmed)
grep -q 'ComputeWaves' import_controller.go                 FOUND
grep -q 'SubPath' import_jobspec.go                         FOUND (x2)
grep -q 'IsAlreadyExists' import_controller.go              FOUND
```

## Requirements Coverage

| Requirement | Status | Evidence |
|-------------|--------|---------|
| IMPORT-01 (ImportComplete condition + planner-park guard) | Implemented | `setImportCondition`; planner guards in plan 05 |
| IMPORT-03 (rekey table construction) | Implemented | `rekeyTable` → `tide-import-rekey-<projectUID>` ConfigMap (D-07 FQ-name keyed) |
| IMPORT-04 (cycle detection before create) | Implemented | `dag.ComputeWaves` on seed-derived DAG; Test 2 asserts ZERO partial CRs |
| IMPORT-05 (PVC subPath containment) | Implemented | `BuildImportJob` two-subPath mounts, no root mount; Test 3 confirms import Job spec |

## Deviations from Plan

None — plan executed exactly as written.

The D-10 cycle-detection approach (seed-derived graph, not buildGlobalEdges) was specified precisely in the plan; implementation follows the spec exactly.

## Known Stubs

None. All wired functionality operates against real envtest CRDs.

## Threat Surface Scan

No new network endpoints or auth paths introduced. Files operate exclusively on K8s CRDs within the project namespace (import_controller.go) and build a Job spec (import_jobspec.go). Both are within the scope of the plan's threat model (T-28-04-01 through T-28-04-05):

| Threat ID | Mitigation | Verified |
|-----------|-----------|---------|
| T-28-04-01 (cyclic seed) | dag.ComputeWaves on seed-derived DAG before any client.Create | Test 2 (Consistently zero CRs) |
| T-28-04-02 (malicious Kind) | Only Milestone/Phase/Plan created (Kind allowlist at controller) | Test 3 |
| T-28-04-03 (PVC mount scope) | Two subPath mounts only; no root mount | import_jobspec.go + Test 1 |
| T-28-04-04 (re-fire / double-materialize) | AlreadyExists=success + ConditionImportComplete guard | Test 4 |
| T-28-04-05 (budget double-count) | ImportController does not trigger rollup; deferred to plan 05 | Design constraint documented |

## Self-Check: PASSED

All created files exist and all commits are verifiable in git history.

| Check | Result |
|-------|--------|
| `internal/controller/import_controller.go` exists | FOUND |
| `internal/controller/import_jobspec.go` exists | FOUND |
| `internal/controller/import_controller_test.go` exists | FOUND |
| `.planning/phases/28-plan-import-core/28-04-SUMMARY.md` exists | FOUND |
| Commit 02c09df (Task 1) exists | FOUND |
| Commit 4fd3453 (Task 2) exists | FOUND |
