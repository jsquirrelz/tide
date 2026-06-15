# Phase 20: SharedContext Injection + Cache Verification Spike - Context

**Gathered:** 2026-06-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Two coupled deliverables. **(1) CACHE-01 verification spike:** empirically settle
whether the stable-prefix-first ordering (Phase 19) yields cross-pod
prefix-cache hits across wave siblings under `claude -p --bare`. **(2)
SharedContext field (CACHE-02–05):** `EnvelopeIn` gains an additive
`SharedContext string` (omitempty; executor path ignores it) that the controller
populates **byte-identically for all wave siblings**, hoisting curated shared
parent context into the stable prefix to grow it toward the cache floor — or,
if the spike shows caching can't fire on the Claude path, as best-effort token
minimization. The SharedContext field ships **either way** (ROADMAP criterion 2
is unconditional); the spike decides cache-benefit-vs-token-minimization framing
and is recorded as a decision in PROJECT.md.

This phase clarifies HOW. WHAT it must achieve is locked by CACHE-01–05 and the
five ROADMAP success criteria. New capabilities — per-level dashboard
observability (Phase 21, OBSV-01–03), a direct-SDK `cache_control` backend
(CACHE-F1), OpenAI/Codex live parity (run-#2 milestone), and wave-sibling
warm-up dispatch (COST-F2) — are out of scope here.

**Load-bearing reality (Phase 18/19 + this phase's multi-vendor research):**
today's rendered prefix is ~200–500 tokens, far below every provider's cache
floor (1,024–4,096), so a *real* wave dispatch cannot show a cache hit yet
regardless of cross-pod scoping. The spike therefore probes with a **synthetic
≥1,024-token identical-prefix prompt** to isolate the genuine unknown: does the
`claude` CLI emit byte-identical prefix bytes across pods, or does it inject a
per-pod working-directory / `--add-dir` path that makes each sibling's prefix
unique? Cross-process cache reuse itself is the documented norm across vendors
(no API cache key includes client-side working-dir/machine/session) — so this is
a **CLI-behavior question, not a caching-fundamentals one**.

</domain>

<decisions>
## Implementation Decisions

### Verification spike (CACHE-01)
- **D-01: 2-pod real-API probe on the durable dogfood cluster.** Build a
  throwaway ≥1,024-token **identical-prefix** prompt and dispatch it twice — two
  pods (or two sequential dispatches) within the 5-minute TTL — through the
  **real credproxy** on the durable `kind-tide-dogfood` cluster, then read
  `cache_read_input_tokens` from the result event / `events.jsonl`. Highest
  fidelity: directly observes whether sibling #2 gets a cross-pod hit. Rejected
  single-process two-call (same CWD/`--add-dir` → can't falsify the per-pod-path
  hypothesis, risks a false positive) and offline-reasoning-only (byte-identity
  ≠ observed cache hit).
- **D-02: Pass bar = `cache_read_input_tokens > 0` on sibling #2 (within TTL)
  AND net-negative realized cost vs no-cache. On FAIL, tee + diff both pods'
  outbound request bodies at the credproxy** to name the exact divergence (CWD?
  `--add-dir` path? per-pod workspace id?). That **root cause** — not a bare
  "it didn't fire" — is what gets recorded in PROJECT.md, and it's the basis for
  the contingency decision (D-08) and any future CACHE-F1 direct-SDK work.

### SharedContext curation & content (CACHE-02 / CACHE-04)
- **D-03: Parent planner emits one curated blob; controller stamps it
  identically.** The parent planner dispatch (which already authors the child
  specs) ALSO emits a single curated `SharedContext` string for the whole wave;
  the controller writes that one blob **byte-identically** into every sibling's
  `EnvelopeIn.SharedContext`. Gets both goals — LLM-quality curation **once** +
  guaranteed byte-identity for caching — and fits TIDE's "planner authors
  children" grain. Rejected deterministic controller-extraction (curation is
  mechanical) and a dedicated summarizer dispatch (extra cost/latency for what
  CACHE-04 frames as a summary).
- **D-04: Content is wave-scoped — parent goal + load-bearing constraints +
  sibling-set overview** (a one-line map of what the other plans/phases in this
  wave cover, so each sibling knows its place in the DAG). Curated by the
  planner that just authored the siblings; "verbatim PLAN/phase-brief dumps" are
  rejected by CACHE-04. Realistically ~300–700 tokens — may not alone clear the
  floor, which the spike + contingency settle. Rejected adding a project-paradigm
  digest (broader cache reuse, but pulls run-wide content the wave doesn't need)
  and minimal-outcome-only (too small).

### Carry path (envelope contract)
- **D-05: Field on parent `EnvelopeOut` → controller copies onto each child
  CRD → renders into child `EnvelopeIn.SharedContext`.** Parent emits one
  `sharedContext` on its `TaskEnvelopeOut` (the existing return channel the
  controller already reads on Job completion). Controller writes that string
  into each child CRD's spec at creation; at dispatch it renders into the child's
  `EnvelopeIn.SharedContext`. Singular by construction (one field → identity
  guaranteed), reuses the artifacts-as-truth flow, and stored on the child spec
  as **input data** (not a schedule cache — does not violate the "rederive,
  don't cache the schedule" rule). Rejected per-child `ChildCRDSpec` field
  (identity becomes the LLM's fragile responsibility) and a sidecar
  `children/_shared.json` (+1 protocol surface vs reusing `EnvelopeOut`).

### Provider neutrality (CACHE-05)
- **D-06: Pure stable-prefix text field now; per-provider descriptors deferred.**
  `SharedContext` is a plain ordered stable-prefix string on the
  provider-agnostic `EnvelopeIn` — **no markers, no provider conditionals**.
  Live-verified on the Claude CLI path only (CACHE-05). The ≥1,024 target is
  documented as *"clears the active model's documented floor,"* **never a
  hardcoded constant** — the per-provider floor TABLE (1,024–4,096; see
  code_context) is recorded in PROJECT.md as a known input. The actual
  per-provider capability descriptor, Gemini explicit-cache-object lifecycle, and
  Bedrock `cachePoint` injection are **deferred to the OpenAI/multi-provider
  milestone** (matches CACHE-05 + the existing CACHE-F1 deferral). Rejected adding
  a `CacheCaps` descriptor now — models OpenAI/Gemini/Bedrock behavior before any
  is live-verified (scope creep against "parity deferred to run-#2").

### Level scope (DAG plumbing — provider-independent)
- **D-07: Uniform on all planner envelopes; executor ignores.**
  `BuildPlannerEnvelope` populates `SharedContext` at every planner level
  (project/milestone/phase/plan); the executor path never renders it (CACHE-02
  lock). The plan-planner→task emission is a harmless no-op (executors ignore the
  field), but the code path stays single and uniform — matches criterion 2's
  literal "populates it identically for all tasks in a wave," carries zero
  provider/level branching, and keeps the field provably provider-neutral.
  Whether a given wave clears its model's floor is an observability/eval question
  (Phase 21), not a plumbing one. Rejected targeted emission (a per-level
  conditional for ~zero token saving + a branch future provider work must reason
  about).

### Contingency (CACHE-01 reframe)
- **D-08: Ship SharedContext regardless; attempt in-phase normalization if
  contained, else scoped follow-up.** If the spike's request-body diff (D-02)
  pinpoints a **cheaply- and safely-normalizable** divergence — e.g. set
  `cmd.Dir` identically across pods, or use a fixed/aliased `--add-dir` path so
  the CLI's system prefix is byte-identical — **attempt that fix within Phase 20**
  and re-run the spike to recover real Claude-path cross-pod cache hits (the win
  the milestone is chasing). **Guard:** only if contained — no per-task pod
  isolation-contract violation, no `charts/tide/values.yaml` change; if the fix
  is large/risky/touches isolation or the chart, record it as a **scoped
  follow-up** instead of expanding the phase mid-flight. Either way the
  SharedContext field ships and PROJECT.md records the outcome. (Aligns with the
  project's "fix the root cause, don't defer" posture; the guard keeps the spike
  a spike.)

### Claude's Discretion
- Exact construction of the throwaway ≥1,024-token probe prompt (filler shape /
  how the identical prefix + small unique tail is generated) — keep it
  deterministic and obviously cache-eligible.
- Concrete wording/format of the curated SharedContext blob (so long as it is
  deterministic byte-identical across siblings per D-03, and stays wave-scoped
  per D-04).
- Whether the spike harness reuses the existing `make eval` credproxy plumbing
  (Phase 18) or a dedicated probe target — researcher/planner call.
- Exact JSON field naming on `EnvelopeOut`/child CRD spec for the carry path
  (D-05), consistent with existing `pkg/dispatch` naming conventions.
- The mechanism that tees outbound request bodies at the credproxy for the FAIL
  diff (D-02) — credproxy-side capture vs CLI-side; whichever is least invasive.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (locked scope)
- `.planning/REQUIREMENTS.md` — CACHE-01–05 (this phase) + binding constraints
  (CLI-based `claude -p --bare`; NO direct-SDK `cache_control`; don't rebuild
  cost accounting). Out-of-Scope table: `cache_control` markers from TIDE,
  forced shared worktree, OpenAI/Codex backend. Future: CACHE-F1 (direct-SDK
  breakpoints), COST-F2 (warm-up dispatch).
- `.planning/ROADMAP.md` §"Phase 20: SharedContext Injection + Cache
  Verification Spike" — 5 success criteria; criterion 2 (SharedContext ships)
  is **unconditional**; criterion 1 mandates the spike decision in PROJECT.md.

### Phase 19 dependency (the stable-prefix structure this phase grows)
- `.planning/phases/19-template-reorder-token-minimization/19-CONTEXT.md` —
  **D-07 reserved the zero-token `{{- /* SharedContext slot — populated in
  Phase 20 (CACHE-02/03) */ -}}` marker** between fixed instructions and the
  volatile suffix: Phase 20's clean insertion point. D-02 kept `.Provider` on
  the struct; D-03 = canonical section order. Templates render via
  `tmpl.Execute`.

### Code (ground truth — files this phase edits/reads)
- `pkg/dispatch/envelope.go` — `EnvelopeIn` (add `SharedContext string`
  json:"sharedContext,omitempty"`) and `EnvelopeOut` (add the parent-emitted
  `sharedContext` carry field per D-05). `TaskEnvelopeIn`/`TaskEnvelopeOut`
  Kind discriminators + `ValidateAPIVersionKind` contract live here.
- `internal/controller/dispatch_helpers.go:218` — `BuildPlannerEnvelope`: the
  uniform population point (D-07). Mirror struct: `dispatch_helpers_test.go`.
- `internal/controller/task_controller.go` — `buildEnvelopeIn` (executor path;
  must NOT render SharedContext per CACHE-02 lock); child-CRD creation from
  parent `EnvelopeOut` (the D-05 stamp point).
- `internal/subagent/anthropic/subagent.go:285–305` — the `claude -p --bare`
  invocation (args, stdin-delivered prompt, `--add-dir eventsDir` with per-task
  UID, credproxy env wiring). **The spike's primary subject** — what could make
  the per-pod prefix diverge.
- `internal/subagent/common/templates/{milestone,project,phase,plan}_planner.tmpl`
  — reference `{{.SharedContext}}` in the D-07 reserved slot (planner templates
  only). `internal/subagent/common/prompt_templates.go` renders them.
- `internal/eval/` + `make eval` (Phase 18) — the `count_tokens` / credproxy
  plumbing the spike can reuse; the goldie golden renders + per-template token
  ratchet the SharedContext template edits must keep green.

### Research (HIGH confidence)
- `.planning/research/SUMMARY.md` — templates < cache floor → caching never
  fires today; cross-pod scoping is "most critical, verify before Phase 3";
  realized savings only across wave siblings within TTL.
- `.planning/research/PITFALLS.md` — CLI-vs-SDK no-`cache_control` lever;
  cache-write premium net-negative on one-shot dispatches.
- **Multi-vendor caching research (this phase, 2026-06-15)** — see code_context
  appendix. Establishes stable-prefix ordering as the portable lever and the
  per-provider floor table; load-bearing for CACHE-05 and the run-#2 milestone.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Phase 18 `make eval` + credproxy plumbing** — the live `count_tokens` /
  signed-token credproxy path on `kind-tide-dogfood`; the spike (D-01) reuses
  this to dispatch real `claude -p --bare` calls and read usage.
- **Phase 18 eval gate (goldie golden renders + per-template token ratchet)** —
  every SharedContext template edit must keep it green and ratchet deliberately.
- **D-07 reserved slot in all four planner templates** — Phase 20 fills it; no
  template restructuring needed, just `{{.SharedContext}}` interpolation.
- **`EnvelopeOut` → child-CRD creation flow** — the existing artifacts-as-truth
  channel D-05 extends with one carry field; no new protocol surface.

### Established Patterns
- `omitempty` additive envelope fields with executor-path-ignores semantics
  (e.g. `PromptPath`, `Branch` are executor-only; `SharedContext` is the
  inverse — planner-only) — the precedent for D-06/D-07's additive, no-branch
  field.
- Per-section gated-green commits + deliberate ratchet/golden updates (Phase
  18/19) — the SharedContext template edits follow the same discipline.

### Integration Points
- `BuildPlannerEnvelope` (uniform populate, D-07) and `buildEnvelopeIn`
  (executor, leaves SharedContext empty) — the two envelope builders.
- The credproxy is the FAIL-diff capture point (D-02): outbound request bodies
  from two pods tee'd + diff'd to name the divergence.

### Multi-vendor prompt-caching research appendix (2026-06-15)

Authoritative findings behind D-06's "pure stable-prefix text, defer descriptors"
and the contingency framing. **Core conclusion: automatic byte-exact prefix
caching that hits across separate processes — controllable purely by
stable-content-first ordering — is the COMMON CASE, not the Anthropic exception.
No vendor's API cache key includes client-side working-dir/machine/session.**

| Provider | Mechanism | Min floor | Cross-process | CLI-text-only reachable? | Cached-token field |
|---|---|---|---|---|---|
| **OpenAI** (4o/4.1/o-/GPT-5.x) | Automatic prefix, org-scoped | 1,024 (+128-tok incr) | YES, explicitly process/machine-independent | YES (marker-free) | `usage.prompt_tokens_details.cached_tokens` |
| **Anthropic** (direct API) | Explicit `cache_control` breakpoints | 1,024 Sonnet/Opus · 4,096 Haiku | YES (prefix-keyed) | NO — marker in request body, CLI owns it | `cache_read_input_tokens` / `cache_creation_input_tokens` |
| **Google Gemini** (implicit) | Automatic prefix, best-effort | 1,024 Flash · 2,048 Pro | Partial — routing/datacenter-dependent (Vertex+pinned-region best) | YES but unreliable | `usageMetadata.cachedContentTokenCount` |
| **Google Gemini** (explicit) | `CachedContent` object + name | 1,024/2,048 | YES (shared by opaque name) | NO — needs object lifecycle | same field |
| **AWS Bedrock** (Claude) | Explicit `cachePoint` blocks | 1,024 or **4,096** (newest Claude) | YES, account-scoped | NO — `cachePoint` in request body | `cacheReadInputTokens`/`cacheWriteInputTokens` (note: `inputTokens` EXCLUDES cached) |
| **AWS Bedrock** (Nova) | Auto (latency) / explicit (cost) | not stated | YES, account-scoped | latency-only without marker | same |
| **DeepSeek** | Automatic disk-based prefix | none (64-tok chunks) | YES | YES (marker-free) | `prompt_cache_hit_tokens`/`prompt_cache_miss_tokens` |
| **Mistral** | Explicit `prompt_cache_key` param | 64 tok | only with same key | NO — needs request param | `usage.prompt_tokens_details.cached_tokens` |
| **xAI Grok** | Auto, routing via `x-grok-conv-id` | not stated | routing-dependent | YES (best-effort) | `usage` cached_tokens |
| **Groq** | Automatic prefix | not stated | YES (recent requests) | YES (marker-free) | `usage` |
| **Venice** | Auto (Claude=auto-injected markers) | not stated | YES, byte-exact prefix | YES for most | response cache stats |
| **Fireworks** (serverless) | Automatic longest-prefix | not stated | YES | YES (marker-free) | per-model usage |
| **Together** (dedicated only) | Auto, per-endpoint KV reuse | not stated | scoped to your endpoint | n/a serverless | not documented |

**Design implications baked into decisions:**
1. Floors vary **1,024–4,096** per provider/model → never hardcode 1,024 (D-06).
2. Marker-required (Anthropic-direct, Bedrock) / object-required (Gemini-explicit)
   / key-param-required (Mistral) caching is **unreachable by a text-only CLI
   dispatcher** — confirms TIDE's "CLI owns cache_control" finding as a
   *multi-vendor* property; those paths are the direct-SDK milestone (CACHE-F1).
3. Stable-prefix `SharedContext` pays off marker-free on OpenAI, DeepSeek, Groq,
   Venice, Fireworks-serverless, Gemini-implicit (best-effort) → the right
   provider-neutral default (D-06).
4. Reporting fields diverge → the reusable eval harness needs a per-provider
   usage normalizer before run-#2 (note for the multi-provider milestone; some
   providers report cached-only, Bedrock's `inputTokens` excludes cached).

*Sources:* OpenAI prompt-caching guide; ai.google.dev/gemini-api/docs/caching +
implicit-caching blog; docs.aws.amazon.com/bedrock prompt-caching; DeepSeek
kv_cache; Mistral/xAI/Groq/Venice/Fireworks/Together official docs (captured in
the discussion research, 2026-06-15).

</code_context>

<specifics>
## Specific Ideas

- **Spike probe shape (illustrative):**
  ```
  probe prompt = [≥1,024-token identical filler/preamble]  ← the cacheable prefix
              + [small per-dispatch unique tail]            ← forces a real generation
  dispatch #1 (pod A) → expect cache_creation_input_tokens > 0
  dispatch #2 (pod B, < 5 min later) → cache_read_input_tokens > 0 ?  ← the verdict
  verdict source: result event `usage` block in events.jsonl
  FAIL path → tee + diff pod-A vs pod-B outbound request prefix bytes at credproxy
  ```
- **Carry path (illustrative):**
  ```
  parent Job done → EnvelopeOut{ children:[...], sharedContext:"<curated>" }
  controller: for each child_i → child CRD spec.sharedContext = sharedContext
  child dispatch → EnvelopeIn.SharedContext = spec.sharedContext   (identical bytes ∀ i)
  planner template: {{.SharedContext}} renders into the D-07 reserved slot
  executor template: never references SharedContext (CACHE-02 lock)
  ```
- **SharedContext content (wave-scoped, D-04):** parent goal (curated) +
  load-bearing constraints (curated, not verbatim) + one-line sibling map
  (e.g. "This wave: 4 plans — [01 auth, 02 api, 03 ui, 04 docs]").

</specifics>

<deferred>
## Deferred Ideas

- **Per-provider cache capability descriptor** (min-prefix floor +
  auto/marker/object mechanism flag) — OpenAI/multi-provider milestone, not now
  (D-06). Models unverified vendor behavior; pulls in scope REQUIREMENTS defers.
- **Gemini explicit `CachedContent` lifecycle** (create-at-rising-tide → name →
  TTL → inject into siblings) and **Bedrock `cachePoint` injection** — the
  guaranteed-cache paths for marker/object providers; require a direct-SDK
  backend (CACHE-F1), unreachable from the CLI path.
- **Per-provider usage normalizer for the eval harness** — cached-token field
  names + total-token semantics diverge across vendors (Bedrock `inputTokens`
  excludes cached); needed before run-#2 live multi-provider eval.
- **OpenAI `prompt_cache_key` routing hint** (with its ~15 req/min-per-key
  overflow ceiling — note the *counter-intuitive* sharding-under-fanout caveat) —
  OpenAI-backend milestone.
- **Wave-sibling warm-up dispatch** (pre-warm the shared prefix before fan-out) —
  COST-F2, future milestone.
- **Project-paradigm digest in SharedContext** (run-wide stable content for
  broader cache reuse) — considered and set aside (D-04 chose wave-scoped); could
  revisit if floors prove hard to clear with wave-scoped content alone.
- **CLI-prefix normalization as its own phase** — only if the spike's divergence
  (D-02) turns out large/risky (D-08 guard); the contained case is handled
  in-phase.

### Reviewed Todos (not folded)
None — no pending todos matched this phase (`todo.match-phase 20` → 0 matches).

</deferred>

---

*Phase: 20-SharedContext Injection + Cache Verification Spike*
*Context gathered: 2026-06-15*
