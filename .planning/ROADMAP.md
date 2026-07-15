# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** — Phases 1–11 (shipped 2026-06-11) — ⚠ shipped on an invalid execution foundation (per-plan waves; see v1.0.2 Spring Tide)
- ✅ **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** — Phases 12–17 (shipped 2026-06-13) — ⚠ same invalid foundation
- ⊘ **v1.0.2 — Ebb Tide: Token & Cost Optimization** — Phases 18–21 (completed; **SUPERSEDED — will not be released**, artifacts preserved). Superseded after dogfood run #2 surfaced the per-plan-waves defect.
- ✅ **v1.0.2 — Spring Tide: Global Execution DAG (severe corrective patch)** — Phases 22–26 (complete; **shipped within the v1.0.3 tag**, not separately tagged). Re-architected execution to ONE global Execution DAG spanning the entire Project — the patch that makes the Topologically-Indexed paradigm real. Superseded Ebb Tide.
- ✅ **v1.0.3 — Spring Tide + Planning Resumption & Cost Resilience** — Phases 22–29 (shipped 2026-06-25, tag `v1.0.3`, published: 7 images + 2 OCI charts). Global Execution DAG end-to-end (22–26) + operator resumption tooling (27–29): budget-bypass resume correctness, plan-import core, and `tide` export/import-envelopes with a kind E2E acceptance gate.
- ✅ **v1.0.4 — tide-import image publish + release-matrix guardrail** — (shipped 2026-06-25, tag `v1.0.4`, published). Patch: publishes the `tide-import` image in the build-images matrix and adds the matrix↔chart image-coverage release gate.
- ✅ **v1.0.5 — Resumable Import: Partial-Tree Resume** — Phase 30 (shipped 2026-06-27, tag `v1.0.5`, published: 8 images + 2 OCI charts + 5 binaries @ 1.0.5, verified anon). adopt-complete + re-plan-incomplete: fixes the v1.0.3 import defect dogfood run #2 surfaced (incomplete-envelope nodes materialized as `Running`-with-no-envelope zombies → stall). Unblocked deferred dogfood run #2. Full archive: [milestones/v1.0.5-ROADMAP.md](milestones/v1.0.5-ROADMAP.md) · [milestones/v1.0.5-REQUIREMENTS.md](milestones/v1.0.5-REQUIREMENTS.md).
- ✅ **v1.0.6 — Adoption-Path Correctness & Dispatch Safety** — Phases 31–33 (shipped 2026-06-29, tag `v1.0.6`, published: 8 images + 2 OCI charts + 5 binaries @ 1.0.6, verified anon). Corrective patch closing the four code-level defects dogfood run #2b surfaced on the adoption path: D1+D2 lifecycle/cost seam (Phase 31), D3 dispatch concurrency cap (Phase 32), D4 planner failure semantics (Phase 33). Audit: tech_debt (13/13 reqs, 0 blockers). Full archive: [milestones/v1.0.6-ROADMAP.md](milestones/v1.0.6-ROADMAP.md) · [milestones/v1.0.6-REQUIREMENTS.md](milestones/v1.0.6-REQUIREMENTS.md) · [milestones/v1.0.6-MILESTONE-AUDIT.md](milestones/v1.0.6-MILESTONE-AUDIT.md).
- 🚧 **v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics** — Phases 34–38 + 40 (in progress; started 2026-07-03). Closes what the first external-repo run (2026-07-03) surfaced short of new subagent stages: the silent wave-parallel integration miss (run branch shipped incomplete yet stamped Complete), the 2.8× Claude-5 budget overcount, git ergonomics (baseRef, agent identity, promptFile), dashboard blind spots (artifact view at approve gates, project view, empty log drawer), the Prometheus setup step, and the v1.0.6 audit tech-debt carry. 23 requirements (INTEG/COST/BASE/SIGN/PROMPT/DASH/TELEM/DEBT), 100% mapped — SIGN-02/03/04 (GPG signing) descoped 2026-07-03 at Phase 36 discussion. **Phase 40 added 2026-07-06** (full API version-lifecycle turn; +7 CRANK requirements minted at plan time → 30 total).
- 📋 **vNext — Specialist Verify Tier + LangGraph Beachhead** — (scoped 2026-07-06 via /gsd:explore; picks up after v1.0.7) — plan-check / level-verify / integration-check stages on a read-only LangGraph specialist image; first rung of the evidence-gated successor-runtime ladder — [framing doc](milestones/vnext-specialist-verify-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **v1.x — LangGraph Authoring Migration (evidence-gated)** — (backlog; reframed 2026-07-06 from "Polyglot Subagent Runtimes: LangGraph Strategy") — planner roles migrate first, executor last, each rung gated on eval-harness evidence; endgame = CLI-deprecation decision + multi-provider via `init_chat_model`, dissolving the standalone OpenAI backend — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **vLater — Dogfood Run #2 (retarget pending)** — (original deliverable — TIDE builds the OpenAI backend — dissolved by multi-provider-via-LangGraph; new build target chosen at scoping; still gated on multi-node infrastructure) — archived Flood Tide phase details remain a starting point: [milestones/v1.0.7-floodtide-ROADMAP.md](milestones/v1.0.7-floodtide-ROADMAP.md)

## Phases

### 🚧 v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics (In Progress)

**Milestone Goal:** Make a second external-repo run trustworthy and reviewable — a pushed run branch provably contains every Succeeded task's work, the budget tally matches the provider console, git ergonomics (baseRef, agent identity, promptFile) work, the dashboard is a sufficient approve-gate review surface, telemetry setup is guided, and the v1.0.6 audit tech-debt is retired.

- [x] **Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA** - Every Succeeded task's worktree branch is provably merged into the run branch (final wave included), merges are serialized and idempotent, boundary push gates on git-verified completeness, and `status.git.lastPushedSHA` arms the force-with-lease fence
- [x] **Phase 35: Git Base Ref** - `spec.git.baseRef` bases a run on any branch/tag/SHA, unresolvable refs fail fast with a typed condition, and the resolved SHA is stamped in `status.git.baseSHA` across both API versions
- [x] **Phase 36: Signed Commits + Bot Identity** - *(descoped 2026-07-03: identity only)* TIDE agent identity (name/email) is uniformly configurable across all three commit sites via `spec.git.agentName`/`agentEmail` → chart → compiled-in default, with the tide-push hardcoded identity removed — GPG signing (SIGN-02/03/04) deferred out of v1.0.7 (completed 2026-07-08)
- [x] **Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States** - Operators review planning artifacts at approve gates, read the outcome prompt and settings, and always see honest log-drawer states — no more PVC reader pods
- [x] **Phase 38: Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry** - Claude 5 pricing rows land verified, `tide apply --prompt-file` works, the telemetry-setup nudge triple ships, and the v1.0.6 audit debt is closed (completed 2026-07-11)
- [ ] **Phase 40: Deprecate v1alpha1 API (Full Version-Lifecycle Turn)** - v1alpha3 becomes the sole served+storage version (carrying the `subagent.levels` semantic rename + ModelSelection drop), v1alpha1 AND v1alpha2 are removed end to end, the envelope contract decouples to `dispatch.tideproject.k8s/v1alpha1`, and the crank machinery (guard, tooling gates) generalizes for v1alpha4

<details>
<summary>✅ v1.0.0 — Self-Hosting MVP (Phases 1–11) — SHIPPED 2026-06-11</summary>

14 phase directories (11 planned + 02.1/02.2/04.1/10/11 inserted) · 137 plans · 965 commits · ~66k LOC Go. Six CRDs + layered-Kahn waves + pluggable subagent dispatch + gates/observability/dashboard/CLI + Helm distribution; release published (binaries, 7 images, 2 OCI charts).

Full archive: [milestones/v1.0.0-ROADMAP.md](milestones/v1.0.0-ROADMAP.md) · [milestones/v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md)

</details>

<details>
<summary>✅ v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion (Phases 12–17) — SHIPPED 2026-06-13</summary>

- [x] Phase 12: Gate Semantics + Reject/Resume (5/5 plans) — completed 2026-06-11
- [x] Phase 13: Dispatch Image Resolution + Provider Halt (7/7 plans) — completed 2026-06-11
- [x] Phase 14: Budget Enforcement + Pricing (7/7 plans) — completed 2026-06-12
- [x] Phase 15: Paper Cuts (7/7 plans) — completed 2026-06-12
- [x] Phase 16: Telemetry Completion (8/8 plans) — completed 2026-06-12
- [x] Phase 17: Tech Debt — Plan Label Backfill + Gate Hardening (4/4 plans) — completed 2026-06-13

38 plans · 46 tasks · 28/28 requirements satisfied (milestone audit: passed).

Full archive: [milestones/v1.0.1-ROADMAP.md](milestones/v1.0.1-ROADMAP.md) · [milestones/v1.0.1-REQUIREMENTS.md](milestones/v1.0.1-REQUIREMENTS.md) · [milestones/v1.0.1-MILESTONE-AUDIT.md](milestones/v1.0.1-MILESTONE-AUDIT.md)

</details>

<details>
<summary>⊘ v1.0.2 — Ebb Tide: Token & Cost Optimization (Phases 18–21) — COMPLETED but SUPERSEDED, will not be released</summary>

**Milestone Goal (as scoped):** Cut TIDE's per-run token spend without degrading output quality — the cost-reduction prep that makes a second TIDE-on-TIDE dogfood run affordable.

- [x] **Phase 18: Eval Harness** — Freeze a v1.0.1 baseline and build the quality gate before any template change (3/3 plans) — completed 2026-06-15
- [x] **Phase 19: Template Reorder + Token Minimization** — Reorder all five templates stable-prefix-first and trim non-essential boilerplate, gated by the harness (4/4 plans) — completed 2026-06-15
- [x] **Phase 20: SharedContext Injection + Cache Verification Spike** — Spike cross-pod cache scoping, then add SharedContext to grow the cacheable shared prefix (reframed to token-minimization-only per CACHE-01 verdict) (5/5 plans) — completed 2026-06-16
- [x] **Phase 21: Cost & Cache Observability** — Surface per-level token accounting and cache-hit metrics on the dashboard (2/2 plans) — Needs Review

Superseded after dogfood run #2 surfaced the per-plan-waves architecture defect. Token/cost + observability work is preserved and folds forward where it still applies; the CACHE-01 decision record lives in PROJECT.md. The detailed phase breakdown for 18–21 is archived in git history (this ROADMAP, pre-Spring-Tide revision) and the per-phase directories under `.planning/phases/` (cleared at v1.0.7 start; recoverable from git history).

</details>

<details>
<summary>✅ v1.0.2 — Spring Tide: Global Execution DAG (Phases 22–26) — COMPLETE, shipped within tag v1.0.3</summary>

**Milestone Goal:** Re-architect execution so waves are derived from ONE global Execution DAG spanning the entire Project (all milestones/phases/plans), assembled after planning completes — making the Topologically-Indexed paradigm real.

- [x] **Phase 22: Dashboard Embed Freshness Fix** (3/3 plans) — completed 2026-06-16
- [x] **Phase 23: Schema Migration + Cross-Scope Dependency Model** (5/5 plans) — completed 2026-06-16
- [x] **Phase 24: Global Wave Derivation Engine** (4/4 plans) — completed 2026-06-16
- [x] **Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption** (3/3 plans) — completed 2026-06-17
- [x] **Phase 26: Multi-Milestone Drive + Spec Conformance** (4/4 plans) — completed 2026-06-17

Full phase details archived in [milestones/v1.0.6-ROADMAP.md](milestones/v1.0.6-ROADMAP.md) (and [milestones/v1.0.5-ROADMAP.md](milestones/v1.0.5-ROADMAP.md)).

</details>

<details>
<summary>✅ v1.0.3 — Planning Resumption & Cost Resilience (Phases 27–29) — SHIPPED 2026-06-25, tag v1.0.3</summary>

**Milestone Goal:** Make interrupted or budget-halted TIDE runs cheaply resumable.

- [x] **Phase 27: Budget-Bypass Resume Correctness** (4/4 plans) — completed 2026-06-18
- [x] **Phase 28: Plan-Import Core** (5/5 plans) — completed 2026-06-18
- [x] **Phase 29: Operator Tooling + E2E** (5/5 plans) — completed 2026-06-22

Full phase details archived in [milestones/v1.0.6-ROADMAP.md](milestones/v1.0.6-ROADMAP.md) · audit: [milestones/v1.0.3-MILESTONE-AUDIT.md](milestones/v1.0.3-MILESTONE-AUDIT.md)

</details>

<details>
<summary>✅ v1.0.5 — Resumable Import: Partial-Tree Resume (Phase 30) — SHIPPED 2026-06-27, tag v1.0.5</summary>

- [x] **Phase 30: Resumable Import — Partial-Tree Resume** (3/3 plans) — completed 2026-06-27 — adopt-complete + re-plan-incomplete driven by shared `IsEnvelopeComplete`; Tier-c kind E2E drives a mixed partial import to `Project=Complete`

Full archive: [milestones/v1.0.5-ROADMAP.md](milestones/v1.0.5-ROADMAP.md) · [milestones/v1.0.5-REQUIREMENTS.md](milestones/v1.0.5-REQUIREMENTS.md)

</details>

<details>
<summary>✅ v1.0.6 — Adoption-Path Correctness & Dispatch Safety (Phases 31–33) — SHIPPED 2026-06-29, tag v1.0.6</summary>

**Milestone Goal:** Close the four code-level defects dogfood run #2b surfaced on the v1.0.5 import/adoption path — so a completing TIDE-on-TIDE run can be relaunched without spending blind or OOM'ing the node.

- [x] **Phase 31: D2+D1 — Adoption Lifecycle Seam** (3/3 plans) — completed 2026-06-28
- [x] **Phase 32: D3 — Dispatch Concurrency Cap** (2/2 plans) — completed 2026-06-29
- [x] **Phase 33: D4 — Planner Failure Semantics** (3/3 plans) — completed 2026-06-29

Full archive: [milestones/v1.0.6-ROADMAP.md](milestones/v1.0.6-ROADMAP.md) · [milestones/v1.0.6-REQUIREMENTS.md](milestones/v1.0.6-REQUIREMENTS.md) · [milestones/v1.0.6-MILESTONE-AUDIT.md](milestones/v1.0.6-MILESTONE-AUDIT.md)

</details>

<details>
<summary>📋 vNext — Specialist Verify Tier + LangGraph Beachhead (Scoped)</summary>

Scoped 2026-07-06 via /gsd:explore. Ships the verify tier of the lifecycle-subagent seed — plan-check (pre-dispatch, goal-backward), level-verify (gate command + deliverables + constraints), integration-check (cross-child E2E at milestone/project boundaries) — as a sixth template class dispatched on a **new read-only LangGraph specialist image** (envelope in/out, git read, bash, `with_structured_output` gate_decision; never commits or authors). plan-check REJECT drives a bounded re-plan loop (findings appended, ≤ N attempts) before `ConditionVerifyHalt`; post-execution BLOCKED halts for a human. The execution DAG stays static and derived — dynamism lives inside the pod and at lifecycle seams, never as runtime DAG mutation.

See [milestones/vnext-specialist-verify-MILESTONE.md](milestones/vnext-specialist-verify-MILESTONE.md) and [notes/langgraph-successor-runtime-strategy.md](notes/langgraph-successor-runtime-strategy.md).

</details>

<details>
<summary>📋 v1.x — LangGraph Authoring Migration, evidence-gated (Backlog; reframed from "Polyglot Subagent Runtimes")</summary>

Reframed 2026-07-06: the Python/LangGraph image is no longer just a second strategy — it is the **candidate successor runtime**. After the specialist beachhead ships, authoring roles migrate planner-first / executor-last, each rung gated on Phase-18 eval-harness evidence; the endgame is a CLI-deprecation decision plus multi-provider via `init_chat_model`, which dissolves the standalone OpenAI-backend build (its remnant: credproxy route-allowlist extension + pricing rows). The original framing doc's parity inventory and contract-conformance table remain the reference for this migration.

See [milestones/v1.x-polyglot-subagent-MILESTONE.md](milestones/v1.x-polyglot-subagent-MILESTONE.md) for parity inventory, contract-conformance table, and provider-firewall gap analysis; [notes/adk-v2-subagent-evaluation.md](notes/adk-v2-subagent-evaluation.md) for the ADK-Go rejection; [notes/langgraph-successor-runtime-strategy.md](notes/langgraph-successor-runtime-strategy.md) for the ladder.

</details>

## Phase Details

### Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA

**Goal**: A pushed run branch provably contains every Succeeded task's work — the wave-parallel integration step cannot silently drop a merge, boundary push is gated on completeness verified from git, and a run can no longer stamp `Complete` while a declared deliverable is missing from the branch. The mechanical, no-LLM degenerate case of the verify-stage seed.
**Depends on**: Nothing (first phase of milestone; headline. The former "before Phase 36's signing" sequencing constraint is void — signing was descoped 2026-07-03)
**Requirements**: INTEG-01, INTEG-02, INTEG-03, INTEG-04, INTEG-05
**Success Criteria** (what must be TRUE):

  1. Every Succeeded task's worktree branch has a merge commit reachable from the run branch — including tasks in a plan's final Kahn wave; a single-wave plan integrates its tasks (the `plan_controller.go:1192` last-wave skip is closed).
  2. Tasks still execute in parallel while run-branch merges are serialized and idempotent — a wave of 2+ parallel tasks integrates every branch (cumulative Succeeded-branch set), and a controller retry re-merges safely with no duplicate or dropped merges.
  3. A boundary push fires only when `git merge-base --is-ancestor` confirms every Succeeded task branch is integrated (always recomputed from git, never a cached verdict); on a miss the operator sees a sticky `integration-incomplete` condition instead of an incomplete run branch being pushed.
  4. After a successful boundary push, `status.git.lastPushedSHA` shows the push envelope's `HeadSHA` — arming the force-with-lease fence.
  5. A kind-suite regression test reproduces the 2-parallel-task final-wave integration miss (RED against the pre-fix code path) and locks the fix.

**Plans**: TBD

### Phase 35: Git Base Ref

**Goal**: Operators can base a run on any branch, tag, or SHA — unresolvable refs fail fast with a typed condition instead of a cryptic worktree-add failure, and the resolved base SHA is stamped in status across both API versions. The milestone's first CRD schema change; its chart bump batches with Phase 36's.
**Depends on**: Nothing (independent of Phase 34; sequenced before Phase 36 so the two CRD/chart changes batch into one chart version bump per the FIXED-contract rule)
**Requirements**: BASE-01, BASE-02, BASE-03
**Success Criteria** (what must be TRUE):

  1. Applying a Project with `spec.git.baseRef` set to a branch, tag, or SHA produces a run branched from that ref; a Project without the field keeps current default-HEAD behavior (no default marker in the CRD — absent means HEAD, one encoding).
  2. An unresolvable baseRef fails fast with a typed condition naming the bad ref (classify-don't-retry) — no retry loop, no cryptic worktree-add failure to decode.
  3. `status.git.baseSHA` shows the resolved base SHA on a running Project.
  4. The new spec/status fields exist in both API versions, survive v1alpha1⇄v1alpha2 conversion round-trip, and survive a `tide-crds` chart upgrade without silent pruning — locked by conversion and CRD upgrade-path tests.

**Plans**: TBD

### Phase 36: Signed Commits + Bot Identity

> **Descoped 2026-07-03 (discussion):** this phase delivers agent identity only (SIGN-01). GPG signing (SIGN-02/03/04) — the Secret ref, the gpg-shim/plumbing spike, key validation, and the Verified-badge docs — is deferred out of v1.0.7; see REQUIREMENTS.md Future Requirements and `.planning/phases/36-signed-commits-bot-identity/36-CONTEXT.md` for the preserved analysis (key-exposure options, spike framing).

**Goal**: The TIDE agent identity (name/email) is uniformly configurable across all three commit sites — harness, integrate, tide-push — via the precedence chain `spec.git.agentName`/`agentEmail` → chart value → compiled-in default, with the tide-push hardcoded identity removed. The bot→agent rename applies everywhere: env vars become `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` and the compiled-in default becomes `TIDE Agent <tide-agent@tideproject.k8s>`.
**Depends on**: Phase 35 (chart/CRD bumps batch into one version bump). The former Phase 34 dependency was signing-specific and no longer applies.
**Requirements**: SIGN-01
**Success Criteria** (what must be TRUE):

  1. Configuring the agent identity once (Project spec or chart value) changes the committer identity at all three commit sites — harness, integrate, tide-push — with Project spec taking precedence over the chart value, and the tide-push hardcoded `tideBotSignature()` removed.
  2. An unconfigured install commits as `TIDE Agent <tide-agent@tideproject.k8s>` at all three sites (one consistent compiled-in default; the `TIDE_BOT_*` env names are gone).
  3. The new `spec.git.agentName`/`agentEmail` CRD fields ride the same chart version bump as Phase 35's `baseRef` (FIXED-contract batching).

**Plans**: 4/4 plans complete

- [x] 36-01-PLAN.md
- [x] 36-02-PLAN.md
- [x] 36-03-PLAN.md
- [x] 36-04-PLAN.md

### Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States

**Goal**: The dashboard is a sufficient approve-gate review surface — operators read the planning artifacts a node produced, the project's outcome prompt and settings, and honest log-drawer states, without spinning up ad-hoc PVC reader pods. Three features sharing one read-only manager-API surface; git is the artifact transport — staged envelopes on the run branch read via gitfetch (reworded per 37-CONTEXT.md; PVC/git remain source of truth).
**Depends on**: Nothing (independent of Phases 34–36; sequenced last among the big items so the UI consumes a settled reporter ConfigMap contract, which this phase also delivers — DASH-02 lands before/with DASH-01)
**Requirements**: DASH-01, DASH-02, DASH-03, DASH-04
**Success Criteria** (what must be TRUE):

  1. Clicking any Planning DAG node shows the artifacts it produced, markdown-rendered (children JSON pretty-printed); on a gate-parked node the artifact renders beside the approve action — an approve decision needs no PVC reader pod.
  2. Planning artifacts are committed to the run branch under `.tide/planning/<kind>/<name>/` at reporter-materialization time and served through the manager's gitfetch store — full artifact visibility with no truncation anywhere in the pipeline (PVC/git stay source of truth). *(Reworded per 37-CONTEXT.md D-01..D-04 — git transport superseded the original ConfigMap display-cache design during phase discussion.)*
  3. The operator can read the outcome prompt and project settings in a dashboard project view.
  4. The log drawer always renders an explicit state — loading, streaming, or pod-gone — and is never silently empty.

**Plans**: 12/12 plans executed

- [x] 37-01-PLAN.md
- [x] 37-02-PLAN.md
- [x] 37-03-PLAN.md
- [x] 37-04-PLAN.md
- [x] 37-05-PLAN.md
- [x] 37-06-PLAN.md
- [x] 37-07-PLAN.md
- [x] 37-08-PLAN.md
- [x] 37-09-PLAN.md
- [x] 37-10-PLAN.md
- [x] 37-11-PLAN.md
- [x] 37-12-PLAN.md

**UI hint**: yes

### Phase 38: Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry

**Goal**: The order-free paper cuts land — the budget tally matches the provider console on Claude 5 family models, `tide apply` accepts a prompt file, telemetry setup is guided at all three surfaces (INSTALL.md, NOTES.txt, dashboard banner), and the v1.0.6 audit tech-debt is retired.
**Depends on**: Nothing (fully independent items; can interleave with earlier phases if useful)
**Requirements**: COST-01, COST-02, COST-03, PROMPT-01, TELEM-01, TELEM-02, TELEM-03, DEBT-01, DEBT-02, DEBT-03
**Research flag**: COST-03 requires one empirical check before the pricing rows ship — tee a credproxy request to observe which cache-write TTL the `claude` CLI dispatch surface uses (5m → 1.25× vs 1h → 2× write multiplier).
**Success Criteria** (what must be TRUE):

  1. A run on a Claude 5 family model (claude-fable-5, claude-opus-4-8, claude-sonnet-5, claude-haiku-4.5) tallies `BudgetStatus.CostSpentCents` at the real per-MTok rates (exact-ID lookup with `-YYYYMMDD` normalizer; cache-write multiplier set from the empirically verified CLI TTL), and an unknown-model most-expensive fallback is observable as a metric/condition — not only a GC'd pod log line.
  2. `tide apply --prompt-file <path>` inlines the file into `spec.outcomePrompt` — no CRD change; the ConfigMap-ref union stays a compatible later addition.
  3. An operator following INSTALL.md's enable-telemetry step (including the kube-prometheus-stack `release:` label fix) ends at a Prometheus Targets page showing TIDE scraped; installing with `prometheus.enabled=false` prints a NOTES.txt warning that run telemetry beyond budget is unavailable, and the dashboard shows a "telemetry disabled" banner distinguishing disabled-by-config from no-data.
  4. The project-level `PlannerRolledUpUID` stamp uses the hardened RetryOnConflict + optimistic-lock pattern (v1.0.6 audit W1), and the rendered chart configmap defaults `plannerConcurrency` to 4, matching values.yaml (W2).
  5. Heavy controller envtest specs run in the integration tier instead of the TEST-01 unit tier, with total spec count conserved across the split (no specs lost).

**Plans**: 7/7 plans complete

- [x] 38-01-PLAN.md
- [x] 38-02-PLAN.md
- [x] 38-03-PLAN.md
- [x] 38-04-PLAN.md
- [x] 38-05-PLAN.md
- [x] 38-06-PLAN.md
- [x] 38-07-PLAN.md

**UI hint**: yes

## Progress

**Execution Order:** 39 -> 34 → 35 → 36 → 37 → 38 (35, 37, 38 are independent and may interleave; 36 requires 34 + 35) · Phase 40 depends only on Phase 39 (complete) and is independent of 34–38 — but as a repo-wide breaking crank it should NOT interleave with another in-flight phase's execution.

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1–11 (see archive) | v1.0.0 | 137/137 | Complete | 2026-06-11 |
| 12–17 (see archive) | v1.0.1 | 38/38 | Complete | 2026-06-13 |
| 18–21 (superseded) | v1.0.2 (Ebb) | 14/14 | Complete (superseded) | 2026-06-16 |
| 22–26 (see archive) | v1.0.2 (Spring Tide) | 19/19 | Complete | 2026-06-17 |
| 27–29 (see archive) | v1.0.3 | 14/14 | Complete | 2026-06-22 |
| 30 (see archive) | v1.0.5 | 3/3 | Complete | 2026-06-27 |
| 31–33 (see archive) | v1.0.6 | 8/8 | Complete | 2026-06-29 |
| 39. Pre-flight Tech-Debt Hardening | v1.0.7 | 2/2 | Complete | 2026-07-04 |
| 34. Run Integrity — Integration-Miss Gate + lastPushedSHA | v1.0.7 | 6/6 | Complete    | 2026-07-08 |
| 35. Git Base Ref | v1.0.7 | 4/4 | Complete | 2026-07-08 |
| 36. Signed Commits + Bot Identity | v1.0.7 | 4/4 | Complete    | 2026-07-08 |
| 37. Dashboard Surfaces — Artifact View, Project View, Log-Drawer States | v1.0.7 | 12/12 | Complete|  |
| 38. Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry | v1.0.7 | 7/7 | Complete    | 2026-07-11 |
| 40. Deprecate v1alpha1 API (Full Version-Lifecycle Turn) | v1.0.7 | 7/7 | Complete    | 2026-07-12 |

### Phase 40: Deprecate v1alpha1 API (Full Version-Lifecycle Turn)

**Goal:** One full API-version crank: introduce v1alpha3 as the sole served+storage version for all 6 CRDs — carrying the folded `subagent.levels` semantic rename (DECIDED mapping in the 2026-07-03 todo) plus user-approved batchable schema fixes — then remove v1alpha1 AND v1alpha2 entirely (Go packages, CRD blocks, chart copies, scheme registrations, stale comments). Reinstall-only migration per Phase 23 D-09; SchemaRevision guard generalized as the permanent crank mechanism; owner-ref dual-accepts dropped; deep docs/samples sweep (INSTALL/gates/git-hosts/project-authoring examples are broken today); envelope contract decoupled to `dispatch.tideproject.k8s/v1alpha1`. Decisions locked in `40-CONTEXT.md`.
**Requirements**: CRANK-01, CRANK-02, CRANK-03, CRANK-04, CRANK-05, CRANK-06, CRANK-07
**Depends on:** Phase 39
**Plans:** 7/7 plans complete

Plans:
**Wave 1**

- [x] 40-01-PLAN.md — Introduce api/v1alpha3 (copy-and-reshape, D-10 ModelSelection drop, storage flip, regen) — wave 1
- [x] 40-02-PLAN.md — Envelope group decoupling to dispatch.tideproject.k8s/v1alpha1 (D-08) — wave 1

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 40-03-PLAN.md — Consumer migration crank: imports, webhooks, schemes, guard (D-04), owner-refs (D-05), fixtures — wave 2

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 40-04-PLAN.md — subagent.levels semantic rename via levelOverrideKey mapping (D-02/D-11) + resolved-model logging — wave 3
- [x] 40-05-PLAN.md — Remove api/v1alpha1 + api/v1alpha2, relocate dogfood test, harden verify-no-aggregates (D-12), regen single-version CRDs — wave 3
- [x] 40-06-PLAN.md — Docs & samples deep accuracy pass (D-06/D-12): migration chapter + levels-remap table, quickstarts, 12 samples — wave 3

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 40-07-PLAN.md — Closeout: verify-no-legacy-api-refs CI gate + full test-int phase gate — wave 4

### Phase 41: Refactoring Review — Non-Breaking Cleanup (12 items)

**Goal:** The 12-item operator-shared refactoring review lands as non-breaking cleanup: quick wins (typed Status.Phase constants, meta.IsStatusConditionTrue, stale scheme comment, dead code/fields, mojibake, test-helper unification) then structural extractions (shared dispatch-holds gate chain, PlannerDeps carrier, condition-polarity normalization, status-helper extraction, magic-literal centralization, log-style decision).
**Requirements**: REFAC-01, REFAC-02, REFAC-04, REFAC-05, REFAC-06, REFAC-07, REFAC-08, REFAC-09, REFAC-10, REFAC-11, REFAC-12 (REFAC-03 pre-satisfied by Phase 40 — IDs map 1:1 to seed item numbers; minted 2026-07-12 per D-08)
**Depends on:** Phase 40 (doing 40 first collapses the dual-version scaffolding items #1/#3 otherwise work around)
**Plans:** 9/9 plans complete

Plans:
**Wave 1**

- [x] 41-01-PLAN.md — Quick wins: meta.IsStatusConditionTrue swap, mojibake fix, AGENTS.md logging policy (items 2/5/12) — wave 1
- [x] 41-02-PLAN.md — Test-driver unification onto reconcileWithRetry + apierrors.IsConflict (item 6) — wave 1

**Wave 2** *(blocked on Wave 1)*

- [x] 41-03-PLAN.md — Dead code deletion: gateDispatch/ensureJob, SubagentImage fields, Wave pools (item 4) — wave 2

**Wave 3** *(blocked on Wave 2)*

- [x] 41-04-PLAN.md — Typed LevelPhase* Status.Phase constants + 117-site sweep (item 1) — wave 3

**Wave 4** *(blocked on Wave 3)*

- [x] 41-05-PLAN.md — checkDispatchHolds gate-chain extraction, one controller per task; Task divergence documented (item 7) — wave 4

**Wave 5** *(blocked on Wave 4)*

- [x] 41-06-PLAN.md — PlannerReconcilerDeps carrier across 4 planner reconcilers incl. Project (item 8) — wave 5

**Wave 6** *(blocked on Wave 5)*

- [x] 41-07-PLAN.md — Leaf status-helper extraction: patch*/consumeApproveAndResume/countChildren (item 10) — wave 6

**Wave 7** *(blocked on Wave 6)*

- [x] 41-08-PLAN.md — ConditionParentUnresolved polarity normalization, True==unresolved (item 9) — wave 7

**Wave 8** *(blocked on Wave 7)*

- [x] 41-09-PLAN.md — Magic literals: owner label keys, SharedPVCName plumb, endpoint/iterations consts (item 11) — wave 8
