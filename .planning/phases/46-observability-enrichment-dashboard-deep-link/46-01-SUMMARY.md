---
phase: 46-observability-enrichment-dashboard-deep-link
plan: 01
subsystem: observability
tags: [opentelemetry, openinference, phoenix, otel-attributes, reporter, cli-flags]

# Dependency graph
requires:
  - phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
    provides: EmitSpans/tracesynth.go's per-call LLM span synthesis pipeline
  - phase: 45-runtime-neutral-adapter-seam
    provides: ReporterOptions field + Args-append pattern (SkipMessageSpans precedent), same-commit flag/Args crash-loop-guard discipline
provides:
  - "otelai.SessionID/Metadata/MetadataJSON/Tags helpers, semconv-backed (ATTR-03)"
  - "ReporterOptions.SessionID/MetadataJSON/Tags fields rendered as --session-id=/--metadata=/--tags= Args on both reporter Job shapes"
  - "tide-reporter CLI flags --session-id/--metadata/--tags registered and threaded into EmitSpans"
  - "EmitSpans(ctx, tracer, calls, artifactPath, sessionID, metadataJSON, tags) stamping session.id/metadata/tag.tags on every emitted LLM span"
affects: [46-observability-enrichment-dashboard-deep-link (later plans wiring the manager-side computation and 5 spawn sites), dashboard-deep-link]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Semconv-module-backed attribute keys (never hand-rolled spec-family string literals) — ATTR-03 source-grep gate"
    - "Manager-authored transport as Job-spec CLI Args, never Env, never pod-writable PVC data (D-05)"
    - "Conditional attribute stamping: absent when empty, never a fabricated empty value"

key-files:
  created: []
  modified:
    - pkg/otelai/attrs.go
    - pkg/otelai/attrs_test.go
    - internal/controller/reporter_jobspec.go
    - internal/controller/reporter_jobspec_test.go
    - cmd/tide-reporter/main.go
    - internal/reporter/tracesynth.go
    - internal/reporter/tracesynth_test.go

key-decisions:
  - "Tags() emits attribute.STRINGSLICE (native list); Metadata()/MetadataJSON() emit attribute.STRING containing JSON — the two encodings are deliberately different per the OpenInference spec (Pitfall 4)"
  - "MetadataJSON is a distinct passthrough helper (no re-marshal) from Metadata() (which marshals) — transport paths where the manager pre-encodes JSON use MetadataJSON; direct map callers use Metadata()"
  - "Session/metadata/tags Args land in the SAME commit as the tide-reporter flag registration (Phase 43 crash-loop guard: an Args entry without a registered flag crash-loops every reporter Job)"

patterns-established:
  - "Conditional per-span enrichment attributes (session.id/metadata/tag.tags) ride the EXISTING EmitSpans per-call loop and the EXISTING BuildReporterJob Args assembly — no new emission call sites, no new markers"

requirements-completed: [OBS-02, OBS-03]

# Metrics
duration: 8min
completed: 2026-07-17
---

# Phase 46 Plan 01: Enrichment-Attribute Plumbing (Reporter Side) Summary

**Semconv-backed session.id/metadata/tag.tags otelai helpers, threaded as manager-authored CLI Args through ReporterOptions and tide-reporter flags, stamped conditionally on every EmitSpans-emitted LLM span.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-07-17T01:20:00-04:00 (approx, first commit 01:20:49)
- **Completed:** 2026-07-17T01:24:58-04:00
- **Tasks:** 3
- **Files modified:** 7

## Accomplishments
- Three new `pkg/otelai` helpers (`SessionID`, `Metadata`, `Tags`) plus the transport-side `MetadataJSON` passthrough, all resolving keys from the `openinference-semantic-conventions` module — `TestKeysUseSemconvModule` stays green.
- `ReporterOptions` gained `SessionID`/`MetadataJSON`/`Tags` fields rendered as `--session-id=`/`--metadata=`/`--tags=` Args on both the materialization and trace-only reporter Job shapes, never Env — matching the `TraceParent` precedent.
- `tide-reporter` registers the three matching CLI flags in the same commit as the Args appends (Phase 43 crash-loop guard honored).
- `EmitSpans` now stamps `session.id`, `metadata`, and `tag.tags` on every emitted LLM span when the manager supplies non-empty values, and omits all three when empty — proven across a multi-call fixture, with per-call `llm.token_count.*` attributes unchanged.

## Task Commits

Each task was committed atomically:

1. **Task 1: otelai SessionID/Metadata/Tags helpers (semconv-backed, correctly typed)** - `66e8609` (feat)
2. **Task 2: ReporterOptions transport fields + Args + tide-reporter flag registration** - `eecccfd` (feat)
3. **Task 3: EmitSpans applies session/metadata/tags to every LLM span** - `3062950` (feat)

