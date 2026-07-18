---
phase: 42-trace-context-foundation-planner-level-span-emission
plan: 04
subsystem: infra
tags: [otel, opentelemetry, opeinference, tracing, controller-runtime, span-emission, kubernetes]

# Dependency graph
requires:
  - phase: 42-01
    provides: "pkg/otelai attrs.go rework (module-backed keys, LLMIdentity/FailureDetail/EnvelopeDegraded, TokenCount with .total)"
  - phase: 42-03
    provides: "MilestoneSpanEmittedUID/PhaseSpanEmittedUID/PlanSpanEmittedUID/PlannerSpanEmittedUID CRD status markers"
provides:
  - "internal/controller/span_emission.go — shared spanEndTime + synthesizePlannerSpan helper, callable from all four planner-level completion handlers"
  - "Milestone + Phase handleJobCompletion wired with marker-gated retroactive AGENT span synthesis"
  - "envtest proof (span_emission_test.go) against a real in-memory OTel exporter"
affects: [42-05, 43-trace-propagation]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Retroactive span synthesis: tracer.Start/span.End both called within a single handler invocation using completedJob.Status timestamps — never held open across a Reconcile() return"
    - "Span-emission idempotency marker deliberately separate from the existing *RolledUpUID marker (envReadOK-independent gate, matching the RetryOnConflict + MergeFromWithOptimisticLock stamping dance)"
    - "Second, envelope-independent ResolveProvider call at completion time to source llm.model_name/llm.provider (never read from the envelope, which has no model field)"

key-files:
  created:
    - internal/controller/span_emission.go
    - internal/controller/span_emission_unit_test.go
    - internal/controller/span_emission_test.go
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go

key-decisions:
  - "spanEndTime falls back to the JobFailed condition's LastTransitionTime when CompletionTime is nil (Pitfall 1 — CompletionTime is only ever set on success)"
  - "Span-emission gate is completedJob != nil && Status.<Level>SpanEmittedUID != completedJob.Name only — never envReadOK or isFirstCompletion, so degraded-envelope Jobs still get exactly one span (D-04)"
  - "promptTokens re-mapped to InputTokens + CacheReadTokens + CacheCreationTokens at the call site (D-08/Pitfall 4) — TokenCount's signature itself is unchanged from plan 42-01"

patterns-established:
  - "Pattern this plan establishes for 42-05 to port 1:1 to Plan/Project: insert the span block after the envelope-read block, before spawnReporterIfNeeded, gated on the level's SpanEmittedUID marker alone"

requirements-completed: [ATTR-01, ATTR-02]

# Metrics
duration: ~55min
completed: 2026-07-15
---

# Phase 42 Plan 04: Planner-Level Span Emission (Milestone + Phase) Summary

**Shared retroactive-span-synthesis helper wired into Milestone + Phase `handleJobCompletion`, proven via 9 unit tests + 7 envtest specs against a real in-memory OTel exporter — attribute-complete AGENT spans (model, provider, token counts) for both succeeded and failed planner Jobs.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-07-15T21:59:00Z (approx, first file read)
- **Completed:** 2026-07-15T22:57:14Z
- **Tasks:** 3
- **Files modified:** 5 (3 created, 2 modified)

