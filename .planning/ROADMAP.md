# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** — Phases 1–11 (shipped 2026-06-11) — ⚠ shipped on an invalid execution foundation (per-plan waves; see v1.0.2 Spring Tide)
- ✅ **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** — Phases 12–17 (shipped 2026-06-13) — ⚠ same invalid foundation
- ⊘ **v1.0.2 — Ebb Tide: Token & Cost Optimization** — Phases 18–21 (completed; **SUPERSEDED — will not be released**, artifacts preserved). Superseded after dogfood run #2 surfaced the per-plan-waves defect.
- 🚧 **v1.0.2 — Spring Tide: Global Execution DAG (severe corrective patch)** — Phases 22–26 (planning). Re-architect execution to ONE global Execution DAG spanning the entire Project — the patch that makes the Topologically-Indexed paradigm real. Supersedes Ebb Tide; preempts the OpenAI/dogfood milestone.
- 📋 **vNext — OpenAI Backend + Dogfood Run #2** — (planned; gated on v1.0.2 Spring Tide landing a correct execution layer)
- 📋 **v1.x — Polyglot Subagent Runtimes: LangGraph Strategy** — (backlog; architecture locked, phases TBD) — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md)

## Phases

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

Superseded after dogfood run #2 surfaced the per-plan-waves architecture defect. Token/cost + observability work is preserved and folds forward where it still applies; the CACHE-01 decision record lives in PROJECT.md. The detailed phase breakdown for 18–21 is archived in git history (this ROADMAP, pre-Spring-Tide revision) and the per-phase directories under `.planning/phases/`.

</details>

### 🚧 v1.0.2 — Spring Tide: Global Execution DAG (In Progress)

**Milestone Goal:** Re-architect execution so waves are derived from ONE global Execution DAG spanning the entire Project (all milestones/phases/plans), assembled after planning completes — making the Topologically-Indexed paradigm real. v1.0.0/v1.0.1 shipped a per-plan-waves layer (`Plan` has no deps, `Task.dependsOn` is plan-local per D-F1, waves are per-plan via `materializeWaves`, no global indegree map). This is the corrective patch that makes the 1.0 line actually be what it claimed.

**Build order (this is a re-architecture):** the breaking CRD/schema foundation and cross-scope dependency model land first; the global scheduler / wave-derivation engine builds on that schema; global dispatch + failure semantics + gates-as-holds + resumption compose over the scheduler; multi-milestone exercise and spec-conformance close the milestone. FIX-01 (dashboard embed) is independent and ships first as a standalone phase.

- [ ] **Phase 22: Dashboard Embed Freshness Fix** — Published images can never ship an SPA bundle older than source; verified against the Telemetry tab
- [ ] **Phase 23: Schema Migration + Cross-Scope Dependency Model** — Breaking CRD changes (Wave re-owned to Project scope, global `wave` label) with a migration path, plus cross-plan/phase/milestone task deps reconciled into one global DAG with cyclic rejection
- [ ] **Phase 24: Global Wave Derivation Engine** — Assemble ONE global Execution DAG after planning and derive global waves via layered Kahn; the bidirectional global wave index, re-derived O(V+E) with no cached schedule
- [ ] **Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption** — Dispatch off the global indegree map vs the completed-task set; wave-boundary failure contract preserved exactly at global scope; gates compose as holds; restart re-derives the whole schedule
- [ ] **Phase 26: Multi-Milestone Drive + Spec Conformance** — A Project drives multiple Milestones via the Milestone DAG with cross-milestone global waves and per-milestone gate policy; the README worked example is an executable conformance test

## Phase Details

### Phase 22: Dashboard Embed Freshness Fix
**Goal**: Every published TIDE image embeds the current dashboard SPA, so a release can never ship a bundle older than its source — closing the dogfood run #2 finding that v1.0.0/v1.0.1 images froze the embedded bundle at pre-telemetry commit `6d7a28f`.
**Depends on**: Nothing (independent of the execution re-architecture; ships first)
**Requirements**: FIX-01
**Success Criteria** (what must be TRUE):
  1. A maintainer builds the dashboard image from a clean checkout and the embedded `cmd/dashboard/embed/dist` bundle is regenerated from current source as part of the image/release path (not committed-stale).
  2. CI fails a build whose embedded `dist` is older than the dashboard source (a staleness gate catches a forgotten regenerate before publish).
  3. A freshly built image, run against a cluster, renders the Telemetry tab — proving the embedded bundle is the current post-telemetry SPA, not the frozen pre-telemetry one.
