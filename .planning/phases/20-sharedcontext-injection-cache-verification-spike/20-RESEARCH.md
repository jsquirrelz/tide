# Phase 20: SharedContext Injection + Cache Verification Spike — Research

**Researched:** 2026-06-15
**Domain:** Go controller + prompt-template plumbing, Anthropic CLI cache-hit verification
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**D-01:** 2-pod real-API probe on the durable `kind-tide-dogfood` cluster. Throwaway ≥1,024-token identical-prefix prompt dispatched twice through the real credproxy; read `cache_read_input_tokens` from the result event / `events.jsonl`. Rejected: single-process two-call (can't falsify per-pod-path hypothesis), offline-reasoning-only.

**D-02:** Pass bar = `cache_read_input_tokens > 0` on sibling #2 within TTL AND net-negative realized cost. FAIL path: tee + diff both pods' outbound request bodies at the credproxy to name the exact divergence (CWD? `--add-dir` path? per-pod workspace id?). Root cause — not "it didn't fire" — recorded in PROJECT.md.

**D-03:** Parent planner emits one curated `SharedContext` blob; controller stamps it byte-identically onto every sibling's `EnvelopeIn.SharedContext`. LLM-quality curation once + guaranteed byte-identity for caching.

**D-04:** Content is wave-scoped — parent goal + load-bearing constraints + one-line sibling-set overview. ~300–700 tokens; no verbatim PLAN.md/phase-brief dumps.

**D-05:** Field on parent `EnvelopeOut` → controller copies onto each child CRD → renders into child `EnvelopeIn.SharedContext`. Reuses artifacts-as-truth flow; stored as input data (not schedule cache — no spec violation).

**D-06:** Pure stable-prefix text field, no markers, no provider conditionals. Per-provider descriptors deferred to OpenAI/multi-provider milestone. Floor documented as "active model's documented floor" (never hardcoded 1,024).

**D-07:** `BuildPlannerEnvelope` populates `SharedContext` at every planner level (project/milestone/phase/plan); executor path (`buildEnvelopeIn`) never renders it (CACHE-02 lock).

**D-08:** If spike's body diff pinpoints a cheaply-normalisable divergence (e.g. set `cmd.Dir` identically, or fixed `--add-dir` path) — attempt fix in-phase and re-run spike. Guard: only if contained, no pod-isolation-contract violation, no chart change. Otherwise scoped follow-up. SharedContext ships regardless.

### Claude's Discretion

- Exact shape of the throwaway ≥1,024-token probe prompt.
- Concrete wording/format of the curated SharedContext blob.
- Whether the spike harness reuses `make eval` plumbing or a dedicated probe target.
- Exact JSON field naming on `EnvelopeOut`/child CRD spec for the carry path.
- Mechanism for teeing outbound request bodies at the credproxy for the FAIL diff.

### Deferred Ideas (OUT OF SCOPE)

- Per-provider cache capability descriptor (`CacheCaps`).
- Gemini explicit `CachedContent` lifecycle, Bedrock `cachePoint` injection.
- Per-provider usage normalizer for the eval harness.
- OpenAI `prompt_cache_key` routing hint.
- Wave-sibling warm-up dispatch (COST-F2).
- Project-paradigm digest in SharedContext.
- CLI-prefix normalization as its own phase (only in-phase if contained per D-08).
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CACHE-01 | Spike verifies whether stable-prefix-first ordering yields cross-pod prefix-cache hits under `claude -p --bare`; result gated in PROJECT.md. | D-01/D-02; spike harness design; `stream_parser.go` cache field mapping; `--add-dir` divergence analysis. |
| CACHE-02 | `EnvelopeIn` gains additive `SharedContext string` (omitempty); executor path ignores it; controller populates identically for all wave siblings. | `pkg/dispatch/envelope.go` additive field; `buildEnvelopeIn` omit path; `BuildPlannerEnvelope` stamp point; Task CRD spec carry field. |
| CACHE-03 | Planner templates reference `{{.SharedContext}}` in stable prefix section; combined stable prefix reaches ≥1,024 tokens on Sonnet/Opus (or Haiku gap explicitly documented). | Four planner templates all have reserved `{{/* SharedContext slot */}}` at identical location; ratchet/golden update protocol defined. |
| CACHE-04 | SharedContext populated from curated summaries, not verbatim PLAN.md / phase-brief dumps. | D-03/D-04 carry path; parent planner emits one blob; controller stamps identically. |
| CACHE-05 | Design carries no Anthropic-only assumptions; verified provider-neutral on Claude path; OpenAI parity deferred. | D-06 pure stable-prefix text field; multi-vendor caching research appendix baked in CONTEXT.md. |
</phase_requirements>

---

## Summary

Phase 20 has two coupled deliverables. Deliverable 1 (CACHE-01): an empirical spike verifying whether `claude -p --bare` emits byte-identical prefix bytes across two pods, or whether the per-pod `--add-dir eventsDir` path (which embeds a per-TaskUID subdirectory) makes each sibling's prefix unique. Deliverable 2 (CACHE-02–05): the additive `SharedContext string` field threaded from parent planner through EnvelopeOut → Task CRD spec → child EnvelopeIn → planner template rendering. The field ships unconditionally regardless of the spike outcome.

The codebase is clean: `SharedContext` does not exist anywhere yet. The four planner templates already have zero-token `{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}` markers at the correct insertion point (between fixed instructions and the volatile suffix). The eval harness (goldie goldens + byte ratchets) is the gate every template commit must keep green. `stream_parser.go` already parses `cache_read_input_tokens` and `cache_creation_input_tokens` into `Usage.CacheReadTokens` and `Usage.CacheCreationTokens` — those fields are the spike's verdict source.

The spike's chicken-and-egg risk is real but structurally isolated: the probe's ≥1,024-token prefix is a throwaway synthetic prompt, not a production dispatch, so it is independent of today's short templates. The only genuine unknown is whether the `--add-dir eventsDir` argument (which is `/workspace/envelopes/<TaskUID>/`) makes the CLI inject a per-pod path into its system prefix — since TaskUID is different per pod, that would break prefix byte-identity and prevent cache hits even with a sufficiently large stable content prefix.

**Primary recommendation:** Build the spike harness as a standalone `cmd/tide-spike` binary (build-tagged `spike`) that mirrors the `make eval` infrastructure — builds credproxy, mints a token, dispatches two sequential real `claude -p --bare` calls against the dogfood cluster — and reads `cache_read_input_tokens` from `events.jsonl`. Then implement the `SharedContext` carry path as three independent commits: (1) `pkg/dispatch/envelope.go` additive fields, (2) controller stamp + Task CRD spec field, (3) template interpolation + golden/ratchet updates.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Spike harness execution | Standalone binary (`cmd/tide-spike`) | `make eval` plumbing reuse | Real `claude -p --bare` invocation; needs credproxy + dogfood cluster; same pattern as `cmd/tide-eval` |
| Cache hit detection | `stream_parser.go` (already parses `cache_read_input_tokens`) | `events.jsonl` audit log | `Usage.CacheReadTokens > 0` on dispatch #2 is the verdict |
| Request-body diff (FAIL path) | credproxy sidecar (tee outbound body) | CLI-side approach (SUS) | Credproxy already sits on every request; body tee is least invasive |
| EnvelopeIn.SharedContext field | `pkg/dispatch/envelope.go` | — | Additive field with `omitempty`; executor path ignores by construction |
| EnvelopeOut.SharedContext carry | `pkg/dispatch/envelope.go` | — | D-05 parent emit channel already used for ChildCRDs |
| Controller stamp (byte-identity) | `BuildPlannerEnvelope` (`dispatch_helpers.go:218`) | — | Uniform populate point for all planner levels (D-07) |
| Executor omit | `buildEnvelopeIn` (`task_controller.go:1325`) | — | Never sets SharedContext; CACHE-02 lock enforced by omission |
| Task CRD spec carry field | `api/v1alpha1/task_types.go` `TaskSpec` | — | Transit field from parent EnvelopeOut to child EnvelopeIn at dispatch |
| Template interpolation | Four planner `.tmpl` files | `prompt_templates.go` (render path unchanged) | `{{.SharedContext}}` replaces the reserved comment marker |
| Golden/ratchet updates | `internal/eval/testdata/goldie/` + `testdata/ratchets/` | `render_test.go` | Every template change gates through `goldenAssert` + `ratchetAssert` |

---

## Standard Stack

No new dependencies. Everything is stdlib or already in go.mod.

### Core (already present)

| Library | Location | Purpose in Phase 20 |
|---------|----------|----------------------|
| `pkg/dispatch/envelope.go` | this repo | Add `SharedContext` on `EnvelopeIn` + `EnvelopeOut` |
| `internal/controller/dispatch_helpers.go` | this repo | `BuildPlannerEnvelope` — uniform stamp point |
| `internal/controller/task_controller.go` | this repo | `buildEnvelopeIn` — executor omit path |
| `internal/subagent/anthropic/stream_parser.go` | this repo | Already parses `cache_read_input_tokens` into `Usage.CacheReadTokens` |
| `internal/subagent/anthropic/subagent.go` | this repo | Spike primary subject: `--add-dir eventsDir` arg, `cmd.Environ()`, no `cmd.Dir` set |
| `cmd/tide-eval/main.go` | this repo | Pattern for spike harness (build tag, credproxy plumbing, `net/http`) |
| `internal/eval/` (goldie + ratchets) | this repo | Gate every template commit |
| `internal/subagent/common/templates/*.tmpl` | this repo | `{{.SharedContext}}` insertion at reserved slot |

### No New External Packages

The spike harness follows `cmd/tide-eval` exactly: `//go:build spike`, stdlib `net/http`, no SDK. No `go get` needed.

---

## Package Legitimacy Audit

No external packages to audit. This phase adds zero new dependencies.

---

## Architecture Patterns

### System Architecture Diagram

```
CACHE-01 Spike Flow
───────────────────
make spike
  └─▶ cmd/tide-spike [build:spike]
        ├─▶ Start credproxy (127.0.0.1:8443)
        ├─▶ Mint HMAC token (signing key = throwaway)
        ├─▶ Dispatch #1: claude -p --bare
        │     stdin = [≥1024-token filler prefix] + [unique tail A]
        │     --add-dir /workspace/envelopes/<UID-A>/
        │     → events.jsonl → ParseStream → Usage.CacheCreationTokens > 0
        └─▶ Dispatch #2 (< 5 min later): claude -p --bare
              stdin = [identical filler prefix] + [unique tail B]
              --add-dir /workspace/envelopes/<UID-B>/
              → events.jsonl → ParseStream → Usage.CacheReadTokens > 0 ? PASS : FAIL
              FAIL path: credproxy body tee → diff /tmp/body-A.json /tmp/body-B.json
              → identify exact divergence bytes

CACHE-02..05 Carry Path
───────────────────────
Parent Planner Job
  └─▶ claude -p --bare (renders planner template with {{.SharedContext}}="")
        Writes children/*.json + emits EnvelopeOut{
          ChildCRDs: [...],
          SharedContext: "<curated wave-scoped summary>"   ← NEW D-05 field
        }

Controller (handleJobCompletion / reporter)
  └─▶ reads EnvelopeOut
        for each child_i in ChildCRDs:
          create Task/Phase/Plan CRD with
            Spec.SharedContext = out.SharedContext   ← NEW Task CRD spec field

Child Dispatch (BuildPlannerEnvelope)
  └─▶ reads child CRD Spec.SharedContext
        → EnvelopeIn.SharedContext = spec.SharedContext  ← identical bytes ∀ siblings
        → template.Execute(buf, envelopeIn)
        → {{.SharedContext}} renders in D-07 slot
        
Executor Dispatch (buildEnvelopeIn)
  └─▶ SharedContext field deliberately omitted / left "" (CACHE-02 lock)
```

### Recommended Project Structure

No structural changes to the directory tree. All changes are additive fields on existing types and template text substitutions.

```
pkg/dispatch/
  envelope.go          — +SharedContext on EnvelopeIn; +SharedContext on EnvelopeOut

api/v1alpha1/
  task_types.go        — +SharedContext on TaskSpec (carry transit field)
  (phase_types.go, plan_types.go, milestone_types.go if those planners need carry)

internal/controller/
  dispatch_helpers.go  — BuildPlannerEnvelope reads SharedContext from parent CRD spec
  task_controller.go   — buildEnvelopeIn: leave SharedContext empty (no change needed)

internal/subagent/common/templates/
  milestone_planner.tmpl  — replace {{/* SharedContext slot */}} with {{.SharedContext}}
  project_planner.tmpl    — same
  phase_planner.tmpl      — same
  plan_planner.tmpl       — same
  (task_executor.tmpl: slot exists but executor NEVER renders SharedContext — CACHE-02)

internal/eval/testdata/
  goldie/*.golden      — regenerate after template change
  ratchets/*.txt       — update deliberately after template change

cmd/tide-spike/
  main.go              — spike harness (//go:build spike; mirrors cmd/tide-eval pattern)
```

### Pattern 1: Additive EnvelopeIn Field with Executor Omit

The existing additive-field pattern (`PromptPath`, `Branch` are executor-only; `Dev` is test-only) provides the precedent. `SharedContext` is the inverse: planner-only. Add with `omitempty`; `buildEnvelopeIn` never references it.

```go
// Source: pkg/dispatch/envelope.go (VERIFIED: codebase)
// Add to EnvelopeIn struct after existing fields:

// SharedContext is the wave-scoped shared context string emitted by the
// parent planner and stamped byte-identically onto all wave siblings by
// the controller at child dispatch time (Phase 20 CACHE-02/D-05). Growing
// the stable prefix toward the provider's cacheable minimum (≥1,024 tokens
// for Sonnet/Opus; 4,096 for Haiku — see PROJECT.md provider floor table).
//
// Planner templates reference {{.SharedContext}} in the D-07 reserved slot.
// Executor dispatches (role="executor") never populate this field (CACHE-02
// lock) — the executor template does not reference it.
SharedContext string `json:"sharedContext,omitempty"`
```

Add to `EnvelopeOut` struct (the parent-emit carry channel, D-05):

```go
// Source: pkg/dispatch/envelope.go (VERIFIED: codebase)
// Add to EnvelopeOut struct:

// SharedContext is the curated wave-scoped shared context string the
// parent planner emits for the orchestrator to stamp byte-identically onto
// each sibling child's EnvelopeIn.SharedContext at dispatch time (D-05).
// Empty for executor-level dispatches.
SharedContext string `json:"sharedContext,omitempty"`
```

**Impact on existing tests:** `omitempty` ensures zero-value `SharedContext=""` never appears in serialized JSON. Existing golden fixtures and envelope round-trip tests that do not set `SharedContext` continue to pass without modification.

### Pattern 2: Spike Harness Mirroring `cmd/tide-eval`

[VERIFIED: codebase] `cmd/tide-eval/main.go` is the canonical pattern:
- `//go:build eval` at file top
- `flag.Parse()` for `-proxy` / `-token` / `-model`
- Reads `TIDE_PROXY_ENDPOINT` + `TIDE_SIGNED_TOKEN` env vars as defaults
- `http.Client{Timeout: 30 * time.Second}` for all credproxy calls
- Must include `anthropic-version: 2023-06-01` header

Spike extends this pattern:
1. Renders a throwaway ≥1,024-token prompt (identical prefix + unique suffix per dispatch)
2. Fires it via `claude -p --bare` (not `count_tokens`) using the same credproxy
3. Reads `events.jsonl` to extract `cache_read_input_tokens` and `cache_creation_input_tokens`
4. On FAIL: reads teed request bodies from credproxy (see §Pitfall 3 for tee options)

Concretely, `cmd/tide-spike/main.go` shells out to the `claude` CLI exactly as `internal/subagent/anthropic/subagent.go:285–294` does:

```go
// Source: internal/subagent/anthropic/subagent.go:285–294 (VERIFIED: codebase)
args := []string{
    "-p",
    "--model", in.Provider.Model,
    "--output-format", "stream-json",
    "--verbose",
    "--include-partial-messages",
    "--permission-mode", "acceptEdits",
    "--add-dir", eventsDir,
    "--bare",
}
cmd := a.execFunc(ctx, a.opts.ClaudeBinary, args...)
cmd.Stdin = strings.NewReader(renderedPrompt)
cmd.Env = append(cmd.Environ(),
    "ANTHROPIC_BASE_URL="+in.ProxyEndpoint,
    "ANTHROPIC_API_KEY="+in.SignedToken,
    "NODE_EXTRA_CA_CERTS="+nodeExtraCACertsPath,
)
```

Note: **`cmd.Dir` is never set** (`grep -c "cmd\.Dir" subagent.go` = 0). The process runs in the Go process's working directory. This is the key observation for the spike: the `--add-dir eventsDir` path is the ONLY per-pod-unique path injected into the CLI invocation. The spike must use two different `eventsDir` values (different TaskUIDs) to faithfully simulate two-pod dispatch.

### Pattern 3: Controller Stamp at `BuildPlannerEnvelope`

[VERIFIED: codebase] `BuildPlannerEnvelope` at `dispatch_helpers.go:218` constructs `EnvelopeIn` for all four planner levels and is the single uniform populate point (D-07).

Current signature:
```go
// Source: internal/controller/dispatch_helpers.go:218 (VERIFIED: codebase)
func BuildPlannerEnvelope(level string, parent metav1.Object, project *tideprojectv1alpha1.Project,
    attempt int, token, prompt string, caps pkgdispatch.Caps, proxyEndpoint string,
    helmDefaults ProviderDefaults) (pkgdispatch.EnvelopeIn, []byte, error)
```

Phase 20 adds `sharedContext string` parameter. Every caller (milestone, phase, plan, project reconcilers) passes the value read off the parent CRD's `Spec.SharedContext`. At most call sites this will be `""`  (until the planner dispatch result circulates), but the signature is uniform.

**Alternative (simpler):** Read `SharedContext` inside `BuildPlannerEnvelope` directly from `parent` via a type switch — eliminates the parameter. Recommended if the parent CRD field is on all four planner-level CRD types. If the field is only on `TaskSpec` (Task-level), the parameter is needed for the controller to pass the resolved value.

### Pattern 4: EnvelopeOut → Task CRD Spec → EnvelopeIn Carry Path

The D-05 carry path follows the existing `ChildCRDs` + `SourcePath` pattern:

1. Parent planner writes `EnvelopeOut.SharedContext = "<curated string>"` to `out.json`
2. Controller reads `out.json` via `EnvReader.ReadOut(...)` in `handleJobCompletion` (milestone_controller.go:538, phase_controller.go, plan_controller.go)
3. Controller passes `out.SharedContext` to `MaterializeChildCRDs` (or the reporter Job path) so the materializer can stamp it onto each child CRD's spec
4. `MaterializeChildCRDs` (`internal/reporter/materialize.go:188`) decodes `child.Spec.Raw` into the typed CRD spec. For the stamp, the cleanest approach is to add a carry field to `ChildCRDSpec` itself, or to pass SharedContext separately and stamp after decode

**Cleanest carry mechanism:** Add `SharedContext string `json:"sharedContext,omitempty"`` to `ChildCRDSpec`. The parent planner sets this field alongside `kind`/`name`/`spec` in the `EnvelopeOut.ChildCRDs` slice entries. `MaterializeChildCRDs` then copies `child.SharedContext` into the concrete CRD's `Spec.SharedContext` after decode, for Task children. This is consistent with how `SourcePath` on `ChildCRDSpec` is already used to stamp `Task.Spec.PromptPath`.

**Alternative:** Add `SharedContext` to `EnvelopeOut` top-level (D-05 literal), and pass it as a parameter to `MaterializeChildCRDs`. Simpler for the parent contract; materializer needs an extra arg. Either is valid; the `ChildCRDSpec` carry matches the existing `SourcePath` precedent more closely.

**Where does the SharedContext live on the child CRD?** The planner-authored children are Phase, Plan, and Task CRDs. For Phase 20, only **Task CRDs** are dispatched with `EnvelopeIn` populated by `buildEnvelopeIn` (executor path — which must NOT set SharedContext). The planner dispatches above the task level use `BuildPlannerEnvelope` which can populate SharedContext from the parent's CRD spec. So the carry field is needed on:

- `api/v1alpha1/task_types.go TaskSpec` — for plan planner → task dispatch path (D-07, plan level)
- Optionally on `PhaseSpec` / `PlanSpec` — for phase planner → plan dispatch and milestone planner → phase dispatch (D-07 uniform). These are also planner levels where `BuildPlannerEnvelope` is called.

The simplest uniform approach: add `SharedContext string `json:"sharedContext,omitempty"`` to all four child CRD specs (MilestoneSpec, PhaseSpec, PlanSpec, TaskSpec). The field is vestigial on Milestone (nothing above project level), but uniformity keeps the code path single.

### Pattern 5: Template Slot Replacement

[VERIFIED: codebase] All four planner templates contain this exact marker:

```
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}
```

- `milestone_planner.tmpl:42` — on its own line, between `machine contract.` and `TaskUID:`
- `project_planner.tmpl:44` — same position
- `phase_planner.tmpl:49` — same position, between `machine contract.` and blank line then `TaskUID:`
- `plan_planner.tmpl:96` — same position

Replace with:
```
{{- if .SharedContext}}
{{.SharedContext}}
{{end -}}
```

Or simpler (renders empty string when not set, produces a blank line):
```
{{.SharedContext}}
```

The `{{- if .SharedContext}}` form is cleaner — no blank line when SharedContext is empty, preserving existing golden files when `SharedContext=""`. This requires golden updates only when SharedContext is non-empty (i.e., only when the eval fixture sets it). The `ratchetAssert` will require an update since byte count changes.

**task_executor.tmpl** has the slot marker at line 24 but must NOT interpolate SharedContext (CACHE-02 lock, executor path ignores it). Leave the marker or remove it — either way, do not add `{{.SharedContext}}` to the executor template.

### Anti-Patterns to Avoid

- **Setting SharedContext on the executor path (`buildEnvelopeIn`):** CACHE-02 lock. The executor template never references SharedContext. Confirmed: `task_executor.tmpl` does not (and must not) interpolate `{{.SharedContext}}`.
- **Hardcoding 1,024 as a constant in production code:** D-06. Document the floor table in PROJECT.md; never embed a numeric floor in controller or template logic.
- **Adding SharedContext to `TerminationStub`:** it must stay under 4 KB. SharedContext is potentially hundreds of tokens / hundreds of bytes. Do NOT add it to the tiny termination message.
- **Caching the SharedContext in `.status`:** violates CRD .status-only + rederive rule. SharedContext is input data on the child Spec, not a schedule cache.
- **Putting SharedContext on `ProviderSpec.Params`:** the anthropic runner has a strict params allow-list (`paramsAllowList` in `subagent.go:68–73`). SharedContext is not a model parameter; it belongs on EnvelopeIn directly.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Cache hit detection | Custom Anthropic API client | `stream_parser.go` `ParseStream` already maps `cache_read_input_tokens` → `Usage.CacheReadTokens` | Already wired; the result event is the authoritative source |
| Token counting | New tokenizer / count_tokens call | `make eval` / `cmd/tide-eval` pattern (already in repo) | Credproxy already allowlists `POST /v1/messages/count_tokens`; no new route needed |
| Credential plumbing for spike | Raw API key in environment | Credproxy + HMAC signed token (existing `make eval` / `run-eval.sh` pattern) | Security rule: raw key never in spike process, only in credproxy sidecar |
| Request body capture | Adding a logging middleware to credproxy | Credproxy body-tee flag or a thin wrapper | The credproxy already owns all outbound requests; adding a `--tee-body-dir` flag is one function |

**Key insight:** The eval infrastructure (`cmd/tide-eval`, `/tmp/tide-eval/run-eval.sh`, golang:1.26.3 container pattern) is production-ready for the spike. The spike replaces `count_tokens` calls with `claude -p --bare` calls; everything else (credproxy startup, token minting, SSL cert handling, `SSL_CERT_FILE` env, container pattern) is identical.

---

## Runtime State Inventory

Step 2.5 SKIPPED — this is not a rename/refactor/migration phase. No runtime state requires migration.

---

## Common Pitfalls

### Pitfall 1: `--add-dir eventsDir` Path Is the Likely Prefix Divergence

**What goes wrong:** The spike dispatches two `claude -p --bare` calls with identical content but different `eventsDir` values (`/workspace/envelopes/<UID-A>/` vs `/workspace/envelopes/<UID-B>/`). If the Claude CLI embeds the `--add-dir` path into its system prompt (e.g., to inform the model where it can write), the two requests will have non-identical request bodies at the prefix level, preventing a cache hit even with ≥1,024 tokens of identical content prefix.

**Why it happens:** The CLI's `--add-dir` flag is a write-scope permission argument. The CLI may serialize it into the model's context or system message to tell the model what directories it can access.

**How to avoid (spike design):** The spike must use two DIFFERENT TaskUIDs (different `eventsDir` paths) to faithfully simulate real pod behavior. If the spike PASSES with different UIDs, cross-pod caching is confirmed. If it FAILS, the FAIL-path diff at the credproxy will show exactly where in the request body the UID-specific path appears. [ASSUMED — CLI internal behavior; requires empirical verification]

**Warning signs:** The credproxy body diff shows a path like `/workspace/envelopes/<UID>` appearing in the first 100 bytes of the `system` field of the request.

**D-08 containment check:** If the divergence is ONLY in `--add-dir`, the fix is to use a fixed/normalized `--add-dir` path across all siblings (e.g., `/workspace/envelopes/shared/` or the parent's dir). Check: does this violate the per-task pod isolation contract? The `--add-dir` scope is write-permission, not read-isolation. If changing it to a shared path does not allow cross-sibling writes, it may be safe. This requires careful evaluation before attempting in-phase.

### Pitfall 2: Spike Chicken-and-Egg with Template Token Floor

**What goes wrong:** The planner tries to demonstrate that SharedContext + templates clears 1,024 tokens, but the spike dispatches the throwaway probe before SharedContext exists. The spike is measuring the WRONG thing (current short templates, not SharedContext-grown templates).

**Why it happens:** Confusing two different questions: (A) does the CLI support cross-pod prefix caching at all? and (B) do the production templates + SharedContext content clear the floor?

**How to avoid:** The spike (CACHE-01) answers question A only. It uses a SYNTHETIC ≥1,024-token probe prompt — not the production templates. Question B is answered by `make eval` after SharedContext is populated with real content. These are distinct test runs. The spike's probe must construct its own ≥1,024-token prefix independently of what the templates currently render.

**Concretely:** The probe filler can be a deterministic padding string like a repeated dictionary word, a lorem-ipsum block, or a simple policy-text paragraph repeated until >1,024 tokens. The key property: both dispatches must have byte-identical prefix bytes for the first N tokens (N > 1,024).

### Pitfall 3: FAIL-Path Body Tee — Non-Invasive Approach

**What goes wrong:** Attempting to capture outbound request bodies from inside the `claude` CLI is not feasible (closed binary). Attempting to capture them by running a MITM proxy between the spike and the credproxy adds complexity.

**How to avoid:** The credproxy (`internal/credproxy/server.go`) already sits on every request. Adding a `--tee-body-dir` flag to credproxy that writes each request body to a file on the first two requests is the least-invasive option. Alternatively, the spike harness can run two sequential calls and rely on the credproxy's existing log (which may or may not capture full request bodies). The simplest approach for the spike: add a `--tee-body-dir /tmp/spike-bodies/` flag to the credproxy, guarded by `//go:build spike` or a runtime flag. This writes `req-1.json` and `req-2.json` which the spike binary diffs. [ASSUMED — credproxy's current logging depth; check `internal/credproxy/server.go` at implementation time]

### Pitfall 4: TerminationStub Size Limit

**What goes wrong:** Adding `SharedContext` to `TerminationStub` would add hundreds of bytes to a 4 KB limit message.

**How to avoid:** `SharedContext` is NEVER added to `TerminationStub`. It stays on the full `EnvelopeOut` in `out.json`. The controller reads the full `EnvelopeOut` via `EnvReader.ReadOut(...)` (already used for `ChildCRDs`) — the termination message is only the tiny-status subset. This is already the correct path.

### Pitfall 5: Golden Files Must Be Updated in the Same Commit as Template Change

**What goes wrong:** Template edit committed, goldie golden not updated, `make test-unit` fails CI.

**How to avoid:** `render_test.go`'s `goldenAssert` uses `goldie.Assert` (not `goldie.Update`). Goldens must be manually regenerated: `go test ./internal/eval/ -update` (goldie's update flag). The ratchet must also be updated manually in `testdata/ratchets/<name>.txt`. Both updates must be in the same commit as the template change — this is the established Phase 18/19 discipline.

