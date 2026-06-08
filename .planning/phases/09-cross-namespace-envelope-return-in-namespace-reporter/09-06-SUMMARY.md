---
phase: 09-cross-namespace-envelope-return-in-namespace-reporter
plan: 06
subsystem: controller
tags: [reporter, materialize, helm, integration-test, REQ-09-01]
dependency_graph:
  requires: [09-04, 09-05]
  provides: [REQ-09-01-complete]
  affects: [project_controller, milestone_controller, phase_controller, plan_controller, charts/tide]
tech_stack:
  added: []
  patterns:
    - "buildReporterJob: deterministic-named, project-namespace-scoped reader Job (mirrors buildCloneJob)"
    - "idempotent Get-then-Create spawn pattern (mirrors triggerBoundaryPush)"
    - "T-09-13: tideproject.k8s/role=reporter label discriminates reader Jobs from dispatch Jobs"
    - "ValidationState=Validated stamp moved to handlePlannerJobCompletion after reporter spawn"
key_files:
  created:
    - internal/controller/reporter_jobspec.go
    - internal/controller/reporter_jobspec_test.go
    - test/integration/kind/reporter_pod_test.go
  modified:
    - internal/controller/project_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - cmd/manager/main.go
    - charts/tide/values.yaml
    - charts/tide/templates/deployment.yaml
    - hack/helm/tide-values.yaml
    - hack/helm/augment-tide-chart.sh
    - test/integration/kind/failure_test.go
    - test/integration/kind/suite_test.go
decisions:
  - "ValidationState=Validated stamp moved: was inside MaterializeChildCRDs block; now set in handlePlannerJobCompletion after reporter spawn (reporter is in-flight → Tasks will appear)"
  - "justMaterialized=true: milestone handler re-uses the boolean for boundary-detection wait after reporter spawn, same semantics as before"
  - "AlreadyExists on reporter Job Create = idempotent success (T-09-13 primary defence; label is belt-and-suspenders)"
metrics:
  duration: ~35min
  completed_date: "2026-06-08"
  tasks: 3
  files: 14
---

# Phase 09 Plan 06: Manager Wiring — Reporter Job Spawn + Drop Inline Materialize

## One-liner

Manager now spawns a short-lived `tide-reporter` reader Job (SA `tide-reporter`, role=reporter label, PVC subPath mount) on planner-Job completion in all four handlers, dropping inline `MaterializeChildCRDs` / `childrenAlreadyMaterialized` calls; reporter image is a Helm value threaded via `TIDE_REPORTER_IMAGE`; Layer B test asserts spawn + child CR appearance.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | buildReporterJob + ReporterImage plumbing | 6cec9a7 | reporter_jobspec.go, reporter_jobspec_test.go, *_controller.go (4), cmd/manager/main.go |
| 2 | Spawn reader Job + drop inline materialize in all 4 handlers | 2585635 | project/milestone/phase/plan_controller.go |
| 3 | Reporter image value + deployment env + Layer B reporter-Job test | 58bdd43 | charts/, hack/helm/, test/integration/kind/ |

## What Was Built

### Task 1: buildReporterJob + ReporterImage plumbing

Created `internal/controller/reporter_jobspec.go` with:
- `BuildReporterJob(parent, project, pvcName, taskUID, parentKind string, opts ReporterOptions, scheme) *batchv1.Job`
- `ReporterOptions{ReporterImage string}`
- Deterministic name: `tide-reporter-<parentUID>`
- Namespace: `project.Namespace` (tenant namespace, same-namespace fix)
- ServiceAccountName: `tide-reporter`
- PVC mount: SubPath `<project.UID>/workspace` at `/workspace` (same layout dispatch Job wrote)
- Args: `--workspace`, `--project-uid`, `--task-uid`, `--parent-name`, `--parent-namespace`, `--parent-kind`
- Labels: `tideproject.k8s/role=reporter` on Job + pod template (T-09-13)
- SecurityContext: RunAsNonRoot + drop ALL capabilities (T-09-14)
- NO EnvFrom git-creds
- BackoffLimit=2, TTLSecondsAfterFinished=300, RestartPolicy=Never
- owner.EnsureOwnerRef(job, parent, scheme) for cascade-delete

