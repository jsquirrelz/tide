# TIDE — v1 Requirements

Scope: the **Self-Hosting MVP**. A fresh `kind` cluster + Helm install + `kubectl apply -f project.yaml` drives this repo's next milestone end-to-end. Every requirement below traces to a v1 feature the research synthesis identified as table-stakes, or to a structural commitment in PROJECT.md / the spec.

## v1 Requirements

### CRDs & Schema

- [ ] **CRD-01**: TIDE defines six CRDs (`Project`, `Milestone`, `Phase`, `Plan`, `Task`, `Wave`) in `apiVersion: tide.io/v1alpha1`, each with separate `Spec` (intent) and `Status` (observed) sections
- [ ] **CRD-02**: Each CRD declares owner-reference cascade to its parent in the hierarchy with `BlockOwnerDeletion: true`, scoped same-namespace
- [ ] **CRD-03**: CRDs ship with CEL validation rules for invariants expressible in CEL (non-empty fields, format constraints, range checks)
- [ ] **CRD-04**: A validating admission webhook handles the cross-object invariants CEL can't express (notably cycle detection across the declared task DAG)
- [ ] **CRD-05**: Conversion-webhook scaffolding is in place from day one, even though only `v1alpha1` exists in v1
- [ ] **CRD-06**: kubebuilder RBAC markers grant the orchestrator the minimum verbs per Kind — no wildcards anywhere

### Kahn-layered DAG library

- [ ] **DAG-01**: `pkg/dag` is a pure Go, stdlib-only package implementing Kahn's algorithm in layered form, returning waves as `[]Set[NodeID]`
- [ ] **DAG-02**: `pkg/dag` detects cycles at the algorithm's termination condition and returns a `CycleError` naming the involved nodes
- [ ] **DAG-03**: `pkg/dag` is consumed twice in TIDE — once for the Planning DAG, once for the Execution DAG — with typed-apart call sites at the package boundary so the two DAGs cannot accidentally cross-pollute
- [ ] **DAG-04**: `pkg/dag` has unit tests pinning the spec's worked example (`Tasks: α,β,γ,δ,ε,ζ,η,θ` → `Waves: [{α,β,γ,ζ},{δ,η},{ε,θ}]`) as a regression fixture
- [ ] **DAG-05**: `pkg/dag` has no imports from the K8s, controller-runtime, or Anthropic-SDK module trees

### Controllers & reconciliation

- [ ] **CTRL-01**: A single controller-runtime `Manager` registers six reconcilers (`ProjectReconciler`, `MilestoneReconciler`, `PhaseReconciler`, `PlanReconciler`, `WaveReconciler`, `TaskReconciler`)
- [ ] **CTRL-02**: Each reconciler is event-driven via `Owns(&batchv1.Job{})` and watches; no `time.Sleep` or blocking inside `Reconcile()`
- [ ] **CTRL-03**: The Manager runs leader-elected; on failover, in-flight work resumes from CRDs + PVC artifacts without losing the indegree map
- [ ] **CTRL-04**: Per-reconciler `MaxConcurrentReconciles` is tunable independently via Helm values
- [ ] **CTRL-05**: Finalizers on each CRD have bounded deadlines and idempotent cleanup logic; the docs include a `kubectl patch` recipe for manual unstick

### Parallelism budgets

- [ ] **POOL-01**: The orchestrator process holds two `chan struct{}` semaphores — `plannerPool` and `executorPool` — sized from Helm values `plannerConcurrency` (default 16) and `executorConcurrency` (default 4)
- [ ] **POOL-02**: On controller restart, both semaphores pre-charge from live Jobs (`kubectl get jobs --field-selector=status.active=1`) so resumption respects current load
- [ ] **POOL-03**: The two pools are never collapsed into a single worker pool — enforced by a custom go-analyzer lint rule rejecting any cross-pool wait

### Subagent dispatch

- [ ] **SUB-01**: `pkg/dispatch` defines a `Subagent` interface — `Run(ctx, EnvelopeIn) (EnvelopeOut, error)` — with envelope types serializable to `result.json`
- [ ] **SUB-02**: `pkg/dispatch.PodJobBackend` is the v1 concrete impl: one K8s `Job` per Task, mounting the Project PVC + creds Secret, exit code = success/failure, agent writes `result.json` and any artifacts to a declared PVC path
- [ ] **SUB-03**: Job dispatch is idempotent — deterministic Job names of the form `tide-task-{task-uid}-{attempt-n}` prevent duplicate dispatch on watch lag
- [ ] **SUB-04**: A `stub-subagent` image (canned envelope, no LLM call) is built and shipped alongside the real one for use in integration tests
- [ ] **SUB-05**: A custom go-analyzer lint rule rejects imports from `github.com/anthropics/*` (or any LLM-provider SDK) inside `pkg/controller/...`, `pkg/dispatch/...`, or `pkg/dag/...` — the orchestrator is provider-firewalled

