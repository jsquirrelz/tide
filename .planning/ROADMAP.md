# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** — Phases 1–11 (shipped 2026-06-11) — ⚠ shipped on an invalid execution foundation (per-plan waves; see v1.0.2 Spring Tide)
- ✅ **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** — Phases 12–17 (shipped 2026-06-13) — ⚠ same invalid foundation
- ⊘ **v1.0.2 — Ebb Tide: Token & Cost Optimization** — Phases 18–21 (completed; **SUPERSEDED — will not be released**, artifacts preserved). Superseded after dogfood run #2 surfaced the per-plan-waves defect.
- 🚧 **v1.0.2 — Spring Tide: Global Execution DAG (severe corrective patch)** — Phases 22–26 (planning). Re-architect execution to ONE global Execution DAG spanning the entire Project — the patch that makes the Topologically-Indexed paradigm real. Supersedes Ebb Tide; preempts the OpenAI/dogfood milestone.
- 🚧 **v1.0.3 — Planning Resumption & Cost Resilience** — Phases 27–29 (in progress). Make interrupted or budget-halted TIDE runs cheaply resumable — a halt must never cost the already-authored plan.
- 📋 **vNext — OpenAI Backend + Dogfood Run #2** — (planned; gated on v1.0.3 landing plan-import/envelope-resumption)
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

- [x] **Phase 22: Dashboard Embed Freshness Fix** — Published images can never ship an SPA bundle older than source; verified against the Telemetry tab
- [ ] **Phase 23: Schema Migration + Cross-Scope Dependency Model** — Breaking CRD changes (Wave re-owned to Project scope, global `wave` label) with a migration path, plus cross-plan/phase/milestone task deps reconciled into one global DAG with cyclic rejection
- [ ] **Phase 24: Global Wave Derivation Engine** — Assemble ONE global Execution DAG after planning and derive global waves via layered Kahn; the bidirectional global wave index, re-derived O(V+E) with no cached schedule
- [ ] **Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption** — Dispatch off the global indegree map vs the completed-task set; wave-boundary failure contract preserved exactly at global scope; gates compose as holds; restart re-derives the whole schedule
- [ ] **Phase 26: Multi-Milestone Drive + Spec Conformance** — A Project drives multiple Milestones via the Milestone DAG with cross-milestone global waves and per-milestone gate policy composing across the DAG; the README worked example is an executable conformance test

### 🚧 v1.0.3 — Planning Resumption & Cost Resilience (In Progress)

**Milestone Goal:** Make interrupted or budget-halted TIDE runs cheaply resumable — a halt (budget, crash, bug) must never cost the already-authored plan. Motivated by dogfood run #2 budget-halting during planning (~$90, zero execution) with no resume path. Builds on Spring Tide's correct execution foundation.

- [ ] **Phase 27: Budget-Bypass Resume Correctness** — Fix the three identified bypass-path bugs and add regression coverage for the `2a5e0dc` ordering fix; ships independently of import work
- [ ] **Phase 28: Plan-Import Core** — Resolve the Approach A vs B design checkpoint FIRST, then implement envelope-import that bridges UID-churn, validates before adoption, runs cycle detection, converts v1alpha1 schema, and never imports Wave CRs
- [ ] **Phase 29: Operator Tooling + E2E** — `tide` CLI import/export commands and the kind integration test proving end-to-end resumption against the `salvage-20260618` fixture

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
Plans:
**Wave 1**

- [x] 25-01-PLAN.md — API vocabulary (FailureProfile enum + FailureHalt condition) + Nyquist Wave 0 RED test scaffolds (DISP-01/02/03, RESUME-01) + A1 coarse-ref grep

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 25-02-PLAN.md — Global dispatch: shared coarse-ref fan-out resolver (depgraph.go) + global computeIndegree/listProjectTasks + globalDependentsMapper watch (DISP-01, DISP-03, RESUME-01)

**Wave 3** *(blocked on Wave 2 completion)*

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

