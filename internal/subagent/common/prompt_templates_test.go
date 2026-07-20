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

package common

import (
	"bytes"
	"strings"
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// childFixture is a test-only render-data fixture matching the bounded
// child-Task summary shape plan_verifier.tmpl ranges over via {{.Children}}
// (Phase 52 D-09's pinned render-data contract). Plans 52-07/52-08/52-09
// supply the real dispatch-time equivalent; this type exists only to
// exercise the template here.
type childFixture struct {
	Name        string
	DependsOn   []string
	Files       []string
	GateCommand string
}

// planVerifierFixture is a test-only render-data fixture for
// plan_verifier.tmpl — embeds EnvelopeIn (for .Verify/.TaskUID/...) plus the
// D-09-pinned .PlanGoal/.Children fields.
type planVerifierFixture struct {
	pkgdispatch.EnvelopeIn
	PlanGoal string
	Children []childFixture
}

// levelVerifierFixture is a test-only render-data fixture for
// phase/milestone/project_verifier.tmpl — embeds EnvelopeIn plus the
// D-09-pinned .LevelGoal field.
type levelVerifierFixture struct {
	pkgdispatch.EnvelopeIn
	LevelGoal string
}

// TestLoadPromptTemplate_HappyPath verifies that LoadPromptTemplate returns a
// non-nil *template.Template for each of the six original {level,role} combos
// and that the template renders against a populated EnvelopeIn without error
// and produces non-empty output.
//
// Table-driven across all six (level, role) combinations: project/milestone/
// phase/plan planners, the task executor, plus the task verifier (Phase 51
// EVAL-04). Matches plan 03-05 Task 1 Test 6. The four Phase 52 D-09
// level-verifier templates (plan/phase/milestone/project) render against a
// different data shape (planVerifierFixture/levelVerifierFixture) and are
// covered separately by TestLoadPromptTemplate_LevelVerifiers.
func TestLoadPromptTemplate_HappyPath(t *testing.T) {
	tests := []struct {
		role  string
		level string
	}{
		{role: "planner", level: "project"},
		{role: "planner", level: "milestone"},
		{role: "planner", level: "phase"},
		{role: "planner", level: "plan"},
		{role: "executor", level: "task"},
		{role: "verifier", level: "task"},
	}

	for _, tc := range tests {
		t.Run(tc.level+"_"+tc.role, func(t *testing.T) {
			tmpl, err := LoadPromptTemplate(tc.role, tc.level)
			if err != nil {
				t.Fatalf("LoadPromptTemplate(%q, %q): %v", tc.role, tc.level, err)
			}
			if tmpl == nil {
				t.Fatal("LoadPromptTemplate returned nil template")
			}

			in := pkgdispatch.EnvelopeIn{
				APIVersion: pkgdispatch.APIVersionV1Alpha1,
				Kind:       pkgdispatch.KindTaskEnvelopeIn,
				TaskUID:    "uid-fixture-1",
				Role:       tc.role,
				Level:      tc.level,
				Prompt:     "fixture prompt",
				Provider: pkgdispatch.ProviderSpec{
					Vendor: "anthropic",
					Model:  "claude-sonnet-4-6",
				},
				Verify: &pkgdispatch.VerifyContext{
					GateCommand: "make test",
				},
			}

			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, in); err != nil {
				t.Fatalf("template.Execute: %v", err)
			}
			if buf.Len() == 0 {
				t.Error("rendered template is empty")
			}
		})
	}
}

