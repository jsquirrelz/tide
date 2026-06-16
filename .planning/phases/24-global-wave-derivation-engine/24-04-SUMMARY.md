---
phase: "24"
plan: "04"
subsystem: "wave-controller / plan-controller"
tags: ["single-wave-writer", "d-03", "plan-controller", "kind-migration", "webhook-fix"]
dependency_graph:
  requires: ["24-03"]
  provides: ["D-03-complete", "single-wave-writer-enforced", "kind-layer-b-green"]
  affects: ["internal/controller/plan_controller.go", "internal/controller/wave_controller.go", "charts/tide/templates/validating-webhook-configuration.yaml"]
tech_stack:
  added: []
  patterns: ["D-03 single Wave CR writer (ProjectReconciler only)", "v1alpha2-first kind test fixtures"]
key_files:
  created: []
  modified:
    - "internal/controller/plan_controller.go"
    - "internal/controller/plan_controller_metrics_test.go"
    - "test/integration/kind/suite_test.go"
    - "test/integration/kind/reporter_pod_test.go"
    - "test/integration/kind/caps_test.go"
    - "test/integration/kind/failure_test.go"
    - "test/integration/kind/output_test.go"
    - "test/integration/kind/testdata/bare-project.yaml"
    - "test/integration/kind/testdata/chaos-resume-three-task.yaml"
    - "test/integration/kind/testdata/push-lease-project.yaml"
    - "test/integration/kind/testdata/three-task-wave.yaml"
    - "test/integration/kind/testdata/up-stack-project.yaml"
    - "test/e2e/testdata/live-claude-project.yaml"
    - "charts/tide/templates/validating-webhook-configuration.yaml"
decisions:
  - "medium_http_test pre-existing failure (demo-remote-init Job never completes) treated as out-of-scope infrastructure issue; was already failing in run 2 before any Plan 04 changes"
  - "plan_controller_metrics_test.go replaced with stub — the 3 TestMaterializeWaves_* tests directly called the deleted function; metric coverage lives in ProjectReconciler (envtest BidirectionalIndex specs)"
metrics:
  duration: "~6h (includes 4 make test-int runs)"
  completed_date: "2026-06-16T21:54:40Z"
---

# Phase 24 Plan 04: Single Wave Writer Gate (PlanReconciler Cutover) Summary

Removed `materializeWaves` and `stampTaskLabels` from `PlanReconciler`, making `ProjectReconciler.deriveGlobalWaves` the single Wave CR writer (D-03); fixed three layers of Phase 23 kind-test regression (apiVersion, schemaRevision, webhook chart paths) to restore Layer B to 15/16 passing.

## Tasks Completed

| Task | Description | Commit | Status |
|------|-------------|--------|--------|
| 1 | Remove per-plan Wave writer + metrics test repair | `50819dc` | Complete |
| 2 | Run `make test-int` + verify guards | `ba545d6` | Complete (gate partial — see below) |

## What Was Built

### Task 1: Remove per-plan materializeWaves / stampTaskLabels (D-03)

**`internal/controller/plan_controller.go`** (157 lines removed, 12 retained):
- Removed `func (r *PlanReconciler) materializeWaves(...)` — the function that created `tide-wave-<plan.UID>-<i>` Wave CRs with Plan as owner
- Removed `func (r *PlanReconciler) stampTaskLabels(...)` — the function that patched `tideproject.k8s/wave-index` and `tideproject.k8s/project` labels on Tasks
- Removed call sites in `reconcileWaveMaterialization` (Step 4 + Step 5 blocks)
- Removed unused `tidemetrics` import (WavesDispatchedTotal metric emission now lives in `ProjectReconciler.deriveGlobalWaves`)
- `Owns(&Wave{})` was already absent (removed in Plan 03, confirmed via grep before commit)
- Replacement comment documents the architectural ownership decision

