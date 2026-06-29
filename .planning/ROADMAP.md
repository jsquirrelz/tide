# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** — Phases 1–11 (shipped 2026-06-11) — ⚠ shipped on an invalid execution foundation (per-plan waves; see v1.0.2 Spring Tide)
- ✅ **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** — Phases 12–17 (shipped 2026-06-13) — ⚠ same invalid foundation
- ⊘ **v1.0.2 — Ebb Tide: Token & Cost Optimization** — Phases 18–21 (completed; **SUPERSEDED — will not be released**, artifacts preserved). Superseded after dogfood run #2 surfaced the per-plan-waves defect.
- ✅ **v1.0.2 — Spring Tide: Global Execution DAG (severe corrective patch)** — Phases 22–26 (complete; **shipped within the v1.0.3 tag**, not separately tagged). Re-architected execution to ONE global Execution DAG spanning the entire Project — the patch that makes the Topologically-Indexed paradigm real. Superseded Ebb Tide.
- ✅ **v1.0.3 — Spring Tide + Planning Resumption & Cost Resilience** — Phases 22–29 (shipped 2026-06-25, tag `v1.0.3`, published: 7 images + 2 OCI charts). Global Execution DAG end-to-end (22–26) + operator resumption tooling (27–29): budget-bypass resume correctness, plan-import core, and `tide` export/import-envelopes with a kind E2E acceptance gate.
- ✅ **v1.0.4 — tide-import image publish + release-matrix guardrail** — (shipped 2026-06-25, tag `v1.0.4`, published). Patch: publishes the `tide-import` image in the build-images matrix and adds the matrix↔chart image-coverage release gate.
- ✅ **v1.0.5 — Resumable Import: Partial-Tree Resume** — Phase 30 (shipped 2026-06-27, tag `v1.0.5`, published: 8 images + 2 OCI charts + 5 binaries @ 1.0.5, verified anon). adopt-complete + re-plan-incomplete: fixes the v1.0.3 import defect dogfood run #2 surfaced (incomplete-envelope nodes materialized as `Running`-with-no-envelope zombies → stall). Unblocks deferred dogfood run #2. Requirements: RESUME-PARTIAL-01..04 (see REQUIREMENTS.md "v1.0.5 Requirements").
- ✅ **v1.0.6 — Adoption-Path Correctness & Dispatch Safety** — Phases 31–33 (shipped 2026-06-29, tag `v1.0.6`, published: 8 images + 2 OCI charts + 5 binaries @ 1.0.6, verified anon). Corrective patch closing the four code-level defects dogfood run #2b surfaced on the adoption path: D1+D2 lifecycle/cost seam (Phase 31), D3 dispatch concurrency cap (Phase 32), D4 planner failure semantics (Phase 33). Audit: tech_debt (13/13 reqs, 0 blockers). Full archive: [milestones/v1.0.6-ROADMAP.md](milestones/v1.0.6-ROADMAP.md) · [milestones/v1.0.6-REQUIREMENTS.md](milestones/v1.0.6-REQUIREMENTS.md) · [milestones/v1.0.6-MILESTONE-AUDIT.md](milestones/v1.0.6-MILESTONE-AUDIT.md).
- 🚧 **v1.0.7 — Flood Tide: TIDE-on-TIDE Self-Hosting Proof** — Phases 34–39 (in progress). Operations/dogfood milestone: the human operates TIDE while **TIDE builds the entire OpenAI backend** — drive a *completing* dogfood run #2 on a slightly-bigger single-node cluster under a hard $100 cap, then review (not merge) TIDE's authored output. Ships the two load-bearing v1.0.7 audit carry-ins (configmap `plannerConcurrency` `16→4`; project-level rollup-marker hardening) as real release artifacts. 16 requirements (PREFLIGHT/INFRA/IMPORT/RUN/REVIEW/RELEASE).
- 📋 **vNext — Extend TIDE: Mergeable OpenAI Backend** — (planned; seeded by v1.0.7's REVIEW output). Rework TIDE's authored OpenAI backend into a mergeable provider-agnostic `Subagent` implementation behind the interface (EXTEND-01), with live functional parity via the CACHE-F1 direct-SDK path (EXTEND-02).
- 📋 **v1.x — Polyglot Subagent Runtimes: LangGraph Strategy** — (backlog; architecture locked, phases TBD) — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md)

## Phases

### 🚧 v1.0.7 — Flood Tide: TIDE-on-TIDE Self-Hosting Proof (In Progress)

**Milestone Goal:** Drive a *completing* dogfood run #2 end-to-end — TIDE orchestrating Claude subagents to build the OpenAI backend — on a slightly-bigger single-node cluster under a hard $100 cap, proving the five-level paradigm self-hosts; then review TIDE's authored output to feed a follow-up "extend TIDE" pass. The phase chain is forced sequential: harden → deploy → import → operate → review → release. **No hand-written backend code this milestone — the backend is TIDE's output, reviewed not merged.**

- [ ] **Phase 34: Pre-flight Tech-Debt Hardening** — Land the two load-bearing v1.0.7 audit carry-ins before launch (configmap `plannerConcurrency` `16→4`; project-level rollup-marker exactly-once hardening)
- [ ] **Phase 35: Infra + Fresh v1.0.7 Deploy** — Stand up a slightly-bigger single-node kind cluster and deploy current-version v1.0.7 TIDE (carrying the PREFLIGHT fixes) wired to the real Anthropic key via credproxy
- [ ] **Phase 36: Salvaged-Tree Import + Dry-Run + Tuning** — Import/adopt the `salvage-20260618` tree, cost-project before launch, set `absoluteCapCents=$100`, and tune dispatch concurrency to the node's memory ceiling
- [ ] **Phase 37: Launch + Operate Run #2 to Completion** — Launch and babysit run #2 to `Project=Complete` under the $100 cap without OOM, root-fixing surfacing orchestrator defects, and deliver a mid-execution dashboard screenshot
- [ ] **Phase 38: Output Review + Extraction** — Code-review TIDE's authored OpenAI backend (expected not mergeable), extract a run retrospective, and record cherry-pick candidates to seed the follow-up "extend TIDE" milestone
- [ ] **Phase 39: Release v1.0.7** — Publish v1.0.7 (images + charts + binaries) carrying the PREFLIGHT tech-debt fixes, verified anonymously on ghcr

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

