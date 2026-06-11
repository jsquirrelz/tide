# Operator Audit Findings — TIDE v1.0.0

Audited 2026-06-10 against [260610-vcp-RESEARCH.md](../../.planning/quick/260610-vcp-audit-codebase-against-k8s-helm-best-pra/260610-vcp-RESEARCH.md).
Classification: **PASS** (meets practice) / **DRIFT** (accidental gap) / **DEVIATION** (deliberate, documented).

---

## Section 1: CRD Design

### CRD-01: Spec/status separation with `/status` subresource enabled

**Classification**: PASS

**Evidence:** All six Kinds carry `+kubebuilder:subresource:status`:
- `api/v1alpha1/project_types.go:427`
- `api/v1alpha1/milestone_types.go:47`
- `api/v1alpha1/phase_types.go:47`
- `api/v1alpha1/plan_types.go:65`
- `api/v1alpha1/task_types.go:140`
- `api/v1alpha1/wave_types.go:57`

**Notes:** The subresource marker is present on every Kind; no Kind uses bare `status` writes against the main endpoint.

---

### CRD-02: Conditions use `metav1.Condition` with `observedGeneration` on every write

**Classification**: DRIFT

**Evidence:** All six status structs embed `Conditions []metav1.Condition` (`api/v1alpha1/project_types.go:406`, `task_types.go:121`, etc.). Reconcilers call `meta.SetStatusCondition` (`internal/controller/plan_controller.go:178`, `plan_controller.go:344`). However, `meta.SetStatusCondition` does NOT automatically populate `observedGeneration` on each `metav1.Condition` — that requires the caller to set `condition.ObservedGeneration = obj.Generation`. A grep across `api/v1alpha1/` and `internal/controller/` finds zero occurrences of `ObservedGeneration` being set on condition objects.

**Recommendation [NICE-TO-HAVE]:** On each `meta.SetStatusCondition` call, set `condition.ObservedGeneration = obj.GetGeneration()` before passing the condition, so clients can distinguish stale status from current status at the per-condition level. Example pattern: pre-construct `metav1.Condition{..., ObservedGeneration: plan.Generation}` before calling `meta.SetStatusCondition`.

---

### CRD-03: Top-level `status.observedGeneration` field

**Classification**: DRIFT

**Evidence:** None of the six status structs (confirmed by grepping `api/v1alpha1/project_types.go`, `task_types.go`, `plan_types.go`, `phase_types.go`, `milestone_types.go`, `wave_types.go`) declare a top-level `ObservedGeneration int64` field. The `metav1.Condition` slice itself does not carry this; it must be a separate field.

**Recommendation [NICE-TO-HAVE]:** Add `ObservedGeneration int64 \`json:"observedGeneration,omitempty"\`` to each status struct, and assign `status.ObservedGeneration = obj.Generation` at the top of each reconcile pass (before early returns) so that `kubectl get <kind> -o yaml` reliably shows whether status reflects the current spec.

---

### CRD-04: Standard condition polarity and shared vocabulary

**Classification**: PASS

**Evidence:** `api/v1alpha1/shared_types.go:25–208` declares a single cross-Kind condition vocabulary: `ConditionPending`, `ConditionReady`, `ConditionReconciling`, `ConditionFailed`, `ConditionValidated`, `ConditionRunning`, `ConditionSucceeded`, and domain-specific additions (`ConditionCloned`, `ConditionBoundaryPushed`, etc.). The four-type base is uniform across all six Kinds. No abnormal-true conditions are used for normal-path states.

**Notes:** `ConditionPending` and `ConditionReady` are conventional. `ConditionReconciling` is an unconventional condition name (the canonical upstream preference is `Ready=False reason=Reconciling`), but it is internally consistent and the shared vocabulary prevents per-Kind drift.

---

### CRD-05: Printer columns

**Classification**: PASS

**Evidence:** All six Kinds carry identical `+kubebuilder:printcolumn` markers for `Phase`, `Status`, and `Age`:
- `api/v1alpha1/wave_types.go:59-61`
- `api/v1alpha1/task_types.go:142-144`
- `api/v1alpha1/plan_types.go:67-69`
- `api/v1alpha1/phase_types.go:49-51`
- `api/v1alpha1/milestone_types.go:49-51`
- `api/v1alpha1/project_types.go:429-431`

---

### CRD-06: CEL validation rules

**Classification**: DRIFT

**Evidence:** Only one CEL rule found across all six CRDs: `api/v1alpha1/project_types.go:299`, validating that `spec.targetRepo` begins with a recognized transport prefix. No other fields on any Kind carry `+kubebuilder:validation:XValidation` markers. Several fields (e.g. `Task.spec.caps.wallClockSeconds`, `Plan.spec.taskDag` adjacency list shape, `Wave` dependency ranges) have invariants that CEL could express but don't have validation rules.