### Subagent harness (in the image)

- [ ] **HARN-01**: The subagent image accepts a role flag (`planner` or `executor`) and a level flag (`milestone` / `phase` / `plan` / `task`) at startup; prompt + tool-allowance derive from those flags
- [ ] **HARN-02**: The harness enforces per-Task wall-clock, iteration, and token caps from envelope settings; exceeding any cap exits non-zero with a structured `cap-hit` reason
- [ ] **HARN-03**: The harness exposes a signed-token credential proxy — the agent process never sees raw `ANTHROPIC_API_KEY`; the proxy attaches the key to outbound requests and refuses requests from outside the agent process
- [ ] **HARN-04**: The harness redacts known secret patterns (API keys, JWTs, AWS-style credentials) from `result.json`, written artifacts, and stdout/stderr before they leave the pod
- [ ] **HARN-05**: The harness validates that artifacts written to the PVC match the envelope's declared output paths — out-of-scope writes are rejected and the Job exits non-zero
- [ ] **HARN-06**: The v1 concrete agent runtime inside the harness is Claude Code in headless mode (`claude -p ... --output-format stream-json`); host `~/.claude/` is never mounted; OAuth flows are not used inside containers

### Persistence & resumption

- [ ] **PERSIST-01**: All persistent state lives in CRD `.status` fields — no SQLite, no external database, no per-run state file
- [ ] **PERSIST-02**: Per-Task CRDs hold small status blocks (`phase`, `completedAt`, `exitCode`, `attempt`); aggregate `Status.Waves` or `Status.Schedule` fields on parent CRDs are explicitly forbidden (review-blocked)
- [ ] **PERSIST-03**: Wave schedules are re-derived from the task DAG on every reconcile via `pkg/dag.ComputeWaves` — there is no cached schedule
- [ ] **PERSIST-04**: A `chaos-resume` integration test kills the orchestrator pod mid-wave and verifies the new leader resumes with no lost or duplicated tasks, using only CRD status + PVC contents

### Plan validation

- [ ] **PLAN-01**: The Plan admission path computes wave structure via `pkg/dag.ComputeWaves` and rejects cyclic DAGs with a structured error naming the involved tasks
- [ ] **PLAN-02**: The Plan admission path reconciles LLM-declared `depends_on` edges against file-touch sets — declarations that don't match the declared file-touch sets generate a warning (strict mode rejects them)
- [ ] **PLAN-03**: Cycle "recovery" features are out of scope — cyclic plans refuse to run and surface the error to the human

### Failure semantics

- [ ] **FAIL-01**: Wave-boundary failure handling follows the spec exactly: a failed Task → siblings in the same wave continue running; dependents in later waves never dispatch (their indegree never reaches zero); non-dependents in later waves dispatch normally under strict-by-default profile
- [ ] **FAIL-02**: Indegree decrement is per-task (not per-wave) — siblings completing in the same wave each decrement their downstream successors independently
- [ ] **FAIL-03**: A token-bucket rate limiter sits between the orchestrator and the LLM provider; 429 responses retry with exponential backoff and surface a `tide_provider_rate_limit_hits_total` Prometheus counter
- [ ] **FAIL-04**: Per-Project budget caps (rolling-window cost + absolute cost ceiling from Helm values) halt dispatch when exceeded and require a `tide approve --bypass-budget` to resume

### Artifacts & git

- [ ] **ART-01**: One RWX PersistentVolumeClaim per Project, layout: `/workspace/{repo,artifacts/M-N/P-N/L-N,envelopes}`
- [ ] **ART-02**: The Helm chart leaves `storageClassName` empty so cluster operators choose RWX driver (EFS / Filestore / Azure Files / `csi-driver-nfs` / Longhorn); docs enumerate the matrix
- [ ] **ART-03**: `pkg/git` (using `go-git/v5`) pushes artifacts at every level boundary (Plan done → push, Phase done → push, Milestone done → push)
- [ ] **ART-04**: Git pushes happen from the orchestrator process, not from subagent pods — one credential surface, one push process
- [ ] **ART-05**: Push uses HTTPS+PAT by default (host-agnostic: GitHub, GitLab, Gitea all work); SSH is supported but documented with host-key caveats
- [ ] **ART-06**: Pushes go to per-run branches `tide/run-<project>-<timestamp>` — never to `main`/`master` — and use `--force-with-lease` only on the per-run branch
- [ ] **ART-07**: Every push runs `gitleaks` on the diff; pattern matches fail the push and surface a `tide_secret_leak_blocked_total` Prometheus counter

