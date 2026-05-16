# Phase 4: Gates, Observability, Dashboard, CLI - Context

**Gathered:** 2026-05-16
**Status:** Ready for research/planning
**Depends on:** Phase 3 (Up-stack reconcilers, pkg/git, gitleaks, real Claude image, chaos-resume — APPROVED conditional with W-1/W-2 deferred and now folded here)

<domain>
## Phase Boundary

Light up four orthogonal but co-located surfaces on top of the Phase 3 reconciler stack:

- **Gates** (GATE-01..03): Per-level human gate policy declared on `Project.Spec.gates` with values `auto | approve | pause` per level (milestone/phase/plan/task) plus `pauseBetweenWaves` boolean. Reconcilers consult policy at every level boundary before auto-advancing. `tide approve` and `tide reject` are the unblock verbs.
- **Observability** (OBS-01..06): zap-behind-logr structured JSON logs from orchestrator + subagent pods; Prometheus metrics with bounded cardinality (label-stopped at `project/phase/plan`); OTel tracing across the dispatch chain; hand-rolled `pkg/otelai` emits OpenInference attribute names on OTel spans (no Go OpenInference SDK exists in 2026); tail-sampling on by default; LLM payloads referenced as PVC artifact paths, never inlined as span attrs. `ServiceMonitor` ships gated behind `prometheus.serviceMonitor.enabled`.
- **CLI** (CLI-01..04): `tide` is a stateless cobra-based client (no local cache) talking to the K8s API with `apply / watch / tail / approve / reject / cancel / resume / inspect-wave / artifact-get` verbs. `tide tail` streams via the K8s `pods/log` subresource.
- **Dashboard** (DASH-01..05): Read-only React dashboard ships as a separate `Deployment` + read-only `ServiceAccount` distinct from the orchestrator's. Renders the Planning DAG and Execution DAG side-by-side via React Flow v12 + dagre + Tailwind v4. Status updates stream over SSE (uni-directional, proxy-friendly). Per-task log streaming opt-in via `pods/log` WebSocket proxy. **Zero mutation endpoints** — all state changes route through `kubectl` or `tide` CLI.

Phase 4 also CLOSES OUT TWO Phase 3 deferred items (folded here):

- **W-1**: Register `tide_secret_leak_blocked_total` Prometheus counter (lives naturally in OBS-02 metrics work); distinguish push-Job exit code 10 (gitleaks finding) from exit 11 (lease rejected) in ProjectReconciler's `PhasePushLeaseFailed` mapping so the right counter increments and the right Condition fires.
- **W-2**: Mid-stack boundary triggers — currently ProjectReconciler fires a push Job ONLY at `Project.Status.Phase=Complete`. Wire `MilestoneReconciler` / `PhaseReconciler` / `PlanReconciler` to trigger a push at their all-children-Succeeded boundary so the 4 D-B2 commit-message shapes (`tide: plan <name> authored + executed`, `tide: phase X authored`, `tide: milestone X authored`, `tide: project complete`) fire at the right times. This couples naturally to GATE-01..03 (the same boundary-detection logic that consults gate policy is the place that triggers push).

Phase 4 does NOT: ship dashboard mutation endpoints (DASH-05 locks read-only; mutations route through CLI/kubectl); add per-CRD gate overrides (Project-CRD-only per D-G1); ship a Go OpenInference SDK (none exists 2026 — hand-rolled `pkg/otelai`); replace OTel collector tail-sampling (collector remains optional; SDK-level is the default per D-O2); ship a multi-cluster CLI (single-cluster kubeconfig per D-C1); add OIDC/SSO dashboard auth (v1.x); broaden subagent provider matrix (`internal/subagent/openai/` etc. remain v1.x community work — Phase 3 D-C1 layering is the contract).

</domain>

<decisions>
## Implementation Decisions

### Gates (GATE-01..03)

- **D-G1:** Gate policy lives on `Project.Spec.gates` ONLY. Single source per Project — applies uniformly to every Milestone/Phase/Plan under that Project. No per-CRD override field on Milestone/Phase/Plan/Task CRDs. Matches ROADMAP wording verbatim ("configurable on the Project CRD"). Schema:
  ```go
  type GateConfig struct {
    Milestone string `json:"milestone,omitempty"` // auto | approve | pause; default: approve
    Phase     string `json:"phase,omitempty"`     // auto | approve | pause; default: auto
    Plan      string `json:"plan,omitempty"`      // auto | approve | pause; default: auto
    Task      string `json:"task,omitempty"`      // auto | approve | pause; default: auto
    PauseBetweenWaves bool `json:"pauseBetweenWaves,omitempty"` // default: false
  }
  ```
  CEL-validated `enum: [auto, approve, pause]` per level. Default policy = `{milestone: approve, phase: auto, plan: auto, task: auto, pauseBetweenWaves: false}` — least-friction sane default.

