# Feature Research — TIDE v1

**Domain:** Kubernetes-native orchestrator for hierarchical agentic coding work (DAG-driven multi-agent code generation)
**Researched:** 2026-05-12
**Confidence:** HIGH for K8s/workflow features and AI orchestration norms (Context7-/official-docs-verified); MEDIUM for some 2026 trend claims (vendor-blog-sourced); HIGH for what is in/out of v1 scope (cross-referenced against `.planning/PROJECT.md` and `README.md`).

---

## Method note

The reference frames for this survey are three populations of comparable systems:

1. **K8s-native workflow engines** — Argo Workflows, Tekton Pipelines, Kueue. These set baseline expectations for *how* a K8s orchestrator behaves (CRDs, suspend/resume, retries, dashboards).
2. **AI agent runtimes on K8s** — kagent (CNCF Sandbox), Dapr Agents v1.0, LangGraph deployments, sigs.k8s.io/agent-sandbox. These set baseline expectations for *what* a multi-agent system on K8s looks like (declarative agent CRDs, MCP/A2A protocols, OTel/OpenInference observability).
3. **Agentic coding orchestrators** — Composio Agent Orchestrator, Claude Code Auto Mode, GitHub Copilot agent assignment, Aider, OpenCode, Cursor agent mode. These set baseline expectations for *coding-specific* features (git worktrees, PR-based review, CI fix loops, per-task agent isolation).

The local reference TIDE is generalizing is `~/.claude/get-shit-done/` (56 Markdown workflows, single-host, GSD subagent types `gsd-executor / gsd-planner / gsd-verifier / gsd-debugger / gsd-codebase-mapper / gsd-phase-researcher / gsd-plan-checker`). TIDE reimplements GSD's *paradigm* (five-level hierarchy + two-DAG + Kahn-layered waves) in a portable Go controller — it does not vendor GSD's Markdown.

Categorization rules used below:
- **Table stakes** — present in two or more comparable systems *and* breaks the paradigm or the user contract if absent.
- **Differentiator** — present in zero or one comparable system *or* uniquely enabled by the two-DAG + 5-level + Kahn-layered shape.
- **Anti-feature** — explicitly out-of-scope per `PROJECT.md` or `README.md`, with the rejection reason intact.
- **Deferred** — table-stakes-shaped but the spec/PROJECT.md punts to post-v1; cited with the rationale.

---

## Feature Landscape

### Table Stakes (Users Expect These — v1 Must Have)