### Auth & tenancy

- [ ] **AUTH-01**: Credentials (LLM API keys + git push tokens) are stored as K8s `Secret` resources; the `Project` CRD references them by name via `secretRef` fields
- [ ] **AUTH-02**: Namespace-per-project tenancy: one TIDE install per cluster, each Project runs in its own namespace with namespace-scoped RBAC
- [ ] **AUTH-03**: The orchestrator's ServiceAccount has no cluster-wide wildcards — RBAC is generated from kubebuilder markers and scoped per-CRD-Kind

### Human gates

- [ ] **GATE-01**: The `Project` CRD has a `gates` field declaring policy per level (`auto` | `approve` | `pause`) — defaults supplied per level
- [ ] **GATE-02**: Slack-tide review (between-wave checkpoint) is supported via a `pause-between-waves: true` setting on Phase or Plan
- [ ] **GATE-03**: `tide approve` advances a paused level boundary; `tide reject` halts the run

### Observability

- [ ] **OBS-01**: Orchestrator and subagent pods emit structured JSON logs via zap-behind-logr; controller-runtime's logger integrates cleanly
- [ ] **OBS-02**: Prometheus metrics expose: waves dispatched, tasks completed/failed, dispatch latency histograms, rate-limit hit counters, secret-leak blocked counters — labels bounded to project/phase/plan (never per-task)
- [ ] **OBS-03**: OpenTelemetry tracing spans the Project → Milestone → Phase → Plan → Task subagent dispatch chain
- [ ] **OBS-04**: LLM/agent spans use OpenInference attribute names (`llm.input_messages`, `llm.token_count.prompt`, `llm.token_count.completion`, agent control-flow attrs) emitted manually via a small internal `pkg/otelai` wrapper — no Go OpenInference SDK exists in 2026
- [ ] **OBS-05**: Trace tail-sampling is enabled by default to bound cost; full LLM payloads are stored as artifact refs (PVC paths) rather than as span attributes
- [ ] **OBS-06**: A `ServiceMonitor` resource is included in the Helm chart, gated behind a value (`prometheus.serviceMonitor.enabled`)

### CLI

- [ ] **CLI-01**: `tide` is a thin stateless cobra-based client (no local cache) talking to the K8s API
- [ ] **CLI-02**: `tide` supports the subcommands `apply`, `watch`, `tail`, `approve`, `reject`, `cancel`, `resume`, `inspect-wave`, `artifact-get`
- [ ] **CLI-03**: `tide inspect-wave` renders the current wave's tasks with status and elapsed time
- [ ] **CLI-04**: `tide tail` streams pod logs for a given Task via the K8s `pods/log` subresource

### Dashboard

- [ ] **DASH-01**: A read-only web dashboard ships as a separate `Deployment` with its own read-only `ServiceAccount` — distinct from the orchestrator's
- [ ] **DASH-02**: The dashboard renders the live Planning DAG and Execution DAG side-by-side using React Flow v12 + dagre + Tailwind v4
- [ ] **DASH-03**: Status updates stream over Server-Sent Events (SSE) — uni-directional, proxy-friendly, no WebSocket upgrade dance
- [ ] **DASH-04**: Pod log streaming uses the apiserver `pods/log` WebSocket proxy (K8s 1.31+); per-task log streams are opt-in (click-to-open) to bound data volume
- [ ] **DASH-05**: The dashboard has no mutation endpoints — all state changes route through `kubectl` or the `tide` CLI

### Distribution

- [ ] **DIST-01**: A Helm chart packages CRDs (as a dedicated subchart for safe upgrades), the controller `Deployment`, the dashboard `Deployment`, RBAC, and namespace setup
- [ ] **DIST-02**: The release pipeline uses `helmify` to convert kubebuilder's Kustomize output into the Helm chart
- [ ] **DIST-03**: An `Apache-2.0` `LICENSE` file is at the repo root; every Go file's package header references the license
- [ ] **DIST-04**: Documentation covers: install, Project authoring with 3 sample CRDs, provider configuration, git remote configuration, failure recovery, RBAC reference, troubleshooting
- [ ] **DIST-05**: An external-operator dry-run acceptance test confirms clone-to-first-run is under 30 minutes for an operator unfamiliar with the codebase

### Testing

