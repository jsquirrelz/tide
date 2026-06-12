# TIDE — Topologically-Indexed Dependency Execution

## What This Is

A Kubernetes-native orchestrator that runs hierarchical agentic coding work as a topologically-sorted DAG of subagent dispatches. A human applies a `Project` CRD (outcome prompt + target repo + creds); TIDE authors `MILESTONE.md`, phase briefs, `PLAN.md` files, and task diffs by dispatching specialist subagents at each level, parallelizing across waves derived from the declared task DAG. Built to be open-sourced and portable across clusters from day one.

## Core Value

**The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.** If everything else fails, TIDE-on-TIDE must work — that's what proves the paradigm and the implementation simultaneously, and it's the bar for "v1 ships."

## Current State (v1.0.0 — SHIPPED 2026-06-11)

v1.0.0 is published: goreleaser binaries (5 platforms), 7 component images and
both Helm charts on GHCR (`oci://ghcr.io/jsquirrelz/tide-charts`), rc-gated
release pipeline with a $0 Docker-in-Docker external-operator dry-run. Live
medium DoD proven on minikube (Project=Complete, real authored commits pushed
to a per-run branch). All 82 v1 requirements delivered — archive at
[milestones/v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md).

## Current Milestone: v1.0.1 Orchestrator Trustworthiness + Telemetry Completion

**Goal:** Fix the dogfood run-1 findings so the orchestrator is trustworthy enough to gate run 2 on, and make the merged telemetry foundation functional — every fix carries a regression test reproducing the run-1 symptom.

**Target features (ordered by what blocks dogfood run 2):**
- Approve-consume gate semantics — ConsumeApprove must not advance a level to Succeeded while children are incomplete (run-1 finding 7, the run-killer); gates.md step 5 docs change with the fix
- Reject/resume recovery — `tide reject` fail-marks children; `tide resume` must recover them (finding 9a; the kubectl status-reset recipe is the behavioral spec)
- Subagent image resolution — implement Image in the ResolveProvider chain at the four dispatch sites; kill the dead `spec.subagent.image` config and reconsider the chart's stub default
- Billing-400 project-wide halt — provider credit exhaustion stops the fan-out instead of burning sessions one at a time
- Pricing + budget UX — current model IDs in the pricing table; surface BudgetBlocked on the Project; bound in-flight overshoot
- Paper cuts — reporter CR labels, boundary-push no-op on clean tree, phase status flapping, artifact-get stub, dashboard status-chip mapping + cross-plan wave view, file-touch overlap
- Telemetry completion — PROM_ENDPOINT config wiring, TelemetryView AppShell mount + Vitest coverage, the six locked metrics in internal/metrics, PromQL query-name alignment, Makefile gate-script wiring, proxy client timeout

**Key context:** kind cluster `tide` with run-1 CRs is the repro environment for the gate-semantics fixes (do not delete without asking). Caps floors (finding 11) already landed on main (47a9aa9). Dogfood run 2 (02-codex-runtime) is gated on this milestone, not part of it. Deferred to a later milestone: full TIDE-on-TIDE headline, docs/audit/ 27-item hardening backlog.

Everything below this line reflects v1 planning state, preserved for reference.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- [x] **CRD set defining Project, Milestone, Phase, Plan, Task (and any orchestrator-internal Run/Wave bookkeeping)** — *Validated in Phase 1: Foundation*. Six CRDs in `v1alpha1` under group `tideproject.k8s` with Spec/Status separation, owner-ref cascade (`BlockOwnerDeletion: true`), CEL inline validation, and validating-admission + conversion-webhook scaffolding (firing as no-ops; Phase 2 wires cycle detection).
- [x] **Layered Kahn algorithm in Go (pure-Go stdlib `pkg/dag`)** — *Validated in Phase 1: Foundation*. `ComputeWaves(nodes, edges) ([][]string, error)` + `CycleError` shape; α…θ regression fixture pins the spec's worked example; DAG-05 import firewall via `make verify-dag-imports` + `tools/analyzers/dagimports/` analyzer with 97% test coverage. Cycle detection is wired at the algorithm layer; Phase 2 wires it into the Plan admission webhook.
- [x] **CRD-`status`-only persistence (no external DB, no SQLite for v1)** — *Validated in Phase 1: Foundation*. `make verify-no-sqlite-dep` greps go.mod for SQLite/Postgres/gorm; `make verify-no-aggregates` greps `api/v1alpha1/*_types.go` for `Schedule`/`Waves[]`/`IndegreeMap`. Both CI-gated; programmatic guard tests in `api/v1alpha1/aggregates_guard_test.go`.
- [x] **Namespace-per-project tenancy with namespace-scoped RBAC** — *Validated in Phase 1: Foundation* (runtime portion). Every reconciler has `WatchNamespace string` field + `predicate.NewPredicateFuncs` + `WithEventFilter` in `SetupWithManager`; Manager binary has `--watch-namespace` flag. Per-namespace `RoleBinding` template lands in Phase 5's Helm chart per the AUTH-02 traceability split.

