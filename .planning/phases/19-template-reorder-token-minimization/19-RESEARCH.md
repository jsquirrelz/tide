# Phase 19: Template Reorder + Token Minimization — Research

**Researched:** 2026-06-15
**Domain:** Go text/template whitespace mechanics + eval harness gating + prompt engineering
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** De-volatilize inline `{{.TaskUID}}` by defining paths once in the volatile suffix. Keep
  the instruction body byte-identical across wave siblings by writing it UID-free/abstract. Pure
  template edit — no in-pod harness change.
- **D-02:** Volatile suffix carries TaskUID only (+ task-dir path mapping + `{{.Prompt}}`). Drop
  the printed `Level`/`Role` lines and the printed `Provider.Vendor`/`Model` lines. `.Provider`
  STAYS on the EnvelopeIn struct — only the printed lines go.
- **D-03:** Canonical section order applied uniformly to all five templates: role preamble →
  fixed instructions → (shared-context slot, reserved — see D-07) → volatile suffix (TaskUID +
  task-dir mapping) → `{{.Prompt}}`.
- **D-04:** Conservative trim. Trim only pure redundancy and formatting slack; every instruction
  keeps its full semantic content. No aggressive imperative-rewrite.
- **D-05:** Confidence check = per-section commit + human review of annotated diff + `make eval`
  token confirmation. No live model A/B.
- **D-06:** Inline `{{- /* … */ -}}` template comments for surviving load-bearing lines.
  Use the `{{- -}}` trim markers so the comment leaves no extra newline. The "why it was safe to
  remove X" record goes in the per-section commit message.
- **D-07:** Reserve the shared-context slot now as a zero-token marker. Place
  `{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}` (with trim-right only)
  between the fixed instructions and the volatile suffix.

### Claude's Discretion
- Exact compressed wording of the paradigm preamble.
- Whether repeated boilerplate is de-duplicated via a shared Go template partial (`{{define}}` /
  `{{template}}` include) vs kept self-contained per file — maintainability call.
- Offline ratchet proxy unit (already fixed by Phase 18 — Phase 19 ratchets numbers DOWN).
- The precise rendered form of the volatile-suffix path mapping.

### Deferred Ideas (OUT OF SCOPE)
- `SharedContext` field on `EnvelopeIn` + identical-per-wave population — Phase 20 (CACHE-02/03).
- Cross-pod prefix-cache verification spike — Phase 20 (CACHE-01).
- Per-level token accounting + cache-hit dashboard panel — Phase 21 (OBSV-01–03).
- LLM-as-judge / semantic-quality scoring — EVAL-F1, deferred.
- Cross-template DRY via shared template partials (left to planner discretion).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PROMPT-01 | All five templates reordered stable-prefix-first (role preamble → fixed instructions → shared context → volatile metadata → per-task prompt) | D-03 locked order; Go text/template reorder is pure text edit; goldie golden regeneration is the gate |
| PROMPT-02 | Volatile per-dispatch metadata (`TaskUID`, `Provider.*`) moved to suffix | D-01 / D-02 locked; TaskUID occurrence map (§Template Audit) shows exactly where to relocate each interpolation |
| PROMPT-03 | Each template carries "why-this-line" annotation produced before trimming | D-06 locked mechanism: `{{- /* WHY */ -}}` on its own line BEFORE the annotated line; whitespace mechanics verified (§Go template whitespace) |
| PROMPT-04 | Non-essential boilerplate trimmed one section at a time, each commit gated green | Commit protocol: template edit + ratchet .txt update + goldie golden regeneration, all in one commit; `make test` is the gate |
| PROMPT-05 | Structured data in the stable prefix serialized with stable key order | Phase 19 is a no-op for PROMPT-05 — templates today interpolate only scalars (`.Level`, `.Role`, `.TaskUID`, `.Provider.Vendor`, `.Provider.Model`, `.Prompt`); no map data in the stable prefix |
</phase_requirements>

---

## Summary

Phase 19 is a pure template-editing phase with three tightly interlocked concerns: (1) getting the
whitespace/comment mechanics exactly right so the Phase 18 eval harness stays green throughout, (2)
atomically updating goldie goldens AND ratchet counts in every commit that changes rendered bytes,
and (3) preserving semantic content while removing only redundant text.