- [ ] **TEST-01**: Unit tests (no LLM, no K8s) cover `pkg/dag`, `pkg/dispatch` envelope encoding/decoding, harness redaction, idempotent Job name generation; run in <30s on CI
- [ ] **TEST-02**: Integration tests use `envtest` + `kind` + `stub-subagent` to exercise full reconcile chains without LLM cost; run in <5 min
- [ ] **TEST-03**: A nightly live E2E test exercises one real Claude-backed Project against a fixture repo, budget-capped per run
- [ ] **TEST-04**: The `chaos-resume` test (kill orchestrator mid-wave) runs in the integration tier

### Bootstrap & self-hosting

- [ ] **BOOT-01**: M0 ("TIDE-on-host runs TIDE-on-self") is a roadmap-named milestone: the developer's host runs `get-shit-done` workflows that produce TIDE's CRDs, `pkg/dag`, controllers, harness, and dispatch — bounded scope, no in-cluster execution yet
- [ ] **BOOT-02**: M_self ("TIDE-in-cluster authors same artifacts") is a roadmap-named milestone: a fresh `kind` cluster with a freshly Helm-installed TIDE runs a Project that authors a complete next-milestone artifact set on this repo
- [ ] **BOOT-03**: M0 and M_self commit to the same `v1alpha1` CRD schema — no breaking schema changes across the bootstrap bridge
- [ ] **BOOT-04**: The v1 release acceptance test is: fresh kind cluster + `helm install tide` + `kubectl apply -f project.yaml` drives this repo's next milestone end-to-end, producing committed artifacts on a per-run branch and a clean status

## v1.x / Deferred Requirements

These are explicit v2+ candidates. Captured here so a future planner doesn't re-derive them from scratch.

- gRPC streaming subagent contract (the Pod-per-task envelope is enough for v1; streaming is additive behind the same `Subagent` interface)
- Conservative wave-boundary failure profile as a per-Project setting
- External Secrets Operator / Vault first-class integration (plain Secrets cover v1)
- PR creation / auto-CI-fix automation per git host (TIDE pushes branches; humans open PRs in v1)
- Multi-tenant cluster posture (per-tenant quotas, cross-tenant RBAC)
- Per-host PR matrix (GitHub, GitLab, Gitea) beyond plain `git push`
- Drag-to-edit DAG in the dashboard
- Kueue integration for cross-cluster quota management
- OLM bundle for OperatorHub listing
- Agent Sandbox / gVisor isolation for hardened multi-tenant runs
- MCP / A2A surface for cross-orchestrator coordination
- Project templates (parameterized scaffolds)
- Native notifications (Slack/email at level boundaries)
- Multi-cluster dispatch

## Out of Scope

Explicit exclusions with reasoning. Re-adding any of these requires PROJECT.md edit + Key Decisions row.

- **Critical-path / HEFT / heterogeneous-resource scheduling** — Spec §"Alternatives considered and rejected" argues these are wrong at the paradigm layer; LLM agent task durations are too high-variance for CPM, and HEFT is premature until subagent pools are heterogeneous.
- **Cycle recovery features** — Cycles are bugs in the declared plan; the orchestrator refuses to run and surfaces. Recovery would mask the bug.
- **Cached wave schedules** — Spec is explicit: O(V+E) re-derivation on every plan edit is intentional. Caching invites stale-schedule bugs.
- **Unifying the two DAGs into a single abstraction** — Planning DAG and Execution DAG have different shapes (fan-out wide vs. fan-out narrow) and different parallelism budgets. Unifying would erase the structural argument for two pools.
- **External database (Postgres/MySQL) for v1** — Artifacts are truth; CRD `.status` is sufficient cache at v1 scale. Per-run SQLite is the same false-precision.
- **Dashboard mutation actions** — Mutations route through `kubectl` / `tide` CLI for a single auth surface. Read-only dashboard avoids parallel auth implementation.
- **Vendored GSD workflow Markdown** — TIDE reads `get-shit-done` as a design reference, not a runtime dependency. Vendoring would couple TIDE to one bootstrap host.
- **Non-Kubernetes runtime (Compose / bare metal / Nomad)** — The K8s pun is load-bearing. Pod isolation, RBAC, watches, and Jobs are what make the dispatch model tractable; without them, this is a different project.
- **Vendor lock-in to one LLM provider** — The `Subagent` interface is provider-agnostic by construction. Provider SDK imports are firewalled from the orchestrator package by lint rule.
- **Wildcard RBAC** — Operator-grade RBAC requires per-Kind verb grants. Wildcards in v1 would set a wrong precedent for an OSS operator.
- **Inline embedding of task lists in parent CRDs** — etcd's 1.5 MiB per-object limit would bite quickly; per-Task CRDs + owner refs are the idiomatic K8s answer.

## Traceability

| REQ-ID | Phase | Notes |
|--------|-------|-------|
| (Filled by roadmap) | | |