// TestLoadPromptTemplate_LevelVerifiers verifies the four Phase 52 D-09
// per-level verifier templates (plan/phase/milestone/project) load, render
// without error against their pinned render-data contract, and each carry
// their level-distinct marker plus the shared coverage-not-conservatism
// directive. plan_verifier.tmpl is goal-backward (four named dimensions);
// phase/milestone/project_verifier.tmpl are observable-outcome ("run branch"
// framing) per D-09.
func TestLoadPromptTemplate_LevelVerifiers(t *testing.T) {
	baseEnvelope := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-level-verifier-fixture",
		Role:       "verifier",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
		Verify: &pkgdispatch.VerifyContext{
			GateCommand:       "make test-int",
			RequiredArtifacts: []string{"artifacts/out.txt"},
		},
	}

	tests := []struct {
		level         string
		levelDistinct string
	}{
		{level: "plan", levelDistinct: "goal alignment"},
		{level: "phase", levelDistinct: "run branch"},
		{level: "milestone", levelDistinct: "run branch"},
		{level: "project", levelDistinct: "run branch"},
	}

	for _, tc := range tests {
		t.Run(tc.level, func(t *testing.T) {
			tmpl, err := LoadPromptTemplate("verifier", tc.level)
			if err != nil {
				t.Fatalf("LoadPromptTemplate(verifier, %q): %v", tc.level, err)
			}

			in := baseEnvelope
			in.Level = tc.level

			var buf bytes.Buffer
			var execErr error
			if tc.level == "plan" {
				execErr = tmpl.Execute(&buf, planVerifierFixture{
					EnvelopeIn: in,
					PlanGoal:   "Ship the per-level verifier templates behind the existing loader.",
					Children: []childFixture{
						{
							Name:        "task-01-plan-verifier-tmpl",
							DependsOn:   nil,
							Files:       []string{"internal/subagent/common/templates/plan_verifier.tmpl"},
							GateCommand: "go test ./internal/subagent/common/...",
						},
					},
				})
			} else {
				execErr = tmpl.Execute(&buf, levelVerifierFixture{
					EnvelopeIn: in,
					LevelGoal:  "Ship v1.0.9 Slack Tide's per-level LoopPolicy parameterization.",
				})
			}
			if execErr != nil {
				t.Fatalf("template.Execute: %v", execErr)
			}

			rendered := buf.String()
			if rendered == "" {
				t.Fatal("rendered template is empty")
			}

			lower := strings.ToLower(rendered)
			if !strings.Contains(lower, "report a finding for every deviation") {
				t.Errorf("%s_verifier.tmpl missing the coverage-not-conservatism directive; rendered:\n%s", tc.level, rendered)
			}
			if !strings.Contains(lower, tc.levelDistinct) {
				t.Errorf("%s_verifier.tmpl missing level-distinct marker %q; rendered:\n%s", tc.level, tc.levelDistinct, rendered)
			}
		})
	}
}

// TestLoadPromptTemplate_PlanVerifier_FourDimensions asserts plan_verifier.tmpl
// names all four ESC-01 goal-backward rubric dimensions explicitly.
func TestLoadPromptTemplate_PlanVerifier_FourDimensions(t *testing.T) {
	tmpl, err := LoadPromptTemplate("verifier", "plan")
	if err != nil {
		t.Fatalf("LoadPromptTemplate(verifier, plan): %v", err)
	}

	in := planVerifierFixture{
		EnvelopeIn: pkgdispatch.EnvelopeIn{
			APIVersion: pkgdispatch.APIVersionV1Alpha1,
			Kind:       pkgdispatch.KindTaskEnvelopeIn,
			TaskUID:    "uid-dimensions-fixture",
			Role:       "verifier",
			Level:      "plan",
			Provider: pkgdispatch.ProviderSpec{
				Vendor: "anthropic",
				Model:  "claude-sonnet-4-6",
			},
			Verify: &pkgdispatch.VerifyContext{GateCommand: "make test-int"},
		},
		PlanGoal: "fixture goal",
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		t.Fatalf("template.Execute: %v", err)
	}
	rendered := strings.ToLower(buf.String())

	for _, dimension := range []string{"goal alignment", "file-touch plausibility", "dependency correctness", "verification derivability"} {
		if !strings.Contains(rendered, dimension) {
			t.Errorf("plan_verifier.tmpl missing rubric dimension %q; rendered:\n%s", dimension, buf.String())
		}
	}
}