### ✅ v1.0.2 — Spring Tide: Global Execution DAG (Complete — shipped within tag v1.0.3)

**Milestone Goal:** Re-architect execution so waves are derived from ONE global Execution DAG spanning the entire Project (all milestones/phases/plans), assembled after planning completes — making the Topologically-Indexed paradigm real.

- [x] **Phase 22: Dashboard Embed Freshness Fix** — Published images can never ship an SPA bundle older than source; verified against the Telemetry tab
- [x] **Phase 23: Schema Migration + Cross-Scope Dependency Model** — Breaking CRD changes (Wave re-owned to Project scope, global `wave` label) with a migration path, plus cross-plan/phase/milestone task deps reconciled into one global DAG with cyclic rejection
- [x] **Phase 24: Global Wave Derivation Engine** — Assemble ONE global Execution DAG after planning and derive global waves via layered Kahn; the bidirectional global wave index, re-derived O(V+E) with no cached schedule
- [x] **Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption** — Dispatch off the global indegree map vs the completed-task set; wave-boundary failure contract preserved exactly at global scope; gates compose as holds; restart re-derives the whole schedule
- [x] **Phase 26: Multi-Milestone Drive + Spec Conformance** — A Project drives multiple Milestones via the Milestone DAG with cross-milestone global waves and per-milestone gate policy composing across the DAG; the README worked example is an executable conformance test

### ✅ v1.0.3 — Planning Resumption & Cost Resilience (Shipped 2026-06-25, tag v1.0.3)

**Milestone Goal:** Make interrupted or budget-halted TIDE runs cheaply resumable.

- [x] **Phase 27: Budget-Bypass Resume Correctness** — Fix the three identified bypass-path bugs and add regression coverage for the `2a5e0dc` ordering fix
- [x] **Phase 28: Plan-Import Core** — Design checkpoint resolved; envelope-import with UID-churn bridge, completeness validation, cycle detection, and no imported Wave CRs
- [x] **Phase 29: Operator Tooling + E2E** — `tide` CLI import/export commands and the kind integration test proving end-to-end resumption against the `salvage-20260618` fixture

### ✅ v1.0.5 — Resumable Import: Partial-Tree Resume (Shipped 2026-06-27, tag v1.0.5)

- [x] **Phase 30: Resumable Import — Partial-Tree Resume** — adopt-complete + re-plan-incomplete driven by shared `IsEnvelopeComplete`; Tier-c kind E2E drives a mixed partial import to `Project=Complete`

### ✅ v1.0.6 — Adoption-Path Correctness & Dispatch Safety (Shipped 2026-06-29, tag v1.0.6)

**Milestone Goal:** Close the four code-level defects dogfood run #2b surfaced on the v1.0.5 import/adoption path — so a completing TIDE-on-TIDE run can be relaunched without spending blind or OOM'ing the node. All fixes are narrow seam edits on existing controller code: no new CRDs, no new go.mod entries, no new persistence surface.

- [x] **Phase 31: D2+D1 — Adoption Lifecycle Seam** — Project advances to `Running` on `ImportComplete=True` (D2), which enables budget rollup and cap enforcement on adopted projects (D1); idempotency guards prevent re-dispatch of the project-planner and double-counting after reporter-Job TTL-GC
- [x] **Phase 32: D3 — Dispatch Concurrency Cap** — Per-level max-in-flight planner cap at steady state, configurable from `charts/tide/values.yaml`, with a sane single-node default
- [x] **Phase 33: D4 — Planner Failure Semantics** — A phase or milestone whose planner exits nonzero with zero children is marked `Failed`, not `Succeeded`; shared `isPlannerFailure` helper across levels; operator recovery via `tide resume --retry-failed`

## Phase Details

### Phase 34: Pre-flight Tech-Debt Hardening

**Goal**: The two load-bearing v1.0.7 audit carry-ins land in the controller and chart before run #2 launches — the single-node OOM-safety default (`plannerConcurrency 16→4`) and exactly-once project-level cost rollup — so the run can be deployed and metered without a latent over-dispatch or a double-counted cap.
**Depends on**: Phase 33 (v1.0.6 adoption-path correctness shipped; these are its carried-in audit debt)
**Requirements**: PREFLIGHT-01, PREFLIGHT-02
**Success Criteria** (what must be TRUE):

  1. Rendering the chart with defaults yields `plannerConcurrency: 4` in the configmap (the prior `16` is gone), and a controller-level assertion confirms a fresh default deploy dispatches at most 4 concurrent planners.
  2. An envtest proves the project-level rollup marker (`PlannerRolledUpUID` / equivalent) follows the milestone/phase exactly-once pattern: a second reconcile after reporter-Job TTL-GC does not increment the project's `CostSpentCents` a second time for the same Job.
  3. The hardened project-level marker stamp uses the same `RetryOnConflict` + re-fetch pattern as the milestone/phase markers (no best-effort last-write-wins window remains at the project level).

**Plans**: 2 plans