**Exact commands (from Phase 18/19 precedent):**
```bash
# Regenerate goldens after template change:
go test ./internal/eval/ -update -run TestGoldenRender
# Verify ratchet (will fail if byte count changed):
go test ./internal/eval/ -run TestByteRatchet
# Update ratchet manually in testdata/ratchets/<name>.txt
# Then re-run to confirm green:
go test ./internal/eval/
```

### Pitfall 6: `SharedContext` Slot in `task_executor.tmpl` Must NOT Be Interpolated

**What goes wrong:** A developer sees the `{{/* SharedContext slot — populated in Phase 20 */}}` comment in `task_executor.tmpl` and adds `{{.SharedContext}}` there, breaking CACHE-02.

**How to avoid:** The executor template's slot comment must either be removed (safest) or left as-is. Planner CI coverage (goldie snapshot tests) catches any template change automatically, but the CACHE-02 lock must be explicit in the implementation plan. The `buildEnvelopeIn` path in `task_controller.go:1347–1369` constructs the executor `EnvelopeIn` without `SharedContext`; `tmpl.Execute(buf, envIn)` will render `{{.SharedContext}}` as an empty string if it appeared in the template — but it must not appear.

### Pitfall 7: `BuildPlannerEnvelope` Signature Change Blast Radius

