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
- ✅ **v1.0.9 — Slack Tide: The Task Loop (Verification-Driven Quality Iteration)** — Phases 48–53 (shipped 2026-07-21, tag `v1.0.9`, published: 9 images + 2 OCI charts + 5 binaries, verified anon) — TIDE closes its first real feedback loop: each Task's artifact is checked by an independent read-only LangGraph evaluator, and a repairable failure drives a fresh attempt with a compact evidence packet, bounded by a `LoopPolicy`, escalating to a human on exhaustion. Ships on a minimal common loop contract (`LoopPolicy`/`LoopStatus`) the wider [five-loop model](notes/five-loop-model.md) reuses, plus Execution-loop hardening and loop-native observability. Supersedes the "vNext — Specialist Verify Tier" framing below (reframed 2026-07-18 from a gate that halts to a loop that closes). Full archive: [milestones/v1.0.9-ROADMAP.md](milestones/v1.0.9-ROADMAP.md) · [milestones/v1.0.9-REQUIREMENTS.md](milestones/v1.0.9-REQUIREMENTS.md).
- 🚧 **v1.0.10 — King Tide: Five Loops, One Successor Runtime, Dynamic Workflows** — Phases 54–65 (scoping 2026-07-21) — TIDE's authoring stack completes the evidence-gated migration to the LangGraph runtime (through the CLI-deprecation decision and the multi-provider `init_chat_model` endgame), all five loops of the [five-loop model](notes/five-loop-model.md) run live (Product, System, Oversight join Execution and Task), and the three dynamic-workflow patterns (adversarial verification, generate-and-filter, tournament) operate at the stage-dispatch seams behind a new shared fan-out primitive. Absorbs and supersedes the "v1.x — LangGraph Authoring Migration" backlog entry below. 30 requirements, 12 phases, 100% mapped.
- ⊘ **v1.x — LangGraph Authoring Migration (evidence-gated)** — (backlog; reframed 2026-07-06 from "Polyglot Subagent Runtimes: LangGraph Strategy") — **ABSORBED into v1.0.10 "King Tide"** (2026-07-21): the evidence-gated planner-first/executor-last ladder + CLI-deprecation decision + multi-provider endgame are now v1.0.10 Phases 54/58/61/65. Framing preserved for reference — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- ⊘ **vNext — Specialist Verify Tier + LangGraph Beachhead** — (scoped 2026-07-06 via /gsd:explore) — **SUPERSEDED by v1.0.9 "Slack Tide"** (reframed 2026-07-18: verification is not a gate that halts, it's the feedback signal that closes a loop; the plan-check/level-verify/integration-check three-stage framing collapsed into ONE loop contract parameterized per level). Framing preserved for reference — [framing doc](milestones/vnext-specialist-verify-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **vLater — Dogfood Run #2 (retarget pending)** — (original deliverable — TIDE builds the OpenAI backend — dissolved by multi-provider-via-LangGraph; new build target chosen at scoping; still gated on multi-node infrastructure) — archived Flood Tide phase details remain a starting point: [milestones/v1.0.7-floodtide-ROADMAP.md](milestones/v1.0.7-floodtide-ROADMAP.md)

## Phases

<details>
<summary>✅ v1.0.9 Slack Tide (Phases 48–53) — COMPLETE 2026-07-21</summary>

- [x] Phase 48: LangGraph Evaluator Image + Credproxy-TLS Spike (5/5 plans) — completed 2026-07-18
- [x] Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema (4/4 plans) — completed 2026-07-18
- [x] Phase 50: Execution-Loop Hardening + Loop-Native Observability (7/7 plans) — completed 2026-07-19
- [x] Phase 51: The Task Loop (8/8 plans) — completed 2026-07-20
- [x] Phase 52: Per-Level LoopPolicy Parameterization (11/11 plans) — completed 2026-07-21
- [x] Phase 53: Chart Config + Dashboard Provenance Surfacing (11/11 plans) — completed 2026-07-21

Full phase details: [milestones/v1.0.9-ROADMAP.md](milestones/v1.0.9-ROADMAP.md)

</details>

### 🚧 v1.0.10 King Tide (Phases 54–65) — SCOPING

**Milestone Goal:** TIDE's authoring stack completes the evidence-gated migration to the LangGraph runtime, all five loops of the five-loop model run live (Product/System/Oversight join Execution/Task), and the three dynamic-workflow patterns operate behind a shared fan-out primitive.

- [ ] **Phase 54: Runtime Selection Foundation + Observability Gap Closure** - Per-level vendor/runtime selection resolves through config; both LangGraph images emit real, correctly-parented spans
- [ ] **Phase 55: Oversight Classifier Feature Schema + Labeled-Record Emission** - Every CRD level carries classifier features; every loop iteration/gate decision emits a training-ready labeled record
- [ ] **Phase 56: System Loop — Eval-Gated Promotion Machinery** - Generic candidate promotion against a trailing baseline, with tested rollback and structural anti-gaming
- [ ] **Phase 57: Shared Fan-Out + Reduce Primitive + Cost/OOM Rails** - N-way dispatch + reduce, with cost/concurrency ceilings proven live before any consumer ships
- [ ] **Phase 58: Write-Capable LangGraph Authoring Image — Planner Rung Migration** - Planner roles migrate to LangGraph, promoted rung-by-rung only on shadow-pair evidence
- [ ] **Phase 59: Adversarial Verification (Judge Panel) at Verify Seams** - K independent, diversity-configured refuter-evaluators converge on a deterministic reduce rule
- [ ] **Phase 60: Generate-and-Filter at Planner Seams** - N candidate plans generated in parallel; only the judged winner materializes children
- [ ] **Phase 61: Executor Rung Migration** - The task-executor rung migrates last, gated on new agent-loop-quality eval dimensions built for it
- [ ] **Phase 62: Tournament Selection (Cost-Gated Opt-In)** - Bracket tournament available as an explicit, budget-precomputed opt-in — never a default posture
- [ ] **Phase 63: Product Loop** - Outcome-judged artifact tree drives bounded, thrash-guarded re-planning via a new Milestone
- [ ] **Phase 64: Oversight Loop + Full-Loop Observability Provenance** - Gate policy resolves from risk + confidence + track record; every new loop/fan-out shape is Phoenix-queryable
- [ ] **Phase 65: Multi-Provider Endgame + CLI-Deprecation Decision** - `init_chat_model` multi-provider dispatch, provider-aware credproxy, and the recorded CLI-deprecation decision

## Phase Details

### Phase 54: Runtime Selection Foundation + Observability Gap Closure
**Goal**: Per-level runtime/vendor selection resolves through the existing config chain, and both LangGraph images (verifier + the forthcoming authoring image) emit real, correctly-parented OpenInference spans.
**Depends on**: Nothing (first phase of v1.0.10)
**Requirements**: MIG-03, OBS-05
**Success Criteria** (what must be TRUE):
  1. An operator can set `Levels.<level>.Vendor` in chart/CRD config and the resolved dispatch uses that vendor — the three hand-rolled inline `Vendor: "langgraph"` call sites are gone, replaced by a generalized `ResolveProvider`.
  2. Selecting an unsupported vendor for a level fails fast at resolution time, not silently at dispatch.
  3. A live LangGraph dispatch produces OpenInference spans visible in Phoenix, correctly parented under the W3C traceparent contract — the shipped `SelfInstruments("langgraph")` zero-spans gap no longer exists.
**Plans**: TBD

### Phase 55: Oversight Classifier Feature Schema + Labeled-Record Emission
**Goal**: Every CRD level carries classifier feature fields, and every loop iteration/gate decision emits a training-ready labeled record — the corpus future ML classifiers will train on, built from day one of every subsequent loop.
**Depends on**: Nothing new (independent of Phase 54; sequenced early so every later loop generates training data from day one)
**Requirements**: OVR-05, OVR-06
**Success Criteria** (what must be TRUE):
  1. Every level's CRD (Task/Plan/Phase/Milestone/Project) exposes a schema-validated `deterministic | non-deterministic` verifiability field plus risk-tier/confidence-bucket/outcome enums.
  2. Each loop iteration and gate decision emits a labeled record (features + measured outcome) to the artifact store/trace stream, never to etcd.
  3. An operator can retrieve the corpus of labeled records for a completed run without querying CRD `.status` history arrays (LOOP-03 compliance).
**Plans**: TBD

### Phase 56: System Loop — Eval-Gated Promotion Machinery
**Goal**: A generic candidate (template/prompt/runtime/config change) can be dispatched through the eval harness, compared against a trailing baseline, and promoted or rejected with a recorded, rollback-tested artifact trail — the mechanism every subsequent migration rung rides.
**Depends on**: Nothing new (extends `internal/eval` independently; sequenced before Phase 58, which consumes it)
**Requirements**: SYS-01, SYS-03, SYS-04
**Success Criteria** (what must be TRUE):
  1. A candidate change is evaluated against a trailing baseline and produces a pass/fail promotion decision with a recorded candidate identity/version and experiment outcome.
  2. Reverting a promoted candidate is a tested, one-step rollback.
  3. A candidate experiment that also touches its own evaluator/baseline/fixtures in the same change is hard-blocked or requires elevated approval — never silently promoted.
  4. System-loop eval/shadow spend reserves and settles against a capped eval budget distinct from production `BudgetCents`; exhausting it halts experiments without touching production dispatch.
**Plans**: TBD

### Phase 57: Shared Fan-Out + Reduce Primitive + Cost/OOM Rails
**Goal**: One logical node can dispatch N sibling Jobs with a pluggable reduce step, bounded by per-shape and per-wave cost/concurrency ceilings proven under load — the single highest-leverage build item in the milestone, since the shadow-pair migration and all three dynamic-workflow patterns are the same mechanism with different N and reduce strategy.
**Depends on**: Nothing new (independent primitive; sequenced before Phase 58, which is its first consumer)
**Requirements**: FAN-01, FAN-02
**Success Criteria** (what must be TRUE):
  1. A node can fan out to N sibling Jobs and converge on a single reduce step with no runtime DAG mutation — waves stay derived.
  2. The mechanism generalizes the existing `ChildCount` succession-gating pattern rather than introducing a parallel bespoke wait mechanism.
  3. A fan-out shape exceeding its configured `maxShape` or the per-wave aggregate cap is refused/parked at dispatch time, not discovered via OOM.
  4. A live single-node kind run drives a real fan-out shape at its configured ceiling without reproducing the dogfood run-2b OOM incident.
**Plans**: TBD
**Research**: true

### Phase 58: Write-Capable LangGraph Authoring Image — Planner Rung Migration
**Goal**: Planner roles migrate to a write-capable LangGraph image behind the unchanged `pkg/dispatch.Subagent` seam, promoted rung-by-rung only on evidence from the System loop, with the shadow-pair comparison riding the fan-out primitive as its degenerate N=2 consumer.
**Depends on**: Phase 54 (vendor/runtime selection), Phase 56 (promotion machinery), Phase 57 (fan-out primitive for shadow-pair)
**Requirements**: MIG-01, MIG-02, MIG-04, SYS-02
**Success Criteria** (what must be TRUE):
  1. A write-capable LangGraph authoring image emits schema-constrained `ChildCRDSpec` structured output, replacing prompt-and-parse for LangGraph-authored levels; the CLI-authored reporter path stays byte-identical for mixed fleets.
  2. A planner rung promotes to LangGraph only after a shadow-pair comparison (CLI vs. LangGraph dispatched on the same fixture) clears the Phase-56 promotion threshold, and can be rolled back to the CLI runtime per-rung.
  3. The shadow-pair comparison runs as a real N=2 consumer of the Phase-57 fan-out primitive, not a separate bespoke mechanism — proving SYS-02's "one promotion mechanism, not a parallel bespoke system" claim.
**Plans**: TBD

### Phase 59: Adversarial Verification (Judge Panel) at Verify Seams
**Goal**: Verify seams can run K independent, diversity-configured refuter-evaluators with a deterministic reduce policy.
**Depends on**: Phase 57 (fan-out primitive)
**Requirements**: FAN-03
**Success Criteria** (what must be TRUE):
  1. A verify seam dispatches K sibling verifier Jobs with distinct prompts/model seats (not K clones of one config) and converges on a documented quorum/dominance reduce rule recorded in config.
  2. The judge panel's verdict rides the existing verdict schema unchanged.
**Plans**: TBD

### Phase 60: Generate-and-Filter at Planner Seams
**Goal**: Planner seams can generate N candidate artifacts in parallel, judge them on the shared verdict schema, and materialize only the winner's children.
**Depends on**: Phase 57 (fan-out primitive)
**Requirements**: FAN-04
**Success Criteria** (what must be TRUE):
  1. N candidate planner artifacts are generated in parallel and judged on the shared verdict schema; only the winning candidate's `ChildCRDSpec`s materialize — the DAG is not multiplied by N.
  2. A runner-up candidate is recorded (salvage), not silently discarded.
  3. Generate-and-filter fan-out stays bounded by the Phase-57 `maxShape`/aggregate-cap rails.
**Plans**: TBD

### Phase 61: Executor Rung Migration
**Goal**: The task-executor rung migrates to the LangGraph runtime last, gated on new agent-loop-quality eval dimensions built specifically for it — the hardest parity bet in the ladder, closed as a scheduled build item rather than an assumption.
**Depends on**: Phase 58 (planner rungs prove the ladder pattern), Phase 56 (promotion machinery, extended with new eval dimensions)
**Requirements**: MIG-05
**Success Criteria** (what must be TRUE):
  1. New eval dimensions (tool-loop behavior, commit-protocol fidelity, diff correctness, tool-error recovery) exist in the harness BEFORE the executor rung is judged — not assumed to fall out of the planner rungs.
  2. The LangGraph executor writes a real git commit inside the pod via the existing two-tier git model (agent identity in pod, `tide-push` keeps remote creds), unchanged from the CLI executor's contract.
  3. The executor rung promotes only after clearing the new eval dimensions against the CLI baseline via the Phase-56 promotion machinery, with a tested rollback to the CLI executor.
**Plans**: TBD
**Research**: true

### Phase 62: Tournament Selection (Cost-Gated Opt-In)
**Goal**: Bracket-style tournament selection is available as an explicit, cost-gated opt-in, never a default posture — the highest-multiplier fan-out shape, proven live before it ships.
**Depends on**: Phase 57 (fan-out primitive), Phase 59 and Phase 60 (reuses both patterns' proven reduce steps)
**Requirements**: FAN-05
**Success Criteria** (what must be TRUE):
  1. A tournament dispatch computes and reserves its full `N·(1+K)+1` budget up front before any bracket Job dispatches.
  2. Tournament judging runs as a bracket (O(n)), never round-robin.
  3. Tournament is off by default and only activates via explicit opt-in config.
  4. A live single-node kind run proves the tournament's highest-multiplier shape stays within its reserved budget and concurrency ceiling.
**Plans**: TBD
**Research**: true

### Phase 63: Product Loop
**Goal**: The Product loop closes at the project/milestone boundary — the artifact tree is judged against the Project's outcome prompt, with bounded, thrash-guarded re-planning.
**Depends on**: Phase 56 (System loop's evidence-gating precedent for high-stakes autonomous re-planning)
**Requirements**: PROD-01, PROD-02, PROD-03
**Success Criteria** (what must be TRUE):
  1. An independent evaluator judges the completed artifact tree against the Project's outcome prompt and returns a verdict on the shared schema.
  2. A REPAIRABLE-class verdict drives re-planning by authoring a NEW Milestone — the D-07 `maxIterations=0` clamp at phase/milestone/project stays untouched, and approve-at-descent is preserved.
  3. Re-planning is bounded by `LoopPolicy` iterations and halts on severity-weighted non-improvement, never discarding completed subtrees wholesale.
  4. A live billable kind run proves outcome-judged red leads to a new-milestone re-plan that converges or halts.
**Plans**: TBD

### Phase 64: Oversight Loop + Full-Loop Observability Provenance
**Goal**: Gate policy resolves from loop level + risk + confidence + measured track record, and — now that Product/System/Oversight loops and every fan-out shape exist — all of them are queryable in Phoenix via the existing loop-native conventions.
**Depends on**: Phase 63 (Product loop, for real risk/history signal), Phase 56 (System loop promotion history)
**Requirements**: OVR-01, OVR-02, OVR-03, OVR-04, OBS-06
**Success Criteria** (what must be TRUE):
  1. `LoopPolicy.Autonomy` is consumed by an extension of the single `ResolveLoopPolicy` resolver — gate policy varies by risk tier + confidence + rolling pass/fail history, never LLM self-reported confidence.
  2. Autonomy adjusts down-fast on failure and up-slow only after a minimum sample size/window of passes at low risk.
  3. Track-record state is bounded rolling counters on status, never history arrays in etcd.
  4. A live cluster run shows a track-record-driven gate-policy change (auto→approve after failures; approve→auto after sustained passes) with human override always available.
  5. Product-loop iterations, System-loop promotions, Oversight escalations, and fan-out sibling groups are all queryable in Phoenix on the existing `loop.*`/`evaluation.*` conventions with no bespoke instrumentation.
**Plans**: TBD
**Research**: true

### Phase 65: Multi-Provider Endgame + CLI-Deprecation Decision
**Goal**: The authoring/executor image dispatches through `init_chat_model` across providers, credproxy is provider-aware, and the accumulated rung evidence resolves the CLI-deprecation decision — the milestone's closing move, sequenced after the ladder proves out.
**Depends on**: Phase 61 (executor rung proven), Phase 58 (planner rungs proven)
**Requirements**: PROV-01, PROV-02, PROV-03, PROV-04, MIG-06
**Success Criteria** (what must be TRUE):
  1. The authoring image resolves its model via `init_chat_model`, dispatching the same envelope through both Anthropic and OpenAI.
  2. credproxy classifies billing-exhaustion and routes upstream per-provider — no longer a singular hardcoded Anthropic matcher/URL.
  3. Each enabled provider passes a conformance suite (golden verdict fixture + child-CRD round-trip) before it can be selected anywhere, failing closed on nonconformance.
  4. A new provider's pricing rows are empirically probed and covered by the drift-check mechanism before that provider dispatches billable work.
  5. A recorded decision doc (parity table per role, cost deltas, incident list) resolves deprecate/retain-as-fallback/retain for the CLI runtime, wired into chart defaults.
**Plans**: TBD

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
| 48–53 (see archive) | v1.0.9 | 46/46 | Complete | 2026-07-21 |
| 54. Runtime Selection Foundation + Obs Gap Closure | v1.0.10 | 0/TBD | Not started | - |
| 55. Oversight Classifier Feature Schema + Labeled Records | v1.0.10 | 0/TBD | Not started | - |
| 56. System Loop — Eval-Gated Promotion Machinery | v1.0.10 | 0/TBD | Not started | - |
| 57. Shared Fan-Out + Reduce Primitive + Cost/OOM Rails | v1.0.10 | 0/TBD | Not started | - |
| 58. Write-Capable LangGraph Authoring Image — Planner Rungs | v1.0.10 | 0/TBD | Not started | - |
| 59. Adversarial Verification (Judge Panel) | v1.0.10 | 0/TBD | Not started | - |
| 60. Generate-and-Filter at Planner Seams | v1.0.10 | 0/TBD | Not started | - |
| 61. Executor Rung Migration | v1.0.10 | 0/TBD | Not started | - |
| 62. Tournament Selection (Cost-Gated Opt-In) | v1.0.10 | 0/TBD | Not started | - |
| 63. Product Loop | v1.0.10 | 0/TBD | Not started | - |
| 64. Oversight Loop + Full-Loop Observability Provenance | v1.0.10 | 0/TBD | Not started | - |
| 65. Multi-Provider Endgame + CLI-Deprecation Decision | v1.0.10 | 0/TBD | Not started | - |
