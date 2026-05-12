# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

The seed of **TIDE** (Topologically-Indexed Dependency Execution) — a Kubernetes-native orchestrator for hierarchical agentic coding work that should eventually run against any project/codebase and be open-sourced for use in other clusters.

Right now the repo contains exactly one artifact: `README.md` — the design spec that the implementation will be built against (it doubles as the project's public-facing README, fitting the eventual OSS posture). As code lands (orchestrator, CRDs/operator, planner & executor subagent harnesses, persistence layer, dispatch logic, observability), update this file with the commands, layout, and architectural notes a future Claude needs.

## The spec is load-bearing

Everything downstream — schemas, APIs, controller logic, persistence — should trace back to the paradigm doc. When designing or implementing, preserve these distinctions; they exist for reasons the doc argues for explicitly:

- **Five-level hierarchy**: Milestone → Phase → Plan → Task → Wave. Each level has its own artifact (`MILESTONE.md`, phase brief, `PLAN.md`, diff, execution schedule) and dependency model. Don't collapse levels or invent new ones; if a real implementation pressure pushes back on the hierarchy, update the spec first.
- **Two distinct DAGs**: the *Planning DAG* (which artifacts must exist before another can be authored — shallow, fans out wide) and the *Execution DAG* (which code must exist before another can be written — deeper, fans out narrow). Same Kahn-layered algorithm runs on both. APIs and CRDs should keep these typed apart, not unified into one "DAG" abstraction.
- **Waves are derived, not declared**. They are the output of layered Kahn on the task DAG. The orchestrator never accepts a wave list as input — only a DAG. Re-deriving on every plan edit is intentional (the spec calls this out: O(V+E), cheap, no stale-schedule caching).
- **Cycles are bugs, not runtime conditions**. Cycle detection happens at plan-validation time and a cyclic DAG refuses to run. Don't add "cycle recovery" features; reject and surface.
- **Failure semantics at wave boundaries** (spec §"Failure handling at wave boundaries") are specific: failed task → siblings in same wave continue (they were declared independent), dependents in later waves never dispatch, non-dependents in later waves dispatch in strict-by-default but halt in conservative profile. Keep this contract intact when implementing the executor.
- **Resumption state is minimal**: indegree map + completed-task set. If the persistence layer starts wanting to store the full schedule, that's a smell — re-derive instead.

## Vocabulary conventions

The water/tide metaphor is intentional and consistent — use it in code names, CRD names, log lines, and docs:

- Rising tide = planning wave fanning out across subagents
- Slack tide = review checkpoint between waves
- Tidal lock = phase whose dependencies have all resolved
- Tidepool = sub-DAG developed in parallel isolation
- TIDE pod = deployment unit running a TIDE orchestrator (the K8s pun is intentional and load-bearing)

Prefer extending the metaphor naturally over coining unrelated terms. If a name doesn't fit, prefer plain prose.

## Implementation guidance (as code lands)

Until the first cut of code exists, treat these as defaults to be confirmed or overridden by the user before committing to them:

- **Open-source target**: design APIs, CRDs, and config to be portable across clusters from day one. Avoid hard-coding to a single LLM provider, a single git host, or a single auth model — abstract behind interfaces.
- **Subagent dispatch is pluggable**. The spec is model-agnostic ("Opus for milestone synthesis, Haiku for mechanical task execution"). The dispatch interface should accept a model/profile selector per level rather than baking in a vendor.
- **Two parallelism budgets**: planner and executor pools are separately sized. Don't unify them into one worker pool — the spec argues planning fans out wide and execution fans out narrow, and that's a deliberate capacity split.
- **Artifacts are the source of truth**, not in-memory state. Every level boundary produces a reviewable file (`MILESTONE.md`, phase brief, `PLAN.md`, diff). Resumption reads from artifacts; the orchestrator's database is a cache/index, not the truth.
- **Human gates are configurable per level**. Approve-every-milestone-but-auto-pass-plans should be as easy to express as fully-autonomous or fully-supervised. Don't bake gate policy into the controller.

## Structural conventions in the spec document

When editing `README.md` (the spec) itself:

- Mermaid diagrams: nested `subgraph` containment for the planning graph; flat wave subgraphs with cross-wave edges for the execution graph. Match the existing style.
- Pseudocode uses Python-ish syntax with numbered-step comments (`# 1.`, `# 2.`).
- Worked examples follow pseudocode; the Kahn example walks the indegree map iteration-by-iteration. New algorithms get the same treatment.
- "Alternatives considered and rejected" is part of the doc's argumentative shape — when proposing a design choice, include the rejected alternatives, not just the winner.
- Voice is tight, declarative, em-dash-heavy. Match it rather than reverting to hedged corporate prose.

## What this file should grow into

As real code arrives, add (and keep current):

- Build/test/lint commands, including how to run a single test
- The top-level layout (orchestrator vs CRDs vs subagent harnesses vs CLI) and the boundary between them
- Cluster-local dev loop (kind/minikube setup, how to deploy CRDs, how to tail orchestrator logs)
- Anything cross-cutting that requires reading multiple files to understand (controller reconcile flow, persistence schema, dispatch path)

Avoid re-stating things obvious from the code itself or from a typical Go/K8s project structure.

<!-- GSD:project-start source:PROJECT.md -->
## Project

**TIDE — Topologically-Indexed Dependency Execution**

A Kubernetes-native orchestrator that runs hierarchical agentic coding work as a topologically-sorted DAG of subagent dispatches. A human applies a `Project` CRD (outcome prompt + target repo + creds); TIDE authors `MILESTONE.md`, phase briefs, `PLAN.md` files, and task diffs by dispatching specialist subagents at each level, parallelizing across waves derived from the declared task DAG. Built to be open-sourced and portable across clusters from day one.

**Core Value:** **The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.** If everything else fails, TIDE-on-TIDE must work — that's what proves the paradigm and the implementation simultaneously, and it's the bar for "v1 ships."

### Constraints

- **Tech stack**: Go + sigs.k8s.io/controller-runtime + kubebuilder — K8s ecosystem default, idiomatic for CRDs/controllers, best contributor pool.
- **Tech stack**: Pluggable subagent runtime via a documented container image contract — never hard-coded to Anthropic SDK; v1 ships with a Claude-backed concrete impl behind the interface.
- **Distribution**: Apache 2.0, Helm chart from v1, designed for installation in arbitrary clusters with no hidden host dependencies.
- **Portability**: No hard-coded git host (GitHub, GitLab, Gitea must all work behind a generic git remote), no hard-coded LLM provider, no hard-coded auth model — abstract behind interfaces.
- **Persistence**: CRD `.status` only for v1 — no external DB, no SQLite. Per-object size stays well under etcd's 1.5 MiB hard limit by keeping per-Task CRDs small and label-indexed.
- **Failure semantics**: Wave boundary contract from spec §"Failure handling at wave boundaries" must be preserved exactly — failed task → siblings continue, dependents in later waves never dispatch, non-dependents dispatch in strict profile. Resumption state = indegree map + completed-task set, nothing more.
- **Resumability**: Long-running agentic work outlives single context windows. Every level boundary is a saved artifact; a fresh orchestrator restart re-derives waves from the task DAG + completed-task set in O(V+E).
- **Observability**: OpenTelemetry tracing must use OpenInference conventions for LLM/agent spans so traces are queryable in standard AI observability tools (Phoenix, LangSmith, Arize) without bespoke instrumentation.
<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->
## Technology Stack

## Recommended Stack
### Core Technologies
| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| **Go** | **1.26** (toolchain ≥ 1.25) | Operator implementation language | controller-runtime main targets `go 1.26.0` in its go.mod. Anthropic Go SDK requires Go 1.23+. Pinning to 1.26 lines up with the latest controller-runtime release line and avoids a second toolchain. |
| **sigs.k8s.io/controller-runtime** | **v0.24.x** (currently v0.24.1; pair with kubebuilder-emitted version) | Manager, reconciler, cache, client, leader election, webhook server, metrics endpoint | The K8s ecosystem default; every K8s-sig operator (cluster-api, capi-providers, kueue, gateway-api) is built on it. v0.24 targets k8s.io/api v1.36 and ships the generics-based webhook builder. Note: v0.23 introduced the breaking `WebhookManagedBy(mgr, &T{})` two-arg form — if you scaffold with kubebuilder v4.14 you inherit this automatically. |
| **sigs.k8s.io/kubebuilder** | **v4.14.0** (April 2026) | Scaffolding tool — CRD types, Go controller skeleton, RBAC, Kustomize manifests, Dockerfile, Makefile, envtest harness | Default scaffolder for new K8s controllers. Pairs with controller-runtime v0.23.3 in v4.14.0; expect bump to v0.24.x in the next 4.x release. Scaffolds Ginkgo + envtest suite, generates CRD manifests from kubebuilder markers (including CEL `x-kubernetes-validations`), generates Kustomize overlays, integrates `controller-gen` for deepcopy/CRD/RBAC generation. |
| **Kubernetes** | target **v1.33+** (v1.36 supported via controller-runtime v0.24) | Runtime platform | v1.29+ is required to use full CEL validation rules in CRDs (GA in 1.29). Pin minimum at 1.33 to stay on a supported upstream release at v1 ship. kind defaults to v1.35 node images. |
| **Anthropic Go SDK** (`github.com/anthropics/anthropic-sdk-go`) | **v1.42.0** (May 11, 2026) | Direct Anthropic API client for the executor subagent harness; reference impl behind the Subagent interface | Officially supported by Anthropic, Stainless-generated, production-stable (v1.42 has 72 releases). Supports Messages, tool use, streaming, batches, files, MCP helpers, betaagent/toolrunner packages, structured outputs, beta memory store. Go 1.23+ minimum. Pin to a minor; the SDK rev-bumps weekly with new beta surfaces, so use `~v1.42.0` not `*`. |
| **Claude Code CLI** | **v2.1.139+** (running inside the executor container image) | Pluggable in-container agent runtime — the v1 concrete Subagent impl | `claude -p "..." --output-format stream-json` runs headless with `ANTHROPIC_API_KEY` env auth (no OAuth flow). stdin/stdout works (with the >10 MB stdin fix in v2.1.128). Spec-aligned with "Pod-per-task Job + result envelope on PVC" — Claude Code's stream-json mode produces parseable events the orchestrator can capture as the typed result envelope. Authenticate via `ANTHROPIC_API_KEY` from the Project CRD's Secret reference. |
| **Helm 3** | latest stable | Distribution format for the operator (CRDs + RBAC + Deployment + ServiceMonitor) | K8s ecosystem default for installable bundles. The CNCF operator ecosystem ships Helm charts as the lingua franca; Kustomize-only excludes Argo CD / Flux users who template-render against Helm. See "Distribution" pattern below for the kubebuilder→Helm workflow. |
| **Kustomize** | bundled with kubebuilder | Internal dev manifests + base for `helmify` conversion | Kubebuilder scaffolds Kustomize natively; we keep it as the dev-loop format (`make deploy`) and generate the Helm chart from it via `helmify` at release time. Two artifacts, one source of truth. |
### Supporting Libraries
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **github.com/onsi/ginkgo/v2** | **v2.28.x** | BDD-style test suite runner | Default kubebuilder scaffold. Use for controller integration tests with envtest (`Describe/Context/It` blocks around reconcile-loop assertions). The previous proposal to remove Ginkgo from kubebuilder defaults was rejected — pure stdlib `testing` is awkward for K8s controllers because every test wants async polling against the API server, which `Eventually(...).Should(Succeed())` expresses cleanly and `for-loop + t.Fatal` does not. |
| **github.com/onsi/gomega** | latest (paired with Ginkgo v2) | Matcher library for envtest assertions | `Eventually`, `Consistently`, `WithTransform` are the load-bearing helpers when asserting reconciliation results that arrive asynchronously. |
| **sigs.k8s.io/controller-runtime/pkg/envtest** | from controller-runtime | Spin up etcd + kube-apiserver locally for tests | Standard kubebuilder-scaffolded harness. Faster than kind for unit/integration; reserve kind for E2E (real kubelet, real pods, real Jobs). |
| **sigs.k8s.io/kind** | **v0.31.0** (Dec 2024 release; latest available May 2026) | Local K8s-in-Docker for E2E + self-hosting demo | Required for the v1 self-hosting demo (TIDE-on-TIDE) — envtest can't run actual Jobs because there's no kubelet. v0.31 supports K8s 1.31 through 1.35. Pin a node image by `@sha256` digest in the demo script for reproducibility. |
| **github.com/go-logr/logr** | **v1.4.x** | Logging interface used by controller-runtime | controller-runtime exposes loggers as `logr.Logger`; you can't avoid this dependency. Treat it as a façade — the *backend* is the choice (zap vs. slog). |
| **go.uber.org/zap** | **v1.28.x** (April 2026) | Structured JSON logging backend behind logr | Operator-sdk and kubebuilder scaffolds default to zap-behind-logr. ~3× faster than slog for the field-heavy "reconciled X" log shape operators emit. APIs are frozen — `1.x` is in long-term stable. slog is *fine* but you'd be choosing standard-library aesthetics over a measurable hot-path speedup that matters when an operator logs every reconcile. |
| **github.com/prometheus/client_golang** | **v1.23.x** | Operator metrics: waves dispatched, tasks completed, dispatch latency histogram, failure rate counter | Official Prometheus instrumentation library. Already a transitive dep of controller-runtime (the manager exposes `/metrics` on port 8080 by default using a global registry from `controller-runtime/pkg/metrics`). Register custom collectors against the same global registry — no separate HTTP server needed. Go 1.23+ minimum. |
| **go.opentelemetry.io/otel** | **v1.43.0** (April 2025; trace API stable, metrics API still v0.65) | Distributed tracing for the Milestone→Phase→Plan→Task subagent chain | Tracing is stable (v1.x). Metrics is `v0.65.x` — usable in production but the API can shift. Use OTel for *tracing* (the spec calls this out explicitly), keep metrics on `client_golang` until the OTel metrics SDK reaches v1. Go 1.25+ required (v1.41 was the last to support Go 1.24). |
| **go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc** | matched to otel/v1.43 | OTLP/gRPC trace exporter | Standard OTLP path. Configurable entirely via `OTEL_EXPORTER_OTLP_ENDPOINT` etc. env vars set on the operator Deployment — no Go config code required for the deployer. |
| **OpenInference semantic conventions** | spec only; **no Go SDK** | LLM/agent span attribute names (`llm.input_messages`, `llm.token_count.prompt`, agent control-flow indicators) | **Important:** OpenInference has Python, TypeScript, and Java instrumentation libraries but **no official Go SDK** in 2026. The conventions are deliberately language-agnostic — you implement them by emitting OTel span attributes with the OpenInference-spec key names manually. Wrap this in a small internal package (`pkg/otelai`) so spans Phoenix/LangSmith/Arize will pick up without bespoke instrumentation. See "What NOT to Use" for why this is the right tradeoff vs. switching to the official OTel GenAI semconv. |
| **github.com/go-chi/chi/v5** | **v5.x** | HTTP router for the read-only dashboard backend served from the operator binary | Standard `net/http`-compatible router (no framework lock-in), works directly with `controller-runtime`'s manager-runnable abstraction (`mgr.Add(&dashboardRunnable{})`). Brings in zero opinions about middleware, validation, or JSON shapes — matches the operator's "this is a long-running process, not a web app" character. Gin/Echo would be overkill for the dashboard's narrow surface (5–10 endpoints + SSE stream). |
| **github.com/go-git/go-git/v5** | **v5.x** | Push artifacts (MILESTONE.md, PLAN.md, diffs) to the target repo at level boundaries from the controller | Pure-Go git impl — no `exec.Command("git", ...)` dependency on system git, no `/bin/git` in the operator image. Works over HTTPS with PAT and SSH with PrivateKey; both auth methods are configurable from the Project CRD's referenced Secret. Caveat: SSH host-key handling is fussy (well-documented gotcha — see PITFALLS); HTTPS-with-token is the smoother path. Pinning v5 (not the deprecated `src-d/go-git`). |
| **k8s.io/client-go** | **matched to controller-runtime's pinned version** (k8s.io/api v0.36 for cr v0.24) | Underlying typed K8s client | Transitive dep; never pin independently. Always let controller-runtime's `go.mod` dictate the k8s.io/* versions to keep `Scheme`, RESTMapper, and CRD types consistent. |
### Frontend (Read-Only Dashboard)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **React 18+** with **TypeScript** | latest stable | Dashboard SPA | Default ecosystem choice for K8s tooling dashboards (Argo, Headlamp, K9s-web). Most likely contributor pool already knows it. Pairs with React Flow naturally. |
| **@xyflow/react** (React Flow v12) | **v12.x** (xyflow umbrella project; @xyflow/svelte at 1.5.2 as of March 2026 means the React side is on the same release cadence) | Live-rendered Planning DAG + Execution DAG with per-task status badges, wave grouping, click-through to artifacts | Best-in-class for *interactive*, customizable node-based UIs in React. Nodes are real DOM elements (not Canvas) — every node can be a real React component with state, badges, log-tail tooltips, animations. Trivially live-updates from SSE: state-driven, just replace `nodes`/`edges` arrays and React Flow diffs the SVG. Strictly superior to Cytoscape.js for the "live status DAG" use case because Cytoscape's canvas-based rendering makes per-node custom UI a pain. |
| **dagre** (via React Flow's dagre example) | **v0.8.x** | Auto-layout DAG nodes for left-to-right wave layout | React Flow doesn't ship a layout — dagre is the standard companion. Good enough for the wave-shape graphs TIDE renders (≤100 nodes per phase typically). For larger graphs you'd reach for elkjs; v1 doesn't need it. |
| **Tailwind CSS v4** | latest stable | Styling | K8s tooling default in 2026; pairs with React/shadcn cleanly. No design system overhead. |
| **Server-Sent Events (SSE)** via `chi`'s standard `http.Flusher` | (no library; stdlib + chi) | Push live wave/task status + streaming kubectl log lines to the SPA | The dashboard is **uni-directional** (server → client, read-only spec) so SSE is the canonical answer over WebSockets. Works through proxies/ingress without protocol upgrade. The operator opens an SSE handler per dashboard connection; informer events on Task CRD status changes get pushed straight through. |
### Development Tools
| Tool | Purpose | Notes |
|------|---------|-------|
| **controller-gen** (sigs.k8s.io/controller-tools) | Generates `zz_generated_deepcopy.go`, CRD manifests, RBAC YAML, webhook configs from Go type markers | Bundled with kubebuilder; pinned in the project Makefile. |
| **setup-envtest** (sigs.k8s.io/controller-runtime/tools/setup-envtest) | Downloads etcd + kube-apiserver binaries for envtest | Scaffolded by kubebuilder; `make test` invokes it. |
| **golangci-lint** | Lint pipeline | The kubebuilder scaffold ships a default `.golangci.yml`. Enable `gosec`, `errcheck`, `staticcheck` at minimum. |
| **helmify** | Convert kubebuilder's Kustomize output to a Helm chart | The CNCF-blessed path for "kubebuilder scaffold + Helm distribution." Run at release time, commit the chart, publish via `helm push` to an OCI registry (GHCR works). |
| **Mermaid (in `README.md`)** | Spec diagrams | The README already uses Mermaid; don't replace it. Dashboard ≠ spec doc; the live DAG view in the browser uses React Flow, the static design diagrams stay in Mermaid in markdown. |
## Installation
# Scaffold the project (do this once; do not redo)
# Core Go deps (added to go.mod by kubebuilder; pin these in go.mod after init)
# Test deps (also added by kubebuilder)
# Frontend (in web/ or dashboard/)
# Helmify (release-time tool, not a runtime dep)
# kind (for local E2E)
## Alternatives Considered
| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| **controller-runtime + kubebuilder** | **operator-sdk** | If you need Operator Lifecycle Manager (OLM) integration or want to publish to OperatorHub.io with one command. Operator-sdk wraps kubebuilder and adds OLM + scorecard + Helm/Ansible operator types. For an OSS-from-day-one project with its own Helm chart and no OLM dependency, the wrapper costs more than it adds — and you can always layer OLM bundling on later from a kubebuilder-scaffolded codebase. |
| **controller-runtime + kubebuilder** | **Hand-rolled `client-go` + informers** | Never for v1. Hand-rolled informers + workqueue is what controller-runtime abstracts; doing it yourself is the K8s equivalent of writing assembly to avoid Go. Reasonable for a 2030 v4 rewrite if controller-runtime's manager grows opinions you outgrow. Not now. |
| **Native K8s Jobs (one Job per task)** | **Argo Workflows** | Argo would handle the wave DAG itself — but TIDE's spec is explicit that **waves are derived from the task DAG by the orchestrator running layered Kahn**, not declared as Workflow.spec.templates. Putting Argo underneath would mean either (a) translating the Task CRDs into Workflow specs on every reconcile (lossy, dual-source-of-truth, defeats the spec's "DAG is source of truth, schedule is derived" principle) or (b) abandoning the spec and accepting Argo's DAG semantics. Native Jobs let the controller own the DAG cleanly: it watches Task CRDs, runs Kahn in-process, creates Jobs at wave boundaries, watches Job status, advances. Reach for Argo only if you find yourself reimplementing artifact passing, retries, parameter templating — by which point the spec has drifted from K8s-native-orchestrator to "thin wrapper over Argo," and we'd update the spec first. |
| **Native K8s Jobs** | **Tekton** | Tekton is CI/CD-shaped (Tasks → Pipelines, declarative pipeline-as-code). It encodes its own DAG, same objection as Argo, plus Tekton is even more CI-specific. Skip. |
| **Anthropic Go SDK + Claude Code CLI (executor side)** | **OpenAI Go SDK / LangChainGo / generic LLM wrapper** | Behind the Subagent interface, swap concrete impls when needed. v1 only ships the Anthropic-backed one. Don't hide everything behind an internal abstraction prematurely — define the interface (`Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)`) at the seam between the controller and the in-pod runtime, and let the concrete impl be Anthropic-specific until there's a second one. |
| **Gonum / dominikbraun/graph for layered Kahn** | **stdlib only** | Layered Kahn is 30 lines of Go over a `map[TaskID]int` (indegrees) and a `map[TaskID][]TaskID` (adjacency). Pulling in a graph library would obscure the algorithm the *entire paradigm* is built around — and the spec walks through Kahn iteration-by-iteration as load-bearing exposition. **Implement in stdlib.** Gonum (v0.17.0, Jan 2026, actively maintained) and dominikbraun/graph (v0.23.0, July 2024, possibly stale) are both real options if you needed cycle-detection, all-paths-between, or non-trivial algorithms — TIDE doesn't. |
| **chi router** | **gin / echo / fiber** | gin/echo bring opinionated middleware stacks and JSON binding — wasted bytes for a 5-endpoint read-only dashboard. fiber uses fasthttp which doesn't compose with `controller-runtime`'s `manager.Runnable` interface (manager expects `net/http`-shaped runnables). chi *is* `net/http` with routing on top. |
| **React Flow** | **Cytoscape.js + cytoscape-dagre** | If you need really large graphs (1000+ nodes) or canvas-rendering performance is the binding constraint. For TIDE's wave-shaped graphs (typically ≤100 nodes per phase, ≤500 total in a v1 run) React Flow is faster to build against and the DOM-node-per-task model lets you put real React components (status badges, log-tail tooltips, kubectl-logs streaming dropdowns) on every node trivially. Cytoscape would force you back into a render-loop-with-extensions mindset. |
| **React Flow** | **htmx + SSE + Mermaid live rerender** | If you want a "no frontend build, no node_modules, ship a single Go binary" posture. htmx + server-rendered Mermaid is genuinely defensible for the dashboard's narrow scope. The tradeoff is interactivity — DAG zoom/pan, click-into-task-pod-logs, real-time wave-progress animations are awkward in htmx. For v1, recommend React Flow; if a contributor strongly prefers htmx-shaped operator tooling, the alternative is real and supported. |
| **slog (stdlib)** | **zap** | zap wins by ~3× in the field-heavy hot path; operator-sdk and kubebuilder both default to zap-behind-logr; the K8s community defaults to zap. slog is the right choice for application services that log a few hundred lines per request — TIDE's reconcile loop runs constantly and logs every transition. |
| **CEL validation rules in CRDs (x-kubernetes-validations)** | **Validating admission webhook** | CEL went GA in Kubernetes 1.29 for CRDs. Inline `+kubebuilder:validation:XValidation:rule=...` markers in the Go types produce in-process API-server validation — no webhook deployment, no cert management, no latency hop. Reserve admission webhooks for *mutation* (defaulting) and cross-resource validation that CEL's single-object scope can't express. TIDE's CRD validation (no cycles in declared edges, wave count ≤ N, etc.) fits CEL almost entirely. |
| **Helm chart distribution** | **OLM bundle / OperatorHub.io** | Adds OLM-cluster prerequisite on the install path. Cuts out users running plain K8s clusters without OLM (most kind/k3s/EKS/GKE/AKS clusters). v1 ships Helm; OLM bundle is a "if we want this in Red Hat Marketplace later" addition. |
## What NOT to Use
| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **External database (Postgres / MySQL) or SQLite** | Spec explicitly out-of-scope. CRD `.status` is technically sufficient at v1 scale (one human watching one run). Adding a DB introduces a second source of truth, schema migrations, backup/restore, and a dependency that breaks the "install this Helm chart and run" story. | Per-Task CRD with a small `.status` block + label-selector queries. In-process indegree map cache, rederivable from the completed-task set. |
| **gRPC streaming subagent protocol** | Spec out-of-scope for v1. The Job-per-task contract (pod-per-task + result envelope on PVC + exit code) is enough. A gRPC streaming sidecar can land in v2 behind the same `Subagent` interface without redesign. | `Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` interface; concrete impl `claudeCodeJobRunner` that creates a Job, mounts the artifact PVC, sets env vars, watches Job completion, reads the envelope file off the PVC. |
| **Critical-path / HEFT / heterogeneous-resource schedulers** | Spec explicitly rejects these. LLM task durations are high-variance and unknowable up-front — CPM degenerates to Kahn-layered anyway. HEFT is premature optimization at the paradigm layer. | **Layered Kahn, in stdlib, 30 lines.** If subagent pools become heterogeneous (Opus for milestones, Haiku for tasks), add a wave-internal sub-scheduler behind Kahn — do not replace Kahn. |
| **Cycle "recovery" / auto-resolve features** | Cycles are bugs in the declared plan, not runtime conditions. The spec is firm: refuse to start a run on a cyclic DAG. | Validate the DAG in the admission CEL rule (or a webhook if CEL is insufficient — the all-paths cycle test may not fit CEL cleanly; fall back to a webhook just for this if needed). Refuse with a clear error message pointing at the cycle. |
| **Wave list as CRD input** | Spec is explicit: orchestrator never accepts a wave list as input — only a DAG. Re-deriving on every plan edit is intentional. | Tasks declare `dependsOn: [taskRef, taskRef]`. The controller computes waves at reconcile time. |
| **Storing the full schedule in `.status`** | Same reason. Resumption state is minimal: indegree map + completed-task set. | Status fields: `completedTasks: []string`, `failedTasks: []string`, optionally `currentWave: int`. Re-derive the wave schedule from the live DAG + completedTasks set on every reconcile. |
| **Hard-coding to one LLM provider in the controller** | Spec explicit constraint. Anthropic-first concrete impl, but provider-agnostic by construction. | All Anthropic-specific code lives behind the `Subagent` interface in `internal/subagent/anthropic/`. The reconciler imports the interface, not the impl. A future `internal/subagent/openai/` slots in without changing controller code. |
| **Vendoring `get-shit-done` Markdown into TIDE** | Spec out-of-scope. GSD is the *paradigm* reference, not a vendored workflow library. Markdown workflows would lock TIDE to GSD's bootstrap host. | Re-implement the planner/executor prompts in Go (compiled-in templates, configurable per level via the Project CRD). GSD informs the *shape* of the prompts; the actual prompts live in the Go binary. |
| **External Secrets Operator first-class integration** | Spec out-of-scope. ESO docs/examples can land later without changing the CRD contract. | Plain K8s `Secret` references on the Project CRD (`spec.secretRefs.anthropicAPIKey`, `spec.secretRefs.gitCredentials`). ESO users sync into a plain Secret and TIDE doesn't know the difference. |
| **OAuth authentication for Claude Code in-container** | Known broken in headless/container environments (claude-code#29983, #7100) — the OAuth redirect URL is rejected by the server when the browser can't reach the device. | `ANTHROPIC_API_KEY` env var from the Project CRD's Secret ref. Mount as `EnvFrom: secretRef`. Documented headless path. |
| **Mounting `~/.claude/` from the host into containers** | Known anti-pattern — the container writes credentials into the host's real `~/.claude/` and exfiltrates state. | Each Job gets its own fresh container filesystem (no host mount); `ANTHROPIC_API_KEY` env var; artifact PVC is the only shared filesystem and it lives in the cluster, not the host. |
| **Switching to the official OpenTelemetry GenAI semconv instead of OpenInference** | OTel GenAI semconv is still in "Development" status as of 2026 (per the spec page); OpenInference is the de-facto convention that Phoenix / LangSmith / Arize *actually consume* today. Switching when GenAI semconv stabilizes is a future migration; building against an unstable convention now is volunteering for churn. | Emit OpenInference attribute names (`llm.input_messages`, `llm.token_count.prompt`, etc.) on OTel spans via the standard otel-go SDK. Plan for a dual-emission shim when GenAI semconv goes stable. |
| **gin / echo / fiber for the dashboard backend** | Brings opinionated middleware stacks and JSON binding the dashboard doesn't need; fiber's fasthttp isn't `net/http`-compatible (won't compose with `manager.Add`). | `github.com/go-chi/chi/v5` registered as a `manager.Runnable` on the controller-runtime manager. |
| **WebSockets for live dashboard updates** | Bidirectional, requires HTTP-upgrade through ingress, more moving parts. Dashboard is read-only by spec, so the upstream channel is unused. | Server-Sent Events (SSE). Uni-directional, plain HTTP, proxy-friendly, htmx-shaped semantics even though we're using React. |
| **dominikbraun/graph** for the wave algorithm | Last release v0.23.0 (July 2024); unclear 2026 maintenance status; overkill for what's 30 lines of stdlib. The spec's argumentative weight rests on layered Kahn being trivially inspectable — wrapping it in a third-party library obscures that. | Write `computeWaves(tasks []Task, edges []Edge) ([][]TaskID, error)` in stdlib. Unit test exhaustively. The spec's worked example is the test case. |
## Stack Patterns by Variant
- Anthropic Go SDK as the v1 concrete Subagent impl behind the interface
- Claude Code CLI inside the executor container image (one Go binary calls `claude -p ... --output-format stream-json`)
- CRD `.status` only — no DB
- Helm chart distribution + Kustomize internal dev loop
- React Flow + dagre dashboard
- Native K8s Jobs, not Argo
- envtest + Ginkgo for unit/integration; kind for the TIDE-on-TIDE E2E
- Add `internal/subagent/openai/` (or wherever) behind the same `Subagent` interface
- Project CRD gains a `spec.subagentProfile: <profile-name>` selector
- No controller changes — the dispatcher picks impl by profile
- First sign: dashboard query shapes need joins / aggregations that label-selector queries can't express
- Add a read-side projection (e.g., Postgres) populated by an informer on the orchestrator; controller still writes only to CRD `.status`
- CRDs remain the source of truth; DB is the cache, per spec
- Add a wave-internal sub-scheduler behind layered Kahn (one wave splits into sub-batches dispatched serially within the wave)
- Do **not** replace Kahn with CPM/HEFT — spec rejects this
- The operator's `/metrics` endpoint is always there (controller-runtime default port 8080)
- The Helm chart's `ServiceMonitor` is gated by `prometheus.enabled` value; default false to avoid CRD-not-found errors on plain clusters
## Version Compatibility
| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `sigs.k8s.io/controller-runtime@v0.24.x` | `k8s.io/api@v0.36.x`, `k8s.io/apimachinery@v0.36.x`, `k8s.io/client-go@v0.36.x` | Pinned together — never bump k8s.io/* independently |
| `sigs.k8s.io/kubebuilder@v4.14.0` | `controller-runtime@v0.23.3` (current); next 4.x release expected to bump to v0.24.x | Kubebuilder scaffolds with a specific cr version; either accept the scaffold version or upgrade in-place after scaffolding |
| `github.com/anthropics/anthropic-sdk-go@v1.42.x` | Go 1.23+ | The SDK rev-bumps weekly with new beta surfaces; pin to a minor (`v1.42.x`), not `latest` |
| `go.opentelemetry.io/otel@v1.43.x` (trace) | `go.opentelemetry.io/otel/metric@v0.65.x`, `go.opentelemetry.io/otel/log@v0.19.x` | Trace API stable (v1.x); metric and log APIs not yet GA (v0.x). Don't conflate the version trains |
| `github.com/onsi/ginkgo/v2@v2.28.x` | `github.com/onsi/gomega@latest` | Always bump together |
| Kubernetes 1.29+ | CEL CRD validation (`x-kubernetes-validations`) | GA in 1.29; only relevant if you support clusters older than 1.29, which you shouldn't |
| `sigs.k8s.io/kind@v0.31.0` | Kubernetes 1.31 through 1.35 (node images) | Pin node image by `@sha256` in E2E scripts |
| Go 1.26 | controller-runtime v0.24, Anthropic SDK v1.42, prometheus/client_golang v1.23, otel v1.43 | Standardize on a single Go toolchain version in `go.mod`'s `toolchain` directive |
## Sources
- **controller-runtime releases (verified May 2026)** — https://github.com/kubernetes-sigs/controller-runtime/releases — v0.24.1 latest, k8s.io/* v1.36 targeted, breaking change in v0.23 for `WebhookManagedBy` two-arg form. HIGH confidence.
- **controller-runtime go.mod (main)** — https://github.com/kubernetes-sigs/controller-runtime/blob/main/go.mod — `go 1.26.0`, `k8s.io/api v0.36.0`. HIGH confidence.
- **kubebuilder releases (verified May 2026)** — https://github.com/kubernetes-sigs/kubebuilder/releases — v4.14.0 (April 30, 2026), pairs with controller-runtime v0.23.3, scaffolding requires Go 1.25.7. HIGH confidence.
- **Kubebuilder Book — Writing tests** — https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html — Ginkgo + envtest scaffolded by default; standard testing rejected for controller suites because async polling is awkward in stdlib. HIGH confidence.
- **Kubebuilder Book — Configuring EnvTest** — https://book.kubebuilder.io/reference/envtest.html — etcd + kube-apiserver, no kubelet; binaries downloaded via `setup-envtest`. HIGH confidence.
- **Kubebuilder Book — Metrics** — https://book.kubebuilder.io/reference/metrics.html — controller-runtime exposes `/metrics` via global Prometheus registry; ServiceMonitor template gated. HIGH confidence.
- **Anthropic Go SDK releases (verified May 2026)** — https://github.com/anthropics/anthropic-sdk-go/releases — v1.42.0 May 11, 2026; production-stable; Stainless-generated; weekly cadence. HIGH confidence.
- **Anthropic Go SDK go.mod** — https://github.com/anthropics/anthropic-sdk-go/blob/main/go.mod — `go 1.23.0`, `toolchain go1.25.8`. HIGH confidence.
- **Anthropic Go SDK README** — https://github.com/anthropics/anthropic-sdk-go — Messages, tool use, streaming, batches, files, betaagent/toolrunner, MCP, memory store, structured outputs. HIGH confidence.
- **Claude Code releases (verified May 2026)** — https://github.com/anthropics/claude-code/releases — v2.1.139 latest, headless `-p` flag, `--output-format stream-json`, stdin piping (with >10 MB fix in v2.1.128). HIGH confidence.
- **Claude Code headless containers** — https://amux.io/guides/claude-code-headless/ and https://docs.docker.com/ai/sandboxes/agents/claude-code/ — `ANTHROPIC_API_KEY` env var bypasses OAuth; never mount host `~/.claude/`. MEDIUM confidence (multiple sources agree on the env-var pattern; the specific mount anti-pattern is documented in community guides). The OAuth-in-headless brokenness is logged in claude-code GitHub issues #29983 and #7100. HIGH confidence on the brokenness.
- **OpenTelemetry Go releases** — https://github.com/open-telemetry/opentelemetry-go/releases — v1.43.0 (April 2025); trace stable, metrics v0.65 still pre-GA; Go 1.25+ required. HIGH confidence.
- **OpenTelemetry GenAI semconv** — https://opentelemetry.io/docs/specs/semconv/gen-ai/ — "Development" status as of 2026; `OTEL_SEMCONV_STABILITY_OPT_IN` flag required. HIGH confidence on status.
- **OpenInference repository** — https://github.com/Arize-ai/openinference — Python (68%), TypeScript (24%), Java (5.6%); **no Go SDK**; conventions are language-agnostic. HIGH confidence.
- **OpenInference semantic conventions** — https://arize-ai.github.io/openinference/spec/semantic_conventions.html — every OpenInference trace is a valid OTLP trace; attribute namespacing (`llm.*`, agent control-flow attrs). HIGH confidence.
- **prometheus/client_golang releases** — https://github.com/prometheus/client_golang/releases — v1.23.2 (September 2025); Go 1.23+ required from v1.23.0. HIGH confidence.
- **kind releases** — https://github.com/kubernetes-sigs/kind/releases — v0.31.0 (December 2024); supports K8s 1.31–1.35. HIGH confidence.
- **Ginkgo releases** — https://github.com/onsi/ginkgo/releases — v2.28.3 (April 2026); actively maintained. HIGH confidence.
- **zap v1.28.0** — https://github.com/uber-go/zap — released April 2026; ~3× faster than slog for field-heavy logging; 1.x stable. HIGH confidence.
- **logr v1.4.3** — https://github.com/go-logr/logr — used by controller-runtime; interoperates with slog via `FromSlogHandler`/`ToSlogHandler`. HIGH confidence.
- **Argo Workflows** — https://github.com/argoproj/argo-workflows — v4.0.5 (April 2026); CNCF graduated; per-task pod model; DAG-shaped. HIGH confidence on existence and shape, MEDIUM confidence on the specific Argo-vs-Jobs tradeoff for TIDE's spec constraints (the recommendation here is a synthesis based on the spec, not direct Argo benchmarks).
- **Gonum** — https://github.com/gonum/gonum — v0.17.0 (January 2026); `topo` package supports topological sort. HIGH confidence on existence and active maintenance.
- **dominikbraun/graph** — https://github.com/dominikbraun/graph/releases — v0.23.0 (July 2024); 2026 maintenance status unclear. MEDIUM confidence on staleness.
- **React Flow / xyflow** — https://github.com/xyflow/xyflow — React Flow 12.x current; DOM-node-per-task; dagre is the standard companion layout. HIGH confidence on feature set; MEDIUM confidence on the specific live-update DAG suitability (synthesis from React Flow's design model).
- **Cytoscape.js** — https://github.com/cytoscape/cytoscape.js — v3.33.3 (April 2026); canvas-rendered. HIGH confidence on availability; the recommendation against it is based on customization-difficulty heuristics from the ecosystem comparison searches.
- **CEL validation for CRDs** — https://kubernetes.io/blog/2022/09/23/crd-validation-rules-beta/ and https://opensource.googleblog.com/2023/11/kubernetes-crd-validation-using-cel.html — GA in K8s 1.29 (full feature set including `messageExpression`, `optionalOldSelf`, transition rules). HIGH confidence.
- **Operator SDK FAQ** — https://sdk.operatorframework.io/docs/faqs/ — wraps kubebuilder, adds OLM + Helm operator types + scorecard. HIGH confidence.
- **helmify** — https://github.com/arttor/helmify — converts Kustomize output to Helm chart; the CNCF-blessed path for kubebuilder→Helm. MEDIUM confidence (it's the most widely cited tool but not the only option; a hand-maintained chart is also viable for a small operator).
- **go-git v5** — https://pkg.go.dev/github.com/go-git/go-git/v5 — pure-Go git impl; HTTP basic auth, SSH PublicKeys auth, both with documented gotchas (SSH host-key handling). HIGH confidence on capability; SSH-pain caveat is from multiple community sources.
- **chi router** — https://github.com/go-chi/chi — `net/http`-shaped router; composes with `manager.Runnable`. HIGH confidence.
- **htmx SSE extension** — https://htmx.org/extensions/sse/ — uni-directional, proxy-friendly. HIGH confidence on the protocol; the React Flow choice over htmx is taste-driven.
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd:quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd:debug` for investigation and bug fixing
- `/gsd:execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd:profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
