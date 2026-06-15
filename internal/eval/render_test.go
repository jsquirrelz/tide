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

package eval

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/sebdah/goldie/v2"

	"github.com/jsquirrelz/tide/internal/subagent/common"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// baseEnvelope holds the deterministic fixture fields shared across all five
// template renders. Every field is a compile-time constant; Provider.Params is
// nil to avoid map-iteration ordering flap. TaskUID is fixed because it is
// interpolated in plan_planner.tmpl and task_executor.tmpl, making it
// load-bearing for golden determinism. Role and Level are deliberately NOT set
// here — they are filled per-template by envelopeFor so each template renders
// with the (role, level) production actually dispatches it under, rather than a
// single planner/plan body that production never sends.
var baseEnvelope = pkgdispatch.EnvelopeIn{
	APIVersion:          "tideproject.k8s/v1alpha1",
	Kind:                "TaskEnvelopeIn",
	TaskUID:             "eval-fixture-uid-000",
	Prompt:              "EVAL FIXTURE: do not submit",
	DeclaredOutputPaths: []string{"internal/eval/testdata/placeholder.go"},
	Provider: pkgdispatch.ProviderSpec{
		Vendor: "anthropic",
		Model:  "claude-sonnet-4-6",
		// Params intentionally nil: avoids map-iteration ordering nondeterminism.
	},
}

// envelopeFor returns a copy of baseEnvelope with Role and Level set to the
// production dispatch shape for the template under test. All five templates
// interpolate {{.Role}} and {{.Level}} (template lines 9-11), so each golden,
// ratchet, and count_tokens floor must measure the body production sends:
// planners as ("planner", <level>) and the executor as ("executor", "task")
// (the values task_controller.go assigns at dispatch).
func envelopeFor(role, level string) pkgdispatch.EnvelopeIn {
	e := baseEnvelope
	e.Role = role
	e.Level = level
	return e
}

// templateCases enumerates all five (role, level) → goldie-name pairs.
var templateCases = []struct {
	role    string
	level   string
	name    string // goldie fixture name and ratchet file stem
}{
	{"planner", "project", "project_planner"},
	{"planner", "milestone", "milestone_planner"},
	{"planner", "phase", "phase_planner"},
	{"planner", "plan", "plan_planner"},
	{"executor", "task", "task_executor"},
}

// TestGoldenRender_ProjectPlanner asserts that the project_planner template
// renders deterministically and matches the committed golden file.
func TestGoldenRender_ProjectPlanner(t *testing.T) {
	goldenAssert(t, "planner", "project", "project_planner")
}

// TestGoldenRender_MilestonePlanner asserts that the milestone_planner template
// renders deterministically and matches the committed golden file.
func TestGoldenRender_MilestonePlanner(t *testing.T) {
	goldenAssert(t, "planner", "milestone", "milestone_planner")
}

// TestGoldenRender_PhasePlanner asserts that the phase_planner template renders
// deterministically and matches the committed golden file.
func TestGoldenRender_PhasePlanner(t *testing.T) {
	goldenAssert(t, "planner", "phase", "phase_planner")
}

// TestGoldenRender_PlanPlanner asserts that the plan_planner template renders
// deterministically and matches the committed golden file. TaskUID is
// interpolated in this template (plan_planner.tmpl:29) — fixedEnvelope pins it.
func TestGoldenRender_PlanPlanner(t *testing.T) {
	goldenAssert(t, "planner", "plan", "plan_planner")
}

// TestGoldenRender_TaskExecutor asserts that the task_executor template renders
// deterministically and matches the committed golden file. TaskUID is
// interpolated in this template — fixedEnvelope pins it.
func TestGoldenRender_TaskExecutor(t *testing.T) {
	goldenAssert(t, "executor", "task", "task_executor")
}

// goldenAssert loads the (role, level) template, renders it with the matching
// per-template envelope, and calls goldie.Assert to compare against the
// committed golden file.
func goldenAssert(t *testing.T, role, level, name string) {
	t.Helper()
	tmpl, err := common.LoadPromptTemplate(role, level)
	if err != nil {
		t.Fatalf("load template (%s, %s): %v", role, level, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, envelopeFor(role, level)); err != nil {
		t.Fatalf("render template (%s, %s): %v", role, level, err)
	}
	g := goldie.New(t, goldie.WithFixtureDir("testdata/goldie"))
	g.Assert(t, name, buf.Bytes())
}

// TestByteRatchet_ProjectPlanner asserts that the project_planner rendered byte
// count does not exceed the committed ceiling in testdata/ratchets/.
func TestByteRatchet_ProjectPlanner(t *testing.T) {
	ratchetAssert(t, "planner", "project", "project_planner")
}

// TestByteRatchet_MilestonePlanner asserts that the milestone_planner rendered
// byte count does not exceed the committed ceiling.
func TestByteRatchet_MilestonePlanner(t *testing.T) {
	ratchetAssert(t, "planner", "milestone", "milestone_planner")
}

// TestByteRatchet_PhasePlanner asserts that the phase_planner rendered byte
// count does not exceed the committed ceiling.
func TestByteRatchet_PhasePlanner(t *testing.T) {
	ratchetAssert(t, "planner", "phase", "phase_planner")
}

// TestByteRatchet_PlanPlanner asserts that the plan_planner rendered byte count
// does not exceed the committed ceiling.
func TestByteRatchet_PlanPlanner(t *testing.T) {
	ratchetAssert(t, "planner", "plan", "plan_planner")
}

// TestByteRatchet_TaskExecutor asserts that the task_executor rendered byte
// count does not exceed the committed ceiling.
func TestByteRatchet_TaskExecutor(t *testing.T) {
	ratchetAssert(t, "executor", "task", "task_executor")
}

// ratchetAssert renders the (role, level) template with its matching envelope
// and compares len(rendered) against the integer in testdata/ratchets/<name>.txt.
// A missing or malformed ratchet file is a fatal error. This is a STRICT
// frozen-byte-count ratchet: any divergence — growth OR shrink — fails, forcing
// a deliberate ratchet update in the same commit as the template change. (A
// growth-only ratchet would let a later trim silently leave a loose ceiling that
// then permits re-growth back to the old size, defeating the "ratchet down after
// trimming" intent — WR-03.)
func ratchetAssert(t *testing.T, role, level, name string) {
	t.Helper()
	tmpl, err := common.LoadPromptTemplate(role, level)
	if err != nil {
		t.Fatalf("load template (%s, %s): %v", role, level, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, envelopeFor(role, level)); err != nil {
		t.Fatalf("render template (%s, %s): %v", role, level, err)
	}

	ratchetFile := "testdata/ratchets/" + name + ".txt"
	data, err := os.ReadFile(ratchetFile)
	if err != nil {
		t.Fatalf("missing ratchet file %s — create it with the rendered byte count to activate the ratchet: %v", ratchetFile, err)
	}
	frozen, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("ratchet file %s malformed (expected single integer): %v", ratchetFile, err)
	}
	actual := buf.Len()
	if actual != frozen {
		t.Errorf("template %s byte count changed: rendered %d bytes, frozen ratchet %d — this is a frozen byte-count baseline; update %s in the same deliberate commit if the template change is intentional",
			name, actual, frozen, ratchetFile)
	}
}
