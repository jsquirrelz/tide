# Phase 53: Chart Config + Dashboard Provenance Surfacing - Context

**Gathered:** 2026-07-21
**Status:** Ready for planning

> **Mode:** `--auto`. All five gray areas auto-resolved to their recommended
> defaults, grounded in ROADMAP §Phase 53 (3 success criteria), REQUIREMENTS
> **CFG-01 / CFG-02 / OBS-04**, the v1.0.9 binding constraints (PROJECT.md /
> STATE.md), research `ARCHITECTURE.md` Q6 + `SUMMARY.md` §5 + `PITFALLS.md`
> §96 (the config-surface material 52-CONTEXT flagged as "Phase 53 material"
> — **read with the superseded-vocabulary caveat below**), Phase 51/52's
> locked decisions, and a live seam scout of `values.yaml` /
> `deployment.yaml` / `signing-secret.yaml` / `cmd/manager/main.go` /
> `dispatch_helpers.go` (the Phase-52 resolvers) / `dispatchVerifier` / the
> dashboard API + component family. One requirement phrasing (CFG-02's
> "enabled at the milestone+project scope") needed interpretation; the
> chosen reading is stated explicitly in D-03 so plan-phase can surface it
> if the operator disagrees. No Option-A-vs-B fork with "no existing source"
> met the bar for an interactive checkpoint under auto-advance.

<domain>
## Phase Boundary

**Close v1.0.9 with the configuration + display layer for the loop tier all
prior phases built** — two independent halves:

1. **CFG-01/02 (chart-first config):** Operators configure the verify/loop
   tier (evaluator image + model, per-level `LoopPolicy` defaults, per-level
   enablement) through `charts/tide/values.yaml` → manager Deployment env →
   a Helm-defaults tier consumed by the Phase-52 resolvers — the exact
   `subagent.levels` → `TIDE_DEFAULT_MODEL_*` → `ProviderDefaults` →
   `resolveImage`/`ResolveProvider` flow, extended. `values.yaml` remains
   the FIXED contract (binary catches up to chart, never reverse). The
   default posture is **bounded and least-surprising**: a fresh install gets
   the Task loop + milestone/project escalation verify on by default; an
   in-place `helm upgrade` leaves the entire tier off — **proven by an
   upgrade-path test**. (Without this phase, an in-place upgrade to v1.0.9
   silently starts spending the moment planners author verification
   contracts — the exact least-surprise violation CFG-02 exists to prevent.)
2. **OBS-04 (dashboard provenance):** The dashboard shows nested loop
   provenance (Project run → Task iteration → Execution attempt/tool spans)
   and renders `VerifyHalt` as a **visually distinct state from `Failed`**,
   with staged findings browsable through the **existing** gitfetch/artifacts
   API — **no new endpoint**, and no iteration history read from etcd
   (LOOP-03: history lives in traces/artifacts).

**Deliberately NOT in this phase:**

- **New loop mechanics.** Phases 49–52 shipped the contract, Task loop,
  per-level parameterization, and resolver. This phase configures and
  displays them; `ResolveLoopPolicy`'s structural clamps (maxIter=0 at
  phase/milestone/project, D-07) are NOT overridable from the chart.
- **Broadening the default posture toward phase/plan levels** — explicit
  future trigger in `research/FEATURES.md:202` (needs a real external run's
  cost + false-positive data first).
- **Dashboard mutation actions** (approve/resume from the dashboard) — the
  dashboard stays read-only; `VerifyHalt` recovery remains `tide resume`.
- **Integration-check as a distinct stage / composite evaluators /
  Product-System-Oversight loops** — named future arc.

Success = ROADMAP §Phase 53's three criteria: (1) chart-first config follows
the existing precedence chain, (2) fresh-install-on / upgrade-off proven by
an upgrade-path test, (3) nested provenance + distinct VerifyHalt rendering
+ findings browsable via the existing artifacts API.

