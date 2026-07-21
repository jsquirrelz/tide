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
- ✅ **v1.0.9 — Slack Tide: The Task Loop (Verification-Driven Quality Iteration)** — Phases 48–53 (complete 2026-07-21; release tag pending the rc-gated pipeline) — TIDE closes its first real feedback loop: each Task's artifact is checked by an independent read-only LangGraph evaluator, and a repairable failure drives a fresh attempt with a compact evidence packet, bounded by a `LoopPolicy`, escalating to a human on exhaustion. Ships on a minimal common loop contract (`LoopPolicy`/`LoopStatus`) the wider [five-loop model](notes/five-loop-model.md) reuses, plus Execution-loop hardening and loop-native observability. Supersedes the "vNext — Specialist Verify Tier" framing below (reframed 2026-07-18 from a gate that halts to a loop that closes). Full archive: [milestones/v1.0.9-ROADMAP.md](milestones/v1.0.9-ROADMAP.md) · [milestones/v1.0.9-REQUIREMENTS.md](milestones/v1.0.9-REQUIREMENTS.md).
- ⊘ **vNext — Specialist Verify Tier + LangGraph Beachhead** — (scoped 2026-07-06 via /gsd:explore) — **SUPERSEDED by v1.0.9 "Slack Tide"** (reframed 2026-07-18: verification is not a gate that halts, it's the feedback signal that closes a loop; the plan-check/level-verify/integration-check three-stage framing collapsed into ONE loop contract parameterized per level). Framing preserved for reference — [framing doc](milestones/vnext-specialist-verify-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **v1.x — LangGraph Authoring Migration (evidence-gated)** — (backlog; reframed 2026-07-06 from "Polyglot Subagent Runtimes: LangGraph Strategy") — planner roles migrate first, executor last, each rung gated on eval-harness evidence; endgame = CLI-deprecation decision + multi-provider via `init_chat_model`, dissolving the standalone OpenAI backend — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
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

Next milestone's phases will be defined by `/gsd:new-milestone` (numbering continues from 53).

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