- [x] 34-01-PLAN.md — Fix configmap plannerConcurrency Helm fallback 16→4 + pure-Go default-cap assertion + kind helm-template render contract test (PREFLIGHT-01)
- [x] 34-02-PLAN.md — Harden project-level PlannerRolledUpUID stamp to RetryOnConflict+re-fetch+optimistic-lock + project-level no-double-count-across-TTL-GC envtest (PREFLIGHT-02)

### Phase 35: Infra + Fresh v1.0.7 Deploy

**Goal**: A fresh, *slightly*-bigger single-node kind cluster is running current-version v1.0.7 TIDE (images + charts carrying the Phase-34 PREFLIGHT fixes), wired to the real Anthropic key through credproxy so subagents dispatch against the real API — the deploy surface run #2 launches on, with the node memory ceiling documented.
**Depends on**: Phase 34 (the deployed artifacts must carry the PREFLIGHT fixes)
**Requirements**: INFRA-01, INFRA-02, INFRA-03
**Success Criteria** (what must be TRUE):

  1. A fresh single-node kind cluster sized slightly above the ~8GiB host (well under 16GB) is up, and its node memory ceiling is documented in the run artifacts.
  2. `helm`/`kubectl` show v1.0.7 TIDE components running on the cluster (not stale, local pre-Spring-Tide, or older-tag artifacts) — the deployed manager image carries the Phase-34 `plannerConcurrency=4` default.
  3. One successful real subagent dispatch against the live Anthropic API completes via credproxy + the key at `~/.tide/anthropic.key` (smoke), confirming creds are wired before any cost-bearing run.

**Plans**: TBD

### Phase 36: Salvaged-Tree Import + Dry-Run + Tuning

**Goal**: The `salvage-20260618` tree (3 Milestones / 15 Phases) is imported and validated onto the fresh cluster, a cost projection is produced so expected spend against the cap is known, `absoluteCapCents=10000` ($100) is set on the Project, and effective planner/executor concurrency is tuned to the node's memory ceiling — so launch proceeds with a known cost envelope and no OOM risk.
**Depends on**: Phase 35 (a deployed, credentialed cluster to import into)
**Requirements**: IMPORT-01, IMPORT-02, IMPORT-03
**Success Criteria** (what must be TRUE):

  1. `tide import-envelopes` adopts the `salvage-20260618` tree onto the cluster and it passes validation (cycle-detection + completeness) before any dispatch — observable as the Project reaching `ImportComplete` with no `ImportFailed`/`CyclicPlanDetected` condition.
  2. A cost-projection / dry-run is produced and recorded before launch, giving expected spend against the $100 cap; `absoluteCapCents=10000` is set and present on the Project CR.
  3. Effective dispatch concurrency (planner and executor pools, separately sized) is chosen to fit the documented node memory ceiling and the chosen values are recorded — the cascade's worst-case in-flight pod count cannot OOM the single node.

**Plans**: TBD

### Phase 37: Launch + Operate Run #2 to Completion

**Goal**: Run #2 is launched and driven to `Project=Complete` on the single node, under the $100 cap, without OOM — the human babysits budget/waves/OOM headroom, root-fixes any orchestrator defects that surface (the v1.0.6 D1–D4 pattern, each with a symptom-reproducing test), and captures + delivers a mid-execution dashboard screenshot. This phase proves the paradigm self-hosts and produces TIDE's authored OpenAI backend as output.
**Depends on**: Phase 36 (imported tree + cap + tuned concurrency)
**Requirements**: RUN-01, RUN-02, RUN-03, RUN-04
**Success Criteria** (what must be TRUE):

  1. The Project reaches `Project=Complete` on the single-node cluster with no OOM event during the run.
  2. Total spend stays within the $100 cap; if the cap halts the run, the human is asked before any cap raise or resume — no blind spend (a clean cap-halt followed by an explicit human approve-to-continue is an acceptable path to completion).
  3. A mid-execution dashboard screenshot is captured during the run and delivered to the user.
  4. Any orchestrator defect that surfaces during the run is root-fixed in-repo with a symptom-reproducing test and the run is relaunched/resumed — defects are fixed at the root, not worked around.

**Plans**: TBD
**UI hint**: yes

### Phase 38: Output Review + Extraction

**Goal**: TIDE's authored OpenAI backend is collected from the run output and code-reviewed for quality/correctness (expected *not* mergeable as-is), a retrospective captures what the paradigm got right/wrong plus surfaced defects and cost/perf, and cherry-pick candidates (keep vs rework) are recorded to seed the follow-up "extend TIDE" milestone.
**Depends on**: Phase 37 (a completed run with authored backend output to review)
**Requirements**: REVIEW-01, REVIEW-02, REVIEW-03
**Success Criteria** (what must be TRUE):

  1. TIDE's authored OpenAI backend code is collected from the run output and a code review is recorded assessing quality/correctness (with the expectation, confirmed or refuted, that it is not mergeable as-is).
  2. A retrospective artifact extracts the run's learnings — what the five-level paradigm got right/wrong, defects surfaced, and cost/performance observations.
  3. Cherry-pick candidates (what to keep vs rework) from the backend output are identified and recorded to seed the follow-up "extend TIDE" milestone (EXTEND-01/02 inputs).

**Plans**: TBD

### Phase 39: Release v1.0.7