// TestLoadPromptTemplate_Verifier_CoverageNotConservatism asserts the
// EVAL-04 content directive: task_verifier.tmpl instructs the evaluator to
// report a finding for every deviation with severity + confidence tags
// (coverage), and never tells it to be conservative or to report only
// high-severity findings — the Opus-4.8 tuning note this template exists to
// satisfy (see 51-CONTEXT.md D-12).
func TestLoadPromptTemplate_Verifier_CoverageNotConservatism(t *testing.T) {
	tmpl, err := LoadPromptTemplate("verifier", "task")
	if err != nil {
		t.Fatalf("LoadPromptTemplate(verifier, task): %v", err)
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-verifier-fixture",
		Role:       "verifier",
		Level:      "task",
		Prompt:     "fixture prompt",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
		Verify: &pkgdispatch.VerifyContext{
			GateCommand:       "make test",
			RequiredArtifacts: []string{"artifacts/out.txt"},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		t.Fatalf("template.Execute: %v", err)
	}
	rendered := buf.String()

	for _, phrase := range []string{"every deviation", "severity", "confidence"} {
		if !strings.Contains(strings.ToLower(rendered), phrase) {
			t.Errorf("task_verifier.tmpl missing coverage directive phrase %q; rendered:\n%s", phrase, rendered)
		}
	}

	lower := strings.ToLower(rendered)
	for _, forbidden := range []string{"conservative", "only high-severity", "only high severity"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("task_verifier.tmpl contains forbidden conservatism directive %q (Opus-4.8 tuning note: drops real low-severity findings)", forbidden)
		}
	}
}

// TestPromptTemplateVersion_BumpedForVerifier asserts PromptTemplateVersion
// was co-bumped with the task_verifier.tmpl addition (the file's own
// MAINTENANCE RULE) — a stale version would silently corrupt cross-attempt
// run-evidence comparison (EXEC-03).
func TestPromptTemplateVersion_BumpedForVerifier(t *testing.T) {
	if PromptTemplateVersion == "v1" {
		t.Errorf("PromptTemplateVersion is still %q; must be bumped in the same commit as task_verifier.tmpl (MAINTENANCE RULE)", PromptTemplateVersion)
	}
}

// TestLoadPromptTemplate_RendersEnvelopeFields asserts that at least one
// shipped template references {{.Level}} or {{.TaskUID}} or {{.Provider.Model}},
// verifying the EnvelopeIn struct is the template-context shape callers must
// pass. (We don't assert which field appears — the .tmpl content is the
// executor's discretion per CONTEXT.md "Claude's Discretion: Prompt-template
// content"; we only assert SOMETHING from the envelope flows through.)
func TestLoadPromptTemplate_RendersEnvelopeFields(t *testing.T) {
	tmpl, err := LoadPromptTemplate("executor", "task")
	if err != nil {
		t.Fatalf("LoadPromptTemplate(executor,task): %v", err)
	}

	const uniqueTaskUID = "TASKUID-EXEC-FIXTURE-XYZ-987"
	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    uniqueTaskUID,
		Role:       "executor",
		Level:      "task",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		t.Fatalf("template.Execute: %v", err)
	}

	// The task_executor.tmpl is required to thread {{.TaskUID}} through (the
	// executor must know the per-Task envelope path /workspace/envelopes/{uid}/).
	if !strings.Contains(buf.String(), uniqueTaskUID) {
		t.Errorf("task_executor template did not thread TaskUID through; got:\n%s", buf.String())
	}
}

// TestPlanPlannerTemplate_FileTouchRule asserts that the plan-planner template
// contains the D-07 sibling-file-overlap rule (CUTS-07 prompt patch). A stable
// phrase from the rule is used as the substring target so that minor rewording
// does not break the assertion — only removing the rule entirely would cause failure.
func TestPlanPlannerTemplate_FileTouchRule(t *testing.T) {
	tmpl, err := LoadPromptTemplate("planner", "plan")
	if err != nil {
		t.Fatalf("LoadPromptTemplate(planner, plan): %v", err)
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-ft-rule-check",
		Role:       "planner",
		Level:      "plan",
		Prompt:     "fixture",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		t.Fatalf("template.Execute: %v", err)
	}

	// The stable phrase from the FILE-TOUCH RULE section added in D-07.
	const rulePhrase = "must not declare the same path"
	if !strings.Contains(buf.String(), rulePhrase) {
		t.Errorf("plan-planner template is missing the D-07 file-touch sibling rule; expected to find %q in rendered output:\n%s",
			rulePhrase, buf.String())
	}
}

