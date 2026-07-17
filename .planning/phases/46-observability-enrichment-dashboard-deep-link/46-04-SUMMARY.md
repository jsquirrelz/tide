---
phase: 46-observability-enrichment-dashboard-deep-link
plan: 04
subsystem: observability
tags: [opentelemetry, openinference, phoenix, otel-attributes, controller, session-id, cost-double-count]

# Dependency graph
requires:
  - phase: 46-observability-enrichment-dashboard-deep-link
    plan: 01
    provides: "otelai.SessionID/Metadata/MetadataJSON/Tags helpers; ReporterOptions.SessionID/MetadataJSON/Tags fields rendered as --session-id=/--metadata=/--tags= Args on both reporter Job shapes"
provides:
  - "buildLevelEnrichment(project, level, levelName, waveIndex) — shared session.id/metadata/tags computation for a level's AGENT span AND its reporter's LLM spans"
  - "synthesizePlannerSpan enriched with session.id/metadata/tags, drops llm.token_count.* at ALL FIVE levels (46 D-03 correction — supersedes RESEARCH.md's Task-only recommendation)"
  - "traceparentForLevel(project, spanIDHex, sampled) — real sampled bit threaded at 5 same-reconcile reporter-spawn sites, literal true (documented limitation) at 4 dispatch-time sites"
  - "All five ReporterOptions construction sites (milestone/phase/plan/project/task) carry SessionID/MetadataJSON/Tags computed from the same buildLevelEnrichment inputs the level's AGENT span used"
