# Phase 21: Cost & Cache Observability — Research

**Researched:** 2026-06-15
**Domain:** Prometheus counter extension (Go) + React dashboard panel (TypeScript/Vitest)
**Confidence:** HIGH — all claims verified against live code with file:line anchors

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Hit-rate ratio is PromQL-derived (`cache_read / (cache_read + cache_creation)`) in
  the panel. No separate gauge metric for the ratio.
- **D-02:** Provider computes `CacheSavingsCents` behind the provider firewall
  (`internal/subagent/anthropic/pricing.go`), carried on `pkg/dispatch.Usage` as a new additive
  `CacheSavingsCents int64` field, and emitted as `tide_cache_savings_cents_total
  {project,phase,plan,wave}` counter in `emitTaskMetrics()`. **Not a dispatch-path change.**
- **D-03:** Hit ratio = `cache_read / (cache_read + cache_creation)`; realized savings =
  `CacheReadTokens × (inputCentsPerMTok − cacheReadCentsPerMTok) / 1_000_000` (= 90% of
  input rate, matching the 0.10× read rate in `pricing.go`).
- **D-04:** Raw metrics only — no inline Phase-20 caveat in the panel.
- **D-05:** Cache-efficiency panel = single-stat trio (hit ratio %, cache-creation tokens,
  realized savings $) + hit-rate sparkline. Complements the existing Token Breakdown chart.
- **D-06:** Per-level breakdown UI in TelemetryView via a phase/plan/wave selector on top of
  grouped PromQL (`sum by(phase|plan|wave)(...)`). No new metric; the four-label set already
  supports this. Paired with a registry arity regression test (OBSV-01 audit guard).

### Claude's Discretion

- Exact JSON field name for the carry field (`CacheSavingsCents` suggested).
- Exact Prometheus metric name/help text (`tide_cache_savings_cents_total` suggested).
- Whether savings is computed in `estimatedCostCents` itself or a sibling helper in `pricing.go`.
- Concrete React shape of the single-stat trio + sparkline (which component to clone).
- Whether D-06 per-level breakdown is a new dropdown on existing panels or a dedicated panel.
- PromQL rate functions (`increase` vs `rate`), matching existing panel style.

### Deferred Ideas (OUT OF SCOPE)

- CACHE-F1: direct-SDK backend with explicit `cache_control` (dispatch-path change).
- Per-provider usage normalizer for multi-provider / OpenAI run-#2.
- Second cost counter `tide_cache_read_cost_cents_total` (redundant at 0.10× rate).
- Per-Task/Phase/Plan CRD `.status` token/cost fields.
- Inline panel caveat explaining the CLI-scaffold cache reality.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OBSV-01 | Per-level token accounting queryable (project/phase/plan/wave labels) without additional instrumentation | Already satisfied at backend by the Phase 16 label set; D-06 surfaces it in the UI + arity regression test |
| OBSV-02 | Cache-hit-rate metric derived from `cache_read` vs `cache_creation`, emitted via existing Prometheus surface | D-01: ratio in PromQL; D-02: new `tide_cache_savings_cents_total` counter is the emitted metric; D-03: savings formula |
| OBSV-03 | Read-only dashboard cache-efficiency panel (hit ratio, creation tokens, realized savings) — **no backend dispatch-path changes** | D-05 panel; savings counter is accounting-path-only, not dispatch-path |
</phase_requirements>

---

## Summary

Phase 21 is primarily an observability surface phase — almost all the accounting machinery was built in Phase 16 (Telemetry Completion). The work divides into three concrete additions: (1) one new Go counter with its carry field and pricing computation, (2) one new React panel with a single-stat trio and sparkline, and (3) a per-level grouping selector on TelemetryView.

**The six existing TELEM-03 metrics** (`tide_tokens_input_total`, `_output_total`, `_cache_read_total`, `_cache_creation_total`, `tide_cost_cents_total`, `tide_task_duration_seconds`) are confirmed live at `internal/metrics/registry.go:185-227` with the locked four-label set `{project, phase, plan, wave}`. The label set already satisfies OBSV-01 at the backend level — PromQL `sum by(phase)` works today. The one genuinely new emitted metric is `tide_cache_savings_cents_total`, computed in the Anthropic provider's `pricing.go`, carried on `pkg/dispatch.Usage.CacheSavingsCents`, and emitted in `emitTaskMetrics()` next to `CostCentsTotal`.

The dashboard half is additive to the existing `TelemetryView.tsx` `PANELS` array. Four patterns are well-established: `PanelDef` shape, `buildQuery` scope/window parameterization, `formatCents()` for dollar display, `fetchQueryRange` graceful degradation, and `vitest` + `@testing-library/react` for component tests. The per-level breakdown (D-06) can be implemented as a new segmented control that switches the `by(...)` clause on existing panel queries, or as a dedicated breakdown panel added to `PANELS`.

