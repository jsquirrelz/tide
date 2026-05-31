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
- [ ] **Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation** — Multi-arch Docker image build + push for all 6 chart-referenced components, chart values.yaml tag-alignment SOT fix, dry-run-v1 cert-manager prereq fix, image-load fallback for local cluster acceptance, BOOT-04 end-to-end revalidation, README + INSTALL.md ship-state corrections

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

**Plans:** 9/13 plans executed

Plans:
**Wave 1**
- [x] 02-01-PLAN.md — pkg/dispatch public envelope contract (EnvelopeIn/Out, Subagent interface, errors) + verify-dispatch-imports gate (Wave 1)
- [x] 02-03-PLAN.md — v1alpha1 schema additions (Project.Spec/Status, Task.Spec.Dev, Plan.Status, shared_types constants) + codegen (Wave 1)

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 02-02-PLAN.md — providerfirewall lint analyzer + cmd/tide-lint multichecker flip (SUB-05) (Wave 2)
- [x] 02-04-PLAN.md — cmd/stub-subagent Go binary + Dockerfile (Wave 2)
- [x] 02-05-PLAN.md — credential proxy (HMAC token + self-signed cert + HTTPS server + cmd/credproxy + Dockerfile) (Wave 2)
- [x] 02-06-PLAN.md — internal/harness package (caps + redact tail-keep buffer + outputs validate + envelope_io) (Wave 2)
- [x] 02-07-PLAN.md — internal/budget package (sync.Map rate bucket + PreCharge + cap check + tally + Prometheus counter) (Wave 2)

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 02-08-PLAN.md — internal/dispatch.Dispatcher interface body + PodJobBackend + JobSpec (native sidecar) + JobName (Wave 3)

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 02-09-PLAN.md — TaskReconciler dispatch body + WaveReconciler observational roll-up + PlanReconciler Wave materialization (Wave 4)

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 02-10-PLAN.md — ProjectReconciler init Job (ART-01) + budget cap halt + bypass annotation watch (Wave 5)
- [x] 02-11-PLAN.md — Plan admission webhook body (cycle detection + file-touch reconciliation + strict/warn precedence) (Wave 5)

**Wave 6** *(blocked on Wave 5 completion)*
- [x] 02-12-PLAN.md — cmd/manager Phase 2 wiring + Helm chart signing-secret template + tide-subagent SA + values keys (Wave 6)

**Wave 7** *(blocked on Wave 6 completion)*
- [x] 02-13-PLAN.md — Integration test tier (envtest Layer A + kind Layer B + cluster.yaml + Make targets + CI gate) (Wave 7)

### Phase 02.2: Layer B kind test timing fixes — bump kindTestTimeout from 4min to 6min so helm --timeout 5m can complete; robust AfterSuite cleanup that handles zombie kind containers when BeforeSuite half-installs; re-scope make test-int wall-time goal to bound only the go test invocation (not test-int-kind-prep image builds + cluster create); optional cert-manager v1.16.2 to v1.20 bump. Closes Phase 02.1's BLOCKED runtime gate captured in 02.1-04-VERIFICATION.md. (INSERTED)

**Goal:** Phase 02.1's BLOCKED runtime gate is closed — `make test-int` clean run reaches 7/7 Layer B specs PASS in ≤ 355s inner go test wall-time AND `KEEP_KIND_CLUSTER=true make test-int` rerun reaches 7/7 PASS, both verified end-to-end on a developer laptop with cert-manager v1.20.2 + helm install --replace + robust AfterSuite zombie cleanup + namespace-local PVC + namespace-local signing key Secret. Empirically closed after 12 iterations (cascades 1–11 CLOSED; chain_status: empirically_closed in 02.2-12-VERIFICATION.md).
**Requirements**: No formal REQ-IDs — Phase 02.2 is debug closeout for Phase 02.1's runtime gate; the de facto requirements are the 4 source-shape fixes + 1 micro-fix enumerated in 02.2-RESEARCH.md.
**Depends on:** Phase 02
**Plans:** 13/13 plans executed

