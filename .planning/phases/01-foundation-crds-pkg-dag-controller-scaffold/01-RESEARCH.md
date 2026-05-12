# Phase 1: Foundation — CRDs, pkg/dag, Controller Scaffold — Research

**Researched:** 2026-05-12
**Domain:** Kubernetes operator scaffold — six `v1alpha1` CRDs, stdlib Kahn-layered DAG, six event-driven reconcilers, two-pool semaphores, webhook scaffolds, custom go-analyzer, helmify chart pair
**Confidence:** HIGH — kubebuilder/controller-runtime patterns are extensively documented; project-level research already converged on the stack and pitfall map. This document translates that into Phase-1-specific guidance.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Module identity & artifact paths**
- **D-A1:** Go module path is `github.com/jsquirrelz/tide`. Every internal import resolves under this prefix. Module name in `go.mod` matches.
- **D-A2:** Container images publish to `ghcr.io/jsquirrelz/` — controller image is `ghcr.io/jsquirrelz/tide-controller`, dashboard image (Phase 4) will be `ghcr.io/jsquirrelz/tide-dashboard`, future tide-lint analyzer image (if shipped) follows the same pattern. GHCR is free for OSS and avoids Docker Hub anonymous-pull rate limits.

**Wave CRD shape**
- **D-B1:** `WaveReconciler` is the sole producer of `Wave` objects. On `Plan` reaching ready state, the reconciler runs `pkg/dag.ComputeWaves` over the Plan's Tasks and creates one `Wave` per layer with owner-ref back to the Plan. Wave names are deterministic — `tide-wave-{plan-uid}-{index}` — so re-derivation on every reconcile is idempotent. **No human or other controller creates `Wave` objects; the admission webhook rejects any client-applied `Wave`.**
- **D-B2:** `Wave.Spec` carries only `planRef` (owning Plan) and `waveIndex` (integer layer position). Every other field — task member list, dispatch state, completion timestamps, failure reasons — lives in `Wave.Status`. This makes the "derived not declared" principle structurally enforced.
- **D-B3:** Cycle detection runs in a **validating admission webhook on `Plan`** (not on `Task`, not on `Wave`). Phase 1 scaffolds the webhook endpoint as a no-op (always Allow); Phase 2 wires the actual rejection logic. CEL is used on Plan/Task/Wave for non-graph invariants.

**Reconciler stub depth**
- **D-C1:** Reconciler stubs are **Standard depth** — each of the six reconcilers registers with the Manager, sets up the `Owns(&batchv1.Job{})` watch, ensures owner refs on create, runs idempotent finalizer cleanup with a bounded deadline on delete, and propagates status conditions (`Pending` / `Ready` / `Failed`). The **only** stubbed-out hole is the subagent dispatch path — Phase 2 fills exactly that.
- **D-C2:** No `time.Sleep`, no blocking I/O, no LLM calls in any `Reconcile()` body in Phase 1.

**Task DAG declaration schema**
- **D-F1:** `Task.Spec.dependsOn` is `[]string` — a list of sibling Task names within the same owning Plan. CEL validates that every string in `dependsOn` refers to a Task in the same Plan.
- **D-F2:** `Task.Spec.filesTouched` is `[]string`, **required and non-empty**. Phase 1 ships the schema field with CEL `MinItems: 1` validation.

**Configuration & distribution**
- **D-E1:** Helm chart ships **in Phase 1**. Two charts: `charts/tide/` (controller-only — Deployment, RBAC, ServiceAccount, ConfigMap, values.yaml exposing `plannerConcurrency: 16`, `executorConcurrency: 4`, `maxConcurrentReconciles` per-Kind) and `charts/tide-crds/` (CRDs as a dedicated subchart for safe `helm upgrade`). Generated via `helmify` from kubebuilder's `config/` Kustomize output. `make helm` target invokes helmify locally.
- **D-E2:** Phase 1's charts must remain Phase 5-compatible (helmify-driven) so Phase 5 just adds templates.

**POOL-03 lint rule**
- **D-D1:** **Working analyzer + CI gate ships in Phase 1.** Custom `golang.org/x/tools/go/analysis` Pass detects any `select` statement that waits on both `plannerPool` and `executorPool` channels. Lives in `tools/analyzers/crosspool/`, has `analysistest` fixtures under `testdata/`, invoked through `cmd/tide-lint`. `make lint` runs locally; `.github/workflows/ci.yaml` fails the PR on violation.

**Sample CRDs**
- **D-G1:** `config/samples/` contains a hand-authored worked-example set: one Project, one Milestone, one Phase, one Plan, and eight Tasks named `alpha` through `theta` whose `dependsOn` edges match the README spec exactly. Applied via `kubectl apply -k config/samples/`, CRDs are accepted with CEL passing. Same task names + edges back the `pkg/dag` Kahn unit test fixture.
- **D-G2:** Sample files named `tide_v1alpha1_<kind>[_<name>].yaml`; `kustomization.yaml` orders them so `kubectl apply -k` respects owner-ref dependencies (Project before Milestone before Phase before Plan before Tasks).

