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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// TestResolveFileTouchMode verifies the D-E3 mode precedence resolution:
//  1. Plan annotation tideproject.k8s/file-touch-mode=strict|warn
//  2. Plan resolved-cache annotation tideproject.k8s/file-touch-mode-resolved
//  3. project.Spec.PlanAdmission.FileTouchMode (if project non-nil)
//  4. clusterDefault (Helm value)
func TestResolveFileTouchMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		plan         *tidev1alpha1.Plan
		project      *tidev1alpha1.Project
		clusterDef   string
		wantMode     string
	}{
		{
			name: "annotation strict wins over project default warn",
			plan: &tidev1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"tideproject.k8s/file-touch-mode": "strict",
					},
				},
			},
			project: &tidev1alpha1.Project{
				Spec: tidev1alpha1.ProjectSpec{
					PlanAdmission: tidev1alpha1.PlanAdmissionConfig{FileTouchMode: "warn"},
				},
			},
			clusterDef: "warn",
			wantMode:   "strict",
		},
		{
			name: "project spec strict wins over cluster default warn when no annotation",
			plan: &tidev1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{},
			},
			project: &tidev1alpha1.Project{
				Spec: tidev1alpha1.ProjectSpec{
					PlanAdmission: tidev1alpha1.PlanAdmissionConfig{FileTouchMode: "strict"},
				},
			},
			clusterDef: "warn",
			wantMode:   "strict",
		},
		{
			name: "cluster default warn when no annotation and project has empty mode",
			plan: &tidev1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{},
			},
			project: &tidev1alpha1.Project{
				Spec: tidev1alpha1.ProjectSpec{
					PlanAdmission: tidev1alpha1.PlanAdmissionConfig{FileTouchMode: ""},
				},
			},
			clusterDef: "warn",
			wantMode:   "warn",
		},
		{
			name:       "nil plan and nil project return cluster default",
			plan:       nil,
			project:    nil,
			clusterDef: "warn",
			wantMode:   "warn",
		},
		{
			name: "bogus annotation value falls through to next precedence layer",
			plan: &tidev1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"tideproject.k8s/file-touch-mode": "bogus-value",
					},
				},
			},
			project: &tidev1alpha1.Project{
				Spec: tidev1alpha1.ProjectSpec{
					PlanAdmission: tidev1alpha1.PlanAdmissionConfig{FileTouchMode: "warn"},
				},
			},
			clusterDef: "warn",
			wantMode:   "warn", // falls through to project (which is "warn") then cluster
		},
		{
			name: "resolved-cache annotation wins when direct annotation absent",
			plan: &tidev1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"tideproject.k8s/file-touch-mode-resolved": "strict",
					},
				},
			},
			project:    nil,
			clusterDef: "warn",
			wantMode:   "strict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveFileTouchMode(tt.plan, tt.project, tt.clusterDef)
			if got != tt.wantMode {
				t.Errorf("ResolveFileTouchMode() = %q; want %q", got, tt.wantMode)
			}
		})
	}
}
