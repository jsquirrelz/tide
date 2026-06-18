---
phase: 28-plan-import-core
plan: 02
subsystem: api
tags: [schema, crd, import, controller-gen]
dependency_graph:
  requires: [28-01]
  provides: [ImportSourceRef, ConditionImportComplete, ImportComplete-condition-vocabulary]
  affects: [api/v1alpha2, config/crd/bases, charts/tide-crds]
tech_stack:
  added: []
  patterns: [kubebuilder-validation-markers, controller-gen-deepcopy, helm-crds-helmify]
key_files:
  created:
    - api/v1alpha2/import_types.go
  modified:
    - api/v1alpha2/project_types.go
    - api/v1alpha2/shared_types.go
    - api/v1alpha2/zz_generated.deepcopy.go
    - config/crd/bases/tideproject.k8s_projects.yaml
    - charts/tide-crds/templates/project-crd.yaml
    - charts/tide-crds/templates/milestone-crd.yaml
    - charts/tide-crds/templates/phase-crd.yaml
    - charts/tide-crds/templates/plan-crd.yaml
    - charts/tide-crds/templates/task-crd.yaml
    - charts/tide-crds/templates/wave-crd.yaml
decisions:
  - "charts/tide-crds is regenerated via make helm-crds (helmify from kustomize CRD output), not charts/tide/crds/ which does not exist"
  - "Pre-existing api/v1alpha1 test failures (TestDogfoodManifests_StrictDecode/RequiredFields) are out-of-scope; they predate this plan"
metrics:
  duration: 12m
  completed_date: "2026-06-18"
---

# Phase 28 Plan 02: API Schema Surface (ImportSourceRef + ImportComplete Vocab) Summary

**One-liner:** Added `ImportSourceRef` struct with MinLength-1 CEL markers, `ProjectSpec.ImportSource` optional pointer field, and `ConditionImportComplete` reason/annotation vocabulary mirroring the FailureHalt const block; regenerated CRD manifests and deepcopy via `make generate manifests helm-crds`.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add ImportSourceRef type + ImportSource field + condition constants | 64595c5 | api/v1alpha2/import_types.go (new), project_types.go, shared_types.go |
| 2 | Regenerate deepcopy + CRD manifests | 926f499 | zz_generated.deepcopy.go, config/crd/bases/tideproject.k8s_projects.yaml, charts/tide-crds/templates/*.yaml |

## What Was Built

**Task 1 — Schema types and condition vocabulary**

Created `api/v1alpha2/import_types.go` with `type ImportSourceRef struct` carrying two fields:
- `SeedManifestConfigMap string` (json: `seedManifestConfigMap`, `+kubebuilder:validation:MinLength=1`)
- `SalvagedPVCSubPath string` (json: `salvagedPVCSubPath`, `+kubebuilder:validation:MinLength=1`)

Appended `ImportSource *ImportSourceRef` to `ProjectSpec` in `project_types.go` after `FailureProfile`, matching the `Git *GitConfig` optional-pointer pattern with `+optional` and `json:"importSource,omitempty"`.

Added Phase 28 const block to `shared_types.go` after the FailureHalt block:
- `ConditionImportComplete = "ImportComplete"`
- `ReasonImportSucceeded = "ImportSucceeded"`
- `ReasonImportFailed = "ImportFailed"`
- `ReasonCyclicPlanDetected = "CyclicPlanDetected"` (IMPORT-04 cycle detection reason)
- `AnnotationRetryImport = "tideproject.k8s/retry-import"` (mirrors AnnotationBillingResumedAt pattern)

**Task 2 — Regenerated artifacts**

- `make generate` regenerated `zz_generated.deepcopy.go` with `ImportSourceRef.DeepCopyInto/DeepCopy` methods and updated `ProjectSpec.DeepCopyInto` to nil-guard the new pointer field.
- `make manifests` regenerated `config/crd/bases/tideproject.k8s_projects.yaml` with the `importSource` property schema (both sub-fields with `minLength: 1`).
- `make helm-crds` regenerated `charts/tide-crds/templates/project-crd.yaml` (and all other CRD templates via helmify from kustomize output). Note: the plan referenced `charts/tide/crds/` which does not exist in this repo; the actual CRD chart is `charts/tide-crds/templates/`.

## Verification

```
go build ./api/... → OK
go build ./...    → OK
grep importSource config/crd/bases/tideproject.k8s_projects.yaml → found (minLength: 1 on both sub-fields)
grep importSource charts/tide-crds/templates/project-crd.yaml → found
grep ImportSourceRef api/v1alpha2/zz_generated.deepcopy.go → DeepCopyInto + DeepCopy generated
make generate manifests (idempotent re-run) → no diff
```

## Deviations from Plan

### Auto-discovered issues

**1. [Rule 1 - Observation] charts/tide/crds/ does not exist**
- **Found during:** Task 2
- **Issue:** The plan references `charts/tide/crds/tideproject.k8s_projects.yaml` but that path does not exist. The actual chart CRD lives at `charts/tide-crds/templates/project-crd.yaml` as a separate helmify-generated subchart.
- **Fix:** Used `make helm-crds` (which runs `kustomize build config/crd | helmify charts/tide-crds`) instead of a non-existent copy step. The plan's intent (chart CRD reflects importSource) is fully satisfied.
- **Files modified:** `charts/tide-crds/templates/*.yaml` (all 6 CRDs regenerated via helmify)
- **Commit:** 926f499

### Pre-existing out-of-scope failures

`TestDogfoodManifests_StrictDecode` and `TestDogfoodManifests_RequiredFields` in `api/v1alpha1` fail with `unknown field "failureProfile"` because the dogfood fixture `02-codex-runtime-project.yaml` uses a field introduced in Phase 25 that does not exist in the v1alpha1 schema. This failure exists on commits predating Plan 28-02 and is unrelated to import schema additions.

## Known Stubs

None. This plan is pure schema/CRD — no runtime behavior, no UI, no data flows to wire.

## Threat Surface Scan

No new network endpoints, auth paths, or file access patterns introduced. The `importSource` field is an operator-only Project field — trust boundary is enforced by K8s RBAC on Projects (D-08 layer 1). The MinLength=1 markers on both sub-fields enforce defense-in-depth against blank-path containment escapes (T-28-02-02).

## Self-Check: PASSED

- `api/v1alpha2/import_types.go` exists and contains `type ImportSourceRef struct`
- `api/v1alpha2/project_types.go` contains `ImportSource *ImportSourceRef`
- `api/v1alpha2/shared_types.go` contains `ConditionImportComplete`, `ReasonCyclicPlanDetected`, `AnnotationRetryImport`
- `api/v1alpha2/zz_generated.deepcopy.go` contains `ImportSourceRef` deepcopy methods
- `config/crd/bases/tideproject.k8s_projects.yaml` contains `importSource` with sub-fields and `minLength: 1`
- `charts/tide-crds/templates/project-crd.yaml` contains `importSource`
- Commits 64595c5 and 926f499 confirmed in git log