- **D-G2:** Reconcilers consult `Project.Spec.gates` at every level boundary. On `approve`, the reconciler sets the child CRD's `Status.Phase=AwaitingApproval` + emits a `WaveOrLevelPaused` Condition and stops dispatching. On `pause`, same but no auto-advance even with approval (requires explicit `tide resume`). On `auto`, advance immediately (today's behavior).

- **D-G3:** Slack-tide between-wave review unblocks via `tide approve --wave <plan>/<N>` (preferred CLI surface) — the CLI writes the annotation `tideproject.k8s/approve-wave-<N>: true` on the Plan CRD. Reconciler watches for that annotation and clears the wave pause. `kubectl annotate plan foo tideproject.k8s/approve-wave-1=true` is the equivalent kubectl path — one mental model, two access surfaces. Avoids new CRD types (no `WaveApproval` CRD) and stays declarative.

- **D-G4:** `tide reject <project>` halts the run: writes `tideproject.k8s/reject: <reason>` annotation; reconciler sets `Project.Status.Phase=Rejected` + halts dispatch + leaves resources in place for human inspection. `tide resume` clears the annotation and re-enters reconciliation. `tide cancel` is destructive (cascades delete to children + cleans PVC); requires `--force` to confirm.

### Observability (OBS-01..06)

- **D-O1:** Logging — zap behind logr; controller-runtime's `ctrl.Log` is the seam (Phase 1+2 already use it). Subagent pods (`internal/harness/`) also use logr+zap. Output format: JSON with `level`, `ts`, `caller`, `msg`, plus structured fields (project, phase, plan, task — same labels as metrics for grep-correlation). No new logging library; just enforce structure via lint rule.

- **D-O2:** Metrics — Prometheus via `client_golang` (per STACK.md / CLAUDE.md). Bounded-cardinality labels: `project`, `phase`, `plan` (no `task` — Pitfall 17 mitigation). Counters that ship in v1:
  - `tide_waves_dispatched_total{project, phase, plan}` (counter)
  - `tide_tasks_completed_total{project, phase, plan}` (counter)
  - `tide_tasks_failed_total{project, phase, plan, reason}` (counter; reason ∈ {exit-1, gitleaks, lease, auth, internal, budget})
  - `tide_dispatch_latency_seconds{level=milestone|phase|plan|task}` (histogram)
  - `tide_provider_rate_limit_hits_total{project, vendor}` (counter; vendor cardinality is single-digit)
  - `tide_secret_leak_blocked_total{project, phase, plan}` (counter — W-1 lands here)
  - `tide_push_jobs_total{project, outcome}` (counter; outcome ∈ {success, leak, lease, auth, internal})
  - `tide_budget_overruns_total{project}` (counter — Phase 2 D-D2 already tracks the data)

  All metrics registered at `cmd/manager/main.go` via prometheus.NewRegistry; orchestrator's manager Runnable exposes `/metrics` on the existing controller-runtime metrics port.

- **D-O3:** Tracing — OTel SDK behind a thin `pkg/otelai` package. **Sampler:** SDK-level `ParentBased(TraceIDRatioBased(0.1))` by default, env-overridable via `OTEL_TRACES_SAMPLER`/`OTEL_TRACES_SAMPLER_ARG` (per OTel spec). Collector remains optional — production setups can swap in OTel Collector with its `tail_sampling` processor for richer policies (error-based, latency-based), but v1.0 does NOT require a collector to function.

- **D-O4:** **`pkg/otelai` shape — THIN attribute helpers.** Public package exports:
  ```go
  // Returns OTel attribute.KeyValue slices matching OpenInference convention.
  func LLMInputMessages(msgs []Message) []attribute.KeyValue
  func LLMOutputMessages(msgs []Message) []attribute.KeyValue
  func TokenCount(prompt, completion, cacheRead, cacheCreation int) []attribute.KeyValue
  func AgentInvocation(name, role, level string) []attribute.KeyValue
  func ArtifactPath(path string) attribute.KeyValue  // OBS-05: PVC path ref, not inlined payload
  ```
  Callers create spans themselves via standard `go.opentelemetry.io/otel/trace` API and apply these attrs. Minimal surface, easy to evolve when OpenInference spec changes. NO span-wrapping DSL; NO opinionated lifecycle helpers.