### Active

<!-- Current scope. Building toward these. v1 = self-hosting MVP. -->

- [ ] Go controller (controller-runtime) reconciling each CRD level, watching status transitions, dispatching subagents — *Phase 1 scaffold landed (six reconcilers at Standard depth with owner refs + finalizers + status conditions; Dispatcher field nil-guarded); Phase 2 wires dispatch*
- [ ] Pluggable Subagent interface — `Pod-per-task Job + typed result envelope on artifact PVC + exit code` is the contract; at least one concrete implementation (Claude Code or direct Anthropic SDK in-container)
- [ ] Artifact persistence: shared PVC during a run; orchestrator pushes to the target repo's git remote at each level boundary
- [ ] Credentials via K8s `Secret` references on the `Project` CRD (LLM API keys + git push creds)
- [ ] Strict-by-default failure profile at wave boundaries (failed task → dependents never dispatch; non-dependents continue)
- [ ] Thin `tide` CLI wrapping the common ops (apply a project, tail a run, inspect a wave, resume)
- [ ] Read-only web dashboard rendering the live Planning + Execution DAGs, wave progress, per-task status, click-through to artifacts; streams `kubectl logs` from running task pods for live feedback
- [ ] Observability: structured JSON logs from orchestrator + subagent pods, Prometheus metrics on the orchestrator (waves dispatched, tasks completed, dispatch latency, failure rate), OpenTelemetry tracing with OpenInference conventions for the Milestone→Phase→Plan→Task subagent chain
- [ ] End-to-end self-hosting demo: TIDE in a cluster (kind for dev) drives its own next milestone on this repo, producing artifacts and a merged set of commits
- [ ] Helm chart / installable bundle (CRDs + controller + RBAC) that another cluster can deploy unmodified
- [ ] Apache 2.0 LICENSE, README/docs sufficient for an external operator to install + run a project end-to-end

### Out of Scope

<!-- Explicit boundaries. Includes reasoning to prevent re-adding. -->

- **Multi-tenant cluster posture** — Defer to post-v1. Namespace-per-project covers the OSS user; full tenant isolation (per-tenant quotas, cross-tenant RBAC) is real work that doesn't move the paradigm.
- **gRPC streaming subagent protocol** — Pod-per-task Job + result envelope is enough for v1. A streaming sidecar can be added later behind the same Subagent interface without redesign.
- **External database (Postgres) / per-run SQLite** — CRD-status-only is technically sufficient at the scale of one human watching one run. Re-evaluate only if dashboard query shapes outgrow label-selector queries.
- **Dashboard mutation actions (retry wave, edit plan, pause/resume)** — v1 dashboard is read-only. Mutations route through `tide` CLI / `kubectl` so there's a single auth surface.
- **Vendored GSD workflow Markdown** — TIDE reads `get-shit-done` as design reference but the planner/executor logic and prompts are reimplemented in Go. Markdown workflows would lock TIDE to one bootstrap host.
- **Critical-path / HEFT / heterogeneous-resource schedulers** — The spec explicitly rejects these at the paradigm layer. Wave-internal sub-scheduling stays a v2+ extension behind layered Kahn.
- **Wave or cycle "recovery" features** — Cycles are bugs detected at plan-validation time. Refuse and surface, don't recover.
- **Non-Kubernetes runtime** — Docker Compose / bare metal / Nomad are deliberately not v1. The K8s pun is load-bearing; pod isolation, RBAC, watches, and Jobs are what make the dispatch model tractable.
- **Vendor lock-in to one LLM provider** — The Subagent interface is provider-agnostic by construction. Anthropic-first concrete impl, but no provider-specific code in the orchestrator.
- **External Secrets Operator first-class integration** — Plain K8s Secrets only for v1. ESO docs/examples can land later without changing the CRD contract.