**Primary recommendation:** Add `CacheSavingsCents` as a sibling computation alongside `estimatedCostCents` in a new `cacheSavingsCents(model, usage)` helper in `pricing.go` (keeps the math isolated and testable); add the carry field to `Usage`; register `CacheSavingsCentsTotal` in `registry.go` mirroring `CostCentsTotal`; emit in `emitTaskMetrics()` with one new `.Add()` call; add the D-05 single-stat panel and D-06 level selector in `TelemetryView.tsx`.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Savings computation | Provider (internal/subagent/anthropic) | — | Provider firewall (D-C1/CLAUDE.md): controller has no price table |
| Usage carry field | pkg/dispatch envelope | — | Carry channel from provider → controller; already established for `EstimatedCostCents` |
| Counter emission | Controller (task_controller.go) | — | Single emission point `emitTaskMetrics()`; already handles all 6 TELEM-03 counters |
| Counter registration | internal/metrics/registry.go | — | init() pattern; MustRegister on controller-runtime registry |
| Hit-rate ratio | Dashboard PromQL layer | — | D-01: ratio cannot aggregate as counter; PromQL-derived at query time |
| Cache-efficiency panel | Dashboard (TelemetryView.tsx) | — | OBSV-03 read-only surface; no new API route |
| Per-level breakdown | Dashboard (TelemetryView.tsx) | — | Grouped PromQL `sum by(...)` + scope/range controls already in TelemetryView |
| PromQL proxy | cmd/dashboard/api/prometheus.go | — | Existing routes GET /api/v1/query + /api/v1/query_range; no new route |

---

## Standard Stack

### Already Built — No New Dependencies

This phase adds no new Go or npm packages. All required tools are already present:

| Component | Location | Version/Status |
|-----------|----------|----------------|
| prometheus/client_golang | go.mod | v1.23+ (already used by registry.go) |
| controller-runtime metrics.Registry | go.mod | v0.24.x |
| recharts | dashboard/web/package.json | already used by TelemetryView |
| vitest | dashboard/web/package.json | v1.6.1 |
| @testing-library/react | dashboard/web/package.json | v16.3.2 |

**Installation:** None required. No new packages.

## Package Legitimacy Audit

> No external packages are being added in this phase. This section is N/A.

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

---

## Architecture Patterns

### System Architecture Diagram

```
[Task Pod] → streams usage (CacheReadTokens, CacheCreationTokens)
                 ↓
[anthropic/subagent.go:345] calls estimatedCostCents(model, usage)
                 ↓
[anthropic/pricing.go] ← NEW: cacheSavingsCents(model, usage)
  returns Usage{..., EstimatedCostCents, CacheSavingsCents}
                 ↓
[pkg/dispatch.Usage] carry field (CacheSavingsCents int64, omitempty)
                 ↓
[task_controller.go:emitTaskMetrics()] ← NEW: .Add(CacheSavingsCents)
                 ↓
[internal/metrics/CacheSavingsCentsTotal] ← NEW counter {project,phase,plan,wave}
                 ↓
[Prometheus scrape]
                 ↓
[cmd/dashboard/api/prometheus.go] proxy GET /api/v1/query + /api/v1/query_range
                 ↓
[TelemetryView.tsx PANELS] ← NEW: cache-efficiency panel (D-05) + level selector (D-06)
  hit ratio = PromQL: cache_read / (cache_read + cache_creation)
  savings $  = formatCents(increase(tide_cache_savings_cents_total)/100)
  creation   = increase(tide_tokens_cache_creation_total)
```

### Recommended Project Structure

No new directories needed. Edits are in-place additions to existing files:

```
internal/
├── metrics/registry.go          # add CacheSavingsCentsTotal var + registration
├── metrics/registry_test.go     # add arity test + presence test + NoTaskLabel pass
├── controller/task_controller.go # add .Add(usage.CacheSavingsCents) in emitTaskMetrics()
└── subagent/anthropic/
    ├── pricing.go                # add cacheSavingsCents() helper
    └── pricing_test.go           # add test cases for savings formula

pkg/
└── dispatch/envelope.go          # add CacheSavingsCents int64 to Usage

dashboard/web/src/components/
├── TelemetryView.tsx             # add cache-efficiency panel + level selector
└── __tests__/TelemetryView.test.tsx  # extend test suite
```

### Pattern 1: Counter Registration (clone of CostCentsTotal)

**What:** Package-level `*prometheus.CounterVec` var, constructed in `init()`, registered via `metrics.Registry.MustRegister`.

**When to use:** Every new TELEM-03-class metric.

**Example (from `internal/metrics/registry.go:213-219`):**
```go
// [VERIFIED: live code at registry.go:213-219]
CostCentsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_cost_cents_total",
        Help: "Estimated cost in US cents consumed by Tasks (Phase 16 TELEM-03).",
    },
    []string{"project", "phase", "plan", "wave"},
)
```

Clone pattern for new savings counter:
```go
// Add after CostCentsTotal var declaration (registry.go:111)
var CacheSavingsCentsTotal *prometheus.CounterVec

// In init(), after CostCentsTotal construction:
CacheSavingsCentsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_cache_savings_cents_total",
        Help: "Realized cache savings in US cents (input price minus cache-read price) per Task (Phase 21 OBSV-02).",
    },
    []string{"project", "phase", "plan", "wave"},
)

// In metrics.Registry.MustRegister(...) block, add:
// CacheSavingsCentsTotal,
```

### Pattern 2: Usage Carry Field (clone of EstimatedCostCents)

**What:** Additive `int64` field on `pkg/dispatch.Usage` with `omitempty` JSON tag, populated by the provider, carried to the controller.