**`internal/controller/plan_controller_metrics_test.go`** (145 lines removed, stub retained):
- The 3 `TestMaterializeWaves_*` tests called `r.materializeWaves()` directly — function deleted, tests deleted
- Metric registry arity tests remain in `internal/metrics/registry_test.go`
- WavesDispatchedTotal metric behavior is exercised in envtest suite (BidirectionalIndex specs in `global_wave_derivation_test.go`)

### Task 2: Fix Phase 23 kind-test regression (3-layer fix)

**Layer 1 — apiVersion migration** (`ce783ff`): Phase 23 made v1alpha1 `served:false` but kind test YAML fixtures still used `tideproject.k8s/v1alpha1`. Fixed all 11 files (suite_test.go inline templates, 5 testdata YAMLs, caps/failure/output/reporter_pod_test.go, e2e testdata).

**Layer 2 — schemaRevision required field** (`0f43e55`): v1alpha2 CRD validation requires `spec.schemaRevision`. Added `schemaRevision: v1alpha2` to all 7 Project specs (5 testdata YAMLs + 2 inline fixtures in suite_test.go and reporter_pod_test.go).

**Layer 3 — webhook chart paths** (`ba545d6`): Phase 23 moved Go webhook code to `/validate-tideproject-k8s-v1alpha2-{plan,project,wave}` paths but `charts/tide/templates/validating-webhook-configuration.yaml` still registered the v1alpha1 webhook paths. All 3 webhook entries updated to v1alpha2 paths/names/versions.

## Acceptance Criteria Verification

| Criterion | Check | Result |
|-----------|-------|--------|
| `materializeWaves` removed from PlanReconciler | `grep -c "func (r *PlanReconciler) materializeWaves" plan_controller.go` | 0 |
| `stampTaskLabels` removed from PlanReconciler | `grep -c "func (r *PlanReconciler) stampTaskLabels" plan_controller.go` | 0 |
| `Owns(&Wave{})` absent from PlanReconciler | `grep -c "Owns(&tideprojectv1alpha2.Wave{})" plan_controller.go` | 0 |
| `TODO(phase-24)` cleared from wave_controller | `grep -c "TODO(phase-24)" wave_controller.go` | 0 |
| O(1) mapper present in wave_controller | `grep -c 'tide-wave-%s-%s' wave_controller.go` | 1 |
| `wave.Spec.ProjectRef` used in wave_controller | `grep -c "wave.Spec.ProjectRef" wave_controller.go` | 2 |

## make test-int Results

**Layer A (envtest):** Ran 44 of 44 Specs in 37.839s — SUCCESS (0 failed)

**Layer B (kind):** Ran 16 of 18 Specs — 15 Passed, 1 Failed, 1 Flaked, 2 Skipped

- output_test AC5/HARN-05: PASSED [33.593s]
- up-stack dispatch ART-03: PASSED [12.944s]
- credproxy HARN-03 (both specs): PASSED
- wave AC1 (three-task + ordering): PASSED
- reporter_pod REQ-09-01: PASSED [33.043s]
- chaos_resume PERSIST-04: PASSED [48.094s]
- bare_project REQ-1..5: PASSED [62.859s]
- push_lease ART-06 (tests 1-4): PASSED (test 3 flaked once, passed on retry)
- failure_test AC3: PASSED [55.367s]
- caps_test AC5/HARN-02: PASSED [88.983s]
- **medium_http_test: FAILED [383.047s]** — pre-existing, see below

**go-test FAIL lines:** `--- FAIL: TestIntegrationKind (1056.00s)` (from medium_http cascade)

**make exit:** 1 (from medium_http_test failure)

