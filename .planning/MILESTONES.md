# Milestones

## v1.0.6 Adoption-Path Correctness & Dispatch Safety (Shipped: 2026-06-29)

**Phases completed:** 3 phases, 8 plans, 12 tasks

**Key accomplishments:**

- Contract-first wave: ConditionProjectPlannerSuppressed (D-01) + MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID scalar markers (D-03) added to api/v1alpha2 with regenerated CRD YAML and Helm chart templates
- Adoption lifecycle seam: adopted Project advances Initialized→Running with zero project-planner Jobs dispatched, durably suppresses re-dispatch via ConditionProjectPlannerSuppressed, and refuses planner dispatch via ConditionBudgetBlocked on an over-cap adopted Project
- Exactly-once child budget rollup gated on durable MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID markers (D-03/D-03a), proven by three Ginkgo envtest specs (ADOPT-02 accrual + ADOPT-04 no double-count across TTL-GC)
- D3 dispatch concurrency cap (Phase 32): live in-flight planner-count gate before pool acquire at all four dispatch sites with a single-node-safe default (plannerConcurrency=4, down from 16); excess dispatches park/requeue, planner and executor pools stay separately sized
- Carried-in hardening (Phase 32): RetryOnConflict + optimistic-lock on the child-level *RolledUpUID marker stamps (WR-02/WR-03); softened the chart plannerConcurrency sizing-policy comment (D-04, value held at 4 for single-node safety)
- D4 planner failure semantics (Phase 33): patchPhaseFailed/patchMilestoneFailed helpers + shared isPlannerFailure guard wired at both phase and milestone succession sites BEFORE the gate-policy hook (CR-01 fix), closing the false-leaf DAG-corruption bug — locked by PLANFAIL-01/02/03 envtests (run under the production approve gate) and a PLANFAIL-04 resume-recovery test

**Audit:** tech_debt — 13/13 requirements satisfied, 0 blockers, 2 verified tech-debt items deferred to v1.0.7 (project-level PlannerRolledUpUID hardening; configmap `default 16`→`4`). See milestones/v1.0.6-MILESTONE-AUDIT.md.
**Released:** tag `v1.0.6` — 8 component images + 2 OCI charts + 5 binaries @ 1.0.6, verified anon on ghcr; GitHub Release live.
**Known deferred items at close:** 22 stale open artifacts (20 historical Phase-02/03 quick-tasks + 1 todo + 1 uat_gap) — see STATE.md Deferred Items.

---

## v1.0.5 Resumable Import: Partial-Tree Resume (Shipped: 2026-06-27)

This close archives all planning work since v1.0.1 — **Phases 22–30, shipped across three release tags** (v1.0.3 Spring Tide + resumption, v1.0.4 image patch, v1.0.5 partial-tree resume). The headline is making the Topologically-Indexed paradigm real (one global Execution DAG) and making a halted run cheaply resumable.

**Scope:** Phases 22–30 · ~36 plans · published tags v1.0.3 / v1.0.4 / v1.0.5.

**Key accomplishments:**

- **Global Execution DAG (Spring Tide, Phases 22–26):** re-architected execution off per-plan waves onto ONE global DAG spanning all Milestones/Phases/Plans — v1alpha2 schema migration (Wave re-owned Plan→Project), global layered-Kahn wave derivation (no cached schedule), global dispatch + wave-boundary failure semantics + gates-as-holds + resumption, multi-milestone drive, and a README-pinned spec-conformance envtest deriving `[{α,β,γ,ζ},{δ,η},{ε,θ}]` with cross-milestone edges honored.
- **Budget-bypass resume correctness (Phase 27):** durable `CloneComplete` / `PlannerRolledUpUID` / `BypassBaselineCents` status fields — a budget halt resumes at Running with no re-clone, planning cost rolls up exactly once across halt→resume (even after reporter-Job TTL-GC), and raising the absolute cap alone makes a resume stick.
- **Plan-import core (Phase 28):** `cmd/tide-import` + `ImportController` adopt pre-authored envelopes by stable identity (UID-churn-safe), validate before adoption, run `dag.ComputeWaves` cycle-detection before any `client.Create`, never import Wave CRs, and gate import behind operator + PVC-origin verification.
- **Operator tooling + E2E (Phase 29):** `tide export-envelopes` / `import-envelopes` (+ `--dry-run`) with a zip-slip-safe bundle format; two-tier kind E2E proving zero-cost resumption against the real `salvage-20260618` fixture (0 planner Jobs at adopted levels, `CostSpentCents==0`).
- **Partial-tree resume (Phase 30, the v1.0.5 patch):** fixes the dogfood-run-#2 defect where incomplete-envelope nodes materialized as `Running`-with-no-envelope zombies — shared `IsEnvelopeComplete` at export time drives adopt-complete + re-plan-incomplete; Tier-c kind E2E drives a mixed partial import all the way to `Project=Complete`.

**Milestone audit (Phases 27–30):** `tech_debt` — 16/16 requirements satisfied, 0 blockers, Nyquist-compliant; non-blocking debt = integration finding F1 (latent legacy-bundle completeness-basis inconsistency) + Phase-27 IN-01/03/04 robustness follow-ups. See `milestones/v1.0.3-MILESTONE-AUDIT.md`.

**Released artifacts (v1.0.5):** 8 component images + 2 OCI charts + 5 binaries @ 1.0.5, GitHub Release v1.0.5, verified anonymously on ghcr.

---

## v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion ✅ SHIPPED 2026-06-13

