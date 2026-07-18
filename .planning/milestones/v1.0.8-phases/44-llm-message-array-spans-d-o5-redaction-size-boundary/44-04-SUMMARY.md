---
phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
plan: 04
subsystem: infra
tags: [opentelemetry, tide-reporter, tracer-provider, shutdown-flush, trace-only]

# Dependency graph
requires:
  - phase: 44-03
    provides: "internal/reporter/tracesynth.go — ReconstructConversation + EmitSpans"
provides:
  - "tide-reporter --trace-only flag (MSG-01) with task-uid-only validation and unconditional exit-0"
  - "newTracerProvider seam + deferred bounded (5s) Shutdown inside runWithClient, covering every exit path (TRACE-03/D-12)"
  - "synthesizeSpans best-effort helper wired into both trace-only and combined modes, never affecting the materialization exit code (D-10)"
affects: [phase-45-runtime-neutral-adapter, phase-46-span-enrichment]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "TracerProvider lifecycle transplanted from cmd/manager/main.go into a one-shot binary's runWithClient seam (one level below os.Exit), not main() (Pitfall 5)"
    - "Package-level constructor seam (var newTracerProvider = otelinit.NewTracerProvider) for test injection, mirroring buildFakeClient's minimalism"

key-files:
  created: []
  modified:
    - cmd/tide-reporter/main.go
    - cmd/tide-reporter/main_test.go

key-decisions:
  - "TP init runs as runWithClient's absolute first action (before flag validation) so every exit path, including the pre-existing invariant-violation returns, flushes the batch span processor"
  - "otel init failure never fails the reporter run — falls back to a no-op ShutdownFunc and continues (D-10), unlike cmd/manager which os.Exit(1)s on the same error"
  - "synthesizeSpans call sits before the K8s client build in combined mode so failed planner Jobs with no/partial out.json still emit conversation spans (D-05)"
  - "Test shutdown-flush proof uses sdktrace.WithSyncer (not WithBatcher) deliberately — it proves Shutdown is invoked with a bounded ctx on every path; batch-drain behavior itself is covered by 44-03's TestEmitSpans_BatchAggregateUnderCeiling"

patterns-established:
  - "Best-effort span-synth helper (synthesizeSpans) callable from multiple entry branches, swallowing all errors to stderr, never returning a value that could influence an exit code"

requirements-completed: [TRACE-03, MSG-01]

# Metrics
duration: 8min
completed: 2026-07-16
---

# Phase 44 Plan 04: tide-reporter trace-only mode + TracerProvider lifecycle Summary

**`tide-reporter` gains a `--trace-only` flag and its first `otelinit.NewTracerProvider` call site, with a deferred 5-second-bounded `Shutdown` proven (by table-driven test) to fire on every `runWithClient` exit path, and a best-effort `synthesizeSpans` step wired into both modes ahead of any point that can fail.**

## Performance

- **Duration:** ~8 min
- **Completed:** 2026-07-16
- **Tasks:** 2/2
- **Files modified:** 2

## Accomplishments

- `--trace-only` flag (MSG-01) registered alongside `reporterConfig.TraceOnly`; a trace-only run validates only `--task-uid`, needs no K8s client, and exits 0 unconditionally regardless of synth/export outcome
- `newTracerProvider` package-level seam over `otelinit.NewTracerProvider`, called as `runWithClient`'s literal first action with a deferred `context.WithTimeout(..., 5*time.Second)` bounded `Shutdown` — one level below `main()`'s `os.Exit`, covering every existing and new exit path (TRACE-03)
- `synthesizeSpans` best-effort helper: derives `events.jsonl`/`in.json` paths, extracts the remote parent via `otelai.ExtractRemoteParent(ctx, cfg.TraceParent)`, calls `reporter.ReconstructConversation` + `reporter.EmitSpans` on `otel.Tracer("tide.reporter")`, and never returns anything that could change the caller's exit code
- Combined-mode synth call placed after flag validation but strictly before the K8s client build, so a failed planner Job with no/partial `out.json` still gets its conversation spans (D-05) and synth failures never alter materialization's exit code (D-10)
- 4 new test functions (`TestRunWithClient_ShutdownOnEveryExitPath` table-driven over 4 exit paths, `TestRunTraceOnly_EmitsSpans`, `TestRunTraceOnly_MissingEventsStillExitsZero`, `TestRunCombined_SynthFailureDoesNotChangeExit`) prove the shutdown discipline and the D-10 exit-code posture in both modes

## Task Commits

1. **Task 1: --trace-only flag + TracerProvider lifecycle + synth step in both modes** - `472a1ec` (feat)
2. **Task 2: Shutdown-on-every-exit-path + trace-only mode tests** - `cec2c44` (test)