**Plans**: 2 plans
- [x] 22-01-PLAN.md — multi-stage Dockerfile.dashboard (node spa-builder) + .dockerignore re-includes + make verify-dashboard-freshness target (freshness + telemetry-marker gate)
- [x] 22-02-PLAN.md — wire verify-dashboard-freshness into ci.yaml (PR gate) and release.yaml helmify-verify (release gate), each with actions/setup-node
**UI hint**: yes

### Phase 23: Schema Migration + Cross-Scope Dependency Model
**Goal**: The CRD surface is re-shaped so wave derivation/ownership lives at Project scope and tasks can declare dependencies across plan/phase/milestone boundaries — all reconciled into one global Execution DAG that rejects cycles at validation — shipped behind a documented migration path that never silently corrupts an in-flight Project.
**Depends on**: Nothing (foundation; Phase 24 builds on this schema). Can run alongside Phase 22.
**Requirements**: SCHEMA-01, SCHEMA-02, SCHEMA-03, DEPS-01, DEPS-02, DEPS-03
**Success Criteria** (what must be TRUE):
  1. A Task can declare a dependency on a Task in another Plan, Phase, OR Milestone via a qualified reference, and the orchestrator resolves it into the global DAG (the plan-local D-F1 restriction is retired).
  2. Plan-, Phase-, and Milestone-level interface dependency declarations are reconciled into the same global task DAG (coarse interface edges resolve to / coexist with task-level edges).
  3. Applying a global dependency set that forms a cycle across plan/phase/milestone boundaries is rejected at validation time with the involved nodes surfaced — no run starts, no recovery attempted.
  4. Wave derivation/ownership is moved off `Plan` to the global (Project) scope, and the locked metric label set `{project,phase,plan,wave}` is preserved with `wave` resemanticized to the global index (the `task` label stays forbidden per the metriccardinality analyzer).
  5. A documented migration/conversion path carries an in-flight Project from the old per-plan schema to the new global schema with a version bump and no silent data loss.
**Plans**: TBD

### Phase 24: Global Wave Derivation Engine
**Goal**: Once project planning completes, the orchestrator assembles ONE global Execution DAG of every Task across all Milestones/Phases/Plans and derives a single monotonic wave schedule by layered Kahn — queryable both directions and re-derived cheaply with no cached schedule.
**Depends on**: Phase 23 (cross-scope deps + global-scope Wave ownership)
**Requirements**: EXEC-01, EXEC-02, EXEC-03, EXEC-04
**Success Criteria** (what must be TRUE):
  1. After planning completes and before any execution dispatch, the orchestrator has assembled a single global Execution DAG containing every Task in the Project across all Milestones/Phases/Plans.
  2. Waves are derived by layered Kahn over that global DAG and carry global, monotonic wave indices — not per-plan `tide-wave-<plan.UID>-<i>` indices.
  3. Given any Task you can resolve its global wave, and given any global wave you can list its Tasks (the README:54 namesake invariant holds Project-wide, not just within a plan).
  4. Adding or completing a task re-derives the whole Project's waves in O(V+E) from the DAG + completed-task set with no schedule cached in `.status` (PERSIST-03 guards still pass).
**Plans**: TBD

### Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption
**Goal**: Execution dispatches off ONE global indegree map versus the completed-task set, the wave-boundary failure contract holds exactly at global scope, gates compose as holds over the global scheduler, and an orchestrator restart re-derives the entire schedule from minimal state.
**Depends on**: Phase 24 (global wave index + re-derivation)
**Requirements**: DISP-01, DISP-02, DISP-03, RESUME-01
**Success Criteria** (what must be TRUE):
  1. A Task dispatches only when ALL its global dependencies are complete (global indegree 0 vs the completed-task set), regardless of which Plan/Phase/Milestone authored it.
  2. When a task fails, its independent siblings in the same global wave continue, its global dependents are never dispatched (their global indegree never reaches zero), and non-dependents dispatch in strict / halt in conservative — exactly the spec §"Failure handling at wave boundaries" contract, now at global scope.
  3. A gate (milestone/phase/plan/task approve) withholds a globally-ready Task until approved and releases it on approval without bypassing dependency readiness; human-gate-policy stays configurable per Project (controller reads policy, does not bake it in).
  4. An orchestrator restart re-derives the entire Project execution schedule from the global indegree map + completed-task set alone, with no other persisted execution state and no cached schedule.