**Goal**: v1.0.7 is published (images + charts + binaries) carrying the Phase-34 PREFLIGHT tech-debt fixes as real release artifacts, verified anonymously on ghcr — so the OOM-safety default and rollup hardening ship, not just sit in main.
**Depends on**: Phase 38 (run proven complete and output reviewed; release closes the milestone)
**Requirements**: RELEASE-01
**Success Criteria** (what must be TRUE):

  1. A `v1.0.7` tag publishes images + charts + binaries (matching the established release-matrix coverage) carrying the PREFLIGHT fixes — the chart's appVersion is bumped as release step one to avoid version skew.
  2. The published v1.0.7 artifacts are verified anonymously on ghcr (pullable without auth), and a GitHub Release is live.

**Plans**: TBD

<details>
<summary>✅ v1.0.0 / v1.0.1 / v1.0.2 (Spring Tide) / v1.0.3 / v1.0.5 / v1.0.6 — Phase Details (Phases 22–33)</summary>

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
  4. Wave derivation/ownership is moved off `Plan` to the global (Project) scope, and the locked metric label set `{project,phase,plan,wave}` is preserved with `wave` resemanticized to the global index (the `task` label stays forbidden per the metric-cardinality analyzer).
  5. A documented migration/conversion path carries an in-flight Project from the old per-plan schema to the new global schema with a version bump and no silent data loss.

**Plans**: 4 plans

- [x] 23-01-PLAN.md — Introduce api/v1alpha2 (WaveSpec re-owned Plan→Project, dependsOn broadened any-level, storageversion moved, schemaRevision discriminator); regen deepcopy/CRDs; extend verify-no-aggregates glob (SCHEMA-01, DEPS-01, DEPS-02)
- [x] 23-02-PLAN.md — Migration wiring: register v1alpha2 scheme, mark v1alpha1 unserved, delete conversion Hub(), re-register D-B1 Wave webhook for v1alpha2, filter per-plan cycle webhook to task-only edges, stub materializeWaves + wave_controller against v1alpha2 (Phase-24 TODOs), write reinstall migration doc (SCHEMA-03, DEPS-01)
- [x] 23-03-PLAN.md — Controller guards: old-object fail-closed RequiresReinstall guard + global cross-scope cycle gate (involved nodes surfaced), confirm wave metric label is global-sourced + lock {project,phase,plan,wave} arity (SCHEMA-02, SCHEMA-03, DEPS-03)
- [x] 23-04-PLAN.md — Consumer migration (gap closure): repoint api/v1alpha1 import path → api/v1alpha2 across all ~137 consumer files; resolve 3 semantic deltas (Wave PlanRef→ProjectRef, test SchemaRevision, webhook FileTouch helper relocation v1alpha1→v1alpha2); flip controller For()/Owns() to v1alpha2 GVKs; migrate envtest suite — operator compiles/vets/runs on the served version (SCHEMA-03)

### Phase 24: Global Wave Derivation Engine

**Goal**: Once project planning completes, the orchestrator assembles ONE global Execution DAG of every Task across all Milestones/Phases/Plans and derives a single monotonic wave schedule by layered Kahn — queryable both directions and re-derived cheaply with no cached schedule.
**Depends on**: Phase 23 (cross-scope deps + global-scope Wave ownership)
**Requirements**: EXEC-01, EXEC-02, EXEC-03, EXEC-04
**Success Criteria** (what must be TRUE):

  1. After planning completes and before any execution dispatch, the orchestrator has assembled a single global Execution DAG containing every Task in the Project across all Milestones/Phases/Plans.
  2. Waves are derived by layered Kahn over that global DAG and carry global, monotonic wave indices — not per-plan `tide-wave-<plan.UID>-<i>` indices.
  3. Given any Task you can resolve its global wave, and given any global wave you can list its Tasks (the README:54 namesake invariant holds Project-wide, not just within a plan).
  4. Adding or completing a task re-derives the whole Project's waves in O(V+E) from the DAG + completed-task set with no schedule cached in `.status` (PERSIST-03 guards still pass).

**Plans**: 4 plans

- [x] 24-01-PLAN.md — Wave 0 envtest scaffold: global-derivation test (README worked example, RED) + cross-scope fixture helpers (EXEC-01..04 contract)
- [x] 24-02-PLAN.md — Extend assembleProjectDepGraph to full fan-out over all four dependsOn carriers (in-memory, de-duped); assemble-once refactor sharing (nodes,edges) with the cycle gate (EXEC-01)
- [x] 24-03-PLAN.md — deriveGlobalWaves + stampGlobalTaskLabels: Project-scoped Wave CRs (tide-wave-<project>-<N>, create/prune, exactly-once metric) + global wave-index label + Owns(&Wave{}); no cached schedule (EXEC-02/03/04)
- [x] 24-04-PLAN.md — Remove per-plan materializeWaves/stampTaskLabels + Owns(&Wave{}); close the four WaveReconciler Phase-24 TODOs (O(1) global mapper); full test-int + verify-guard gate (EXEC-02/03)

### Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption

**Goal**: Execution dispatches off ONE global indegree map versus the completed-task set, the wave-boundary failure contract holds exactly at global scope, gates compose as holds over the global scheduler, and an orchestrator restart re-derives the entire schedule from minimal state.
**Depends on**: Phase 24 (global wave index + re-derivation)
**Requirements**: DISP-01, DISP-02, DISP-03, RESUME-01
**Success Criteria** (what must be TRUE):

  1. A Task dispatches only when ALL its global dependencies are complete (global indegree 0 vs the completed-task set), regardless of which Plan/Phase/Milestone authored it.
  2. When a task fails, its independent siblings in the same global wave continue, its global dependents are never dispatched (their global indegree never reaches zero), and non-dependents dispatch in strict / halt in conservative — exactly the spec §"Failure handling at wave boundaries" contract, now at global scope.
  3. A gate (milestone/phase/plan/task approve) withholds a globally-ready Task until approved and releases it on approval without bypassing dependency readiness; human-gate-policy stays configurable per Project (controller reads policy, does not bake it in).
  4. An orchestrator restart re-derives the entire Project execution schedule from the global indegree map + completed-task set alone, with no other persisted execution state and no cached schedule.

