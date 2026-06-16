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

// Package v1alpha2 contains v1alpha2 admission webhooks for the TIDE project.
package v1alpha2

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// wavelog is the named logger for the Wave validating webhook.
var wavelog = logf.Log.WithName("wave-webhook-v1alpha2") //nolint:logcheck // controller-runtime logf idiom

// SetupWaveWebhookWithManager registers the validating webhook for v1alpha2.Wave with
// the controller-runtime Manager. This is the D-B1 guard ported to v1alpha2:
// only the WaveReconciler may create Wave objects; client-applied Waves are rejected.
func SetupWaveWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &tideprojectv1alpha2.Wave{}).
		WithValidator(&WaveCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-tideproject-k8s-v1alpha2-wave,mutating=false,failurePolicy=fail,sideEffects=None,groups=tideproject.k8s,resources=waves,verbs=create;update,versions=v1alpha2,name=vwave-v1alpha2.kb.io,admissionReviewVersions=v1

// WaveCustomValidator validates v1alpha2.Wave objects.
//
// D-B1: only the WaveReconciler may create Wave CRs. The webhook enforces this
// by rejecting any Wave that arrives without a reconciler-stamped owner reference.
// Client-applied Waves (kubectl apply, ad-hoc creates) are rejected at admission —
// the WaveReconciler is the sole producer.
type WaveCustomValidator struct{}

// ValidateCreate rejects any Wave lacking an owner reference (D-B1: the
// WaveReconciler is the sole producer of Wave objects).
func (v *WaveCustomValidator) ValidateCreate(_ context.Context, obj *tideprojectv1alpha2.Wave) (admission.Warnings, error) {
	wavelog.V(1).Info("ValidateCreate (D-B1 rejection wired)", "name", obj.GetName())
	if len(obj.GetOwnerReferences()) == 0 {
		return nil, fmt.Errorf("wave %s/%s rejected per D-B1: client-applied Waves not allowed; the WaveReconciler is the sole producer", obj.Namespace, obj.Name)
	}
	return nil, nil
}

// ValidateUpdate allows updates without additional restrictions (D-B1 is a
// Create-path guard; mutations to WaveIndex or ProjectRef may be rejected in
// a future phase if needed).
func (v *WaveCustomValidator) ValidateUpdate(_ context.Context, _ *tideprojectv1alpha2.Wave, newObj *tideprojectv1alpha2.Wave) (admission.Warnings, error) {
	wavelog.V(1).Info("ValidateUpdate", "name", newObj.GetName())
	return nil, nil
}

// ValidateDelete is a no-op; owner-ref cascade from the parent Project handles
// Wave cleanup. A future phase may add a D-B1-style guard on the delete path.
func (v *WaveCustomValidator) ValidateDelete(_ context.Context, obj *tideprojectv1alpha2.Wave) (admission.Warnings, error) {
	wavelog.V(1).Info("ValidateDelete (no-op)", "name", obj.GetName())
	return nil, nil
}
