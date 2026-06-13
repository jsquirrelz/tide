---
phase: 17-address-tech-debt-plan-label-backfill-gate-hardening
plan: "01"
subsystem: controller/reporter
tags: [label-backfill, cuts-01, debt-01, idempotency, tdd]
dependency_graph:
  requires: []
  provides: [plan-label-backfill, project-milestone-create-site-stamp]
  affects: [plan_controller, materialize]
tech_stack:
  added: []
  patterns: [idempotent-label-backfill, type-switch-special-case, tdd-red-green]
key_files:
  created: []
  modified:
    - internal/controller/plan_controller.go
    - internal/controller/plan_controller_test.go
    - internal/reporter/materialize.go
    - internal/reporter/materialize_test.go
decisions:
  - "Reuse existing resolveProjectName (returns (string,error)) rather than hand-rolling a new helper; guard adapts to err == nil && name != ''"
  - "Project→Milestone stamp uses *Project type-switch at existing create-site rather than a new code path — fail-open semantics preserved"
metrics:
  duration: "~25 minutes"
  completed: "2026-06-13"
  tasks_completed: 2
  files_modified: 4
---

# Phase 17 Plan 01: Plan-label backfill + Project→Milestone create-site stamp Summary

Plan-level project-label backfill in PlanReconciler (DEBT-01) and Project→Milestone reporter-edge create-site stamp (15-WR-03), both implemented via TDD with envtest and unit specs.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 RED | Failing envtest spec for Plan backfill | bbbd166 | internal/controller/plan_controller_test.go |
| 1 GREEN | Backfill block in PlanReconciler.Reconcile | dedc0c1 | internal/controller/plan_controller.go |
| 2 RED | Failing unit test for create-site stamp | 5e4130e | internal/reporter/materialize_test.go |
| 2 GREEN | *Project type-switch in materialize.go | 9bd79c1 | internal/reporter/materialize.go |

## What Was Built

**Task 1 — Plan-level project-label backfill (DEBT-01):**

Inserted a label-absent backfill block in `PlanReconciler.Reconcile` between step 4 (owner-ref, ends ~:173) and step 5 (Dispatcher seam, :199). The block mirrors `phase_controller.go:168-186` exactly with one required adaptation: `resolveProjectName` returns `(string, error)` rather than a bare string, so the guard is `if name, err := r.resolveProjectName(ctx, &plan); err == nil && name != ""`. Orphan Plans (unresolvable chain → `ErrParentUnresolved`) are skipped silently. The `client.MergeFrom` patch with nil-map init makes the second reconcile a no-op (ResourceVersion unchanged — idempotency contract).

**Task 2 — Project→Milestone reporter-edge create-site stamp (15-WR-03):**

At the existing `owner.StampProjectLabel` call in `MaterializeChildCRDs` (:materialize.go), resolved `projectName` via a `*tideprojectv1alpha1.Project` type-switch before calling `StampProjectLabel`. When the parent IS a `*Project`, uses `parent.GetName()` instead of `parent.GetLabels()[owner.LabelProject]` (which is always `""` — a Project has no self-referencing project label). All other parent types fall through to the existing label resolution unchanged.

## Acceptance Criteria Verification

- `grep -nE 'plan\.Labels\[owner\.LabelProject\] == ""' internal/controller/plan_controller.go` returns line 182, which is before `if r.Dispatcher != nil` at line 199.
- No string literal `"tideproject.k8s/project"` in the new backfill block — uses `owner.LabelProject`.
- `grep -nE 'parent\.\(\*tideprojectv1alpha1\.Project\)' internal/reporter/materialize.go` returns line 261.
- `go test ./internal/controller/... -run TestControllers -count=1 -short` exits 0.
- `go test ./internal/reporter/... -run Materialize -count=1` exits 0.

## Deviations from Plan

None — plan executed exactly as written. The REQUIRED ADAPTATION from 17-PATTERNS.md Item 1 (resolver returns `(string, error)` not `""`) was applied as specified.

## Known Stubs

None.

## Threat Flags

None. Both changes operate on existing trust boundaries:
- Backfill reuses `resolveProjectName`/`resolveProjectForPlan` — the Items[0] mis-routing fallback was already removed (T-17-01 mitigated by existing code).
- Reporter stamp resolves from `parent.GetName()` for `*Project` — in-namespace trusted component (T-17-02 accepted).
- Orphan Plans stay unlabeled on `ErrParentUnresolved` (T-17-03 mitigated).

## Self-Check: PASSED

- internal/controller/plan_controller.go modified: FOUND (contains backfill block at line 182)
- internal/controller/plan_controller_test.go modified: FOUND (contains "project-label backfill" Describe block)
- internal/reporter/materialize.go modified: FOUND (contains *Project type-switch at line 261)
- internal/reporter/materialize_test.go modified: FOUND (contains TestMaterializeChildCRDsProjectParentStampsLabelAtCreateSite)
- Commits bbbd166, dedc0c1, 5e4130e, 9bd79c1: FOUND in git log
