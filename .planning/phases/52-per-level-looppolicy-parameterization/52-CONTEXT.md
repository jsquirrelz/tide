# Phase 52: Per-Level LoopPolicy Parameterization - Context

**Gathered:** 2026-07-20
**Status:** Ready for planning

> **Mode:** `--auto`. All five gray areas auto-resolved to their recommended
> defaults, grounded in ROADMAP §Phase 52 (3 success criteria), REQUIREMENTS
> **ESC-01** (the sole requirement), the v1.0.9 binding constraints (PROJECT.md
> / STATE.md), the [five-loop model](../../notes/five-loop-model.md) §"v1.0.9
> cut" (the `maxIterations = 1 vs 0` unification), Phase 51's locked decisions
> (D-01's generalization path; the `OnExhaustion` per-value differentiation
> explicitly deferred HERE by `task_types.go:145`), and a live seam scout of
> `loop_types.go` / `task_types.go` / `plan_controller.go` / `level_status.go`
> / `verify_halt.go` / the template family. No genuinely-open Option-A-vs-B
> fork with "no existing source" surfaced — every call below extends a locked
> precedent, so none met the bar for an interactive checkpoint under auto-advance.

<domain>
## Phase Boundary

**The same verification contract runs at every level, purely as different
`LoopPolicy` parameterizations** (ESC-01) — this phase generalizes the
Phase-51 Task loop machinery upward; it invents no new loop concepts:

1. **Plan/plan-check** runs with `maxIterations:1` — its **own counter**
   (default 1, never shared with the Task loop's counter, never shared with
   planner infra-retry or wave-integration `Attempts`) — against a
   **goal-backward rubric** (goal alignment, file-touch plausibility,
   dependency correctness, verification derivability), with
   **severity-weighted stall detection** before escalating. A REPAIRABLE
   plan-check verdict drives ONE fresh planner attempt (re-plan) seeded with
   the findings; a stalled or exhausted loop escalates.
2. **Phase/Milestone/Project** run with `maxIterations:0` — any verify
   finding at these levels escalates straight to `requireApproval` rather
   than auto-repairing, because these levels close on their **observable
   outcome**, not task-completion (post-execution rework discards paid work).
3. **Gate policy is resolved from the loop-level field on `LoopPolicy`**, not
   from CRD kind/hierarchy position — **one resolver function serves all
   levels** (SC3). This includes finally differentiating
   `onExhaustion: escalate` vs `requireApproval` (Phase 51 shipped both
   values resolving identically to `ConditionVerifyHalt`; per-value
   differentiation was explicitly deferred to this phase).

**Deliberately NOT in this phase:**

- **Chart-first config surface + default posture** (evaluator image/model,
  per-level `LoopPolicy` defaults in `values.yaml`, off-on-in-place-upgrade)
  → **Phase 53** (CFG-01/02). This phase's CRD fields are the surface the
  chart later populates; no `values.yaml` edits here (FIXED contract).
- **Dashboard nested-provenance + `VerifyHalt` visual state** → **Phase 53**
  (OBS-04).
- **Composite evaluators / risk+confidence gate resolution** → named future
  arc (the Oversight loop resolves gate policy from level+risk+confidence+
  history; this phase resolves from **level** only).

Success = a Plan whose plan-check verdict is REPAIRABLE gets exactly one
findings-seeded re-plan then halts (stall detection can halt it earlier); a
Phase/Milestone/Project verify finding escalates straight to approval with
zero auto-repair; and one `ResolveLoopPolicy`-style function — keyed on the
loop-level field, not the CRD kind — produces every level's effective policy.

</domain>

<decisions>
## Implementation Decisions

### Per-level schema placement & resolution precedence (auto-resolved)

