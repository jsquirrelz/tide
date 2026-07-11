/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// pricing_fallback.go — PricingFallbackActive condition helper for COST-02
// (Phase 38 D-02).
//
// When a subagent's model misses the effective price table even after
// normalization, the provider bills the dispatch at the conservative
// (most-expensive) tier and stamps the unmatched model ID on
// Usage.PricingFallbackModel. Every budget-rollup site (all five reconcilers)
// calls setPricingFallbackIfNeeded right after budget.RollUpUsage so the
// fallback surfaces as (a) a durable Project condition that survives pod GC
// and shows on Prometheus-less installs, and (b) a tide_pricing_fallback_total
// counter increment where telemetry is enabled. The subagent pod's stderr
// warning stays, but is no longer the only signal.
//
// Lifecycle: the condition is sticky for the run's lifetime — there is no
// clearer in v1.0.7. It is informational ONLY: unlike BillingHalt there is no
// check* dispatch gate reading it and nothing halts (RESEARCH anti-pattern
// list — observability only). The metric counts every fallback-priced rollup;
// the condition dedupes on repeat rollups of the same unknown model to avoid
// status churn.
//
// Provider-firewall note: this file classifies at the envelope level
// (Usage.PricingFallbackModel is provider-neutral) with no SDK import. The
// Anthropic-specific price-table lookup that SETS the flag lives in
// internal/subagent/anthropic/pricing.go; this helper is legal in package
// controller.
package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
)

// setPricingFallbackIfNeeded records an unknown-model pricing fallback on the
// Project: increments tide_pricing_fallback_total{project, model} and stamps
// the informational PricingFallbackActive=True condition naming the unmatched
// model. The patch error is returned so callers can log it non-fatally (the
// tokens were already billed; the surface is best-effort).
//
// Nil project and empty fallbackModel are safe no-ops (return nil). When the
// condition is already True with a Message naming the same model, the metric
// still increments (it counts dispatch rollups) but the status patch is
// skipped to avoid churn.
func setPricingFallbackIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha2.Project, fallbackModel string) error {
	if project == nil || fallbackModel == "" {
		return nil
	}
	tidemetrics.PricingFallbackTotal.WithLabelValues(project.Name, fallbackModel).Inc()
	// Re-fetch + RetryOnConflict + MergeFromWithOptimisticLock (mirrors the
	// marker-stamp pattern at every call site): a plain merge patch against
	// the caller's cache-stale snapshot replaces the ENTIRE conditions array,
	// silently erasing conditions written concurrently (BillingHalt,
	// FailureHalt, BudgetBlocked, BoundaryPushed, ImportComplete) — the
	// condition-clobber class fixed in d864860/91a67c2/957fbc1. The lock makes
	// a concurrent write a retryable conflict instead of a silent erase.
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &tideprojectv1alpha2.Project{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(project), latest); err != nil {
			return err
		}
		existing := meta.FindStatusCondition(latest.Status.Conditions, tideprojectv1alpha2.ConditionPricingFallbackActive)
		// Anchor the dedupe on the quoted form the Message embeds (%q below): a
		// bare Contains false-positives on model IDs that are substrings of an
		// already-surfaced one (claude-sonnet-6 vs claude-sonnet-6-1, or a dated
		// alias of either), silently dropping the newer unknown model.
		if existing != nil && existing.Status == metav1.ConditionTrue &&
			strings.Contains(existing.Message, strconv.Quote(fallbackModel)) {
			return nil // same unknown model already surfaced — no status churn
		}
		patch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
		meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:   tideprojectv1alpha2.ConditionPricingFallbackActive,
			Status: metav1.ConditionTrue,
			Reason: tideprojectv1alpha2.ReasonUnknownModelPriced,
			// %q keeps envelope-derived content inert — the model ID lands as a
			// quoted string only, never as formatting directives (T-38-06).
			Message: fmt.Sprintf("pricing: model %q missing from the price table; dispatches billed at the conservative (most-expensive) tier. "+
				"Fix the table or set pricing.overrides in values.yaml.", fallbackModel),
			LastTransitionTime: metav1.Now(),
		})
		return c.Status().Patch(ctx, latest, patch)
	})
}