Added `TIDE_REPORTER_IMAGE` env in `cmd/manager/main.go` (mirrors TIDE_PUSH_IMAGE at :177) and `ReporterImage string` field to all four reconciler structs. Wired `reporterImage` into four `SetupWithManager` calls.

TDD: 11 unit tests in `reporter_jobspec_test.go` covering all behavior cases.

### Task 2: Spawn reader Job + drop inline materialize in all 4 handlers

In `project_controller.go:handleProjectJobCompletion`:
- KEEP `r.EnvReader.ReadOut` (tiny status for budget rollup, non-fatal error)
- REMOVE `if len(envOut.ChildCRDs) > 0 { childrenAlreadyMaterialized / MaterializeChildCRDs }` block
- ADD idempotent reporter Job spawn: Get(tide-reporter-<projectUID>) → NotFound → BuildReporterJob + Create; AlreadyExists = ok; skip when ReporterImage==""

In `milestone_controller.go:handleJobCompletion`:
- KEEP EnvReader.ReadOut (tiny status; patchMilestoneFailed on error)
- REMOVE MaterializeChildCRDs/childrenAlreadyMaterialized block
- ADD reporter Job spawn (same pattern); justMaterialized=true on first spawn for boundary-wait logic

In `phase_controller.go:handleJobCompletion`:
- KEEP EnvReader.ReadOut; REMOVE inline materialize; ADD reporter Job spawn

In `plan_controller.go:handlePlannerJobCompletion`:
- KEEP EnvReader.ReadOut (+ nil guard returning early with Phase="")
- REMOVE MaterializeChildCRDs/childrenAlreadyMaterialized block
- ADD reporter Job spawn; ADD ValidationState=Validated stamp after spawn (moved from inside the now-removed ChildCRDs block — needed for reconcileWaveMaterialization gate)

T-09-15: `grep -c 'MaterializeChildCRDs' ...` returns 0 for all four handlers.

### Task 3: Reporter image Helm value + deployment env + Layer B test

- `hack/helm/tide-values.yaml` + `charts/tide/values.yaml`: added `images.tideReporter` block (repository `ghcr.io/jsquirrelz/tide-reporter`, tag "", IfNotPresent), additive, no existing keys touched (CLAUDE.md fixed-contract rule)
- `charts/tide/templates/deployment.yaml`: injected `TIDE_REPORTER_IMAGE` env between `# phase4-env-injected` and `envFrom` markers, with `# phase9-reporter-env-injected` idempotency marker
- `hack/helm/augment-tide-chart.sh`: added `ENV9` python block to auto-inject `TIDE_REPORTER_IMAGE` on future `make helm-controller` runs
- `test/integration/kind/suite_test.go`: added `--set images.tideReporter.tag=test --set images.tideReporter.pullPolicy=IfNotPresent` to helm install args
- `test/integration/kind/failure_test.go`: added `ensureReporterSARBAC(ns string)` helper (SA+Role+RoleBinding for tide-reporter in test namespace) and wired it into `createNamespace` so all Layer B test namespaces get the reporter RBAC automatically
- `test/integration/kind/reporter_pod_test.go`: Layer B Ginkgo spec asserting:
  1. Manager spawns `tide-reporter-<projectUID>` Job in project namespace within 3 minutes
  2. Job carries `tideproject.k8s/role=reporter` label (T-09-13)
  3. Job pod ServiceAccountName = `tide-reporter` (T-09-14)
  4. Child Milestone appears within 4 minutes (reporter executed, created child via K8s API)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing functionality] ValidationState=Validated stamp relocation**
