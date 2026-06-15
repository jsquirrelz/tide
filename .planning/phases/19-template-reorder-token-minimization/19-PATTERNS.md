# Phase 19: Template Reorder + Token Minimization - Pattern Map

**Mapped:** 2026-06-15
**Files analyzed:** 8 (5 templates + 2 testdata groups + 1 render file)
**Analogs found:** 8 / 8

---

## File Classification

| Modified File | Role | Data Flow | Closest Analog / Exemplar | Match Quality |
|---------------|------|-----------|--------------------------|---------------|
| `internal/subagent/common/templates/milestone_planner.tmpl` | template | transform | `task_executor.tmpl` (cleanest skeleton) | exact-skeleton |
| `internal/subagent/common/templates/project_planner.tmpl` | template | transform | `milestone_planner.tmpl` | exact-skeleton |
| `internal/subagent/common/templates/phase_planner.tmpl` | template | transform | `milestone_planner.tmpl` | exact-skeleton |
| `internal/subagent/common/templates/plan_planner.tmpl` | template | transform | `milestone_planner.tmpl` | exact-skeleton |
| `internal/subagent/common/templates/task_executor.tmpl` | template | transform | `milestone_planner.tmpl` (for structure) + self (for filesystem block) | role-match |
| `internal/eval/testdata/goldie/*.golden` (×5) | test-fixture | batch | committed goldens in same dir | exact |
| `internal/eval/testdata/ratchets/*.txt` (×5) | test-fixture | batch | committed ratchets in same dir | exact |
| `internal/subagent/common/prompt_templates.go` | utility | request-response | self (no change needed per Option A) | read-only verify |

---

## Pattern Assignments

### All five `.tmpl` files — skeleton reference exemplar

The **current** shared skeleton (extracted from all five files; this is the before-state):

**Section A — Role preamble** (line 1, template-specific):
```
You are TIDE's <role>.
```

**Section B — TIDE paradigm paragraph** (lines 3-7, nearly identical across all five):
```
TIDE (Topologically-Indexed Dependency Execution) runs hierarchical agentic
coding work as a Milestone → Phase → Plan → Task → Wave DAG, dispatched as
Kubernetes subagent Jobs. Read the project spec at /workspace/repo.git/README.md
before authoring anything — the spec is load-bearing and describes the
five-level paradigm and the wave-derivation rules.
```
`phase_planner.tmpl` and `plan_planner.tmpl` add "and the owning MILESTONE.md / PHASE-BRIEF.md" — template-specific variation.

**Section C — Dispatch metadata block** (lines 9-14, identical across all five):
```
Dispatch metadata for this run:
  Level:           {{.Level}}
  Role:            {{.Role}}
  TaskUID:         {{.TaskUID}}
  Provider.Vendor: {{.Provider.Vendor}}
  Provider.Model:  {{.Provider.Model}}
```
This entire block is REMOVED by D-02. TaskUID migrates to the volatile suffix (Section V below).

**Section D — "Your job" instructions** (lines 16+, template-specific):
Each template has 2-8 bullets describing its level-specific decomposition duty. `task_executor.tmpl` has no HOW-TO-EMIT children block; it has a filesystem layout block instead.

**Section E — HOW TO EMIT block** (planner templates only):
Contains inline `{{.TaskUID}}` path interpolations, e.g.:
```
  /workspace/envelopes/{{.TaskUID}}/children/phase-01.json
  /workspace/envelopes/{{.TaskUID}}/children/phase-02.json
```
These inline UIDs are REMOVED by D-01. The HOW-TO-EMIT header and child JSON shape description survive; only the example paths become abstract.

**Section F — Footer directives** (last 2-3 lines before `{{.Prompt}}`):
```
Write ONLY into the children/ directory shown above; files elsewhere are
ignored. The orchestrator reads every *.json there into typed child CRDs.
Markdown is the human-review surface; the JSON files are the machine contract.
```

**Section G — Prompt terminator** (final 2 lines):
```
Original prompt:
{{.Prompt}}
```
(`project_planner.tmpl` uses "Original prompt (the project outcome):" — template-specific variant.)

