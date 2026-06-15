# Phase 19: Template Reorder + Token Minimization - Context

**Gathered:** 2026-06-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Restructure all five compiled-in prompt templates
(`internal/subagent/common/templates/{milestone,project,phase,plan}_planner.tmpl`
+ `task_executor.tmpl`) into a **stable-prefix-first** order and **trim
non-essential boilerplate** — each change gated green by the Phase 18 eval
harness (`make test` deterministic protocol-compliance gate + per-template
token ratchet + goldie golden renders).

This phase clarifies HOW to reorder and trim. WHAT it must achieve is locked by
PROMPT-01–05. New capabilities — the `SharedContext` field/population (Phase
20), cross-pod cache verification (Phase 20), and dashboard observability
(Phase 21) — are out of scope here.

**Load-bearing reality (from Phase 18 research + live EVAL-05):** today's
templates are ~200 tokens, well below the 1,024-token cache floor, so
prefix-caching **never fires regardless of ordering**. The reorder's *cache*
benefit is therefore **forward-looking** — it sets up Phase 20's SharedContext
to grow the shared prefix past the floor. Phase 19's **immediate** win is token
minimization + a clean, cache-ready structure. Do not justify Phase 19 by
near-term cache hits; justify it by token reduction and Phase-20 readiness.

</domain>

<decisions>
## Implementation Decisions

### Reorder structure (PROMPT-01 / PROMPT-02)
- **D-01:** **De-volatilize inline `{{.TaskUID}}` by defining paths once in the
  volatile suffix.** Today `{{.TaskUID}}` is not only in the metadata header —
  it appears inline in the emit-path instructions of every planner and
  throughout `task_executor`'s filesystem-layout block. Keep the instruction
  body **byte-identical across wave siblings** by writing it UID-free and
  abstract ("write one JSON file per child into your children/ directory") and
  putting the single concrete path mapping in the volatile suffix
  (`Your task dir: /workspace/envelopes/<uid>/ → children/ out.json
  worktrees/...`). **Pure template edit — no in-pod harness change.** (Chose
  this over a CWD-relative convention specifically to avoid harness scope.)
- **D-02:** **Volatile suffix carries TaskUID only** (plus the task-dir path
  mapping derived from it, plus `{{.Prompt}}`). **Drop the printed
  `Level`/`Role` lines** (redundant with the role preamble — each template is
  already level/role-specialized) **and the printed `Provider.Vendor`/`Model`
  lines** (not load-bearing: nothing in the instructions depends on them, the
  model inherently knows its own identity, and provenance lives in the
  envelope `Usage` + telemetry/events.jsonl).
  - **Critical:** dropping the *printed* Provider lines does **not** remove
    `.Provider` from the `EnvelopeIn`/template data struct. It stays available
    for future provider-conditional template logic (`{{if eq .Provider.Vendor
    "openai"}}`) — that's an OpenAI-backend-milestone concern, and it loses
    nothing by the printed line going away.
- **D-03:** **Canonical section order applied uniformly to all five templates:**
  role preamble → fixed instructions → *(shared-context slot, reserved — see
  D-07)* → volatile suffix (TaskUID + task-dir mapping) → `{{.Prompt}}`.

### Trim posture (PROMPT-04)
- **D-04:** **Conservative trim.** The eval gate is **protocol-compliance only**
  (child-CRD parse / declared-output-path / DAG acyclicity) — per Phase 18 D-05
  there is **NO LLM-judge this milestone**, so the gate catches *structural*
  breakage but will **NOT** catch a plan that simply got *worse* from
  over-trimmed instructions. Given run #2 quality is the milestone's whole
  point and PITFALLS flags over-trimming as catastrophic, trim **only pure
  redundancy and formatting slack**: compress the descriptive paradigm prose
  down to its load-bearing "read the spec at `/workspace/repo.git/README.md` —
  it's load-bearing" directive, dedupe the repeated "Markdown is the
  human-review surface; the JSON files are the machine contract" line, tighten
  whitespace. **Every instruction keeps its full semantic content.** No
  aggressive imperative-rewrite.
