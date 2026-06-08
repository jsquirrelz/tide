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

// Package main tests for stub-subagent planner-mode behavior (REQ-3, Phase 7).
//
// These tests are intentionally RED at Wave 0: the stub's dispatchSuccess does
// not yet branch on Role=="planner". All planner-level tests will fail until
// Plan 07-03 (Wave 1) implements dispatchPlannerSuccess in main.go.
//
// Compile-check: these tests call run() (not dispatchPlannerSuccess directly),
// so the file compiles and passes `go build ./cmd/stub-subagent/...` even
// before the production code lands. The tests fail at runtime (RED) until
// Wave 1.
//
// Test names follow the established pattern in main_test.go (stdlib testing,
// no Ginkgo/Gomega). withWorkspaceRoot is re-used from main_test.go (same
// package) for workspace isolation.
package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"k8s.io/apimachinery/pkg/runtime"
)

// makePlannerEnvelope writes an EnvelopeIn JSON suitable for planner-mode
// tests to dir/in.json and returns the path. Role is always "planner"; Level
// and parentName are parameterised so each test can exercise a distinct level.
// Dev is nil — the planner branch must NOT require Dev.TestMode (it branches
// on Role, not on testMode).
func makePlannerEnvelope(t *testing.T, dir, level, parentName string) string {
	t.Helper()
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "planner-test-uid-" + level,
		Role:       "planner",
		Level:      level,
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "stub",
			Model:  "stub",
		},
		Dispatch: &pkgdispatch.DispatchMeta{ParentName: parentName},
		Caps: pkgdispatch.Caps{
			WallClockSeconds: 600,
			Iterations:       20,
		},
		// Dev is intentionally nil — planner dispatch branches on Role, not TestMode.
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal planner envelope: %v", err)
	}
	inPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inPath, data, 0o644); err != nil {
		t.Fatalf("write planner in.json: %v", err)
	}
	return inPath
}

// readPlannerOutEnvelope reads and unmarshals the out.json from dir.
func readPlannerOutEnvelope(t *testing.T, dir string) pkgdispatch.EnvelopeOut {
	t.Helper()
	outPath := filepath.Join(dir, "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read planner out.json: %v", err)
	}
	var out pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal planner out.json: %v", err)
	}
	return out
}

