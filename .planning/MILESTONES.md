# Milestones

## v1.0.5 Resumable Import: Partial-Tree Resume (Shipped: 2026-06-27)

**Phases completed:** 9 phases, 36 plans, 48 tasks

**Key accomplishments:**

- Multi-stage Dockerfile.dashboard with digest-pinned node:22-alpine spa-builder that regenerates dist/ from source on every image build, plus a make verify-dashboard-freshness gate that fails on stale dist/ or a missing panel-cache-efficiency telemetry marker
- Wire `make verify-dashboard-freshness` into ci.yaml (PR-time gate in the `test` job) and release.yaml (release-time gate as a step in `helmify-verify`), using `actions/setup-node@v4` (node 22, npm cache on `dashboard/web/package-lock.json`) before each invocation
- One-liner:
- 1. [Rule 2 - Missing Critical Functionality] Ported project_webhook to v1alpha2
- Files:
- Task 1 — bulk path repoint (commit 8ec1dbe):
- Commit:
- Commit:
- `internal/controller/plan_controller.go`
- Coarse-ref `DependsOn` is PRESENT in authored fixtures.
- Envtest:
- Conservative failure halt via `ConditionFailureHalt` — checkFailureHalt at four execution dispatch gates, cleared by `tide resume --retry-failed` — turns DISP-02 strict+conservative and resume unit tests GREEN (51/51 envtest, 7+2 unit tests)
- One-liner:
- Wave aggregator adds ZeroMembers phase (OQ-3 root fix) + in-flight-safe prune guard; globalDependentsMapper fires only on phase/dependsOn transitions (WR-02), proven by 7-case unit test
- SPEC-01 envtest derives [{α,β,γ,ζ},{δ,η},{ε,θ}] from 2-Milestone real CRDs with cross-milestone γ→η honored; MS-03 proves approve/auto/full-supervised gate profiles via status-inject fixture approach
- GlobalExecutionDAGView.tsx
- PlannerRolledUpUID-gated rollup in handleProjectJobCompletion prevents double-counting planning cost on halt→resume when the reporter Job has TTL-GC'd; BYPASS-05 TTL-GC companion envtest proves the nil-Job path rolls up exactly once
- Budget bypass acknowledges prior spend as a durable baseline (BypassBaselineCents) so raising the absolute cap alone makes a resume stick, and re-halt now names which cap fired (AbsoluteCapReached vs RollingWindowCapReached) with current spend + both cap values
- Task 1 — `charts/tide/values.yaml`:
- One-liner:
- `internal/controller/import_controller.go`
- One-liner:
- 1. [Rule 2 - Missing cross-pkg surface] Exported StampChildCount and ComputeEnvelopeSHA256 from pkg/bundle
- 1. [Rule 1 - Bug] `APIVersionV1Alpha2` does not exist in pkg/dispatch
- test/integration/kind/testdata/import-small-fixture/
- Two-tier kind E2E proving zero-cost resumption: small fixture drains to all-Milestones-Succeeded via stub subagents + live tide export-envelopes → import-envelopes round-trip adopts milestone/phase levels; salvage-20260618 import asserts 0 planner Jobs at milestone/phase levels and CostSpentCents==0 before plan dispatch (D-11/D-14/D-17).
- One-liner:
- Tier c E2E proves partial-import partial-tree resumes all the way to Project=Complete; deleteNamespaceAndWait eliminates inter-tier namespace contention so all three import-resume tiers pass together

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