| # | Feature | Why Expected | Complexity | Notes |
|---|---------|--------------|------------|-------|
| TS-1 | **Apply-a-Project lifecycle**: `kubectl apply -f project.yaml` (or `tide apply`) submits a run; controller reconciles to terminal state | Standard K8s operator UX. Argo Workflows, Tekton, kagent, Dapr all work this way. Without it, TIDE isn't K8s-native — it's a script in a Pod. | MEDIUM | `Project` CRD already in PROJECT.md Active set. Reconciler walks Project → Milestone → Phase → Plan → Task CRDs. |
| TS-2 | **Watch / status**: live status via `kubectl get tideruns -w` and `tide watch` | K8s convention. Argo `argo watch`, Tekton `tkn pr logs -f`, kagent watches A2A streams. Without it, users can't tell if work is progressing. | LOW | CRD `.status` blocks updated per reconcile; OK to start with `kubectl`-level UX before dashboard exists. |
| TS-3 | **Cancel / terminate**: `tide cancel` or `kubectl delete tiderun` stops in-flight subagents | Long-running agentic work is expensive (LLM tokens, compute). Argo Workflows supports `argo terminate`. Without cancel, users can't stop a runaway run mid-wave. | MEDIUM | Job deletion + propagation policy. Finalizer to ensure subagent Pods are reaped. Spec §"Failure handling at wave boundaries" is the post-cancel resumption contract. |
| TS-4 | **Resume after restart**: orchestrator restart re-derives waves from artifacts + completed-task set in O(V+E) | Spec §"Failure handling at wave boundaries" requires this. CRD-status-only persistence (PROJECT.md) makes it the only correctness story. Temporal, Argo, LangGraph all support resume; without it, an orchestrator restart wastes everything in flight. | HIGH | This is the load-bearing correctness property. Indegree map + completed-task set is the persisted state. Verify on every controller restart in tests. |
| TS-5 | **Retry on transient failure**: per-task retry policy (count + backoff) | Argo `retryStrategy`, Tekton retries, Composio AO `retries: 2`. LLM tool calls + network calls fail transiently; without retries, a 95%-reliable subagent run aborts ~5% of waves. | LOW | Configurable on the Task CRD; default to `retries: 2` with exponential backoff (matches Composio AO default). Distinguish transient (timeout, rate-limit, exit-code-1-on-flaky-test) from permanent (cycle, plan-validation-error) failures. |
| TS-6 | **Cycle detection at plan-validation time**, reject + surface | Spec §"Why this specific algorithm" — cycles are bugs, not runtime conditions. Falls out of Kahn's termination check for free. | LOW | Already part of the algorithm; just needs a clean error surface on the CRD `.status.conditions`. |
| TS-7 | **Wave-derived execution schedule**: waves are output of Kahn-layered, not input | Spec §"Wave computation". CLAUDE.md: "Waves are derived, not declared." Re-derive on every plan edit (O(V+E), no caching). | LOW | Algorithm itself is textbook; the discipline is *not* accepting a wave list at the API boundary. CRD validation rejects any attempt to declare a `wave` field on a Task. |
| TS-8 | **Pod-per-task isolation**: each Task dispatches a K8s Job whose Pod runs one subagent invocation | PROJECT.md decision. Matches how Claude Code is typically run; matches sigs.k8s.io/agent-sandbox model. Filesystem isolation per task means failure of one task can't corrupt sibling tasks in the same wave. | MEDIUM | The Subagent contract is `Job + typed result envelope on PVC + exit code`. Shared PVC for artifacts within a run; ephemeral container filesystem otherwise. |
| TS-9 | **Capacity caps**: separate `plannerConcurrency` and `executorConcurrency` budgets on Project (or controller-wide defaults) | Spec §"Two parallelism budgets" + CLAUDE.md. Kueue exists in the ecosystem precisely because raw `Job.parallelism` doesn't compose with multi-tenant quota concerns. v1 doesn't need Kueue integration but does need per-pool caps so a runaway wave can't OOM the cluster. | LOW | Two integer fields on the Project CRD; the executor honors them by dispatching `min(|W_k|, capacity)` per spec §3. No Kueue dependency v1. |
| TS-10 | **Per-level human gates with policy in the Project CRD**: approve every milestone but auto-pass plans (or any other combination) | Spec §"Why this is advantageous" point 7. CLAUDE.md: "Human gates are configurable per level." Argo Workflows supports `suspend` templates; Tekton has manual-approval-gate; Composio AO has `escalateAfter` timers. Without per-level gates, TIDE's paradigm value proposition collapses to "an Argo Workflow." | MEDIUM | `gates: { milestone: required, phase: auto, plan: auto, task: auto }` on the Project. Implementation: controller pauses reconcile + writes a `Pending Approval` condition; `tide approve` (or `kubectl annotate`) clears it. |
| TS-11 | **Artifact persistence at level boundaries**: `MILESTONE.md`, phase brief, `PLAN.md`, task diff written to shared PVC during run; pushed to target git remote at each level boundary | Spec §"Artifacts as source of truth." Without artifact persistence, the resumability story (TS-4) is impossible — there's nothing to resume *from*. Argo artifacts go to S3/GCS; TIDE's choice is PVC + git, which fits the reviewable-artifact-cadence model better. | MEDIUM | PVC for in-run; git push at level boundary. Single cred surface (git remote). |
| TS-12 | **Git remote integration**: clone target repo on Project apply, push at level boundaries, run on a per-Project branch | Composio AO uses git worktrees per agent; Argo Workflows supports git artifact inputs; every agentic coding tool integrates with git. Without git, TIDE produces orphaned PVC files. | MEDIUM | Use `go-git` or shelled-out `git`; the abstraction must be host-agnostic (GitHub, GitLab, Gitea, Bitbucket — see TS-22). Auth via K8s Secret (TS-19). |
| TS-13 | **Per-task structured logs streamed via `kubectl logs`** | Argo, Tekton, kagent — all stream Pod logs. Anthropic 2026 Agentic Coding Trends Report notes long-running agents (hours) make live log streams necessary for trust. Without it, users can't debug a stalled subagent. | LOW | Standard K8s; orchestrator + subagents emit structured JSON to stdout. Already in PROJECT.md. |
| TS-14 | **Structured JSON logs from orchestrator + Prometheus metrics**: waves dispatched, tasks completed, dispatch latency, failure rate | PROJECT.md Active. Argo and Tekton export equivalent metrics. Without metrics, capacity tuning is guesswork. | LOW | Standard `controller-runtime` exposes `/metrics`. Add domain counters/histograms in reconciler. |
| TS-15 | **OpenTelemetry tracing with OpenInference conventions** for Milestone→Phase→Plan→Task spans | PROJECT.md constraint. OpenInference is the de-facto standard for AI agent spans (Phoenix, LangSmith, Arize all consume it). Plain OTel GenAI conventions are still Experimental as of early 2026. Without OpenInference, traces require bespoke instrumentation downstream. | MEDIUM | Use `arize-ai/openinference` Go SDK if available, else hand-rolled span attributes following the spec's ten span kinds (CHAIN, LLM, TOOL, AGENT, etc.). Verify against current OpenInference spec at implementation time. |
| TS-16 | **Strict-by-default failure profile** at wave boundaries: failed task → dependents never dispatch, non-dependents in later waves continue | Spec §"Failure handling at wave boundaries" requires this contract exactly. PROJECT.md decision. CLAUDE.md: "Keep this contract intact when implementing the executor." Without it, the "failed task fails its wave, not its plan" property in spec §"Why this is advantageous" point 5 is broken. | MEDIUM | The reconciler must distinguish "dependent-of-failed" from "non-dependent" when scheduling the next wave. Indegree map already encodes this; just need the conditional dispatch. |
| TS-17 | **Plan validation before dispatch**: CRD admission webhook (or controller-side validation) rejects cyclic DAGs and dangling deps before any subagent is dispatched | Argo refuses to start workflows with invalid DAGs. Tekton rejects unconnected tasks. Without pre-dispatch validation, a bad plan wastes a wave's worth of subagent dispatches before failing. | MEDIUM | Pure Go validator; reuse the Kahn algorithm's cycle-detection branch. Optional CRD admission webhook for a faster failure surface. |
| TS-18 | **CRD set: Project, Milestone, Phase, Plan, Task** with their typed status blocks | PROJECT.md Active. The five-level paradigm is the product; CRDs are how it's expressed in K8s. Collapsing any level invalidates the spec. | HIGH | Kubebuilder scaffolding + per-CRD reconciler. Per-Task CRD size stays small to respect etcd's 1.5 MiB limit (PROJECT.md constraint). |
| TS-19 | **Credentials via K8s Secret references on Project**: LLM API keys, git push creds | PROJECT.md Active. kagent uses K8s Secrets. Plain Secrets is the lowest-friction starting point; ESO (External Secrets Operator) integration is anti-feature for v1 (see AF-9). | LOW | `spec.credentials: { llmSecretRef, gitSecretRef }`. Mount as env vars into subagent Pods via the dispatch interface. |
| TS-20 | **Namespace-per-project tenancy** with namespace-scoped RBAC | PROJECT.md decision. Standard K8s tenancy posture; kagent/Argo behave the same way. Full multi-tenant (cross-tenant quotas) is anti-feature for v1 (AF-1). | LOW | RBAC bundled in the Helm chart (TS-23). One TIDE install per cluster; one namespace per project. |
| TS-21 | **Pluggable Subagent interface — provider-agnostic dispatch** | PROJECT.md constraint. The OSS posture demands no vendor lock-in; the spec is model-agnostic ("Opus for milestone synthesis, Haiku for mechanical task execution"). kagent supports OpenAI/Azure/Anthropic/Vertex/Ollama; TIDE must match that ceiling structurally even if v1 ships only Claude-backed concrete impl. | MEDIUM | Define `Subagent` interface in Go: `Dispatch(ctx, spec) → (resultEnvelope, error)`. Concrete impl: Pod-spec template + Job dispatch + envelope reader on the PVC. Configurable per-level model selector. |
| TS-22 | **Git-host-agnostic remote push** (GitHub, GitLab, Gitea minimum) | PROJECT.md constraint. The OSS posture requires no hard-coded git host. Composio AO is tracker-agnostic (GitHub + Linear); kagent doesn't bind to any single source-of-truth. | LOW | Use generic git protocol (HTTPS or SSH) via `go-git`; the URL is a `Project` field. Per-host PR-creation is anti-feature for v1 (AF-12) — push only, humans open PRs. |
| TS-23 | **Helm chart distribution** with CRDs + controller + RBAC bundled | PROJECT.md Active. Argo, Tekton, kagent, Dapr all ship Helm charts. Without one, "deploy TIDE to your cluster" becomes a multi-step manual install. | MEDIUM | `helm install tide tide/tide` + values for `plannerConcurrency`, `executorConcurrency`, `image.tag`. CRDs in `/templates/` (not `/crds/`) so `helm upgrade` can roll them. |
| TS-24 | **`tide` CLI** wrapping common ops: apply, watch, tail, approve, cancel, resume, inspect-wave | PROJECT.md Active. Argo has `argo`, Tekton has `tkn`, Dapr has `dapr`. Without a CLI, the only interface is raw `kubectl` + custom resources, which is hostile for new users. | MEDIUM | Cobra-based; thin wrapper over the K8s API. Mutations only via CLI/`kubectl` since dashboard is read-only (PROJECT.md + AF-3). |
| TS-25 | **Read-only web dashboard** rendering Planning DAG + Execution DAG, wave progress, per-task status, click-through to artifacts; streams `kubectl logs` from running task Pods | PROJECT.md Active. Argo UI, Tekton Dashboard, kagent UI all exist. Anthropic 2026 Agentic Coding Trends Report: long-running agents (hours) make live visibility necessary for trust. Without it, the only view into a run is `kubectl get` polling. | HIGH | Separate Deployment; reads CRDs via the K8s API, streams logs via the API server's log endpoint. **Read-only** is the discipline: all mutations route through CLI / `kubectl` for a single auth surface (PROJECT.md, AF-3). |
| TS-26 | **End-to-end self-hosting demo**: TIDE in a kind cluster drives its own next milestone on this repo, producing artifacts + merged commits | PROJECT.md Core Value: "TIDE-on-TIDE must work — that's the bar for 'v1 ships.'" Without it, the paradigm isn't proven and the implementation isn't proven; they're proven together or not at all. | HIGH | This is the integration test for everything. Not a unit test; the full pipeline. |
| TS-27 | **Apache 2.0 license + docs sufficient for an external operator to install and run a project end-to-end** | PROJECT.md Active. K8s ecosystem default. Without an external user being able to install + run, "open source" is aspirational. | LOW | LICENSE file + README.md (already exists, doubles as spec) + quickstart docs. |

