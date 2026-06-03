---
phase: 08-medium-sample-http-transport-and-production-git-transport-po
plan: "03"
subsystem: api-cel-validation
tags: [cel, crd, admission, git-transport, chart-sot]
dependency_graph:
  requires: [08-01]
  provides: [cel-file-rejection-at-admission, chart-crd-updated]
  affects: [api/v1alpha1/project_types.go, config/crd/bases/tideproject.k8s_projects.yaml, charts/tide-crds/templates/project-crd.yaml]
tech_stack:
  added: []
  patterns: [kubebuilder-cel-xvalidation, controller-gen-regen, make-helm-sot]
key_files:
  created: []
  modified:
    - api/v1alpha1/project_types.go
    - config/crd/bases/tideproject.k8s_projects.yaml
    - charts/tide-crds/templates/project-crd.yaml
    - api/v1alpha1/phase3_schema_test.go
decisions:
  - "CEL XValidation rule tightened: file:// dropped, http:// and https:// listed explicitly as separate startsWith checks, git@ retained"
  - "GitConfig.RepoURL Pattern changed from ^(https?://|file:///).+ to ^(https?://|git@).+"
  - "chart SOT maintained via make generate manifests + make helm; no hand-edits to charts/"
metrics:
  duration: "~15min"
  completed: "2026-06-03"
  tasks: 2
  files: 4
---

# Phase 08 Plan 03: CEL targetRepo Validator Tightening — Summary

**One-liner:** CEL XValidation tightened to reject file:// at admission (explicit http://, https://, git@ rules); CRD bases and Helm chart regenerated via controller-gen + make helm; Test A GREEN.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Update CEL markers in api/v1alpha1/project_types.go | 6af5b2f | api/v1alpha1/project_types.go |
| 2 | Regenerate CRD manifests and Helm chart | 6b9f478 | config/crd/bases/tideproject.k8s_projects.yaml, charts/tide-crds/templates/project-crd.yaml, api/v1alpha1/phase3_schema_test.go |

## What Was Done

**Task 1** made two targeted changes to `api/v1alpha1/project_types.go` (markers and comments only, no runtime Go code):

1. `GitConfig.RepoURL` Pattern marker replaced:
   - Old: `// +kubebuilder:validation:Pattern=\`^(https?://|file:///).+\``
   - New: `// +kubebuilder:validation:Pattern=\`^(https?://|git@).+\``

2. `ProjectSpec` XValidation marker replaced:
   - Old: rule `startsWith('http') || startsWith('git@') || startsWith('file://')` with message about "http(s), SSH, or file://"
   - New: rule `startsWith('http://') || startsWith('https://') || startsWith('git@')` with explicit message "file:// is not a supported production transport (go-git's file:// transport requires a system git binary absent from production images)"

The new rule is more precise: two separate startsWith checks for http:// and https:// (the old `startsWith('http')` would technically match `http` without the `://` too), plus git@ for SSH. file:// is removed.

**Task 2** ran the full codegen pipeline:
- `make generate manifests` → controller-gen regenerated `config/crd/bases/tideproject.k8s_projects.yaml`
- `make helm` → hack/helm SOT + new CRD bases → `charts/tide-crds/templates/project-crd.yaml`
- `git diff --quiet charts/` exits 0 after commit (chart SOT clean, only CEL-driven diff)
- `make test` passes (all unit packages ok)
- Envtest admission suite 33/33 PASS (26s isolated run): Test A (file:// rejection) is GREEN

## Verification Results

1. `grep -n "file:// is not a supported production transport" api/v1alpha1/project_types.go` → line 272 (XValidation message)
2. `grep -n 'git@' api/v1alpha1/project_types.go | grep Pattern` → line 211 (new Pattern)
3. `grep -n "not a supported production transport" config/crd/bases/tideproject.k8s_projects.yaml` → line 402 (generated CRD)
4. `grep -n "not a supported production transport" charts/tide-crds/templates/project-crd.yaml` → line 402-403 (chart CRD)
5. `git diff --quiet charts/` → exit 0 (chart SOT clean after commit)
6. `make test` → all packages ok, no FAIL
7. Envtest CEL suite: 33/33 PASS — Test A (file:// rejected) GREEN, Tests B/C/D (https/http/git@ admitted) GREEN

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed TestProjectCRDSchemaHasRepoURLPattern asserting old pattern**

- **Found during:** Task 2 (make test run)
- **Issue:** `api/v1alpha1/phase3_schema_test.go` `TestProjectCRDSchemaHasRepoURLPattern` used regex `pattern:\s+\^\(https\?://\|file:///\)\.\+` to verify the CRD YAML — exactly the old pattern we just replaced. After regeneration, the pattern in the YAML is `^(https?://|git@).+`, so the test failed.
- **Fix:** Updated the test to verify `pattern:\s+\^\(https\?://\|git@\)\.\+` and updated the test comment to document the 08-03 change.
- **Files modified:** api/v1alpha1/phase3_schema_test.go
- **Commit:** 6b9f478 (included in Task 2 commit)

## Known Stubs

None. This plan modifies only CRD markers and their generated outputs.

## Threat Flags

No new security-relevant surface beyond what the plan's threat model covers (T-08-03-01 through T-08-03-04 all mitigated).

## Notes on FAIL-04 Budget Test

The `test/integration/envtest` Ginkgo suite includes a `budget_test.go` FAIL-04 spec that sometimes times out at 60s under load. This is pre-existing (last touched in `b023a26` — not modified by this plan). `make test` uses `-short` which skips the envtest suite, so `make test` exits 0. The isolated 26s envtest run (all 33 specs) passed cleanly. The FAIL-04 timeout only appeared in a full unfiltered envtest run under concurrent load.

## Self-Check

### Created files exist

- [x] `.planning/phases/08-medium-sample-http-transport-and-production-git-transport-po/08-03-SUMMARY.md` — this file

### Key files modified and committed

- [x] `api/v1alpha1/project_types.go` — commit 6af5b2f
- [x] `config/crd/bases/tideproject.k8s_projects.yaml` — commit 6b9f478
- [x] `charts/tide-crds/templates/project-crd.yaml` — commit 6b9f478
- [x] `api/v1alpha1/phase3_schema_test.go` — commit 6b9f478

### Commits exist

- [x] 6af5b2f — feat(08-03): tighten CEL XValidation to reject file:// targetRepo at admission
- [x] 6b9f478 — feat(08-03): regenerate CRD + chart via make generate manifests + make helm

## Self-Check: PASSED