affects: [47-phoenix-self-host-proof (the live end-to-end trace-tree proof depends on session.id/metadata/tags being queryable and cost views not double-counting)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared enrichment computation (buildLevelEnrichment) called once per completion, feeding both the AGENT span attributes and the reporter Job's transport Args — guarantees byte-identical session.id/metadata/tags across a level's AGENT span and its sibling LLM spans"
    - "Local `sampled` variable seeded true, overwritten only when this reconcile actually emits a span — same-reconcile reporter spawns get the real bit; cross-reconcile dispatch-time sites document why they cannot"
    - "Struct-parameter refactor (spawnReporterIfNeeded now takes ReporterOptions) instead of unbounded positional-parameter growth"

key-files:
  created: []
  modified:
    - internal/controller/span_emission.go
    - internal/controller/span_emission_unit_test.go
    - internal/controller/span_emission_test.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go

key-decisions:
  - "D-03 scope correction (planner_correction, authoritative over RESEARCH.md): dropped llm.token_count.* from ALL FIVE levels' AGENT spans, not Task-only — the combined-mode reporter Job (spawned at all four planner-level completions) ALSO synthesizes per-call LLM message spans via the same synthesizeSpans code path the trace-only Task Job uses, so every level has a sibling per-call token-count source and Phoenix's span-kind-agnostic SpanCost rollup would double-count any level that kept a rolled-up total"
  - "buildLevelEnrichment omits gate_profile for level=project (Gates struct has no Project field) and omits any metadata key whose value would be empty — absence over a fabricated empty value"
  - "spawnReporterIfNeeded refactored from 6 trailing positional params to a single ReporterOptions param — the plan explicitly permitted either shape; the struct form was chosen since ReporterOptions had already grown past readable-positional size after Plan 46-01"
  - "Dispatch-time traceparent sites (subagent Job env, 4 of them) keep sampled=true unconditionally with a comment citing the cross-reconcile limitation — no new {Level}TraceSampled status field (RESEARCH Pitfall 3's rejected over-scope honored)"

patterns-established:
  - "Enrichment-attribute-sharing contract: any future span-emitting call site that wants session.id/metadata/tags MUST route through buildLevelEnrichment with the same (level, levelName, waveIndex) inputs its sibling spans use — never hand-compute a subset"

requirements-completed: [OBS-02, OBS-03]

# Metrics
duration: 23min
completed: 2026-07-17
---

# Phase 46 Plan 04: Manager-Side Span Enrichment + D-03 Cost Double-Count Fix Summary

**session.id/metadata/tags on all five AGENT dispatch spans and their reporter-spawned LLM spans, plus the D-03 fix dropping llm.token_count.* from every AGENT span (not just Task) so Phoenix's SpanCost rollup counts each run's tokens exactly once.**

## Performance

- **Duration:** 23 min
- **Started:** 2026-07-17T01:38:01-04:00 (wave-1 merge base)
- **Completed:** 2026-07-17T02:01:26-04:00
- **Tasks:** 2
- **Files modified:** 9 (8 unique — span_emission.go and its two test files, plus 5 controllers and dispatch_helpers.go)

## Accomplishments
- `buildLevelEnrichment` computes the session.id/metadata/tags triple from a single set of inputs (project, level, levelName, waveIndex) shared identically between a level's own AGENT span and its reporter's LLM spans — Phoenix's ProjectSession/tag-filter grouping now has a byte-identical join key.
- `synthesizePlannerSpan` no longer emits `otelai.TokenCount` at ANY level — verified with a new `TestSynthesizePlannerSpanOmitsTokenCount` regression guard parameterized across project/milestone/phase/plan/task, plus evolved (not deleted) assertions in 5 pre-existing envtest specs.
- The D-02 sampled bit is now real wherever knowable: 5 same-reconcile reporter-spawn sites thread the actual `span.SpanContext().IsSampled()` value captured before `End()`; 4 cross-reconcile dispatch-time sites keep a documented `true` literal (no new CRD field, honoring RESEARCH Pitfall 3's rejected schema change).
- All five `ReporterOptions` construction sites now populate `SessionID`/`MetadataJSON`/`Tags`, wiring the Plan 46-01 transport layer end to end.

## Task Commits

Each task was committed atomically:

1. **Task 1: Enrichment attributes + D-03 token-count drop in synthesizePlannerSpan (+ call-site signature updates)** - `0cc9c01` (feat)
2. **Task 2: D-02 sampled-bit threading + enrichment transport at the five reporter spawns** - `dc6bd45` (feat)

_Note: worktree mode — SUMMARY.md commit follows this file; STATE.md/ROADMAP.md are excluded (orchestrator-owned)._

## Files Created/Modified
- `internal/controller/span_emission.go` - `buildLevelEnrichment` (new); `synthesizePlannerSpan` gains `levelName`/`waveIndex` params, returns `(spanID, sampled, emitted)`, drops `otelai.TokenCount`, adds `otelai.SessionID`/`MetadataJSON`/`Tags`; `traceparentForLevel` gains a `sampled bool` param
- `internal/controller/span_emission_unit_test.go` - evolved `TestSynthesizePlannerSpanSucceededComplete` (D-03 citation, new enrichment assertions); new `TestSynthesizePlannerSpanOmitsTokenCount`, `TestBuildLevelEnrichment{ProjectOmitsGateProfile,ConservativeFailureHalt,StrictDefault,NilProject}`, `TestTraceparentForLevelCarriesRealSampledBit`; all 9 pre-existing `synthesizePlannerSpan(...)` call sites updated to the new signature
- `internal/controller/span_emission_test.go` - 6 envtest specs (one per level plus the Task output-path-validation case) evolved from positive `llm.token_count.*` assertions to negative (D-03 citation) assertions
- `internal/controller/dispatch_helpers.go` - `spawnReporterIfNeeded` refactored to accept a single `ReporterOptions` param instead of 6 trailing positional args
- `internal/controller/{milestone,phase,plan,project,task}_controller.go` - each level's `handle*JobCompletion`/`emitTaskSpanOnce` threads a local `sampled` variable and calls `buildLevelEnrichment`; each dispatch-time site passes `traceparentForLevel(..., true)` with a documented-limitation comment; `task_controller.go`'s `emitTaskSpanOnce` now returns `bool` and `spawnTaskTraceReporterIfNeeded` gained a `sampled` parameter

## Decisions Made
- Followed the plan's `<planner_correction>` block verbatim: dropped token counts from all five levels, not just Task, and cited the evidence chain (combined-mode reporter Jobs also run `synthesizeSpans`) in the code comment at the removal site.
- Chose the `ReporterOptions`-struct refactor for `spawnReporterIfNeeded` over a further positional extension — the plan explicitly permitted either; the struct form keeps the signature stable for future `ReporterOptions` field growth.
- Reused each controller's pre-existing `projectUID` local variable (already nil-safe) for `SessionID` at the milestone/phase/plan reporter-spawn sites rather than introducing a new nil-check; `project_controller.go` and `task_controller.go` use `project`/`project.UID` directly since those call sites are already proven non-nil by their surrounding guards.

## Deviations from Plan

None — plan executed exactly as written, including the authoritative `<planner_correction>` override of RESEARCH.md's Task-only D-03 scope. All `must_haves.truths`, `artifacts`, and `key_links` in the plan frontmatter are satisfied:
- Every AGENT dispatch span (all five levels) carries `session.id`/metadata/`tag.tags`, emitted inside `synthesizePlannerSpan` via the Plan 46-01 `otelai` helpers.
- No AGENT dispatch span carries `llm.token_count.*` at any level (verified by `TestSynthesizePlannerSpanOmitsTokenCount` and the evolved envtest specs).
- The five reporter-spawn `ReporterOptions` literals populate `SessionID`/`MetadataJSON`/`Tags` from the same `buildLevelEnrichment` inputs the level's AGENT span used.
- `synthesizePlannerSpan` returns the emitted span's real `IsSampled()` bit; same-reconcile reporter spawns thread it; the four dispatch-time sites keep `sampled=true` with a limitation comment; no new CRD status field was added (confirmed via `git status api/v1alpha3/ config/crd/` returning empty both before and after Task 2).
- Task-level metadata carries `wave_index` from `owner.LabelWaveIndex`; planner levels omit it; a missing label degrades to an absent key (verified by `TestBuildLevelEnrichmentStrictDefault`'s empty-waveIndex case).
- The 6 pre-existing `llm.token_count.*` positive assertions in `span_emission_test.go` were evolved (not deleted) with 46 D-03 citations, matching the MSG-03 precedent.

## Issues Encountered

None. `go build ./...` shows the same pre-existing, unrelated `cmd/tide-demo-init` embed-directive failure documented in Plan 46-01's summary (confirmed present on the wave-1 merge base before this plan's changes, outside `files_modified`); all scoped builds/tests (`./internal/controller/...`, `./pkg/otelai/...`, `./internal/reporter/...`) pass cleanly, including the full `internal/controller` envtest suite (233+ specs, ~112s, zero `FAIL` lines) run twice — once after Task 1, once after Task 2.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- OBS-02/OBS-03 are fully wired manager-side: every dispatch span and every reporter-emitted LLM span for the same run now carries a matching `session.id`, and Phoenix's cost rollups will no longer double-count planner-level spend now that AGENT spans carry no token counts at any level.
- Plan 46-05 (or whichever plan wires the dashboard deep link, per the phase's OBS-01/OBS-04 scope) can rely on `session.id` == `Project.UID` being present on every span in a run's trace tree without additional manager-side changes.
- Phase 47's live Phoenix proof (PROOF-01) should specifically verify a multi-level run's SpanCost/session rollup sums to the per-call LLM spans' totals exactly once — this plan's D-03 fix is the correctness precondition for that verification, but it has not itself been proven against a live Phoenix instance (no live Phoenix in this plan's scope).

---
*Phase: 46-observability-enrichment-dashboard-deep-link*
*Completed: 2026-07-17*
