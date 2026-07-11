---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
plan: 02
subsystem: controller-observability
tags: [cost, pricing, conditions, prometheus, envelope, provider-firewall]
requires: []
provides:
  - "Usage.PricingFallbackModel wire field (pkg/dispatch/envelope.go) — consumed by plan 38-06's provider-side flag setter"
  - "ConditionPricingFallbackActive + ReasonUnknownModelPriced constants (api/v1alpha2/shared_types.go)"
  - "setPricingFallbackIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha2.Project, fallbackModel string) error (internal/controller/pricing_fallback.go)"
  - "tide_pricing_fallback_total{project, model} counter (internal/metrics/registry.go)"
affects: [38-06]
tech-stack:
  added: []
  patterns:
    - "set*IfNeeded sticky-condition helper (billing_halt.go family), informational variant — no check* dispatch gate"
    - "fallback hook placed inside the exactly-once rollup guards at every budget.RollUpUsage site"
key-files:
  created:
    - pkg/dispatch/envelope_fallback_test.go
    - internal/controller/pricing_fallback.go
    - internal/controller/pricing_fallback_test.go
  modified:
    - pkg/dispatch/envelope.go
    - api/v1alpha2/shared_types.go
    - internal/metrics/registry.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go
decisions:
  - "Metric counts every fallback-priced rollup; condition dedupes on repeat rollups of the same model (Message-contains check) to avoid status churn"
  - "Hook placed after the rollup if/else inside the RolledUpUID marker guard at planner sites, so the exactly-once guards bound the metric"
  - "Message embeds the model ID via %q only — no formatting directives from envelope content (T-38-06)"
metrics:
  duration: 13m
  completed: 2026-07-11
status: complete
---

# Phase 38 Plan 02: Pricing-Fallback Observability Summary

Provider-neutral `Usage.PricingFallbackModel` envelope field rolled up at all seven controller budget-rollup sites into a sticky informational `PricingFallbackActive` Project condition plus a `tide_pricing_fallback_total{project, model}` Prometheus counter (COST-02 / D-02).

## Contract for plan 38-06 (provider side)

- **Wire field:** `Usage.PricingFallbackModel string` with JSON tag `pricingFallbackModel,omitempty` in `pkg/dispatch/envelope.go` — set it to the unmatched model ID when the post-normalizer price-table lookup misses; leave empty in the priced case.
- **Helper signature:** `setPricingFallbackIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha2.Project, fallbackModel string) error` in `internal/controller/pricing_fallback.go` — already hooked at every rollup site; 38-06 needs no controller changes.

## What was built

- **Task 1 (TDD):** wire field + `ConditionPricingFallbackActive` / `ReasonUnknownModelPriced` constants + `setPricingFallbackIfNeeded` helper (modeled on billing_halt.go: nil/empty-safe no-op, patch error returned for non-fatal logging, file-header lifecycle doc) + `tide_pricing_fallback_total` CounterVec registered in the metrics init() block. JSON contract locked by omitempty/round-trip tests.
- **Task 2:** hook at all 7 `budget.RollUpUsage(ctx` sites (milestone/phase/plan/project ×1, task ×3), placed inside the same exactly-once guards, non-fatal error handling mirroring `setBillingHaltIfNeeded`. Grep-count equality gate passes (7 == 7). Envtest specs (4, `PricingFallback` focus) lock: stamp with Reason/Message naming the model, empty-model and nil-project no-ops, and dedupe-vs-count semantics (condition once, counter reads +2 after two rollups via `testutil.ToFloat64`).

## Verification observed

- `go test ./pkg/dispatch/ -run TestUsagePricingFallback -v` — 2/2 PASS
- `go test ./internal/controller/ -short -ginkgo.focus='PricingFallback'` — `Ran 4 of 204 Specs … SUCCESS! 4 Passed | 0 Failed`
- Full unit tier for affected packages green: `internal/controller` (83s), `pkg/dispatch`, `internal/metrics`, `api/v1alpha2` all `ok`
- Controller diff is additive only: `git diff` on the five controllers shows 39 insertions, 0 deletions — no rollup guard, marker, or return path touched
- No Anthropic identifier in the pkg/dispatch or api/v1alpha2 additions (0 grep hits); the helper's single "anthropic" mention is the provider-firewall prose note (same as billing_halt.go)
- Helper contains no `check*` dispatch gate — informational only

## Deviations from Plan

### Auto-fixed Issues

None — plan executed as written.

### Deferred (pre-existing, out of scope)

**`go build ./...` fails at baseline on `cmd/tide-demo-init`** — `//go:embed all:fixture` has no matching files when the fixture scaffold isn't positioned, and it aborts `go list ./...` entirely. Reproduced identically on the main checkout at the same commit; logged in `deferred-items.md`. Verification used explicit package sets instead (`./pkg/... ./api/... ./internal/... ./cmd/tide/ ./cmd/dashboard/... ./cmd/credproxy/...`), all building clean.

**Environment note:** RESEARCH Pitfall 7 ("Go absent on host Mac") is stale — go1.26.4 and the 1.36.2 envtest assets are present on the host, so all tests ran locally without the dev VM.

## Threat mitigations applied

- T-38-04 (repudiation, GC'd stderr signal): this plan is the mitigation — durable condition + counter
- T-38-05 (metric cardinality): `model` label documented as operator-config-bounded in the Help/doc comment; metriccardinality analyzer only forbids `task`
- T-38-06 (message injection): model ID embedded via `%q` only

## Commits

- `bc8d4b3` test(38-02): add failing test for Usage.PricingFallbackModel wire contract
- `4c68463` feat(38-02): add provider-neutral pricing-fallback transport and surface
- `4d1c44c` feat(38-02): hook pricing-fallback surface at all seven budget-rollup sites

## Self-Check: PASSED

- pkg/dispatch/envelope_fallback_test.go, internal/controller/pricing_fallback.go, internal/controller/pricing_fallback_test.go — FOUND
- Commits bc8d4b3, 4c68463, 4d1c44c — FOUND on worktree-agent branch