**Carried-in debt from Phase 25** (deferred, non-blocking — folded into P26 planning, covered by Plan 26-02):
  - **Wave-prune in-flight guard (OQ-3, inherited Phase-24 debt):** re-deriving waves can prune a wave that still has in-flight (`Running`) tasks. The naive guard (`skip if Wave.Status.Phase != "Succeeded"`) conflicts with the wave aggregator marking *zero-member* waves `Running`, which broke the pre-existing CR-01 `PruneShrink` regression test (Phase 25 commits `2a97a7a`→`e7c14f7` reverted it). The proper fix distinguishes "zero-member wave" from "wave with real Running tasks" and touches the wave aggregator — Phase 26 territory. Wave CRs are display artifacts (`computeGlobalIndegree` reads Task `.status` only), so this does not affect the dispatch contract.
  - **WR-02 (perf, from Phase 25 code review):** `globalDependentsMapper`'s Task watch has no event predicate, so it full-re-derives global dependents on every Task event. Add a predicate to fire only on status-phase / dependsOn changes.

**Plans**: 4 plans

**Wave 1**

- [x] 26-01-PLAN.md — D-01 N-milestone project_planner template (+ golden/ratchet, idempotency guard on Job existence) + D-03 §6d milestone fan-out removal + README planning-DAG-edge note + DEPS-02 reinterpretation (MS-01, MS-02)

**Wave 2** *(blocked on 26-01)*

- [x] 26-02-PLAN.md — Carried-in debt: D-08 OQ-3 wave-aggregator ZeroMembers phase + in-flight-safe prune guard (CR-01 PruneShrink stays green) + D-09 WR-02 globalDependentsMapper watch predicate + unit test (MS-02, SPEC-01)
- [x] 26-03-PLAN.md — D-06 SPEC-01 + MS-03 conformance envtest: 2-milestone α…θ fixture (cross-milestone γ→η), assert `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`; N milestone planning-hold composition; cross-linked to README (SPEC-01, MS-01, MS-02, MS-03)

**Wave 3** *(blocked on 26-03)*

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

**Wave 1**

- [x] 27-01-PLAN.md — Add durable status fields (CloneComplete, PlannerRolledUpUID, BypassBaselineCents) + make manifests/generate; confirm QQH-01 ordering test GREEN baseline (D-06, BYPASS-05 verify-green)

**Wave 2** *(blocked on 27-01)*

- [x] 27-02-PLAN.md — BYPASS-01 bypass targets PhaseRunning on initialized projects + init-Job BranchName guard; BYPASS-02 durable CloneComplete clone-dispatch guard + set-on-success + idempotency envtest

**Wave 3** *(blocked on 27-02; shares project_controller.go)*

- [x] 27-03-PLAN.md — BYPASS-03 PlannerRolledUpUID rollup-once guard in handleProjectJobCompletion; BYPASS-05 TTL-GC double-count companion envtest

**Wave 4** *(blocked on 27-02; shares project_controller.go)*

- [x] 27-04-PLAN.md — BYPASS-04 acknowledged-spend baseline + which-cap observability in handleBudgetGate (D-04, overrides RESEARCH Pattern 4); IsCapExceeded unchanged + call-site audit + unit/envtest coverage

### Phase 28: Plan-Import Core

**Goal**: A fresh Project run adopts pre-authored planner envelopes and skips the planner for every level whose valid envelope already exists — resolving the UID-churn problem via a stable identity scheme, validating every envelope before adoption, running cycle detection before materializing any child CRDs, converting v1alpha1 schema, and never importing Wave CRs.
**Depends on**: Phase 27 (correct bypass path; import layered on a working resume mechanism)
**Requirements**: IMPORT-01, IMPORT-02, IMPORT-03, IMPORT-04, IMPORT-05
**Notes**: **DESIGN CHECKPOINT REQUIRED BEFORE IMPLEMENTATION.** The first deliverable of Phase 28's plan-phase is resolving the Approach A (name-based / stable-key envelope directory, favored by STACK+FEATURES research) vs Approach B (UID-rewrite import step via a one-shot `ImportController` + `tide-import` Job, favored by ARCHITECTURE research) design decision. The salvage fixture (`salvage-20260618/pvc-envelopes.tgz`) contains only UID-keyed `envelopes/<oldUID>/` paths — no stable-key paths were ever written — which narrows the practical gap between the two approaches. No implementation plan may be written until this choice is resolved via `/gsd:discuss-phase` or `/gsd:spec-phase`.
**Success Criteria** (what must be TRUE):

  1. A fresh `kubectl apply` of an already-planned Project adopts pre-authored envelopes and proceeds straight to materialize-then-execute, with no planner Jobs dispatched for levels whose valid envelope exists — confirmed by zero planner Pod appearances in the run log.
  2. An envelope is only adopted after passing a completeness-and-schema check (`len(ChildCRDs) == ChildCount`, correct `APIVersionV1Alpha1`, no partial-write): any incomplete, wrong-schema, or mismatched envelope causes the planner to run normally, and a valid-looking stale envelope is never silently adopted.
  3. Envelopes authored under prior CRD UIDs are matched to the new run's CRs by stable identity (object name + parent chain), with no cross-object or cross-project aliasing — UID churn does not produce incorrect envelope adoption.
  4. Before any child CRDs are created from an imported envelope, `dag.ComputeWaves` runs explicitly on the full task set; a cyclic or unresolved imported graph produces an `ImportFailed / CyclicPlanDetected` condition, no partial CRs are created, and Wave CRs are always re-derived by `deriveGlobalWaves` (never imported).
  5. Import is operator-gated and verifies envelope origin against the per-namespace PVC before materializing into the CRD API channel — no unverified third-party envelope reaches `client.Create`.