### Differentiators (Where TIDE's Design Pays Off — v1 Differentiation)

These are the features that justify TIDE existing alongside Argo / Temporal / kagent rather than being a `Workflow` template inside one of them.

| # | Feature | Value Proposition | Complexity | Notes |
|---|---------|-------------------|------------|-------|
| D-1 | **Two typed DAGs (Planning + Execution) at the API boundary, not one unified `Workflow.spec.dag`** | Argo and Tekton both model a flat DAG. The spec argues planning fans out wide (most phases plan in parallel from one architecture spec) and execution fans out narrow (file-level deps serialize work). Two separate DAGs let TIDE size two separate parallelism budgets (TS-9) and run two distinct review cadences — value that's literally impossible to express in Argo's data model. | MEDIUM | CRD field names keep them apart: `Project.spec.planningGraph` vs `.executionGraph`. Same Kahn-layered algorithm runs on both, but the *types* are different and the CRDs distinguish them. CLAUDE.md: "APIs and CRDs should keep these typed apart, not unified into one 'DAG' abstraction." |
| D-2 | **Five-level hierarchy expressed as five CRDs** (Project, Milestone, Phase, Plan, Task), each with its own typed reviewable artifact | Argo has Workflow + Template + Step. Tekton has Pipeline + Task. kagent has Agent + Tool. None of them model the milestone-phase-plan-task-wave cognitive hierarchy that bounded-context agentic work needs (spec §"Why this is advantageous" point 1: "Context-window economics"). The five-level shape is what makes Opus-for-milestones-and-Haiku-for-tasks even *expressible* (spec §"Why this is advantageous" point 6). | HIGH | Each level needs its own controller, status block, and dispatch profile. The big one. Five well-shaped CRDs with proper validation will be a substantial part of v1. |
| D-3 | **Wave-derived schedule, re-derived on every plan edit** — orchestrator never accepts a wave list as input | Argo accepts an explicit DAG of tasks but doesn't expose "waves" as a first-class queryable concept. Temporal exposes activities but not a wave abstraction. TIDE's `WaveStatus` on the Plan/Task CRD is queryable: "give me all tasks in wave 3 of plan A.1.1" is a label selector. Spec §"Wave computation" property 5: monotonic under DAG edits, so a plan revision mid-run is well-behaved. | MEDIUM | The orchestrator's reconciler recomputes waves whenever a Plan's task DAG changes; no schedule cache. CLAUDE.md: "If the persistence layer starts wanting to store the full schedule, that's a smell — re-derive instead." |
| D-4 | **Indegree-map + completed-task-set resumption** (and *only* that — no in-flight wave snapshots, no scheduler state) | Temporal persists full workflow state (event history); LangGraph persists checkpoints to PostgreSQL. TIDE's resumption state is *the minimum possible*: indegree map (derivable from artifacts) + completed-task set (derivable from git history). This is the property that lets TIDE survive controller-pod-eviction without an external DB — and it's a direct consequence of the Kahn-layered choice. | MEDIUM | CLAUDE.md: "Resumption state is minimal: indegree map + completed-task set. If the persistence layer starts wanting to store the full schedule, that's a smell." This is the load-bearing property that makes "CRD-status-only persistence" (PROJECT.md) viable. |
| D-5 | **Per-level model selection** (e.g., Opus for milestone synthesis, Haiku for plan execution) configured on the Project CRD | Spec §"Why this is advantageous" point 6. kagent supports multi-LLM but per-agent, not per-cognitive-level. Argo and Temporal don't model cognitive levels at all. TIDE makes this a first-class field. | LOW | `spec.subagents.milestone.model`, `.phase.model`, `.plan.model`, `.task.model` — defaults sensible, override-able. Honored by the Subagent dispatch interface (TS-21). |
| D-6 | **Per-level human gate policy as a Project CRD field** (combinatorial: approve-milestones-auto-pass-plans, or any other combination) | Argo has `suspend` templates but they're per-template, not policy-by-level. Tekton manual-approval-gate is per-PipelineRun. Composio AO has `escalateAfter` but it's a single timer. TIDE's per-*level* gates (Milestone / Phase / Plan / Task each independently) express the spec's "scales from fully supervised to fully autonomous without restructuring" property. | MEDIUM | See TS-10. The differentiation is the *combinatorial* per-level policy, not just the existence of gates. |
| D-7 | **Slack-tide review checkpoints between waves**, with optional human review of wave outputs | Argo's `suspend` templates are insertable between tasks but require explicit graph edits. TIDE's wave model gives free between-wave checkpoints by construction — every wave is a natural review boundary, no extra graph nodes needed. Maps the spec's "Slack tide" vocabulary onto a controller behavior. | MEDIUM | Optional per-Project: `betweenWaveReview: { phase: required, plan: auto }`. When required, controller writes a `Pending Wave Review` condition after each wave's join barrier. |
| D-8 | **Live planning + execution DAG dashboards (two distinct views)** rendering both DAGs side-by-side | Argo UI shows one DAG. kagent shows agent topology, not work topology. None of the comparables render the two-DAG split visually. TIDE's dashboard makes the structural distinction legible to a human reviewer. | HIGH | Folded into TS-25 but the *content* is the differentiator — two graphs, not one. Mermaid/D3 rendering of nested Project→Milestone→Phase→Plan→Task containment for Planning; flat wave-subgraph view for Execution (matches spec's Mermaid diagrams). |
| D-9 | **Artifacts-as-source-of-truth, CRDs-as-index**: resumption reads from artifacts (`MILESTONE.md`, `PLAN.md`, task diffs), CRD `.status` is a cache | Temporal's source of truth is the event history in its DB. Argo's is the Workflow object. TIDE inverts: the source of truth is the Markdown files in the target repo, which means a human can hand-edit a `PLAN.md` between waves and TIDE re-derives the schedule on next reconcile. Spec + PROJECT.md + CLAUDE.md all hammer this. | MEDIUM | Reconcile flow: read artifacts → diff against `.status` → re-derive waves → dispatch. Disagreement between artifact and `.status` always defers to artifact. |
| D-10 | **Cycle-as-bug, not cycle-as-runtime-condition** | Argo has a `DAGRetryStrategy` that could in principle support cyclic graphs (it doesn't, but the door isn't slammed). TIDE refuses to start a run on a cyclic DAG — there is no recovery path. The spec is explicit. | LOW | Falls out of TS-6 + TS-17. The *differentiation* is the discipline: no future PR will add cycle-recovery features (CLAUDE.md, README §"Failure handling"). |
| D-11 | **Water/tide vocabulary** in CRD names, log lines, dashboard, docs (Rising tide, Slack tide, Tidal lock, Tidepool, TIDE pod) | Argo's vocabulary is generic. Tekton's is CI-shaped. TIDE's vocabulary is consistent and load-bearing — the K8s pun on "TIDE pod" is intentional (CLAUDE.md). Vocabulary discipline is a quality signal for an OSS project. | LOW | Code review enforces it. PROJECT.md Context: "Used in code names, CRD names, log lines, docs." |
| D-12 | **Two separately-sized worker pools** (planner concurrency != executor concurrency) reflected in the Helm chart values + Project CRD | Argo has a single `parallelism`. Kueue has cohorts/cluster-queues but they're capacity primitives, not cognitive-pool primitives. The fact that planning DAGs fan out wide and execution DAGs fan out narrow is a spec-level argument that TIDE materializes into two integer fields. | LOW | Helm: `plannerConcurrency: 8`, `executorConcurrency: 4`. CRD-level override per Project. The split is what justifies the architecture. |
| D-13 | **Self-hosting demonstration as v1 acceptance test** — TIDE drives its own next milestone | Argo doesn't run Argo's own development. Temporal doesn't either. The dogfood test is real and high-bar: if TIDE can't drive its own next milestone, the paradigm is wrong, the controller is wrong, or both. | HIGH | TS-26 is the table-stakes form ("does it work end-to-end"); D-13 is the *narrative* form ("this is how we know it's good"). Listed in both intentionally. |