### Claude's Discretion
- Webhook certificate strategy for Phase 1 (envtest-only — kubebuilder's auto-generated dev certs sufficient; cert-manager integration ships Phase 5)
- Conversion-webhook scaffold shape — pick whatever kubebuilder v4.14 emits for a single-version CRD's hub/spoke registration; no v1beta1 stubs until real
- Finalizer name convention — pick a `tide.io/<kind>-cleanup` form and apply uniformly
- Repo top-level layout details (`cmd/manager/main.go` vs `cmd/tide-controller/main.go`) — follow kubebuilder v4.14 scaffold defaults except where Recommended Project Structure overrides
- Status condition vocabulary — pick a small canonical set (`Pending`, `Ready`, `Reconciling`, `Failed`) and apply uniformly across all six CRDs
- Helm `Chart.yaml` `appVersion` / `version` initial values, image tag scheme (likely `v0.1.0-dev` pending first real release tag)
- `cmd/tide-lint` CLI surface beyond "runs the analyzer over the module"
- Unit-test framework choice — Ginkgo v2 + Gomega is kubebuilder default for the controller suite; `pkg/dag` may use stdlib `testing` with `t.Run` table tests
- Whether Phase 1's CI matrix includes a `kind v0.31` E2E run (recommended skip — `envtest` is enough; `kind` E2E lands in Phase 2)

### Deferred Ideas (OUT OF SCOPE)
- Dashboard chart template, `ServiceMonitor`, LICENSE headers, full external-operator docs — Phase 5 (DIST-01..05)
- Webhook actual cycle detection — Phase 2 (REQ-PLAN-01)
- File-touch ↔ `dependsOn` reconciliation — Phase 2 (REQ-PLAN-02)
- Real `Subagent` interface design — Phase 2 (REQ-SUB-01)
- kind E2E test tier — Phase 2 (REQ-TEST-02)
- `tide` CLI — Phase 4 (REQ-CLI-01..04)
- Per-level model selection field consumption on Project CRD — Phase 2/4 (schema slot reserved in P1)
- Conversion webhook actually doing conversion — beyond v1 (Phase 1 scaffolds hub/spoke only)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CRD-01 | Six CRDs in `apiVersion: tide.io/v1alpha1` with Spec/Status separation | §CRD Schema Shape; §kubebuilder Scaffolding Sequence |
| CRD-02 | Owner-reference cascade with `BlockOwnerDeletion: true`, scoped same-namespace | §Owner-Ref Helper; cites ARCHITECTURE.md Pattern 2 |
| CRD-03 | CEL validation rules for invariants CEL can express | §CEL Validation Markers Per Kind |
| CRD-04 | Validating admission webhook scaffold (no-op in P1) | §Webhook Scaffolding |
| CRD-05 | Conversion-webhook scaffolding from day one | §Webhook Scaffolding §Conversion (Pitfall 16) |
| CRD-06 | Kubebuilder RBAC markers, no wildcards | §RBAC Markers |
| DAG-01 | Pure-Go stdlib-only Kahn-layered library | §pkg/dag API §Package Layout |
| DAG-02 | Cycle detection at termination, returns `CycleError` | §pkg/dag CycleError Shape |
| DAG-03 | Typed-apart call sites for Planning vs Execution DAGs | §pkg/dag API §Typed-Apart Call Sites |
| DAG-04 | α…θ worked example pinned as regression fixture | §pkg/dag Test Strategy |
| DAG-05 | No K8s/controller-runtime/Anthropic-SDK imports | §pkg/dag Import Firewall Enforcement |
| CTRL-01 | One Manager, six reconcilers registered | §Manager Wiring |
| CTRL-02 | Event-driven `Owns(&batchv1.Job{})`, no sleep/blocking | §Reconciler Stub Anatomy |
| CTRL-03 | Leader election; in-flight resumption foundation | §Leader Election + Resumption Hooks |
| CTRL-04 | Per-reconciler `MaxConcurrentReconciles` tunable from Helm | §Configuration Plumbing |
| CTRL-05 | Finalizers with bounded deadlines, idempotent cleanup | §Finalizer Recipe |
| POOL-01 | Two `chan struct{}` semaphores, Helm-configured sizes | §Two-Pool Plumbing |
| POOL-02 | Pre-charge from live Jobs on restart | §Pool Pre-Charge on Restart |
| POOL-03 | Custom go-analyzer rejects cross-pool wait | §POOL-03 Custom Analyzer |
| AUTH-02 | Namespace-per-project tenancy, namespace-scoped RBAC | §RBAC Markers §Namespace Tenancy |
| AUTH-03 | No cluster-wide wildcards in orchestrator ServiceAccount | §RBAC Markers |
| PERSIST-01 | CRD `.status` only — no DB, no SQLite | §Status Schema Discipline (Pitfall 4 prevention) |
| PERSIST-02 | Per-Task status blocks small; aggregate Schedule fields forbidden | §Status Schema Discipline (Pitfall 2 prevention) |
| BOOT-01 | M0 commitment marker — Phases 1-4 = TIDE-on-host via GSD | §Bootstrap Commitment in P1 |
| BOOT-03 | Single `v1alpha1` schema across M0 → M_self; no breaking changes | §CRD Versioning Discipline (Pitfall 16 prevention) |
| TEST-01 | Unit tests for `pkg/dag` and core packages, <30s on CI | §Test Strategy + Sampling Rate |
</phase_requirements>

## Summary

Phase 1 lays the operator skeleton from an empty repo. The work splits cleanly into **eight task families**:

1. **Bootstrap scaffold** — `kubebuilder init` + six `kubebuilder create api` invocations + module setup
2. **CRD types** (`api/v1alpha1/`) — Spec/Status separation for six Kinds with CEL markers
3. **`pkg/dag`** — pure-Go stdlib Kahn-layered library with `CycleError` + α…θ fixture test
4. **Reconciler scaffold** (`internal/controller/`) — six "Standard depth" reconciler stubs with owner-ref handling, finalizers, status conditions
5. **Two-pool semaphores** (`internal/pool/`) — `plannerPool` + `executorPool` with pre-charge on restart
6. **Webhook scaffolding** (`internal/webhook/v1alpha1/`) — validating + conversion webhooks (no-op until P2)
7. **POOL-03 analyzer + CI gate** — `tools/analyzers/crosspool/` + `cmd/tide-lint/` + GitHub Action
8. **Distribution** — kubebuilder Kustomize (`config/`) → helmify → `charts/tide/` + `charts/tide-crds/` + sample CRDs in `config/samples/`

The phase is execution-heavy but novelty-light: kubebuilder good-practices are extensively documented, the project-level research already converged on the stack, and the user has locked the load-bearing structural decisions in CONTEXT.md. The dominant risk is **pitfall density at scaffold time** — eight critical/serious pitfalls bake in here if mis-shaped (P1 long-running reconcile, P3 DAG unification, P4 status-as-truth, P6 unified pool, P15 RBAC scope creep, P16 breaking CRD changes, P21 finalizer leaks, P23 wrong owner refs). Each gets a concrete prevention mechanism mapped to a verification step in §Validation Architecture.

**Primary recommendation:** Follow kubebuilder v4.14 scaffolding verbatim, hand-edit the Spec/Status types to match the CRD shapes below, and treat the eight pitfalls as PR-blocking constraints rather than soft preferences. The α…θ worked example is the single regression anchor that ties the spec, the test fixture, and the sample CRDs together — make it the spine of the phase's verification story.

## Standard Stack (cited from project research; do NOT re-derive)

Use CLAUDE.md's Technology Stack table verbatim. Versions, alternatives, and "What NOT to Use" are locked at the project level. Phase 1 touches:

| Technology | Version (pinned in CLAUDE.md) | Phase 1 Usage |
|------------|-------------------------------|---------------|
| Go | **1.26** (toolchain ≥ 1.25) | `go.mod` module declaration; toolchain directive |
| controller-runtime | **v0.24.x** (currently v0.24.1) | Manager, reconcilers, webhook server, leader election, metrics |
| kubebuilder | **v4.14.0** | Scaffold all six CRDs + controller skeleton + Kustomize + Makefile + envtest harness |
| Kubernetes target | **v1.33+** | CEL CRD validation (GA in 1.29; we use full feature set) |
| zap | **v1.28.x** | Structured JSON logging behind logr (kubebuilder default) |
| logr | **v1.4.x** | Logging interface (controller-runtime exposes) |
| prometheus/client_golang | **v1.23.x** | Transitive via controller-runtime; manager exposes `/metrics` on port 8080. No custom metrics in P1 (those land P4) |
| Ginkgo v2 | **v2.28.x** + Gomega | Controller envtest suite |
| stdlib `testing` | — | `pkg/dag` table tests (Ginkgo overkill for pure synchronous package) |
| `golang.org/x/tools/go/analysis` | latest | POOL-03 custom analyzer Pass framework |
| `helmify` | latest | Release-time conversion of `config/` → `charts/` |
| `controller-gen` | bundled with kubebuilder | DeepCopy, CRD manifests, RBAC YAML, webhook configs from markers |
| `setup-envtest` | bundled with controller-runtime | Downloads etcd + kube-apiserver binaries for envtest |

**Explicitly NOT in Phase 1** (cited from CLAUDE.md "What NOT to Use"):
- Anthropic Go SDK — Phase 2 (subagent harness)
- `go-git/v5` — Phase 3 (git integration)
- `go.opentelemetry.io/otel` — Phase 4 (observability)
- `chi/v5` — Phase 4 (dashboard backend)
- React Flow / Tailwind / dagre — Phase 4 (dashboard frontend)
- `Gonum` / `dominikbraun/graph` — never (use stdlib for Kahn)

**Version verification status:** All versions verified at project-research time (May 2026 against upstream releases). No need to re-verify at Phase 1 plan-time unless a new release lands between now and execution.

## kubebuilder Scaffolding Sequence

The exact command sequence to produce the v1alpha1 scaffold. Run **once**; do not redo.

```bash
# 1. Initialize the project (sets go.mod, Makefile, Dockerfile, config/, hack/, etc.)
kubebuilder init \
  --domain tide.io \
  --repo github.com/jsquirrelz/tide \
  --owner "TIDE Authors" \
  --license apache2 \
  --project-name tide

# 2. Scaffold each of the six CRDs with controller + types
#    --resource: emit api/v1alpha1/<kind>_types.go
#    --controller: emit internal/controller/<kind>_controller.go
#    Webhooks are added separately in step 3 (kubebuilder create webhook).
kubebuilder create api --group tide --version v1alpha1 --kind Project    --resource --controller
kubebuilder create api --group tide --version v1alpha1 --kind Milestone  --resource --controller
kubebuilder create api --group tide --version v1alpha1 --kind Phase      --resource --controller
kubebuilder create api --group tide --version v1alpha1 --kind Plan       --resource --controller
kubebuilder create api --group tide --version v1alpha1 --kind Task       --resource --controller
kubebuilder create api --group tide --version v1alpha1 --kind Wave       --resource --controller

# 3. Scaffold webhooks for the Kinds that need them.
#    Plan needs validating admission (cycle detection in P2 — no-op in P1) + conversion (for Pitfall 16 future-proofing).
#    Wave needs validating admission (rejects all client-applied Waves per D-B1).
#    Task gets CEL-only — cross-task invariants (filesTouched non-empty, dependsOn refers to siblings) fit CEL.
#    Project/Milestone/Phase get CEL-only for Phase 1.
kubebuilder create webhook --group tide --version v1alpha1 --kind Plan --conversion --programmatic-validation
kubebuilder create webhook --group tide --version v1alpha1 --kind Wave --programmatic-validation

# 4. Generate manifests + deepcopy + RBAC YAML from markers
make generate    # zz_generated_deepcopy.go for each api type
make manifests   # CRD YAML in config/crd/bases/, RBAC YAML in config/rbac/, webhook configs in config/webhook/
```

**What's auto-generated vs. hand-edited:**

| Path | Auto-Generated | Hand-Edited |
|------|----------------|-------------|
| `go.mod`, `go.sum` | Yes (init + module fetches) | Pin versions per Stack table |
| `Makefile` | Yes | Add `make helm`, `make lint`, `make tide-lint` targets |
| `Dockerfile` | Yes | Update FROM base image; multi-arch build |
| `config/manager/manager.yaml` | Yes | Add ConfigMap volume mount for runtime config |
| `config/rbac/role.yaml` | Yes (from `+kubebuilder:rbac:` markers) | Markers are hand-authored on reconcilers |
| `config/crd/bases/*.yaml` | Yes (from struct tags + markers) | Markers are hand-authored on types |
| `config/webhook/*.yaml` | Yes | Cert annotations for envtest dev certs |
| `config/samples/*.yaml` | Skeleton auto-generated | Replace with α…θ worked example per D-G1 |
| `api/v1alpha1/*_types.go` | Skeleton with empty Spec/Status structs | Fill Spec/Status fields per §CRD Schema Shape |
| `internal/controller/*_controller.go` | Skeleton `Reconcile()` returning empty + `SetupWithManager` | Fill per §Reconciler Stub Anatomy |
| `internal/webhook/v1alpha1/plan_webhook.go` | Skeleton with empty `ValidateCreate/Update/Delete` + `ConvertTo/From` | Return nil/Allow (no-op) until Phase 2 |
| `cmd/main.go` (kubebuilder default) | Yes | Replace with custom Manager wiring per §Manager Wiring |

**Webhook scaffolding flag reference (sources verified):**
- `kubebuilder create webhook ... --programmatic-validation` → emits `ValidateCreate/Update/Delete` stubs in `internal/webhook/v1alpha1/<kind>_webhook.go`
- `kubebuilder create webhook ... --defaulting` → emits `Default()` stub (we don't need this in P1)
- `kubebuilder create webhook ... --conversion` → emits hub/spoke conversion machinery and webhook server registration (Pitfall 16 future-proofing — even though only v1alpha1 exists, the infrastructure for serving conversions is in place)

**Important nuance:** `kubebuilder create api` does NOT take `--conversion --defaulting --programmatic-validation` flags directly — those belong to `kubebuilder create webhook`. The two-step pattern (`create api` then `create webhook`) is the v4.14 idiom. Source: https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html

## CRD Schema Shape (six Kinds)

All six CRDs live in `api/v1alpha1/`. Group: `tide.io`. Version: `v1alpha1`. Scope: **Namespaced** for all six.

### Status Condition Vocabulary (apply uniformly across all six)

Canonical conditions (`metav1.Condition` slice on each Kind's `.status.conditions`):

| Condition Type | Status Transitions | Meaning |
|----------------|-------------------|---------|
| `Pending` | True → False | Object exists, reconciler hasn't started work yet |
| `Ready` | False → True | Object is in its terminal-success state |
| `Reconciling` | True / False | Reconciler is actively transitioning state (transient) |
| `Failed` | False → True (sticky) | Object hit a terminal failure that needs human intervention |

Use `meta.SetStatusCondition()` from `k8s.io/apimachinery/pkg/api/meta` to manage these. The condition reasons should be machine-readable CamelCase (`SubagentDispatchFailed`, `FinalizerTimedOut`, etc.).

### Project (CRD-01, CRD-02)

```go
type ProjectSpec struct {
    // TargetRepo is the git URL of the repository TIDE will drive.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    TargetRepo string `json:"targetRepo"`

    // SecretRefs holds references to K8s Secrets in the same namespace.
    // Phase 1 reserves the schema; Phase 3 wires consumption.
    // +kubebuilder:validation:Optional
    SecretRefs SecretRefs `json:"secretRefs,omitempty"`

    // ModelSelection holds per-level model identifiers.
    // Phase 1 reserves the schema; Phase 2/4 wires consumption.
    // +kubebuilder:validation:Optional
    ModelSelection ModelSelection `json:"modelSelection,omitempty"`

    // Gates declares per-level approval policy.
    // Phase 1 reserves the schema; Phase 4 wires consumption.
    // +kubebuilder:validation:Optional
    Gates Gates `json:"gates,omitempty"`
}

type SecretRefs struct {
    // Phase 3 wires these.
    AnthropicAPIKey  string `json:"anthropicAPIKey,omitempty"`  // Secret name in same namespace
    GitCredentials   string `json:"gitCredentials,omitempty"`
}

type ModelSelection struct {
    Milestone string `json:"milestone,omitempty"`
    Phase     string `json:"phase,omitempty"`
    Plan      string `json:"plan,omitempty"`
    Task      string `json:"task,omitempty"`
}

type Gates struct {
    Milestone string `json:"milestone,omitempty"`  // "auto" | "approve" | "pause"
    Phase     string `json:"phase,omitempty"`
    Plan      string `json:"plan,omitempty"`
    Task      string `json:"task,omitempty"`
}

type ProjectStatus struct {
    // +kubebuilder:validation:Optional
    Phase string `json:"phase,omitempty"`  // "Pending" | "Running" | "Complete" | "Failed"

    // Conditions follows the K8s standard condition pattern.
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

CEL markers (CRD-03):
```go
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http') || self.targetRepo.startsWith('git@')",message="targetRepo must be a valid http(s) or SSH git URL"
```

### Milestone (CRD-01, CRD-02)

Minimal in Phase 1 (dispatch is P2/P3):

```go
type MilestoneSpec struct {
    // ProjectRef is the owning Project's name (also enforced via ownerReferences).
    // +kubebuilder:validation:Required
    ProjectRef string `json:"projectRef"`

    // DependsOn lists sibling Milestone names within the same Project.
    // Empty for the first milestone in a Project.
    // +kubebuilder:validation:Optional
    DependsOn []string `json:"dependsOn,omitempty"`
}

type MilestoneStatus struct {
    Phase      string             `json:"phase,omitempty"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Phase 2+ adds: ArtifactRef (PVC path to MILESTONE.md), CompletedAt timestamp
}
```

### Phase (CRD-01, CRD-02)

Same shape as Milestone — `DependsOn []string` (sibling Phase names within the owning Milestone):

```go
type PhaseSpec struct {
    // +kubebuilder:validation:Required
    MilestoneRef string `json:"milestoneRef"`

    // +kubebuilder:validation:Optional
    DependsOn []string `json:"dependsOn,omitempty"`
}

type PhaseStatus struct {
    Phase      string             `json:"phase,omitempty"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### Plan (CRD-01, CRD-02, CRD-04, CRD-05)

```go
type PlanSpec struct {
    // +kubebuilder:validation:Required
    PhaseRef string `json:"phaseRef"`
}

type PlanStatus struct {
    Phase      string             `json:"phase,omitempty"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Phase 2 adds: ValidationState ("Valid" | "Cyclic" | "FileTouchMismatch"), CycleEdges []string
    // PERSIST-02: NO Schedule, NO Waves array, NO indegree map cached here.
}
```

Webhook coverage:
- **Validating admission webhook** scaffolded with `--programmatic-validation` (Phase 1 returns nil/Allow; Phase 2 wires cycle detection via `pkg/dag.ComputeWaves`)
- **Conversion webhook** scaffolded with `--conversion` (Phase 1: hub/spoke registration only, no v1beta1 spoke yet — Pitfall 16 future-proofing)

### Task (CRD-01, CRD-02, CRD-03 with strong CEL)

```go
type TaskSpec struct {
    // +kubebuilder:validation:Required
    PlanRef string `json:"planRef"`

    // DependsOn is a list of sibling Task names within the same Plan.
    // Strings only — no cross-Plan references (CEL validates).
    // +kubebuilder:validation:Optional
    DependsOn []string `json:"dependsOn,omitempty"`

    // FilesTouched lists paths under /workspace/repo/ the Task will write.
    // Required and non-empty per D-F2.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinItems=1
    FilesTouched []string `json:"filesTouched"`

    // PromptRef (Phase 2): name of the configured subagent prompt template.
    // Phase 1: optional placeholder.
    // +kubebuilder:validation:Optional
    PromptRef string `json:"promptRef,omitempty"`
}

type TaskStatus struct {
    Phase      string             `json:"phase,omitempty"`  // "Pending" | "Dispatching" | "Running" | "Succeeded" | "Failed"
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Phase 2 adds: ActiveJobName, Attempt int, CompletedAt, ExitCode, EnvelopeDigest
    // PERSIST-02: small status block. No log lines, no LLM payloads.
}
```

CEL markers:
```go
// +kubebuilder:validation:XValidation:rule="!('-' in self.filesTouched.exists(p, p == ''))",message="filesTouched paths must be non-empty strings"
// Cross-Task "dependsOn refers to sibling" cannot be expressed in CEL (cross-object). Phase 2 wires this in the Plan webhook.
```

### Wave (CRD-01, CRD-02, CRD-04 webhook-rejects-client-applies)

Per D-B2, `Spec` carries ONLY `planRef` + `waveIndex`. Everything else lives in `Status`:

```go
type WaveSpec struct {
    // +kubebuilder:validation:Required
    PlanRef string `json:"planRef"`

    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Minimum=0
    WaveIndex int `json:"waveIndex"`
}

type WaveStatus struct {
    Phase      string             `json:"phase,omitempty"`  // "Pending" | "Dispatching" | "Complete" | "Failed"
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // TaskRefs is the set of Task names in this wave. Populated by WaveReconciler
    // at create time by re-deriving from pkg/dag.ComputeWaves over the Plan's Tasks.
    // +kubebuilder:validation:Optional
    TaskRefs []string `json:"taskRefs,omitempty"`

    // DispatchedAt and CompletedAt are stamped by WaveReconciler.
    // +kubebuilder:validation:Optional
    DispatchedAt *metav1.Time `json:"dispatchedAt,omitempty"`
    CompletedAt  *metav1.Time `json:"completedAt,omitempty"`
}
```

**Wave validating webhook** (scaffolded in P1, no-op until P2): rejects any client-applied Wave per D-B1. P2 wires the rejection logic; P1 just registers the webhook endpoint.

### Anti-Patterns to Avoid

Cited from ARCHITECTURE.md §Anti-Patterns. Phase 1 PRs must not regress these:

- **AP-1 Single mega-orchestrator controller** — six distinct reconcilers, not one walks-everything
- **AP-2 Persisting wave schedule in CRD status** — `Wave.Status.TaskRefs` is OK (observation, re-derived); `Plan.Status.Schedule` is forbidden
- **AP-3 Inline sub-resources** — six Kinds with owner refs, not nested structs
- **AP-7 Wave as inline field of Plan** — Wave is its own Kind per D-B1/B2

## Architecture Patterns

Cited from ARCHITECTURE.md. Phase 1 wires these:

### Pattern 1: One Reconciler Per CRD Kind
Six reconcilers on one Manager. See §Manager Wiring below.

### Pattern 2: Owner-Reference Cascade
Each child sets `metadata.ownerReferences[0]` to its parent with `BlockOwnerDeletion: true, controller: true`. Same-namespace only (enforced by helper). See §Owner-Ref Helper below.

### Pattern 3: Two-DAG, One Algorithm (`pkg/dag` used twice)
Pure-Go package. See §pkg/dag API below.

### Pattern 4: Two Parallelism Budgets as In-Memory Semaphores
Two `chan struct{}` semaphores in the Manager process. See §Two-Pool Plumbing below.

### Recommended Project Structure (cited from ARCHITECTURE.md)

```
.
├── cmd/
│   ├── manager/main.go             # orchestrator entry point
│   └── tide-lint/main.go           # custom analyzer CLI (POOL-03)
├── api/
│   └── v1alpha1/
│       ├── groupversion_info.go
│       ├── project_types.go
│       ├── milestone_types.go
│       ├── phase_types.go
│       ├── plan_types.go
│       ├── task_types.go
│       ├── wave_types.go
│       ├── shared_types.go         # Status conditions vocab, common helpers
│       └── zz_generated_deepcopy.go
├── internal/
│   ├── controller/
│   │   ├── project_controller.go
│   │   ├── milestone_controller.go
│   │   ├── phase_controller.go
│   │   ├── plan_controller.go
│   │   ├── task_controller.go
│   │   ├── wave_controller.go
│   │   └── suite_test.go           # Ginkgo+envtest TestMain
│   ├── webhook/
│   │   └── v1alpha1/
│   │       ├── plan_webhook.go     # validating + conversion (no-op)
│   │       └── wave_webhook.go     # validating (reject-all-client-applies — no-op P1)
│   ├── pool/
│   │   ├── pool.go                 # Pool type, Acquire/Release, PreCharge
│   │   └── pool_test.go
│   ├── owner/
│   │   ├── owner.go                # EnsureOwnerRef same-namespace helper
│   │   └── owner_test.go
│   ├── finalizer/
│   │   └── finalizer.go            # Bounded-deadline finalizer recipe
│   └── config/
│       └── config.go               # Runtime config loaded from /etc/tide/config.yaml
├── pkg/
│   └── dag/
│       ├── kahn.go                 # ComputeWaves, CycleError
│       ├── kahn_test.go            # α…θ fixture + cycle fixtures + edge cases
│       └── doc.go                  # package docs naming Planning vs Execution call sites
├── tools/
│   └── analyzers/
│       └── crosspool/
│           ├── analyzer.go         # golang.org/x/tools/go/analysis Pass
│           ├── analyzer_test.go    # analysistest.Run
│           └── testdata/
│               └── src/
│                   ├── valid/      # one valid example
│                   └── violation/  # one cross-pool-wait example
├── config/                         # kubebuilder Kustomize output
│   ├── crd/bases/                  # six CRD YAMLs
│   ├── rbac/                       # ServiceAccount, Role, RoleBinding (no wildcards)
│   ├── manager/                    # Deployment + ConfigMap volume mount
│   ├── webhook/                    # webhook configs + dev certs
│   ├── samples/                    # α…θ worked example per D-G1
│   │   ├── kustomization.yaml      # orders by owner-ref depth
│   │   ├── tide_v1alpha1_project.yaml
│   │   ├── tide_v1alpha1_milestone.yaml
│   │   ├── tide_v1alpha1_phase.yaml
│   │   ├── tide_v1alpha1_plan.yaml
│   │   ├── tide_v1alpha1_task_alpha.yaml
│   │   ├── tide_v1alpha1_task_beta.yaml
│   │   ├── ... (eight Tasks, alpha through theta)
│   │   └── tide_v1alpha1_task_theta.yaml
│   └── default/                    # kustomize composition
├── charts/
│   ├── tide/                       # helmify output (controller-only)
│   │   ├── Chart.yaml
│   │   ├── values.yaml             # plannerConcurrency, executorConcurrency, maxConcurrentReconciles
│   │   └── templates/
│   └── tide-crds/                  # helmify output (CRD subchart for safe helm upgrade)
│       ├── Chart.yaml
│       └── templates/
├── hack/                           # codegen helpers
├── .github/
│   └── workflows/
│       └── ci.yaml                 # go test, make lint, make tide-lint
├── Makefile
├── Dockerfile
├── go.mod
└── go.sum
```

### Reconciler Stub Anatomy (Standard depth per D-C1)

Every reconciler follows this six-step pattern. No `time.Sleep`, no blocking I/O, no LLM calls — that's D-C2 and Pitfall 1 prevention:

```go
func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := ctrl.LoggerFrom(ctx)

    // 1. Fetch the object.
    var project tidev1alpha1.Project
    if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Finalizer: if being deleted, run bounded-deadline cleanup, then remove finalizer.
    finalizerName := "tide.io/project-cleanup"
    if !project.DeletionTimestamp.IsZero() {
        return r.handleDeletion(ctx, &project, finalizerName)
    }

    // 3. Ensure finalizer is set (idempotent).
    if !controllerutil.ContainsFinalizer(&project, finalizerName) {
        controllerutil.AddFinalizer(&project, finalizerName)
        if err := r.Update(ctx, &project); err != nil {
            return ctrl.Result{}, err
        }
        return ctrl.Result{}, nil // requeue after update
    }

    // 4. Ensure owner-refs on children (for parent Kinds; Project has no parent so skip).
    //    For child reconcilers: ensureOwnerRef(child, &parent) before creating.

    // 5. Reconcile state — Phase 1: only status conditions get set.
    //    Phase 2+ adds: subagent dispatch via r.Dispatcher (stubbed nil in P1).
    if r.Dispatcher != nil {
        // Phase 2+ fills this body
    }

    // 6. Update status conditions and return.
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:               "Ready",
        Status:             metav1.ConditionTrue,
        Reason:             "Initialized",
        Message:            "Project scaffolded; awaiting dispatch logic (Phase 2)",
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Update(ctx, &project); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}

// SetupWithManager wires the watch, including Owns(&batchv1.Job{}) per CTRL-02.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&tidev1alpha1.Project{}).
        Owns(&batchv1.Job{}).
        WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
        Complete(r)
}
```

**Reconciler struct shape** — same for all six, with placeholders for Phase 2+:

```go
type ProjectReconciler struct {
    client.Client
    Scheme *runtime.Scheme

    // MaxConcurrentReconciles tuned from runtime config per CTRL-04.
    MaxConcurrentReconciles int

    // PlannerPool / ExecutorPool injected per Pattern 4. Nil in tests that skip pool wiring.
    PlannerPool  *pool.Pool   // used by Milestone/Phase/Plan reconcilers in P2
    ExecutorPool *pool.Pool   // used by Wave/Task reconcilers in P2

    // Dispatcher is the seam for Phase 2's subagent interface.
    // Nil in Phase 1; Phase 2 injects a real value.
    Dispatcher dispatch.Dispatcher  // interface declared in pkg/dispatch (Phase 2)
}
```

**Important Phase 1 ↔ Phase 2 boundary:** The `Dispatcher` field shape is reserved but the `pkg/dispatch` package's interface contents are Phase 2 work (per CONTEXT.md "Integration Points"). Phase 1 lands `pkg/dispatch/doc.go` reserving the package name and a single empty interface placeholder (`type Dispatcher interface{}`); Phase 2 designs `Subagent.Run()` and replaces. This avoids a Phase 2 refactor.

## Manager Wiring (CTRL-01, CTRL-03, CTRL-04)

`cmd/manager/main.go`:

```go
func main() {
    var configPath string
    flag.StringVar(&configPath, "config", "/etc/tide/config.yaml", "Path to runtime config")
    flag.Parse()

    cfg, err := config.Load(configPath)
    must(err)

    ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        Scheme:                 scheme,
        LeaderElection:         true,
        LeaderElectionID:       "tide-controller-leader.tide.io",
        HealthProbeBindAddress: ":8081",
        Metrics: metricsserver.Options{BindAddress: ":8080"},
        WebhookServer: webhook.NewServer(webhook.Options{Port: 9443}),
    })
    must(err)

    // 1. Two semaphores per POOL-01 (sized from config per CTRL-04).
    plannerPool := pool.New(cfg.PlannerConcurrency, "planner")
    executorPool := pool.New(cfg.ExecutorConcurrency, "executor")

    // 2. Pre-charge from live Jobs per POOL-02.
    if err := plannerPool.PreCharge(ctx, mgr.GetClient(), "tide.io/role=planner"); err != nil {
        log.Error(err, "planner pool pre-charge")
    }
    if err := executorPool.PreCharge(ctx, mgr.GetClient(), "tide.io/role=executor"); err != nil {
        log.Error(err, "executor pool pre-charge")
    }

    // 3. Register all six reconcilers per CTRL-01.
    must((&controller.ProjectReconciler{
        Client:                  mgr.GetClient(),
        Scheme:                  mgr.GetScheme(),
        MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Project,
    }).SetupWithManager(mgr))
    must((&controller.MilestoneReconciler{
        Client:                  mgr.GetClient(),
        Scheme:                  mgr.GetScheme(),
        MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Milestone,
        PlannerPool:             plannerPool,
    }).SetupWithManager(mgr))
    must((&controller.PhaseReconciler{...}).SetupWithManager(mgr))
    must((&controller.PlanReconciler{...}).SetupWithManager(mgr))
    must((&controller.WaveReconciler{
        Client:                  mgr.GetClient(),
        Scheme:                  mgr.GetScheme(),
        MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Wave,
        ExecutorPool:            executorPool,
    }).SetupWithManager(mgr))
    must((&controller.TaskReconciler{
        Client:                  mgr.GetClient(),
        Scheme:                  mgr.GetScheme(),
        MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Task,
        ExecutorPool:            executorPool,
    }).SetupWithManager(mgr))

    // 4. Register webhooks (P1: scaffolded as no-ops).
    must(builder.WebhookManagedBy(mgr, &tidev1alpha1.Plan{}).
        WithValidator(&webhookv1alpha1.PlanValidator{}).
        WithCustomConversion(...).
        Complete())
    must(builder.WebhookManagedBy(mgr, &tidev1alpha1.Wave{}).
        WithValidator(&webhookv1alpha1.WaveValidator{}).
        Complete())

    must(mgr.Start(ctrl.SetupSignalHandler()))
}
```

### Configuration Plumbing (CTRL-04)

`internal/config/config.go` loads from `/etc/tide/config.yaml` (mounted from ConfigMap by the Helm chart):

```go
type Config struct {
    PlannerConcurrency   int                 `yaml:"plannerConcurrency"`   // default 16
    ExecutorConcurrency  int                 `yaml:"executorConcurrency"`  // default 4
    MaxConcurrentReconciles MaxConcurrentReconciles `yaml:"maxConcurrentReconciles"`
}

