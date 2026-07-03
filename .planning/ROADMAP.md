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
- 🚧 **v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics** — Phases 34–38 (in progress; started 2026-07-03). Closes what the first external-repo run (2026-07-03) surfaced short of new subagent stages: the silent wave-parallel integration miss (run branch shipped incomplete yet stamped Complete), the 2.8× Claude-5 budget overcount, git ergonomics (baseRef, signed commits, promptFile), dashboard blind spots (artifact view at approve gates, project view, empty log drawer), the Prometheus setup step, and the v1.0.6 audit tech-debt carry. 26 requirements (INTEG/COST/BASE/SIGN/PROMPT/DASH/TELEM/DEBT), 100% mapped.
- 📋 **vNext — OpenAI Backend + Dogfood Run #2** — (planned; gated on v1.0.7 run-integrity fixes + adequate multi-node infrastructure)
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

### 🚧 v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics (In Progress)

**Milestone Goal:** Make a second external-repo run trustworthy and reviewable — a pushed run branch provably contains every Succeeded task's work, the budget tally matches the provider console, git ergonomics (baseRef, signed commits, promptFile) work, the dashboard is a sufficient approve-gate review surface, telemetry setup is guided, and the v1.0.6 audit tech-debt is retired.

- [ ] **Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA** - Every Succeeded task's worktree branch is provably merged into the run branch (final wave included), merges are serialized and idempotent, boundary push gates on git-verified completeness, and `status.git.lastPushedSHA` arms the force-with-lease fence
- [ ] **Phase 35: Git Base Ref** - `spec.git.baseRef` bases a run on any branch/tag/SHA, unresolvable refs fail fast with a typed condition, and the resolved SHA is stamped in `status.git.baseSHA` across both API versions
- [ ] **Phase 36: Signed Commits + Bot Identity** - TIDE Bot identity is uniformly configurable and, with an opt-in signing-key Secret ref, commits at all three sites are GPG-signed — with operator docs that earn the Verified badge
- [ ] **Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States** - Operators review planning artifacts at approve gates, read the outcome prompt and settings, and always see honest log-drawer states — no more PVC reader pods
- [ ] **Phase 38: Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry** - Claude 5 pricing rows land verified, `tide apply --prompt-file` works, the telemetry-setup nudge triple ships, and the v1.0.6 audit debt is closed

<details>
<summary>📋 vNext — OpenAI Backend + Dogfood Run #2 (Planned)</summary>

Scope TBD. Extends credproxy route allowlist for OpenAI paths, wires an OpenAI provider into the dispatch chain, and runs dogfood run #2. Gated on v1.0.7 run-integrity fixes + adequate multi-node infrastructure (single-node kind cannot hold the parallelism; needs ≥16 GiB or a multi-node cluster).

</details>

<details>
<summary>📋 v1.x — Polyglot Subagent Runtimes: LangGraph Strategy (Backlog)</summary>

Architecture locked; task breakdown deferred. The `claude` CLI subagent becomes one named strategy behind the existing `pkg/dispatch.Subagent` image contract; a second Python/LangGraph container image implements the same envelope contract for full agent-loop parity. Sequenced after the OpenAI-backend milestone.

See [milestones/v1.x-polyglot-subagent-MILESTONE.md](milestones/v1.x-polyglot-subagent-MILESTONE.md) for the full framing: parity inventory, contract-conformance table, provider-firewall gap analysis, alternatives considered, and open questions.

</details>

## Phase Details

### Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA

**Goal**: A pushed run branch provably contains every Succeeded task's work — the wave-parallel integration step cannot silently drop a merge, boundary push is gated on completeness verified from git, and a run can no longer stamp `Complete` while a declared deliverable is missing from the branch. The mechanical, no-LLM degenerate case of the verify-stage seed.
**Depends on**: Nothing (first phase of milestone; headline — merge code must stabilize before Phase 36's signing touches the same sites)
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

**Goal**: TIDE Bot commits are uniformly attributed and, with an opt-in signing-key Secret ref, GPG-signed at all three commit sites — harness, integrate, tide-push — with operator docs that earn the Verified badge on GitHub, GitLab, and Gitea. Absent ref preserves today's unsigned behavior exactly.
**Depends on**: Phase 34 (signing lands on stabilized merge code — same three commit sites), Phase 35 (chart/CRD bumps batch into one version bump)
**Requirements**: SIGN-01, SIGN-02, SIGN-03, SIGN-04
**Research flag**: `research: true` — spike gpg-shim vs plumbing-level merge-commit signing before planning (go-git cannot create signed three-way merges via `SignKey`; `--no-commit` + go-git commit silently flattens merge topology). Contains an ASK-FIRST scope decision: signing-key exposure at the harness commit site (a mounted key in an LLM-executing pod is a signing oracle — sign-controller-sites-only vs harness restructure vs documented risk).
**Success Criteria** (what must be TRUE):
  1. Configuring the bot identity (name/email) once changes the committer identity at all three commit sites — harness, integrate, tide-push — and the tide-push hardcoded identity is removed.
  2. With a signing-key Secret ref configured, commits from all three sites — including integrate merge commits — pass `git verify-commit`; with no ref configured, commits remain unsigned exactly as today.
  3. An invalid signing key (unarmored, passphrase-protected, or UID email mismatching the bot identity) surfaces a clear failure condition at first reconcile — not discovered at commit time mid-run.
  4. An operator following the docs recipe (machine account + key generation + public-key upload) sees the Verified badge on GitHub/GitLab/Gitea; UAT includes one manual push to a real GitHub repo that contains an integrate merge commit.
**Plans**: TBD

### Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States

**Goal**: The dashboard is a sufficient approve-gate review surface — operators read the planning artifacts a node produced, the project's outcome prompt and settings, and honest log-drawer states, without spinning up ad-hoc PVC reader pods. Three features sharing one read-only manager-API surface; the reporter's ConfigMap display cache is the transport (PVC/git remain source of truth).
**Depends on**: Nothing (independent of Phases 34–36; sequenced last among the big items so the UI consumes a settled reporter ConfigMap contract, which this phase also delivers — DASH-02 lands before/with DASH-01)
**Requirements**: DASH-01, DASH-02, DASH-03, DASH-04
**Success Criteria** (what must be TRUE):
  1. Clicking any Planning DAG node shows the artifacts it produced, markdown-rendered (children JSON pretty-printed); on a gate-parked node the artifact renders beside the approve action — an approve decision needs no PVC reader pod.
  2. Planning artifacts persist as size-capped, owner-ref'd ConfigMaps written at reporter materialization time; an oversize artifact renders with a visible truncation marker, and deleting the owning CR garbage-collects its artifact ConfigMaps (PVC/git stay source of truth).
  3. The operator can read the outcome prompt and project settings in a dashboard project view.
  4. The log drawer always renders an explicit state — loading, streaming, or pod-gone — and is never silently empty.
**Plans**: TBD
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
**Plans**: TBD
**UI hint**: yes

## Progress

**Execution Order:** 34 → 35 → 36 → 37 → 38 (35, 37, 38 are independent and may interleave; 36 requires 34 + 35)

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1–11 (see archive) | v1.0.0 | 137/137 | Complete | 2026-06-11 |
| 12–17 (see archive) | v1.0.1 | 38/38 | Complete | 2026-06-13 |
| 18–21 (superseded) | v1.0.2 (Ebb) | 14/14 | Complete (superseded) | 2026-06-16 |
| 22–26 (see archive) | v1.0.2 (Spring Tide) | 19/19 | Complete | 2026-06-17 |
| 27–29 (see archive) | v1.0.3 | 14/14 | Complete | 2026-06-22 |
| 30 (see archive) | v1.0.5 | 3/3 | Complete | 2026-06-27 |
| 31–33 (see archive) | v1.0.6 | 8/8 | Complete | 2026-06-29 |
| 34. Run Integrity — Integration-Miss Gate + lastPushedSHA | v1.0.7 | 0/TBD | Not started | - |
| 35. Git Base Ref | v1.0.7 | 0/TBD | Not started | - |
| 36. Signed Commits + Bot Identity | v1.0.7 | 0/TBD | Not started | - |
| 37. Dashboard Surfaces — Artifact View, Project View, Log-Drawer States | v1.0.7 | 0/TBD | Not started | - |
| 38. Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry | v1.0.7 | 0/TBD | Not started | - |
