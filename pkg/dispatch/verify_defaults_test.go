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

package dispatch

import (
	"strings"
	"testing"
)

// TestParseVerifyLevelDefaults covers the fail-closed per-level verify
// config parser (D-01/D-04 chart transport schema), mirroring
// TestParsePricingOverrides' shape in pricing_test.go.
func TestParseVerifyLevelDefaults(t *testing.T) {
	t.Run("empty_string", func(t *testing.T) {
		got, err := ParseVerifyLevelDefaults("")
		if err != nil {
			t.Fatalf("empty string: want nil error, got %v", err)
		}
		if got == nil {
			t.Fatal("empty string: want non-nil map, got nil")
		}
		if len(got) != 0 {
			t.Errorf("empty string: want empty map, got len=%d", len(got))
		}
	})

	t.Run("empty_object", func(t *testing.T) {
		got, err := ParseVerifyLevelDefaults("{}")
		if err != nil {
			t.Fatalf("empty object: want nil error, got %v", err)
		}
		if got == nil {
			t.Fatal("empty object: want non-nil map, got nil")
		}
		if len(got) != 0 {
			t.Errorf("empty object: want empty map, got len=%d", len(got))
		}
	})

	t.Run("valid_full_config", func(t *testing.T) {
		s := `{"task":{"enabled":true,"maxIterations":3,"onExhaustion":"requireApproval"},"plan":{"enabled":false,"maxIterations":1,"onExhaustion":"requireApproval"},"phase":{"enabled":false},"milestone":{"enabled":true,"onExhaustion":"requireApproval"},"project":{"enabled":true,"onExhaustion":"requireApproval"}}`
		got, err := ParseVerifyLevelDefaults(s)
		if err != nil {
			t.Fatalf("valid config: want nil error, got %v", err)
		}
		if len(got) != 5 {
			t.Fatalf("valid config: want 5 entries, got %d", len(got))
		}

		task, ok := got["task"]
		if !ok {
			t.Fatal("valid config: expected task key in result")
		}
		if !task.Enabled || task.MaxIterations != 3 || task.OnExhaustion != "requireApproval" {
			t.Errorf("task = %+v; want {Enabled:true MaxIterations:3 OnExhaustion:requireApproval}", task)
		}

		plan, ok := got["plan"]
		if !ok {
			t.Fatal("valid config: expected plan key in result")
		}
		if plan.Enabled || plan.MaxIterations != 1 || plan.OnExhaustion != "requireApproval" {
			t.Errorf("plan = %+v; want {Enabled:false MaxIterations:1 OnExhaustion:requireApproval}", plan)
		}

		phase, ok := got["phase"]
		if !ok {
			t.Fatal("valid config: expected phase key in result")
		}
		if phase.Enabled || phase.MaxIterations != 0 || phase.OnExhaustion != "" {
			t.Errorf("phase = %+v; want {Enabled:false MaxIterations:0 OnExhaustion:\"\"}", phase)
		}

		milestone, ok := got["milestone"]
		if !ok {
			t.Fatal("valid config: expected milestone key in result")
		}
		if !milestone.Enabled || milestone.MaxIterations != 0 || milestone.OnExhaustion != "requireApproval" {
			t.Errorf("milestone = %+v; want {Enabled:true MaxIterations:0 OnExhaustion:requireApproval}", milestone)
		}

		project, ok := got["project"]
		if !ok {
			t.Fatal("valid config: expected project key in result")
		}
		if !project.Enabled || project.MaxIterations != 0 || project.OnExhaustion != "requireApproval" {
			t.Errorf("project = %+v; want {Enabled:true MaxIterations:0 OnExhaustion:requireApproval}", project)
		}
	})

	t.Run("malformed_json_rejected", func(t *testing.T) {
		_, err := ParseVerifyLevelDefaults(`{"task":invalid}`)
		if err == nil {
			t.Fatal("malformed JSON: want error, got nil")
		}
		if !strings.Contains(err.Error(), "invalid JSON") {
			t.Errorf("error should contain %q, got: %v", "invalid JSON", err)
		}
	})

	t.Run("unknown_level_key_rejected", func(t *testing.T) {
		_, err := ParseVerifyLevelDefaults(`{"milestne":{"enabled":true}}`)
		if err == nil {
			t.Fatal("unknown level key: want error, got nil")
		}
		if !strings.Contains(err.Error(), "milestne") {
			t.Errorf("error should name the offending key 'milestne', got: %v", err)
		}
	})

	t.Run("negative_max_iterations_rejected", func(t *testing.T) {
		_, err := ParseVerifyLevelDefaults(`{"task":{"enabled":true,"maxIterations":-1}}`)
		if err == nil {
			t.Fatal("negative maxIterations: want error, got nil")
		}
		if !strings.Contains(err.Error(), "task") {
			t.Errorf("error should name the offending level 'task', got: %v", err)
		}
		if !strings.Contains(err.Error(), "-1") {
			t.Errorf("error should name the offending value '-1', got: %v", err)
		}
	})

	t.Run("invalid_on_exhaustion_rejected", func(t *testing.T) {
		_, err := ParseVerifyLevelDefaults(`{"task":{"enabled":true,"onExhaustion":"bogus"}}`)
		if err == nil {
			t.Fatal("invalid onExhaustion: want error, got nil")
		}
		if !strings.Contains(err.Error(), "task") {
			t.Errorf("error should name the offending level 'task', got: %v", err)
		}
		if !strings.Contains(err.Error(), "bogus") {
			t.Errorf("error should name the offending value 'bogus', got: %v", err)
		}
	})

	t.Run("whitespace_around_empty_object", func(t *testing.T) {
		got, err := ParseVerifyLevelDefaults("  {}  ")
		if err != nil {
			t.Fatalf("whitespace-padded empty: want nil error, got %v", err)
		}
		if len(got) != 0 {
			t.Errorf("whitespace-padded empty: want empty map, got len=%d", len(got))
		}
	})
}
