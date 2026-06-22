# PLAN.md — plan-02-codex-pricing-table

**Plan:** plan-02-codex-pricing-table  
**Phase:** phase-01-codex-pricing-usage (Milestone 03 — Heterogeneous Provider Dispatch)  
**Date:** 2026-06-18  
**Status:** Authoring tasks

---

## Objective

Lay the pure-Go cost/usage foundation for the Codex (OpenAI) subagent. Deliver
`internal/subagent/codex/pricing.go` (per-model pricing table mirroring
`anthropic/pricing.go`; OpenAI-specific field mapping with no `cacheWrite`
concept), a `doc.go` package declaration, and a comprehensive offline test
suite in `pricing_test.go`.

**Scope boundary:** No CLI invocation, no container, no controller, no CRD
changes. Only the new `internal/subagent/codex/` package directory and its
three files. All code stays behind the `pkg/dispatch.Subagent` interface
firewall (D-C1).

---

## Context — What Already Exists (Do Not Rebuild)

The following infrastructure ships in main and is consumed as-is:

- `pkg/dispatch.Usage` — the provider-agnostic token/cost struct this plan
  populates. Fields: `InputTokens`, `OutputTokens`, `CacheReadTokens`,
  `CacheCreationTokens`, `EstimatedCostCents`, `Iterations`.
- `internal/subagent/anthropic/pricing.go` — the reference implementation
  this plan mirrors. Pattern: `modelPrice` struct, `priceTable` map,
  `conservativeTier` fallback, `estimatedCostCents` helper function. Mirror
  this pattern exactly.
- `pkg/dispatch.PriceOverride` and `ParsePricingOverrides` — provider-agnostic
  override mechanism used by the claude-subagent shim. Phase-02 will wire these
  for the codex shim; phase-01 does not need to call them.
- Provider firewall enforcement at `make verify-import-firewall` and
  `make verify-dispatch-imports` — zero Codex imports must appear in
  `internal/controller/` or `cmd/manager/`.

---

## Task DAG

```
task-01-codex-pricing-impl  (wave 1, no deps)
         │
         ▼
task-02-codex-pricing-tests (wave 2, depends on task-01)
```

Two-task, two-wave plan. Wave 1 establishes the package and pricing logic;
wave 2 proves correctness with offline unit tests.

---

## Tasks

### task-01-codex-pricing-impl

**Objective:** Create the `internal/subagent/codex/` package with package
documentation (`doc.go`) and the pricing/usage-normalization implementation
(`pricing.go`).

**Files touched:**
- `internal/subagent/codex/doc.go` (create)
- `internal/subagent/codex/pricing.go` (create)

**Acceptance:** `go build ./internal/subagent/codex/` succeeds. Both files are
present on disk with Apache 2.0 headers. `pricing.go` exports `NormalizeUsage`,
`EstimatedCostCents`, and `CacheSavingsCents` functions and declares at least
three model entries in the package-level `priceTable`.

---

### task-02-codex-pricing-tests

**Objective:** Create `internal/subagent/codex/pricing_test.go` with unit
tests that prove the pricing table math, usage normalization, and
`CacheSavingsCents` computation are correct — all offline, no API calls.

**Files touched:**
- `internal/subagent/codex/pricing_test.go` (create)

**Depends on:** task-01-codex-pricing-impl

**Acceptance:** `go test ./internal/subagent/codex/` exits 0 with at minimum:
- `TestNormalizeUsage` passes (field mapping + CacheCreationTokens == 0 invariant)
- `TestEstimatedCostCents_KnownModel` passes
- `TestEstimatedCostCents_UnknownModel` passes (conservative fallback ≥ any
  known-model estimate for same token count)
- `TestCacheSavingsCents` passes
- `TestPriceTable_ConservativeTierIsMostExpensive` passes

---

## Acceptance Criteria (Plan-Level)

All of the following must be verifiable from disk after both tasks complete:

1. `internal/subagent/codex/doc.go` exists with Apache 2.0 header and
   `package codex` declaration referencing D-C1.
2. `internal/subagent/codex/pricing.go` exists with Apache 2.0 header,
   `package codex`, and exports `NormalizeUsage`, `EstimatedCostCents`,
   `CacheSavingsCents`.
3. `internal/subagent/codex/pricing_test.go` exists with ≥ 5 test functions.
4. `go build ./internal/subagent/codex/` exits 0.
5. `go test ./internal/subagent/codex/` exits 0 (fully offline — no
   `OPENAI_API_KEY` required).
6. `grep -r "internal/subagent/codex" internal/controller/ cmd/manager/` returns
   empty (provider firewall intact — D-C1).

---

## Constraints

- Apache 2.0 license header on all new files.
- No real OpenAI API call in any file or test — 100% offline.
- No import of `internal/controller/`, `cmd/manager/`, or any OpenAI SDK in
  the codex package (provider firewall D-C1).
- `CacheCreationTokens` in `NormalizeUsage` output is always 0 — OpenAI
  caching is automatic (marker-free, 1,024-token floor) with no cache-write
  billing concept.
- The conservative fallback on priceTable miss must use the most expensive
  known tier (never under-report spend — T-09-01).
- `CacheSavingsCents` = cost avoided by serving `cacheReadTokens` at the
  discounted cache rate vs the full input rate. Negative savings (cacheRead
  rate ≥ input rate) clamp to 0.
