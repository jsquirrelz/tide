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

// Package v1alpha3_test carries structural unit tests for the v1alpha3 API type
// package. These tests validate the type-level shape of the Phase 40 CRANK-01
// copy-and-reshape (D-10 ModelSelection removal, D-02 artifact-first LevelOverrides
// semantics, SchemaRevision discriminator) without spinning up an envtest harness.
//
// Test name map:
//   - TestProjectSpecV1alpha3  — D-10: ProjectSpec has NO field ModelSelection
//   - TestLevelOverridesShape  — D-02: LevelOverrides retains fields
//     Milestone/Phase/Plan/Task (semantic rename, not a struct rename)
//   - TestSchemaRevisionField  — D-09: ProjectSpec.SchemaRevision exists with
//     json tag "schemaRevision"
package v1alpha3_test

import (
	"reflect"
	"testing"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// TestProjectSpecV1alpha3 asserts D-10: ProjectSpec no longer carries a
// ModelSelection field. Negative assertion catching a regression that
// reintroduces the dead field (zero readers outside api/ per RESEARCH.md).
func TestProjectSpecV1alpha3(t *testing.T) {
	projectSpecType := reflect.TypeFor[tidev1alpha3.ProjectSpec]()
	if _, ok := projectSpecType.FieldByName("ModelSelection"); ok {
		t.Errorf("ProjectSpec has field ModelSelection — D-10 requires it removed (dead field, duplicates Subagent.Levels)")
	}
}

// TestLevelOverridesShape asserts D-02: LevelOverrides retains the Go field
// names Milestone/Phase/Plan/Task and their JSON tags. D-02 is a semantic
// rename of what each key MEANS, not a struct/field rename — this test proves
// the shape survived the reshape.
func TestLevelOverridesShape(t *testing.T) {
	overrides := tidev1alpha3.LevelOverrides{
		Milestone: &tidev1alpha3.LevelConfig{Model: "claude-fable-5"},
		Phase:     &tidev1alpha3.LevelConfig{Model: "claude-opus-4-8"},
		Plan:      &tidev1alpha3.LevelConfig{Model: "claude-sonnet-5"},
		Task:      &tidev1alpha3.LevelConfig{Model: "claude-haiku-4-5"},
	}

	if overrides.Milestone.Model != "claude-fable-5" {
		t.Errorf("Milestone.Model = %q, want %q", overrides.Milestone.Model, "claude-fable-5")
	}

	levelOverridesType := reflect.TypeFor[tidev1alpha3.LevelOverrides]()

	// Assert json tags are unchanged from v1alpha2 (lowercase, singular).
	wantTags := map[string]string{
		"Milestone": "milestone,omitempty",
		"Phase":     "phase,omitempty",
		"Plan":      "plan,omitempty",
		"Task":      "task,omitempty",
	}
	for field, wantTag := range wantTags {
		f, ok := levelOverridesType.FieldByName(field)
		if !ok {
			t.Errorf("LevelOverrides missing field %s — D-02 is a semantic rename, not a struct rename", field)
			continue
		}
		if got := f.Tag.Get("json"); got != wantTag {
			t.Errorf("LevelOverrides.%s json tag = %q, want %q", field, got, wantTag)
		}
	}
}

// TestSchemaRevisionField asserts D-09: ProjectSpec.SchemaRevision exists
// with json tag "schemaRevision" and round-trips the v1alpha3 discriminator
// value. This is the fail-closed guard's discriminator field.
func TestSchemaRevisionField(t *testing.T) {
	project := tidev1alpha3.Project{
		Spec: tidev1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://github.com/owner/repo.git",
		},
	}

	if project.Spec.SchemaRevision != "v1alpha3" {
		t.Errorf("Spec.SchemaRevision = %q, want %q", project.Spec.SchemaRevision, "v1alpha3")
	}

	projectSpecType := reflect.TypeFor[tidev1alpha3.ProjectSpec]()
	f, ok := projectSpecType.FieldByName("SchemaRevision")
	if !ok {
		t.Fatalf("ProjectSpec missing field SchemaRevision — D-09 requires the discriminator field")
	}
	if got := f.Tag.Get("json"); got != "schemaRevision" {
		t.Errorf("SchemaRevision json tag = %q, want %q", got, "schemaRevision")
	}
}
