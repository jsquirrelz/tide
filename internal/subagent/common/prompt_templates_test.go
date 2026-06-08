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

// TestLoadPromptTemplate_HappyPath verifies that LoadPromptTemplate returns a
// non-nil *template.Template for each of the five shipped {level,role} combos
// and that the template renders against a populated EnvelopeIn without error
// and produces non-empty output.
//
// Table-driven across all five (level, role) combinations: project/milestone/
// phase/plan planners plus the task executor. Matches plan 03-05 Task 1 Test 6.
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
