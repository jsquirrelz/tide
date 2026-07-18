---
phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
plan: 03
subsystem: observability
tags: [opentelemetry, openinference, events-jsonl, redaction, size-boundary, reporter]

# Dependency graph
requires:
  - phase: 44-01
    provides: "redact.String(s string) — the MSG-02 non-streaming redaction pass; pkg/otelai.Message.ToolCalls/Contents + LLMSpanKind/TimingSynthetic/ParseDegraded marker helpers"
  - phase: 44-02
    provides: "OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 + OTEL_EXPORTER_OTLP_ENDPOINT reporter Job env plumbing this plan's batch-aggregate test proves against"
provides:
  - "internal/reporter/tracesynth.go — CallSpan, ReconstructConversation, EmitSpans: the events.jsonl -> LLM-span synthesizer (the phase's core deliverable)"
  - "internal/reporter/testdata/* — synthetic events.jsonl fixtures authored from the verified schema (never copied from the real dogfood corpus)"
affects: [44-04-reporter-binary-message-spans, 44-05-task-level-trace-only-spawn, 45-adapter-seam]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Composite package-doc pattern: tracesynth.go states its own import-safety contract (no controller-package back-edge) alongside materialize.go's, both living in package reporter"
    - "Tolerant-skip JSONL walk via common.ReadLines, mirroring stream_parser.go's ParseStream posture on the read side (D-11)"
    - "Redact-then-truncate as a single wrapper function (redactTruncate) — one call site enforces the D-09 ordering everywhere, rather than trusting every message-processing call site to get the order right independently"
    - "Whole-span budget degrades to role-only messages (not per-message truncation) when the SUM of a side's attribute bytes exceeds the cap — a different failure mode than the per-message floor"
    - "Real aggregate-batch risk (Pitfall 3) is proven via an inline recordingExporter test double capturing BatchSpanProcessor's actual ExportSpans call shape, not via a single-oversized-message test"

key-files:
  created:
    - internal/reporter/tracesynth.go
    - internal/reporter/tracesynth_test.go
    - internal/reporter/testdata/events_sample.jsonl
    - internal/reporter/testdata/events_truncated.jsonl
    - internal/reporter/testdata/in_planner.json
    - internal/reporter/testdata/in_executor.json
    - internal/reporter/testdata/children/task-01.json
  modified:
    - .planning/REQUIREMENTS.md

key-decisions:
  - "ReconstructConversation takes an explicit third workspaceRoot parameter (eventsPath, inJSONPath, workspaceRoot) rather than deriving the workspace root from eventsPath's own directory — the plan's action text explicitly permitted this ('pass the workspace root explicitly as a third param if cleaner'); deriving it from eventsPath's path structure would hard-code an assumption about how many directory levels separate events.jsonl from the workspace root, which doesn't hold for this plan's flat testdata/ fixture layout"
  - "in_executor.json's promptPath fixture value is 'children/task-01.json' rather than the illustrative 'envelopes/planner-uid/children/task-01.json' string shown in the plan's action-text prose — the prose was demonstrating the real production convention, not dictating literal fixture bytes; the plan's own files_modified list places the physical fixture directly at testdata/children/task-01.json (no envelopes/planner-uid/ nesting), so the promptPath value was set to resolve consistently against that declared physical path"
  - "D-09 (locked, non-negotiable) enforced via a single wrapper, redactTruncate, used as the sole call site in boundedMessageAttrs for every message Content, ToolCall.ArgumentsJSON, and reasoning Contents[].Text — centralizing the ordering in one function makes the 'redact before truncate, always' invariant impossible to violate at a new call site by accident"
  - "The whole-span-budget degrade (512 KiB) reuses otelai.ParseDegraded() rather than a new dedicated marker attribute — the plan's action text explicitly left this as 'ParseDegraded-style marker per the action's rule', and reusing the existing marker avoids adding a third tide.trace.* constant for what is semantically the same signal (this span's content is incomplete/degraded)"
  - "Batch-aggregate risk (Pitfall 3, the real risk per RESEARCH's fixture analysis) is verified by exercising the OTel SDK's own BatchSpanProcessor via OTEL_BSP_MAX_EXPORT_BATCH_SIZE, not by adding any custom chunking code to tracesynth.go — matches RESEARCH's explicit 'Don't Hand-Roll' guidance"

patterns-established:
  - "recordingExporter (inline sdktrace.SpanExporter test double capturing per-Export batch shape) — construct-only-what-the-test-needs, no shared test-double package, per PATTERNS.md's stated house style"

requirements-completed: [MSG-02, MSG-03]

# Metrics
duration: ~20min
completed: 2026-07-16
---

# Phase 44 Plan 03: LLM Message-Array Spans — events.jsonl Synthesizer Summary

