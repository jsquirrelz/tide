---
phase: 23-schema-migration-cross-scope-dependency-model
plan: "04"
subsystem: api-migration
tags: [v1alpha2, schema-migration, breaking-change, webhooks, controllers]
requires:
  - "23-01: v1alpha2 served+storage, v1alpha1 unserved"
  - "23-02: webhooks moved to v1alpha2"
  - "23-03: Wave controller on v1alpha2 + Project SchemaRevision guard"
provides:
  - "operator compiles, vets, and runs entirely on v1alpha2 (SCHEMA-03)"
  - "internal/webhook/v1alpha2 FileTouch helpers (exported, v1alpha2-typed)"
  - "envtest suite registers v1alpha2 + uses webhookv1alpha2.Setup*"
affects:
  - "all consumers of api/v1alpha1 (controllers, gates, dispatch, budget, reporter, cmd, dashboard, tests)"
tech-stack:
  added: []
  patterns:
    - "import-path repoint keeping alias tokens (lowest-churn version migration)"
    - "Wave→Plan association derived from Wave.Status.TaskRefs membership (Phase-24 owns global derivation)"
key-files:
  created:
    - internal/webhook/v1alpha2/file_touch_utils.go
    - internal/webhook/v1alpha2/strict_mode.go
    - internal/webhook/v1alpha2/strict_mode_test.go
  modified:
    - internal/controller/{milestone,phase,plan,project,task}_controller.go
    - internal/controller/plan_controller.go (webhookv1alpha2 import)
    - cmd/dashboard/api/{plans,tasks,informer_bridge}.go
    - test/integration/envtest/suite_test.go
    - "~50 test files (SchemaRevision + WaveSpec ProjectRef)"
  deleted:
    - internal/webhook/v1alpha1/ (file_touch_utils.go, strict_mode.go, strict_mode_test.go)
decisions:
  - "Repoint import PATH only, keep alias tokens — ~2,700 call sites compile unchanged"
  - "Rename alias token to v1alpha2 only in the 5 reconcilers (textual acceptance grep) + suite_test"
  - "Move webhook helpers to v1alpha2 (option a) — clean 1:1 relocation, deleted redundant unexported plan_webhook.go duplicates"
  - "Wave roll-up tests get unique WaveIndex (90..95) — reflects real global-wave model, isolates namespace-global label match"
metrics:
  duration: "~32 min"
  completed: "2026-06-16"
  task_commits: 3
  files_changed: 134
---

# Phase 23 Plan 04: v1alpha2 Consumer Migration Summary

Completed the breaking v1alpha2 migration: every consumer (controllers, gates, dispatch, budget, reporter, CLI, dashboard, ~80 test files) now imports `api/v1alpha2`; the operator compiles, vets, and runs on the served version, satisfying SCHEMA-03.

## What Was Built

**Task 1 — bulk path repoint (commit 8ec1dbe):** Repointed the import PATH `github.com/jsquirrelz/tide/api/v1alpha1` → `.../api/v1alpha2` across 130 consumer files (import lines only), keeping the alias tokens (`tideprojectv1alpha1`, `tidev1alpha1`) so the ~2,700 call sites compiled unchanged. Excluded the `api/v1alpha1/*` type-definition tests and `project_controller_v2_guard_test.go`.

**Task 2 — semantic deltas + GVK flip (commit bff8df9):**
- **DELTA 3 (webhook helpers):** Moved `file_touch_utils.go` + `strict_mode.go` (+ test) into `internal/webhook/v1alpha2/`, retyped to v1alpha2 with exported `FileTouchMismatchPair` / `ComputeFileTouchMismatches` / `SummariseMismatches` / `ResolveFileTouchMode`. Deleted the redundant unexported equivalents from `plan_webhook.go` (kept one definition each), repointed `plan_controller`'s import to `webhookv1alpha2`, and deleted the now-empty `internal/webhook/v1alpha1/` package.
- **DELTA 1 (Wave ProjectRef):** Dashboard reads (`plans.go`, `tasks.go`, `informer_bridge.go`) now derive the Plan association from `Wave.Status.TaskRefs` membership / resolve the project via `Spec.ProjectRef` in one hop. Test WaveSpec builders construct `ProjectRef + WaveIndex`.
- **Controller GVKs:** Renamed the `tideprojectv1alpha1` alias token to a v1alpha2 token in the 5 reconcilers so `For()/Owns()` register v1alpha2 GVKs (textual acceptance grep: zero `For(&tideprojectv1alpha1.` / `Owns(&tideprojectv1alpha1.`). Collapsed the duplicate v1alpha2 imports left in plan/project controllers.

