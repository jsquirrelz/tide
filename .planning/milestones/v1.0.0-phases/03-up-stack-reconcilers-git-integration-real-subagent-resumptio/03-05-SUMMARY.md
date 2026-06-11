---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 05
subsystem: subagent
tags: [anthropic, claude-cli, stream-json, prompt-templates, jsonl, go-embed, provider-firewall]

requires:
  - phase: 02
    provides: pkg/dispatch.Subagent interface + EnvelopeIn/EnvelopeOut shape + credproxy signed-token contract
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: Wave 1 (Plan 03-01) extends pkg/dispatch.Usage with CacheReadTokens + CacheCreationTokens fields consumed here
provides:
  - internal/subagent/common — provider-agnostic JSONL line reader (16MB budget) + go:embed prompt template loader + four v1 templates
  - internal/subagent/anthropic — pkg/dispatch.Subagent implementation that exec's `claude -p --bare --output-format stream-json`
  - Stream-json parser mapping snake_case (input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens) → camelCase pkg/dispatch.Usage per D-C5
  - Anthropic provider sentinel + Q3 params allow-list (temperature, thinking_budget, top_p, top_k) fail-fast at Run() entry
  - Testable execFunc seam (exec.CommandContext indirection) so unit tests run without claude CLI on PATH
affects:
  - 03-06 (claude harness wiring will consume Anthropic.Run via cmd/claude-subagent)
  - 03-07 (Dockerfile + image shim builds the per-Pod runtime around this package)
  - Phase 4 OpenInference parsing (reads /workspace/envelopes/{TaskUID}/events.jsonl audit log this plan writes)

tech-stack:
  added:
    - embed (stdlib go:embed for compiled-in prompt templates — anti-pattern guardrail vs vendoring GSD Markdown)
    - text/template (stdlib — prompt-template rendering with EnvelopeIn as exec context)
    - bufio.Scanner with 16MB buffer ceiling (JSONL line reading per RESEARCH Pattern 5)
    - os/exec.CommandContext (CLI shell-out, NOT Anthropic Go SDK — HARN-06 decision)
  patterns:
    - Provider firewall layering — internal/subagent/common/ is CRD-agnostic + vendor-neutral; internal/subagent/anthropic/ is the only Anthropic-aware site, enforced by tools/analyzers/providerfirewall
    - Test seam via function field — execFunc on *Anthropic lets tests inject `bash -c 'cat fixture.jsonl'` without requiring claude CLI
    - Defensive parse — non-JSON lines tolerated and teed to events.jsonl audit log for Phase 4 forensic use; only structured result event drives Usage/Result extraction
    - Dispatch vs task-level error distinction — exec.Wait non-zero surfaces via EnvelopeOut.ExitCode + Reason (return nil err); I/O setup failures return non-nil err per pkg/dispatch godoc

key-files:
  created:
    - internal/subagent/common/stream_reader.go (50 LOC — ReadLines with 16MB budget)
    - internal/subagent/common/stream_reader_test.go (112 LOC — 5 tests)
    - internal/subagent/common/prompt_templates.go (49 LOC — go:embed loader)
    - internal/subagent/common/prompt_templates_test.go (118 LOC — 3 tests incl. table-driven)
    - internal/subagent/common/templates/milestone_planner.tmpl
    - internal/subagent/common/templates/phase_planner.tmpl
    - internal/subagent/common/templates/plan_planner.tmpl
    - internal/subagent/common/templates/task_executor.tmpl
    - internal/subagent/anthropic/stream_parser.go (100 LOC — ParseStream → Usage + Result)
    - internal/subagent/anthropic/stream_parser_test.go (109 LOC — 4 tests)
    - internal/subagent/anthropic/subagent.go (240 LOC — Subagent.Run impl)
    - internal/subagent/anthropic/subagent_test.go (237 LOC — 5 tests)
  modified: []

