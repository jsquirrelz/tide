---
phase: 34-run-integrity-integration-miss-gate-lastpushedsha
plan: "02"
subsystem: api, internal/controller, internal/metrics
tags: [integ-02, integ-03, integ-04, api-parity, metrics]
requirements: [INTEG-02, INTEG-03, INTEG-04]

dependency_graph:
  requires: []
  provides:
    - "ConditionIntegrationIncomplete / ReasonIntegrationIncomplete / ReasonMergeConflict (both API versions)"
    - "WaveIntegrationStatus struct + Plan.Status.WaveIntegration (both API versions, CRD regenerated)"
    - "internal/controller/git_writer.go: succeededTaskBranches, gitWriterInFlightCount, readJobPushEnvelope"
    - "git-writer Job labels on buildPushJob output"
    - "tide_integration_outcomes_total{project,outcome} metric"
  affects:
    - api/v1alpha1/shared_types.go, api/v1alpha1/plan_types.go, api/v1alpha1/zz_generated.deepcopy.go
    - api/v1alpha2/shared_types.go, api/v1alpha2/plan_types.go, api/v1alpha2/zz_generated.deepcopy.go
    - config/crd/bases/tideproject.k8s_plans.yaml
    - internal/controller/git_writer.go (new), internal/controller/git_writer_test.go (new)
    - internal/controller/push_helpers.go, internal/controller/push_helpers_test.go
    - internal/controller/project_controller.go (pushResultEnvelope fields + readPushEnvelope delegation)
    - internal/metrics/registry.go, internal/metrics/registry_test.go

tech_stack:
  added: []
  patterns:
    - "plannerInFlightCount (dispatch_helpers.go) List-gate shape, adapted for gitWriterInFlightCount"
    - "BoundaryPushStatus shape mirrored for WaveIntegrationStatus"

key_files:
  created:
    - internal/controller/git_writer.go
    - internal/controller/git_writer_test.go
  modified:
    - api/v1alpha1/shared_types.go
    - api/v1alpha2/shared_types.go
    - api/v1alpha1/plan_types.go
    - api/v1alpha2/plan_types.go
    - internal/controller/push_helpers.go
    - internal/controller/push_helpers_test.go
    - internal/controller/project_controller.go
    - internal/metrics/registry.go
    - internal/metrics/registry_test.go

decisions:
  - "v1alpha1 is an unserved (+kubebuilder:unservedversion), storage-compat-only schema kept for the reinstall-required guard; mirrored the new vocabulary/status field there anyway for parity with the existing IntegratedThroughWave/BoundaryPushStatus precedent, per CONTEXT's dual-version parity instruction."
  - "readPushEnvelope on ProjectReconciler now delegates to the new package-level readJobPushEnvelope (git_writer.go) so PlanReconciler can reuse the identical termination-log-only read path for wave-integration Job classification, without an import cycle."

metrics:
  duration: "~1h"
  completed: "2026-07-04"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 11
---

# Phase 34 Plan 02: Shared Contracts — API Vocabulary, git-writer Helpers, Metric — Summary

**One-liner:** Landed every contract 34-04/34-05 consume: Project condition/reason vocabulary (`ConditionIntegrationIncomplete`, `ReasonIntegrationIncomplete`, `ReasonMergeConflict`) in both API versions, `Plan.Status.WaveIntegrationStatus` (both versions, CRD regenerated), the three shared helpers in a new `internal/controller/git_writer.go` (`succeededTaskBranches`, `gitWriterInFlightCount`, `readJobPushEnvelope`), git-writer Job labels on `buildPushJob`, extended `pushResultEnvelope` fields, and the `tide_integration_outcomes_total` metric.

## Tasks Completed

| Task | Name | Files |
|------|------|-------|
| 1 | API vocabulary + WaveIntegrationStatus in both versions + CRD regen | api/v1alpha{1,2}/{shared,plan}_types.go, zz_generated.deepcopy.go, config/crd/bases/tideproject.k8s_plans.yaml |
| 2 | git-writer labels + shared helpers | internal/controller/git_writer.go (new), git_writer_test.go (new), push_helpers.go, push_helpers_test.go, project_controller.go |
| 3 | IntegrationOutcomesTotal metric | internal/metrics/registry.go, registry_test.go |

## Verification Results (all commands actually run this session)

- `go build ./api/...` — PASS
- `go test ./api/... -count=1` — PASS (both v1alpha1, v1alpha2)
- `grep -c 'ConditionIntegrationIncomplete = "IntegrationIncomplete"' api/v1alpha2/shared_types.go` → 1; same in v1alpha1 → 1
- `grep -c 'ReasonMergeConflict = "MergeConflict"' api/v1alpha2/shared_types.go` → 1; v1alpha1 → 1
- `grep -c 'WaveIntegrationStatus struct' api/v1alpha{1,2}/plan_types.go` → 1 each
- `grep -c 'waveIntegration' config/crd/bases/tideproject.k8s_plans.yaml` → 2 (both served versions)
- `go test ./internal/controller/... -run 'GitWriter|BuildPushJob' -count=1` (envtest, `KUBEBUILDER_ASSETS` set) — PASS, 6/6 new specs green
- `go test ./internal/metrics/... -count=1` — PASS

## Deviations from Plan Text

- None material. Envelope reader promoted to package-level exactly as specified; `readPushEnvelope` reduced to a one-line delegation.
