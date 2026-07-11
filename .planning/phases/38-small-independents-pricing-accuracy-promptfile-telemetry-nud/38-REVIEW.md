---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
reviewed: 2026-07-11T14:04:55Z
depth: standard
files_reviewed: 47
files_reviewed_list:
  - api/v1alpha2/shared_types.go
  - charts/tide/templates/dashboard-deployment.yaml
  - charts/tide/templates/NOTES.txt
  - charts/tide/values.yaml
  - cmd/dashboard/api/config_test.go
  - cmd/dashboard/api/config.go
  - cmd/dashboard/main_test.go
  - cmd/dashboard/main.go
  - cmd/dashboard/router.go
  - cmd/tide/apply.go
  - cmd/tide/cmd_test.go
  - dashboard/web/src/components/__tests__/TelemetryView.test.tsx
  - dashboard/web/src/components/TelemetryDisabledBanner.test.tsx
  - dashboard/web/src/components/TelemetryDisabledBanner.tsx
  - dashboard/web/src/components/TelemetryView.tsx
  - docs/INSTALL.md
  - hack/helm/assert-telemetry-render.sh
  - hack/helm/augment-tide-chart.sh
  - hack/helm/tide-values.yaml
  - internal/controller/boundary_push_test.go
  - internal/controller/budget_blocked_regression_test.go
  - internal/controller/child_rollup_idempotency_test.go
  - internal/controller/file_touch_gate_test.go
  - internal/controller/import_controller_test.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/plan_wavepause_test.go
  - internal/controller/pricing_fallback_test.go
  - internal/controller/pricing_fallback.go
  - internal/controller/project_baseref_halt_test.go
  - internal/controller/project_boundary_push_test.go
  - internal/controller/project_controller_test.go
  - internal/controller/project_controller.go
  - internal/controller/project_rollup_idempotency_test.go
  - internal/controller/suite_test.go
  - internal/controller/task_controller.go
  - internal/metrics/registry.go
  - internal/subagent/anthropic/pricing_test.go
  - internal/subagent/anthropic/pricing.go
  - internal/subagent/anthropic/run_mix_regression_test.go
  - internal/subagent/anthropic/subagent_test.go
  - internal/subagent/anthropic/subagent.go
  - internal/subagent/anthropic/testdata/first_run_2026-07-03_usage.json
  - pkg/dispatch/envelope_fallback_test.go
  - pkg/dispatch/envelope.go
findings:
  critical: 0
  warning: 4
  info: 2
  total: 6
status: issues_found
---

# Phase 38: Code Review Report

**Reviewed:** 2026-07-11T14:04:55Z
**Depth:** standard
**Files Reviewed:** 47
**Status:** issues_found

## Summary

Reviewed the Phase 38 diff (`7866329..HEAD`, +2094/−40 across 46 files) covering: the claude-sonnet-5 price row + date-suffix normalizer + cache-write multiplier constant (COST-01/D-01/D-08), the `PricingFallbackModel` envelope field + `PricingFallbackActive` condition + `tide_pricing_fallback_total` metric (COST-02/D-02), the `tide apply --prompt-file` flag (D-09/D-10/D-11), the `prometheus.enabled` umbrella key + NOTES.txt warning + dashboard `/api/v1/config` + telemetry banner (TELEM-02/03, D-12/D-14), and the `Label("heavy")` unit-tier split (DEBT-03).

Verification performed during review: `go vet` clean on all changed Go packages; `go test -short` green for `cmd/tide`, `cmd/dashboard/...`, `internal/subagent/anthropic`, `pkg/dispatch`; the NOTES.txt heredoc in `hack/helm/augment-tide-chart.sh` is byte-identical to `charts/tide/templates/NOTES.txt`; `hack/helm/tide-values.yaml` and `charts/tide/values.yaml` prometheus blocks are byte-identical; the ServiceMonitor carries the `control-plane: controller-manager` label INSTALL.md's step-3 label command relies on; `make test-heavy` runs without `-short` so the new `BeforeEach` heavy-skip in `suite_test.go` is consistent with the Makefile tiers.