**When to use:** Any provider-computed accounting value that flows to the controller's emission.

**Example (from `pkg/dispatch/envelope.go:280-282`):**
```go
// [VERIFIED: live code at envelope.go:280-282]
EstimatedCostCents int64 `json:"estimatedCostCents"`
```

New field to add after `EstimatedCostCents`:
```go
// CacheSavingsCents is the realized savings in US cents for this task:
// CacheReadTokens × (inputCentsPerMTok − cacheReadCentsPerMTok) / 1_000_000.
// Computed by the provider (provider firewall D-C1), zero when CacheReadTokens is zero.
CacheSavingsCents int64 `json:"cacheSavingsCents,omitempty"`
```

Note: `EstimatedCostCents` itself lacks `omitempty` — CONTEXT.md explicitly notes `CacheSavingsCents` should be additive/omitempty. Using `omitempty` keeps executor `out.json` documents small (zero when no cache reads occurred).

### Pattern 3: Provider-Side Cost Computation

**What:** Helper in `internal/subagent/anthropic/pricing.go` that reads the per-instance `a.prices` table (never the package-level `priceTable` — T-14-02).

**When to use:** Any new pricing calculation behind the provider firewall.

**Example (from `pricing.go:132-157`):**
The existing `estimatedCostCents` uses integer arithmetic with ceiling division. The savings computation is simpler (no ceiling needed — savings can be zero for a task with zero cache reads):

```go
// cacheSavingsCents returns the realized cache savings in US cents
// for the given model and usage. Savings = CacheReadTokens × (inputRate − readRate) / 1e6.
// The 0.90× factor comes from the locked 0.10× read rate in the price table.
// Returns 0 when CacheReadTokens is zero (no reads → no savings).
// Never negative: readRate is always ≤ inputRate in the priceTable invariant.
func (a *Anthropic) cacheSavingsCents(model string, u pkgdispatch.Usage) int64 {
    if u.CacheReadTokens == 0 {
        return 0
    }
    price, ok := a.prices[model]
    if !ok {
        price = conservativeTier
    }
    savings := u.CacheReadTokens * (price.inputCentsPerMTok - price.cacheReadCentsPerMTok)
    return savings / 1_000_000
}
```

Key note: Unlike `estimatedCostCents`, no ceiling division is needed here — truncation toward zero is correct for savings (conservative, never overstates the saving). The insertion point is immediately after the `estimatedCostCents` call at `subagent.go:345`:

```go
// subagent.go:345 (existing):
usage.EstimatedCostCents = a.estimatedCostCents(in.Provider.Model, usage)
// ADD after:
usage.CacheSavingsCents = a.cacheSavingsCents(in.Provider.Model, usage)
```

### Pattern 4: emitTaskMetrics() Emission (clone of CostCentsTotal)

**What:** Single `.Add()` call in `emitTaskMetrics()` with the same four labels.

**Example (from `task_controller.go:1072`):**
```go
// [VERIFIED: live code at task_controller.go:1072]
tidemetrics.CostCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.EstimatedCostCents))
// ADD after:
tidemetrics.CacheSavingsCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheSavingsCents))
```

### Pattern 5: TelemetryView PanelDef (for cache-efficiency panel)

**What:** A `PanelDef` entry appended to the `PANELS` array at `TelemetryView.tsx:134`.

**Current PANELS array:** 4 entries — "cost-over-time", "dispatch-counts", "failure-rate", "token-breakdown".

**Key PanelDef fields:**
- `id` — used as `data-testid="panel-{id}"` (test harness relies on this)
- `label` — header text (uppercase via CSS)
- `series[]` — array of `SeriesDef` with `buildQuery(scope, project, window)`
- `yFormatter` — tick/tooltip formatter
- `stacked?` — boolean for stacked area chart
- `failureColor?` — boolean for red palette

**formatCents helper** (`TelemetryView.tsx:232-234`):
```typescript
// [VERIFIED: live code at TelemetryView.tsx:232-234]
function formatCents(cents: number): string {
  return "$" + (cents / 100).toFixed(2);
}
```
The savings counter is in cents (integer); to display as dollars: `formatCents(Math.round(v))`. This is exactly how "Cost Over Time" formats its values at `TelemetryView.tsx:138`.

**D-05 cache-efficiency panel — single-stat trio + sparkline:**

The existing `TimeSeriesChart` renders recharts `AreaChart` — it works for sparklines. For the single-stat display (three glanceable numbers), the planner must decide whether to:

(a) Add a new sub-component (e.g., `CacheEfficiencyCard`) that renders three stat numbers above a small sparkline of the hit-rate over time, mounted inside a `ChartPanel`-style container, OR
(b) Add the panel as a `PanelDef` with the sparkline and let the `ChartPanel` / `TimeSeriesChart` render it — but note `TimeSeriesChart` is a time-series chart, not a stat trio natively.

**Recommendation:** Add a `CacheEfficiencyPanel` sub-component that fetches three instant values (for the stat trio: hit ratio, creation tokens, savings $) via `/api/v1/query` + one range series (for the sparkline) via `/api/v1/query_range`. The instant query path already exists in `prometheus.go:64-66` (`PrometheusHandler.Query` → `/api/v1/query`). The existing `fetchQueryRange` helper in `TelemetryView.tsx:240-294` handles `query_range`; a parallel `fetchInstant` helper would hit `GET /api/v1/query?query=...&time=...`.

