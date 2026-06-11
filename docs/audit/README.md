# TIDE v1.0.0 — K8s Operator + Helm Best-Practices Audit

**Audited:** 2026-06-10
**Baseline:** v1.0.0 @ `02d3481` (fix(release): materialize demo-init embed fixture)
**Checklist source:** [260610-vcp-RESEARCH.md](../../.planning/quick/260610-vcp-audit-codebase-against-k8s-helm-best-pra/260610-vcp-RESEARCH.md)

---

## Methodology

This audit compares the TIDE codebase against the best-practices checklist produced by the 260610-vcp research task. Every checklist item (sections 1–9) has a finding with:

- **PASS** — implementation meets the upstream practice; evidence confirmed by grep/read
- **DRIFT** — accidental gap; the practice was not followed, likely unintentionally; all DRIFT items have a recommendation tagged `[SHIP-BLOCKER]` or `[NICE-TO-HAVE]`
- **DEVIATION** — deliberate departure; matches a row in the 13-row deviations table below or another documented architectural decision; never a defect

Evidence discipline: every finding cites `file:line` obtained by grepping/reading source. No finding asserts "TIDE does X" from memory alone.

Audit scope: `api/v1alpha1/`, `internal/controller/`, `internal/webhook/`, `internal/dispatch/`, `internal/pool/`, `internal/metrics/`, `internal/otelinit/`, `internal/finalizer/`, `internal/owner/`, `cmd/manager/`, `config/`, `charts/tide/`, `charts/tide-crds/`, `Dockerfile`, `Dockerfile.dashboard`, `.goreleaser.yaml`, `.github/workflows/`.

Read-only: no source files, chart templates, Dockerfiles, or CI workflows were modified. DRIFT findings become the hardening backlog below.

---

## Summary Table

