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

package v1alpha2

import (
	"context"
	"fmt"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

var projectlog = logf.Log.WithName("project-webhook-v1alpha2") //nolint:logcheck // controller-runtime logf idiom

// SetupProjectWebhookWithManager registers the v1alpha2.Project validating webhook.
// Phase 04.1 P4.2: rejects PathPrefix values matching admin/billing surfaces
// regardless of operator configuration (defense in depth alongside the
// runtime credproxy enforcer).
func SetupProjectWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &tideprojectv1alpha2.Project{}).
		WithValidator(&ProjectCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-tideproject-k8s-v1alpha2-project,mutating=false,failurePolicy=fail,sideEffects=None,groups=tideproject.k8s,resources=projects,verbs=create;update,versions=v1alpha2,name=vproject-v1alpha2.kb.io,admissionReviewVersions=v1

// ProjectCustomValidator validates v1alpha2.Project objects.
//
// Phase 04.1 P4.2: enforces the credproxy-allowlist denylist — PathPrefix
// matching admin/billing surfaces is rejected at admission regardless of
// operator configuration. The hardcoded denylist is the primary gate; the
// runtime credproxy enforcer is the secondary fail-safe.
type ProjectCustomValidator struct{}

// ValidateCreate is invoked on every Project POST.
func (v *ProjectCustomValidator) ValidateCreate(_ context.Context, obj *tideprojectv1alpha2.Project) (admission.Warnings, error) {
	projectlog.V(1).Info("ValidateCreate (P4.2 denylist enforcement)", "name", obj.GetName())
	return v.validate(obj)
}

// ValidateUpdate is invoked on every Project PUT/PATCH.
func (v *ProjectCustomValidator) ValidateUpdate(_ context.Context, _ *tideprojectv1alpha2.Project, newObj *tideprojectv1alpha2.Project) (admission.Warnings, error) {
	projectlog.V(1).Info("ValidateUpdate (P4.2 denylist enforcement)", "name", newObj.GetName())
	return v.validate(newObj)
}

// ValidateDelete is a no-op — denylist enforcement is create/update-only.
func (v *ProjectCustomValidator) ValidateDelete(_ context.Context, _ *tideprojectv1alpha2.Project) (admission.Warnings, error) {
	return nil, nil
}

// validate enforces the credproxy-allowlist denylist (Phase 04.1 P4.2):
// PathPrefix matching admin/billing surfaces is rejected even if the operator
// tries to add them. The hardcoded denylist is intentionally narrow — only
// admin and billing paths.
func (v *ProjectCustomValidator) validate(project *tideprojectv1alpha2.Project) (admission.Warnings, error) {
	denied := []string{"/v1/admin", "/v1/billing"}

	for i, prov := range project.Spec.Providers {
		for j, route := range prov.AllowedRoutes {
			for _, badPrefix := range denied {
				if route.PathPrefix == badPrefix || strings.HasPrefix(route.PathPrefix, badPrefix+"/") {
					return nil, fmt.Errorf(
						"spec.providers[%d].allowedRoutes[%d]: PathPrefix %q is on the denylist (Phase 04.1 P4.2 defense-in-depth)",
						i, j, route.PathPrefix,
					)
				}
			}
		}
	}
	return nil, nil
}
