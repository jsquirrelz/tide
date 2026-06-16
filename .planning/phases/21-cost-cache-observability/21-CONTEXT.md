# Phase 21: Cost & Cache Observability - Context

**Gathered:** 2026-06-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Make per-level token spend and cache efficiency **visible to operators** on the
read-only dashboard, observing the results of the Phase 18–20 work in a running
cluster. The phase reads the **existing** Prometheus counters and adds exactly
**one** new emitted metric (realized cache savings, in cents) computed in the
accounting rollup — **no dispatch-path changes** (OBSV-03).

**Load-bearing reality (Phase 20 CACHE-01 spike — see PROJECT.md decision
record):** cross-pod prefix caching *does fire* under `claude -p --bare`, but
only on the CLI's own ~1.1–1.3k-token tool/system scaffold; caller-controlled
content (TIDE templates + SharedContext) is re-created every dispatch and never
cross-pod cache-reads. So today's hit-rate and savings are **structurally low
and dominated by the CLI scaffold**, not TIDE's SharedContext work. The caller-
content cache payoff is deferred to the direct-SDK backend (CACHE-F1). The
panel surfaces the numbers **as measured** (raw, no inline caveat — D-04).

**Already built — this phase does NOT rebuild (Phase 16 Telemetry Completion):**
- All six metrics (`tide_tokens_input_total`, `_output_total`,
  `_cache_read_total`, `_cache_creation_total`, `tide_cost_cents_total`,
  `tide_task_duration_seconds`) are registered with the **locked label set
  `{project, phase, plan, wave}`** (`internal/metrics/registry.go:185-227`).
- `pkg/dispatch.Usage` already parses `CacheReadTokens` / `CacheCreationTokens`
  (`envelope.go:267-301`); `emitTaskMetrics()` already increments all six
  counters (`task_controller.go:1044-1099`).
- TelemetryView already charts input/output/cache-read/cache-creation as a
  stacked "Token Breakdown" time-series panel (`TelemetryView.tsx:186-226`).

So OBSV-01's per-level labels **already exist**; the genuinely-new surface is a
derived hit-rate, an emitted realized-savings counter, a cache-efficiency panel,
and per-level slicing in the UI.

**Out of scope (own phases / deferred):** the direct-SDK `cache_control` backend
that would realize caller-content caching (CACHE-F1); any change to how
dispatches are made; OpenAI/multi-provider usage normalization (run-#2
milestone); per-Task/Phase/Plan CRD `.status` token fields (the Prometheus
labels already attribute per level — no CRD status work needed).

</domain>

<decisions>
## Implementation Decisions

### Hit-rate derivation (OBSV-02)
- **D-01: Split — emit a *savings* counter, but derive the *hit-rate ratio* in
  PromQL.** A ratio does not aggregate as a counter (you cannot `sum()` ratios
  across pods/levels), so the correct Prometheus idiom is to emit the components
  as counters — already done (`_cache_read_total`, `_cache_creation_total`) —
  and divide at query time in the panel. Criterion 2 allows "`tide_cache_hit_rate`
  **or equivalent**"; the PromQL-derived ratio is the equivalent. Initially we
  scoped hit-rate as fully PromQL-derived; the user **corrected this for the
  dollar figure only** (see D-02) — the unitless ratio stays PromQL.
  - Hit-rate panel query (illustrative):
    ```
    sum(increase(tide_tokens_cache_read_total{project="..."}[$w]))
    /
    sum(increase(tide_tokens_cache_read_total{project="..."}[$w])
      + increase(tide_tokens_cache_creation_total{project="..."}[$w]))
    ```