The five templates share an almost-identical skeleton: role line → TIDE paradigm paragraph →
"Dispatch metadata" block (Level/Role/TaskUID/Provider.*) → "Your job" → "HOW TO EMIT" (with
inline `{{.TaskUID}}` path interpolations) → `Original prompt: {{.Prompt}}`. The reorder collapses
the metadata block, lifts it to the volatile suffix, and converts path examples from
`/workspace/envelopes/{{.TaskUID}}/children/...` inline directives into abstract instructions
(`write one JSON file per child into your children/ directory`) with a single concrete path
mapping in the volatile suffix.

The Phase 18 harness is a STRICT frozen-byte-count ratchet: any byte change (growth OR shrink)
fails `make test` until the ratchet `.txt` file is updated in the same commit. Goldie golden
files also fail on any content change. This means EVERY commit touching a template requires
three simultaneous edits: the `.tmpl` file, its ratchet `.txt`, and its `.golden` file.

**Primary recommendation:** Work one template at a time; within each template work one section at a
time; in each commit update the `.tmpl`, the corresponding `.golden`, AND the corresponding
`.txt` ratchet simultaneously so `make test` stays green at every commit boundary.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Prompt rendering | Compiled-in Go templates (`internal/subagent/common/`) | n/a | Embedded via `go:embed`; no runtime filesystem dependency |
| Token gating | Eval harness (`internal/eval/`) + `make test` | `make eval` (online) | Deterministic offline gate in unit tier; online confirmation as manual step |
| Ratchet enforcement | `testdata/ratchets/*.txt` + `ratchetAssert` | n/a | Strict frozen-byte-count; manual integer update in same commit as template change |
| Golden diffing | `testdata/goldie/*.golden` + goldie v2 | n/a | `go test -update ./internal/eval/...` regenerates |
| Volatile data injection | `pkg/dispatch/envelope.go EnvelopeIn` | n/a | `.Provider` stays on struct; only printed lines removed |

---

## Standard Stack

### Core (already installed — no new dependencies)

| Library | Version | Purpose | Note |
|---------|---------|---------|------|
| `text/template` | Go stdlib | Template parsing + rendering | `{{- /* */ -}}` trim mechanics verified [VERIFIED] |
| `github.com/sebdah/goldie/v2` | v2.8.0 | Golden file assertions | Test-only; `go test -update` flag [VERIFIED: go.mod] |
| `make test` | Makefile:85 | Unit gate: manifests + generate + fmt + vet + `go test -short` | Phase 19 gate per D-02a [VERIFIED: codebase] |

**No new dependencies.** Phase 19 edits five `.tmpl` files and three groups of testdata files.
All tooling landed in Phase 18.

---

## Package Legitimacy Audit

No external packages are installed in Phase 19. All tooling is already in `go.mod`.

---

## Architecture Patterns

### System Architecture Diagram

```
.tmpl file change
      |
      v
  make test  ─────────────────────────────────────────────────────┐
      |                                                            |
      |── go test ./internal/eval/ (ratchetAssert)                |
      |       reads testdata/ratchets/<name>.txt                  |
      |       compares len(rendered) == frozen integer            |
      |       FAIL if ANY divergence (grow or shrink)             |
      |                                                            |
      |── go test ./internal/eval/ (goldenAssert)                 |
      |       goldie.Assert(t, name, rendered)                    |
      |       compares rendered bytes to testdata/goldie/<n>.golden|
      |       FAIL if any byte differs                             |
      |                                                            |
      └────────── PASS only if BOTH pass ─────────────────────────┘

Per-commit required set (all three in same commit):
  1. internal/subagent/common/templates/<name>.tmpl  ← template change
  2. internal/eval/testdata/goldie/<name>.golden     ← regenerated
  3. internal/eval/testdata/ratchets/<name>.txt      ← new byte count
```

### Recommended Commit Protocol

Per commit (one section of one template at a time):

```bash
# 1. Edit the .tmpl file

# 2. Regenerate goldie golden (captures new rendered bytes)
go test -update ./internal/eval/ -run TestGoldenRender_<TemplateName>

# 3. Compute new byte count and update ratchet
go test ./internal/eval/ -run TestByteRatchet_<TemplateName> 2>&1
# It will print: "rendered 2014 bytes, frozen ratchet 2214"
# Manually update: echo "2014" > internal/eval/testdata/ratchets/<name>.txt

# 4. Confirm both tests pass
go test ./internal/eval/ -run "TestGoldenRender_<Name>|TestByteRatchet_<Name>"

# 5. Full gate
make test
```

### Template Section Audit (current state, verified by reading source)

**All five templates** currently share this skeleton [VERIFIED: source read]:

```
Section A — Role preamble (1 line, template-specific)
  "You are TIDE's <role>."

Section B — TIDE paradigm paragraph (3-4 lines)
  "TIDE (Topologically-Indexed Dependency Execution) runs hierarchical..."
  Ends with "Read the project spec at /workspace/repo.git/README.md before
  authoring anything — the spec is load-bearing..."

Section C — Dispatch metadata block (6 lines)
  "Dispatch metadata for this run:"
    Level:           {{.Level}}
    Role:            {{.Role}}
    TaskUID:         {{.TaskUID}}
    Provider.Vendor: {{.Provider.Vendor}}
    Provider.Model:  {{.Provider.Model}}

Section D — "Your job" instructions (template-specific, 4-8 bullets)

Section E — "HOW TO EMIT" with inline {{.TaskUID}} paths (template-specific)
  Example paths: /workspace/envelopes/{{.TaskUID}}/children/phase-01.json

Section F — Footer directives (1-3 lines, partially shared)
  "Write ONLY into the children/ directory..."
  "Markdown is the human-review surface; the JSON files are the machine contract."

Section G — Prompt terminator
  "Original prompt:"  (or "Original prompt (the project outcome):" in project_planner)
  {{.Prompt}}
```

**Target structure per D-03:**

```
Section A — Role preamble (unchanged — template-specific, no UID)
Section B — Fixed instructions (paradigm + Your job + HOW TO EMIT — UID-free, abstract paths)
Section D-07 — Shared-context slot marker (zero-token comment)
Section V — Volatile suffix (TaskUID + path mapping derived from it)
Section G — {{.Prompt}}
```

### TaskUID Occurrence Map (current, verified)

`[VERIFIED: source read of all five .tmpl files]`

| Template | TaskUID occurrences | Locations |
|----------|--------------------|-----------| 
| `milestone_planner.tmpl` | 3 | line 12 (metadata block), lines 29-30 (children/ path examples) |
| `project_planner.tmpl` | 2 | line 12 (metadata block), line 32 (children/ path example) |
| `phase_planner.tmpl` | 3 | line 11 (metadata block), lines 28-29 (children/ path examples) |
| `plan_planner.tmpl` | 3 | line 11 (metadata block), lines 29-30 (children/ path examples) |
| `task_executor.tmpl` | 6 | line 11 (metadata block), lines 16-19 (filesystem layout: in.json, out.json, events.jsonl, worktrees/), lines 23-24 (Your job instructions) |

After D-01 / D-02 reorder: TaskUID appears **once** in each template, in the volatile suffix only.

### Current Byte Counts (ratchet baseline)

`[VERIFIED: testdata/ratchets/*.txt read]`

| Template | Current bytes | Notes |
|----------|--------------|-------|
| `project_planner` | 2474 | |
| `milestone_planner` | 2214 | |
| `phase_planner` | 2271 | |
| `plan_planner` | 4281 | Largest — has the FILE-TOUCH RULE + JSON escaping sections |
| `task_executor` | 1961 | Smallest — no HOW TO EMIT children block |

Phase 19 ratchets all five DOWN after trim. Phase 18 froze them at un-trimmed v1.0.1 counts.

---

## Go text/template Whitespace Mechanics (Verified by execution)

`[VERIFIED: ran live Go programs in /tmp/tmpltest*.go]`

### The `-` trim marker

The `{{-` left-trim marker removes ALL trailing whitespace from the immediately preceding text
node (the literal text between the previous action and this one). The `-}}` right-trim marker
removes ALL leading whitespace from the immediately following text node.

"Whitespace" = space, horizontal tab, carriage return, newline.

**Critical:** trim markers consume ALL contiguous whitespace, including multiple newlines.
`"text\n\n{{- /* comment */ -}}\n\nTaskUID"` produces `"textTaskUID"` — both blank lines and
the comment's own newline are all consumed.

### Comment behavior (verified results)

| Template source | Rendered output | Explanation |
|-----------------|-----------------|-------------|
| `"before\n{{/* comment */}}\nafter\n"` | `"before\n\nafter\n"` | Comment-line's own \n becomes a blank line |
| `"before\n{{- /* comment */ -}}\nafter\n"` | `"beforeafter\n"` | Trim-left eats \n before, trim-right eats \n after |
| `"before\n{{- /* comment */}}\nafter\n"` | `"before\nafter\n"` | Trim-left eats preceding \n; own \n preserved |
| `"before\n{{/* comment */ -}}\nafter\n"` | `"before\nafter\n"` | Own \n trimmed by trim-right; preceding \n preserved |
| `"text.{{- /* WHY */ -}}\nNext line.\n"` | `"text.Next line.\n"` | Both trim markers merge lines |