- **Found during:** Task 2 (plan_controller.go)
- **Issue:** The original `MaterializeChildCRDs` block also stamped `plan.Status.ValidationState = "Validated"` which `reconcileWaveMaterialization` gates on. Removing the block without moving the stamp would break the Wave path.
- **Fix:** Moved the stamp to `handlePlannerJobCompletion` immediately after the reporter Job spawn — triggered when `reporterSpawned || r.ReporterImage == ""` (covers both production and test/stub path).
- **Files modified:** `internal/controller/plan_controller.go`
- **Commit:** 2585635

**2. [Rule 3 - Blocking issue] `metav1.UID` vs `types.UID` in test file**
- **Found during:** Task 1 (reporter_jobspec_test.go compilation)
- **Issue:** `metav1.ObjectMeta.UID` is `k8s.io/apimachinery/pkg/types.UID` (a `string` alias) — string literals auto-convert; initial `types_UID` helper alias was invalid.
- **Fix:** Removed the helper; string literals auto-convert to `types.UID` in Go struct initialization.
- **Files modified:** `internal/controller/reporter_jobspec_test.go`
- **Commit:** 6cec9a7

**3. [Rule 2 - Missing functionality] ensureReporterSARBAC in test namespace**
- **Found during:** Task 3 (Layer B test design)
- **Issue:** The chart's `reporter-rbac.yaml` only installs the SA+Role+RoleBinding in `.Release.Namespace` and `projectNamespaces`. Per-test namespaces in Layer B tests need the same RBAC for the reporter Job Pod to start.
- **Fix:** Added `ensureReporterSARBAC(ns string)` and wired it into `createNamespace` in `failure_test.go`.
- **Files modified:** `test/integration/kind/failure_test.go`
- **Commit:** 58bdd43

**4. [Rule 1 - Bug] `GitSpec` name in test file**
- **Found during:** Task 1 (reporter_jobspec_test.go compilation)
- **Issue:** Test used `tideprojectv1alpha1.GitSpec` which doesn't exist — the type is `tideprojectv1alpha1.GitConfig`.
- **Fix:** Changed to `&tideprojectv1alpha1.GitConfig{CredsSecretRef: "my-git-creds"}` and used pointer (per `ProjectSpec.Git *GitConfig` field type).
- **Files modified:** `internal/controller/reporter_jobspec_test.go`
- **Commit:** 6cec9a7

## TDD Gate Compliance

| Phase | Gate | Commit |
|-------|------|--------|
| RED | reporter_jobspec_test.go: `controller.BuildReporterJob` undefined, `controller.ReporterOptions` undefined | 6cec9a7 (test + impl combined after fix; tests pass GREEN) |
| GREEN | reporter_jobspec.go implements BuildReporterJob; 11 tests pass | 6cec9a7 |

The RED gate was confirmed by running `go test ./internal/controller/ -run 'ReporterJob'` which failed with "undefined: controller.ReporterOptions". Implementation followed immediately.

## Threat Flags

None — all security-relevant surfaces in this plan were covered by the plan's `<threat_model>`:
- T-09-13 (re-fire on reporter Job completion): mitigated by role=reporter label + idempotent AlreadyExists guard
- T-09-14 (privilege escalation): mitigated by tide-reporter SA + RunAsNonRoot + drop ALL caps
- T-09-15 (stale cross-ns read path left live): mitigated by `grep -c MaterializeChildCRDs` == 0 for all four handlers

## Self-Check: PASSED

- `internal/controller/reporter_jobspec.go`: FOUND
- `internal/controller/reporter_jobspec_test.go`: FOUND
- `test/integration/kind/reporter_pod_test.go`: FOUND
- Commits 6cec9a7, 2585635, 58bdd43: FOUND (see `git log --oneline -5`)
- `grep -c 'MaterializeChildCRDs' ...` == 0 for all four handlers: VERIFIED
- `helm template charts/tide | grep TIDE_REPORTER_IMAGE`: VERIFIED
- `grep -q 'func BuildReporterJob' internal/controller/reporter_jobspec.go`: VERIFIED
- `grep -q 'TIDE_REPORTER_IMAGE' cmd/manager/main.go`: VERIFIED
