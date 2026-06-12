---
phase: 15-paper-cuts
plan: "01"
subsystem: owner-labels
tags: [cuts-01, label-stamping, backfill, approve-discovery]
dependency_graph:
  requires: []
  provides: [owner.LabelProject, owner.StampProjectLabel, reporter-label-stamping, reconciler-label-backfill, cuts01-regression-tests]
  affects: [internal/reporter/materialize.go, internal/controller/milestone_controller.go, internal/controller/phase_controller.go, cmd/tide/approve_test.go]
tech_stack:
  added: []
  patterns: [label-presence-guard, MergeFrom-patch, owner-chain-walk, fail-open-no-op]
key_files:
  created:
    - internal/owner/label.go
    - internal/owner/label_test.go
  modified:
    - internal/reporter/materialize.go
    - internal/reporter/materialize_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/milestone_controller_test.go
    - internal/controller/phase_controller.go
    - internal/controller/phase_controller_test.go
    - cmd/tide/approve_test.go
    - internal/gates/boundary.go
decisions:
  - "D-01: StampProjectLabel helper in owner package — shared seam mirrors EnsureOwnerRef shape, no-op on empty string (fail-open)"
  - "D-02: approve discovery unchanged — label-filter-only contract preserved; TestApproveUnlabeledMilestoneNotDiscovered pins it"
  - "D-03: backfill inserted after step 4 (owner-ref) and before step 5 (dispatch) so parked AwaitingApproval CRs also self-heal"
  - "D-04: project label only — no role/level label parity in this plan"
  - "Milestone resolveProjectName uses spec.projectRef directly (1 Get); Phase uses spec.milestoneRef → Milestone.spec.projectRef (2 Gets)"
metrics:
  duration: "~35 minutes"
  completed_date: "2026-06-12"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 10
---

# Phase 15 Plan 01: Label Stamping + Reporter Create-Site + Backfill Summary

Universal `tideproject.k8s/project` label stamping at creation via a shared `owner.StampProjectLabel` helper, reporter create-site wiring, and idempotent reconciler backfill for pre-Phase-15 CRs — closes CUTS-01 run-1 finding 6 ("no level awaiting approval" despite parked CR).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| RED | StampProjectLabel failing tests | f325bff | internal/owner/label_test.go |
| 1 | StampProjectLabel helper + reporter stamping (D-01) | 911e0b3 | internal/owner/label.go, internal/reporter/materialize.go, internal/reporter/materialize_test.go |
| 2 | Reconciler backfill for unlabeled CRs (D-03) | f4f36a3 | internal/controller/milestone_controller.go, internal/controller/phase_controller.go, +test files |
| 3 | approve-discovery regression + boundary.go comment | cfb91c7 | cmd/tide/approve_test.go, internal/gates/boundary.go |

## What Was Built

**D-01 — StampProjectLabel helper (`internal/owner/label.go`)**

Exported `LabelProject = "tideproject.k8s/project"` constant and `StampProjectLabel(obj metav1.Object, projectName string)` function. Semantics: no-op on empty string (fail-open); lazily inits nil labels map; overwrites pre-existing LLM-authored project labels with the authoritative parent value (T-15-01 mitigation, mirrors `stampParentRef` doctrine).

**D-01 — Reporter create-site stamping (`internal/reporter/materialize.go`)**

One-line insertion in `MaterializeChildCRDs` between `stampParentRef` and `owner.EnsureOwnerRef`: `owner.StampProjectLabel(obj, parent.GetLabels()[owner.LabelProject])`. Parent label missing = no-op (RESEARCH Pitfall 1 — don't prevent creation).

**D-03 — Reconciler backfill (both controllers)**

In `MilestoneReconciler.Reconcile` and `PhaseReconciler.Reconcile`, step 4b (after owner-ref ensure, before planner dispatch): when `labels[owner.LabelProject]` is empty, resolve the project name via the OwnerRef chain (Milestone: 1 Get via spec.projectRef; Phase: 2 Gets via spec.milestoneRef → Milestone.spec.projectRef) and apply a metadata-only `client.MergeFrom` patch. Guard: only patch when label is missing (presence guard makes second reconcile a no-op — T-15-03). Orphans skip silently.

**D-02 locked — approve_test.go CUTS-01 regression**

Two new tests: `TestApproveUnlabeledMilestoneNotDiscovered` (pins the symptom — unlabeled CR is NOT found) and `TestApproveLabeledMilestoneDiscoveredFirstCall` (pins the fix — labeled CR IS found on first call). `approve.go` is not modified; D-02 discovery is label-filter-only by design.

**boundary.go comment update**

Updated the stale "only Plan→Task stamps the label" comment in `BoundaryDetected` to reflect Phase 15 reality: every create site now stamps via `StampProjectLabel`; OwnerRef filter is retained as defensive fallback for pre-Phase-15 CRs pending backfill. Comment-only change.

## Deviations from Plan

None — plan executed exactly as written.

## TDD Gate Compliance

- RED gate: `test(15-01)` commit f325bff — failing `label_test.go` + `materialize_test.go` finding-6 regression
- GREEN gate: `feat(15-01)` commit 911e0b3 — all five behaviors green
- GREEN gate: `feat(15-01)` commit f4f36a3 — envtest backfill specs green (130/130 passed)
- GREEN gate: `feat(15-01)` commit cfb91c7 — approve tests green

## Test Results

```
ok  github.com/jsquirrelz/tide/internal/owner     0.504s
ok  github.com/jsquirrelz/tide/internal/reporter  0.936s
ok  github.com/jsquirrelz/tide/cmd/tide            1.142s
ok  github.com/jsquirrelz/tide/internal/controller 54.006s  (130/130 specs)
```

## Known Stubs

None. All data flows are wired.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries. T-15-01 (label injection) and T-15-02 (backfill patch EoP) are mitigated as designed.

## Self-Check: PASSED

Files verified:

- `internal/owner/label.go` — exists, exports `LabelProject` and `StampProjectLabel`
- `internal/reporter/materialize.go` — contains `owner.StampProjectLabel` before `owner.EnsureOwnerRef`
- `internal/controller/milestone_controller.go` — contains backfill guarded by `owner.LabelProject` check
- `internal/controller/phase_controller.go` — contains backfill with Phase→Milestone→Project chain
- `cmd/tide/approve_test.go` — contains both CUTS-01 regression test functions
- `internal/gates/boundary.go` — comment updated, no statement changes

Commits verified:

- f325bff, 911e0b3, f4f36a3, cfb91c7 — all present in `git log --oneline`