The panel should be inserted into a grid cell alongside the existing PANELS rendering, or integrated into the PANELS array if the implementation can represent stat trios in the `PanelState` union type. The planner has discretion here (CONTEXT.md "Claude's Discretion").

**D-06 per-level breakdown:**

Current scope control is `ScopeKind = "project" | "all"`. The per-level selector adds a breakdown dimension: `BreakdownKind = "none" | "phase" | "plan" | "wave"`. When breakdown is active, queries change from:
```
sum(increase(tide_cost_cents_total{project="X"}[$w]))
```
to:
```
sum by(phase)(increase(tide_cost_cents_total{project="X"}[$w]))
```

The existing key-derivation logic in `fetchPanel` (`TelemetryView.tsx:336-353`) already handles per-project splitting by reading `matrix.metric["project"]`. Extending it to split by `matrix.metric["phase"|"plan"|"wave"]` follows the same pattern. Add a `BreakdownKind` state variable and a new segmented control (`SegmentedControl` component already at `TelemetryView.tsx:701-743`).

### Pattern 6: Registry Test (for new counter)

**What:** Two tests must be added to `registry_test.go`:
1. Add `CacheSavingsCentsTotal` to `TestRegistry_AllMetricFamiliesPresent` seed and `want` slice.
2. Add `TestRegistry_CacheSavingsCentsLabelArity` mirroring `TestRegistry_CostCentsLabelArity`.

Seed pattern (4 labels, same as all TELEM-03 counters):
```go
tidemetrics.CacheSavingsCentsTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
```

Want slice addition:
```go
"tide_cache_savings_cents_total",
```

### Anti-Patterns to Avoid

- **Pricing in the controller:** `emitTaskMetrics()` must never import price rates. It only reads `usage.CacheSavingsCents` (already computed by the provider). The provider firewall (D-C1) is enforced by `tools/analyzers/providerfirewall` and `CLAUDE.md`.
- **`task` label in new counter:** `metriccardinality` AST analyzer rejects `"task"` in any label slice. The savings counter uses `{project, phase, plan, wave}` — identical to the other five TELEM-03 counters.
- **Mutating the package-level `priceTable`:** `cacheSavingsCents` must read `a.prices` (the per-instance clone), never `priceTable` directly (T-14-02). Follow the same pattern as `estimatedCostCents` at `pricing.go:133`.
- **Changing the dispatch path for OBSV-03:** The savings computation is in the task completion accounting path (`subagent.go` post-`ParseStream`), not in the Job creation or queue path.
- **Ceiling division for savings:** Unlike `estimatedCostCents`, savings truncation (not ceiling) is appropriate. Never overstate savings.
- **Frontend price constants:** The dashboard must not hardcode price rates. Savings are read from the emitted counter, which is already in cents.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Cents → dollars display | Custom formatter | `formatCents()` at `TelemetryView.tsx:232` | Already tested; consistent with Cost Over Time panel |
| PromQL proxy | New HTTP handler | `/api/v1/query_range` + `/api/v1/query` routes at `router.go:179-180` | Already registered; degradation already handled |
| Savings pricing | Price constants in dashboard | `tide_cache_savings_cents_total` counter (provider computes) | Provider firewall; dashboard has no model price list |
| Instant query fetch | New fetch wrapper | Clone `fetchQueryRange` → `fetchInstant` using `/api/v1/query` | Same degradation contract, same PrometheusHandler |
| PromQL hit-rate gauge | New Go metric | PromQL `sum(increase(cache_read[...])) / (sum(increase(cache_read[...])) + sum(increase(cache_creation[...])))` | Ratios don't aggregate; D-01 locked |

---

## File:Line Anchor Verification

All claims verified against live code. Corrections to the briefing doc follow:

### registry.go — CONFIRMED, with clarification

- `internal/metrics/registry.go:185-227` — **CONFIRMED.** `TokensInputTotal` is defined at line 185; `TaskDurationSeconds` histogram closes at line 227. The six TELEM-03 vars are declared as package-level `*prometheus.CounterVec` / `*prometheus.HistogramVec` at lines 98-115 (declarations) and constructed at 185-227 (init body). The `MustRegister` call is at lines 229-244.
- **New counter insertion point:** Declare `CacheSavingsCentsTotal *prometheus.CounterVec` after `CostCentsTotal` declaration (after line 111). Construct after `CostCentsTotal` construction (after line 219). Add to `MustRegister` list (inside lines 229-244 block).

### envelope.go — CONFIRMED, with note on omitempty

- `pkg/dispatch/envelope.go:267-301` — **CONFIRMED.** `Usage` struct starts at line 270; `CacheReadTokens` at 289; `CacheCreationTokens` at 300; struct closes at 301.
- **Note:** `EstimatedCostCents` at line 282 does NOT have `omitempty` (unlike the `CacheSavingsCents` briefing says it should). This is intentional — every task should always report a cost. For savings, `omitempty` is correct because zero savings (no cache hits) is the common case and need not be serialized.

### task_controller.go — CONFIRMED, with exact line numbers

