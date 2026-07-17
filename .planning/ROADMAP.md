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
- 🚧 **v1.0.8 — Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix** — Phases 42–47 (in progress; started 2026-07-15). TIDE runs become observable in a self-hosted Arize Phoenix — the Milestone→Phase→Plan→Task dispatch chain emits real OpenInference/OTel spans (dispatch-chain AGENT spans, full LLM message arrays, W3C traceparent propagation), a documented self-hosted Phoenix recipe wires the chart's existing OTLP endpoint end-to-end, and a runtime-neutral adapter seam keeps the trace contract stable ahead of the LangGraph beachhead.
- 📋 **vNext — Specialist Verify Tier + LangGraph Beachhead** — (scoped 2026-07-06 via /gsd:explore; picks up after v1.0.8) — plan-check / level-verify / integration-check stages on a read-only LangGraph specialist image; first rung of the evidence-gated successor-runtime ladder — [framing doc](milestones/vnext-specialist-verify-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **v1.x — LangGraph Authoring Migration (evidence-gated)** — (backlog; reframed 2026-07-06 from "Polyglot Subagent Runtimes: LangGraph Strategy") — planner roles migrate first, executor last, each rung gated on eval-harness evidence; endgame = CLI-deprecation decision + multi-provider via `init_chat_model`, dissolving the standalone OpenAI backend — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **vLater — Dogfood Run #2 (retarget pending)** — (original deliverable — TIDE builds the OpenAI backend — dissolved by multi-provider-via-LangGraph; new build target chosen at scoping; still gated on multi-node infrastructure) — archived Flood Tide phase details remain a starting point: [milestones/v1.0.7-floodtide-ROADMAP.md](milestones/v1.0.7-floodtide-ROADMAP.md)

## Phases

### 🚧 v1.0.8 — Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix (In Progress)

**Milestone Goal:** TIDE runs are observable in a self-hosted Arize Phoenix — the Milestone→Phase→Plan→Task dispatch chain emits real OpenTelemetry spans with OpenInference attributes (including full LLM input/output message arrays), and a documented Phoenix self-host recipe wires the chart's existing OTLP endpoint to consume them natively.

- [ ] **Phase 42: Trace-Context Foundation + Planner-Level Span Emission** - Pure trace-context helpers plus attribute-complete AGENT spans for the four planning-DAG levels (Project/Milestone/Phase/Plan)
- [ ] **Phase 43: Task-Level Parity + Trace-Context Propagation** - The Task level gains its own dispatch span, W3C `traceparent` propagates into subagent and reporter Job envs, and per-level trace IDs persist in `.status.trace`
- [ ] **Phase 44: LLM Message-Array Spans + D-O5 Redaction/Size Boundary** - The reporter's trace-only mode turns a Task's `events.jsonl` into redacted, size-bounded LLM message-array spans — the milestone's headline capability
- [ ] **Phase 45: Runtime-Neutral Adapter Seam** - The message-span synthesizer becomes a per-runtime adapter behind the Subagent seam, proven by a self-instrumenting-stub contract test
- [ ] **Phase 46: Observability Enrichment + Dashboard Deep Link** - Sampler default, `session.id`, metadata/tag enrichment, and a Phoenix deep link from the dashboard's DAG nodes
- [ ] **Phase 47: Self-Hosted Phoenix Install + End-to-End Proof** - Documented Phoenix install (both storage paths, auth override, OTLP wiring) and a live run's trace tree captured as milestone-close evidence

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

## Phase Details

### Phase 42: Trace-Context Foundation + Planner-Level Span Emission

**Goal**: Lay the pure, K8s-independent trace-context primitives (deterministic TraceID from Project UID, W3C `traceparent` formatting/extraction, retroactive span timestamps) and wire them into the four *existing* planner-level Job-completion handlers (Project/Milestone/Phase/Plan) — real, attribute-complete AGENT spans appear for these levels before any propagation or Task-level work exists, using only data (model, cost, duration, token counts) the manager already holds.
**Depends on**: Nothing (first phase of milestone)
**Requirements**: ATTR-01, ATTR-02, ATTR-03
**Success Criteria** (what must be TRUE):

  1. An operator pointed at any OTLP-compatible backend sees a real AGENT-kind span for every completed Project/Milestone/Phase/Plan, and every one of those spans carries `llm.model_name` and `llm.provider` — the two attributes Phoenix needs to avoid rendering `$0.00`/blank cost (ATTR-01).
  2. Each of those spans also carries `llm.token_count.total` alongside the existing prompt/completion/cache-split token attributes (ATTR-02).
  3. Every attribute key emitted by `pkg/otelai` resolves from the official `openinference-semantic-conventions` Go module rather than a hand-rolled string constant, so the same keys `openinference-instrumentation-langchain` will emit natively already match (ATTR-03).

**Plans**: 5 plans (3 waves)

Plans:
**Wave 1**

- [x] 42-01-PLAN.md — Adopt openinference-semantic-conventions v0.1.1 + rework pkg/otelai attrs (D-05/D-06/D-07/D-08; blocking legitimacy checkpoint)
- [x] 42-02-PLAN.md — Pure trace-context primitives: TraceIDFromUID / FormatTraceparent / ExtractRemoteParent (Phase 43 seam; Option A independent roots)
- [x] 42-03-PLAN.md — Span-emission idempotency marker fields on all four planner CRD statuses + manifest regen

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 42-04-PLAN.md — Shared retroactive span synthesizer + Milestone/Phase handler wiring + envtest specs

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 42-05-PLAN.md — Plan/Project handler wiring + envtest specs + full Layer A phase gate

### Phase 43: Task-Level Parity + Trace-Context Propagation

**Goal**: Close the last dispatch-chain gap (the Task/executor level has zero span emission today) and thread a W3C `traceparent` one hop at a time from the manager into both the subagent Job and the reporter Job, so a single Project run composes into ONE connected, navigable trace tree instead of five disconnected roots — with each level's IDs durably anchored in its own CRD status for later reads.
**Depends on**: Phase 42 (reuses `pkg/otelai/tracecontext.go` and the attribute-complete span pattern)
**Requirements**: TRACE-01, TRACE-02, PROP-01, PROP-02
**Success Criteria** (what must be TRUE):

  1. The manager emits a real AGENT dispatch span at Task Job completion, closing the "all five level-completion sites" gap — Task now has the same span-emission call-site pattern as the other four levels (TRACE-01).
  2. A single Project run renders as one connected trace: the trace ID is deterministically derivable from the Project's UID, and every level's span (Project→Milestone→Phase→Plan→Task) parents correctly under its immediate parent's span (TRACE-02).
  3. W3C `traceparent` is present as data in both the subagent Job's env and the reporter Job's env at dispatch time — the runtime-neutral contract applied at both pod hops, so a child process's own spans (synthesized today, self-instrumented in a future runtime) parent identically either way (PROP-01).
  4. Each level's trace/span IDs persist in that level's `.status.trace` field after completion — a durable, re-readable parent-carrier surviving reconciler restarts (PROP-02).

**Plans**: 5 plans

**Wave 1** *(parallel, independent)*

- [x] 43-01-PLAN.md — Durable `{Level}TraceSpanID` fields on all five CRD statuses + Task's `TaskSpanEmittedUID` marker + CRD manifest regen (PROP-02 surface)
- [x] 43-02-PLAN.md — Traceparent carriers in both Job builders (BuildOptions/ReporterOptions) + tide-reporter `--traceparent` flag registration (crash-loop guard)

**Wave 2** *(blocked on Wave 1)*

- [x] 43-03-PLAN.md — Parenting retrofit: two-sided `synthesizePlannerSpan` signature, deterministic TraceID, immediate-parent fetches, second status patch at all four planner completion handlers + envtest linkage/persistence assertions

**Wave 3** *(blocked on Wave 2; 04 ∥ 05)*

- [x] 43-04-PLAN.md — Real traceparent values at both pod hops for the four planner levels (dispatch-prep TRACEPARENT env + reporter `--traceparent` Args) + envtest proof
- [x] 43-05-PLAN.md — Task-level parity: `emitTaskSpanOnce` covering all four terminal paths (generalized Option B), `TaskTraceSpanID` persistence, Task dispatch hop + fifth envtest Describe block

### Phase 44: LLM Message-Array Spans + D-O5 Redaction/Size Boundary

> **Research flag**: needs a research pass before planning — the exact `events.jsonl` multi-turn schema and a concrete byte-threshold constant for the D-O5 inline-vs-`ArtifactPath` decision are unverified beyond the schema comment in `stream_parser.go`; pull real fixture files and pick a specific number, don't defer to "some threshold."

**Goal**: Give the Task level's full LLM conversation — until now the richest data in the system with zero in-namespace observability consumer — its first path into Phoenix as message-array spans, safely: every message passes the existing secret-redaction machinery before emission, and payload size is explicitly bounded under OTLP's 4 MB ceiling rather than left as a silent drop risk. The milestone's headline, highest-risk phase.
**Depends on**: Phase 43 (correct parenting requires propagation already wired)
**Requirements**: MSG-01, MSG-02, MSG-03, TRACE-03
**Success Criteria** (what must be TRUE):

  1. The reporter Job gains a trace-only mode — no child-CR materialization — that reads a completed Task's `events.jsonl` and emits `LLMInputMessages`/`LLMOutputMessages` spans, closing the gap where the Task level has no in-namespace consumer of its own conversation today (MSG-01).
  2. Every message attribute is populated only after passing `internal/harness/redact.SecretPatterns`; a secret planted in a fixture `events.jsonl` never reaches the emitted span, even though the source file is written raw/unredacted by design (MSG-02).
  3. Message-array spans stay under the OTLP 4 MB ceiling: content above the documented byte threshold truncates with an explicit truncation marker and carries `ArtifactPath(events.jsonl)` on the same span for full-fidelity reference; `TestNoPayloadHelperOnPublicSurface` is updated deliberately to reflect the new bounded-payload surface, not deleted (MSG-03).
  4. `tide-reporter` — a short-lived, `os.Exit`-driven one-shot binary — calls its TracerProvider's deferred Shutdown on every exit path, so killing the process mid-batch never silently drops spans (TRACE-03).

**Plans**: 5 plans (4 waves)

Plans:
**Wave 1**

- [x] 44-01-PLAN.md — redact.String + pkg/otelai tool-call/reasoning encoding, LLMSpanKind, markers, D-O5 doc/guard evolution
- [x] 44-02-PLAN.md — ReporterOptions OTLP env forwarding + OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 + trace-only Job shape + four planner spawn sites

**Wave 2** *(blocked on 44-01)*

- [x] 44-03-PLAN.md — internal/reporter/tracesynth.go: events.jsonl→LLM-span synthesizer, redact-before-truncate, triple size guard, batch-aggregate proof

**Wave 3** *(blocked on 44-03)*

- [x] 44-04-PLAN.md — tide-reporter --trace-only mode + TracerProvider lifecycle with shutdown-on-every-exit-path (TRACE-03) + combined-run synth step

**Wave 4** *(blocked on 44-02 + 44-04)*

- [x] 44-05-PLAN.md — Task trace-only reporter spawn (D-06 gated) + manager deps wiring + envtest proof

### Phase 45: Runtime-Neutral Adapter Seam

**Goal**: Turn the events.jsonl→spans synthesizer from Phase 44 into a per-runtime adapter behind the Subagent seam, so a future self-instrumenting runtime (the LangGraph beachhead) can skip synthesis without any TIDE call site caring which runtime is live. Pure forward-compatibility scaffolding — every vendor today returns "not self-instrumenting," so behavior is unchanged; the seam and its contract test exist now so this isn't discovered for the first time when LangGraph actually lands.
**Depends on**: Phase 44 (adapts the tracesynth.go synthesizer just built)
**Requirements**: ADAPT-01
**Success Criteria** (what must be TRUE):

  1. A self-instrumenting capability flag travels as data derived from the manager's resolved `Provider.Vendor` — never a hard-coded per-runtime branch in the reporter or manager.
  2. When the flag is set for a Task's resolved provider, the reporter skips message-span synthesis entirely for that Task — no double-emission path exists.
  3. A contract test using a stub self-instrumenting runtime proves zero duplicate spans end-to-end (env-carrier extraction only — no LangGraph-specific span shape assumed).

**Plans**: 2 plans

Plans:
- [x] 45-01-PLAN.md — SelfInstruments capability table + ReporterOptions/BuildReporterJob transport + flag computation at all 5 reporter-spawn sites (manager side)
- [x] 45-02-PLAN.md — Reporter --skip-message-spans parse + synthesizeSpans sole skip point + D-09 stub-runtime contract test + tracesynth.go doc contract (reporter side)

### Phase 46: Observability Enrichment + Dashboard Deep Link

**Goal**: Make the now-complete trace tree actually useful to an operator inside Phoenix and inside TIDE's own dashboard — a sane default sampler (so a demo run isn't a coin flip), a session identity that lets Phoenix compute an independent cost cross-check, filterable metadata/tags, and a one-click deep link from any DAG node straight to its trace.
**Depends on**: Phase 44 (enriches both the dispatch-chain AGENT spans and the message-array spans, so both span families must already exist); Phase 43 (OBS-04 reads the `.status.trace` field PROP-02 introduced)
**Requirements**: OBS-01, OBS-02, OBS-03, OBS-04
**Success Criteria** (what must be TRUE):

  1. The chart's trace-sampler default is 1.0 (up from 0.1), with the opt-down for high-volume installs documented — a demo run no longer has a 90% chance that any given span never reaches Phoenix (OBS-01).
  2. Every span carries `session.id` = Project UID, so Phoenix's session view computes an independent per-run token/cost rollup an operator can cross-check against TIDE's own budget tally (OBS-02).
  3. Spans carry `metadata`/`tag.tags` enrichment (level kind + name, wave index, gate profile, failure-halt state), so an operator can filter Phoenix's DSL to "every span from Phase N" or "every conservative-profile run" without leaving Phoenix (OBS-03).
  4. Each Planning/Execution DAG node in the TIDE dashboard deep-links to its Phoenix trace (reading IDs from `.status.trace`) when a `phoenix.baseURL` chart value is configured, and renders no dead button when it isn't (OBS-04).

