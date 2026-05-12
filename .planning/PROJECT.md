# TIDE — Topologically-Indexed Dependency Execution

## What This Is

A Kubernetes-native orchestrator that runs hierarchical agentic coding work as a topologically-sorted DAG of subagent dispatches. A human applies a `Project` CRD (outcome prompt + target repo + creds); TIDE authors `MILESTONE.md`, phase briefs, `PLAN.md` files, and task diffs by dispatching specialist subagents at each level, parallelizing across waves derived from the declared task DAG. Built to be open-sourced and portable across clusters from day one.

## Core Value

**The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.** If everything else fails, TIDE-on-TIDE must work — that's what proves the paradigm and the implementation simultaneously, and it's the bar for "v1 ships."

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

(None yet — ship to validate)

### Active

<!-- Current scope. Building toward these. v1 = self-hosting MVP. -->

- [ ] CRD set defining Project, Milestone, Phase, Plan, Task (and any orchestrator-internal Run/Wave bookkeeping)
- [ ] Go controller (controller-runtime) reconciling each CRD level, watching status transitions, dispatching subagents
- [ ] Layered Kahn algorithm in Go, runs on both the Planning DAG and the Execution DAG, cycle detection at validation time
- [ ] Pluggable Subagent interface — `Pod-per-task Job + typed result envelope on artifact PVC + exit code` is the contract; at least one concrete implementation (Claude Code or direct Anthropic SDK in-container)
- [ ] Artifact persistence: shared PVC during a run; orchestrator pushes to the target repo's git remote at each level boundary
- [ ] CRD-`status`-only persistence — per-task CRD with small status block, in-process indegree cache, no external DB and no SQLite for v1
- [ ] Namespace-per-project tenancy — one TIDE install per cluster, projects isolated by namespace, RBAC scoped accordingly
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
| Go + controller-runtime + kubebuilder | K8s ecosystem default; idiomatic CRDs/controllers; best contributor pool for an OSS K8s operator | — Pending |
| v1 = self-hosting MVP (TIDE drives its own next milestone) | Dogfood test proves paradigm and implementation simultaneously; bounded scope that's still real work | — Pending |
| Pluggable Subagent interface from day one | Spec is provider-agnostic; OSS posture demands no vendor lock-in; defining the contract early prevents Anthropic-specific code leaking into the orchestrator | — Pending |
| Pod-per-task K8s Job + result envelope on PVC + log streaming | Simplest contract that's K8s-native and matches how Claude Code already runs; streaming gRPC sidecar deferred to v2 behind the same interface | — Pending |
| Reimplement GSD paradigm in Go (not vendor Markdown) | TIDE is the production K8s generalization of the paradigm, not a port; vendoring workflows would couple TIDE to a single bootstrap host | — Pending |
| Artifacts on PVC during run; git push at level boundaries | Lower-latency than per-artifact commits; matches reviewable level-boundary cadence; one cred surface (git remote) | — Pending |
| CRD-`status`-only persistence for v1 | Technically sufficient at v1 scale; aligns with spec's "DB is cache, not truth"; defers operational burden until proven needed | — Pending |
| Namespace-per-project tenancy | Right tradeoff for OSS posture without v1 scope creep into full multi-tenancy | — Pending |
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
*Last updated: 2026-05-12 after initialization*
