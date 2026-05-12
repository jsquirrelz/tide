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

// wavelog is the named logger for the Wave validating webhook.
//
// Phase 1: bodies are explicit no-ops (always Allow). Phase 2 fills the
// reject-client-applies logic per D-B1: only the WaveReconciler should
// produce Wave objects, and the webhook enforces that contract.
var wavelog = logf.Log.WithName("wave-webhook")

// SetupWaveWebhookWithManager registers the validating webhook for Wave with
// the controller-runtime Manager. Wave is a single-version Kind (v1alpha1
// only) — no conversion webhook is needed.
func SetupWaveWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &tideprojectv1alpha1.Wave{}).
		WithValidator(&WaveCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-tideproject-k8s-v1alpha1-wave,mutating=false,failurePolicy=fail,sideEffects=None,groups=tideproject.k8s,resources=waves,verbs=create;update,versions=v1alpha1,name=vwave-v1alpha1.kb.io,admissionReviewVersions=v1

// WaveCustomValidator validates Wave objects.
//
// Phase 1: no-op (always Allow) — endpoint exists and is registered with the
// Manager, but the validator returns nil, nil unconditionally.
//
// Phase 2: wires the reject-client-applies contract per D-B1. Only the
// WaveReconciler should ever create Wave objects (derived from the Plan's
// Tasks via pkg/dag.ComputeWaves); humans and other controllers are
// rejected. The webhook detects "client-applied" by the absence of the
// WaveReconciler-stamped owner-ref pointing at a Plan.
type WaveCustomValidator struct{}

// ValidateCreate is invoked on every Wave POST. Phase 1 is a no-op.
//
// Phase 2 wires the D-B1 rejection — only the WaveReconciler should be
// the sole producer of Wave objects:
//
//	if !hasReconcilerStampedOwnerRef(wave) {
//	    return nil, fmt.Errorf("Wave %s/%s rejected per D-B1: client-applied Waves not allowed; the WaveReconciler is the sole producer", wave.Namespace, wave.Name)
//	}
//
// In Phase 1 the validator always returns (nil, nil) so the test harness
// can exercise the registration plumbing without depending on the not-yet-
// existent WaveReconciler.
func (v *WaveCustomValidator) ValidateCreate(_ context.Context, obj *tideprojectv1alpha1.Wave) (admission.Warnings, error) {
	wavelog.V(1).Info("ValidateCreate (no-op in Phase 1 — D-B1 rejection wires in Phase 2)", "name", obj.GetName())
	// Phase 2: reject client-applied Waves lacking the WaveReconciler-stamped owner-ref.
	return nil, nil
}

// ValidateUpdate is invoked on every Wave PUT/PATCH. Phase 1 is a no-op.
//
// Phase 2 may wire additional invariants (e.g. WaveIndex cannot be mutated
// after create; PlanRef cannot be re-targeted). D-B1's rejection is
// primarily a Create-path concern; Update validation is a Phase 2 follow-on.
func (v *WaveCustomValidator) ValidateUpdate(_ context.Context, _ *tideprojectv1alpha1.Wave, newObj *tideprojectv1alpha1.Wave) (admission.Warnings, error) {
	wavelog.V(1).Info("ValidateUpdate (no-op in Phase 1)", "name", newObj.GetName())
	// Phase 2: optionally reject mutations to WaveIndex or PlanRef.
	return nil, nil
}

// ValidateDelete is invoked on every Wave DELETE. Phase 1 is a no-op.
//
// Phase 2 may wire a guard so only the WaveReconciler (via owner-ref
// cascade from Plan deletion) can delete a Wave. Mirror of D-B1 on the
// delete path.
func (v *WaveCustomValidator) ValidateDelete(_ context.Context, obj *tideprojectv1alpha1.Wave) (admission.Warnings, error) {
	wavelog.V(1).Info("ValidateDelete (no-op in Phase 1)", "name", obj.GetName())
	// Phase 2: optionally enforce D-B1 on the delete path.
	return nil, nil
}