// TestPlanPlannerTemplate_RepairFindingsAbsentUnchanged asserts that
// rendering plan_planner.tmpl with RepairFindings unset produces the exact
// same output as rendering with RepairFindings explicitly set to an empty
// slice — proving the D-04 findings block contributes zero bytes when there
// is nothing to report (the "renders identically to today" requirement).
// The authoritative byte-for-byte proof against the PRE-D-04 template is the
// frozen internal/eval golden file + byte ratchet for plan_planner (both
// left uncommitted-to-change by this plan): those compare against a
// committed artifact from before this template edit, which this in-package
// test cannot reproduce without duplicating that fixture.
func TestPlanPlannerTemplate_RepairFindingsAbsentUnchanged(t *testing.T) {
	tmpl, err := LoadPromptTemplate("planner", "plan")
	if err != nil {
		t.Fatalf("LoadPromptTemplate(planner, plan): %v", err)
	}

	base := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-repair-findings-absent",
		Role:       "planner",
		Level:      "plan",
		Prompt:     "fixture prompt",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
	}

	var unset, empty bytes.Buffer
	unsetIn := base
	if err := tmpl.Execute(&unset, unsetIn); err != nil {
		t.Fatalf("template.Execute (unset): %v", err)
	}

	emptyIn := base
	emptyIn.RepairFindings = []pkgdispatch.RepairFinding{}
	if err := tmpl.Execute(&empty, emptyIn); err != nil {
		t.Fatalf("template.Execute (empty slice): %v", err)
	}

	if unset.String() != empty.String() {
		t.Errorf("plan_planner.tmpl renders differently for nil vs. empty RepairFindings:\nnil:\n%s\nempty:\n%s", unset.String(), empty.String())
	}
	if strings.Contains(unset.String(), "prior plan-check attempt") {
		t.Errorf("plan_planner.tmpl rendered the D-04 findings block with no RepairFindings set:\n%s", unset.String())
	}
}

// TestPlanPlannerTemplate_RepairFindingsBlock asserts that a populated
// RepairFindings slice renders EVERY finding's severity/confidence/summary
// into the D-04 re-plan evidence block, addressed positively ("address
// EVERY finding"), not just the first one.
func TestPlanPlannerTemplate_RepairFindingsBlock(t *testing.T) {
	tmpl, err := LoadPromptTemplate("planner", "plan")
	if err != nil {
		t.Fatalf("LoadPromptTemplate(planner, plan): %v", err)
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-repair-findings-populated",
		Role:       "planner",
		Level:      "plan",
		Prompt:     "fixture prompt",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
		RepairFindings: []pkgdispatch.RepairFinding{
			{Severity: "blocker", Confidence: "high", Summary: "task-02 declares no verification contract"},
			{Severity: "advisory", Confidence: "medium", Summary: "task-01 and task-03 file-touch overlap is unexplained"},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		t.Fatalf("template.Execute: %v", err)
	}
	rendered := buf.String()

	if !strings.Contains(rendered, "Address EVERY finding") {
		t.Errorf("plan_planner.tmpl RepairFindings block missing positive-instruction framing; rendered:\n%s", rendered)
	}
	for _, want := range []string{
		"[blocker/high] task-02 declares no verification contract",
		"[advisory/medium] task-01 and task-03 file-touch overlap is unexplained",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("plan_planner.tmpl RepairFindings block missing finding %q; rendered:\n%s", want, rendered)
		}
	}
}

// TestLoadPromptTemplate_Unknown asserts LoadPromptTemplate returns an error
// for unknown (role, level) combinations. The file template.ParseFS attempts
// to load does not exist in the embed.FS, so this should surface fs.ErrNotExist
// (wrapped by template.ParseFS).
func TestLoadPromptTemplate_Unknown(t *testing.T) {
	cases := [][2]string{
		{"invalid", "invalid"},
		{"planner", "task"},   // valid role + valid level, invalid combo
		{"executor", "phase"}, // valid role + valid level, invalid combo
	}
	for _, c := range cases {
		t.Run(c[0]+"_"+c[1], func(t *testing.T) {
			tmpl, err := LoadPromptTemplate(c[0], c[1])
			if err == nil {
				t.Errorf("LoadPromptTemplate(%q,%q): expected error, got nil (tmpl=%v)", c[0], c[1], tmpl)
			}
		})
	}
}
