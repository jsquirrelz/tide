---
phase: 42-trace-context-foundation-planner-level-span-emission
plan: 05
subsystem: infra
tags: [otel, opentelemetry, openinference, tracing, controller-runtime, span-emission, kubernetes]

# Dependency graph
requires:
  - phase: 42-04
    provides: "internal/controller/span_emission.go shared helper (synthesizePlannerSpan/spanEndTime), wired into Milestone + Phase handleJobCompletion, proven envtest pattern"
  - phase: 42-03
    provides: "PlanSpanEmittedUID/PlannerSpanEmittedUID CRD status markers on Plan/Project"
provides:
  - "internal/controller/plan_controller.go handlePlannerJobCompletion wired with marker-gated retroactive AGENT span synthesis"
  - "internal/controller/project_controller.go handleProjectJobCompletion wired with marker-gated retroactive AGENT span synthesis, deliberately outside the ImportSource budget-rollup suppression branch"
  - "envtest proof (span_emission_test.go extended) — all four planner levels now proven against a real in-memory OTel exporter"
affects: [43-trace-propagation]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "1:1 port of the 42-04 marker-gated span-emission skeleton to Plan/Project — no new patterns introduced, closes the planner-level span-emission surface"
    - "Project-level span emission is explicitly NOT suppressed for importSource projects — the completedJob != nil gate handles imports naturally (an import with no real completed Job never reaches the emitter)"

key-files:
  created: []
  modified:
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/span_emission_test.go

key-decisions:
  - "Project span block placed after the envelope-read block and before the reporter-spawn block, sitting BEFORE (source-order) the D-11/R-13 ImportSource rollup-suppression branch — confirmed via line-number assertion (1817 < 1892)"
  - "Plan-level span block inserted after the envelope-read block completes (line 536), which is naturally past the existing early-return-on-nil-EnvReader branch (pre-existing plan_controller.go behavior, unchanged)"
  - "Project-level envelope is keyed project.UID/project.UID in the test fixtures, matching the handler's self-keyed ReadOut call (project IS its own parent at this level)"

patterns-established: []

requirements-completed: [ATTR-01, ATTR-02]

# Metrics
duration: ~25min
completed: 2026-07-15
---

# Phase 42 Plan 05: Planner-Level Span Emission (Plan + Project) Summary

**Ported the 42-04 marker-gated retroactive-span pattern to the last two planner levels (Plan/Project), extended the envtest suite to 13/13 specs across all four levels, and confirmed the full Layer A gate green — closing Phase 42's attribute-complete AGENT span emission goal for the entire Milestone→Phase→Plan→Project chain.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-15T23:03:00Z (approx, first file read)
- **Completed:** 2026-07-15T23:27:31Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments
- `plan_controller.go`'s `handlePlannerJobCompletion` and `project_controller.go`'s `handleProjectJobCompletion` both emit exactly one retroactive `tide.dispatch.<level>` AGENT span per planner Job attempt, gated by the durable `PlanSpanEmittedUID`/`PlannerSpanEmittedUID` markers — independent of `envReadOK`/`isFirstCompletion`, matching the 42-04 skeleton byte-for-byte
- Project-level span emission is confirmed source-order-independent of the D-11/R-13 ImportSource budget-rollup suppression branch — only rollup double-counting is suppressed for imports, never span emission (the `completedJob != nil` gate handles imports naturally, since an import with no real completed Job never reaches the emitter)
- Extended `span_emission_test.go` with "SpanEmission — Plan level" and "SpanEmission — Project level" Describe blocks (3 specs each: succeeded+idempotent, failed-Job with condition-derived end time, nil-completedJob zero-spans), bringing the suite to 13/13 total specs across all four planner levels
- Full Layer A gate green: `make test-int-fast` MAKE_EXIT=0, zero `--- FAIL`/`FAIL` lines — Layer A1 (`test/integration/envtest`) 56/56 specs, Layer A2 (`internal/controller` heavy-labeled suite including all 13 SpanEmission specs) all passing

## Task Commits

Each task was committed atomically:

1. **Task 1: Wire plan + project handlers with marker-gated emission** - `21e6419` (feat)
2. **Task 2: envtest specs for Plan + Project levels and full Layer A gate** - `c73e0d1` (test)

**Plan metadata:** committed separately by the orchestrator after wave merge (worktree mode — no shared-file writes from this agent)

## Files Created/Modified
- `internal/controller/plan_controller.go` - span-emission block inserted into `handlePlannerJobCompletion`, gated on `PlanSpanEmittedUID`, placed after the envelope-read block and before the reporter-spawn block
- `internal/controller/project_controller.go` - identical port gated on `PlannerSpanEmittedUID` (directly on `.Status`, not `.Status.Budget`), placed before the D-11/R-13 ImportSource suppression branch with an explanatory comment on why span emission is NOT suppressed for imports
- `internal/controller/span_emission_test.go` - extended with Plan-level and Project-level `Describe` blocks (3 `It` cases each), mirroring the 42-04 Milestone/Phase shape and the Plan/Project fixture chains from `child_rollup_idempotency_test.go`/`project_rollup_idempotency_test.go`

## Decisions Made
- Reused the exact `retry.RetryOnConflict` + re-fetch + already-set short-circuit + `MergeFromWithOptimisticLock` marker-stamping dance from 42-04, swapping only the field name and receiver type per level (`*tideprojectv1alpha3.Plan` / `tidev1alpha3.Project` — the latter is project_controller.go's existing import alias for the same `api/v1alpha3` package, not a distinct type)
- Kept the Plan-level insertion point exactly where the plan specified: after the envelope-read block completes, which in `plan_controller.go`'s case sits after the function's existing early-return-on-nil-EnvReader branch — left that pre-existing control flow untouched since the plan did not ask to change it
- Project-level test fixtures set `Status.Phase = "Running"` before invoking the handler, mirroring `project_rollup_idempotency_test.go`'s precedent, even though span emission itself has no `Status.Phase` gate (kept for fixture-shape consistency with the established rollup test)

## Deviations from Plan

None - plan executed exactly as written. Both insertion points, marker names, and test fixture shapes matched the plan's `<interfaces>` block and 42-PATTERNS.md exactly on the first pass; no rework was needed.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- All four planner-level completion handlers (Milestone, Phase, Plan, Project) now emit attribute-complete, marker-gated retroactive AGENT spans — Phase 42's goal state is realized
- 13/13 SpanEmission envtest specs green: `go test ./internal/controller/ -ginkgo.label-filter='heavy' -ginkgo.focus='SpanEmission'` → `Ran 13 of 217 Specs ... SUCCESS! -- 13 Passed | 0 Failed | 0 Pending | 204 Skipped`
- Full Layer A gate confirmed twice for reliability: `make test-int-fast` → `MAKE_EXIT=0`, `grep -cE '^--- FAIL|^FAIL\s'` → `0` on both runs; Layer A1 56/56, Layer A2 (heavy suite) all green
- `grep -rn 'synthesizePlannerSpan' internal/controller/*.go` → exactly 4 handler call sites (milestone, phase, plan, project) + the shared definition in `span_emission.go`
- No blockers. Phase 43's trace-context propagation work (parent SpanContext injection across levels) is deliberately out of scope here — Option A (independent-root spans, plan 42-02's recorded decision) is preserved at all four levels
- `git status --short` clean after both task commits — no untracked files, no unexpected deletions

---
*Phase: 42-trace-context-foundation-planner-level-span-emission*
*Completed: 2026-07-15*

## Self-Check: PASSED

All created/modified files and commit hashes verified present on disk / in git history (see below).
