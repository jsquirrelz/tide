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

package controller

import (
	"testing"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// TestVerificationEnabledForLevel is a pure-function unit test in its own
// file — NOT relying on a -run/-ginkgo.focus filter against the shared
// TestControllers Ginkgo suite (Phase 51-03 lesson: such a filter vacuously
// passes 0 specs against internal/controller's sole envtest entry point).
//
// Covers D-04's precedence: authored Project-scope config > chart per-level
// enabled > off.
func TestVerificationEnabledForLevel(t *testing.T) {
	t.Run("authored Project-scope entry wins over chart disabled", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
			Verification: tideprojectv1alpha3.VerificationDefaults{
				Task: &tideprojectv1alpha3.VerificationSpec{GateCommand: "go test ./..."},
			},
		}}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"task": {Enabled: false},
		}}
		if got := verificationEnabledForLevel(project, "task", chart); !got {
			t.Errorf("verificationEnabledForLevel() = %v, want true (authored entry outranks chart default)", got)
		}
	})

	t.Run("nil authored, chart enabled true -> true", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"task": {Enabled: true},
		}}
		if got := verificationEnabledForLevel(project, "task", chart); !got {
			t.Errorf("verificationEnabledForLevel() = %v, want true (chart tier applies)", got)
		}
	})

	t.Run("nil authored, chart enabled false -> false", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"task": {Enabled: false},
		}}
		if got := verificationEnabledForLevel(project, "task", chart); got {
			t.Errorf("verificationEnabledForLevel() = %v, want false", got)
		}
	})

	t.Run("nil authored, level absent from chart map -> false", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"plan": {Enabled: true},
		}}
		if got := verificationEnabledForLevel(project, "task", chart); got {
			t.Errorf("verificationEnabledForLevel() = %v, want false (level absent from chart map)", got)
		}
	})

	t.Run("nil project, chart enabled true -> true (chart tier still applies)", func(t *testing.T) {
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"task": {Enabled: true},
		}}
		if got := verificationEnabledForLevel(nil, "task", chart); !got {
			t.Errorf("verificationEnabledForLevel() = %v, want true", got)
		}
	})

	t.Run("each of the 5 level strings routes to its own VerificationDefaults pointer field", func(t *testing.T) {
		cases := []struct {
			level string
			spec  *tideprojectv1alpha3.VerificationDefaults
		}{
			{"task", &tideprojectv1alpha3.VerificationDefaults{Task: &tideprojectv1alpha3.VerificationSpec{GateCommand: "x"}}},
			{"plan", &tideprojectv1alpha3.VerificationDefaults{Plan: &tideprojectv1alpha3.VerificationSpec{GateCommand: "x"}}},
			{"phase", &tideprojectv1alpha3.VerificationDefaults{Phase: &tideprojectv1alpha3.VerificationSpec{GateCommand: "x"}}},
			{"milestone", &tideprojectv1alpha3.VerificationDefaults{Milestone: &tideprojectv1alpha3.VerificationSpec{GateCommand: "x"}}},
			{"project", &tideprojectv1alpha3.VerificationDefaults{Project: &tideprojectv1alpha3.VerificationSpec{GateCommand: "x"}}},
		}
		chart := VerifyDefaults{} // no chart config at all -- proves the authored field alone drives it
		for _, tc := range cases {
			project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{Verification: *tc.spec}}
			if got := verificationEnabledForLevel(project, tc.level, chart); !got {
				t.Errorf("level %q: verificationEnabledForLevel() = %v, want true", tc.level, got)
			}
			// Every OTHER level on the same Project must stay false -- proves no
			// cross-level leakage through the wrong pointer field.
			for _, other := range []string{"task", "plan", "phase", "milestone", "project"} {
				if other == tc.level {
					continue
				}
				if got := verificationEnabledForLevel(project, other, chart); got {
					t.Errorf("level %q: authoring %q leaked enablement onto %q", tc.level, tc.level, other)
				}
			}
		}
	})
}
