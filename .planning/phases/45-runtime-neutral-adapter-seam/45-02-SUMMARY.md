---
phase: 45-runtime-neutral-adapter-seam
plan: 02
subsystem: telemetry
tags: [otel, opentelemetry, opeinference, reporter, adapter-seam, flag-parsing, contract-test]

# Dependency graph
requires:
  - phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
    provides: "internal/reporter/tracesynth.go's EmitSpans/ReconstructConversation LLM message-array synthesizer and the cmd/tide-reporter TracerProvider call site (synthesizeSpans, TRACE-03) this plan wraps in a skip guard"
provides:
  - "reporterConfig.SkipMessageSpans + the --skip-message-spans bareword flag on the reporter binary"
  - "synthesizeSpans's D-05 sole skip point — the literal first statement, before path construction and the sentinel os.Stat"
  - "cmd/tide-reporter/adapter_seam_test.go's D-09 contract test proving zero duplicate spans when a self-instrumenting runtime and a skipped reporter run share one exporter"
  - "D-08 doc contract on internal/reporter/tracesynth.go naming it the anthropic-CLI runtime's adapter and pkg/dispatch.SelfInstruments as the routing datum"
affects: [46-observability-enrichment, langgraph-specialist-beachhead]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Bareword CLI flag (fs.Bool) for a reporter skip signal, mirroring the existing --trace-only convention — Args-based, never Env"
    - "Skip guard as the literal first statement of the function it guards, before any I/O, so a skip touches zero PVC paths"
    - "Contract test proving a shared exporter reaches exact span-count parity (not just >=1) as the anti-duplication assertion"

key-files:
  created:
    - cmd/tide-reporter/adapter_seam_test.go
  modified:
    - cmd/tide-reporter/main.go
    - cmd/tide-reporter/main_test.go
    - internal/reporter/tracesynth.go

key-decisions:
  - "Reused installStubTracerProvider's newTracerProvider seam for the reporter's own synthesis path but had to also directly call otel.SetTracerProvider before creating the stub's own span, since the seam only installs the global provider lazily when runWithClient invokes it — the stub's span is created before that call"
  - "Reworded two 'LangGraph'-mentioning doc comments in adapter_seam_test.go to 'vendor-specific' to satisfy the plan's literal zero-hits grep acceptance criterion, without changing the actual test behavior or scope"

patterns-established:
  - "D-05 single-skip-point discipline: a capability-suppression guard belongs as the first statement of the one function with both the flag and the about-to-run side effect — not scattered across call sites"

requirements-completed: [ADAPT-01]

# Metrics
duration: 9min
completed: 2026-07-17
---

# Phase 45 Plan 02: Reporter-Side Runtime-Neutral Adapter Seam Summary

**Reporter parses `--skip-message-spans` into `reporterConfig`, guards `synthesizeSpans` as the sole D-05 skip point, and a new contract test proves a self-instrumenting runtime's span coexists with a skipped reporter run at exactly zero duplicates.**

## Performance

- **Duration:** 9 min
- **Started:** 2026-07-17T02:30:35Z
- **Completed:** 2026-07-17T02:38:48Z
- **Tasks:** 2 completed
- **Files modified:** 4 (1 created, 3 modified)

## Accomplishments
- `reporterConfig.SkipMessageSpans` + the `--skip-message-spans` bareword flag land in `parseFlags`, default false (D-03 absent-means-synthesize), verified via present/absent subtests asserting the parsed struct field directly (Pitfall 3 guard).
- `synthesizeSpans` returns as its literal first statement when the flag is set — before `eventsPath`/`sentinelPath` construction and before the sentinel `os.Stat` — so a skipped run touches no PVC path and writes no sentinel (D-05).
- `TestRunTraceOnly_SkipsSynthesisWhenFlagSet` proves the inverse of `TestRunTraceOnly_EmitsSpans` against the identical fixture: zero spans, absent sentinel, exit 0, skip line on stderr; `TestRunTraceOnly_EmitsSpans` itself is byte-unmodified (D-10 inverse pin, confirmed via insert-only diff hunks).
- `cmd/tide-reporter/adapter_seam_test.go`'s `TestAdapterSeam_SelfInstrumentingRuntimeNoDuplicateSpans` is the ADAPT-01 criterion-3 proof: a stub self-instrumenting runtime and a reporter run sharing one `tracetest.InMemoryExporter` produce exactly 1 span (the stub's own), using generic `otelai.ExtractRemoteParent` env-carrier extraction only — no LangGraph-specific span-shape assumption.
- `internal/reporter/tracesynth.go`'s package doc and the D-07 inline comment now name the file as the anthropic-CLI runtime's adapter and point at `pkg/dispatch.SelfInstruments` + the reporter's skip guard as the routing seam — comment-only, zero logic change (verified via diff).

