# Phase 20: SharedContext Injection + Cache Verification Spike - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-15
**Phase:** 20-SharedContext Injection + Cache Verification Spike
**Areas discussed:** Spike probe & pass bar, SharedContext content & curation source, Scope across levels, Contingency posture (+ a provider-neutrality area surfaced mid-discussion)

---

## Spike probe & pass bar (CACHE-01)

### Probe shape

| Option | Description | Selected |
|--------|-------------|----------|
| 2-pod real-API probe on dogfood cluster | ≥1,024-token identical-prefix prompt dispatched twice through the real credproxy on kind-tide-dogfood; read cache_read_input_tokens | ✓ |
| Single-process two-call probe | Two sequential `claude -p --bare` from one host; can't falsify per-pod-path hypothesis (false-positive risk) | |
| Offline / reasoning-only | Diff outbound request bytes; byte-identity ≠ observed cache hit | |

**User's choice:** 2-pod real-API probe on dogfood cluster.
**Notes:** Matches the project's observe-first / real-API discipline; directly observes whether sibling #2 gets a cross-pod hit.

### Pass bar

| Option | Description | Selected |
|--------|-------------|----------|
| cache_read>0 on sibling #2 = PASS; capture request-body diff on FAIL | PASS = cache_read_input_tokens>0 + net-negative cost; on FAIL tee+diff both pods' request prefixes to name the root cause → PROJECT.md | ✓ |
| cache_read>0 = PASS; record bare outcome on FAIL | Same PASS, but no request-body forensics — weaker basis for the reframe | |

**User's choice:** cache_read>0 on sibling #2 = PASS; request-body diff on FAIL.
**Notes:** The root-cause divergence (CWD / --add-dir / workspace), not "it didn't fire," is what gets recorded — basis for the contingency decision and future direct-SDK work.

---

## SharedContext content & curation source (CACHE-02 / CACHE-04)

### Curation source

| Option | Description | Selected |
|--------|-------------|----------|
| Parent planner emits one blob; controller stamps it identically | LLM curation once + guaranteed byte-identity; fits "planner authors children" grain | ✓ |
| Controller deterministically extracts from parent artifact | Byte-stable, zero extra tokens, but mechanical curation | |
| Dedicated summarization dispatch | Best curation, but +1 dispatch cost/latency | |

**User's choice:** Parent planner emits one blob; controller stamps it identically.

### Content

| Option | Description | Selected |
|--------|-------------|----------|
| Parent goal + constraints + sibling-set overview | Wave-scoped, ~300–700 tokens; may not alone clear floor | ✓ |
| Above + curated project-paradigm digest | Bigger prefix + wider reuse, but pulls run-wide content | |
| Minimal parent outcome only | Smallest; leans on contingency | |

**User's choice:** Wave-scoped — parent goal + load-bearing constraints + sibling-set overview.

### Carry path

| Option | Description | Selected |
|--------|-------------|----------|
| Field on parent EnvelopeOut → controller copies onto each child CRD | Singular by construction, reuses existing artifacts-as-truth flow, stored as input data | ✓ |
| Per-child field on each ChildCRDSpec | Identity becomes the LLM's fragile responsibility | |
| Sidecar artifact (children/_shared.json) | +1 protocol surface vs reusing EnvelopeOut | |

**User's choice:** Field on parent EnvelopeOut → controller copies onto each child CRD.

---

## Provider neutrality (surfaced mid-discussion — CACHE-05)

**User prompt:** "This seems very Anthropic focused and not generically extensible. It may be worth a quick search of OpenAI, Google, Amazon, Venice, etc. vendor documentation to determine what makes sense."

Ran parallel research across OpenAI, Google Gemini, AWS Bedrock, and DeepSeek/Mistral/xAI/Groq/Venice/Fireworks/Together. Synthesis recorded in CONTEXT.md code_context appendix.

| Option | Description | Selected |
|--------|-------------|----------|
| Pure stable-prefix text now; per-provider descriptors deferred | Plain ordered stable-prefix string, no markers/branches, verify on Claude path only; floors-vary table + marker/object-provider deferral recorded in PROJECT.md | ✓ |
| Add a per-provider capability descriptor now | MinPrefixTokens + mechanism flag wired per provider now; models unverified vendor behavior (scope creep) | |

**User's choice:** Pure stable-prefix text now; descriptors deferred.
**Notes:** Research validated stable-prefix ordering as the portable, marker-free lever (common case across vendors, not Anthropic-specific); confirmed floors vary 1,024–4,096 and that marker/object/key-param caching is unreachable by a text-only CLI dispatcher — those paths belong to the direct-SDK / multi-provider milestone.

---

## Scope across levels (CACHE-02/03 — re-posed provider-independent)

| Option | Description | Selected |
|--------|-------------|----------|
| Uniform on all planner envelopes; executor ignores | Single uniform code path, zero provider/level branching, matches criterion 2 literally | ✓ |
| Targeted: only planners whose children are planners | Per-level conditional for ~zero token saving | |

**User's choice:** Uniform on all planner envelopes; executor ignores (locked).

---

## Contingency posture (CACHE-01 reframe)

| Option | Description | Selected |
|--------|-------------|----------|
| Attempt in-phase normalization if contained; else record as follow-up | If the spike pinpoints a cheaply/safely normalizable divergence (cmd.Dir / --add-dir alias), fix in-phase + re-run; guard against isolation/chart changes; else scoped follow-up | ✓ |
| Ship + reframe to token-minimization-only; normalization is its own phase | Cleanest boundary, but leaves a possibly-easy Claude-path cache win on the table | |

**User's choice:** Attempt in-phase normalization if contained; else scoped follow-up.
**Notes:** Aligns with the project's fix-the-root-cause posture; the SharedContext field ships either way and PROJECT.md records the outcome.

---

## Claude's Discretion

- Exact throwaway ≥1,024-token probe-prompt construction (deterministic, obviously cache-eligible).
- Concrete wording/format of the curated SharedContext blob (deterministic byte-identical, wave-scoped).
- Whether the spike reuses the Phase 18 `make eval` credproxy plumbing or a dedicated probe target.
- Exact JSON field naming on EnvelopeOut / child CRD spec for the carry path.
- The credproxy-side vs CLI-side mechanism for teeing outbound request bodies for the FAIL diff.

## Deferred Ideas

- Per-provider cache capability descriptor (floor + mechanism flag) — multi-provider milestone.
- Gemini explicit CachedContent lifecycle + Bedrock cachePoint injection — direct-SDK backend (CACHE-F1).
- Per-provider usage normalizer for the eval harness — before run-#2.
- OpenAI prompt_cache_key routing hint (with the ~15 req/min overflow caveat) — OpenAI-backend milestone.
- Wave-sibling warm-up dispatch — COST-F2.
- Project-paradigm digest in SharedContext — considered, set aside (wave-scoped chosen); revisit if floors hard to clear.
- CLI-prefix normalization as its own phase — only if the spike's divergence is large/risky (D-08 guard).