**Plans**: TBD

### Phase 26: Multi-Milestone Drive + Spec Conformance
**Goal**: A single Project drives multiple Milestones end-to-end via the Milestone DAG, with Tasks from different Milestones sharing global waves and per-milestone gate policy composing across the DAG — and the README cross-plan/cross-phase/cross-milestone worked example is pinned as an executable conformance test.
**Depends on**: Phase 25 (global dispatch + gates + failure semantics)
**Requirements**: MS-01, MS-02, MS-03, SPEC-01
**Success Criteria** (what must be TRUE):
  1. Planning emits a Milestone DAG from `Milestone.dependsOn` (schema-present, never exercised), and every milestone's Tasks join the single global Execution DAG so one Project drives multiple Milestones.
  2. A Task in one Milestone can share a global wave with a Task in another Milestone, and cross-milestone task dependencies are expressible and honored (the literal README execution example).
  3. Milestone-level gate policy composes across the Milestone DAG — approve-every-milestone works for N milestones, and full-auto and full-supervised remain expressible.
  4. The README execution-DAG worked example (tasks α…θ, cross-plan/phase/milestone edges) is encoded as an executable test that produces the documented global wave schedule `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`, and the README and implementation agree.
**Plans**: TBD

<details>
<summary>📋 vNext — OpenAI Backend + Dogfood Run #2 (Planned)</summary>

Scope TBD. Extends credproxy route allowlist for OpenAI paths, wires an OpenAI provider into the dispatch chain, and runs dogfood run #2. Gated on v1.0.2 Spring Tide landing a correct execution layer.

</details>

<details>
<summary>📋 v1.x — Polyglot Subagent Runtimes: LangGraph Strategy (Backlog)</summary>

Architecture locked; task breakdown deferred. The `claude` CLI subagent becomes one named strategy behind the existing `pkg/dispatch.Subagent` image contract; a second Python/LangGraph container image implements the same envelope contract for full agent-loop parity. Sequenced after v1.0.2 "Spring Tide" and after the OpenAI-backend milestone.

See [milestones/v1.x-polyglot-subagent-MILESTONE.md](milestones/v1.x-polyglot-subagent-MILESTONE.md) for the full framing: parity inventory, contract-conformance table, provider-firewall gap analysis, alternatives considered, and open questions.

</details>

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1–11 (see archive) | v1.0.0 | 137/137 | Complete | 2026-06-11 |
| 12. Gate Semantics + Reject/Resume | v1.0.1 | 5/5 | Complete | 2026-06-11 |
| 13. Dispatch Image Resolution + Provider Halt | v1.0.1 | 7/7 | Complete | 2026-06-11 |
| 14. Budget Enforcement + Pricing | v1.0.1 | 7/7 | Complete | 2026-06-12 |
| 15. Paper Cuts | v1.0.1 | 7/7 | Complete | 2026-06-12 |
| 16. Telemetry Completion | v1.0.1 | 8/8 | Complete | 2026-06-12 |
| 17. Tech Debt — Plan Label Backfill + Gate Hardening | v1.0.1 | 4/4 | Complete | 2026-06-13 |
| 18. Eval Harness | v1.0.2 (Ebb, superseded) | 3/3 | Complete | 2026-06-15 |
| 19. Template Reorder + Token Minimization | v1.0.2 (Ebb, superseded) | 4/4 | Complete | 2026-06-15 |
| 20. SharedContext Injection + Cache Verification Spike | v1.0.2 (Ebb, superseded) | 5/5 | Complete | 2026-06-16 |
| 21. Cost & Cache Observability | v1.0.2 (Ebb, superseded) | 2/2 | Needs Review | - |
| 22. Dashboard Embed Freshness Fix | v1.0.2 (Spring Tide) | 2/2 | Complete   | 2026-06-16 |
| 23. Schema Migration + Cross-Scope Dependency Model | v1.0.2 (Spring Tide) | 0/0 | Not started | - |
| 24. Global Wave Derivation Engine | v1.0.2 (Spring Tide) | 0/0 | Not started | - |
| 25. Global Dispatch, Failure Semantics, Gates & Resumption | v1.0.2 (Spring Tide) | 0/0 | Not started | - |
| 26. Multi-Milestone Drive + Spec Conformance | v1.0.2 (Spring Tide) | 0/0 | Not started | - |
</content>
</invoke>