_Note: worktree mode — SUMMARY.md commit follows this file; STATE.md/ROADMAP.md are excluded (orchestrator-owned)._

## Files Created/Modified
- `pkg/otelai/attrs.go` - `SessionID`, `Metadata`, `MetadataJSON`, `Tags` helpers
- `pkg/otelai/attrs_test.go` - `TestSessionID`, `TestMetadata`, `TestMetadataJSON`, `TestTags`
- `internal/controller/reporter_jobspec.go` - `ReporterOptions.SessionID/MetadataJSON/Tags` fields + conditional Args appends (both Job shapes)
- `internal/controller/reporter_jobspec_test.go` - present/absent subtests for all three new Args, on both Job shapes
- `cmd/tide-reporter/main.go` - `--session-id`/`--metadata`/`--tags` flags, `reporterConfig` fields, `synthesizeSpans` comma-split + threading into `EmitSpans`
- `internal/reporter/tracesynth.go` - `EmitSpans` signature extended with `sessionID`/`metadataJSON`/`tags`, conditional `SetAttributes` calls
- `internal/reporter/tracesynth_test.go` - `TestEmitSpans_EnrichmentTriple`, `TestEmitSpans_EnrichmentTripleOmittedWhenEmpty`, `TestEmitSpans_TokenCountUnchangedWithEnrichment`; all 13 pre-existing `EmitSpans(...)` call sites updated to the new 7-arg signature with `"", "", nil`

## Decisions Made
- `MetadataJSON` (passthrough, no marshal) is a distinct helper from `Metadata` (marshals a map) — per the plan's Task 3 action, the transport path threads pre-encoded JSON from the manager, so the reporter never re-marshals.
- Kept `Tags` as `attribute.StringSlice` (never JSON-encoded) — the Pitfall 4 regression guard the plan called out explicitly, verified with an assertion on `Value.Type() == attribute.STRINGSLICE`.

## Deviations from Plan

None — plan executed exactly as written. All `must_haves.truths`, `artifacts`, and `key_links` in the plan frontmatter are satisfied:
- `otelai.SessionID/Metadata/MetadataJSON/Tags` exist, resolve keys from `semconv.SessionID/Metadata/TagTags`.
- `Tags()` emits `attribute.STRINGSLICE`; `Metadata()`/`MetadataJSON()` emit `attribute.STRING` JSON.
- `BuildReporterJob` renders the three Args conditionally on both Job shapes; `tide-reporter` registers all three flags in the same commit.
- `EmitSpans` stamps all three attributes on every LLM span when non-empty, omits them when empty.

## Issues Encountered
- `go build ./...` fails on a pre-existing, unrelated `cmd/tide-demo-init` embed directive (`all:fixture: no matching files found`) — confirmed present on `HEAD` before any of this plan's changes and outside `files_modified`; scoped builds/tests (`./pkg/otelai/...`, `./internal/reporter/...`, `./internal/controller/...`, `./cmd/tide-reporter/...`) all pass cleanly. Logged to `deferred-items.md` scope note below (out of scope per plan boundary, not fixed).
- `internal/controller`'s envtest suite requires `KUBEBUILDER_ASSETS` (etcd/kube-apiserver binaries) not present by default in this environment — ran `make setup-envtest` to fetch them, then re-ran `go test ./pkg/otelai/... ./internal/reporter/... ./internal/controller/...` with `KUBEBUILDER_ASSETS` set: all three packages green (`internal/controller` 233 specs, 111.8s), zero `FAIL` lines.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- The reporter-side half of OBS-02/OBS-03 is complete and tested: `otelai` helpers, `ReporterOptions`→Args→flags transport, and `EmitSpans` stamping are all locked per the plan's `<interfaces>` contract (identifiers unchanged, ready for Plan 46-04 to consume).
- Plan 46-04 (manager side) still owes: computing `SessionID`/`MetadataJSON`/`Tags` values at the five `ReporterOptions{...}` construction sites (`dispatch_helpers.go`, `task_controller.go`, `plan_controller.go`, `project_controller.go`, and the milestone/phase equivalents) and threading them alongside `TraceParent`/`SkipMessageSpans`. No blockers — the reporter-side surface this plan builds is stable and byte-identical for zero-value callers today (every existing `ReporterOptions{...}` call site continues to compile and behave unchanged since the new fields are additive-optional).

---
*Phase: 46-observability-enrichment-dashboard-deep-link*
*Completed: 2026-07-17*
