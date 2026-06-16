---
phase: 21-cost-cache-observability
verified: 2026-06-16T05:45:00Z
status: human_needed
score: 8/8 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: n/a
human_verification:
  - test: "Deploy the dashboard against a running cluster with Prometheus scraping TIDE, open TelemetryView, and confirm the Cache Efficiency panel renders hit ratio %, cache-creation tokens, and realized savings $ from live counters."
    expected: "Fifth panel labeled 'Cache Efficiency' shows three live figures (hit %, creation tokens, saved $) plus a hit-rate sparkline; values come from real tide_tokens_cache_* and tide_cache_savings_cents_total series."
    why_human: "Requires a live cluster with Prometheus + completed Tasks; grep/unit tests confirm the wiring and degradation paths but not real PromQL data rendering against a running scrape target."
  - test: "In TelemetryView, click the per-level selector (Project/Phase/Plan/Wave) and confirm every panel re-queries with sum by(<dim>) aggregation and renders per-level series."
    expected: "Selecting Phase/Plan/Wave switches all five panels to grouped series keyed by the chosen dimension; selecting Project restores ungrouped totals."
    why_human: "Visual confirmation of grouped series rendering against live Prometheus data; the query-string switch is unit-tested but the rendered breakdown is not."
---

# Phase 21: Cost & Cache Observability Verification Report

**Phase Goal:** Per-level token accounting and cache efficiency are visible on the dashboard so operators can observe the results of the Phase 18–20 work in a running cluster.
**Verified:** 2026-06-16T05:45:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | `tide_cache_savings_cents_total` counter registered with labels `{project,phase,plan,wave}` (no `task`) | ✓ VERIFIED | `internal/metrics/registry.go:226-232` — `NewCounterVec` Name `tide_cache_savings_cents_total`, label slice `[]string{"project","phase","plan","wave"}`; `MustRegister` entry at line 257. `make lint` (metriccardinality analyzer) exits 0. |
| 2 | `cacheSavingsCents` computes `CacheReadTokens × (inputRate − readRate) / 1e6` with truncation, behind the provider firewall | ✓ VERIFIED | `internal/subagent/anthropic/pricing.go:133-150` — early-return 0 on zero read, reads `a.prices[model]` (per-instance clone, T-14-02), `conservativeTier` fallback, `savings / million` truncation. Controller does NOT import `internal/subagent/anthropic` (firewall grep clean). |
| 3 | `Usage.CacheSavingsCents int64` carries the value provider→controller with omitempty | ✓ VERIFIED | `pkg/dispatch/envelope.go:289` — `CacheSavingsCents int64 \`json:"cacheSavingsCents,omitempty"\``. |
| 4 | `emitTaskMetrics()` emits the savings counter next to `CostCentsTotal`, identical labels, no pricing math in controller | ✓ VERIFIED | `internal/controller/task_controller.go:1073` — `CacheSavingsCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheSavingsCents))` immediately after the cost `.Add()`. Reads a plain int64; no pricing import. |
| 5 | Per-level queryable: all six counters retain `{project,phase,plan,wave}`; label-arity test exists | ✓ VERIFIED | `internal/metrics/registry_test.go:232` `TestRegistry_CacheSavingsCentsLabelArity`; seed at line 72; want-list entry at line 98. `go test ./internal/metrics/` green; `make lint` confirms label invariant. |
| 6 | Hit ratio is PromQL-derived (D-01) — no new gauge; savings counter + components satisfy ROADMAP criterion 2 "gauge or equivalent" | ✓ VERIFIED | No `tide_cache_hit_rate` gauge in registry.go (grep). Hit ratio computed in `CacheEfficiencyPanel` as `cache_read / (cache_read + cache_creation)` PromQL (`TelemetryView.tsx` ~684). D-01 (CONTEXT.md:51-65) is the user-corrected, documented equivalent; not a scope reduction. |
| 7 | `CacheEfficiencyPanel` (`panel-cache-efficiency`) renders hit ratio + creation tokens + realized savings via existing `/api/v1/query_range`; no dispatch-path change (OBSV-03, D-04) | ✓ VERIFIED | `TelemetryView.tsx:640` `CacheEfficiencyPanel`, `data-testid="panel-cache-efficiency"`, trio + NaN→"—", three queries (read/creation/`tide_cache_savings_cents_total`) via `fetchQueryRange`. No inline Phase-20 caveat (D-04). `subagent.go` diff (847c56f) = one additive usage line; `claude -p --bare` invocation untouched. |
| 8 | Per-level `telemetry-level-selector` (BreakdownKind Project/Phase/Plan/Wave) drives `sum by(<dim>)(...)` across all panels | ✓ VERIFIED | `TelemetryView.tsx` — `BreakdownKind` type (6 refs), `breakdown` state + `breakdownRef` polling, `levelOptions` (4 opts), `SegmentedControl testId="telemetry-level-selector"` at line 1252; 10 buildQuery branches on breakdown; `fetchPanel` reads `matrix.metric[breakdown]` (line 366). |