**Plans**: 3 plans

- [x] 25-01-PLAN.md — API vocabulary (FailureProfile enum + FailureHalt condition) + Nyquist Wave 0 RED test scaffolds (DISP-01/02/03, RESUME-01) + A1 coarse-ref grep
- [x] 25-02-PLAN.md — Global dispatch: shared coarse-ref fan-out resolver (depgraph.go) + global computeIndegree/listProjectTasks + globalDependentsMapper watch (DISP-01, DISP-03, RESUME-01)
- [x] 25-03-PLAN.md — Failure semantics: failure_halt.go + checkFailureHalt at four execution dispatch sites + tide resume --retry-failed clear + wave-prune guard (DISP-02)

### Phase 26: Multi-Milestone Drive + Spec Conformance

**Goal**: A single Project drives multiple Milestones end-to-end via the Milestone DAG, with Tasks from different Milestones sharing global waves and per-milestone gate policy composing across the DAG — and the README cross-plan/cross-phase/cross-milestone worked example is pinned as an executable conformance test.
**Depends on**: Phase 25 (global dispatch + gates + failure semantics)
**Requirements**: MS-01, MS-02, MS-03, SPEC-01
**Success Criteria** (what must be TRUE):

  1. Planning emits a Milestone DAG from `Milestone.dependsOn` (schema-present, never exercised), and every milestone's Tasks join the single global Execution DAG so one Project drives multiple Milestones.
  2. A Task in one Milestone can share a global wave with a Task in another Milestone, and cross-milestone task dependencies are expressible and honored (the literal README execution example).
  3. Milestone-level gate policy composes across the Milestone DAG — approve-every-milestone works for N milestones, and full-auto and full-supervised remain expressible.
  4. The README execution-DAG worked example (tasks α…θ, cross-plan/phase/milestone edges) is encoded as an executable test that produces the documented global wave schedule `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`, and the README and implementation agree.

**Plans**: 4 plans

- [x] 26-01-PLAN.md — D-01 N-milestone project_planner template (+ golden/ratchet, idempotency guard on Job existence) + D-03 §6d milestone fan-out removal + README planning-DAG-edge note + DEPS-02 reinterpretation (MS-01, MS-02)
- [x] 26-02-PLAN.md — Carried-in debt: D-08 OQ-3 wave-aggregator ZeroMembers phase + in-flight-safe prune guard (CR-01 PruneShrink stays green) + D-09 WR-02 globalDependentsMapper watch predicate + unit test (MS-02, SPEC-01)
- [x] 26-03-PLAN.md — D-06 SPEC-01 + MS-03 conformance envtest: 2-milestone α…θ fixture (cross-milestone γ→η), assert `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`; N milestone planning-hold composition; cross-linked to README (SPEC-01, MS-01, MS-02, MS-03)
- [x] 26-04-PLAN.md — D-07 GlobalExecutionDAGView + GET /api/v1/projects/{name}/execution-dag + EmptyState variants + App wiring (embed regenerated); live-cluster screenshots of the SPEC-01 fixture replace both README mermaid diagrams (SPEC-01)

### Phase 27: Budget-Bypass Resume Correctness

**Goal**: A budget-halted Project resumes at `Running` without re-initializing the workspace or double-counting planning cost, cap-raise ergonomics no longer require raising both caps in lockstep, and the `2a5e0dc` planner-completion ordering fix has regression coverage — all without touching the import path.
**Depends on**: Phase 26 (v1.0.2 Spring Tide complete; correctness baseline established)
**Requirements**: BYPASS-01, BYPASS-02, BYPASS-03, BYPASS-04, BYPASS-05
**Success Criteria** (what must be TRUE):

  1. Clearing a budget halt (`tideproject.k8s/bypass-budget`) resumes the project at `Running`, not `Pending` — no workspace re-init or re-clone Job fires when `Status.Git.BranchName` is already set.
  2. A resume never re-dispatches the clone Job when the workspace is already initialized — the guard is a durable `CloneComplete` status flag, not reporter-Job existence (TTL-GC-safe).
  3. Planning cost is rolled up exactly once across a halt-resume cycle — a durable `PlannerRolledUpUID` marker prevents double-count when the reporter Job has been garbage-collected during a halt.
  4. Raising the absolute budget cap alone clears a budget halt without the rolling-window cap immediately re-halting dispatch (both cap values are evaluated together before halting resumes).
  5. An envtest asserts that when the planner Job completes, the reporter Job spawns AND the planner cost rolls up while the planner Job still exists — locking in the `2a5e0dc` ordering fix against regression.

**Plans**: 4 plans

- [x] 27-01-PLAN.md — Add durable status fields (CloneComplete, PlannerRolledUpUID, BypassBaselineCents) + make manifests/generate; confirm QQH-01 ordering test GREEN baseline (D-06, BYPASS-05 verify-green)
- [x] 27-02-PLAN.md — BYPASS-01 bypass targets PhaseRunning on initialized projects + init-Job BranchName guard; BYPASS-02 durable CloneComplete clone-dispatch guard + set-on-success + idempotency envtest
- [x] 27-03-PLAN.md — BYPASS-03 PlannerRolledUpUID rollup-once guard in handleProjectJobCompletion; BYPASS-05 TTL-GC double-count companion envtest
- [x] 27-04-PLAN.md — BYPASS-04 acknowledged-spend baseline + which-cap observability in handleBudgetGate (D-04, overrides RESEARCH Pattern 4); IsCapExceeded unchanged + call-site audit + unit/envtest coverage