key-decisions:
  - "Per HARN-06: shell out to `claude` CLI instead of importing github.com/anthropics/anthropic-sdk-go. The CLI bundles the agent loop, hooks, MCP, skills, and bash/file tools we would otherwise have to re-implement in Go. Phase 3 does not depend on the Anthropic Go SDK module."
  - "Per RESEARCH §Critical new finding: --bare flag is REQUIRED on every claude invocation. It skips auto-discovery of the host claude config dir, .mcp.json, hooks, skills, plugins, and any CLAUDE-doc auto-memory — the per-Pod runtime is hermetic by construction. This aligns with the CLAUDE.md anti-pattern 'never mount host ~/.claude/'."
  - "Per Q3 RESOLVED (03-RESEARCH line 933): Provider.Params keys are reject-unknown at startup. Allow-list is {temperature, thinking_budget, top_p, top_k}. Fail-fast catches typos at apply time rather than letting them silently disappear into a passthrough."
  - "Per D-C5: stream-json snake_case → pkg/dispatch.Usage camelCase mapping drops the 'Input' infix on cache fields (cache_read_input_tokens → CacheReadTokens, not CacheReadInputTokens). Phase 2's Usage already separates input/output via dedicated fields, so qualifying cache counters with 'Input' would be redundant."
  - "Testable seam: execFunc field on *Anthropic. New() defaults it to exec.CommandContext; tests override with a fixture-serving fake. Production callers do not touch execFunc — it's behind the struct, not in Options."
  - "Dispatch-level vs task-level error split (pkg/dispatch godoc contract): exec.Wait non-zero returns (EnvelopeOut{ExitCode: N, Reason: stderr}, nil) — task failed but dispatch succeeded. I/O setup errors (mkdir events dir, exec.Start) return non-nil err — dispatch itself failed."

patterns-established:
  - "Pattern: provider-agnostic common package — JSONL line reading + prompt template loading live in internal/subagent/common (no CRD types, no vendor names). The asymmetry vs internal/subagent/anthropic enforces D-C1 layering at the package boundary, not just at the type boundary."
  - "Pattern: go:embed prompt templates — //go:embed templates/*.tmpl + template.ParseFS gives a single LoadPromptTemplate(role, level) entrypoint that ships prompt content compiled into the binary. Zero runtime filesystem dependency on prompt files; no ConfigMap mount needed."
  - "Pattern: defensive stream parsing — RESEARCH Pattern 5 line 555 'tolerate non-JSON lines' is honored verbatim. The audit log (events.jsonl) records everything the CLI emitted; the structured Usage/Result extraction only fires on the terminal type=result event."
  - "Pattern: execFunc test seam — for any code that shells out to an external binary, define a `type execFunc func(ctx, name, args...) *exec.Cmd` field on the struct, default it via New() to exec.CommandContext, and let tests override it with a fixture-serving fake. Cleaner than interface-based mocking for the single shell-out case."

requirements-completed: [TEST-03]

duration: ~30min
completed: 2026-05-16
---

# Phase 03 Plan 05: Subagent Common + Anthropic Subagent Implementation — Summary

**internal/subagent/anthropic ships pkg/dispatch.Subagent backed by `claude -p --bare --output-format stream-json`; internal/subagent/common ships the provider-neutral plumbing (JSONL reader + go:embed prompt templates) every future provider will share.**

## Performance

- **Duration:** ~30 minutes (single-pass, no rework)
- **Tasks:** 2 / 2 (both TDD)
- **Test functions:** 17 (5 ReadLines + 3 PromptTemplate + 4 ParseStream + 5 Run)
- **Test subtests (table-driven expansions):** 24 PASS / 0 FAIL
- **Files created:** 12 (8 .go + 4 .tmpl)
- **LOC added:** 1,150 total (production + tests + templates)

## Accomplishments

