---
phase: 27-budget-bypass-resume-correctness
plan: "01"
subsystem: api/v1alpha2
tags: [schema, crd, status-fields, bypass-correctness, envtest]
dependency_graph:
  requires: []
  provides: [CloneComplete-field, PlannerRolledUpUID-field, BypassBaselineCents-field, CRD-schema-foundation]
  affects: [config/crd/bases/tideproject.k8s_projects.yaml, api/v1alpha2/zz_generated.deepcopy.go]
tech_stack:
  added: []
  patterns: ["+optional omitempty three-line field shape", "make manifests + make generate codegen pipeline"]
key_files:
  created: []
  modified:
    - api/v1alpha2/project_types.go
    - config/crd/bases/tideproject.k8s_projects.yaml
decisions:
  - "All three fields are additive +optional omitempty scalars — no CRD storage-version bump required (D-06)"
  - "BypassBaselineCents placed on BudgetStatus alongside PlannerRolledUpUID; acknowledged-spend comparison logic stays in handleBudgetGate (D-04)"
  - "zz_generated.deepcopy.go required no diff — scalar fields covered by *out = *in struct copy already emitted by controller-gen for GitStatus and BudgetStatus"
metrics:
  duration: "~12 minutes"
  completed_date: "2026-06-18"
  tasks_completed: 2
  files_modified: 2
---

# Phase 27 Plan 01: CRD Schema Foundation Summary

Three additive `+optional omitempty` status fields added to `api/v1alpha2/project_types.go`, CRD YAML regenerated via `make manifests`, and QQH-01 ordering-regression envtest confirmed GREEN (5/5 specs, 9.4s) on the new schema.

## Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add three durable status fields and regenerate | 149c684 | api/v1alpha2/project_types.go, config/crd/bases/tideproject.k8s_projects.yaml |
| 2 | Confirm QQH-01 ordering-regression envtest GREEN | (read-only) | internal/controller/project_planner_completion_test.go |

## What Was Built

Three additive CRD status fields that the Wave 2+ controller-logic fixes (BYPASS-02/03/04) depend on:

- `GitStatus.CloneComplete bool` (`cloneComplete,omitempty`) — durable flag gating clone Job re-dispatch on resume; replaces TTL-unreliable Job-existence check (BYPASS-02)
- `BudgetStatus.PlannerRolledUpUID string` (`plannerRolledUpUID,omitempty`) — rollup-once idempotency marker preventing double-counting across halt→resume cycles (BYPASS-03)
- `BudgetStatus.BypassBaselineCents int64` (`bypassBaselineCents,omitempty`) — acknowledged-spend baseline; re-halt fires only on new spend since the bypass (BYPASS-04)

All three follow the existing three-line `// doc comment\n// +optional\nField Type \`json:"name,omitempty"\`` shape used throughout `GitStatus` and `BudgetStatus`. No schema version bump — additive optional fields only (D-06).

## QQH-01 Baseline (BYPASS-05 Gate)

Focused run against `project_planner_completion_test.go`:

```
Ran 5 of 139 Specs in 9.433 seconds
SUCCESS! -- 5 Passed | 0 Failed | 0 Pending | 134 Skipped
```

The ordering-regression test (committed in `2a5e0dc`) is confirmed GREEN on the new schema. This establishes the BYPASS-05 baseline before the TTL-GC companion scenario lands in Plan 03.

## Verification

```
grep -c 'cloneComplete\|plannerRolledUpUID\|bypassBaselineCents' config/crd/bases/tideproject.k8s_projects.yaml
# → 3

go build ./api/... && go vet ./api/...
# → clean exit 0
```

## Deviations from Plan

None — plan executed exactly as written.

`zz_generated.deepcopy.go` had no diff because all three new fields are scalars (bool, string, int64) and `controller-gen` already emits `*out = *in` for `GitStatus.DeepCopyInto` and `BudgetStatus.DeepCopyInto`, which covers scalar fields completely. This is expected behavior, not a missing regeneration.

## Known Stubs

None.

## Threat Flags

None. The three new fields are controller-written status fields only. No new network endpoints, auth paths, file access patterns, or trust-boundary schema changes were introduced. T-27-02 (CRD schema regen tampering) is mitigated: `make manifests && make generate` are deterministic codegen and CI re-runs both.

## Self-Check: PASSED

- [x] `api/v1alpha2/project_types.go` modified: `149c684` — FOUND
- [x] `config/crd/bases/tideproject.k8s_projects.yaml` contains `cloneComplete`, `plannerRolledUpUID`, `bypassBaselineCents` — VERIFIED
- [x] `go build ./api/... && go vet ./api/...` — clean
- [x] QQH-01 envtest: `Ran 5 of 139 Specs … SUCCESS! -- 5 Passed | 0 Failed` — VERIFIED