## Accomplishments
- `spanEndTime` + `synthesizePlannerSpan` shared helper (`internal/controller/span_emission.go`) synthesizes exactly one retroactive `tide.dispatch.<level>` AGENT span per planner Job attempt, correctly branching success (CompletionTime) vs failure (JobFailed condition's LastTransitionTime) end timestamps
- Milestone and Phase `handleJobCompletion` twins wired with marker-gated emission (`MilestoneSpanEmittedUID` / `PhaseSpanEmittedUID`), inserted after the envelope-read block and before `spawnReporterIfNeeded`, deliberately independent of `envReadOK` and `isFirstCompletion`
- Degraded-envelope Jobs (`envReadOK=false`) still emit a span carrying `tide.envelope.degraded=true` with the model name still resolved via a second `ResolveProvider` call — zero token-count attributes
- Failed Jobs emit `codes.Error` status with the classified `Reason` as description plus `tide.exit_code`/`tide.reason` attributes when the envelope is readable
- Token counts re-mapped at the call site so `llm.token_count.prompt` includes cache subsets (D-08) and `llm.token_count.total` = prompt + completion
- 7 envtest specs prove idempotency (second call with the same Job, re-fetched marker, does not duplicate the span), the nil-Job no-op path, and the failed-Job end-timestamp derivation — all against a real `tracetest.InMemoryExporter`

## Task Commits

Each task was committed atomically:

1. **Task 1: Shared synthesis helper — span_emission.go + plain unit tests** - `98490ff` (feat, TDD)
2. **Task 2: Wire milestone + phase handlers with marker-gated emission** - `b17e1fe` (feat)
3. **Task 3: envtest SpanEmission specs — Milestone + Phase levels** - `1781f62` (test)

**Plan metadata:** committed separately by the orchestrator after wave merge (worktree mode — no shared-file writes from this agent)

## Files Created/Modified
- `internal/controller/span_emission.go` - `spanEndTime` (success/failure/nil timestamp resolution) + `synthesizePlannerSpan` (tracer.Start/SetAttributes/SetStatus/span.End, D-01..D-04/D-07/D-08)
- `internal/controller/span_emission_unit_test.go` - 9 plain `testing.T` tests covering every outcome branch (nil job, succeeded, failed, degraded envelope, nil project + empty model)
- `internal/controller/span_emission_test.go` - 7 Ginkgo envtest specs (4 milestone + 3 phase) against a real in-memory OTel exporter
- `internal/controller/milestone_controller.go` - span-emission block inserted into `handleJobCompletion`, gated on `MilestoneSpanEmittedUID`
- `internal/controller/phase_controller.go` - identical 1:1 port gated on `PhaseSpanEmittedUID`

## Decisions Made
- Reused `isJobSucceeded`/`isJobFailed` from `project_controller.go` verbatim rather than duplicating Job-outcome logic (per 42-PATTERNS.md's explicit instruction)
- Kept the marker-stamping dance byte-identical to the existing `*RolledUpUID` pattern (RetryOnConflict + re-fetch + already-set short-circuit + `MergeFromWithOptimisticLock`) so the two deliberate deviations (no envReadOK gate, no isFirstCompletion gate) are the only differences from the established precedent
- Test fixtures set `Project.Spec.Subagent.Model` directly (not per-level `Levels` overrides) so `ResolveProvider` resolves the same model at every level via the mid-chain fallback, per the plan's explicit guidance

## Deviations from Plan

None - plan executed exactly as written. Two comment wordings were adjusted mid-task (removing the literal substrings `time.Now()` and `synthesizePlannerSpan` from doc comments) so the plan's own blind-grep acceptance criteria (`grep -c 'time.Now()'` and "exactly 1 call site per file") would not false-positive on self-referential comments — this is a wording-only adjustment to satisfy the plan's own verification commands, not a behavior change, so it is not logged as a Rule 1/2/3 deviation.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- The exact pattern (shared helper + marker-gated insertion + envtest shape) is fully established and green — plan 42-05 ports it 1:1 to Plan and Project levels
- `make test-int-fast`: Layer A1 (`test/integration/envtest`) 56/56 specs green; Layer A2 (`internal/controller` heavy-labeled Ginkgo suite, including the 7 new SpanEmission specs) green — `MAKE_EXIT=0`, zero `--- FAIL`/`FAIL` lines
- No blockers. Phase 43's propagation work (parent SpanContext injection) is deliberately out of scope here — Option A (independent-root spans) was preserved per plan 42-02's recorded decision

---
*Phase: 42-trace-context-foundation-planner-level-span-emission*
*Completed: 2026-07-15*

## Self-Check: PASSED

All created files and commit hashes verified present on disk / in git history.