**Task 3 — envtest suite + SchemaRevision (commit 3071f70):** Migrated `suite_test.go` to `webhookv1alpha2.Setup*` + v1alpha2 scheme registration. Added `Spec.SchemaRevision: "v1alpha2"` to every success-path test Project so the 23-03 fail-closed guard admits them (the guard test keeps its deliberately-empty fixture). Gave the WaveReconciler roll-up specs unique WaveIndices (90..95) to isolate the namespace-global wave-index label match.

## Verification (observed, not assumed)

| Gate | Result |
|------|--------|
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `make verify-no-aggregates` | exit 0 (OK: no aggregate schedule fields) |
| `make verify-dag-imports` | exit 0 (OK: pkg/dag imports clean) |
| `For(&tideprojectv1alpha1.` / `Owns(&...)` in controllers | 0 |
| `WaveSpec{...PlanRef...}` literals | 0 |
| `internal/webhook/v1alpha1/` dir | DELETED |
| suite_test registers v1alpha2 AddToScheme + webhookv1alpha2.Setup | YES |
| envtest package compiles (`go test -c`) | exit 0 |
| `git diff go.mod go.sum` | empty (no dependency drift) |
| Unit tests: controller (140/140), dashboard, cmd, budget, dispatch, webhook, api | all pass |

Controller package unit tier run with `KUBEBUILDER_ASSETS` (envtest 1.33.0): **140 Passed, 0 Failed, 1 Skipped** after SchemaRevision fixtures + wave-index isolation.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] WaveReconciler roll-up test isolation under namespace-global wave-index matching**
- **Found during:** Task 3 (running the controller unit tier)
- **Issue:** 23-03 rewrote the WaveReconciler to select member Tasks by the `tideproject.k8s/wave-index` label across the whole namespace (the documented Phase-24 TODO). The `wave_controller_test.go` specs all created Waves at index 0, so a single wave's roll-up matched leaked index-0 Tasks from other specs (`gate-plan4-task-1/2` bleeding into `wave-task-ref-a/b`). This never surfaced before because the package did not compile until this plan.
- **Fix:** Assigned each WaveReconciler roll-up spec a unique WaveIndex (90..95). This reflects the real global-wave model (indices are unique per Project), so the namespace-global label match isolates each test's Tasks. No assertion depended on a specific index value.
- **Files modified:** internal/controller/wave_controller_test.go
- **Commit:** 3071f70

**2. [Rule 3 - Blocking] `cmd/tide-demo-init` //go:embed fixture pre-existing build break**
- **Found during:** Task 1 (first `go build ./...`)
- **Issue:** `cmd/tide-demo-init/main.go`'s `//go:embed all:fixture` failed because the gitignored `fixture/` directory is materialized at build time (unmodified by this plan — pre-existing).
- **Fix:** Ran `make demo-fixture` (`go generate ./cmd/tide-demo-init/...`) to materialize the gitignored fixture, as the Makefile's `vet`/`test` targets do. Not committed (gitignored).
- **Files modified:** none tracked (generated fixture is gitignored)

### Out-of-scope, reverted
- A broad `gofmt -w` touched `internal/metrics/wave_label_test.go` (pre-existing gofmt struct-alignment debt, unrelated to the migration). Reverted to keep the commit scoped.

## Known Stubs

None introduced. Existing Phase-24 stubs are carried forward with `// TODO(phase-24)` markers:
- `materializeWaves` still creates per-plan Waves named `tide-wave-<plan.UID>-<i>` with `ProjectRef` (global derivation deferred to Phase 24).
- WaveReconciler `taskToWaveMapper` and member-task selection use a namespace-global wave-index (ProjectRef-scoped index is Phase 24).
- Dashboard test/cleanup paths note where the real ProjectRef will be plumbed once the global assembler creates Waves.

## Threat Surface

All `<threat_model>` mitigations satisfied: T-23-11 (controller GVKs flipped — zero v1alpha1 For/Owns), T-23-12 (FileTouch helpers retyped to v1alpha2, single definition, EXACT-equality intersection preserved), T-23-13 (SchemaRevision on all success-path test Projects; rejection fixture keeps invalid revision). T-23-SC: `git diff go.mod go.sum` empty — no package installs.

No new threat surface introduced.

## Self-Check: PASSED

- Created files exist: file_touch_utils.go, strict_mode.go, strict_mode_test.go (v1alpha2), 23-04-SUMMARY.md
- Task commits exist: 8ec1dbe, bff8df9, 3071f70
- internal/webhook/v1alpha1/ confirmed deleted