- `task_controller.go:1044-1099` — **CONFIRMED.** `emitTaskMetrics()` signature is at line 1044. The five TELEM-03 counter `.Add()` calls are at lines 1068-1072:
  - Line 1068: `TokensInputTotal`
  - Line 1069: `TokensOutputTotal`
  - Line 1070: `TokensCacheReadTotal`
  - Line 1071: `TokensCacheCreationTotal`
  - Line 1072: `CostCentsTotal`
- **New `.Add()` insertion point:** After line 1072, add `CacheSavingsCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheSavingsCents))`.
- `TaskDurationSeconds` observe call is at line 1081.
- Function ends at line 1099.

### pricing.go — CONFIRMED

- `internal/subagent/anthropic/pricing.go:132-157` — **CONFIRMED.** `estimatedCostCents` method starts at line 132. The actual computation (numerator sum and ceiling division) is at lines 145-156. Function ends at line 157.
- **Savings insertion point in subagent.go:** `usage.EstimatedCostCents = a.estimatedCostCents(in.Provider.Model, usage)` is at line 345. Add `usage.CacheSavingsCents = a.cacheSavingsCents(in.Provider.Model, usage)` immediately after.

### TelemetryView.tsx — CONFIRMED, with layout notes

- **Lines 1-33:** File-level comment (correct per the briefing).
- **`PANELS` array: lines 134-227** — **CONFIRMED.** `const PANELS: PanelDef[] = [` at line 134; array closes at line 227 (`];`). The "Token Breakdown" panel is at lines 187-226. The "cache read" series in Token Breakdown queries `tide_tokens_cache_read_total` (line 214) and `tide_tokens_cache_creation_total` (line 221).
- **`fetchQueryRange` function: lines 240-294** — **CONFIRMED.** Takes `query, startSec, endSec, step`; calls `GET /api/v1/query_range`.
- **`formatCents` function: lines 232-234** — **CONFIRMED.** `"$" + (cents / 100).toFixed(2)`.
- **Scope/range controls: lines 845-933** — The range selector options (`rangeOptions`) are built at line 845; scope toggle at line 851. The rendering is in the JSX block starting line 857.
- **Budget card: lines 597-688** — **CONFIRMED.**
- **`PANELS.map` render loop: line 988-990** — Current loop renders all PANELS as `ChartPanel`. New cache-efficiency panel (D-05) that requires a stat trio + sparkline hybrid will need either a new branch in the render loop or a new panel type.

### router.go — CONFIRMED

- `cmd/dashboard/router.go:179-180` — **CONFIRMED.** `r.Get("/query", promHandler.Query)` at line 179; `r.Get("/query_range", promHandler.QueryRange)` at line 180.

### prometheus.go — CONFIRMED

- `cmd/dashboard/api/prometheus.go:55-137` — **CONFIRMED.** `PrometheusHandler` struct at line 55. `Query` handler (instant) at line 64; `QueryRange` at line 70. Degradation sentinel (`{"status":"unavailable"}`) returned at lines 81-83 when `h.Endpoint == ""`. `HTTP 502` returned for unreachable upstream at lines 111-116.

---

## Common Pitfalls

### Pitfall 1: task Label in New Counter
**What goes wrong:** `metriccardinality` AST analyzer (run during `make lint`) rejects `"task"` string literal in any `NewCounterVec` / `NewHistogramVec` label slice. The check is literal-only (string constant bypasses it — documented escape hatch) but the correct fix is to not use `task`.
**Why it happens:** Temptation to add per-task attribution when debugging.
**How to avoid:** Use `{project, phase, plan, wave}` — identical to all other TELEM-03 counters. The `TestRegistry_NoTaskLabel` test also catches this at test time.
**Warning signs:** `make lint` fails with `metriccardinality: "task" label forbidden`.

### Pitfall 2: Provider Firewall Violation
**What goes wrong:** `tools/analyzers/providerfirewall` rejects any import of `internal/subagent/anthropic` in `internal/controller` or `pkg/dispatch`. Also enforced by `verify-import-firewall` in `make lint`.
**Why it happens:** Trying to compute savings in the controller for convenience.
**How to avoid:** The savings computation MUST live in `internal/subagent/anthropic/pricing.go`. The controller only reads `usage.CacheSavingsCents` — a plain `int64`.
**Warning signs:** `make lint` fails with `providerfirewall` diagnostic.

### Pitfall 3: Savings Formula — Integer Division Truncation
**What goes wrong:** Go integer division truncates toward zero. For small `CacheReadTokens` values (e.g., 100 tokens), `savings = 100 * 270 / 1_000_000 = 0` (rounds down). This is correct behavior (truncation is conservative for savings), but tests must account for it.
**Why it happens:** Savings are tiny for real observed token counts today (structurally low hit-rate per Phase 20 CACHE-01 spike).
**How to avoid:** Test with token counts ≥ 1,000,000 to produce nonzero savings values. Use the same token scale as existing `pricing_test.go` cases.
**Warning signs:** `cacheSavingsCents` returns 0 for small token counts — this is correct, not a bug.

