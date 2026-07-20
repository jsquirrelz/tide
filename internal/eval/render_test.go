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
	APIVersion:          pkgdispatch.APIVersionV1Alpha1,
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
	role  string
	level string
	name  string // goldie fixture name and ratchet file stem
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
// interpolated in this template (plan_planner.tmpl:29) — the fixture pins it.
func TestGoldenRender_PlanPlanner(t *testing.T) {
	goldenAssert(t, "planner", "plan", "plan_planner")
}

// TestGoldenRender_TaskExecutor asserts that the task_executor template renders
// deterministically and matches the committed golden file. TaskUID is
// interpolated in this template — the fixture pins it.
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
// count matches the committed frozen byte count in testdata/ratchets/.
func TestByteRatchet_ProjectPlanner(t *testing.T) {
	ratchetAssert(t, "planner", "project", "project_planner")
}

// TestByteRatchet_MilestonePlanner asserts that the milestone_planner rendered
// byte count matches the committed frozen byte count.
func TestByteRatchet_MilestonePlanner(t *testing.T) {
	ratchetAssert(t, "planner", "milestone", "milestone_planner")
}

// TestByteRatchet_PhasePlanner asserts that the phase_planner rendered byte
// count matches the committed frozen byte count.
func TestByteRatchet_PhasePlanner(t *testing.T) {
	ratchetAssert(t, "planner", "phase", "phase_planner")
}

// TestByteRatchet_PlanPlanner asserts that the plan_planner rendered byte count
// matches the committed frozen byte count.
func TestByteRatchet_PlanPlanner(t *testing.T) {
	ratchetAssert(t, "planner", "plan", "plan_planner")
}

// TestByteRatchet_TaskExecutor asserts that the task_executor rendered byte
// count matches the committed frozen byte count.
func TestByteRatchet_TaskExecutor(t *testing.T) {
	ratchetAssert(t, "executor", "task", "task_executor")
}

// TestNoMapInterpolation guards PROMPT-05 against future regression. PROMPT-05
// ("structured data serialized deterministically") is a confirmed no-op for
// Phase 19: the five templates interpolate only scalar string fields (.Level,
// .Role, .TaskUID, .Prompt, .Provider.Vendor, .Provider.Model). The only
// map-typed field on EnvelopeIn is ProviderSpec.Params (map[string]string), and
// no template references it. This test asserts that invariant remains true —
// if a future edit introduces {{range .Provider.Params}} or any other map-range
// iteration, the test fails CI, signaling that stable-key-order serialization
// must be added (per PROMPT-05 scope). The guard is deterministic and
// zero-network.
func TestNoMapInterpolation(t *testing.T) {
	// templateStem derives the .tmpl filename stem from (level, role), mirroring
	// the naming convention in LoadPromptTemplate: <level>_<role>.tmpl.
	for _, tc := range templateCases {
		t.Run(tc.name, func(t *testing.T) {
			path := "../subagent/common/templates/" + tc.name + ".tmpl"
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read template source %s: %v", path, err)
			}
			text := string(src)
			// Assert no .Params reference — ProviderSpec.Params is the only
			// map[string]string field on EnvelopeIn; its use in a template would
			// introduce map-iteration nondeterminism in the stable prefix.
			if strings.Contains(text, ".Params") {
				t.Errorf("template %s references .Params (map[string]string field); "+
					"map-typed interpolation in the stable prefix introduces key-order "+
					"nondeterminism — add stable-key-order serialization per PROMPT-05 "+
					"before using .Params in a template", tc.name)
			}
			// Assert no {{range}} action over anything but the one known-safe
			// slice field — a range over a map produces nondeterministic key
			// ordering in Go text/template output. {{range .RepairFindings}}
			// (plan_planner.tmpl, Phase 52 D-04) ranges []RepairFinding, a
			// slice, not a map, so it is explicitly excluded from this guard.
			withoutSafeRanges := strings.ReplaceAll(text, "{{range .RepairFindings}}", "")
			if strings.Contains(withoutSafeRanges, "{{range") || strings.Contains(withoutSafeRanges, "{{ range") {
				t.Errorf("template %s contains a {{range}} action; if this iterates a "+
					"map, key-order nondeterminism contaminates the stable prefix — "+
					"verify the range target is a slice (safe) or add stable-key-order "+
					"serialization per PROMPT-05 if it iterates a map", tc.name)
			}
		})
	}
}