### Phase 28: Plan-Import Core

**Goal**: A fresh Project run adopts pre-authored planner envelopes and skips the planner for every level whose valid envelope already exists — resolving the UID-churn problem via a stable identity scheme, validating every envelope before adoption, running cycle detection before materializing any child CRDs, converting v1alpha1 schema, and never importing Wave CRs.
**Depends on**: Phase 27 (correct bypass path; import layered on a working resume mechanism)
**Requirements**: IMPORT-01, IMPORT-02, IMPORT-03, IMPORT-04, IMPORT-05
**Success Criteria** (what must be TRUE):

  1. A fresh `kubectl apply` of an already-planned Project adopts pre-authored envelopes and proceeds straight to materialize-then-execute, with no planner Jobs dispatched for levels whose valid envelope exists — confirmed by zero planner Pod appearances in the run log.
  2. An envelope is only adopted after passing a completeness-and-schema check (`len(ChildCRDs) == ChildCount`, correct `APIVersionV1Alpha1`, no partial-write): any incomplete, wrong-schema, or mismatched envelope causes the planner to run normally, and a valid-looking stale envelope is never silently adopted.
  3. Envelopes authored under prior CRD UIDs are matched to the new run's CRs by stable identity (object name + parent chain), with no cross-object or cross-project aliasing — UID churn does not produce incorrect envelope adoption.
  4. Before any child CRDs are created from an imported envelope, `dag.ComputeWaves` runs explicitly on the full task set; a cyclic or unresolved imported graph produces an `ImportFailed / CyclicPlanDetected` condition, no partial CRs are created, and Wave CRs are always re-derived by `deriveGlobalWaves` (never imported).
  5. Import is operator-gated and verifies envelope origin against the per-namespace PVC before materializing into the CRD API channel — no unverified third-party envelope reaches `client.Create`.

**Plans**: 5 plans

- [x] 28-01-PLAN.md — Chart FIXED contract: images.tideImport block + TIDE_IMPORT_IMAGE env (wave 1)
- [x] 28-02-PLAN.md — api/v1alpha2 schema: ImportSourceRef field + ImportComplete condition vocab + regen CRD/deepcopy (wave 1)
- [x] 28-03-PLAN.md — cmd/tide-import binary + Dockerfile: copy/rekey/atomic-rewrite + schema-convert + completeness/Kind/traversal validation (wave 2)
- [x] 28-04-PLAN.md — ImportController state machine: seed→materialize→rekey, cycle-detect-before-create, containment-scoped import Job (wave 2)
- [x] 28-05-PLAN.md — 5-site ImportComplete park guard + budget-rollup suppression + manager registration (wave 3)

### Phase 29: Operator Tooling + E2E

**Goal**: Operators can export a Project's planner envelopes to a portable bundle and import a bundle into a new run via the `tide` CLI, with a dry-run mode that reports what would be adopted vs re-planned — and a kind integration test proves end-to-end resumption against the real salvage fixture.
**Depends on**: Phase 28 (import mechanism correct and validated)
**Requirements**: TOOL-01, TOOL-02
**Success Criteria** (what must be TRUE):

  1. `tide export-envelopes` writes a portable bundle (tgz or directory) of a Project's planner envelopes from the per-namespace PVC that can be transported across cluster teardowns.
  2. `tide import-envelopes --dry-run` reports which envelopes would be adopted and which would be re-planned (schema mismatch, completeness failure, cycle) without writing anything — giving the operator a preview before committing to import.
  3. `tide import-envelopes` (live mode) seeds a new run with the exported bundle so the reconciler adopts valid envelopes on next reconcile, confirmed by the operator seeing zero planner Jobs for adopted levels.
  4. A kind integration test imports the `examples/projects/dogfood/salvage-20260618` fixture into a fresh cluster, lets the reconciler run, asserts all Milestones reach `Succeeded` with no planner Jobs dispatched for already-imported levels, and confirms no planning cost was re-paid.

**Plans**: 5 plans

- [x] 29-01-PLAN.md — pkg/bundle/ foundation: BundleEntry/BundleManifest (seed superset + sha256), zip-slip-safe tgz codec, childCount-stamp (D-16a), offline dry-run validator (schema + completeness + sha256 + ComputeWaves cycle) (TOOL-01)
- [x] 29-02-PLAN.md — `tide export-envelopes`: reused inspector pod (tar subtree) + seed-manifest generation from live CRs (FQName/oldUID/dependsOn/status/sha256) + legacy childCount repair + bundle assembly (TOOL-01)
- [x] 29-03-PLAN.md — `tide import-envelopes` + `--dry-run`: offline adopt/re-plan table + json + cycle hard-reject (D-07/08/09), live stage-only loader pod (SPDY exec) + seed ConfigMap + surfaced project.yaml (D-05/06) (TOOL-01)
- [x] 29-04-PLAN.md — one-time salvage childCount patch (D-16b) + small drain-to-Succeeded fixture (D-11a) + test-int-kind-prep tide CLI build (D-10) (TOOL-02)
- [x] 29-05-PLAN.md — kind E2E driving the real CLI: tier a small fixture → all-Milestones-Succeeded; tier b salvage → 0 `{milestone,phase}` planner Jobs + $0 re-paid (D-11b/D-17), long-test gated (D-12) (TOOL-02)

### Phase 30: Resumable Import — Partial-Tree Resume