**Plans**: 5 plans (3 waves)
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

**Plans**: 5 plans (4 waves)

**Wave 1**

- [ ] 29-01-PLAN.md — pkg/bundle/ foundation: BundleEntry/BundleManifest (seed superset + sha256), zip-slip-safe tgz codec, childCount-stamp (D-16a), offline dry-run validator (schema + completeness + sha256 + ComputeWaves cycle) (TOOL-01)

**Wave 2**

- [ ] 29-02-PLAN.md — `tide export-envelopes`: reused inspector pod (tar subtree) + seed-manifest generation from live CRs (FQName/oldUID/dependsOn/status/sha256) + legacy childCount repair + bundle assembly (TOOL-01)

**Wave 3** *(29-03 blocked on 29-02 via subcommands.go; 29-04 parallel)*

- [ ] 29-03-PLAN.md — `tide import-envelopes` + `--dry-run`: offline adopt/re-plan table + json + cycle hard-reject (D-07/08/09), live stage-only loader pod (SPDY exec) + seed ConfigMap + surfaced project.yaml (D-05/06) (TOOL-01)
- [ ] 29-04-PLAN.md — one-time salvage childCount patch (D-16b) + small drain-to-Succeeded fixture (D-11a) + test-int-kind-prep tide CLI build (D-10) (TOOL-02)

**Wave 4** *(blocked on 29-02, 29-03, 29-04)*

- [ ] 29-05-PLAN.md — kind E2E driving the real CLI: tier a small fixture → all-Milestones-Succeeded; tier b salvage → 0 `{milestone,phase}` planner Jobs + $0 re-paid (D-11b/D-17), long-test gated (D-12) (TOOL-02)


<details>
<summary>📋 vNext — OpenAI Backend + Dogfood Run #2 (Planned)</summary>

Scope TBD. Extends credproxy route allowlist for OpenAI paths, wires an OpenAI provider into the dispatch chain, and runs dogfood run #2. Gated on v1.0.3 making the run cheaply resumable if it halts mid-planning again.

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
| 22. Dashboard Embed Freshness Fix | v1.0.2 (Spring Tide) | 3/3 | Complete | 2026-06-16 |
| 23. Schema Migration + Cross-Scope Dependency Model | v1.0.2 (Spring Tide) | 5/5 | Complete | 2026-06-16 |
| 24. Global Wave Derivation Engine | v1.0.2 (Spring Tide) | 4/4 | Complete | 2026-06-16 |
| 25. Global Dispatch, Failure Semantics, Gates & Resumption | v1.0.2 (Spring Tide) | 3/3 | Complete | 2026-06-17 |
| 26. Multi-Milestone Drive + Spec Conformance | v1.0.2 (Spring Tide) | 4/4 | Complete | 2026-06-17 |
| 27. Budget-Bypass Resume Correctness | v1.0.3 | 4/4 | Complete   | 2026-06-18 |
| 28. Plan-Import Core | v1.0.3 | 5/5 | Complete   | 2026-06-18 |
| 29. Operator Tooling + E2E | v1.0.3 | 0/TBD | Not started | - |
