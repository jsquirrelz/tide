# Phase 21: Cost & Cache Observability — Pattern Map

**Mapped:** 2026-06-15
**Files analyzed:** 7 new/modified files
**Analogs found:** 7 / 7

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/metrics/registry.go` | metric-registry | CRUD (counter registration) | itself (`CostCentsTotal` var + init block) | exact |
| `internal/metrics/registry_test.go` | test | CRUD | itself (`TestRegistry_CostCentsLabelArity`, `TestRegistry_AllMetricFamiliesPresent`) | exact |
| `pkg/dispatch/envelope.go` | model | request-response | itself (`EstimatedCostCents int64` field on `Usage`) | exact |
| `internal/subagent/anthropic/pricing.go` | service | transform | itself (`estimatedCostCents` method, lines 132–157) | exact |
| `internal/controller/task_controller.go` | controller | event-driven | itself (`emitTaskMetrics()` block, lines 1068–1072) | exact |
| `dashboard/web/src/components/TelemetryView.tsx` | component | request-response | itself (`PANELS` array entries + `BudgetCard` + `SegmentedControl`, lines 134–743) | exact |
| `dashboard/web/src/components/__tests__/TelemetryView.test.tsx` | test | request-response | itself (degradation suites at lines 134–171; `toHaveLength(4)` at lines 142, 156) | exact |

---

## Pattern Assignments

### `internal/metrics/registry.go` (metric-registry, counter registration)

**Change type:** Add one new `*prometheus.CounterVec` var declaration and construction block.

**Analog:** `CostCentsTotal` in the same file.

**Var declaration pattern** (lines 110–111 — the slot immediately before `TaskDurationSeconds`):
```go
// CostCentsTotal counts the estimated cost in US cents consumed by Tasks.
var CostCentsTotal *prometheus.CounterVec
```
New var to insert after line 111:
```go
// CacheSavingsCentsTotal counts the realized cache savings in US cents per Task.
var CacheSavingsCentsTotal *prometheus.CounterVec
```

**Construction pattern in `init()`** (lines 213–219 — exact live code):
```go
CostCentsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_cost_cents_total",
        Help: "Estimated cost in US cents consumed by Tasks (Phase 16 TELEM-03).",
    },
    []string{"project", "phase", "plan", "wave"},
)
```
Clone immediately after line 219:
```go
CacheSavingsCentsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_cache_savings_cents_total",
        Help: "Realized cache savings in US cents (input price minus cache-read price) per Task (Phase 21 OBSV-02).",
    },
    []string{"project", "phase", "plan", "wave"},
)
```

**MustRegister pattern** (lines 229–244 — exact live code):
```go
metrics.Registry.MustRegister(
    WavesDispatchedTotal,
    // ... existing entries ...
    // Phase 16 TELEM-03:
    TokensInputTotal,
    TokensOutputTotal,
    TokensCacheReadTotal,
    TokensCacheCreationTotal,
    CostCentsTotal,
    TaskDurationSeconds,
)
```
Add `CacheSavingsCentsTotal,` after `CostCentsTotal,` in the existing block (before `TaskDurationSeconds`).

**Critical constraints:**
- Label slice MUST be `[]string{"project", "phase", "plan", "wave"}` — identical to all six TELEM-03 counters.
- Do NOT include `"task"` in any label slice — `metriccardinality` AST analyzer rejects it at `make lint`.
- Do NOT call `MustRegister` on `ProviderRateLimitHitsTotal` (the alias re-export comment at line 246 explains why: it would panic on duplicate registration).

---

### `internal/metrics/registry_test.go` (test, CRUD)

**Change type:** Extend two existing tests + add one new arity test.

**Analog 1 — seed pattern in `TestRegistry_AllMetricFamiliesPresent`** (lines 64–70, exact live code):
```go
// Phase 16 TELEM-03: seed six new metric families ({project, phase, plan, wave} = 4 args).
tidemetrics.TokensInputTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
tidemetrics.TokensOutputTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
tidemetrics.TokensCacheReadTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
tidemetrics.TokensCacheCreationTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
tidemetrics.CostCentsTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
tidemetrics.TaskDurationSeconds.WithLabelValues("__seed__", "ph", "pl", "w").Observe(0)
```
Add after the `CostCentsTotal` seed line:
```go
tidemetrics.CacheSavingsCentsTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
```

**Analog 2 — want slice in `TestRegistry_AllMetricFamiliesPresent`** (lines 80–95, exact live code):
```go
want := []string{
    // ...
    // Phase 16 TELEM-03 locked metrics:
    "tide_tokens_input_total",
    "tide_tokens_output_total",
    "tide_tokens_cache_read_total",
    "tide_tokens_cache_creation_total",
    "tide_cost_cents_total",
    "tide_task_duration_seconds",
}
```
Add `"tide_cache_savings_cents_total",` after `"tide_cost_cents_total",`.

**Analog 3 — arity test pattern** (lines 214–220, exact live code):
```go
// TestRegistry_CostCentsLabelArity asserts arity {project, phase, plan, wave} = 4.
func TestRegistry_CostCentsLabelArity(t *testing.T) {
    tidemetrics.CostCentsTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
    if got := testutil.ToFloat64(tidemetrics.CostCentsTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
        t.Errorf("CostCentsTotal counter = %v, want >= 1", got)
    }
}
```
Clone immediately after as:
```go
// TestRegistry_CacheSavingsCentsLabelArity asserts arity {project, phase, plan, wave} = 4.
// Also serves as the OBSV-01 audit guard: the savings counter carries the same
// four-label set that makes per-level PromQL slicing work without extra instrumentation.
func TestRegistry_CacheSavingsCentsLabelArity(t *testing.T) {
    tidemetrics.CacheSavingsCentsTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
    if got := testutil.ToFloat64(tidemetrics.CacheSavingsCentsTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
        t.Errorf("CacheSavingsCentsTotal counter = %v, want >= 1", got)
    }
}
```

---

### `pkg/dispatch/envelope.go` (model, request-response)

**Change type:** Add one `int64` field to the `Usage` struct.

**Analog — `EstimatedCostCents` field** (lines 279–282, exact live code):
```go
// EstimatedCostCents is the estimated US-cent cost for this task, rounded
// up to the nearest cent. Used for Project.Status.budget.costSpentCents
// rollup.
EstimatedCostCents int64 `json:"estimatedCostCents"`
```

Add immediately after line 282:
```go
// CacheSavingsCents is the realized savings in US cents for this task:
// CacheReadTokens × (inputCentsPerMTok − cacheReadCentsPerMTok) / 1_000_000.
// Computed by the provider (provider firewall D-C1), zero when CacheReadTokens
// is zero. Omitted from JSON when zero (common case — no cache reads).
CacheSavingsCents int64 `json:"cacheSavingsCents,omitempty"`
```

**Key difference from analog:** `EstimatedCostCents` does NOT have `omitempty` (every task always has a cost). `CacheSavingsCents` MUST have `omitempty` because zero is the common case (no cache reads) and serializing it would bloat `out.json` unnecessarily. Run `TestNewTerminationStub_StaysSmall` in `pkg/dispatch/` after adding the field.

---

### `internal/subagent/anthropic/pricing.go` (service, transform)

**Change type:** Add one new method `cacheSavingsCents` alongside `estimatedCostCents`.

**Analog — `estimatedCostCents` method** (lines 132–157, exact live code):
```go
func (a *Anthropic) estimatedCostCents(model string, u pkgdispatch.Usage) int64 {
    price, ok := a.prices[model]
    if !ok {
        fmt.Fprintf(os.Stderr, "pricing: unknown model %q, using conservative default (most-expensive known tier)\n", model)
        price = conservativeTier
    }

    numerator := u.InputTokens*price.inputCentsPerMTok +
        u.OutputTokens*price.outputCentsPerMTok +
        u.CacheReadTokens*price.cacheReadCentsPerMTok +
        u.CacheCreationTokens*price.cacheWriteCentsPerMTok

    if numerator == 0 {
        return 0
    }

    const million = int64(1_000_000)
    // Ceiling division: round up any sub-cent fraction to 1 cent.
    return (numerator + million - 1) / million
}
```

New method to add immediately after line 157:
```go
// cacheSavingsCents returns the realized cache savings in US cents for the
// given model and usage. Savings = CacheReadTokens × (inputRate − readRate) / 1e6.
// The 0.90× factor comes from the locked 0.10× read rate in the price table.
//
// Returns 0 when CacheReadTokens is zero (no reads → no savings).
// Uses truncation (not ceiling) — conservative for savings: never overstates.
// Reads a.prices (per-instance clone), never the package-level priceTable (T-14-02).
func (a *Anthropic) cacheSavingsCents(model string, u pkgdispatch.Usage) int64 {
    if u.CacheReadTokens == 0 {
        return 0
    }
    price, ok := a.prices[model]
    if !ok {
        price = conservativeTier
    }
    // savings = CacheReadTokens × (inputCentsPerMTok − cacheReadCentsPerMTok) / 1_000_000
    savings := u.CacheReadTokens * (price.inputCentsPerMTok - price.cacheReadCentsPerMTok)
    return savings / 1_000_000
}
```

**Key differences from `estimatedCostCents`:**
- No `fmt.Fprintf` stderr warning on table miss (savings fallback to `conservativeTier` is silent — correctness is not impaired; the conservative tier produces the highest savings estimate, which is acceptable).
- Truncation division (`/ million`) not ceiling division (`(n + million - 1) / million`) — truncation is correct for savings (never overstate).
- Early-return on zero `CacheReadTokens` avoids the division entirely.

**Call site in `subagent.go`** (line 345, exact live code):
```go
usage.EstimatedCostCents = a.estimatedCostCents(in.Provider.Model, usage)
```
Add immediately after:
```go
usage.CacheSavingsCents = a.cacheSavingsCents(in.Provider.Model, usage)
```
Both lines must stay inside `internal/subagent/anthropic/` (provider firewall D-C1). The `out` struct assembled at line 347 embeds `usage`, so the new field flows into `EnvelopeOut.Usage.CacheSavingsCents` automatically.

**Test analog — `pricing_test.go`** (lines 52–68, exact live code):
```go
t.Run("haiku_cache_read", func(t *testing.T) {
    // 1_000_000 cache-read tokens * 10 cents/MTok / 1_000_000 = 10 cents
    u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
    got := a.estimatedCostCents("claude-haiku-4-5", u)
    if got != 10 {
        t.Errorf("haiku cacheRead=1M: want 10 cents, got %d", got)
    }
})
```
New test function to add in `pricing_test.go`, following the same sub-test structure:
```go
func TestCacheSavingsCents(t *testing.T) {
    a := New(Options{})

    t.Run("haiku_1M_read_tokens", func(t *testing.T) {
        // haiku: input=100, read=10 => savings = 1_000_000 * (100-10) / 1_000_000 = 90 cents
        u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
        got := a.cacheSavingsCents("claude-haiku-4-5", u)
        if got != 90 {
            t.Errorf("haiku 1M cache-read: want 90 cents, got %d", got)
        }
    })

    t.Run("sonnet_1M_read_tokens", func(t *testing.T) {
        // sonnet-4-6: input=300, read=30 => savings = 1_000_000 * (300-30) / 1_000_000 = 270 cents
        u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
        got := a.cacheSavingsCents("claude-sonnet-4-6", u)
        if got != 270 {
            t.Errorf("sonnet 1M cache-read: want 270 cents, got %d", got)
        }
    })

    t.Run("zero_read_tokens", func(t *testing.T) {
        // No cache reads → no savings.
        u := pkgdispatch.Usage{InputTokens: 1_000_000}
        got := a.cacheSavingsCents("claude-haiku-4-5", u)
        if got != 0 {
            t.Errorf("zero cache-read: want 0, got %d", got)
        }
    })

    t.Run("truncation_not_ceiling", func(t *testing.T) {
        // 100 cache-read tokens * 90 / 1_000_000 = 0.009 cents → truncates to 0 (correct).
        u := pkgdispatch.Usage{CacheReadTokens: 100}
        got := a.cacheSavingsCents("claude-haiku-4-5", u)
        if got != 0 {
            t.Errorf("sub-cent savings should truncate to 0, got %d", got)
        }
    })

    t.Run("unknown_model_uses_conservative", func(t *testing.T) {
        // Unknown model falls back to conservativeTier (fable-5: input=1000, read=100).
        // savings = 1_000_000 * (1000-100) / 1_000_000 = 900 cents
        u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
        got := a.cacheSavingsCents("claude-unknown-model", u)
        if got != 900 {
            t.Errorf("unknown model: want 900 cents (conservative), got %d", got)
        }
    })
}
```

---

### `internal/controller/task_controller.go` (controller, event-driven)

**Change type:** Add one `.Add()` call in `emitTaskMetrics()`.

**Analog — the five existing emissions** (lines 1068–1072, exact live code):
```go
tidemetrics.TokensInputTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.InputTokens))
tidemetrics.TokensOutputTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.OutputTokens))
tidemetrics.TokensCacheReadTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheReadTokens))
tidemetrics.TokensCacheCreationTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheCreationTokens))
tidemetrics.CostCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.EstimatedCostCents))
```

Add immediately after line 1072:
```go
tidemetrics.CacheSavingsCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheSavingsCents))
```

**Critical constraints:**
- No pricing math here — the controller reads `usage.CacheSavingsCents` (a plain `int64`) and emits it. `emitTaskMetrics()` must never import `internal/subagent/anthropic` (provider firewall D-C1; `providerfirewall` analyzer enforces this at `make lint`).
- Same four label values (`projectName, phase, plan, wave`) as every other TELEM-03 counter — copy the existing argument order exactly.

---

### `dashboard/web/src/components/TelemetryView.tsx` (component, request-response)

**Change type:** Two additive changes — (A) new `CacheEfficiencyPanel` sub-component + PANELS render branch, (B) new per-level `SegmentedControl` in the toolbar.

#### A. Cache-Efficiency Panel (D-05)

**Analog 1 — `PANELS` array entry for "cost-over-time"** (lines 135–148, exact live code):
```typescript
{
  id: "cost-over-time",
  label: "Cost Over Time",
  yFormatter: (v: number) => formatCents(Math.round(v)),
  series: [
    {
      key: "cost",
      buildQuery: (scope, project, window) =>
        scope === "project"
          ? `sum(increase(tide_cost_cents_total{project="${project}"}[${window}]))`
          : `sum(increase(tide_cost_cents_total[${window}])) by (project)`,
    },
  ],
},
```
The cache-efficiency panel is **NOT** a standard `PanelDef` entry because it renders a stat trio, not a time-series chart. It must be rendered OUTSIDE the `PANELS.map` loop as a separate element in the `data-testid="telemetry-panels"` grid.

**Analog 2 — `BudgetCard` container shell** (lines 597–687, exact live code):
```tsx
function BudgetCard({ project, projectName, showName = false, testId = "budget-card" }: BudgetCardProps) {
  return (
    <div
      data-testid={testId}
      className="flex flex-col gap-1 rounded border p-4"
      style={{
        borderColor: "var(--color-border-subtle)",
        background: "var(--color-surface-raised)",
      }}
    >
      <h3
        style={{
          fontSize: "12px",
          fontWeight: 600,
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-muted)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          margin: 0,
        }}
      >
        Budget
      </h3>
      {/* metric figure: */}
      <div
        style={{
          fontSize: "20px",
          fontWeight: 600,
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-primary)",
        }}
      >
        {formatCents(budget.currentSpend)}
      </div>