**What goes wrong:** Adding a `sharedContext string` parameter to `BuildPlannerEnvelope` requires updating all callers: `milestone_controller.go:416`, `phase_controller.go:379`, `plan_controller.go:398`, `project_controller.go` (if any), plus all tests in `dispatch_helpers_test.go:102` and `dispatch_helpers_test.go:263`.

**How to avoid:** Enumerate all call sites before the PR. The safe approach is to make the parameter explicitly named and check that no other code calls `BuildPlannerEnvelope` outside `internal/controller/`:

```bash
grep -rn "BuildPlannerEnvelope" /path/to/repo --include="*.go"
```

Current confirmed call sites (from codebase grep): `milestone_controller.go:416`, `phase_controller.go:379`, `plan_controller.go:398`, `dispatch_helpers_test.go:124`, `dispatch_helpers_test.go:272`.

---

## Code Examples

### Reading Cache Tokens from `events.jsonl` (Spike Verdict)

```go
// Source: internal/subagent/anthropic/stream_parser.go (VERIFIED: codebase)
// stream_parser.go already maps:
//   cache_read_input_tokens     → Usage.CacheReadTokens
//   cache_creation_input_tokens → Usage.CacheCreationTokens
//
// Spike verdict:
//   dispatch #1: usage.CacheCreationTokens > 0  → cache write confirmed
//   dispatch #2: usage.CacheReadTokens > 0       → PASS (cross-pod hit confirmed)
//              : usage.CacheReadTokens == 0      → FAIL (need body diff)
usage, _, err := ParseStream(stdout, eventsFile)
if err != nil { /* handle */ }
fmt.Printf("dispatch result: read=%d create=%d\n",
    usage.CacheReadTokens, usage.CacheCreationTokens)
```