**⚠ Superseded-vocabulary caveat (load-bearing for research):** research
`ARCHITECTURE.md` Q6 / `SUMMARY.md` §5 propose a
`verify.stages.{planCheck,levelVerify,integrationCheck}` chart shape. That
three-stage vocabulary **pre-dates the five-loop reframe** — Phase 52
shipped ONE verification contract parameterized per level
(`ResolveLoopPolicy`, keyed on `LoopPolicy.Level`). The chart surface is
therefore **per-LEVEL** (mirroring `subagent.levels`), never per-stage. Use
Q6 for the precedence/posture reasoning, not its literal YAML.

</domain>

<decisions>
## Implementation Decisions

### Chart config shape + env flow (CFG-01) (auto-resolved)

- **D-01 (a `subagent.verify` block, per-LEVEL, riding the existing env →
  defaults flow):** Extend the existing `subagent:` block
  (`values.yaml:252`) with a `verify:` sub-block — `image` + `model`
  scalars plus a per-level map keyed `task|plan|phase|milestone|project`
  carrying `enabled` + `LoopPolicy` defaults (`maxIterations`,
  `onExhaustion`; exact field set is Claude's discretion within the
  authored-schema fields that exist on `VerificationSpec`). Flow:
  `values.yaml` → manager Deployment env (deployment.yaml's existing
  documented env block) → a `VerifyDefaults`-style struct on the reconciler
  Deps (mirroring `HelmProviderDefaults`, `dispatch_helpers.go:142-212`) →
  consumed by the Phase-52 resolvers. Env encoding: the two scalars ride
  the existing vars (`TIDE_VERIFIER_IMAGE` — already read at
  `cmd/manager/main.go:213-219` with a compiled default; the chart finally
  supplies it — plus a new verifier-model var); the per-level map rides
  **one structured JSON env var** (the `TIDE_PRICING_OVERRIDES_JSON`
  precedent for structured config — 15 discrete `TIDE_VERIFY_*` vars would
  bloat the Deployment template for no benefit). *Rejected: a top-level
  `verification:` values block (the tier is subagent dispatch config —
  `subagent.verify` sits beside the `subagent.levels` chain CFG-01 names);
  the Q6 per-stage shape (superseded vocabulary, see caveat above).*
- **D-02 (evaluator image/model precedence = authored `Evaluator` >
  chart default > compiled default):** `VerificationSpec.Evaluator`
  (already resolved through the Task > Plan > Project walk and carried on
  `LoopPolicy.EvaluatorRef`) is the per-Project/per-level override tier;
  the chart's `subagent.verify.image`/`model` is the Helm-default tier;
  the compiled-in defaults (`main.go:219`; verifier model currently
  inherits the task-level executor model at `task_controller.go:2331-2332`)
  are the last resort. This is the `resolveImage` chain shape applied to
  the verifier — one resolution helper, no per-site precedence walks.
  Dedicated verifier-model default replaces the "borrow the task executor
  model" fallback only when the chart/env supplies one; absent config keeps
  today's behavior.

### Default posture semantics (CFG-02) (auto-resolved)

