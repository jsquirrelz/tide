# Project Research Summary

**Project:** TIDE v1.0.2 — Ebb Tide: Token & Cost Optimization
**Domain:** Prompt engineering + eval harness additions to a K8s-native agentic LLM orchestrator
**Researched:** 2026-06-15
**Confidence:** HIGH

## Executive Summary

TIDE v1.0.2 is a targeted cost-optimization milestone, not a new product. The research reveals a sharp reframing that must lead all planning: TIDE's current five prompt templates are approximately 200 tokens each — well below the 1,024-token prefix-caching minimum for Sonnet/Opus and the 4,096-token minimum for Haiku 4.5. **Caching never fires today.** Compounding this, `{{.TaskUID}}` and `{{.Provider.Vendor}}` appear in the stable boilerplate near the top of every template, so even if the threshold were met, wave-sibling dispatches would diverge at line 9-10 and each produce a unique cache entry. The templates are structured backwards for caching: volatile dispatch metadata first, stable instructions after. Fixing this ordering — putting stable content first, moving `{{.TaskUID}}` and `{{.Prompt}}` to the suffix — is the single highest-ROI structural change in the entire milestone.

The path to realized savings requires building a quality gate before making any template change. The correct sequence is: (1) freeze a baseline from v1.0.1 templates, (2) build the eval harness with protocol-compliance checks as the primary gate (child-CRD parse success, declared output paths, DAG acyclicity — all deterministic, no LLM judge required for the gate), (3) reorder templates stable-first and trim non-essential boilerplate, (4) add a `SharedContext` field to `EnvelopeIn` so wave-sibling dispatches share an identical plan/phase context block as a hoisted stable prefix, growing the shared prefix to ≥1,024 tokens where caching activates. The cache-write premium (1.25× on Anthropic) means caching is net-negative for one-shot dispatches; realized savings only materialize when wave siblings share the warm cache within the 5-minute TTL window. The eval harness must compute this correctly using the existing `estimatedCostCents` function — not a re-implementation.

The cost accounting and metrics infrastructure is already correct and complete. `stream_parser.go` reads the right source (the `result` event, not streaming placeholders). `pricing.go` applies four-field cost calculation with correct cache-read discount and write premium. All six Prometheus counters already emit the needed data. The dashboard just needs a cache-hit-ratio panel reading those existing counters. The primary risk is over-trimming load-bearing instructions — every clause in the current templates exists because a production cascade demonstrated its necessity. The annotation gate (categorize each clause before trimming) and the eval harness's protocol-compliance checks together prevent quality regression.

## Key Findings

### Recommended Stack

No net-new production dependencies. One new test module: `github.com/sebdah/goldie/v2 v2.8.0` (golden-file snapshot testing, test-only). The `count_tokens` API is useful offline in the eval harness via a four-line `net/http` call to the credproxy (already allowlisted); the Anthropic Go SDK must not be added.

**Core additions:**
- `github.com/sebdah/goldie/v2 v2.8.0` — golden-file snapshot testing for rendered templates; `AssertJson` for structured diffs; test-only import.
- `net/http` + `encoding/json` (stdlib) — pre-flight token counting via `POST /v1/messages/count_tokens` through the existing credproxy; no SDK, no new module.
- `internal/eval/` (new package, zero new deps) — deterministic test files (prompt size ratchet, stable prefix length, offline cost replay, structural quality gate); runs in `make test-unit`, no network, no LLM calls.
- `internal/subagent/anthropic/tokencounter.go` (new file, stdlib only) — `CountTokens(...)` behind `//go:build eval`.

**Must NOT add:** tiktoken or any local Claude tokenizer (wrong by design), Anthropic Go SDK (CLAUDE.md constraint), external SaaS eval platforms, any separate eval database.

### Expected Features

**Must have (P1 — all required for v1.0.2):**
- Stable-prefix-first ordering in all five templates; `{{.TaskUID}}` and `{{.Provider.*}}` removed from stable sections.
- Boilerplate audit: measure token count; grow stable prefix to ≥1,024 tokens with SharedContext.
- Deterministic serialization of structured data in the stable prefix.
- Eval harness `internal/eval/` with golden-set baseline + deterministic test types.
- Protocol-compliance checks as the primary eval gate: child-CRD parse success, declared output paths, DAG acyclicity.
- `SharedContext string` on `EnvelopeIn`, populated identically for all wave siblings.
- Cache hit rate metric from events.jsonl as a Prometheus gauge.
- Per-level token accounting as Prometheus labels on existing counters.

**Should have (P2 — after baseline established):**
- Cost + quality dashboard panel surfacing cache hit ratio and tokens-per-level.
- Context curation: summaries over verbatim PLAN.md dumps in SharedContext.
- Per-level token budget profiles as Helm values / CRD fields.

**Defer to v1.0.3+:** wave-sibling warm-up dispatch, direct SDK backend with explicit `cache_control`, OpenAI/Codex backend, full LLM-as-judge semantic scoring pipeline.

**Confirmed anti-features:** `cache_control` injection via `ProviderSpec.Params` (not possible on the CLI path), KV-cache sharing via forced shared worktree (violates isolation), per-task real-time events.jsonl streaming for live cache feedback.

### Architecture Approach

All five components slot cleanly into the existing dispatch pipeline without touching the hot path. Template restructuring is pure `.tmpl` changes; `tmpl.Execute(&buf, in)` handles new fields automatically. The SharedContext field is additive (`omitempty`; executor dispatches ignore it). The eval harness imports only `internal/subagent/common` and `internal/subagent/anthropic` — not `internal/controller` or CRD types beyond `pkg/dispatch`.