### Additive Field in `EnvelopeIn` (CACHE-02)

```go
// Source: pkg/dispatch/envelope.go (pattern from existing omitempty fields)
// Add after the Dev field (last field):
SharedContext string `json:"sharedContext,omitempty"`
```

Existing serialization tests (`TestNewTerminationStub_StaysSmall`, golden render tests) pass without change because `omitempty` suppresses the field when empty.

### `BuildPlannerEnvelope` with SharedContext Stamp (D-07)

```go
// Source: internal/controller/dispatch_helpers.go:218 (VERIFIED: codebase)
// Current envIn construction — add SharedContext field:
envIn := pkgdispatch.EnvelopeIn{
    // ... existing fields unchanged ...
    SharedContext: sharedContext,  // NEW: populated from parent CRD spec
}
```

### Template Interpolation at Reserved Slot

```
// Source: all four planner templates (VERIFIED: codebase)
// Current (Phase 19 output):
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}

// Phase 20 replacement:
{{- if .SharedContext}}
{{.SharedContext}}
{{end -}}
```

The `{{- if .SharedContext}}` form produces no output when `SharedContext=""` — preserving existing golden bytes for the test fixture (which does not set `SharedContext`).

### `buildEnvelopeIn` Executor Path — No Change Required

```go
// Source: internal/controller/task_controller.go:1347 (VERIFIED: codebase)
// The executor EnvelopeIn literal omits SharedContext by construction:
envIn := pkgdispatch.EnvelopeIn{
    APIVersion:          pkgdispatch.APIVersionV1Alpha1,
    Kind:                pkgdispatch.KindTaskEnvelopeIn,
    TaskUID:             string(task.UID),
    Role:                "executor",
    Level:               "task",
    PromptPath:          task.Spec.PromptPath,
    Branch:              project.Status.Git.BranchName,
    FilesTouched:        task.Spec.FilesTouched,
    DependsOn:           task.Spec.DependsOn,
    DeclaredOutputPaths: task.Spec.DeclaredOutputPaths,
    Caps:                caps,
    Provider:            ResolveProvider(project, "task", r.Deps.HelmProviderDefaults),
    ProxyEndpoint:       "https://127.0.0.1:8443",
    SignedToken:         token,
    Dev:                 dev,
    // SharedContext intentionally absent — CACHE-02 lock (executor ignores it)
}
```