No blockers. Four warnings: two correctness gaps in the new pricing-fallback condition helper (substring dedupe false-positive; non-optimistic conditions patch in a repo that just fixed three condition-clobber bugs), one single-source-of-truth violation on the D-08 cache-write multiplier, and one factually-wrong banner message in a reachable partial-config state. Two info items.

## Warnings

### WR-01: Pricing-fallback condition dedupe has substring false-positives

**File:** `internal/controller/pricing_fallback.go:72-75`
**Issue:** The churn-dedupe check is `strings.Contains(existing.Message, fallbackModel)`. Model IDs are prefixes of each other (`claude-sonnet-6` is a substring of `claude-sonnet-6-1`, and the `-YYYYMMDD` normalizer guarantees dated/undated variants coexist). If the condition already names `claude-sonnet-6-1-20270101` and a second unknown model `claude-sonnet-6-1` (or any substring of the message text) rolls up, `Contains` matches and the status patch is skipped — the new unknown model is never surfaced in the condition, defeating the operator-visibility purpose of COST-02. The metric still increments, but the condition (the Prometheus-less-install surface) stays stale.
**Fix:** Anchor the match on the quoted form the message actually embeds (the `%q` in the Sprintf):
```go
if existing != nil && existing.Status == metav1.ConditionTrue &&
	strings.Contains(existing.Message, strconv.Quote(fallbackModel)) {
	return nil
}
```

### WR-02: setPricingFallbackIfNeeded patches status.conditions without optimistic lock — condition-clobber class

**File:** `internal/controller/pricing_fallback.go:77-88`
**Issue:** The helper patches with plain `client.MergeFrom(project.DeepCopy())`. A JSON merge patch that touches `status.conditions` replaces the **entire conditions array** with the caller's snapshot + the new condition. The `project` pointer passed in at all five call sites was read earlier in the reconcile (from cache) and `budget.RollUpUsage` deliberately does NOT refresh it (it refetches internally into `latest`, copying back only `.Status.Budget` — `internal/budget/tally.go:60-82`). Any condition written concurrently between the caller's read and this patch — `BillingHalt`, `FailureHalt`, `BudgetBlocked`, `BoundaryPushed`, `ImportComplete` — is silently erased. Erasing a halt condition resumes dispatch that should be blocked. This is exactly the clobber class the last three commits on this branch fixed (`d864860` terminal-status clobber, `91a67c2` ImportComplete clobber, `957fbc1` BoundaryPushed supersede), and the marker-stamp code ~20 lines above each new call site already uses the hardened pattern. The pre-existing `billing_halt.go`/`budget_blocked.go`/`failure_halt.go` helpers share this exposure, but this phase adds five new writers at high-traffic completion points.
**Fix:** Mirror the WR-02 pattern already used in the same functions — refetch + `RetryOnConflict` + optimistic lock:
```go
return retry.RetryOnConflict(retry.DefaultRetry, func() error {
	latest := &tideprojectv1alpha2.Project{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(project), latest); err != nil {
		return err
	}
	existing := meta.FindStatusCondition(latest.Status.Conditions, tideprojectv1alpha2.ConditionPricingFallbackActive)
	if existing != nil && existing.Status == metav1.ConditionTrue &&
		strings.Contains(existing.Message, strconv.Quote(fallbackModel)) {
		return nil
	}
	patch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
	meta.SetStatusCondition(&latest.Status.Conditions, /* ... */)
	return c.Status().Patch(ctx, latest, patch)
})
```

### WR-03: Cache-rate derivation for pricing overrides hardcodes 125/100, bypassing the D-08 "single multiplier" constant

