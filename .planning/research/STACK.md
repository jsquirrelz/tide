# Stack Research — TIDE v1.0.2 Ebb Tide (Token & Cost Optimization + Eval Harness)

**Domain:** Cost-optimization additions to an existing Go/Kubernetes operator + a reusable quality/cost eval harness
**Researched:** 2026-06-15
**Confidence:** HIGH for token-counting approach (verified against official Anthropic docs + codebase inspection); MEDIUM for golden-file library choice (two solid options, strong convention either way); MEDIUM for LLM-output quality eval (structurally-verifiable subset is HIGH; semantic subset needs pragmatic tradeoffs).

This file covers ONLY the stack additions and changes needed for v1.0.2. The full prior stack (controller-runtime, Ginkgo, Prometheus, OTel, etc.) is unchanged from the v1.0.1 research and is not restated here.

---

## Context: What Is Already In Place

Verified from codebase inspection before researching:

- `internal/subagent/anthropic/stream_parser.go` — `ParseStream` reads the CLI's `--output-format stream-json` stdout, tees raw bytes to `events.jsonl`, and extracts the terminal `result` event's `usage` block: `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`.
- `internal/subagent/anthropic/pricing.go` — per-model price table (cents/MTok, four token dimensions), `estimatedCostCents()`, conservative fallback tier.
- `internal/metrics/registry.go` — six Prometheus counters/histograms for token + cost telemetry: `tide_tokens_input_total`, `tide_tokens_output_total`, `tide_tokens_cache_read_total`, `tide_tokens_cache_creation_total`, `tide_cost_cents_total`, `tide_task_duration_seconds`.
- `internal/subagent/common/prompt_templates.go` — five `text/template` prompt templates embedded via `go:embed`; `LoadPromptTemplate(role, level)` API.
- `internal/subagent/common/prompt_templates_test.go` — existing tests: render-non-empty, field-threading, file-touch rule phrase assertion. No golden file or token-size assertions yet.
- `internal/credproxy/server.go` — `POST /v1/messages/count_tokens` is already in the credproxy allowlist.

---

## 1. Token Counting: Use the Stream-JSON `result` Event (No Change Needed)

### Ruling

The current approach — parsing the terminal `result` event's `usage` block from `--output-format stream-json` stdout — is correct and accurate. **No new library or API call is needed for post-hoc token counting.**

### Evidence

The Anthropic gille.ai article that reported "100x undercounting" was specifically about on-disk JSONL session log files written to `~/.claude/projects/*/`. Those files capture streaming-placeholder `usage` blocks from `message_delta` events that are never updated. That article does not examine the stdout `result` event.

The `result` event in `--output-format stream-json` stdout is a different data path. It is written once, after the full session completes, and contains cumulative token totals for the entire session. The `stream_parser_test.go` fixtures confirm the field names and shapes: `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`. The takopi.dev cheatsheet and the backgroundclaude.com stream-json article both show the `result` event containing final `usage` totals. TIDE's `ParseStream` only reads `type == "result"` events, never streaming partial events — so it is unaffected by the streaming placeholder issue.

**Confidence: HIGH.** The stream `result` event is the right source for post-hoc accounting. The existing `ParseStream` implementation is correct.

### Why NOT tiktoken

tiktoken is OpenAI's BPE tokenizer. Anthropic has not published its production tokenizer. The Xenova/claude-tokenizer on HuggingFace is a community reverse-engineering effort. Claude models use byte-level BPE with NFKC normalization that differs from GPT tokenization. Using tiktoken on Claude prompts produces incorrect counts — they diverge, especially on non-ASCII text, code, and emoji. The `count_tokens` API (below) is the only correct pre-flight counter.

**Do not add tiktoken or any local tokenizer as a Go dependency.** The counts would be wrong.

### Pre-Flight Token Counting: `POST /v1/messages/count_tokens`

For the eval harness and prompt-optimization development workflow only (not in the hot dispatch path), the Anthropic `count_tokens` API is the right tool for measuring prompt sizes before dispatch.

- **Endpoint:** `POST https://api.anthropic.com/v1/messages/count_tokens`
- **Auth:** standard `x-api-key` header (already handled by credproxy)
- **Request:** same JSON shape as `POST /v1/messages` minus `stream`
- **Response:** `{ "input_tokens": N }` — synchronous, no inference, free (subject to RPM limits)
- **RPM limits by usage tier:** 100/2000/4000/8000 RPM (separate quota from inference)
- **Note:** Does not account for prompt caching; returns uncached input token count only