### Realized savings as an emitted metric (OBSV-02 / OBSV-03)
- **D-02: Provider-computed savings cents, emitted as a counter — mirrors the
  existing `tide_cost_cents_total` flow.** "Realized savings" is a *dollar*
  figure and the **controller has no pricing**, but the **provider does**
  (`internal/subagent/anthropic/pricing.go`). So compute it where pricing lives,
  behind the D-C1 provider firewall, exactly as `estimatedCostCents` is today;
  carry it on `Usage` as an additive `CacheSavingsCents` field; emit
  `tide_cache_savings_cents_total{project,phase,plan,wave}` in `emitTaskMetrics()`
  next to cost. This needs **no frontend price constant, no chart knob, no
  model-guessing**, and satisfies criterion 2's literal "metric emitted via the
  Prometheus surface" better than a PromQL counterfactual would. It is **not a
  dispatch-path change** — it is accounting in the existing reconcile rollup, so
  it honors OBSV-03.
  - Computation (per the locked 0.10× read / 1.25× write rule):
    ```go
    savingsCents = CacheReadTokens *
        (inputCentsPerMTok - cacheReadCentsPerMTok) / 1_000_000   // = 0.90 × input rate
    ```
  - Provider-neutral by construction: each backend computes its own
    `CacheSavingsCents`; the controller just emits the counter (no Anthropic
    branch in the controller).
  - **One counter, not two** — rejected also emitting a separate
    `tide_cache_read_cost_cents_total`: savings = 9 × cache-read-cost given the
    fixed 0.10× rate, so the second counter is largely redundant. (Reconsider in
    planning only if per-class cost attribution proves needed.)
- **Rejected: PromQL-derived dollar savings in the dashboard.** The dashboard is
  pure PromQL with **no pricing source**, and `tide_cost_cents_total` is a single
  aggregate (input+output+read+write summed), so PromQL cannot isolate cache-read
  cost to back into a rate. A frontend price constant (hardcoded or chart-config)
  would be wrong on any model change. Emitting where pricing already lives avoids
  the whole problem.

