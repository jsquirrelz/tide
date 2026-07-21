---
phase: 50-execution-loop-hardening-loop-native-observability
plan: 02
subsystem: observability
tags: [go, otel, opentelemetry, attribute-helpers, tdd]

# Dependency graph
requires:
  - phase: 50-execution-loop-hardening-loop-native-observability
    plan: 01
    provides: "TerminalReason enum + RunEvidence struct this plan's loop.exit_reason/loop.candidate_version doc comments reference conceptually (no direct code dependency — this plan only defines the otelai vocabulary)"
provides:
  - "pkg/otelai.LoopAttributes/LoopRunID/LoopIteration/EvaluationAttributes/HumanIntervention helpers — the OBS-01 span-attribute vocabulary"
  - "9 loop.*/evaluation.*/human_intervention consts with a documented not-tide.-prefixed rationale and per-key population contract"
  - "LoopKindExecution exported const"
affects: [50-06-agent-span-stamping, 50-07-llm-span-stamping, 51-langgraph-evaluator]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "TIDE-custom const block mirroring the existing tide.* idiom (pkg/otelai/attrs.go:92) but deliberately NOT tide.-prefixed — a documented, reviewed deviation for a cross-vendor loop-native convention"
    - "Absent-when-empty conditional attribute append (mirrors the sessionID/metadataJSON/tags triple in EmitSpans/synthesizePlannerSpan) applied to LoopAttributes' three optional fields"

key-files:
  created: []
  modified:
    - pkg/otelai/attrs.go
    - pkg/otelai/attrs_test.go

key-decisions:
  - "LoopAttributes' 6-attribute order is kind/run_id/iteration (always) then parent_run_id/candidate_version/exit_reason (conditional) — matches the plan action text's literal ordering, not the const-declaration order, so the 3-entry omit case is a clean prefix of the 6-entry full case."
  - "Doc comments reference key names as bare text (never double-quoted) to avoid tripping the plan's own grep -c '\"loop\\.' pkg/otelai/attrs.go acceptance check, which counts only the 6 const-block string literals."

requirements-completed: [OBS-01]

# Metrics
duration: 1min
completed: 2026-07-19
---

# Phase 50 Plan 02: Loop-Native Span Attribute Vocabulary Summary

**Defines the 9 OBS-01 loop-native span attribute keys (`loop.*`/`evaluation.*`/`human_intervention`) as `pkg/otelai` consts + typed helper functions — vocabulary only, no stamping sites wired.**

## Performance

- **Duration:** 1 min (commit-to-commit)
- **Started:** 2026-07-19T00:10:44-04:00 (first task commit)
- **Completed:** 2026-07-19T00:11:44-04:00
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- Added a new TIDE-custom const block to `pkg/otelai/attrs.go` defining all 9 keys (`loop.kind`, `loop.run_id`, `loop.parent_run_id`, `loop.iteration`, `loop.candidate_version`, `loop.exit_reason`, `evaluation.result`, `evaluation.version`, `human_intervention`), with a doc comment that explicitly states the deviation from the file's "only `tide.*` may be hand-rolled" convention and documents Phase 51 as the sole populator of `evaluation.*`/`human_intervention`.
- Added `LoopKindExecution = "execution"` — the sole `loop.kind` value Phase 50 emits.
- Added 5 helpers mirroring the file's existing shapes: `LoopAttributes` (positional-args composite, mirrors `AgentInvocation`/`FailureDetail`, with absent-when-empty optionals), `LoopRunID`/`LoopIteration` (single-attribute correlating subset for per-call LLM spans, mirrors `SessionID`), `EvaluationAttributes` (two-attribute pair, mirrors `LLMIdentity`'s always+conditional shape but both fields required here), `HumanIntervention` (bare-bool marker, mirrors `EnvelopeDegraded`).
- Confirmed `TestKeysUseSemconvModule` (the ATTR-03 source-grep guard) passes unmodified — its forbidden-prefix regex (`llm.`/`openinference.`/`gen_ai.`/`agent.`) does not block `loop.`/`evaluation.` prefixes, exactly as RESEARCH.md's Pitfall 3 predicted.
- Added 6 new exact-equality tests (`TestLoopAttributes_FullyPopulated`, `TestLoopAttributes_OmitsEmptyOptionals`, `TestLoopRunID`, `TestLoopIteration`, `TestEvaluationAttributes`, `TestHumanIntervention`) mirroring `TestSessionID`'s `reflect.DeepEqual` shape; `TestLoopAttributes_OmitsEmptyOptionals` additionally asserts no returned `KeyValue` carries a fabricated empty-string value.

## Task Commits

1. **Task 1: loop.*/evaluation.*/human_intervention consts + helper functions** - `551f23ab` (feat) — const block + `LoopKindExecution` + `LoopAttributes`/`LoopRunID`/`LoopIteration`/`EvaluationAttributes`/`HumanIntervention` in `pkg/otelai/attrs.go`; `go build ./pkg/otelai/` and `TestKeysUseSemconvModule` verified green before commit.
2. **Task 2: Exact-equality tests for every new helper** - `a61eb501` (test) — 6 new tests in `pkg/otelai/attrs_test.go`; full `go test ./pkg/otelai/...` verified green (29 top-level tests, including `TestKeysUseSemconvModule` unmodified).

**Plan metadata:** pending (this SUMMARY's own commit)

## Files Created/Modified
- `pkg/otelai/attrs.go` - new const block (9 keys) + `LoopKindExecution` + `LoopAttributes`/`LoopRunID`/`LoopIteration`/`EvaluationAttributes`/`HumanIntervention` helpers
- `pkg/otelai/attrs_test.go` - 6 new exact-equality tests for the new helpers

## Decisions Made
- Kept `LoopAttributes`' argument order (`kind, runID, parentRunID string, iteration int, candidateVersion, exitReason string`) matching the plan's exact signature, but the *returned attribute order* follows the plan's action-text prose (kind/run_id/iteration always-three, then the three conditionals) rather than the const-declaration order — this makes the omit-case output a literal prefix of the full-case output, which is easier to reason about at call sites and in the tests.
- All doc-comment prose referencing `loop.`-prefixed key names avoids wrapping them in double quotes, so the plan's own `grep -c '"loop\.' pkg/otelai/attrs.go` acceptance check (which must return exactly 6) counts only the const-block literals, not comment prose.
- No stamping sites were touched — `grep -rn '"loop\.\|"evaluation\.\|"human_intervention"' --include="*.go" internal/ cmd/ | grep -v _test` returns 0 hits, confirming the vocabulary-only scope fence held (stamping is Plans 50-06/50-07).

## Deviations from Plan

None — plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Plans 50-06 (AGENT span) and 50-07 (LLM-span subset) can now call `otelai.LoopAttributes`/`otelai.LoopRunID`/`otelai.LoopIteration` directly — no string literals needed at either stamping site.
- Phase 51's LangGraph evaluator can call `otelai.EvaluationAttributes`/`otelai.HumanIntervention` using the same literal key strings TIDE defines here, satisfying the cross-vendor convention D-05 requires.
- Scope fence confirmed intact: `evaluation.result`/`evaluation.version`/`human_intervention` are defined but have zero non-test callers; no producer was wired.

---
*Phase: 50-execution-loop-hardening-loop-native-observability*
*Completed: 2026-07-19*

## Self-Check: PASSED

All created/modified files and task commit hashes verified present on disk / in `git log --oneline --all`.