**Recommendation [NICE-TO-HAVE]:** Extend CEL rules incrementally — e.g. `Task.spec.caps.wallClockSeconds >= 0`, `Project.spec.absoluteCapCents >= 0`. Cycle detection is the one case where CEL can't express all-paths (DEVIATION row — webhook handles it).

---

### CRD-07: Versioning and conversion

**Classification**: DRIFT

**Evidence:** Only `api/v1alpha1/plan_types.go:64` carries `+kubebuilder:storageversion`. The other five Kinds (`Project`, `Milestone`, `Phase`, `Task`, `Wave`) lack the `+kubebuilder:storageversion` marker. While v1alpha1 is currently the only served version and hub/spoke conversion is unnecessary today, the marker discipline is inconsistent and will create ambiguity when a v1beta1 surface is added.

**Recommendation [NICE-TO-HAVE]:** Add `+kubebuilder:storageversion` to all six Kind types (or confirm `controller-gen` derives this from the CRD manifests already — if the generated CRDs have `storage: true` on all six, the risk is documentation only). Regardless, establish the one-storage-version convention explicitly before adding a second API version.

---

### CRD-08: Status stays small relative to etcd limits

**Classification**: DEVIATION

**Evidence:** `api/v1alpha1/shared_types.go` deliberately uses CRD `.status` only — no external DB or SQLite. Status structs carry a bounded `Conditions` slice, a `Phase` string, and a small set of scalar fields. Envelope output (`out.json`) is stored on a per-namespace PVC, not in etcd. This matches the DEVIATION row: "CRD-`.status`-only persistence".

**Deviation cites:** Deviations table row 1: "CRD-`.status`-only persistence (no DB/SQLite)" — CLAUDE.md Project Constraints + CI gate `verify-no-sqlite-dep`.

---

### CRD-09: Finalizers

**Classification**: PASS

**Evidence:** All six reconcilers use domain-qualified finalizer names:
- `internal/controller/milestone_controller.go:53`: `"tideproject.k8s/milestone-cleanup"`
- `internal/controller/phase_controller.go:53`: `"tideproject.k8s/phase-cleanup"`
- `internal/controller/plan_controller.go:56`: `"tideproject.k8s/plan-cleanup"`
- `internal/controller/task_controller.go:60`: `"tideproject.k8s/task-cleanup"`
- `internal/controller/wave_controller.go:43`: `"tideproject.k8s/wave-cleanup"`
- `internal/controller/project_controller.go:222`: `controllerutil.AddFinalizer(&project, projectFinalizer)` (constant defined elsewhere in same file)

The `internal/finalizer/finalizer.go` package provides a bounded-deadline helper that prevents indefinite deletion blocks (Pitfall 21): on `context.DeadlineExceeded` the finalizer is forcibly removed with a log warning; on transient errors the finalizer is retained and the reconcile is requeued.

**Notes:** The bounded-deadline finalizer forcibly removes on timeout — a deliberate tradeoff (avoids deletion-stuck) documented inline at `finalizer.go:65-74`.

---

### CRD-10: Owner references

**Classification**: PASS

**Evidence:** `internal/owner/owner.go:41-63` wraps `controllerutil.SetControllerReference` with `Controller=true` and `BlockOwnerDeletion=true`. All Job creation paths (`internal/controller/reporter_jobspec.go:219`, `internal/controller/push_helpers.go:118-119`) call `owner.EnsureOwnerRef`. Cross-namespace owner refs are avoided — all child objects are created in the same namespace as the parent CRD.

---

### CRD-11: Schema defaults

**Classification**: DRIFT

**Evidence:** A search for `+kubebuilder:default` across `api/v1alpha1/` returns no results. Defaults that exist (e.g. `fileTouchMode`, `requestsPerMinute`, model names) are wired as flag defaults in `cmd/manager/main.go` and chart `values.yaml`, not as CRD schema defaults. Users who bypass Helm and apply CRDs directly with `kubectl apply` will see no schema defaults.

**Recommendation [NICE-TO-HAVE]:** Add `+kubebuilder:default` markers for scalar fields with well-defined defaults (e.g. `Task.spec.caps.wallClockSeconds`, `Plan.spec.gates.policy`). This improves `kubectl explain` output and makes the effective spec visible without Helm.

---

## Section 2: Controller / Reconciler Patterns

### RECON-01: Idempotent, level-based reconciliation

**Classification**: PASS

**Evidence:** Every reconciler pattern reads object state fresh each invocation. Example: `internal/controller/project_controller.go:203` opens with `logger := logf.FromContext(ctx)` followed by a fresh `r.Get(ctx, req.NamespacedName, &project)`. The `internal/controller/task_controller.go` follows a multi-branch match on observed `.status.phase` rather than assuming prior state. `Owns(&batchv1.Job{})` at `project_controller.go:1345` and `task_controller.go:1248` ensures children trigger re-reconcile on drift.

---