### Anti-Features (Deliberately NOT in v1, With Reason)

These are features that comparable systems have or that users will plausibly request — and that we are not building in v1. Citing the rejection rationale prevents re-litigation in phase research later.

| # | Anti-Feature | Why Requested | Why Rejected for v1 | Alternative / Future Path |
|---|--------------|---------------|---------------------|---------------------------|
| AF-1 | **Multi-tenant cluster posture** (cross-tenant quotas, per-tenant RBAC isolation, OIDC integration) | Enterprise users running multiple teams' agents in one cluster will eventually want this. | PROJECT.md Out of Scope: "Namespace-per-project covers the OSS user; full tenant isolation is real work that doesn't move the paradigm." | Post-v1. Namespace-per-project (TS-20) is a forward-compatible foundation. |
| AF-2 | **gRPC streaming subagent protocol** (or any non-Job dispatch mechanism for v1) | Streaming partial results from a long-running subagent is lower-latency than the Job-+-PVC-+-exit-code envelope. kagent uses A2A streaming. | PROJECT.md Out of Scope: "Pod-per-task Job + result envelope is enough for v1. A streaming sidecar can be added later behind the same Subagent interface without redesign." | v2 — slot a streaming concrete impl behind the existing Subagent interface (TS-21). Don't redesign the interface for v1. |
| AF-3 | **Mutation actions on the dashboard** (retry wave, edit plan, pause/resume, approve gate, cancel) | Argo UI supports all of these. Users will absolutely ask for them. | PROJECT.md Out of Scope: "v1 dashboard is read-only. Mutations route through `tide` CLI / `kubectl` so there's a single auth surface." Read-only also keeps the dashboard component shippable in v1. | Post-v1, behind the same RBAC the CLI uses. The CLI is the v1 mutation surface. |
| AF-4 | **External database (Postgres / SQLite) for run history, scheduling cache, or audit trail** | Temporal needs a DB. LangGraph deployments need Postgres. The instinct will be "we should persist runs somewhere queryable." | PROJECT.md Out of Scope: "CRD-status-only is technically sufficient at the scale of one human watching one run. Re-evaluate only if dashboard query shapes outgrow label-selector queries." Spec + CLAUDE.md: "the orchestrator's database is a cache/index, not the truth." | Post-v1 — and only if dashboard query needs actually outgrow label-selector queries. The artifacts in git are the durable record. |
| AF-5 | **Vendored GSD Markdown workflows** (copying `~/.claude/get-shit-done/workflows/*.md` into the orchestrator container) | It would be faster to ship if the planner just shelled out to Claude Code with the existing GSD Markdown. | PROJECT.md Out of Scope: "TIDE reads `get-shit-done` as design reference but the planner/executor logic and prompts are reimplemented in Go. Markdown workflows would lock TIDE to one bootstrap host." Portability is the v1 OSS posture; vendoring locks TIDE to a single bootstrap. | Reimplement the GSD *paradigm* in Go; embed prompts as Go templates owned by TIDE. Provider/host portable by construction. |
| AF-6 | **Critical-path / HEFT / heterogeneous-resource schedulers at the wave layer** | When subagent pools become heterogeneous (Opus for hard tasks, Haiku for mechanical), there's a real argument for CPM-style scheduling. | Spec §"Alternatives considered and rejected": "Premature at the paradigm layer. If subagent pools become heterogeneous, TIDE adds a wave-internal sub-scheduler rather than replacing Kahn-layered at the wave level." PROJECT.md Out of Scope. | v2+ — wave-internal sub-scheduling, *behind* Kahn-layered at the wave level. Layered Kahn stays the wave producer. |
| AF-7 | **Wave or cycle "recovery"** (automatic cycle resolution, wave retry-with-relaxed-deps) | Users may ask "the plan has a cycle, can you fix it?" or "the wave failed, can you retry with task X removed?" | PROJECT.md + CLAUDE.md + spec: "Cycles are bugs detected at plan-validation time. Refuse and surface, don't recover." The plan is the source of truth; if the plan is wrong, the human fixes the plan. | None. This stays rejected. |
| AF-8 | **Non-Kubernetes runtime** (Docker Compose, bare metal, Nomad, local-process orchestration) | Some users will want to run TIDE on a single Mac without K8s. | PROJECT.md Out of Scope: "The K8s pun is load-bearing; pod isolation, RBAC, watches, and Jobs are what make the dispatch model tractable." | For local-Mac users: that's what GSD is for. TIDE is the K8s generalization. Run TIDE in `kind` if you need a local cluster. |
| AF-9 | **External Secrets Operator (ESO) / Vault first-class integration** | Enterprise users want ESO/Vault. kagent supports SPIFFE for workload identity. | PROJECT.md Out of Scope: "Plain K8s Secrets only for v1. ESO docs/examples can land later without changing the CRD contract." | The `Project.spec.credentials.{llmSecretRef, gitSecretRef}` contract is forward-compatible with ESO — ESO can write the K8s Secret. Docs example in v1.x. |
| AF-10 | **Vendor lock-in to one LLM provider** (Anthropic-specific code in the orchestrator) | It would be faster to ship if the orchestrator could call the Anthropic SDK directly. | PROJECT.md Out of Scope. The Subagent interface (TS-21) is provider-agnostic by construction. | Anthropic-backed concrete impl ships in v1; the interface forbids the orchestrator from knowing it's Anthropic. |
| AF-11 | **Cycle-recovery, schedule-caching, full-event-history storage** (any persistence pattern beyond indegree-map + completed-task-set) | Temporal-pattern thinking: "let's store the schedule for fast resume." | CLAUDE.md: "If the persistence layer starts wanting to store the full schedule, that's a smell — re-derive instead." Re-derivation is O(V+E) and cheap. | None. Re-derive. |
| AF-12 | **Per-git-host PR creation** (open the PR via GitHub API / GitLab API / Gitea API) | Composio AO does this; users will want it. | The host abstraction (TS-22) is "push to a remote." The *human* opens the PR in their host's UI v1. Per-host PR creation is a 3-way matrix of API integrations that's premature before v1 ships. | v1.x — a PR-opener plugin per host, behind a `Project.spec.git.prAutomation: { provider: github | gitlab | gitea }` field that doesn't exist v1. |
| AF-13 | **Auto-CI-fix feedback loop** (subagent reads CI failure, patches, re-pushes) | Composio AO does this; GitHub Copilot agent-assignment does this; Claude Code Auto Mode does this. | Out of scope for v1 — TIDE's "execution complete" boundary is "task diff merged into the project branch." What happens *after* the branch lands (PR review, CI, merge to main) is a downstream concern. Trying to include CI fix loops in v1 expands the project surface area significantly without moving the paradigm. | v1.x — a `Project.spec.postExecution.ciFixLoop: bool` field, implemented as additional subagent dispatches against CI-failure artifacts. |
| AF-14 | **Multi-cluster dispatch** (Kueue MultiKueue-style) | Kueue is prioritizing MultiKueue for 2026. Enterprise users running pooled GPU clusters will want this. | Not requested. v1 = single cluster. PROJECT.md Out of Scope (implicit via "one TIDE install per cluster"). | v2+. |
| AF-15 | **MCP / A2A protocol surface** (TIDE-as-MCP-server, TIDE-agent-as-A2A-callable) | kagent makes a big deal of MCP + A2A support. Users coming from kagent will expect it. | Out of scope. TIDE's subagents *may* speak MCP internally (the Claude-Code-backed impl does), but TIDE itself is not an MCP server; it's an orchestrator. A2A is for inter-agent comms, not orchestrator-to-agent. | If demand emerges post-v1, expose a TIDE-as-MCP server that wraps `tide apply` / `tide watch`. v2+. |
| AF-16 | **Drag-to-edit DAG in the dashboard** | "Why can't I just drag the boxes around to add a dep?" | Dashboard is read-only (AF-3). Plan edits go through `PLAN.md` in git, which is the source of truth (D-9). | None. The artifact is the editor. |