## Context

- **The spec is load-bearing.** `README.md` (270 lines) is the design doc and the public-facing README. Schemas, APIs, controller logic, and persistence trace back to it. When implementation pressure pushes back on the hierarchy or the two-DAG split, the spec updates first — then the code.
- **Bootstrap path.** TIDE itself is built using `get-shit-done` (the local Markdown-based workflow system at `~/.claude/get-shit-done/`) on the developer's host. Once TIDE runs a Milestone end-to-end against this repo (the self-hosting MVP), it earns the right to drive its own next milestone. GSD's *paradigm* informs TIDE's design heavily; GSD's *code/Markdown* is not vendored into TIDE — the planner, executor, and dispatch logic are reimplemented in Go so TIDE is portable.
- **Two parallelism budgets.** Planner pool and executor pool are separately sized — the spec argues planning fans out wide (most phases plan in parallel from one architecture spec) and execution fans out narrow (real file-level deps serialize work). The orchestrator config exposes both budgets independently.
- **Artifacts as source of truth.** Every level boundary produces a reviewable file (`MILESTONE.md`, phase brief, `PLAN.md`, diff). Resumption reads from artifacts; the CRD `.status` index is a cache, rederivable from artifacts + completed-task set.
- **Vocabulary discipline.** The water/tide metaphor is intentional and consistent — Rising tide (planning wave), Slack tide (review checkpoint), Tidal lock (deps resolved phase), Tidepool (parallel sub-DAG), TIDE pod (deployment unit). Used in code names, CRD names, log lines, docs. Extend the metaphor over coining unrelated terms; fall back to plain prose if a name doesn't fit.
- **Human gates per level.** The spec requires gate policy be configurable per level (approve-every-milestone-but-auto-pass-plans should be as easy as fully-autonomous or fully-supervised). Gates are configured per `Project`, not baked into the controller.

## Constraints

- **Tech stack**: Go + sigs.k8s.io/controller-runtime + kubebuilder — K8s ecosystem default, idiomatic for CRDs/controllers, best contributor pool.
- **Tech stack**: Pluggable subagent runtime via a documented container image contract — never hard-coded to Anthropic SDK; v1 ships with a Claude-backed concrete impl behind the interface.
- **Distribution**: Apache 2.0, Helm chart from v1, designed for installation in arbitrary clusters with no hidden host dependencies.
- **Portability**: No hard-coded git host (GitHub, GitLab, Gitea must all work behind a generic git remote), no hard-coded LLM provider, no hard-coded auth model — abstract behind interfaces.
- **Persistence**: CRD `.status` only for v1 — no external DB, no SQLite. Per-object size stays well under etcd's 1.5 MiB hard limit by keeping per-Task CRDs small and label-indexed.
- **Failure semantics**: Wave boundary contract from spec §"Failure handling at wave boundaries" must be preserved exactly — failed task → siblings continue, dependents in later waves never dispatch, non-dependents dispatch in strict profile. Resumption state = indegree map + completed-task set, nothing more.
- **Resumability**: Long-running agentic work outlives single context windows. Every level boundary is a saved artifact; a fresh orchestrator restart re-derives waves from the task DAG + completed-task set in O(V+E).
- **Observability**: OpenTelemetry tracing must use OpenInference conventions for LLM/agent spans so traces are queryable in standard AI observability tools (Phoenix, LangSmith, Arize) without bespoke instrumentation.

## Key Decisions

