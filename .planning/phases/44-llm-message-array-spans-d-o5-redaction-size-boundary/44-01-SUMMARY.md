---
phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
plan: 01
subsystem: observability
tags: [opentelemetry, otelai, redaction, openinference, semconv]

# Dependency graph
requires:
  - phase: 42-trace-context-foundation-planner-level-span-emission
    provides: pkg/otelai attribute helpers (LLMInputMessages/LLMOutputMessages/TokenCount/ArtifactPath), tracecontext.go, the ATTR-03/D-05 semconv-resolution house style, TestNoPayloadHelperOnPublicSurface/TestKeysUseSemconvModule guard tests
  - phase: 43-task-level-parity-trace-context-propagation
    provides: Task-level span parity and traceparent propagation this phase's synthesizer will nest under (consumed by later 44-0x plans, not this one)
provides:
  - "redact.String(s string) string — non-streaming MSG-02 secret-redaction pass reusing SecretPatterns"
  - "pkg/otelai Message.ToolCalls/Contents extension — spec-native tool-call and reasoning-block encoding (D-03)"
  - "pkg/otelai.LLMSpanKind()/TimingSynthetic()/ParseDegraded() marker helpers"
  - "pkg/otelai/doc.go D-O5 contract evolved to bounded-inline + ArtifactPath co-attribute"
affects: [44-02, 44-03, 44-04, 44-05, tracesynth.go, cmd/tide-reporter]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Non-streaming redaction helper beside a streaming one, sharing the same package-level pattern denylist (redact.String beside RedactingWriter)"
    - "Spec-native attribute encoding via module indexers where they exist (LLM{Input,Output}MessageToolCallKey), constant-composed keys where they don't (message.contents family)"
    - "Marker-attribute convention (EnvelopeDegraded-style single Bool attribute) extended to two new tide.* namespace entries"

key-files:
  created: []
  modified:
    - internal/harness/redact/redact.go
    - internal/harness/redact/redact_test.go
    - pkg/otelai/attrs.go
    - pkg/otelai/attrs_test.go
    - pkg/otelai/doc.go

key-decisions:
  - "D-09 (locked, non-negotiable): redact.String's doc comment mandates callers redact BEFORE truncating — a truncation cut can split a secret so the pattern no longer matches"
  - "D-03 resolved YES: openinference-semantic-conventions v0.1.1 carries full tool-call and message.contents/reasoning key families, confirmed via go doc against the vendored module — spec-native encoding, no stringified fallback needed"
  - "message.contents keys have no public module indexer, so they are composed via fmt.Sprintf over semconv constants (satisfies TestKeysUseSemconvModule, which rejects raw literals, not constant compositions)"
  - "D-O5 contract evolved from 'always defer to ArtifactPath' to 'bounded-inline (redact-then-truncate) + ArtifactPath co-attribute' — guard test forbidden-name list is unchanged, only the doc contract and its enforcement comment evolved"

patterns-established:
  - "Marker attributes (TimingSynthetic/ParseDegraded) omit restating their key's literal string in the function doc comment when the const declaration above already states it — keeps grep-based guard/citation checks precise"

requirements-completed: [MSG-02, MSG-03]

# Metrics
duration: 33min
completed: 2026-07-16
---

# Phase 44 Plan 01: Redaction + otelai Tool-Call/Reasoning Encoding Foundation Summary

**Non-streaming `redact.String` (MSG-02) plus `pkg/otelai` spec-native tool-call/reasoning-block encoding, an `LLMSpanKind` helper, two new `tide.trace.*` markers, and the deliberate D-O5 bounded-inline + ArtifactPath co-attribute doc evolution (MSG-03) — the two foundation surfaces Plan 44-03's synthesizer will consume.**

## Performance