### Deferred (Table-Stakes-Shaped Features Punted to Post-v1)

These look like table stakes but PROJECT.md explicitly punts them, with rationale.

| # | Feature | Looks Like Table Stakes Because | Why Deferred |
|---|---------|-------------------------------|--------------|
| DF-1 | **Conservative failure profile** (halt wave on first task failure, including non-dependent siblings) | Spec §"Failure handling" explicitly names it as a configurable profile. Some users will want it from day one. | Strict-by-default is the v1 default per PROJECT.md. Conservative profile lands as a per-Project setting "later if needed." Not in v1 scope. |
| DF-2 | **PR-opening + comment-resolution loops** | Composio AO, GitHub Copilot agent-assignment, Claude Code Auto Mode all do it. Users coming from those will expect it. | AF-12 + AF-13. The v1 boundary is "diff pushed to remote branch." PR lifecycle is downstream and per-git-host. v1.x. |
| DF-3 | **Custom resource validation webhook** (vs controller-side validation only) | All mature K8s operators ship admission webhooks for faster rejection. | TS-17 covers correctness; the webhook is an optimization on rejection latency. Helm chart can include a cert-manager-based webhook in v1.x. |
| DF-4 | **MultiKueue / Kueue integration for capacity management** | Kueue is the K8s-native solution for capacity quotas; TIDE's `plannerConcurrency`/`executorConcurrency` is a poor substitute at scale. | v1 caps are integer-on-the-Project. Kueue integration is real work that doesn't move the paradigm. v2+. |
| DF-5 | **OLM bundle + OperatorHub listing** | Mature operators ship OLM bundles. OpenShift users will expect it. | Helm chart (TS-23) is the v1 distribution. OLM bundle can be generated from the Helm chart later; not v1 scope. |
| DF-6 | **Agent Sandbox / gVisor / Kata Containers integration** for stronger pod isolation | sigs.k8s.io/agent-sandbox is the K8s SIG Apps recommendation for AI agent sandboxing. kagent's SandboxAgent CRD wraps it. | v1 uses standard K8s Pods with pod-per-task isolation; gVisor/Kata sandboxing is a hardening layer, not a correctness requirement. v2+. |
| DF-7 | **Workflow templates / Project templates** ("scaffold me a TIDE Project for a typical web-app milestone") | Argo has `WorkflowTemplate`, Tekton has `ClusterTask`. Lowers the barrier to first run. | v1 ships docs + examples; templating CRD comes after the CRDs themselves are stable. v1.x. |
| DF-8 | **External notification hooks** (Slack on gate pending, email on milestone complete) | Most workflow engines have webhook outputs. | v1 emits structured logs + Prometheus metrics; downstream tooling (Alertmanager, OTel exporters) covers notification by composition. Native webhooks v1.x. |

---

## Feature Dependencies

The dependency graph between features (read top to bottom; arrows = "requires"):