**`internal/reporter/tracesynth.go`: `ReconstructConversation` turns a real-schema `events.jsonl` into one `CallSpan` per API call (seeded from `in.json`'s out-of-band prompt), and `EmitSpans` emits each as a redacted, size-bounded OpenInference LLM span — redact-before-truncate proven with a straddling-secret test, and the real aggregate-batch risk (not a single oversized span) proven with a 32-span `BatchSpanProcessor` test.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-16T22:52:12Z
- **Completed:** 2026-07-16T23:09:12Z
- **Tasks:** 2 (both `tdd="true"`, full RED→GREEN cycle each)
- **Files created:** 7
- **Files modified:** 1 (`.planning/REQUIREMENTS.md`)

## Accomplishments

- `ReconstructConversation(eventsPath, inJSONPath, workspaceRoot string) ([]CallSpan, error)` — walks `events.jsonl` (tolerant-skip via `common.ReadLines`), grouping per-content-block `assistant` events by `message.id` within one `message_start`..`message_stop` window into ONE aggregated `CallSpan.OutputMessages` entry (Pitfall 1); seeds conversation turn 0 from `in.json`'s `.prompt` or one `.promptPath` hop to a `children/task-NN.json`'s `.spec.prompt` (Pitfall 2); marks a call `Degraded` when the stream ends mid-call or a line fails to parse (D-05/D-11), never erroring on malformed input
- `EmitSpans(ctx, tracer, calls, artifactPath string) error` — one LLM-kind span per `CallSpan`: `openinference.span.kind=LLM`, `llm.provider="anthropic"` (D-07, deliberately hardcoded — this synthesizer parses the Anthropic CLI stream specifically), per-call pre-summed `TokenCount` (D-08), `ArtifactPath` on every span, `TimingSynthetic` always (no in-band absolute call timestamp exists), `ParseDegraded` when the call or either side degraded
- Triple size guard implemented and independently tested: 32 KiB per-message truncation floor with an explicit elided-byte-count marker (D-08 head+tail shape), 512 KiB whole-span budget that degrades a side to role-only messages, and the real aggregate-batch risk proven via an inline `recordingExporter` test double against `OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6` (Pitfall 3 — the risk is many modest spans in one export batch, not one oversized span)
- D-09 (redact-before-truncate, locked/non-negotiable) centralized in one wrapper (`redactTruncate`) so every message-processing call site inherits the correct ordering automatically; proven behaviorally with a secret deliberately positioned to straddle the 16 KiB truncation cut
- 5 synthetic fixtures authored from the verified `events.jsonl` schema (RESEARCH.md, never copied from the real dogfood corpus): a 3-call clean conversation with thinking/tool_use/text blocks and a planted fake secret, a truncated/malformed variant, and the `in.json`/`children/task-NN.json` prompt-seeding pair

## Task Commits

Both tasks followed the full RED → GREEN TDD cycle (no refactor commit needed — implementation was correct on first GREEN run for both):

1. **Task 1 RED: failing tests + fixtures for ReconstructConversation** - `734d124` (test)
2. **Task 1 GREEN: implement ReconstructConversation** - `c5d1a1b` (feat)
3. **Task 2 RED: failing tests for EmitSpans** - `612b86f` (test)
4. **Task 2 GREEN: implement EmitSpans redact-then-truncate + triple size guard** - `e38ed2d` (feat)

**Plan metadata:** (this commit, docs — includes `.planning/REQUIREMENTS.md`)

## Files Created/Modified

- `internal/reporter/tracesynth.go` - `Usage`/`CallSpan` types, the raw `events.jsonl` line shapes, `ReconstructConversation`, `truncateHeadTail`/`redactTruncate`/`boundedMessageAttrs`, `EmitSpans`
- `internal/reporter/tracesynth_test.go` - 5 `ReconstructConversation` tests + 7 `EmitSpans` tests (span shape, redaction, redact-before-truncate straddle proof, oversized-message truncation, whole-span-budget degrade, degraded marker, batch-aggregate-under-ceiling)
- `internal/reporter/testdata/events_sample.jsonl` - synthetic 3-call fixture (system init, thinking+tool_use+text blocks, two tool_result events including a planted fake Anthropic-key-shaped secret, terminal result line)
- `internal/reporter/testdata/events_truncated.jsonl` - same head through call 1, then a dangling `message_start` with no `message_stop` plus a trailing non-JSON garbage line
- `internal/reporter/testdata/in_planner.json` / `in_executor.json` - `EnvelopeIn`-shaped seed fixtures (direct `.prompt` vs `.promptPath` hop)
- `internal/reporter/testdata/children/task-01.json` - the `.spec.prompt` artifact `in_executor.json`'s `promptPath` resolves to
- `.planning/REQUIREMENTS.md` - MSG-02/MSG-03 marked complete (this plan is their last spanning plan, per 44-01's SUMMARY note); MSG-01 deliberately left Pending (see Deviations)

## Decisions Made

See `key-decisions` in frontmatter for the four implementation-shape decisions (workspaceRoot as an explicit third parameter, the `in_executor.json`/`children/task-01.json` fixture-path consistency choice, the single `redactTruncate` wrapper, and reusing `ParseDegraded` for the whole-span-budget degrade).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - blocking] Corrected a premature `requirements.mark-complete` invocation for MSG-01**
- **Found during:** final state-update step
- **Issue:** This plan's frontmatter lists `requirements: [MSG-01, MSG-02, MSG-03]`, and 44-01's SUMMARY had left a note that "the correct gating plan (44-03...) should mark these complete." Running `requirements.mark-complete MSG-01 MSG-02 MSG-03` against that note alone would have been premature: checking 44-04-PLAN.md and 44-05-PLAN.md's own frontmatter shows MSG-01 ALSO spans those two not-yet-executed plans (`requirements: [TRACE-03, MSG-01]` and `requirements: [MSG-01]` respectively) — MSG-01's formal text ("a trace-only reporter mode... closing the gap where the richest LLM conversation currently has no in-namespace consumer") describes the reporter binary's trace-only invocation mode and the Task-level spawn site, which are 44-04/44-05's scope, not this plan's (this plan builds the library the reporter binary will call). MSG-02 and MSG-03, by contrast, have no later spanning plan (44-03 is their last one) and are genuinely fully satisfied here.
- **Fix:** Reverted the `MSG-01` checkbox and traceability-table row back to unchecked/Pending in `.planning/REQUIREMENTS.md`, keeping `MSG-02`/`MSG-03` marked Complete.
- **Files modified:** `.planning/REQUIREMENTS.md`
- **Commit:** folded into this plan's final metadata commit (no separate task commit — caught during the state-update step, before any commit was made)

No other deviations — the plan's task actions (fixture shapes, reconstruction algorithm, size-boundary constants, span attributes) were implemented as specified, and both TDD cycles reached GREEN on the first implementation attempt with no debugging iterations needed.

## Issues Encountered

- Two documentation-comment occurrences of the literal string `internal/controller` in `tracesynth.go`'s file header tripped the acceptance criterion `grep -c 'internal/controller' internal/reporter/tracesynth.go` returning 2 instead of 0 (the criterion checks for the literal substring anywhere in the file, including prose, not just import statements). Reworded both comments ("no back-edge into the controller package" / "the controller package's span_emission.go") to preserve the same meaning without the literal path substring.
- `golangci-lint` (built fresh via `make golangci-lint`, not pre-installed) flagged 3 issues on first run: an unchecked `f.Close()` on the read-only `events.jsonl` file (fixed via the codebase's existing `defer func() { _ = f.Close() }()` house pattern, copied from `internal/subagent/anthropic/subagent.go:316`), and two `for i := 0; i < N; i++` loops in the test file flagged by `modernize`/`prealloc` (converted to `for range N` with pre-sized `make([]T, 0, N)` slices). All fixed before declaring Task 2 complete; `golangci-lint run ./internal/reporter/...` now reports 0 issues.
- `go build ./...` (whole repo) fails on a pre-existing, unrelated issue: `cmd/tide-demo-init/main.go:112: pattern all:fixture: no matching files found` — confirmed via `git log --oneline -3 -- cmd/tide-demo-init/main.go` that the file was last touched in commits unrelated to Phase 44 (15/lint-fixes/05-12), and 44-01's SUMMARY independently documented the same pre-existing issue. Out of scope per the deviation rules' scope boundary; this plan's actual build/test surface (`go build ./internal/reporter/...`, `go test ./internal/reporter/... -count=1`) is clean.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `internal/reporter/tracesynth.go`'s `ReconstructConversation`/`EmitSpans` are complete, unit-tested (12 tests, all green), and ready for 44-04 (`cmd/tide-reporter/main.go` trace-only mode + `TracerProvider` wiring) and 44-05 (the Task-level completion handler that spawns the trace-only reporter Job) to call into.
- MSG-01 is deliberately left Pending in `.planning/REQUIREMENTS.md` — 44-04 and 44-05 are its remaining spanning plans; whichever of them lands last should mark it complete.
- No blockers for 44-04/44-05.

## Self-Check: PASSED

- FOUND: internal/reporter/tracesynth.go
- FOUND: internal/reporter/tracesynth_test.go
- FOUND: internal/reporter/testdata/events_sample.jsonl
- FOUND: internal/reporter/testdata/events_truncated.jsonl
- FOUND: internal/reporter/testdata/in_planner.json
- FOUND: internal/reporter/testdata/in_executor.json
- FOUND: internal/reporter/testdata/children/task-01.json
- FOUND commit: 734d124 (test)
- FOUND commit: c5d1a1b (feat)
- FOUND commit: 612b86f (test)
- FOUND commit: e38ed2d (feat)

## TDD Gate Compliance

Both tasks show the full RED (`test(...)`) → GREEN (`feat(...)`) commit sequence in `git log`, with RED confirmed via an actual build failure (`undefined: ReconstructConversation` / `undefined: EmitSpans`) before any implementation existed. No REFACTOR commit was needed for either task — both reached GREEN cleanly on the first implementation pass.

---
*Phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary*
*Completed: 2026-07-16*