### Formula (OBSV-02 / OBSV-03)
- **D-03: Hit ratio = `cache_read / (cache_read + cache_creation)`; realized
  savings = `cache_read_tokens × 0.90 × input_price`** (the 90% discount on
  reads, matching `pricing.go`'s 0.10× read rate). Read-vs-write framing of the
  ratio; savings computed backend-side per D-02. `cache_creation` tokens are
  shown as their own raw stat.

### Panel framing (OBSV-03)
- **D-04: Raw metrics only — no inline Phase-20 caveat in the panel.** Render hit
  ratio, cache-creation tokens, and realized savings as measured; do **not** add
  an inline footnote explaining the CLI-scaffold reality. Keeps the panel simple
  and consistent with the other (caveat-free) TelemetryView panels; the Phase-20
  context lives in PROJECT.md / this CONTEXT for operators who need it.
  *(Trade-off acknowledged: a low hit-rate could be misread as "TIDE caching
  broken" rather than "caller-content caching awaits CACHE-F1" — accepted by the
  user for simplicity.)*

### Panel form (OBSV-03)
- **D-05: Single-stat trio + hit-rate sparkline.** Three glanceable stats for the
  selected window — hit ratio %, cache-creation tokens, realized savings $ (from
  `tide_cache_savings_cents_total`) — plus a small hit-ratio sparkline over time.
  Complements the existing detailed stacked "Token Breakdown" chart rather than
  duplicating it. Rejected a full hit-rate time-series panel (less glanceable for
  current-state numbers; the trio reads at a glance).

### Per-level attribution surfacing (OBSV-01)
- **D-06: Add a per-level breakdown UI to TelemetryView.** The labels already
  make spend queryable per level via PromQL (criterion 1 literally satisfied at
  the backend), but the user wants operators to *slice in the browser*: add a
  phase/plan/wave breakdown/selector (today TelemetryView scopes only
  project/all). This is grouped PromQL (`sum by(phase|plan|wave)(...)`) over the
  existing counters — no new metric, no backend change. Pairs an
  audit/regression test asserting the label arity is intact (so "queryable
  without additional instrumentation" stays true) with the new UI.

### Claude's Discretion
- Exact JSON field name for the new `Usage` carry field (`CacheSavingsCents` vs
  similar), consistent with the existing `EstimatedCostCents` naming.
- Exact Prometheus metric name/help text for the savings counter
  (`tide_cache_savings_cents_total` suggested), matching the `_total` /
  cents-counter conventions of `tide_cost_cents_total`.
- Whether the savings cents is computed in `estimatedCostCents` itself (returning
  both) or a sibling helper in `pricing.go` — whichever keeps the provider
  firewall clean and the math testable.
- Concrete React shape of the single-stat trio + sparkline (which existing
  TelemetryView panel/component to clone; how the level selector composes with
  the existing scope/range controls).
- Whether the per-level breakdown (D-06) is a new dropdown on the existing panels
  or a dedicated breakdown panel — planner's call against the `PANELS` array
  conventions.
- PromQL window/rate functions for the hit-rate ratio (`increase` vs `rate`),
  matching existing panel query style.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (locked scope)
- `.planning/REQUIREMENTS.md` §"Cost & Cache Observability (OBSV)" — OBSV-01
  (per-level token accounting queryable), OBSV-02 (cache-hit-rate metric derived
  from `cache_read` vs `cache_creation`), OBSV-03 (read-only dashboard
  cache-efficiency panel, **no backend dispatch-path changes**).
- `.planning/ROADMAP.md` §"Phase 21: Cost & Cache Observability" — 3 success
  criteria. Note the mild tension between criterion 2 ("gauge emitted") and
  criterion 3 / OBSV-03 ("read existing counters, no dispatch-path changes"),
  resolved by D-01/D-02: emit a savings *counter* in the accounting rollup
  (not the dispatch path); derive the hit-*ratio* in PromQL.

### Phase 20 outcome (the cache reality this phase visualizes)
- `.planning/PROJECT.md` §"CACHE-01 decision record (Phase 20)" — cross-pod
  caching fires only on the CLI scaffold; caller content never cache-reads on
  the CLI path; cache benefit deferred to CACHE-F1. **This is why hit-rate is
  structurally low** and frames what the panel actually shows.
- `.planning/phases/20-sharedcontext-injection-cache-verification-spike/20-CONTEXT.md`
  — the per-provider cache-floor table and `cache_read`/`cache_creation` usage
  field mapping (Anthropic `cache_read_input_tokens` →
  `Usage.CacheReadTokens`).

### Code (ground truth — files this phase edits/reads)
- `internal/metrics/registry.go:185-227` — the six locked counters + label set
  `{project, phase, plan, wave}`. Add `CacheSavingsCentsTotal` here (D-02),
  mirroring `CostCentsTotal` (registry.go:213-219). Tests:
  `internal/metrics/registry_test.go` (label-arity assertions — extend for the
  new counter and keep the OBSV-01 arity guard).
- `internal/controller/task_controller.go:1044-1099` — `emitTaskMetrics()`, the
  single emission point. Add the savings `.Add()` next to the cost emission
  (line ~1072), labeled identically. **Do not touch the dispatch path.**
- `pkg/dispatch/envelope.go:267-301` — `Usage` struct; add the additive
  `CacheSavingsCents int64` field next to `EstimatedCostCents` (D-02). It is the
  carry channel from provider → controller.
- `internal/subagent/anthropic/pricing.go` — `priceTable` (per-model
  `inputCentsPerMTok` / `cacheReadCentsPerMTok`) and `estimatedCostCents`
  (pricing.go:132-157). Compute savings here behind the provider firewall.
  Tests: `pricing_test.go`, `cost_parity_test.go`, `internal/eval/cost_replay_test.go`.
- `dashboard/web/src/components/TelemetryView.tsx` — the `PANELS` array
  (134-227, existing "Token Breakdown" at 186-226 is the cache-token reference),
  the `query_range` data path (~255), the scope/range controls (~768), and the
  budget card (597-688). Add the cache-efficiency trio + sparkline panel (D-05)
  and the per-level breakdown (D-06).
- `cmd/dashboard/router.go:179-180` + `cmd/dashboard/api/prometheus.go:55-137` —
  the PromQL proxy (`/api/v1/query`, `/api/v1/query_range`) the dashboard reads
  through; degradation contract returns `{"status":"unavailable"}` when no
  Prometheus is configured. No new route needed (panels are PromQL).

### Chart / metrics exposure
- `charts/tide/templates/servicemonitor.yaml` + `charts/tide/values.yaml:306+` —
  the `prometheus.serviceMonitor.enabled=false`-by-default scrape surface. **No
  PrometheusRule surface exists today** (only ServiceMonitor) — relevant because
  D-01 deliberately avoids a recording rule (derive in panel instead).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Existing cost-emission flow** (`tide_cost_cents_total`): the exact pattern
  D-02 clones — provider computes cents (`estimatedCostCents`) → carried on
  `Usage.EstimatedCostCents` → emitted in `emitTaskMetrics()` with
  `{project,phase,plan,wave}`. Add savings as a parallel field + counter.
- **Existing "Token Breakdown" panel** (`TelemetryView.tsx:186-226`): already
  queries `tide_tokens_cache_read_total` / `tide_tokens_cache_creation_total` —
  the cache-token reference and the hit-rate numerator/denominator source.
- **PromQL proxy + degradation contract** (`api/prometheus.go`): panels work
  through the existing `query_range` proxy; graceful `unavailable` when no
  Prometheus — the new panel must tolerate this like the others.
- **`registry_test.go` label-arity assertions**: extend for the savings counter;
  these also serve as the OBSV-01 "queryable per level, no extra instrumentation"
  audit guard (D-06).

### Established Patterns
- **`omitempty` additive `Usage`/envelope fields** (e.g. `EstimatedCostCents`):
  the precedent for `CacheSavingsCents` — additive, provider-populated.
- **Provider firewall (D-C1 / CLAUDE.md anti-pattern):** all pricing math lives
  in `internal/subagent/anthropic/`; the controller never imports a price table.
  D-02's savings computation respects this exactly.
- **Locked label set `{project,phase,plan,wave}`; `task` label FORBIDDEN**
  (Pitfall 17, enforced by the metriccardinality analyzer). The savings counter
  uses the same four labels — no `task`.

### Integration Points
- Provider → controller: `Usage.CacheSavingsCents` (new field).
- Controller → Prometheus: `emitTaskMetrics()` (new `.Add()` next to cost).
- Prometheus → dashboard: existing `query_range` proxy; new PromQL in the panel.

</code_context>

<specifics>
## Specific Ideas

- **Cache-efficiency panel (D-05, raw per D-04):**
  ```
  ┌ Cache Efficiency ───────────────┐
  │   11.9%     12,296 tok    $0.04  │
  │   hit       creation      saved  │
  │   ▁▂▃▅▇▆▅   hit-rate over time   │
  └─────────────────────────────────┘
  hit       = cache_read / (cache_read + cache_creation)   [PromQL]
  creation  = increase(tide_tokens_cache_creation_total)   [PromQL]
  saved $   = increase(tide_cache_savings_cents_total)/100  [emitted counter, D-02]
  ```
- **Savings computation (D-02/D-03):**
  ```
  savingsCents = CacheReadTokens × (inputCentsPerMTok − cacheReadCentsPerMTok) / 1e6
               = CacheReadTokens × inputCentsPerMTok × 0.90 / 1e6   (0.10× read rate)
  ```
- **Per-level slice (D-06):** `sum by(phase)(increase(tide_cost_cents_total{project="X"}[$w]))`,
  same for `tokens_input/output/cache_*` and the savings counter — grouped by
  phase | plan | wave via a TelemetryView selector.

</specifics>

<deferred>
## Deferred Ideas

- **CACHE-F1 — direct-SDK backend to realize caller-content cross-pod caching**
  (sets the system prompt explicitly, no per-request `cch` nonce, places
  `cache_control` on the shared prefix). This is what would make the hit-rate and
  savings numbers actually reflect TIDE's SharedContext work. It is a
  **dispatch-path/backend change — explicitly out of scope for Phase 21**
  (OBSV-03). Future direct-SDK milestone.
- **Per-provider usage normalizer** (cached-token field names + total-token
  semantics diverge; Bedrock `inputTokens` excludes cached) — needed before
  run-#2 live multi-provider eval, not now.
- **Second cost counter `tide_cache_read_cost_cents_total`** (actual spend on
  reads, alongside savings) — set aside as largely redundant (savings = 9× read
  cost at the fixed 0.10× rate); revisit only if per-token-class cost attribution
  is requested.
- **Per-Task/Phase/Plan CRD `.status` token/cost fields** — not needed: the
  Prometheus labels already attribute spend per level. Avoid duplicating
  accounting into CRD status (etcd-size discipline).
- **Inline panel caveat explaining the CLI-scaffold cache reality** — considered
  and set aside (D-04 chose raw metrics); revisit if operators misread the low
  hit-rate.

### Reviewed Todos (not folded)
- **`cache-f1-direct-sdk-cross-pod-caching.md`** (todo.match-phase score 0.6) —
  reviewed, **not folded**. It is the CACHE-F1 direct-SDK caching backend, a
  dispatch-path change that Phase 21 explicitly excludes (OBSV-03). Belongs to
  the future direct-SDK milestone, not this observability phase.

</deferred>

---

*Phase: 21-Cost & Cache Observability*
*Context gathered: 2026-06-15*
