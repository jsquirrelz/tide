# Feature Research — TIDE v1.0.2 Ebb Tide: Token & Cost Optimization

**Domain:** Cost optimization features for a CLI-based agentic LLM orchestrator (TIDE)
**Researched:** 2026-06-15
**Confidence:** HIGH for provider caching mechanics (official docs verified); HIGH for prompt
structuring techniques (multiple cross-verified sources); MEDIUM for eval harness patterns
(WebSearch + practical synthesis, no single authoritative spec); HIGH for TIDE-specific
dependency mapping (direct inspection of codebase).

---

## Research Context

TIDE dispatches LLM subagents by shelling out to the Claude Code CLI (`claude -p --bare`,
prompt via stdin). The CLI manages its own prompt caching — TIDE does NOT set
`cache_control` directly. The levers available to TIDE are:

1. **Prompt structuring** — ordering template content stable-first so Claude Code's
   automatic prefix caching (and OpenAI/Codex's automatic prefix caching) maximizes cache
   hits across wave-sibling dispatches.
2. **Token minimization** — trimming static boilerplate, curating interpolated context,
   deduplicating shared context across wave-sibling dispatches.
3. **Quality + cost eval harness** — a reusable harness to regression-gate prompt/template
   changes and report token/cost deltas (including cache-read vs cache-write accounting).
4. **Observability surface** — surfacing already-emitted cache + token metrics meaningfully
   on the dashboard.

---

## Provider Caching Mechanics Reference

Understanding these mechanics directly shapes which prompt-structuring techniques matter
and which are irrelevant from TIDE's CLI-based vantage point.

### Anthropic (Claude Code CLI path — the live v1 path)

**How Claude Code handles caching automatically (HIGH confidence — official docs):**

Claude Code adds `cache_control` breakpoints automatically, structured in three layers:

| Layer | Content | Invalidated when |
|-------|---------|-----------------|
| System prompt | Core instructions, tool definitions, output style | Tool set changes, Claude Code upgrade |
| Project context | CLAUDE.md, auto-memory, unscoped rules | Session start, after `/clear` or `/compact` |
| Conversation | Messages, responses, tool results | Every turn (append-only) |

When TIDE dispatches `claude -p --bare`, each invocation is a fresh one-turn session:
- Layer 1 (system + tool definitions) is cached after the first dispatch.
- Layer 2 (project context — the CLAUDE.md in the worktree) is cached after the first
  dispatch for that worktree.
- Layer 3 (the conversation) is always the volatile `{{.Prompt}}` suffix — this is the
  only part that changes per-task.

Key cache mechanics:
- **Minimum 1,024 tokens** before caching activates (Sonnet/Opus); 4,096 for Haiku 4.5
  and Opus 4.5/4.6. TIDE's current templates (project_planner + task_executor) are well
  below 1,024 tokens each — caching does NOT currently activate for TIDE's own prompts
  without growth.
- **TTL:** 5 minutes on API key (default); 1 hour on Claude subscription
  (`ENABLE_PROMPT_CACHING_1H=1`). Wave siblings dispatched in parallel within a 5-minute
  window share the cache.
- **Cache prefix ordering is fixed:** tools → system → project context → conversation.
  Any change earlier in the chain busts everything after it.
- **Subagent sessions (which is what TIDE creates) use 5-minute TTL** even on a
  subscription. The Claude Code doc explicitly states: "Subagents use the five-minute TTL
  even on a subscription."
- **Cache is scoped per working directory.** TIDE's per-task worktrees each have a
  distinct path — the system prompt embeds the working directory. Wave siblings each in a
  different worktree do NOT share the cache prefix, even if their templates are identical.

**Implication for TIDE:** The system prompt + tool definitions layer is the stable
prefix. The `{{.Prompt}}` is the volatile suffix. The structural gain from putting
stable boilerplate first is already correct in the templates — but the templates are
currently too short to cross the 1,024-token caching threshold. The path to caching is:
grow the stable prefix to ≥1,024 tokens with shared context (wave/plan context as a
reusable prefix), then keep `{{.Prompt}}` as the last volatile block.

### OpenAI / Codex (next milestone — provider-agnostic design now)

**Mechanics (HIGH confidence — official docs):**
- **Automatic, no opt-in.** No `cache_control` field; the API detects prefix matches
  automatically on ≥1,024-token prompts (128-token increment granularity).
- **TTL:** 5–10 minutes inactivity (up to 1 hour maximum; 24 hours on gpt-5.x).
- **Prompt cache key** parameter available to improve routing stickiness for parallel
  dispatches — wave-sibling calls sharing the same prefix should use the same key.
- **What breaks cache:** any character difference in the prefix (including JSON key order,
  whitespace, timestamps, UUIDs).
- **Stable prefix ordering:** identical to Anthropic — role/persona first, instructions,
  tool definitions, conversation history, current query last.

**Implication for TIDE:** The same template-structuring discipline that maximizes Claude
Code cache hits also maximizes OpenAI prefix cache hits. Provider-agnostic by design is
achievable by following the stable-prefix-first rule universally.

### Other providers (Bedrock, Vertex, LLM gateways)

- **Bedrock:** Anthropic-style explicit cache_control required; support varies by model
  and region; minimum prefix length requirements differ per model.
- **Vertex AI:** Separate context caching API with explicit TTL; different from Anthropic's
  breakpoint model.
- **LLM gateways (LiteLLM, OpenRouter):** Forwarding varies; cache behavior depends on
  the target provider. TIDE should not assume gateway-level caching works.

For v1.0.2, model these as "design for Anthropic + OpenAI; verify gateway behavior
per-provider at integration time."

---

## Feature Landscape

### Table Stakes (Users Expect These — v1.0.2 Must Have)

| # | Feature | Why Expected | Complexity | TIDE Dependency |
|---|---------|--------------|------------|-----------------|
| TS-C1 | **Stable-prefix-first prompt template ordering** — system role + static boilerplate first, `{{.Prompt}}` last in all five templates | Cache hit requires exact prefix match; the volatile task prompt is the only thing that should change across wave siblings. Any static content appearing after `{{.Prompt}}` is a cache buster. Current templates already put `{{.Prompt}}` last but the stable prefix is ~200 tokens (far below the 1,024 minimum). | LOW | `internal/subagent/common/templates/*.tmpl` — modify template ordering |
| TS-C2 | **No volatile data in the stable prefix** — strip timestamps, UUIDs, request IDs, and per-dispatch metadata from the fixed portion of templates | Even one UUID injected into the boilerplate before `{{.Prompt}}` invalidates the entire prefix. Current templates include `{{.TaskUID}}` and `{{.Provider.Vendor}}` inline in the stable boilerplate — these are per-dispatch volatile data that break the prefix. | LOW | `internal/subagent/common/templates/*.tmpl` + `pkg/dispatch/EnvelopeIn` |
| TS-C3 | **Shared wave/plan context block as reusable stable prefix across wave-sibling dispatches** — wave siblings share the same plan-level context (phase brief, plan objective, milestone outcome); injecting this as a stable block before `{{.Prompt}}` lets siblings share a ≥1,024-token cached prefix | Wave siblings in the same plan all need the same plan context. If each subagent re-reads it cold, zero cache reuse. If it's the shared stable prefix (injected identically into all siblings), they all hit the same cache entry after the first dispatch warms it. | MEDIUM | Requires the dispatch layer to inject a `SharedContext` block from the owning Plan/Phase CRD into the template execution context |
| TS-C4 | **Deterministic context serialization** — any structured data (task lists, dependency lists, file sets) injected into the stable prefix must be serialized in sorted, deterministic order | JSON key ordering is part of the cache key. Non-deterministic serialization (e.g. Go map iteration, unordered JSON) produces different token sequences across dispatches → cache miss every time. | LOW | `pkg/dispatch/` + template data preparation code |
| TS-C5 | **Boilerplate audit and trim** — measure current token count per template; identify and remove redundant verbosity; target ≥1,024-token stable prefix, ≤30% overhead vs task-specific content | The TIDE-specific preamble ("TIDE (Topologically-Indexed Dependency Execution) runs hierarchical agentic coding work as a Milestone → Phase → Plan → Task → Wave DAG…") is repeated verbatim in all five templates. It counts as stable boilerplate — good for caching once padded — but should also be audited for density. Over-trimming degrades quality; under-trimming wastes tokens. The target is a stable prefix that is: (a) sufficient to hit the 1,024-token threshold, (b) semantically correct and complete for the level, (c) not padded with filler. | MEDIUM | All five templates + a token-counting utility |
| TS-C6 | **Token budget enforcement at template render time** — check rendered token count for the stable prefix portion before dispatch; log a warning if the stable prefix falls below 1,024 tokens (cache inactive) or if the total prompt exceeds a configurable per-level ceiling | Prevents silently spending full input-token rates when the template is too short to cache. Also prevents runaway context growth from verbose `SharedContext` injection. | LOW | `pkg/dispatch/` + `internal/subagent/common/prompt_templates.go` |
| TS-C7 | **Cache hit rate metric** — track `cache_read_input_tokens` and `cache_creation_input_tokens` from the `events.jsonl` envelope already written by the harness; compute cache hit rate = `cache_read / (cache_read + cache_creation + input)` per dispatch; expose as a Prometheus gauge | TIDE already emits `events.jsonl` with provider-emitted raw events. Cache token fields are already present in the usage data. Surfacing the hit rate as a metric closes the feedback loop — without measurement, there's no way to know if restructuring worked. | LOW | `internal/reporter/` + `events.jsonl` parsing already in place |
| TS-C8 | **Per-level token and cost accounting in the existing metrics** — `cache_read_tokens`, `cache_write_tokens`, `input_tokens`, `output_tokens` broken out by level (project/milestone/phase/plan/task) as Prometheus labels | v1.0.1 already ships raw token + cost metrics. The gap is per-level breakout: which level is spending the most? Planner levels tend to be smaller; executor levels accumulate tool-call context. Per-level labeling makes the expensive levels visible. | LOW | Extends existing Prometheus metrics in `internal/reporter/` |

### Differentiators (Competitive Advantage — Meaningful for v1.0.2)

| # | Feature | Value Proposition | Complexity | TIDE Dependency |
|---|---------|-------------------|------------|-----------------|
| D-C1 | **Wave-sibling prefix warm-up dispatch** — before dispatching wave N, emit a single no-content "warm-up" call (or use the first dispatch) that seeds the shared stable prefix into the provider's cache; subsequent siblings in the same wave are cache hits | Wave siblings are dispatched in parallel. With a cold cache, the first sibling to land warms it; all others race against the cache-write settling time. An explicit warm-up (or carefully sequenced first dispatch) seeds the cache before siblings arrive. For Anthropic, this could be a `max_tokens=0` probe if the CLI supports it; more practically, just ensure the first dispatch in each wave uses the identical stable prefix as all siblings. | HIGH | Requires orchestrator-level wave dispatch sequencing; the dispatcher would send one "seed" job first, then fan out siblings. Significant change to the wave dispatch loop. |
| D-C2 | **Context curation for plan-level shared context** — select and inject only task-relevant context into `SharedContext` (phase objective + plan DAG summary + sibling task summaries) rather than dumping the full `PLAN.md`; prefer summaries over verbatim content | Full `PLAN.md` dumps into the subagent context are a common anti-pattern in multi-agent systems. Token overhead is paid on every dispatch; most tasks only need a 3–5 sentence oriented summary. Curated context = lower token spend + tighter stable prefix = better cache hit rate + less distraction for the subagent. Research shows content quality matters more than presence — LLM-generated context additions produce only marginal gains (4%) at 20% token overhead. | MEDIUM | Requires a `SharedContext` builder in the dispatcher that summarizes rather than verbatim-includes |
| D-C3 | **Per-template token budget profiles configurable per level** — Helm values / Project CRD fields for `maxStablePrefix` and `maxTaskPrompt` per level | Milestone planners produce richer prompts than task executors. Allowing per-level token budgets prevents over-constraining executors (which need tool-call context to grow) while capping planner verbosity. | LOW | Helm values + CRD field + dispatch-time enforcement |
| D-C4 | **Cost + quality dashboard panel** — surface cache hit rate, tokens-per-level, cost-per-run, and cost-per-task-type as a dashboard panel; compare current run vs baseline | v1.0.1 ships token + budget metrics but they're not surfaced visually. A cost panel answers "did this template change reduce cost?" at a glance. Pairs with the eval harness (D-C5) for the feedback loop. | MEDIUM | Dashboard component; reads existing Prometheus metrics |
| D-C5 | **Quality + cost eval harness** — a reusable Go test package (`internal/eval/`) that: (a) runs a fixed set of canonical dispatch scenarios against the real CLI, (b) records output quality scores (LLM-as-judge rubric on structured JSON output), (c) records token/cost/cache metrics, (d) compares against a stored baseline, (e) fails CI if quality degrades beyond a threshold | This is the regression gate for all future prompt/template changes. Without it, every template edit is a manual quality check. With it, `make eval` catches regressions before they ship. The harness is the thing that makes "trim boilerplate" safe — you can verify quality is preserved. | HIGH | New `internal/eval/` package; depends on `events.jsonl` parsing + a golden-set fixture directory |
| D-C6 | **LLM-as-judge rubric for agentic coding task quality** — a structured JSON rubric applied by a judge LLM (separate from the task executor) scoring: completeness (did it produce all required files?), correctness (do produced artifacts satisfy the acceptance criterion?), instruction-following (did it follow the declared-output-paths contract?), and plan coherence (for planners: does the child-CRD set make sense?) | Research consensus: boolean scoring per dimension is more reliable than 1–10 scales; require evidence before score; target ≥0.80 Spearman correlation with human expert judgment for production deployment. The four dimensions map directly to TIDE's output contract (files, acceptance criteria, envelope contract, plan shape). | MEDIUM | Requires a judge LLM call in the eval harness; judge prompt should be a separate template with its own golden set |

### Anti-Features (Deliberately NOT in v1.0.2)

| # | Anti-Feature | Why Requested | Why Rejected | Alternative |
|---|--------------|---------------|--------------|-------------|
| AF-C1 | **Direct Anthropic SDK backend with explicit `cache_control`** | Would let TIDE set cache breakpoints at exactly the right location, giving full control over TTL and breakpoint placement without depending on CLI behavior | PROJECT.md v1.0.2 constraint: "Stay CLI-based — no direct-SDK `cache_control` subagent backend." The CLI path is the v1 contract; adding a direct SDK path this milestone expands scope significantly without proving the prompt-structuring approach first. | Stay CLI-based; verify that proper template structuring achieves ≥70% cache hit rate via metrics; SDK path is a future milestone if the gap proves unbridgeable. |
| AF-C2 | **Aggressive boilerplate removal that degrades output quality** | Shorter prompts = lower token cost per dispatch. | Quality is the constraint, not token count. Research on context trimming shows "implicit-contract loss" when instructions are trimmed below task-completion threshold. Eval harness (D-C5) must gate all trimming. "Best-effort reduction, quality-gated" is the milestone constraint. | Trim incrementally; run eval harness after each reduction; stop trimming when quality score drops. |
| AF-C3 | **Per-task prompt compression / summarization using a secondary LLM** | Prompt compression (e.g., LLMLingua-style) can reduce long task prompts by 5–10×. | Adds latency (a compression LLM call before every dispatch), cost (another LLM call), and a quality risk (compressed prompts lose precision). TIDE's task prompts are authored by the plan planner — they're already designed to be concise. Compression adds complexity for marginal gain. | Ensure plan-planner templates instruct the planner to write concise task prompts; the plan_planner.tmpl already says "Write it as if briefing an engineer." Enforce this at the prompt level, not with a runtime compressor. |
| AF-C4 | **KV-cache sharing across worktrees** (forcing all wave siblings to use the same working directory to share the Claude Code cache) | Claude Code's cache is scoped per working directory. If all siblings shared one directory, they'd share the cache prefix. | Violates the per-task worktree isolation model (TS-8 in v1 FEATURES.md). Worktrees are the isolation unit; sharing them for cache savings trades correctness for cost. | The correct solution is provider-level shared prefix caching (the prefix match is by content, not path — restructure the stable prefix to not include the path, or investigate the `prompt_cache_key` parameter for OpenAI path once that backend ships). |
| AF-C5 | **Per-task `events.jsonl` real-time streaming parsing for live cache hit feedback** | Would allow live dashboard updates showing cache hit/miss per turn within a running task. | TIDE's executor-level dispatch is single-turn for planner levels; multi-turn only for task executor sessions. The `events.jsonl` is already written; parsing it post-completion is sufficient for metrics. Real-time streaming adds significant complexity to the reporter pipeline for marginal dashboard value at this milestone. | Parse `events.jsonl` post-completion as today; surface aggregated metrics per dispatch and per-run. |
| AF-C6 | **OpenAI/Codex subagent backend** | Natural partner to the prompt-structuring work; would allow verifying provider-agnostic cache behavior. | Explicitly out of scope for v1.0.2 per PROJECT.md: "Out of scope (→ next milestone): the OpenAI/Codex subagent backend and dogfood run #2 itself." | Design prompt structuring to be provider-agnostic (it is, by the stable-prefix-first rule); verify on the OpenAI path in the next milestone. |
| AF-C7 | **Eval harness with human annotation pipeline** | LLM-as-judge calibrated against human expert annotations is more reliable. | Human annotation pipeline is infra-heavy (annotation UI, annotator pool, disagreement resolution) and out of scope for v1.0.2. The eval harness is a CI tool, not a research platform. | Ship with LLM-as-judge (D-C6); calibrate judge against a small hand-labeled golden set (10–20 canonical scenarios); defer annotation pipeline to post-v1.0.2. |
| AF-C8 | **Cache pre-warming service** (a persistent sidecar that re-issues warm-up calls before cache TTL expires) | Would keep the 5-minute cache warm indefinitely, effectively making all dispatches cache hits. | Adds a persistent process, requires tracking TTL per provider per model, and is Anthropic-path-specific (OpenAI cache TTL is not directly controllable). At TIDE's dispatch frequency, wave siblings arrive within 30–60 seconds — the cache stays warm naturally during an active run. | Rely on natural cache warming during active runs; accept cold-start cost on the first dispatch per wave. |

---

## Feature Dependencies

```
TS-C4 (deterministic serialization)
    └──requires──> TS-C3 (SharedContext injection)
                       └──requires──> TS-C1 (stable-prefix ordering)
                                          └──requires──> TS-C2 (no volatile data in prefix)

TS-C6 (token budget enforcement)
    └──requires──> TS-C5 (boilerplate audit)
                       └──requires──> TS-C1 (stable-prefix ordering)

TS-C7 (cache hit rate metric)
    ├──requires──> existing events.jsonl parsing (already in internal/reporter/)
    └──enhances──> D-C4 (cost+quality dashboard panel)

TS-C8 (per-level token accounting)
    └──enhances──> D-C4 (cost+quality dashboard panel)

D-C5 (eval harness)
    ├──requires──> TS-C7 (cache hit rate metric) — harness reports cache deltas
    ├──requires──> TS-C8 (per-level token accounting) — harness reports cost deltas
    └──requires──> D-C6 (LLM-as-judge rubric) — harness needs a quality scorer

D-C6 (LLM-as-judge)
    └──requires──> D-C5 (eval harness) — judge runs within the harness

D-C1 (warm-up dispatch)
    └──requires──> TS-C3 (SharedContext injection) — warm-up only makes sense if siblings share a stable prefix

D-C2 (context curation)
    └──enhances──> TS-C3 (SharedContext injection) — curation determines what goes in SharedContext

TS-C1, TS-C2, TS-C5 (template restructuring)
    └──gated-by──> D-C5 (eval harness) — don't ship template changes without regression gate
```

### Dependency notes

- **TS-C1 + TS-C2 are the foundation.** Every caching benefit depends on the stable
  prefix being truly stable (no volatile data). These are template edits — low complexity,
  but they must be done first.
- **TS-C3 (SharedContext) is the highest-leverage structural change.** Without it, wave
  siblings each start with a cold cache for the plan/phase context. With it, after the
  first sibling, all subsequent siblings in the same wave hit the cache for the largest
  portion of their input. This is where the token spend actually drops.
- **D-C5 (eval harness) must gate all template changes.** The correct sequence is:
  implement harness → establish baseline → restructure templates → verify quality preserved →
  ship. Doing template changes before the harness exists is unsafe.
- **D-C1 (warm-up dispatch) is HIGH complexity and depends on D-C5.** Do not build it
  without first verifying that the simpler TS-C1/C2/C3 changes don't already achieve
  sufficient cache hit rate.

---

## MVP Definition

### Launch With (v1.0.2 — "Ebb Tide")

Minimum viable cost optimization — sufficient to reduce token spend measurably while
proving quality is preserved.

- [ ] **TS-C1** — Stable-prefix-first ordering in all five templates — *why essential:
  zero cache benefit is achievable without this; it's the structural prerequisite for
  everything else*
- [ ] **TS-C2** — Remove `{{.TaskUID}}`, `{{.Provider.Vendor}}`, `{{.Provider.Model}}`
  from the stable boilerplate section of templates — *why essential: current templates
  include volatile dispatch metadata in the prefix, busting cache on every unique TaskUID*
- [ ] **TS-C5** — Boilerplate audit: measure token count per template; grow stable prefix
  to ≥1,024 tokens with the SharedContext block; document the template token budget — *why
  essential: caching simply does not activate below 1,024 tokens — TIDE's current ~200-token
  templates get zero cache benefit*
- [ ] **TS-C4** — Deterministic serialization for any structured data in the stable prefix
  — *why essential: non-deterministic Go map serialization produces different token sequences,
  busting cache silently*
- [ ] **TS-C7** — Cache hit rate metric from events.jsonl — *why essential: without
  measurement, there's no signal that restructuring worked*
- [ ] **TS-C8** — Per-level token accounting as Prometheus labels — *why essential:
  identifies which level to optimize first*
- [ ] **D-C5** — Eval harness (`internal/eval/`) with golden-set fixtures and baseline
  storage — *why essential: the quality gate that makes all template changes safe to ship*
- [ ] **D-C6** — LLM-as-judge rubric (four dimensions: completeness, correctness,
  instruction-following, plan coherence) — *why essential: harness is useless without a
  quality scorer*
- [ ] **TS-C3** — SharedContext injection block — plan objective + phase brief summary
  injected identically into all wave-sibling dispatches — *why essential: this is where
  the actual per-wave cache savings come from*

### Add After Validation (v1.0.2 extension)

Add these once the baseline is established and the eval harness is green.

- [ ] **D-C4** — Cost + quality dashboard panel — *trigger: TS-C7 + TS-C8 metrics exist;
  add visualization once the data is there*
- [ ] **D-C2** — Context curation (summaries over verbatim content in SharedContext) —
  *trigger: SharedContext (TS-C3) is working; tune what goes in it based on eval harness
  quality scores*
- [ ] **D-C3** — Per-level token budget profiles (Helm/CRD config) — *trigger: enough
  runs to know which levels need different budgets*

### Future Consideration (v1.0.3+)

- [ ] **D-C1** — Wave-sibling warm-up dispatch — *why defer: high complexity; only
  worthwhile if natural cache warming doesn't suffice at TIDE's dispatch cadence*
- [ ] **AF-C1 reversed** — Direct SDK backend with explicit `cache_control` — *why defer:
  proves CLI path first; SDK path unlocks explicit TTL control and 1-hour cache if needed*
- [ ] **AF-C6 reversed** — OpenAI/Codex backend + provider-agnostic cache verification
  — *scope-constrained to next milestone explicitly*

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| TS-C1 (stable-prefix ordering) | HIGH — prerequisite for all caching | LOW — template edits | P1 |
| TS-C2 (no volatile data in prefix) | HIGH — currently breaks all caching | LOW — template edits | P1 |
| TS-C5 (boilerplate audit, grow to 1,024 tokens) | HIGH — caching inactive below threshold | MEDIUM — requires token counting | P1 |
| D-C5 (eval harness) | HIGH — quality gate for all changes | HIGH — new package + golden sets | P1 |
| D-C6 (LLM-as-judge rubric) | HIGH — makes eval harness useful | MEDIUM — judge prompt + fixture labels | P1 |
| TS-C3 (SharedContext injection) | HIGH — the main per-wave savings source | MEDIUM — dispatcher changes | P1 |
| TS-C4 (deterministic serialization) | MEDIUM — silent cache buster | LOW — sort keys everywhere | P1 |
| TS-C7 (cache hit rate metric) | HIGH — measurement proves the work | LOW — parse existing events.jsonl | P1 |
| TS-C8 (per-level token accounting) | MEDIUM — identifies hot levels | LOW — extend existing metrics | P1 |
| D-C4 (cost+quality dashboard panel) | MEDIUM — visibility | MEDIUM — dashboard component | P2 |
| D-C2 (context curation) | MEDIUM — marginal savings over SharedContext | MEDIUM — curation logic | P2 |
| D-C3 (per-level token budget profiles) | LOW — tuning knob | LOW — Helm values + enforcement | P2 |
| D-C1 (wave warm-up dispatch) | MEDIUM — cache-hit guarantee for wave siblings | HIGH — dispatch loop change | P3 |

**Priority key:**
- P1: Required for v1.0.2 — directly delivers the milestone goal
- P2: Add after baseline established, within milestone
- P3: Future milestone

---

## Implementation Notes by Category

### Template Restructuring (TS-C1, TS-C2, TS-C5)

Current templates (`internal/subagent/common/templates/*.tmpl`) structure:

```
[TIDE preamble paragraph — ~200 tokens, stable]
[Dispatch metadata block with {{.TaskUID}}, {{.Provider.Vendor}}, {{.Provider.Model}} — volatile]
[Role-specific instructions — stable]
[HOW TO EMIT child CRDs block — stable]
Original prompt:
{{.Prompt}}
```

Target structure:

```
[Role declaration — stable, ~50 tokens]
[TIDE system context paragraph — stable, ~200 tokens]
[SharedContext block — stable for all siblings in the same wave, ~500-800 tokens]
  - Milestone outcome (1-2 sentences)
  - Phase objective (1-2 sentences)
  - Plan scope summary (3-5 sentences for plan/task levels; absent for project/milestone)
  - Sibling task list (for task executor: what other tasks exist in this wave, declared-output-paths)
[Role-specific instructions — stable for each level, ~200 tokens]
[Output contract / child-CRD format — stable for each level, ~200 tokens]
[Filesystem layout (task executor only) — stable, ~100 tokens]
Original prompt:
{{.Prompt}}
```

Target total stable prefix: ≥1,024 tokens across all five templates.
Target `{{.Prompt}}` suffix: the minimum per-task instruction, no shared context repeated.

The key changes:
1. Move `{{.TaskUID}}` out of the stable body — it can appear in a metadata header that's
   explicitly NOT part of the cached prefix (i.e., after `{{.Prompt}}`), or be removed
   entirely if the executor already reads it from `in.json`.
2. Move `{{.Provider.Vendor}}` and `{{.Provider.Model}}` to the volatile suffix or remove
   them (the subagent already knows its model; stating it in the prompt is redundant).
3. Add a `{{.SharedContext}}` template variable populated by the dispatcher from the
   owning Plan/Phase/Milestone CRD. This is the same across all siblings.

### Eval Harness Design (D-C5, D-C6)

The harness is a Go test package at `internal/eval/` with:

- **Golden scenarios directory** (`internal/eval/testdata/scenarios/`) — each scenario is:
  - `input.json` — an `EnvelopeIn` struct for one dispatch
  - `expected_output_schema.json` — the JSON schema the output must satisfy
  - `quality_criteria.json` — the four rubric dimensions with pass/fail thresholds
  - `baseline.json` — recorded token counts, cache metrics, quality scores from the
    approved baseline run

- **Harness runner** — dispatches each scenario through the real template rendering
  pipeline (not a mock); records actual output + token usage

- **LLM-as-judge scorer** — submits `(scenario_input, actual_output, quality_criteria)`
  to a judge LLM (separate Claude call, not the same dispatch); receives structured JSON
  with per-dimension boolean scores + evidence

- **Comparison and gate** — compares quality scores + token metrics against `baseline.json`;
  fails if any quality dimension regresses; reports token/cost delta vs baseline

- **CI integration** — `make eval` runs the harness; required green before merging any
  template or dispatcher change

Judge LLM rubric (four dimensions, boolean each):

| Dimension | Pass Criterion |
|-----------|---------------|
| Completeness | All declared-output-paths files were produced |
| Correctness | Produced artifacts satisfy the task acceptance criterion |
| Instruction-following | Output conforms to the envelope contract (JSON only in children/, no prose) |
| Plan coherence (planners) | Child CRDs form a valid, non-cyclic dependency graph |

### SharedContext Construction (TS-C3)

The dispatcher must build a `SharedContext` string that is:
1. **Identical** for all tasks in the same wave of the same plan (determinism required)
2. **Stable** across dispatch calls (no timestamps, no run IDs)
3. **Concise** — 500–800 tokens, not a verbatim PLAN.md dump

Content of SharedContext per level:

| Template level | SharedContext content |
|---------------|----------------------|
| `project_planner` | Project outcome statement only (already in `{{.Prompt}}`; SharedContext may be empty at this level) |
| `milestone_planner` | Milestone scope from Project outcome + project constraints summary |
| `phase_planner` | Milestone outcome + sibling phase names + declared dependency edges |
| `plan_planner` | Phase objective + plan scope + sibling plan names + file-touch constraints |
| `task_executor` | Plan objective + wave number + sibling task names + their declared-output-paths |

---

## Sources

### Provider caching mechanics (HIGH confidence — official docs)

- [Claude Code prompt caching official docs](https://code.claude.com/docs/en/prompt-caching) — the authoritative source on how Claude Code's three-layer cache structure works, subagent TTL behavior, what invalidates the cache, working directory scoping
- [Anthropic API prompt caching docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) — cache_control breakpoints, minimum token thresholds per model (verified: 1,024 for Sonnet/Opus, 4,096 for Haiku 4.5 and Opus 4.5/4.6), TTL options (5 min default, 1 hr opt-in), pricing multipliers
- [OpenAI API prompt caching guide](https://developers.openai.com/api/docs/guides/prompt-caching) — automatic prefix caching mechanics, 1,024-token minimum, 128-token increment granularity, prompt_cache_key parameter

### Prompt structuring for cache maximization (HIGH confidence — cross-verified)

- [KV-Cache aware prompt engineering](https://ankitbko.github.io/blog/2025/08/prompt-engineering-kv-cache/) — stable prefix / volatile suffix pattern, what content belongs where, concrete examples of cache-busting content (timestamps, UUIDs, dynamic personalization)
- [Prompt cache hit rate engineering 2026](https://agentmarketcap.ai/blog/2026/04/11/prompt-cache-hit-rate-engineering-2026) — measuring cache hit rate formula, what breaks cache hits, token layout playbook, case study (7% → 74% hit rate from prefix optimization)
- [OpenAI Prompt Caching 201 cookbook](https://developers.openai.com/cookbook/examples/prompt_caching_201) — multi-agent / parallel dispatch structuring, prompt_cache_key for routing stickiness, ~15 RPM per prefix limit

### Token minimization and context curation (MEDIUM confidence — WebSearch, multiple sources)

- [AI Agent Loop Token Costs: Context Constraints (Augment Code)](https://www.augmentcode.com/guides/ai-agent-loop-token-cost-context-constraints) — sliding-window context trimming, implicit-contract loss from over-trimming, must pair trimming with pinned state
- [Escaping the Context Bottleneck (arxiv 2604.11462)](https://arxiv.org/html/2604.11462v1) — active context curation with RL; human-curated context: 4% gain at 20% token overhead; quality matters more than presence
- [Contextual Memory Virtualisation (arxiv 2602.22402)](https://arxiv.org/pdf/2602.22402) — structurally lossless trimming: mechanical overhead (file dumps, base64, metadata) consumes most context tokens; streaming algorithm strips overhead while preserving message content
- [Stop Wasting Your Tokens (arxiv 2510.26585)](https://arxiv.org/html/2510.26585v2) — sub-agent returns 1,000–1,500 token condensed findings; orchestrator works with summaries, not full reasoning chains

### Eval harness patterns (MEDIUM confidence — WebSearch synthesis)

- [Building an Evaluation Harness for Production AI Agents (Towards Data Science)](https://towardsdatascience.com/building-an-evaluation-harness-for-production-ai-agents-a-12-metric-framework-from-100-deployments/) — 12-metric framework from 100+ deployments; self-improving loop: online eval flags failures → offline golden set grows
- [LLM-as-a-Judge: A Practical Guide (Towards Data Science)](https://towardsdatascience.com/llm-as-a-judge-a-practical-guide/) — boolean scoring more reliable than 1–10; require evidence before score; target ≥0.80 Spearman correlation with human expert for production
- [Rubric-Based Evaluations & LLM-as-a-Judge (Medium, Adnan Masood)](https://medium.com/@adnanmasood/rubric-based-evals-llm-as-a-judge-methodologies-and-empirical-validation-in-domain-context-71936b989e80) — structured JSON output with evidence citations, calibrated against human annotation, concrete rubric design for domain-specific contexts
- [Agent Evaluation Framework (Galileo)](https://galileo.ai/blog/agent-evaluation-framework-metrics-rubrics-benchmarks) — assesses task completion quality, tool selection rationale, planning effectiveness as distinct rubric axes

---

*Feature research for: TIDE v1.0.2 Ebb Tide — Token & Cost Optimization*
*Researched: 2026-06-15*
*Scope: NEW cost-optimization + eval features only; v1 features are in the prior FEATURES.md version (2026-05-12)*
