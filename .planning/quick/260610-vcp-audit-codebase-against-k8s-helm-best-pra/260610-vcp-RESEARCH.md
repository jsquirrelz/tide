# Quick Task: K8s Operator + Helm Chart Best-Practices Audit — Research

**Researched:** 2026-06-10
**Domain:** Kubernetes operator (Go + controller-runtime + kubebuilder) and Helm chart best practices, 2025–2026 upstream state
**Confidence:** HIGH (nearly all items cited from official upstream docs: kubernetes.io, helm.sh, book.kubebuilder.io, sdk.operatorframework.io, CNCF TAG App Delivery)

## Summary

This research produces the comparison baseline for an audit of the TIDE codebase against current Kubernetes operator and Helm chart best practices. It is a citable checklist, organized into the nine areas the audit will walk: CRD design, reconciler patterns, RBAC, workload security, Helm conventions, image/build supply chain, webhooks, observability, and operator maturity. Each item is concrete and checkable ("grep/inspect X, expect Y"), with a one-line source.

The checklist reflects the 2025–2026 upstream state, which differs from older guidance in a few load-bearing ways: metrics endpoints are now secured via controller-runtime's `WithAuthenticationAndAuthorization` filter instead of kube-rbac-proxy (kubebuilder ≥ v4.1 scaffolds this by default); CEL validation rules are GA and preferred over validating webhooks for expressible constraints; Pod Security Standards `restricted` profile is the de-facto securityContext contract; and supply-chain expectations (SBOM, cosign keyless signing, pinned digests, multi-arch) have become table stakes for OSS distribution.

**Primary recommendation:** Audit each area against the checklist below, classifying every finding as PASS / DRIFT (accidental gap) / DEVIATION (deliberate, listed in the final section) — the deviations section exists precisely so the audit doesn't flag intentional architecture as defects.

## Scope Constraints (from task focus)