type MaxConcurrentReconciles struct {
    Project   int `yaml:"project"`   // default 1
    Milestone int `yaml:"milestone"` // default 1
    Phase     int `yaml:"phase"`     // default 2
    Plan      int `yaml:"plan"`      // default 4
    Wave      int `yaml:"wave"`      // default 8
    Task      int `yaml:"task"`      // default 16
}
```

`values.yaml` (Helm) exposes these. The Helm chart renders them into a ConfigMap; the Deployment mounts it at `/etc/tide/config.yaml`.

**Why ConfigMap not env vars:** structured config with nested maps is awkward in env-vars; ConfigMap is the idiomatic K8s path for hierarchical runtime config.

### Leader Election + Resumption Hooks (CTRL-03)

`LeaderElection: true` enables controller-runtime's built-in leader-election machinery (uses K8s `Lease` objects under the hood). On failover, the new leader's Manager starts, watches re-fire initial reconciles for every existing CRD, and per-Reconcile re-derivation does the rest. Phase 1 ships the leader-election scaffolding; **the chaos-resume test** (PERSIST-04) lives in Phase 3 because it needs actual Jobs to kill, which Phase 1 doesn't create.

**What Phase 1 must verify:** start two Manager processes against the same envtest cluster, kill one, see the other take over. (Envtest supports this — leader election is API-driven.)

## Owner-Ref Helper (CRD-02, Pitfall 23 prevention)

`internal/owner/owner.go`:

```go
// EnsureOwnerRef sets the controller-style owner reference on child pointing to parent.
// Returns an error if parent and child are in different namespaces (cross-namespace
// owner refs are silently ignored by K8s — Pitfall 23).
func EnsureOwnerRef(child, parent metav1.Object, scheme *runtime.Scheme) error {
    if child.GetNamespace() != parent.GetNamespace() {
        return fmt.Errorf("cross-namespace owner ref: parent=%s/%s child=%s/%s",
            parent.GetNamespace(), parent.GetName(),
            child.GetNamespace(), child.GetName())
    }
    parentRO, ok := parent.(runtime.Object)
    if !ok {
        return fmt.Errorf("parent is not a runtime.Object")
    }
    childRO, ok := child.(runtime.Object)
    if !ok {
        return fmt.Errorf("child is not a runtime.Object")
    }
    return controllerutil.SetControllerReference(parent, child, scheme,
        controllerutil.WithBlockOwnerDeletion(true))
}
```

Every reconciler creating a child resource calls this helper. Phase 1 unit-tests it with table tests covering:
- Same-namespace, valid → no error, owner-ref set with `BlockOwnerDeletion: true`
- Different namespaces → error returned (Pitfall 23 prevention)
- Nil parent or nil child → error returned

**Why a custom helper instead of just `controllerutil.SetControllerReference` directly:** the same-namespace check is the load-bearing Pitfall 23 prevention. controller-runtime doesn't enforce it; we do.

## Finalizer Recipe (CTRL-05, Pitfall 21 prevention)

`internal/finalizer/finalizer.go`:

```go
// HandleDeletion runs the cleanup logic for `obj` with a bounded deadline.
// If cleanup exceeds the deadline, logs loudly, removes the finalizer anyway,
// surfaces a `FinalizerTimedOut` condition.
func HandleDeletion(
    ctx context.Context,
    c client.Client,
    obj client.Object,
    finalizerName string,
    cleanup func(context.Context) error,
    timeout time.Duration,
) (ctrl.Result, error) {
    if !controllerutil.ContainsFinalizer(obj, finalizerName) {
        return ctrl.Result{}, nil
    }

    cleanupCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    if err := cleanup(cleanupCtx); err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            // Log loudly, surface condition, remove finalizer anyway.
            log.FromContext(ctx).Error(err, "finalizer cleanup deadline exceeded; forcibly removing",
                "object", klog.KObj(obj), "deadline", timeout)
            // Set Failed condition via a status update helper here (omitted for brevity).
            controllerutil.RemoveFinalizer(obj, finalizerName)
            return ctrl.Result{}, c.Update(ctx, obj)
        }
        // Non-timeout error: requeue, don't remove finalizer.
        return ctrl.Result{Requeue: true}, err
    }

    // Cleanup succeeded — remove finalizer to allow GC.
    controllerutil.RemoveFinalizer(obj, finalizerName)
    return ctrl.Result{}, c.Update(ctx, obj)
}
```

**Phase 1 cleanup is a no-op** for all six Kinds (no Jobs exist yet to clean up). The recipe ships; the cleanup function bodies grow with Phase 2's dispatch.

**Finalizer name convention** (Claude's Discretion per CONTEXT.md): `tide.io/<kind>-cleanup` — e.g. `tide.io/project-cleanup`, `tide.io/wave-cleanup`.

**Bounded deadline:** Recommend 5 minutes for the cleanup timeout (matches Pitfall 21's documented industry pattern). Configurable per-Kind if cleanup grows expensive; Phase 1 uses a single constant.

**Documented manual unstick** (required by CTRL-05): a runbook entry showing `kubectl patch <kind> <name> --type=merge -p '{"metadata":{"finalizers":null}}'` for operators when the controller is genuinely down. Phase 1 includes this in `docs/RBAC.md` or `docs/troubleshooting.md` (deferred to Phase 5 for full docs, but the patch command lands in Phase 1's CHANGELOG/README at minimum).

## pkg/dag API (DAG-01 through DAG-05)

### Package shape

`pkg/dag/kahn.go`:

```go
// Package dag is a pure-Go, stdlib-only implementation of Kahn's algorithm
// in layered form. It is consumed twice in TIDE:
//
//   1. Planning DAG: nodes are artifacts to author (MILESTONE.md, phase briefs, PLAN.md).
//      Edges are "this artifact's authoring requires another artifact's interface."
//      Used by Milestone/Phase reconcilers (Phase 2/3 — not Phase 1).
//
//   2. Execution DAG: nodes are Tasks. Edges are declared task dependencies.
//      Used by the Plan admission webhook (Phase 2) and the WaveReconciler (Phase 2).
//
// Per DAG-03, callers wrap pkg/dag outputs in their respective domain types
// (PlanningWave, ExecutionWave) rather than passing the raw [][]NodeID around.
// This keeps the two DAGs typed apart at the API level while sharing the algorithm.
//
// Per DAG-05, this package may NOT import:
//   - k8s.io/* (any)
//   - sigs.k8s.io/* (any)
//   - github.com/anthropics/* (any)
// Enforced by the import-firewall analyzer in tools/analyzers/.
package dag

