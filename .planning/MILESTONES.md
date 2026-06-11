# Milestones

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