```
TS-18 (CRD set)
    ├──> TS-1 (Project lifecycle)
    │       ├──> TS-2 (watch/status)
    │       ├──> TS-3 (cancel)
    │       ├──> TS-17 (plan validation)
    │       └──> TS-19 (creds via Secret)
    │
    ├──> TS-7 (wave-derived) ──> TS-6 (cycle detection) ──> TS-17 (plan validation)
    │       └──> TS-16 (strict-by-default failure profile)
    │               └──> TS-4 (resume after restart)
    │                       └──> D-4 (indegree-map+completed-set resumption)
    │
    └──> TS-8 (pod-per-task) ──> TS-11 (artifact PVC) ──> TS-12 (git push)
                                                              └──> TS-22 (host-agnostic remote)

TS-21 (pluggable Subagent interface)
    ├──> D-5 (per-level model selection)
    ├──> TS-5 (retry on transient failure)
    └──> AF-10 prevention (no Anthropic-specific code in orchestrator)

TS-9 (capacity caps)
    └──> D-12 (two separately-sized pools)

TS-10 (per-level human gates)
    ├──> D-6 (per-level gate policy as Project CRD field)
    └──> D-7 (slack-tide between-wave review)

TS-13 (per-task logs) + TS-14 (metrics) + TS-15 (OTel/OpenInference traces)
    └──> TS-25 (dashboard) ──> D-8 (two-DAG dashboard view)

TS-23 (Helm chart) + TS-24 (CLI) + TS-25 (dashboard) + TS-27 (license + docs)
    └──> TS-26 (self-hosting demo) ──> D-13 (self-hosting as v1 acceptance)

D-1 (two typed DAGs) ──> D-2 (five CRDs) ──> D-3 (wave-derived schedule queryable)
                                                  └──> D-9 (artifacts-as-truth)
```

### Dependency notes

- **The five CRDs (D-2) are the longest-pole dependency.** Every other Active item in PROJECT.md hangs off them. Get the CRDs right (correct Spec/Status separation, proper validation, +kubebuilder:subresource:status everywhere) before reconcilers grow real logic.
- **D-4 (minimal resumption state) is what makes AF-4 (no external DB) viable.** If D-4 is wrong, the v1 persistence story collapses and you'll be reaching for Postgres.
- **TS-21 (pluggable Subagent interface) is what makes AF-10 (no vendor lock-in) enforceable.** Define the interface in the controller package; concrete impl lives in a separate package. If reconciler code ever imports the Anthropic SDK directly, you've broken the contract.
- **D-1 (two typed DAGs) is what makes D-12 (two parallelism pools) and D-6 (per-level gates) coherent.** If you collapse to one DAG, you lose the cognitive-level argument that justifies the rest.
- **TS-26 (self-hosting demo) is the only feature that exercises every other feature simultaneously.** Plan for it to fail the first N times you try; it's the integration-test-of-everything.

---

## MVP Definition

### Launch With (v1) — Bound by PROJECT.md Active

These are the v1 cuts. All are either in PROJECT.md Active or are direct table-stakes/differentiator dependencies of those items.

- [ ] **TS-18 — CRD set: Project / Milestone / Phase / Plan / Task** (the five-level hierarchy as K8s API objects) — load-bearing for everything else
- [ ] **TS-1 to TS-3 — Lifecycle: apply / watch / cancel** — basic K8s operator UX
- [ ] **TS-4 + D-4 — Resume after orchestrator restart** with minimal persistence (indegree map + completed-task set) — the correctness property
- [ ] **TS-6 + TS-7 + TS-17 — Wave-derived schedule, cycle detection, plan validation** — the algorithm
- [ ] **D-1 + D-2 + D-3 — Two typed DAGs, five CRDs, wave-derived schedule queryable** — the structural differentiation
- [ ] **TS-8 + TS-11 + TS-12 + TS-22 — Pod-per-task isolation, artifact PVC, git push at level boundary, host-agnostic remote** — the dispatch + artifact path
- [ ] **TS-16 — Strict-by-default failure profile at wave boundaries** — the contract
- [ ] **TS-5 — Retry on transient failure** (default 2 retries, exponential backoff)
- [ ] **TS-9 + D-12 — Separate planner / executor concurrency caps** — the two-budget split
- [ ] **TS-10 + D-6 + D-7 — Per-level human gates, gate policy as CRD field, slack-tide between-wave review** — the human-in-the-loop differentiation
- [ ] **TS-13 + TS-14 + TS-15 — Structured logs, Prometheus metrics, OTel+OpenInference traces** — the observability baseline
- [ ] **TS-19 + TS-20 — K8s Secret creds, namespace-per-project tenancy** — the security/tenancy minimum
- [ ] **TS-21 + D-5 + AF-10 prevention — Pluggable Subagent interface, per-level model selection, no Anthropic-specific code in orchestrator** — the OSS portability promise
- [ ] **TS-23 + TS-24 + TS-25 + D-8 — Helm chart, CLI, read-only dashboard with two-DAG view** — the distribution + UX surface
- [ ] **TS-26 + D-13 — Self-hosting demo: TIDE drives its own next milestone on this repo** — the acceptance test
- [ ] **TS-27 — Apache 2.0 + docs sufficient for an external operator** — the OSS posture
- [ ] **D-10 — Cycle-as-bug discipline** — codified in `CONTRIBUTING.md` and PR review
- [ ] **D-11 — Water/tide vocabulary** in CRD names, log lines, dashboard, docs
- [ ] **D-9 — Artifacts-as-source-of-truth, CRDs-as-index** — codified in reconciler comments + tests

### Add After Validation (v1.x)

Triggered after v1 ships and real users surface real needs.

- [ ] **DF-1 — Conservative failure profile** — if users hit cascading non-dependent failures and want hard-stop behavior
- [ ] **DF-3 — Validation webhook** — if users complain about late rejection of invalid plans
- [ ] **DF-5 — OLM bundle** — if OpenShift users request it
- [ ] **DF-7 — Project templates** — if first-run-friction is the bottleneck on adoption
- [ ] **DF-8 — Notification hooks (Slack / webhook)** — if structured logs + Alertmanager aren't enough for ops users
- [ ] **AF-9 → ESO/Vault docs+examples** — once enterprise users start asking
- [ ] **AF-12 → Per-host PR creation** — implemented behind a `prAutomation` field, GitHub first
- [ ] **AF-13 → Auto-CI-fix feedback loop** — behind a `postExecution.ciFixLoop` field

### Future Consideration (v2+)

Real work that doesn't move the v1 paradigm.

- [ ] **AF-1 — Full multi-tenant posture** (per-tenant quotas, cross-tenant RBAC, OIDC)
- [ ] **AF-2 — gRPC streaming subagent protocol** (behind the existing Subagent interface)
- [ ] **AF-3 — Dashboard mutation actions** (retry wave, edit plan, pause/resume from UI)
- [ ] **AF-4 — External DB** (re-evaluate only if dashboard query shapes outgrow label selectors)
- [ ] **AF-6 — Wave-internal sub-scheduler** for heterogeneous subagent pools (HEFT-like, behind Kahn-layered)
- [ ] **AF-14 — Multi-cluster dispatch** (Kueue MultiKueue integration)
- [ ] **AF-15 — MCP/A2A protocol surface** (if demand emerges)
- [ ] **DF-4 — Kueue integration** for capacity management at scale
- [ ] **DF-6 — Agent Sandbox / gVisor / Kata Containers** for hardened pod isolation

---

## Feature Prioritization Matrix