**Target skeleton per D-03** (the after-state all five must reach):
```
Section A — Role preamble (unchanged)
Section B — Fixed instructions: paradigm compressed + Your job + HOW TO EMIT (UID-free)
Section D-07 — Shared-context slot marker (bare {{/* ... */}} — no trim markers)
Section V — Volatile suffix (TaskUID + path mapping)
Section G — {{.Prompt}}
```

---

### `internal/subagent/common/templates/task_executor.tmpl` — special case

`task_executor.tmpl` is structurally different from the four planners:
- Has a **filesystem layout block** (lines 15-20) with 6 `{{.TaskUID}}` occurrences that are
  all path interpolations (in.json, out.json, events.jsonl, worktrees/). These are the
  highest-count UID occurrences in any template. Per D-01, they ALL move to the volatile suffix.
- Has **no children/ HOW-TO-EMIT block** — it emits `out.json` not child CRDs.
- "Your job" bullets (lines 22-24) reference `{{.TaskUID}}` paths directly.

**Current content** (`internal/subagent/common/templates/task_executor.tmpl`, full file, 41 lines):

Filesystem layout block (lines 15-20) — the volatile-suffix target for this template:
```
Filesystem layout (mounted by the in-pod harness):
  /workspace/envelopes/{{.TaskUID}}/in.json      — your full input envelope
  /workspace/envelopes/{{.TaskUID}}/out.json     — write your result here
  /workspace/envelopes/{{.TaskUID}}/events.jsonl — raw event log (provider-emitted)
  /workspace/worktrees/{{.TaskUID}}/             — per-Task git worktree; operate here
  /workspace/repo.git/                            — bare clone (read-only)
```

After reorder, the abstract instructions in Section B say "operate in your worktree; read
in.json" (without UID); the volatile suffix carries the concrete mapping:
```
TaskUID: {{.TaskUID}}
Your task dir:
  /workspace/envelopes/{{.TaskUID}}/in.json      — your full input envelope
  /workspace/envelopes/{{.TaskUID}}/out.json     — write your result here
  /workspace/envelopes/{{.TaskUID}}/events.jsonl — raw event log (provider-emitted)
  /workspace/worktrees/{{.TaskUID}}/             — per-Task git worktree; operate here
  /workspace/repo.git/                            — bare clone (read-only)
```

---

### D-06 Annotation Pattern (apply to all five templates, Commit A)

Place the annotation comment on its own line BEFORE the annotated line, using trim-right only
(`-}}` absent on right side of comment open is wrong; the correct form is `{{/* */ -}}`):

**Correct zero-impact annotation** (preserves surrounding newlines):
```
{{/* WHY: "JSON files are the machine contract" — admission webhook parses *.json
     in children/; Markdown writes are ignored by the controller. Removing this
     loses the model's awareness of the two-artifact contract. */ -}}
Markdown is the human-review surface; the JSON files are the machine contract.
```

The `-}}` trim-right on the comment's closing brace consumes the comment line's own `\n`,
so the comment adds zero bytes to rendered output and the line above it retains its `\n`.

**What NOT to use** (both trim markers collapse surrounding newlines unintentionally):
```
{{- /* WHY */ -}}   ← WRONG: eats preceding \n, merges lines
```

---

### D-07 Slot Reservation Pattern (apply to all five templates, Commit B)

Place the shared-context slot marker between the last fixed-instruction line and the volatile
suffix. Use **no trim markers** (bare `{{/* ... */}}`):

```
...final fixed instruction line.
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}

TaskUID: {{.TaskUID}}
Your task dir: /workspace/envelopes/{{.TaskUID}}/  (children/  out.json  worktrees/...)
```

Rendered output: `"...final fixed instruction line.\n\nTaskUID: uid-123\n"` — the comment
line's own `\n` becomes the blank-line section separator; the explicit `\n` after the comment
in source creates the second blank line before "TaskUID:".

**What NOT to use:**
```
{{- /* slot */ -}}  ← WRONG: collapses blank line, merges sections into one paragraph
{{/* slot */}}      ← WRONG if no blank line follows: adds double-blank when blank line is also in source
```

Verified by live Go programs (documented in 19-RESEARCH.md §Go text/template Whitespace Mechanics).

---

### Volatile Suffix Shape (all planner templates, Commit B)

After removing the dispatch metadata block (Section C) and converting inline UID paths to
abstract text, the volatile suffix for the four planner templates is:

```
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}

TaskUID: {{.TaskUID}}
Your task dir: /workspace/envelopes/{{.TaskUID}}/  (children/  out.json  worktrees/...)

Original prompt:
{{.Prompt}}
```

For `task_executor.tmpl`, the volatile suffix carries the full filesystem layout block
(which has the most UID occurrences — 5 path lines + 1 metadata block = 6 total pre-reorder):

```
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}

TaskUID: {{.TaskUID}}
Filesystem layout:
  /workspace/envelopes/{{.TaskUID}}/in.json      — your full input envelope
  /workspace/envelopes/{{.TaskUID}}/out.json     — write your result here
  /workspace/envelopes/{{.TaskUID}}/events.jsonl — raw event log (provider-emitted)
  /workspace/worktrees/{{.TaskUID}}/             — per-Task git worktree; operate here
  /workspace/repo.git/                            — bare clone (read-only)

Original prompt:
{{.Prompt}}
```

---

## Shared Patterns: Goldie Golden + Ratchet Update Protocol

This is the core cross-cutting discipline for Phase 19. Every commit touching a template
**must** update all three in the same commit: `.tmpl` + `.golden` + `.txt`.

### Source: `internal/eval/render_test.go`

**Golden assertion function** (lines 114-126):
```go
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
```

**Ratchet assertion function** (lines 166-191) — STRICT frozen-byte-count:
```go
func ratchetAssert(t *testing.T, role, level, name string) {
    t.Helper()
    // ... load template, render to buf ...
    ratchetFile := "testdata/ratchets/" + name + ".txt"
    data, err := os.ReadFile(ratchetFile)
    // ... parse frozen int ...
    actual := buf.Len()
    if actual != frozen {
        t.Errorf("template %s byte count changed: rendered %d bytes, frozen ratchet %d — "+
            "update %s in the same deliberate commit if the template change is intentional",
            name, actual, frozen, ratchetFile)
    }
}
```

Key: `actual != frozen` — strict equality, not `>`. Shrink fails just as growth fails.

**Fixture envelope** (lines 40-51 — `baseEnvelope`):
```go
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
```

The fixture `TaskUID` is `"eval-fixture-uid-000"` — this is what gets interpolated in
golden files. Every path example in the template that uses `{{.TaskUID}}` will render as
`eval-fixture-uid-000` in the golden.

**Per-template (role, level) pairings** (lines 67-77):
```go
var templateCases = []struct {
    role  string
    level string
    name  string
}{
    {"planner", "project", "project_planner"},
    {"planner", "milestone", "milestone_planner"},
    {"planner", "phase", "phase_planner"},
    {"planner", "plan", "plan_planner"},
    {"executor", "task", "task_executor"},
}
```

Individual test functions exist for each template — `TestGoldenRender_MilestonePlanner`,
`TestByteRatchet_MilestonePlanner`, etc. The pattern `TestGoldenRender_<CamelName>` is how
the planner writes `-run` filter arguments.

### Golden Regeneration Command
```bash
# Regenerate one specific golden (after template edit):
go test -update ./internal/eval/ -run TestGoldenRender_MilestonePlanner

# Regenerate all five at once (use after a structural reorder that touches all):
go test -update ./internal/eval/ -run "TestGoldenRender_"
```

`-update` is goldie v2's standard flag (`flag.Bool("update", ...)` in goldie.go:64). It writes
actual rendered bytes to `testdata/goldie/<name>.golden`. Golden directory is configured via
`goldie.WithFixtureDir("testdata/goldie")` (render_test.go:124).

### Ratchet Update Command (MANUAL — no -update equivalent)
```bash
# 1. Run failing test to get the new actual byte count:
go test ./internal/eval/ -run TestByteRatchet_MilestonePlanner -v 2>&1 | grep "byte count changed"
# Output: "template milestone_planner byte count changed: rendered 1987 bytes, frozen ratchet 2214"

# 2. Update the ratchet file with the new count:
echo "1987" > internal/eval/testdata/ratchets/milestone_planner.txt

# 3. Confirm the ratchet passes:
go test ./internal/eval/ -run TestByteRatchet_MilestonePlanner
```

Ratchet file location: `internal/eval/testdata/ratchets/<name>.txt` where `<name>` is the
goldie name (e.g. `milestone_planner`, `task_executor`). Each file contains a single integer.

