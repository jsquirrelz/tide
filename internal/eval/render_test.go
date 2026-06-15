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

// fixedEnvelope is the deterministic fixture used for all golden renders and
// byte ratchet tests. Every field is a compile-time constant; Provider.Params
// is nil to avoid map-iteration ordering flap. TaskUID is fixed because it is
// interpolated in plan_planner.tmpl and task_executor.tmpl, making it
// load-bearing for golden determinism.
var fixedEnvelope = pkgdispatch.EnvelopeIn{
	APIVersion:          "tideproject.k8s/v1alpha1",
	Kind:                "TaskEnvelopeIn",
	TaskUID:             "eval-fixture-uid-000",
	Role:                "planner",
	Level:               "plan",
	Prompt:              "EVAL FIXTURE: do not submit",
	DeclaredOutputPaths: []string{"internal/eval/testdata/placeholder.go"},
	Provider: pkgdispatch.ProviderSpec{
		Vendor: "anthropic",
		Model:  "claude-sonnet-4-6",
		// Params intentionally nil: avoids map-iteration ordering nondeterminism.
	},
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

// goldenAssert loads the (role, level) template, renders it with fixedEnvelope,
// and calls goldie.Assert to compare against the committed golden file.
func goldenAssert(t *testing.T, role, level, name string) {
	t.Helper()
	tmpl, err := common.LoadPromptTemplate(role, level)
	if err != nil {
		t.Fatalf("load template (%s, %s): %v", role, level, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, fixedEnvelope); err != nil {
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

// ratchetAssert renders the (role, level) template with fixedEnvelope and
// compares len(rendered) against the integer in testdata/ratchets/<name>.txt.
// A missing or malformed ratchet file is a fatal error. Exceeding the ceiling
// is a non-fatal error that names the ratchet file and instructs the maintainer.
func ratchetAssert(t *testing.T, role, level, name string) {
	t.Helper()
	tmpl, err := common.LoadPromptTemplate(role, level)
	if err != nil {
		t.Fatalf("load template (%s, %s): %v", role, level, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, fixedEnvelope); err != nil {
		t.Fatalf("render template (%s, %s): %v", role, level, err)
	}

	ratchetFile := "testdata/ratchets/" + name + ".txt"
	data, err := os.ReadFile(ratchetFile)
	if err != nil {
		t.Fatalf("missing ratchet file %s — create it with the rendered byte count to activate the ratchet: %v", ratchetFile, err)
	}
	ceiling, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("ratchet file %s malformed (expected single integer): %v", ratchetFile, err)
	}
	actual := buf.Len()
	if actual > ceiling {
		t.Errorf("template %s grew: rendered %d bytes, ratchet ceiling %d — update %s if growth is intentional",
			name, actual, ceiling, ratchetFile)
	}
}
