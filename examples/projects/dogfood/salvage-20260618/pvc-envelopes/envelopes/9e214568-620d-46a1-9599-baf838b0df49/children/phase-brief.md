# Phase Brief — phase-01-codex-pricing-usage

**Phase:** phase-01-codex-pricing-usage  
**Milestone:** milestone-03-hetero-integration  
**Status:** Planned  
**Wave:** 1 (no dependencies — dispatches in parallel with phase-04-per-level-vendor-switch)

---

## Objective

Author the pure-Go pricing and usage-normalization foundation for the Codex
subagent — the layer that maps OpenAI's token-usage block into
`pkg/dispatch.Usage` and prices each model against a cents-per-MTok table
mirroring `internal/subagent/anthropic/pricing.go`. No CLI invocation, no
container, no controller touch — the deliverable boundary is a compilable
Go package (`internal/subagent/codex/`) containing `pricing.go`, `usage.go`,
and their exhaustive offline tests.

---

## Scope

**In scope:**

- `internal/subagent/codex/pricing.go` — `modelPrice` struct + `priceTable`
  keyed on OpenAI/Codex model IDs; `conservativeTier` fallback (most-expensive
  known tier); `estimatedCostCents` method on a `Codex` receiver — exact mirror
  of the Anthropic pattern. Initial model set: `codex-1` (the shipping Codex
  model). Cache pricing: `cacheRead` at the OpenAI discounted cached-input rate
  (~50% of input); **no `cacheWrite` concept** (OpenAI caching is automatic,
  marker-free — `CacheCreationTokens` stays 0 always).

- `internal/subagent/codex/usage.go` — `normalizeUsage` function that maps an
  OpenAI usage response struct into `pkg/dispatch.Usage` per the D5 contract:
  `prompt_tokens → InputTokens`, `completion_tokens → OutputTokens`,
  `prompt_tokens_details.cached_tokens → CacheReadTokens`,
  `CacheCreationTokens` fixed at 0. Computes `CacheSavingsCents` (the delta
  between full input price and discounted cache-read price for the cached
  tokens). `EstimatedCostCents` delegated to `estimatedCostCents`.

- `internal/subagent/codex/doc.go` — package-level doc referencing the D-C1
  layering pattern and the D-D2 Usage contract; Apache 2.0 header.

- `internal/subagent/codex/pricing_test.go` — comprehensive offline tests
  covering: per-model table correctness (input/output/cacheRead rates), ceiling
  division (sub-cent rounds up), unknown-model conservative fallback (non-zero,
  ≥ any known-model rate), zero-tokens → 0 cents, `PricingOverrides` merge
  (per-instance table clone, package-level var never mutated), and the
  `CacheCreationTokens == 0` invariant for all Codex models.

- `internal/subagent/codex/usage_test.go` — tests for `normalizeUsage`:
  field mapping correctness, 1,024-token cache floor handling, zero usage, and
  `CacheSavingsCents` computation.

**Out of scope for this phase:** the Codex CLI runner (`run.go`, `client.go`,
`stream_parser.go`), the container image (`Dockerfile`), the cmd entrypoint,
and any controller or chart changes.

---

## Deliverables

1. `internal/subagent/codex/doc.go` — package doc, Apache 2.0 header.
2. `internal/subagent/codex/pricing.go` — price table, `estimatedCostCents`.
3. `internal/subagent/codex/usage.go` — `normalizeUsage`, OpenAI usage struct.
4. `internal/subagent/codex/pricing_test.go` — table/cost/fallback tests.
5. `internal/subagent/codex/usage_test.go` — normalization tests.

---

## Verification Gates

| Gate | Command | Pass condition |
|------|---------|---------------|
| Compile | `go build ./internal/subagent/codex/...` | Exit 0, zero errors |
| Unit tests | `go test ./internal/subagent/codex/...` | Exit 0, all subtests pass |
| Full suite | `make test` | Exit 0, no regressions |
| Import firewall | `make verify-import-firewall` | No Codex/OpenAI imports in controller or cmd/manager |
| Dispatch imports | `make verify-dispatch-imports` | Green |
| Zero OpenAI calls | Run tests offline (no `OPENAI_API_KEY` set) | Exit 0 |

---

## Interface Contracts (locked — phase-02 depends on these)

- **Vendor sentinel:** the `"openai"` string is the compile-time vendorSentinel
  used by phase-02's `run.go` and matched by phase-04's `ResolveProvider`.
  Authored here as a package-level constant in `doc.go` or `pricing.go`.
- **`normalizeUsage` signature:** `normalizeUsage(model string, raw openaiUsage) pkg/dispatch.Usage`
  — phase-02's `run.go` imports this directly.
- **`CacheCreationTokens == 0` invariant:** the zero-cache-creation guarantee
  (OpenAI has no explicit cache-write concept) is tested here and relied upon
  by the budget rollup (phase-06 exit criterion 8).
