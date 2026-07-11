---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
plan: 06
subsystem: subagent
tags: [pricing, cost-tally, prompt-caching, normalizer, regression-fixture]
requires:
  - "38-01: COST-03 probe verdict (cacheWriteMultiplier = 125/100) and the zero-recovery usage fixture"
  - "38-02: Usage.PricingFallbackModel field at pkg/dispatch/envelope.go:322"
provides:
  - "claude-sonnet-5 price row at sticker rates 300/1500/30/375 cents/MTok (COST-01)"
  - "lookupPrice D-01 normalizer (exact -> one -YYYYMMDD strip retry -> conservative tier) shared by estimatedCostCents and cacheSavingsCents"
  - "cacheWriteMultNum/cacheWriteMultDen = 125/100 single-sourced with 38-01-PROBE-RESULT.md citation (D-08 / COST-03)"
  - "Usage.PricingFallbackModel stamped on post-normalizer misses in envelope assembly (D-02 provider side, feeds 38-02's condition + metric)"
  - "Run-mix regression: 2026-07-03 replay pinned at 473 cents, < 615 and < 1086 bounds (D-04)"
affects: [38-02-consumers, budget-rollup]
tech-stack:
  added: []
  patterns: ["single-sourced rate multiplier constant with in-repo probe-evidence citation (D-08)"]
key-files:
  created:
    - internal/subagent/anthropic/run_mix_regression_test.go
  modified:
    - internal/subagent/anthropic/pricing.go
    - internal/subagent/anthropic/subagent.go
    - internal/subagent/anthropic/pricing_test.go
    - internal/subagent/anthropic/subagent_test.go
key-decisions:
  - "cacheWriteMultiplier compiled as 125/100 per the 38-01 probe verdict (only {\"type\":\"ephemeral\"} cache_control observed, 5m TTL) — existing per-row literals already complied, zero numeric change to old rows"
  - "sonnet-5 row compiled at sticker $3/$15 (not intro $2/$10 which runs through 2026-08-31) — never under-counts, durable past the intro window (RESEARCH Pitfall 1)"
  - "sonnet-5 cacheWrite expressed as the constant expression 300 * cacheWriteMultNum / cacheWriteMultDen (a genuine derivation site) rather than a bare 375 literal"
  - "Run-mix replay uses a provenance-disclosed synthetic reconstruction (fixture dispatches: [] after the PVC loss) that reproduces BOTH documented aggregates exactly: 1086c under the old conservative fallback and 384c under intro pricing"
patterns-established: []
requirements-completed: [COST-01, COST-03]
duration: 15min
completed: 2026-07-11
metrics:
  tasks: 2
  files_modified: 5
  commits: 3
status: complete
---

# Phase 38 Plan 06: Sonnet-5 Pricing Row + D-01 Normalizer + Run-Mix Regression Summary

**claude-sonnet-5 priced at sticker rates ($3/$15) through a D-01 date-suffix normalizer shared by both cost paths, cache-write premium single-sourced as 125/100 citing the 38-01 probe verbatim, Usage.PricingFallbackModel stamped on post-normalizer misses, and the $10.86-vs-$3.84 first-run overcount locked as a pinned 473-cent regression.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-07-11T13:22:39Z
- **Completed:** 2026-07-11T13:38:00Z
- **Tasks:** 2 (both auto; task 1 TDD)
- **Files modified:** 5 (1 created, 4 modified), all inside `internal/subagent/anthropic/` (provider firewall held)

## Accomplishments