- **D-05:** **Confidence check beyond the structural gate** = per-section commit
  (PROMPT-04 already mandates one-section-per-commit, each gated green) +
  **human review of the annotated diff** + **`make eval` token confirmation**
  (count dropped, ratchet didn't accidentally grow). **No live model A/B**;
  semantic judging stays deferred (EVAL-F1).

### Annotation mechanism (PROMPT-03)
- **D-06:** **Inline `{{- /* … */ -}}` template comments for surviving
  load-bearing lines** — zero-token (Go strips template comments at render),
  co-located with the line they justify, the durable "why this stays" record.
  **Use the `{{- -}}` trim markers** so the comment leaves no extra newline and
  goldens stay byte-identical (a bare `{{/* */}}` on its own line WOULD add a
  blank line to rendered output — must be trimmed). The **"why it was safe to
  remove X"** record goes in the **per-section commit message** (which
  PROMPT-04 requires anyway). Annotation and code never drift. (Chose inline
  over a sidecar ANNOTATIONS.md to avoid a separate artifact that drifts.)

### Shared-context slot timing (PROMPT-01)
- **D-07:** **Reserve the shared-context slot now as a zero-token marker.** Place
  `{{- /* SharedContext slot — populated in Phase 20 (CACHE-02/03) */ -}}`
  between the fixed instructions and the volatile suffix. Renders to nothing
  (no dead text, goldens unaffected), makes Phase 19 satisfy PROMPT-01's stated
  order **literally**, documents the canonical structure, and gives Phase 20 a
  clean, unambiguous insertion point. Consistent with D-06.

### Claude's Discretion
- Exact compressed wording of the paradigm preamble (keep the load-bearing
  "read the spec, it's load-bearing" directive intact).
- Whether the repeated boilerplate is de-duplicated via a shared Go template
  partial/`{{define}}`/`{{template}}` include vs kept self-contained per file —
  maintainability call for the planner. **Note:** cross-template DRY does NOT
  affect caching (cache is per-wave-sibling at the same level; different levels
  render different prompts), so decide it on maintainability grounds only.
- Offline ratchet proxy unit is already fixed by Phase 18 (D-01a) — Phase 19
  just ratchets the committed numbers **down** to the trimmed result.
- The precise rendered form of the volatile-suffix path mapping (labels,
  layout) — keep it deterministic so goldens don't flap.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (locked scope)
- `.planning/REQUIREMENTS.md` — PROMPT-01–05 (this phase) + binding constraints
  (CLI-based `claude -p --bare`, NO direct-SDK `cache_control`, don't rebuild
  cost accounting). CACHE-01–05 (Phase 20) and OBSV-01–03 (Phase 21) are NOT
  this phase.
- `.planning/ROADMAP.md` §"Phase 19: Template Reorder + Token Minimization" —
  5 success criteria. Note the `make test-unit` wording → read as `make test`
  per Phase 18 D-02a.

### Phase 18 dependency (the gate this phase runs against)
- `.planning/phases/18-eval-harness/18-CONTEXT.md` — D-01 (per-template
  no-growth ratchet; Phase 19 ratchets DOWN), D-02/D-02a (deterministic gate
  runs in `make test`; no `make test-unit` target), D-05 (deterministic-only,
  **no LLM-judge** this milestone), D-03 (`make eval` count_tokens tool).
- `internal/eval/` — the deterministic gate + goldie golden renders +
  per-template token ratchet. Every trim commit must keep this green.
- `make eval` (`//go:build eval`, via credproxy `count_tokens`) — the live
  token-count confirmation tool used in D-05.
- `testdata/baselines/<template>.golden` — regenerate with goldie `-update`
  after the reorder; the diff is the PR review artifact.

### Research (HIGH confidence)
- `.planning/research/SUMMARY.md` — templates ~200 tokens < 1,024 cache floor →
  caching never fires today; reorder is forward-looking for Phase 20.
- `.planning/research/PITFALLS.md` — over-trimming load-bearing instructions is
  catastrophic (anchors the conservative posture, D-04).