- This research feeds an **audit**, not a feature build. Output is a best-practices checklist with citations.
- The audit itself (executor's job) compares the TIDE codebase against this checklist and records findings as documents in `docs/`.
- TIDE's pinned stack frames what "current" means: Go 1.26, controller-runtime v0.24.x, kubebuilder v4.14, Helm 3, Kubernetes ≥ 1.33, CRD-`.status`-only persistence.

## Project Constraints (from CLAUDE.md)

Directives that bound the audit's recommendations (the audit must not recommend approaches that contradict these):

- CRD `.status` only — no external DB, no SQLite; per-object size well under etcd's 1.5 MiB limit.
- CEL CRD validation (`x-kubernetes-validations`) preferred over admission webhooks — except cycle detection where CEL can't express all-paths.
- `charts/tide/values.yaml` is a FIXED contract — binary catches up to chart, never reverse.
- Chart defaults `prometheus.enabled=false` (ServiceMonitor) to avoid CRD-not-found on plain clusters.
- Never bump `k8s.io/*` independently — controller-runtime's `go.mod` dictates.
- No host `~/.claude/` mounts in executor containers; `ANTHROPIC_API_KEY` from a K8s Secret.
- Don't cache the wave schedule in `.status` — rederive from completed-task set.
- All Anthropic-specific code behind the `Subagent` interface in `internal/subagent/anthropic/`.
- zap behind logr (not slog); chi router as `manager.Runnable`.
- API group is `tideproject.k8s` — never `tide.io`.

---

## Best-Practices Checklist

### 1. CRD Design

- [ ] **Spec/status separation with `/status` subresource enabled** (`+kubebuilder:subresource:status`): main endpoint mutates only metadata+spec; only controllers write status. [CITED: kubernetes/community sig-architecture api-conventions.md]
- [ ] **Conditions use `metav1.Condition`** with `type` (CamelCase), `status` (True/False/Unknown), `reason`, `message`, `lastTransitionTime`, and **`observedGeneration`** set on every condition write; manipulate via `meta.SetStatusCondition` (apimachinery `pkg/api/meta/conditions.go`). [CITED: api-conventions.md §Typical Status Properties; apimachinery types.go]
- [ ] **Top-level `status.observedGeneration`** reported by the reconciler so clients can tell whether status reflects the latest spec. [CITED: api-conventions.md]
- [ ] **Standard condition polarity:** prefer abnormal-true conditions sparingly; a `Ready`-style summary condition is conventional. Condition types should be consistent across Kinds (TIDE uses a shared 4-type vocabulary — check uniformity). [CITED: api-conventions.md §Conditions]
- [ ] **Printer columns** (`+kubebuilder:printcolumn`) for the operationally important fields (phase/ready/age) so `kubectl get` is useful without `-o yaml`. [CITED: book.kubebuilder.io/reference/markers/crd.html]
- [ ] **CEL validation rules** (`+kubebuilder:validation:XValidation` / `x-kubernetes-validations`) for invariants expressible in CEL — GA since K8s 1.29; cheaper and more portable than webhooks; use `messageExpression`/`reason` for actionable errors; mind the per-CRD CEL cost budget. [CITED: kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-rules]
- [ ] **Versioning & conversion:** storage version marked (`+kubebuilder:storageversion`); hub/spoke conversion scaffolding present before a second version ships; no breaking schema changes within a served version. [CITED: book.kubebuilder.io/multiversion-tutorial/tutorial.html]
- [ ] **Status stays small relative to etcd limits:** no unbounded lists, no large blobs, no per-event accumulation in `.status`; large/rapidly-changing data goes in separate objects or external artifact stores. etcd default request limit is 1.5 MiB per object. [CITED: api-conventions.md ("status that may be large… should be put into separate objects"); etcd.io/docs — request size limit]
- [ ] **Finalizers:** named with a domain-qualified key (`<group>/finalizer`); added idempotently before external resources are created; removed only after cleanup succeeds; deletion path tolerates partial cleanup and re-entry (no finalizer leaks blocking namespace deletion). [CITED: book.kubebuilder.io/reference/using-finalizers.html]
- [ ] **Owner references:** child objects set controller owner ref (`Controller=true`) via `ctrl.SetControllerReference`; `BlockOwnerDeletion` only where foreground cascade ordering matters; owner and dependent must be in the same namespace (cross-namespace owner refs are invalid). [CITED: kubernetes.io/docs/concepts/architecture/garbage-collection/ + book.kubebuilder.io]
- [ ] **Defaults declared in the schema** (`+kubebuilder:default`) rather than imperatively in the reconciler, so `kubectl get` shows the effective spec. [CITED: book.kubebuilder.io/reference/markers/crd-validation.html]

### 2. Controller / Reconciler Patterns

- [ ] **Idempotent, level-based reconciliation:** Reconcile reads all needed state fresh each invocation, computes desired state, converges — never assumes it's reacting to a specific event (edge-triggered logic is a bug). Check-before-create on every owned resource. [CITED: controller-runtime FAQ — github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md]
- [ ] **Requeue strategy:** return `err` for real failures (exponential backoff); `RequeueAfter` for time-based polling; never both; no sub-10s busy-poll loops — prefer watches over polling. [CITED: controller-runtime FAQ + pkg.go.dev/sigs.k8s.io/controller-runtime]
- [ ] **No long-running work inside Reconcile:** reconciles complete in seconds; long operations are delegated (Jobs, async status checks) and observed on subsequent reconciles. [CITED: Operator SDK best practices — sdk.operatorframework.io/docs/best-practices/]
- [ ] **Error wrapping:** errors wrapped with `%w` and context (`fmt.Errorf("fetching X: %w", err)`); `apierrors.IsNotFound`/`IsConflict` handled explicitly (NotFound on the primary → return nil, not error). [CITED: book.kubebuilder.io cronjob tutorial; Go stdlib convention] [ASSUMED for the exact wrapping style — multiple valid house styles exist]
- [ ] **Status writes via the status subresource, conflict-safe:** use `Status().Update()`/`Status().Patch()`; prefer Patch (MergeFrom or SSA) over Update where concurrent writers exist; controller-runtime supports Server-Side Apply for typed clients in recent versions — SSA with a stable field manager is the conflict-free option for shared objects. [CITED: controller-runtime FAQ; sdk.operatorframework.io/docs/building-operators/golang/references/client/]
- [ ] **Watch filtering with predicates:** `GenerationChangedPredicate` (or equivalent) on primary watches so the controller's own status writes don't retrigger reconcile loops; label/namespace predicates where scope is bounded (TIDE: `--watch-namespace` + `WithEventFilter`). [CITED: pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/predicate]
- [ ] **`Owns()`/`Watches()` declared for every secondary resource the controller mutates** so drift in children triggers reconciliation (e.g. Jobs owned by Task). [CITED: book.kubebuilder.io/cronjob-tutorial/controller-implementation.html]
- [ ] **Leader election enabled** for the manager (`LeaderElection: true`, lease-based) so multi-replica deploys are safe; `LeaderElectionReleaseOnCancel: true` for fast handover on graceful stop. [CITED: pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager]
- [ ] **Graceful shutdown:** manager started with `ctrl.SetupSignalHandler()`; `GracefulShutdownTimeout` respected; runnables (chi router, SSE) stop on context cancel. [CITED: pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager]
- [ ] **Workqueue metrics observed:** controller-runtime exports `workqueue_depth`, `workqueue_adds_total`, `workqueue_queue_duration_seconds`, `controller_runtime_reconcile_*` by default on the metrics endpoint — nothing should disable the global registry. [CITED: book.kubebuilder.io/reference/metrics-reference.html]
- [ ] **MaxConcurrentReconciles set deliberately** per controller where parallelism matters (TIDE's planner/executor pool sizing is adjacent but distinct — check both are independently configured). [CITED: pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller]
- [ ] **Resumption derived from observed state, not cached state** (generic form: rebuild in-memory indexes from List on startup; never trust in-process state across restarts). [CITED: controller-runtime FAQ; TIDE spec makes this a hard constraint]

### 3. RBAC

- [ ] **Least privilege, resource-by-resource:** RBAC markers (`+kubebuilder:rbac`) enumerate exact groups/resources/verbs; **no wildcard verbs or resources** in the operator's own role. [CITED: sdk.operatorframework.io/docs/best-practices/ + book.kubebuilder.io/reference/markers/rbac.html]
- [ ] **`status` and `finalizers` subresource permissions split** from the main resource (get/update/patch on `<kind>/status`, update on `<kind>/finalizers`) — only what the controller writes. [CITED: book.kubebuilder.io scaffolded role.yaml shape]
- [ ] **Namespace-scoped Roles where the operator watches a namespace; ClusterRole only for genuinely cluster-scoped needs** (CRD read, webhook config). Per-namespace RoleBindings for namespace-per-tenant models (TIDE's namespace-per-project). [CITED: kubernetes.io/docs/reference/access-authn-authz/rbac/]
- [ ] **ClusterRole aggregation** (`aggregationRule` / `rbac.authorization.k8s.io/aggregate-to-admin|edit|view` labels) used to grant users CRD access by extending built-in roles instead of bespoke roles. [CITED: kubernetes.io/docs/reference/access-authn-authz/rbac/#aggregated-clusterroles]
- [ ] **Privilege-escalation prevention respected:** the operator can't grant permissions it doesn't hold (RBAC escalation check); operator does not have `escalate`/`bind` verbs unless it legitimately manages RBAC — if it creates Roles/RoleBindings (TIDE provisions per-namespace RBAC), verbs are scoped to the exact resourceNames/rules needed. [CITED: kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping]
- [ ] **Dedicated ServiceAccounts per workload class** (manager vs dispatched Jobs vs zero-verb subagent SA), `automountServiceAccountToken: false` on pods that don't need API access. [CITED: kubernetes.io/docs/concepts/security/ — service account hardening]

### 4. Pod / Workload Security

- [ ] **Pod Security Standards `restricted` compliance** for the manager and all dispatched pods: `runAsNonRoot: true`, `seccompProfile.type: RuntimeDefault` (or Localhost), `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`. [CITED: kubernetes.io/docs/concepts/security/pod-security-standards/]
- [ ] **`readOnlyRootFilesystem: true`** with explicit emptyDir/PVC mounts for writable paths. [CITED: PSS hardening guidance — kubernetes.io]
- [ ] **Resource requests AND limits** set on the manager container; requests set on dispatched Job pods (limits per workload policy); kubebuilder scaffolds defaults in `manager.yaml` — chart must carry them through. [CITED: book.kubebuilder.io scaffold + kubernetes.io resource management docs]
- [ ] **Liveness + readiness probes** on the manager wired to controller-runtime's healthz/readyz endpoints (`mgr.AddHealthzCheck`/`AddReadyzCheck`, `--health-probe-bind-address`). [CITED: book.kubebuilder.io scaffold; pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/healthz]
- [ ] **PodDisruptionBudget** for the manager when `replicas > 1` (chart-conditional); pointless at replicas=1 — absence is acceptable for single-replica defaults but the chart should support it. [CITED: kubernetes.io/docs/tasks/run-application/configure-pdb/]
- [ ] **Topology spread / anti-affinity** chart-expressible for HA deployments (optional at v1 single-replica). [CITED: kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/]
- [ ] **`priorityClassName` exposed in values** so cluster admins can protect the controller from eviction. [CITED: kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/]
- [ ] **Jobs hardened:** `activeDeadlineSeconds`, `backoffLimit`, `ttlSecondsAfterFinished` on dispatched Jobs to bound runaway and garbage. [CITED: kubernetes.io/docs/concepts/workloads/controllers/job/]

### 5. Helm Chart Conventions

- [ ] **Chart.yaml complete:** `apiVersion: v2`, `description`, `type: application`, `kubeVersion` constraint, `home`/`sources`/`maintainers`, semver `version` distinct from `appVersion`. [CITED: helm.sh/docs/topics/charts/]
- [ ] **`values.schema.json` present** to validate user-supplied values at install/upgrade time. [CITED: helm.sh/docs/topics/charts/#schema-files]
- [ ] **`_helpers.tpl` naming:** templates namespaced as `<chart>.<name>` (e.g. `tide.fullname`, `tide.labels`); chart names lowercase-dash; values camelCase. [CITED: helm.sh/docs/chart_best_practices/]
- [ ] **Standard labels on every object:** `app.kubernetes.io/name`, `instance`, `version`, `managed-by: {{ .Release.Service }}`, `helm.sh/chart`; selector labels frozen to the name/instance pair (selectors are immutable). [CITED: helm.sh/docs/chart_best_practices/labels/]
- [ ] **CRD handling — know the `crds/` dir limitations:** Helm installs `crds/` once and never upgrades/deletes them. For operators that evolve CRDs, the documented mitigation is a **separate CRDs chart** (TIDE already ships a CRD subchart — verify upgrade story is documented) or out-of-band `kubectl apply` of CRDs. [CITED: helm.sh/docs/topics/charts/#limitations-on-crds + helm.sh/docs/chart_best_practices/custom_resource_definitions/]
- [ ] **No `latest` image tags:** default tag empty → resolves to `.Chart.AppVersion`; values allow digest pinning (`image.digest`) for supply-chain-conscious installs. [CITED: helm.sh/docs/chart_best_practices/pods/ — "image tag should not be latest"]
- [ ] **`NOTES.txt`** prints post-install next steps (how to apply a Project, check status, find the dashboard). [CITED: helm.sh/docs/chart_template_guide/notes_files/]
- [ ] **`helm lint` clean; chart-testing (`ct lint` / `ct install`) in CI** against a kind cluster. [CITED: github.com/helm/chart-testing]
- [ ] **`helm template` output passes kubeconform/server-side dry-run** for the pinned `kubeVersion` range. [ASSUMED — common practice, no single official doc]
- [ ] **Optional integrations guarded:** ServiceMonitor and other CRD-dependent objects behind a flag defaulting off (TIDE's `prometheus.enabled=false` is exactly this practice). [CITED: helm community convention; TIDE CLAUDE.md codifies it]
- [ ] **Helm test hooks** (`templates/tests/`) for a minimal post-install smoke check. [CITED: helm.sh/docs/topics/chart_tests/]

### 6. Image / Build Supply Chain

- [ ] **Multi-stage builds** — build stage with full toolchain, runtime stage minimal; `CGO_ENABLED=0` static binary for Go. [CITED: docs.docker.com/build/building/multi-stage/]
- [ ] **Distroless/nonroot base** (`gcr.io/distroless/static:nonroot` is the kubebuilder default) — no shell, no package manager in runtime images; debug variants only for debug tags. [CITED: book.kubebuilder.io scaffold Dockerfile; github.com/GoogleContainerTools/distroless]
- [ ] **Numeric non-root USER** in the Dockerfile (required for `runAsNonRoot` to be verifiable without runtime UID lookup). [CITED: kubernetes.io PSS + distroless `:nonroot` = 65532]
- [ ] **Base images pinned by digest** in Dockerfiles; kind node images pinned by `@sha256` in E2E (TIDE rule). [CITED: supply-chain hardening guidance — SLSA/OpenSSF; TIDE CLAUDE.md]
- [ ] **SBOM generated per image** (syft → SPDX or CycloneDX) and attached/attested at release. [CITED: openssf.org + sbomify guides; goreleaser supports `sboms:` natively]
- [ ] **Images signed with cosign** — keyless (OIDC via GitHub Actions ID token, Fulcio cert, Rekor transparency log) preferred over long-lived keys. [CITED: openssf.org/blog Sigstore guidance; docs.sigstore.dev]
- [ ] **Multi-arch manifests** (linux/amd64 + linux/arm64) via buildx or goreleaser docker manifests. [CITED: docs.docker.com/build/building/multi-platform/]
- [ ] **Vulnerability scan in CI** (trivy/grype) on built images with a failure threshold. [CITED: common OSS release practice; aquasecurity/trivy docs] [ASSUMED as "expected" severity threshold — no single upstream mandate]
- [ ] **Image inventory completeness:** every image the chart or controller references at runtime is in the publish matrix (TIDE's cascade: tide-reporter missing from the 6-image inventory was a real ship-blocker — audit should verify chart-referenced images == published images). [VERIFIED: project history, .planning/STATE.md]

### 7. Webhooks

- [ ] **Cert management automated:** cert-manager Certificate + Injector annotations (`cert-manager.io/inject-ca-from`) or controller-runtime's rotating cert support; no hand-rolled long-lived certs. [CITED: book.kubebuilder.io/cronjob-tutorial/cert-manager.html]
- [ ] **`failurePolicy` chosen deliberately:** `Fail` for validation that guards correctness (TIDE's cycle rejection should fail-closed), `Ignore` only where a missed mutation is safe; document the choice. [CITED: kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/]
- [ ] **`timeoutSeconds` small** (well under the 10s default; 1–30 allowed) — webhooks add latency to every matching API call. [CITED: kubernetes.io admission webhook good practices — kubernetes.io/docs/concepts/cluster-administration/admission-webhooks-good-practices/]
- [ ] **Scope narrowly with rules + `namespaceSelector`/`objectSelector`:** exclude `kube-system` and the operator's own namespace where interception could deadlock the operator's own startup (webhook pod can't start because its own admission webhook isn't serving — TIDE hit the not-yet-serving race in CI). [CITED: kubernetes.io admission-webhooks-good-practices]
- [ ] **`sideEffects: None` and idempotent admission logic** (webhook may be called multiple times for one request). [CITED: kubernetes.io extensible-admission-controllers]
- [ ] **Matching only the resources/operations needed** (e.g. Plan CREATE/UPDATE only — not all Kinds, not DELETE). [CITED: kubernetes.io admission-webhooks-good-practices]
- [ ] **Prefer CEL/ValidatingAdmissionPolicy over webhooks where expressible** — in-process, no availability risk; webhook reserved for logic CEL can't express (TIDE's all-paths cycle detection). [CITED: kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/]

### 8. Observability

- [ ] **Metrics endpoint secured the modern way:** controller-runtime ≥ v0.19 serves metrics over HTTPS with `metrics/filters.WithAuthenticationAndAuthorization` (kubebuilder ≥ v4.1 default); kube-rbac-proxy sidecar is deprecated/discontinued for this purpose. Check `cmd/main.go` metrics options + the chart exposes the secure port with appropriate RBAC (`tokenreviews`/`subjectaccessreviews` ClusterRole for the SA). [CITED: book.kubebuilder.io/reference/metrics.html; controller-runtime PR #2407]
- [ ] **Metrics not bound to 0.0.0.0 unauthenticated** — either secured serving or loopback-only with a scrape sidecar. [CITED: book.kubebuilder.io/reference/metrics.html]
- [ ] **Structured logging via logr** with consistent key-value fields (no printf logs in reconcile paths); per-reconcile logger from `log.FromContext(ctx)` carrying the request name/namespace. [CITED: book.kubebuilder.io/cronjob-tutorial — logging conventions; TIDE pins zap behind logr]
- [ ] **Kubernetes Events emitted for user-significant transitions** (`record.EventRecorder`): dispatch failures, gate decisions, validation rejections — events are the user's first debugging surface. [CITED: sdk.operatorframework.io best practices; api-conventions.md §Events]
- [ ] **Custom domain metrics** beyond the built-ins (waves dispatched, tasks completed, dispatch latency, failure rate — TIDE's stated list) registered on the controller-runtime `metrics.Registry`. [CITED: book.kubebuilder.io/reference/metrics-custom.html]
- [ ] **OTel traces for the agentic chain** with OpenInference attribute names (TIDE constraint — not OTel GenAI semconv, which is still pre-stable); spans for Milestone→Phase→Plan→Task dispatch chain. [CITED: TIDE PROJECT.md constraint; arize.com OpenInference spec] [ASSUMED for OpenInference attribute completeness — hand-rolled pkg/otelai, no Go SDK exists]
- [ ] **healthz/readyz endpoints separate from metrics**, unauthenticated-OK (probe-only content). [CITED: book.kubebuilder.io scaffold]

### 9. Operator Capability Levels & Graceful Degradation

- [ ] **Know your level and document it:** Operator Framework defines Level 1 (Basic Install) → 2 (Seamless Upgrades) → 3 (Full Lifecycle: backup/recovery) → 4 (Deep Insights: metrics/alerts/workload analysis) → 5 (Auto Pilot: auto-scaling/healing/tuning). TIDE at v1 plausibly claims Level 1 + most of Level 4's metrics posture; the audit should state the claimed level in docs. [CITED: sdk.operatorframework.io/docs/overview/operator-capabilities/]
- [ ] **Graceful degradation on missing optional deps:** absence of Prometheus Operator CRDs, cert-manager, or a metrics stack must not break install (TIDE's `prometheus.enabled=false` default; cert-manager is a documented hard prereq — that's acceptable if INSTALL.md says so prominently). [CITED: CNCF Operator White Paper — tag-app-delivery.cncf.io/whitepapers/operator/]
- [ ] **Operator does not own what it doesn't manage:** no mutation of objects it didn't create; labels/owner-refs identify managed objects. [CITED: CNCF Operator White Paper best practices]
- [ ] **One controller per Kind; clear single-writer ownership of each status field.** [CITED: CNCF Operator White Paper; api-conventions.md]
- [ ] **Upgrade path documented** (CRD upgrades out-of-band of Helm, version skew between CRDs chart and controller chart). [CITED: helm.sh custom_resource_definitions best practices]

---

## Known TIDE-Specific Deliberate Deviations

The audit must classify these as DEVIATION (deliberate, documented), not DRIFT. Source: project CLAUDE.md + PROJECT.md decisions table.

| Deviation | Upstream default it departs from | Rationale of record |
|---|---|---|
| CRD-`.status`-only persistence (no DB/SQLite) | Operators with large state often externalize it | v1 scale = one human, one run; size kept under etcd limits; CI-gated (`verify-no-sqlite-dep`, `verify-no-aggregates`) |
| CEL validation preferred; webhook ONLY for cycle detection | Many operators default to webhooks for all validation | CEL is in-process/no availability risk; cycle all-paths check exceeds CEL |
| `prometheus.enabled=false` chart default | Charts often default ServiceMonitor on | Avoids CRD-not-found failures on plain clusters |
| Chart `values.yaml` = FIXED contract; binary conforms to chart | Chart typically follows code | Phase 02.2 anti-pattern lesson; prevents chart churn |
| Native K8s Jobs, not Argo/Tekton | Workflow engines common for DAG execution | Orchestrator owns the DAG; waves are derived, never declared |
| Layered Kahn in stdlib, no graph library | Gonum/dominikbraun-graph common | Spec exposition is iteration-by-iteration; import-firewalled (`verify-dag-imports`) |
| OpenInference attrs on OTel spans, not OTel GenAI semconv | GenAI semconv is the OTel-native path | GenAI semconv pre-stable in 2026; Phoenix/LangSmith/Arize consume OpenInference today |
| zap behind logr, not slog | slog is the stdlib-modern choice | ~3× hot-path win for field-heavy reconcile logs |
| chi as `manager.Runnable` | Standalone HTTP servers common | Composes with manager lifecycle; fasthttp routers don't |
| No wave list as CRD input; no schedule cached in `.status` | Caching derived state is common | Rederive from DAG + completed-task set, O(V+E) — spec invariant |
| Helm chart pair (controller chart + CRDs subchart via pinned helmify v0.4.17) | Single chart with `crds/` dir | Works around Helm's never-upgrade-CRDs limitation; regeneration is idempotent via hack/helm augment layer |
| Read-only dashboard; mutations via CLI/kubectl only | Dashboards often mutate | Single auth surface |
| `ANTHROPIC_API_KEY` env from Secret; never mount host `~/.claude/` | — | Claude Code OAuth headless is broken (claude-code#29983, #7100) |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `%w` error-wrapping with context-prefix style as the audited convention | §2 | Low — style choice; audit should check consistency, not a specific style |
| A2 | `helm template` + kubeconform dry-run as expected CI practice | §5 | Low — widespread but not an official Helm mandate; treat as recommended, not required |
| A3 | Vulnerability-scan failure threshold (trivy/grype) as expected | §6 | Low — no upstream mandate on threshold; recommend HIGH/CRITICAL gate |
| A4 | OpenInference attribute-name completeness of hand-rolled `pkg/otelai` | §8 | Medium — no Go SDK exists; audit should diff emitted attrs against the OpenInference spec |

## Open Questions

1. **CRD subchart upgrade story** — Helm never upgrades `crds/`-dir CRDs, but a CRDs *subchart* with CRDs as templates CAN upgrade them (with the risk that uninstall deletes CRDs and all CRs). The audit should verify which mechanism TIDE's CRD chart uses and that INSTALL.md documents the upgrade + uninstall semantics either way.
2. **Metrics auth posture** — TIDE scaffolded on kubebuilder v4.14 (post-v4.1), so `WithAuthenticationAndAuthorization` should be the default; the audit should confirm it wasn't disabled during the `--metrics-bind-address` flag work in Phase 02.2 cascade fixes.

## Sources

### Primary (HIGH confidence — official upstream)
- kubernetes/community — [sig-architecture api-conventions.md](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md) (spec/status, conditions, observedGeneration, status size)
- [book.kubebuilder.io/reference/metrics.html](https://book.kubebuilder.io/reference/metrics.html) (secure metrics, WithAuthenticationAndAuthorization, kube-rbac-proxy discontinuation)
- [controller-runtime FAQ](https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md) (idempotency, requeue, patch vs update)
- [controller-runtime PR #2407](https://github.com/kubernetes-sigs/controller-runtime/pull/2407) (secure metrics serving introduction)
- [helm.sh/docs/topics/charts/](https://helm.sh/docs/topics/charts/) (Chart.yaml, values.schema.json, crds/ limitations)
- [helm.sh/docs/chart_best_practices/labels/](https://helm.sh/docs/chart_best_practices/labels/) (app.kubernetes.io labels)
- [kubernetes.io — Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) (restricted profile)
- [kubernetes.io — Admission Webhook Good Practices](https://kubernetes.io/docs/concepts/cluster-administration/admission-webhooks-good-practices/) (failurePolicy, timeouts, namespaceSelector, deadlocks)
- [kubernetes.io — Dynamic Admission Control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) (sideEffects, idempotency)
- [sdk.operatorframework.io — Operator Capability Levels](https://sdk.operatorframework.io/docs/overview/operator-capabilities/) (levels 1–5)
- [CNCF Operator White Paper — TAG App Delivery](https://tag-app-delivery.cncf.io/whitepapers/operator/) (design pattern, security, graceful degradation)
- [Operator SDK — Controller Runtime Client API](https://sdk.operatorframework.io/docs/building-operators/golang/references/client/) (status subresource client usage)

### Secondary (MEDIUM confidence — verified against official sources)
- [OpenSSF — Sigstore container signing at scale](https://openssf.org/blog/2024/02/16/scaling-up-supply-chain-security-implementing-sigstore-for-seamless-container-image-signing/) (cosign keyless)
- apimachinery [`pkg/apis/meta/v1/types.go`](https://github.com/kubernetes/apimachinery/blob/master/pkg/apis/meta/v1/types.go) (metav1.Condition shape)

### Tertiary (LOW confidence — community practice, flagged in checklist)
- kubeconform-in-CI and trivy severity thresholds (widespread convention, no single upstream mandate)

## Metadata

**Confidence breakdown:**
- CRD design / reconciler patterns: HIGH — sig-architecture conventions + kubebuilder book + controller-runtime FAQ
- Helm conventions: HIGH — helm.sh official docs
- Security (PSS, webhooks, RBAC): HIGH — kubernetes.io official docs
- Supply chain: MEDIUM-HIGH — OpenSSF/Sigstore official, thresholds conventional
- Deviations table: HIGH — sourced directly from project CLAUDE.md/PROJECT.md

**Research date:** 2026-06-10
**Valid until:** ~2026-07-10 (K8s/Helm conventions are stable; metrics-auth scaffolding details track kubebuilder minor releases)