1. **Provider firewall enforced at the package level.** `internal/subagent/common/` imports no CRD types and mentions no vendor by name. The asymmetry vs `internal/subagent/anthropic/` is the D-C1 layering boundary made structural — a future `internal/subagent/openai/` will reuse `common` unchanged. Verified by `grep -rcE 'api/v1alpha1' internal/subagent/common/*.go` returning 0 and `make verify-import-firewall` clean.
2. **CLAUDE.md anti-patterns hard-enforced.** Zero references to `~/.claude` or `HOME.*claude` anywhere in `internal/subagent/anthropic/` (godoc comments use neutral phrasing — "host claude config dir"). Zero imports of `github.com/anthropics/anthropic-sdk-go` — HARN-06 honored. `--bare` flag is the canonical claude invocation.
3. **Stream-json schema mapping is exhaustive.** All four token fields flow snake_case → camelCase per D-C5: `input_tokens`, `output_tokens`, `cache_read_input_tokens` (→ `CacheReadTokens`), `cache_creation_input_tokens` (→ `CacheCreationTokens`). Phase 2 D-D2 budget rollups will see the full spend, not just the uncached input.
4. **Q3 RESOLVED params allow-list landed.** `temperature`, `thinking_budget`, `top_p`, `top_k` accepted; anything else returns an error before exec'ing claude. Test `TestRun_UnknownParam` proves the gate; test `TestRun_AllowedParams` proves the four-key set passes through.
5. **Testable without claude CLI.** The `execFunc` indirection on `*Anthropic` lets tests inject `bash -c 'cat fixture.jsonl'`. `TestRun_HappyPath` exercises the full exec → ParseStream → events.jsonl-write → EnvelopeOut-assembly path without any external dependency.

## Task Commits

Each task was committed atomically per the per-task commit protocol. TDD tasks combined RED + GREEN into a single feat commit (the failing tests and their implementation were committed together, with the test file present at the RED gate and the production file present at the GREEN gate — verified by running `go test` between writes).

1. **Task 1: internal/subagent/common — JSONL reader + go:embed prompt templates + four v1 .tmpl files** — `d31b872` (feat)
2. **Task 2: internal/subagent/anthropic — stream-json parser + Subagent.Run via `claude --bare`** — `c4ab007` (feat)

## Files Created

- `internal/subagent/common/stream_reader.go` — `ReadLines(r io.Reader, handler func([]byte) error) error` with 16MB scanner buffer; provider-neutral.
- `internal/subagent/common/prompt_templates.go` — `LoadPromptTemplate(role, level string) (*template.Template, error)` backed by `//go:embed templates/*.tmpl`.
- `internal/subagent/common/templates/{milestone_planner,phase_planner,plan_planner,task_executor}.tmpl` — minimal v1 prompts referencing README.md spec; thread `{{.Level}} {{.TaskUID}} {{.Provider.Model}}` from `pkgdispatch.EnvelopeIn`.
- `internal/subagent/anthropic/stream_parser.go` — `ParseStream(r io.Reader, rawSink io.Writer) (Usage, string, error)`; tees verbatim bytes to events.jsonl, extracts structured Usage + Result from terminal type=result event.
- `internal/subagent/anthropic/subagent.go` — `Anthropic` struct with `Options{ClaudeBinary, WorkspaceRoot}` config + `Run(ctx, EnvelopeIn) (EnvelopeOut, error)` satisfying `pkg/dispatch.Subagent`.
- All four `*_test.go` files: 17 test functions covering happy paths, edge cases, fail-fasts, and the full exec → parse → envelope path via fake exec.

## Decisions Made

