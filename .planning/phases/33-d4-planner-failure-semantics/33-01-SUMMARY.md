---
phase: 33-d4-planner-failure-semantics
plan: 01
subsystem: controller
tags: [planner-failure, D4, shared-helper, api-vocab, unit-test]
dependency_graph:
  requires: []
  provides: [isPlannerFailure predicate, ReasonPlannerFailed constant]
  affects: [internal/controller, api/v1alpha2]
tech_stack:
  added: []
  patterns: [shared-helper file (mirrors failure_halt.go), stdlib table test]
key_files:
  created:
    - internal/controller/planner_failure.go
    - internal/controller/planner_failure_test.go
  modified:
    - api/v1alpha2/shared_types.go
decisions:
  - "isPlannerFailure uses exactly envReadOK && out.ExitCode != 0 && out.ChildCount == 0 per D-06; no expansion for ExitCode!=0/ChildCount>0 case (deferred, per RESEARCH OQ-1)"
  - "ReasonPlannerFailed added to v1alpha2 only; v1alpha1 untouched (controllers import v1alpha2 exclusively)"
  - "Plan and project deliberately excluded from guard scope; documented in package doc with citation to D-02"
metrics:
  duration: ~10 minutes
  completed: 2026-06-29
  tasks: 2
  files_created: 2
  files_modified: 1
---

# Phase 33 Plan 01: D4 Wave-0 Foundation Summary

**One-liner:** `isPlannerFailure` predicate (false-leaf guard) and `ReasonPlannerFailed` vocabulary — Wave-0 foundation for D4 planner failure semantics at phase and milestone succession sites.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add ReasonPlannerFailed to v1alpha2 shared_types.go | 298d9c3 | api/v1alpha2/shared_types.go |
| 2 | Create isPlannerFailure shared helper + pure unit test | 00f9cd0 | internal/controller/planner_failure.go, internal/controller/planner_failure_test.go |

## What Was Built

**`internal/controller/planner_failure.go`** — Package-level `isPlannerFailure(out pkgdispatch.EnvelopeOut, envReadOK bool) bool` returning exactly `envReadOK && out.ExitCode != 0 && out.ChildCount == 0`. The file-level package doc names phase and milestone as the only guarded levels and cites D-02 (33-CONTEXT.md) explaining why plan and project are deliberately excluded — the `gates.BoundaryDetected` path returns `matched > 0` (false on zero children) so zero-child failed planners at those levels cannot falsely advance the parent. File mirrors the shared-helper convention of `failure_halt.go` and `billing_halt.go`.

**`internal/controller/planner_failure_test.go`** — Stdlib `testing` table test `TestIsPlannerFailure` with 4 parallel subtests:
- false-leaf (ExitCode=1, ChildCount=0, envReadOK=true) → true
- genuine-leaf (ExitCode=0, ChildCount=0, envReadOK=true) → false (PLANFAIL-03 invariant)
- unreadable envelope (ExitCode=1, ChildCount=0, envReadOK=false) → false
- children present (ExitCode=1, ChildCount=3, envReadOK=true) → false

**`api/v1alpha2/shared_types.go`** — New Phase 33 const block with `ReasonPlannerFailed = "PlannerFailed"` inserted after the Phase 11 `ReasonWaveIntegrationFailed` block. v1alpha1 untouched.

## Verification Results

```
go test ./internal/controller/... -run TestIsPlannerFailure -count=1  → PASS (4/4 subtests)
go build ./api/... ./internal/controller/...                          → exit 0
grep ReasonPlannerFailed api/v1alpha2/shared_types.go                 → FOUND
grep -r ReasonPlannerFailed api/v1alpha1/                             → 0 hits (clean)
grep -c 'envReadOK && out.ExitCode != 0 && out.ChildCount == 0' internal/controller/*.go → 1 (single source of truth)
```

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — this plan adds a pure predicate and a string constant; no UI or data flow is involved.

## Threat Flags

None — no new external surface introduced. T-33-01 mitigation (4-case unit table pins predicate correctness) is in place.

## Self-Check: PASSED

- [x] `internal/controller/planner_failure.go` exists and contains `func isPlannerFailure(`
- [x] `internal/controller/planner_failure_test.go` exists and contains `func TestIsPlannerFailure(`
- [x] `api/v1alpha2/shared_types.go` contains `ReasonPlannerFailed = "PlannerFailed"`
- [x] Commit 298d9c3 (Task 1) exists
- [x] Commit 00f9cd0 (Task 2) exists
- [x] `go test ./internal/controller/... -run TestIsPlannerFailure -count=1` exits 0 (4/4)
- [x] `go build ./api/... ./internal/controller/...` exits 0