<!-- Decisions that constrain future work. Add throughout project lifecycle. -->

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go + controller-runtime + kubebuilder | K8s ecosystem default; idiomatic CRDs/controllers; best contributor pool for an OSS K8s operator | ✓ Validated in Phase 1 — Go 1.26 + controller-runtime v0.24.1 + kubebuilder v4.14.0 pinned and shipping |
| v1 = self-hosting MVP (TIDE drives its own next milestone) | Dogfood test proves paradigm and implementation simultaneously; bounded scope that's still real work | — Pending |
| Pluggable Subagent interface from day one | Spec is provider-agnostic; OSS posture demands no vendor lock-in; defining the contract early prevents Anthropic-specific code leaking into the orchestrator | — Pending |
| Pod-per-task K8s Job + result envelope on PVC + log streaming | Simplest contract that's K8s-native and matches how Claude Code already runs; streaming gRPC sidecar deferred to v2 behind the same interface | — Pending |
| Reimplement GSD paradigm in Go (not vendor Markdown) | TIDE is the production K8s generalization of the paradigm, not a port; vendoring workflows would couple TIDE to a single bootstrap host | — Pending |
| Artifacts on PVC during run; git push at level boundaries | Lower-latency than per-artifact commits; matches reviewable level-boundary cadence; one cred surface (git remote) | — Pending |
| CRD-`status`-only persistence for v1 | Technically sufficient at v1 scale; aligns with spec's "DB is cache, not truth"; defers operational burden until proven needed | ✓ Validated in Phase 1 — `make verify-no-sqlite-dep` + `make verify-no-aggregates` CI-gated |
| Namespace-per-project tenancy | Right tradeoff for OSS posture without v1 scope creep into full multi-tenancy | ✓ Validated in Phase 1 (runtime) — runtime predicate + `--watch-namespace` flag; per-namespace RoleBinding template deferred to Phase 5 Helm chart |
| K8s Secrets referenced by Project CRD for creds | Standard K8s, no extra deps; ESO/Vault integrations land later without breaking the contract | — Pending |
| Strict-by-default failure profile | Spec default; maximizes throughput on independent task failures; conservative profile becomes a per-Project setting later if needed | — Pending |
| Read-only web dashboard for v1 | All mutations go through `kubectl` / `tide` CLI for a single auth surface; viewer-only keeps scope honest | — Pending |
| Apache 2.0 license | K8s ecosystem default; patent grant; friendliest to enterprise contributors and downstream commercial use | — Pending |
| OpenTelemetry tracing with OpenInference conventions | Standard OTel infra compat + AI-native span attributes queryable in Phoenix/LangSmith/Arize without bespoke instrumentation | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd:transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-06-12 — Phase 16 (telemetry-completion) complete; TELEM-01..06 validated. v1.0.1 milestone phases 12–16 all complete.*

## Current State

Phase 16 (telemetry-completion) complete — 8/8 plans (5 original + 3 gap-closure), VERIFICATION passed 6/6 after one gap-closure cycle. The merged run-1 telemetry foundation is functional end-to-end: `PROM_ENDPOINT` drives the PromQL proxy (env read in `cmd/dashboard/main.go`; dead `prometheusEndpoint` YAML surface excised); the proxy is hardened (30s bounded client, `r.Context()` propagation, base-path preservation, three-shape degradation intact); the six locked metrics (`tide_tokens_{input,output,cache_read,cache_creation}_total`, `tide_cost_cents_total`, `tide_task_duration_seconds`) emit at all three `RollUpUsage` terminal seams with `{project, phase, plan, wave}` labels exactly-once, plus the previously-never-emitted `tide_waves_dispatched_total`/`tide_tasks_completed_total`/`tide_tasks_failed_total` now emit at their commit points; TelemetryView ships finished (recharts AreaCharts, locked D-06 queries, project/all-projects scope with per-project series, 24h/7d/30d range + 60s visibility-aware polling, budget card grid) mounted behind a header DAGs|Telemetry switcher; `make helm-telemetry-assert` + `helm-assert` wired into ci.yaml's helm-lint job. Validated in Phase 16: TELEM-01..TELEM-06. Known UX-polish leftovers (review WR-01/WR-02: project-switch refetch staleness ≤1 poll cycle, no stale-response guard on rapid toggles) judged non-blocking.

**Milestone status:** Phase 16 was the last v1.0.1 phase — all 5 phases (12–16) complete. Next: milestone audit/completion (`/gsd:audit-milestone` → `/gsd:complete-milestone`), then dogfood run 2 (02-codex-runtime) which this milestone gates.
