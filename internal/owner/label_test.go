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

// Package owner unit tests for StampProjectLabel (CUTS-01 / D-01).
package owner

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStampProjectLabel(t *testing.T) {
	tests := []struct {
		name        string
		initial     map[string]string
		projectName string
		wantLabels  map[string]string
		wantNoLabel bool // true => label must be absent after the call
	}{
		{
			name:        "nil labels map — creates map and sets label",
			initial:     nil,
			projectName: "my-project",
			wantLabels:  map[string]string{LabelProject: "my-project"},
		},
		{
			name:        "empty projectName is no-op — labels untouched",
			initial:     nil,
			projectName: "",
			wantNoLabel: true,
		},
		{
			name:        "overwrites pre-existing LLM-authored project label",
			initial:     map[string]string{LabelProject: "llm-wrote-this"},
			projectName: "authoritative-parent",
			wantLabels:  map[string]string{LabelProject: "authoritative-parent"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj",
					Namespace: "ns",
					Labels:    tc.initial,
				},
			}
			StampProjectLabel(obj, tc.projectName)

			if tc.wantNoLabel {
				if v := obj.GetLabels()[LabelProject]; v != "" {
					t.Errorf("expected no %s label; got %q", LabelProject, v)
				}
				return
			}
			for k, want := range tc.wantLabels {
				if got := obj.GetLabels()[k]; got != want {
					t.Errorf("label[%q] = %q; want %q", k, got, want)
				}
			}
		})
	}
}