- **D-O5:** **No LLM payloads in span attrs.** Per OBS-05, span attributes carry only the PVC artifact path reference (e.g., `attr.ArtifactPath("/workspace/envelopes/<task-uid>/events.jsonl")`). Full streaming-event log stays on the PVC; trace consumers fetch on demand. Bounds span size (etcd-friendly; collector-friendly).

- **D-O6:** ServiceMonitor — `charts/tide/templates/servicemonitor.yaml` gated by `.Values.prometheus.serviceMonitor.enabled` (default `false`, per CLAUDE.md anti-pattern "Default the chart's `ServiceMonitor` to `prometheus.enabled=false` to avoid CRD-not-found on plain clusters"). When true, ServiceMonitor scrapes the controller-manager `/metrics` endpoint.

### CLI (CLI-01..04)

- **D-C1:** `tide` is a stateless cobra-based CLI (no local cache). Talks directly to the K8s API using the standard kubeconfig resolution chain (`$KUBECONFIG` env → `~/.kube/config` → in-cluster SA). NO `tide login` flow — reuses existing kubectl authentication so `tide` works wherever `kubectl get pods` works. Single binary `cmd/tide/`.

- **D-C2:** Distribution: GitHub Releases (per OS/arch via goreleaser) + Krew plugin manifest so `kubectl tide ...` works after `kubectl krew install tide`. Container image (`ghcr.io/jsquirrelz/tide-cli:vX`) ships as a side artifact for CI use but is not the primary distribution channel.

- **D-C3:** Subcommands (final v1.0 set):
  - `tide apply -f <project.yaml>` — wrapper around `kubectl apply`; surfaces helpful errors on schema validation
  - `tide watch <project>` — long-running watch on the Project + child CRDs; renders progress
  - `tide tail <task>` — streams pod logs via `pods/log` subresource (CLI-04)
  - `tide approve <project> [--wave <plan>/<N>]` — clears the level-pause OR wave-pause annotation
  - `tide reject <project> [--reason ...]` — halts the run; writes reject annotation
  - `tide cancel <project> [--force]` — destructive (cascades + PVC cleanup); requires `--force`
  - `tide resume <project>` — clears reject; re-enters reconciliation
  - `tide inspect-wave <plan> [--wave N]` — renders the wave's task list with status + elapsed time (CLI-03)
  - `tide artifact-get <ref>` — fetches a PVC artifact via a short-lived API-server proxied pod-exec
  - `tide describe-budget <project>` — surfaces budget tally vs cap (uses Phase 2 D-D2 data)

  Additional verbs (`tide approve --force-push`, `tide retry-push` from Phase 3 deferred) wrap the existing annotation-driven bypass: `tideproject.k8s/bypass-push-lease`, `tideproject.k8s/retry-push`.

- **D-C4:** Output format: human-readable by default; `--output json` for machine-parseable. No YAML output (kubectl already does that). No table-formatting library — use stdlib `text/tabwriter` for tabular output (consistent with kubectl).

### Dashboard (DASH-01..05)