### Pitfall 4: PromQL Hit-Rate Division by Zero
**What goes wrong:** If both `cache_read` and `cache_creation` are zero (no dispatches yet), the hit-rate PromQL query divides by zero → `+Inf` or `NaN`.
**Why it happens:** Prometheus returns `NaN` for `0/0` and `+Inf` for `n/0`; recharts renders `NaN` as 0 but may show visual artifacts.
**How to avoid:** Add an `or vector(0)` or use `/(... > 0)` guard in the PromQL query. Example pattern from CONTEXT.md: `increase(cache_read[$w]) / (increase(cache_read[$w]) + increase(cache_creation[$w]))`. This returns `NaN` when denominator is 0. In the UI, handle `NaN` in the `yFormatter` for the hit-rate stat (show "—" or "0%").

### Pitfall 5: TelemetryView Test Count Hardcoded on "4 notices"
**What goes wrong:** Existing `TelemetryView.test.tsx` at lines 135-143 asserts `expect(notices).toHaveLength(4)` — exactly 4 `data-testid="telemetry-unavailable-notice"` elements. Adding a new panel that is also backed by PromQL will increase this count to 5 (or more).
**Why it happens:** Tests assert the current count as an exact value.
**How to avoid:** Update the `toHaveLength(4)` assertions to `toHaveLength(5)` (or however many panels total) when adding the cache-efficiency panel. Also update the degradation tests in describe blocks at lines 134-170.

### Pitfall 6: TerminationStub Size Constraint
**What goes wrong:** `pkg/dispatch.TerminationStub` is asserted to be < 4 KB when marshalled. Adding `CacheSavingsCents` to `Usage` increases the stub size.
**Why it happens:** `TerminationStub.Usage` embeds the full `Usage` struct (line 362).
**How to avoid:** Adding one `int64` field to `Usage` adds ~20 bytes of JSON (`"cacheSavingsCents":0,` + zero omit). `omitempty` ensures it's omitted when zero (the common case). The 4 KB limit has significant headroom. Run `TestNewTerminationStub_StaysSmall` in `pkg/dispatch/` to verify after the change.

### Pitfall 7: Cents vs Dollars in Dashboard
**What goes wrong:** The counter emits cents (integer). Displaying raw counter value as dollars without `/100` division shows 100× inflated figures.
**Why it happens:** The existing `tide_cost_cents_total` is also in cents; `formatCents` divides by 100 at display time.
**How to avoid:** Use `formatCents(Math.round(v))` where `v` is the raw counter increment (in cents). The `increase()` function returns a float; `Math.round` before passing to `formatCents` is the correct pattern (consistent with line 138: `formatCents(Math.round(v))`).

---

## Savings Computation — Verified Math

From `pricing.go` (verified live):
- `cacheReadCentsPerMTok = 0.10 × inputCentsPerMTok` for all models in `priceTable` (e.g., sonnet: input=300, read=30; haiku: input=100, read=10)
- `savings = CacheReadTokens × (inputCentsPerMTok − cacheReadCentsPerMTok) / 1_000_000`
- `savings = CacheReadTokens × 0.90 × inputCentsPerMTok / 1_000_000`

For sonnet-4-6 (the current default model, 300 cents/MTok input):
- `savings = CacheReadTokens × (300 − 30) / 1_000_000 = CacheReadTokens × 270 / 1_000_000`
- At 1,000,000 cache-read tokens: savings = 270 cents = $2.70

This matches CONTEXT.md's formula exactly. The helper can be named `cacheSavingsCents` (symmetry with `estimatedCostCents`) or `savingsCents` — planner's discretion.

---

## Build, Test, and Verify Commands

All commands verified against `Makefile` and `dashboard/web/package.json`.

### Go (metrics + pricing)

| Purpose | Command | Scope |
|---------|---------|-------|
| Unit tests (metrics + pricing) | `make test` | Runs full unit tier including `internal/metrics/` and `internal/subagent/anthropic/` |
| Unit tests without manifests/generate | `make test-only` | Faster; assumes prep already done |
| Just pricing tests | `go test ./internal/subagent/anthropic/ -run TestEstimatedCostCents` | Quick |
| Just registry tests | `go test ./internal/metrics/ -run TestRegistry` | Quick |
| Lint (includes metriccardinality + providerfirewall) | `make lint` | Required before PR |
| go vet | `make vet` | Also part of `make test` |

### Dashboard (TypeScript / Vitest)

| Purpose | Command | Location |
|---------|---------|----------|
| All frontend tests | `npm run test` (= `vitest run`) | `dashboard/web/` |
| Watch mode | `npm run test:watch` | `dashboard/web/` |
| Type check | `npm run lint` (= `tsc -b`) | `dashboard/web/` |
| Full build + tests | `make dashboard-frontend` | Repo root (`cd dashboard/web && npm ci && npm run build && npm run test`) |
| Dashboard binary | `make dashboard-build` | Builds Go binary embedding the SPA |

### Integration (touches metrics? — NO for this phase)

