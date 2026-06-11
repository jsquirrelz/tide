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

// budget_blocked.go — BudgetBlocked condition helpers for BUDGET-02 (Phase 14).
//
// D-04: When the TaskReconciler's dispatch gate observes cap breach, it calls
// setBudgetBlockedIfNeeded to stamp BudgetBlocked=True on the Project. All
// five dispatch gates call checkBudgetBlocked before dispatching; if blocked
// they park with a 30s requeue.
//
// This is the fourth dispatch-entry hold (after CheckRejected, checkParentApproval,
// checkBillingHalt). BudgetBlocked and BillingHalt are NOT mutually exclusive —
// both may be true simultaneously. Add checkBudgetBlocked AFTER checkBillingHalt
// in every dispatch gate sequence.
//
// setBudgetBlockedIfNeeded is BIDIRECTIONAL: it stamps BudgetBlocked=True when
// IsCapExceeded returns true, and clears it back to False (Reason=BudgetCapCleared)
// when IsCapExceeded returns false and the condition is currently True. Without the
// clear path, an operator raising Spec.Budget.AbsoluteCapCents could never recover
// dispatch because the gate would park forever on the stale True condition with a
// 30s requeue. The bypass annotation does NOT clear the condition — it reflects cap
// reality; bypass is checked separately at each gate via budget.IsBypassed.
package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/budget"
)

// checkBudgetBlocked returns true if the Project has a BudgetBlocked=True condition,
// indicating all new dispatch should be parked until the cap is raised.
//
// Nil-safe: a nil project returns false (no block — the reconciler handles the
// missing-project case separately).
func checkBudgetBlocked(project *tideprojectv1alpha1.Project) bool {
	if project == nil {
		return false
	}
	for _, c := range project.Status.Conditions {
		if c.Type == tideprojectv1alpha1.ConditionBudgetBlocked &&
			c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// setBudgetBlockedIfNeeded stamps BudgetBlocked=True on project when the cap is
// exceeded, or clears it to False when the cap is no longer exceeded and the
// condition is currently True. Idempotent for the set path (exits early if already
// set). Nil project is a safe no-op.
//
// reservedCents is the current total in-flight reservation from ReservationStore.TotalReserved();
// it is included in the condition Message for operator visibility.
//
// Called by the TaskReconciler after each cap check. The patch error is returned
// so callers can log it non-fatally.
func setBudgetBlockedIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha1.Project, reservedCents int64) error {
	if project == nil {
		return nil
	}

	capExceeded := budget.IsCapExceeded(project)

	if capExceeded {
		// Idempotent check — avoid a spurious patch if already set.
		existing := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
		if existing != nil && existing.Status == metav1.ConditionTrue {
			return nil
		}
		patch := client.MergeFrom(project.DeepCopy())
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:   tideprojectv1alpha1.ConditionBudgetBlocked,
			Status: metav1.ConditionTrue,
			Reason: tideprojectv1alpha1.ReasonBudgetCapReached,
			Message: fmt.Sprintf(
				"Cost spent %d cents (+ %d reserved) exceeds cap %d cents; dispatch halted project-wide",
				project.Status.Budget.CostSpentCents,
				reservedCents,
				project.Spec.Budget.AbsoluteCapCents,
			),
			LastTransitionTime: metav1.Now(),
		})
		return c.Status().Patch(ctx, project, patch)
	}

	// Cap NOT exceeded — clear the condition if it is currently True so that an
	// operator raising the cap recovers dispatch on the next reconcile cycle.
	existing := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
	if existing == nil || existing.Status != metav1.ConditionTrue {
		return nil // condition absent or already False — no-op
	}
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBudgetBlocked,
		Status:             metav1.ConditionFalse,
		Reason:             tideprojectv1alpha1.ReasonBudgetCapCleared,
		Message:            "Budget cap is no longer exceeded; dispatch resumed.",
		LastTransitionTime: metav1.Now(),
	})
	return c.Status().Patch(ctx, project, patch)
}
