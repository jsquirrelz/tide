---
phase: 14-budget-enforcement-pricing
plan: "01"
subsystem: pricing
tags: [pricing, budget, provider-firewall, go]
dependency_graph:
  requires: []
  provides:
    - pkg/dispatch.PriceOverride
    - pkg/dispatch.ParsePricingOverrides
    - internal/subagent/anthropic six-entry priceTable (D-01)
    - internal/subagent/anthropic per-instance effective table via maps.Clone (D-02/T-14-02)
    - TIDE_PRICING_OVERRIDES_JSON env transport (jobspec + claude-subagent)
  affects:
    - internal/subagent/anthropic cost computation path
    - internal/dispatch/podjob Job construction
    - cmd/claude-subagent binary startup
tech_stack:
  added:
    - stdlib "maps" package (Go 1.26, maps.Clone for T-14-02 race safety)
  patterns:
    - per-instance effective price table via maps.Clone + override merge
    - env var transport (TIDE_PRICING_OVERRIDES_JSON) from jobspec through subagent container
    - provider-agnostic override type in pkg/dispatch (D-C1 firewall intact)
key_files:
  created:
    - pkg/dispatch/pricing.go
    - pkg/dispatch/pricing_test.go
  modified:
    - internal/subagent/anthropic/pricing.go
    - internal/subagent/anthropic/pricing_test.go
    - internal/subagent/anthropic/subagent.go
    - internal/dispatch/podjob/jobspec.go
    - internal/dispatch/podjob/jobspec_test.go
    - cmd/claude-subagent/main.go
    - cmd/claude-subagent/main_test.go
    - cmd/claude-subagent/commit_test.go
decisions:
  - "estimatedCostCents converted from package func to *Anthropic method so it reads a.prices (per-instance clone) rather than the package-level var — satisfies T-14-02 without introducing a mutex"
  - "PriceOverride type lives in pkg/dispatch not internal/subagent/anthropic so cmd/manager can validate the JSON at startup (Plan 14-05) without importing the anthropic package (D-C1 provider firewall)"
  - "Cache field auto-derivation in New() uses integer arithmetic (input/10 and input*125/100) matching Anthropic prompt-caching rate schedule"
  - "parsePricingOverridesFromEnv in cmd/claude-subagent logs loud warning and falls back to compiled table on invalid JSON (defense-in-depth; manager validates at startup in Plan 14-05)"
metrics:
  duration: "~45 minutes"
  completed: "2026-06-11"
  tasks_completed: 3
  tasks_total: 3
  files_changed: 10
---

# Phase 14 Plan 01: Pricing Table Correction + Override Transport Summary

Corrected six-entry Anthropic priceTable with D-01 verified rates, added provider-firewalled override mechanism end-to-end from jobspec env stamp through claude-subagent parse to per-instance effective table.

## Tasks Completed

| # | Name | Commit | Key Files |
|---|------|--------|-----------|
| 1 | Correct six-entry priceTable + conservativeTier = fable-5 | `dccb98c` | `internal/subagent/anthropic/pricing.go`, `pricing_test.go` |
| 2 | Provider-agnostic PriceOverride + ParsePricingOverrides in pkg/dispatch | `ec5b087` | `pkg/dispatch/pricing.go`, `pricing_test.go` |
| 3 | Override merge in anthropic.New + env transport | `5cfdf9c` | `subagent.go`, `jobspec.go`, `main.go` + tests |
| — | Lint fixes (lll + gofmt) | `c5b488e` | `pkg/dispatch/pricing.go`, `jobspec_test.go` |

## What Was Built

**D-01: Price table correction.** The compiled `priceTable` in `internal/subagent/anthropic/pricing.go` now has 6 entries:
- `claude-fable-5` ($10/$50/MTok input/output) — new entry, now the `conservativeTier` fallback
- `claude-opus-4-8` ($5/$25/MTok) — new entry
- `claude-opus-4-7` ($5/$25/MTok) — corrected from $15/$75 (3× over-billing)
- `claude-opus-4-6` ($5/$25/MTok) — new entry
- `claude-sonnet-4-6` ($3/$15/MTok) — unchanged
- `claude-haiku-4-5` ($1/$5/MTok) — unchanged

Sessions on current model IDs no longer fall to the conservative default ("pricing: unknown model" warning gone for the six IDs in use).

**D-02 (provider side): Per-instance effective table via maps.Clone.** `New(opts)` clones `priceTable` and merges `opts.PricingOverrides` into the per-instance `a.prices` map. `estimatedCostCents` is now a method that reads `a.prices`, never the package-level var. `go test -race` confirms no concurrent write to shared state (T-14-02).