// NodeID is the unique identifier of a node in the DAG. Generic strings — callers
// project domain identifiers (Task names, artifact names) into this type.
type NodeID = string

// Edge expresses "From must complete before To."
type Edge struct {
    From NodeID
    To   NodeID
}

// CycleError is returned by ComputeWaves when the input graph contains a cycle.
// The error names every node involved in the unresolvable indegree state per DAG-02.
type CycleError struct {
    InvolvedNodes []NodeID
}

func (e *CycleError) Error() string {
    return fmt.Sprintf("cyclic DAG: nodes with unresolvable indegrees: %v", e.InvolvedNodes)
}

// ComputeWaves runs layered Kahn's algorithm over nodes and edges.
//
// Returns the layered topological sort as [][]NodeID where each inner slice is
// one wave (set of nodes whose upstream dependencies are all satisfied once
// previous waves have completed).
//
// Within each wave, NodeIDs are sorted lexicographically for deterministic output.
//
// Returns *CycleError if the graph contains a cycle.
//
// Complexity: O(V + E).
//
// Per DAG-04, the spec's worked example (Tasks: α,β,γ,δ,ε,ζ,η,θ →
// Waves: [{α,β,γ,ζ},{δ,η},{ε,θ}]) is pinned as a regression fixture in kahn_test.go.
func ComputeWaves(nodes []NodeID, edges []Edge) ([][]NodeID, error) {
    // Implementation: textbook Kahn-layered. ~30 lines stdlib.
    // (Full impl in §Code Examples below.)
}
```

**Return type:** `[][]NodeID` — a slice of waves, each wave a sorted slice of node IDs. REQ-DAG-01 says "as `[]Set[NodeID]`" — `[][]NodeID` with internal sorting is the idiomatic-Go realization of that contract (Go has no built-in set; a sorted slice provides set-like semantics with stable iteration). Document this explicitly in the package doc comment.

**Why string node IDs and not generics:** the spec is going to be used with Task names (strings from CRD .metadata.name) and artifact names (strings from filesystem paths). Generic over `comparable` adds complexity for zero callers benefit at v1. If a future second consumer needs typed IDs, type-aliasing `type TaskID = string` at the callsite preserves the algorithm.

### Typed-Apart Call Sites (DAG-03)

Phase 1 ships `pkg/dag` as a leaf package. Phase 2 introduces the two call-site wrappers:

```go
// pkg/planning/wave.go (Phase 2/3)
type PlanningWave []string
func ComputePlanningWaves(artifacts []ArtifactRef) ([]PlanningWave, error) { ... }

