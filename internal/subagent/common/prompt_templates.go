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

// LoadPromptTemplate returns the compiled-in Go text/template for the given
// (role, level) tuple. v1 ships four templates:
//
//   - role="planner",  level="milestone"  → templates/milestone_planner.tmpl
//   - role="planner",  level="phase"      → templates/phase_planner.tmpl
//   - role="planner",  level="plan"       → templates/plan_planner.tmpl
//   - role="executor", level="task"       → templates/task_executor.tmpl
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