**File:** `internal/subagent/anthropic/subagent.go:154,160` (cross-referenced from `internal/subagent/anthropic/pricing.go:54-74`)
**Issue:** This phase introduces `cacheWriteMultNum`/`cacheWriteMultDen` documented as "the ONE place to flip on the next TTL shift (D-08)", locked by `TestCacheWriteMultiplierConsistency`. But `New()`'s override-merge path independently hardcodes the same ratios when deriving cache rates for overrides that omit them: `base.cacheWriteCentsPerMTok = override.InputCentsPerMTok * 125 / 100` (and `/ 10` for cacheRead). The consistency test iterates only the compiled `priceTable`, so a future flip to the 1h-TTL 2× rate would update every compiled row while operator-override-derived rows silently stay at 1.25× — precisely the drift D-08 was built to prevent, and no test goes red.
**Fix:** Use the constants in the derivation:
```go
base.cacheWriteCentsPerMTok = override.InputCentsPerMTok * cacheWriteMultNum / cacheWriteMultDen
```
and extend `TestCacheWriteMultiplierConsistency` (or add a case to the override tests) asserting a derive-path override's cacheWrite equals `input × cacheWriteMultNum / cacheWriteMultDen`.

### WR-04: Telemetry banner claims "prometheus.enabled is false" when config says it is true

**File:** `dashboard/web/src/components/TelemetryView.tsx:1246-1249` (copy at `dashboard/web/src/components/TelemetryDisabledBanner.tsx:33-38`)
**Issue:** The derivation is `if (telemetryEnabled === false || allPanelsUnavailable) bannerState = "disabled-by-config"`. When `telemetryEnabled === true` but every panel resolves the proxy's unavailable sentinel — reachable by setting `prometheus.enabled=true` without `prometheus.endpoint` (INSTALL.md requires all three values; forgetting one is the likely operator error) — the view renders the locked copy "prometheus.enabled is false — run telemetry beyond the budget tally is dark", a factually wrong statement that sends the operator to re-set a value they already set. The `allPanelsUnavailable` fallback was meant as a defensive signal for the config-fetch-failed (`null`) case (the test suite covers `null + unavailable`, never `true + unavailable`).
**Fix:** Restrict the defensive fallback to the unknown-config case:
```ts
if (telemetryEnabled === false || (telemetryEnabled === null && allPanelsUnavailable)) {
  bannerState = "disabled-by-config";
}
```
(or add a distinct "endpoint not configured" state for `true && allPanelsUnavailable`), and add the `true + unavailable` case to `TelemetryView.test.tsx`.

## Info

### IN-01: conservativeTier resolves from the compiled table, not the per-instance merged table

**File:** `internal/subagent/anthropic/pricing.go:158,184`
**Issue:** `conservativeTier` is a package-level var frozen from the compiled `priceTable` at init. `lookupPrice` otherwise resolves everything against `a.prices` (the New()-merged clone, per T-14-02), but on a miss it returns the package-level tier. If an operator's `pricing.overrides` raises `claude-fable-5`'s rates (or adds a costlier model), unknown-model fallback still bills at the stale compiled fable-5 rates — the documented "most-expensive known tier" guarantee drifts under overrides and can under-count relative to intent.
**Fix:** Resolve the fallback per-instance, e.g. return `a.prices["claude-fable-5"]` (picks up overrides to the fallback row) or compute the max-output-rate row of `a.prices` in `New()`.

### IN-02: D-09 override guard ignores the NestedString error for a non-string outcomePrompt

**File:** `cmd/tide/apply.go:153-157`
**Issue:** `existing, found, _ := unstructured.NestedString(prj.Object, "spec", "outcomePrompt")` — when the manifest sets `spec.outcomePrompt` to a non-string (a YAML block map or number), `NestedString` returns an error with `found=false`, so the D-09 "no silent override" guard does not fire and the malformed value is silently replaced by the prompt-file content instead of surfacing the conflict.
**Fix:** Capture the error and treat it as a conflict/parse failure:
```go
existing, found, err := unstructured.NestedString(prj.Object, "spec", "outcomePrompt")
if err != nil {
	return nil, fmt.Errorf("%s: spec.outcomePrompt is not a string: %w", path, err)
}
```

---

_Reviewed: 2026-07-11T14:04:55Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