// internal/controller/wave_controller.go (Phase 2)
type ExecutionWave []string
func computeExecutionWaves(tasks []tidev1alpha1.Task) ([]ExecutionWave, error) { ... }
```

Both internally call `dag.ComputeWaves(...)` and wrap the result. Phase 1's job is just to make `pkg/dag` the only place the algorithm lives; the wrapping happens later. Document this in `pkg/dag/doc.go` so future contributors see the intent.

### Import Firewall Enforcement (DAG-05)

Three options for enforcing "no K8s/controller-runtime/Anthropic imports":

| Mechanism | Implementation | Pros | Cons |
|-----------|----------------|------|------|
| **Make target with `go list -deps`** | `make verify-dag-imports` greps the dependency tree of `./pkg/dag/...` for forbidden prefixes | Simple, no extra tooling | Easy to forget to run; CI-only |
| **Custom go-analyzer (banimports-style)** | Pass walks `*ast.ImportSpec` for `pkg/dag/*.go` files; flags forbidden imports | Catches violations at vet time | Slightly more code; need fixtures |
| **`forbidigo` config in golangci-lint** | `.golangci.yml` rule: `pkg/dag/**: forbid import "k8s.io"` etc. | Reuses existing lint pipeline | golangci-lint version coupling |

**Recommended:** Use `make verify-dag-imports` as a Makefile target invoking `go list -deps ./pkg/dag/... | grep -E '^(k8s.io|sigs.k8s.io|github.com/anthropics)' && exit 1 || exit 0`. Wire into CI. Simple, no new dependencies, fails loudly. Future Phase 2 could add the analyzer for richer error messages, but Phase 1 doesn't need it — the rule is a one-grep check.

This is **separate from** the POOL-03 cross-pool-wait analyzer (D-D1). POOL-03 is genuinely novel detection (AST shape matching). DAG-05 is a dependency-graph check — different mechanism.

### pkg/dag Test Strategy (DAG-04)

`pkg/dag/kahn_test.go` — table tests using stdlib `testing.T.Run`:

| Test Case | Input | Expected Output |
|-----------|-------|-----------------|
| `AlphaThroughTheta` (the α…θ regression fixture) | Nodes: α,β,γ,δ,ε,ζ,η,θ; Edges: α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ | `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]` |
| `EmptyGraph` | Nodes: []; Edges: [] | `[]` (no waves) |
| `SingleNode` | Nodes: [α]; Edges: [] | `[{α}]` |
| `FullyParallel` | Nodes: [α,β,γ]; Edges: [] | `[{α,β,γ}]` |
| `LinearChain` | Nodes: [α,β,γ]; Edges: α→β, β→γ | `[{α},{β},{γ}]` |
| `CycleSimple` | Nodes: [α,β]; Edges: α→β, β→α | `CycleError{InvolvedNodes: [α,β]}` |
| `CycleWithIslands` | Nodes: [α,β,γ,δ]; Edges: α→β, γ→δ, δ→γ | `CycleError{InvolvedNodes: [γ,δ]}` (α,β resolve cleanly; γ,δ cycle is named) |
| `DependsOnNonexistent` | Nodes: [α]; Edges: α→β | Error (β not in node set) or panic — define behavior explicitly in API |
| `DuplicateEdges` | Nodes: [α,β]; Edges: α→β, α→β | `[{α},{β}]` (duplicate edges idempotent) |

**Determinism check:** Run `AlphaThroughTheta` 100 times in a loop, assert identical output every time (sort within each wave guarantees this).

**Performance check (optional in P1):** A benchmark `BenchmarkComputeWaves_1000Nodes` with a randomly-generated DAG of 1000 nodes. Confirms O(V+E) scaling. Useful regression anchor.

## Two-Pool Plumbing (POOL-01, POOL-02)

`internal/pool/pool.go`:

```go
type Pool struct {
    sem  chan struct{}
    name string  // "planner" | "executor" — used in logs and metrics
}

func New(capacity int, name string) *Pool {
    return &Pool{
        sem:  make(chan struct{}, capacity),
        name: name,
    }
}

// Acquire blocks until a slot is available or ctx is cancelled.
func (p *Pool) Acquire(ctx context.Context) error {
    select {
    case p.sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

// Release frees a slot. Idempotent in the sense that over-releasing panics
// (use only on a successful Acquire's return).
func (p *Pool) Release() {
    <-p.sem
}

// PreCharge consumes slots equal to the count of live Jobs matching labelSelector.
// Called once at Manager startup per POOL-02.
func (p *Pool) PreCharge(ctx context.Context, c client.Client, labelSelector string) error {
    var jobs batchv1.JobList
    sel, err := labels.Parse(labelSelector)
    if err != nil {
        return err
    }
    if err := c.List(ctx, &jobs, &client.ListOptions{LabelSelector: sel}); err != nil {
        return err
    }
    consumed := 0
    for _, j := range jobs.Items {
        if j.Status.Active > 0 {
            select {
            case p.sem <- struct{}{}:
                consumed++
            default:
                return fmt.Errorf("pool %s capacity exceeded by pre-charge: %d live jobs > capacity %d",
                    p.name, len(jobs.Items), cap(p.sem))
            }
        }
    }
    return nil
}
```

**Phase 1 usage of the pools:** the pools are introduced in Phase 1 (POOL-01 says so) but Phase 1 does NOT call `Acquire`/`Release` anywhere — there's no dispatch. The pools are constructed in `cmd/manager/main.go`, pre-charged on startup, and passed into the Reconciler structs as fields. Phase 2 is the first to call `Acquire` (in the WaveReconciler's dispatch path).

**Why introduce them in P1 even though they're unused?** Pitfall 6 (unified pool) is a "Serious" pitfall that bakes in at scaffold time. The POOL-03 analyzer needs both `plannerPool` and `executorPool` field names to exist in the codebase to detect the cross-pool-wait pattern. Phase 1 wiring sets up the analyzer's target surface.

**Phase 1 unit tests for `pkg/pool`:**
- `TestPoolAcquireRelease` — capacity-N pool, Acquire N times, attempt N+1th (blocks); Release once, N+1th unblocks.
- `TestPoolAcquireCtxCancel` — Acquire on full pool with ctx that gets cancelled returns `ctx.Err()`.
- `TestPoolPreChargeFromZeroJobs` — empty Job list, PreCharge returns nil, capacity unchanged.
- `TestPoolPreChargeFromLiveJobs` — three active Jobs matching selector, PreCharge consumes 3 slots; subsequent Acquire on capacity-4 pool succeeds once, blocks on next.
- `TestPoolPreChargeOverflow` — five live Jobs, capacity-4 pool, PreCharge returns descriptive error.

## POOL-03 Custom Analyzer (D-D1)

### Detection target

The analyzer rejects any Go source that contains a `select` statement (or `go select`, `if-select`, etc.) waiting on both `plannerPool` and `executorPool` channel sends/receives in the same case set. The pattern that must fail:

```go
select {
case plannerPool.sem <- struct{}{}:   // VIOLATION
case executorPool.sem <- struct{}{}:  // VIOLATION
case <-ctx.Done():
    return ctx.Err()
}
```

Also forbidden: any `Acquire` call where the receiver could be either pool determined at runtime (e.g. `pickPool(spec).Acquire(ctx)` where `pickPool` returns `*Pool` chosen between the two). This is harder to detect statically; the v1 analyzer focuses on the literal `select` shape and leaves the dynamic-pick case to PR review (and Pitfall 6's `WorkerPool`-type-named smell test).

### Pass framework usage

`tools/analyzers/crosspool/analyzer.go`:

```go
package crosspool

import (
    "go/ast"
    "go/types"

    "golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
    Name: "crosspool",
    Doc:  "rejects select statements that wait on both planner and executor pools (POOL-03 / Pitfall 6 prevention)",
    Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
    for _, f := range pass.Files {
        ast.Inspect(f, func(n ast.Node) bool {
            sel, ok := n.(*ast.SelectStmt)
            if !ok {
                return true
            }
            hasPlanner := false
            hasExecutor := false
            for _, commClause := range sel.Body.List {
                cc, _ := commClause.(*ast.CommClause)
                // Match send/recv on identifiers whose object is named *Pool and
                // whose variable name contains "planner" or "executor" (case-insensitive).
                if matchPool(pass.TypesInfo, cc.Comm, "planner") {
                    hasPlanner = true
                }
                if matchPool(pass.TypesInfo, cc.Comm, "executor") {
                    hasExecutor = true
                }
            }
            if hasPlanner && hasExecutor {
                pass.Reportf(sel.Pos(), "cross-pool wait: select waits on both planner and executor pools (Pitfall 6 / POOL-03 violation)")
            }
            return true
        })
    }
    return nil, nil
}

func matchPool(info *types.Info, stmt ast.Stmt, want string) bool {
    // Implementation: walk the comm clause's expressions, find an Ident
    // whose Obj.Name contains `want` (case-insensitive) and whose Obj.Type is *pool.Pool.
}
```

### Fixture layout

`tools/analyzers/crosspool/testdata/src/`:

```
testdata/src/
├── valid/
│   ├── go.mod                # module testdata/valid
│   └── main.go               # uses ONLY plannerPool OR ONLY executorPool in a select
└── violation/
    ├── go.mod                # module testdata/violation
    └── main.go               # select waits on both pools — should flag
```

`analyzer_test.go`:

```go
package crosspool

import (
    "testing"

    "golang.org/x/tools/go/analysis/analysistest"
)

func TestCrosspool(t *testing.T) {
    testdata := analysistest.TestData()
    analysistest.Run(t, testdata, Analyzer, "violation")  // expects diagnostic
    analysistest.Run(t, testdata, Analyzer, "valid")      // expects no diagnostic
}
```

### CLI entrypoint

`cmd/tide-lint/main.go`:

```go
package main

import (
    "golang.org/x/tools/go/analysis/singlechecker"

    "github.com/jsquirrelz/tide/tools/analyzers/crosspool"
)

func main() {
    singlechecker.Main(crosspool.Analyzer)
}
```

**Why `singlechecker` not `unitchecker`:** `unitchecker` is for go-vet integration via `vettool=...`. `singlechecker` is the standalone "run this one analyzer over a module" entrypoint. The Phase 1 use case is `go run ./cmd/tide-lint ./...` from `make lint` and CI — that's `singlechecker`.

If you later want `go vet -vettool=$(which tide-lint) ./...`, switch to `multichecker` and register multiple analyzers (the DAG-05 import firewall analyzer could land alongside).

### Makefile + CI wiring

`Makefile`:

```makefile
.PHONY: lint tide-lint helm

lint: tide-lint
	go vet ./...
	golangci-lint run

tide-lint:
	go run ./cmd/tide-lint ./...

helm: manifests
	helmify charts/tide < <(kustomize build config/default)
	helmify charts/tide-crds < <(kustomize build config/crd)
```

`.github/workflows/ci.yaml`:

```yaml
name: ci
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make verify-dag-imports   # DAG-05 enforcement
      - run: make lint                  # includes tide-lint (POOL-03)
      - run: make test                  # ginkgo envtest + pkg/dag table tests
```

## Webhook Scaffolding (CRD-04, CRD-05)

`kubebuilder create webhook --kind Plan --conversion --programmatic-validation` emits:

- `internal/webhook/v1alpha1/plan_webhook.go` with empty `ValidateCreate/Update/Delete` methods and a `ConvertTo/ConvertFrom` registration
- Updates to `cmd/main.go` registering the webhook
- `config/webhook/manifests.yaml` with `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration` entries

`kubebuilder create webhook --kind Wave --programmatic-validation` similar but no `--conversion`.

### Phase 1 webhook bodies (no-op until Phase 2)

`internal/webhook/v1alpha1/plan_webhook.go`:

```go
type PlanValidator struct{}

// ValidateCreate is called on POST. Phase 1: no-op (always Allow).
// Phase 2: wires cycle detection via pkg/dag.ComputeWaves over the Plan's Tasks.
func (v *PlanValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
    return nil, nil
}

func (v *PlanValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
    return nil, nil
}

func (v *PlanValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
    return nil, nil
}

// ConvertTo/ConvertFrom are scaffold-only (no second version exists in v1).
// Phase 1: returns nil/identity. Future v1beta1 fills these.
```

`internal/webhook/v1alpha1/wave_webhook.go`:

```go
type WaveValidator struct{}

// Phase 1: no-op. Phase 2: rejects client-applied Waves per D-B1
// (Wave objects are only created by WaveReconciler).
func (v *WaveValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
    return nil, nil
}
// ... same for Update/Delete
```

### Cert handling for envtest

kubebuilder v4.14 scaffolds webhook configs with placeholder cert annotations. For envtest in Phase 1:

- envtest starts a webhook server on `localhost:9443` using auto-generated self-signed certs
- The `ValidatingWebhookConfiguration` in `config/webhook/manifests.yaml` references the cluster's CA bundle (placeholder; envtest substitutes)
- No cert-manager needed for Phase 1 (cert-manager integration lands Phase 5)

If envtest webhook setup proves fiddly, the kubebuilder docs at https://book.kubebuilder.io/cronjob-tutorial/running-webhook.html cover the local-development pattern. There's a known gotcha (https://github.com/kubernetes-sigs/kubebuilder/discussions/4855) about CA injection lines in `kustomization.yaml` — flag for Phase 1 plan authors to check kubebuilder's emitted comments carefully.

## RBAC Markers (CRD-06, AUTH-02, AUTH-03)

Every reconciler declares its RBAC markers as Go comments above `Reconcile`:

```go
// +kubebuilder:rbac:groups=tide.io,resources=projects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tide.io,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tide.io,resources=projects/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
func (r *ProjectReconciler) Reconcile(...) { ... }
```

**Minimum verb set per Kind for Phase 1:**

| Kind | Resources | Verbs | Reason |
|------|-----------|-------|--------|
| Project | `projects` | `get;list;watch;create;update;patch;delete` | Full lifecycle (orchestrator creates child Milestones) |
| Project | `projects/status` | `get;update;patch` | Status subresource (separate from spec writes) |
| Project | `projects/finalizers` | `update` | Finalizer management |
| Milestone | `milestones` | `get;list;watch;create;update;patch;delete` | Full lifecycle (creates child Phases in P2+) |
| Milestone | `milestones/status` | `get;update;patch` | — |
| Milestone | `milestones/finalizers` | `update` | — |
| (same shape for Phase, Plan, Task, Wave) |  |  |  |
| Built-in `batchv1.Job` | `jobs` | `get;list;watch;create;delete` | `Owns(&batchv1.Job{})` watch + Phase 2 dispatch |
| Built-in `corev1.Pod` | `pods` | `get;list;watch` | Status reporting |
| Built-in events | `events` | `create;patch` | Event recording |
| Coordination | `leases` | `get;list;watch;create;update;patch;delete` | Leader election (CTRL-03) |

**No `verbs=*` or `resources=*` anywhere** (Pitfall 15 / AUTH-03 prevention). PR review checklist item: grep `config/rbac/role.yaml` for `'*'` — must return zero matches in Phase 1.

**Namespace tenancy (AUTH-02):** the orchestrator runs in its own namespace (`tide-system` by Helm default) but its `Role` is generated as a `ClusterRole` by controller-gen because the reconcilers watch multiple namespaces. Phase 1 makes the ClusterRole minimum and documents the namespace-scoping in `docs/rbac.md`.

**Phase 1 explicitly does NOT need:**
- `secrets` (Phase 3 wires Secret refs for git creds + LLM keys)
- `configmaps` cluster-wide (Helm chart's ConfigMap is in the controller namespace; controller-runtime reads it as a file mount, not a K8s API call)
- `persistentvolumeclaims` (Phase 2 wires per-Project PVC)
- `customresourcedefinitions` cluster-wide (controller doesn't manage CRDs itself; Helm chart installs them)

## Helm Chart Pair (D-E1, D-E2)

### helmify workflow

kubebuilder produces `config/` (Kustomize). helmify reads Kustomize output and writes Helm templates.

`Makefile`:

```makefile
.PHONY: helm helm-controller helm-crds

helm: helm-controller helm-crds

helm-controller: manifests
	mkdir -p charts/tide
	kustomize build config/default | helmify charts/tide

helm-crds: manifests
	mkdir -p charts/tide-crds
	kustomize build config/crd | helmify charts/tide-crds
```

**Important nuance:** helmify produces one chart per invocation. To split CRDs into a subchart, you point it at `config/crd/` (CRDs only) for `charts/tide-crds/` and `config/default/` (everything else) for `charts/tide/`. This means `config/default/kustomization.yaml` must NOT include `config/crd/` if you want them in separate charts — or you customize the helmify command. Verify against helmify's docs at https://github.com/arttor/helmify.

### Helm values surface

`charts/tide/values.yaml`:

```yaml
image:
  repository: ghcr.io/jsquirrelz/tide-controller
  tag: v0.1.0-dev
  pullPolicy: IfNotPresent

plannerConcurrency: 16
executorConcurrency: 4

maxConcurrentReconciles:
  project: 1
  milestone: 1
  phase: 2
  plan: 4
  wave: 8
  task: 16

leaderElection:
  enabled: true
  namespace: ""    # defaults to release namespace
```

### ConfigMap rendering pattern

The Helm template renders these values into a ConfigMap at `templates/configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tide-controller-config
  namespace: {{ .Release.Namespace }}
data:
  config.yaml: |
    plannerConcurrency: {{ .Values.plannerConcurrency }}
    executorConcurrency: {{ .Values.executorConcurrency }}
    maxConcurrentReconciles:
      project: {{ .Values.maxConcurrentReconciles.project }}
      milestone: {{ .Values.maxConcurrentReconciles.milestone }}
      phase: {{ .Values.maxConcurrentReconciles.phase }}
      plan: {{ .Values.maxConcurrentReconciles.plan }}
      wave: {{ .Values.maxConcurrentReconciles.wave }}
      task: {{ .Values.maxConcurrentReconciles.task }}
```

The Deployment mounts this as `/etc/tide/config.yaml`:

```yaml
volumes:
  - name: tide-config
    configMap:
      name: tide-controller-config
containers:
  - name: manager
    volumeMounts:
      - name: tide-config
        mountPath: /etc/tide
        readOnly: true
    args:
      - --config=/etc/tide/config.yaml
      - --leader-elect
```

**Why ConfigMap not env vars** (re-stated for emphasis): structured nested config (the `maxConcurrentReconciles` map) doesn't render cleanly as flat env vars. ConfigMap mount + YAML parse is the idiomatic K8s path.

## Don't Hand-Roll (Phase 1 specific)

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Topological sort | Custom DAG library wrapper | stdlib `pkg/dag` — Kahn-layered in ~30 lines | Spec's argumentative weight rests on transparent algorithm |
| CRD scaffolding | Hand-written types + manifests | `kubebuilder create api` | controller-gen generates DeepCopy + CRD YAML + RBAC from markers |
| Webhook server | Custom HTTP server with cert handling | controller-runtime's `webhook.Server` registered with Manager | Lifecycle, TLS, cert injection all handled |
| Leader election | Custom Lease lock | `ctrl.Options{LeaderElection: true}` | Built-in, tested, integrates with health checks |
| Owner-ref setting | Direct `metadata.ownerReferences` writes | `controllerutil.SetControllerReference` wrapped in `EnsureOwnerRef` helper | Helper enforces same-namespace (Pitfall 23) |
| Finalizer management | Manual `metadata.finalizers` slice manipulation | `controllerutil.AddFinalizer` / `ContainsFinalizer` / `RemoveFinalizer` | Idempotent, well-tested |
| Status condition tracking | Custom condition struct | `metav1.Condition` + `meta.SetStatusCondition` | K8s convention; tooling (kubectl) renders nicely |
| Channel-based semaphore | Custom sync.Cond pattern | `chan struct{}` buffered to capacity | Spec is explicit; channel implementation is the canonical Go idiom |
| Custom analyzer Pass | Hand-rolled token walker | `golang.org/x/tools/go/analysis` Pass framework | `analysistest` fixtures, `singlechecker.Main` boilerplate |
| Kustomize → Helm conversion | Hand-maintained Helm templates | `helmify` from CNCF | Single source of truth (kubebuilder Kustomize) |
| envtest harness | Custom kube-apiserver startup | `sigs.k8s.io/controller-runtime/pkg/envtest` | Scaffolded by kubebuilder |

**Key insight:** Phase 1 is mostly "use the kubebuilder scaffolds correctly." The only genuinely novel custom code is `pkg/dag` (intentionally minimal) and the POOL-03 analyzer (one Pass, ~100 lines).

## Common Pitfalls (Phase-1 specific — eight listed by REQUIREMENTS.md traceability)

### Pitfall 1: Long-running work inside the reconcile loop

**Phase 1 prevention recipe:** No `time.Sleep`, no blocking I/O, no LLM calls in any `Reconcile()` body (D-C2). Reconcile must answer "what should the world look like now?" and return. Watches re-trigger via `Owns(&batchv1.Job{})`.

**Verification:** grep test in CI: `grep -nE 'time\.Sleep|<-time\.After|<-(context|ctx)\.Done\(\)' internal/controller/*.go` must return zero matches (or only matches inside `select` statements that also have `case <-ctx.Done()`). The POOL-03 analyzer doesn't cover this; a separate analyzer or `forbidigo` rule does.

**Warning signs in code review:** any function call from inside `Reconcile` that wraps a `time.Tick`, channel `<-`, or external HTTP/SDK call without a deadline.

### Pitfall 3: Planning DAG and Execution DAG collapse

**Phase 1 prevention recipe:** `pkg/dag` is the *only* place the algorithm lives; the two call-site wrappers (`ComputePlanningWaves`, `computeExecutionWaves`) land in Phase 2 with distinct return types. Phase 1's job: document the intent in `pkg/dag/doc.go` and resist any "unified `Graph` type" PR.

**Verification:** package doc inspection — `pkg/dag/doc.go` MUST contain the two-DAG language explicitly. Grep test: `grep -E 'type (Graph|DAG) ' pkg/dag/*.go` returns zero matches (no top-level `Graph` or `DAG` types in pkg/dag; only `NodeID`, `Edge`, `CycleError`).

### Pitfall 4: CRD `.status` as truth instead of cache

**Phase 1 prevention recipe:** `.status` schema discipline. No field named `Schedule`, `Waves`, `IndegreeMap`, `CachedDag`, `DerivedDag` on any Kind. The only "schedule-shaped" data allowed is `Wave.Status.TaskRefs` (observation — re-derived on every reconcile, idempotent).

**Verification:** PR-template checklist item + grep test in CI:

```bash
grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag' api/v1alpha1/*_types.go && exit 1 || exit 0
```

### Pitfall 6: Unified planner + executor worker pool

**Phase 1 prevention recipe:** Two distinct `*pool.Pool` fields on Reconciler structs (`PlannerPool`, `ExecutorPool`). POOL-03 analyzer + `make tide-lint` CI gate.

**Verification:** the analyzer itself + analysistest fixtures cover this. CI failure on violation.

### Pitfall 15: K8s RBAC scope creep

**Phase 1 prevention recipe:** kubebuilder RBAC markers per-controller with enumerated verbs. No `verbs=*` or `resources=*`.

**Verification:** grep test in CI:

```bash
grep -nE 'verbs="?\*"?|resources="?\*"?' config/rbac/role.yaml && exit 1 || exit 0
```

### Pitfall 16: Breaking CRD schema changes after release

**Phase 1 prevention recipe:** `v1alpha1` for everything. Conversion webhook scaffolded from day one (CRD-05). Dedicated CRD subchart (`charts/tide-crds/`) for safe `helm upgrade`. Every new field is **optional** with sensible defaults; `+kubebuilder:validation:Required` only where genuinely necessary.

**Verification:** grep `api/v1alpha1/*_types.go` for `+kubebuilder:validation:Required` — every match must be load-bearing (i.e., the field's absence would invalidate the CRD). Track required fields in a CHANGELOG-style decision log.

### Pitfall 21: Finalizer leaks

**Phase 1 prevention recipe:** the bounded-deadline finalizer recipe in `internal/finalizer/`. Documented manual unstick command.

**Verification:** unit tests for `HandleDeletion` covering deadline-exceeded, idempotent-cleanup, and successful-removal paths. Documented `kubectl patch` unstick in the runbook.

### Pitfall 23: Missing or wrong owner references

**Phase 1 prevention recipe:** `EnsureOwnerRef` helper enforces same-namespace; rejects cross-namespace. Every CRD-creates-CRD operation goes through it.

**Verification:** unit tests covering same-namespace (success), different-namespace (error), nil-parent (error), nil-child (error). Code review: grep for `SetControllerReference` and `SetOwnerReference` direct calls — must be only inside the helper.

## Validation Architecture

> Note: workflow.nyquist_validation is not set in .planning/config.json — treat as enabled. This section is required.

### Test Framework

| Property | Value |
|----------|-------|
| Framework (`pkg/dag`) | stdlib `testing` v1.26; `t.Run` table tests |
| Framework (controller suite) | `github.com/onsi/ginkgo/v2@v2.28.x` + `github.com/onsi/gomega@latest` + `sigs.k8s.io/controller-runtime/pkg/envtest` |
| Framework (analyzer) | `golang.org/x/tools/go/analysis/analysistest` |
| Framework (pool) | stdlib `testing` |
| Config file | `internal/controller/suite_test.go` (kubebuilder scaffold-generated) |
| Quick run command | `go test ./pkg/dag/... ./internal/pool/... ./tools/analyzers/...` (<5s, no envtest) |
| Full suite command | `make test` (invokes setup-envtest, runs Ginkgo + stdlib tests, ~30s budget per TEST-01) |
| Phase gate | Full suite green + `make lint` green (includes POOL-03 analyzer) before `/gsd:verify-work` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| CRD-01 | Six CRDs apply cleanly with Spec/Status separation | envtest integration | `go test ./internal/controller/... -run TestCRDsAccept` | ❌ Wave 0 — `internal/controller/suite_test.go` scaffolded by kubebuilder; assertions hand-added |
| CRD-02 | Owner-ref cascade cleans up children on parent delete | envtest integration | `go test ./internal/controller/... -run TestOwnerRefCascade` | ❌ Wave 0 |
| CRD-02 | `EnsureOwnerRef` rejects cross-namespace | unit test | `go test ./internal/owner/...` | ❌ Wave 0 — `internal/owner/owner_test.go` |
| CRD-03 | CEL validation enforces non-empty `filesTouched` | envtest integration | `go test ./internal/controller/... -run TestCELValidation` | ❌ Wave 0 |
| CRD-04 | Validating webhook scaffolded, returns Allow (no-op) | envtest integration | `go test ./internal/webhook/... -run TestPlanValidatorNoOp` | ❌ Wave 0 |
| CRD-05 | Conversion webhook scaffolded; hub/spoke registration works | envtest integration | `go test ./internal/webhook/... -run TestConversionRoundtrip` | ❌ Wave 0 |
| CRD-06 | No wildcards in generated RBAC | CI grep check | `! grep -nE 'verbs="?\*"?\|resources="?\*"?' config/rbac/role.yaml` | ❌ Wave 0 — Makefile target |
| DAG-01 | `ComputeWaves` returns waves as `[]Set[NodeID]` (impl as `[][]NodeID` sorted) | unit table test | `go test ./pkg/dag/... -run TestComputeWaves` | ❌ Wave 0 |
| DAG-02 | Cycle returns `CycleError` naming involved nodes | unit table test | `go test ./pkg/dag/... -run TestComputeWaves/Cycle.*` | ❌ Wave 0 |
| DAG-03 | Typed-apart at call sites (P1: only doc-comment + package boundary) | static doc inspection | grep for two-DAG language in `pkg/dag/doc.go` | ❌ Wave 0 |
| DAG-04 | α…θ worked example pinned | unit table test | `go test ./pkg/dag/... -run TestComputeWaves/AlphaThroughTheta` | ❌ Wave 0 |
| DAG-05 | No K8s/controller-runtime/Anthropic imports | CI dependency check | `make verify-dag-imports` (custom Makefile target) | ❌ Wave 0 |
| CTRL-01 | All six reconcilers register on the Manager | envtest integration | `go test ./internal/controller/... -run TestManagerSetup` (asserts 6 controllers registered) | ❌ Wave 0 |
| CTRL-02 | Reconcile() has no Sleep/block; `Owns(&batchv1.Job{})` set on every reconciler | static grep + unit reconcile test | `! grep -nE 'time\.Sleep' internal/controller/*.go` + envtest TestReconcileReturnsQuickly (p99 < 100ms) | ❌ Wave 0 — Makefile target + envtest assertion |
| CTRL-03 | Leader election active; failover transfers control | envtest integration | `go test ./internal/controller/... -run TestLeaderElection` (start 2 mgrs, kill leader, assert other takes lease) | ❌ Wave 0 |
| CTRL-04 | `MaxConcurrentReconciles` tunable from config | unit + envtest | `go test ./internal/config/... -run TestConfigLoad` + envtest TestMaxConcurrentReconcilesHonored | ❌ Wave 0 — `internal/config/config_test.go` |
| CTRL-05 | Finalizer with bounded deadline + idempotent cleanup | unit test | `go test ./internal/finalizer/...` | ❌ Wave 0 — `internal/finalizer/finalizer_test.go` |
| CTRL-05 | Finalizer cleanup runs on deletion in envtest | envtest integration | `go test ./internal/controller/... -run TestFinalizerLifecycle` | ❌ Wave 0 |
| POOL-01 | Two `chan struct{}` semaphores with capacities from config | unit test | `go test ./internal/pool/... -run TestPoolAcquireRelease` | ❌ Wave 0 — `internal/pool/pool_test.go` |
| POOL-02 | Pre-charge from live Jobs on restart | unit test (fake client) | `go test ./internal/pool/... -run TestPoolPreCharge.*` | ❌ Wave 0 |
| POOL-03 | Custom analyzer rejects cross-pool waits | analysistest | `go test ./tools/analyzers/crosspool/...` | ❌ Wave 0 — `tools/analyzers/crosspool/analyzer_test.go` |
| POOL-03 | `make tide-lint` returns non-zero on violation | CI gate | `make tide-lint` (invokes `go run ./cmd/tide-lint ./...`) | ❌ Wave 0 — Makefile target + GitHub Action |
| AUTH-02 | Namespace-scoped RBAC documented; no cluster-wildcards | CI grep check | `! grep -nE 'verbs="?\*"?\|resources="?\*"?' config/rbac/role.yaml` | ❌ Wave 0 |
| AUTH-03 | Orchestrator ServiceAccount has no cluster-wide wildcards | CI grep check + manual inspect | `! grep "ClusterRole" config/rbac/ \| grep -v "leases"` (only leader-election ClusterRole is allowed) | ❌ Wave 0 |
| PERSIST-01 | No DB or SQLite dependency in `go.mod` | CI dependency check | `! grep -nE 'database/sql\|github.com/mattn/go-sqlite3\|gorm.io' go.mod` | ❌ Wave 0 — Makefile target |
| PERSIST-02 | No `Status.Waves` / `Status.Schedule` / `IndegreeMap` on any CRD | CI grep check | `! grep -nE 'Schedule\|Waves *\[\]\|IndegreeMap' api/v1alpha1/*_types.go` | ❌ Wave 0 — Makefile target |
| BOOT-01 | M0 commitment is in `.planning/ROADMAP.md` | manual checklist | grep for "M0" in ROADMAP.md | ✅ Exists |
| BOOT-03 | Single `v1alpha1` group/version on all six CRDs | static check | `grep -E '^// \+kubebuilder:.*v1alpha1' api/v1alpha1/groupversion_info.go` returns exactly one match | ❌ Wave 0 |
| TEST-01 | Test suite runs in <30s on CI | CI timing assertion | `time make test` — fail if > 30s | ❌ Wave 0 — CI workflow step |

### Sampling Rate

- **Per task commit:** `go test ./pkg/dag/... ./internal/pool/... ./internal/owner/... ./internal/finalizer/... ./tools/analyzers/...` (under 5s, no envtest startup)
- **Per wave merge:** `make test` (full envtest suite, ~25s; under 30s budget per TEST-01)
- **Phase gate:** `make lint && make test && make verify-dag-imports && make tide-lint` all green before `/gsd:verify-work`

### Wave 0 Gaps

The following files don't yet exist and need to be created in Wave 0 of Phase 1's plan:

- [ ] `pkg/dag/kahn.go` — Kahn-layered impl
- [ ] `pkg/dag/kahn_test.go` — α…θ fixture + 8 other table-test cases (DAG-04)
- [ ] `pkg/dag/doc.go` — two-DAG language for the package boundary
- [ ] `api/v1alpha1/*_types.go` — six type files with Spec/Status + CEL markers
- [ ] `api/v1alpha1/shared_types.go` — Status condition vocabulary
- [ ] `internal/controller/*_controller.go` — six reconciler stubs
- [ ] `internal/controller/suite_test.go` — Ginkgo+envtest TestMain (kubebuilder scaffolds the skeleton; assertions hand-added)
- [ ] `internal/owner/owner.go` + `owner_test.go` — `EnsureOwnerRef` helper
- [ ] `internal/finalizer/finalizer.go` + `finalizer_test.go` — `HandleDeletion` recipe
- [ ] `internal/pool/pool.go` + `pool_test.go` — semaphore + PreCharge
- [ ] `internal/config/config.go` + `config_test.go` — runtime config loader
- [ ] `internal/webhook/v1alpha1/plan_webhook.go` — no-op validator + conversion stubs
- [ ] `internal/webhook/v1alpha1/wave_webhook.go` — no-op validator
- [ ] `cmd/manager/main.go` — Manager wiring (replaces kubebuilder default)
- [ ] `cmd/tide-lint/main.go` — singlechecker entrypoint
- [ ] `tools/analyzers/crosspool/analyzer.go` + `analyzer_test.go` — Pass + analysistest
- [ ] `tools/analyzers/crosspool/testdata/src/valid/main.go` + `testdata/src/violation/main.go` — fixtures
- [ ] `config/samples/kustomization.yaml` — orders α…θ apply for owner-ref cascade
- [ ] `config/samples/tide_v1alpha1_*.yaml` — Project, Milestone, Phase, Plan, 8 Tasks (α…θ)
- [ ] `Makefile` targets: `helm`, `helm-controller`, `helm-crds`, `lint`, `tide-lint`, `verify-dag-imports`, `verify-no-aggregates`, `verify-no-rbac-wildcards`
- [ ] `.github/workflows/ci.yaml` — go test + lint + tide-lint + grep checks
- [ ] `charts/tide/` — helmify output for controller chart
- [ ] `charts/tide-crds/` — helmify output for CRD subchart
- [ ] `charts/tide/values.yaml` — exposed config surface
- [ ] Framework installs: `kubebuilder init` + `kubebuilder create api` + `kubebuilder create webhook` (one-time at Wave 0 start)

## Phase 1 ↔ Phase 2 Boundary

For each "stubbed for Phase 2" item in CONTEXT.md, the Phase 1 placeholder that makes Phase 2 a body-fill rather than a refactor:

| Phase 2 work | Phase 1 placeholder |
|--------------|---------------------|
| `pkg/dispatch.Subagent` interface design (REQ-SUB-01) | `pkg/dispatch/doc.go` reserves package; `type Dispatcher interface{}` (empty, to be replaced) |
| `Dispatcher` field call site in reconcilers | `r.Dispatcher` field declared (nil in P1); `Reconcile` body has `if r.Dispatcher != nil { /* Phase 2 fills */ }` guard |
| Cycle detection in Plan webhook (REQ-PLAN-01) | `PlanValidator.ValidateCreate` returns `nil, nil` (always Allow) — Phase 2 calls `pkg/dag.ComputeWaves` here |
| Wave webhook rejects client-applies (D-B1) | `WaveValidator.ValidateCreate` returns `nil, nil` — Phase 2 adds `return nil, fmt.Errorf(...)` |
| File-touch reconciliation (REQ-PLAN-02) | `Task.Spec.FilesTouched []string` with `MinItems: 1` CEL — Phase 2 wires consumption in Plan webhook |
| Per-Task semaphore acquire/release | `r.PlannerPool`/`r.ExecutorPool` fields populated from `cmd/main.go`; no callsite yet in Reconcile bodies |
| Subagent dispatch path | `Reconcile` body has `// Phase 2: r.Dispatcher.Run(ctx, ...)` comment marker |

