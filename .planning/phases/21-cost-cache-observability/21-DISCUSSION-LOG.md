# Phase 21: Cost & Cache Observability - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-15
**Phase:** 21-Cost & Cache Observability
**Areas discussed:** Hit-rate source, Realized-savings formula, Panel framing, OBSV-01 scope, Savings-metric shape, Panel form

---

## Hit-rate source (OBSV-02 derivation)

| Option | Description | Selected |
|--------|-------------|----------|
| PromQL-derived in dashboard | No new metric; panel computes the ratio from existing cache_read/cache_creation counters | ✓ (initial) |
| Prometheus recording rule | New PrometheusRule chart template emitting `tide:cache_hit_rate:ratio` | |
| Backend recording gauge | Gauge in internal/metrics set from counters via a collector goroutine | |

**User's choice:** PromQL-derived in dashboard — **later refined** (see "Savings-metric shape").
**Notes:** Original answer was "no new metric at all." User then asked whether dollars are already emitted as a metric; on confirming `tide_cost_cents_total` exists, the user retracted the no-metric stance **for the dollar savings figure** and chose to emit savings as a metric. The unitless hit-*ratio* stays PromQL-derived (ratios don't aggregate as counters).

---

## Realized-savings formula (OBSV-02/03)

| Option | Description | Selected |
|--------|-------------|----------|
| read/(read+creation) + 0.9× savings | hit = cache_read/(cache_read+cache_creation); savings = cache_read × 0.90 × input_price | ✓ |
| read/(read+input) ratio | hit = cache_read/(cache_read+input) ("coverage" framing) | |
| You decide | Defer formula to research/planning | |

**User's choice:** read/(read+creation) + 0.9× savings.
**Notes:** Matches the locked Anthropic pricing model (read = 0.10× input, write = 1.25× input) in `pricing.go`.

---

## Panel framing (OBSV-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Show metrics + honest caveat | Render numbers plus a tooltip noting hits reflect CLI scaffold (CACHE-F1 deferred) | |
| Raw metrics only | Just the numbers, no caveat | ✓ |

**User's choice:** Raw metrics only.
**Notes:** Trade-off accepted — a low hit-rate could be misread as "TIDE caching broken" rather than "caller-content caching awaits CACHE-F1"; user prioritized panel simplicity and consistency with the other caveat-free panels. Phase-20 context preserved in PROJECT.md / CONTEXT.md.

---

## OBSV-01 scope

| Option | Description | Selected |
|--------|-------------|----------|
| Verify-only + PromQL examples | Confirm labels satisfy per-level queryability; document example PromQL; no UI change | |
| Add per-level breakdown UI | phase/plan/wave dropdown/breakdown in TelemetryView | ✓ |

**User's choice:** Add per-level breakdown UI.
**Notes:** Labels already exist (Phase 16), so criterion 1 is satisfied at the backend; user wants operators to slice spend per level in the browser. Grouped PromQL over existing counters — no new metric.

---

## Savings-metric shape (refinement after the cost-metric question)

| Option | Description | Selected |
|--------|-------------|----------|
| Provider-computed, emit counter | Compute savings cents behind the provider firewall (next to estimatedCostCents), carry on Usage, emit `tide_cache_savings_cents_total{project,phase,plan,wave}` | ✓ |
| Also emit total cache-read cost | Above + a separate cache-read-cost counter | |
| You decide | Baseline = provider-computed counter; defer the extra counter | |

**User's choice:** Provider-computed, emit counter (single savings counter).
**Notes:** Resolves the price-source problem — the controller has no pricing but the provider does. Not a dispatch-path change (accounting in the existing reconcile rollup), so OBSV-03 is honored; satisfies criterion 2's literal "metric emitted." Second cache-read-cost counter rejected as redundant (savings = 9× read cost at the fixed 0.10× rate).

---

## Panel form (OBSV-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Single-stat trio + hit-rate sparkline | Three glanceable stats (hit %, creation tokens, savings $) + sparkline | ✓ |
| Hit-rate time-series panel | Full time-series line of hit-ratio in the existing panel style | |
| You decide | Defer form to research/planning | |

**User's choice:** Single-stat trio + hit-rate sparkline.
**Notes:** Complements the existing detailed stacked "Token Breakdown" chart rather than duplicating it.

---

## Claude's Discretion

- Exact `Usage` field name (`CacheSavingsCents`) and Prometheus metric name/help text (`tide_cache_savings_cents_total`), matching existing conventions.
- Whether savings cents is computed in `estimatedCostCents` itself or a sibling helper in `pricing.go`.
- Concrete React shape of the trio + sparkline (which existing panel/component to clone) and how the per-level selector composes with the existing scope/range controls.
- Whether per-level breakdown is a dropdown on existing panels or a dedicated panel.
- PromQL window/rate functions for the hit-rate ratio.

## Deferred Ideas

- CACHE-F1 direct-SDK backend (realizes caller-content caching) — out of scope (dispatch-path change); future direct-SDK milestone. Also the reviewed-not-folded todo `cache-f1-direct-sdk-cross-pod-caching.md`.
- Per-provider usage normalizer — run-#2 multi-provider milestone.
- Second `tide_cache_read_cost_cents_total` counter — redundant for now.
- Per-Task/Phase/Plan CRD `.status` token/cost fields — not needed (labels already attribute per level).
- Inline panel caveat about CLI-scaffold cache reality — set aside (D-04).
