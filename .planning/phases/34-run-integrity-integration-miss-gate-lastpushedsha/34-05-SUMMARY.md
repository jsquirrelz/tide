---
phase: 34-run-integrity-integration-miss-gate-lastpushedsha
plan: "05"
subsystem: internal/controller, cmd/tide, internal/gates
tags: [integ-03, integ-04, lastpushedsha, tide-resume]
requirements: [INTEG-03, INTEG-04]

dependency_graph:
  requires: ["34-02", "34-03"]
  provides:
    - "Status.Git.LastPushedSHA stamped in the push-Job success arm (D-14)"
    - "reconcileBoundaryPush(ctx, project, dispatchIfMissing bool) + mid-run terminal-Job observation"
    - "ConditionIntegrationIncomplete arms: retry-then-stick (miss) + immediate park (conflict)"
    - "gates.AnnotationResetBoundaryPush + tide resume stamps it + controller consumes it once"
  affects:
    - internal/controller/project_controller.go
    - internal/controller/project_boundary_push_test.go
    - cmd/tide/resume.go
    - cmd/tide/resume_test.go
    - internal/gates/annotation.go

tech_stack:
  added: []
  patterns:
    - "resetBoundaryPushAnnotation moved to internal/gates.AnnotationResetBoundaryPush (a package already shared between cmd/tide and internal/controller) rather than a private const duplicated in each — mirrors the existing gates.AnnotationReject pattern"

key_files:
  modified:
    - internal/controller/project_controller.go
    - internal/controller/project_boundary_push_test.go
    - cmd/tide/resume.go
    - cmd/tide/resume_test.go
    - internal/gates/annotation.go

decisions:
  - "Mid-run observation (Open Question 2, option b): reconcilePhase3Lifecycle now Gets the deterministic tide-push-<project.UID> Job immediately after the Complete fast-path check; if it exists and is terminal, calls reconcileBoundaryPush(ctx, project, false) — dispatchIfMissing=false means this path can OBSERVE but never INITIATE a push. Only the Complete fast-path passes dispatchIfMissing=true."
  - "ConditionIntegrationIncomplete is deliberately NOT an early-return guard at the top of reconcileBoundaryPush (unlike leak/lease). A test-driven fix: the first version added a leak/lease-style guard, which broke D-13's 'auto-clears whenever a later verify+push succeeds' contract by short-circuiting BEFORE the success arm could ever run. The Attempts>=cap arm is the sole mechanism that actually keeps a parked miss from re-dispatching; it re-asserts the sticky condition with its own once-only guard."
  - "The D-12 missing-branch detail message is computed and captured into BoundaryPush.LastError (prefixed `integration-incomplete: `) AT CLASSIFICATION TIME, not re-derived at the cap arm — because the Job/pod carrying the envelope may be TTL'd (300s) by the time the cap is reached, exactly as the plan's pin specifies."
  - "resetBoundaryPushAnnotation was promoted from a private internal/controller const to gates.AnnotationResetBoundaryPush so both cmd/tide/resume.go and internal/controller/project_controller.go reference one exported symbol, matching the existing gates.AnnotationReject / gates.ConsumeReject shared-constant pattern rather than duplicating the literal string in two packages."

metrics:
  duration: "~2h"
  completed: "2026-07-04"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 5
---

# Phase 34 Plan 05: Project-Level SHA Stamp + Condition Arms + tide resume — Summary

**One-liner:** `Status.Git.LastPushedSHA` is now stamped from the push envelope's `HeadSHA` in the SAME patch that sets `BoundaryPushed=True` (armed for the first time ever, per the doc-comment promise at the old :472 that never had wiring); mid-run (pre-Complete) push-Job terminal states are now observed via `Owns(&batchv1.Job{})`; an integration-completeness miss rides the #13b bounded retry then parks sticky (`ConditionIntegrationIncomplete`/`ReasonIntegrationIncomplete`, D-12 named detail), while a merge conflict parks immediately (`ReasonMergeConflict`, zero retries burned); `tide resume` now also resets the boundary-push retry state via a new `tideproject.k8s/reset-boundary-push` annotation.

## Tasks Completed

| Task | Name | Files |
|------|------|-------|
| 1 | Success arm SHA stamp + mid-run observation + gated cumulative dispatch | project_controller.go, project_boundary_push_test.go |
| 2 | Failure arms — miss retry-then-stick + conflict park | project_controller.go, project_boundary_push_test.go |
| 3 | tide resume — boundary-push reset via annotation | cmd/tide/resume.go, resume_test.go, internal/gates/annotation.go, project_controller.go |

## Verification Results (all commands actually run this session)

- `go build ./internal/controller/... ./cmd/tide/...` — PASS
- `go test ./internal/controller/... -count=1` (envtest) — PASS, full suite green (185 specs) including 4 new LastPushedSHA specs, 3 new integration-miss/conflict specs, 1 new reset-boundary-push consumption spec
- `go test ./cmd/tide/... -count=1` — PASS, 18 tests (16 pre-existing unmodified + 2 new: stamps-reset-annotation, does-not-stamp-when-clean)
- `grep -c 'Status.Git.LastPushedSHA = ' internal/controller/project_controller.go` ≥ 1
- `grep -c 'dispatchIfMissing' internal/controller/project_controller.go` ≥ 2
- `grep -c 'succeededTaskBranches' internal/controller/project_controller.go` ≥ 1
- `grep -c 'case "merge-conflict"' internal/controller/project_controller.go` = 1, conflict body contains no re-dispatch call (verified by test: Attempts stays 0)
- `grep -c 'ConditionIntegrationIncomplete' internal/controller/project_controller.go` ≥ 3
- `grep -c 'reset-boundary-push' cmd/tide/resume.go` ≥ 1; `grep -c 'AnnotationResetBoundaryPush' internal/controller/project_controller.go` ≥ 2

## Deviations from Plan Text

- The annotation constant was placed in `internal/gates` (exported, shared) rather than as a private `internal/controller` const duplicated by string literal in `cmd/tide` — a deliberate improvement consistent with the existing `gates.AnnotationReject` pattern the plan's own Task 3 action cites as precedent, not a scope change.