**Major components:**
1. Template restructuring (`templates/*.tmpl`) — role preamble → fixed instructions → `{{.SharedContext}}` → volatile metadata (TaskUID, Branch) → `{{.Prompt}}`.
2. SharedContext field (`pkg/dispatch/envelope.go` + `dispatch_helpers.go`) — additive; identical across all tasks in a wave; grows shared cacheable prefix to ≥1,024 tokens.
3. Eval harness (`internal/eval/`) — deterministic test files; golden baselines in `testdata/baselines/`; recorded `events.jsonl` fixtures; runs in `make test-unit`, no network.
4. Token minimization pass (`templates/*.tmpl`) — annotation-gated; one section at a time; harness gates each commit.
5. Dashboard cache observability (React frontend) — cache-hit-ratio panel consuming existing `tide_tokens_cache_read_total` / `tide_tokens_cache_creation_total`; no backend changes.

**Files that do NOT change:** `stream_parser.go`, `pricing.go`, `budget/tally.go`, `metrics/registry.go`, `task_controller.go` (executor path), `credproxy/server.go`, `pkg/dispatch/provider.go`.

### Critical Pitfalls

1. **Over-trimming load-bearing instructions (Catastrophic)** — every template clause exists because a production cascade proved its necessity. Prevention: annotation file before trimming; child-CRD parse success rate as gate criterion.
2. **Volatile `{{.TaskUID}}` in stable prefix busts every wave-sibling cache (Serious)** — currently near line 10 of every template. Prevention: stable-first restructure as the first change.
3. **CLI-vs-SDK gap: no `cache_control` lever (Serious)** — TIDE cannot set cache breakpoints; cache hit rate is an outcome metric, not a configurable parameter. Do not promise explicit breakpoint placement.
4. **Cache-write premium net-negative on one-shot dispatches (Moderate)** — savings only materialize across wave siblings within the 5-min TTL. Harness reports REALIZED savings per-wave, not per-dispatch.
5. **Cost re-implementation in eval harness diverges from `estimatedCostCents` (Moderate)** — delegate all cost math to `estimatedCostCents`; assert parity within 1 cent.

**Additional:** LLM-as-judge flakiness (deterministic checks are the gate), no stable baseline (freeze before first edit), provider-specific assumptions in harness (import only `pkg/dispatch`), Haiku 4,096-token minimum possibly unmet (verify or document Sonnet/Opus-only benefit).

## Implications for Roadmap

Suggested phases: **4**

### Phase 1: Baseline Capture and Eval Harness Foundation
Quality gate must exist before any template change. No dependencies. Delivers `internal/eval/` with deterministic test types, `testdata/baselines/` golden files from v1.0.1 templates, recorded `events.jsonl` fixtures, frozen baseline snapshot, goldie v2 snapshot tests. Avoids the no-stable-baseline and cost-re-implementation pitfalls.

### Phase 2: Template Reorder and Token Minimization
Highest-ROI change; must precede SharedContext so the stable prefix is structurally correct before being grown. Delivers all five templates restructured stable-first; `{{.TaskUID}}`/`{{.Provider.*}}` moved to the volatile suffix; per-template "why-this-line" annotation; trimmed boilerplate with protocol-compliance preserved; deterministic serialization. Avoids over-trimming, volatile-prefix, and mid-wave-model-change pitfalls.

### Phase 3: SharedContext Injection and Planner Wiring
Widest blast radius (`pkg/dispatch/envelope.go` + controller + three planner templates + eval fixtures); comes after simpler changes are proven. Delivers `SharedContext string` on `EnvelopeIn`; `BuildPlannerEnvelope` populates it identically per wave; planner templates reference `{{.SharedContext}}`; stable prefix grows to ≥1,024 tokens; updated baselines. **Needs a research sub-phase** (see flags).

### Phase 4: Metrics, Observability, and Dashboard
All data already flows; this phase makes it visible. Independent; parallelizable with Phases 2-3 once metric names are confirmed. Delivers per-level token accounting labels, cache-hit-rate metric, dashboard cache-efficiency panel (hit ratio, creation tokens, realized savings), `make eval` target.

### Research Flags

**Needs research during Phase 3 planning:**
- **Cross-pod cache scoping with `--bare` (most critical):** the assumption that stable-prefix-first ordering yields cache hits across wave siblings in different pods must be experimentally verified before Phase 3. If cross-pod cache is path-scoped (each pod has a unique working directory embedded in the CLI system prompt), SharedContext provides zero benefit and the milestone reframes to "best-effort token minimization only."
- **SharedContext source design:** controller PVC read vs. CRD `.status` summary for populating `SharedContext`.
- **Wave dispatch interval vs. 5-minute TTL:** measure actual cache-hit degradation for long waves (>5 min spread).
- **Haiku 4,096-token gap:** verify whether the restructured templates reach Haiku's minimum; if not, document caching benefit as Sonnet/Opus-only.

**Standard patterns (no research phase needed):** Phase 1 (eval harness + golden files), Phase 2 (template reorder + trim), Phase 4 (dashboard panel).

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Direct codebase inspection; single new test module |
| Features | HIGH | Provider caching mechanics from official docs; TIDE mapping from codebase |
| Architecture | HIGH | All findings from direct production codebase inspection |
| Pitfalls | HIGH | Template/pricing findings from source; caching foot-guns cross-verified |

**Overall confidence:** HIGH

### Gaps to Address
- Cross-pod cache scoping with `--bare` (most critical — verify before Phase 3; could reframe the milestone).
- Haiku 4,096-token threshold (verify; may scope caching benefit to Sonnet/Opus).
- Annotation file format convention (agree before Phase 2).
- LLM-as-judge scope decision for v1.0.2 (deterministic checks are the gate; decide if any supplementary judging is in scope before Phase 1).