**Plans**: 5 plans (2 waves)
**UI hint**: yes

Plans:
**Wave 1** *(parallel, independent)*

- [x] 46-01-PLAN.md — otelai SessionID/Metadata/Tags helpers + ReporterOptions/CLI transport + EmitSpans enrichment (reporter side)
- [x] 46-02-PLAN.md — Sampler default 0.1→1.0 (every surface) + phoenix.baseURL chart value + helm render gates + honest opt-down docs
- [x] 46-03-PLAN.md — Dashboard backend: PHOENIX_BASE_URL config chain + traceId/traceSpanId on projectDetail/childRef/taskDetail

**Wave 2** *(46-04 blocked on 46-01; 46-05 blocked on 46-03)*

- [ ] 46-04-PLAN.md — Manager-side enrichment on all five AGENT spans + D-03 token-count drop (all five levels, planner-corrected vs research's Task-only) + D-02 sampled-bit threading
- [ ] 46-05-PLAN.md — SPA deep link: phoenixLink.ts + shared PhoenixTraceLink at both mount points (NodeDetailPanel + TaskDetailDrawer)

### Phase 47: Self-Hosted Phoenix Install + End-to-End Proof

> **Research flag**: re-fetch the Phoenix chart/appVersion pin fresh immediately before authoring INSTALL.md — the chart ships near-daily (9 versions in ~9 days at research time) and the number recorded in research/STACK.md (`10.0.0`/`18.0.0`) should not be trusted without a live check.

**Goal**: An operator can stand up a self-hosted Phoenix from documented, non-default-safe overrides, point TIDE's existing `otel.exporter.endpoint` chart value at it, and see a real run's complete five-level trace tree — including redacted message arrays — rendered and queryable. This is the milestone's acceptance bar; PHX-01/PHX-02 documentation can be drafted in parallel with Phases 42–46, but the live proof gates on them.
**Depends on**: Phase 46 (a live run needs the full enrichment + deep-link surface to be a meaningful proof)
**Requirements**: PHX-01, PHX-02, PROOF-01
**Success Criteria** (what must be TRUE):

  1. INSTALL.md/observability.md walks an operator through a self-hosted Phoenix install covering both storage paths — PVC-backed SQLite for kind/dev matching TIDE's own posture, and bundled Postgres for durability — and explicitly calls out overriding the chart's `auth.enableAuth=true` default (PHX-01).
  2. The `otel.exporter.endpoint` wiring is documented end-to-end using the required bare `host:port` form (not scheme-prefixed, which `otlptracegrpc.WithEndpoint` silently rejects), and NOTES.txt nudges an operator toward the Phoenix step when tracing is dark (PHX-02).
  3. A live run's complete five-level trace tree — including redacted message arrays at the Task level — is visible and queryable in a self-hosted Phoenix, with screenshots and trace IDs captured as milestone-close evidence (PROOF-01).

**Plans**: TBD

## Progress

**Execution Order (v1.0.8):** 42 → 43 → 44 → 45 → 46 → 47. Strict dependency chain: 43 needs 42's helpers, 44 needs 43's propagation, 45 adapts 44's synthesizer, 46 enriches both 43's and 44's span families, 47's live proof gates on 46 (though PHX-01/PHX-02 docs may draft in parallel).

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
| 42. Trace-Context Foundation + Planner-Level Span Emission | v1.0.8 | 5/5 | Complete    | 2026-07-16 |
| 43. Task-Level Parity + Trace-Context Propagation | v1.0.8 | 5/5 | Complete    | 2026-07-16 |
| 44. LLM Message-Array Spans + D-O5 Redaction/Size Boundary | v1.0.8 | 5/5 | Complete    | 2026-07-17 |
| 45. Runtime-Neutral Adapter Seam | v1.0.8 | 2/2 | Complete    | 2026-07-17 |
| 46. Observability Enrichment + Dashboard Deep Link | v1.0.8 | 3/5 | In Progress|  |
| 47. Self-Hosted Phoenix Install + End-to-End Proof | v1.0.8 | 0/TBD | Not started | - |
