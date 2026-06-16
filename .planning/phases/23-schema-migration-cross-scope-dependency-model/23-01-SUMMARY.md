---
phase: 23-schema-migration-cross-scope-dependency-model
plan: "01"
subsystem: api/schema
tags: [crd, schema, v1alpha2, breaking-change, kahn, dag]
dependency_graph:
  requires: []
  provides: [api/v1alpha2 package, CRD manifests with v1alpha2 storage]
  affects: [internal/controller, internal/webhook, cmd/manager (plans 23-02/23-03)]
tech_stack:
  added: []
  patterns:
    - kubebuilder v4 multi-version CRD (v1alpha2 served+storage, v1alpha1 unserved)
    - CEL XValidation on DependsOn (empty-string rejection at admission)
    - +kubebuilder:unservedversion for clean-break migration (D-09)
key_files:
  created:
    - api/v1alpha2/groupversion_info.go
    - api/v1alpha2/wave_types.go
    - api/v1alpha2/task_types.go
    - api/v1alpha2/plan_types.go
    - api/v1alpha2/phase_types.go
    - api/v1alpha2/milestone_types.go
    - api/v1alpha2/project_types.go
    - api/v1alpha2/shared_types.go
    - api/v1alpha2/zz_generated.deepcopy.go
    - api/v1alpha2/schema_test.go
  modified:
    - api/v1alpha1/plan_types.go (+unservedversion, -storageversion)
    - api/v1alpha1/wave_types.go (+unservedversion)
    - api/v1alpha1/task_types.go (+unservedversion)
    - api/v1alpha1/phase_types.go (+unservedversion)
    - api/v1alpha1/milestone_types.go (+unservedversion)
    - api/v1alpha1/project_types.go (+unservedversion)
    - config/crd/bases/tideproject.k8s_waves.yaml
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_projects.yaml
    - Makefile
decisions:
  - "Added +kubebuilder:unservedversion to all six v1alpha1 Kinds (not just Plan) to make all CRDs served:false for v1alpha1, satisfying the must_have truth that v1alpha1 is marked unserved"
  - "Removed 'sibling' word from all v1alpha2 DependsOn doc comments to satisfy literal zero-count acceptance criterion"
  - "SchemaRevision string discriminator (Required, Enum=v1alpha2) is the D-09 old-object guard field; its absence in v1alpha1 makes it the clean type-level discriminator"
metrics:
  duration: ~25 minutes
  completed: 2026-06-16
  tasks_completed: 2
  tasks_total: 2
  files_created: 10
  files_modified: 14
---

# Phase 23 Plan 01: v1alpha2 Schema Package + CRD Migration Summary

**One-liner:** v1alpha2 API package introducing Project-scoped global Wave ownership (ProjectRef replaces PlanRef), any-level cross-scope DependsOn on every hierarchy level, PlanSpec.DependsOn added, ProjectSpec.SchemaRevision discriminator for D-09 old-object guard, all six Kinds as v1alpha2 storage+served with v1alpha1 unserved — deepcopy and CRD YAML regenerated.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Create api/v1alpha2 package — Spring Tide schema reshape | 67cb313 | api/v1alpha2/{8 types + deepcopy}, api/v1alpha1/plan_types.go |
| 2 | Regenerate deepcopy + CRDs, extend aggregates guard, structural unit tests | 6196ba7 | config/crd/bases/{6 CRDs}, api/v1alpha1/{5 type files}, Makefile, schema_test.go |

## What Was Built

### Task 1: v1alpha2 Package (SCHEMA-01, DEPS-01, DEPS-02, D-07, D-09)

**api/v1alpha2/wave_types.go** — WaveSpec re-owned from Plan to Project scope (SCHEMA-01, D-07):
- `ProjectRef string` replaces `PlanRef string`; `+kubebuilder:validation:MinLength=1`
- `WaveIndex int` re-documented as global monotonic 0-indexed position across the entire Project; PERSIST-03 / verify-no-aggregates citation retained
- `+kubebuilder:storageversion` on root marker block

**api/v1alpha2/task_types.go** — D-F1 plan-local restriction retired (DEPS-01):
- `DependsOn []string` accepts any Task, Plan, Phase, or Milestone name in the Project
- CEL `XValidation` rule: `!self.exists(d, d == '')` — rejects empty-string entries at admission (T-23-01)
- `PlanRef` retained for ownership; any-level target doc comment, no "sibling" language

**api/v1alpha2/plan_types.go** — PlanSpec.DependsOn added (DEPS-02):
- `DependsOn []string` with progressive refinement D-03 documentation
- `+kubebuilder:storageversion` on root marker (moved from v1alpha1)

**api/v1alpha2/phase_types.go / milestone_types.go** — any-level DependsOn:
- Doc comments updated to "any level node (Milestone/Phase/Plan/Task) in this Project"
- `+kubebuilder:storageversion` on root marker blocks

**api/v1alpha2/project_types.go** — SchemaRevision discriminator (D-09):
- `SchemaRevision string` with `+kubebuilder:validation:Required` and `+kubebuilder:validation:Enum=v1alpha2`
- Absent from v1alpha1 ProjectSpec — the Plan-03 reconciler head guard discriminates on this field
- `+kubebuilder:storageversion` on root marker

**api/v1alpha2/shared_types.go** — full v1alpha1 vocabulary mirrored plus two new constants:
- `ReasonRequiresReinstall = "RequiresReinstall"` — Plan-03 old-object fail-closed guard
- `ReasonGlobalCycleDetected = "GlobalCycleDetected"` — Plan-03 global cycle gate

