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

// Package budget — see doc.go for package overview.
package budget

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// RollUpUsage Patches Project.Status.Budget by adding usage's token counts and
// estimated cost to the running cumulative totals.
//
// Called once per Task completion by TaskReconciler (Plan 09) after reading the
// EnvelopeOut — one Status write per Task completion (D-D2). Churn is
// proportional to throughput, not to reconcile frequency.
//
// Concurrency: with MaxConcurrentReconciles > 1 two completions can roll up
// near-simultaneously. The increment re-fetches the Project and patches with
// an optimistic lock, retrying on conflict, so concurrent roll-ups serialize
// instead of last-write-wins clobbering the tally — an under-counted
// CostSpentCents directly defeats IsCapExceeded and HasHeadroom.
//
// WindowStart is set to time.Now() on the first call (zero value). Once set
// it is preserved. The rolling-window reset path is implemented in
// MaybeResetWindow (Phase 04.1 P4.1) and called by ProjectReconciler at
// the start of each budget-gate evaluation.
//
// On success the caller's project.Status.Budget is updated to the rolled-up
// state — TaskReconciler feeds the same struct to setBudgetBlockedIfNeeded
// immediately after roll-up.
//
// Returns nil on success. Wraps Get/Status().Patch errors with context.
func RollUpUsage(ctx context.Context, c client.Client, project *tidev1alpha2.Project, usage pkgdispatch.Usage) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch so the increment applies to the latest tally, not a stale
		// read captured before a concurrent completion's patch landed.
		latest := &tidev1alpha2.Project{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(project), latest); err != nil {
			return err
		}
		// The patch carries latest's resourceVersion: a concurrent roll-up
		// that landed in between returns a Conflict and this closure re-runs
		// against the newer tally.
		patch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})

		// Accumulate token and cost tallies.
		latest.Status.Budget.TokensSpent += usage.InputTokens + usage.OutputTokens
		latest.Status.Budget.CostSpentCents += usage.EstimatedCostCents

		// Set WindowStart on first call; preserve once set.
		if latest.Status.Budget.WindowStart == nil {
			now := metav1.Now()
			latest.Status.Budget.WindowStart = &now
		}

		if err := c.Status().Patch(ctx, latest, patch); err != nil {
			return err
		}
		project.Status.Budget = latest.Status.Budget
		return nil
	})
	if err != nil {
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
func MaybeResetWindow(ctx context.Context, c client.Client, project *tidev1alpha2.Project, now time.Time) (bool, error) {
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
