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

// Package v1alpha2_test carries structural unit tests for the v1alpha2 API type
// package. These tests validate the type-level shape of the Spring Tide schema
// reshape (SCHEMA-01, DEPS-01, DEPS-02) without spinning up an envtest harness.
// CEL admission-time behavior is exercised by Plan 03's envtest suite.
//
// Test name map (matches VALIDATION.md run-name map):
//   - TestWaveSpec   — SCHEMA-01: WaveSpec.ProjectRef replaces PlanRef; WaveIndex is global
//   - TestTaskDependsOn — DEPS-01: TaskSpec.DependsOn accepts cross-scope names
//   - TestPlanDependsOn — DEPS-02: PlanSpec.DependsOn field present and validates
package v1alpha2_test

import (
	"reflect"
	"testing"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// TestWaveSpec asserts SCHEMA-01: WaveSpec carries ProjectRef (not PlanRef) and
// WaveIndex. Uses reflect to confirm there is no field named PlanRef on WaveSpec —
// the old Plan-scoped ownership is gone at the type level.
func TestWaveSpec(t *testing.T) {
	// Construct a v1alpha2.Wave with ProjectRef + WaveIndex.
	w := tidev1alpha2.Wave{
		Spec: tidev1alpha2.WaveSpec{
			ProjectRef: "my-project",
			WaveIndex:  3,
		},
	}

	// Assert fields round-trip on the constructed value.
	if got := w.Spec.ProjectRef; got != "my-project" {
		t.Errorf("Spec.ProjectRef = %q, want %q", got, "my-project")
	}
	if got := w.Spec.WaveIndex; got != 3 {
		t.Errorf("Spec.WaveIndex = %d, want 3", got)
	}

	// Assert via reflect that there is NO field named "PlanRef" on WaveSpec.
	// This catches any regression that re-introduces the old per-plan ownership field.
	waveSpecType := reflect.TypeOf(tidev1alpha2.WaveSpec{})
	if _, ok := waveSpecType.FieldByName("PlanRef"); ok {
		t.Errorf("WaveSpec has field PlanRef — old Plan-scoped ownership must be removed (SCHEMA-01)")
	}

	// Assert ProjectRef IS present.
	if _, ok := waveSpecType.FieldByName("ProjectRef"); !ok {
		t.Errorf("WaveSpec missing field ProjectRef — SCHEMA-01 requires Project-scope re-ownership")
	}

	// Assert WaveIndex IS present.
	if _, ok := waveSpecType.FieldByName("WaveIndex"); !ok {
		t.Errorf("WaveSpec missing field WaveIndex")
	}
}

// TestTaskDependsOn asserts DEPS-01: TaskSpec.DependsOn accepts entries that name
// tasks in other plans (cross-scope) as well as coarse scope refs (naming a
// Milestone). D-F1 (plan-local restriction) must be gone at the Go-type level;
// there is no plan-local filtering — all entries are retained as authored.
func TestTaskDependsOn(t *testing.T) {
	// A Task whose DependsOn names a task in another plan (cross-scope, fully
	// qualified structural ID) and a Milestone scope node (coarse ref).
	crossScopeDep := "milestone-b-phase-3-plan-c-task-07"
	coarseRef := "milestone-a"

	task := tidev1alpha2.Task{
		Spec: tidev1alpha2.TaskSpec{
			PlanRef:             "my-plan",
			FilesTouched:        []string{"src/main.go"},
			PromptPath:          "envelopes/uid/children/task-01.json",
			DeclaredOutputPaths: []string{"src/main.go"},
			DependsOn:           []string{crossScopeDep, coarseRef},
		},
	}

	// Assert both entries are retained on the struct (no plan-local filtering).
	if n := len(task.Spec.DependsOn); n != 2 {
		t.Fatalf("DependsOn has %d entries, want 2", n)
	}
	if task.Spec.DependsOn[0] != crossScopeDep {
		t.Errorf("DependsOn[0] = %q, want %q", task.Spec.DependsOn[0], crossScopeDep)
	}
	if task.Spec.DependsOn[1] != coarseRef {
		t.Errorf("DependsOn[1] = %q, want %q", task.Spec.DependsOn[1], coarseRef)
	}

	// Assert via reflect that DependsOn IS present on TaskSpec.
	taskSpecType := reflect.TypeOf(tidev1alpha2.TaskSpec{})
	if _, ok := taskSpecType.FieldByName("DependsOn"); !ok {
		t.Errorf("TaskSpec missing field DependsOn — DEPS-01 requires the field")
	}

	// PlanRef must still be present for ownership (D-F1 retirement removes only
	// the plan-local restriction on DependsOn, not the ownership field).
	if _, ok := taskSpecType.FieldByName("PlanRef"); !ok {
		t.Errorf("TaskSpec missing field PlanRef — ownership field must be retained")
	}
}

// TestPlanDependsOn asserts DEPS-02: PlanSpec has a DependsOn field and it
// round-trips correctly. Also asserts via reflect that the field exists.
func TestPlanDependsOn(t *testing.T) {
	// Construct a Plan with coarse-level DependsOn entries — one naming another
	// Phase (phase-level dep) and one naming a specific Task (task-level dep).
	plan := tidev1alpha2.Plan{
		Spec: tidev1alpha2.PlanSpec{
			PhaseRef:  "my-phase",
			DependsOn: []string{"phase-2", "plan-x-task-01"},
		},
	}

	// Assert round-trip.
	if n := len(plan.Spec.DependsOn); n != 2 {
		t.Fatalf("PlanSpec.DependsOn has %d entries, want 2", n)
	}
	if plan.Spec.DependsOn[0] != "phase-2" {
		t.Errorf("DependsOn[0] = %q, want %q", plan.Spec.DependsOn[0], "phase-2")
	}
	if plan.Spec.DependsOn[1] != "plan-x-task-01" {
		t.Errorf("DependsOn[1] = %q, want %q", plan.Spec.DependsOn[1], "plan-x-task-01")
	}

	// Assert via reflect that DependsOn IS present on PlanSpec (DEPS-02).
	planSpecType := reflect.TypeOf(tidev1alpha2.PlanSpec{})
	if _, ok := planSpecType.FieldByName("DependsOn"); !ok {
		t.Errorf("PlanSpec missing field DependsOn — DEPS-02 requires the field")
	}

	// PhaseRef must still be present for ownership.
	if _, ok := planSpecType.FieldByName("PhaseRef"); !ok {
		t.Errorf("PlanSpec missing field PhaseRef — ownership field must be retained")
	}
}