## Code Examples

### `pkg/dag/kahn.go` (full reference impl, ~50 lines stdlib)

```go
// Package dag implements Kahn's algorithm in layered form for the TIDE orchestrator.
// See doc.go for the two-DAG application context.
package dag

import (
    "fmt"
    "sort"
)

type NodeID = string

type Edge struct {
    From NodeID
    To   NodeID
}

type CycleError struct {
    InvolvedNodes []NodeID
}

func (e *CycleError) Error() string {
    return fmt.Sprintf("cyclic DAG: nodes with unresolvable indegrees: %v", e.InvolvedNodes)
}

// ComputeWaves returns the layered topological sort of (nodes, edges).
// Each returned wave is sorted lexicographically for determinism.
// Returns *CycleError if the graph contains a cycle.
// Complexity: O(V + E).
func ComputeWaves(nodes []NodeID, edges []Edge) ([][]NodeID, error) {
    indegree := make(map[NodeID]int, len(nodes))
    nodeSet := make(map[NodeID]struct{}, len(nodes))
    for _, n := range nodes {
        indegree[n] = 0
        nodeSet[n] = struct{}{}
    }

    succ := make(map[NodeID][]NodeID)
    for _, e := range edges {
        if _, ok := nodeSet[e.From]; !ok {
            return nil, fmt.Errorf("edge references unknown node: %s", e.From)
        }
        if _, ok := nodeSet[e.To]; !ok {
            return nil, fmt.Errorf("edge references unknown node: %s", e.To)
        }
        indegree[e.To]++
        succ[e.From] = append(succ[e.From], e.To)
    }

    var waves [][]NodeID
    remaining := make(map[NodeID]struct{}, len(nodes))
    for _, n := range nodes {
        remaining[n] = struct{}{}
    }

    for len(remaining) > 0 {
        var current []NodeID
        for id := range remaining {
            if indegree[id] == 0 {
                current = append(current, id)
            }
        }
        if len(current) == 0 {
            involved := make([]NodeID, 0, len(remaining))
            for id := range remaining {
                involved = append(involved, id)
            }
            sort.Strings(involved)
            return nil, &CycleError{InvolvedNodes: involved}
        }
        sort.Strings(current)
        waves = append(waves, current)
        for _, id := range current {
            delete(remaining, id)
            for _, s := range succ[id] {
                indegree[s]--
            }
        }
    }
    return waves, nil
}
```