Plans:
- [x] 02.2-01-PLAN.md — Source-shape fixes (kindTestTimeout 7m, cert-manager v1.20.2, cleanupKindCluster helper, helm --replace, CI YAML DUR-check drop) + end-to-end runtime verification (Wave 1)
- [x] 02.2-03-PLAN.md — Chart PVC accessModes Helm values key (override-only; production default ReadWriteMany preserved) + test --set RWO override + runtime re-verification (Wave 2)
- [x] 02.2-04-PLAN.md — TACTICAL: define --metrics-bind-address flag in cmd/manager — close 02.2-03 BLOCKED gate + runtime re-verification (cascade-2: Wave 3)
- [x] 02.2-05-PLAN.md — TACTICAL: define --webhook-cert-path flag + wire into webhook.Options.CertDir — close 02.2-04 BLOCKED gate (cascade-3: Wave 4)
- [x] 02.2-06-PLAN.md — TACTICAL: Makefile test budget 300s→600s — close 02.2-05 BLOCKED gate (cascade-4: Wave 5)
- [x] 02.2-07-PLAN.md — TACTICAL: credproxy fixture-incomplete harness-bug — close 02.2-06 BLOCKED gate (cascade-5: Wave 6)
- [x] 02.2-08-PLAN.md — TACTICAL: Eventually-timeout-too-tight spec-flake fixes — close 02.2-07 BLOCKED gate (cascade-6: Wave 7)
- [x] 02.2-09-PLAN.md — TACTICAL: caps/output/failure-fixture-incomplete harness-bug — close 02.2-08 BLOCKED gate (cascade-7: Wave 8)
- [x] 02.2-10-PLAN.md — TACTICAL: production-wiring-gap (Dispatcher field nil) — close 02.2-09 BLOCKED gate (cascade-8: Wave 9)
- [x] 02.2-11-PLAN.md — TACTICAL: cascade-9 sub-classes A+B+C closure (Job activeDeadlineSeconds, Layer A AC1 Eventually budget, Makefile timeout safety-net: Wave 10)
- [x] 02.2-12-PLAN.md — TACTICAL: cascade-10 PVC namespace-scoping + cascade-11 Secret namespace-scoping — Pod-status envelope transport architectural pivot + ensureProjectsPVC + ensureSigningKeySecret (Wave 11)
- [x] 02.2-02-PLAN.md — ROADMAP/STATE closeout, gated on 02.2-12-VERIFICATION.md gate_decision=APPROVED (Wave 12)

### Phase 02.1: Debug + fix the Layer B kind integration test suite so make test-int runs end-to-end on a developer laptop. Phase 2 shipped the test files + CI wiring; this phase makes them actually run. Goals: tide-controller-manager Deployment reaches Ready in kind, Plan webhook service has live endpoints, all 7 Layer B Ginkgo specs pass (3-task wave, fail injection, wall-clock cap, output-path violation, credproxy sidecar topology + listening log). (INSERTED)

**Goal:** Layer B integration test suite (`make test-int`) runs end-to-end on developer laptop — `tide-controller-manager` Deployment reaches Ready in kind via Helm install, Plan webhook service has live endpoints, all 7 Layer B Ginkgo specs pass.
**Requirements**: No formal REQ-IDs — Phase 02.1 is debug + fix; spec is the ROADMAP goals (controller-manager-ready, webhook-endpoints-live, 7-specs-pass).
**Depends on:** Phase 2
**Plans:** 3/5 plans executed

Plans:
- [x] 02.1-01-PLAN.md — Baseline capture + lock D-01/D-02/D-03 decisions (Wave 1)
- [x] 02.1-02-PLAN.md — Helm-install BeforeSuite pivot + ensureSubagentSA helper (Wave 2)
- [x] 02.1-03-PLAN.md — Credproxy boot-banner for log-line spec (Wave 2)
- [ ] 02.1-04-PLAN.md — CI integration + idempotency verification + phase closeout (Wave 3)

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

**Plans:** 11 plans