```
The trio panel clones this container shell (same border/background/padding/header style). Each of the three stat figures uses 20px/600/mono per UI-SPEC (the BudgetCard figure style is the locked spec — NOT 24px/700).

**Analog 3 — `ChartPanel` panel container** (lines 535–586, exact live code):
```tsx
function ChartPanel({ def, state, range }: ChartPanelProps) {
  return (
    <div
      data-testid={`panel-${def.id}`}
      className="flex flex-col gap-2 rounded border p-4"
      style={{
        borderColor: "var(--color-border-subtle)",
        background: "var(--color-surface-raised)",
      }}
    >
      {state.kind === "unavailable" && <TelemetryUnavailableNotice />}
      {state.kind === "unreachable" && (
        <TelemetryUnavailableNotice message={state.message} />
      )}
```
The cache-efficiency panel's outer container must use `data-testid="panel-cache-efficiency"` (so the test harness's `getAllByTestId("telemetry-unavailable-notice")` count works) and must render `<TelemetryUnavailableNotice />` on degraded fetch results (matching the existing four ChartPanels).

**Analog 4 — `formatCents` helper** (lines 232–234, exact live code):
```typescript
function formatCents(cents: number): string {
  return "$" + (cents / 100).toFixed(2);
}
```
Use `formatCents(Math.round(v))` for the "saved $" stat where `v` is the raw counter increment in cents. Same pattern as line 138 (`yFormatter: (v: number) => formatCents(Math.round(v))`).

**Analog 5 — `fetchQueryRange` helper** (lines 240–294, exact live code):
```typescript
async function fetchQueryRange(
  query: string,
  startSec: number,
  endSec: number,
  step: number,
): Promise<{ kind: "data"; result: PromMatrix[] } | { kind: "unavailable" } | { kind: "unreachable"; message: string }>
```
The cache-efficiency panel fetches three range queries (hit ratio, creation tokens, savings $) via the existing `fetchQueryRange`. No new fetch wrapper needed for the stat trio — use `increase(counter[window])` range queries for all three (not instant queries), consistent with all existing panels. The sparkline uses the same `fetchQueryRange` for the hit-ratio time series.

**Render placement in JSX** (lines 983–991, exact live code):
```tsx
{/* Chart panels — Prometheus-backed (UI-SPEC C4) */}
<div
  data-testid="telemetry-panels"
  className="grid grid-cols-2 gap-4"
>
  {PANELS.map((def, idx) => (
    <ChartPanel key={def.id} def={def} state={panelStates[idx]} range={range} />
  ))}
</div>
```
The cache-efficiency panel is appended as a sibling to `PANELS.map(...)` inside the same `data-testid="telemetry-panels"` grid div. It occupies one grid cell alongside the existing four.

**PromQL queries for the three stats (D-03, D-04):**
```typescript
// Hit ratio (PromQL-derived, D-01) — returns NaN when denominator is zero
`sum(increase(tide_tokens_cache_read_total{project="${project}"}[${window}]))
 / (sum(increase(tide_tokens_cache_read_total{project="${project}"}[${window}]))
    + sum(increase(tide_tokens_cache_creation_total{project="${project}"}[${window}])))`

// Cache creation tokens
`sum(increase(tide_tokens_cache_creation_total{project="${project}"}[${window}]))`

// Realized savings $ (from the new counter, D-02)
`sum(increase(tide_cache_savings_cents_total{project="${project}"}[${window}]))`
```
Handle `NaN` hit-ratio (division by zero when no cache activity) by displaying `"—"` in the stat figure — per UI-SPEC copywriting contract.

**`TimeSeriesChart` for the sparkline** (lines 424–527). The sparkline is the hit-ratio range series rendered at `height={48}` (48px per UI-SPEC spacing contract). Clone the existing `<TimeSeriesChart>` with `panelDef.stacked=false`, a single series, using `SERIES_PALETTE[0]` (neutral, not accent/red).

#### B. Per-Level Breakdown Selector (D-06)

**Analog — `SegmentedControl` component** (lines 701–743, exact live code):
```tsx
function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  testId,
  useAriaPressed = true,
}: SegmentedControlProps<T>) {
  return (
    <div
      data-testid={testId}
      style={{
        display: "inline-flex",
        borderRadius: "4px",
        border: "1px solid var(--color-border-subtle)",
      }}
    >
      {options.map((opt) => {
        const isActive = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            aria-pressed={useAriaPressed ? isActive : undefined}
            onClick={() => onChange(opt.value)}
            style={{
              padding: "4px 8px",
              fontSize: "12px",
              fontWeight: 600,
              fontFamily: opt.mono ? "var(--font-mono)" : "var(--font-sans)",
              cursor: "pointer",
              border: "none",
              borderRadius: "3px",
              background: isActive ? "var(--color-surface-overlay)" : "transparent",
              color: isActive ? "var(--color-text-primary)" : "var(--color-text-muted)",
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
```
The per-level selector reuses `SegmentedControl` with `testId="telemetry-level-selector"`. Options array per UI-SPEC:
```typescript
type BreakdownKind = "none" | "phase" | "plan" | "wave";

const levelOptions: Array<{ value: BreakdownKind; label: string; mono?: boolean }> = [
  { value: "none",  label: "Project" },
  { value: "phase", label: "Phase",  mono: true },
  { value: "plan",  label: "Plan",   mono: true },
  { value: "wave",  label: "Wave",   mono: true },
];
```

**State variable to add** (alongside `scope` and `range` at lines 759–768):
```typescript
const [breakdown, setBreakdown] = useState<BreakdownKind>("none");
```
A `breakdownRef` (following the `scopeRef`/`rangeRef` pattern at lines 777–782) lets the polling callback read the latest value without re-registering the interval.

**Toolbar placement** (lines 863–932, exact live code):
```tsx
<div className="flex items-center justify-between">
  {/* Scope toggle (left) */}
  <div data-testid="telemetry-scope-toggle" ...>
    {scopeOptions.map(...)}
  </div>
  {/* Range selector (right) */}
  <div data-testid="telemetry-range-selector" ...>
    {rangeOptions.map(...)}
  </div>
</div>
```
Per UI-SPEC, insert the level selector between scope and range, grouping scope+level in a left `flex gap-2` cluster:
```tsx
<div className="flex items-center justify-between">
  <div className="flex gap-2">
    {/* Scope toggle */}
    <div data-testid="telemetry-scope-toggle" ...>...</div>
    {/* Level breakdown selector */}
    <SegmentedControl
      options={levelOptions}
      value={breakdown}
      onChange={setBreakdown}
      testId="telemetry-level-selector"
    />
  </div>
  {/* Range selector stays right */}
  <div data-testid="telemetry-range-selector" ...>...</div>
</div>
```

**`buildQuery` extension for breakdown** — when `breakdown !== "none"`, the `by(...)` clause is appended to all panel queries. The existing `scope === "project"` / `scope === "all"` branching in each `buildQuery` (lines 143–146, 157–167, etc.) gains a third dimension. The key-derivation in `fetchPanel` (lines 340–349) that reads `matrix.metric["project"]` extends to `matrix.metric[breakdown]` when breakdown is active. This is the same pattern — different label key, same logic.

---

### `dashboard/web/src/components/__tests__/TelemetryView.test.tsx` (test, request-response)

**Change type:** Update two hardcoded length assertions + add new test suites.

**The two assertions that MUST be updated** (lines 142 and 156, exact live code):

Line 142 (degradation shape 1 test):
```typescript
const notices = screen.getAllByTestId("telemetry-unavailable-notice");
expect(notices).toHaveLength(4);  // ← change to 5 when panel is added
```

Line 156 (degradation shape 2 test):
```typescript
const notices = screen.getAllByTestId("telemetry-unavailable-notice");
expect(notices).toHaveLength(4);  // ← change to 5 when panel is added
```
Both become `toHaveLength(5)` once the cache-efficiency panel is added (it also renders `<TelemetryUnavailableNotice />` on degraded fetches, exactly like the four existing ChartPanels).

**New test suite analog — degradation pattern** (lines 134–143, the complete describe block):
```typescript
describe("TelemetryView — degradation: 200 unavailable sentinel (TELEM-02)", () => {
  it("renders TelemetryUnavailableNotice in all four panel slots", async () => {
    stubFetchOK({ status: "unavailable" });
    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject="p1" />,
    );
    await flushInitialFetch();
    const notices = screen.getAllByTestId("telemetry-unavailable-notice");
    expect(notices).toHaveLength(4);
  });
});
```

New tests to add following the same structure:
```typescript
describe("TelemetryView — cache-efficiency panel (OBSV-03)", () => {
  it("renders panel-cache-efficiency in the panels grid", async () => {
    stubFetchOK(SUCCESS_PAYLOAD);
    render(<TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />);
    await flushInitialFetch();
    expect(screen.getByTestId("panel-cache-efficiency")).toBeDefined();
  });

  it("renders TelemetryUnavailableNotice in cache-efficiency panel when unavailable", async () => {
    stubFetchOK({ status: "unavailable" });
    render(<TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />);
    await flushInitialFetch();
    const notices = screen.getAllByTestId("telemetry-unavailable-notice");
    expect(notices).toHaveLength(5); // 4 existing + 1 cache-efficiency
  });

  it("queries include tide_cache_savings_cents_total", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);
    render(<TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />);
    await flushInitialFetch();
    const calls = (fetchFn as ReturnType<typeof vi.fn>).mock.calls as [string][];
    const hasSavings = calls.some(([url]) => {
      const params = new URLSearchParams(url.split("?")[1] ?? "");
      return (params.get("query") ?? "").includes("tide_cache_savings_cents_total");
    });
    expect(hasSavings).toBe(true);
  });
});

describe("TelemetryView — per-level selector (D-06)", () => {
  it("renders telemetry-level-selector control", () => {
    stubFetchOK({ status: "unavailable" });
    render(<TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />);
    expect(screen.getByTestId("telemetry-level-selector")).toBeDefined();
  });

  it("clicking Phase fires queries with by(phase) aggregation", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);
    render(<TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />);
    await flushInitialFetch();
    fetchFn.mockClear();

    const phaseBtn = screen.getByText("Phase");
    await act(async () => {
      fireEvent.click(phaseBtn);
      await vi.advanceTimersByTimeAsync(0);
    });

    const calls = (fetchFn as ReturnType<typeof vi.fn>).mock.calls as [string][];
    const hasByPhase = calls.some(([url]) => {
      const params = new URLSearchParams(url.split("?")[1] ?? "");
      return (params.get("query") ?? "").includes("by(phase)") ||
             (params.get("query") ?? "").includes("by (phase)");
    });
    expect(hasByPhase).toBe(true);
  });
});
```

---

## Shared Patterns

### Counter Registration (apply to: `registry.go`, `registry_test.go`)

**Source:** `internal/metrics/registry.go` — `CostCentsTotal` var decl (line 111) + init construction (lines 213–219) + MustRegister list (lines 237–244).

Pattern: package-level `var Foo *prometheus.CounterVec` → constructed in `init()` with `prometheus.NewCounterVec(CounterOpts{Name, Help}, labelSlice)` → added to `metrics.Registry.MustRegister(...)`. Label slice for all TELEM-03 counters is always `[]string{"project", "phase", "plan", "wave"}`. The `metriccardinality` AST analyzer enforces this at `make lint`.

### Provider Firewall (apply to: `pricing.go`, `subagent.go`, `task_controller.go`)

**Source:** `internal/subagent/anthropic/pricing.go` `estimatedCostCents` (line 132) + `subagent.go` call site (line 345).

Pattern: All pricing math lives in `internal/subagent/anthropic/`. The controller (`task_controller.go`) only reads plain typed fields (`usage.CacheSavingsCents int64`) — it never imports a price table or `internal/subagent/anthropic`. Enforced by `tools/analyzers/providerfirewall` at `make lint`.

### PromQL Degradation Contract (apply to: `TelemetryView.tsx`, `TelemetryView.test.tsx`)

**Source:** `dashboard/web/src/components/TelemetryView.tsx` — `fetchQueryRange` (lines 240–294) + `ChartPanel` degradation branches (lines 559–583) + `TelemetryUnavailableNotice` import (line 46).

Pattern: Every Prometheus-backed panel renders `<TelemetryUnavailableNotice />` on `kind: "unavailable"` and `<TelemetryUnavailableNotice message={state.message} />` on `kind: "unreachable"`. The test harness counts `getAllByTestId("telemetry-unavailable-notice")` to assert total panel count — one notice per panel in degraded mode.

### `formatCents` Display (apply to: `TelemetryView.tsx`)

**Source:** `dashboard/web/src/components/TelemetryView.tsx` line 232–234, called at line 138.

Pattern: `formatCents(Math.round(v))` where `v` is the raw cents increment from `increase(counter[window])`. The `Math.round` is needed because `increase()` returns a float. `formatCents` divides by 100 and formats to two decimal places.

---

## No Analog Found

All seven files have exact or near-exact in-repo analogs. No net-new patterns required.

The only novel element is the **`CacheEfficiencyPanel` sub-component shape** (stat trio + sparkline in one panel) — there is no existing three-stat panel in `TelemetryView.tsx`. The planner must compose it from `BudgetCard`'s container shell (for the trio) and `TimeSeriesChart` at 48px height (for the sparkline). These components exist; the composition is new.

---

## Metadata

**Analog search scope:** `internal/metrics/`, `pkg/dispatch/`, `internal/subagent/anthropic/`, `internal/controller/`, `dashboard/web/src/components/`
**Files read:** 9 source files (registry.go, registry_test.go, envelope.go, pricing.go, pricing_test.go, subagent.go, task_controller.go, TelemetryView.tsx, TelemetryView.test.tsx) + TelemetryUnavailableNotice.tsx
**Pattern extraction date:** 2026-06-15