Source: synthesized from ARCHITECTURE.md Pattern 3 example + the spec's pseudocode + standard layered-Kahn idioms.

### α…θ regression fixture (DAG-04)

```go
// pkg/dag/kahn_test.go

func TestComputeWaves_AlphaThroughTheta(t *testing.T) {
    nodes := []NodeID{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
    edges := []Edge{
        {From: "alpha", To: "delta"},
        {From: "beta", To: "delta"},
        {From: "gamma", To: "eta"},
        {From: "zeta", To: "eta"},
        {From: "delta", To: "epsilon"},
        {From: "eta", To: "theta"},
    }

    want := [][]NodeID{
        {"alpha", "beta", "gamma", "zeta"},
        {"delta", "eta"},
        {"epsilon", "theta"},
    }

    got, err := ComputeWaves(nodes, edges)
    if err != nil {
        t.Fatalf("ComputeWaves returned unexpected error: %v", err)
    }
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("waves mismatch\n got: %v\nwant: %v", got, want)
    }
}
```

Source: matches the spec's worked example in `README.md` Wave Computation section.

### `config/samples/` α…θ Task fixture (D-G1)

```yaml
# config/samples/tide_v1alpha1_task_delta.yaml
apiVersion: tide.io/v1alpha1
kind: Task
metadata:
  name: delta
  namespace: tide-samples
  labels:
    tide.io/plan: sample-plan
spec:
  planRef: sample-plan
  dependsOn:
    - alpha
    - beta
  filesTouched:
    - pkg/example/delta.go
```

