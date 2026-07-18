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
- ✅ **v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics** — Phases 34–41 (shipped 2026-07-15, tag `v1.0.7`). Closed what the first external-repo run (2026-07-03) surfaced short of new subagent stages: the silent wave-parallel integration miss, the 2.8× Claude-5 budget overcount, git ergonomics (baseRef, agent identity, promptFile), dashboard blind spots (artifact view at approve gates, project view, log-drawer states), the Prometheus setup step, and the v1.0.6 audit tech-debt carry — plus the Phase 40 full API version-lifecycle crank (v1alpha3 sole version) and the Phase 41 12-item refactoring review. 44 requirements, 44 satisfied; audit tech_debt, 0 blockers. Full archive: [milestones/v1.0.7-ROADMAP.md](milestones/v1.0.7-ROADMAP.md) · [milestones/v1.0.7-REQUIREMENTS.md](milestones/v1.0.7-REQUIREMENTS.md) · [milestones/v1.0.7-MILESTONE-AUDIT.md](milestones/v1.0.7-MILESTONE-AUDIT.md).
- ✅ **v1.0.8 — Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix** — Phases 42–47 (shipped 2026-07-17, tag `v1.0.8`, published: 8 images + 2 OCI charts + 5 binaries, verified anon). TIDE runs are observable in a self-hosted Arize Phoenix — the Milestone→Phase→Plan→Task dispatch chain emits real OpenInference/OTel spans (dispatch-chain AGENT spans, full LLM message arrays, W3C traceparent propagation), a documented self-hosted Phoenix recipe wires the chart’s existing OTLP endpoint end-to-end, and a runtime-neutral adapter seam keeps the trace contract stable ahead of the LangGraph beachhead. Live PROOF-01: a 392-span five-level trace tree, human-signed-off. Full archive: [milestones/v1.0.8-ROADMAP.md](milestones/v1.0.8-ROADMAP.md) · [milestones/v1.0.8-REQUIREMENTS.md](milestones/v1.0.8-REQUIREMENTS.md).
- 📋 **vNext — Specialist Verify Tier + LangGraph Beachhead** — (scoped 2026-07-06 via /gsd:explore; picks up after v1.0.8) — plan-check / level-verify / integration-check stages on a read-only LangGraph specialist image; first rung of the evidence-gated successor-runtime ladder — [framing doc](milestones/vnext-specialist-verify-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **v1.x — LangGraph Authoring Migration (evidence-gated)** — (backlog; reframed 2026-07-06 from "Polyglot Subagent Runtimes: LangGraph Strategy") — planner roles migrate first, executor last, each rung gated on eval-harness evidence; endgame = CLI-deprecation decision + multi-provider via `init_chat_model`, dissolving the standalone OpenAI backend — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **vLater — Dogfood Run #2 (retarget pending)** — (original deliverable — TIDE builds the OpenAI backend — dissolved by multi-provider-via-LangGraph; new build target chosen at scoping; still gated on multi-node infrastructure) — archived Flood Tide phase details remain a starting point: [milestones/v1.0.7-floodtide-ROADMAP.md](milestones/v1.0.7-floodtide-ROADMAP.md)

## Phases

<details>
<summary>✅ v1.0.8 — Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix (Phases 42–47) — SHIPPED 2026-07-17, tag v1.0.8</summary>

**Milestone Goal:** TIDE runs are observable in a self-hosted Arize Phoenix — the Milestone→Phase→Plan→Task dispatch chain emits real OpenTelemetry spans with OpenInference attributes (including full LLM input/output message arrays), and a documented Phoenix self-host recipe wires the chart’s existing OTLP endpoint to consume them natively.

- [x] **Phase 42: Trace-Context Foundation + Planner-Level Span Emission** (5/5 plans) — completed 2026-07-16 — pure `pkg/otelai/tracecontext.go` + attribute-complete AGENT spans for the four planner levels
- [x] **Phase 43: Task-Level Parity + Trace-Context Propagation** (5/5 plans) — completed 2026-07-16 — Task dispatch span, W3C `traceparent` at both pod hops, per-level IDs in `.status.trace`
- [x] **Phase 44: LLM Message-Array Spans + D-O5 Redaction/Size Boundary** (5/5 plans) — completed 2026-07-17 — trace-only reporter turns `events.jsonl` into redacted, size-bounded LLM spans (the headline)
- [x] **Phase 45: Runtime-Neutral Adapter Seam** (2/2 plans) — completed 2026-07-17 — `SelfInstruments` capability flag + skip-synthesis contract test, byte-identical today
- [x] **Phase 46: Observability Enrichment + Dashboard Deep Link** (5/5 plans) — completed 2026-07-17 — sampler 1.0, `session.id`, metadata/tags, `<PhoenixTraceLink>` deep link
- [x] **Phase 47: Self-Hosted Phoenix Install + End-to-End Proof** (10/10 plans) — completed 2026-07-17 — live 392-span five-level trace tree in self-hosted Phoenix; PROOF-01 human-signed-off

Full archive: [milestones/v1.0.8-ROADMAP.md](milestones/v1.0.8-ROADMAP.md) · [milestones/v1.0.8-REQUIREMENTS.md](milestones/v1.0.8-REQUIREMENTS.md)

</details>

<details>
<summary>✅ v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics (Phases 34–41) — SHIPPED 2026-07-15</summary>

**Milestone Goal:** Make a second external-repo run trustworthy and reviewable — a pushed run branch provably contains every Succeeded task's work, the budget tally matches the provider console, git ergonomics (baseRef, agent identity, promptFile) work, the dashboard is a sufficient approve-gate review surface, telemetry setup is guided, and the v1.0.6 audit tech-debt is retired.

- [x] **Phase 39: Pre-flight Tech-Debt Hardening** (2/2 plans) — completed 2026-07-04 — plannerConcurrency chart default 16→4 + project-level exactly-once cost rollup
- [x] **Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA** (6/6 plans) — completed 2026-07-08 — final-wave integration fixed, merges serialized (flock), boundary push gated on git-verified completeness, `lastPushedSHA` arms the force-with-lease fence
- [x] **Phase 35: Git Base Ref** (4/4 plans) — completed 2026-07-08 — `spec.git.baseRef` (branch/tag/SHA), typed fail-fast on unresolvable refs, `status.git.baseSHA` stamped
- [x] **Phase 36: Signed Commits + Bot Identity** (4/4 plans) — completed 2026-07-08 — *(descoped: identity only)* agent identity uniformly configurable at all 3 commit sites; GPG (SIGN-02/03/04) deferred to Future Requirements
- [x] **Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States** (12/12 plans) — completed 2026-07-09 (verified 2026-07-15) — approve-gate artifact review via git-transport staged envelopes, project settings view, honest log-drawer states
- [x] **Phase 38: Small Independents — Pricing, promptFile, Telemetry Nudge, Tech-Debt Carry** (7/7 plans) — completed 2026-07-11 — Claude 5 pricing verified live, `--prompt-file`, telemetry setup triple, DEBT-01..03 retired
- [x] **Phase 40: Deprecate v1alpha1 API (Full Version-Lifecycle Turn)** (7/7 plans) — completed 2026-07-12 — v1alpha3 sole served+storage version, `subagent.levels` semantic rename, envelope contract decoupled, `verify-no-legacy-api-refs` CI gate
- [x] **Phase 41: Refactoring Review — Non-Breaking Cleanup** (9/9 plans) — completed 2026-07-13 — 12 REFAC items: typed LevelPhase constants, checkDispatchHolds/PlannerDeps extractions, polarity fix, retry-driver unification, label/PVC-name centralization

Full archive: [milestones/v1.0.7-ROADMAP.md](milestones/v1.0.7-ROADMAP.md) · [milestones/v1.0.7-REQUIREMENTS.md](milestones/v1.0.7-REQUIREMENTS.md) · [milestones/v1.0.7-MILESTONE-AUDIT.md](milestones/v1.0.7-MILESTONE-AUDIT.md)

</details>

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

Reframed 2026-07-06: the Python/LangGraph image is no longer just a second strategy — it is the **candidate successor runtime**. After the specialist beachhead ships, authoring roles migrate planner-first / executor-last, each rung gated on eval-harness evidence; the endgame is a CLI-deprecation decision plus multi-provider via `init_chat_model`, which dissolves the standalone OpenAI-backend build (its remnant: credproxy route-allowlist extension + pricing rows). The original framing doc's parity inventory and contract-conformance table remain the reference for this migration.

See [milestones/v1.x-polyglot-subagent-MILESTONE.md](milestones/v1.x-polyglot-subagent-MILESTONE.md) for parity inventory, contract-conformance table, and provider-firewall gap analysis; [notes/adk-v2-subagent-evaluation.md](notes/adk-v2-subagent-evaluation.md) for the ADK-Go rejection; [notes/langgraph-successor-runtime-strategy.md](notes/langgraph-successor-runtime-strategy.md) for the ladder.

</details>

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1–11 (see archive) | v1.0.0 | 137/137 | Complete | 2026-06-11 |
| 12–17 (see archive) | v1.0.1 | 38/38 | Complete | 2026-06-13 |
| 18–21 (superseded) | v1.0.2 (Ebb) | 14/14 | Complete (superseded) | 2026-06-16 |
| 22–26 (see archive) | v1.0.2 (Spring Tide) | 19/19 | Complete | 2026-06-17 |
| 27–29 (see archive) | v1.0.3 | 14/14 | Complete | 2026-06-22 |
| 30 (see archive) | v1.0.5 | 3/3 | Complete | 2026-06-27 |
| 31–33 (see archive) | v1.0.6 | 8/8 | Complete | 2026-06-29 |
| 34–41 (see archive) | v1.0.7 | 51/51 | Complete | 2026-07-15 |
| 42–47 (see archive) | v1.0.8 | 32/32 | Complete | 2026-07-17 |