### Code (ground truth — the files this phase edits / reads)
- `internal/subagent/common/templates/milestone_planner.tmpl`,
  `project_planner.tmpl`, `phase_planner.tmpl`, `plan_planner.tmpl`,
  `task_executor.tmpl` — **the five templates to reorder + trim.** Current
  shared skeleton: role line → verbatim TIDE-paradigm paragraph → "Dispatch
  metadata" block (`Level/Role/TaskUID/Provider.*`) → "Your job" → "HOW TO
  EMIT" (with inline `{{.TaskUID}}` paths) → `Original prompt: {{.Prompt}}`.
- `internal/subagent/common/prompt_templates.go` — renders via
  `tmpl.Execute(&buf, in)`. Read to confirm trim-marker/whitespace behavior.
- `pkg/dispatch/envelope.go:39` — `EnvelopeIn` (`TaskUID`, `Role`, `Level`,
  `Prompt`, `Provider.*`). `.Provider` STAYS on the struct (D-02); only the
  printed lines go. Phase 20 adds `SharedContext` here — out of scope for 19.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- Phase 18's eval package (`internal/eval/`), goldie golden renders, and the
  per-template token ratchet — the complete gate Phase 19 trims against. No new
  harness needed; Phase 19 only edits templates + ratchet numbers + goldens.
- `make eval` (Phase 18) — the live `count_tokens` confirmation tool for D-05.

### Established Patterns
- All five templates share an identical skeleton (role → paradigm → metadata →
  job → emit → prompt), so the reorder applies one uniform transform across all
  five. `task_executor` differs in detail (filesystem-layout block, `{{.Prompt}}`
  sourced from `PromptPath` in-pod) but takes the same prefix/suffix treatment.
- Go template comments `{{- /* */ -}}` are the zero-token annotation primitive
  (D-06) and the slot-reservation primitive (D-07).
- Per-section-commit + gated-green discipline (PROMPT-04) mirrors Phase 18's
  goldie/ratchet "deliberate reviewed commit to change a baseline number" rule.

### Integration Points
- No production hot-path changes beyond the template files: `stream_parser.go`,
  `pricing.go`, `metrics/registry.go`, and the executor dispatch path are
  read-only for this phase (per Phase 18 ARCHITECTURE.md).
- `.Provider` remains a live field on `EnvelopeIn` for future
  provider-conditional template logic (OpenAI-backend milestone), even though
  its printed lines are dropped.

</code_context>

<specifics>
## Specific Ideas

- Target volatile-suffix shape (illustrative, from discussion):
  ```
  ...fixed instructions...
  Write one JSON file per Phase into your children/ directory (path below).
  {{- /* SharedContext slot — populated in Phase 20 (CACHE-02/03) */ -}}

  TaskUID: {{.TaskUID}}
  Your task dir: /workspace/envelopes/{{.TaskUID}}/  (children/  out.json  worktrees/...)
  Original prompt:
  {{.Prompt}}
  ```
- The descriptive paradigm sentence compresses to its load-bearing core: "Read
  the project spec at `/workspace/repo.git/README.md` first — it's load-bearing
  and describes the five-level paradigm and wave-derivation rules."

</specifics>

<deferred>
## Deferred Ideas

- **`SharedContext` field on `EnvelopeIn` + identical-per-wave population** —
  Phase 20 (CACHE-02/03). Phase 19 only reserves the zero-token slot (D-07).
- **Cross-pod prefix-cache verification spike under `claude -p --bare`** —
  Phase 20 (CACHE-01); gates whether SharedContext is for cache benefit or
  token-minimization-only.
- **Per-level token accounting + cache-hit dashboard panel** — Phase 21
  (OBSV-01–03).
- **LLM-as-judge / semantic-quality scoring** — EVAL-F1, deferred to a later
  milestone. Would let an *aggressive* trim posture be safely revisited; until
  then the structural-only gate keeps Phase 19 conservative (D-04).
- **Cross-template DRY via shared template partials** — left to planner
  discretion (maintainability only; no caching impact).

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 19-Template Reorder + Token Minimization*
*Context gathered: 2026-06-15*