### D-06 annotation pattern (load-bearing line comment)

The correct pattern to annotate a load-bearing line without changing its rendered output is to
place the comment on its **own line preceding** the annotated line using trim-left only:

```
{{- /* WHY: kept because model needs concrete path to write output file */ -}}
Write ONLY into the children/ directory shown above; files elsewhere are ignored.
```

But note: `{{- /* */ -}}` with both trim markers on its own line EATS the preceding line's
trailing `\n`. This means the comment silently merges with the previous content in the output.

**For a truly zero-impact annotation** that preserves surrounding layout: place the comment on
its own line with trim-right only (`{{/* WHY */ -}}`). The preceding `\n` is preserved (kept as
the end-of-section separator), and the comment's own `\n` is consumed by trim-right.

```
...preceding section ends here.
{{/* WHY: line below is load-bearing; model uses it for worktree coordination */ -}}
Write ONLY into the children/ directory shown above; files elsewhere are ignored.
```

This produces: `"...preceding section ends here.\nWrite ONLY into..."` — the comment leaves
no trace in rendered output, the preceding `\n` is preserved.

### D-07 slot reservation pattern

The D-07 marker must render zero bytes but preserve a blank line between the fixed instructions
and the volatile suffix (to separate sections visually in the final rendered prompt).

**Correct pattern** (trim-right only, no trim-left):

```
...final fixed instruction line.
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}
TaskUID: {{.TaskUID}}
```

Rendered output: `"...final fixed instruction line.\n\nTaskUID: uid-123\n"` — one blank line
between sections. [VERIFIED by live Go program]

**Why NOT `{{- /* */ -}}`:** Both trim markers collapse the blank-line separator, producing
`"...final instruction line.TaskUID: uid-123\n"` — sections merge. [VERIFIED]