Implementation: plain `net/http` `POST` to the credproxy (which already allows `/v1/messages/count_tokens`). No SDK needed. Construct the JSON body with `encoding/json`. Response is a single `input_tokens` int64.

**New package location:** `internal/subagent/anthropic/tokencounter.go` — a `CountTokens(ctx, model, systemPrompt, userText string) (int64, error)` function used only by the eval harness, never in the hot dispatch path.

**Important caveat:** the `count_tokens` response is for the baseline uncached input. It is not a cost prediction — cache hit rates depend on the actual serving layer. Use it to measure *prompt size reduction* (before/after template changes) as a leading indicator, not as a direct cost figure. Cost delta comes from the `result` event usage block after an actual dispatch.

---

## 2. Prompt Size Measurement: Plain HTTP to count_tokens

**No new Go module needed.** The call is four lines with `net/http` + `encoding/json`. Do not import the Anthropic Go SDK for this; the no-SDK constraint is load-bearing per CLAUDE.md.

The only new Go code is:
1. A request struct with `Model string`, `System string`, and `Messages []struct{ Role, Content string }` JSON fields.
2. A response struct with `InputTokens int64 \`json:"input_tokens"\`` field.
3. Standard HTTP POST using `http.DefaultClient` or a passed `*http.Client`.

For the eval harness, this is called once per template variant per level, offline, before any dispatch. It gives the token-count delta between the current template and a proposed trimmed template without spending inference budget.

---

## 3. Golden-File Testing for Prompt Templates

### Ruling

Use **goldie v2** (`github.com/sebdah/goldie/v2 v2.8.0`) for golden-file snapshot testing of rendered prompt templates.

### Rationale

The existing `prompt_templates_test.go` uses `strings.Contains` for specific phrases and `buf.Len() > 0` for non-empty checks. These tests confirm correctness of individual rules but do not catch accidental token-count regressions when template wording changes. Adding golden files closes that gap:

- A golden file captures the full rendered output of each template for a canonical fixture `EnvelopeIn`.
- After a template edit, `go test ./... -update` regenerates the golden files.
- CI runs without `-update`; any diff fails the test, making prompt changes a deliberate act with a visible diff in the PR.
- This directly gates "prompt size did not accidentally grow" without any API call.

### Why goldie v2 over alternatives

| Option | Assessment |
|--------|------------|
| `goldie/v2` (sebdah) | Purpose-built for Go golden-file testing; `AssertJson` for structured output; `go test -update` regeneration; MIT; v2.8.0 released Oct 2025; aligns with Go community convention |
| `gotest.tools/v3/golden` | Apache-2.0, leaner API surface, works; but fewer features (no JSON-aware diff); only 6 known importers on pkg.go.dev — smaller community footprint |
| Hand-rolled with `-update` flag | Zero new dependency; viable for simple text; lacks JSON diff, fixture directory convention, diff formatting |
| Snapshot libraries from other ecosystems | Not Go-native; wrong |

goldie v2 wins because the eval harness will compare both plain-text template renders AND JSON-encoded token-count snapshots (output of `count_tokens` calls on fixtures). `g.AssertJson(t, name, jsonBytes)` gives structured diff output that makes "which token went where" visible in test failures. The `-update` flag convention is identical to what's already established in Go stdlib (gofmt uses the same pattern).

### Integration pattern

```go
// internal/subagent/common/prompt_templates_golden_test.go
func TestTemplateGolden(t *testing.T) {
    g := goldie.New(t)
    in := canonicalFixture() // shared helper returning a populated EnvelopeIn
    for _, tc := range allTemplateCombos {
        var buf bytes.Buffer
        tmpl, _ := LoadPromptTemplate(tc.role, tc.level)
        _ = tmpl.Execute(&buf, in)
        g.Assert(t, tc.level+"_"+tc.role, buf.Bytes())
    }
}
```

Golden files land in `internal/subagent/common/testdata/*.golden` (goldie v2 default).

Regenerate: `go test ./internal/subagent/common/... -update`

**No embedded LLM call; purely deterministic; no network; runs in CI without credentials.**

### Installation

```bash
go get github.com/sebdah/goldie/v2@v2.8.0
```

---

## 4. Token-Count Snapshot Testing

For the eval harness, capture the `count_tokens` result (input token count) for each template variant as a golden JSON file:

```json
// testdata/token_counts/task_executor.golden.json
{ "input_tokens": 1247 }
```

On every PR that touches a template, CI calls `count_tokens` via the credproxy in a dedicated `go test -tags=eval` build tag, updates the golden if `-update` is set, and fails the build if the actual count deviates by more than a configurable threshold (default: +5%). This is the cost-regression gate.

Implementation: a separate `eval_test.go` file guarded by `//go:build eval`. The `eval` build tag keeps it out of the standard `make test-unit` and `make test-int` runs; it runs only in a dedicated `make eval` target or in a scheduled CI job. The test reads `ANTHROPIC_API_KEY` from env (or skips if absent).

---

## 5. Quality + Cost Eval Harness: Architecture

### Design principles

- **Offline-first:** structural checks (template rendering, token-count delta, JSON schema conformance) run in `make test-unit` with no network.
- **API-gated subset:** token-count API calls and live dispatch evaluation run behind `//go:build eval` and `make eval`.
- **No external SaaS:** Arize Phoenix, LangSmith, Promptfoo, DeepEval, etc. are out. They introduce hidden host dependencies and vendor lock-in that contradict CLAUDE.md constraints. The harness is self-contained Go test code.
- **Threshold-based gating, not equality:** LLM outputs are non-deterministic. The harness gates on token-count delta (deterministic), structural properties of the output (regex, JSON schema), and a manually reviewed golden sample — not on exact text match for live dispatch outputs.

### Two-layer structure

**Layer A — Deterministic (no API, runs in `make test-unit`):**

1. Golden-file test for every rendered template against canonical `EnvelopeIn` fixtures. Catches wording changes that affect size.
2. Structural assertions on rendered templates: required phrases present (`strings.Contains`), no forbidden patterns, byte-length bounds (soft warning at N bytes, hard failure at M bytes). Extend `prompt_templates_test.go`.
3. Template metadata: a `TemplateStats(role, level string) Stats` function that returns rendered byte count, line count, and an estimated "section count" for the canonical fixture. Used in the eval report.

**Layer B — API-gated (requires `ANTHROPIC_API_KEY`, runs in `make eval`):**

1. `count_tokens` call per template variant → JSON golden file with `input_tokens`. Threshold gate: new count must be ≤ 105% of the golden (fail if prompt grew by >5%).
2. Optionally: a live dispatch on a canned micro-project (a fixture project with a 5-task DAG, deterministic inputs, small scope) via the stub-subagent path, measuring actual `result` event usage. Compare to golden `{ "input_tokens": N, "output_tokens": N, "cache_read_tokens": N, "cache_creation_tokens": N, "cost_cents": N }`. Gate on cost_cents ≤ 110% of golden.
3. Structural quality assertions on live dispatch output: required JSON keys present in `EnvelopeOut`, result text non-empty, no error subtype, section headers present (regex). These are the "output quality" gates. They are structural (not semantic) by design.

### Why structural-only quality eval

Semantic LLM-as-a-judge evaluation (e.g., "is this PLAN.md good?") requires calling a judge model, which costs money, introduces non-determinism in the gate itself, and creates a circular dependency (the judge could be the same model being optimized). For a coding orchestrator, the outputs have deterministic structural properties that are sufficient quality indicators:

- `EnvelopeOut` is valid JSON with required fields.
- The result text is non-empty.
- Task-level output contains expected section headers (e.g., `## Implementation`).
- Token counts are within bounds.
- No `is_error: true` in the result event.

These structural checks are implementable with `encoding/json`, `regexp`, and `strings` from the standard library. Add one helper package `internal/eval/` that provides `Assertions(t testing.TB, out EnvelopeOut, opts ...AssertOpt)` — composable, no external eval framework.

---

## 6. What NOT to Add