No code change needed here. `SharedContext` defaults to `""` (zero value) and serializes as absent due to `omitempty`.

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| SharedContext slot = zero-token comment marker | `{{.SharedContext}}` interpolation with content | Phase 20 (this phase) | Planner templates now grow stable prefix toward cache floor |
| No EnvelopeOut carry field for curated context | `EnvelopeOut.SharedContext` emitted by parent planner | Phase 20 | D-05 carry path enables byte-identical cross-sibling population |
| Cross-pod caching status = unknown | Empirically determined by spike (CACHE-01) | Phase 20 | Decision recorded in PROJECT.md; frames all future caching work |

**Deprecated/outdated:**
- The `{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}` comment in all four planner templates: replaced by `{{.SharedContext}}` interpolation in this phase.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `claude -p --bare` may embed `--add-dir` path into its system prompt, causing per-pod prefix divergence | Pitfall 1; CACHE-01 spike design | If wrong (CLI does NOT embed it), the spike will PASS trivially and there is no divergence to fix. Lower risk than assuming caching works — the spike confirms. |
| A2 | The credproxy does not currently log full outbound request bodies; a `--tee-body-dir` flag is needed for the FAIL-path diff | Pitfall 3; Claude's Discretion | If credproxy already logs bodies at DEBUG level, no new flag is needed. Check `internal/credproxy/server.go` before implementing. |
| A3 | The parent planner's `out.json` SharedContext string is readable by the controller via the existing `EnvReader.ReadOut(...)` path (same path used for ChildCRDs) | Carry path architecture | True by construction — `EnvelopeOut` is the full out.json struct; ReadOut returns it whole. Low risk. |
| A4 | Adding `SharedContext` to `PhaseSpec` / `PlanSpec` / `MilestoneSpec` (in addition to `TaskSpec`) is acceptable for the uniform carry path | Architecture; Don't Hand-Roll | If CRD field proliferation is a concern, only add to `TaskSpec` and handle Phase/Plan levels differently. Medium risk — ask at planning time. |
| A5 | `{{- if .SharedContext}}` conditional in templates preserves existing goldie golden byte-identity when `SharedContext=""` in the eval fixture | Template interpolation pattern | If the conditional adds whitespace even when false, goldens will break. Verify with `go test ./internal/eval/` in Wave 0 before committing template changes. |

