---
phase: 30-resumable-import-partial-tree-resume-adopt-complete-re-plan
plan: "01"
subsystem: import
tags: [import, resumability, partial-tree, envelope-completeness, tdd]
dependency_graph:
  requires: []
  provides: [pkg/dispatch.IsEnvelopeComplete, export-completeness-gating, import-partial-envtest]
  affects: [cmd/tide-import, cmd/tide, internal/controller/import]
tech_stack:
  added: []
  patterns: [table-driven-tests, ginkgo-envtest, shared-completeness-helper]
key_files:
  created: []
  modified:
    - pkg/dispatch/envelope.go
    - pkg/dispatch/envelope_test.go
    - cmd/tide-import/main.go
    - cmd/tide/export_envelopes_run.go
    - cmd/tide/export_envelopes_test.go
    - internal/controller/import_controller_test.go
decisions:
  - "Promoted isEnvelopeComplete from cmd/tide-import/main.go to pkg/dispatch.IsEnvelopeComplete as exported single source of truth"
  - "seedStatusFor helper in export_envelopes_run.go unifies per-node completeness decision across all three CR levels (Milestone/Phase/Plan)"
  - "Updated TestExportEnvelopesSeedManifest to supply complete envelopes for ms+ph — semantically correct since those nodes only preserve status when they have complete envelopes"
metrics:
  duration: "~11 minutes"
  completed_date: "2026-06-26"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 6
---

# Phase 30 Plan 01: Shared Completeness Helper + Export-Time Status Gating Summary

Exported `pkg/dispatch.IsEnvelopeComplete` as a single source of truth and wired export-time empty-Status for incomplete/missing-envelope seed nodes, with envtest pinning the per-node materialization branch.

## What Was Built

### Task 1: Promote IsEnvelopeComplete to pkg/dispatch (RESUME-PARTIAL-01)

Added `func IsEnvelopeComplete(env EnvelopeOut) bool` to `pkg/dispatch/envelope.go` with the exact semantics of the former `cmd/tide-import/main.go:isEnvelopeComplete`:
- ExitCode==0 AND len(ChildCRDs)==ChildCount
- Strict equality: leaf 0==0 passes; populated ChildCRDs with ChildCount==0 is malformed → rejected (WR-02)
- Full doc comment references WR-02 invariant and RESUME-PARTIAL-01

Removed the local `isEnvelopeComplete` from `cmd/tide-import/main.go` and replaced the call site with `pkgdispatch.IsEnvelopeComplete`. Added a 5-case table-driven test in `pkg/dispatch/envelope_test.go` covering all specified behavior cases.

### Task 2: Export sets Status="" for incomplete/missing-envelope seed nodes

Added `seedStatusFor(uid, liveStatus string, envelopes map[string][]byte) string` helper in `export_envelopes_run.go`:
- Present AND complete envelope → returns liveStatus (adopt salvaged status)
- Missing envelope → returns "" (re-plannable)
- Present but corrupt (unmarshal error) → returns "" (fail-closed, T-30-01-01)
- Present but incomplete (IsEnvelopeComplete==false) → returns "" (re-plannable)

Wired all three `entry.Status` assignment sites in `buildSeedManifest` (Milestones ~l375, Phases ~l408, Plans ~l437) through `seedStatusFor` instead of directly copying `cr.Status.Phase`. SHA256 stamping unchanged — still computed for any present envelope bytes.

Added `TestBuildSeedManifest_CompletenessGating` in `export_envelopes_test.go` with 3 Plan nodes (complete/incomplete/missing) asserting the three status outcomes. Updated `TestExportEnvelopesSeedManifest` to supply complete envelopes for Milestone and Phase nodes (corrects the test to match the new semantics: only nodes with complete envelopes get their live status preserved).

### Task 3: envtest pinning per-node complete/incomplete import materialization branch (RESUME-PARTIAL-04)

Added Test 5 (`RESUME-PARTIAL-04`) to `ImportReconciler` Ginkgo describe block in `import_controller_test.go`. The test drives `reconcileCreatingCRs` against a seed with:
- 1 Milestone (status:"Succeeded") + 1 Phase (status:"Succeeded") + 2 Plans
  - `pl-partial-complete` (status:"Running") — complete envelope path
  - `pl-partial-incomplete` (status:"") — incomplete/missing envelope path

Assertions (all passing):
- Both Plan CRs exist post-reconcile (incomplete node materialized, not omitted — identity preserved, Fork 2)
- plComplete: `Status.Phase=="Running"`, `Status.ValidationState=="Validated"` (GAP-12 stamped)
- plIncomplete: `Status.Phase==""`, `Status.ValidationState==""` (wave gate stays closed)
- plIncomplete `Spec.DependsOn` contains `plCompleteName` (adopted-dependent edges preserved)

## Deviations from Plan

None — plan executed exactly as written.

The `TestExportEnvelopesSeedManifest` update is not a deviation but a necessary correction: the test previously asserted `msEntry.Status == "Succeeded"` while providing no envelope for the Milestone UID. Under the new (correct) semantics, a Milestone with no envelope gets Status="" in the seed. Supplying complete envelopes for ms and ph in the test fixture corrects it to test the intended complete-envelope path.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries. The `seedStatusFor` function only reads `envelopes` map bytes (already present in memory from PVC tgz processing) and calls `json.Unmarshal` + `pkgdispatch.IsEnvelopeComplete`. The fail-closed behavior on unmarshal error (treats corrupt bytes as incomplete → Status="") matches the T-30-01-01 threat mitigation in the plan's threat register.

## Self-Check: PASSED

| Check | Result |
|-------|--------|
| pkg/dispatch/envelope.go exists | FOUND |
| cmd/tide-import/main.go exists | FOUND |
| cmd/tide/export_envelopes_run.go exists | FOUND |
| internal/controller/import_controller_test.go exists | FOUND |
| 30-01-SUMMARY.md exists | FOUND |
| RED test commit 4c8eea5 | FOUND |
| GREEN feat commit 92237a0 | FOUND |
| RED test commit e2e7cf4 | FOUND |
| GREEN feat commit afb33bb | FOUND |
| envtest commit d88d2ad | FOUND |
| func IsEnvelopeComplete in pkg/dispatch: 1 definition | PASS |
| func isEnvelopeComplete in cmd/tide-import: 0 (removed) | PASS |