**Why NOT bare `{{/* */}}` on its own line without trim-right:** A bare comment on its own line
adds a blank line to the rendered output (the comment-line's `\n` survives as a blank line):
`"...section.\n\nTaskUID: uid\n"` becomes `"...section.\n\n\nTaskUID: uid\n"` — double-blank.
[VERIFIED: bare_comment_own_line test]

**Summary:** `{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}` (no trim
markers at all) placed on its own line between the last instruction and the volatile suffix
produces the cleanest output: exactly one blank line from the comment-line's `\n`.

---

## PROMPT-05 Analysis (Is it a no-op for Phase 19?)

`[VERIFIED: grep of all .tmpl files + provider.go read]`

**Conclusion: PROMPT-05 is a no-op safeguard for Phase 19. No concrete work needed now.**

The five templates interpolate only scalar string fields from `EnvelopeIn`:
- `.Level`, `.Role`, `.TaskUID`, `.Prompt` — `string` fields
- `.Provider.Vendor`, `.Provider.Model` — `string` fields on `ProviderSpec`

`ProviderSpec.Params` is `map[string]string` and is the only map-typed field on `EnvelopeIn`.
**No template references `.Params` or any other map-typed field.** [VERIFIED: `grep -rn "Params"` on all `.tmpl` files returned zero hits]

The only other structured data is `.Provider` itself (a struct, not a map) — Go templates
render struct fields deterministically via field access, not map-range, so key-order
non-determinism does not apply.

The stable-prefix-first reorder moves `.Provider.Vendor` and `.Provider.Model` OUT of the
stable prefix into the volatile suffix (D-02). Once they're in the volatile suffix, they no
longer affect stable-prefix byte identity. The dropped `Provider.Params` map-range concern is
moot: Params was never in a template, and the baseEnvelope fixture in `render_test.go` already
pins `Params` to `nil` to avoid map-iteration ordering flap (line 49: comment confirms this).

PROMPT-05's concrete implementation gate — "structured data serialized deterministically" — is
already satisfied by the fixture's `Params: nil` discipline. No template changes required for
PROMPT-05; it closes as a confirmed no-op when all five templates are confirmed to interpolate
only scalar fields.

---

## Phase 18 Eval Harness Mechanics (how to drive it)

`[VERIFIED: source read of internal/eval/render_test.go, Makefile, goldie.go]`

### Goldie golden regeneration

```bash
# Regenerate ALL five goldens (run after reorder commit to capture new layout)
go test -update ./internal/eval/ -run "TestGoldenRender_"

# Regenerate one specific golden
go test -update ./internal/eval/ -run TestGoldenRender_MilestonePlanner
```

`-update` is a standard goldie v2 flag (defined as `flag.Bool("update", ...)` in goldie.go:64).
It writes the actual rendered bytes to `testdata/goldie/<name>.golden`. The goldie fixture
directory is configured as `testdata/goldie` via `goldie.WithFixtureDir("testdata/goldie")`
(render_test.go:124).

### Ratchet update (MANUAL — no auto-update flag)

The ratchet is a STRICT frozen-byte-count: any divergence (grow or shrink) fails. There is no
`-update` equivalent for ratchets. The update procedure is:

```bash
# 1. Run the failing test to get the actual byte count
go test ./internal/eval/ -run TestByteRatchet_MilestonePlanner -v 2>&1
# Output: "template milestone_planner byte count changed: rendered 1987 bytes, frozen ratchet 2214"

# 2. Update the ratchet file manually
echo "1987" > internal/eval/testdata/ratchets/milestone_planner.txt

# 3. Confirm
go test ./internal/eval/ -run TestByteRatchet_MilestonePlanner
```

Ratchet files: `internal/eval/testdata/ratchets/{project,milestone,phase,plan}_planner.txt` and
`task_executor.txt`. Each contains a single integer (the frozen rendered byte count).

### Full gate command

```bash
make test
```

This runs: `manifests generate fmt vet setup-envtest go test -short -timeout 120s ./... (excluding /e2e and /test/integration)`. The eval tests in `internal/eval/` are included with no build tag.
`make test-only` skips the prep steps (use after first `make test` run warms envtest).

### `make eval` (online token confirmation, D-05)

```bash
TIDE_PROXY_ENDPOINT=https://127.0.0.1:8443 TIDE_SIGNED_TOKEN=<token> make eval
```

Requires credproxy running + valid signed token. Run after each trim commit to confirm the
real Anthropic token count went down. Results are per-template actual token counts + whether
each prefix clears the 1,024-token cache floor. `cmd/tide-eval/` behind `//go:build eval`.
**This is a maintainer report tool, not a gate.** The gate is `make test`.

---

## Anti-Patterns to Avoid

### Anti-Pattern 1: Updating golden OR ratchet without the other
Goldie and ratchet both check the rendered bytes of the same template. They must be updated
together, in the same commit as the template change. Updating just the golden leaves the
ratchet failing; updating just the ratchet leaves the golden failing.

### Anti-Pattern 2: Using `{{- /* comment */ -}}` for D-07 slot reservation
Both trim markers collapse the section separator blank line. The slot marker must use no trim
markers (bare `{{/* ... */}}`) on its own line to produce a blank line in the output, OR use
trim-right only. See whitespace mechanics section above.

### Anti-Pattern 3: Trimming the paradigm paragraph entirely
The "Read the project spec at `/workspace/repo.git/README.md` first — it's load-bearing" line
is NOT boilerplate. It is the primary directive that prevents in-context guessing from
overriding the authoritative spec. PITFALLS.md flags over-trimming as catastrophic for run
quality (D-04 rationale). Conservative trim = compress the contextual description around this
directive, not the directive itself.

### Anti-Pattern 4: Removing the duplicate "Markdown is the human-review surface; the JSON
files are the machine contract" line without annotating first
This line appears in multiple templates and is repeated within plan_planner.tmpl. It IS a
D-04 trim candidate (pure redundancy), but only AFTER annotation pass confirms the semantic
content is captured elsewhere and the section commit message documents the removal rationale.

### Anti-Pattern 5: Splitting the reorder commit from the golden/ratchet commit
The STRICT ratchet fails `make test` the instant bytes change. The reorder changes bytes.
`make test` must be green at every commit boundary. Therefore the template change, golden
regeneration, and ratchet update MUST be in the same commit.

### Anti-Pattern 6: Adding cross-template DRY ({{define}}/{{template}} includes) without
updating LoadPromptTemplate
`LoadPromptTemplate` calls `template.ParseFS(templateFS, name)` with a single filename.
If a shared template file using `{{define "block"}}...{{end}}` is added, `LoadPromptTemplate`
must be updated to `template.ParseFS(templateFS, "templates/shared.tmpl", name)` to include
the shared file. The planner must decide on DRY vs self-contained before writing tasks; this
is a Claude's Discretion decision.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead |
|---------|-------------|-------------|
| Byte count diffing | Custom diff tool | Goldie v2 (already installed) |
| Token counting | Guessed tokenizer | `make eval` → Anthropic `count_tokens` API via credproxy |
| Ratchet auto-update | Script that writes .txt | Manual integer edit (the discipline IS the feature) |
| Cross-template DRY | Custom preprocessor | Go text/template `{{define}}` + `{{template}}` (if chosen) |

---

## Safe Sequencing (PROMPT-03 before PROMPT-01/02)

The safe commit sequence within each template:

```
Commit A — Annotation pass (PROMPT-03)
  Action:   Add {{/* WHY */ -}} comments before each section; NO content removed
  Ratchet:  Changes IF annotation adds whitespace → update .txt
  Golden:   Regenerate if annotation changes rendered bytes
  Gate:     make test green
  Value:    Creates the durable "why it stays" record before any removal

Commit B — Reorder (PROMPT-01 / PROMPT-02)
  Action:   Move sections to D-03 order; convert inline UIDs to abstract text;
            add volatile suffix; add D-07 slot marker
  Ratchet:  WILL change (reorder changes byte count) → update .txt
  Golden:   MUST regenerate (content changes) → go test -update
  Gate:     make test green
  Risk:     Reorder alone typically reduces bytes (drops 5 metadata lines × 20 chars)

Commit C, D, E... — Trim commits (PROMPT-04), one section per commit
  Action:   Remove one section's redundancy; update commit msg with removal rationale
  Ratchet:  WILL decrease → update .txt to lower number
  Golden:   MUST regenerate
  Gate:     make test green after each commit
```

**Does reorder alone change rendered bytes?** YES. Removing the Dispatch metadata block (6
lines × ~20 chars ≈ 120 bytes) and converting inline UID paths to abstract text changes the
byte count. Golden MUST be regenerated. The fixture `eval-fixture-uid-000` is fixed, so
goldie diffs are deterministic and serve as the PR review artifact.

---

## Cross-Template DRY: Planner's Decision

`[VERIFIED: prompt_templates.go:67 — single-file ParseFS call]`

**Option A — Self-contained per file (no code change):**
Each `.tmpl` file is a complete standalone prompt. Trivially compatible with the current
`LoadPromptTemplate` which calls `template.ParseFS(templateFS, name)` with one filename.
Redundancy between files is acceptable since different levels render different prompts (no
caching benefit from cross-template DRY, per 19-CONTEXT.md).

**Option B — Shared template partial (`{{define}}` in a shared file):**
Requires adding `templates/shared_preamble.tmpl` and updating `LoadPromptTemplate` to
`template.ParseFS(templateFS, "templates/shared_preamble.tmpl", name)`. Go text/template
supports this: the named template is the main file; the shared file defines blocks consumed
via `{{template "blockname" .}}`. Would require a `LoadPromptTemplate` change (a code change
beyond pure template edits) and would touch `prompt_templates.go` and its tests.

**Recommendation for planner:** Choose Option A for Phase 19. It keeps all changes to `.tmpl`
files only (pure template edit per D-01), avoids `LoadPromptTemplate` refactor scope, and the
redundancy across five files is tolerable (five files ≈ five templates, each only touched at
its own level). Option B can be revisited in a future maintenance phase.

---

## Common Pitfalls

### Pitfall 1: The STRICT ratchet fails on shrink as well as growth
**What goes wrong:** Developer trims a template, goldie -update regenerates the golden, but
forgets to update the ratchet .txt. `make test` fails on TestByteRatchet_*.
**Why:** The ratchet is STRICT (growth-only ratchets allow re-expansion back to old size after
a shrink; STRICT ratchets prevent this). "Any divergence" = failure.
**How to avoid:** Always update the ratchet .txt in the same commit as the template change.
Run `go test ./internal/eval/ -run TestByteRatchet_<Name> -v` to read the actual byte count.

### Pitfall 2: Trim-left on the D-07 slot comment collapses section separator
**What goes wrong:** `{{- /* slot */ -}}` eats both surrounding newlines, merging the fixed
instructions section with the volatile suffix into one unbroken paragraph.
**How to avoid:** Use bare `{{/* slot */}}` or trim-right-only `{{/* slot */ -}}`. See
verified whitespace mechanics above.

### Pitfall 3: Over-trimming the HOW TO EMIT block
**What goes wrong:** The HOW TO EMIT block's abstract-path instructions ARE the machine
contract. The path format, child CRD shape, field requirements, and "REQUIRED" emphasis are
all load-bearing (multiple production cascades fixed bugs caught here). Only remove text that
is LITERALLY redundant with another surviving line.
**How to avoid:** Annotate every line before trimming (PROMPT-03 commit first). The annotation
pass forces explicit "why this stays / why this can go" judgment before removal.

### Pitfall 4: Forgetting that task_executor has 6 TaskUID occurrences
**What goes wrong:** The task_executor has a filesystem layout block (lines 16-19) with 4
UID paths AND two more in the "Your job" bullets (lines 23-24) plus the metadata block
(line 11). Forgetting the "Your job" occurrences leaves the stable-prefix non-abstract.
**How to avoid:** Use the TaskUID occurrence map in this document. After reorder, do
`grep -n "TaskUID" task_executor.tmpl` to verify exactly 1 occurrence remains (the volatile
suffix).

### Pitfall 5: Confusing ratchet byte count with Anthropic token count
**What goes wrong:** Developer assumes the ratchet .txt number IS the token count and tries
to compare it to the 1,024-token cache floor.
**Why:** Ratchet uses `len(rendered)` (UTF-8 bytes). Anthropic token count is different (a
2-4 char word ≈ 1 token). Run `make eval` to get the actual token count for cache floor
comparison.

---

## Code Examples

### Correct volatile suffix shape (after D-01/D-02 reorder)

```
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}

TaskUID: {{.TaskUID}}
Your task dir: /workspace/envelopes/{{.TaskUID}}/  (children/  out.json  worktrees/...)

Original prompt:
{{.Prompt}}
```

Rendered bytes: exactly 1 blank line before TaskUID (from the `{{/* */}}` line's own `\n`),
exactly 1 blank line after the path mapping (from the explicit `\n` in source).

### D-06 annotation for a load-bearing line

```
{{/* WHY: "JSON files are the machine contract" — the admission webhook parses *.json
     in children/; Markdown writes are ignored by the controller. Removing this line
     loses the model's awareness of the two-artifact contract. */ -}}
Markdown is the human-review surface; the JSON files are the machine contract.
```

The trim-right marker (`-}}`) removes the `\n` after the comment so the comment does not
produce a blank line. The preceding `\n` (from the line above) is preserved.

### Goldie regeneration command

```bash
# Regenerate all five goldens after reorder commit
go test -update ./internal/eval/ -run "TestGoldenRender_"

# Verify goldens and ratchets pass
make test
```

### Ratchet update after trim

```bash
# Get new byte count
go test ./internal/eval/ -run TestByteRatchet_PlanPlanner -v 2>&1 | grep "byte count changed"
# Output: "rendered 3921 bytes, frozen ratchet 4281"

# Update
echo "3921" > internal/eval/testdata/ratchets/plan_planner.txt

# Confirm
go test ./internal/eval/ -run TestByteRatchet_PlanPlanner
```

---

## State of the Art

| Old Approach | Current Approach | Notes |
|--------------|------------------|-------|
| Metadata block in stable prefix (Level/Role/TaskUID/Provider.*) | TaskUID-only in volatile suffix; Level/Role/Provider lines dropped | D-02 locked; Phase 19 implements |
| Inline `{{.TaskUID}}` paths in HOW TO EMIT | Abstract instructions + concrete path mapping in volatile suffix | D-01 locked |
| No annotation mechanism | `{{/* WHY */ -}}` comments co-located with load-bearing lines | D-06 locked |
| No shared-context slot | Zero-token `{{/* SharedContext slot — Phase 20 */}}` marker | D-07 locked |

---

## Runtime State Inventory

Omitted — this is a template-edit phase (no rename, no rebrand, no migration). The templates
are compiled into the binary via `go:embed`. The only "runtime state" is the committed
ratchet/golden files, which are the phase's own artifacts.

---

## Open Questions

1. **DRY: shared template partial or self-contained per file?**
   - What we know: Option A (self-contained) requires no code change; Option B requires
     updating `LoadPromptTemplate` + a new `shared_preamble.tmpl`.
   - Recommendation: planner chooses Option A for Phase 19 (pure template edit scope).

2. **Exact wording of compressed paradigm preamble (Claude's Discretion)**
   - What we know: Must preserve "Read the project spec at `/workspace/repo.git/README.md`
     first — it's load-bearing" as its load-bearing core.
   - What's unclear: Whether the TIDE acronym expansion sentence should be condensed to one
     line or dropped (it appears in every template and adds ~100 bytes).
   - Recommendation: Keep the spec-read directive verbatim; compress the TIDE acronym line to
     "TIDE is a Kubernetes-native hierarchical agentic work orchestrator; read the spec at
     /workspace/repo.git/README.md before authoring — it is load-bearing." This preserves the
     directive while removing the "Milestone → Phase → Plan → Task → Wave DAG, dispatched as
     Kubernetes subagent Jobs" explanation (redundant with the spec itself).

---

## Environment Availability

| Dependency | Required By | Available | Fallback |
|------------|------------|-----------|----------|
| `go test` | Unit gate (`make test`) | Yes (Go in PATH) | n/a |
| `make` | Test + eval targets | Yes | run `go test` directly |
| credproxy | `make eval` (online token count) | Manual setup needed | Skip `make eval` for offline-only work; run when credproxy available |

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go testing + goldie v2.8.0 |
| Config file | None — uses `go test` directly |
| Quick run command | `go test ./internal/eval/` |
| Full suite command | `make test` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command |
|--------|----------|-----------|-------------------|
| PROMPT-01 | Five templates in D-03 order | golden render diff | `go test ./internal/eval/ -run TestGoldenRender_` (golden shows structure) |
| PROMPT-02 | TaskUID/Provider.* in volatile suffix only | golden render check | `go test ./internal/eval/ -run TestGoldenRender_` + `grep -c TaskUID *.tmpl` |
| PROMPT-03 | Annotation comments present | human review of annotated diff | Code review; no automated check |
| PROMPT-04 | Trim preserves protocol compliance | protocol gate | `go test ./internal/eval/ -run TestDAGAcyclicity|TestChildCRD|TestDeclaredOutputPath` |
| PROMPT-05 | No map-typed data in stable prefix | confirmed no-op | `grep -rn "Params" templates/*.tmpl` returns zero hits |

### Sampling Rate

- **Per task commit:** `go test ./internal/eval/` (fast, zero-network, ~1s)
- **Per wave merge:** `make test` (includes fmt/vet/envtest prep)
- **Phase gate:** `make test` green on final commit

### Wave 0 Gaps

None — existing test infrastructure (goldie goldens + ratchets + protocol tests in `internal/eval/`) covers all phase requirements. No new test files needed.

---

## Security Domain

Phase 19 edits only `.tmpl` files and `testdata/` files. No authentication, session management,
access control, input validation paths, or cryptography is involved. ASVS does not apply.

---

## Sources

### Primary (HIGH confidence)
- `internal/eval/render_test.go` — goldie golden assertion pattern, ratchet assertion
  implementation, fixture fields, goldie fixture directory
- `internal/eval/doc.go` — offline ratchet vs online `make eval` boundary
- `internal/subagent/common/templates/*.tmpl` — actual current template content (all five read)
- `internal/eval/testdata/ratchets/*.txt` — current byte counts (all five read)
- `internal/eval/testdata/goldie/*.golden` — current rendered content (milestone + plan read)
- `pkg/dispatch/envelope.go` + `pkg/dispatch/provider.go` — `EnvelopeIn` fields, `ProviderSpec`
  struct with `Params map[string]string`
- `internal/subagent/common/prompt_templates.go` — `LoadPromptTemplate`, single-file `ParseFS`
- `Makefile:85,89,205-217` — `make test`, `make test-only`, `make eval` targets
- `/Users/justinsearles/go/pkg/mod/github.com/sebdah/goldie/v2@v2.8.0/goldie.go` — `-update`
  flag definition (`flag.Bool("update", ...)`)
- `/tmp/tmpltest*.go` (live Go programs) — verified all whitespace/comment mechanics

### Secondary (MEDIUM confidence)
- `https://pkg.go.dev/text/template#hdr-Text_and_spaces` — official Go docs on trim markers
  and comment behavior

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Conservative wording compression saves 15-25% of bytes on the paradigm paragraph (≈50-150 bytes per template) | Summary | Actual savings may be less; ratchet confirms after trimming |
| A2 | Cross-template DRY (Option B) would require updating `LoadPromptTemplate` to pass two files to `ParseFS` | Cross-template DRY section | If Go handles it differently, the code change scope differs |

All other claims in this research were verified by direct source read or live Go program
execution.

---

## Metadata

**Confidence breakdown:**
- Go text/template whitespace mechanics: HIGH — verified by running live Go programs
- Current template structure + TaskUID map: HIGH — verified by reading all five .tmpl files
- Eval harness mechanics: HIGH — verified by reading render_test.go, goldie source, Makefile
- PROMPT-05 no-op status: HIGH — verified by grep of all .tmpl files
- Byte counts: HIGH — read directly from testdata/ratchets/*.txt
- Trim quantum estimate (A1): LOW — actual savings depend on content decisions

**Research date:** 2026-06-15
**Valid until:** Until any of the five .tmpl files or `internal/eval/` is modified