**Override transport (D-02 env plumbing).** `BuildOptions.PricingOverridesJSON` → `TIDE_PRICING_OVERRIDES_JSON` env on both `JobKindExecutor` and `JobKindPlanner` subagent containers. `cmd/claude-subagent` reads the env at startup, parses via `pkgdispatch.ParsePricingOverrides`, and passes the map as `Options.PricingOverrides`. Invalid JSON logs a loud warning and falls back to the compiled table (defense-in-depth; manager-side validation deferred to Plan 14-05).

**Provider-agnostic contract (pkg/dispatch/pricing.go).** `PriceOverride` struct and `ParsePricingOverrides` live in `pkg/dispatch` so `cmd/manager` can validate operator-supplied pricing JSON at startup without importing `internal/subagent/anthropic` — the D-C1 provider firewall stays intact.

## Verification Results

```
go test ./internal/subagent/anthropic/... ./pkg/dispatch/... ./internal/dispatch/podjob/...
ok    internal/subagent/anthropic   (all 20 subtests including fable5, opus47 regression, override_merge)
ok    pkg/dispatch                  (all 12 subtests including zero-input rejection with model ID in error)
ok    internal/dispatch/podjob      (all existing + 2 new TIDE_PRICING_OVERRIDES_JSON tests)

go test -race ./internal/subagent/anthropic/... → ok (T-14-02 race clean)
golangci-lint → 0 issues
grep 'outputCentsPerMTok:     7500' internal/subagent/anthropic/pricing.go → 0 lines
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Converted estimatedCostCents from package function to receiver method**
- **Found during:** Task 3 implementation
- **Issue:** The plan specified converting to a method, but the existing test file called it as a bare function. After conversion, all test calls broke.
- **Fix:** Rewrote `pricing_test.go` to use `a := New(Options{})` and call `a.estimatedCostCents(...)` throughout, adding the `override_merge` and `new_model_via_override` test cases in the same pass.
- **Files modified:** `internal/subagent/anthropic/pricing_test.go`
- **Commit:** `5cfdf9c`

**2. [Rule 3 - Blocking] Updated cmd/claude-subagent test seams for new newSubagent signature**
- **Found during:** Task 3 — `newSubagent` func signature changed from 2 to 3 params
- **Issue:** `main_test.go` and `commit_test.go` replace the `newSubagent` var in tests; the signature mismatch prevented compilation.
- **Fix:** Added `_ map[string]pkgdispatch.PriceOverride` as ignored third parameter in all four test seam replacements.
- **Files modified:** `cmd/claude-subagent/main_test.go`, `cmd/claude-subagent/commit_test.go`
- **Commit:** `5cfdf9c`

**3. [Rule 1 - Lint] Line-too-long and gofmt offenses**
- **Found during:** Post-Task 3 lint run
- **Issue:** `pkg/dispatch/pricing.go` had 4 lines exceeding 120 chars (lll); `jobspec_test.go` had a gofmt whitespace issue.
- **Fix:** Broke long error format strings onto two lines; fixed map literal alignment.
- **Files modified:** `pkg/dispatch/pricing.go`, `internal/dispatch/podjob/jobspec_test.go`
- **Commit:** `c5b488e`

**Out-of-scope pre-existing issue logged:**
- `cmd/tide-demo-init/main.go:112` has a broken embed pattern (`//go:embed all:fixture`) causing `go build ./...` to fail on that binary. Unrelated to this plan; deferred to appropriate future plan.

## Known Stubs

None — all data paths are wired. The controller call site for `BuildOptions.PricingOverridesJSON` is intentionally deferred to Plan 14-05 per plan specification ("Populating the new BuildOptions field at the controller call sites is Plans 14-03/14-05 — do NOT touch the controllers in this plan").

## Threat Flags

No new threat surface beyond what the plan's threat model already covers (T-14-01 through T-14-SC).

## Self-Check: PASSED

Created files:
- [x] `pkg/dispatch/pricing.go` — exists
- [x] `pkg/dispatch/pricing_test.go` — exists

Commits exist:
- [x] `dccb98c` — feat(14-01): correct six-entry priceTable
- [x] `ec5b087` — feat(14-01): add PriceOverride type + ParsePricingOverrides
- [x] `5cfdf9c` — feat(14-01): override merge + TIDE_PRICING_OVERRIDES_JSON transport
- [x] `c5b488e` — style(14-01): fix lint offenses