// assertSpecContainsKey unmarshals a runtime.RawExtension Spec into a
// map[string]interface{} and asserts that the given key is present. Only key
// presence is checked — not the value — so tests pass across child-name
// variants.
func assertSpecContainsKey(t *testing.T, spec runtime.RawExtension, key string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(spec.Raw, &m); err != nil {
		t.Fatalf("unmarshal ChildCRD Spec.Raw: %v", err)
	}
	if _, ok := m[key]; !ok {
		t.Errorf("ChildCRD Spec.Raw missing key %q; got keys: %v", key, mapKeys(m))
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestPlannerProject asserts that the stub planner at level="project" emits
// exactly one Milestone ChildCRD whose Spec.Raw contains "projectRef".
//
// RED until Plan 07-03 implements dispatchPlannerSuccess.
func TestPlannerProject(t *testing.T) {
	dir := t.TempDir()
	withWorkspaceRoot(t, dir)

	inPath := makePlannerEnvelope(t, dir, "project", "test-project")

	code := run(context.Background(), inPath, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("planner/project: want exit 0, got %d", code)
	}

	out := readPlannerOutEnvelope(t, dir)

	if len(out.ChildCRDs) != 1 {
		t.Fatalf("planner/project: ChildCRDs len = %d, want 1", len(out.ChildCRDs))
	}
	child := out.ChildCRDs[0]
	if child.Kind != "Milestone" {
		t.Errorf("planner/project: ChildCRD Kind = %q, want %q", child.Kind, "Milestone")
	}
	assertSpecContainsKey(t, child.Spec, "projectRef")
}

// TestPlannerMilestone asserts that the stub planner at level="milestone"
// emits exactly one Phase ChildCRD whose Spec.Raw contains "milestoneRef".
//
// RED until Plan 07-03 implements dispatchPlannerSuccess.
func TestPlannerMilestone(t *testing.T) {
	dir := t.TempDir()
	withWorkspaceRoot(t, dir)

	inPath := makePlannerEnvelope(t, dir, "milestone", "test-milestone")

	code := run(context.Background(), inPath, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("planner/milestone: want exit 0, got %d", code)
	}

	out := readPlannerOutEnvelope(t, dir)

	if len(out.ChildCRDs) != 1 {
		t.Fatalf("planner/milestone: ChildCRDs len = %d, want 1", len(out.ChildCRDs))
	}
	child := out.ChildCRDs[0]
	if child.Kind != "Phase" {
		t.Errorf("planner/milestone: ChildCRD Kind = %q, want %q", child.Kind, "Phase")
	}
	assertSpecContainsKey(t, child.Spec, "milestoneRef")
}

// TestPlannerPhase asserts that the stub planner at level="phase" emits
// exactly one Plan ChildCRD whose Spec.Raw contains "phaseRef".
//
// RED until Plan 07-03 implements dispatchPlannerSuccess.
func TestPlannerPhase(t *testing.T) {
	dir := t.TempDir()
	withWorkspaceRoot(t, dir)

	inPath := makePlannerEnvelope(t, dir, "phase", "test-phase")

	code := run(context.Background(), inPath, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("planner/phase: want exit 0, got %d", code)
	}

	out := readPlannerOutEnvelope(t, dir)

	if len(out.ChildCRDs) != 1 {
		t.Fatalf("planner/phase: ChildCRDs len = %d, want 1", len(out.ChildCRDs))
	}
	child := out.ChildCRDs[0]
	if child.Kind != "Plan" {
		t.Errorf("planner/phase: ChildCRD Kind = %q, want %q", child.Kind, "Plan")
	}
	assertSpecContainsKey(t, child.Spec, "phaseRef")
}

// TestPlannerPlan asserts that the stub planner at level="plan" emits exactly
// one Task ChildCRD whose Spec.Raw contains "planRef", "filesTouched",
// "declaredOutputPaths", and "testMode":"success".
//
// RED until Plan 07-03 implements dispatchPlannerSuccess.
func TestPlannerPlan(t *testing.T) {
	dir := t.TempDir()
	withWorkspaceRoot(t, dir)

	inPath := makePlannerEnvelope(t, dir, "plan", "test-plan")

	code := run(context.Background(), inPath, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("planner/plan: want exit 0, got %d", code)
	}

	out := readPlannerOutEnvelope(t, dir)

	if len(out.ChildCRDs) != 1 {
		t.Fatalf("planner/plan: ChildCRDs len = %d, want 1", len(out.ChildCRDs))
	}
	child := out.ChildCRDs[0]
	if child.Kind != "Task" {
		t.Errorf("planner/plan: ChildCRD Kind = %q, want %q", child.Kind, "Task")
	}
	assertSpecContainsKey(t, child.Spec, "planRef")
	assertSpecContainsKey(t, child.Spec, "filesTouched")
	assertSpecContainsKey(t, child.Spec, "declaredOutputPaths")

	// Assert testMode:"success" is present in the dev sub-object.
	var m map[string]any
	if err := json.Unmarshal(child.Spec.Raw, &m); err != nil {
		t.Fatalf("unmarshal Task Spec.Raw: %v", err)
	}
	dev, ok := m["dev"].(map[string]any)
	if !ok {
		t.Fatalf("planner/plan: Task Spec.Raw missing \"dev\" object; got keys: %v", mapKeys(m))
	}
	if tm, _ := dev["testMode"].(string); tm != "success" {
		t.Errorf("planner/plan: Task dev.testMode = %q, want %q", tm, "success")
	}
}

// TestPlannerTaskLeaf asserts that the stub planner at level="task" emits
// zero ChildCRDs (leaf executor path — tasks have no planner children).
//
// RED until Plan 07-03: the current dispatchSuccess always emits zero ChildCRDs
// regardless of Role, so this test actually passes today. It is kept here to
// act as a regression guard after Plan 07-03 lands the planner branch.
func TestPlannerTaskLeaf(t *testing.T) {
	dir := t.TempDir()
	withWorkspaceRoot(t, dir)

	inPath := makePlannerEnvelope(t, dir, "task", "test-plan")

	code := run(context.Background(), inPath, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("planner/task leaf: want exit 0, got %d", code)
	}

	out := readPlannerOutEnvelope(t, dir)

	if len(out.ChildCRDs) != 0 {
		t.Errorf("planner/task leaf: ChildCRDs len = %d, want 0 (leaf path)", len(out.ChildCRDs))
	}
}

// TestExecutorPathUnchanged asserts that the existing executor dispatch path
// (Role="" or Dev.TestMode="success") is unaffected by any planner branch
// added in Plan 07-03. This is a no-regression guard.
func TestExecutorPathUnchanged(t *testing.T) {
	dir := t.TempDir()
	withWorkspaceRoot(t, dir)

	// Use the existing makeEnvelope helper from main_test.go (same package).
	// Role defaults to "executor"; Dev.TestMode = "success".
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	inPath := makeEnvelope(t, dir, "success", []string{artifactDir})

	code := run(context.Background(), inPath, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("executor path: want exit 0, got %d", code)
	}

	out := readPlannerOutEnvelope(t, dir)
	if len(out.ChildCRDs) != 0 {
		t.Errorf("executor path: ChildCRDs len = %d, want 0", len(out.ChildCRDs))
	}
	if out.ExitCode != 0 {
		t.Errorf("executor path: ExitCode = %d, want 0", out.ExitCode)
	}
}