**Score:** 8/8 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `internal/metrics/registry.go` | CacheSavingsCentsTotal var + NewCounterVec + MustRegister | ✓ VERIFIED | Lines 117, 226-232, 257 |
| `internal/subagent/anthropic/pricing.go` | `cacheSavingsCents` method | ✓ VERIFIED | Lines 133-150, truncation, firewall-safe |
| `pkg/dispatch/envelope.go` | `CacheSavingsCents int64` omitempty field | ✓ VERIFIED | Line 289 |
| `internal/controller/task_controller.go` | `.Add(float64(usage.CacheSavingsCents))` in emitTaskMetrics | ✓ VERIFIED | Line 1073 |
| `internal/metrics/registry_test.go` | arity test + seed/want | ✓ VERIFIED | Lines 72, 98, 232 |
| `internal/subagent/anthropic/pricing_test.go` | `TestCacheSavingsCents` 5 sub-tests | ✓ VERIFIED | Lines 259-310: haiku_1M, sonnet_1M, zero, truncation, unknown_model (5) |
| `dashboard/web/src/components/TelemetryView.tsx` | CacheEfficiencyPanel + BreakdownKind + selector | ✓ VERIFIED | Panel @640, selector @1252 |
| `dashboard/web/.../__tests__/TelemetryView.test.tsx` | cache-efficiency + level-selector suites; toHaveLength(5) | ✓ VERIFIED | toHaveLength(5)×3, toHaveLength(4)×0, both new describe blocks present |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| pricing.go `cacheSavingsCents` | `Usage.CacheSavingsCents` | subagent.go:346 | ✓ WIRED | `usage.CacheSavingsCents = a.cacheSavingsCents(...)` |
| `Usage.CacheSavingsCents` | `CacheSavingsCentsTotal` | emitTaskMetrics:1073 | ✓ WIRED | `.WithLabelValues(...).Add(float64(...))` |
| `CacheEfficiencyPanel` | `/api/v1/query_range` | fetchQueryRange (3 queries) | ✓ WIRED | hit-ratio/creation/savings; savings query references `tide_cache_savings_cents_total` |
| `BreakdownKind` state | PANELS buildQuery | `sum by(<dim>)(...)` injection | ✓ WIRED | 10 branch sites + panel queries + fetchPanel key-derivation |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Go pricing/registry/dispatch pkgs green | `go test ./internal/metrics ./internal/subagent/anthropic ./pkg/dispatch -count=1` | 3 pkgs `ok` | ✓ PASS |
| Controller emit test green | `go test ./internal/controller -run emitTaskMetrics -count=1` | `ok` | ✓ PASS |
| Go lint clean (metriccardinality + providerfirewall) | `make lint` | `0 issues.` | ✓ PASS |
| Frontend suite green | `npm run test` | `204 passed (204)`; TelemetryView 24 tests | ✓ PASS |
| TypeScript clean | `npm run lint` (tsc -b) | exit 0 | ✓ PASS |
| Full dashboard build | `make dashboard-frontend` | exit 0; embed dist rebuilt | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| OBSV-01 | 21-01, 21-02 | Per-level token accounting queryable | ✓ SATISFIED | Locked 4-label set on all counters (incl. new savings); arity guard test; UI level selector for browser slicing |
| OBSV-02 | 21-01 | Cache-hit-rate metric from cache_read vs cache_creation, emitted via Prometheus | ✓ SATISFIED | Savings counter emitted; hit-rate PromQL-derived from the two existing cache counters (D-01 equivalent) |
| OBSV-03 | 21-02 | Read-only dashboard cache-efficiency panel, no dispatch-path changes | ✓ SATISFIED | CacheEfficiencyPanel reads existing + new counters via query_range proxy; subagent.go dispatch path untouched (1 additive accounting line only) |

