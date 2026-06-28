---
phase: 31-d2-d1-adoption-lifecycle-seam
plan: "01"
subsystem: api
tags: [kubernetes, crd, controller-runtime, kubebuilder, api-types, status-conditions]

# Dependency graph
requires:
  - phase: 28-import
    provides: ConditionImportComplete pattern that ConditionProjectPlannerSuppressed mirrors
  - phase: 27-budget-rollup
    provides: BudgetStatus.PlannerRolledUpUID prior-art pattern for child-level markers
provides:
  - "ConditionProjectPlannerSuppressed + ReasonAdoptionComplete constants in api/v1alpha2/shared_types.go"
  - "MilestoneStatus.MilestoneRolledUpUID scalar marker (D-03 level-specific)"
  - "PhaseStatus.PhaseRolledUpUID scalar marker (D-03 level-specific)"
  - "PlanStatus.PlanRolledUpUID scalar marker (D-03a new, level-specific)"
  - "Regenerated CRD YAML + Helm chart CRD templates with new status properties"
affects:
  - 31-02 (project_controller.go D2 suppression — reads ConditionProjectPlannerSuppressed)
  - 31-03 (milestone/phase/plan controllers D1 rollup — reads *RolledUpUID markers)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Level-specific rollup marker pattern (D-03): MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID on each child's own .status — additive scalars, not a shared/generic field"
    - "Durable dispatch-suppressing .status condition (D-01): ConditionProjectPlannerSuppressed joins the BillingHalt/BudgetBlocked/ImportComplete family"

key-files:
  created: []
  modified:
    - api/v1alpha2/shared_types.go
    - api/v1alpha2/milestone_types.go
    - api/v1alpha2/phase_types.go
    - api/v1alpha2/plan_types.go
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - charts/tide-crds/templates/milestone-crd.yaml
    - charts/tide-crds/templates/phase-crd.yaml
    - charts/tide-crds/templates/plan-crd.yaml

key-decisions:
  - "D-03a: PlanStatus had no prior rollup marker (Phase 27/30 only added project-level BudgetStatus.PlannerRolledUpUID); added PlanRolledUpUID as a new D-03a addition"
  - "zz_generated.deepcopy.go unchanged: scalar string fields copy by assignment — no DeepCopyInto changes needed"
  - "Helm chart CRD templates (charts/tide-crds/) also updated by chart-reproducibility pre-commit hook — included in Task 2 commit"

patterns-established:
  - "Per-level rollup markers use level-specific names (MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID) — D-03 rejected a shared generic name for auditing clarity"
  - "New dispatch-suppressing conditions follow the Phase 28 ImportComplete block style in shared_types.go with full doc comment explaining durable semantics"

requirements-completed: [ADOPT-01, ADOPT-04, ADOPT-05]

# Metrics
duration: 18min
completed: 2026-06-28
---

# Phase 31 Plan 01: API Vocabulary — Suppression Condition + Per-Level Rollup Markers

**Contract-first wave: ConditionProjectPlannerSuppressed (D-01) + MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID scalar markers (D-03) added to api/v1alpha2 with regenerated CRD YAML and Helm chart templates**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-06-28T~20:00Z
- **Completed:** 2026-06-28T~20:18Z
- **Tasks:** 2
- **Files modified:** 10

## Accomplishments

- Added `ConditionProjectPlannerSuppressed = "ProjectPlannerSuppressed"` and `ReasonAdoptionComplete = "AdoptionComplete"` constants to `api/v1alpha2/shared_types.go`, joining the durable dispatch-suppressing condition family (BillingHalt / BudgetBlocked / FailureHalt / ImportComplete)
- Added `MilestoneStatus.MilestoneRolledUpUID`, `PhaseStatus.PhaseRolledUpUID`, and `PlanStatus.PlanRolledUpUID` — three level-specific scalar string markers per D-03 (D-03a confirmed PlanStatus had no prior marker in Phase 27/30)
- `make generate manifests` ran clean and idempotently; CRD YAML files and Helm chart CRD templates updated; `go build ./api/... ./internal/... ./cmd/manager/...` exits 0

## Task Commits

Each task was committed atomically:

1. **Task 1: Add suppression condition + per-child-level rollup markers to API types** - `21a18cf` (feat)
2. **Task 2: Regenerate DeepCopy + CRD manifests and confirm build** - `18c5696` (chore)

## Files Created/Modified

- `api/v1alpha2/shared_types.go` — Phase 31 const block: `ConditionProjectPlannerSuppressed` + `ReasonAdoptionComplete`
- `api/v1alpha2/milestone_types.go` — `MilestoneStatus.MilestoneRolledUpUID string` (json: milestoneRolledUpUID)
- `api/v1alpha2/phase_types.go` — `PhaseStatus.PhaseRolledUpUID string` (json: phaseRolledUpUID)
- `api/v1alpha2/plan_types.go` — `PlanStatus.PlanRolledUpUID string` (json: planRolledUpUID, D-03a new)
- `config/crd/bases/tideproject.k8s_milestones.yaml` — milestoneRolledUpUID status property added
- `config/crd/bases/tideproject.k8s_phases.yaml` — phaseRolledUpUID status property added
- `config/crd/bases/tideproject.k8s_plans.yaml` — planRolledUpUID status property added
- `charts/tide-crds/templates/milestone-crd.yaml` — propagated by chart-reproducibility hook
- `charts/tide-crds/templates/phase-crd.yaml` — propagated by chart-reproducibility hook
- `charts/tide-crds/templates/plan-crd.yaml` — propagated by chart-reproducibility hook

## Decisions Made

- **D-03a confirmed:** Grepped `api/v1alpha2/plan_types.go` for `RolledUp` — no existing marker. `PlanRolledUpUID` is a new D-03a addition, not a reuse.
- **zz_generated.deepcopy.go unchanged:** Scalar `string` fields are copied by value in Go's struct assignment — the existing `DeepCopyInto` methods handle them correctly without generated code changes. Verified by running `make generate` twice and confirming no diff.
- **Helm chart CRD templates included:** The `chart-reproducibility` pre-commit hook runs `make helm` and verifies `charts/` matches a fresh generation. It auto-updated the three Helm chart CRD templates and they were staged in the Task 2 commit.

## Deviations from Plan

None — plan executed exactly as written. The Helm chart CRD template update was triggered automatically by the chart-reproducibility pre-commit hook (expected behavior per CLAUDE.md "binary catches up to chart"), not a deviation.

## Issues Encountered

- `go build ./...` fails on `cmd/tide-demo-init` with `pattern all:fixture: no matching files found` — the `fixture/` subdirectory is untracked in git and absent from the worktree. This is a pre-existing condition unrelated to this plan (the fixture dir exists untracked in the main checkout). Verified by confirming `git ls-files cmd/tide-demo-init/fixture/` returns empty on main. All affected packages (`./api/...`, `./internal/...`, `./cmd/manager/...`) build cleanly.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- Plans 02 and 03 can now implement against the new symbols with no further type changes.
- Plan 02 reads `tidev1alpha2.ConditionProjectPlannerSuppressed` via `meta.FindStatusCondition` and stamps it via `meta.SetStatusCondition + Status().Patch(MergeFrom(base))` (D-07 single patch).
- Plan 03 reads `milestone.Status.MilestoneRolledUpUID`, `phase.Status.PhaseRolledUpUID`, `plan.Status.PlanRolledUpUID` as idempotency gates before `budget.RollUpUsage` invocations.
- No blockers.

## Self-Check: PASSED

- `api/v1alpha2/shared_types.go`: `ConditionProjectPlannerSuppressed` present (grep confirms 2 occurrences: const name + value)
- `api/v1alpha2/milestone_types.go`: `MilestoneRolledUpUID` present
- `api/v1alpha2/phase_types.go`: `PhaseRolledUpUID` present
- `api/v1alpha2/plan_types.go`: `PlanRolledUpUID` present
- CRD YAMLs: `milestoneRolledUpUID`, `phaseRolledUpUID`, `planRolledUpUID` all present
- Task 1 commit `21a18cf` exists in git log
- Task 2 commit `18c5696` exists in git log
- `project_types.go` `PlannerRolledUpUID` count: 2 (unchanged, D-10 preserved)
- `charts/tide/values.yaml` diff: empty (D-11 preserved)

---
*Phase: 31-d2-d1-adoption-lifecycle-seam*
*Completed: 2026-06-28*
