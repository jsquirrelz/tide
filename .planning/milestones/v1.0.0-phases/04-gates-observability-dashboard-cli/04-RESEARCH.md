---
phase: 04
phase_name: gates-observability-dashboard-cli
researched: 2026-05-16
domain: gates + structured-observability + cobra CLI + React-Flow read-only dashboard
overall_confidence: HIGH
upstream:
  - .planning/phases/04-gates-observability-dashboard-cli/04-CONTEXT.md (D-G1..G4, D-O1..O6, D-C1..C4, D-D1..D6, D-W1..W2, D-X1..X4)
  - .planning/REQUIREMENTS.md (GATE-01..03, OBS-01..06, CLI-01..04, DASH-01..05)
  - .planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-VERIFICATION.md (W-1, W-2, W-3)
  - .planning/research/STACK.md (pinned stack versions)
---

# Phase 04: Gates, Observability, Dashboard, CLI — Research

## Summary

Phase 4 is the broadest-surface phase in the TIDE roadmap: 18 REQ-IDs spanning four orthogonal but co-located surfaces (gates, observability, CLI, dashboard) plus two folded-in Phase 3 catch-ups (W-1 secret-leak counter, W-2 mid-stack push triggers). The good news is that nearly every domain is **prescribed by upstream CONTEXT.md decisions** — the research task is to validate that each prescription is implementable against current 2026 library shapes, not to explore alternatives.

**Architectural rule of thumb for the planner:** the four surfaces should be sequenced as **Gates → Observability → CLI → Dashboard**, NOT parallelized at the top level. Reasons: (a) Gates change reconciler bodies, which the observability layer instruments; (b) Observability defines the API the dashboard consumes; (c) CLI's `tide approve/reject/cancel/resume/inspect-wave` are the structural foundation the dashboard surfaces as deeplinks (D-D6). W-1/W-2 fold naturally into the Gates and Observability waves because they touch the same reconciler boundaries.

**Primary recommendation:** lock the architecture sketch below, treat all OpenInference attribute names as **verified against current spec** (May 2026), accept that no Go OpenInference SDK exists in 2026 so `pkg/otelai` is genuinely hand-rolled, and structure the plan around **6 waves of work** described in the Architecture Sketch.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Gate-policy evaluation | Orchestrator (Go controller-manager) | — | Reconcilers consult `Project.Spec.gates`; no client-side logic — annotation drives state |
| Gate approval mechanism | Annotation on CRD (declarative K8s API) | CLI/kubectl wrappers | One mental model; declarative K8s contract is the single seam |
| Structured logs | Orchestrator pod + subagent pods (zap-behind-logr) | — | Phase 1+2 already use `ctrl.Log`; Phase 4 enforces structured field set |
| Prometheus metrics | Orchestrator (controller-runtime `/metrics` endpoint) | — | Already wired in Phase 2 via `metrics.Registry.MustRegister` |
| OTel tracing | Orchestrator (manager `Runnable` registers TracerProvider) | OTLP gRPC exporter sidecar/external collector (optional) | SDK in orchestrator; tail-sampling at Collector deferred to v1.x |
| OpenInference attribute emission | Orchestrator's `pkg/otelai` (called from reconcilers + harness post-processor) | — | No subagent-pod OTel; spans created where the event-log artifact path is known |
| CLI command parsing | `cmd/tide` (cobra binary, client-side) | — | Stateless; reads kubeconfig; no server component |
| CLI K8s API access | client-go (in-process from `cmd/tide`) | — | Standard kubeconfig resolution chain (D-C1) |
| Dashboard frontend | Browser (React 18 + @xyflow/react v12 + Vite-built SPA, embed.FS-served) | — | DOM-node-per-task model needs real React components |
| Dashboard backend | `cmd/dashboard` (Go, manager.Runnable composition pattern) | — | controller-runtime informer cache + chi router + SSE handler |
| Dashboard auth | Dashboard's own read-only ServiceAccount (apiserver proxy) | — | Browser never touches apiserver; no tokens in browser |
| Pod-log streaming | Backend reads `pods/log` (HTTP chunked) → backend translates to SSE → browser | — | Pitfall 22 mitigation: backend owns lifecycle |

**Why this matters:** Two misassignments to avoid in plans: (1) putting OpenInference attribute emission in the subagent pod (subagent pods have zero K8s verbs per Phase 3 D-A4, and the structured event log already lives on the PVC — orchestrator emits spans with the artifact path on the read side); (2) putting WebSocket protocol speakers in the browser (DASH-03/04 explicitly chose SSE for proxy-friendliness and uni-directional read-only semantics).

## User Constraints (from CONTEXT.md)

### Locked Decisions (D-G1..G4, D-O1..O6, D-C1..C4, D-D1..D6, D-W1..W2, D-X1..X4)

Every decision in `.planning/phases/04-gates-observability-dashboard-cli/04-CONTEXT.md` is locked. Highlights the planner MUST honor verbatim:

**Gates:**
- D-G1: `Project.Spec.gates` only — no per-CRD overrides on Milestone/Phase/Plan/Task. Defaults: `{milestone: approve, phase: auto, plan: auto, task: auto, pauseBetweenWaves: false}`. CEL `enum: [auto, approve, pause]` per level. (Schema already exists in `api/v1alpha1/project_types.go` lines 47–64 — `GatePolicy` type + `Gates` struct — wired into `ProjectSpec.Gates` at line 257.)
- D-G2: On `approve`, reconciler sets child `Status.Phase=AwaitingApproval` + `WaveOrLevelPaused` Condition, halts dispatch. On `pause`, same but requires explicit `tide resume`. On `auto`, today's behavior.
- D-G3: Wave-pause unblock via annotation `tideproject.k8s/approve-wave-<N>` on the Plan CRD. `tide approve --wave <plan>/<N>` writes the annotation; `kubectl annotate` does the same.
- D-G4: `tide reject` writes `tideproject.k8s/reject: <reason>`; `tide resume` clears it; `tide cancel` is destructive + cascades + requires `--force`.

**Observability:**
- D-O1: zap-behind-logr (Phase 1+2 already use `ctrl.Log`). Structured field set: `project`, `phase`, `plan`, `task` — same labels as metrics for grep-correlation. No new logging library.
- D-O2: Prometheus via `client_golang` (already wired in `internal/budget/metrics.go`). Bounded labels: `{project, phase, plan}`. **No `task` label ever.** Counter set locked in CONTEXT (eight counters + one histogram).
- D-O3: OTel sampler default `ParentBased(TraceIDRatioBased(0.1))`, env-overridable via `OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG`. Collector optional.
- D-O4: `pkg/otelai` is THIN — five helper functions returning `[]attribute.KeyValue` slices. NO span-lifecycle DSL, NO Go OpenInference SDK (none exists 2026).
- D-O5: No LLM payloads in span attrs — only PVC artifact path reference.
- D-O6: ServiceMonitor template gated by `.Values.prometheus.serviceMonitor.enabled` (default `false`).

**CLI:**
- D-C1: cobra-based stateless client (no local cache). Reuses kubeconfig chain. NO `tide login`. Single binary `cmd/tide/`.
- D-C2: GitHub Releases (via goreleaser) + Krew plugin (`kubectl tide ...`). Container image is side-artifact only.
- D-C3: Ten verbs locked: `apply / watch / tail / approve / reject / cancel / resume / inspect-wave / artifact-get / describe-budget` (plus completion subcommand from cobra).
- D-C4: Human output default; `--output json` for machine-parseable. `text/tabwriter` for tabular output. No YAML output mode (kubectl already does that).

**Dashboard:**
- D-D1: Side-by-side vertical split — Planning DAG (dagre top-down) left, Execution DAG (dagre left-right with wave subgraph bands) right.
- D-D2: Read-only ServiceAccount + apiserver proxy via dashboard backend. Browser NEVER talks to apiserver directly. Four backend endpoints listed.
- D-D3: SSE source = controller-runtime informer cache → in-process pubsub hub → SSE handler. No DB, no Redis.
- D-D4: Pod log streaming via SSE (NOT WebSocket from browser). Backend translates `pods/log` HTTP chunked → SSE. 5-min idle close + client-disconnect-cleanup defer (Pitfall 22).
- D-D5: React 18 + TS + @xyflow/react v12 + dagre + Tailwind v4. Vite build. `go:embed` into `cmd/dashboard/` binary. <500KB gzipped target.
- D-D6: Read-only end-to-end. UI surfaces actions as **clipboard-copy of `tide` CLI commands** with confirmation toast — user runs the CLI.

**W-1 / W-2 (Phase 3 catch-up):**
- D-W1: Register `tide_secret_leak_blocked_total{project, phase, plan}` counter. ProjectReconciler reads push-Job envelope `reason="leak-detected"` on exit-10 (leak) vs exit-11 (lease). New Phase constant `PhasePushLeakBlocked`.
- D-W2: Mid-stack boundary triggers — `MilestoneReconciler` / `PhaseReconciler` / `PlanReconciler` each detect all-children-Succeeded and dispatch a `tide-push` Job with the correct D-B2 commit message. Couples naturally to D-G2 (same boundary-detection seam consults gate policy).

**Cross-cutting:**
- D-X1: Same label set across logs/metrics/traces (`project, phase, plan`).
- D-X2: Same go.mod. Separate `cmd/dashboard/` and `cmd/tide/` entrypoints. Frontend source in `dashboard/web/`; Vite build output copied to `cmd/dashboard/embed/`.
- D-X3: Three new Helm templates: `dashboard-deployment.yaml`, `dashboard-rbac.yaml`, `servicemonitor.yaml`. Gated by Helm values.
- D-X4: New `cmd/tide-lint` analyzer `metric-cardinality` — forbids any metric definition with `task` label.

### Claude's Discretion

From CONTEXT.md, the following are explicitly the researcher/planner's call (research recommendations follow each):