- **D-01 (schema lands on `Plan.Spec` + `Project.Spec` ONLY — the exact shape
  Phase 51's D-01 named):** Add `Verification VerificationSpec` to
  `Plan.Spec` (the plan-check contract) and a **per-level defaults map** to
  `Project.Spec` mirroring the `Gates` per-level shape
  (`project_types.go:39` — one optional `VerificationSpec` per level:
  `task`/`plan`/`phase`/`milestone`/`project`). **No `Phase.Spec` /
  `Milestone.Spec` verification fields** — those levels resolve from the
  Project map, exactly as `Gates` already covers them from Project scope.
  Resolution precedence: **Task > Plan > Project** (`ResolveProvider` /
  `Gates` precedent) — an explicit contract on the level's own spec wins,
  else the Project per-level default applies, else the stage is off (no
  contract → no verifier dispatch → today's behavior, which keeps Phase 53's
  off-on-upgrade posture trivially true). The same CEL
  immutable-once-Locked rule from `task_types.go` applies verbatim to the
  new `VerificationSpec` sites (it's the same standalone type).

### The loop-level field + one gate-policy resolver (auto-resolved)

- **D-02 (add `Level` to `LoopPolicy` + a single resolver function):**
  `LoopPolicy` (`api/v1alpha3/loop_types.go:37`) gains a `Level` field
  (enum: `task|plan|phase|milestone|project`) — SC3's "loop-level field on
  `LoopPolicy`" is literal. One resolver (shape:
  `ResolveLoopPolicy(project, plan, task, level) → effective LoopPolicy`,
  exact signature Claude's discretion) maps level → default parameterization
  — task: `maxIterations:N` (from the contract, auto-repair), plan:
  `maxIterations:1`, phase/milestone/project: `maxIterations:0` — then
  merges the explicitly-authored `VerificationSpec`/`LoopPolicy` values over
  those defaults. **All five controllers call this one function**; none of
  them switch on CRD kind to pick gate policy. The existing Task-loop call
  sites migrate onto it (no behavior change for Task at default settings —
  proven by the existing 51-suite staying green).

### Plan-check loop mechanics (auto-resolved)

- **D-03 (trigger = Plan `Verifying` sub-state after child materialization;
  Wave-1 task dispatch gates on APPROVED):** Plan-check dispatches when the
  planner Job has completed AND the reporter has finished materializing
  child Tasks (`plan_controller.go:866` seam) — the rubric's
  dependency-correctness / verification-derivability checks need the
  authored child specs to exist. The Plan enters a `Verifying` phase
  (mirroring Task's `LevelPhaseVerifying`, generalized or a sibling
  constant — Claude's discretion) and **no child Task dispatches until the
  plan-check verdict is APPROVED** — child CRDs existing is free; dispatch
  is what spends. This is the cheapest pre-spend rejection point (the
  research's "catches a bad plan before any task spends money").
- **D-04 (re-plan = ONE fresh planner attempt, same template + findings
  appended):** REPAIRABLE → re-dispatch the **same** `plan_planner.tmpl`
  (no new authoring template) with the verdict's `findings[]` appended as a
  compact evidence block (the plan-level analog of Task's D-04 evidence
  packet: original spec + bounded findings, never the prior planner's full
  context). The re-plan mints a fresh planner attempt (existing
  `tide-plan-<uid>-<attempt>` identity); superseded children from the
  rejected attempt must never dispatch (they are replaced/reconciled by the
  re-materialization — exact child-reconciliation mechanics are a research/
  planning question, but the invariant is fixed: **no stale-attempt Task
  ever dispatches**, which the Verifying gate already guarantees).
- **D-05 (severity-weighted stall detection, LOOP-03-compliant):** Before
  consuming a remaining iteration, compare the new verdict's severity-
  weighted finding score against the previous iteration's — derivable
  entirely from `LoopStatus.LastEvaluation` (`FindingsCount` +
  `HighSeverityCount` — current-iteration summary only, NO history array).
  If the score does not strictly decrease, halt immediately ("re-plan loop
  stalled") rather than burn remaining budget. Weighting scheme (e.g.
  high-severity counts dominating) is Claude's discretion; the structural
  requirement is that a non-improving re-plan exits early even when
  `maxIterations` would allow another attempt. With the default
  `maxIterations:1` the stall check is exercised when operators raise the
  bound — cover it with a `maxIterations:2` test.
- **D-06 (plan-check gets its OWN counter — `LoopStatus` embedded in
  `PlanStatus`):** Embed the shared `LoopStatus` in `Plan.Status`
  (`LoopStatus.Iteration` counts quality re-plans only), exactly as Task
  did (`task_types.go:330`). It is distinct from: the Task loop's counter,
  the planner's infra-retry attempt identity, and Phase-34's
  wave-integration `Attempts` (`plan_types.go:67`) — infra-retry ≠
  quality-iteration holds at plan level too (Phase 51 D-05's distinction,
  applied one level up). The `TestLoopStatus_NoForbiddenFields`-style
  compile-time guard extends to the new embedding.

### Level-verify at maxIterations:0 + onExhaustion differentiation (auto-resolved)

- **D-07 (trigger = after all children Succeeded, before the level stamps
  `Succeeded` / boundary push):** For Phase/Milestone/Project with a
  resolved verification contract, the verifier dispatches at the moment the
  level would otherwise stamp `Succeeded` (`level_status.go`
  `patch{Level}Succeeded` seam) — semantic verification of the observable
  outcome (gate command run for real in a worktree at the run-branch tip,
  declared artifacts exist), running alongside the existing mechanical
  merge-ancestry completeness gate. `maxIterations:0` means **any**
  non-APPROVED verdict escalates immediately — there is no repair branch at
  these levels, and the resolver (D-02) is what encodes that, not level-
  specific if-statements.
- **D-08 (per-value `onExhaustion` semantics — uniform across ALL levels,
  including Task):** This phase closes Phase 51's declared-but-uniform gap
  (`task_types.go:145`): `requireApproval` routes through the **existing
  human-gate machinery** (`AwaitingApproval` + `consumeApproveAndResume`,
  `level_status.go:128` — ESC-02's "enforces requireApproval through the
  existing gate machinery" is literal), parking the checked level for a
  `tide approve`-shaped resume; `escalate` fires the project-wide
  `ConditionVerifyHalt` (`verify_halt.go`, cleared by `tide resume` with
  the time-fence). The differentiation lives in ONE place (the exhaustion/
  escalation path fed by D-02's resolver), so Task inherits it in the same
  commit that gives Phase/Milestone/Project their semantics. ESC-03's
  invariant extends: neither exit reinterprets `Failed` wave semantics —
  regression coverage asserts siblings/conservative-profile propagation
  stay untouched at the newly-verified levels too.

### Per-level verifier prompts (auto-resolved)

- **D-09 (per-level `<level>_verifier.tmpl` files, existing loader, no new
  machinery):** Add `plan_verifier.tmpl` carrying the **goal-backward
  rubric** (goal alignment, file-touch plausibility, dependency
  correctness, verification derivability — ESC-01's four named dimensions)
  and `phase_verifier.tmpl` / `milestone_verifier.tmpl` /
  `project_verifier.tmpl` prompting observable-outcome verification
  (deliverables exist on the run branch, gate command exit honored,
  constraint paths untouched) — all behind the existing
  `LoadPromptTemplate("verifier", level)` convention
  (`prompt_templates.go`), rendered orchestrator-side (EVAL-04: no Python
  port). All four follow **coverage-not-conservatism**: emit a finding for
  every deviation with severity + confidence tags; the resolver/config
  decides what blocks. `task_verifier.tmpl` is untouched.

### Cross-cutting (falls out of Phase 51 — verify, don't rebuild)

- **D-10 (the Phase-51 safety rails apply to every new dispatch site
  as-is):** Per-level verifier dispatches count against the existing
  `verifierInFlightCount` cap and reserve through `BudgetCents`/the
  `ReservationStore` (ESC-04 machinery — same pool, no new counter);
  verdicts flow through the fail-closed `ClassifyVerdict` (empty/malformed
  → BLOCKED, never APPROVED); deterministic gate-command failure dominates
  the judge at every level; each new site emits the `EVALUATOR`-kind
  sibling span via the existing `synthesizeEvaluatorSpan` path with the
  level-appropriate `tide.dispatch.<level>.verify` name. The plan must
  include tests proving these hold at the NEW sites (a plan-check dispatch
  is counted + reserved + fail-closed), not re-implementations.

### Claude's Discretion

- Exact Go field names / JSON tags for the `Project.Spec` per-level
  verification map and `LoopPolicy.Level`; the resolver's signature and
  package home (`dispatch_helpers.go`'s `ResolveProvider` idiom vs a
  dedicated file) — within D-01/D-02.
- Whether Plan reuses `LevelPhaseVerifying` or mints a sibling constant
  (the constant is currently doc-commented Task-only) — within D-03.
- The exact severity-weighting scheme for stall detection — within D-05's
  strictly-decreasing requirement.
- Child-Task reconciliation mechanics on re-plan (supersede/delete/replace)
  — within D-04's no-stale-dispatch invariant.
- The `EVALUATOR`-span attribute details and `LoopStatus` embedding sites
  for phase/milestone/project statuses (embed only where a loop actually
  runs; `maxIterations:0` levels may need only `LastEvaluation` + exit
  surface) — within D-07/LOOP-03.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (scope authority)
- `.planning/REQUIREMENTS.md` — **ESC-01** (the sole requirement: per-level
  parameterization, plan-check rubric + own counter + severity-weighted
  stall detection, `maxIterations:0` escalation, gate policy from loop
  level). Also re-read ESC-02/03/04 (Complete — the machinery this phase
  generalizes, not re-opens) and the Out of Scope table ("Auto-repair at
  Phase/Milestone/Project level" is explicitly excluded).
- `.planning/ROADMAP.md` §"Phase 52" — the 3 success criteria this CONTEXT
  locks against; §"Phase 53" confirms chart config + dashboard are NOT here.

### Milestone framing (the unification this phase completes)
- `.planning/notes/five-loop-model.md` §"v1.0.9 'Slack Tide' cut" (line 81 —
  "my earlier plan-check-loops / level-verify-halts asymmetry was
  `maxIterations = 1 vs 0` under one contract") + §"Per-loop disposition"
  and §"Load-bearing rules" (line 85).
- `.planning/research/FEATURES.md` §"Bounded plan-check re-plan loop"
  (lines 90–110 — the re-plan pseudocode: same template, findings appended
  verbatim, issue-count-must-decrease stall halt; note its `REJECT` verdict
  vocabulary was superseded by Phase 49's APPROVED|REPAIRABLE|BLOCKED, and
  its `maxAttempts` default recommendation was superseded by the scoping
  decision N=1) and §"level-verify — post-execution, pre-Succeeded/pre-push"
  (the D-07 trigger/reads/executes table).
- `.planning/research/ARCHITECTURE.md` Q1 (stage dispatch edges), Q6/Q7
  (config surface + integration-check — **Phase 53 material**, read for
  boundary awareness only). Caveat: this research pre-dates the five-loop
  reframe — where it says "three stages", the requirement is ONE contract
  parameterized per level.

### Prior-phase hand-offs (the locked decisions this phase extends)
- `.planning/phases/51-the-task-loop/51-CONTEXT.md` — D-01 (the
  generalization path this phase walks: `Plan.Spec`/`Project.Spec`, Task >
  Plan > Project), D-04 (evidence-packet shape D-04-here mirrors), D-05
  (infra-retry ≠ quality-iteration — applied at plan level by D-06-here),
  D-08/D-09/D-10 (anti-gaming, VerifyHalt, concurrency — machinery reused
  as-is), and its `<deferred>` routing per-level verification HERE.
- `.planning/phases/49-common-loop-contract-verdict-envelope-persistence-schema/49-CONTEXT.md`
  — `LoopPolicy`/`LoopStatus` design intent (shared types embedded in
  domain CRDs; LOOP-03 no-history rule the D-05/D-06 state model obeys).

### The seams this phase edits (source of truth — read before coding)
- `api/v1alpha3/loop_types.go:37,92,141` — `LoopPolicy` (D-02 adds `Level`),
  `LoopStatus` (D-06 embeds in `PlanStatus`), `EvaluationSummary`
  (`FindingsCount`/`HighSeverityCount` — D-05's stall inputs).
- `api/v1alpha3/task_types.go:68-150,213-219,324-330` — `VerificationSpec`
  (the standalone type D-01 re-embeds; its CEL immutability rule; the
  `OnExhaustion` doc comment at `:145` explicitly deferring per-value
  differentiation to THIS phase), `TaskSpec.Verification`,
  `TaskStatus.LoopStatus` (the embedding precedent).
- `api/v1alpha3/plan_types.go:56-85` — `PlanStatus` (D-06 embedding site;
  the wave-integration `Attempts` counter D-06 stays distinct from) +
  `Plan.Spec` (D-01 adds `Verification`).
- `api/v1alpha3/project_types.go:39-52,400-402` — `Gates` (the per-level
  map shape D-01's Project defaults mirror) + `GatePolicy`
  (auto|approve|pause — the existing human-gate vocabulary D-08's
  `requireApproval` routes through).
- `api/v1alpha3/shared_types.go:337-380,468-511` — the VerifyHalt condition
  vocabulary + `LevelPhaseVerifying`/`LevelPhaseVerifyHalted` (both
  doc-commented "Task-only" — D-03/D-07 generalize or sibling them) +
  `LevelPhaseAwaitingApproval` (D-08's requireApproval target).
- `internal/controller/task_controller.go:391-397,650-700,1288-1533,2099` —
  `checkVerifyingState` / `dispatchVerifier` / `handleVerifierCompletion` /
  `repairOrHalt` — the Task-loop state machine D-03/D-07 generalize from
  (factor shared helpers, don't fork — the `depgraph.go` shared-resolver
  precedent).
- `internal/controller/plan_controller.go:236,507,866,927` —
  `reconcilePlannerDispatch` / `handlePlannerJobCompletion` / the
  "reporter still materializing Task children" seam (D-03's trigger) /
  `patchPlanSucceeded`.
- `internal/controller/level_status.go:48-128,169` — `patchLevelStatus` +
  `consumeApproveAndResume` (D-07's pre-Succeeded hook + D-08's approval
  machinery; note its D-04 invariant: the helper never advances to
  Succeeded).
- `internal/controller/verify_halt.go` + `internal/controller/dispatch_helpers.go:283,480-580`
  — `checkVerifyHalt`/`setVerifyHaltIfNeeded` (D-08's escalate arm),
  `ResolveProvider` (D-01/D-02's resolver idiom), `plannerInFlightCount`/
  `checkDispatchHolds` (the uniform hold chain every new dispatch respects).
- `internal/subagent/common/prompt_templates.go` +
  `internal/subagent/common/templates/` — `LoadPromptTemplate(role, level)`
  + the six existing `.tmpl` files (D-09 adds four `<level>_verifier.tmpl`).
- `pkg/dispatch/verdict.go` + `pkg/dispatch/envelope.go:431-454` —
  `ClassifyVerdict` (fail-closed), `VerifyContext` (level-agnostic wire
  fields the new dispatch sites populate — check whether any field is
  Task-assuming before reuse).
- `docs/templates/minimal-loop-project/` — SLICE template ("closes on
  observable outcome, not task-completion" — D-07's semantic source) +
  `evals/README.md` integrity rules (deterministic dominates, at every level).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`VerificationSpec` (standalone type, `task_types.go:91`)** — designed in
  Phase 51 explicitly for re-embedding; D-01 adds two new homes with zero
  shape changes. Its CEL immutability rule travels with it.
- **The entire Task-loop state machine** (`checkVerifyingState`,
  `dispatchVerifier`, `handleVerifierCompletion`, `repairOrHalt`,
  `verify_halt.go`) — D-03/D-07/D-08 generalize these; the `depgraph.go`
  shared-fan-out precedent says factor a shared helper, not five forks.
- **`LoopStatus` + `TestLoopStatus_NoForbiddenFields` guard** — the D-06
  embedding + its compile-time no-history enforcement pattern.
- **`Gates` per-level map + `consumeApproveAndResume`** — D-01's Project-
  defaults shape and D-08's requireApproval machinery both already exist.
- **`verifierInFlightCount` + `ReservationStore` + `ClassifyVerdict` +
  `synthesizeEvaluatorSpan`** — D-10: every new dispatch site rides these
  unchanged.
- **`LoadPromptTemplate(role, level)`** — D-09 adds four templates with zero
  loader changes.

### Established Patterns
- **`ResolveProvider(project, level, defaults)` precedence chain** — the
  exact idiom D-02's `ResolveLoopPolicy` follows (explicit > Project default
  > off).
- **CEL `x-kubernetes-validations`, not webhooks** — D-01's new
  `VerificationSpec` sites carry the same immutability rule.
- **Halt classes are additive project-wide conditions with time-fenced
  resume** (BillingHalt → FailureHalt → VerifyHalt) — D-08's escalate arm
  reuses VerifyHalt; no fourth class is minted.
- **Infra-retry ≠ quality-iteration, grep-distinguishable** (Phase 51 D-05)
  — D-06 applies the same split at plan level (planner attempt identity vs
  `LoopStatus.Iteration`).
- **Resumption = re-derive, never persist history** (LOOP-03) — D-05's
  stall detection reads only `LastEvaluation`, D-06's counter is a summary.

### Integration Points
- **`api/v1alpha3/`** — `plan_types.go` (+`Verification`, +`LoopStatus`),
  `project_types.go` (+per-level verification map), `loop_types.go`
  (+`Level`), `shared_types.go` (Verifying/VerifyHalted doc-scope updates).
  `make manifests` CRD-YAML diffs are expected this phase (unlike 49).
- **`internal/controller/plan_controller.go`** — the plan-check loop:
  Verifying sub-state, verifier dispatch after child materialization,
  re-plan with findings, stall detection (D-03/D-04/D-05/D-06).
- **`internal/controller/{phase,milestone,project}_controller.go` +
  `level_status.go`** — the pre-Succeeded level-verify hook + escalation
  (D-07/D-08).
- **`internal/controller/` shared resolver home** — `ResolveLoopPolicy`
  (D-02) next to `ResolveProvider`.
- **`internal/subagent/common/templates/`** — four new verifier templates
  (D-09).

</code_context>

<specifics>
## Specific Ideas

- **The resolver is the phase's centerpiece (SC3):** one function, keyed on
  `LoopPolicy.Level`, produces every level's effective policy — grep should
  find NO controller switching on CRD kind to decide gate policy after this
  phase.
- **`maxIterations:1` means exactly one re-plan** — Phase 51's
  `repairOrHalt` counts the original attempt in `Attempt >= MaxIterations`;
  plan-check's counter semantics must be stated equally precisely (one
  authoring attempt + one re-plan, then escalate) and tested at the boundary.
- **Stall detection halts EARLY** — a non-improving re-plan exits before
  consuming remaining iterations; with default 1 it's latent, so the test
  raises the bound to 2 to prove it.
- **`requireApproval` parks the level; `escalate` freezes the project** —
  the per-value split Phase 51 deliberately shipped uniform. Task inherits
  the split in the same commit.
- **A level with no resolved contract dispatches no verifier** — absence of
  config is the off-switch; today's behavior is the default, which is what
  makes Phase 53's off-on-upgrade posture trivial.

</specifics>

<deferred>
## Deferred Ideas

- **Chart-first config surface** (per-level `LoopPolicy` defaults in
  `values.yaml`, evaluator image/model, `subagent.levels`/`resolveImage`
  precedence, new-install vs in-place-upgrade posture) → **Phase 53**
  (CFG-01/02). This phase's CRD fields are the API that chart populates.
- **Dashboard nested loop provenance + `VerifyHalt` visual state + staged
  findings browsing** → **Phase 53** (OBS-04).
- **Integration-check as a distinct cross-level stage** (research Q7 —
  build/test the whole run branch at milestone/project boundary) — insofar
  as it exceeds a `project`-level verification contract, it is Phase 53+/
  future; this phase ships the project-level contract only.
- **Gate policy from risk + confidence + historical performance** (beyond
  loop level) → Oversight-loop arc (five-loop model).
- **Composite evaluators** (schema conformance, security, diff-scope beyond
  deterministic + single LLM judge) → named future arc.

### Reviewed Todos (not folded)
- **`cache-f1-direct-sdk-cross-pod-caching`** (score 0.4 — keyword
  false-positive on "phase") — explicitly deferred to vNext+ (STATE.md
  Pending Todos); reviewed-not-folded in Phases 50 and 51 for the same
  reason; unrelated to per-level parameterization. Prior decision respected
  over the auto-fold threshold.
- **`2026-07-03-signed-commits-verified-badge`** (score 0.2, below
  threshold) — GPG signing scope, deferred by choice since v1.0.7.

</deferred>

---

*Phase: 52-per-level-looppolicy-parameterization*
*Context gathered: 2026-07-20*