**Goal**: Make the import feature resume a PARTIALLY-completed tree — the primary use case dogfood run #2 proved it could not handle. adopt-complete + re-plan-incomplete driven by per-node envelope completeness; Tier-c kind E2E drives a mixed partial import to `Project=Complete`.
**Depends on**: Phase 29 (import mechanism + Tier-a/b E2E)
**Requirements**: RESUME-PARTIAL-01, RESUME-PARTIAL-02, RESUME-PARTIAL-03, RESUME-PARTIAL-04
**Success Criteria** (what must be TRUE):

  1. A bundle where some nodes have complete envelopes and others have incomplete or missing envelopes is imported; complete nodes adopt their salvaged status and do not trigger a re-plan; incomplete nodes materialize with empty Status and re-plan from scratch — confirmed by the Tier-c E2E.
  2. No incomplete-envelope node ever materializes as `Running`-with-no-envelope; the zombie shape that stalled run #2 is structurally impossible under the new completeness-first materialization path.
  3. A post-`ImportComplete` project-planner guard prevents re-dispatch after the import finishes, even across manager restarts, via a durable adoption sentinel.
  4. A Tier-c kind E2E drives a mixed partial import all the way to `Project=Complete`, with no planner Jobs fired for the complete-envelope nodes and at least one re-plan Job fired for the incomplete node.

**Plans**: 3 plans

- [x] 30-01-PLAN.md — Export-time completeness bridge (shared IsEnvelopeComplete; incomplete/missing → empty Status) + per-node materialization envtest [RESUME-PARTIAL-01/04]
- [x] 30-02-PLAN.md — Tighten project-planner guard to ImportComplete+owned-Milestones (no post-import re-dispatch) + envtest [RESUME-PARTIAL-02]
- [x] 30-03-PLAN.md — Partial-tree fixture + Tier c kind E2E driving partial import to Project=Complete [RESUME-PARTIAL-03]

### Phase 31: D2+D1 — Adoption Lifecycle Seam

**Goal**: An adopted Project advances from `Initialized` to `Running` after `ImportComplete=True` without dispatching a project-planner Job (D2), and as a result the budget meter accrues spend and enforces `absoluteCapCents` correctly on the adoption path (D1) — closing the "spent blind" failure at one shared call site in `reconcileProjectPlannerDispatch`.
**Depends on**: Phase 30 (import-resume foundation; Phase 31 is the first seam fix on top of it)
**Requirements**: ADOPT-01, ADOPT-02, ADOPT-03, ADOPT-04, ADOPT-05
**Success Criteria** (what must be TRUE):

  1. An adopted Project transitions from `Initialized` to `Running` after `ImportComplete=True` is set, with no project-planner Job dispatched — observable as zero `role=project-planner` Jobs in the namespace after the condition is set.
  2. As milestone/phase/plan planners complete under an adopted Project, `Project.Status.Budget.CostSpentCents` and `TokensSpent` increase — observable by watching the Project CR's status as downstream Jobs complete.
  3. When an adopted Project's `absoluteCapCents` is exceeded, the budget halt fires and the cascade stops dispatching new Jobs — observable as a `BudgetBlocked` condition on the Project CR and no further planner Jobs appearing.
  4. Budget rollup is exactly-once per reporter Job across halt→resume cycles and after reporter-Job TTL-GC — a second reconcile after the 300-second GC window does not increment `CostSpentCents` a second time for the same Job.
  5. The normal (non-import) Project lifecycle is unchanged — envtest confirms a non-import Project still dispatches a project-planner Job and advances normally, and a manager restart on an adopted-but-Running Project does not re-dispatch the project-planner.

**Plans**: 3 plans

- [x] 31-01-PLAN.md — API types: ConditionProjectPlannerSuppressed + per-child-level PlannerRolledUpUID markers; regenerate DeepCopy + CRD manifests
- [x] 31-02-PLAN.md — D2 seam: durable suppression short-circuit + single-patch Initialized→Running advance before pool acquire; envtest ADOPT-01/03/05
- [x] 31-03-PLAN.md — D1 idempotency: marker-gated exactly-once child rollup (milestone/phase/plan) across reporter-Job TTL-GC; envtest ADOPT-02/04

### Phase 32: D3 — Dispatch Concurrency Cap

**Goal**: In-flight planner Jobs are bounded at steady state by a configurable per-level cap (`plannerConcurrency`) so the planning cascade cannot OOM a single-node cluster; the cap parks excess dispatches rather than silently truncating a wave; planner and executor pools remain separately sized.
**Depends on**: Phase 31 (adoption seam fixed; Phase 32 is independent of D1/D2 but follows by priority)
**Requirements**: CONCUR-01, CONCUR-02, CONCUR-03, CONCUR-04
**Success Criteria** (what must be TRUE):

  1. With `plannerConcurrency=N`, at most N planner Jobs are Running simultaneously in the cluster at steady state, regardless of how many reconcile rounds fire — observable by watching `kubectl get jobs -l tideproject.k8s/role=planner` while 5+ Milestones are enqueued.
  2. `plannerConcurrency` defaults to a value that is safe for a single-node kind cluster (canonical value set in planning) in `charts/tide/values.yaml`, with the prior `16` value no longer present.
  3. The executor pool (`executorConcurrency`) is unchanged; `make lint` passes the cross-pool analyzer with the pools remaining separately sized.
  4. A dispatch deferred by the cap emits a log line identifying the deferred level and requeues — it is never silently dropped, and the operator can observe a stalled wave by seeing the log lines accumulate without new Jobs starting.

**Plans**: 2 plans

- [x] 32-01-PLAN.md — D3 dispatch concurrency cap: plannerInFlightCount gate before pool acquire at all four sites + default 16→4 (CONCUR-01..04)
- [x] 32-02-PLAN.md — Carried-in hardening: RetryOnConflict marker stamps (WR-02/03) + suppression-patch comment fix (WR-01) + single-patch test (WR-04)

