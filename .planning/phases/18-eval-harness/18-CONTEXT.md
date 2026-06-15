# Phase 18: Eval Harness - Context

**Gathered:** 2026-06-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Build the quality + cost gate that must exist **before any template/prompt change** in the rest of v1.0.2. Phase 18 delivers:

1. A **frozen v1.0.1 baseline** — golden renders of all five prompt templates + a recorded four-field-Usage snapshot, committed under `testdata/`.
2. A **deterministic eval harness** (`internal/eval/`) whose primary gate is protocol-compliance (child-CRD parse success, declared-output-path presence, DAG acyclicity) — no LLM judge.
3. Golden-file snapshot tests (goldie/v2) + a per-template token ratchet that catch accidental prompt drift/growth automatically in CI.
4. A cost-delta check that **delegates to the existing `(*Anthropic).estimatedCostCents`** (asserts parity, no re-implementation) and reports REALIZED per-wave savings (cache-write premium subtracted).
5. A `make eval` maintainer tool for live `count_tokens` pre-flight via the existing credproxy.

This phase clarifies HOW to build the harness. WHAT it must do is locked by requirements EVAL-01–06. New capabilities (template reorder, SharedContext, dashboard) belong to Phases 19–21.
</domain>

<decisions>
## Implementation Decisions

### Eval architecture — split by "does it touch the network/model", not by concept
- **D-02:** All eval code lives in a new **`internal/eval/` package** (named/owned as eval). But the **deterministic gate runs in the existing `make test` unit tier** — NOT behind a build tag, NOT in a separate `make test-unit` target. Rationale: EVAL-06 requires the regression gate to fire automatically, for free, zero-network, on every PR. Gating it behind creds/network would silently disable the tripwire.
- **D-02a:** **No `make test-unit` target is added.** The deterministic eval tests are plain Go tests in `internal/eval/` and already run under `make test`. Roadmap/requirements wording that says `make test-unit` should be read as `make test` (the unit tier). The ONE new Makefile target is `make eval` (online, below).
- **D-05:** **Deterministic-only gate this milestone.** The gate is purely the protocol-compliance checks (child-CRD parse / declared-output-path / DAG acyclicity), zero-network. **No LLM-as-judge** scaffold in v1.0.2 — full semantic judging stays deferred (EVAL-F1). When a judge eventually lands, it joins the *online* surface behind `make eval`; it is NOT goldie (you cannot byte-diff non-deterministic model output).

### Token ratchet (EVAL-06)
- **D-01:** **Per-template no-growth snapshot.** Each template's size is committed as testdata; ANY growth above the committed number hard-fails `make test`. Updating the number is a deliberate, reviewed commit (same discipline as goldie golden files). Phase 18 freezes the snapshot at current (un-trimmed) v1.0.1 counts so the initial gate is "no accidental growth"; Phase 19 ratchets the numbers DOWN after trimming.
- **D-01a (research detail):** The offline ratchet cannot call `count_tokens` (no network in `make test`, and Anthropic has no exact local tokenizer — do NOT add tiktoken or a guessed tokenizer). It must snapshot a **deterministic offline proxy** of size (bytes / runes / whitespace-words of the rendered output). Research/planning picks the proxy unit. The authoritative provider token count comes from `make eval` (below), used to tune thresholds and verify the 1,024 cache floor.

### count_tokens pre-flight (EVAL-05) — a tool, not a test
- **D-03:** Ship the `count_tokens` pre-flight as a **small command (`cmd/tide-eval/` or equivalent) behind `//go:build eval`, invoked by `make eval`** — NOT a `go test`. It is a maintainer report tool (network + creds + running credproxy), not a deterministic assertion. It renders each template, POSTs to the credproxy's already-allowlisted `POST /v1/messages/count_tokens` (stdlib `net/http` + `encoding/json`, NO Anthropic SDK), and prints per-template real token counts + whether each stable prefix clears the 1,024-token cache minimum (Sonnet/Opus; document the Haiku 4,096 gap).

### Realized-savings fixture (EVAL-04)
- **D-04:** **Capture one real `claude -p --bare` dispatch** and freeze its `events.jsonl` as the canonical fixture. No run-1 telemetry is committed in-repo, so a one-time minimal real-dispatch capture path is required (flagged for research — keep it cheap). The fixture must populate **all four token dimensions** that `estimatedCostCents` sums — `InputTokens`, `OutputTokens`, `CacheReadTokens`, `CacheCreationTokens` — because a cache-creation/cache-read pair is what proves the realized-savings math (cache-write premium subtracted, per-wave not per-dispatch). The cost-delta check itself is a deterministic **test** in `make test` (pure arithmetic over the captured fixture); only the live token count is the online tool.