**One-liner:** Turn the self-hosting MVP into an orchestrator trustworthy
enough to gate a second dogfood run on — every dogfood run-1 finding fixed
with a regression test that reproduces its symptom, the telemetry foundation
completed end-to-end, and the milestone-audit tech-debt subset closed.

**Stats:** 6 phases (12–17) · 38 plans · 46 tasks · 330 commits ·
+43.7k/−0.9k LOC across 274 files · 2026-06-11 → 2026-06-13 (~2 days).
28/28 requirements satisfied (milestone audit: passed, zero blockers).

**Key accomplishments:**

1. **Gate semantics run-killer fixed (Phase 12):** approve sits at descent
   (review the authored artifact before children spend), approval never jumps
   a level past incomplete children, dispatch holds while a parent is parked,
   reject parks instead of fail-marking, and `tide resume --retry-failed` is
   the one sanctioned recovery verb. (GATE-01..04, RESUME-01)

2. **Image dispatch chain + provider halt (Phase 13):** `resolveImage`
   precedence (`Levels.<level>.Image` → `Spec.Subagent.Image` → helm default)
   wired at all six dispatch sites — closing the v1.0 stub-image bug — and a
   provider billing-400 raises a project-wide `BillingHalt` instead of burning
   sessions one at a time. (DISPATCH-01/02, HALT-01)

3. **Budget enforcement made visible (Phase 14):** current model IDs resolve
   in the pricing table (no `unknown model` fallback), a `BudgetBlocked`
   condition surfaces on the Project CR and dashboard, in-flight overshoot is
   bounded via a reserve/settle ReservationStore, and pricing-drift is
   automated. (BUDGET-01/02/03)

4. **Seven run-1 paper cuts closed (Phase 15):** reporter CR project labels,
   clean-tree push no-op, phase status-flapping convergence, a real
   `artifact-get` inspector Pod, the dashboard Complete chip, a cross-plan
   running-waves view (label-selector–derived), and strict-mode file-touch
   overlap rejection at admission. (CUTS-01..07)

5. **Telemetry foundation completed (Phase 16):** `PROM_ENDPOINT` drives the
   PromQL proxy, TelemetryView mounts as a tab, the six locked metrics emit
   with `{project, phase, wave}` labels, panel queries use the real metric
   names, the `hack/helm` gate is wired into the Makefile, and the proxy
   client is bounded. (TELEM-01..06)

6. **Audit tech-debt subset closed (Phase 17):** PlanReconciler self-heals the
   `tideproject.k8s/project` label (with the Project→Milestone reporter-edge
   create-site stamp), reject short-circuits ahead of reporter spawn without
   deleting in-flight Jobs, the approve guard is narrowed to the approval
   target, and a transient Plan envelope-read error is non-fatal. (DEBT-01..04)

**Known deferred items at close:** 15 v1.0.0-era quick-task records
acknowledged as administrative (work landed; artifact status fields never
flipped) — see STATE.md Deferred Items. Remaining audit robustness/UX notes
(WR-01 + Phase 13/15/16 misc) formally accepted into the docs/audit hardening
backlog, all adjudicated non-blocking.

**Archives:** [v1.0.1-ROADMAP.md](milestones/v1.0.1-ROADMAP.md) ·
[v1.0.1-REQUIREMENTS.md](milestones/v1.0.1-REQUIREMENTS.md) ·
[v1.0.1-MILESTONE-AUDIT.md](milestones/v1.0.1-MILESTONE-AUDIT.md)

---

## v1.0.0 — Self-Hosting MVP ✅ SHIPPED 2026-06-11

**One-liner:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave)
runs as a real Kubernetes operator — installed via Helm into any cluster,
driving LLM subagent Jobs across derived waves, with gates, budget caps,
observability, a dashboard, and a CLI.

**Stats:** 14 phase directories (11 planned + 02.1/02.2/04.1/10/11 inserted) ·
137 plans · 965 commits · ~66k LOC Go · 2026-05-12 → 2026-06-11 (30 days).

**Key accomplishments:**

1. Six `tideproject.k8s/v1alpha1` CRDs with CEL validation + cycle-rejecting
   admission webhook; waves derived (never declared) via pure-Go layered Kahn.

2. Provider-firewalled subagent dispatch: `Subagent` interface → PodJob
   backend, signed-token credproxy (raw API key never enters the agent
   process), wall-clock/iteration/token caps, secret redaction, output-path
   validation.

3. Up-stack planner cascade with envelopes-as-artifacts: per-namespace PVC
   workspaces, in-namespace reporter Job materializing child CRs, ChildCount-
   gated succession, per-level boundary pushes to per-run git branches with
   gitleaks scanning and force-with-lease.

4. Gates (auto/approve/pause per level + between-wave slack tide), Prometheus
   + OTel/OpenInference observability, read-only SSE dashboard (two-DAG React
   Flow), `tide` CLI (9 verbs, kubectl-plugin compatible).

5. Distribution: Apache-2.0, helmify-generated chart pair on OCI, multi-arch
   images ×7, goreleaser binaries, rc-gated release pipeline whose dry-run
   simulates an external operator in Docker-in-Docker at $0 LLM cost.

6. Proof: live medium DoD on minikube — Project=Complete with real
   Claude-authored commits pushed to a run branch; $0 stub flow Complete in
   ~100s on a fresh kind cluster.

**Known deferred items at close:** 11 quick-task records and 1 UAT counting
artifact acknowledged as administrative (work landed; artifact status fields
never flipped) — see STATE.md Deferred Items. 4 of 137 plans from the final
ship sprint lack SUMMARY.md files.

**Archives:** [v1.0.0-ROADMAP.md](milestones/v1.0.0-ROADMAP.md) ·
[v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md)
