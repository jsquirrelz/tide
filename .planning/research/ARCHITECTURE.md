# Architecture Research

**Domain:** Token & cost optimization integration into an existing K8s-native LLM orchestrator
**Researched:** 2026-06-15
**Confidence:** HIGH (all findings from direct inspection of the production codebase at commit 58d0ae6)

---

## System Overview — Current State (v1.0.1)

The dispatch pipeline has four layers. Reading the actual files yields this exact boundary map:

```
┌─────────────────────────────────────────────────────────────────────┐
│  CONTROLLER LAYER  (internal/controller/)                            │
│                                                                      │
│  ┌──────────────────────────┐  ┌──────────────────────────────────┐ │
│  │ dispatch_helpers.go      │  │ task_controller.go               │ │
│  │ BuildPlannerEnvelope()   │  │ buildEnvelopeIn()                │ │
│  │  → ResolveProvider()     │  │  → ResolveProvider()             │ │
│  │  → json.Marshal(envIn)   │  │  → json.Marshal(envIn)           │ │
│  └──────────┬───────────────┘  └────────────┬─────────────────────┘ │
│             │ planner path                   │ executor path          │
│             │ (project/milestone/phase/plan) │ (task level)          │
└─────────────┼───────────────────────────────┼──────────────────────┘
              │ EnvelopeIn.Prompt (set here)   │ EnvelopeIn.PromptPath
              │ EnvelopeIn.Provider            │ (read in-pod)
              ▼                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│  ENVELOPE CONTRACT  (pkg/dispatch/)                                  │
│                                                                      │
│  EnvelopeIn {                                                        │
│    Prompt      string   // full rendered prompt (planner path)       │
│    PromptPath  string   // PVC-relative path (executor path)         │
│    Provider    ProviderSpec {Vendor, Model, Params}                  │
│    Caps        Caps     // wall-clock, iterations, tokens            │
│    ...                                                               │
│  }                                                                   │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ written to PVC as in.json
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│  ANTHROPIC SUBAGENT  (internal/subagent/anthropic/)                  │
│                                                                      │
│  subagent.go: Run(ctx, in EnvelopeIn) → EnvelopeOut                 │
│    1. Vendor fail-fast                                               │
│    2. Params allow-list check                                        │
│    3. In-pod prompt read (readPromptArtifact if PromptPath set)      │
│    4. common.LoadPromptTemplate(in.Role, in.Level) → *tmpl          │
│    5. tmpl.Execute(&buf, in) → renderedPrompt                        │
│    6. exec claude -p --model <M> --output-format stream-json --bare  │
│       stdin=renderedPrompt                                           │
│    7. ParseStream(stdout, events.jsonl)                              │
│    8. estimatedCostCents(model, usage) → EnvelopeOut                │
└─────────────────────────────────────────────────────────────────────┘
                               │ Usage{InputTokens, OutputTokens,
                               │       CacheReadTokens, CacheCreationTokens}
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│  BUDGET + METRICS  (internal/budget/ + internal/metrics/)            │
│                                                                      │
│  budget.RollUpUsage() → Project.Status.Budget (K8s Status patch)    │
│  ReservationStore.Reserve/Settle/Release (in-process, rederivable)  │
│  metrics.TokensInputTotal / CacheReadTotal / CacheCreationTotal      │
│          (CounterVec labels: project, phase, plan, wave)             │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Component Responsibilities (Existing)

| Component | File(s) | Responsibility |
|-----------|---------|----------------|
| BuildPlannerEnvelope | `internal/controller/dispatch_helpers.go` | Builds planner EnvelopeIn; stamps Role="planner", Level, Prompt (outcome prompt verbatim), Provider via ResolveProvider; marshals to JSON |
| buildEnvelopeIn | `internal/controller/task_controller.go:1325` | Builds executor EnvelopeIn; stamps Role="executor", PromptPath (PVC-relative path to child CRD), Branch, FilesTouched, Provider via ResolveProvider |
| LoadPromptTemplate | `internal/subagent/common/prompt_templates.go` | Loads embedded `*.tmpl` by `(role, level)` key; returns `*template.Template`; called fresh per dispatch inside the subagent pod |
| *.tmpl files | `internal/subagent/common/templates/` | Five templates (project_planner, milestone_planner, phase_planner, plan_planner, task_executor); each has a STABLE BOILERPLATE PREAMBLE block (role intro, TIDE description, dispatch metadata table, instructions) followed by a VOLATILE SUFFIX `{{.Prompt}}` |
| Anthropic.Run | `internal/subagent/anthropic/subagent.go` | Executes the full dispatch cycle in-pod: template render → CLI invocation → stream parse → cost computation → EnvelopeOut |
| ParseStream | `internal/subagent/anthropic/stream_parser.go` | Reads stream-json JSONL, extracts `result` event fields: `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens` |
| pricing.go | `internal/subagent/anthropic/pricing.go` | Per-model price table (6 models); `estimatedCostCents()` multiplies token counts by per-model rate including separate cache-read and cache-write rates |
| budget.RollUpUsage | `internal/budget/tally.go` | Optimistic-lock K8s Status patch; accumulates InputTokens+OutputTokens → TokensSpent, EstimatedCostCents → CostSpentCents |
| ReservationStore | `internal/budget/reservation.go` | In-process sync.Map; pre-charges per-dispatch estimate; rederivable from Job labels on restart |
| metrics registry | `internal/metrics/registry.go` | Prometheus CounterVec: `tide_tokens_{input,output,cache_read,cache_creation}_total` and `tide_cost_cents_total`, all with labels `{project, phase, plan, wave}` |

---

## Where Stable-Prefix-First Context Assembly Slots In

The critical finding from reading the actual templates: **prompt structuring belongs in the `.tmpl` files, not at the envelope-build sites**.

Here is why. The dispatch pipeline assembles the final prompt in two sequential steps:

1. **Envelope-build site** (controller): sets `EnvelopeIn.Prompt` (planner path) or `EnvelopeIn.PromptPath` (executor path). The controller writes the volatile per-dispatch content here — the outcome prompt, or the path to the per-task instruction artifact.

2. **Template render** (in-pod, `Anthropic.Run` step 5): calls `tmpl.Execute(&buf, in)` with `in` as the data context. The template author controls exactly what appears before `{{.Prompt}}` and what appears after it.

The boilerplate that is stable across all dispatches of the same `(role, level)` pair is already concentrated in the `.tmpl` files. The volatile suffix is already isolated to `{{.Prompt}}` at the bottom of each template. This is exactly the structure that server-side prefix caching exploits: if the stable portion of the rendered prompt is identical byte-for-byte across wave-sibling dispatches, the provider's prefix cache reuses it without re-billing the full input at the standard rate.

**Consequence for integration**: cache-aware prompt structuring is a pure template-layer change — reorder content within each `.tmpl` to maximize stable prefix length before `{{.Prompt}}`. The envelope-build sites (`BuildPlannerEnvelope`, `buildEnvelopeIn`) do not need to change for basic prefix ordering.

However, there is one exception: **shared wave/plan context hoisting**. When sibling dispatches in the same wave all receive the same parent artifact context (e.g., the same `MILESTONE.md` content injected as part of the planner prompt for phase-level tasks), that context currently lives in `EnvelopeIn.Prompt` (the volatile suffix). To hoist it into the stable prefix, the template would need to read it from a well-known PVC path rather than receiving it inline — or the envelope-build site would need to split context into a `SharedContext string` field alongside the volatile `Prompt string`. This is the main architectural decision for the token-minimization work.

---

## Recommended Architecture for v1.0.2

### Component 1: Template Restructuring (modified files)

**Files modified:** `internal/subagent/common/templates/*.tmpl` (all five)

**What changes:** Within each template, hoist everything that is stable across all dispatches of that `(role, level)` pair as high as possible in the document, before the `{{.Prompt}}` interpolation. The dispatch metadata block (Level, Role, TaskUID, Provider.Vendor, Provider.Model) is already near the top in all five templates and is semi-stable (TaskUID varies per dispatch). TaskUID must move after the stable prefix — it is unique per call and would break the shared prefix cache bucket.

**Revised template structure per role/level:**

```
[1] ROLE/SYSTEM PREAMBLE           — fully stable: role intro, TIDE description,
                                     paradigm overview, protocol rules (read-only)
[2] FIXED CONTEXT INSTRUCTIONS     — fully stable: filesystem layout, output format
                                     requirements, child-CRD JSON schema examples
[3] VOLATILE DISPATCH METADATA     — varies: TaskUID, Branch (executor only)
[4] {{.Prompt}}                    — fully volatile: per-dispatch outcome/task text
```

Moving TaskUID and Branch from position [2] to position [3] (below the large stable block) is the key change. Sections [1] and [2] can then be identical bytes for every dispatch of that role+level, which is the unit the provider prefix-caches.

**Data flow impact:** None — `tmpl.Execute(&buf, in)` signature is unchanged. The `.Prompt` field on `EnvelopeIn` is still rendered at the same place; only the ordering within the template changes.

### Component 2: Shared Prefix Context Field (new field on EnvelopeIn, modified build sites)

**Files modified:**
- `pkg/dispatch/envelope.go` — add `SharedContext string` field to `EnvelopeIn`
- `internal/controller/dispatch_helpers.go` (`BuildPlannerEnvelope`) — populate `SharedContext` with the level-appropriate parent artifact summary
- `internal/subagent/common/templates/*.tmpl` (milestone, phase, plan planners) — reference `{{.SharedContext}}` between the stable preamble and the volatile `{{.Prompt}}`

**Rationale:** All phase planners for the same milestone wave receive the same `MILESTONE.md` content. All plan planners for the same phase wave receive the same phase brief. This shared content currently rides inside the `Prompt` string (inline) or is re-read from disk in-pod per dispatch. Promoting it to a named field lets:
- The template place it precisely after the fully-stable preamble, maximizing the shared prefix length.
- The envelope-build site supply it from a single PVC read rather than embedding it redundantly per dispatch.

**Complexity note:** This is additive — `SharedContext omitempty` in JSON means executor dispatches that don't use it add zero bytes. The executor template ignores the field. Only planner templates for levels milestone, phase, and plan reference it.

### Component 3: Token Minimization Pass (modified files)

**Files modified:** `internal/subagent/common/templates/*.tmpl` (all five)

**What changes:** Audit and trim each template for token-heavy content that does not affect output quality:
- The TIDE paradigm overview appears in all four planner templates (project, milestone, phase, plan). It can be condensed to 3-4 sentences rather than 8-10.
- The child-CRD JSON schema examples in the planner templates are long. A single compact example per kind is sufficient; the current templates include redundant field-by-field annotations that the model already handles from the field names.
- The task_executor template instructs the model to write its result to `out.json` — this is harness-owned and the model cannot usefully act on it; it adds tokens with no signal.

**Measurement gate:** The eval harness (Component 4) must run against a baseline snapshot before any template changes commit, so each edit can be compared to baseline token counts.

### Component 4: Eval Harness (new package)

**New files:** `internal/eval/` package

**What it measures:**

1. **Rendered prompt size** — run `LoadPromptTemplate` + `tmpl.Execute` with realistic fixture `EnvelopeIn` values; assert `len(renderedPrompt)` ratchets down vs baseline (a `testdata/baselines/` golden file).
2. **Cache-hit prefix length** — given two sibling EnvelopeIn values with different `TaskUID` and `Prompt` but the same `(Role, Level)` and `SharedContext`, measure the byte position of the first byte that differs. Assert this length is >= a declared minimum (the stable prefix floor).
3. **Offline cost replay** — read a recorded `events.jsonl` (captured from a real run and committed under `internal/eval/testdata/`), feed through `ParseStream` + `estimatedCostCents`, compute expected total cost with and without cache-read tokens, assert the result matches a golden value. This verifies that the cost accounting correctly credits cache reads at 0.10x rather than full input rate.
4. **Quality regression gate** — a table-driven test of `(template, input_fixture) → expected_output_sections` checks that required structural elements (child-CRD JSON schema mention, `{{.Prompt}}` interpolation present, required path mentions) survive after template edits. This is not a full LLM eval; it is a static structural gate on the rendered output.

**Where it runs:**

The harness is a standard Go test package. It does not invoke the `claude` CLI, does not require a cluster, and does not make any network calls. It runs in `make test` (the unit-test suite) alongside existing tests. This is the right fit for a template-rendered output check: no external dependencies, deterministic, fast.

The harness does NOT attempt to measure actual server-side cache-hit rates — that would require a live provider call and would be flaky. Instead it measures the structural conditions that make cache hits possible (stable prefix length, byte identity of the stable region). Actual cache-hit rate is an observed metric from production runs via `tide_tokens_cache_read_total`.

**Package structure:**

```
internal/eval/
├── doc.go                           # package overview
├── prompt_size_test.go              # Component 4 test 1: rendered prompt size baselines
├── cache_prefix_test.go             # Component 4 test 2: stable prefix length assertions
├── cost_replay_test.go              # Component 4 test 3: offline cost replay
├── quality_gate_test.go             # Component 4 test 4: structural content assertions
└── testdata/
    ├── baselines/
    │   ├── project_planner.txt      # golden rendered size (bytes) for project planner
    │   ├── milestone_planner.txt    # golden for milestone planner
    │   ├── phase_planner.txt
    │   ├── plan_planner.txt
    │   └── task_executor.txt
    └── fixtures/
        ├── stream_opus.jsonl        # recorded stream-json output from a real run
        └── stream_haiku.jsonl
```

**Dependency graph:** `internal/eval` imports `internal/subagent/common` (for `LoadPromptTemplate`) and `internal/subagent/anthropic` (for `ParseStream` and `estimatedCostCents`). It does NOT import `internal/controller` or any CRD types beyond `pkg/dispatch` envelope types. This keeps the eval package importable without pulling in the full controller dependency graph.

**Gating:** The baselines use a ratchet pattern — the test asserts `renderedSize <= baseline` rather than `renderedSize == baseline`. This means template edits that reduce token count always pass; edits that increase it fail and require an explicit baseline bump. Baseline files are committed and tracked in git so diffs are human-reviewable.

### Component 5: Cache Hit Observability on Dashboard (modified files)

**Files modified:** Dashboard frontend (React/SSE)

No new metrics required — the existing counters already capture all four token dimensions. The existing `tide_tokens_cache_read_total` and `tide_tokens_cache_creation_total` metrics are already emitted with labels `{project, phase, plan, wave}`.

**What changes:** Add a "cache efficiency" panel to the dashboard showing:
- `cache_read / (input + cache_creation + cache_read)` as a hit ratio
- `cache_creation` tokens (the "warmup tax" on the first dispatch of a new prefix)
- Cost savings estimate = `cache_read_tokens * (standard_input_rate - cache_read_rate)` per project

The data is already flowing through the pipeline. This is a frontend display change only.

---

## Data Flow — After v1.0.2

```
BuildPlannerEnvelope (dispatch_helpers.go)
    ├── sets EnvelopeIn.Prompt       = outcomePrompt (volatile)
    └── sets EnvelopeIn.SharedContext = parentArtifactSummary (wave-shared, NEW)
          │
          ▼
buildEnvelopeIn (task_controller.go)
    ├── sets EnvelopeIn.PromptPath  = PVC path to per-task child spec (volatile)
    └── (SharedContext unused for executor dispatches)
          │
          ▼
PVC: in.json written → subagent pod mounts and reads
          │
          ▼
Anthropic.Run (subagent.go)
    ├── readPromptArtifact(PromptPath) → in.Prompt   [executor only]
    ├── LoadPromptTemplate(role, level)
    └── tmpl.Execute(&buf, in)
          │
          Rendered prompt structure (AFTER v1.0.2):
          [STABLE PREAMBLE — role intro, TIDE paradigm, output schema, examples]
          [{{.SharedContext}} — parent artifact summary, same for all wave siblings]
          [VOLATILE — TaskUID, Branch, {{.Prompt}} (per-task instruction)]
          │
          ▼
exec claude -p ... stdin=renderedPrompt
          │
          ▼
ParseStream → Usage{InputTokens, OutputTokens, CacheReadTokens, CacheCreationTokens}
          │
          ▼
estimatedCostCents (credits CacheReadTokens at 0.10x input rate)
          │
          ▼
EnvelopeOut → TerminationStub → RollUpUsage → Project.Status.Budget
          │
          ▼
metrics.TokensCacheReadTotal.WithLabelValues(...).Add(usage.CacheReadTokens)
metrics.TokensCacheCreationTotal.WithLabelValues(...).Add(usage.CacheCreationTokens)
```

---

## New vs Modified Files

### New Files

| File | Component | What it is |
|------|-----------|------------|
| `internal/eval/doc.go` | Eval harness | Package doc |
| `internal/eval/prompt_size_test.go` | Eval harness | Rendered prompt size ratchet tests |
| `internal/eval/cache_prefix_test.go` | Eval harness | Stable prefix length assertions |
| `internal/eval/cost_replay_test.go` | Eval harness | Offline cost replay from recorded events.jsonl |
| `internal/eval/quality_gate_test.go` | Eval harness | Structural content gate for templates |
| `internal/eval/testdata/baselines/*.txt` | Eval harness | Golden baseline prompt sizes (one per template) |
| `internal/eval/testdata/fixtures/*.jsonl` | Eval harness | Recorded stream-json output from real runs |

### Modified Files

| File | Component | What changes |
|------|-----------|-------------|
| `pkg/dispatch/envelope.go` | SharedContext field | Add `SharedContext string \`json:"sharedContext,omitempty"\`` to `EnvelopeIn` |
| `internal/controller/dispatch_helpers.go` | BuildPlannerEnvelope | Populate `SharedContext` from parent artifact summary at planner level |
| `internal/subagent/common/templates/project_planner.tmpl` | Template restructuring + token minimization | Reorder: stable preamble first, TaskUID/volatile last; trim verbosity |
| `internal/subagent/common/templates/milestone_planner.tmpl` | Template restructuring + token minimization | Same restructuring; add `{{.SharedContext}}` slot |
| `internal/subagent/common/templates/phase_planner.tmpl` | Template restructuring + token minimization | Same restructuring; add `{{.SharedContext}}` slot |
| `internal/subagent/common/templates/plan_planner.tmpl` | Template restructuring + token minimization | Same restructuring; add `{{.SharedContext}}` slot |
| `internal/subagent/common/templates/task_executor.tmpl` | Token minimization | Trim non-actionable instructions; TaskUID stays in volatile section |
| Dashboard frontend | Cache observability | Add cache-hit-ratio panel consuming existing Prometheus counters |

**Files that do NOT change:**
- `internal/subagent/anthropic/subagent.go` — the `tmpl.Execute(&buf, in)` call handles the new `SharedContext` field automatically via the Go template data context; the template is the authority
- `internal/subagent/anthropic/stream_parser.go` — already captures all four token dimensions; no change needed
- `internal/subagent/anthropic/pricing.go` — already prices cache reads at 0.10x and cache writes at 1.25x; no change needed
- `internal/budget/tally.go` — `RollUpUsage` does not need to track cache tokens separately at the budget layer (that is a metrics concern); no change
- `internal/metrics/registry.go` — all required counters already exist; no new metrics needed
- `pkg/dispatch/provider.go` — `ProviderSpec` is unchanged; caching is a prompt-structure concern, not a provider-param concern
- `internal/controller/task_controller.go` (buildEnvelopeIn) — executor dispatches do not use `SharedContext`; no change needed for the executor path

---

## Dependency-Ordered Build Sequence

The order below respects import dependencies and the eval harness gating constraint (baselines must be captured from the pre-change codebase before any template edits commit).

**Step 1 — Capture baselines (no code changes)**

Run the template renderer against all five templates with representative fixture inputs, record the rendered prompt sizes in `internal/eval/testdata/baselines/`. This is a prerequisite: without a baseline, the ratchet tests have nothing to compare against. Commit the baseline files as the first commit of the milestone.

**Step 2 — Eval harness skeleton (new package, no deps on Steps 3-5)**

Create `internal/eval/` with the four test files and testdata. Tests pass against the unchanged templates (sizes <= baseline because they equal baseline). This gives the gating harness before any template changes go in, so every subsequent step is regression-tested.

**Step 3 — Template restructuring (modified .tmpl files, no schema changes)**

Reorder content in all five templates to place the stable preamble first and the volatile suffix (TaskUID, Branch, `{{.Prompt}}`) last. No API changes; no envelope-build site changes. The eval harness catches any regressions. Prompt sizes should stay approximately equal or slightly increase (restructuring alone does not reduce tokens; it improves cache-hit probability).

**Step 4 — Token minimization (modified .tmpl files, no schema changes)**

Trim verbosity from all five templates. The eval harness ratchet will confirm sizes drop. Quality gate tests confirm required structural elements survive. This step is iterative — each trim is a separate commit so the delta is reviewable.

**Step 5 — SharedContext field and planner wiring (schema + build-site change)**

Add `SharedContext string` to `EnvelopeIn`, update `BuildPlannerEnvelope` to populate it, and update the milestone/phase/plan planner templates to reference `{{.SharedContext}}`. Update the eval harness fixtures to include `SharedContext` in the representative inputs and update baselines to reflect the new template structure. This step has the widest blast radius (touches `pkg/dispatch/envelope.go`, `dispatch_helpers.go`, three templates, and the eval fixtures) and therefore comes last, after the simpler changes are proven by the harness.

**Step 6 — Dashboard cache observability (frontend only)**

Add the cache-hit-ratio panel to the dashboard. This is entirely independent of Steps 1-5 and can be developed in parallel once the metric names are confirmed (they are already confirmed: `tide_tokens_cache_read_total`, `tide_tokens_cache_creation_total`). No backend changes.

---

## Architectural Patterns

### Pattern 1: Ratchet Test for Template Token Counts

**What:** Store the golden rendered-prompt byte count in a committed text file. The test asserts `actual <= golden`. Any increase requires a deliberate baseline bump (reviewed in the PR diff). Any decrease passes automatically.

**When to use:** Whenever a file's size is a correctness constraint (template sizes directly translate to token costs). Ratchets are better than equality assertions here because equal baselines would prevent beneficial reductions from passing CI without manual intervention.

**Trade-offs:** Golden files add a commit step when intentionally increasing template size. Acceptable given that increases are the case we want to require explicit review for.

### Pattern 2: Stable Prefix First in Template Ordering

**What:** In each compiled-in Go template, order content from most-stable to least-stable top-to-bottom. The LLM provider's prefix cache keys on an exact byte prefix of the input. Content that is identical across all dispatches of the same `(role, level)` goes first; per-dispatch volatile content (TaskUID, per-task prompt) goes last.

**When to use:** Every template that is reused across multiple dispatches of the same shape. For TIDE this means all five templates benefit, but the milestone/phase/plan planners benefit most because they fan out across wave siblings.

**Trade-offs:** Template readability slightly decreases (the "important" per-task instruction appears at the bottom rather than near the top). This is an acceptable trade against the cache efficiency gain.

### Pattern 3: Offline Fixture Replay for Cost Accounting Verification

**What:** Commit a representative `events.jsonl` file captured from a real run. Tests feed it through `ParseStream` and `estimatedCostCents` and assert the resulting `Usage` and cost match golden values. This verifies that cache-read tokens are credited at 0.10x rather than full input rate — without requiring a live provider call.

**When to use:** Whenever the billing logic changes (pricing table updates, new token dimensions, formula changes). The recorded fixture is immune to provider-side API changes.

**Trade-offs:** The fixture goes stale if the stream-json format changes significantly. Mitigated by the existing `events.jsonl` audit log produced by every real dispatch — new fixtures can be captured from any run.

---

## Anti-Patterns

### Anti-Pattern 1: cache_control injection via ProviderSpec.Params

**What people do:** Try to inject `cache_control` breakpoints through `EnvelopeIn.Provider.Params` to force explicit cache checkpoints.

**Why it's wrong:** `Params` is the model-parameter passthrough (temperature, thinking_budget, top_p, top_k). The anthropic runner's `paramsAllowList` explicitly rejects any key not in that set. More fundamentally, `claude --bare` via CLI has no `--cache-control` flag — the CLI manages caching automatically based on prompt structure. Injecting cache_control is a direct-SDK concept that does not apply to the CLI dispatch path.

**Do this instead:** Structure the prompt so the stable prefix is long and identical across sibling dispatches. The CLI's automatic prefix caching keys on exact byte prefixes; no explicit injection needed.

### Anti-Pattern 2: Hoisting shared context into Prompt at the controller side

**What people do:** Read parent artifact content (MILESTONE.md, phase brief) in the controller before dispatch, embed it inline in `EnvelopeIn.Prompt`, and rely on the template just interpolating `{{.Prompt}}` as a blob.

**Why it's wrong:** The entire shared parent context goes into the volatile suffix (after `{{.Prompt}}` interpolation position). Two wave-sibling dispatches will have different TaskUIDs and different per-task instructions but identical parent context — but if the parent context is inside `{{.Prompt}}` there is no way for the provider's prefix cache to distinguish the shared portion. The result: every dispatch is treated as a fully unique input even though 80% of the content is repeated.

**Do this instead:** Use `SharedContext` (a named field on `EnvelopeIn`) that the template places before `{{.Prompt}}`. The stable preamble + `SharedContext` form the shared prefix; only `TaskUID` and `{{.Prompt}}` are unique per dispatch.

### Anti-Pattern 3: Per-task prompt size as the primary cost metric

**What people do:** Focus on reducing `len(EnvelopeIn.Prompt)` (the per-task instruction) to cut costs.

**Why it's wrong:** The per-task instruction is already the smallest part of the rendered prompt — typically the plan planner's description of one task, a few hundred tokens. The boilerplate preamble (role intro, TIDE paradigm overview, output schema) is 3-5x larger and is repeated for every dispatch. Minimizing the volatile suffix has diminishing returns compared to trimming the stable prefix once.

**Do this instead:** Measure the rendered prompt at the template level (stable prefix + SharedContext + Prompt) and target the stable prefix for trimming first.

---

## Scalability Considerations

| Concern | Current State | After v1.0.2 |
|---------|--------------|--------------|
| Token cost per planner wave | O(n * full_template_size) — each sibling re-pays for the full preamble | O(n * volatile_size + 1 * stable_prefix_cache_write) — siblings share the prefix cache after the first dispatch warms it |
| Cache warmup cost | N/A (no prefix ordering guarantee) | 1.25x input rate for the first dispatch of a wave; subsequent siblings pay 0.10x for the shared prefix |
| Template maintenance surface | Five separate .tmpl files, content duplicated across planners | Same five files; stable prefix is still per-file but easier to audit because it is isolated at the top |
| Eval gate coverage | None — template changes have no automated check | Full coverage: size ratchet + prefix length + cost replay + structural quality |

---

## Integration Points Summary

| Integration Point | Existing Mechanism | v1.0.2 Change |
|-------------------|--------------------|---------------|
| Stable prefix ordering | None — templates are unordered | Template restructuring (Step 3) |
| Shared context across wave siblings | Inline in `Prompt` field | New `SharedContext` field on `EnvelopeIn` (Step 5) |
| Token minimization | None | Template verbosity trim (Step 4) |
| Cost accounting for cache reads | Already correct — `estimatedCostCents` uses `cacheReadCentsPerMTok` | No change needed |
| Metrics for cache efficiency | Already emitted — `tide_tokens_cache_read_total`, `_cache_creation_total` | Dashboard panel only (Step 6) |
| Eval harness | None | New `internal/eval/` package (Step 2) |

---

## Sources

All findings are from direct inspection of the following files in the production codebase (commit `58d0ae6`):

- `internal/subagent/common/prompt_templates.go`
- `internal/subagent/common/templates/*.tmpl` (all five)
- `internal/subagent/common/stream_reader.go`
- `internal/subagent/anthropic/subagent.go`
- `internal/subagent/anthropic/stream_parser.go`
- `internal/subagent/anthropic/pricing.go`
- `internal/controller/dispatch_helpers.go`
- `internal/controller/task_controller.go`
- `pkg/dispatch/envelope.go`
- `pkg/dispatch/provider.go`
- `pkg/dispatch/pricing.go`
- `internal/budget/tally.go`
- `internal/budget/reservation.go`
- `internal/metrics/registry.go`

---

*Architecture research for: TIDE v1.0.2 Ebb Tide — Token & Cost Optimization*
*Researched: 2026-06-15*