- **D-D1:** **Side-by-side vertical split** with resizable divider:
  - **Left pane**: Planning DAG — the full hierarchy Project → Milestone → Phase → Plan (nodes are React components, status badges per node, color-coded by Phase status)
  - **Right pane**: Execution DAG — tasks within the currently-selected Plan, rendered with wave subgraphs (cross-wave edges visible per README spec's "flat wave subgraphs with cross-wave edges" guidance)
  - Clicking a Plan node in the left pane switches the right pane to that Plan's execution DAG. Smooth transition (no full page reload).
  - Layout via React Flow v12 + dagre (top-down for Planning, left-right for Execution — proven layouts).

- **D-D2:** Dashboard auth = own read-only ServiceAccount + apiserver proxy through dashboard backend. Browser NEVER talks to apiserver directly. Architecture:
  - Dashboard `Deployment` ships its own `ServiceAccount` (`tide-dashboard`) with `ClusterRole` granting `get/list/watch` on `projects.tideproject.k8s`, `milestones`, `phases`, `plans`, `tasks`, `waves` and `pods/log` subresource — no other verbs, no other resources.
  - Dashboard backend (Go binary in `cmd/dashboard/`) is a manager.Runnable using controller-runtime client. Exposes:
    - `GET /api/v1/projects` — list (paginated)
    - `GET /api/v1/projects/{name}` — single (with embedded children for the planning-DAG render)
    - `GET /api/v1/projects/{name}/events?stream=sse` — SSE stream of watch events (DASH-03)
    - `GET /api/v1/tasks/{name}/log?stream=sse` — pod log via apiserver proxy (DASH-04, opt-in)
  - Browser talks to dashboard only. No tokens in browser. No direct apiserver calls.

- **D-D3:** SSE source — backend maintains long-running watches (controller-runtime informer cache) against the K8s API; on every watch event, dispatches a small JSON delta to all SSE-connected browsers via a fan-out hub. No DB, no Redis — in-process pubsub keyed by Project name. Reconnect via standard EventSource `Last-Event-ID` semantics.

- **D-D4:** Log streaming — opt-in click-to-open per task (DASH-04). Browser opens an SSE connection (NOT WebSocket from browser; backend translates pod-log SSE→client SSE). Backend uses K8s `pods/log` API (which is plain HTTP streaming) and proxies to the client. Pitfall 22 (websocket leak) avoided: every backend log stream has a 5-minute idle close + client-disconnect-cleanup defer.

- **D-D5:** Frontend tooling — React 18 + TypeScript + React Flow v12 + @xyflow/react + dagre + Tailwind v4 (per STACK.md). Vite build. Output bundle baked into the dashboard binary via Go `embed.FS` (single image to deploy). No separate static asset CDN; small bundle target (<500KB gzipped).

- **D-D6:** Dashboard is read-only end-to-end — NO mutation endpoints (DASH-05). UI surfaces actions like "Approve" as deeplinks that copy the `tide approve <project>` command to clipboard with a confirmation toast — the user runs the CLI. Avoids needing write RBAC on the dashboard SA.

### W-1 + W-2 (Phase 3 catch-up — folded here)

- **D-W1:** Register `tide_secret_leak_blocked_total{project, phase, plan}` Prometheus counter at `cmd/manager/main.go` startup as part of OBS-02 metric set. ProjectReconciler reads the push-Job's `EnvelopeOut.reason` field (already set by Phase 3 03-06 — `reason="leak-detected"`); on exit-10 (leak), increment this counter and set `Project.Status.Phase=PushLeakBlocked` (new phase constant, distinct from `PushLeaseFailed` which stays for exit-11). Adds one new Phase constant and one switch arm; otherwise drops cleanly into the Phase 3 code paths.

- **D-W2:** Mid-stack boundary triggers — `MilestoneReconciler` / `PhaseReconciler` / `PlanReconciler` each get a "boundary detection" step in their existing 6-step body (Phase 3 D-A2): after observing all children Succeeded, before marking the level Succeeded, dispatch a tide-push Job with the appropriate D-B2 commit message via `buildCommitMessage(level, name)`. The boundary-detection logic is the same place that consults gate policy (D-G2) for `approve` / `pause` — they share the seam. Naturally couples GATE-01..03 work with W-2 work.

### Cross-cutting

- **D-X1:** All new metrics, traces, and log fields use the same label set (`project`, `phase`, `plan`) so grep-correlation across logs/metrics/traces works without bespoke joining.

- **D-X2:** Dashboard, CLI, and orchestrator are in the same go.mod. Dashboard binary and CLI binary are separate `cmd/` entrypoints (`cmd/dashboard/`, `cmd/tide/`). Dashboard's frontend lives in `dashboard/web/` with its own Vite/TypeScript config; produced bundle copied into `cmd/dashboard/embed/` via Makefile target.

- **D-X3:** Helm chart adds three new templates: `dashboard-deployment.yaml` + `dashboard-rbac.yaml` (read-only ClusterRole) + `servicemonitor.yaml` (gated). All gated by `.Values.dashboard.enabled` (default true) and `.Values.prometheus.serviceMonitor.enabled` (default false).

- **D-X4:** Pitfall mitigations explicitly addressed:
  - **Pitfall 17 (metric cardinality explosion)** — bounded labels enforced via `cmd/tide-lint` analyzer addition: forbid metric definitions with `task` label
  - **Pitfall 22 (websocket leak)** — backend log-stream idle timeout + client-disconnect-cleanup defer

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before researching or planning.**

### Project paradigm + vocabulary
- `README.md` — Five-level paradigm, two-DAG (Planning + Execution) framing, water/tide vocabulary, wave-boundary failure semantics, Mermaid diagram conventions
- `CLAUDE.md` — Project working rules (observe first, execute don't ask, verify before claiming); API group `tideproject.k8s` invariant; anti-patterns including the ServiceMonitor-disabled-by-default rule
- `.planning/PROJECT.md` — Vision, locked Key Decisions
- `.planning/REQUIREMENTS.md` — All 18 Phase 4 REQ-IDs (GATE-01..03, OBS-01..06, CLI-01..04, DASH-01..05)
- `.planning/STATE.md` — Current cursor; Phase 3 just completed CONDITIONAL

### Phase 3 carry-forward (decisions that constrain Phase 4)
- `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-CONTEXT.md`:
  - D-A1 dual-output dispatch contract (Markdown artifact + EnvelopeOut.childCRDs) — Phase 4 OBS-04 traces extract OpenInference attrs from the events.jsonl reference path
  - D-A2 four reconciler dispatch sites — Phase 4 W-2 wires push triggers at each
  - D-B2 four commit-message shapes — Phase 4 W-2 fires them at the right boundaries
  - D-B5 deterministic Job naming — Phase 4 metric counters use the same name scheme
  - D-C5 stream-json events.jsonl preservation — Phase 4 OBS-04 reads this for OpenInference attribute extraction (no re-parse)
- `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-VERIFICATION.md` — W-1, W-2, W-3 details (W-3 RWX driver matrix doc is likely Phase 5 DIST-04)
- `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-08-SUMMARY.md` lines 55-56 — explicit deferral statements for W-1/W-2 the planner can quote

### Phase 1+2 patterns
- `internal/controller/task_controller.go` — 6-step reconciler body template (the gate-policy hook lands here in Phase 4's gates work)
- `internal/controller/project_controller.go` — current Phase 3 push trigger at `Project.Status.Phase=Complete` (Phase 4 W-2 extends to mid-stack)
- `cmd/manager/main.go` + `cmd/manager/env.go` (Phase 3 Wave 5) — Helm env injection pattern; Phase 4 adds new env vars for OTel exporter endpoint + sampler config

### External tech specifications
- OpenInference attribute names — verify against current spec at https://github.com/Arize-ai/openinference (Phase 4 researcher should confirm key names + types haven't drifted since Phase 3 RESEARCH note)
- React Flow v12 — https://reactflow.dev/ (component-as-node pattern is the v1 dashboard's core affordance)
- cobra — https://github.com/spf13/cobra (kubectl uses this; consistent UX expected)
- Krew plugin index — https://krew.sigs.k8s.io/ for `kubectl tide` distribution

</canonical_refs>

<specifics>
## Specific Ideas

- **Two-DAG rendering — Planning DAG uses dagre top-down; Execution DAG uses dagre left-right.** Matches the README's argument that planning fans out wide (many siblings per level) while execution fans out narrow (deeper chains with wave-bands). Layout direction reinforces the paradigm.

- **Wave subgraphs in Execution DAG must be visually distinct from cross-wave edges.** Per README, "flat wave subgraphs with cross-wave edges" — render each wave as a vertical band with a subtle background color; cross-wave edges go from one band to the next horizontally. Don't bury the wave-bands inside collapsed subgraphs (that's the Planning DAG's affordance).

- **`tide watch` should render the live state as it would appear in the dashboard's left pane** — not a separate UI grammar. Same vocabulary (Project → Milestone → Phase → Plan → Task), same status badges in text form (✓ ⟳ ⚠ ✗ ⊘). Reduces cognitive load for users who toggle between CLI and dashboard.

- **`tide inspect-wave` output format**: borrow `kubectl get pods -o wide` patterns — columns NAME, STATUS, AGE, ATTEMPT, plus a final SCHEDULED-IN-WAVE column. Sortable by status by default; `--sort-by` flag respected.

- **Metric `tide_dispatch_latency_seconds` histogram buckets**: choose buckets sized for K8s API + LLM-inference latency reality — e.g., `[0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800]` (100ms to 30min). Default Prometheus buckets are too small for LLM workloads.

- **OpenInference `agent.invocation` span name**: use `tide.dispatch.<level>` to be unambiguous in Phoenix/LangSmith — humans should be able to filter by `tide.dispatch.milestone` to see only milestone-level planner subagent runs.

- **Dashboard backend health/readiness**: `/healthz` returns 200 once informer cache is synced. Helm chart's readiness probe gates traffic on this.

- **CLI completion**: `tide completion bash|zsh|fish|powershell` ships from cobra's standard completion subcommand. Krew installs typically wire this up automatically.

- **CLI `--context` / `--namespace` flags**: standard kubectl-aligned. Default namespace from kubeconfig. Document that `tide` runs in the namespace of the Project unless overridden.

- **Folder layout**:
  ```
  cmd/tide/                # CLI entrypoint
  cmd/dashboard/           # Dashboard backend (Go) — manager.Runnable
  cmd/dashboard/embed/     # Vite-built frontend bundle (embed.FS)
  dashboard/web/           # Frontend source (React + TS + Vite)
  pkg/otelai/              # OpenInference attribute helpers (public)
  internal/gates/          # Gate-policy evaluation (used by reconcilers)
  internal/metrics/        # Prometheus collector registration + helpers
  ```

</specifics>

<deferred>
## Deferred Ideas

- **Per-CRD gate override (`Milestone.Spec.gates`, `Phase.Spec.gates`, `Plan.Spec.gates`)** — rejected in favor of D-G1 Project-CRD-only. Possible v1.x if users need finer granularity.
- **`WaveApproval` / `PauseRequest` CRD** — rejected in favor of annotation-driven approval (D-G3). Possible v1.x if annotation-overload becomes a problem.
- **Tabbed / stacked / inline-expand dashboard layouts** — rejected in favor of D-D1 side-by-side vertical split. Possible v1.x A/B test if the side-by-side is cramped on narrow screens.
- **Browser-direct apiserver auth (token paste UX)** — rejected in favor of D-D2 dashboard-SA-proxy. Security + UX better through the proxy.
- **OIDC/SSO dashboard auth via reverse proxy (Dex, Keycloak, cloud IDP)** — deferred to v1.x. v1.0 ships dashboard-SA-only (Kubernetes RBAC governs who can port-forward / ingress to the dashboard).
- **Span-wrapping DSL for `pkg/otelai`** — rejected in favor of D-O4 thin attribute helpers. OpenInference spec churn would be expensive to chase in a DSL.
- **OTel Collector tail-sampling as v1.0 default** — collector remains optional, SDK-level (D-O3) is the default. Collector tail-sampling is documented as a production hardening step in `docs/observability.md`.
- **CLI container-image distribution as primary** — rejected in favor of D-C2 GH release + Krew. Container ships as a side artifact for CI.
- **`tide login` flow with token** — rejected in favor of D-C1 kubeconfig reuse. No new auth.
- **CLI YAML output mode** — rejected (kubectl already does YAML). `tide ... -o json` only.
- **Dashboard mutation endpoints (in-dashboard approve/reject)** — rejected per DASH-05. All state changes route through CLI/kubectl. Possible v2.x if user data shows people prefer in-dashboard actions.
- **Multi-cluster CLI auth** — deferred. v1.0 is single-cluster (one kubeconfig context at a time).
- **Per-task metric labels** — explicitly forbidden by OBS-02 + Pitfall 17. Phase 4 adds a `cmd/tide-lint` analyzer (`tide-lint metric-cardinality`) to fail CI on any metric definition that includes a `task` label.
- **gRPC streaming subagent contract** — v2+ per REQUIREMENTS.md.
- **Multi-provider subagent matrix** (`internal/subagent/openai/`, `internal/subagent/google/`) — v1.x or community contributions per Phase 3 D-C1.
- **PR creation per git host** — v2+ per REQUIREMENTS.md.
- **Dashboard exposed via Ingress with custom DNS** — v1.0 ships dashboard as a `ClusterIP` Service; ingress/route configuration is the cluster operator's call (Helm value `dashboard.ingress.enabled` can be added in Phase 5 if needed).
- **RWX PVC driver matrix doc (W-3 from Phase 3 verification)** — likely Phase 5 DIST-04 scope; not Phase 4's concern.

</deferred>

---

*Phase: 04-gates-observability-dashboard-cli*
*Context gathered: 2026-05-16 via /gsd-discuss-phase*
*Successor of: Phase 3 (Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption) — CONDITIONAL, W-1/W-2 folded into D-W1/D-W2 here.*