### RECON-02: Requeue strategy

**Classification**: DRIFT

**Evidence:** The code uses three legitimate requeue patterns: `return err` for real failures, `RequeueAfter` for polling, and `Requeue: true` for immediate requeue. However, several 5-second polling loops exist:
- `internal/controller/project_controller.go:1125` — waiting for reporter Milestones to materialize
- `internal/controller/milestone_controller.go:507,522,537` — waiting for planner Job to progress
- `internal/controller/phase_controller.go:420,435,450`
- `internal/controller/plan_controller.go:472,725,754`

The 5-second floor is the shortest acceptable polling interval per the checklist's "no sub-10s busy-poll loops." These are all ≥5s, which is borderline. They could be eliminated by ensuring the relevant `Owns()` or `Watches()` declarations cover the secondary resource transitions that end the wait — but only if the watches are already declared.

**Recommendation [NICE-TO-HAVE]:** Audit each `RequeueAfter: 5s` site to confirm a corresponding `Owns()` or `Watches()` declaration covers the event that ends the wait. If it does, the `RequeueAfter` is a fallback safety net (acceptable). If not, promote the watch and remove the polling loop.

---

### RECON-03: No long-running work inside Reconcile

**Classification**: PASS

**Evidence:** All long operations are delegated to Kubernetes Jobs (`batchv1.Job` via `internal/dispatch/podjob/`). Reconcilers dispatch Jobs and observe their status on subsequent reconciles — they don't block on Job completion. The `internal/dispatch/podjob/jobspec.go` is build-only; reconcilers create the Job and return.

---

### RECON-04: Error wrapping

**Classification**: PASS

**Evidence:** `internal/controller/project_controller.go:318,324,453` shows consistent `fmt.Errorf("context: %w", err)` wrapping. `apierrors.IsNotFound` is handled explicitly in every reconciler (e.g. `task_controller.go`: returns `nil` on primary-object NotFound, not error).

---

### RECON-05: Status writes via status subresource, conflict-safe

**Classification**: PASS

**Evidence:** `internal/controller/task_controller.go:191` uses `r.Status().Update(ctx, &task)`. Heavy-writer paths use `MergeFrom` + `Status().Patch()`: `task_controller.go:292-300`, `320-328`, `352-361` etc. The MergeFrom patch pattern is used throughout the task reconciler which has the most concurrent writers (planner/executor pools). The project reconciler uses `Status().Update()` for the primary path.

**Notes:** SSA (Server-Side Apply) is not used; MergeFrom patch is the conflict-mitigation strategy. This is acceptable for v1 but SSA with a stable field manager is the upstream-recommended next step.

---

### RECON-06: Watch filtering with predicates

**Classification**: PASS

**Evidence:** `internal/controller/project_controller.go:1341` wires `predicate.GenerationChangedPredicate{}` on the primary `For()` watch. All six reconcilers apply a namespace predicate via `WithEventFilter(nsPred)`: `project_controller.go:1347`, `milestone_controller.go:694`, `phase_controller.go:616`, `plan_controller.go:1200`, `wave_controller.go:282`, `task_controller.go:1260`.

