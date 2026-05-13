# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Overview

TIDE v1 is the **Self-Hosting MVP**: the five-level paradigm (Project → Milestone → Phase → Plan → Task with Waves derived) running as a real Kubernetes orchestrator that can drive its own next milestone end-to-end. The journey is five phases. The first four constitute **M0** — TIDE-on-host built via GSD on the developer's machine, producing a `kind`-installable operator with CRDs, two-DAG dispatch, real Claude-backed subagents, observability, dashboard, and CLI. The fifth phase is **M_self** — distribution polish plus the v1 acceptance test: a fresh `kind` cluster + `helm install tide` + `kubectl apply -f project.yaml` drives this repo's next milestone end-to-end, producing committed artifacts on a per-run branch. M_self consumes M0's artifacts against the same `v1alpha1` CRD schema, with no breaking changes across the bridge. Build order is foundation-before-fanout: Phase 1 is the densest pitfall window (CRD schema + controller scaffold + `pkg/dag` — eight critical/serious pitfalls bake in here); Phase 2 layers the dogfood-critical innermost pair (TaskReconciler + WaveReconciler + dispatch + harness with budget caps and signed-token proxy) with the stub subagent to decouple K8s-time learning from LLM-time learning; Phase 3 adds the up-stack reconcilers, real Claude-backed subagent, and git push at level boundaries; Phase 4 fans out gates, observability, dashboard, and CLI (independent components, parallelizable internally); Phase 5 packages, documents, and proves it works.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

**Milestone groupings:**
- **M0 (TIDE-on-host)**: Phases 1-4 — TIDE built via GSD, ready to install in a cluster
- **M_self (TIDE-in-cluster authors same artifacts)**: Phase 5 — fresh kind + helm install + project apply

- [ ] **Phase 1: Foundation — CRDs, pkg/dag, Controller Scaffold** — Six CRDs, pure-Go layered Kahn, six reconciler stubs, two-pool semaphores, RBAC, finalizers — densest pitfall window
- [ ] **Phase 2: Dispatch & Plan Validation — Innermost Reconcilers + Harness** — Subagent interface, PodJobBackend, stub-subagent image, harness budget caps + signed-token proxy, plan admission with cycle detection and file-touch reconciliation, strict-by-default wave failure semantics, token-bucket rate limiting
- [ ] **Phase 3: Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption** — Plan/Phase/Milestone/Project reconcilers, `pkg/git` with HTTPS+PAT and gitleaks, real Claude-Code-backed subagent replaces stub, chaos-resume acceptance test proves indegree+completed-set resumption
- [ ] **Phase 4: Gates, Observability, Dashboard, CLI** — Per-level gate policy, structured logs + Prometheus + OTel/OpenInference, two-DAG read-only dashboard, `tide` CLI
- [ ] **Phase 5: Distribution & Self-Hosting Acceptance** — Helm chart, Apache 2.0, docs, external-operator dry-run, fresh kind + helm install + project apply drives this repo's next milestone

## Phase Details