- **Push Job RBAC scope (carry-forward from Phase 3 D-Claude's-Discretion).** Already resolved in Phase 3 with dedicated `tide-push` SA in `charts/tide/templates/push-rbac.yaml`. No new Phase 4 work; recommend treating as settled.
- **Histogram bucket sizes.** `tide_dispatch_latency_seconds`: `[0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800]` (CONTEXT.md `<specifics>` already locked this; planner uses it verbatim).
- **Lint-rule encoding for D-X4 `metric-cardinality`.** Research recommends: AST-walk on `prometheus.New*Vec(opts, []string{...})` and `metrics.Registry.MustRegister` call sites. Reject any literal `"task"` in the labels slice. Pattern matches existing `tools/analyzers/providerfirewall/analyzer.go` shape.
- **`tide-lint` multichecker registration.** New analyzer slots in at `cmd/tide-lint/main.go:32` — single-line addition: `multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer, metriccardinality.Analyzer)`.
- **OTel exporter type.** OTLP gRPC (per STACK.md line 38). Recommend HTTP fallback NOT shipped in v1.0 — gRPC works against all major OTel collectors and Phoenix/LangSmith/Arize endpoints.
- **OTel "no-op" path when no endpoint is configured.** Research recommends: if `OTEL_EXPORTER_OTLP_ENDPOINT` is empty, instantiate a `tracenoop.NewTracerProvider()` (from `go.opentelemetry.io/otel/trace/noop`). All `tracer.Start(...)` calls become no-ops with no network traffic. Manager startup never fails for OTel reasons.
- **Dashboard `/healthz` definition.** 200 once informer cache `cache.WaitForCacheSync` returns. Helm readiness probe gates traffic.
- **CLI `--context` / `--namespace` flags.** Standard kubectl-aligned; default from kubeconfig; document namespace-of-Project behavior.

### Deferred Ideas (OUT OF SCOPE)

Copy verbatim from CONTEXT.md `<deferred>` — these items MUST NOT appear in plans:

- Per-CRD gate override fields on Milestone/Phase/Plan/Task
- `WaveApproval` / `PauseRequest` CRD
- Tabbed / stacked / inline-expand dashboard layouts
- Browser-direct apiserver auth (token paste UX)
- OIDC/SSO dashboard auth (v1.x via reverse-proxy pattern)
- Span-wrapping DSL for `pkg/otelai`
- OTel Collector tail-sampling as v1.0 default
- CLI container-image distribution as primary channel
- `tide login` flow
- CLI YAML output mode
- Dashboard mutation endpoints
- Multi-cluster CLI auth
- Per-task metric labels
- gRPC streaming subagent contract
- Multi-provider subagent matrix
- PR creation per git host
- Dashboard Ingress with custom DNS
- RWX PVC driver matrix doc (W-3 — Phase 5 DIST-04)

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| GATE-01 | `Project.Spec.gates` field with per-level policy | Schema already exists at `api/v1alpha1/project_types.go:47-64`. Phase 4 wires reconciler-side consumption only. |
| GATE-02 | Slack-tide between-wave checkpoint | `PauseBetweenWaves` field already present at `project_types.go:63`. WaveReconciler consults at wave boundary. |
| GATE-03 | `tide approve` / `tide reject` | Annotation-driven (D-G3/G4). CLI writes; reconciler watches via `client.Watches(&Project{}, ...)` with annotation predicate. |
| OBS-01 | Structured JSON logs via zap-behind-logr | Already wired via `ctrl.SetLogger(zap.New(...))` at `cmd/manager/main.go:132`. Phase 4 enforces structured field set in all `logger.Info/Error` call sites. |
| OBS-02 | Prometheus metrics with bounded cardinality | `internal/budget/metrics.go` is the reference pattern. New `internal/metrics/registry.go` centralizes all eight v1 counters + one histogram. Linter D-X4 enforces. |
| OBS-03 | OTel tracing spans full chain | `pkg/otelai` ships span helpers; reconcilers create spans with `tracer.Start(ctx, "tide.dispatch.<level>")` keyed off `Project.UID` for `trace_id` continuity. |
| OBS-04 | OpenInference attribute names via `pkg/otelai` | Verified against arize-ai/openinference spec (see Open Question #2 below). Five helpers: `LLMInputMessages`, `LLMOutputMessages`, `TokenCount`, `AgentInvocation`, `ArtifactPath`. |
| OBS-05 | Tail-sampling default; LLM payloads as artifact refs | SDK-level `TraceIDRatioBased(0.1)` (D-O3). Payload→PVC path only (D-O5). Phoenix/LangSmith fetch on demand. |
| OBS-06 | ServiceMonitor gated by Helm value | New `charts/tide/templates/servicemonitor.yaml`. Pattern: `{{ if .Values.prometheus.serviceMonitor.enabled }}`. |
| CLI-01 | Stateless cobra CLI talking to K8s API | `cmd/tide/main.go` + cobra v1.9.x. Kubeconfig via `client-go/tools/clientcmd`. |
| CLI-02 | Ten subcommands | Cobra subcommand-per-file pattern. See "Files Likely to Be Created" §CLI. |
| CLI-03 | `tide inspect-wave` renders wave + statuses | `text/tabwriter` table output. Columns: NAME, STATUS, AGE, ATTEMPT, SCHEDULED-IN-WAVE. |
| CLI-04 | `tide tail` streams pod logs via `pods/log` | client-go `clientset.CoreV1().Pods(ns).GetLogs(name, &v1.PodLogOptions{Follow: true}).Stream(ctx)`. |
| DASH-01 | Separate Deployment + read-only ServiceAccount | New `cmd/dashboard/` binary + Helm template `dashboard-deployment.yaml` + `dashboard-rbac.yaml`. |
| DASH-02 | React Flow v12 + dagre + Tailwind v4, side-by-side | `dashboard/web/` Vite project. Component-as-node pattern verified (see Open Question #1). |
| DASH-03 | SSE for status updates | chi router `http.Flusher`. controller-runtime informer cache → in-process pubsub → SSE handler. |
| DASH-04 | Pod log streaming via apiserver proxy (opt-in) | Backend uses `pods/log` HTTP chunked → translates to SSE for browser. Idle timeout + cleanup defer. |
| DASH-05 | No mutation endpoints | All four endpoints are GET. Architecturally enforced (no PUT/POST/PATCH/DELETE handlers registered). |

## Standard Stack

### Core (already pinned in STACK.md, verified for Phase 4)
| Library | Version | Purpose | Verification |
|---------|---------|---------|--------------|
| `sigs.k8s.io/controller-runtime` | v0.24.x | Manager.Runnable composition pattern for dashboard backend; informer cache for SSE fan-out | [VERIFIED: STACK.md, in use Phase 1-3] |
| `go.opentelemetry.io/otel` | v1.43.0 (trace API stable) | OTel SDK behind `pkg/otelai` | [VERIFIED: STACK.md line 37; trace v1 API stable in 2026] |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | matched to v1.43 | OTLP/gRPC trace exporter | [VERIFIED: STACK.md line 38] |
| `go.opentelemetry.io/otel/trace/noop` | matched to v1.43 | No-op TracerProvider when no endpoint configured | [CITED: pkg.go.dev/go.opentelemetry.io/otel/trace/noop] |
| `github.com/prometheus/client_golang` | v1.23.x | Metrics — already wired Phase 2 (`internal/budget/metrics.go`) | [VERIFIED: STACK.md line 36; in repo go.mod] |
| `sigs.k8s.io/controller-runtime/pkg/metrics` | from controller-runtime | Shared `metrics.Registry` for all custom collectors | [VERIFIED: pattern at `internal/budget/metrics.go:37`] |
| `go.uber.org/zap` v1.28.x + `github.com/go-logr/logr` v1.4.x | matched | Structured JSON logging (already in use) | [VERIFIED: cmd/manager/main.go:132] |
| `github.com/go-chi/chi/v5` | v5.x | HTTP router for dashboard backend (manager.Runnable composition) | [VERIFIED: STACK.md line 40] |

### Supporting (Phase 4 newly introduced)
| Library | Version | Purpose | Verification |
|---------|---------|---------|--------------|
| `github.com/spf13/cobra` | v1.9.x | CLI command structure | [VERIFIED: cobra v1.9.1 latest per Context7 lookup; kubectl + helm + kubebuilder all use this] |
| `k8s.io/client-go` (already transitive) | matched to controller-runtime | Kubeconfig resolution + dynamic client for CLI; `clientset.CoreV1().Pods().GetLogs()` for tail | [VERIFIED: STACK.md line 42] |
| `k8s.io/cli-runtime/pkg/genericclioptions` | matched to client-go | kubectl-style `--context`, `--namespace`, `--kubeconfig` flag handling | [CITED: pkg.go.dev/k8s.io/cli-runtime/pkg/genericclioptions] |
| `github.com/goreleaser/goreleaser` | v2.x | Multi-OS/arch binary release pipeline | [CITED: goreleaser.com/customization/builds — Go 1.26 supported] |

### Frontend (Phase 4 newly introduced)
| Library | Version | Purpose | Verification |
|---------|---------|---------|--------------|
| React 18 + TypeScript 5 | latest stable | Dashboard SPA | [VERIFIED: STACK.md line 49] |
| `@xyflow/react` (React Flow v12) | v12.x | Two-DAG renderer with component-as-node | [VERIFIED: Context7 `/xyflow/xyflow` — current v12 API uses `nodeTypes` map + `Handle` + `NodeProps<T>`; useNodesInitialized hook for dagre layout trigger] |
| `dagre` + `@types/dagre` | v0.8.x | Auto-layout (TB for Planning, LR for Execution) | [VERIFIED: STACK.md line 51 + React Flow v12 dagre example pattern is current] |
| Tailwind v4 | latest | Styling | [VERIFIED: STACK.md line 52] |
| Vite | v6.x | Build tool | [CITED: vitejs.dev — v6 is current for May 2026 React 18 builds] |

### Alternatives Considered (and rejected for Phase 4)
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `@xyflow/react` v12 | Cytoscape.js + cytoscape-dagre | Cytoscape canvas-rendered — per-node React component (status badges, click-into-log-stream) is painful. React Flow's DOM-node-per-task is the right fit for live status. (Already settled in STACK.md.) |
| cobra | urfave/cli, kingpin | cobra is the K8s ecosystem default (kubectl, helm, kubebuilder, eksctl all use it). Krew expects cobra completion shape. |
| OTel GenAI semconv | OpenInference attribute names | OTel GenAI semconv is STILL in "Development" status as of May 2026 [VERIFIED: opentelemetry.io/docs/specs/semconv/gen-ai/ — "This transition plan will be updated to include stable version before the GenAI conventions are marked as stable"]. Phoenix / LangSmith / Arize consume OpenInference today (not GenAI semconv). Dual-emission is a v1.x consideration — D-O4 rejects GenAI for v1.0. |
| OTel for metrics | client_golang for metrics | OTel metrics API is still v0.65.x in May 2026 (per STACK.md line 37). Metric API breaks possible. Tracing is stable v1.x. Keep metrics on prometheus/client_golang per CLAUDE.md anti-pattern. |
| WebSocket from browser | SSE from browser | DASH-03/04 are uni-directional, proxy-friendly, and read-only by spec. SSE wins; WebSocket would add HTTP-upgrade handshake + bidirectional channel we don't use. |

**Installation (additive to go.mod):**
```bash
go get github.com/spf13/cobra@v1.9.1
go get k8s.io/cli-runtime@latest  # genericclioptions
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.43.0
go get go.opentelemetry.io/otel/trace/noop@v1.43.0
go get go.opentelemetry.io/otel/sdk@v1.43.0
go get go.opentelemetry.io/otel/sdk/trace@v1.43.0
```

**Version verification (run during Wave 1 of execution, not at planning time):**
```bash
npm view @xyflow/react version       # confirm v12.x current
npm view tailwindcss version          # confirm v4 stable
go list -m github.com/spf13/cobra     # confirm v1.9.x
go list -m go.opentelemetry.io/otel   # confirm v1.43.x
```

## Architecture Patterns

### System Architecture Diagram

```
                                      ┌─────────────────────────────────────┐
                                      │  Browser (Dashboard SPA — read-only)│
                                      │  - React 18 + @xyflow/react v12     │
                                      │  - Planning DAG (TB) | Execution DAG│
                                      │  - SSE consumer for status + logs   │
                                      │  - "Copy CLI command" toast for ops │
                                      └─────────────────┬───────────────────┘
                                                        │ HTTP/SSE
                                                        ▼
       ┌───────────────────────────┐    ┌──────────────────────────────────┐
       │ cmd/tide (cobra CLI)      │    │ cmd/dashboard (Go manager.Runnable)│
       │ - kubeconfig client       │    │ - chi router + go:embed SPA       │
       │ - apply/watch/tail/approve│    │ - GET /api/v1/projects (paginated)│
       │ - reject/cancel/resume    │    │ - GET /api/v1/projects/{n}        │
       │ - inspect-wave/artifact   │    │ - GET .../events?stream=sse       │
       │ - completion              │    │ - GET .../tasks/{n}/log?stream=sse│
       └─────┬─────────────────────┘    │   (5min idle close, ctx-cancel)   │
             │                          │ - informer cache → pubsub hub     │
             │ k8s API                  │ - dashboard-SA ClusterRole:        │
             │                          │   {projects,milestones,phases,    │
             │                          │    plans,tasks,waves}/get;list;   │
             │                          │    watch + pods/log read          │
             │                          └─────┬────────────────────────────┘
             │                                │
             └────────────────────────────────┼───────► K8s apiserver
                                              │              │
                                              │              ▼
                              ┌───────────────┴──────────────────────┐
                              │ cmd/manager (orchestrator)            │
                              │ ┌─────────────────────────────────┐  │
                              │ │ Phase 1+2+3 reconcilers (6 Kinds)│  │
                              │ │ - Standard 6-step body            │  │
                              │ │ - NEW: gates-policy hook before   │  │
                              │ │   advance (D-G2)                  │  │
                              │ │ - NEW: boundary push trigger (D-W2)│  │
                              │ └────────────┬─────────────────────┘  │
                              │              │                         │
                              │              ▼                         │
                              │ ┌─────────────────────────────────┐   │
                              │ │ internal/gates/                  │   │
                              │ │ - EvaluatePolicy(spec, level)    │   │
                              │ │ - CheckAnnotation(crd, key)      │   │
                              │ └─────────────────────────────────┘   │
                              │ ┌─────────────────────────────────┐   │
                              │ │ internal/metrics/registry.go      │   │
                              │ │ - 8 counters + 1 histogram         │   │
                              │ │ - bounded {project,phase,plan} only│   │
                              │ │ - registered via metrics.Registry  │   │
                              │ └─────────────────────────────────┘   │
                              │ ┌─────────────────────────────────┐   │
                              │ │ pkg/otelai/ (PUBLIC, thin)        │   │
                              │ │ - LLMInputMessages([]Message)     │   │
                              │ │ - LLMOutputMessages([]Message)    │   │
                              │ │ - TokenCount(p,c,cr,cc)           │   │
                              │ │ - AgentInvocation(name,role,lvl)  │   │
                              │ │ - ArtifactPath(pvcPath) — D-O5    │   │
                              │ │ Each returns []attribute.KeyValue │   │
                              │ │ NO span lifecycle DSL             │   │
                              │ └────────────┬─────────────────────┘   │
                              │              │                         │
                              │              ▼                         │
                              │       OTel SDK v1.43                   │
                              │   ParentBased(TraceIDRatioBased(0.1))  │
                              │       env-overridable samplers          │
                              │   OTLP gRPC exporter (optional)         │
                              └──────────────────────────────────────────┘
                                              │
                                              ▼
                              External OTel Collector + Phoenix/LangSmith/Arize
                                  (optional — orchestrator works without)

                              ┌──────────────────────────────────────────┐
                              │ cmd/tide-lint (multichecker)              │
                              │ - crosspool (Phase 1)                     │
                              │ - providerfirewall (Phase 2)              │
                              │ - NEW: metric-cardinality (Phase 4 D-X4)  │
                              │   rejects "task" label in metrics defs    │
                              └──────────────────────────────────────────┘
```

### Recommended Project Structure (Phase 4 additions to existing tree)
```
cmd/
├── tide/                    # NEW — cobra CLI binary (D-C1)
│   ├── main.go              # root + Execute()
│   ├── apply.go             # tide apply -f
│   ├── watch.go             # tide watch
│   ├── tail.go              # tide tail
│   ├── approve.go           # tide approve [--wave]
│   ├── reject.go            # tide reject
│   ├── cancel.go            # tide cancel [--force]
│   ├── resume.go            # tide resume
│   ├── inspect_wave.go      # tide inspect-wave
│   ├── artifact_get.go      # tide artifact-get
│   ├── describe_budget.go   # tide describe-budget
│   └── root_flags.go        # genericclioptions wiring
├── dashboard/               # NEW — Go backend binary (D-D2, D-X2)
│   ├── main.go              # manager.Runnable composition + chi router
│   ├── api/
│   │   ├── projects.go      # GET /api/v1/projects[/{name}]
│   │   ├── events_sse.go    # GET /api/v1/projects/{n}/events?stream=sse
│   │   └── logs_sse.go      # GET /api/v1/tasks/{n}/log?stream=sse
│   ├── hub/
│   │   └── pubsub.go        # in-process informer-event fan-out (D-D3)
│   └── embed/               # go:embed target — Vite build copies here
│       └── dist/...         # produced by `cd dashboard/web && npm run build`
dashboard/                   # NEW — frontend source (D-X2)
└── web/
    ├── package.json
    ├── vite.config.ts
    ├── tailwind.config.ts
    ├── tsconfig.json
    ├── src/
    │   ├── main.tsx
    │   ├── App.tsx          # side-by-side split layout (D-D1)
    │   ├── components/
    │   │   ├── PlanningDAG.tsx   # dagre top-down
    │   │   ├── ExecutionDAG.tsx  # dagre left-right, wave subgraph bands
    │   │   ├── ProjectNode.tsx   # custom node — status badges
    │   │   ├── MilestoneNode.tsx
    │   │   ├── PhaseNode.tsx
    │   │   ├── PlanNode.tsx
    │   │   ├── TaskNode.tsx
    │   │   └── LogStream.tsx     # click-to-open per-task SSE
    │   └── lib/
    │       ├── sse.ts            # EventSource wrapper + Last-Event-ID
    │       └── layout.ts         # dagre integration
internal/
├── gates/                   # NEW (D-G2 — already prefix-allocated in CONTEXT specifics)
│   ├── policy.go            # EvaluatePolicy(gates Gates, level string) GatePolicy
│   ├── annotation.go        # CheckApprove / CheckReject helpers
│   └── policy_test.go
├── metrics/                 # NEW (D-X4 — single registration site)
│   ├── registry.go          # all 8 counters + 1 histogram
│   └── registry_test.go
├── otelinit/                # NEW (D-O3 — tracer provider lifecycle)
│   ├── provider.go          # NewTracerProvider / Shutdown
│   └── provider_test.go
pkg/
└── otelai/                  # NEW PUBLIC (D-O4)
    ├── attrs.go             # five helpers — return []attribute.KeyValue
    ├── attrs_test.go
    └── doc.go
tools/analyzers/
└── metriccardinality/       # NEW (D-X4)
    ├── analyzer.go
    ├── analyzer_test.go
    └── testdata/
charts/tide/templates/
├── dashboard-deployment.yaml    # NEW (D-X3)
├── dashboard-rbac.yaml          # NEW (D-X3 — ClusterRole get;list;watch on
│                                #         {projects,milestones,phases,plans,
│                                #          tasks,waves} + pods/log)
└── servicemonitor.yaml          # NEW (D-O6 + D-X3 — gated)
```

### Pattern 1: Gate-policy seam in the Standard 6-step reconciler body

**What:** Insert gate-policy evaluation as a NEW step between "child CRDs materialized" and "advance parent status."

**When to use:** All four up-stack reconcilers (Milestone, Phase, Plan, Project) and the wave-boundary inside the Plan/Wave reconcilers (`PauseBetweenWaves`).

**Example pseudocode (extends Milestone reconciler — see `internal/controller/milestone_controller.go:135-275`):**
```go
// Source: pattern extends internal/controller/milestone_controller.go (Phase 3)
// In handleJobCompletion, BEFORE patchMilestoneSucceeded:

func (r *MilestoneReconciler) handleJobCompletion(...) (ctrl.Result, error) {
    // ... existing EnvelopeReader + MaterializeChildCRDs ...

    // NEW: gate-policy check (D-G2).
    policy := gates.EvaluatePolicy(project.Spec.Gates, "milestone")
    if policy == "approve" || policy == "pause" {
        if !gates.CheckApprove(ms, "milestone") {
            return r.patchMilestoneAwaitingApproval(ctx, ms, policy)
        }
    }
    if gates.CheckReject(project) {
        return r.patchMilestoneFailed(ctx, ms, "RejectedByUser", "...")
    }

    // NEW: mid-stack push trigger (D-W2). Fires once per boundary.
    if err := r.maybeTriggerBoundaryPush(ctx, ms, project); err != nil {
        return ctrl.Result{}, err
    }

    return r.patchMilestoneSucceeded(ctx, ms)
}
```

### Pattern 2: SSE handler with EventSource fan-out

**What:** chi route exposes an SSE-streaming endpoint backed by an in-process pubsub hub fed by controller-runtime informer events.

**When to use:** Dashboard `/api/v1/projects/{n}/events?stream=sse` (DASH-03).

**Example pseudocode:**
```go
// Source: pattern derived from MDN EventSource semantics + chi http.Flusher idiom
func (s *Server) handleEventsSSE(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming unsupported", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no") // Pitfall 23 — disable nginx buffering

    projectName := chi.URLParam(r, "name")
    lastEventID := r.Header.Get("Last-Event-ID")
    sub := s.hub.Subscribe(projectName, lastEventID)
    defer s.hub.Unsubscribe(sub)

    // Send a heartbeat comment every 15s so proxies don't close idle connections.
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return // client disconnect — defer runs Unsubscribe (Pitfall 22)
        case <-ticker.C:
            fmt.Fprint(w, ": heartbeat\n\n")
            flusher.Flush()
        case ev := <-sub.Events:
            fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, ev.JSON)
            flusher.Flush()
        }
    }
}
```

### Pattern 3: OTel TracerProvider with no-op fallback

**What:** Initialize OTel tracing at manager startup; degrade to no-op when no OTLP endpoint is configured.

**Example pseudocode:**
```go
// Source: pattern derived from go.opentelemetry.io/otel/sdk/trace + .../trace/noop
// internal/otelinit/provider.go
func NewTracerProvider(ctx context.Context) (trace.TracerProvider, func(context.Context) error, error) {
    endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if endpoint == "" {
        // D-O3 / Discretion item — no-op when not configured. Reconcilers
        // call tracer.Start(...) freely; they become no-ops.
        return tracenoop.NewTracerProvider(), func(context.Context) error { return nil }, nil
    }
    exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint))
    if err != nil {
        return nil, nil, fmt.Errorf("otlptrace: %w", err)
    }
    // OTEL_TRACES_SAMPLER + _ARG honored by sdktrace.NewTracerProvider when no
    // explicit WithSampler() is passed — env-driven sampling per D-O3.
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exp),
        sdktrace.WithResource(newResource()),
        // No WithSampler — defer to OTEL_TRACES_SAMPLER env var.
    )
    otel.SetTracerProvider(tp)
    return tp, tp.Shutdown, nil
}
```

### Anti-Patterns to Avoid

- **Don't put OpenInference attribute emission in the subagent pod.** Phase 3 D-A4 mandates subagent pods have zero K8s verbs; they also have no OTel SDK in the image (out of scope). The orchestrator's `internal/subagent/anthropic/` reads the stream-json `events.jsonl` from PVC AFTER the Job completes and emits spans then. Span timestamps reflect event timestamps from the log, not wall-clock-at-emit.
- **Don't add a `task` label to ANY metric.** D-X4 lint forbids it; runtime cardinality explosion is the failure mode (Pitfall 17).
- **Don't expose mutation endpoints on the dashboard.** DASH-05 is architecturally enforced — the dashboard backend registers ONLY GET handlers. Any future PR adding PUT/POST/PATCH/DELETE to `cmd/dashboard/api/` fails review.
- **Don't talk to the apiserver directly from the browser.** No CORS holes, no token-in-localStorage pattern. Browser → backend → apiserver only (D-D2).
- **Don't bake OTel exporter endpoint into the binary.** All OTel config flows through env vars (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`). Helm chart sets the env vars; the binary just reads them.
- **Don't ship a YAML output mode on the CLI.** D-C4 — kubectl already handles YAML. `tide ... -o json` only.
- **Don't render the planning DAG with cross-wave subgraph bands.** Per README spec: subgraph nesting is for the **planning** DAG (containment hierarchy), wave bands are for the **execution** DAG (flat layout with cross-wave edges). Reversing them obscures the structural argument.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| CLI argument parsing | Custom flag-walker | cobra v1.9.x | Subcommand routing, autogenerated `--help`, completion subcommand for bash/zsh/fish/powershell, kubectl-style UX expectations. |
| Kubeconfig resolution | Manual KUBECONFIG env parsing | `k8s.io/cli-runtime/pkg/genericclioptions` | Handles `$KUBECONFIG`, `~/.kube/config`, `--context`, `--namespace`, in-cluster SA — same way kubectl does. |
| Pod log streaming server-side | Raw HTTP-chunked-transfer parsing | `clientset.CoreV1().Pods(ns).GetLogs(name, &v1.PodLogOptions{Follow:true}).Stream(ctx)` | client-go handles reconnect, EOF on pod restart, follow semantics correctly. |
| Tab-separated CLI tables | Custom column-width logic | `text/tabwriter` (stdlib) | Mirrors kubectl exactly (consistent UX); zero dep cost. |
| Prometheus counter cardinality enforcement | Runtime panic on label drift | `cmd/tide-lint` AST analyzer at compile-time (D-X4) | Lint > runtime check — bad metric definitions never reach production. |
| OpenInference attribute construction | Hand-built `attribute.KeyValue` slices everywhere | `pkg/otelai` thin helpers (D-O4) | Single seam to evolve when OpenInference spec drifts. Minimal surface, easy to read, no DSL. |
| Cycle detection in plan validation | Custom algorithm | `pkg/dag.ComputeWaves` already does this (Phase 1) | Phase 1's algorithm already returns `CycleError` naming nodes. Phase 4 gate logic doesn't touch this. |
| Front-end DAG layout | Pixel-positioning nodes manually | dagre + `useNodesInitialized` hook (React Flow v12) | Industry-standard top-down + LR layout — dagre handles it; React Flow v12 has `useNodesInitialized` to schedule layout after measurement. |
| SSE protocol semantics | Custom WebSocket wrapper | EventSource (browser stdlib) + chi `http.Flusher` (Go stdlib) | Reconnect, Last-Event-ID, ID-based replay, automatic in browser — all free. |
| Go binary multi-arch release | Manual `go build GOOS=...; tar` | goreleaser v2.x | Signs releases, builds homebrew tap, Krew manifest, container image, checksums.txt — all from one `.goreleaser.yaml`. |
| Krew plugin packaging | Hand-rolled plugin manifest | `krew-release-bot` GitHub Action + goreleaser hooks | Auto-PRs the Krew plugin manifest on every release. |
| K8s informer fan-out for dashboard SSE | Custom event broker | controller-runtime's manager-managed informer cache + in-process pubsub | Cache is already running; subscribe via `mgr.GetCache().GetInformer(...).AddEventHandler(...)`. No Redis, no DB. |

**Key insight:** Phase 4 is the most "compose existing pieces" phase in TIDE. Almost every surface has a well-trodden library path that the K8s/Go/React ecosystem has converged on. The risk in this phase is **NOT inventing where invention is unnecessary** — every "shouldn't we just X..." prompt should be answered with the library above. The genuinely-novel surface is exactly one package: `pkg/otelai` (because no Go OpenInference SDK exists). Everything else is composition.

## Common Pitfalls

### Pitfall 17 (carry-forward, restated): Metric cardinality explosion

**What goes wrong:** A well-intentioned commit adds `task` to a metric's labels: `tide_dispatch_latency_seconds{project, phase, plan, task}`. Across a 200-task run, that's 200× the cardinality; over a week of runs, Prometheus storage explodes and queries time out.

**Why it happens:** "But I just want to see per-task latency!" — natural mental model for someone used to APM tools where every request is its own dimension.

**How to avoid:** Per-task observability lives in **traces** (where high-cardinality span attributes are FREE — they're not indexed), not metrics. The `cmd/tide-lint metric-cardinality` analyzer (D-X4) catches this at compile time. Phase 4 ships the analyzer.

**Warning signs:** Prometheus `prometheus_tsdb_head_series` growing linearly with task count. Per-Project queries returning >1000 series.

### Pitfall 22 (carry-forward, restated): Dashboard pod-log stream leaks

**What goes wrong:** A user opens the dashboard, clicks into a task's log stream, walks away. Browser tab closes; backend's `pods/log` stream stays open; goroutine keeps allocating buffer space; orchestrator OOMs after N users × M tabs.

**Why it happens:** SSE has no explicit "client disconnected" event — the backend has to detect it via `r.Context().Done()` (closed when the underlying HTTP connection drops).

**How to avoid:** Backend log-stream handler MUST:
1. Always run `select` on `r.Context().Done()`.
2. Always `defer` the `pods/log` stream's `Close()`.
3. Set a 5-min idle timeout (close even if client is connected but no log lines arrive — limits long-running attached streams).
4. Cap concurrent active streams per ServiceAccount (rate-limit at the handler).

**Warning signs:** Goroutine count climbing monotonically; `cmd/dashboard` heap profile shows growing `http.bodyEOFSignal` instances.

### NEW Pitfall 23: SSE through nginx-ingress + reverse-proxy buffering

**What goes wrong:** Dashboard works perfectly in `kubectl port-forward` but events arrive in a 4KB burst (or never) when accessed through a cluster ingress. Browser logs no errors — it's just waiting for the buffer to fill.

**Why it happens:** nginx-ingress (and most reverse proxies) buffer HTTP response bodies by default. SSE responses are infinitely streaming, so the buffer never flushes until full. Also the proxy may add a default 60s timeout for idle connections.

**How to avoid:**
1. Set `X-Accel-Buffering: no` header on SSE responses (nginx-specific, but harmless on other proxies).
2. Set `Cache-Control: no-cache`.
3. Send a `:heartbeat\n\n` comment every 15s to keep idle connections alive.
4. Document the operator-side requirement: any Ingress in front of the dashboard needs `proxy_buffering off` and `proxy_read_timeout 1h` (or equivalent).

**Warning signs:** Dashboard works locally but is "frozen" when accessed via cluster ingress. SSE messages arrive in batches with the buffer size in bytes between them. Browser DevTools shows `EventSource` open but `onmessage` never fires.

### NEW Pitfall 24: OTel sampler env-var sentinel confusion

**What goes wrong:** Operator sets `OTEL_TRACES_SAMPLER=traceidratio` and `OTEL_TRACES_SAMPLER_ARG=0.5` expecting 50% sampling, but ALL traces are sampled in production. Reason: an explicit `sdktrace.WithSampler(...)` was passed to `sdktrace.NewTracerProvider(...)` in code, overriding env-driven config.

**Why it happens:** The OTel Go SDK only consults `OTEL_TRACES_SAMPLER` when NO `WithSampler` option is supplied to the TracerProvider constructor. The default is `parentbased_always_on`, so if you forget to omit `WithSampler` you silently get always-on.

**How to avoid:**
- `internal/otelinit/provider.go` constructs the TracerProvider WITHOUT `WithSampler(...)`. Document this with a comment line — it's the kind of "harmless-looking line" that gets added in a future refactor.
- Unit test: spin up a TracerProvider with `OTEL_TRACES_SAMPLER=traceidratio` and `OTEL_TRACES_SAMPLER_ARG=0.0`, assert all spans are dropped.

**Warning signs:** OTel storage costs grow linearly with traffic regardless of `OTEL_TRACES_SAMPLER` settings. `sdktrace.WithSampler` greps positive in `internal/otelinit/`.

### NEW Pitfall 25: cobra subcommand context-cancel propagation

**What goes wrong:** User runs `tide tail my-task` and presses Ctrl-C. The pod-log stream keeps fetching for another 30 seconds because the cobra command's context was tied to `context.Background()` instead of the signal-handling parent.

**Why it happens:** cobra v1.9 introduced `cmd.Context()` which is `context.Background()` by default. To get signal handling, the root command's `Execute()` must be invoked via `ExecuteContext(signal.NotifyContext(...))`.

**How to avoid:**
- Root command in `cmd/tide/main.go` wraps execution in `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` before calling `rootCmd.ExecuteContext(...)`.
- Every long-running subcommand (`tide tail`, `tide watch`) threads `cmd.Context()` into the K8s API call.

**Warning signs:** Ctrl-C doesn't return the prompt immediately on `tide tail` / `tide watch`. Goroutine leak visible in pprof if you `pprof -seconds 30` during a `tide watch` that was Ctrl-C'd.

### NEW Pitfall 26: React Flow v12 layout-flicker on dynamic node insertion

**What goes wrong:** SSE event arrives; a new Task node appears at `position: {x:0, y:0}` on top of an existing node; user sees a flash before dagre re-runs and positions it correctly.

**Why it happens:** React Flow renders nodes immediately on `setNodes(...)`. dagre's layout requires measured node dimensions (DOM heights/widths), which aren't known until after the first render. So inserted nodes get positioned at the seed position, then "snap" when `useNodesInitialized` fires.

**How to avoid:**
- Insert new nodes with `style: { opacity: 0 }`. Run layout in `useNodesInitialized` effect, then set `opacity: 1` in a second tick.
- OR: precompute approximate position from the parent node + dagre's default ranksep.
- Document for plan-time: a 1-2 frame flicker is acceptable for v1.0; defer fancy "fade-in" animations.

**Warning signs:** SSE-driven node insertion causes a visible flicker on every update.

### NEW Pitfall 27: Krew plugin name vs. cobra `Use:` mismatch

**What goes wrong:** Krew installs the plugin as `kubectl-tide`. cobra `rootCmd.Use = "tide"` declares the program name as `tide`. Help output reads `Usage: tide ...` but the user typed `kubectl tide ...`. Confusing — and `tide completion bash` doesn't generate `kubectl_complete-tide`.

**Why it happens:** Krew renames the binary to `kubectl-<plugin>` but cobra's introspection still sees the binary name.

**How to avoid:**
- cobra root command: `Use: filepath.Base(os.Args[0])` (resolves to `tide` or `kubectl-tide` automatically).
- Completion: support both `tide completion bash` AND `kubectl tide completion bash` by symlink-aware help text.
- Document the dual invocation in `docs/cli.md`.

**Warning signs:** Krew install works but `kubectl tide --help` shows `Usage: tide ...` (technically wrong but harmless); or `kubectl tide completion bash` produces a script that doesn't actually wire kubectl completion.

## Open Question Resolutions

### #1 — React Flow v12 + @xyflow/react v12 + dagre two-DAG layout patterns

**Question:** Confirm the current React Flow v12 API for component-as-node, custom edge rendering, wave-subgraph backgrounds, and dagre integration. Pitfalls (re-render storms, edge-routing under dynamic node insertion). Minimum bundle size (<500KB gzipped target).

**Resolution (HIGH confidence):**
- **Component-as-node pattern verified** via Context7 `/xyflow/xyflow` lookup. The current shape is exactly:
  ```tsx
  type TaskNode = Node<{ status: string; attempt: number }, 'task'>;
  function TaskNodeComponent({ data, selected }: NodeProps<TaskNode>) { ... }
  const nodeTypes = { task: TaskNodeComponent };
  <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} />
  ```
  Strongly-typed; per-node React components carry status badges, log-stream click handlers, animations.
- **Dagre integration:** use the `useNodesInitialized` hook (returns `true` once all nodes have measured DOM dimensions) to trigger dagre layout in a `useEffect`. Then `setNodes(...)` with positions + `fitView({padding: 0.2})`. Verified via Context7 example.
- **Wave subgraph bands:** React Flow v12 supports background rectangles via the `<Background />` component but for **wave bands** the cleaner approach is rendering rectangle nodes at z-index 0 sized to cover the wave's column, with task nodes on top at z-index 10. Each wave-rect node has `selectable: false, draggable: false`.
- **Cross-wave edges:** React Flow v12 routes edges automatically via Bezier or Step paths. For LR wave-band layout, set edge `type: 'smoothstep'` and let dagre's `acyclicer: 'greedy'` handle routing. No additional config needed.
- **Re-render storm pitfall:** Use `useNodesState`/`useEdgesState` (React Flow v12 hooks) to avoid re-rendering the entire ReactFlow tree on every node change. Memoize node-type registrations: `const nodeTypes = useMemo(() => ({ task: TaskNodeComponent, ... }), [])`.
- **Bundle size:** React 18 + React Flow v12 + dagre + Tailwind v4 (CSS) gzipped at ~280KB based on similar projects (e.g., Argo Workflows dashboard, Headlamp). Well under 500KB target. Verified by inspecting comparable open-source projects' published Vite builds. [CONFIDENCE: MEDIUM — bundle size depends on tree-shaking and Tailwind purge config; concrete measurement happens at Wave 5 build time.]

**Concrete recommendation for planner:**
- Wave 4 plan creates `dashboard/web/` with Vite + React + TS + Tailwind v4 + React Flow v12 + dagre.
- Wave 5 plan implements `PlanningDAG.tsx` (TB) and `ExecutionDAG.tsx` (LR) + 5 node components.
- Wave 5 acceptance includes `npm run build && du -sh dist/assets/*.js.br` < 500KB.

### #2 — OpenInference attribute names — current 2026 spec

**Question:** Verify against arize-ai/openinference: current attribute names, JSON encoding rules for input/output messages, Phoenix/LangSmith/Arize divergences, OTel GenAI semconv stability.

**Resolution (HIGH confidence):**

**Verified attribute keys (Context7 `/arize-ai/openinference` spec page):**
| Key | Type | Notes |
|-----|------|-------|
| `openinference.span.kind` | string | Required. `"LLM"` for LLM spans, `"AGENT"` for agent spans, `"TOOL"` for tool spans. |
| `llm.system` | string | Required for LLM. `"anthropic"` for Claude-backed dispatch. |
| `llm.model_name` | string | e.g., `"claude-sonnet-4-6"`, `"claude-haiku-4-5"`. |
| `llm.invocation_parameters` | string (JSON-encoded) | Temperature, max_tokens, etc., as JSON object string. |
| `llm.input_messages` | flat keyed attrs | NOT a JSON array. Flattens as `llm.input_messages.0.message.role`, `llm.input_messages.0.message.content`, `llm.input_messages.1.message.role`, etc. |
| `llm.output_messages` | flat keyed attrs | Same flat shape. |
| `llm.token_count.prompt` | int | Integer count of prompt tokens. |
| `llm.token_count.completion` | int | Integer count of completion tokens. |
| `llm.token_count.total` | int | Optional sum. |
| `llm.token_count.prompt_details.cache_read` | int | Anthropic cache-read tokens (D-C5 envelope already preserves this). |
| `llm.token_count.prompt_details.cache_write` | int | Anthropic cache-creation tokens. |
| `input.value` | string (JSON) | Raw input payload as a JSON string — but D-O5 says we use ArtifactPath instead. |
| `input.mime_type` | string | `"application/json"`. |
| `output.value` | string (JSON) | Raw output payload — also defer to ArtifactPath per D-O5. |
| `output.mime_type` | string | `"application/json"`. |

**For agent spans (OBS-04 — dispatch chain spans):**
| Key | Type | Notes |
|-----|------|-------|
| `openinference.span.kind` | string | `"AGENT"` |
| Span name convention | string | Use `tide.dispatch.<level>` (per CONTEXT specifics). |

**JSON encoding rule:** Messages flatten — NOT arrays. So `LLMInputMessages([]Message)` in `pkg/otelai` returns:
```go
[]attribute.KeyValue{
    attribute.String("llm.input_messages.0.message.role", "user"),
    attribute.String("llm.input_messages.0.message.content", "..."),
    attribute.String("llm.input_messages.1.message.role", "assistant"),
    // ...
}
```
This matches the spec's `Completion Span Example` JSON (verified via Context7 docs fetch). Reading consumers (Phoenix, LangSmith, Arize) all parse the flat shape.

**Consumer compatibility:** Phoenix (Arize-hosted), LangSmith, and Arize Platform all consume OpenInference attribute names with the same flat encoding. No 2026 divergences observed in the spec.

**OTel GenAI semconv status (verified May 2026):** Still in "Development" status per `opentelemetry.io/docs/specs/semconv/gen-ai/`. The page explicitly says "This transition plan will be updated to include stable version before the GenAI conventions are marked as stable." D-O4's rejection stands: use OpenInference for v1.0.

**Concrete recommendation for planner:**
- `pkg/otelai/attrs.go` implements exactly 5 exported helpers returning `[]attribute.KeyValue` slices with the verified attribute keys above.
- Wave 2 plan includes a unit test that asserts each helper produces the exact key strings above (this is the "lock in" against future spec drift — if Arize changes a key, the test fails loudly).
- Reference: spec source is `https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md` (cite in `pkg/otelai/doc.go`).

### #3 — OTel sampler config — env-overridable

**Question:** Confirm OTel Go SDK v1.43.x respects `OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG` natively (no custom wiring needed); document exact env values per supported sampler; confirm tail-sampling is a Collector concern (not SDK).

**Resolution (HIGH confidence):**

**Env-driven sampler — recognized values (verified against OTel spec May 2026):**
| `OTEL_TRACES_SAMPLER` | `OTEL_TRACES_SAMPLER_ARG` | Behavior |
|------------------------|---------------------------|----------|
| `always_on` (default if unset) | ignored | Sample everything |
| `always_off` | ignored | Sample nothing |
| `traceidratio` | float 0.0–1.0 | Sample fraction by trace ID |
| `parentbased_always_on` | ignored | Always on, respect parent decision |
| `parentbased_always_off` | ignored | Always off, respect parent decision |
| `parentbased_traceidratio` | float 0.0–1.0 | **D-O3 default — 0.1** |
| `parentbased_jaeger_remote` / `jaeger_remote` | jaeger config | Remote sampler config |
| `xray` | AWS-specific | AWS X-Ray sampler |

**Go SDK native support (verified):** The `sdktrace.NewTracerProvider(...)` constructor, when called WITHOUT a `WithSampler(...)` option, defaults to `ParentBased(AlwaysSample())`. To honor `OTEL_TRACES_SAMPLER`, the application MUST NOT pass `WithSampler`. The SDK's `autoexport` package and the OTel-Go contrib `autosampler` package (`go.opentelemetry.io/contrib/samplers/autosampler` — verify availability at Wave 2 implementation time) provide explicit env-var-driven samplers. [VERIFIED: STACK.md + opentelemetry-go README; default-without-WithSampler is the recommended pattern.]

**Tail-sampling — confirmed Collector concern:** The Go SDK supports only head-sampling (decided at span creation). Tail-sampling (decided after span completion based on error status, latency, etc.) lives in the OTel Collector's `tail_sampling` processor. D-O3 correctly defers Collector deployment to v1.x. v1.0 ships with head-sampling only.

**Concrete recommendation for planner:**
- `internal/otelinit/provider.go` constructs TracerProvider WITHOUT `WithSampler(...)` to honor env vars.
- Helm chart's controller Deployment env block defaults:
  ```yaml
  env:
    - name: OTEL_TRACES_SAMPLER
      value: parentbased_traceidratio
    - name: OTEL_TRACES_SAMPLER_ARG
      value: "0.1"
  ```
- `docs/observability.md` (Phase 5 work, but draft outline in Phase 4) documents how to swap to `traceidratio` + 1.0 for full sampling during debugging.
- Pitfall 24 (above) covers the gotcha of forgetting and adding `WithSampler` back.

### #4 — OTel Go SDK v1.43.x — trace API stability vs metrics v0.65

**Question:** Confirm Phase 4 uses OTel for traces ONLY. OTLP exporter choice (gRPC vs HTTP). No-op/disabled path.

**Resolution (HIGH confidence):**
- **Tracing only.** Per STACK.md and CLAUDE.md anti-pattern: keep metrics on prometheus/client_golang. OTel metrics is still v0.65.x and API can shift; OTel trace v1.x is stable. Phase 4 uses OTel for traces only.
- **OTLP gRPC exporter** is the v1.0 default (`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`). Configurable entirely via env vars per STACK.md line 38: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_HEADERS`, etc.
- **HTTP exporter NOT shipped in v1.0** — gRPC is the universal path; Phoenix/LangSmith/Arize all accept OTLP gRPC. HTTP is a v1.x consideration.
- **No-op/disabled path:** when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset, `internal/otelinit/provider.go` returns `tracenoop.NewTracerProvider()` (from `go.opentelemetry.io/otel/trace/noop`). All `tracer.Start(...)` calls become no-ops with zero network traffic. Manager startup NEVER fails for OTel-related reasons. This is the "kind cluster works without an OTel collector" property.

**Env vars natively supported by the Go SDK (no custom code):**
| Env Var | Behavior |
|---------|----------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | gRPC endpoint URL |
| `OTEL_EXPORTER_OTLP_HEADERS` | Comma-sep `k1=v1,k2=v2` (e.g., auth tokens) |
| `OTEL_SERVICE_NAME` | Resource attribute `service.name` |
| `OTEL_RESOURCE_ATTRIBUTES` | Comma-sep additional resource attrs |
| `OTEL_TRACES_SAMPLER` / `_ARG` | Sampler config (per Q3) |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | Exporter timeout in milliseconds |

**Concrete recommendation for planner:**
- Wave 2 plan creates `internal/otelinit/provider.go` with the no-op fallback explicit in the code.
- Wave 2 plan creates a unit test asserting no-op behavior when env is unset (spy on Exporter — must NOT be called).
- Wave 6 plan extends Helm chart's controller env block with OTel vars and ServiceMonitor template.

### #5 — Prometheus client_golang v1.23 — bounded-cardinality enforcement pattern

**Question:** Existing linting prior art? How to encode the rule? Histogram buckets.

**Resolution (HIGH confidence):**

**Prior art:** Two relevant tools exist:
1. **promlint** — focuses on metric NAMING (snake_case, _total suffix on counters, etc.), not label cardinality. Doesn't fit D-X4.
2. **go/analysis** custom analyzers — exactly the pattern TIDE already uses for `crosspool` (Phase 1) and `providerfirewall` (Phase 2). Reference implementations live at `tools/analyzers/crosspool/analyzer.go` and `tools/analyzers/providerfirewall/analyzer.go`.

**Encoding the rule (recommended approach):**
- AST-walk for calls to `prometheus.NewCounterVec`, `prometheus.NewHistogramVec`, `prometheus.NewGaugeVec`, `prometheus.NewSummaryVec`.
- Inspect the second argument (always `[]string{...}` literal in idiomatic registration).
- Reject any element that's the string literal `"task"`.
- Emit a diagnostic with the file:line of the offending element and a fix suggestion.
- Pattern: replicate `tools/analyzers/providerfirewall/analyzer.go` 1:1 with the inspector pointing at `prometheus.*Vec` instead of imports.

**Histogram buckets (D-Discretion):**
- `tide_dispatch_latency_seconds` uses `[0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800]` — per CONTEXT.md `<specifics>`.
- `prometheus.NewHistogramVec` accepts a `Buckets []float64` field in `HistogramOpts`. Verified — the `internal/budget/metrics.go` pattern can be copied 1:1 with the bucket slice.

**Concrete recommendation for planner:**
- Wave 1 plan creates `internal/metrics/registry.go` registering all 8 counters + 1 histogram via `metrics.Registry.MustRegister(...)`. Bucket slice constant defined at top of file.
- Wave 1 plan creates `tools/analyzers/metriccardinality/analyzer.go` + tests + testdata fixture.
- Wave 1 plan updates `cmd/tide-lint/main.go` to `multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer, metriccardinality.Analyzer)`.
- Wave 1 plan updates Makefile `tide-lint` target to remain in `go run ./cmd/tide-lint ./...` form.

### #6 — SSE-through-ingress concerns

**Question:** SSE pitfalls behind nginx-ingress. EventSource reconnect semantics. Go SSE close-via-Flush. Fan-out memory.

**Resolution (HIGH confidence):**

**nginx-ingress pitfalls (Pitfall 23 above):**
- nginx buffers HTTP response bodies by default. SSE responses get held in the buffer forever (it never fills). Mitigation: `X-Accel-Buffering: no` response header on every SSE response. nginx-specific but harmless on other proxies.
- nginx default `proxy_read_timeout` is 60s. After 60s of no body bytes, nginx terminates the connection. Mitigation: backend sends `: heartbeat\n\n` comments every 15s (well under default timeouts on every common proxy).
- Operator may need to set Ingress annotations: `nginx.ingress.kubernetes.io/proxy-buffering: "off"`, `nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"`.

**EventSource reconnect semantics (verified, browser stdlib):**
- Browser auto-reconnects on connection drop. Wait time defaults to 3 seconds; server can override with `retry: <ms>\n` in the stream.
- On reconnect, browser sends `Last-Event-ID: <last-id-seen>` header. Server uses this for replay.
- TIDE backend assigns monotonic integer IDs per connection's event stream; on reconnect, replay from `Last-Event-ID + 1` (cap replay at 100 events to bound memory).

**Go SSE write through Flush (verified):**
- `http.ResponseWriter` must be type-asserted to `http.Flusher`. Write bytes, call `flusher.Flush()` to push them through the kernel/proxy.
- Context cancellation (`r.Context().Done()` closes when client disconnects) does NOT auto-trigger Flush; it just lets the handler exit. The next write attempt returns an error; or the handler's `select` watches `r.Context().Done()` and exits cleanly.

**Fan-out memory bound (D-D3 in-process pubsub):**
- Hub keyed by Project name. Per-subscriber channel buffer = 64 events; when full, drop oldest (preserves newest = current state).
- Per-process upper bound: N subscribers × 64 events × ~200 bytes/event = ~12KB per subscriber. 100 subscribers = 1.2MB. Acceptable for v1.0.
- For >100 concurrent subscribers (operator scale), switch to a server-sent broadcast loop with no per-subscriber buffer. Out of v1.0 scope.

**Concrete recommendation for planner:**
- Wave 4 plan `cmd/dashboard/api/events_sse.go` includes the heartbeat + `X-Accel-Buffering: no` pattern.
- Wave 4 plan `cmd/dashboard/hub/pubsub.go` implements the 64-event buffer + drop-oldest policy.
- `docs/dashboard.md` (Phase 5 draft) documents the nginx-ingress annotations.

### #7 — K8s pods/log subresource streaming

**Question:** client-go API for streaming logs. Reconnect on pod restart. Goroutine-leak mitigation. apiserver proxy quirks.

**Resolution (HIGH confidence):**

**API surface:**
```go
import (
    corev1 "k8s.io/api/core/v1"
    "k8s.io/client-go/kubernetes"
)
req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
    Follow:     true,
    Container:  "subagent",        // explicit container — Pods have credproxy + subagent
    TailLines:  ptr.To(int64(100)), // last 100 lines on connect
    Timestamps: true,
})
stream, err := req.Stream(ctx)
if err != nil { ... }
defer stream.Close()
// Read line-by-line; pipe to SSE writer.
```

**Reconnect semantics:**
- If pod restarts during `Follow:true`, the stream returns EOF. The backend handler should NOT auto-reconnect — surface EOF to the SSE client. Browser EventSource auto-reconnects; on reconnect, the dashboard backend opens a fresh `Pods.GetLogs(...).Stream(ctx)` for the same pod (which by then may be the new pod restart).
- If pod is gone (deleted), `Stream()` returns a 404 error. Surface as SSE `event: pod-gone` + close.

**Goroutine-leak mitigation (Pitfall 22):**
- Always wrap the read loop in a `select` watching `ctx.Done()`.
- Set 5-min idle timeout: track time since last byte; if exceeded, close stream + send SSE `event: idle-timeout` + close.
- Track active stream count per dashboard SA; cap at e.g. 50 concurrent.
- Defer `stream.Close()` immediately after `Stream()` returns.

**apiserver proxy quirks:**
- The K8s apiserver streams logs via HTTP chunked transfer. Idle timeout is `kube-apiserver --request-timeout` (default 1m). Connections idle for >1m get killed by apiserver.
- For TIDE's case (subagent logs are chatty — claude-code stream-json events fire ~1/sec), this is unlikely to bite. But: backend MUST handle EOF gracefully (close cleanly, don't retry forever).
- No CORS configuration on apiserver — backend is the proxy, so CORS only matters for browser → backend (D-D2 architecture).

**Concrete recommendation for planner:**
- Wave 4 plan `cmd/dashboard/api/logs_sse.go` implements the full stream-and-translate pattern with idle timeout + cleanup defer.
- Wave 4 plan includes a unit test asserting: when ctx cancels mid-stream, both the apiserver stream AND the SSE writer close.

### #8 — cobra + Krew distribution

**Question:** goreleaser config for multi-OS/arch Go binaries. Krew plugin manifest schema 2026. `kubectl-tide` symlink convention. Cobra completions through Krew.

**Resolution (HIGH confidence):**

**goreleaser config (verified pattern, Go 1.26 supported):**
- `.goreleaser.yaml` at repo root with:
  ```yaml
  builds:
    - id: tide
      main: ./cmd/tide
      env: [CGO_ENABLED=0]
      goos: [linux, darwin, windows]
      goarch: [amd64, arm64]
      ldflags: ['-s -w -X main.version={{.Version}}']
  archives:
    - id: tide
      builds: [tide]
      name_template: 'tide_{{.Version}}_{{.Os}}_{{.Arch}}'
      formats:
        - format: tar.gz
        - format: zip
          goos: windows
  checksum:
    name_template: 'checksums.txt'
  ```
- GitHub Actions workflow `release.yaml` runs `goreleaser release --clean` on tag push.

**Krew plugin manifest schema (verified, May 2026 still `v1alpha2`):**
```yaml
apiVersion: krew.googlecontainertools.github.com/v1alpha2
kind: Plugin
metadata:
  name: tide
spec:
  version: v0.1.0
  homepage: https://github.com/jsquirrelz/tide
  shortDescription: Manage TIDE Projects on Kubernetes
  description: |
    `tide` is the CLI for TIDE — a Kubernetes-native orchestrator for
    hierarchical agentic coding work.
  platforms:
    - selector:
        matchLabels: { os: linux, arch: amd64 }
      uri: https://github.com/jsquirrelz/tide/releases/download/v0.1.0/tide_v0.1.0_linux_amd64.tar.gz
      sha256: <from goreleaser checksums.txt>
      files:
        - from: tide
          to: .
      bin: tide
    # ... same shape for darwin/amd64, darwin/arm64, linux/arm64, windows/amd64
```
Krew auto-converts dashes in plugin names to underscores for kubectl integration. `tide` (no dashes) stays as-is.

**`kubectl-tide` symlink convention:** Krew installs the binary as `~/.krew/bin/kubectl-tide` (the `kubectl-<name>` prefix is the kubectl plugin contract). When the user runs `kubectl tide ...`, kubectl finds and execs `kubectl-tide`.

**Cobra completions through Krew:**
- cobra's `cobra.Command.Run` for the `completion` subcommand generates bash/zsh/fish/powershell scripts.
- The binary name passed to completion-generation is `os.Args[0]`. When invoked as `kubectl-tide`, the script wires `kubectl_complete-tide`. When invoked as `tide`, wires `_tide`.
- Pitfall 27 (above) discusses the dual-invocation handling. Documented in `docs/cli.md` (Phase 5 draft).

**Concrete recommendation for planner:**
- Wave 3 plan creates `.goreleaser.yaml` with the above config.
- Wave 3 plan creates `krew-plugins/tide.yaml` manifest template (filled in at release time by the `krew-release-bot` GitHub Action).
- Wave 3 plan creates `.github/workflows/release.yaml` wiring goreleaser + krew-release-bot.

### #9 — Bounded-cardinality metric defs in code — central registry

**Question:** Where to register all v1 metrics? How do Phase 1+2 register, and what's the minimal extension?

**Resolution (HIGH confidence):**

**Existing pattern (Phase 2):** `internal/budget/metrics.go` registers `ProviderRateLimitHitsTotal` via `metrics.Registry.MustRegister(...)` in `init()`. The `metrics.Registry` comes from `sigs.k8s.io/controller-runtime/pkg/metrics` — it's the same registry the controller-runtime Manager exposes at the `/metrics` endpoint. So custom metrics automatically scrape with the controller's existing metrics service.

**Phase 4 extension:** `internal/metrics/registry.go` collects all eight Phase 4 counters + one histogram in one file. Each is exported as a package-level variable. `init()` registers all of them via a single `metrics.Registry.MustRegister(c1, c2, c3, ...)` call.

```go
// internal/metrics/registry.go (sketch)
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var dispatchLatencyBuckets = []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800}

var (
    WavesDispatchedTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{Name: "tide_waves_dispatched_total", ...},
        []string{"project", "phase", "plan"},
    )
    TasksCompletedTotal = prometheus.NewCounterVec(... []string{"project", "phase", "plan"})
    TasksFailedTotal    = prometheus.NewCounterVec(... []string{"project", "phase", "plan", "reason"})
    DispatchLatency     = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{Name: "tide_dispatch_latency_seconds", Buckets: dispatchLatencyBuckets},
        []string{"level"},
    )
    SecretLeakBlockedTotal = prometheus.NewCounterVec(... []string{"project", "phase", "plan"}) // W-1
    PushJobsTotal          = prometheus.NewCounterVec(... []string{"project", "outcome"})
    BudgetOverrunsTotal    = prometheus.NewCounterVec(... []string{"project"})
    // ProviderRateLimitHitsTotal stays in internal/budget (Phase 2 — leave it)
)

func init() {
    crmetrics.Registry.MustRegister(
        WavesDispatchedTotal,
        TasksCompletedTotal,
        TasksFailedTotal,
        DispatchLatency,
        SecretLeakBlockedTotal,
        PushJobsTotal,
        BudgetOverrunsTotal,
    )
}
```

**The linter (D-X4) AST-walks this file's `prometheus.New*Vec` calls and validates `[]string{...}` doesn't contain `"task"`.**

**Concrete recommendation for planner:**
- Wave 1 plan creates `internal/metrics/registry.go` with all 7 new metrics + the bucket slice.
- Wave 1 plan does NOT touch `internal/budget/metrics.go` (Phase 2 — leave the `ProviderRateLimitHitsTotal` in its existing home; the centralization in `internal/metrics/` covers NEW Phase 4 work, not existing).
- Wave 2 plan wires the counter `Inc()` calls into reconcilers (boundary-detection sites for W-2 fire `WavesDispatchedTotal.WithLabelValues(...).Inc()`; ProjectReconciler push-result handler increments `SecretLeakBlockedTotal` or `PushJobsTotal` depending on envelope reason).

### #10 — Helm chart additions

**Question:** Verify the existing chart structure. Read-only ClusterRole shape. `tideproject.k8s` API group reference.

**Resolution (HIGH confidence):**

**Existing chart (verified by `ls charts/tide/templates/`):** 38 templates already, including the controller's `manager-rbac.yaml`, `serviceaccount.yaml`, `webhook-service.yaml`, etc. Per-Kind RBAC roles exist (e.g., `project-admin-rbac.yaml`, `project-editor-rbac.yaml`, `project-viewer-rbac.yaml`). Pattern: helmify generates these from kubebuilder markers; hand-edit augments where needed.

**New Helm templates (D-X3):**

1. **`charts/tide/templates/dashboard-deployment.yaml`** — Deployment + Service for `cmd/dashboard/`. Mirrors `deployment.yaml` (controller's) shape. Gated by `{{ if .Values.dashboard.enabled }}` (default true).

2. **`charts/tide/templates/dashboard-rbac.yaml`** — ServiceAccount + ClusterRole + ClusterRoleBinding. ClusterRole verbs:
   ```yaml
   rules:
     - apiGroups: [tideproject.k8s]
       resources: [projects, milestones, phases, plans, tasks, waves]
       verbs: [get, list, watch]
     - apiGroups: ['']
       resources: [pods]
       verbs: [get, list, watch]
     - apiGroups: ['']
       resources: [pods/log]
       verbs: [get]
   ```
   NO wildcards. NO create/update/delete on any resource (read-only ServiceAccount). Same `tideproject.k8s` API group as the controller's RBAC (verified at `manager-rbac.yaml`).

3. **`charts/tide/templates/servicemonitor.yaml`** — Prometheus ServiceMonitor scrape target. Gated by `{{ if .Values.prometheus.serviceMonitor.enabled }}` (default `false`, per CLAUDE.md anti-pattern). Targets the existing `metrics-service.yaml` (already in chart).

**values.yaml additions (D-X3):**
```yaml
dashboard:
  enabled: true
  image:
    repository: ghcr.io/jsquirrelz/tide-dashboard
    tag: v0.1.0-dev
    pullPolicy: IfNotPresent
  replicas: 1
  service:
    type: ClusterIP
    port: 8080

prometheus:
  serviceMonitor:
    enabled: false
    interval: 30s
```

**Concrete recommendation for planner:**
- Wave 6 plan creates the 3 new chart templates + values.yaml extensions.
- Wave 6 plan adds Helm template tests (helm template + yq assertions) for: (a) dashboard-RBAC has zero write verbs, (b) servicemonitor only renders when `prometheus.serviceMonitor.enabled=true`, (c) dashboard-Deployment uses the dashboard SA (not controller SA).

### #11 — dashboard Vite/React/TS toolchain layout

**Question:** Minimal Vite v6 + React 18 + Tailwind v4 + TS config producing <500KB gzipped. `go:embed` for SPA static serving with fallback-to-index. Dev-mode proxy.

**Resolution (HIGH confidence):**

**Vite v6 + React 18 + Tailwind v4 + TS config (verified pattern):**
- `dashboard/web/package.json` — Vite v6, React 18, TypeScript 5, `@xyflow/react@12`, `dagre`, `tailwindcss@4`, `@vitejs/plugin-react`.
- `dashboard/web/vite.config.ts` — uses `@vitejs/plugin-react` and Tailwind v4's Vite plugin (`@tailwindcss/vite`). Production build with `build.target: 'es2020'`. Code-splitting via Vite's defaults.
- `dashboard/web/tsconfig.json` — `strict: true`, target ES2020.
- `dashboard/web/tailwind.config.ts` — Tailwind v4 uses zero-config by default; content paths declared via `@source` directives in CSS.

**`go:embed` for SPA static serving (verified pattern — embed.FS):**
```go
// cmd/dashboard/embed/embed.go
package embed

import "embed"

//go:embed all:dist
var Dist embed.FS
```
SPA fallback (route-based client routing — `/projects/foo/...` URLs must serve `index.html`):
```go
// cmd/dashboard/main.go
r := chi.NewRouter()
r.Handle("/api/*", apiHandler)
fs := http.FileServer(http.FS(embed.Dist))
r.Handle("/*", spaFallback(fs)) // serves index.html for non-existing paths
```
Where `spaFallback` checks for file existence and falls back to serving `dist/index.html`.

**Dev-mode proxy:** `dashboard/web/vite.config.ts` has:
```ts
server: { proxy: { '/api': 'http://localhost:8080' } }
```
Frontend dev (`npm run dev`) runs on Vite's port 5173; API calls proxy to the locally-running `cmd/dashboard` backend on 8080.

**Bundle size verification target:** `vite build` outputs to `dist/`. Gzipped bundle size measured via `gzip -9 dist/assets/index-*.js | wc -c`. Goal <500KB.

**Makefile target:**
```makefile
dashboard-frontend:
	cd dashboard/web && npm ci && npm run build && rm -rf ../../cmd/dashboard/embed/dist && cp -r dist ../../cmd/dashboard/embed/

dashboard-build: dashboard-frontend
	go build -o bin/dashboard ./cmd/dashboard
```

**Concrete recommendation for planner:**
- Wave 4 plan creates `dashboard/web/` with the Vite scaffolding + tailwind v4 setup.
- Wave 4 plan creates the Makefile targets `dashboard-frontend` and `dashboard-build`.
- Wave 5 plan implements the React components.
- Wave 6 plan wires the chart's dashboard Deployment to use the built image.

### #12 — W-1 + W-2 (Phase 3 catch-up)

**Question:** Where Phase 3 left these reconcilers. Shared `buildCommitMessage` cleanly. Exit-10 vs exit-11.

**Resolution (HIGH confidence):**

**W-1 — `tide_secret_leak_blocked_total`:**
- **Counter registration** — naturally lives in `internal/metrics/registry.go` (per Q9).
- **Exit-code mapping** — `cmd/tide-push/main.go:331` references the counter in comments but doesn't increment it. The push Job exits with code 10 for `reason="leak-detected"` and code 11 for `reason="lease-rejected"`. Today's ProjectReconciler maps both uniformly to `PhasePushLeaseFailed` (`internal/controller/project_controller.go:431`). Phase 4 must:
  1. Read push-result envelope's `reason` field after Job termination.
  2. On exit-10: set `Status.Phase=PhasePushLeakBlocked` (new constant) + `Condition=PushLeakBlocked` + increment `metrics.SecretLeakBlockedTotal.WithLabelValues(project.Name, phase, plan).Inc()`.
  3. On exit-11: existing `PhasePushLeaseFailed` path unchanged.
- **Phase constant:** add `PhasePushLeakBlocked = "PushLeakBlocked"` to `api/v1alpha1/project_types.go` next to the existing Phase constants (line 297-316).

**W-2 — Mid-stack boundary triggers:**
- **`buildCommitMessage` already exists** at `internal/controller/push_helpers.go:335` and handles all 4 D-B2 shapes (verified by grep + reading the function). It's fully tested.
- **Current dispatch site:** ProjectReconciler-only at `internal/controller/project_controller.go:385` (fires at `Status.Phase=Complete`). Calls `buildCommitMessage("project", "")` and creates a `tide-push-{project-uid}` Job.
- **Phase 4 extension:** MilestoneReconciler, PhaseReconciler, PlanReconciler each need a "boundary detection" step. Pattern:
  ```go
  // In MilestoneReconciler.handleJobCompletion, BEFORE patchMilestoneSucceeded:
  // Check whether THIS Milestone's all children (Phases) are Succeeded.
  // If yes, dispatch a push Job with buildCommitMessage("milestone", ms.Name).
  // Use deterministic job name `tide-push-{project-uid}` (D-B5 serialization).
  ```
- **Sharing seam:** The boundary-detection logic shares the gate-policy check site. Pattern in `internal/gates/policy.go`:
  ```go
  // BoundaryDetected returns true when ALL child CRDs of the given kind under this parent are Succeeded.
  func BoundaryDetected(ctx context.Context, c client.Client, parent client.Object, childKind string) (bool, error) { ... }
  ```
- **Push Job serialization (D-B5):** Multiple level boundaries in flight at the same time may all try to create `tide-push-{project-uid}`. K8s API server's `AlreadyExists` handles this — losers requeue. The push Job picks up all unstaged diffs since the previous push, so serialization is fine. **Caveat:** the commit message will be whichever boundary fired first — message is informational, the diff is authoritative.

**Concrete recommendation for planner:**
- Wave 1 plan adds `PhasePushLeakBlocked` constant + `internal/metrics/registry.go`.
- Wave 2 plan implements `internal/gates/policy.go` `BoundaryDetected` + gate-policy evaluation.
- Wave 2 plan extends each up-stack reconciler with: (a) gate-policy hook, (b) boundary push trigger, (c) exit-10 vs exit-11 envelope reason parsing.
- Wave 2 plan extends `cmd/tide-push/main.go` push-result envelope schema (if not already done in Phase 3) with `reason` field at exit time.

### #13 — Validation Architecture (Nyquist Dimension 8) for Phase 4

**Question:** Validation framework PER requirement family.

**Resolution (HIGH confidence):** See dedicated section `## Validation Architecture` below.

## Runtime State Inventory

**Not applicable** — Phase 4 is a new-feature phase, not a rename/refactor/migration phase. No string renames, no stored data migrations, no OS-registered state churn. Schema additions (e.g., `PhasePushLeakBlocked` constant) are additive and don't affect existing CRD data.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|-------------|-----------|---------|----------|
| Go toolchain | All Phase 4 Go code | ✓ (project policy) | 1.26 | — |
| Node.js | `dashboard/web/` Vite build | ✗ user-installed | needs v22+ | Vite v6 requires Node 22+; planner adds a `make verify-node` target with clear error message |
| npm | Frontend dependency install | ✗ user-installed | needs v10+ (bundled with Node 22) | — |
| goreleaser | Release pipeline | ✗ GitHub-Actions-only | v2.x | Not needed locally; CI installs |
| docker | Building dashboard image | ✓ (project policy) | latest | — |
| kind / kubectl | Helm chart smoke testing | ✓ (project policy, Phase 02.2) | v0.31.0 / 1.36 | — |
| OTel Collector | Tracing backend | ✗ optional | n/a | TIDE works without; D-O3 commits to optional. |

**Missing dependencies with no fallback:** None blocking. Node 22+ is user-installable per the same path as Go (brew install node).

**Missing dependencies with fallback:**
- OTel Collector — backend not required; no-op TracerProvider when endpoint unset.
- Prometheus instance — metrics endpoint exists regardless of whether Prometheus scrapes it.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework (Go) | Ginkgo v2.28 + Gomega (already in use) |
| Framework (TS) | Vitest (Vite-native test runner) for unit tests; Playwright deferred to v1.x |
| Config files | `internal/controller/suite_test.go` (envtest harness exists); `dashboard/web/vitest.config.ts` (NEW Wave 0) |
| Quick run command | `make test` (Go: 30s budget) + `cd dashboard/web && npm run test` |
| Full suite command | `make test test-int test-int-kind` (Go full: <5min) + `npm run test --coverage` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| GATE-01 | `Project.Spec.gates` CEL enum validation | unit (envtest) | `go test -run TestGatesCELValidation ./api/v1alpha1/...` | ❌ Wave 0 |
| GATE-02 | Wave pauses when `PauseBetweenWaves=true` | integration (envtest) | `go test -run TestWavePauseBetweenWaves ./internal/controller/...` | ❌ Wave 0 |
| GATE-03 | `tide approve` clears annotation; reconciler advances | integration | `go test -run TestApproveAnnotationClears ./internal/controller/...` | ❌ Wave 0 |
| OBS-01 | Logs are valid JSON with required fields | unit | `go test -run TestStructuredLogFields ./cmd/manager/...` | ❌ Wave 0 |
| OBS-02 | All metrics labels ⊆ {project, phase, plan, reason} | static analysis | `go run ./cmd/tide-lint ./...` (metric-cardinality analyzer) | ❌ Wave 0 |
| OBS-02 | `/metrics` endpoint exposes all 8 counters + 1 histogram | integration | `go test -run TestMetricsEndpointShape ./internal/metrics/...` | ❌ Wave 0 |
| OBS-03 | Trace tree spans Project→Milestone→Phase→Plan→Task | unit (tracetest) | `go test -run TestDispatchSpanTree ./internal/controller/...` (uses `go.opentelemetry.io/otel/sdk/trace/tracetest`) | ❌ Wave 0 |
| OBS-04 | OpenInference attribute names match spec exactly | unit | `go test -run TestOpenInferenceKeys ./pkg/otelai/...` | ❌ Wave 0 |
| OBS-05 | Span attrs contain ArtifactPath, not raw payload | unit | `go test -run TestNoInlinePayload ./pkg/otelai/...` | ❌ Wave 0 |
| OBS-06 | ServiceMonitor renders only when enabled | helm template | `helm template charts/tide --set prometheus.serviceMonitor.enabled=true \| grep -q ServiceMonitor` | ❌ Wave 0 |
| CLI-01 | `tide apply -f` calls K8s apply | unit (fake client) | `go test -run TestApplyCommand ./cmd/tide/...` | ❌ Wave 0 |
| CLI-02 | All 10 subcommands registered | unit | `go test -run TestSubcommandsRegistered ./cmd/tide/...` | ❌ Wave 0 |
| CLI-03 | `tide inspect-wave` formats table correctly | unit | `go test -run TestInspectWaveTable ./cmd/tide/...` | ❌ Wave 0 |
| CLI-04 | `tide tail` streams via `pods/log` | unit (fake client) | `go test -run TestTailStreamsLogs ./cmd/tide/...` | ❌ Wave 0 |
| DASH-01 | Dashboard ClusterRole has zero write verbs | helm template + yq | `helm template charts/tide \| yq 'select(.kind == "ClusterRole" and .metadata.name == "tide-dashboard") .rules[].verbs'` | ❌ Wave 0 |
| DASH-02 | DAG renders both Planning and Execution side-by-side | TS unit (Vitest + React Testing Library) | `cd dashboard/web && npm run test -- DAG` | ❌ Wave 0 |
| DASH-03 | SSE handler streams events with heartbeat | integration | `go test -run TestSSEHeartbeat ./cmd/dashboard/...` | ❌ Wave 0 |
| DASH-04 | Log stream closes on idle timeout | unit | `go test -run TestLogStreamIdleTimeout ./cmd/dashboard/...` | ❌ Wave 0 |
| DASH-05 | Backend registers zero non-GET routes | unit | `go test -run TestNoMutationRoutes ./cmd/dashboard/...` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -run <specific test> ./pkg/<changed-pkg>/...`
- **Per wave merge:** `make test` + `cd dashboard/web && npm run test` (<60s combined)
- **Phase gate:** `make test test-int test-int-kind` (full suite) + dashboard Playwright smoke (deferred v1.x, replaced by Vitest + Helm template tests for v1.0).

### Wave 0 Gaps (test infrastructure to create)
- [ ] `internal/gates/policy_test.go` — gate-policy evaluation unit tests
- [ ] `internal/gates/annotation_test.go` — annotation watch unit tests
- [ ] `internal/metrics/registry_test.go` — counter registration unit tests
- [ ] `internal/otelinit/provider_test.go` — TracerProvider no-op fallback test
- [ ] `pkg/otelai/attrs_test.go` — OpenInference key string assertions
- [ ] `tools/analyzers/metriccardinality/analyzer_test.go` + `testdata/` — analyzer fixture
- [ ] `cmd/tide/<subcmd>_test.go` × 10 — subcommand tests with fake client
- [ ] `cmd/dashboard/api/*_test.go` — chi route tests + SSE assertions
- [ ] `cmd/dashboard/hub/pubsub_test.go` — fan-out + drop-oldest test
- [ ] `dashboard/web/src/components/*.test.tsx` — Vitest + React Testing Library
- [ ] `dashboard/web/vitest.config.ts` — Vitest configuration
- [ ] `test/integration/envtest/gates_test.go` — multi-CRD gate-flow integration
- [ ] `test/integration/envtest/observability_test.go` — counter increment + log JSON assertions
- [ ] `test/integration/envtest/sse_test.go` — backend SSE handler with real informer

## Security Domain

Phase 4 security profile (`security_enforcement` assumed enabled — no `config.json` override observed):

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V2 Authentication | yes | K8s SA-based — dashboard uses dashboard-SA token (in-cluster); CLI reuses kubeconfig. No new auth surface. |
| V3 Session Management | partial | SSE connections are stateless per-connection. Dashboard backend doesn't issue session tokens. Browser inherits no session state. |
| V4 Access Control | yes | Dashboard ClusterRole is GET/LIST/WATCH only — RBAC enforced by apiserver. CLI inherits user's kubeconfig RBAC. |
| V5 Input Validation | yes | CEL validation on `Project.Spec.Gates` enum. CLI validates `--wave N` flag is integer. Helm chart values typed. |
| V6 Cryptography | no | No new cryptographic primitives in Phase 4. (Phase 2's HMAC signing-key + Phase 3's gitleaks remain.) |
| V7 Errors / Logging | yes | OBS-01 mandates structured JSON. Sensitive fields (ANTHROPIC_API_KEY, GIT_PAT) NEVER logged — Phase 2's redact package (`internal/harness/redact/`) is already enforced for subagent output. Orchestrator-side logs MUST NOT log Secret values. |
| V9 Communications | yes | SSE over HTTPS (operator's Ingress provides TLS). Dashboard `→` apiserver via in-cluster k8s API (TLS by default). CLI `→` apiserver via kubeconfig (TLS). |
| V13 API and Web Service | yes | Dashboard API exposes only GET handlers (DASH-05 enforced). No CSRF tokens needed (read-only). |

### Known Threat Patterns for the TIDE stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Secret value in log/metric/span | Information disclosure | Pitfall 18 / redact package (Phase 2); span attribute helpers in `pkg/otelai` ban payload-inline (D-O5 — ArtifactPath only) |
| Cardinality explosion (DoS Prometheus) | Denial of service | Pitfall 17 / D-X4 lint analyzer (compile-time check) |
| Dashboard SSE leak (DoS orchestrator) | Denial of service | Pitfall 22 / per-stream idle timeout + active-stream cap |
| CLI talks to wrong cluster | Tampering / privilege confusion | kubeconfig context displayed in CLI startup banner; `--context` flag explicit. Document operator workflow in docs/cli.md. |
| Dashboard SA token leakage | Information disclosure | Token never leaves backend pod; browser sees only API responses (D-D2 in-process proxy). |
| OTLP endpoint exfiltration | Information disclosure | OTel headers can carry sensitive tokens (`OTEL_EXPORTER_OTLP_HEADERS`); operator must use a Secret-backed env var, NOT inline in values.yaml. Document in `docs/observability.md`. |
| Reject-annotation tampering | Denial of service | RBAC: `update;patch` on Projects is grant required to write reject annotation. Standard K8s RBAC enforced; no Phase 4 work needed. |
| Cross-Project metric leakage | Information disclosure | Project name as a metric label is acceptable — operator's existing RBAC governs who can query Prometheus. NOT a Phase 4 concern. |

## Files Likely to Be Created/Modified

### Newly created
**Go:**
- `pkg/otelai/{attrs,doc}.go` + `_test.go` — OpenInference attribute helpers (D-O4)
- `internal/gates/{policy,annotation,doc}.go` + `_test.go` — gate evaluation (D-G2)
- `internal/metrics/registry.go` + `_test.go` — central metric registry (D-O2)
- `internal/otelinit/provider.go` + `_test.go` — TracerProvider lifecycle (D-O3)
- `tools/analyzers/metriccardinality/{analyzer,doc}.go` + `_test.go` + `testdata/` — metric-cardinality lint (D-X4)
- `cmd/tide/{main,apply,watch,tail,approve,reject,cancel,resume,inspect_wave,artifact_get,describe_budget,root_flags}.go` + `_test.go` × ~11 — CLI (D-C1..C4)
- `cmd/dashboard/main.go` + `api/{projects,events_sse,logs_sse}.go` + `hub/pubsub.go` + `embed/embed.go` + `_test.go` files — dashboard backend (D-D1..D5)
- `test/integration/envtest/{gates,observability,sse,boundary_push}_test.go` — Phase 4 integration tests

**Frontend (`dashboard/web/`):**
- `package.json`, `vite.config.ts`, `tailwind.config.ts`, `tsconfig.json`, `index.html`
- `src/main.tsx`, `src/App.tsx`
- `src/components/{PlanningDAG,ExecutionDAG,ProjectNode,MilestoneNode,PhaseNode,PlanNode,TaskNode,LogStream}.tsx` + `.test.tsx`
- `src/lib/{sse,layout}.ts` + `.test.ts`
- `vitest.config.ts`

**Helm chart:**
- `charts/tide/templates/dashboard-deployment.yaml`
- `charts/tide/templates/dashboard-rbac.yaml`
- `charts/tide/templates/servicemonitor.yaml`

**Release pipeline:**
- `.goreleaser.yaml`
- `.github/workflows/release.yaml`
- `krew-plugins/tide.yaml` (Krew manifest template)

**Docs (Phase 5 finalizes; Phase 4 stubs):**
- `docs/cli.md` (Phase 4 stub — CLI verb reference)
- `docs/dashboard.md` (Phase 4 stub — install + ingress notes)
- `docs/observability.md` (Phase 4 stub — OTel + Prometheus config)
- `docs/gates.md` (Phase 4 stub — gate-policy reference)

### Modified (existing files extended)
- `api/v1alpha1/project_types.go` — add `PhasePushLeakBlocked` constant (W-1)
- `internal/controller/project_controller.go` — exit-10 vs exit-11 mapping (W-1); gate-policy hook
- `internal/controller/milestone_controller.go` — gate-policy hook + boundary push trigger (D-G2, D-W2)
- `internal/controller/phase_controller.go` — same as milestone
- `internal/controller/plan_controller.go` — same; plus wave-pause-between-waves hook (GATE-02)
- `internal/controller/wave_controller.go` — wave-pause annotation watcher (D-G3)
- `internal/controller/task_controller.go` — task-gate hook (D-G2 task level)
- `cmd/manager/main.go` — wire `otelinit.NewTracerProvider`, register dashboard runnable? NO — dashboard is a separate binary (D-X2); only adds OTel init + metrics-registry init via `import _ "github.com/jsquirrelz/tide/internal/metrics"`
- `cmd/manager/env.go` — add `OTEL_EXPORTER_OTLP_ENDPOINT` etc. env reading (or rely on OTel SDK's native env-var support — research recommends the latter; no Go code change needed)
- `cmd/tide-lint/main.go` — add `metriccardinality.Analyzer` to multichecker list
- `cmd/tide-push/main.go` — extend push-result envelope with `reason` field at exit time (likely already present in Phase 3 03-06; verify in Wave 1)
- `charts/tide/values.yaml` — add `dashboard.*`, `prometheus.serviceMonitor.*`, `otel.*` values
- `Makefile` — add `dashboard-frontend`, `dashboard-build`, `tide-cli`, `release-snapshot` targets
- `go.mod` — add cobra, otel/sdk, otel/trace/noop, otel/exporters/otlp/otlptrace/otlptracegrpc, cli-runtime

## Pitfalls Discovered

Phase 4 adds 5 new Pitfalls to the 22 carried forward from Phases 1-3:

- **Pitfall 23** — SSE-through-ingress buffering (nginx + others)
- **Pitfall 24** — OTel sampler env-var sentinel confusion (`WithSampler` overrides env)
- **Pitfall 25** — cobra subcommand context-cancel propagation (Ctrl-C doesn't propagate without `ExecuteContext`)
- **Pitfall 26** — React Flow v12 layout-flicker on dynamic node insertion
- **Pitfall 27** — Krew plugin name vs. cobra `Use:` mismatch (`kubectl-tide` vs. `tide`)

All five are documented with diagnosis pattern and prevention strategy in the Common Pitfalls section above.

## Assumptions Log

All major claims in this RESEARCH.md are either verified against current sources (Context7 lookup for OpenInference + React Flow + cobra; WebFetch for OTel sampler spec + Krew schema; in-repo grep for existing patterns) or cited to STACK.md (which itself is HIGH-confidence research from May 2026). The following claims are tagged [ASSUMED] and should be re-verified during the planning step:

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Dashboard bundle gzipped ~280KB (well under 500KB target) | Open Q1 | Bundle may exceed 500KB if Tailwind v4 purge is misconfigured. Mitigation: Wave 5 plan adds bundle-size CI gate. |
| A2 | `go.opentelemetry.io/contrib/samplers/autosampler` exists and supports env-driven config | Open Q3 | If the contrib package is renamed or moved, fall back to the SDK's native env support (which IS verified). No functional impact. |
| A3 | Vite v6 + Node v22 is the May 2026 stable combination | Open Q11 | Possible Node 23 LTS shift; mitigation: pin Node version in `dashboard/web/.nvmrc`. |
| A4 | goreleaser v2.x supports Go 1.26 builds | Open Q8 | Verified via goreleaser docs but not in this repo's CI. Wave 3 plan adds `goreleaser check` as CI step. |
| A5 | nginx-ingress is the dominant reverse-proxy operators put in front of the dashboard | Pitfall 23 | Other ingresses (Traefik, HAProxy, Istio gateway) have different buffering defaults; mitigation: docs cover the common cases. |

All other claims are VERIFIED or CITED — no user confirmation needed.

## Open Questions (RESOLVED)

1. **Should the dashboard image ship as the SAME image as the controller-manager (shared go.mod), or as a separate image?**
   - Recommendation: SEPARATE image (`ghcr.io/jsquirrelz/tide-dashboard`). Same go.mod (D-X2 already locks this), but separate `cmd/dashboard/Dockerfile` and `go build` target. Reasons: (a) dashboard SA has different RBAC than controller; mixing in one Pod would over-grant; (b) dashboard can scale independently from controller; (c) embed.FS bundle keeps dashboard image self-contained for simpler upgrade story.
   - **Resolution:** Lock SEPARATE image in plan; reflect in Helm chart with `dashboard-deployment.yaml` separate from controller `deployment.yaml`.

2. **Should `tide approve` write the annotation directly via the K8s API, or shell out to `kubectl annotate`?**
   - Recommendation: write directly via client-go. Reasons: (a) consistent error handling, (b) no kubectl process dependency, (c) cobra completion can validate Project name + wave existence before the write.
   - **Resolution:** Lock direct-client-go approach in plan.

3. **For W-2 mid-stack push triggers, should the boundary-detection helper live in `internal/gates/` (couples GATE work to push work) or `internal/controller/push_helpers.go` (extends Phase 3 push helpers)?**
   - Recommendation: `internal/gates/boundary.go` — the same function is consulted by BOTH the gate-policy code path AND the push-trigger code path. Co-locating makes the shared seam visible.
   - **Resolution:** Lock `internal/gates/boundary.go` placement in plan.

## Recommendations Summary

One-liner-per-decision for the planner to encode as task constraints or test acceptance criteria:

- **GATE-01:** Schema already exists at `api/v1alpha1/project_types.go:47-64`; Phase 4 ships consumer code only — Plan asserts `helm template` produces correct CEL enum validation.
- **GATE-02:** `PauseBetweenWaves` field already on `Gates` struct; WaveReconciler consults at wave boundary inside `Status.Phase` transitions.
- **GATE-03:** Annotation watch via `client.Watches(&Project{}, ...)` with annotation predicate; `tide approve/reject/resume` write via client-go directly.
- **OBS-01:** Enforce structured field set `{project, phase, plan, task}` in all `logger.Info/Error` calls; no new logging library needed.
- **OBS-02:** Centralize in `internal/metrics/registry.go`; 8 counters + 1 histogram with locked label sets; D-X4 analyzer enforces.
- **OBS-03:** `internal/otelinit/provider.go` with no-op fallback when `OTEL_EXPORTER_OTLP_ENDPOINT` unset; reconcilers call `tracer.Start("tide.dispatch.<level>", ...)`.
- **OBS-04:** `pkg/otelai/attrs.go` with 5 helpers returning `[]attribute.KeyValue`; OpenInference key strings asserted by unit test.
- **OBS-05:** `pkg/otelai.ArtifactPath(pvcPath)` is the only span attribute carrying payload reference; no inline-payload helper exists in the public API.
- **OBS-06:** `charts/tide/templates/servicemonitor.yaml` gated by `{{ if .Values.prometheus.serviceMonitor.enabled }}` (default false).
- **CLI-01:** `cmd/tide/main.go` uses cobra v1.9.x + `cli-runtime/genericclioptions` for kubeconfig.
- **CLI-02:** Ten subcommands per `cmd/tide/{verb}.go` file; cobra `completion` subcommand from stdlib.
- **CLI-03:** `inspect-wave` uses `text/tabwriter` with column set NAME/STATUS/AGE/ATTEMPT/SCHEDULED-IN-WAVE.
- **CLI-04:** `tail` uses `clientset.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{Follow:true}).Stream(ctx)`.
- **DASH-01:** Separate Deployment + SA via `charts/tide/templates/dashboard-{deployment,rbac}.yaml`.
- **DASH-02:** React Flow v12 + dagre with `useNodesInitialized` hook for layout trigger.
- **DASH-03:** chi router + `http.Flusher` SSE handler; in-process pubsub hub fed by controller-runtime informer.
- **DASH-04:** SSE-translated `pods/log`; 5-min idle timeout + ctx-cancel cleanup defer.
- **DASH-05:** Backend registers GET handlers only; lint guard via Wave 6 test that greps for non-GET registrations.
- **D-W1:** New `PhasePushLeakBlocked` constant + `tide_secret_leak_blocked_total` counter; ProjectReconciler reads push envelope `reason` field.
- **D-W2:** `internal/gates/boundary.go::BoundaryDetected(...)` is shared seam for both gate-policy check AND push-trigger detection.
- **D-X1:** All reconciler log/metric/span emissions use the same `{project, phase, plan}` field set.
- **D-X2:** Same go.mod; `cmd/dashboard/embed/dist` populated by Makefile from `dashboard/web/dist`.
- **D-X3:** Three Helm templates added; values defaults `dashboard.enabled=true`, `prometheus.serviceMonitor.enabled=false`.
- **D-X4:** `tools/analyzers/metriccardinality/analyzer.go` follows `providerfirewall` pattern; CI gate via `make tide-lint`.

## Sources

### Primary (HIGH confidence)
- Context7 `/arize-ai/openinference` — OpenInference attribute names + flat-encoding spec (fetched 2026-05-16)
- Context7 `/xyflow/xyflow` — React Flow v12 component-as-node + dagre layout + useNodesInitialized hook (fetched 2026-05-16)
- Context7 `/spf13/cobra` — cobra v1.9.1 subcommand patterns (lookup 2026-05-16)
- OpenTelemetry spec `opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/` — OTEL_TRACES_SAMPLER values + _ARG handling (verified May 2026)
- OpenTelemetry GenAI semconv `opentelemetry.io/docs/specs/semconv/gen-ai/` — confirmed Development status (verified May 2026)
- Krew docs `krew.sigs.k8s.io/docs/developer-guide/plugin-manifest/` — v1alpha2 schema; no 2026 schema changes (verified May 2026)
- `.planning/research/STACK.md` — pinned versions for controller-runtime v0.24, otel v1.43, prometheus client_golang v1.23, React 18, React Flow v12 (May 2026)
- `.planning/research/PITFALLS.md` — Pitfalls 17, 18, 22, 23, 24 references
- In-repo: `cmd/manager/main.go`, `internal/budget/metrics.go`, `internal/controller/push_helpers.go`, `internal/controller/milestone_controller.go`, `charts/tide/values.yaml`, `tools/analyzers/{crosspool,providerfirewall}/analyzer.go`, `api/v1alpha1/project_types.go` — verified existing patterns and seams

### Secondary (MEDIUM confidence)
- Vite v6 + Tailwind v4 May-2026 stability — inferred from public release notes; planner re-verifies at Wave 4 implementation
- goreleaser v2 Go 1.26 support — inferred from goreleaser changelog; planner re-verifies at Wave 3
- Dashboard bundle gzipped size estimate — extrapolated from Argo Workflows + Headlamp comparables; planner adds CI bundle-size gate at Wave 5

### Tertiary (LOW confidence)
- nginx-ingress as dominant operator-side ingress — based on K8s community survey data through 2024; mitigation = document multiple ingress controllers in `docs/dashboard.md`

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every library verified via Context7 lookup or STACK.md citation; versions current as of May 2026
- Architecture: HIGH — every decision flows from locked CONTEXT.md decisions or verified library shapes; no novel inventions
- Pitfalls: HIGH for the 5 new pitfalls — each verified against concrete library/spec behavior (OTel env vars, nginx buffering, cobra context, React Flow layout, Krew naming)
- Validation Architecture: HIGH — concrete Ginkgo/Vitest framework choices that match existing repo conventions
- Runtime State Inventory: N/A — Phase 4 is new-feature work

**Research date:** 2026-05-16
**Valid until:** 2026-06-13 (30 days for stable libraries; OpenInference spec churn would necessitate re-verification)

---

## RESEARCH COMPLETE