Eight similar files for α through θ, with `dependsOn` matching the spec edge set:
- alpha, beta, gamma, zeta → no deps (wave 1)
- delta → [alpha, beta]; eta → [gamma, zeta] (wave 2)
- epsilon → [delta]; theta → [eta] (wave 3)

`config/samples/kustomization.yaml` orders the apply so Project lands first, then Milestone, Phase, Plan, then all eight Tasks (kustomize doesn't enforce order for `kubectl apply -k`, but a top-down resource list approximates owner-ref dependency order).

## State of the Art

Phase 1 uses 2026-current versions of foundational tooling. No deprecated approaches at risk of being chosen:

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Hand-rolled `client-go` + informers + workqueue | `sigs.k8s.io/controller-runtime` Manager + reconcilers | K8s 1.10 era → now | Standard; kubebuilder scaffolds this |
| One controller per Manager | One Manager, multiple reconcilers (one per Kind) | controller-runtime v0.6+ | Pattern 1 |
| Validating admission via webhook for everything | CEL `x-kubernetes-validations` for what CEL handles; webhooks for cross-object | K8s 1.29 (GA) | Convergence #1 — CEL for non-graph invariants, webhook for cycles only |
| `kubectl-style` ad-hoc CRD schema edits | Conversion webhook from day one + alpha versioning | After first wave of CRD-upgrade-pain reports | Pitfall 16 prevention |
| SPDY for `kubectl logs` and `exec` | WebSocket transport | K8s 1.31 (Aug 2024) | Phase 4 dashboard impact only |
| One subagent image per role (planner vs executor) | One image, role/level flags | TIDE design choice | Pattern 5 — Phase 2 work; Phase 1 reserves the namespace |
| `Gonum` / `dominikbraun/graph` for layered Kahn | stdlib (30 lines) | Project research convergence | Spec readability + zero dep |

**No deprecated/outdated patterns in Phase 1's scope.**

## Open Questions

Nothing critical. The genuine uncertainties are noted as Claude's Discretion in CONTEXT.md and the user has explicitly delegated them. None block planning.

Minor flag: **kubebuilder v4.14 + controller-runtime version match.** kubebuilder v4.14.0 scaffolds with controller-runtime v0.23.3 (per CLAUDE.md). The next kubebuilder release should bump to v0.24.x. Two recommended paths:
1. Scaffold with v4.14 (gets v0.23.3), then `go get sigs.k8s.io/controller-runtime@v0.24.1` to upgrade in place. Test that `kubebuilder create webhook` still works after upgrade.
2. Wait for the next kubebuilder 4.x release that pairs with v0.24.x natively.

**Recommendation:** Option 1 — kubebuilder's scaffold output is mostly version-neutral; the controller-runtime bump is a Makefile + go.mod edit. Source: kubebuilder release notes don't gate webhook scaffolding on the cr version.

## Sources

### Primary (HIGH confidence — already loaded in project research)

- `.planning/research/SUMMARY.md` — Convergence table (Phase 1 contracts), divergence #1 (CEL+webhook split), #6 (Wave as own Kind), #9 (v1alpha1+conversion-webhook scaffold), Build order rationale
- `.planning/research/ARCHITECTURE.md` — Patterns 1-4 (one reconciler per Kind, owner-ref cascade, two-DAG one-algorithm, two parallelism budgets), Recommended Project Structure, Anti-Patterns 1-3+7
- `.planning/research/PITFALLS.md` — P1 (long-running reconcile), P3 (DAG unification), P4 (status-as-truth), P6 (unified pool), P15 (RBAC scope creep), P16 (breaking CRDs), P21 (finalizer leaks), P23 (wrong owner refs)
- `.planning/research/STACK.md` — Pinned versions: Go 1.26, controller-runtime v0.24.x, kubebuilder v4.14.0, K8s 1.33+, Ginkgo v2.28, zap v1.28, helmify
- `CLAUDE.md` — Technology Stack table (verbatim source for versions), "What NOT to Use" (Gonum, gRPC, external DB, wildcards), Stack Patterns by Variant
- `.planning/REQUIREMENTS.md` — 26 REQ-IDs in Phase 1's coverage; traceability table
- `.planning/ROADMAP.md` §"Phase 1: Foundation" — Goal, dependencies, five success criteria
- `README.md` — TIDE spec (worked example α…θ; failure semantics; two-DAG distinction; algorithm pseudocode)
- `.planning/phases/01-foundation-crds-pkg-dag-controller-scaffold/01-CONTEXT.md` — Locked user decisions (D-A1, D-A2, D-B1..B3, D-C1..C2, D-D1, D-E1..E2, D-F1..F2, D-G1..G2)

### Secondary (MEDIUM confidence — Phase-1-specific external references)

- Kubebuilder Book §Webhook implementation — https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html — `kubebuilder create webhook` flags `--conversion`, `--defaulting`, `--programmatic-validation` confirmed (MEDIUM — verified via web search May 2026; not pinned to v4.14 docs explicitly)
- Kubebuilder discussion on conversion webhook scaffolding — https://github.com/kubernetes-sigs/kubebuilder/discussions/4855 — CA injection lines gotcha for `kustomization.yaml`
- `golang.org/x/tools/go/analysis` package docs — Pass framework, `analysistest.Run`, `singlechecker.Main` (HIGH — standard Go library)
- helmify README — https://github.com/arttor/helmify — Kustomize → Helm conversion (cited from project STACK.md)
- Kubernetes RBAC good practices — https://kubernetes.io/docs/concepts/security/rbac-good-practices/ — enumerate verbs, no wildcards (cited from PITFALLS.md P15)
- Kubernetes Finalizers — https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/ — `kubectl patch` unstick recipe (cited from PITFALLS.md P21)
- Kubernetes CRD Versioning — https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/ — conversion webhook patterns (cited from PITFALLS.md P16)

### Tertiary (LOW confidence — flag for verification at execution time)

- None. All Phase-1-specific guidance traces to either the locked CONTEXT.md decisions, the project-level research synthesis, or kubebuilder/controller-runtime official docs.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions are project-level locked, no Phase 1 deviation
- Architecture patterns: HIGH — ARCHITECTURE.md Patterns 1-4 directly apply
- CRD shape: HIGH — schema derived from locked user decisions + spec
- Pitfall prevention: HIGH — eight Phase-1-mapped pitfalls each have explicit prevention recipe + verification step
- Webhook scaffolding mechanics: MEDIUM — kubebuilder v4.14 flag combinations confirmed but not exhaustively tested in this research pass; plan authors should verify against scaffold output
- POOL-03 analyzer detection scope: MEDIUM — the literal-`select` AST shape is straightforward; the dynamic-pool-pick case (`pickPool(spec).Acquire(...)`) is explicitly out of scope for the v1 analyzer (mitigated by PR review + `WorkerPool`-name smell test)

**Research date:** 2026-05-12
**Valid until:** 2026-06-12 (kubebuilder + controller-runtime release cadence is monthly; re-verify if execution slips past this date)