### Phase 1: Foundation — CRDs, pkg/dag, Controller Scaffold
**Goal**: A `kubebuilder`-scaffolded Go project with six `v1alpha1` CRDs, a pure-Go layered-Kahn library, six event-driven reconciler stubs on one Manager, two separately-sized parallelism semaphores, kubebuilder RBAC with no wildcards, and finalizers with bounded deadlines — ready to receive dispatch logic in Phase 2.
**Depends on**: Nothing (first phase)
**Requirements**: CRD-01, CRD-02, CRD-03, CRD-04, CRD-05, CRD-06, DAG-01, DAG-02, DAG-03, DAG-04, DAG-05, CTRL-01, CTRL-02, CTRL-03, CTRL-04, CTRL-05, POOL-01, POOL-02, POOL-03, AUTH-02, AUTH-03, PERSIST-01, PERSIST-02, BOOT-01, BOOT-03, TEST-01
**Success Criteria** (what must be TRUE):
  1. A developer can `kubectl apply` a sample Project / Milestone / Phase / Plan / Task / Wave CRD against an `envtest`-spun apiserver and see each CRD accepted with CEL validation enforcing non-empty fields and a validating admission webhook scaffolded (firing as a no-op until Phase 2 wires it)
  2. A developer can `go test ./pkg/dag/...` and see the spec's worked example (`α,β,γ,δ,ε,ζ,η,θ` → `[{α,β,γ,ζ},{δ,η},{ε,θ}]`) pinned as a passing regression fixture, plus cycle detection returns a `CycleError` naming involved nodes — all with zero K8s, controller-runtime, or Anthropic-SDK imports under `pkg/dag/`
  3. A developer can start the Manager locally and confirm six reconcilers registered (`Project/Milestone/Phase/Plan/Wave/Task`), each event-driven via `Owns(&batchv1.Job{})` with no `time.Sleep` or blocking I/O inside `Reconcile()`, with leader election active, and per-reconciler `MaxConcurrentReconciles` tunable from Helm values
  4. A developer can inspect the running Manager's process state and see two distinct `chan struct{}` semaphores (`plannerPool` size 16 default, `executorPool` size 4 default), pre-charged on startup from any pre-existing live Jobs, and a custom go-analyzer lint rule rejecting any code path that waits on both pools — verifying the two pools cannot be silently unified
  5. A `kubectl describe sa tide-orchestrator` shows RBAC with no wildcards (per-CRD-Kind verb grants only), and a deliberate `kubectl delete project sample-project` cascades cleanly via owner references with `BlockOwnerDeletion: true` while finalizers run idempotent cleanup under a bounded deadline
**Plans:** 11 plans

Plans:
- [x] 01-01-PLAN.md — kubebuilder scaffold + module init + 6 CRD scaffolds + 2 webhook scaffolds (Wave 1)
- [x] 01-02-PLAN.md — pkg/dag pure-Go Kahn-layered library + α…θ regression fixture + DAG-05 import firewall (Wave 1)
- [x] 01-03-PLAN.md — POOL-03 custom analyzer + cmd/tide-lint + CI gate (Wave 1)
- [x] 01-04-PLAN.md — internal helper packages (owner, finalizer, pool, config) + dispatch placeholder (Wave 1)
- [x] 01-05-PLAN.md — CRD types (Spec/Status) + CEL markers + shared status conditions + PERSIST-02 gate (Wave 2)
- [x] 01-06-PLAN.md — Six reconcilers at Standard depth + envtest assertions + Pitfall 1 gate (Wave 2)
- [x] 01-07-PLAN.md — Plan + Wave webhook no-op bodies with Phase 2 wire-points + envtest assertions (Wave 2)
- [x] 01-08-PLAN.md — cmd/manager/main.go Manager wiring + leader-election envtest (Wave 3)
- [x] 01-09-PLAN.md — Per-Kind RBAC marker audit + AUTH-03 CI gate (Wave 3)
- [x] 01-10-PLAN.md — α…θ sample CRDs in config/samples/ (Wave 3)
- [x] 01-11-PLAN.md — Helm chart pair via helmify + final CI workflow with TEST-01 timing assertion (Wave 3)