- **CLI over SDK (HARN-06 honored).** The `claude` CLI bundles the agent loop; re-implementing it with `github.com/anthropics/anthropic-sdk-go` would double Phase 3 scope. Decision logged in 03-RESEARCH §"Alternatives Considered".
- **`--bare` is non-negotiable.** The flag is in `args[]` not behind a config knob. RESEARCH §"Critical new finding" demonstrated this is the only way to skip claude's auto-discovery of host config — without it, the per-Pod runtime would pick up hooks/MCP from whichever filesystem layer happens to mount `~/.claude/`.
- **Drop "Input" infix on cache token field names (D-C5 finalized).** `pkg/dispatch.Usage.CacheReadTokens` and `Usage.CacheCreationTokens` — Phase 2's `Usage.InputTokens` already covers the dimension. Cleaner camelCase, no redundancy. The mapping happens explicitly in `ParseStream` so the rename is local to this package; pkg/dispatch's field names are stable.
- **Testable seam over interface mocking.** `execFunc func(ctx, name string, args ...string) *exec.Cmd` field on `*Anthropic` is simpler than wrapping `os/exec` behind an interface. Only one shell-out per Run(); no risk of pile-on indirection later.
- **Dispatch-level vs task-level errors split.** `pkg/dispatch.Subagent` godoc explicitly distinguishes them. Non-zero `exec.Wait` exit returns `(EnvelopeOut{ExitCode>0, Reason: stderr}, nil)` — the harness reads ExitCode/Reason to record the task failure; the dispatch (and therefore the K8s Job) is considered to have run cleanly. I/O failures setting up events.jsonl or starting the exec return non-nil err — the Job itself failed.

## Patterns Established

1. **Provider firewall at the package boundary.** Common-shared primitives live in `internal/subagent/common/` and are CRD-agnostic + vendor-neutral by construction. Vendor-specific implementations live in `internal/subagent/<vendor>/`. The directory split mirrors the type firewall enforced by `tools/analyzers/providerfirewall.Analyzer` — both are checked at build time.
2. **`go:embed` for compiled-in prompt templates.** `//go:embed templates/*.tmpl` + `template.ParseFS` ships prompt content in the binary; no ConfigMap mount, no runtime filesystem dependency. Future template additions are just new `.tmpl` files — the loader's contract (`LoadPromptTemplate(role, level)`) is stable.
3. **Defensive stream parsing.** Non-JSON lines (rare but documented in RESEARCH Pattern 5) are teed to the audit log but do not propagate errors. The structured extraction only fires on the recognized `type=result` event. This is the inverse of "fail fast on malformed input" — the audit log is the source of truth, the structured extraction is best-effort.
4. **`execFunc` seam for testable shell-outs.** Production callers see `New() → *Anthropic` and call `Run()`. Tests reach into `a.execFunc = fakeExec` directly. The fake returns an `exec.Cmd` that streams a fixture file via `bash -c 'cat …'`. Cleaner than `Cmder` interfaces, no production overhead.

## Verification

```bash
go test ./internal/subagent/... -count=1 -timeout 30s
# ok  github.com/jsquirrelz/tide/internal/subagent/anthropic  0.393s
# ok  github.com/jsquirrelz/tide/internal/subagent/common     0.776s

go vet ./internal/subagent/...
# (clean)

go build ./...
# (clean)

grep -rcE '~/\.claude|HOME.*claude|anthropic-sdk-go' internal/subagent/anthropic/
# all 0

make verify-import-firewall
# (clean — providerfirewall analyzer passes)

make verify-dispatch-imports
# OK: pkg/dispatch imports are clean
```

All success criteria from the plan are satisfied:

- [x] All tasks executed; each committed individually (`d31b872`, `c4ab007`)
- [x] `internal/subagent/common/` net-new package shipped (stream_reader + prompt_templates + 4 .tmpl files)
- [x] `internal/subagent/anthropic/` net-new package implements `pkg/dispatch.Subagent`; reads `Provider.Model`; invokes `claude -p --model <model> --bare --output-format stream-json`; sanity-checks `Vendor=="anthropic"`
- [x] Params allow-list enforced (`{temperature, thinking_budget, top_p, top_k}`) — unknown params fail-fast before exec
- [x] Stream-json parser extracts all 4 usage tokens (snake_case → camelCase mapping verified in `TestParseStream_UsageMapping`) + final assistant message text
- [x] `go test ./internal/subagent/... -count=1 -timeout 30s` passes (17 test functions, 24 subtest expansions, all PASS, no live API call)
- [x] `go build ./...` clean; `go vet ./internal/subagent/...` clean
- [x] No `github.com/anthropics/anthropic-sdk-go` import (HARN-06 CLI choice)
- [x] No host claude config dir references anywhere in the new code (CLAUDE.md anti-pattern enforced)