// sharedContextFixture is a deterministic wave-scoped SharedContext blob
// representative of D-04 content: parent goal + load-bearing constraints +
// one-line sibling-set overview. Fixed to ensure golden determinism.
// This blob exercises CACHE-03's ordering assertion: it must appear BEFORE
// TaskUID: in every planner template's rendered output.
const sharedContextFixture = `Wave context (curated by parent planner):
Goal: Ship the v1.0.2 SharedContext injection so stable-prefix bytes grow toward the provider cache floor.
Constraints: CLI-based dispatch only (claude -p --bare); no direct-SDK cache_control; ratchets must not regress.
This wave: 5 plans — [20-01 envelope fields, 20-02 template injection, 20-03 controller stamp, 20-04 spike harness, 20-05 eval gate].`

// goldenAssertWithSharedContext loads the (role, level) template, renders it
// with a non-empty SharedContext set on the envelope, and calls goldie.Assert
// to compare against the committed golden file. The test also asserts that the
// SharedContext blob appears BEFORE the "TaskUID:" line in the rendered output,
// proving CACHE-03's stable-prefix ordering (blob is in the cacheable prefix,
// not after the volatile suffix).
func goldenAssertWithSharedContext(t *testing.T, role, level, name string) {
	t.Helper()
	tmpl, err := common.LoadPromptTemplate(role, level)
	if err != nil {
		t.Fatalf("load template (%s, %s): %v", role, level, err)
	}
	e := envelopeFor(role, level)
	e.SharedContext = sharedContextFixture
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, e); err != nil {
		t.Fatalf("render template (%s, %s): %v", role, level, err)
	}
	rendered := buf.String()

	// CACHE-03 ordering assertion: SharedContext blob must appear before
	// "TaskUID:" in the rendered output (stable prefix precedes volatile suffix).
	scIdx := strings.Index(rendered, sharedContextFixture)
	uidIdx := strings.Index(rendered, "TaskUID:")
	if scIdx < 0 {
		t.Errorf("template %s: SharedContext blob not found in rendered output", name)
	}
	if uidIdx < 0 {
		t.Errorf("template %s: 'TaskUID:' not found in rendered output", name)
	}
	if scIdx >= 0 && uidIdx >= 0 && scIdx >= uidIdx {
		t.Errorf("template %s: SharedContext blob (offset %d) does not precede TaskUID: (offset %d) — "+
			"CACHE-03 ordering violated: blob must be in the stable prefix, before the volatile suffix",
			name, scIdx, uidIdx)
	}

	g := goldie.New(t, goldie.WithFixtureDir("testdata/goldie"))
	g.Assert(t, name, buf.Bytes())
}

// TestGoldenRender_MilestonePlannerWithSharedContext asserts that the
// milestone_planner template renders the non-empty SharedContext blob in the
// stable prefix (before TaskUID:), proving CACHE-03 ordering.
// Golden file: testdata/goldie/milestone_planner_with_shared_context.golden
// This golden + ratchet are deliberately re-baselined in Plan 20-02; the
// empty-fixture golden/ratchet (milestone=1862) is unchanged.
func TestGoldenRender_MilestonePlannerWithSharedContext(t *testing.T) {
	goldenAssertWithSharedContext(t, "planner", "milestone", "milestone_planner_with_shared_context")
}

// TestGoldenRender_ProjectPlannerWithSharedContext asserts CACHE-03 ordering
// for the project_planner template with a non-empty SharedContext.
func TestGoldenRender_ProjectPlannerWithSharedContext(t *testing.T) {
	goldenAssertWithSharedContext(t, "planner", "project", "project_planner_with_shared_context")
}

// TestGoldenRender_PhasePlannerWithSharedContext asserts CACHE-03 ordering
// for the phase_planner template with a non-empty SharedContext.
func TestGoldenRender_PhasePlannerWithSharedContext(t *testing.T) {
	goldenAssertWithSharedContext(t, "planner", "phase", "phase_planner_with_shared_context")
}

// TestGoldenRender_PlanPlannerWithSharedContext asserts CACHE-03 ordering
// for the plan_planner template with a non-empty SharedContext.
func TestGoldenRender_PlanPlannerWithSharedContext(t *testing.T) {
	goldenAssertWithSharedContext(t, "planner", "plan", "plan_planner_with_shared_context")
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