Selecting the top-priority items (the long poles) — full prioritization is the table sets above.

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| TS-18 (CRD set) | HIGH | HIGH | P1 |
| TS-26 + D-13 (self-hosting demo) | HIGH (it's the v1 acceptance test) | HIGH | P1 |
| TS-4 + D-4 (resume with minimal state) | HIGH (load-bearing correctness) | HIGH | P1 |
| TS-21 (pluggable Subagent interface) | HIGH (OSS portability) | MEDIUM | P1 |
| D-1 + D-2 (two typed DAGs, five CRDs) | HIGH (structural differentiation) | HIGH | P1 |
| TS-10 + D-6 (per-level gates as CRD field) | HIGH (paradigm value) | MEDIUM | P1 |
| TS-25 + D-8 (dashboard, two-DAG view) | HIGH (trust signal for long-running agents) | HIGH | P1 |
| TS-23 (Helm chart) | HIGH (OSS distribution) | MEDIUM | P1 |
| TS-15 (OTel + OpenInference) | MEDIUM (queryability in standard tools) | MEDIUM | P1 |
| TS-5 (retry on transient failure) | HIGH (every LLM-driven system has flaky calls) | LOW | P1 |
| TS-9 + D-12 (two-pool concurrency) | MEDIUM (correctness in capacity-bound runs) | LOW | P1 |
| DF-1 (conservative failure profile) | LOW initially (becomes HIGH if cascading non-dependent failures surface) | LOW | P2 |
| AF-13 (auto-CI-fix loop, post-v1) | HIGH (Composio AO / Copilot expectation) | HIGH | P2 |
| AF-2 (gRPC streaming, post-v1) | MEDIUM (latency improvement, not correctness) | HIGH | P3 |
| AF-1 (multi-tenant posture, post-v1) | HIGH for enterprise | HIGH | P3 |

**Priority key:**
- P1: Must have for v1 — directly in PROJECT.md Active
- P2: v1.x — add post-launch if validated by real use
- P3: v2+ — real work that doesn't move the v1 paradigm

---

## Competitor Feature Analysis

A side-by-side of the closest comparables on the features that most distinguish TIDE.

| Feature | Argo Workflows | Tekton Pipelines | kagent | Temporal | Composio AO | LangGraph deploy | TIDE v1 |
|---------|---------------|------------------|--------|----------|-------------|------------------|---------|
| K8s-native CRDs | Yes (Workflow) | Yes (Pipeline/Task) | Yes (Agent/Tool) | No (its own DB) | No (host process) | No (host process + Postgres) | **Yes (5 CRDs)** |
| DAG support | Flat DAG | Flat DAG | Multi-agent topology, not a work DAG | Activity graph | Task list per agent | StateGraph | **Two typed DAGs (Planning + Execution)** |
| Cycle detection | Yes (rejection at submit) | Yes | n/a | n/a | n/a | n/a | **Yes (rejection at validation, Kahn-derived)** |
| Wave abstraction | No (DAG levels not exposed) | No | n/a | No | No | No | **Yes (Kahn-layered, queryable)** |
| Pod-per-task | Yes (default) | Yes (default) | Per agent | n/a | Process or container | n/a | **Yes (Job-per-Task)** |
| Per-task retries | Yes (retryStrategy) | Yes | n/a | Yes | Yes (`retries: 2`) | Yes (graph node retry) | **Yes (2 default)** |
| Suspend / resume | Yes (suspend template) | Yes (TaskRun pending) | n/a | Yes (durable) | n/a | Yes (checkpointed) | **Yes (per-level gates)** |
| Cancel | Yes (terminate) | Yes (cancel) | n/a | Yes | n/a | n/a | **Yes** |
| Resume after orchestrator restart | Yes (workflow object survives) | Yes (PipelineRun survives) | n/a | Yes (DB-backed) | n/a (host process state) | Yes (Postgres checkpoint) | **Yes (indegree map + completed-set, no DB)** |
| Artifact handling | S3/GCS/MinIO | PVC | n/a | Inline / state | Git worktree | n/a | **PVC + git push at level boundary** |
| Git integration | Input artifact via git clone | Yes (resolvers) | Indirect | Indirect | Yes (worktrees + PRs) | Indirect | **Yes (clone + push, host-agnostic)** |
| PR automation | No | No (CI tools layer on top) | No | No | **Yes (full PR lifecycle)** | No | **No (v1) — push only, PR opening v1.x** |
| Human approval gates | Yes (suspend template) | Yes (manual-approval-gate) | n/a | Yes (HITL) | Yes (`escalateAfter`) | Yes (interrupt) | **Yes, per-level policy on CRD** |
| Per-level gate policy | No (per-template, not per-cognitive-level) | No | n/a | No | No (single timer) | No | **Yes — paradigm differentiator** |
| Multi-LLM support | n/a | n/a | Yes (multi-provider) | Via SDK | Agent-agnostic | Yes | **Yes (interface) — Anthropic-first concrete impl** |
| Per-level model selection | n/a | n/a | Per-agent, not per-cognitive-level | n/a | n/a | n/a | **Yes — paradigm differentiator** |
| Read-only dashboard | Yes (Argo UI) | Yes (Tekton Dashboard) | Yes | Yes (Temporal UI) | Yes | Yes (LangSmith) | **Yes — two-DAG view (differentiator)** |
| Dashboard mutations | Yes | Yes | Yes | Yes | Yes | Yes | **No (read-only — single auth surface)** |
| OTel tracing | Yes | Yes | Yes (OpenTelemetry + A2A) | Yes | n/a | Yes (LangSmith) | **Yes (OpenInference conventions)** |
| OpenInference conventions | No (OTel generic) | No | Yes | No | No | Partial | **Yes — paradigm-aligned** |
| Helm chart | Yes | Yes | Yes | Yes (Cloud or self-host) | n/a (CLI tool) | Yes (LangSmith chart) | **Yes (Apache 2.0)** |
| OLM bundle / OperatorHub | Yes | Yes | Likely (CNCF Sandbox) | No | n/a | No | **No v1 (DF-5)** |
| Multi-tenant | Per-namespace | Per-namespace | Per-namespace | Per-namespace + tenant cloud | n/a (per-user) | Yes (self-host) | **Namespace-per-project v1 (AF-1 for full)** |
| Self-hosting demo | Argo doesn't run Argo's CI | n/a | n/a | n/a | The CLAUDE.md self-improvement loop | n/a | **Yes — v1 acceptance test (D-13)** |
| Vocabulary | Generic ("workflow", "step") | CI-shaped ("pipeline", "task") | Generic | Generic ("workflow", "activity") | Generic | Graph-shaped | **Water/tide, consistent and load-bearing** |

### Key competitor takeaways

- **No comparable system has the two-typed-DAGs property.** Every other system has a single DAG primitive (or no DAG at all, in Temporal's case). This is genuine differentiation — not just marketing.
- **No comparable system has the five-level hierarchy as five CRDs.** kagent has Agent + Tool; Argo has Workflow + WorkflowTemplate + Step. The cognitive-level argument is unique to TIDE.
- **kagent is the closest neighbor.** Same posture (K8s-native, CRD-driven, multi-LLM, OTel-instrumented, Helm-distributed). It does *agent topology*; TIDE does *work topology*. The CNCF Sandbox listing for kagent is a good model for TIDE's eventual donation path.
- **Composio Agent Orchestrator is the closest functional neighbor in the coding-specific space.** Same parallel-agent-on-coding-work shape. But Composio AO runs as a host process (not K8s) and includes PR-lifecycle automation that TIDE deliberately defers (AF-12, AF-13). The split is intentional: TIDE wins on K8s-native + paradigm clarity; Composio AO wins on out-of-the-box completeness for GitHub-centric flows.
- **Temporal's durable-execution model is what TIDE achieves *without* a database.** The minimal-resumption-state property (D-4) is the structural argument for that — and it's a direct consequence of choosing Kahn-layered over event-history-based scheduling.

---

## Implications for the Roadmap (Phase Ordering)

The dependency graph above implies a natural phase ordering for v1:

1. **CRDs first** (TS-18, D-2). Five CRDs with proper Spec/Status separation and validation. Reconcilers are no-op stubs at this stage.
2. **Kahn-layered algorithm + plan validation** (TS-6, TS-7, TS-17, D-3). Pure-Go library, no controller dependencies. Cycle detection + wave derivation. Unit tests against the spec's worked example.
3. **Subagent interface + concrete Claude-Code impl** (TS-21, D-5, AF-10 prevention). Define interface; ship a Pod-spec-based concrete impl; verify zero Anthropic SDK imports outside the concrete impl package.
4. **Reconciler + dispatch path** (TS-1 to TS-3, TS-8, TS-9, TS-11, TS-16). The orchestrator's reconcile loops. Strict-by-default failure profile. PVC artifact handling. Pod-per-task dispatch.
5. **Resumption + minimal persistence** (TS-4, D-4, D-9). Indegree-map + completed-task-set. Controller-restart integration test. Artifacts-as-truth reconcile flow.
6. **Git integration** (TS-12, TS-22, TS-19). Clone on apply; push at level boundary. Host-agnostic via `go-git`. Secret-based creds.
7. **Human gates** (TS-10, D-6, D-7). Per-level gate policy on Project CRD. `tide approve` CLI. Slack-tide between-wave review.
8. **Observability** (TS-13, TS-14, TS-15). Structured logs; Prometheus metrics; OTel + OpenInference spans.
9. **CLI** (TS-24). Cobra-based wrapper.
10. **Dashboard** (TS-25, D-8). Read-only, two-DAG view, log streaming.
11. **Distribution** (TS-23, TS-27). Helm chart with CRDs + controller + RBAC. Apache 2.0. README/docs.
12. **Self-hosting demo** (TS-26, D-13). The acceptance test. Likely to surface bugs across all prior phases — budget time accordingly.

Phases 1–3 are the foundation. Phase 4 is the largest. Phases 5–11 can fan out somewhat (artifacts/git, gates, observability, CLI, dashboard, distribution can be parallel after phase 4). Phase 12 is sequential and the longest pole on calendar time because it's the integration of everything.

---

## Sources

### Comparable systems (K8s workflow engines)
- [Argo Workflows official documentation](https://argo-workflows.readthedocs.io/en/latest/) — DAG, retries, suspend/resume, artifacts
- [Tekton Pipelines v1.11.0 release blog](https://tekton.dev/blog/2026/03/30/tekton-pipelines-v1.11.0-taskrun-pending-multi-url-hub-resolver-and-pvc-auto-cleanup/) — TaskRun pending status (approval gates)
- [Tekton manual approval gate](https://tekton.dev/vault/operator-main/manualapprovalgate/)
- [Kueue overview](https://kueue.sigs.k8s.io/docs/overview/) — capacity, quotas, multi-cluster
- [Argo Workflows suspending walk-through](https://argo-workflows.readthedocs.io/en/latest/walk-through/suspending/)

### Comparable systems (AI agent runtimes on K8s)
- [kagent docs and CRD model](https://kagent.dev/) — agent CRDs, multi-LLM, OTel + A2A
- [kagent GitHub](https://github.com/kagent-dev/kagent)
- [sigs.k8s.io/agent-sandbox blog](https://kubernetes.io/blog/2026/03/20/running-agents-on-kubernetes-with-agent-sandbox/) — Pod-per-agent isolation, gVisor
- [Dapr Agents v1.0 release](https://www.diagrid.io/blog/dapr-agents-1-0-durable-cloud-native-production-ready) — durable multi-agent workflows
- [LangGraph + LangSmith self-hosted deployment](https://docs.langchain.com/langsmith/self-hosted)
- [Temporal for AI](https://temporal.io/solutions/ai) — durable execution model

### Comparable systems (agentic coding orchestrators)
- [Composio Agent Orchestrator](https://github.com/ComposioHQ/agent-orchestrator) — parallel coding agents, git worktrees, PRs
- [9 open-source agent orchestrators for AI coding (2026)](https://www.augmentcode.com/tools/open-source-agent-orchestrators)
- [AI coding tools compared (Claude Code, OpenCode, Cursor, Aider) — 2026](https://www.requesty.ai/blog/agentic-coding-tools-compared-2026-claude-code-cursor-codex-aider)
- [Claude Code Auto Mode (InfoQ)](https://www.infoq.com/news/2026/05/anthropic-claude-code-auto-mode/)

### Standards and conventions
- [OpenInference semantic conventions](https://arize-ai.github.io/openinference/spec/) — AI span kinds, attribute schema
- [OpenTelemetry GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/) — generic LLM span conventions (still Experimental as of early 2026)

### Research and trend reports
- [Anthropic 2026 Agentic Coding Trends Report](https://resources.anthropic.com/hubfs/2026%20Agentic%20Coding%20Trends%20Report.pdf) — multi-agent coordination, long-running agents, HITL patterns
- [MAST — Multi-Agent System Failure Taxonomy](https://arxiv.org/abs/2503.13657) — 14 failure modes in multi-agent LLM systems (validates the importance of TS-16 strict failure semantics + D-10 cycle-as-bug)

### Kubernetes/controller best practices
- [Kubebuilder book — status subresource](https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html)
- [controller-runtime package docs](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [OpenShift Pipelines manual approval pattern](https://docs.redhat.com/en/documentation/red_hat_openshift_pipelines/1.15/html/creating_cicd_pipelines/using-manual-approval)

### TIDE-internal references (load-bearing for categorization decisions)
- `/Users/justinsearles/Projects/tide/README.md` — design spec (load-bearing for all feature rationale)
- `/Users/justinsearles/Projects/tide/.planning/PROJECT.md` — Active / Out of Scope (load-bearing for v1 in/out decisions)
- `/Users/justinsearles/Projects/tide/CLAUDE.md` — implementation discipline (load-bearing for anti-feature rationale)
- `~/.claude/get-shit-done/workflows/` — the reference paradigm TIDE generalizes (56 workflows, single-host, GSD subagent types)

---
*Feature research for: TIDE v1 — Kubernetes-native hierarchical agentic coding orchestrator*
*Researched: 2026-05-12*