### Current Frozen Byte Counts (ratchets baseline before Phase 19 trims)

| Template | Ratchet file | Frozen bytes |
|----------|-------------|-------------|
| `project_planner` | `testdata/ratchets/project_planner.txt` | 2474 |
| `milestone_planner` | `testdata/ratchets/milestone_planner.txt` | 2214 |
| `phase_planner` | `testdata/ratchets/phase_planner.txt` | 2271 |
| `plan_planner` | `testdata/ratchets/plan_planner.txt` | 4281 |
| `task_executor` | `testdata/ratchets/task_executor.txt` | 1961 |

Phase 19 ratchets all five DOWN. Each trim commit replaces the frozen count with the new lower count.

### Full Gate Command
```bash
make test
```
Expands to (Makefile:86):
```bash
KUBEBUILDER_ASSETS="..." go test -short -timeout 120s \
  $(go list ./... | grep -v /e2e | grep -v /test/integration) -coverprofile cover.out
```
`internal/eval/` is included (no build tag on eval tests). Run after each per-section commit.

### Online Token Confirmation (D-05, non-gate)
```bash
TIDE_PROXY_ENDPOINT=https://127.0.0.1:8443 TIDE_SIGNED_TOKEN=<token> make eval
```
Requires credproxy. Not a gate — use to confirm actual Anthropic token count decreased after
trim commits. The ratchet byte count is UTF-8 bytes; token count differs.

---

## `internal/subagent/common/prompt_templates.go` — No Change Needed (Option A)

**Source: `internal/subagent/common/prompt_templates.go`, lines 65-72**:
```go
func LoadPromptTemplate(role, level string) (*template.Template, error) {
    name := fmt.Sprintf("templates/%s_%s.tmpl", level, role)
    tmpl, err := template.ParseFS(templateFS, name)
    if err != nil {
        return nil, fmt.Errorf("common: load prompt template %q: %w", name, err)
    }
    return tmpl, nil
}
```

`template.ParseFS(templateFS, name)` takes a **single filename**. Under Option A (self-contained
templates, no shared partials), this requires NO change. If Phase 19 adds a `{{define}}` partial
in a shared file (Option B), `ParseFS` must be called with both filenames. Research recommends
Option A — leave `prompt_templates.go` untouched.

The `//go:embed templates/*.tmpl` directive on line 32 automatically picks up any `.tmpl` files
added to the templates directory, but since Phase 19 does not add new files this is informational.

---

## Commit Protocol (per-section, all five templates)

The planner must structure tasks around this three-file-per-commit discipline:

**Commit A — Annotation pass (PROMPT-03)**
- Edit: add `{{/* WHY */ -}}` comments before each load-bearing line in one template
- Command: `make test` (verify golden + ratchet still pass — annotation MIGHT add bytes)
- If annotation changes bytes: run golden regeneration + ratchet update in same commit
- Value: establishes "why this stays" record before any removal

**Commit B — Reorder (PROMPT-01 / PROMPT-02)**
- Edit: move sections to D-03 order; remove dispatch metadata block; convert inline UID paths
  to abstract text; add volatile suffix; add D-07 slot marker
- Command: `go test -update ./internal/eval/ -run TestGoldenRender_<Name>` then read new byte
  count, `echo <count> > testdata/ratchets/<name>.txt`
- Gate: `make test` must be green before commit

**Commits C, D, E... — Trim commits (PROMPT-04), one section per commit**
- Edit: remove one redundant section; write per-section commit message with removal rationale
- Command: goldie update + ratchet update + `make test`
- Ratchet count decreases each time

**Verification after each commit:**
```bash
# Quick: eval package only
go test ./internal/eval/

# Full: all unit tiers including fmt/vet
make test
```

---

## No Analog Found

None — every file modified in Phase 19 has a direct analog (the existing template files for
the template edits; the existing golden/ratchet files for the testdata updates; the existing
render_test.go for the harness pattern). Phase 19 is purely in-place editing.

---

## Metadata

**Analog search scope:** `internal/subagent/common/templates/`, `internal/eval/`, `internal/eval/testdata/`
**Files read:** 5 templates + render_test.go + prompt_templates.go + envelope.go (partial) + 5 ratchet files + 2 golden files + Makefile (targets)
**Pattern extraction date:** 2026-06-15