**If this table were empty:** All claims in this research were verified or cited — no user confirmation needed. (It is not empty.)

---

## Open Questions

1. **Exact mechanism for request-body tee at credproxy (FAIL path)**
   - What we know: credproxy sits on all outbound requests; it already logs method + path
   - What's unclear: does it log full body at any log level? or does a new `--tee-body-dir` flag need to be added?
   - Recommendation: read `internal/credproxy/server.go` in Wave 0; if a full-body log already exists at DEBUG level, use it. Otherwise add a minimal `--tee-body-dir` flag behind a build tag or runtime flag.

2. **Scope of `SharedContext` carry field — all CRD spec types or only Task?**
   - What we know: D-07 says "uniform on all planner envelopes"; `BuildPlannerEnvelope` is called for milestone/phase/plan/project levels
   - What's unclear: should `MilestoneSpec`, `PhaseSpec`, `PlanSpec` all get a `SharedContext` field, or is it enough to carry it only on `TaskSpec`?
   - Recommendation: add to all four child CRD spec types for uniformity (matches D-07 literal). The field is vestigial on Milestone (no parent above project) but costs nothing. This avoids a level-conditional branch in `BuildPlannerEnvelope`.

3. **Probe construction for the spike: does the `--add-dir` path appear in the request prefix?**
   - What we know: `--add-dir` is a CLI write-permission scope argument; cmd.Dir is not set in production; the CLI may or may not advertise the scoped dir in the system prompt
   - What's unclear: whether the path is in the first `system` message bytes or in a later dynamic context
   - Recommendation: the FAIL-path body diff answers this empirically. The spike binary should print the first 500 bytes of each request body's `system` field on FAIL for quick human inspection.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `kind-tide-dogfood` cluster | CACHE-01 spike (D-01) | [ASSUMED] | — | Rebuild per memory note: `kind create cluster --name tide-dogfood` |