## Task Commits

Each task was committed atomically:

1. **Task 1: Parse --skip-message-spans into reporterConfig and guard synthesizeSpans as the D-05 sole skip point** - `10757a9` (feat)
2. **Task 2: Author the D-09 adapter-seam contract test and the D-08 doc contract on tracesynth.go** - `9f83245` (test)

_Note: both tasks were `tdd="true"`; Task 1's RED phase was a compile failure (missing `SkipMessageSpans` field) confirmed before implementation — GREEN and the commit landed together per this plan's commit granularity. Task 2's new test exercised already-implemented Task 1 behavior, so its own RED/GREEN cycle surfaced as a genuine assertion failure (span-count 0 vs want 1) caused by TracerProvider installation timing, fixed before commit._

## Files Created/Modified
- `cmd/tide-reporter/main.go` - `reporterConfig.SkipMessageSpans` field, `--skip-message-spans` flag registration in `parseFlags`, and the D-05 skip guard as `synthesizeSpans`'s first statement
- `cmd/tide-reporter/main_test.go` - `TestParseFlagsSkipMessageSpans` (present/absent subtests) and `TestRunTraceOnly_SkipsSynthesisWhenFlagSet`
- `cmd/tide-reporter/adapter_seam_test.go` (new) - `TestAdapterSeam_SelfInstrumentingRuntimeNoDuplicateSpans`, the D-09 contract test
- `internal/reporter/tracesynth.go` - D-08 doc-contract additions to the package doc and the D-07 inline comment (comment-only)

## Decisions Made
- **TracerProvider installation timing in the contract test:** `installStubTracerProvider`'s `newTracerProvider` seam only calls `otel.SetTracerProvider` lazily, inside `runWithClient`. The stub runtime's own span is created *before* `runWithClient` runs (by design — the plan requires "emit the stub's span FIRST, then run the reporter, so a duplicate would be unambiguous"), so the initial run produced 0 spans instead of 1 — the stub span was recorded against the previous (no-op) global provider. Fixed by explicitly calling `otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))` immediately after `installStubTracerProvider`, before creating the stub span. `installStubTracerProvider` still captures the true original provider for cleanup (called first), so test isolation is preserved; `runWithClient`'s later seam invocation installs a second provider instance synced to the same exporter, which is harmless.
- **"LangGraph"-free wording:** the plan's acceptance criteria require `grep -i langgraph adapter_seam_test.go` to return zero hits, including in comments. Two explanatory comments originally said "no LangGraph-specific span-shape assumption" (matching the plan's own action-block prose); reworded to "vendor-specific" to satisfy the literal grep gate without changing test semantics.

## Deviations from Plan

None - plan executed exactly as written. The TracerProvider-timing fix and the LangGraph-wording fix were both within Task 2's own acceptance criteria (test must pass; grep must return zero hits) — not scope changes, just getting the specified test green and matching the specified acceptance gate exactly.

## Issues Encountered
- `go build ./...` fails repo-wide on `cmd/tide-demo-init/main.go:112` (`//go:embed all:fixture`, pattern: no matching files found) — this is a pre-existing, unrelated issue: `cmd/tide-demo-init/fixture/` is gitignored and materialized via `go:generate` from `examples/tide-demo-fixture/` (confirmed via `.gitignore` + the file's own doc comment), not part of this plan's `files_modified`. Worked around by scoping all build/test verification to the plan's actual packages (`./cmd/tide-reporter/...`, `./internal/reporter/...`, `./internal/controller/...`, `./pkg/dispatch/...`), which all build and test clean. Logged here per the scope-boundary rule; not fixed.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

ADAPT-01 criteria 2 and 3 are satisfied on the reporter side. This plan is independent of Plan 01 (manager-side `pkg/dispatch.SelfInstruments` + the 5 controller call sites that compute and thread the flag) — zero file overlap, and the shared contract (`--skip-message-spans` flag string) is locked identically in both plan texts. Once Plan 01 lands, the two sides compose: the manager will pass `--skip-message-spans` on the reporter Job Args for any vendor `SelfInstruments` reports true (currently none — every vendor still resolves false, D-03 default-safe), and this plan's guard will honor it. No blockers for Phase 46 (observability enrichment) or the future LangGraph specialist beachhead, which is the actual consumer of a `SelfInstruments`-true vendor.

---
*Phase: 45-runtime-neutral-adapter-seam*
*Completed: 2026-07-17*