- **D-03 (fresh-install default posture — the locked interpretation):**
  CFG-02's "Task-loop auto-repair + Plan/Milestone/Project escalation
  enabled at the **milestone+project scope**" is read as: **task ON
  (auto-repair, the milestone's headline feature works out of the box),
  milestone ON + project ON (escalation verify at the level boundaries —
  the 2026-07-03 bug class), plan OFF and phase OFF by default** (matching
  research SUMMARY §5's "plan-check off by default" and PITFALLS §96's
  narrowest-useful-scope argument; CFG-02's own list omits phase).
  Fresh-install boundedness comes from the existing structural rails, not
  from disabling the task tier: `defaultVerifierConcurrencyCap` (2),
  `LoopPolicy.BudgetCents` + the ReservationStore, `maxIterations`, and
  `onExhaustion: requireApproval` as the human backstop. *Rejected: task
  OFF for fresh installs (PITFALLS §96's literal "off at task tier" —
  it pre-dates the five-loop reframe; ROADMAP SC2 and CFG-02 both
  explicitly name "Task-loop auto-repair" as enabled on fresh installs,
  and requirement text is authoritative over research).* The
  interpretation is called out here precisely so a human can veto it
  cheaply at plan review.
- **D-04 (enablement gates DISPATCH, not authoring — one chokepoint):**
  The per-level `enabled` posture is enforced at the verifier dispatch
  sites via one shared helper beside `ResolveVerificationSpec` (the same
  seam that already gates on `GateCommand != ""`), NOT by suppressing
  planner contract-authoring. Planners keep authoring verification
  contracts unconditionally; a disabled level behaves exactly like
  today's no-contract path (level stamps through without verify — zero
  spend), and flipping posture on later activates already-authored
  contracts without re-planning. Precedence per level: authored
  Project-scope explicit config wins over chart default (a level whose
  `Project.Spec.Verification.<level>` entry is authored is explicitly ON
  — operator intent on the CR outranks the install default), else chart
  per-level `enabled`, else off. *Rejected: gating contract authoring in
  the planner templates — splits the off-switch across two surfaces and
  makes posture flips require re-planning.*

### Install-vs-upgrade differentiation (CFG-02) (auto-resolved)

- **D-05 (sticky install-time posture via the signing-secret `lookup` +
  `resource-policy: keep` idiom):** A plain values default cannot satisfy
  CFG-02 (a single default is either on-for-both or off-for-both), and
  bare `.Release.IsInstall` flips a fresh install's posture OFF on its
  first `helm upgrade`. Use the chart's own established mechanism for
  install-time state (`signing-secret.yaml:1` — `lookup` + if-not guard +
  `resource-policy: keep`): on fresh install, create a small posture
  marker (ConfigMap) recording the enabled default; on upgrade, `lookup`
  finds it (fresh-install lineage → stays on) or doesn't (pre-existing
  install → tier off). An explicit values override
  (`enabled|disabled|auto`, default `auto`) always wins over the marker,
  so operators can force either posture from values alone. *Rejected:
  values-default off + documented `--set` at install (research Q6's
  ServiceMonitor-pattern suggestion — fails ROADMAP SC2's "a fresh
  install gets [it] by default"); bare IsInstall (posture flip-flop).*
- **D-06 (the upgrade-path test = helm-template render pair + kind
  upgrade proof):** Prove CFG-02 at two layers: (1) a helm-template
  contract test (the `agent_identity_chart_test.go` /
  `baseref_crd_render_test.go` precedent in `test/integration/kind/`)
  rendering the chart plain (install posture → verify env ON) and with
  `--is-upgrade` (no marker lookup possible → verify env OFF); (2) a kind
  test exercising the sticky path — `helm install` → assert on →
  `helm upgrade` → assert STILL on (marker survived), and upgrade-onto-
  pre-existing-install → assert off. Research question flagged for the
  researcher: `helm template` runs with `IsInstall=true` and a nil
  `lookup` — confirm the template degrades safely under CI render, the
  goreleaser/helmify-verify release path, and `tide`'s own install docs.

### Nested loop provenance on the dashboard (OBS-04) (auto-resolved)

- **D-07 (provenance = current LoopStatus summary from CRD + findings via
  the artifacts API + Phoenix deep-link for spans — no new endpoint, no
  etcd history):** Extend the existing task payload
  (`cmd/dashboard/api/tasks.go:79-180` — already carries
  `attempt`/`attemptMax`) with the loop-provenance summary the CRDs
  already hold: `LoopStatus.Iteration`/`ExitReason`,
  `LastEvaluation.{Decision,FindingsCount,HighSeverityCount}`, and the
  derived `loopRunID`/`attemptID` identity — current-iteration summary
  ONLY (LOOP-03: no history array exists to render). Per-iteration
  drill-down rides the two stores that DO hold history: staged findings
  per attempt on the run branch through the **existing**
  `GET /api/v1/nodes/{kind}/{name}/artifacts` gitfetch endpoint (Phase 49
  task-findings staging — `findings.json` browsable in the existing
  `ArtifactViewer`), and execution attempt/tool spans through a
  Phoenix deep-link (the `PhoenixTraceLink`/`phoenixLink.ts` precedent —
  the v1.0.8 trace tree already nests Project run → level →
  attempt → per-call LLM spans with `loop.*` attributes; Phoenix is the
  span-level provenance surface, the dashboard links into it). Note
  `attemptMax` currently derives from `Caps.Iterations`
  (`tasks.go:135-138`) — the loop-provenance display must key off
  `spec.verification.maxIterations`, not conflate the two (infra-caps ≠
  quality-iterations, the Phase-51 D-05 distinction surfacing in the UI).
  *Rejected: a new provenance endpoint (OBS-04 says no new endpoint); a
  full iteration-history timeline from CRD status (LOOP-03 violation —
  the data does not exist in etcd, by design).*
- **D-08 (surface = extend TaskDetailDrawer + NodeDetailPanel, not a new
  top-level view):** The nested provenance renders as a loop section in
  the existing detail surfaces (`TaskDetailDrawer.tsx` — iteration
  counter, verdict summary chips, findings link, Phoenix link; plan-check
  provenance mirrors it on the Plan detail surface since Phase 52
  embedded `LoopStatus` in `PlanStatus`). The DAG node shells
  (`TideNodeShell`) show the state vocabulary (D-09); the drawer carries
  the depth. ROADMAP's `UI hint: yes` noted — the planner may pull
  `/gsd:ui-phase 53` for a UI-SPEC if the drawer section grows beyond a
  straightforward extension; per the Opus-4.8 tuning note, any generated
  restyle must specify the existing dev-tool palette, not drift to model
  defaults.

### VerifyHalt visual vocabulary (OBS-04) (auto-resolved)

- **D-09 (VerifyHalt = blocked-family condition + two new phase rows,
  distinct from Failed at every surface):**
  - **Server:** `cmd/dashboard/api/projects.go:386` currently filters
    blocking conditions to `BudgetBlocked`/`BillingHalt` only — add
    `ConditionVerifyHalt` (`shared_types.go:371`) to the filter. The wire
    shape is an open string; the frontend renders unknown types as
    nothing, so server + table land together.
  - **ConditionBadge:** new `VerifyHalt` row in `CONDITION_TABLE`
    (`ConditionBadge.tsx`) — blocked-family color
    (`--color-status-blocked`: policy-halted, operator-recoverable — the
    same family as BillingHalt and deliberately NOT the Failed/error
    color), distinct icon + label ("Verify halted"), SR description
    naming the `tide resume` recovery path (mirroring BillingHalt's row;
    the 14-UI-SPEC §C1 vocabulary table gains a row, not a divergence).
  - **StatusBadge:** the 11-value phase union (`StatusBadge.tsx`) gains
    `Verifying` (running-family activity treatment) and `VerifyHalted`
    (blocked-family, visually distinct from `Failed` by color family +
    glyph + label) — today both new Phase-51/52 phases fall through the
    unknown-value fallback. `TideNodeShell`/DAG nodes inherit via
    `STATUS_TABLE`.
  - A component test asserts `VerifyHalted`'s presentation differs from
    `Failed`'s (color token + label + icon) — the OBS-04 "visually
    distinct" criterion as an executable check.

### Cross-cutting (verification hygiene — the v1.0.8 release lesson)

- **D-10 (run the ci.yaml-only gates inside this phase's verification):**
  This phase touches BOTH surfaces the release-cascade lesson names:
  chart edits (`examples_image_pin_test`, helm-template contract tests,
  the mandatory chart `appVersion` discipline at release) and dashboard
  SPA edits (`make verify-dashboard-freshness` — the embedded
  `cmd/dashboard/embed/dist` must be regenerated with the SPA change or
  the Phase-22 gate trips). Phase verification must run `make lint`,
  `make verify-dashboard-freshness`, and the kind chart/pin tests — not
  wait for release pre-flight to catch them (STATE.md Blockers, v1.0.8
  lesson).

### Claude's Discretion

- Exact values-key spellings under `subagent.verify`, the JSON env var
  name/schema for per-level defaults, and the `VerifyDefaults` Go struct
  shape/home — within D-01.
- The posture-marker ConfigMap name/shape and the override value
  vocabulary — within D-05.
- The verifier-model env var name and whether the model default lands in
  `ProviderDefaults.Models` under a dedicated key vs a sibling field —
  within D-02.
- Icon choices and exact labels for VerifyHalt/Verifying/VerifyHalted rows
  — within D-09's color-family + distinct-from-Failed constraints.
- Whether plan-check provenance gets its own drawer section this phase or
  a minimal iteration/verdict line — within D-07/D-08 (Task is the
  required depth; Plan must at least surface its LoopStatus summary).
- Whether the observed `FailureHalt` dashboard gap (see Deferred) is
  trivially closed in the same D-09 table edit or left deferred — it is
  NOT required by OBS-04.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (scope authority)
- `.planning/REQUIREMENTS.md` — **CFG-01** (chart-first surface, existing
  precedence chain, values.yaml FIXED), **CFG-02** (bounded default
  posture, fresh-install on / in-place-upgrade off), **OBS-04** (nested
  provenance, VerifyHalt distinct from Failed, findings via existing
  gitfetch/artifacts API, no new endpoint).
- `.planning/ROADMAP.md` §"Phase 53" — the 3 success criteria this CONTEXT
  locks against (`UI hint: yes`).

### Research (config surface + posture — superseded-vocabulary caveat applies)
- `.planning/research/ARCHITECTURE.md` Q6 (lines ~200-260) — the chart-first
  `subagent.verify` direction, posture options table, precedence-chain
  reasoning. **Caveat:** its per-stage YAML
  (`stages.{planCheck,levelVerify,integrationCheck}`) is superseded by the
  five-loop reframe — the surface is per-LEVEL (`LoopPolicy` defaults).
- `.planning/research/SUMMARY.md` §5 — "chart-first `subagent.verify`
  block… default posture milestone+project-only… full off for existing
  installs upgrading in-place" (D-03/D-05's grounding).
- `.planning/research/PITFALLS.md` §96 — the cost-multiplier argument for a
  narrow default (D-03 adopts its plan/phase-off, overrides its task-off
  per requirement text).
- `.planning/research/FEATURES.md` :202 — the explicit future trigger for
  broadening the posture (kept OUT of this phase).
- `.planning/notes/five-loop-model.md` — the reframe that supersedes the
  three-stage vocabulary.

### Prior-phase hand-offs (the machinery this phase configures/displays)
- `.planning/phases/52-per-level-looppolicy-parameterization/52-CONTEXT.md`
  — D-01 (per-level schema/precedence), D-02 (`ResolveLoopPolicy`), D-07
  (maxIter=0 clamp — chart must NOT override), D-08 (requireApproval vs
  escalate), and its `<deferred>` routing chart + dashboard HERE.
- `.planning/phases/51-the-task-loop/51-CONTEXT.md` — D-01
  (`TaskSpec.verification`), D-02 (`"langgraph"` vendor), D-10
  (verifier concurrency cap), the VerifierImage wiring history.
- `.planning/phases/50-execution-loop-hardening-loop-native-observability/50-CONTEXT.md`
  — `loop.*`/`evaluation.*` span keys + derived `loopRunID`/`attemptID`
  (D-07's identity fields), LOOP-03 (no history in etcd — the provenance
  display constraint).

### Chart + manager seams (CFG — source of truth, read before coding)
- `charts/tide/values.yaml` :222-264 (`subagent.defaults`/`levels` block
  D-01 extends; the documented resolution-chain comment), :283-288
  (`signingKey` — the lookup idiom's values side).
- `charts/tide/templates/deployment.yaml` :52-63 (the env block where
  `CLAUDE_SUBAGENT_IMAGE`/`TIDE_DEFAULT_MODEL_*` flow; D-01's new envs land
  beside them).
- `charts/tide/templates/signing-secret.yaml` — the `lookup` + if-not +
  `resource-policy: keep` first-install idiom D-05 reuses.
- `cmd/manager/main.go` :186-262 — the env → `ProviderDefaults` wiring
  (`tideHelmProviderDefaults`), `TIDE_VERIFIER_IMAGE` read (:213-219,
  compiled default the chart catches up to), the flag-override precedence
  comment.
- `internal/controller/dispatch_helpers.go` :142-212 (`ProviderDefaults` +
  Deps — the `VerifyDefaults` mirror site), :356-486
  (`projectLevelVerificationDefault` / `resolveAuthoredVerification` /
  `ResolveVerificationSpec` / `ResolveLoopPolicy` — where the chart tier
  layers in), :561-610 (`resolveImage` — the chain CFG-01 names).
- `internal/controller/task_controller.go` :2160-2260 (`dispatchVerifier` —
  the dispatch site D-04's enablement gate fronts; the `VerifierImage`
  empty-skip precedent), :2325-2335 (verifier Provider: `Vendor:
  "langgraph"`, model borrowed from task-level executor — D-02 replaces).
- `test/integration/kind/agent_identity_chart_test.go` +
  `baseref_crd_render_test.go` — the helm-template contract-test precedent
  D-06's render pair follows; `verifier_concurrency_test.go` +
  `level_verify_worktree_test.go` — the verify-tier kind precedents.

### Dashboard seams (OBS-04 — source of truth, read before coding)
- `api/v1alpha3/shared_types.go` :345-371 (`ConditionVerifyHalt`),
  :500-536 (`LevelPhaseVerifying`/`LevelPhaseVerifyHalted` — the exact
  strings the frontend vocabulary adds).
- `cmd/dashboard/api/projects.go` :365-390 (the blockingConditions filter
  D-09 extends), `cmd/dashboard/api/tasks.go` :79-180 (the task payload
  D-07 extends; the `attemptMax`-from-`Caps.Iterations` conflation to keep
  distinct), `cmd/dashboard/api/artifacts.go` (the 37-07 gitfetch
  endpoint findings browse rides — unchanged).
- `dashboard/web/src/components/ConditionBadge.tsx` (`CONDITION_TABLE` —
  the locked §C1 vocabulary D-09 adds a row to),
  `StatusBadge.tsx` (the 11-phase union + `STATUS_TABLE`),
  `TideNodeShell.tsx`, `TaskDetailDrawer.tsx`, `NodeDetailPanel.tsx`
  (D-08's surfaces), `PhoenixTraceLink.tsx` +
  `dashboard/web/src/lib/phoenixLink.ts` (the deep-link precedent),
  `dashboard/web/src/lib/tasks.ts` :96-113 (the payload mirror).
- `Makefile` `verify-dashboard-freshness` + `cmd/dashboard/embed/`
  (Phase-22 gate — D-10: SPA edits must regenerate the embed).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **The whole `subagent.levels` → env → `ProviderDefaults` → resolver
  pipeline** — CFG-01 is a second instance of an existing, documented flow;
  no new plumbing pattern is invented.
- **`TIDE_VERIFIER_IMAGE` is already read** (`main.go:219`) with a
  compiled default and an empty-skip guard at the dispatch site — the
  chart side is the only missing piece of the image path.
- **`VerificationSpec.Evaluator` → `LoopPolicy.EvaluatorRef`** — the
  per-Project override tier for evaluator image/model already resolves
  through the Phase-52 walk; D-02 just adds the Helm tier beneath it.
- **`signing-secret.yaml`'s lookup+keep idiom** — the chart's proven
  install-vs-upgrade state mechanism D-05 reuses verbatim.
- **`ConditionBadge`/`StatusBadge` locked vocabulary tables** — additive
  rows with defensive unknown-type fallbacks; server + table can land in
  either order without breaking rendering.
- **`ArtifactViewer` + the 37-07 artifacts endpoint + `PhoenixTraceLink`**
  — the three display surfaces D-07 composes; zero new endpoints.
- **Helm-template contract tests in `test/integration/kind/`** — the
  upgrade-path render test has an exact precedent shape.

### Established Patterns
- **Chart is the FIXED contract; binary catches up** — this phase is the
  sanctioned moment `values.yaml` gains the verify surface; additive keys
  only, no edits to pre-existing keys.
- **Structured config rides one JSON env** (`TIDE_PRICING_OVERRIDES_JSON`)
  vs scalars ride discrete envs (`TIDE_DEFAULT_MODEL_*`) — D-01 follows
  both precedents by kind.
- **Absence of config is the off-switch** (Phase 52: no contract → no
  dispatch) — D-04's posture gate composes with, never replaces, the
  `GateCommand != ""` activation key.
- **Halt classes render as blocked-family, not error-family**
  (BudgetBlocked/BillingHalt precedent) — VerifyHalt joins the family,
  satisfying "distinct from Failed" by construction.
- **LOOP-03 / traces-not-etcd** — the provenance UI reads the
  current-iteration summary from status and links out (artifacts,
  Phoenix) for history; it never expects a history array.

### Integration Points
- **`charts/tide/`** — `values.yaml` verify block, `deployment.yaml` env,
  new posture-marker template (D-05).
- **`cmd/manager/main.go` + `internal/controller/dispatch_helpers.go`** —
  env reads, `VerifyDefaults` on Deps, the enablement helper + chart tier
  in the resolvers (D-01/D-02/D-04).
- **`internal/controller/task_controller.go` (+ the Phase-52 plan/level
  verifier dispatch sites)** — the enablement gate fronts every verifier
  dispatch (D-04).
- **`cmd/dashboard/api/{projects,tasks}.go` +
  `dashboard/web/src/{components,lib}/`** — condition filter, payload
  fields, badge rows, drawer sections (D-07/D-08/D-09).
- **`test/integration/kind/`** — the upgrade-path render pair + sticky
  kind test (D-06).

</code_context>

<specifics>
## Specific Ideas

- **The upgrade-path test is a success criterion, not a nice-to-have** —
  ROADMAP SC2 says "proven by an upgrade-path test"; D-06's render pair +
  sticky kind proof is the deliverable shape.
- **The chart must not be able to re-open D-07's clamp** — per-level chart
  defaults feed `maxIterations`/`onExhaustion` DEFAULTS, but
  phase/milestone/project maxIter stays clamped to 0 in `ResolveLoopPolicy`
  regardless of chart values; a test pins it.
- **`attemptMax` ≠ `maxIterations` in the UI** — the dashboard currently
  derives `attemptMax` from `Caps.Iterations`; loop provenance displays
  quality-iteration bounds from the verification contract, keeping the
  Phase-51 infra-vs-quality distinction visible to operators.
- **VerifyHalt's SR description names the recovery verb** (`tide resume`),
  mirroring BillingHalt's row — the badge teaches the fix.
- **CFG-02's phrasing was interpreted (D-03)** — task ON / plan OFF /
  phase OFF / milestone+project ON for fresh installs. If the operator
  intended task OFF by default, D-03 is the single decision to flip; the
  gate mechanics (D-04) and upgrade posture (D-05) are unaffected.

</specifics>

<deferred>
## Deferred Ideas

- **Broadening the default posture to phase/plan levels** — explicit
  future trigger (`research/FEATURES.md:202`): after ≥1 full external-repo
  run with the verify tier active and acceptable false-positive data.
- **`FailureHalt` missing from the dashboard's blockingConditions filter**
  (`projects.go:386` surfaces only BudgetBlocked/BillingHalt — observed
  during the scout). Not OBS-04 scope; either trivially closed in the
  D-09 table edit at planner discretion or captured as a todo.
- **Dashboard mutation actions for VerifyHalt recovery** (approve/resume
  buttons) — dashboard stays read-only (PROJECT.md Out of Scope).
- **Integration-check as a distinct rubric/stage beyond the project-level
  contract** — future arc (research Q7).
- **Per-Project fine-grained disable knob** (an explicit `enabled: false`
  on `Project.Spec.Verification.<level>` overriding an authored contract)
  — the chart + authored-contract precedence covers v1.0.9; add only when
  a real operator needs per-CR opt-out.

### Reviewed Todos (not folded)
The `--auto` ≥0.4 auto-fold default was overridden by the scope guardrail —
same disposition as Phases 50/51/52, both matches are keyword
false-positives against explicitly-deferred work:

- **`2026-07-03-signed-commits-verified-badge`** (score 0.9 — keyword
  false-positive on "tide/phase"; area "git") — GPG signing, formally
  tracked as SIGN-02/03/04 in Future Requirements, deferred by choice
  since v1.0.7; zero chart-config/dashboard overlap.
- **`cache-f1-direct-sdk-cross-pod-caching`** (score 0.6) — direct-SDK
  caching backend, explicitly deferred to vNext+ (STATE.md Pending Todos);
  unrelated to this phase. Prior decision respected over the auto-fold
  threshold.

</deferred>

---

*Phase: 53-chart-config-dashboard-provenance-surfacing*
*Context gathered: 2026-07-21*