| `~/.tide/anthropic.key` | Real API key for spike | [ASSUMED] | — | Per memory: "real key durably at `~/.tide/anthropic.key`" |
| `golang:1.26.3` container (for macOS SSL) | `make eval` / spike harness | [ASSUMED] | 1.26.3 | macOS ignores `SSL_CERT_FILE`; run spike inside the container |
| `/tmp/tide-eval/run-eval.sh` helper | Spike harness pattern reference | [ASSUMED present] | — | Recreate per memory note if missing |
| `credproxy` binary (buildable) | Spike harness | ✓ | — | `go build ./cmd/credproxy` |
| `claude` CLI | Spike (actual dispatch) | [ASSUMED] | ≥v2.1.139 | None — required |

**Missing dependencies with no fallback:**
- `claude` CLI ≥v2.1.139 (required for `--bare` flag, D-01). Verify at spike time: `claude --version`.

**Missing dependencies with fallback:**
- `kind-tide-dogfood` cluster — rebuild if stale; the memory note documents the recipe.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go `testing` + goldie/v2 (already in repo) |
| Config file | none (tests in `internal/eval/` run under `go test ./...`) |
| Quick run command | `go test ./internal/eval/ -run TestGoldenRender` |
| Full suite command | `make test-unit` (or `go test ./...`) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CACHE-01 | `cache_read_input_tokens > 0` on dispatch #2 (within TTL) | manual/spike run | `make spike` (new target) or `go run -tags spike ./cmd/tide-spike/` | ❌ Wave 0 (new binary) |
| CACHE-01 | FAIL path: body diff names divergence | manual inspection | spike binary prints diff on exit code 1 | ❌ Wave 0 (new binary) |
| CACHE-02 | `EnvelopeIn.SharedContext` field exists on struct | unit (compile-time) | `go build ./pkg/dispatch/` | ❌ Wave 0 (field doesn't exist yet) |
| CACHE-02 | Executor path (`buildEnvelopeIn`) does not set SharedContext | unit | `TestBuildEnvelopeInExecutorIgnoresSharedContext` (new) in `task_controller_test.go` | ❌ Wave 0 |
| CACHE-02 | `BuildPlannerEnvelope` populates SharedContext identically for wave siblings | unit | `TestBuildPlannerEnvelopeSharedContext` (new) in `dispatch_helpers_test.go` | ❌ Wave 0 |
| CACHE-03 | Planner templates render `{{.SharedContext}}` in stable prefix | unit (golden) | `go test ./internal/eval/ -run TestGoldenRender` | ✅ (goldens exist; need update after template change) |
| CACHE-03 | Combined stable prefix ≥1,024 tokens (Sonnet/Opus) after SharedContext | online eval | `make eval` with SharedContext fixture populated | ✅ (`cmd/tide-eval` exists; fixture update needed) |
| CACHE-03 | Ratchet updated after template growth | unit | `go test ./internal/eval/ -run TestByteRatchet` | ✅ (ratchets exist; need deliberate update) |
| CACHE-04 | SharedContext is curated blob (no verbatim PLAN.md dump) | manual review | human review of parent planner output in spike run | manual-only |
| CACHE-05 | `SharedContext` field has no provider-specific markers | code review | `grep -n "anthropic\|openai\|cache_control" pkg/dispatch/envelope.go` | automated spot-check |
| CACHE-05 | `EnvelopeIn.SharedContext` is a plain string (no vendor fields) | compile-time | `go vet ./pkg/dispatch/` | ✅ once field exists |

### Sampling Rate

- **Per task commit:** `go test ./internal/eval/ && go test ./pkg/dispatch/ && go build ./...`
- **Per wave merge:** `make test-unit`
- **Phase gate:** Full suite green (`make test-unit`) before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `cmd/tide-spike/main.go` — CACHE-01 spike harness (`//go:build spike`)
- [ ] `Makefile` `spike:` target — mirrors `eval:` target
- [ ] `TestBuildEnvelopeInExecutorIgnoresSharedContext` — new test in `internal/controller/task_controller_test.go`
- [ ] `TestBuildPlannerEnvelopeSharedContext` — new test in `internal/controller/dispatch_helpers_test.go`
- [ ] `TestEnvelopeInSharedContextOmitEmpty` — new test in `pkg/dispatch/` to verify `omitempty` suppresses field when empty
- [ ] goldie fixture update for eval fixture (add `SharedContext` to `baseEnvelope` in `render_test.go` for golden comparison)

---

## Security Domain

`security_enforcement` not explicitly set to false in config — treating as enabled.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | no | — |
| V4 Access Control | partial | `SharedContext` is populated by the controller, never by the subagent (no LLM-authored field flows into EnvelopeIn.SharedContext without controller mediation) |
| V5 Input Validation | yes | `SharedContext` on EnvelopeOut is LLM-authored. The controller must treat it as untrusted: size-cap before storing on child CRD spec (prevent etcd size abuse — 1 MiB soft limit per object) |
| V6 Cryptography | no | — |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| LLM-authored SharedContext contains injection targeting downstream planner | Tampering | `SharedContext` is stored as a plain string and rendered as template text; it does NOT go through any shell/exec path. Text injection into a sibling planner's stable prefix is a prompt-injection risk, not a code-execution risk. Curated D-04 content + wave-scope limits blast radius. |
| Oversized SharedContext exhausting etcd per-object limit | DoS | Add a size cap (e.g., 64 KiB) at the controller when reading `out.SharedContext` before writing to child CRD spec. `TerminationStub` already stays < 4 KB; SharedContext on `out.json` has no current cap — one is needed. |
| SharedContext tee/diff file written to predictable path exploited by concurrent spike runs | Tampering | Use a tmpdir with a random suffix for body tee files; clean up after spike exits. |

---

## Sources

### Primary (HIGH confidence — VERIFIED codebase)

- `pkg/dispatch/envelope.go` — `EnvelopeIn`, `EnvelopeOut`, `ChildCRDSpec`, `Usage` struct fields (`CacheReadTokens`, `CacheCreationTokens`); `ValidateAPIVersionKind`
- `internal/subagent/anthropic/subagent.go:285–294` — `claude -p --bare` invocation args; no `cmd.Dir` set; `--add-dir eventsDir` with per-TaskUID path; `cmd.Environ()` credproxy wiring
- `internal/subagent/anthropic/stream_parser.go` — `ParseStream`; `streamUsage.CacheReadInputTokens` → `Usage.CacheReadTokens`; `streamUsage.CacheCreationInputTokens` → `Usage.CacheCreationTokens`; result event as verdict source
- `internal/controller/dispatch_helpers.go:218` — `BuildPlannerEnvelope` signature and body; all four planner-level call sites confirmed
- `internal/controller/task_controller.go:1325` — `buildEnvelopeIn` executor path; all fields enumerated; `SharedContext` absent by construction
- `internal/controller/milestone_controller.go:499,533–545` — `handleJobCompletion`; `EnvReader.ReadOut(...)` returning full `EnvelopeOut`
- `internal/reporter/materialize.go:188` — `MaterializeChildCRDs`; `ChildCRDSpec.SourcePath` → `Task.Spec.PromptPath` stamp precedent
- `internal/subagent/common/templates/*.tmpl` — all four planner templates verified; SharedContext slot location confirmed (milestone:42, project:44, phase:49, plan:96, executor:24)
- `internal/subagent/common/prompt_templates.go` — `LoadPromptTemplate`; `tmpl.Execute(&buf, envelopeIn)` render pattern
- `internal/eval/render_test.go` — `goldenAssert`, `ratchetAssert`; `baseEnvelope` fixture; `envelopeFor(role, level)` pattern
- `cmd/tide-eval/main.go` — complete spike harness pattern; `//go:build eval`; `countTokens` via stdlib `net/http`; credproxy wiring
- `/tmp/tide-eval/run-eval.sh` — golang:1.26.3 container pattern; `SSL_CERT_FILE` env; throwaway signing key; token minting
- `internal/eval/testdata/ratchets/` — current byte ratchets: milestone=1862, phase=1974, plan=3985, project=2193, task=1566
- `api/v1alpha1/task_types.go:69` — `TaskSpec` fields; confirmed no `SharedContext` exists yet
- `.planning/phases/19-template-reorder-token-minimization/19-CONTEXT.md:91–97` — D-07 reserved slot spec; `{{- /* SharedContext slot */ -}}` is the exact marker placed by Phase 19

### Secondary (MEDIUM confidence)

- `.planning/research/SUMMARY.md` — templates ~200 tokens, below cache floor; cross-pod scoping is the critical unknown
- `.planning/research/PITFALLS.md` — CLI-vs-SDK no-`cache_control` lever; cache-write premium net-negative one-shot
- `.planning/phases/20-sharedcontext-injection-cache-verification-spike/20-CONTEXT.md code_context appendix` — multi-vendor caching table; design implications for D-06; floor table 1,024–4,096

---

## Metadata

**Confidence breakdown:**
- Spike harness design: HIGH — `cmd/tide-eval` is the exact blueprint; `/tmp/tide-eval/run-eval.sh` covers credproxy + container pattern; `stream_parser.go` cache field parsing confirmed
- Envelope field plumbing: HIGH — `pkg/dispatch/envelope.go` fully read; `BuildPlannerEnvelope` and `buildEnvelopeIn` both fully read; no SharedContext exists yet
- Template interpolation: HIGH — all four planner templates read; slot locations verified by line number; goldie/ratchet protocol confirmed from `render_test.go`
- Spike PASS/FAIL outcome: LOW — empirical question; spike verifies; cannot be determined from codebase alone
- Credproxy body tee mechanism: MEDIUM — credproxy allowlist path confirmed; full-body logging depth unverified

**Research date:** 2026-06-15
**Valid until:** 2026-07-15 (stable: no fast-moving external dependencies; all findings from codebase)