### Claude's Discretion
- Offline ratchet proxy unit (bytes vs runes vs words) — planner/research to choose.
- Exact command path/name for the `make eval` tool (`cmd/tide-eval/` is a suggestion).
- Canonical EnvelopeIn fixture shape for golden renders (must be fixed/deterministic; PROMPT-05's stable key-order serialization matters so goldens don't flap).
- Cheapest real-dispatch capture mechanism for the events.jsonl fixture.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (locked scope)
- `.planning/REQUIREMENTS.md` — EVAL-01–06 (this phase) + binding constraints (CLI-based, no SDK, no `cache_control`, don't rebuild cost accounting). Future: EVAL-F1 (LLM-judge, deferred).
- `.planning/ROADMAP.md` §"Phase 18: Eval Harness" — 5 success criteria. Note the `make test-unit` references → read as `make test` per D-02a.

### Research (HIGH confidence; read before planning)
- `.planning/research/SUMMARY.md` — milestone reframing (templates ~200 tokens < 1,024 cache min → caching never fires today), correct build sequence, eval-harness scope, anti-features.
- `.planning/research/PITFALLS.md` — over-trimming load-bearing instructions (catastrophic); cost re-implementation divergence; cache-write premium net-negative on one-shot dispatches.
- `.planning/research/ARCHITECTURE.md` — `internal/eval/` slotting; files that MUST NOT change (`stream_parser.go`, `pricing.go`, `metrics/registry.go`, executor path).
- `.planning/research/STACK.md` — `github.com/sebdah/goldie/v2 v2.8.0` (test-only); stdlib `net/http` for count_tokens; NO new production deps.

### Code (ground truth — read the cited lines)
- `internal/subagent/anthropic/pricing.go:132` — `(*Anthropic).estimatedCostCents(model, Usage)`. **Unexported method on `*Anthropic`** → the cost-parity check must call it in-package or via an exported wrapper. Sums four token dimensions; conservative fallback on unknown model; ceiling division to whole cents. Delegate, do not re-implement (EVAL-04).
- `pkg/dispatch/envelope.go:252` — `Usage` struct (InputTokens / OutputTokens / EstimatedCostCents + CacheReadTokens / CacheCreationTokens). `:39` — `EnvelopeIn` (Phase 20 adds `SharedContext` here; out of scope for 18).
- `internal/subagent/anthropic/stream_parser.go` — emits the four-field Usage from the `result` event. Use its existing test fixtures as the shape-of-truth for the captured fixture. Do NOT modify.
- `internal/credproxy/server.go:105-106` — credproxy already allowlists `POST /v1/messages` and `POST /v1/messages/count_tokens`; reverse-proxy `Director` injects the real key. The `make eval` tool POSTs here; never handles the key itself.
- `internal/subagent/common/templates/{milestone,project,phase,plan}_planner.tmpl`, `task_executor.tmpl` — the five templates to baseline. Rendered via `tmpl.Execute(&buf, in)`.
- `Makefile` — unit tier is `test` (prep + unit) / `test-only` (no prep); `test-int*` is the integration tier. `make eval` is new (D-03).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `(*Anthropic).estimatedCostCents` (`pricing.go:132`) — the ONLY cost authority; cost-parity test delegates to it. Unexported → needs in-package test or an exported thin wrapper.
- `stream_parser.go` test fixtures — canonical shape of the four-field `Usage`; reuse to build the realized-savings fixture rather than inventing a shape.
- credproxy `count_tokens` allowlist (`server.go:105-106`) — the network path for `make eval` already exists; no proxy changes needed.
- The five `.tmpl` files render with a plain `EnvelopeIn`; goldie snapshots a fixed render of each.

### Established Patterns
- Unit tier = `make test` / `test-only`; integration tier = `make test-int*`. New deterministic eval tests join the unit tier with no build tag. `make eval` (online, `//go:build eval`) is the one new target.
- `//go:build eval` already anticipated by requirements as the guard for network-touching code.
- Project verifies absence of forbidden deps via `make verify-*` targets — keep goldie test-only and avoid adding the Anthropic SDK (CLAUDE.md anti-pattern).

### Integration Points
- `internal/eval/` imports only `internal/subagent/common` + `internal/subagent/anthropic` + `pkg/dispatch` — NOT `internal/controller` or CRD types (per ARCHITECTURE.md).
- Baselines + fixtures live under `testdata/` (goldie convention: `testdata/baselines/<name>.golden`; `-update` regenerates).
- No hot-path changes: `stream_parser.go`, `pricing.go`, `metrics/registry.go`, executor path are read-only for this phase.

</code_context>

<specifics>
## Specific Ideas

- goldie usage: `g := goldie.New(t); g.Assert(t, "<template>", rendered)`; first run with `-update` writes the frozen golden; later diffs are the PR review artifact.
- `make eval` output: per-template real token count + a 1,024-floor pass/fail per template (the metric Phase 20 depends on).
- The dividing line for ALL eval work: deterministic + offline → `make test`; touches network/model (count_tokens now, judge later) → `make eval`.

</specifics>

<deferred>
## Deferred Ideas

- **LLM-as-judge / semantic-quality scoring** — EVAL-F1, deferred to a later milestone. Would join the `make eval` online surface, not goldie.
- **Template reorder / token trimming** — Phase 19 (PROMPT-01–05). Phase 18 only freezes and gates; it does not edit templates.
- **`SharedContext` on `EnvelopeIn`** — Phase 20 (CACHE-02/03).
- **Per-level token accounting + cache-hit dashboard panel** — Phase 21 (OBSV-01–03).

</deferred>

---

*Phase: 18-Eval Harness*
*Context gathered: 2026-06-15*