### Phase 33: D4 — Planner Failure Semantics

**Goal**: A phase or milestone whose planner exits nonzero with zero children is marked `Failed` (not `Succeeded`), using a shared `isPlannerFailure` helper across both controllers — mirroring the Phase-30 plan-level guard — so a failed planner cannot corrupt the planning DAG by falsely advancing its parent.
**Depends on**: Phase 31 (adoption seam; independent of D3, sequenced after 32 by severity)
**Requirements**: PLANFAIL-01, PLANFAIL-02, PLANFAIL-03, PLANFAIL-04
**Success Criteria** (what must be TRUE):

  1. A phase planner that exits nonzero with zero children produced results in `Phase.Status.Phase=Failed` — observable in the Phase CR and confirmed by envtest with `exitCode=1, childCount=0`.
  2. A milestone planner that exits nonzero with zero children results in `Milestone.Status.Phase=Failed` — same guard and helper applied at the milestone controller level.
  3. A genuine leaf planner that exits zero with zero children still transitions to `Succeeded` — the fail check is ordered before the succeed check and requires `exitCode != 0`; envtest with `exitCode=0, childCount=0` remains green.
  4. A falsely-Failed phase or milestone is recoverable via `tide resume --retry-failed` without triggering a controller retry storm — the guard patches a permanent `Failed` condition rather than returning a Go error, and no automatic retry loop fires.

**Plans**: 3 plans

- [x] 33-01-PLAN.md — shared isPlannerFailure helper + ReasonPlannerFailed constant + unit test (Wave 1)
- [x] 33-02-PLAN.md — carried-in D3 sizing-policy doc fix in values.yaml (Wave 1, parallel)
- [x] 33-03-PLAN.md — patchPhaseFailed/patchMilestoneFailed helpers + guard insertion at both sites + envtests PLANFAIL-01/02/03 + resume recovery PLANFAIL-04 (Wave 2)

</details>

<details>
<summary>📋 vNext — Extend TIDE: Mergeable OpenAI Backend (Planned)</summary>

Scope seeded by v1.0.7's REVIEW output (Phase 38 cherry-pick candidates). Rework TIDE's authored OpenAI backend into a mergeable, provider-agnostic `Subagent` implementation behind the interface (EXTEND-01), and prove live functional parity by driving a real subagent dispatch through the OpenAI backend end-to-end — the CACHE-F1 direct-SDK path that realizes cross-pod prompt caching (EXTEND-02).

</details>

<details>
<summary>📋 v1.x — Polyglot Subagent Runtimes: LangGraph Strategy (Backlog)</summary>

Architecture locked; task breakdown deferred. The `claude` CLI subagent becomes one named strategy behind the existing `pkg/dispatch.Subagent` image contract; a second Python/LangGraph container image implements the same envelope contract for full agent-loop parity. Sequenced after the OpenAI-backend milestone.

See [milestones/v1.x-polyglot-subagent-MILESTONE.md](milestones/v1.x-polyglot-subagent-MILESTONE.md) for the full framing: parity inventory, contract-conformance table, provider-firewall gap analysis, alternatives considered, and open questions.

</details>

## Progress

**Execution Order:**
v1.0.7 phases execute in numeric order: 34 → 35 → 36 → 37 → 38 → 39 (forced sequential — each phase's deliverable is the next's prerequisite).

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
| 22. Dashboard Embed Freshness Fix | v1.0.2 (Spring Tide) | 3/3 | Complete | 2026-06-16 |
| 23. Schema Migration + Cross-Scope Dependency Model | v1.0.2 (Spring Tide) | 5/5 | Complete | 2026-06-16 |
| 24. Global Wave Derivation Engine | v1.0.2 (Spring Tide) | 4/4 | Complete | 2026-06-16 |
| 25. Global Dispatch, Failure Semantics, Gates & Resumption | v1.0.2 (Spring Tide) | 3/3 | Complete | 2026-06-17 |
| 26. Multi-Milestone Drive + Spec Conformance | v1.0.2 (Spring Tide) | 4/4 | Complete | 2026-06-17 |
| 27. Budget-Bypass Resume Correctness | v1.0.3 | 4/4 | Complete | 2026-06-18 |
| 28. Plan-Import Core | v1.0.3 | 5/5 | Complete | 2026-06-18 |
| 29. Operator Tooling + E2E | v1.0.3 | 5/5 | Complete | 2026-06-22 |
| 30. Resumable Import — Partial-Tree Resume | v1.0.5 | 3/3 | Complete | 2026-06-27 |
| 31. D2+D1 — Adoption Lifecycle Seam | v1.0.6 | 3/3 | Complete | 2026-06-28 |
| 32. D3 — Dispatch Concurrency Cap | v1.0.6 | 2/2 | Complete | 2026-06-29 |
| 33. D4 — Planner Failure Semantics | v1.0.6 | 3/3 | Complete | 2026-06-29 |
| 34. Pre-flight Tech-Debt Hardening | v1.0.7 | 2/2 | Complete   | 2026-06-29 |
| 35. Infra + Fresh v1.0.7 Deploy | v1.0.7 | 0/TBD | Not started | - |
| 36. Salvaged-Tree Import + Dry-Run + Tuning | v1.0.7 | 0/TBD | Not started | - |
| 37. Launch + Operate Run #2 to Completion | v1.0.7 | 0/TBD | Not started | - |
| 38. Output Review + Extraction | v1.0.7 | 0/TBD | Not started | - |
| 39. Release v1.0.7 | v1.0.7 | 0/TBD | Not started | - |