### Phase 2: Dispatch & Plan Validation — Innermost Reconcilers + Harness
**Goal**: A working dogfood-critical pair — `TaskReconciler` + `WaveReconciler` — can dispatch a manually-applied Plan-with-tasks against a stub subagent image, honor wave boundaries with strict-by-default per-task indegree decrement, enforce per-Task and per-Project budget caps in the harness, validate plans at admission for cycles and file-touch consistency, and survive 429s from a fake provider via a token-bucket rate limiter. No LLM tokens are spent in this phase's tests.
**Depends on**: Phase 1
**Requirements**: SUB-01, SUB-02, SUB-03, SUB-04, SUB-05, HARN-01, HARN-02, HARN-03, HARN-04, HARN-05, HARN-06, PLAN-01, PLAN-02, PLAN-03, FAIL-01, FAIL-02, FAIL-03, FAIL-04, PERSIST-03, ART-01, TEST-02
**Success Criteria** (what must be TRUE):
  1. A developer can `kubectl apply` a manually-authored Plan with a fixed task DAG and watch the WaveReconciler dispatch one K8s Job per Task in the first wave (each named deterministically `tide-task-{task-uid}-{attempt-n}`), wait for envelopes on the per-Project RWX PVC at `/workspace/{repo,artifacts/M-N/P-N/L-N,envelopes}`, and advance to wave 2 only after wave 1's tasks terminate — with the stub-subagent image producing canned envelopes and no Anthropic SDK imports detectable via the custom go-analyzer lint rule under `pkg/controller/...`, `pkg/dispatch/...`, or `pkg/dag/...`
  2. A developer can `kubectl apply` a cyclic Plan and see the admission path reject it with a structured error naming the involved tasks (no `Wave` resource ever produced); applying a Plan whose LLM-declared `depends_on` disagrees with the file-touch sets produces an admission warning (strict mode rejects)
  3. A developer can inject a failure into one Task in a three-task wave (via the stub image's failure flag) and verify (a) sibling Tasks in the same wave continue and complete, (b) Tasks in wave k+1 that depend on the failed Task never dispatch (their indegree never reaches zero — verified by `Task.status.phase == Pending` forever), (c) non-dependent Tasks in wave k+1 dispatch normally under strict-by-default — and that indegree decrements are per-task, not per-wave
  4. A developer can run the integration test tier (`envtest` + `kind` + `stub-subagent`) in under 5 minutes, exercising full reconcile chains without any LLM cost, including a synthetic 429 storm that the token-bucket rate limiter absorbs (verifying `tide_provider_rate_limit_hits_total` increments and dispatch continues with exponential backoff) and a per-Project absolute cost cap that pauses dispatch and requires `tide approve --bypass-budget` to resume
  5. A `kubectl describe pod` on a running subagent shows the harness enforcing wall-clock + iteration + token caps from the envelope, redacting known secret patterns (API keys, JWTs, AWS-style) from `result.json` and stdout, the signed-token credential proxy intercepting outbound requests so the agent process never sees raw `ANTHROPIC_API_KEY`, and the post-Job validator rejecting any artifact write outside declared output paths

**Research flag**: Recommend `/gsd:research-phase` during planning — densest novel territory (per-Job mount scoping, signed-token proxy, harness budget enforcement, rate-bucket-aware dispatch, file-touch-derived-edges admission).

**Plans:** 13 plans

Plans:
**Wave 1**
- [x] 02-01-PLAN.md — pkg/dispatch public envelope contract (EnvelopeIn/Out, Subagent interface, errors) + verify-dispatch-imports gate (Wave 1)
- [x] 02-03-PLAN.md — v1alpha1 schema additions (Project.Spec/Status, Task.Spec.Dev, Plan.Status, shared_types constants) + codegen (Wave 1)

**Wave 2** *(blocked on Wave 1 completion)*
- [ ] 02-02-PLAN.md — providerfirewall lint analyzer + cmd/tide-lint multichecker flip (SUB-05) (Wave 2)
- [ ] 02-04-PLAN.md — cmd/stub-subagent Go binary + Dockerfile (Wave 2)
- [ ] 02-05-PLAN.md — credential proxy (HMAC token + self-signed cert + HTTPS server + cmd/credproxy + Dockerfile) (Wave 2)
- [ ] 02-06-PLAN.md — internal/harness package (caps + redact tail-keep buffer + outputs validate + envelope_io) (Wave 2)
- [ ] 02-07-PLAN.md — internal/budget package (sync.Map rate bucket + PreCharge + cap check + tally + Prometheus counter) (Wave 2)

**Wave 3** *(blocked on Wave 2 completion)*
- [ ] 02-08-PLAN.md — internal/dispatch.Dispatcher interface body + PodJobBackend + JobSpec (native sidecar) + JobName (Wave 3)

**Wave 4** *(blocked on Wave 3 completion)*
- [ ] 02-09-PLAN.md — TaskReconciler dispatch body + WaveReconciler observational roll-up + PlanReconciler Wave materialization (Wave 4)

**Wave 5** *(blocked on Wave 4 completion)*
- [ ] 02-10-PLAN.md — ProjectReconciler init Job (ART-01) + budget cap halt + bypass annotation watch (Wave 5)
- [ ] 02-11-PLAN.md — Plan admission webhook body (cycle detection + file-touch reconciliation + strict/warn precedence) (Wave 5)

**Wave 6** *(blocked on Wave 5 completion)*
- [ ] 02-12-PLAN.md — cmd/manager Phase 2 wiring + Helm chart signing-secret template + tide-subagent SA + values keys (Wave 6)

**Wave 7** *(blocked on Wave 6 completion)*
- [ ] 02-13-PLAN.md — Integration test tier (envtest Layer A + kind Layer B + cluster.yaml + Make targets + CI gate) (Wave 7)

### Phase 3: Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption
**Goal**: The full reconciler stack (Plan → Phase → Milestone → Project) drives planner-subagent dispatch to author `PLAN.md` / phase brief / `MILESTONE.md`, the orchestrator pushes artifacts at every level boundary via `pkg/git` (HTTPS+PAT default, host-agnostic, per-run branches, `--force-with-lease`, never `main`, gitleaks at every push), the stub-subagent is replaced by a real Claude-Code-backed image inside the same Subagent interface, and a chaos-resume test proves the orchestrator survives mid-wave pod kill using only CRD status + PVC contents.
**Depends on**: Phase 2
**Requirements**: ART-02, ART-03, ART-04, ART-05, ART-06, ART-07, AUTH-01, PERSIST-04, TEST-03, TEST-04
**Success Criteria** (what must be TRUE):
  1. A developer can `kubectl apply` a Project CRD with a `secretRef` to an LLM API key Secret and a git creds Secret in the same namespace, watch the orchestrator clone the target repo into the per-Project PVC, dispatch a planner subagent to author `MILESTONE.md`, see the orchestrator (not a subagent pod) commit and push that artifact to a per-run branch `tide/run-<project>-<timestamp>` via HTTPS+PAT against a generic git remote (GitHub fixture verified; documented to work against GitLab/Gitea behind the same interface), and confirm push uses `--force-with-lease` and never targets `main`
  2. A developer can introduce a deliberate secret pattern into a subagent's output and see `gitleaks` scan the diff at push time, fail the push, and increment the `tide_secret_leak_blocked_total` Prometheus counter — confirming secret leakage is blocked at the push boundary
  3. A developer can `kubectl apply` a Project and watch the real Claude-Code-backed subagent image (running `claude -p ... --output-format stream-json` with `ANTHROPIC_API_KEY` from EnvFrom Secret, never mounting host `~/.claude/`, no OAuth) author a real `PLAN.md` for a small fixture milestone, the PlanReconciler validate its declared task DAG via `pkg/dag`, materialize Task and Wave CRDs, and dispatch executor Jobs — the dispatch contract from Phase 2 unchanged, only the image content swapped
  4. A developer can run the nightly live E2E test against one real Claude-backed Project on a fixture repo, see it complete within the per-run budget cap, and inspect the per-run branch on the remote to find committed artifacts with structured commit messages at each level boundary (`tide: milestone M-001 authored`, `tide: phase P-001 authored`, etc.)
  5. A developer can run the `chaos-resume` integration test that kills the orchestrator pod mid-wave with three Tasks in flight, see a new leader take over within the leader-election lease window, re-derive the wave schedule from `pkg/dag.ComputeWaves` against the live Task CRDs + PVC contents only (no persisted schedule), and observe no duplicate dispatch (deterministic Job names prevent it) and no lost tasks — verifying resumption state is exactly indegree map + completed-task set

**Research flag**: Recommend `/gsd:research-phase` during planning — `go-git` vs shell-out tradeoffs for non-GitHub hosts; RWX PVC driver matrix testing; per-run branch + `--force-with-lease` integration design; TIDE-overwrites-human-commits coordination (Pitfall 13).

**Plans**: TBD

### Phase 4: Gates, Observability, Dashboard, CLI
**Goal**: Per-level human gate policy is configurable on the Project CRD; structured JSON logs flow from orchestrator and subagent pods; Prometheus metrics expose bounded-cardinality counters/histograms; OpenTelemetry traces span the Milestone→Phase→Plan→Task subagent chain with hand-rolled OpenInference attributes; a read-only React-Flow dashboard renders the live Planning + Execution DAGs side-by-side; and a `tide` cobra CLI wraps the common ops (apply / watch / tail / approve / reject / cancel / resume / inspect-wave / artifact-get).
**Depends on**: Phase 3
**Requirements**: GATE-01, GATE-02, GATE-03, OBS-01, OBS-02, OBS-03, OBS-04, OBS-05, OBS-06, CLI-01, CLI-02, CLI-03, CLI-04, DASH-01, DASH-02, DASH-03, DASH-04, DASH-05
**Success Criteria** (what must be TRUE):
  1. A developer can author a Project CRD with `gates: { milestone: approve, phase: auto, plan: auto, task: auto, pauseBetweenWaves: true }` and watch the orchestrator pause at the configured boundaries — `tide approve <project>` advances a paused level, `tide reject` halts the run, and slack-tide between-wave review fires after every wave's join when enabled
  2. A developer can `kubectl logs deploy/tide-controller` and see structured JSON via zap-behind-logr, scrape `/metrics` and find bounded-cardinality counters (`tide_waves_dispatched_total`, `tide_tasks_completed_total`, `tide_tasks_failed_total`, dispatch-latency histograms, `tide_provider_rate_limit_hits_total`, `tide_secret_leak_blocked_total`) labeled at most by `project/phase/plan` (never per-task), and configure an optional `ServiceMonitor` via Helm value `prometheus.serviceMonitor.enabled`
  3. A developer can point an OTLP collector at the orchestrator's `OTEL_EXPORTER_OTLP_ENDPOINT` and see the full Project → Milestone → Phase → Plan → Task subagent chain rendered as a single trace tree in Phoenix / LangSmith / Arize without bespoke instrumentation — verifying the hand-rolled `pkg/otelai` wrapper emits OpenInference attribute names (`llm.input_messages`, `llm.token_count.prompt`, `llm.token_count.completion`, agent control-flow attrs) on OTel spans, with tail-sampling on by default and full LLM payloads referenced as PVC artifact paths rather than inlined as span attributes
  4. A user can open the dashboard at `https://tide-dashboard.<cluster>/` and see live Planning + Execution DAGs rendered side-by-side via React Flow v12 + dagre + Tailwind v4, with per-task status badges, status updates streaming over Server-Sent Events, click-through to opt-in `pods/log` WebSocket streams via the apiserver proxy — and verify the dashboard exposes zero mutation endpoints (all state changes route through `kubectl` or `tide` CLI) and runs in its own Deployment with its own read-only ServiceAccount distinct from the orchestrator's
  5. A user can run `tide apply -f project.yaml`, `tide watch <project>`, `tide tail <task>`, `tide approve <project>`, `tide inspect-wave <plan>`, and `tide artifact-get <ref>` from a single stateless cobra-based CLI (no local cache) talking to the K8s API — and confirm `tide tail` streams pod logs via the `pods/log` subresource without any local caching

**Research flag**: Recommend `/gsd:research-phase` during planning — React Flow vs htmx is contributor-pool-shaping; two-DAG view UX needs prototyping; SSE-through-ingress concerns; observability data volume (Pitfall 17); dashboard websocket leak prevention (Pitfall 22).

**Plans**: TBD
**UI hint**: yes

### Phase 5: Distribution & Self-Hosting Acceptance
**Goal**: A Helm chart packages CRDs (dedicated subchart for safe upgrades), the controller Deployment, the dashboard Deployment, RBAC, and namespace setup; the release pipeline uses `helmify`; an Apache 2.0 LICENSE is at the repo root; documentation covers install / Project authoring with 3 sample CRDs / provider configuration / git remote configuration / failure recovery / RBAC reference / troubleshooting; an external-operator dry-run confirms clone-to-first-run under 30 minutes; and **the v1 acceptance test passes**: a fresh `kind` cluster + `helm install tide` + `kubectl apply -f project.yaml` drives this repo's next milestone end-to-end, producing committed artifacts on a per-run branch with a clean status. M_self consumes M0's artifacts against the same `v1alpha1` CRD schema with no breaking changes.
**Depends on**: Phase 4
**Requirements**: DIST-01, DIST-02, DIST-03, DIST-04, DIST-05, BOOT-02, BOOT-04
**Success Criteria** (what must be TRUE):
  1. An external operator (running on a clean machine with no prior TIDE checkout) can `git clone` the repo, follow the README quickstart, run `kind create cluster` + `helm repo add tide ...` + `helm install tide tide/tide`, and have a working TIDE installation with CRDs registered, controller + dashboard Deployments healthy, and RBAC namespace-scoped — all in under 30 minutes
  2. The Helm chart packages CRDs as a dedicated subchart (so `helm upgrade` is safe for schema-stable bumps), the controller Deployment, the dashboard Deployment, RBAC, and namespace setup; the release pipeline runs `helmify` to convert kubebuilder's Kustomize output into the published Helm chart; an `Apache-2.0` `LICENSE` file is at the repo root; every Go file's package header references the license
  3. An external operator can read `docs/` and find sections covering: install steps, Project authoring with three sample CRDs (small / medium / large fixture projects), provider configuration (LLM key Secret + per-level model selection), git remote configuration (HTTPS+PAT default, SSH with host-key caveats), failure recovery (chaos-resume, budget-cap bypass, gate approval), RBAC reference (per-Kind verbs), and troubleshooting (finalizer stuck, `kubectl patch` manual unstick recipe)
  4. The v1 acceptance test runs green: a fresh `kind` cluster + `helm install tide` + `kubectl apply -f project.yaml` (where `project.yaml` points at this TIDE repo as the target) drives this repo's next milestone end-to-end — the orchestrator authors `MILESTONE.md` / phase briefs / `PLAN.md` files via real Claude-backed subagents, dispatches per-wave executor Jobs, pushes artifacts to a per-run branch on the remote with `--force-with-lease`, and terminates with a clean `Project.status.phase = Complete`
  5. M_self consumes the artifacts M0 (Phases 1-4) produced under the same `v1alpha1` CRD schema with zero breaking changes — verified by applying the M0-authored sample CRDs against the M_self-installed TIDE and seeing them reconcile without conversion-webhook activation (conversion-webhook scaffolding is in place from day one but unused in v1)

**Research flag**: Recommend `/gsd:research-phase` during planning — self-hosting demo exercises everything; map demo's exact apply→author→plan→dispatch→push sequence against TIDE-on-host behavior to surface drift before integration test runs; OSS-adoption-death-by-missing-docs prevention (Pitfall 24).

**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundation — CRDs, pkg/dag, Controller Scaffold | 0/TBD | Not started | - |
| 2. Dispatch & Plan Validation — Innermost Reconcilers + Harness | 0/TBD | Not started | - |
| 3. Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption | 0/TBD | Not started | - |
| 4. Gates, Observability, Dashboard, CLI | 0/TBD | Not started | - |
| 5. Distribution & Self-Hosting Acceptance | 0/TBD | Not started | - |
