// Package budget — see doc.go for package overview.
package budget

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// RollUpUsage Patches Project.Status.Budget by adding usage's token counts and
// estimated cost to the running cumulative totals.
//
// Called once per Task completion by TaskReconciler (Plan 09) after reading the
// EnvelopeOut — one Status write per Task completion (D-D2). Churn is
// proportional to throughput, not to reconcile frequency.
//
// WindowStart is set to time.Now() on the first call (zero value). Once set
// it is preserved. The rolling-window reset path is implemented in
// MaybeResetWindow (Phase 04.1 P4.1) and called by ProjectReconciler at
// the start of each budget-gate evaluation.
//
// Returns nil on success. Wraps client.Status().Patch errors with context.
func RollUpUsage(ctx context.Context, c client.Client, project *tidev1alpha1.Project, usage pkgdispatch.Usage) error {
	// Capture the baseline for the MergeFrom patch.
	patch := client.MergeFrom(project.DeepCopy())

	// Accumulate token and cost tallies.
	project.Status.Budget.TokensSpent += usage.InputTokens + usage.OutputTokens
	project.Status.Budget.CostSpentCents += usage.EstimatedCostCents

	// Set WindowStart on first call; preserve once set.
	if project.Status.Budget.WindowStart == nil {
		now := metav1.Now()
		project.Status.Budget.WindowStart = &now
	}

	if err := c.Status().Patch(ctx, project, patch); err != nil {
		return fmt.Errorf("budget: tally roll-up: %w", err)
	}
	return nil
}

// MaybeResetWindow checks if the rolling window has elapsed since WindowStart
// and zeros the tally + advances WindowStart if so. Idempotent: returns
// false (no-op) when window hasn't elapsed, when RollingWindowCapCents is
// unconfigured (zero or negative), or when WindowStart is nil.
//
// Called by ProjectReconciler at the start of each budget-gate evaluation so
// the cap reflects current-window spend. Concurrent reconciles are safe
// because the Status patch uses MergeFrom — double-resets are idempotent.
//
// Window duration defaults to 24h when Spec.Budget.RollingWindowDuration is nil
// (Phase 04.1 P4.1 per locked user decision 1).
//
// Returns (true, nil) when the window was reset. Returns (false, nil) when the
// window has not elapsed or no rolling cap is configured. Returns (false, err)
// on Status patch failure.
//
// Phase 04.1 P4.1.
func MaybeResetWindow(ctx context.Context, c client.Client, project *tidev1alpha1.Project, now time.Time) (bool, error) {
	if project == nil {
		return false, nil
	}
	if project.Spec.Budget.RollingWindowCapCents <= 0 {
		return false, nil
	}
	if project.Status.Budget.WindowStart == nil {
		return false, nil
	}

	// Default window duration is 24h; use configured duration when set.
	windowDur := 24 * time.Hour
	if project.Spec.Budget.RollingWindowDuration != nil {
		windowDur = project.Spec.Budget.RollingWindowDuration.Duration
	}

	if now.Sub(project.Status.Budget.WindowStart.Time) < windowDur {
		return false, nil
	}

	// Window has elapsed — zero the tally and advance WindowStart.
	patch := client.MergeFrom(project.DeepCopy())
	project.Status.Budget.CostSpentCents = 0
	project.Status.Budget.TokensSpent = 0
	nowMeta := metav1.NewTime(now)
	project.Status.Budget.WindowStart = &nowMeta
	if err := c.Status().Patch(ctx, project, patch); err != nil {
		return false, fmt.Errorf("budget: window reset: %w", err)
	}
	return true, nil
}