No orphaned requirement IDs — REQUIREMENTS.md maps OBSV-01/02/03 to Phase 21, all claimed by plans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| — | — | None | — | No debt markers (TBD/FIXME/XXX), no TODO/HACK/PLACEHOLDER, no stubs in any phase-21-modified file. "—" NaN display is intentional D-04 behavior. |

### Executor Deviation Review (validated as safe, not regressions)

| Deviation | Plan | Assessment |
| --- | --- | --- |
| `copyloopvar` fix in `internal/eval/render_test.go` (removed redundant `tc := tc`) | 21-01 | Pre-existing Phase-19-era code; formatting-only, Go 1.22+ loop semantics make it redundant. Rule-1 auto-fix to unblock `make lint`. Not masking a real problem. |
| `lll` fix in `cmd/credproxy/main.go` (wrapped 205-char `flag.StringVar`) | 21-01 | Pre-existing Phase-20-era CACHE-01 code; line-wrap only, no behavior change. Rule-1 auto-fix. Safe. |
| TS narrowing in TimeSeriesChart Tooltip formatters + `isSingleFailure` | 21-02 | Pre-existing recharts type mismatch (`ValueType`/`ReactNode`, `boolean\|undefined`); correctness-preserving narrowings needed for `tsc` clean. Confined to the pre-existing chart wrapper, not the new panel. Safe. |

### Human Verification Required

#### 1. Cache Efficiency panel renders live in a running cluster

**Test:** Deploy the dashboard against a cluster with Prometheus scraping TIDE, open TelemetryView, confirm the Cache Efficiency panel shows hit ratio %, cache-creation tokens, and realized savings $ from live counters.
**Expected:** Fifth panel "Cache Efficiency" renders the trio + hit-rate sparkline from real `tide_tokens_cache_*` and `tide_cache_savings_cents_total` series.
**Why human:** Requires a live cluster with completed Tasks and a Prometheus scrape target; wiring and degradation are unit-verified but live PromQL rendering is not.

#### 2. Per-level breakdown selector slices in the browser

**Test:** Click the Project/Phase/Plan/Wave selector and confirm every panel re-queries with `sum by(<dim>)` and renders per-level series.
**Expected:** Phase/Plan/Wave switch all five panels to grouped series; Project restores totals.
**Why human:** Visual confirmation of grouped series against live data; the query-string switch is unit-tested but the rendered breakdown is not.

### Gaps Summary

No gaps. All 8 observable truths, 8 artifacts, 4 key links, and 3 requirements (OBSV-01/02/03) are verified against live code. Go tests (4 packages), `make lint` (0 issues, firewall + cardinality analyzers green), vitest (204/204), `tsc` (clean), and `make dashboard-frontend` (exit 0) all confirmed by independent re-run.

The ROADMAP criterion-2 tension ("`tide_cache_hit_rate` gauge or equivalent") is resolved by locked decision D-01: no new gauge is emitted; the hit ratio is PromQL-derived from the existing `cache_read`/`cache_creation` counters, and the new `tide_cache_savings_cents_total` counter supplies the emitted-metric half. This is the documented, user-corrected equivalent — counts as VERIFIED, not a deviation.

Status is **human_needed** (not passed) solely because the phase goal — "visible on the dashboard … in a running cluster" — has a visual/live-data component that grep and unit tests cannot confirm. All automated checks pass; two human spot-checks remain.

---

_Verified: 2026-06-16T05:45:00Z_
_Verifier: Claude (gsd-verifier)_