| Item | Why Not |
|------|---------|
| Anthropic Go SDK (`github.com/anthropics/anthropic-sdk-go`) | Violates CLAUDE.md no-SDK constraint; `count_tokens` is 4 lines of `net/http` |
| tiktoken or any local Claude tokenizer | Anthropic has not published its tokenizer; any local approximation is wrong by design; `count_tokens` API is the only correct counter |
| DeepEval, Promptfoo, LangSmith, Arize | External SaaS or Python frameworks; violate no-hidden-host-dependency constraint; require per-project setup outside the Go test suite |
| Arize Phoenix (self-hosted) | Heavy operational dependency just for an eval harness; the existing OTel export already feeds Phoenix if operators want it; do not require it for CI |
| `gonum/graph` or any graph library for eval DAG traversal | The existing `pkg/dag` stdlib Kahn implementation is sufficient; no new graph dep |
| Separate eval database (SQLite, Postgres) | Violates CRD-status-only constraint; golden JSON files in `testdata/` are the persistence layer |
| `github.com/nao1215/golden` | Younger fork of goldie; less adoption; go with the original sebdah/goldie |
| `gotest.tools/v3/golden` as primary | Fewer features for this use case (no JSON-aware diff); usable but goldie v2 is better fit |
| Separate eval service / sidecar | Overkill for a development-time regression gate; the Go test suite IS the harness |
| Fuzzing for prompt templates | The templates are manually curated prose, not parser code; fuzzing adds no value here |

---

## 7. Summary: New Go Modules

Only one net-new module is added to `go.mod`:

| Module | Version | Purpose | Used In |
|--------|---------|---------|---------|
| `github.com/sebdah/goldie/v2` | `v2.8.0` | Golden-file snapshot testing for rendered prompt templates and token-count JSON | `_test.go` files (test-only) |

No production imports. No new controller-runtime or K8s-ecosystem dependencies. No Anthropic SDK.

The `count_tokens` caller is plain `net/http` with `encoding/json` — already in Go stdlib.

The eval harness is `internal/eval/` — pure Go, zero new dependencies beyond the existing test stack (Ginkgo/Gomega are optional here; standard `testing` package is sufficient for the eval layer).

---

## 8. Integration Points with Existing Code

| Existing Component | What Changes |
|-------------------|-------------|
| `stream_parser.go` | No change. `result` event usage block is already the correct token source. |
| `pricing.go` | No change for the harness. Consider adding `TokenSizeWarningBytes` and `TokenSizeLimitBytes` constants for the Layer A structural gate. |
| `prompt_templates.go` | Add `TemplateStats(role, level string) Stats` alongside `LoadPromptTemplate`. Returns `{ByteCount, LineCount int}` for the canonical fixture. |
| `prompt_templates_test.go` | Add golden-file tests and byte-bound assertions without modifying existing tests. |
| `internal/credproxy/server.go` | No change. `/v1/messages/count_tokens` is already in the allowlist. |
| `internal/metrics/registry.go` | No change. Existing token counters already capture what the eval harness needs. |
| `Makefile` | Add `make eval` target: `go test -tags=eval ./internal/eval/... -v` |

---

## Sources

- [Anthropic Token Counting API docs](https://platform.claude.com/docs/en/build-with-claude/token-counting) — endpoint, request shape, response `{ "input_tokens": N }`, free/RPM, no caching semantics (HIGH confidence, official)
- [goldie v2.8.0 on pkg.go.dev](https://pkg.go.dev/github.com/sebdah/goldie/v2) — version, `AssertJson`, `-update` flag, MIT license (HIGH confidence, official pkg.go.dev)
- [gotest.tools/v3/golden on pkg.go.dev](https://pkg.go.dev/gotest.tools/v3/golden) — v3.5.2, Apache-2.0, simpler API, 6 importers (MEDIUM confidence)
- [Claude Code JSONL token undercount article](https://gille.ai/en/blog/claude-code-jsonl-logs-undercount-tokens/) — confirms inaccuracy is specific to on-disk `~/.claude/projects/` session logs, NOT stdout `result` events (MEDIUM confidence; article author's scope is clearly the session log files)
- [Claude stream-json cheatsheet](https://takopi.dev/reference/runners/claude/stream-json-cheatsheet/) — `result` event shape with `usage` block (MEDIUM confidence)
- [Token counting without tokenizer — GoPenAI](https://blog.gopenai.com/counting-claude-tokens-without-a-tokenizer-e767f2b6e632) — why local tokenizers are wrong for Claude; API-only approach (MEDIUM confidence)
- [Anthropic tokenizer incompatibility — DEV Community](https://dev.to/jerown/anthropic-never-released-their-tokenizer-heres-what-we-found-testing-the-alternatives-b05) — no production tokenizer published; Xenova is community reverse-engineering (MEDIUM confidence)
- Codebase inspection: `stream_parser.go`, `stream_parser_test.go`, `pricing.go`, `prompt_templates.go`, `prompt_templates_test.go`, `registry.go`, `credproxy/server.go` — direct read, HIGH confidence