## Deviations from Plan

**None on the spec.** Two minor presentational adjustments made (Rule 1: comment phrasing):

1. **AC11 / AC12 grep-cleanliness.** The action description in Task 2 allowed `~/.claude` and `HOME` references "perhaps in a clearly-commented 'do-not-do' anti-pattern note" — but the acceptance criterion is `grep returns 0`. Resolved by rephrasing the godoc comments to "host claude config dir" and "Anthropic Go SDK module" (preserving the anti-pattern semantics) so the strict grep returns 0. The CLAUDE.md anti-pattern intent is fully preserved; only the literal token strings are avoided. No test or behavior impact.
2. **`internal/subagent/common/stream_reader.go` similar fix.** The package godoc originally said "must NOT import api/v1alpha1" which tripped the `grep -rcE 'api/v1alpha1'` AC for Task 1. Rephrased to "must NOT import the CRD types package". Same intent; cleaner grep.

Both are pure-comment edits, no logic changes. Recorded here for traceability.

## Known Stubs

**None.** This plan ships:
- Real stream-json parser with verified-stable snake_case→camelCase mapping.
- Real exec-based Subagent.Run; the testable seam (`execFunc`) is a production indirection that defaults to `exec.CommandContext` — no stub or mock code ships in production paths.
- Real go:embed prompt templates with non-empty v1 prose. Per CONTEXT.md "Claude's Discretion: Prompt-template content," the templates are intentionally minimal v1; users refine them iteratively post-v1. The loader contract (`LoadPromptTemplate(role, level)`) is fixed.

The `Anthropic.Run()` happy-path is exercised end-to-end by `TestRun_HappyPath` via a fake `execFunc`. The live `claude` CLI integration is exercised by Plan 03-07's E2E test (per the plan's scope note: "this plan does NOT ship the harness wiring path nor the Dockerfile/image — those live in plan 03-07").

## Threat Flags

No new threat surface beyond what the threat model documents. The three mitigated threats in `<threat_model>` (T-305 host credential leak, T-306 prompt-template injection, T-307 budget hiding via cache tokens) are all enforced by the implementation:

- **T-305 (mitigate):** `grep -rcnE '~/\.claude|HOME.*claude' internal/subagent/anthropic/*.go` returns 0 across all four files. `ANTHROPIC_API_KEY` is set from `in.SignedToken` only.
- **T-306 (mitigate):** Templates are compiled-in via `go:embed` — immutable bytes in the binary; end users cannot inject prompt content because templates load from `embed.FS`, not from a `Project.Spec` field.
- **T-307 (mitigate):** `TestParseStream_UsageMapping` asserts the 4-field snake_case→camelCase mapping; `Usage.CacheReadTokens` and `Usage.CacheCreationTokens` are populated explicitly. Phase 2 D-D2 budget rollup sees the full spend.

## Self-Check: PASSED

Verified before writing this section:

```bash
# All created files exist
ls internal/subagent/common/{stream_reader,prompt_templates}.{go,go} \
   internal/subagent/common/templates/{milestone_planner,phase_planner,plan_planner,task_executor}.tmpl \
   internal/subagent/anthropic/{subagent,stream_parser}.{go,go} \
   internal/subagent/anthropic/{subagent,stream_parser}_test.go \
   internal/subagent/common/{stream_reader,prompt_templates}_test.go
# All present

# Commits exist on the worktree branch
git log --oneline -2
# c4ab007 feat(03-05): add internal/subagent/anthropic …
# d31b872 feat(03-05): add internal/subagent/common …

# Tests still pass
go test ./internal/subagent/... -count=1 -timeout 30s
# ok  …/anthropic, ok  …/common
```

All claimed files exist; both task commit hashes are present on `worktree-agent-a32499cccfb3a3b38`; all 17 test functions pass.