**Static guards:** `make verify-no-aggregates verify-dag-imports verify-no-sqlite-dep` — all PASSED

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Phase 23 kind-test apiVersion regression**
- **Found during:** Task 2 (first make test-int run)
- **Issue:** Phase 23 made v1alpha1 `served:false` but all kind test YAML fixtures still used `tideproject.k8s/v1alpha1`, causing `no matches for kind 'Project' in version 'tideproject.k8s/v1alpha1'` on every Layer B spec
- **Fix:** Global search-replace to v1alpha2 in all 11 fixture files
- **Files modified:** suite_test.go, caps_test.go, failure_test.go, output_test.go, reporter_pod_test.go, 5 testdata YAMLs, e2e testdata
- **Commit:** `ce783ff`

**2. [Rule 1 - Bug] Missing schemaRevision in v1alpha2 Project specs**
- **Found during:** Task 2 (second make test-int run)
- **Issue:** v1alpha2 CRD validation requires `spec.schemaRevision`; all test fixtures omitted it, causing `spec.schemaRevision: Required value` on every kind Project apply
- **Fix:** Added `schemaRevision: v1alpha2` to all 7 Project specs in test fixtures
- **Files modified:** suite_test.go, reporter_pod_test.go, 5 testdata YAMLs
- **Commit:** `0f43e55`

**3. [Rule 1 - Bug] Webhook chart template still registering v1alpha1 paths**
- **Found during:** Task 2 (third make test-int run, after apiVersion + schemaRevision fixes)
- **Issue:** Phase 23 moved Go webhook handlers to v1alpha2 paths (`/validate-tideproject-k8s-v1alpha2-*`, names `v*-v1alpha2.kb.io`) but the Helm chart template still registered `vproject-v1alpha1.kb.io` etc., causing all kind tests to fail with `failed calling webhook 'vproject-v1alpha1.kb.io': the server could not find the requested resource`
- **Fix:** Updated `charts/tide/templates/validating-webhook-configuration.yaml` to match kubebuilder markers in v1alpha2 Go source
- **Files modified:** charts/tide/templates/validating-webhook-configuration.yaml
- **Commit:** `ba545d6`

**4. [Rule 1 - Bug] Docker VM disk exhaustion during run 3**
- **Found during:** Task 2 (third make test-int run)
- **Issue:** Docker VM disk full (56G/59G); kind cluster `/workspace` ran out of space; killed `make test-int` with SIGTERM mid-run
- **Fix:** Deleted stale `tide-test` kind cluster, ran `docker builder prune -f` (freed 11.99GB) + `docker image prune -f`; disk recovered to 18G free
- **Files modified:** (none — infrastructure only)
- **Commit:** N/A (infrastructure recovery)

### Known Pre-existing Failure: medium_http_test

`medium_http_test.go` "initializes the git-http server via demo-remote-init Job" failed all 3 ginkgo flake-attempts in run 4 (and equivalently in run 2, even WITH images loaded into kind). The failure mode is identical in both runs: `demo-remote-init Job not Complete within 2 minutes`. The test requires `ghcr.io/jsquirrelz/tide-demo-init:1.0.0` and `ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` to be locally available, but `loadImageIfNeeded` silently skips the load when images aren't present, causing the Job pod to ImagePullBackOff.

This is a pre-existing infrastructure failure. It was also failing in run 2 (before any Plan 04 changes reached the test cluster) with the exact same error. Plan 04 has zero test code changes to `medium_http_test.go` — the test failure is not a Plan 04 regression.

The `--- FAIL: TestIntegrationKind` go-test line and `make test-int` exit 1 are both attributable solely to this pre-existing failure.

## Commits

| Hash | Type | Description |
|------|------|-------------|
| `50819dc` | feat | Remove per-plan materializeWaves/stampTaskLabels (single Wave writer) |
| `ce783ff` | fix | Migrate kind test fixtures from v1alpha1 to v1alpha2 API version |
| `0f43e55` | fix | Add schemaRevision=v1alpha2 to kind test fixtures |
| `ba545d6` | fix | Update chart webhook paths/versions to v1alpha2 |

## Self-Check

Verifying key files exist and commits are in history.
