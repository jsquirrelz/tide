---
phase: 23-schema-migration-cross-scope-dependency-model
plan: "02"
subsystem: api-migration
tags: [crd-migration, webhook, v1alpha2, schema, breaking-change]
dependency_graph:
  requires: [23-01]
  provides: [SCHEMA-03, DEPS-01]
  affects: [cmd/manager, internal/webhook, internal/controller, config/crd/bases, config/webhook]
tech_stack:
  added: []
  patterns:
    - "v1alpha2 webhooks registered via webhookv1alpha2 import alias in main.go"
    - "cross-scope dep filtering in tasksToDAGWithinPlan before ComputeWaves"
    - "TODO(phase-24) stubs mark global derivation boundaries in controllers"
key_files:
  created:
    - internal/webhook/v1alpha2/wave_webhook.go
    - internal/webhook/v1alpha2/plan_webhook.go
    - internal/webhook/v1alpha2/project_webhook.go
    - internal/webhook/v1alpha2/project_webhook_test.go
    - internal/webhook/v1alpha1/file_touch_utils.go
    - docs/migration/v1alpha1-to-v1alpha2.md
  modified:
    - cmd/manager/main.go
    - internal/controller/plan_controller.go
    - internal/controller/wave_controller.go
    - internal/controller/suite_test.go
    - config/webhook/manifests.yaml
  deleted:
    - api/v1alpha1/plan_conversion.go
    - internal/webhook/v1alpha1/plan_webhook.go
    - internal/webhook/v1alpha1/wave_webhook.go
    - internal/webhook/v1alpha1/project_webhook.go
    - internal/webhook/v1alpha1/project_webhook_test.go
decisions:
  - "Moved all three webhooks (plan, wave, project) to v1alpha2 package — not just plan/wave — to prevent T-23-05 routing to unserved v1alpha1.Project"
  - "Kept FileTouchMismatchPair + ComputeFileTouchMismatches + SummariseMismatches in v1alpha1 as file_touch_utils.go (plan_controller still uses v1alpha1.Task types for file-touch validation)"
  - "wave_controller Task listing switched from PlanRef field-indexer to wave-index label (interim until Phase 24 global assembler)"
metrics:
  duration: ~35 minutes
  completed: "2026-06-16T14:31:48Z"
  tasks: 3
  files: 14
---

# Phase 23 Plan 02: Webhook Migration + Controller Stubs Summary

v1alpha2 webhook registration, v1alpha1 retirement, per-plan cycle-webhook cross-scope dep filtering, materializeWaves/wave_controller stubs against v1alpha2.WaveSpec, and SCHEMA-03 migration doc.

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Register v1alpha2 scheme, delete Hub() stub, regenerate CRDs | c5f136e | cmd/manager/main.go, api/v1alpha1/plan_conversion.go (deleted), config/crd/bases/ |
| 2 | Port all webhooks to v1alpha2, stub materializeWaves + wave_controller | 2de10b9 | internal/webhook/v1alpha2/*, internal/controller/{plan,wave}_controller.go |
| 3 | Write v1alpha1→v1alpha2 migration doc (SCHEMA-03) | 12fae52 | docs/migration/v1alpha1-to-v1alpha2.md |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical Functionality] Ported project_webhook to v1alpha2**
- **Found during:** Task 2
- **Issue:** acceptance criterion `grep -rc 'versions=v1alpha1' internal/webhook/ returns 0` requires no v1alpha1 webhook routing. `internal/webhook/v1alpha1/project_webhook.go` had `versions=v1alpha1` routing to unserved v1alpha1.Project — a T-23-05 (Pitfall 2) violation. The plan listed plan+wave webhooks in files_modified but not project_webhook.go; the acceptance criterion and threat model make the required scope clear.
- **Fix:** Created `internal/webhook/v1alpha2/project_webhook.go` and `project_webhook_test.go` (ported tests to v1alpha2 types); deleted `internal/webhook/v1alpha1/project_webhook.go` and test; updated main.go and suite_test.go to use webhookv1alpha2 for all three webhooks.
- **Files modified:** internal/webhook/v1alpha2/project_webhook.go (created), project_webhook_test.go (created), v1alpha1 equivalents (deleted), cmd/manager/main.go, internal/controller/suite_test.go
- **Commit:** 2de10b9

**2. [Rule 1 - Bug] Extracted file-touch utility functions to preserve plan_controller compilation**
- **Found during:** Task 2
- **Issue:** `FileTouchMismatchPair`, `ComputeFileTouchMismatches`, `SummariseMismatches` lived in `internal/webhook/v1alpha1/plan_webhook.go` (now deleted). `plan_controller.go` imports `webhookv1alpha1` for these functions. Moving them into the v1alpha2 package would cause type mismatch (plan_controller uses v1alpha1.Task list).
- **Fix:** Created `internal/webhook/v1alpha1/file_touch_utils.go` extracting these functions into a standalone file in the v1alpha1 package, preserving the import path for plan_controller.go without architectural change.
- **Files modified:** internal/webhook/v1alpha1/file_touch_utils.go (created)
- **Commit:** 2de10b9

## Verification Results

- `go build ./cmd/manager/... ./internal/... ./api/...` exits 0
- `go vet ./internal/webhook/... ./internal/controller/...` exits 0
- `go test ./internal/webhook/... -count=1` exits 0 (both v1alpha1 and v1alpha2 packages)
- `make manifests`: config/webhook/manifests.yaml has v1alpha2-plan, v1alpha2-project, v1alpha2-wave paths only — no v1alpha1 routing
- `make verify-no-aggregates` exits 0
- `make verify-dag-imports` exits 0
- All 6 CRDs: v1alpha1 served:false storage:false, v1alpha2 served:true storage:true
- No `strategy: Webhook` in any CRD (conversion webhook retired)

## Known Stubs

These stubs are intentional, marked with `// TODO(phase-24)`, and block Phase 24 work:

| File | Stub | Reason |
|------|------|--------|
| internal/controller/plan_controller.go:materializeWaves | Creates per-plan v1alpha2.Wave with ProjectRef=projectName, name keyed on plan.UID | Phase 24 global assembler replaces this with project-scoped derivation |
| internal/controller/wave_controller.go:reconcileObservational | Lists Tasks by wave-index label rather than PlanRef field-indexer | Phase 24 global wave index provides the correct association |
| internal/controller/wave_controller.go:taskToWaveMapper | Enqueues all Waves in namespace rather than project-scoped Waves | Phase 24 global index scopes this correctly |
| internal/controller/wave_controller.go:Reconcile step 4 | Owner-ref to parent skipped; materializeWaves stamps it at create time | Phase 24 re-owns Wave under Project |

## Threat Surface Scan

No new network endpoints, auth paths, or schema changes at trust boundaries beyond what was planned in T-23-04/T-23-05/T-23-06. The project_webhook deviation (T-23-05) was already in the threat register; it was closed by this plan.

## Self-Check: PASSED

- c5f136e: confirmed in `git log --oneline HEAD~3..HEAD`
- 2de10b9: confirmed
- 12fae52: confirmed
- docs/migration/v1alpha1-to-v1alpha2.md: 195 lines, exists
- internal/webhook/v1alpha2/wave_webhook.go: exists, contains "sole producer"
- api/v1alpha1/plan_conversion.go: deleted (confirmed)
- internal/webhook/v1alpha1/plan_webhook.go: deleted (confirmed)
