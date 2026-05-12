# TIDE — Project Research Summary

**Project:** TIDE — Topologically-Indexed Dependency Execution
**Domain:** Kubernetes-native orchestrator for hierarchical agentic coding work
**Researched:** 2026-05-12
**Overall confidence:** HIGH

## Executive Summary

TIDE is a K8s-native operator that runs the five-level paradigm (Project → Milestone → Phase → Plan → Task, with Waves derived) as five CRDs and six reconcilers on a single controller-runtime Manager. The four research dimensions converge on a tight, opinionated technical posture: Go 1.26 + controller-runtime v0.24 + kubebuilder v4.14, one reconciler per Kind, one Manager with two separately-sized parallelism budgets (planner pool ~16, executor pool ~4), a pluggable `Subagent` interface whose v1 concrete impl is Claude Code (with Anthropic SDK fallback) dispatched as `Pod-per-Task Job + envelope on PVC + exit code`, CRD-`.status`-only persistence with resumption state being exactly `indegree map + completed-task set` re-derived from artifacts on every reconcile, a read-only React Flow dashboard as a separate Deployment, Helm + Apache 2.0 distribution, and a single self-hosting acceptance test ("TIDE in kind drives its own next milestone on this repo") as the v1 bar.

The load-bearing structural commitments — two typed DAGs sharing one pure `pkg/dag` Kahn-layered implementation, artifacts-as-source-of-truth with CRDs as cache, cycles-as-bugs (no recovery), strict-by-default wave-boundary failure semantics — are reinforced from at least three of the four research dimensions and should be treated as hard contracts the roadmap and CRD schemas must encode, not soft preferences subject to refactor pressure. The paradigm itself is the differentiator: no comparable system (Argo, Tekton, Temporal, kagent, LangGraph, Composio AO) ships two typed DAGs, five cognitive levels as CRDs, or per-level human gate policy on a single object.

The dominant risk concentration is Phase 1 (CRD schema + controller scaffold), which all four dimensions independently flag as the densest pitfall window: long-running reconcile, status-as-truth resumption bugs, DAG unification, unified worker pool collapse, RBAC scope creep, breaking CRD schema changes, finalizer leaks, and wrong owner refs all bake in here. Phase 2 (Kahn + Subagent harness + admission) follows with the load-bearing security and correctness hazards: subagent context bleed via shared PVC, runaway agent loops draining budget, LLM rate-limit cascade failures, secret leakage, watch-lag duplicate dispatch. The bootstrap-deadlock pitfall (Pitfall 12) is uniquely addressed at the roadmap-construction phase — the roadmap itself must name an explicit M0 ("TIDE-on-host runs TIDE-on-self via GSD") and M_self ("TIDE-in-cluster authors same artifacts") with bounded scope.

## Key Findings

