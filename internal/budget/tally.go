// Package budget — see doc.go for package overview.
package budget

import (
	"context"
	"fmt"

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
// WindowStart is set to time.Now() on the first call (zero value). Once set, it
// is preserved — the window resets on the next billing-period boundary, which
// Plan 10's ProjectReconciler handles separately.
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