- **COST-01:** `claude-sonnet-5` row added at sticker rates — input 300, output 1500, cacheRead 30, cacheWrite `300 * cacheWriteMultNum / cacheWriteMultDen` (= 375) cents/MTok. Row comment records the intro-pricing caveat ($2/$10 through 2026-08-31; sticker compiled deliberately).
- **D-01 normalizer:** `lookupPrice(model)` — exact `a.prices` hit, then exactly one trailing `-\d{8}$` strip retry, then `(conservativeTier, false)`. NO family prefix-matching (T-38-16); both `estimatedCostCents` (loud stderr warning retained) and `cacheSavingsCents` (silent, per contract) resolve through it.
- **D-08 / COST-03:** `cacheWriteMultNum`/`cacheWriteMultDen` = **125/100**, with a comment citing 38-01-PROBE-RESULT.md verbatim (probe date 2026-07-11, `claude --version` 2.1.207, model claude-haiku-4-5, only `"cache_control":{"type":"ephemeral"}` observed, no `"ttl":"1h"` anywhere, host-CLI caveat) plus the flip rule (5m → 1.25×; 1h → 2×). At 125/100 every existing row literal already complied — zero numeric change to old rows, locked by `TestCacheWriteMultiplierConsistency`.
- **D-02 provider side:** envelope assembly stamps `usage.PricingFallbackModel = in.Provider.Model` when `lookupPrice` misses post-normalizer, feeding plan 38-02's `PricingFallbackActive` condition and Prometheus counter. Empty for exact hits AND normalizer hits.
- **D-04 regression:** `TestRunMixRegression_FirstRun20260703` replays the run mix through the new table with three separate locks: pinned exact **473 cents**, `< 615` (1.6 × the console's 384¢ — Pitfall-1 tolerance), and `< 1086` (the 2.8× overcount headline) — each with its own failure message.

## Task Commits

Each task was committed atomically (task 1 via TDD RED/GREEN):

1. **Task 1 RED: failing tests for row/normalizer/multiplier/fallback** - `88351d0` (test)
2. **Task 1 GREEN: sonnet-5 row + lookupPrice + multiplier consts + flag stamping** - `3a91a85` (feat)
3. **Task 2: run-mix regression fixture test** - `13b97a9` (test)

## Files Created/Modified

- `internal/subagent/anthropic/pricing.go` - sonnet-5 row, `dateSuffixRe`, `lookupPrice`, `cacheWriteMultNum`/`Den` consts with probe citation, both cost paths rewired, header rate list updated
- `internal/subagent/anthropic/subagent.go` - `PricingFallbackModel` stamped in envelope assembly on post-normalizer miss
- `internal/subagent/anthropic/pricing_test.go` - sonnet-5 row pins, normalizer behaviors (dated hit, no family matching, unknown+date, override-through-normalizer), multiplier consistency, savings-shares-normalizer
- `internal/subagent/anthropic/subagent_test.go` - `TestRun_PricingFallbackModel` (unknown stamped / known empty / dated-known empty) via the fake-exec fixture
- `internal/subagent/anthropic/run_mix_regression_test.go` - D-04 replay with CACHE-01-style evidence header and reconstruction disclosure

## Decisions Made

- **Multiplier value 125/100** per the unambiguous 38-01 probe verdict; the comment carries the host-CLI 2.1.207 evidence-grade caveat so a future pinned-CLI divergence triggers a re-probe, not an archaeology session.
- **Sticker over intro pricing** for the sonnet-5 row — never under-counts, no 2026-09-01 cliff.
- **Reconstruction constrained to both aggregates:** the synthetic token counts were solved so the old conservative-fallback replay lands at exactly 1086¢ ($10.86) and the intro-priced replay at exactly 384¢ ($3.84) — making the synthetic mix maximally faithful to the only surviving ground truth.

## Deviations from Plan

**1. [Pre-authorized D-04 fallback] Run-mix replay uses synthetic reconstruction, not real per-dispatch usage**
- **Found during:** Task 2
- **Issue:** `testdata/first_run_2026-07-03_usage.json` ships `dispatches: []` — the 38-01 export was zero-recovery (the tide-cashboard PVC, reclaimPolicy Delete, was destroyed before export). Real per-dispatch token counts do not exist.
- **Adaptation:** the test loads the fixture and, only when the dispatches array is empty, falls back to a synthetic 7-dispatch set (3 sonnet-5 + 1 fable-5 + 3 haiku-4-5, per the provenance's prose mix) whose counts reproduce both documented run-level aggregates exactly (1086¢ old-table / 384¢ intro-priced). The reconstruction is disclosed in the file header per the plan's empty-array branch; real dispatches added to the fixture later take precedence automatically (pin must then be re-derived).
- **Files modified:** internal/subagent/anthropic/run_mix_regression_test.go
- **Commit:** 13b97a9

No other deviations — no Rule 1-3 auto-fixes were needed.

## Verification

- `go test ./internal/subagent/anthropic/ -run 'TestEstimatedCostCents|TestCacheSavingsCents|TestCostParity|Lookup|Normalizer|Multiplier|Fallback' -v` — all pass; all pre-existing conservative-tier fixtures unchanged and green
- `go test ./internal/subagent/anthropic/ -run RunMix -v` — pass (pinned 473¢ verified on first run)
- `make test` unit tier — **MAKE_EXIT=0**, no `--- FAIL` lines (full envtest unit tier green)
- Acceptance greps: `"claude-sonnet-5"` in pricing.go == 1; `cacheWriteMultNum` count == 5 (>= 2); only the `conservativeTier` initializer reads `priceTable[` (T-14-02); no file outside `internal/subagent/anthropic/` touched by code tasks
- `hack/check-pricing-drift.sh` post-check: non-blocking — the published pricing page fetch returned 404s (network/page-layout dependent); script exited 0

## Output (per plan spec)

- **Pinned fixture sum:** 473 cents
- **Multiplier compiled:** cacheWriteMultiplier = 125/100 (5-minute TTL, per 38-01-PROBE-RESULT.md)

## Next Phase Readiness

- Plan 38-02's rollup surface now receives real `PricingFallbackModel` stamps from the provider on genuinely-unknown models only (normalizer hits excluded).
- If Anthropic ships a 1-hour-TTL default or the pinned subagent-image CLI diverges: flip `cacheWriteMultNum` to 200, update per-row literals, and `TestCacheWriteMultiplierConsistency` enforces the rest.

## Self-Check: PASSED

- FOUND: internal/subagent/anthropic/pricing.go
- FOUND: internal/subagent/anthropic/run_mix_regression_test.go
- FOUND: commit 88351d0 (Task 1 RED)
- FOUND: commit 3a91a85 (Task 1 GREEN)
- FOUND: commit 13b97a9 (Task 2)
- `grep -c 'PricingFallbackModel' internal/subagent/anthropic/subagent.go` == 1 (stamp site)
- `make test` MAKE_EXIT=0

---
*Phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud*
*Completed: 2026-07-11*
