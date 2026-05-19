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

// Package gates — see doc.go for package overview.
package gates

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// phaseSucceeded is the canonical Status.Phase value the four reconcilers
// patch on terminal success. Kept as a private constant so the boundary
// check is a single point of comparison if the vocabulary ever shifts.
const phaseSucceeded = "Succeeded"

// BoundaryDetected returns true iff every child CRD of childKind under
// parent has Status.Phase=Succeeded — the shared seam consumed by BOTH the
// up-stack gate hooks (D-G2) and the W-2 mid-stack push triggers.
//
// childKind ∈ {"Milestone", "Phase", "Plan", "Task"}. Any other value
// returns (false, error) so a typo in a downstream caller surfaces loudly
// rather than silently passing through.
//
// Filtering: the function lists all children of childKind in parent's
// namespace, then filters by controller-style OwnerRef (metav1.IsControlledBy
// — matches the controllerutil.SetControllerReference convention used by
// MaterializeChildCRDs in internal/controller/dispatch_helpers.go). Label-
// based pre-filtering is intentionally NOT used: not every reconciler stamps
// the canonical tideproject.k8s/project label on every child (only
// Plan→Task stamps it explicitly per internal/controller/plan_controller.go),
// so owner refs are the universal seam.
//
// Childless contract: returns (false, nil) when the filtered child set is
// empty. "All children Succeeded" is vacuously true on an empty set, but a
// vacuous boundary is NOT a real boundary — at least one child must have
// existed for the level transition to be meaningful. Threat T-04-W2
// mitigation: this prevents an empty-projection loop in the caller.
//
// The function performs no writes; calling it twice on the same state
// returns the same value (idempotent, pure-over-state).
func BoundaryDetected(ctx context.Context, c client.Client, parent client.Object, childKind string) (bool, error) {
	if parent == nil {
		return false, fmt.Errorf("BoundaryDetected: parent is nil")
	}
	if c == nil {
		return false, fmt.Errorf("BoundaryDetected: client is nil")
	}

	switch childKind {
	case "Milestone":
		var list tideprojectv1alpha1.MilestoneList
		if err := c.List(ctx, &list, client.InNamespace(parent.GetNamespace())); err != nil {
			return false, fmt.Errorf("BoundaryDetected: list Milestones: %w", err)
		}
		matched := 0
		for i := range list.Items {
			child := &list.Items[i]
			if !metav1.IsControlledBy(child, parent) {
				continue
			}
			matched++
			if child.Status.Phase != phaseSucceeded {
				return false, nil
			}
		}
		return matched > 0, nil

	case "Phase":
		var list tideprojectv1alpha1.PhaseList
		if err := c.List(ctx, &list, client.InNamespace(parent.GetNamespace())); err != nil {
			return false, fmt.Errorf("BoundaryDetected: list Phases: %w", err)
		}
		matched := 0
		for i := range list.Items {
			child := &list.Items[i]
			if !metav1.IsControlledBy(child, parent) {
				continue
			}
			matched++
			if child.Status.Phase != phaseSucceeded {
				return false, nil
			}
		}
		return matched > 0, nil

	case "Plan":
		var list tideprojectv1alpha1.PlanList
		if err := c.List(ctx, &list, client.InNamespace(parent.GetNamespace())); err != nil {
			return false, fmt.Errorf("BoundaryDetected: list Plans: %w", err)
		}
		matched := 0
		for i := range list.Items {
			child := &list.Items[i]
			if !metav1.IsControlledBy(child, parent) {
				continue
			}
			matched++
			if child.Status.Phase != phaseSucceeded {
				return false, nil
			}
		}
		return matched > 0, nil

	case "Task":
		var list tideprojectv1alpha1.TaskList
		if err := c.List(ctx, &list, client.InNamespace(parent.GetNamespace())); err != nil {
			return false, fmt.Errorf("BoundaryDetected: list Tasks: %w", err)
		}
		matched := 0
		for i := range list.Items {
			child := &list.Items[i]
			if !metav1.IsControlledBy(child, parent) {
				continue
			}
			matched++
			if child.Status.Phase != phaseSucceeded {
				return false, nil
			}
		}
		return matched > 0, nil

	default:
		return false, fmt.Errorf("BoundaryDetected: unsupported childKind %q (allowed: Milestone, Phase, Plan, Task)", childKind)
	}
}
