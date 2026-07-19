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
	"embed"
	"fmt"
	"text/template"
)

// CLAUDE.md anti-pattern, verbatim: "Don't vendor get-shit-done Markdown.
// Re-implement planner/executor prompts as compiled-in Go templates."
// The templates below are embedded into the binary at build time via go:embed
// so the running pod has zero runtime filesystem dependency on prompt content
// — there is no `kubectl apply -f prompts.yaml` story, no ConfigMap mount, no
// host bind-mount. The bytes ship inside the subagent image.

//go:embed templates/*.tmpl
var templateFS embed.FS

// PromptTemplateVersion is the compiled-in prompt-template version stamped
// onto [pkgdispatch.RunEvidence.PromptVersion] (Phase 50 D-03/EXEC-03). It is
// a single package-level version covering every template in this package —
// NOT per-template — matching the repo's explicit-compiled-const idiom
// (e.g. highSeverityFindingToken in pkg/dispatch/envelope.go) over an
// auto-derived content hash.
//
// MAINTENANCE RULE (applies to every template file in templates/*.tmpl, not
// just one): bump this const in the SAME commit as any prompt-template
// content change. PromptVersion is a run-evidence field consumed for
// cross-attempt evidence comparison (EXEC-03); a stale value silently
// corrupts that comparison by claiming two attempts ran the same prompt when
// they did not.
const PromptTemplateVersion = "v3"

// LoadPromptTemplate returns the compiled-in Go text/template for the given
// (role, level) tuple. v2 (Phase 51 EVAL-04) ships six templates — one per
// orchestrator-dispatched planner/executor/verifier level (project,
// milestone, phase, plan, task, plus task's verifier):
//
//   - role="planner",  level="project"    → templates/project_planner.tmpl
//   - role="planner",  level="milestone"  → templates/milestone_planner.tmpl
//   - role="planner",  level="phase"      → templates/phase_planner.tmpl
//   - role="planner",  level="plan"       → templates/plan_planner.tmpl
//   - role="executor", level="task"       → templates/task_executor.tmpl
//   - role="verifier", level="task"       → templates/task_verifier.tmpl
//
// The project-level planner authors the Milestone from Project.Spec.outcome —
// mirroring the stub-subagent's case "project" project→Milestone authoring
// (cmd/stub-subagent/main.go). The level set MUST stay project→milestone→phase
// →plan→task (CLAUDE.md: "Don't collapse levels or invent new ones").
//
// Filename convention is "<level>_<role>.tmpl" — see PATTERNS.md
// §"internal/subagent/common/prompt_templates.go (NEW)". The template is
// loaded fresh on each call (template.ParseFS reads from the embed.FS — no
// disk I/O); callers that want a cached parse should cache the returned
// *template.Template themselves.
//
// The expected template execution context is a [pkgdispatch.EnvelopeIn] value:
// templates may reference {{.Level}}, {{.TaskUID}}, {{.Role}},
// {{.Provider.Model}}, {{.Provider.Vendor}}, etc. Per CONTEXT.md §"Claude's
// Discretion: Prompt-template content", the exact prose is v1-minimal and
// users refine the templates iteratively post-v1; the loader contract is
// fixed.
//
// Returns a wrapped fs.ErrNotExist (via template.ParseFS) if no template
// matches the requested (role, level) tuple.
func LoadPromptTemplate(role, level string) (*template.Template, error) {
	name := fmt.Sprintf("templates/%s_%s.tmpl", level, role)
	tmpl, err := template.ParseFS(templateFS, name)
	if err != nil {
		return nil, fmt.Errorf("common: load prompt template %q: %w", name, err)
	}
	return tmpl, nil
}