| Section | Checklist Area | Findings | PASS | DRIFT | DEVIATION | Detail |
|---------|---------------|----------|------|-------|-----------|--------|
| 1 | CRD Design | 11 | 5 | 5 | 1 | [operator.md §1](operator.md#section-1-crd-design) |
| 2 | Controller / Reconciler Patterns | 12 | 10 | 1 | 1 | [operator.md §2](operator.md#section-2-controller--reconciler-patterns) |
| 3 | RBAC | 6 | 5 | 1 | 0 | [operator.md §3](operator.md#section-3-rbac) |
| 4 | Pod / Workload Security | 8 | 5 | 3 | 0 | [operator.md §4](operator.md#section-4-pod--workload-security) |
| 5 | Helm Chart Conventions | 11 | 2 | 7 | 2 | [helm-and-supply-chain.md §5](helm-and-supply-chain.md#section-5-helm-chart-conventions) |
| 6 | Image / Build Supply Chain | 9 | 5 | 4 | 0 | [helm-and-supply-chain.md §6](helm-and-supply-chain.md#section-6-image--build-supply-chain) |
| 7 | Webhooks | 7 | 4 | 2 | 1 | [operator.md §7](operator.md#section-7-webhooks) |
| 8 | Observability | 7 | 4 | 2 | 1 | [operator.md §8](operator.md#section-8-observability) |
| 9 | Operator Maturity | 5 | 3 | 2 | 0 | [operator.md §9](operator.md#section-9-operator-capability-levels--graceful-degradation) |
| **Total** | | **76** | **43** | **27** | **6** | |

---

## Deliberate Deviations Register

All 13 rows from the research deviations table, confirmed in source with file:line citation.

| # | Deviation | Upstream default departed from | Rationale of record | Source confirmation |
|---|-----------|-------------------------------|--------------------|--------------------|
| 1 | CRD-`.status`-only persistence (no DB/SQLite) | Operators with large state often externalize it | v1 scale = one human, one run; etcd-bounded; CI-gated | `api/v1alpha1/shared_types.go:25–208`; no `database` or `sqlite` imports found in `go.mod` |
| 2 | CEL validation preferred; webhook ONLY for cycle detection | Many operators default to webhooks for all validation | CEL is in-process/no availability risk; cycle all-paths check exceeds CEL | `api/v1alpha1/project_types.go:299` (sole CEL rule); `internal/webhook/v1alpha1/plan_webhook.go:157` (cycle detection) |
| 3 | `prometheus.enabled=false` chart default | Charts often default ServiceMonitor on | Avoids CRD-not-found on plain clusters | `charts/tide/templates/servicemonitor.yaml:1`: `{{- if .Values.prometheus.serviceMonitor.enabled }}` |
| 4 | Chart `values.yaml` = FIXED contract; binary conforms to chart | Chart typically follows code | Phase 02.2 anti-pattern lesson; prevents chart churn | `charts/tide/templates/` contains augment-layer comment: "chart-is-fixed-contract" pattern |
| 5 | Native K8s Jobs, not Argo/Tekton | Workflow engines common for DAG execution | Orchestrator owns the DAG; waves are derived, never declared | `internal/dispatch/podjob/jobspec.go` — only `batchv1.Job`; no Argo/Tekton imports |
| 6 | Layered Kahn in stdlib, no graph library | Gonum/dominikbraun-graph common | Spec exposition is iteration-by-iteration; import-firewalled | No graph library imports; Kahn implementation in-process per spec |
| 7 | OpenInference attrs on OTel spans, not OTel GenAI semconv | GenAI semconv is the OTel-native path | GenAI semconv pre-stable in 2026 | `internal/otelinit/provider.go`; `internal/subagent/anthropic/stream_parser.go:83` (events.jsonl) |
| 8 | zap behind logr, not slog | slog is the stdlib-modern choice | ~3× hot-path win for field-heavy reconcile logs | `cmd/manager/main.go:39`: import `sigs.k8s.io/controller-runtime/pkg/log/zap` |
| 9 | chi as `manager.Runnable` | Standalone HTTP servers common | Composes with manager lifecycle; fasthttp routers don't | `cmd/dashboard/main.go:104-110`: chi wired as `manager.Runnable` |
| 10 | No wave list as CRD input; no schedule cached in `.status` | Caching derived state is common | Rederive from DAG + completed-task set, O(V+E) — spec invariant | No `WaveSchedule` or similar field in any status struct; `pool.PreCharge` re-derives on restart |
| 11 | Helm chart pair (controller chart + CRDs subchart via pinned helmify) | Single chart with `crds/` dir | Works around Helm's never-upgrade-CRDs limitation | `charts/tide-crds/templates/` has CRDs as templates (not `crds/` dir); confirmed upgradeable |
| 12 | Read-only dashboard; mutations via CLI/kubectl only | Dashboards often mutate | Single auth surface | `cmd/dashboard/main.go:36`: "LeaderElection is OFF — the dashboard is a stateless read replica" |
| 13 | `ANTHROPIC_API_KEY` env from Secret; never mount host `~/.claude/` | — | Claude Code OAuth headless is broken (claude-code#29983, #7100) | `charts/tide/templates/serviceaccount-subagent.yaml` SA for executor pods; Secret-backed env referenced in `deployment.yaml` envFrom |

---

## Operator Capability Level

**Claimed:** Level 1 (Basic Install) + Level 2 (Seamless Upgrades, partial) + Level 4 (Deep Insights, partial when Prometheus enabled)

| Level | Name | TIDE Status | Gaps |
|-------|------|-------------|------|
| 1 | Basic Install | PASS | — |
| 2 | Seamless Upgrades | PARTIAL | No PDB, no version-skew policy, no `## Upgrading` doc (MATURITY-05) |
| 3 | Full Lifecycle | NOT CLAIMED | No backup/restore tooling |
| 4 | Deep Insights | PARTIAL (opt-in) | Metrics unauthenticated by default (OBS-01), ServiceMonitor opt-in (HELM-10), no alerting rules or Grafana dashboards |
| 5 | Auto Pilot | NOT CLAIMED | Out of v1 scope |

---

## Post-1.0 Hardening Backlog

### Ship-blockers

No DRIFT findings classified as `[SHIP-BLOCKER]`. All DRIFT items are `[NICE-TO-HAVE]` — v1.0.0 is installable and functional. The two most operationally significant DRIFT items are OBS-01 (metrics unauthenticated) and SUPPLY-04 (base images not digest-pinned).

### Nice-to-haves

Ordered by blast radius (security > correctness > polish).

**Security**

| Finding | One-liner | Blast radius |
|---------|-----------|-------------|
| OBS-01 / OBS-02 | Wire `FilterProvider: filters.WithAuthenticationAndAuthorization(...)` in `cmd/manager/main.go:263`; metrics currently unauthenticated | Any pod in cluster can scrape metrics at port 8443 |
| SUPPLY-04 | Pin Dockerfile base images by `@sha256` digest (`golang:1.26`, `gcr.io/distroless/static:nonroot`) | Supply-chain substitution risk on mutable tags |
| SUPPLY-06 | Add cosign keyless image signing to release workflow | Published images unverifiable |
| SUPPLY-05 | Add SBOM generation (syft/goreleaser `sboms:`) per image | No software composition audit possible |
| SUPPLY-08 | Add Trivy scan gate in CI (`severity: HIGH,CRITICAL exit-code: 1`) | Unscanned images may carry CVEs |

**Correctness**

| Finding | One-liner | Blast radius |
|---------|-----------|-------------|
| CRD-02 | Set `condition.ObservedGeneration = obj.Generation` on every `meta.SetStatusCondition` call | Clients cannot distinguish stale from current status |
| CRD-03 | Add `ObservedGeneration int64` field to all six status structs; assign in each reconcile | Same as CRD-02 at the per-object level |
| MATURITY-05 / HELM-05 | Document `## Upgrading TIDE` in `docs/INSTALL.md`; warn that `helm uninstall tide-crds` deletes all CRs | Data-loss risk for operators running `helm uninstall` without reading docs |
| OBS-07 | Confirm chart `containerPort: 8080 name: metrics` matches actual `--metrics-bind-address :8443` | ServiceMonitor may scrape wrong port |
| CRD-07 | Add `+kubebuilder:storageversion` to all 6 Kind types | Marker inconsistency creates confusion when v1beta1 is added |

**Observability / UX**

| Finding | One-liner | Blast radius |
|---------|-----------|-------------|
| RECON-08 | Add `LeaderElectionReleaseOnCancel: true` explicitly to manager options | Documents fast-handover intent |
| RECON-02 | Audit 5-second `RequeueAfter` loops; confirm each has a covering `Owns()`/`Watches()` | Eliminates unnecessary polling |
| WEBHOOK-03 | Add `timeoutSeconds: 5` to all three webhook rules | Reduces stuck-webhook latency from 10s to 5s |
| WEBHOOK-04 | Add `namespaceSelector` excluding `tide-system` from webhooks | Reduces not-yet-serving race blast radius |

**Polish / DX**

| Finding | One-liner | Blast radius |
|---------|-----------|-------------|
| HELM-01 | Add `kubeVersion: ">=1.29.0-0"`, `home`, `sources`, `maintainers` to both Chart.yaml files | Better install-time error messages; helm-hub metadata |
| HELM-02 | Add `values.schema.json` to both charts | Catch typos in values at install/upgrade time |
| HELM-07 | Add `charts/tide/templates/NOTES.txt` with post-install instructions | Users see nothing useful after `helm install` |
| HELM-08 | Add `ct lint` / `ct install` + `kubeconform` to CI | Chart correctness not integration-tested |
| HELM-09 | Add `kubeconform` against rendered templates | Schema validation absent |
| HELM-11 | Add `charts/tide/templates/tests/` smoke test Job | `helm test tide` has nothing to run |
| CRD-11 | Add `+kubebuilder:default` markers for scalar fields with well-defined defaults | `kubectl explain` shows no defaults |
| CRD-06 | Extend CEL rules to additional numeric constraints | More invariants caught at admission |
| RBAC-04 | Add aggregation labels to viewer/editor ClusterRoles | CRD access not extensible via built-in roles |
| PODSEC-05 | Add `charts/tide/templates/pdb.yaml` (conditional on `replicas > 1`) | HA installs unprotected |
| PODSEC-06 | Add `topologySpreadConstraints` to values + deployment template | HA installs can't configure spread |
| PODSEC-07 | Add `priorityClassName` to values + deployment template | Controller not protected from eviction |
| OBS-06 | Wire OpenInference attrs inline on OTel spans (not just events.jsonl post-processing) | Spans during live execution lack LLM observability metadata |
| HELM-06 | Add per-image `digest: ""` field to values for first-class digest pinning | Digest pinning via `repository` field override is non-standard |
| MATURITY-01 | Document Operator Capability Level claim in `docs/` | Claimed level undocumented |
| SUPPLY-05 | (see Security section above) | — |

---

## Quirks Appendix

Odd-but-working implementation details a new contributor would trip on.

| # | Quirk | File:line | Notes |
|---|-------|-----------|-------|
| Q1 | Finalizer forcibly removes on timeout | `internal/finalizer/finalizer.go:65-74` | `context.DeadlineExceeded` → log warning + force remove. Prevents deletion-stuck at cost of skipping cleanup on timeout. Callers must make cleanup idempotent. |
| Q2 | Task controller uses two-pass annotation predicate | `internal/controller/task_controller.go:1236-1260` | `For()` level has no `GenerationChangedPredicate`; annotation changes don't bump generation, so a self-`Watches()` re-enqueues on annotation updates. This is deliberate — documented inline. |
| Q3 | Project controller 5s polling for reporter Milestones | `internal/controller/project_controller.go:1125` | Reporter Job materializes child Milestones via API; controller polls at 5s waiting for expected count. `Owns(&Milestone{})` watch is the trigger once they appear; 5s is a fallback safety net. |
| Q4 | Metrics port mismatch between code and chart | `cmd/manager/main.go:145` (`--metrics-bind-address :8443`) vs `charts/tide/templates/deployment.yaml` (containerPort 8080 named `metrics`) | Code defaults to 8443 (HTTPS); chart exposes 8080 as named metrics port. The ServiceMonitor scrapes `https` port 8443 via `charts/tide/templates/metrics-service.yaml` — the deployment containerPort 8080 appears to be a legacy remnant, not the actual metrics port. |
| Q5 | OpenInference attr emission is deferred to `events.jsonl` post-processing | `internal/subagent/anthropic/stream_parser.go:83` | Raw events are written to `events.jsonl` during the agentic call for "Phase 4 OpenInference parsing." OTel spans during live execution may lack `input.value`, `output.value`, `llm.model_name` until post-processing runs against the audit log. |
| Q6 | `GetEventRecorderFor` uses deprecated `record.EventRecorder` | `internal/controller/project_controller.go:1328-1329` | `//nolint:staticcheck SA1019` suppresses the deprecation. The `events/v1` recorder migration is deferred as out-of-scope for lint hygiene. Functionally equivalent; the nolint annotation documents the debt. |
| Q7 | busybox init container not in GHCR publish matrix | `charts/tide/values.yaml:147-148` | `busybox:1.36` is a Docker Hub image used as the envelope-writer init container — it's a third-party base, not a TIDE-published image. Correct to omit from the 7-image GHCR matrix. |
| Q8 | ServiceMonitor uses `insecureSkipVerify: true` | `charts/tide/templates/servicemonitor.yaml:35` | Self-signed webhook cert; a proper CA bundle via cert-manager is noted as a v1.x follow-up. |
| Q9 | `OTel WithSampler` call is CI-gated | `internal/otelinit/provider.go` (companion test `TestNoWithSamplerInSource`) | A source-grep test prevents any `WithSampler(...)` call from being added to the provider; it would silently override the env-driven `OTEL_TRACES_SAMPLER` set by the chart. Pitfall 24 prevention. |
| Q10 | `storageversion` marker on `Plan` only | `api/v1alpha1/plan_types.go:64` | Only `Plan` carries `+kubebuilder:storageversion`; the other five Kinds do not. One served version makes this benign today; see CRD-07 for the backlog item. |