**api/v1alpha1/plan_types.go** — `+kubebuilder:storageversion` removed (Pitfall 1: prevents dual-storage-version controller-gen error)

**api/v1alpha2/zz_generated.deepcopy.go** — produced by `make generate` (controller-gen v0.20.1)

### Task 2: CRD Regeneration + Guard Extension + Unit Tests

**api/v1alpha1/{wave,task,plan,phase,milestone,project}_types.go** — `+kubebuilder:unservedversion` added to all six Kinds (Pattern 1, D-09 clean break). Makes CRDs emit `served: false` for v1alpha1 without a conversion webhook.

**config/crd/bases/ (six CRDs)** — regenerated by `make manifests`:
- v1alpha1: `served: false, storage: false` on all six Kinds
- v1alpha2: `served: true, storage: true` on all six Kinds
- No `conversion.strategy: Webhook` on any CRD (correct for D-09 clean break)

**Makefile verify-no-aggregates** — extended grep from `api/v1alpha1/*_types.go` to `api/v1alpha1/*_types.go api/v1alpha2/*_types.go` (Pitfall 3: guard now covers the new package)

**api/v1alpha2/schema_test.go** — three structural unit tests (stdlib `testing`, no envtest):
- `TestWaveSpec`: constructs Wave with ProjectRef+WaveIndex=3; asserts round-trip and reflect.TypeOf confirms no PlanRef field (SCHEMA-01)
- `TestTaskDependsOn`: constructs Task with cross-scope deps ("milestone-b-phase-3-plan-c-task-07", "milestone-a"); asserts both entries retained, no plan-local filtering (DEPS-01)
- `TestPlanDependsOn`: constructs Plan with DependsOn=["phase-2","plan-x-task-01"]; asserts round-trip; reflect confirms DependsOn field exists (DEPS-02)
- All three pass: `ok github.com/jsquirrelz/tide/api/v1alpha2 0.394s`

## Verification Results

```
go build ./api/v1alpha2/...       → exit 0
go vet ./api/v1alpha2/...         → exit 0
go test ./api/v1alpha2/... -count=1 → PASS (TestWaveSpec, TestTaskDependsOn, TestPlanDependsOn + all others)
make verify-no-aggregates         → OK: no aggregate schedule fields
grep strategy: Webhook crd/bases/ → 0 matches across all 6 CRDs
v1alpha1 served: false            → confirmed on all 6 CRDs
v1alpha2 served:true storage:true → confirmed on all 6 CRDs
storageversion on v1alpha1/plan   → 0 (removed)
storageversion on v1alpha2/plan   → 1 (present)
ProjectRef in v1alpha2/wave       → 2 occurrences
PlanRef in v1alpha2/wave          → 0 (removed)
DependsOn in v1alpha2/plan        → 2 occurrences
SchemaRevision in v1alpha2/project → 4 occurrences
sibling in v1alpha2/phase+milestone+task → 0 (all removed)
```

## Deviations from Plan

### Auto-added Missing Critical Functionality

**1. [Rule 2 - Missing] Added +kubebuilder:unservedversion to all six v1alpha1 Kinds**
- **Found during:** Task 2, after make manifests showed v1alpha1 `served: true` in CRDs
- **Issue:** The plan's `files_modified` listed only `api/v1alpha1/plan_types.go` for v1alpha1 changes. But the `must_haves.truths` requires "v1alpha1 marked unserved" and Task 2's acceptance criterion requires `served: false` in all six CRDs. This requires `+kubebuilder:unservedversion` on all six Kinds, not just Plan.
- **Fix:** Added `+kubebuilder:unservedversion` to the root marker blocks of Wave, Task, Plan, Phase, Milestone, and Project in v1alpha1.
- **Files modified:** api/v1alpha1/wave_types.go, api/v1alpha1/task_types.go, api/v1alpha1/plan_types.go, api/v1alpha1/phase_types.go, api/v1alpha1/milestone_types.go, api/v1alpha1/project_types.go
- **Commit:** 6196ba7

### Non-Material Deviation

**2. grep -A2 'name: v1alpha2' acceptance criterion** — The plan's acceptance criterion `grep -A2 'name: v1alpha2' config/crd/bases/tideproject.k8s_plans.yaml | grep -c 'storage: true'` returns 0 because controller-gen places the `storage: true` line 145 lines after `name: v1alpha2` in the CRD YAML (the schema occupies all the intermediate lines). The actual CRD content is correct: v1alpha2 has `storage: true` (confirmed via direct grep). This is a plan-authoring issue with the `-A2` distance — the requirement is satisfied.

## Known Stubs

None. All fields are concrete schema declarations; no placeholder values. The Phase 24 assembler (not this plan) wires the global wave derivation logic.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: input_validation | api/v1alpha2/task_types.go | CEL XValidation on DependsOn rejects empty strings at admission (T-23-01 mitigated) |

## Self-Check

- api/v1alpha2/groupversion_info.go: FOUND
- api/v1alpha2/wave_types.go: FOUND
- api/v1alpha2/task_types.go: FOUND
- api/v1alpha2/plan_types.go: FOUND
- api/v1alpha2/phase_types.go: FOUND
- api/v1alpha2/milestone_types.go: FOUND
- api/v1alpha2/project_types.go: FOUND
- api/v1alpha2/shared_types.go: FOUND
- api/v1alpha2/zz_generated.deepcopy.go: FOUND
- api/v1alpha2/schema_test.go: FOUND
- commit 67cb313: FOUND
- commit 6196ba7: FOUND

## Self-Check: PASSED