- **Duration:** 33 min
- **Started:** 2026-07-16T22:11:00Z
- **Completed:** 2026-07-16T22:44:00Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments
- `redact.String(s string) string` added beside `RedactingWriter`, reusing the exact `SecretPatterns` denylist against a fully-materialized string; documented D-09 caller contract (redact before truncate)
- `pkg/otelai.Message` extended with optional `ToolCalls []ToolCall` / `Contents []MessageContent` fields; `flattenMessages` emits spec-native `message.tool_calls.*` (via the module's own indexers) and `message.contents.*` (constant-composed) keys when populated, with byte-identical legacy 2-key-per-message output when nil
- `LLMSpanKind()`, `TimingSynthetic()`, `ParseDegraded()` helpers added, backed by two new `tide.trace.*` marker constants
- `pkg/otelai/doc.go` evolved: public-surface enumeration now 11 helpers; D-O5 section rewritten from "prefer ArtifactPath" to the bounded-inline (redact-then-truncate) + ArtifactPath co-attribute contract
- Both guard tests (`TestNoPayloadHelperOnPublicSurface`, `TestKeysUseSemconvModule`) verified passing unmodified in their assertions — only doc comments extended

## Task Commits

1. **Task 1: Add redact.String — the non-streaming MSG-02 pass** - `47ae287` (feat)
2. **Task 2: pkg/otelai — tool-call/reasoning encoding, LLMSpanKind, markers, D-O5 doc evolution** - `c61fd7a` (feat)

**Plan metadata:** (this commit, docs)

## Files Created/Modified
- `internal/harness/redact/redact.go` - added `String(s string) string`, doc-commented with the D-09 caller contract
- `internal/harness/redact/redact_test.go` - added `TestString` reusing the exact `TestRedactingWriter` fixtures plus an empty-string case
- `pkg/otelai/attrs.go` - `ToolCall`/`MessageContent` types, `Message.ToolCalls`/`Message.Contents` fields, extended `flattenMessages` + new `messageContentKey` composer, `LLMSpanKind`/`TimingSynthetic`/`ParseDegraded` helpers, two new `tide.trace.*` consts
- `pkg/otelai/attrs_test.go` - exact-key-string tests for tool calls and reasoning content, legacy-shape backward-compat test, tests for the three new helpers, extended `TestEmptyInputsNoPanic`, updated `TestNoPayloadHelperOnPublicSurface`'s doc comment (forbidden list unchanged)
- `pkg/otelai/doc.go` - public-surface enumeration (8 → 11 helpers) and D-O5 section rewrite

## Decisions Made
- Verified `LLMOutputMessageToolCallKey`/`LLMInputMessageToolCallKey`/`SpanKindLLM`/`MessageContents` family directly against the vendored module source (`go doc` + reading `indexers.go`/`attributes.go`/`enums.go`) before coding, per the plan's `read_first` instruction — all signatures matched the plan's cited interface exactly, no surprises.
- Kept `TimingSynthetic()`/`ParseDegraded()` function doc comments from restating their marker key's literal string a second time (the const declaration above already states it) — this was an explicit self-correction after the first draft caused the `tide.trace.timing_synthetic|tide.trace.parse_degraded` grep-count acceptance criterion to read 4 instead of the plan's expected 2.

## Deviations from Plan

None - plan executed exactly as written. The one self-correction (marker doc-comment wording, see Decisions above) was made before committing and is not a deviation from the plan's action text — it is exactly what the plan's acceptance criteria specified.

## Issues Encountered
- `cmd/tide-demo-init/main.go` fails `go build ./...` on `//go:embed all:fixture` (missing fixture directory) — confirmed via `git log` that this file was not touched by this plan or any recent commit on this branch; pre-existing, unrelated to `pkg/otelai`/`internal/harness/redact`, and out of scope per the deviation rules' scope boundary. The plan's actual required build check (`go build ./pkg/otelai/... ./internal/controller/...`) passed cleanly.
- `golangci-lint` was not pre-installed in this environment; built via `make golangci-lint` (downloads + custom-plugin build, ~1 min) before running `golangci-lint run ./internal/harness/redact/... ./pkg/otelai/...` — 0 issues.
- **REQUIREMENTS.md left untouched (deliberately).** This plan's frontmatter lists `requirements: [MSG-02, MSG-03]`, but both IDs also appear in later plans' frontmatter (`MSG-02` again in 44-03; `MSG-03` in 44-02 and 44-03) — the same multi-plan-spanning-requirement pattern already established in Phase 43 (PROP-01/PROP-02/TRACE-01/TRACE-02 each spanned 2-3 plans, closed only by the final integrating plan). Neither requirement's full text is satisfied yet: MSG-02 requires `LLMInputMessages`/`LLMOutputMessages` to be populated from real `events.jsonl` content through the redaction pass (the actual call site lands in 44-03); MSG-03 requires the per-message truncation + `ArtifactPath` co-attribute wiring (44-02/44-03). This plan initially ran `gsd-sdk query requirements.mark-complete MSG-02 MSG-03`, then reverted `.planning/REQUIREMENTS.md` via targeted `git checkout` after recognizing the premature-completion risk — the correct gating plan (44-03, which lists all of MSG-01/MSG-02/MSG-03) should mark these complete.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Both foundation surfaces this plan's objective named are in place and unit-tested: `redact.String` for Plan 44-0x's `tracesynth.go` redaction pass, and `pkg/otelai`'s extended `Message`/`LLMSpanKind`/`TimingSynthetic`/`ParseDegraded` for the synthesizer's span-emission call sites.
- No blockers. `internal/reporter/tracesynth.go` (not yet created) can now import `pkg/otelai` and `internal/harness/redact` and consume every helper this plan added without further `pkg/otelai` changes, per the plan's stated purpose ("interface-first — Plan 44-03's synthesizer consumes these helpers").

## Self-Check: PASSED

All 5 modified files confirmed present on disk; both task commits (`47ae287`, `c61fd7a`) confirmed present in `git log --oneline --all`.

---
*Phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary*
*Completed: 2026-07-16*