**Notes:** The task controller uses a deliberate two-pass strategy (`task_controller.go:1236-1244`): the `For()` level does NOT use `GenerationChangedPredicate` for annotations (annotation changes don't bump generation), and a self-`Watches()` re-enqueues on annotation changes. This is documented inline and correct.

---

### RECON-07: `Owns()`/`Watches()` for secondary resources

**Classification**: PASS

**Evidence:**
- `project_controller.go:1345-1346`: `Owns(&batchv1.Job{})`, `Owns(&tideprojectv1alpha1.Milestone{})`
- `task_controller.go:1247-1253`: `For(&Task{})`, `Owns(&batchv1.Job{})`, `Watches(...)` for Plan changes
- Wave, Phase, Milestone controllers similarly declare `Owns()` and `Watches()` for their child Kinds

---

### RECON-08: Leader election enabled

**Classification**: PASS

**Evidence:** `cmd/manager/main.go:257`: `LeaderElection: leaderElect` (default true). `LeaderElectionID: "tide-controller-leader.tideproject.k8s"` at `main.go:258`. Leader-election timing is configurable via env vars (`cmd/manager/env.go:115`, `resolveLeaderElectionTiming()`). Test coverage at `internal/controller/leader_election_test.go:127-129`.

**Notes:** `LeaderElectionReleaseOnCancel` is not explicitly set in `cmd/manager/main.go`. The default in controller-runtime v0.24 is `true` when the context is cancelled via `ctrl.SetupSignalHandler()`, but an explicit `LeaderElectionReleaseOnCancel: true` would make the intent visible.

**Recommendation [NICE-TO-HAVE]:** Add `LeaderElectionReleaseOnCancel: true` explicitly to the manager options at `cmd/manager/main.go:~257` to document the fast-handover intent.

---

### RECON-09: Graceful shutdown

**Classification**: PASS

**Evidence:** `cmd/manager/main.go:206`: `signalCtx := ctrl.SetupSignalHandler()`. The manager is started with `mgr.Start(signalCtx)`. The OTel shutdown is deferred at line 215 with a bounded context. The chi router and SSE server are registered as `manager.Runnable` and stop on context cancel.

---

### RECON-10: Workqueue metrics observed

**Classification**: PASS

**Evidence:** `internal/metrics/doc.go:27` confirms that `internal/metrics` registers on `controller-runtime`'s `metrics.Registry`. The blank import at `cmd/manager/main.go:49-56` ensures the `init()` fires at startup. controller-runtime's default workqueue metrics (`workqueue_depth`, `controller_runtime_reconcile_*`) are emitted by the global registry — nothing disables it.

---

### RECON-11: MaxConcurrentReconciles set deliberately

**Classification**: PASS

**Evidence:** All five reconcilers expose `MaxConcurrentReconciles int` on the reconciler struct and wire it into `WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles})`:
- `milestone_controller.go:65,695`
- `plan_controller.go:67,1201`
- `phase_controller.go:62,617`
- `wave_controller.go:55,283`
- `project_controller.go:150`

Separately, `cfg.PlannerConcurrency` and `cfg.ExecutorConcurrency` control pool sizing (`cmd/manager/main.go:240-244`).

---

### RECON-12: Resumption derived from observed state

**Classification**: DEVIATION

**Evidence:** `internal/pool/pool.go` (referenced at `cmd/manager/main.go:292-300`) calls `PreCharge` on startup which re-derives in-process pool state from live Jobs via `List`. No wave schedule is persisted in `.status`. This is the spec invariant: "rederive from completed-task set in O(V+E)."

**Deviation cites:** Deviations table row 10: "No wave list as CRD input; no schedule cached in `.status`" — TIDE spec invariant + CLAUDE.md constraint.

---

## Section 3: RBAC

### RBAC-01: Least privilege, no wildcard verbs

**Classification**: PASS

**Evidence:** All `+kubebuilder:rbac` markers use explicit verb lists (e.g. `get;list;watch;create;update;patch;delete` for owned Kinds, `get;list;watch` for read-only Kinds, `create;patch` for events). No wildcard `*` verbs or resources found in any marker across `internal/controller/` and `cmd/`.

---

### RBAC-02: Status and finalizers subresource permissions split

**Classification**: PASS

**Evidence:** Every controlled Kind has three separate markers:
- Main resource: `get;list;watch;create;update;patch;delete`
- `<kind>/status`: `get;update;patch`
- `<kind>/finalizers`: `update`

Example: `internal/controller/task_controller.go:117-119`.

---

### RBAC-03: Namespace-scoped Roles; per-namespace RoleBindings

**Classification**: PASS

**Evidence:** `charts/tide/templates/per-namespace-rolebinding.yaml` renders one `RoleBinding` per entry in `.Values.projectNamespaces`, binding the controller-manager SA (in `tide-system`) to the manager ClusterRole scoped to each project namespace. `config/rbac/role.yaml` is a ClusterRole (required for CRD read and webhook config), but per-namespace grants are Roles via the rolebinding template.

---

### RBAC-04: ClusterRole aggregation

**Classification**: DRIFT

**Evidence:** Viewer and editor roles exist (`config/rbac/project_viewer_role.yaml`, `project_editor_role.yaml`, `milestone_viewer_role.yaml`, etc.) but none carry `rbac.authorization.k8s.io/aggregate-to-admin`, `aggregate-to-edit`, or `aggregate-to-view` labels. Users cannot grant CRD access by extending the built-in `admin`/`edit`/`view` roles; they must bind the custom ClusterRoles explicitly.

**Recommendation [NICE-TO-HAVE]:** Add aggregation labels to the viewer/editor/admin ClusterRoles so `kubectl` users with `admin` binding to a namespace automatically gain CRD access. Affects `config/rbac/` and the corresponding chart templates.

---

### RBAC-05: Privilege-escalation prevention

**Classification**: PASS

**Evidence:** The controller creates per-namespace RoleBindings (`charts/tide/templates/per-namespace-rolebinding.yaml`) and per-namespace SA + Role + RoleBinding for the reporter (`charts/tide/templates/reporter-rbac.yaml`) and push (`charts/tide/templates/push-rbac.yaml`). The controller's ClusterRole does not carry `escalate` or `bind` verbs. The per-namespace grants are scoped to the exact CRD kinds and verbs needed — no wildcards.

---

### RBAC-06: Dedicated ServiceAccounts per workload class

**Classification**: PASS

**Evidence:**
- `charts/tide/templates/serviceaccount-subagent.yaml`: SA for executor Job pods
- `charts/tide/templates/reporter-rbac.yaml:8`: SA for reporter Job pods (`automountServiceAccountToken: true` — required for API access)
- `charts/tide/templates/push-rbac.yaml:8`: SA for push Job pods (`automountServiceAccountToken: true` — required)
- Manager SA in `charts/tide/templates/serviceaccount.yaml`

**Notes:** `automountServiceAccountToken: true` is explicitly set on workload SAs that need API access. The manager deployment's `automountServiceAccountToken` is not explicitly set (defaults to `true` for the SA-level setting). This is acceptable — manager needs API access. No zero-verb "no-op" SA cases exist where `automountServiceAccountToken: false` would be appropriate since every Job SA needs API access for its function.

---

## Section 4: Pod / Workload Security

### PODSEC-01: Pod Security Standards `restricted` compliance

**Classification**: PASS

**Evidence:** `config/manager/manager.yaml:57-90` sets:
- `runAsNonRoot: true`
- `seccompProfile.type: RuntimeDefault`
- `allowPrivilegeEscalation: false`
- `capabilities.drop: ["ALL"]`

`charts/tide/templates/dashboard-deployment.yaml:34-84` mirrors the same set. The chart's `values.yaml:30` sets `containerSecurityContext` that is templated into the deployment (`deployment.yaml:87`).

---

### PODSEC-02: `readOnlyRootFilesystem: true`

**Classification**: PASS

**Evidence:** `config/manager/manager.yaml:86`: `readOnlyRootFilesystem: true` (container-level). `charts/tide/values.yaml:30`: `readOnlyRootFilesystem: true` in the default `containerSecurityContext`. Writable paths use explicit `volumeMounts` (e.g. `/tmp/k8s-webhook-server/serving-certs`, `/workspaces` PVC).

---

### PODSEC-03: Resource requests and limits

**Classification**: PASS

**Evidence:** `config/manager/manager.yaml` defines:
```
resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi
```
The chart's `deployment.yaml` templates `resources: {{- toYaml .Values.controllerManager.manager.resources | nindent 10 }}` — chart carries resources through from values. Dispatched Job pods: `internal/dispatch/podjob/jobspec.go` builds the Job spec; resource requests on Job pods are policy-driven via `values.yaml` subagent defaults.

---

### PODSEC-04: Liveness and readiness probes

**Classification**: PASS

**Evidence:** `charts/tide/templates/deployment.yaml`:
- `livenessProbe.httpGet.path: /healthz port: 8081 initialDelaySeconds: 15 periodSeconds: 20`
- `readinessProbe.httpGet.path: /readyz port: 8081 initialDelaySeconds: 5 periodSeconds: 10`

`cmd/manager/main.go:271-278`: `mgr.AddHealthzCheck("healthz", healthz.Ping)` and `AddReadyzCheck("readyz", healthz.Ping)` wire the controller-runtime handlers.

---

### PODSEC-05: PodDisruptionBudget

**Classification**: DRIFT

**Evidence:** No PDB template found in `charts/tide/templates/`. Default replicas is 1 (`values.yaml: replicas: 1`), making a PDB pointless at the default. However, the chart does not include a `pdb.yaml` template conditioned on `replicas > 1`, so multi-replica HA installs have no PDB support.

**Recommendation [NICE-TO-HAVE]:** Add `charts/tide/templates/pdb.yaml` gated on `{{ if gt (int .Values.controllerManager.replicas) 1 }}` with `minAvailable: 1`.

---

### PODSEC-06: Topology spread / anti-affinity

**Classification**: DRIFT

**Evidence:** No `topologySpreadConstraints` or `podAntiAffinity` in `charts/tide/templates/deployment.yaml` or `charts/tide/values.yaml`.

**Recommendation [NICE-TO-HAVE]:** Add a `topologySpreadConstraints` block in values under `controllerManager.topologySpreadConstraints: []` so HA deployers can configure it without chart modification.

---

### PODSEC-07: `priorityClassName` exposed in values

**Classification**: DRIFT

**Evidence:** No `priorityClassName` field in `charts/tide/values.yaml` or `charts/tide/templates/deployment.yaml`.

**Recommendation [NICE-TO-HAVE]:** Add `controllerManager.priorityClassName: ""` to `values.yaml` and wire it into the Pod spec template.

---

### PODSEC-08: Jobs hardened

**Classification**: PASS

**Evidence:**
- `backoffLimit: 0` at `internal/dispatch/podjob/jobspec.go:146` — retries are controller-side via attempt counter (documented in comment)
- `activeDeadlineSeconds` set at `jobspec.go:67` via `DefaultCaps` derivation (floor + grace)
- `TTLSecondsAfterFinished: DefaultTTLSecondsAfterFinished` (600s) at `jobspec.go:445`
- Push Jobs: `TTLSecondsAfterFinished: 300s` at `push_helpers.go:212,312`
- Reporter Jobs: `TTLSecondsAfterFinished` set at `reporter_jobspec.go:175`

---

## Section 7: Webhooks

### WEBHOOK-01: Cert management automated

**Classification**: PASS

**Evidence:** `charts/tide/templates/serving-cert.yaml` creates a cert-manager `Certificate` resource. `charts/tide/templates/validating-webhook-configuration.yaml:6` carries `cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/{{ include "tide.fullname" . }}-serving-cert`. The selfsigned issuer is at `charts/tide/templates/selfsigned-issuer.yaml`. No hand-rolled long-lived certs.

---

### WEBHOOK-02: `failurePolicy` chosen deliberately

**Classification**: PASS

**Evidence:** All three webhooks (plan, project, wave) use `failurePolicy: Fail` in both `config/webhook/manifests.yaml:14,34,54` and `charts/tide/templates/validating-webhook-configuration.yaml:17,37,57`. This is correct for validation that guards correctness (cycle rejection, DAG validation) — fail-closed is right here.

---

### WEBHOOK-03: `timeoutSeconds` configured

**Classification**: DRIFT

**Evidence:** Neither `config/webhook/manifests.yaml` nor `charts/tide/templates/validating-webhook-configuration.yaml` sets `timeoutSeconds`. The Kubernetes default is 10 seconds. This is not dangerous, but explicit is better — a stuck webhook will block all CREATE/UPDATE operations on matching Kinds for the full 10 seconds before failing.

**Recommendation [NICE-TO-HAVE]:** Add `timeoutSeconds: 5` to each webhook rule in both the kustomize manifest and the chart template. 5s is well-motivated (validation is CPU-only: topological sort is O(V+E)).

---

### WEBHOOK-04: Scope narrowed with `namespaceSelector`/`objectSelector`

**Classification**: DRIFT

**Evidence:** Neither `config/webhook/manifests.yaml` nor `charts/tide/templates/validating-webhook-configuration.yaml` sets `namespaceSelector` or `objectSelector`. The webhooks intercept all CREATE/UPDATE on plans/projects/waves cluster-wide, including in `kube-system` and the operator's own `tide-system` namespace. TIDE's own Plans created during self-operation go through the webhook — this is intentional and works, but a `namespaceSelector` excluding unrelated system namespaces would reduce blast radius.

**Notes:** TIDE hit a not-yet-serving race in CI (referenced in RESEARCH.md §7 and rc.5 fix history). The dry-run script added a retry loop (`3dd9665`), but the underlying cause (webhook intercepting the operator's own namespace on startup) could be mitigated by a `namespaceSelector` that excludes `tide-system` from the operator's own webhook scope.

**Recommendation [NICE-TO-HAVE]:** Add `namespaceSelector` excluding `tide-system` from at least the project/wave webhooks (Plans in `tide-system` are not a TIDE use case). This reduces the not-yet-serving window's blast radius.

---

### WEBHOOK-05: `sideEffects: None`

**Classification**: PASS

**Evidence:** All three webhooks carry `sideEffects: None` in both `config/webhook/manifests.yaml:26,46,66` and `charts/tide/templates/validating-webhook-configuration.yaml:29,49,69`.

---

### WEBHOOK-06: Matching only needed resources/operations

**Classification**: PASS

**Evidence:** Each webhook matches exactly its target Kind and operations `CREATE, UPDATE` only — no DELETE, no wildcard Kinds. Example: plan webhook rules: `groups: [tideproject.k8s], apiVersions: [v1alpha1], operations: [CREATE, UPDATE], resources: [plans]` (`config/webhook/manifests.yaml:16-22`).

---

### WEBHOOK-07: Prefer CEL/ValidatingAdmissionPolicy where expressible

**Classification**: DEVIATION

**Evidence:** The single CEL rule at `api/v1alpha1/project_types.go:299` handles the `targetRepo` URL format check. Cycle detection (the complex validation) is handled by the Plan webhook (`internal/webhook/v1alpha1/plan_webhook.go:157`). This is the documented DEVIATION: webhooks are used only where CEL can't express the logic.

**Deviation cites:** Deviations table row 2: "CEL validation preferred; webhook ONLY for cycle detection" — CLAUDE.md constraint.

---

## Section 8: Observability

### OBS-01: Metrics endpoint secured the modern way

**Classification**: DRIFT

**Evidence:** `cmd/manager/main.go:263`: `Metrics: metricsserver.Options{BindAddress: metricsAddr}` — this uses only `BindAddress`. The `metricsserver.Options` struct in controller-runtime v0.24+ supports a `FilterProvider` field accepting `filters.WithAuthenticationAndAuthorization(...)` for token-review-backed auth. This FilterProvider is NOT set. The `config/rbac/metrics_auth_role.yaml` and chart `metrics-auth-rbac.yaml` both provision `tokenreviews` and `subjectaccessreviews` ClusterRole permissions for the manager SA — the infrastructure is there, but the in-process filter that uses it is absent.

**Notes:** This resolves Research Open Question 2. The `--metrics-bind-address` defaults to `:8443` (HTTPS port by convention), suggesting secure serving was intended, but the `FilterProvider` was not wired. The RBAC manifests were scaffolded but the code didn't complete the loop.

**Recommendation [NICE-TO-HAVE]:** Add `FilterProvider: filters.WithAuthenticationAndAuthorization(mgr.GetCache(), mgr.GetScheme())` (or equivalent for controller-runtime v0.24 API) to `metricsserver.Options` in `cmd/manager/main.go:263`. Requires importing `sigs.k8s.io/controller-runtime/pkg/metrics/filters`. Without this, any pod in the cluster that can reach port 8443 on the manager can scrape metrics unauthenticated.

---

### OBS-02: Metrics not bound to 0.0.0.0 unauthenticated

**Classification**: DRIFT

**Evidence:** Default `--metrics-bind-address: :8443` at `cmd/manager/main.go:145` binds to all interfaces. Without `FilterProvider`, metrics are accessible unauthenticated. The `metricsserver.Options` does not set `SecureServing: true` explicitly either. See OBS-01 for the same finding.

**Recommendation [NICE-TO-HAVE]:** Same fix as OBS-01 — wire FilterProvider. Together OBS-01 and OBS-02 are one fix.

---

### OBS-03: Structured logging via logr

**Classification**: PASS

**Evidence:** All reconcilers use `logger := logf.FromContext(ctx)` at the top of `Reconcile()` (e.g. `project_controller.go:203`, `task_controller.go` similarly). `cmd/manager/main.go:39` imports `sigs.k8s.io/controller-runtime/pkg/log/zap`. zap is wired behind logr per CLAUDE.md constraint.

---

### OBS-04: Kubernetes Events for user-significant transitions

**Classification**: PASS

**Evidence:**
- `internal/webhook/v1alpha1/plan_webhook.go:157`: `v.Recorder.Eventf(plan, corev1.EventTypeWarning, "CycleDetected", ...)`
- `internal/webhook/v1alpha1/plan_webhook.go:182`: `v.Recorder.Eventf(plan, ..., "FileTouchMismatch", ...)`
- `internal/controller/project_controller.go:601`: `r.Recorder.Eventf(project, corev1.EventTypeWarning, ReasonPushFailed, ...)`
- EventRecorder wired at `project_controller.go:1330`, `milestone_controller.go:674`, `phase_controller.go:596`

**Notes:** The `GetEventRecorderFor` call uses the deprecated `record.EventRecorder` type (SA1019 suppressed with `//nolint:staticcheck`). Migrating to the `events/v1` recorder is deferred as out-of-scope for lint hygiene — documented inline.

---

### OBS-05: Custom domain metrics

**Classification**: PASS

**Evidence:** `internal/metrics/registry.go:91-170` registers on `metrics.Registry`:
- `WavesDispatchedTotal`, `TasksCompletedTotal`, `TasksFailedTotal` (counters)
- `DispatchLatency` (histogram)
- `SecretLeakBlockedTotal`, `PushJobsTotal`, `BudgetOverrunsTotal`
- `internal/budget/metrics.go:46`: `ProviderRateLimitHitsTotal`

Total: 8 custom metrics across two packages. `internal/metrics/doc.go` documents the inventory.

---

### OBS-06: OTel traces with OpenInference attributes

**Classification**: DEVIATION (partially DRIFT for attribute completeness)

**Evidence:** `internal/otelinit/provider.go` constructs a no-op TracerProvider when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset (zero overhead for plain clusters) or the real OTLP gRPC TP when set. `cmd/manager/main.go:215`: `tp, otelShutdown, err := otelinit.NewTracerProvider(signalCtx)`. OTel spans are started in the agentic chain. However, the `internal/subagent/anthropic/stream_parser.go:31,56,83` writes raw events to `events.jsonl` for "Phase 4 OpenInference parsing" — meaning the OpenInference attribute emission is deferred to post-processing of the events file, not inline on OTel spans during the agentic call.

**Deviation cites:** Deviations table row 7: "OpenInference attrs on OTel spans, not OTel GenAI semconv" — CLAUDE.md constraint.

**Notes (Assumption A4):** The hand-rolled approach means OpenInference attribute completeness cannot be confirmed by source grep alone. The `events.jsonl` post-processing path for OpenInference attrs is plausible but means spans during live execution may lack `input.value`, `output.value`, `llm.model_name` etc. until post-processing runs. This is a known architectural choice.

---

### OBS-07: healthz/readyz endpoints separate from metrics

**Classification**: PASS

**Evidence:** `cmd/manager/main.go:262`: `HealthProbeBindAddress: ":8081"` is separate from `Metrics: metricsserver.Options{BindAddress: ":8443"}`. `deployment.yaml` exposes port 8081 as `health` and 8080 as `metrics` (separate named ports).

**Notes:** The metrics port in `deployment.yaml` is named `metrics` at containerPort 8080, while `cmd/manager/main.go` defaults `metricsAddr` to `:8443`. This is an apparent inconsistency — the chart may be mapping the wrong port. If the manager actually serves on 8443 but the chart's `deployment.yaml` exposes port 8080 as `metrics`, the ServiceMonitor would scrape the wrong port.

**Recommendation [NICE-TO-HAVE]:** Audit whether the chart's `containerPort: 8080 name: metrics` matches the actual `--metrics-bind-address` default `:8443`. If they differ, correct the chart port definition.

---

## Section 9: Operator Capability Levels & Graceful Degradation

### MATURITY-01: Capability level known and documented

**Classification**: DRIFT

**Evidence:** TIDE is not assessed against the Operator Framework 5-level capability model anywhere in `docs/`, `README.md`, or `docs/INSTALL.md`. Evidence gathered across this audit suggests:

- **Level 1 (Basic Install):** PASS — Helm chart pair ships, CRDs install, manager deploys, basic reconciliation works.
- **Level 2 (Seamless Upgrades):** PARTIAL — CRDs are in a separate chart that supports upgrade; the controller itself can be upgraded via `helm upgrade tide`. However, no PDB, no topology spread, no documented version-skew policy between the two charts.
- **Level 3 (Full Lifecycle — backup/recovery):** NOT CLAIMED — no backup/restore tooling; PVC contents (per-namespace `out.json`) are not backed up by TIDE itself.
- **Level 4 (Deep Insights):** PARTIAL — 8 custom Prometheus metrics, OTel tracing, Kubernetes Events. Gaps: ServiceMonitor is opt-in (not default), metrics are unauthenticated (OBS-01), no alerting rules or Grafana dashboards shipped.
- **Level 5 (Auto Pilot):** NOT CLAIMED — no auto-scaling, auto-healing, or auto-tuning.

**Claimed level: Level 2 (with Level 4 metrics posture where enabled).**

**Recommendation [NICE-TO-HAVE]:** Add a `## Operator Capability Level` section to `docs/README.md` or `docs/production.md` stating the claimed level and what gaps exist before Level 3/4 are fully satisfied.

---

### MATURITY-02: Graceful degradation on missing optional deps

**Classification**: PASS

**Evidence:**
- Prometheus Operator CRDs: `charts/tide/templates/servicemonitor.yaml:1` gates on `prometheus.serviceMonitor.enabled=false` — install does not break when Prometheus Operator is absent.
- cert-manager: documented as a hard prerequisite at `docs/INSTALL.md:101-114` — explicit instruction to install before the `tide` chart. Absence breaks install (by design), and this is prominently documented.
- OTel collector: `internal/otelinit/provider.go` returns a no-op TracerProvider when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset — zero overhead, no broken install.

**Deviation cites:** Deviations table row 3: "`prometheus.enabled=false` chart default" — CLAUDE.md constraint.

---

### MATURITY-03: Operator does not own what it doesn't manage

**Classification**: PASS

**Evidence:** The operator only mutates objects it created (Jobs, child CRDs). `internal/owner/owner.go` ensures all owned objects carry owner refs. Per-namespace RoleBindings (`charts/tide/templates/per-namespace-rolebinding.yaml`) are only created for namespaces explicitly listed in `.Values.projectNamespaces` — the operator does not self-discover and mutate arbitrary namespace resources. No evidence of wildcard List+Update patterns on pre-existing objects found in reconcilers.

---

### MATURITY-04: One controller per Kind; clear single-writer ownership

**Classification**: PASS

**Evidence:** Six Kinds, six reconcilers (`project_controller.go`, `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `wave_controller.go`, `task_controller.go`), each registered via its own `SetupWithManager` call. No two controllers write the same status field on the same Kind. The reporter Job writes child CRDs (Milestone/Phase/Plan/Task objects) but does so via the API-server as a separate actor with its own SA — it does not share the reconciler loop.

---

### MATURITY-05: Upgrade path documented

**Classification**: DRIFT

**Evidence:** `docs/INSTALL.md` has no `## Upgrade` section. The install order (CRDs first, then tide) is documented for fresh install. The upgrade procedure — run `helm upgrade tide-crds ...` then `helm upgrade tide ...` in the same order — is not documented. The version-skew policy (whether the controller is forward/backward compatible with older CRDs or not) is undocumented. The risk of `helm uninstall tide-crds` deleting all CRs is not warned.

**Deviation cites:** HELM-05 covers the CRD chart upgrade story in detail; this finding covers the higher-level upgrade procedure gap.

**Recommendation [NICE-TO-HAVE]:** Add `## Upgrading TIDE` section to `docs/INSTALL.md` with: upgrade order, `helm upgrade` commands for both charts, version-skew note, and a prominent warning that `helm uninstall tide-crds` is destructive.