_Note: Task 1 was `tdd="true"` per frontmatter but is additive wiring onto an existing tested seam with no isolated red/green cycle available (extending `runWithClient`, not introducing a fresh testable unit) — Task 2 supplies the full regression proof for both tasks' behavior in one pass, verified together via `go test ./cmd/tide-reporter/ -count=1`._

## Files Created/Modified

- `cmd/tide-reporter/main.go` - `--trace-only` flag, `reporterConfig.TraceOnly`, `newTracerProvider` seam, TP init + deferred bounded Shutdown as `runWithClient`'s first action, trace-only branch, `synthesizeSpans` helper wired into both modes, updated package doc comment
- `cmd/tide-reporter/main_test.go` - `shutdownRecorder` + `installStubTracerProvider` test helpers, `writeTraceOnlyFixture` fixture builder, 4 new test functions

## Decisions Made

- otel init failure inside `runWithClient` degrades to a no-op `ShutdownFunc` and continues rather than returning `exitGenericFail` — required by D-10 ("otel init failure must not fail materialization") and explicitly different from `cmd/manager/main.go`'s `os.Exit(1)` on the same error, since the manager is a long-running process that can afford to fail loudly at boot while the reporter is a one-shot Job whose primary job (materialization or trace-only exit) must not be gated on tracing infra.
- The Task 2 shutdown-flush test intentionally uses `sdktrace.WithSyncer`, not `WithBatcher`, per the plan's explicit rationale: the seam test's job is to prove `Shutdown` is invoked with a bounded context on every path (the discipline TRACE-03 requires), while the SDK's own contract guarantees the batch queue drains on `Shutdown` — that batch-drain behavior is already covered by 44-03's `TestEmitSpans_BatchAggregateUnderCeiling`.
- `synthesizeSpans` artifact-path attribute is built via string concatenation (`"envelopes/" + cfg.TaskUID + "/events.jsonl"`) rather than `filepath.Join`, matching the plan's literal specification and the workspace-relative attribute values already used in 44-03's tests/fixtures — this value is an OpenInference attribute string, not a filesystem path evaluated on this host, so no OS-path-separator ambiguity applies.

## Deviations from Plan

None - plan executed exactly as written. Both tasks' acceptance criteria were verified directly:

- `grep -c 'fs.Bool("trace-only"' cmd/tide-reporter/main.go` → 1
- `grep -c 'var newTracerProvider = otelinit.NewTracerProvider' cmd/tide-reporter/main.go` → 1
- `grep -c '5\*time.Second' cmd/tide-reporter/main.go` → 1, and the containing `defer` is inside `runWithClient` (confirmed by line-order grep)
- `synthesizeSpans` call in the combined path appears before the "Build the K8s client" comment (confirmed by line-order grep)
- All 8 pre-existing tests plus 4 new tests pass unmodified/as specified

## Issues Encountered

`golangci-lint` is not installed in this execution environment (verified via `which golangci-lint` and a filesystem search — absent), so the plan's third verification line (`golangci-lint run ./cmd/tide-reporter/...` introduces 0 new issues) could not be executed directly. Substituted `go vet ./cmd/tide-reporter/...` (clean, no output) and `gofmt -l` on both modified files (clean, no output) as the closest available substitutes. A separate, pre-existing, unrelated build failure exists in `cmd/tide-demo-init/main.go` (`pattern all:fixture: no matching files found` — an embed directive over a missing/gitignored fixture directory, last touched in commit `25fce55`, untouched by this plan) surfaced only when running `go build ./...` across the whole repo; `go build ./cmd/tide-reporter/...` (the plan's actual verification target) is unaffected and passes cleanly. Out of scope per the executor's scope-boundary rule — not fixed, noted here for visibility.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `tide-reporter` now has a working trace-only invocation surface and a proven shutdown-on-every-exit-path discipline; Phase 44's remaining plans (manager-side D-05/D-06 spawn wiring, `pkg/otelai`/`internal/harness/redact` helpers) can build on this without touching `cmd/tide-reporter/main.go` again for the TP lifecycle itself.
- No blockers. The manager-side spawn gating (D-06: skip trace-only spawns when no OTLP endpoint configured) and the `BuildReporterJob` Args wiring for `--trace-only`/`--traceparent` remain open for a sibling/later plan in this phase — this plan's scope was strictly `cmd/tide-reporter/main.go` + its test file per the frontmatter `files_modified` list.

---
*Phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary*
*Completed: 2026-07-16*

## Self-Check: PASSED

- FOUND: cmd/tide-reporter/main.go
- FOUND: cmd/tide-reporter/main_test.go
- FOUND commit: 472a1ec
- FOUND commit: cec2c44