Plans:
- [x] 03-01-PLAN.md — pkg/dispatch envelope schema bump: Provider/Role/Level + ChildCRDSpec + cache tokens (Wave 1)
- [x] 03-02-PLAN.md — Project CRD Spec/Status extensions + stub-subagent wait-for-signal mode (Wave 1)
- [x] 03-03-PLAN.md — pkg/git package: Clone, Fetch, AddWorktree, AddPath, Commit, Push with ForceWithLease (Wave 2)
- [x] 03-04-PLAN.md — internal/gitleaks scanner + embedded default rules (Wave 2)
- [x] 03-05-PLAN.md — internal/subagent/common (stream reader + prompt templates) + internal/subagent/anthropic (Wave 2)
- [x] 03-06-PLAN.md — cmd/tide-push binary + Dockerfile + push_helpers (buildPushJob/buildCloneJob) + commit-message support (Wave 3)
- [x] 03-07-PLAN.md — cmd/claude-subagent shim + Dockerfile with @anthropic-ai/claude-code@2.1.142 + harness EnsureWorktree (D-B4) (Wave 3)
- [x] 03-08-PLAN.md — dispatch_helpers + Milestone/Phase/Plan reconciler bodies + ProjectReconciler clone+push extensions + buildCommitMessage (D-B2) (Wave 4)
- [x] 03-09-PLAN.md — cmd/manager wiring + Helm values + push-rbac + docs/git-hosts.md (Wave 5)
- [x] 03-10-PLAN.md — Layer B integration tests: chaos_resume + push_lease + up_stack_dispatch (Wave 6)
- [x] 03-11-PLAN.md — TEST-03 live nightly E2E (//go:build live-e2e + budget-capped fixture) + docs/live-e2e.md (Wave 6)

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

**Plans:** 17 plans

Plans:
- [x] 04-01-PLAN.md — internal/metrics registry + Phase4 API constants + cmd/manager metrics blank import (Wave 1)
- [x] 04-02-PLAN.md — metriccardinality lint analyzer + cmd/tide-lint multichecker registration (Wave 1)
- [x] 04-03-PLAN.md — pkg/otelai OpenInference helpers + internal/otelinit TracerProvider + cmd/manager OTel wiring (Wave 2)
- [x] 04-04-PLAN.md — internal/gates package (policy + annotation + boundary shared seam) (Wave 2)
- [x] 04-05-PLAN.md — Gate-policy hooks in Milestone/Phase/Plan/Task + PauseBetweenWaves + AnnotationChangedPredicate (Wave 3)
- [x] 04-07-PLAN.md — cmd/tide skeleton + read-only verbs (apply/watch/inspect-wave/artifact-get/describe-budget) (Wave 3)
- [x] 04-06-PLAN.md — W-1 exit-10/exit-11 split + counter + W-2 mid-stack boundary push triggers (Wave 4)
- [x] 04-08-PLAN.md — cmd/tide annotation-writer verbs (approve/reject/cancel/resume) + tail (Wave 4)
- [x] 04-09-PLAN.md — .goreleaser.yaml + Krew manifest + release.yaml workflow + docs/cli.md (Wave 4)
- [x] 04-10-PLAN.md — cmd/dashboard skeleton + chi router + Hub + projects API + zero-mutation guard (Wave 4)
- [x] 04-11-PLAN.md — Dashboard SSE handlers (events + pod-log) + informer-bridge (Wave 5)
- [x] 04-12-PLAN.md — dashboard/web scaffold + design tokens + chrome components (Wave 6)
- [x] 04-15-PLAN.md — Status/primitive components (StatusBadge, ProjectPicker, ClipboardCopyAction, WaveBackground) (Wave 7)
- [x] 04-13-PLAN.md — DAG views + 5 custom nodes + TaskDetailDrawer + dagre layout (Wave 8)
- [x] 04-16-PLAN.md — PodLogStreamer + SSE hooks + ANSI parser + EmptyState/ErrorState/LoadingState + bundle-size gate + Makefile embed (Wave 9)
- [x] 04-14-PLAN.md — Helm chart additions (dashboard-deployment + RBAC + ServiceMonitor) + E2E smoke (Wave 10)
- [x] 04-17-PLAN.md — last-mile App.tsx wiring: useProjects + useTasks + useTaskDetail hooks + GET /api/v1/plans/{name} + GET /api/v1/tasks/{name} backend (Wave 11)

**Research flag**: Recommend `/gsd:research-phase` during planning — React Flow vs htmx is contributor-pool-shaping; two-DAG view UX needs prototyping; SSE-through-ingress concerns; observability data volume (Pitfall 17); dashboard websocket leak prevention (Pitfall 22).

**UI hint**: yes

### Phase 04.1: Pre-v1 audit fixes + cross-phase UAT closeout — fix the four P1 production-wiring defects surfaced by the 2026-05-20 merged audit (ProjectReconciler-unwired, skeletal planner Jobs, 300s↔60s caps deadline mismatch, first-Project-in-namespace fallback), close the four P2 CI/tooling-hygiene items (dead dagimports analyzer, two verify-* steps missing from ci.yaml, unguarded `go mod tidy` in test.yml + test-e2e.yml, missing live-e2e workflow), apply the P3 code-shape improvements (reconciler-hotspot extraction in `reconcileDispatch`, `TaskReconciler` deps carrier), implement the P4 improvements (budget rolling-window reset, configurable cred-proxy upstream allowlist, `PROD_OVERRIDE_REQUIRED` markers on dev image tag defaults, logging convention sweep), and burn down the 15 outstanding human-UAT items in 02-/03-/04-VERIFICATION.md against a real kind cluster (now agent-runnable via Docker + kind + Chrome DevTools MCP). Closes the gap between "audit reports the bug" and "Phase 5's v1 acceptance test can actually run." (INSERTED)

**Goal:** Every finding in `docs/audit-report-2026-05-20-merged.md` is resolved in source AND verified, and the 15 cross-phase human-UAT items move from `status: human_needed` to `status: pass` (or generate fix plans if they actually fail under live cluster execution). Exit condition: a green Layer B `make test-int` rerun, a green `make test-e2e-kind`, a live dashboard smoke (Chrome DevTools MCP against `kubectl port-forward svc/tide-dashboard`), and `gsd-sdk query audit-uat` reporting `summary.total_items: 0`. With this phase clean, Phase 5's TIDE-on-TIDE acceptance test is a fair test of v1 distribution and not a debugging session for known defects.
**Requirements**: No formal REQ-IDs — Phase 04.1 is debug + fix + verification closeout for the merged-audit findings.
**Depends on:** Phase 4
**Success Criteria** (what must be TRUE):
  1. **P1.1** — `cmd/manager/main.go` constructs `ProjectReconciler` with `Dispatcher: dispatcher`; a manager-construction test fails if any required dependency is omitted (EnvReader clause dropped per locked decision — ProjectReconciler does not dispatch subagents)
  2. **P1.2** — `internal/dispatch/podjob` factors `BuildJobSpec` accepting a `JobKind` discriminator; mounts the per-Project PVC, runs credproxy sidecar, mints a signed token with shared caps-defaulting; `internal/controller/planner_job_helpers.go` deleted; Milestone/Phase/Plan reconcilers stop discarding the serialized `EnvelopeIn`
  3. **P1.3** — A single shared `podjob.DefaultCaps(caps)` helper applies the 300s wall-clock floor before BOTH token mint (`task_controller.go`) AND Job `activeDeadlineSeconds` derivation (`jobspec.go`); nil-caps test asserts both derive identical deadlines
  4. **P1.4** — `TaskReconciler.resolveProject`, `PlanReconciler.resolveProjectName`, AND `PodJobBackend.Run` (3rd site missed by audit) no longer return `projectList.Items[0]`; on miss, caller sets `ParentUnresolved` condition and requeues
  5. **P2.1–P2.4** — `dagimports.Analyzer` wired into `cmd/tide-lint`; `verify-dispatch-imports` + `verify-import-firewall` named in `ci.yaml`; `test.yml` + `test-e2e.yml` follow `go mod tidy` with `git diff --exit-code go.mod go.sum`; `.github/workflows/live-e2e.yml` exists with `workflow_dispatch:` ONLY (locked decision — no `schedule:` cron)
  6. **P3.1** — `TaskReconciler.reconcileDispatch` decomposed into 4 named methods (gateChecks / acquireDispatchSlots / prepareDispatch / createDispatchJob), each ≤ 80 lines
  7. **P3.2** — `TaskReconciler` dispatch-tier fields consolidated into `TaskReconcilerDeps` carrier mirroring `HelmProviderDefaults`
  8. **P4.1** — `ProjectReconciler.MaybeResetWindow` zeros `CostSpentCents` + `TokensSpent` on `now.Sub(WindowStart) >= window`; new `RollingWindowDuration *metav1.Duration` field (locked decision — backward-compatible, omitempty, default 24h); WR-02 caveat removed
  9. **P4.2** — cred-proxy allowlist driven by `Spec.Providers[*].AllowedRoutes` field with hardcoded safe defaults; webhook denylist rejects `/v1/admin` + `/v1/billing`; new routes addable without rebuilding credproxy image
  10. **P4.3** — `// PROD_OVERRIDE_REQUIRED` comments on `TIDE_PUSH_IMAGE` + `CLAUDE_SUBAGENT_IMAGE` dev tag defaults
  11. **P4.4** — Reconciler log strings under `internal/controller/` follow k8s conventions; `logcheck` linter tightened
  12. **UAT closeout** — All 15 cross-phase `human_needed` UAT items flip to `pass` (Phase 03 items 3+4 stale-re-verified per locked decision — Phase 4 source already closed them)
  13. `gsd-sdk query audit-uat` reports `summary.total_items: 0` before Phase 5 starts (or 1-2 with explicit caveats documented)

**Planning hint — wave staggering** (planner re-layered Kahn-style):
- **Wave 0:** Plan 01 — Tooling readiness gate (Docker + kind + helm + gsd-sdk + Chrome DevTools MCP probes)
- **Wave 1:** Plans 02 + 03 — P1.1 wire ProjectReconciler.Dispatcher + P1.3 DefaultCaps helper (parallel; no file overlap)
- **Wave 2:** Plan 04 — P1.4 ParentUnresolved (shares task_controller.go with Plan 03, so sequential)
- **Wave 3:** Plans 05 + 06 — P1.2 planner Job contract + P2.1-P2.4 CI hygiene (parallel)
- **Wave 4:** Plan 07 — P3.1 reconcileDispatch decomposition (depends on P1 fixes)
- **Wave 5:** Plan 08 — P3.2 TaskReconcilerDeps carrier (shares task_controller.go with Plan 07, so sequential)
- **Wave 6:** Plans 09 + 12 — P4.1 budget rolling-window + Phase 02 UAT runner (parallel; different file sets)
- **Wave 7:** Plans 10 + 13 — P4.2 credproxy allowlist + Phase 03 UAT runner (parallel)
- **Wave 8:** Plan 11 — P4.3 + P4.4 PROD_OVERRIDE + logging sweep (shares cmd/manager/main.go with Plan 10, so sequential)
- **Wave 9:** Plan 14 — Phase 04 UAT runner (depends on Plans 10/11/12/13)
- **Wave 10:** Plan 15 — Phase 04.1 closeout (depends on all UAT runners)

(Waves 0-10; planner re-layered Kahn-style from the original 7-wave hint to honor file-overlap implicit dependencies.)

**Plans:** 15/15 plans executed

Plans:
- [x] 04.1-01-PLAN.md — Wave 0 tooling readiness gate (Wave 0)
- [x] 04.1-02-PLAN.md — P1.1 wire ProjectReconciler.Dispatcher + manager-construction test (Wave 1)
- [x] 04.1-03-PLAN.md — P1.3 shared DefaultCaps helper for 300s wall-clock floor (Wave 1)
- [x] 04.1-04-PLAN.md — P1.4 remove first-Project fallback (3 sites) + ParentUnresolved condition (Wave 2)
- [x] 04.1-05-PLAN.md — P1.2 planner Job contract refactor via JobKind discriminator (Wave 3)
- [x] 04.1-06-PLAN.md — P2.1-P2.4 CI/tooling hygiene bundle (Wave 3)
- [x] 04.1-07-PLAN.md — P3.1 TaskReconciler.reconcileDispatch decomposition (Wave 4)
- [x] 04.1-08-PLAN.md — P3.2 TaskReconcilerDeps carrier struct (Wave 5)
- [x] 04.1-09-PLAN.md — P4.1 budget rolling-window reset + RollingWindowDuration field (Wave 6)
- [x] 04.1-10-PLAN.md — P4.2 cred-proxy upstream allowlist via Spec.Providers[*].AllowedRoutes (Wave 7)
- [x] 04.1-11-PLAN.md — P4.3 PROD_OVERRIDE markers + P4.4 logging convention sweep + logcheck tighten (Wave 8)
- [x] 04.1-12-PLAN.md — Phase 02 UAT runner — make test-int 6/6 items closed (Wave 6)
- [x] 04.1-13-PLAN.md — Phase 03 UAT runner — items 3+4 stale-flipped, 1+2+5 verified (Wave 6)
- [x] 04.1-14-PLAN.md — Phase 04 UAT runner — gate flow + dashboard + CLI (Wave 9)
- [x] 04.1-15-PLAN.md — Phase 04.1 closeout — ROADMAP + STATE update (Wave 10)

### Phase 5: Distribution & Self-Hosting Acceptance
**Goal**: A Helm chart packages CRDs (dedicated subchart for safe upgrades), the controller Deployment, the dashboard Deployment, RBAC, and namespace setup; the release pipeline uses `helmify`; an Apache 2.0 LICENSE is at the repo root; documentation covers install / Project authoring with 3 sample CRDs / provider configuration / git remote configuration / failure recovery / RBAC reference / troubleshooting; an external-operator dry-run confirms clone-to-first-run under 30 minutes; and **the v1 acceptance test passes**: a fresh `kind` cluster + `helm install tide` + `kubectl apply -f project.yaml` drives this repo's next milestone end-to-end, producing committed artifacts on a per-run branch with a clean status. M_self consumes M0's artifacts against the same `v1alpha1` CRD schema with no breaking changes.
**Depends on**: Phase 04.1
**Requirements**: DIST-01, DIST-02, DIST-03, DIST-04, DIST-05, BOOT-02, BOOT-04
**Success Criteria** (what must be TRUE):
  1. An external operator (running on a clean machine with no prior TIDE checkout) can `git clone` the repo, follow the README quickstart, run `kind create cluster` + `helm repo add tide ...` + `helm install tide tide/tide`, and have a working TIDE installation with CRDs registered, controller + dashboard Deployments healthy, and RBAC namespace-scoped — all in under 30 minutes
  2. The Helm chart packages CRDs as a dedicated subchart (so `helm upgrade` is safe for schema-stable bumps), the controller Deployment, the dashboard Deployment, RBAC, and namespace setup; the release pipeline runs `helmify` to convert kubebuilder's Kustomize output into the published Helm chart; an `Apache-2.0` `LICENSE` file is at the repo root; every Go file's package header references the license
  3. An external operator can read `docs/` and find sections covering: install steps, Project authoring with three sample CRDs (small / medium / large fixture projects), provider configuration (LLM key Secret + per-level model selection), git remote configuration (HTTPS+PAT default, SSH with host-key caveats), failure recovery (chaos-resume, budget-cap bypass, gate approval), RBAC reference (per-Kind verbs), and troubleshooting (finalizer stuck, `kubectl patch` manual unstick recipe)
  4. The v1 acceptance test runs green: a fresh `kind` cluster + `helm install tide` + `kubectl apply -f project.yaml` (where `project.yaml` points at this TIDE repo as the target) drives this repo's next milestone end-to-end — the orchestrator authors `MILESTONE.md` / phase briefs / `PLAN.md` files via real Claude-backed subagents, dispatches per-wave executor Jobs, pushes artifacts to a per-run branch on the remote with `--force-with-lease`, and terminates with a clean `Project.status.phase = Complete`
  5. M_self consumes the artifacts M0 (Phases 1-4) produced under the same `v1alpha1` CRD schema with zero breaking changes — verified by applying the M0-authored sample CRDs against the M_self-installed TIDE and seeing them reconcile without conversion-webhook activation (conversion-webhook scaffolding is in place from day one but unused in v1)

**Research flag**: Recommend `/gsd:research-phase` during planning — self-hosting demo exercises everything; map demo's exact apply→author→plan→dispatch→push sequence against TIDE-on-host behavior to surface drift before integration test runs; OSS-adoption-death-by-missing-docs prevention (Pitfall 24).

**Plans:** 16/16 plans executed

Plans:
**Wave 1** *(parallel — no cross-deps; mostly disjoint file paths)*
- [x] 05-01-PLAN.md — LICENSE + NOTICE + verify-license.sh (DIST-03)
- [x] 05-02-PLAN.md — CONTRIBUTING.md + SECURITY.md (DIST-04)
- [x] 05-03-PLAN.md — README Quickstart prepend (DIST-04, D-C1)
- [x] 05-04-PLAN.md — docs/README.md index + concepts.md + verify-docs-coverage.sh (DIST-04, D-C3 — 11-entry index)
- [x] 05-05-PLAN.md — Chart.yaml lockstep version bump 0.1.0-dev → 1.0.0 (DIST-01, D-X3)
- [x] 05-06-PLAN.md — examples/tide-demo-fixture/ MIT-licensed scaffold (DIST-04, D-B3)
- [x] 05-09-PLAN.md — docs/rbac.md (DIST-04 + AUTH-02 catch-up doc + D-X7)
- [x] 05-10-PLAN.md — docs/troubleshooting.md (DIST-04, D-C4 — 13-row table)

**Wave 2** *(depends on Wave 1 — docs reference samples; chart additions depend on 05-05 version lock)*
- [x] 05-07-PLAN.md — docs/INSTALL.md (DIST-04 + D-C2 + Pitfall 4 mitigation)
- [x] 05-08-PLAN.md — docs/project-authoring.md (DIST-04 + Variant B prompt guidance)
- [x] 05-11-PLAN.md — examples/projects/{small,large}/ samples (DIST-04 + BOOT-04 acceptance project.yaml)
- [x] 05-13-PLAN.md — per-namespace-rolebinding.yaml + projectNamespaces values key (DIST-01 + AUTH-02 catch-up template)
- [x] 05-14-PLAN.md — CRD-subchart resource-policy: keep annotation, Wave 1 per HIGH-2 (DIST-01, Pitfall 2)

**Wave 3** *(depends on Wave 1 + Wave 2 — medium sample uses cmd/tide-demo-init binary)*
- [x] 05-12-PLAN.md — cmd/tide-demo-init/ binary + medium/ sample (DIST-04 + D-B3 + RESEARCH Topic 4; embed strategy locked per MEDIUM-11)

**Wave 4** *(depends on chart-finalized + samples-finalized)*
- [x] 05-15-PLAN.md — Makefile dry-run-v1 + acceptance-v1 + 4 hack/scripts (DIST-05 + BOOT-02 + BOOT-04; 3-of-4 commit shapes per MEDIUM-6)

**Wave 5** *(depends on dry-run plumbing)*
- [x] 05-16-PLAN.md — release.yaml extensions + dry-run.yaml (DIST-01 + DIST-02 + DIST-05; parent-version-filtered rc match per MEDIUM-9)

**Wave 6** *(depends on all 16)*
- [x] 05-17-PLAN.md — Phase 5 closeout (ROADMAP + STATE update + 05-SUMMARY.md)

### Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation
**Goal**: Every Docker image the `charts/tide` chart references is buildable and publishable from a real pipeline, the chart's component tags resolve to the chart `appVersion` instead of the dead `v0.1.0-dev` pin, and the BOOT-04 operator ritual completes end-to-end green at **$0** (locally-built + kind-loaded images, no real LLM spend) — closing the image-publish gap that the 2026-05-30 BOOT-04 retry exposed.
**Depends on**: Phase 5
**Requirements**: IMG-01, CHART-01, DRY-01, IMG-LOAD-01, ACC-01, DOC-01, HYG-01
**Plans:** 4/6 plans executed

Plans:
- [x] 06-01-PLAN.md — CHART-01 SOT tag alignment (5 v0.1.0-dev → appVersion) + HYG-01 gitignore + troubleshooting + A7 project.yaml fix (Wave 1)
- [x] 06-02-PLAN.md — D-02 Dockerfile --platform=$BUILDPLATFORM cross-compile refactor across all 6 component images (Wave 2)
- [x] 06-03-PLAN.md — D-01/D-04 build-images matrix job in release.yaml + chart-publish needs extension (Wave 2)
- [x] 06-04-PLAN.md — IMG-LOAD-01/DRY-01/D-05: load-images-if-needed.sh helper + acceptance-v1.sh $0 mode + dry-run-v1.sh cert-manager + Makefile targets (Wave 3)
- [x] 06-05-PLAN.md — DOC-01 INSTALL.md Maintainer image-publish section + premature-claim audit (Wave 4)
- [x] 06-06-PLAN.md — ACC-01 $0 BOOT-04 closeout gate: make acceptance-v1-smoke green + D-06 evidence captured (Wave 5)


## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundation — CRDs, pkg/dag, Controller Scaffold | 0/TBD | Not started | - |
| 2. Dispatch & Plan Validation — Innermost Reconcilers + Harness | 9/13 | In Progress|  |
| 02.2. Layer B kind test timing fixes (INSERTED) | 13/13 | Complete | 2026-05-14 |
| 3. Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption | 0/TBD | Not started | - |
| 4. Gates, Observability, Dashboard, CLI | 17/17 | Complete | 2026-05-21 |
| 04.1. Pre-v1 audit fixes + cross-phase UAT closeout (INSERTED) | 15/15 | Complete | 2026-05-22 |
| 5. Distribution & Self-Hosting Acceptance | 17/17 | Complete | 2026-05-23 |
| 6. v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation | 6/6 | Complete | 2026-05-30 |
| 7. Project-to-Milestone Authoring and Self-Bootstrap | 0/6 | In Progress | - |

8 of 9 milestone phases complete — Phase 7 planning complete; executing to close cascade-7 (v1.0 ship blocker).

### Phase 7: Project-to-Milestone Authoring and Self-Bootstrap

**Goal:** A bare `Project` CRD self-bootstraps the full five-level cascade — TIDE authors its `Milestone`, which drives `Phase → Plan → Task` — and reaches `Project status.phase=Complete` at `$0` (stub-driven, no API key), closing cascade-7, the v1.0 ship blocker.

**Requirements**: REQ-1, REQ-2, REQ-3, REQ-4, REQ-5, REQ-6, REQ-7 (REQ-7 splits into REQ-7a ValidationState stamp + REQ-7b patchPlanSucceeded)
**Depends on:** Phase 3 (down-stack Milestone→Phase→Plan→Task reconcilers — already wired), Phase 6 (image-publish pipeline — shipped)
**Plans:** 6 plans

**Acceptance gate:** `make acceptance-v1-smoke` reaches `Project status.phase=Complete` at `$0` (no API key). On green, v1.0 is ship-ready.

**Scope-of-record:** `.planning/phases/07-project-to-milestone-authoring-and-self-bootstrap/07-SPEC.md`

Plans:
**Wave 0** *(parallel — test scaffolds before implementation)*
- [ ] 07-01-PLAN.md — Wave 0 test scaffolds: stub-subagent planner unit test (RED) + bare-project.yaml fixture (REQ-3, REQ-5)
- [ ] 07-02-PLAN.md — Wave 0 Layer B integration spec: bare_project_test.go asserting full cascade + Project=Complete (REQ-1, REQ-2, REQ-4, REQ-5, REQ-7a, REQ-7b)

**Wave 1** *(blocked on Wave 0 — unit test must exist before implementation)*
- [ ] 07-03-PLAN.md — Stub planner-mode ChildCRD tree (dispatchPlannerSuccess) + parentName injection in BuildPlannerEnvelope (REQ-3)

**Wave 2** *(blocked on Wave 1 — needs stub parentName contract)*
- [ ] 07-04-PLAN.md — Down-stack fixes: ValidationState=Validated stamp in handlePlannerJobCompletion + patchPlanSucceeded via BoundaryDetected(plan,Task) (REQ-7a, REQ-7b)

**Wave 3** *(blocked on Wave 2 — needs down-stack fixes to be in place)*
- [ ] 07-05-PLAN.md — ProjectReconciler 5th dispatch site: 5 struct fields + manager wiring + reconcileProjectPlannerDispatch + handleProjectJobCompletion + checkProjectComplete (REQ-1, REQ-2, REQ-4)

**Wave 4** *(blocked on Wave 3 — all production code must be green)*
- [ ] 07-06-PLAN.md — Final acceptance gate: make test-int (7+1/7 Layer B + 18/18 Layer A) + make acceptance-v1-smoke → Project=Complete (REQ-6)