The metrics are emitted by the task controller at task completion. Phase 21 does not modify the dispatch path or integration test fixtures. The integration test suite (`make test-int`, `make test-int-fast`) does not need to change for Phase 21. The existing stub subagent sets `Usage{}` (all zeros), so no metric values propagate in integration tests — and no integration test currently asserts metric values. The new counter will register cleanly (no duplicate registration; it's in the existing `init()`).

---

## State of the Art

| Old Approach | Current Approach | Impact on Phase 21 |
|--------------|------------------|-------------------|
| No cache-efficiency visibility | Phase 16 built all four cache token counters with `{project,phase,plan,wave}` labels | OBSV-01 already satisfied at backend; Phase 21 surfaces it in UI |
| Manual cost inspection | `tide_cost_cents_total` counter + dashboard Cost Over Time panel | Pattern for savings counter is a direct clone |
| No realized-savings metric | Phase 21 adds `tide_cache_savings_cents_total` | The new emitted counter (D-02) |
| No cache-efficiency panel | Phase 21 adds D-05 panel | First panel to use instant query + range query in the same render |

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Go framework | Ginkgo v2.28 + standard `testing` |
| Go config file | `go.mod` (no separate test config) |
| Go quick run | `go test ./internal/metrics/ ./internal/subagent/anthropic/ ./pkg/dispatch/` |
| Go full suite | `make test` |
| Dashboard framework | Vitest v1.6.1 |
| Dashboard config | `dashboard/web/vitest.config.ts` |
| Dashboard quick run | `cd dashboard/web && npm run test` |
| Dashboard full | `make dashboard-frontend` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| OBSV-01 | `tide_tokens_*_total` carry `{project,phase,plan,wave}` labels | unit | `go test ./internal/metrics/ -run TestRegistry` | Yes (`registry_test.go`) |
| OBSV-01 (new) | `tide_cache_savings_cents_total` arity = 4 labels | unit | `go test ./internal/metrics/ -run TestRegistry_CacheSavingsCentsLabelArity` | No — Wave 0 gap |
| OBSV-01 (new) | `tide_cache_savings_cents_total` appears in Gather() | unit | `go test ./internal/metrics/ -run TestRegistry_AllMetricFamiliesPresent` | Needs update |
| OBSV-02 | `cacheSavingsCents()` computes 90%-of-input-rate savings correctly | unit | `go test ./internal/subagent/anthropic/ -run TestCacheSavingsCents` | No — Wave 0 gap |
| OBSV-02 | `CacheSavingsCents` field on `Usage` round-trips JSON | unit | `go test ./pkg/dispatch/ -run TestUsage` | Needs new test |
| OBSV-02 | `emitTaskMetrics()` emits savings counter | unit | `go test ./internal/controller/ -run TestEmitTaskMetrics` | Needs new test |
| OBSV-03 | Cache-efficiency panel renders in TelemetryView | unit | `cd dashboard/web && npm run test` | No — Wave 0 gap |
| OBSV-03 | Per-level breakdown selector fires grouped PromQL | unit | `cd dashboard/web && npm run test` | No — Wave 0 gap |
| OBSV-03 | Degradation: new panel tolerates `{status:"unavailable"}` | unit | `cd dashboard/web && npm run test` | No — Wave 0 gap |

### Sampling Rate

- **Per task commit:** `go test ./internal/metrics/ ./internal/subagent/anthropic/` + `cd dashboard/web && npm run test`
- **Per wave merge:** `make test` + `make dashboard-frontend`
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `internal/metrics/registry_test.go` — extend `TestRegistry_AllMetricFamiliesPresent` (add seed + want entry for `tide_cache_savings_cents_total`)
- [ ] `internal/metrics/registry_test.go` — add `TestRegistry_CacheSavingsCentsLabelArity`
- [ ] `internal/subagent/anthropic/pricing_test.go` — add `TestCacheSavingsCents` test cases
- [ ] `dashboard/web/src/components/__tests__/TelemetryView.test.tsx` — add cache-efficiency panel tests; update `toHaveLength(4)` to reflect new panel count
- [ ] `pkg/dispatch/envelope_test.go` (if exists, else new) — `CacheSavingsCents` JSON round-trip

---

## Security Domain

`security_enforcement` key absent from `.planning/config.json` — treated as enabled.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | n/a (no auth surface changed) |
| V3 Session Management | No | n/a |
| V4 Access Control | No | Dashboard is read-only by design (DASH-05); no mutation paths added |
| V5 Input Validation | Partial | PromQL proxy passes queries verbatim to upstream; proxy already validates endpoint config, not individual queries. New panel queries are static strings built from bounded enum values (`scope`, `range`, `level`) |
| V6 Cryptography | No | n/a |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| PromQL injection via level selector | Tampering | Level values are TypeScript enum literals (`"phase" | "plan" | "wave"`), not free-form user input; no user-supplied string reaches the PromQL query string |
| Dashboard data leakage | Information Disclosure | Dashboard is already read-only (DASH-05); no new authenticated endpoints; the existing ClusterRole is already asserted to be `{get, list, watch}` only via `make helm-rbac-assert` |
| Counter cardinality DoS | Denial of Service | `{project,phase,plan,wave}` label set is bounded by the existing TIDE hierarchy; same exposure as existing TELEM-03 counters; `metriccardinality` analyzer enforces no unbounded labels |

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | All Go tests/builds | Yes (project-level) | 1.26.x | — |
| Node/npm | Dashboard tests/build | Yes (project-level) | node_modules present | — |
| Prometheus | E2E panel validation | Not required for unit tests | — | Unit tests mock fetch; no live Prometheus needed |

---

## Open Questions

1. **D-05 single-stat trio — instant vs range for the three stats**
   - What we know: `/api/v1/query` (instant) exists in the proxy and returns `resultType:"vector"` (not `"matrix"`). The existing `TelemetryView.tsx` only uses `query_range` (matrix).
   - What's unclear: Should the trio stats use instant queries for current-window totals, or use `increase(counter[window])` range queries (same as existing panels)?
   - Recommendation: Use `increase(counter[window])` via `query_range` for the trio stats (consistent with existing panels; simpler data path; same `fetchQueryRange` helper). The "current state" is well-represented by the window sum. Only add a separate instant query if the planner determines that a true "current value" (last sample only) is needed.

2. **D-06 per-level breakdown — new panel vs dropdown on existing panels**
   - What we know: CONTEXT.md says "add a phase/plan/wave breakdown/selector"; it's a "Claude's Discretion" item.
   - What's unclear: Whether to add a new standalone "Spend by Level" panel in PANELS, or to add a dropdown that switches the `by(...)` clause on ALL existing panels.
   - Recommendation: New segmented control on the toolbar (alongside the scope toggle and range selector) that sets `breakdownDimension: "none" | "phase" | "plan" | "wave"`. When `breakdownDimension != "none"`, all panel queries switch to `sum by(<dim>)(...)`. This is consistent with how the scope toggle changes all panels simultaneously.

3. **TerminationStub size test — explicit check**
   - What we know: There is a `TestNewTerminationStub_StaysSmall` test somewhere in `pkg/dispatch/`.
   - Recommendation: After adding `CacheSavingsCents int64` with `omitempty` to `Usage`, verify the test still passes. The JSON for a zero savings field is omitted; the JSON for a nonzero field adds ~27 bytes — well within the 4 KB limit.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `TestNewTerminationStub_StaysSmall` exists in `pkg/dispatch/` | Open Questions | Low — if missing, the 4KB constraint is not tested; new test would be a Wave 0 gap |
| A2 | Adding `CacheSavingsCents` to `Usage` doesn't break existing JSON decoders in harness code | Pitfall 6 | Low — Go JSON `omitempty` + additive field; old decoders ignore unknown fields by default |
| A3 | Prometheus `resultType:"vector"` (instant query) is handled by frontend if needed for D-05 | Open Question 1 | Medium — if instant queries return a different shape, `fetchQueryRange` can't be reused; need `fetchInstant` helper. Mitigated by recommendation to use `query_range` for everything |

---

## Sources

### Primary (HIGH confidence — verified against live code)

- `internal/metrics/registry.go` — var declarations (lines 98-115), TELEM-03 construction (185-227), MustRegister (229-244); verified 2026-06-15
- `pkg/dispatch/envelope.go` — `Usage` struct (lines 270-301), `CacheReadTokens` (289), `CacheCreationTokens` (300), `EstimatedCostCents` (282); verified 2026-06-15
- `internal/controller/task_controller.go` — `emitTaskMetrics()` (1044-1099), emission block (1068-1072); verified 2026-06-15
- `internal/subagent/anthropic/pricing.go` — `modelPrice` struct (49-54), `priceTable` (60-111), `estimatedCostCents` (132-157); verified 2026-06-15
- `internal/subagent/anthropic/subagent.go` — `EstimatedCostCents` assignment (line 345); verified 2026-06-15
- `dashboard/web/src/components/TelemetryView.tsx` — `PANELS` (134-227), `formatCents` (232-234), `fetchQueryRange` (240-294), scope/range controls (845-933), `ChartPanel` render loop (988-990); verified 2026-06-15
- `cmd/dashboard/api/prometheus.go` — `PrometheusHandler.Query` (64), `QueryRange` (70), degradation sentinel (81-83), 502 path (111-116); verified 2026-06-15
- `cmd/dashboard/router.go:179-180` — PromQL proxy route registration; verified 2026-06-15
- `internal/metrics/registry_test.go` — all arity tests, `TestRegistry_NoTaskLabel`, `TestRegistry_AllMetricFamiliesPresent`; verified 2026-06-15
- `dashboard/web/src/components/__tests__/TelemetryView.test.tsx` — `toHaveLength(4)` assertions at lines 142, 156; verified 2026-06-15
- `dashboard/web/package.json` — `"test": "vitest run"`, `"lint": "tsc -b"`; verified 2026-06-15
- `Makefile` — `make test` (line 85), `make test-only` (88), `make dashboard-frontend` (278-279), `make lint` (240), `make eval` (205-217); verified 2026-06-15
- `.planning/config.json` — `nyquist_validation: true`, `commit_docs: true`; verified 2026-06-15

### Secondary (MEDIUM confidence)

- `internal/subagent/anthropic/pricing_test.go` — cache rate assertions (`haiku_cache_read`, `fable5_cache_rates`); confirm 0.10× read / 1.25× write rules; verified 2026-06-15
- `internal/subagent/anthropic/cost_parity_test.go` — `TestCostParity_RealizedSavings`; confirms savings direction for high cache-read:write scenarios; verified 2026-06-15

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new packages; all existing
- Architecture patterns: HIGH — verified against live code with file:line anchors
- Pitfalls: HIGH — grounded in actual code constraints (AST analyzer, test assertions, TerminationStub limit)
- Dashboard patterns: HIGH — full TelemetryView.tsx read with line-level verification

**Research date:** 2026-06-15
**Valid until:** 2026-07-15 (stable codebase; no external dependency drift expected)