**Recommended Stack (HIGH):** Go 1.26 + controller-runtime v0.24 + kubebuilder v4.14; native K8s Jobs (not Argo/Tekton — they encode DAG semantics that would shadow TIDE's); Anthropic SDK v1.42 + Claude Code v2.1.139+ headless mode; CRD `.status` only, no DB; CEL CRD validation (K8s 1.29 GA); Helm 3 + helmify; `prometheus/client_golang` + OTel v1.43; **OpenInference attribute conventions hand-emitted (no Go SDK exists in 2026)** wrapped in `pkg/otelai`; React Flow v12 + dagre + Tailwind v4 + SSE for the dashboard; `go-chi/chi/v5` HTTP router (composes with `manager.Runnable`); `go-git/v5` HTTPS+PAT default; Ginkgo v2 + envtest + kind v0.31 for testing.

**Expected Features (HIGH):** 27 table-stakes (TS-1..27), 13 differentiators (D-1..13), 16 anti-features (AF-1..16), 8 deferred (DF-1..8). Closest neighbor by posture is kagent (does *agent topology*; TIDE does *work topology*). Closest by functional shape is Composio AO (parallel coding agents, but host-process and includes PR-lifecycle automation TIDE defers). v1 must-have spans: apply/watch/cancel/resume lifecycle, Pod-per-Task isolation, wave-derived schedule + cycle detection + plan validation, strict-by-default failure profile, resume via indegree+completed-set, artifacts on PVC + git push at level boundaries (host-agnostic), five CRDs, per-level gate policy, two-pool concurrency, pluggable Subagent interface, per-level model selection, read-only two-DAG dashboard, structured logs + Prometheus + OTel/OpenInference, K8s Secret refs, namespace-per-project, Helm chart, Apache 2.0 + docs, **self-hosting demo as v1 acceptance test**. Unique TIDE differentiators: two typed DAGs at the API boundary, five-level hierarchy as five CRDs, slack-tide between-wave checkpoints, two-DAG dashboard view, self-hosting as acceptance test, water/tide vocabulary.

**Architecture Approach (HIGH):** Single in-cluster control plane (one TIDE Deployment per cluster install, leader-elected, namespace-per-Project tenancy). Six reconcilers on one Manager — `Project/Milestone/Phase/Plan/Wave/Task`. `pkg/dag` (pure Go, no K8s deps) consumed twice with typed-apart call sites. `pkg/dispatch` Subagent interface + `PodJobBackend`. Two `chan struct{}` semaphores for two budgets, pre-charged on restart from live Job state. Six CRDs with owner-reference cascade + same-namespace + `BlockOwnerDeletion: true`. One PVC per Project (RWX, layout `/workspace/{repo,artifacts/M-N/P-N/L-N,envelopes}`). One subagent image with role/level flags (not two — divergence is prompt + tool-allowance, not binary). Git push at level boundaries **from orchestrator, not subagent pods** (one cred handler, one process). Dashboard as separate Deployment with read-only ServiceAccount, direct apiserver `pods/log` WebSocket proxy. `tide` CLI as thin stateless cobra wrapper.

**Critical Pitfalls (HIGH):** 24 named pitfalls, **9 Catastrophic**. Top critical and prevention:

1. **Bootstrap deadlock (Pitfall 12)** — explicit M0 + M_self in roadmap, single CRD schema across the bridge. *Phase 0.*
2. **Long-running reconcile (Pitfall 1)** — event-driven `Owns(&batchv1.Job{})`, lint rule against Sleep/blocking. *Phase 1.*
3. **Status-as-truth resumption bug (Pitfall 4)** — `.status` is observation, not derivation; schema review checklist. *Phase 1.*
4. **Cached wave schedule (Pitfall 2)** — wave-derivation function is pure; review-block any `Status.Waves`/`Status.Schedule` PR. *Phase 2.*
5. **Subagent context bleed via shared PVC (Pitfall 7)** — per-Job mount scoping; harness validates diff against declared output paths. *Phase 2.*
6. **Runaway agent loops draining budget (Pitfall 8)** — per-Task wall-clock/iteration/token caps, per-Project rolling-window gate, per-Project absolute cap. *Phase 2.*
7. **Secret leakage in artifacts/logs (Pitfall 18)** — harness signed-token proxy, log redaction, gitleaks at push boundary. *Phases 2+3.*
8. **Breaking CRD schema changes (Pitfall 16)** — alpha versioning, conversion-webhook scaffold from day one, dedicated CRD subchart. *Phase 1.*
9. **OSS adoption death by missing docs (Pitfall 24)** — external-operator dry-run, <30min clone-to-first-run. *Phase 5.*

Plus 15 Serious: DAG unification (Pitfall 3), cycle "recovery" creep (5), unified worker pool (6), RBAC scope creep (15), finalizer leaks (21), wrong owner refs (23), indegree-on-partial-failure (10), watch-lag duplicate dispatch (11), rate-limit cascade (9), TIDE-overwrites-human-commits (13), hallucinated `depends_on` (19), provider/host leaks (14), observability data volume (17), dashboard websocket leaks (22), test-cost flakiness (20).

## Convergence: Hard Contracts (3+ Dimensions Agree)

Sixteen items where multiple dimensions converge. These are non-negotiable for v1:

| Contract | Stack | Features | Architecture | Pitfalls |
|----------|:-----:|:--------:|:------------:|:--------:|
| `pkg/dag` pure-Go, stdlib-only Kahn-layered library | rejects Gonum/dominikbraun | TS-7, TS-6, D-3 | Pattern 3 | P2, P3, P10 |
| Two CRD-typed DAGs, never unified | rejects Argo flat DAG | D-1 | typed apart | P3 Serious |
| Pod-per-Task Job + envelope on PVC + exit code | Claude Code stream-json | TS-8, TS-21 | Pattern 5 | P7, P11 |
| CRD `.status` only, no DB/SQLite | rejects Postgres/SQLite | TS-4, D-4 | Pattern 4 | P4 catastrophic |
| Resumption = indegree + completed-set, O(V+E) | derived | TS-4, D-4 | Resumption Flow | P2, P4 |
| Two separately-sized parallelism budgets | Helm values | TS-9, D-12 | Pattern 4 | P6 Serious |
| Pluggable Subagent interface; zero Anthropic in orchestrator | interface firewall | TS-21, AF-10 | Pattern 5 | P14 lint rule |
| Cycles rejected at plan-validation, no recovery | CEL/webhook | TS-6, TS-17, D-10 | PlanReconciler | P5 Serious |
| Strict-by-default wave-boundary failure | — | TS-16, DF-1 deferred | strict-profile | P10 |
| Artifacts-as-source-of-truth, CRDs as cache | — | D-9 | Resumption Flow | P4 |
| One reconciler per CRD Kind | controller-runtime best practice | TS-18 | Pattern 1 + AP1 | P1, P6 |
| Read-only dashboard, mutations via CLI/kubectl | React Flow + SSE | AF-3, D-8 | Pattern 8 | P22 |
| Helm + Apache 2.0 + namespace-per-project | helmify | TS-20, TS-23, TS-27 | namespace tenancy | P15, P24 |
| Self-hosting MVP as v1 acceptance test | kind v0.31 | TS-26, D-13 | step 13 | P12 |
| OpenInference on OTel (no Go SDK, hand-rolled) | hand-rolled | TS-15 | pkg/otel | P17 |
| Water/tide vocabulary across CRDs/logs/dashboard | — | D-11 | — | culturally enforced |

## Divergence: Open Questions for Requirements / Roadmap

Eleven places where dimensions diverged or left a real decision unresolved. The roadmap author should not re-decide silently:

1. **CEL-only vs. validating admission webhook** for CRD invariants — Stack prefers CEL; Pitfall 5 says "webhook on Plan and Task" for cycle detection. Resolution: CEL for what it handles cleanly, webhook only if cross-object cycle check exceeds CEL. (Phase 1.)
2. **`go-git` HTTPS+PAT vs SSH vs shell-out to `git`** — Stack notes SSH host-key fussiness. Resolution: default HTTPS+PAT; SSH documented with caveat. (Phase 3.)
3. **PVC RWX driver matrix the Helm chart documents** — MEDIUM confidence per Architecture. Resolution: leave `storageClassName` empty; docs enumerate EFS/Filestore/Azure Files/NFS-CSI/Longhorn; kind dev loop uses `csi-driver-nfs`. (Phase 3/5.)
4. **Per-Project vs global semaphores when many Projects share a cluster** — flagged as v1.1 tuning. Resolution: v1 = global simple; v1.1 if starvation surfaces.
5. **React Flow vs htmx + Mermaid for dashboard** — Stack calls this taste-driven. Resolution: React Flow v1; htmx alternative documented. (Phase 4.)
6. **`Wave` as its own CRD Kind vs inline field of `Plan`** — Architecture argues strongly for separate Kind; PROJECT.md leaves it ambiguous. Resolution: separate `Wave` Kind. (Phase 1.)
7. **Dashboard inside Manager vs separate Deployment** — Architecture Pattern 8 is opinionated about separate Deployment. Resolution: separate. (Phase 4.)
8. **Conservative vs strict failure profile as v1 default** — Resolution: strict per PROJECT.md; conservative becomes per-Project setting only after real cascading-failure cases.
9. **CRD versioning strategy (alpha free-iteration vs formal alpha→beta→v1)** — Pitfall 16 prefers formal tiers. Resolution: v1alpha1 for all of v1; conversion-webhook scaffolding from day one. (Phase 1.)
10. **Per-level model selection in v1 CRD vs hard-coded defaults** — Feature D-5 argues for CRD field; PROJECT.md ambiguous. Resolution: expose in CRD because differentiation argument depends on it. (Phase 2/4.)
11. **File-touch derived edges as admission validation** — Pitfall 19 strongly recommends; Features/Architecture don't surface explicitly. Resolution: add as Phase 2 admission validation (mismatch = warning or strict-mode rejection).

## Implications for Roadmap

The build order in `ARCHITECTURE.md` ("Build Order Implications" §) is the strongest baseline. STACK pins technologies into phases; FEATURES says which capabilities each phase delivers; PITFALLS says which phase carries the densest baked-in risks (Phase 1) vs. security/correctness fanout (Phase 2). Suggested shape:

**Phase 0: Roadmap Construction** — Uniquely addresses Pitfall 12 (bootstrap deadlock). Names M0 (TIDE-on-host runs TIDE-on-self via GSD, bounded scope) and M_self (TIDE-in-cluster authors same artifacts via fresh-kind-cluster acceptance test). Commits to v1alpha1 stability across the bridge.

**Phase 1: Foundation — CRDs + Controller Scaffold + `pkg/dag`** — Densest pitfall window. Delivers: `pkg/dag` (pure Go Kahn-layered with cycle detection), six CRD types (`Project/Milestone/Phase/Plan/Task/Wave`) with Spec/Status separation + alpha versioning + conversion-webhook scaffolding, owner-ref helper (same-namespace + `BlockOwnerDeletion: true`), six reconciler stubs on one Manager with independent `MaxConcurrentReconciles`, two `chan struct{}` semaphores wired with `plannerConcurrency`/`executorConcurrency` Helm values, kubebuilder RBAC markers (no wildcards), finalizers with bounded deadline + idempotence. Implements TS-18, TS-9, D-12, TS-6, TS-7, D-1, D-2, D-10, D-11. Avoids Pitfalls 1, 3, 4, 6, 15, 16, 21, 23.

**Phase 2: Subagent Dispatch + Plan Validation + Innermost Reconcilers** — The dogfood-critical pair (`TaskReconciler` + `WaveReconciler`). Delivers: `pkg/dispatch` interface + `PodJobBackend` + `stub-subagent` (for tests), subagent image (one image, role/level flags, budget caps, signed-token proxy, log redaction), token-bucket rate limiter (`tide_provider_rate_limit_hits_total`), idempotent Job dispatch (deterministic Job names `tide-task-{task-uid}-{attempt}`), per-task indegree decrement (not per-wave), plan admission with cycle detection + file-touch-derived-edge reconciliation against LLM-declared `depends_on`, strict-by-default wave-boundary failure handling, custom go-analyzer lint rule (no Anthropic SDK imports in orchestrator package). Implements TS-21, TS-8, TS-5, TS-16, TS-17, D-5, AF-10 prevention. Avoids Pitfalls 2, 5, 7, 8, 9, 10, 11, 14, 18 (harness side), 19, 20.

**Phase 3: Artifacts + Git Integration + Up-Stack Reconcilers** — Delivers: PVC layout helper, `pkg/git` (HTTPS+PAT default, host-agnostic, per-run branches `tide/run-<project>-<timestamp>`, `--force-with-lease` only, never `main`), gitleaks scanner at every push (fail on pattern match), `PlanReconciler`/`PhaseReconciler`/`MilestoneReconciler`/`ProjectReconciler`, real Claude-Code-backed subagent image replacing stub, resumption acceptance test (kill orchestrator mid-wave, verify resume from CRD status + PVC only). Implements TS-11, TS-12, TS-22, TS-19, TS-4, D-4, D-9, TS-1, TS-2, TS-3, TS-20. Avoids Pitfalls 13, 18 (push side).

**Phase 4: Human Gates + Observability + Dashboard + CLI** — Parallelizable after Phase 3. Delivers: per-level gate policy on Project CRD + `tide approve` + slack-tide between-wave review, structured JSON logs (zap-behind-logr), Prometheus metrics with bounded cardinality (project/phase/plan only, no per-task labels), OTel tracing with hand-rolled `pkg/otelai` emitting OpenInference attributes + tail-sampling + LLM payloads as artifact refs, `tide` CLI (cobra: apply/watch/tail/approve/cancel/resume/inspect-wave/artifact-get), read-only dashboard (React Flow + dagre + Tailwind + SSE for status + WebSocket for pod logs via apiserver) on separate Deployment with separate read-only ServiceAccount + idle-timeout + stream-rate cap + per-task log opt-in. Implements TS-10, D-6, D-7, TS-13, TS-14, TS-15, TS-24, TS-25, D-8. Avoids Pitfalls 17, 22.

**Phase 5: Distribution + OSS Readiness + Self-Hosting Demo** — Delivers: Helm chart (CRDs + controller Deployment + dashboard Deployment + RBAC + namespace; dedicated CRD subchart for upgrades; ServiceMonitor gated), Apache 2.0 LICENSE, docs (install + Project authoring with 3 sample CRDs + provider configuration + git remote configuration + failure recovery + RBAC reference + troubleshooting), external-operator dry-run acceptance test (<30 min clone-to-first-run), **self-hosting demo: fresh kind + Helm install + `kubectl apply -f project.yaml` drives this repo's next milestone end-to-end**. Implements TS-23, TS-26, D-13, TS-27. Avoids Pitfall 24.

**Phase ordering rationale:** Foundation before fanout (Phase 1 is longest-pole AND densest pitfall window). Dispatch correctness before up-stack (Phase 2 with stub before Phase 3 with real LLM decouples K8s-time learning from LLM-time learning). Observability + UX in parallel after dispatch (Phase 4 components don't depend on each other). Self-hosting last and longest (Phase 5 exercises everything simultaneously — budget for it being the longest calendar item).

**Research flags** — Phases likely needing `/gsd:research-phase` during planning:
- **Phase 2:** densest novel territory (per-Job mount scoping, signed-token proxy, harness budget enforcement, rate-bucket-aware dispatch, file-touch-derived-edges admission).
- **Phase 3:** `go-git` vs shell-out tradeoffs for non-GitHub hosts; RWX PVC driver matrix testing; per-run branch + `--force-with-lease` integration design.
- **Phase 4:** React Flow vs htmx is contributor-pool-shaping; two-DAG view UX needs prototyping; SSE-through-ingress concerns.
- **Phase 5:** self-hosting demo exercises everything; map demo's exact apply→author→plan→dispatch→push sequence against TIDE-on-host behavior to surface drift before integration test runs.

Phases with established patterns (skip research-phase):
- **Phase 1:** kubebuilder good-practices well-documented; follow them carefully.

## v1 Feature Consensus (For Requirements Author)

All four dimensions agree these ship in v1:

- Five CRDs (Project/Milestone/Phase/Plan/Task) + Wave CRD, alpha versioning, conversion-webhook scaffolding
- `pkg/dag` Kahn-layered + cycle detection (pure Go, stdlib only)
- Six reconcilers on one Manager, event-driven, independent concurrency
- Two parallelism budgets (`plannerConcurrency`, `executorConcurrency`)
- Pluggable Subagent interface + `PodJobBackend` + `stub-subagent` (zero Anthropic SDK imports in orchestrator)
- One subagent image (role/level flags, budget caps, signed-token proxy, log redaction)
- CRD `.status` only for persistence; resumption = indegree + completed-set, O(V+E)
- Strict-by-default failure profile (per-task indegree decrement, not per-wave)
- Cycle rejection at admission (CEL or webhook) with useful error
- File-touch-derived-edge reconciliation in admission
- Idempotent Job dispatch (deterministic names)
- Token-bucket rate limiter; 429 → controller retry
- Per-Task budget caps + per-Project rolling-window + absolute cost cap
- One PVC per Project, RWX, layout `/workspace/{repo,artifacts,envelopes}`
- Git push at level boundaries from orchestrator (HTTPS+PAT, host-agnostic, per-run branches, `--force-with-lease`, never `main`, gitleaks scanner)
- K8s Secret refs for LLM + git creds; namespace-per-project
- Per-level human gate policy on Project CRD; slack-tide between-wave review (optional)
- Per-level model selection on Project CRD
- Structured JSON logs + Prometheus metrics (bounded cardinality) + OTel tracing (OpenInference hand-emitted, tail-sampling)
- `tide` CLI (cobra, stateless)
- Read-only dashboard (separate Deployment, React Flow, two-DAG view, apiserver log proxy, idle-timeout, per-task log opt-in)
- Helm chart (CRDs + controller + dashboard + RBAC + dedicated CRD subchart; ServiceMonitor gated)
- Kubebuilder RBAC markers — no wildcards anywhere
- Finalizers with bounded deadline + idempotence + documented manual unstick
- Owner-ref helper (same-namespace + `BlockOwnerDeletion: true`)
- Three test tiers (unit no-LLM <30s; integration with stub-subagent <5min; live E2E nightly cost-capped)
- Apache 2.0 + docs sufficient for external operator
- **Self-hosting demo as v1 acceptance test** (fresh kind + Helm install + Project apply, <30 min clone-to-first-run for unfamiliar operator)

**Explicitly v1.x or v2+ — do NOT add to v1:** multi-tenant posture, gRPC streaming subagent, external DB/SQLite, dashboard mutations, vendored GSD Markdown, CPM/HEFT schedulers, cycle/wave recovery, non-K8s runtime, vendor lock-in, ESO/Vault first-class, per-host PR creation, auto-CI-fix loop, multi-cluster dispatch, MCP/A2A surface, drag-to-edit DAG, Kueue integration, OLM bundle, Agent Sandbox/gVisor, Project templates, native notifications, conservative failure profile, validation webhook for everything.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All core tech verified against May-2026 upstream releases. MEDIUM only for OpenInference-on-Go (no SDK, hand-rolled) and React-vs-htmx (taste-driven). |
| Features | HIGH | K8s/workflow + AI orchestration norms verified; v1 in/out cross-referenced against PROJECT.md and README spec. MEDIUM only for some 2026 trend claims. |
| Architecture | HIGH | K8s operator patterns + CRD ownership well-documented. MEDIUM on dashboard log-streaming topology (opinionated) and PVC RWX driver choice (deploy-time, deferred to docs). |
| Pitfalls | HIGH | Cross-verified against spec + PROJECT.md + controller-runtime docs + 2026 industry reporting. Each mapping has explicit prevention/verification. |

**Overall: HIGH.**

**Gaps:** The eleven divergence items above are the active gaps. Most resolve at specific phase-design times. Additional gap: dashboard two-DAG-view UX prototyping — recommend a Phase 4 wireframe/Figma spike before React Flow code lands.

## Sources

**Primary (HIGH):** `README.md` (TIDE spec), `.planning/PROJECT.md`, `CLAUDE.md`, controller-runtime v0.24.x releases, kubebuilder v4.14.0 releases, Anthropic Go SDK v1.42.0, Claude Code v2.1.139+, OTel Go v1.43.0, OpenInference semantic conventions, prometheus/client_golang v1.23.2, kind v0.31.0, Kubebuilder Book (Good Practices, EnvTest, Finalizers), Kubernetes Finalizers / CRD Versioning / RBAC Good Practices / cascading deletion / etcd limits / 1.31 WebSocket transition.

**Secondary (MEDIUM):** kagent, Argo Workflows, Tekton Pipelines, Composio AO, LangGraph/Temporal, React Flow xyflow, helmify, go-git/v5, chi router, Anthropic 2026 Agentic Coding Trends Report, MAST failure taxonomy (arxiv:2503.13657), 2026 industry reporting (OneUptime, Uptrace, RelayPlane, LangWatch, GitGuardian, Microsoft Security, arxiv prompt-injection papers), Bootstrapping (compilers) Wikipedia.
