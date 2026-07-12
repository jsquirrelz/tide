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

package v1alpha3

import (
	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// ResolveFileTouchMode returns the active file-touch validation mode per D-E3 precedence:
//  1. Plan annotation tideproject.k8s/file-touch-mode=strict|warn (direct override)
//  2. Plan annotation tideproject.k8s/file-touch-mode-resolved=strict|warn (resolved-cache
//     annotation that PlanReconciler may stamp after reading Project.Spec; Phase 2 trade-off
//     documented in RESEARCH.md Open Question #1: the webhook does NOT walk owner refs to
//     find the Project at admission time — 3 Gets per validate would add latency.)
//  3. project.Spec.PlanAdmission.FileTouchMode (if project non-nil)
//  4. clusterDefault (Helm value; "warn" if unset)
//
// Bogus annotation values (anything other than "strict" or "warn") are silently
// ignored and fall through to the next precedence layer.
//
// Returns "warn" if no value is set anywhere (defense-in-depth — Helm chart default).
func ResolveFileTouchMode(plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, clusterDefault string) string {
	if plan != nil {
		// Precedence 1: direct annotation on Plan.
		if v, ok := plan.Annotations["tideproject.k8s/file-touch-mode"]; ok {
			if v == "strict" || v == "warn" {
				return v
			}
			// Bogus value — fall through.
		}

		// Precedence 2: resolved-cache annotation stamped by PlanReconciler.
		// This allows the webhook to honor the Project.Spec value without doing
		// an additional Get call at admission time (Phase 2 trade-off).
		if v, ok := plan.Annotations["tideproject.k8s/file-touch-mode-resolved"]; ok {
			if v == "strict" || v == "warn" {
				return v
			}
			// Bogus cached value — fall through.
		}
	}

	// Precedence 3: Project.Spec.PlanAdmission.FileTouchMode.
	if project != nil && project.Spec.PlanAdmission.FileTouchMode != "" {
		return project.Spec.PlanAdmission.FileTouchMode
	}

	// Precedence 4: cluster-level Helm default.
	if clusterDefault != "" {
		return clusterDefault
	}

	// Final fallback — ensure we always return a safe non-empty value.
	return "warn"
}
