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

package v1alpha1

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// planlog is the named logger for the Plan validating + conversion webhook.
//
// Phase 1: bodies are explicit no-ops (always Allow). Phase 2 fills validation
// logic inside the documented seams below per REQ-PLAN-01 / D-B3.
var planlog = logf.Log.WithName("plan-webhook")

// SetupPlanWebhookWithManager registers the validating webhook for Plan with
// the controller-runtime Manager. The Hub() conversion stub is registered via
// api/v1alpha1/plan_conversion.go — v1alpha1 IS the hub (CRD-05 / Pitfall 16
// future-proofing) so no ConvertTo/ConvertFrom is needed in Phase 1 because no
// v1beta1 spoke exists yet.
func SetupPlanWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &tideprojectv1alpha1.Plan{}).
		WithValidator(&PlanCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-tideproject-k8s-v1alpha1-plan,mutating=false,failurePolicy=fail,sideEffects=None,groups=tideproject.k8s,resources=plans,verbs=create;update,versions=v1alpha1,name=vplan-v1alpha1.kb.io,admissionReviewVersions=v1

// PlanCustomValidator validates Plan objects.
//
// Phase 1: no-op (always Allow) — endpoint exists and is registered with the
// Manager, but the validator returns nil, nil unconditionally.
//
// Phase 2: wires cycle detection via pkg/dag.ComputeWaves per D-B3 /
// REQ-PLAN-01. The Plan webhook is the chosen seam (not Task, not Wave)
// because the cyclic-DAG invariant is a Plan-level invariant: a Plan owns
// its Tasks, and the cycle is over Task→Task edges declared on the Plan.
type PlanCustomValidator struct{}

// ValidateCreate is invoked on every Plan POST. Phase 1 is a no-op that
// always returns (nil, nil) — i.e. Allow with no warnings.
//
// Phase 2 wires the cycle-detection seam (D-B3 / REQ-PLAN-01):
//
//	tasks, err := r.listTasksForPlan(ctx, plan)
//	if err != nil { return nil, fmt.Errorf("plan rejected: failed to list tasks: %w", err) }
//	nodeIDs, edges := tasksToDAG(tasks)
//	if _, err := dag.ComputeWaves(nodeIDs, edges); err != nil {
//	    var cyclic *dag.CycleError
//	    if errors.As(err, &cyclic) {
//	        return nil, fmt.Errorf("plan %s/%s rejected: cyclic task DAG involving %v", plan.Namespace, plan.Name, cyclic.InvolvedNodes)
//	    }
//	    return nil, err
//	}
func (v *PlanCustomValidator) ValidateCreate(_ context.Context, obj *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateCreate (no-op in Phase 1 — REQ-PLAN-01 cycle detection wires in Phase 2)", "name", obj.GetName())
	// Phase 2: invoke pkg/dag.ComputeWaves here; reject *CycleError with structured message naming Tasks involved.
	return nil, nil
}

// ValidateUpdate is invoked on every Plan PUT/PATCH. Phase 1 is a no-op.
//
// Phase 2 re-runs the same cycle detection as ValidateCreate — a Plan edit
// can introduce a cycle that didn't exist at create time (D-B3).
func (v *PlanCustomValidator) ValidateUpdate(_ context.Context, _ *tideprojectv1alpha1.Plan, newObj *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateUpdate (no-op in Phase 1 — REQ-PLAN-01 cycle detection wires in Phase 2)", "name", newObj.GetName())
	// Phase 2: invoke pkg/dag.ComputeWaves here on newObj; reject *CycleError.
	return nil, nil
}

// ValidateDelete is invoked on every Plan DELETE. Phase 1 is a no-op.
//
// Phase 2 may wire a guard against deleting Plans whose Waves are still
// dispatching, though the current spec lets owner-ref cascade handle that.
// The endpoint is registered as a no-op so the Phase 2 hook point exists.
func (v *PlanCustomValidator) ValidateDelete(_ context.Context, obj *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateDelete (no-op in Phase 1)", "name", obj.GetName())
	// Phase 2: optionally guard against deletion while Waves are dispatching.
	return nil, nil
}
