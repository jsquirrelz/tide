---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
fixed_at: 2026-07-11T14:25:00Z
review_path: .planning/phases/38-small-independents-pricing-accuracy-promptfile-telemetry-nud/38-REVIEW.md
iteration: 1
findings_in_scope: 4
fixed: 4
skipped: 0
status: all_fixed
---

# Phase 38: Code Review Fix Report

**Fixed at:** 2026-07-11T14:25:00Z
**Source review:** .planning/phases/38-small-independents-pricing-accuracy-promptfile-telemetry-nud/38-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 4 (fix_scope: critical_warning — WR-01..WR-04; IN-01/IN-02 out of scope)
- Fixed: 4
- Skipped: 0

## Fixed Issues

### WR-01: Pricing-fallback condition dedupe has substring false-positives

**Files modified:** `internal/controller/pricing_fallback.go`
**Commit:** 173c75e1
**Applied fix:** Anchored the churn-dedupe match on `strconv.Quote(fallbackModel)` — the exact quoted form the `%q` Sprintf embeds in the condition Message — so a model ID that is a substring of an already-surfaced one (e.g. `claude-sonnet-6` vs `claude-sonnet-6-1`) no longer suppresses the status patch. Added the `strconv` import.

### WR-02: setPricingFallbackIfNeeded patches status.conditions without optimistic lock

**Files modified:** `internal/controller/pricing_fallback.go`
**Commit:** f823095f
**Applied fix:** Wrapped the condition stamp in `retry.RetryOnConflict(retry.DefaultRetry, ...)` with a fresh `Get` into `latest` and `client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})`, mirroring the marker-stamp idiom already used at every call site (and commits d864860/91a67c2/957fbc1). The dedupe check moved inside the retry closure against `latest`; the metric increment stays outside (counts rollups, not retries). A concurrent conditions write is now a retryable conflict instead of a silent whole-array clobber.

### WR-03: Cache-rate derivation for pricing overrides hardcodes 125/100

**Files modified:** `internal/subagent/anthropic/subagent.go`, `internal/subagent/anthropic/pricing_test.go`
**Commit:** 03ddace9
**Applied fix:** `New()`'s override-merge derive path now computes `override.InputCentsPerMTok * cacheWriteMultNum / cacheWriteMultDen` (the D-08 single-flip constants) instead of the hardcoded `* 125 / 100`. Extended `TestCacheWriteMultiplierConsistency` with an `override_derive_path` subtest asserting an override that omits cache fields derives cacheWrite from the constants — a future TTL flip now moves compiled and override-derived rows together, and drift goes red.

### WR-04: Telemetry banner claims "prometheus.enabled is false" when config says it is true

**Files modified:** `dashboard/web/src/components/TelemetryView.tsx`, `dashboard/web/src/components/__tests__/TelemetryView.test.tsx`
**Commit:** 74a1f145
**Applied fix:** Restricted the defensive `allPanelsUnavailable` fallback to the unknown-config case: `telemetryEnabled === false || (telemetryEnabled === null && allPanelsUnavailable)`. A confirmed `telemetryEnabled: true` with every panel unavailable (e.g. `prometheus.endpoint` left unset) now renders no view-level banner — the per-panel TelemetryUnavailableNotice owns that degradation. Added the missing `true + unavailable` test case and updated the Banner Contract comments in both files. `TelemetryDisabledBanner.tsx` copy unchanged (still correct for the two states that reach it).

## Verification

- `go build ./...` — OK (after `make demo-fixture`, a pre-existing `go:embed` generation prerequisite unrelated to these fixes)
- `go test -short ./internal/subagent/anthropic/ ./pkg/dispatch/` — ok
- `go test -short ./internal/controller/` (full package under envtest, KUBEBUILDER_ASSETS 1.36.2) — ok in 55.7s, including all four PricingFallback specs against the new WR-01/WR-02 implementation
- `dashboard/web`: `vitest run` on TelemetryView + TelemetryDisabledBanner suites — 36/36 passed (includes the new WR-04 case); `tsc -b` clean
- `make lint` — 0 issues, exit 0 (golangci-lint + DAG/dispatch import firewalls + tide-lint)

---

_Fixed: 2026-07-11T14:25:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
