# Phase 28: Plan-Import Core - Pattern Map

**Mapped:** 2026-06-18
**Files analyzed:** 11 new/modified files
**Analogs found:** 11 / 11

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `api/v1alpha2/import_types.go` | model | transform | `api/v1alpha2/project_types.go` (GitConfig struct pattern) | role-match |
| `api/v1alpha2/project_types.go` (add ImportSource field) | model | transform | `api/v1alpha2/project_types.go` (FailureProfile field + Git *GitConfig optional pointer) | exact |
| `api/v1alpha2/shared_types.go` (add constants) | config | — | `api/v1alpha2/shared_types.go` lines 206–261 (ConditionBillingHalt block) | exact |
| `internal/controller/import_controller.go` | controller | request-response | `internal/controller/project_controller.go` (ProjectReconciler — init Job state machine) | exact |
| `cmd/tide-import/main.go` | utility | file-I/O | `cmd/tide-reporter/main.go` | exact |
| `images/tide-import/Dockerfile` | config | — | `images/tide-reporter/Dockerfile` | exact |
| `charts/tide/values.yaml` (add tideImport block) | config | — | `charts/tide/values.yaml` lines 192–195 (tideReporter block) | exact |
| `charts/tide/templates/deployment.yaml` (add TIDE_IMPORT_IMAGE) | config | — | `charts/tide/templates/deployment.yaml` lines 98–99 (TIDE_REPORTER_IMAGE) | exact |
| `cmd/manager/main.go` (wire ImportController) | config | — | `cmd/manager/main.go` lines 197–203 + 394–424 (reporterImage + controller wiring) | exact |
| `internal/controller/{project,milestone,phase,plan,task}_controller.go` (guard) | middleware | request-response | `internal/controller/project_controller.go` lines 1050–1069 (checkBillingHalt guard before pool acquire) | exact |
| `internal/controller/import_controller_test.go` + `cmd/tide-import/main_test.go` | test | — | Existing envtest suites in `internal/controller/` | role-match |

---

## Pattern Assignments

### `api/v1alpha2/import_types.go` (model, transform)

**Analog:** `api/v1alpha2/project_types.go` — `GitConfig` struct (pointer, `+optional`, `+kubebuilder:validation:MinLength`)

**Struct declaration pattern** (project_types.go ~387–394):
```go
// Git declares the per-Project target repo + creds for artifact push
// (Phase 3 D-B6). Required for any Project whose lifecycle reaches push;
// optional in v1.0 only for purely transient/test Projects.
// Pointer so omitempty fully elides the field when absent.
// +optional
Git *GitConfig `json:"git,omitempty"`
```

**New type to create** (mirror GitConfig's field style):
```go
// ImportSourceRef declares the envelope salvage source for this Project.
// When set, ImportController drives the one-shot import state machine
// before any planner dispatch fires (Phase 28 D-02).
type ImportSourceRef struct {
    // SeedManifestConfigMap names a ConfigMap in the project namespace
    // carrying the seed manifest JSON (FQ-name → oldUID + initial status).
    // +kubebuilder:validation:MinLength=1
    SeedManifestConfigMap string `json:"seedManifestConfigMap"`

    // SalvagedPVCSubPath is the sub-path within the shared tide-projects PVC
    // where the salvaged envelopes reside, e.g. "<oldProjectUID>/workspace".
    // +kubebuilder:validation:MinLength=1
    SalvagedPVCSubPath string `json:"salvagedPVCSubPath"`
}
```

---

### `api/v1alpha2/project_types.go` — add ImportSource field (model)

**Analog:** `api/v1alpha2/project_types.go` — the `FailureProfile` and `Git` fields at lines 394–406

**Field addition pattern** — append after `FailureProfile` at line 405:
```go
// FailureProfile controls how a task execution failure affects non-dependent
// work in later waves. ...
// +optional
FailureProfile FailureProfileType `json:"failureProfile,omitempty"`

// ImportSource declares envelope salvage source for this Project.
// When non-nil, ImportController runs the one-shot UID-rewrite import
// state machine and all five planner-dispatch sites park until
// ImportComplete=True (Phase 28 D-01/D-02).
// +optional
ImportSource *ImportSourceRef `json:"importSource,omitempty"`
```

---

### `api/v1alpha2/shared_types.go` — add condition/reason constants (config)

**Analog:** `api/v1alpha2/shared_types.go` lines 206–261 — the BillingHalt, BudgetBlocked, and FailureHalt `const` blocks

**Exact block pattern to mirror** (lines 206–223):
```go
// Phase 13 condition + reason vocabulary — provider billing halt (HALT-01).
const (
    // ConditionBillingHalt — provider returned a credit-exhaustion 400; ...
    ConditionBillingHalt = "BillingHalt"
    // ReasonCreditBalanceTooLow — ...
    ReasonCreditBalanceTooLow = "CreditBalanceTooLow"
    // AnnotationBillingResumedAt — RFC3339 timestamp stamped by `tide resume`
    // when clearing the BillingHalt condition. ...
    AnnotationBillingResumedAt = "tideproject.k8s/billing-resumed-at"
)
```

**New block to add** (immediately after the FailureHalt block ending at line 261):
```go
// Phase 28 condition + reason vocabulary — envelope import (IMPORT-01..05).
const (
    // ConditionImportComplete — the one-shot UID-rewrite import Job has
    // completed successfully; planner-dispatch holds clear at all 5 sites.
    // Set by ImportController on Project.Status.Conditions.
    ConditionImportComplete = "ImportComplete"

    // ReasonImportSucceeded — tide-import Job exited 0; all envelopes copied
    // and schema-converted to new-UID paths.
    ReasonImportSucceeded = "ImportSucceeded"

    // ReasonImportFailed — tide-import Job exited non-zero, or envelope
    // validation failed (ChildCount mismatch, Kind not allowlisted, cycle
    // detected). Operator must investigate and optionally apply AnnotationRetryImport.
    ReasonImportFailed = "ImportFailed"

    // AnnotationRetryImport — applied by operator to trigger an import retry
    // after ImportFailed; consumed by ImportController to reset import state.
    // Mirrors AnnotationBillingResumedAt pattern.
    AnnotationRetryImport = "tideproject.k8s/retry-import"
)
```

---

### `internal/controller/import_controller.go` (controller, request-response)

**Analog:** `internal/controller/project_controller.go` — the entire file serves as the structural analog (Reconcile fetch/delete/finalizer/dispatch pattern + buildInitJob for the Job builder + SetupWithManager)

**Reconciler struct pattern** (project_controller.go lines 150–200):
```go
type ProjectReconciler struct {
    client.Client
    Scheme *runtime.Scheme

    MaxConcurrentReconciles int
    WatchNamespace          string
    // ... image fields, pool fields
}
```

**New struct** (mirrors the shape; import controller is simpler — no pool, no dispatcher):
```go
type ImportReconciler struct {
    client.Client
    Scheme *runtime.Scheme

    MaxConcurrentReconciles int
    WatchNamespace          string

    // ImportImage is the image ref for the tide-import Job.
    // When empty, import Job creation is skipped (mirrors ReporterImage skip).
    ImportImage string

    // SharedPVCName is the cluster-wide PVC name (default "tide-projects").
    SharedPVCName string
}
```

**Reconcile fast-path + idempotency pattern** (RESEARCH.md Pattern 1, modeled on project_controller.go lines 212–302):
```go
func (r *ImportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var project tidev1alpha2.Project
    if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    // Fast path: only act on Projects with importSource declared.
    if project.Spec.ImportSource == nil {
        return ctrl.Result{}, nil
    }
    // Idempotency guard: already complete (D-12).
    c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha2.ConditionImportComplete)
    if c != nil && c.Status == metav1.ConditionTrue {
        return ctrl.Result{}, nil
    }
    // ... state machine dispatch ...
}
```

**Condition-set pattern** (project_controller.go uses `meta.SetStatusCondition` + `r.Status().Patch` throughout, e.g. lines 265–275):
```go
statusPatch := client.MergeFrom(ms.DeepCopy())
meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha2.ConditionWaveOrLevelPaused,
    Status:             metav1.ConditionFalse,
    Reason:             tideprojectv1alpha2.ReasonApprovedByUser,
    Message:            "Milestone approved; children will dispatch",
    LastTransitionTime: metav1.Now(),
})
if err := r.Status().Patch(ctx, ms, statusPatch); err != nil {
    return ctrl.Result{}, err
}
```

**AlreadyExists-is-success CR creation pattern** (`internal/reporter/materialize.go` lines 297–302):
```go
if err := c.Create(ctx, obj); err != nil {
    if !apierrors.IsAlreadyExists(err) {
        return fmt.Errorf("MaterializeChildCRDs: create %s/%s: %w", child.Kind, child.Name, err)
    }
    // AlreadyExists: idempotent success (SUB-03 / Pitfall F).
}
```

**Import Job builder** (model on `buildInitJob` at project_controller.go lines 1404–1476 AND `BuildReporterJob` at reporter_jobspec.go lines 121–224 for the dual-subPath PVC mount; the import Job requires TWO PVC mounts — old-workspace read-only + new-workspace read-write):

```go
// Two-subPath PVC mount pattern (mirrors reporter_jobspec.go line 205 SubPath pattern):
Volumes: []corev1.Volume{{
    Name: "tide-projects",
    VolumeSource: corev1.VolumeSource{
        PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
            ClaimName: resolvedPVCName,
        },
    },
}},
Containers: []corev1.Container{{
    Name:  "tide-import",
    Image: opts.ImportImage,
    VolumeMounts: []corev1.VolumeMount{
        {
            Name:      "tide-projects",
            MountPath: "/old-workspace",
            SubPath:   fmt.Sprintf("%s/workspace", oldProjectUID), // read-only
            ReadOnly:  true,
        },
        {
            Name:      "tide-projects",
            MountPath: "/new-workspace",
            SubPath:   fmt.Sprintf("%s/workspace", string(project.UID)), // read-write
        },
    },
}},
```

Security context pattern (reporter_jobspec.go lines 156–162 — mirror exactly):
```go
runAsNonRoot := true
allowPrivEsc := false
sc := &corev1.SecurityContext{
    RunAsNonRoot:             &runAsNonRoot,
    AllowPrivilegeEscalation: &allowPrivEsc,
    Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
}
```

**SetupWithManager pattern** (project_controller.go lines 1914–1947):
```go
func (r *ImportReconciler) SetupWithManager(mgr ctrl.Manager) error {
    nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
        if r.WatchNamespace == "" {
            return true
        }
        return obj.GetNamespace() == r.WatchNamespace
    })
    return ctrl.NewControllerManagedBy(mgr).
        For(&tidev1alpha2.Project{},
            builder.WithPredicates(predicate.Or(
                predicate.GenerationChangedPredicate{},
                predicate.AnnotationChangedPredicate{},
            )),
        ).
        Owns(&batchv1.Job{}).
        WithEventFilter(nsPred).
        WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
        Named("import").
        Complete(r)
}
```

**RBAC markers** (mirror project_controller.go lines 202–209 — add configmaps since seed is a ConfigMap):
```go
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones;phases;plans,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
```

---

### `cmd/tide-import/main.go` (utility, file-I/O)

**Analog:** `cmd/tide-reporter/main.go` (entire file, 248 lines)

**Binary skeleton** — copy the full structure from tide-reporter:

Package doc + exit code constants (tide-reporter lines 17–76):
```go
// Command tide-import is the in-namespace envelope-rekey Job binary (Phase 28).
//
// Reads a rekey table (fqName→oldUID, fqName→newUID pairs) from stdin as JSON.
// For each pair: copies envelope files from /old-workspace/envelopes/<oldUID>/
// to /new-workspace/envelopes/<newUID>/ using cp -n semantics (no-clobber).
// Rewrites out.json.taskUID atomically to newUID. Runs schema conversion
// (json.Unmarshal→json.Marshal through typed v1alpha2 structs) on child Spec.Raw.
//
// Exit-code map:
//  0 — success: all envelopes copied and converted
//  1 — generic failure (I/O error, unmarshal error)
//  2 — invariant violation (bad stdin JSON, path traversal, Kind not in allowlist)
package main
```

Flag parsing + config struct (tide-reporter lines 58–107 — no K8s client needed for tide-import; stdin replaces flags for the rekey table):
```go
type importConfig struct {
    OldWorkspace string // "/old-workspace"
    NewWorkspace string // "/new-workspace"
}

func main() {
    fs := flag.NewFlagSet("tide-import", flag.ExitOnError)
    oldWorkspace := fs.String("old-workspace", "/old-workspace", "mount point for salvaged PVC subPath")
    newWorkspace := fs.String("new-workspace", "/new-workspace", "mount point for new project PVC subPath")
    if err := fs.Parse(os.Args[1:]); err != nil {
        fmt.Fprintf(os.Stderr, "tide-import: flag parse: %v\n", err)
        os.Exit(exitInvariant)
    }
    // ... ctx, signal, os.Exit(run(...))
}
```

No-clobber copy + atomic rewrite pattern (RESEARCH.md Pattern 3; inlined directly):
```go
func copyFileNoClobber(dst, src string) error {
    if _, err := os.Stat(dst); err == nil {
        return nil // destination exists; skip (cp -n behavior)
    }
    data, err := os.ReadFile(src)
    if err != nil {
        return err
    }
    tmp := dst + ".tmp"
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, dst) // atomic on Linux ext4/tmpfs
}

func rewriteTaskUID(outPath, newUID string) error {
    data, err := os.ReadFile(outPath)
    if err != nil {
        return err
    }
    var out pkgdispatch.EnvelopeOut
    if err := json.Unmarshal(data, &out); err != nil {
        return err
    }
    if out.TaskUID == newUID {
        return nil // already correct; no-op
    }
    out.TaskUID = newUID
    newData, err := json.Marshal(out)
    if err != nil {
        return err
    }
    tmp := outPath + ".tmp"
    if err := os.WriteFile(tmp, newData, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, outPath)
}
```

Schema conversion (RESEARCH.md Pattern 4; uses ONLY `encoding/json` + `api/v1alpha2`):
```go
func convertSpecRaw(kind string, rawBytes json.RawMessage) (json.RawMessage, error) {
    switch kind {
    case "Milestone":
        var spec tidev1alpha2.MilestoneSpec
        if err := json.Unmarshal(rawBytes, &spec); err != nil {
            return nil, fmt.Errorf("unmarshal Milestone spec: %w", err)
        }
        return json.Marshal(spec)
    case "Phase":
        var spec tidev1alpha2.PhaseSpec
        // ...
    case "Plan":
        var spec tidev1alpha2.PlanSpec
        // ...
    case "Task":
        var spec tidev1alpha2.TaskSpec
        // ...
    default:
        return nil, fmt.Errorf("unsupported Kind %q in import conversion", kind)
    }
}
```

**Imports block** (no K8s client — pure stdlib + internal; differs from tide-reporter which needs K8s client):
```go
import (
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "os"
    "os/signal"
    "path/filepath"
    "syscall"

    tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
    pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)
```

---

### `images/tide-import/Dockerfile` (config)

**Analog:** `images/tide-reporter/Dockerfile` (entire file, 44 lines) — copy exactly, adjusting only the binary name and COPY paths

**Full pattern** (tide-reporter Dockerfile lines 1–44):
```dockerfile
# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:a6a091... AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# tide-import imports: api/v1alpha2, pkg/dispatch, cmd/tide-import.
# No internal/reporter or internal/owner (pure filesystem I/O, no K8s client).
COPY api/ api/
COPY pkg/dispatch/ pkg/dispatch/
COPY cmd/tide-import/ cmd/tide-import/

RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/tide-import \
      ./cmd/tide-import

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c...
COPY --from=builder /out/tide-import /usr/local/bin/tide-import
ENTRYPOINT ["/usr/local/bin/tide-import"]
```

**Key difference from tide-reporter:** COPY block includes `api/` and `pkg/dispatch/` but NOT `internal/reporter/` or `internal/owner/` — tide-import is pure filesystem I/O, no K8s client.

---

### `charts/tide/values.yaml` — add tideImport block (config)

**Analog:** `charts/tide/values.yaml` lines 186–195 (tideReporter block)

**Exact pattern to mirror** (lines 192–195):
```yaml
  tideReporter:
    repository: ghcr.io/jsquirrelz/tide-reporter
    tag: ""
    pullPolicy: IfNotPresent
```

**New block to add** (immediately after tideReporter, before the subagent.defaults comment at line 197):
```yaml
  # Phase 28 (IMPORT-01): in-namespace UID-rewrite import Job image.
  # Wired to ImportController via TIDE_IMPORT_IMAGE env var on the manager Deployment.
  # When empty, ImportController skips Job creation (mirrors tideReporter empty-skip).
  tideImport:
    repository: ghcr.io/jsquirrelz/tide-import
    tag: ""
    pullPolicy: IfNotPresent
```

**CAUTION:** `values.yaml` is the FIXED CONTRACT per CLAUDE.md — add this block first, before implementing binary or Dockerfile.

---

### `charts/tide/templates/deployment.yaml` — add TIDE_IMPORT_IMAGE (config)

**Analog:** `charts/tide/templates/deployment.yaml` lines 98–100

**Exact pattern to mirror** (lines 98–99):
```yaml
        - name: TIDE_REPORTER_IMAGE
          value: "{{ .Values.images.tideReporter.repository }}:{{ .Values.images.tideReporter.tag | default .Chart.AppVersion }}"
        # phase9-reporter-env-injected
```

**New env var to add** (immediately after the reporter block, replacing/extending the `phase9-reporter-env-injected` sentinel):
```yaml
        - name: TIDE_IMPORT_IMAGE
          value: "{{ .Values.images.tideImport.repository }}:{{ .Values.images.tideImport.tag | default .Chart.AppVersion }}"
        # phase28-import-env-injected
```

---

### `cmd/manager/main.go` — wire ImportController (config)

**Analog:** `cmd/manager/main.go` lines 197–203 (reporterImage env read) and lines 397–424 (ProjectReconciler wiring)

**Env read pattern** (lines 197–203):
```go
// TIDE_REPORTER_IMAGE → four reconcilers' ReporterImage field (Phase 09 plan 09-06).
// ...
reporterImage := envOrDefault("TIDE_REPORTER_IMAGE", "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev")
```

**New env read to add** (immediately after reporterImage line):
```go
// TIDE_IMPORT_IMAGE → ImportController's ImportImage field (Phase 28).
// When empty, ImportController skips Job creation (mirrors TIDE_REPORTER_IMAGE skip).
// PROD_OVERRIDE_REQUIRED: dev default; production deployments must override via Helm.
importImage := envOrDefault("TIDE_IMPORT_IMAGE", "ghcr.io/jsquirrelz/tide-import:v0.1.0-dev")
```

**Controller registration pattern** (lines 394–424 for ProjectReconciler; new ImportReconciler is simpler — no pool, no dispatcher, no signing key):
```go
if err := (&controller.ImportReconciler{
    Client:                  mgr.GetClient(),
    Scheme:                  mgr.GetScheme(),
    MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Project, // reuse Project concurrency or 1
    WatchNamespace:          watchNamespace,
    ImportImage:             importImage,
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "Import")
    os.Exit(1)
}
```

---

### Five dispatch-site guards (middleware, request-response)

**Analog:** `internal/controller/project_controller.go` lines 1050–1069 — the `checkBillingHalt` + `checkBudgetBlocked` guards before pool acquire

**Exact guard structure to mirror** (lines 1050–1069):
```go
// Phase 13 HALT-01 / D-05: third dispatch-entry hold (after CheckRejected +
// parent-approval); park, never fail; cleared by tide resume.
// Position: BEFORE pool acquire and BEFORE Job creation (Pitfall 2).
if checkBillingHalt(project) {
    logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
        "project", project.Name)
    return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}
```

**Import guard (identical at 4 planner sites):**
```go
// Phase 28 IMPORT-01: park planner dispatch until import completes.
// Position: after terminal short-circuit (Step 1), BEFORE pool acquire (Pitfall 2).
if project.Spec.ImportSource != nil {
    c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha2.ConditionImportComplete)
    if c == nil || c.Status != metav1.ConditionTrue {
        logger.V(1).Info("import pending; holding planner dispatch", "project", project.Name)
        return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
    }
}
```

**Insertion positions (from RESEARCH.md §Confirmed Code Locations):**

| File | After line | Before |
|------|------------|--------|
| `project_controller.go` | ~1005 (terminal switch closing brace) | line 1007 `jobName :=` |
| `milestone_controller.go` | ~282 (AwaitingApproval block closing brace) | line 284 `jobName :=` |
| `phase_controller.go` | equivalent AwaitingApproval block closing brace | `jobName :=` declaration |
| `plan_controller.go` | equivalent AwaitingApproval block closing brace | `jobName :=` declaration |
| `task_controller.go` | `gateChecks` after resolveProject (line ~339–360) and BEFORE CheckRejected (line ~366) | `gates.CheckRejected(project)` call |

**Task site requires `project` from `resolveProject`** — the guard reads `project.Spec.ImportSource` and `project.Status.Conditions`, so it must fire AFTER `resolveProject` succeeds at line ~339 and BEFORE the `CheckRejected` short-circuit at line ~366. Exact text from `gateChecks`:
```go
// Step 3: Resolve Project.
project, err := r.resolveProject(ctx, task)
// ... error handling ...

// [INSERT IMPORT GUARD HERE — project is available]
if project.Spec.ImportSource != nil {
    c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha2.ConditionImportComplete)
    if c == nil || c.Status != metav1.ConditionTrue {
        logger.V(1).Info("import pending; holding task dispatch", "task", task.Name)
        return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 5 * time.Second}}, nil
    }
}

// Plan 04-05 reject short-circuit (D-G1) — EXISTING, do not move
if gates.CheckRejected(project) {
```

---

### Tests: `internal/controller/import_controller_test.go` + `cmd/tide-import/main_test.go` (test)

**Analog:** Any existing `*_controller_test.go` in `internal/controller/` for the envtest-backed ImportController test. The `cmd/tide-import/main_test.go` uses no K8s — pure tempdir.

**Envtest test pattern** (from existing controller tests; the key pattern is envtest suite setup + table-driven `Describe`/`It` blocks using Ginkgo/Gomega):
- Use `envtest.Environment` with CRD scheme
- Create `Project` with `spec.importSource` set
- Create seed `ConfigMap`
- Assert `ImportComplete=True` condition on Project
- Assert child Milestone/Phase/Plan CRs exist
- Assert import Job was created and completed

**Unit test for tide-import binary** (pure filesystem, no envtest):
- Use `t.TempDir()` for old-workspace and new-workspace
- Write synthetic `in.json`/`out.json`/`children/` files at old-UID paths
- Call `run(ctx, cfg, stdout, stderr)` (the testable seam, mirroring tide-reporter's `runWithClient` seam at line 119)
- Assert new-UID paths contain correctly rewritten files
- Table-drive: no-clobber skip, atomic rename, schema conversion, Kind-allowlist rejection

---

## Shared Patterns

### Condition-Set with MergeFrom Patch
**Source:** `internal/controller/milestone_controller.go` lines 265–275
**Apply to:** ImportController at every state transition (Pending→CreatingCRs, CreatingCRs→CopyingEnvelopes, etc.)
```go
statusPatch := client.MergeFrom(ms.DeepCopy())
meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha2.ConditionWaveOrLevelPaused,
    Status:             metav1.ConditionFalse,
    Reason:             tideprojectv1alpha2.ReasonApprovedByUser,
    Message:            "...",
    LastTransitionTime: metav1.Now(),
})
if err := r.Status().Patch(ctx, ms, statusPatch); err != nil {
    return ctrl.Result{}, err
}
```

### AlreadyExists-is-Success
**Source:** `internal/reporter/materialize.go` lines 297–302
**Apply to:** ImportController's `client.Create` calls for seed CR materialization
```go
if err := c.Create(ctx, obj); err != nil {
    if !apierrors.IsAlreadyExists(err) {
        return fmt.Errorf("create %s/%s: %w", child.Kind, child.Name, err)
    }
    // AlreadyExists: idempotent success (SUB-03 / Pitfall F).
}
```

### envOrDefault Env Read
**Source:** `cmd/manager/main.go` (used throughout, e.g. line 195 `tidePushImage`, line 203 `reporterImage`)
**Apply to:** Reading TIDE_IMPORT_IMAGE in `cmd/manager/main.go`
```go
importImage := envOrDefault("TIDE_IMPORT_IMAGE", "ghcr.io/jsquirrelz/tide-import:v0.1.0-dev")
```

### Hardened Pod Security Context
**Source:** `internal/controller/reporter_jobspec.go` lines 156–162
**Apply to:** tide-import Job container spec
```go
runAsNonRoot := true
allowPrivEsc := false
sc := &corev1.SecurityContext{
    RunAsNonRoot:             &runAsNonRoot,
    AllowPrivilegeEscalation: &allowPrivEsc,
    Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
}
```

### Deterministic Job Naming (AlreadyExists idempotency)
**Source:** `internal/controller/reporter_jobspec.go` line 169; `internal/controller/project_controller.go` line 1007
**Apply to:** tide-import Job name in ImportController
```go
// Reporter: fmt.Sprintf("tide-reporter-%s", parent.GetUID())
// Init:     fmt.Sprintf("tide-init-%s", project.UID)
// Import:   fmt.Sprintf("tide-import-%s", project.UID)
```

### PVC subPath Mount (reporter_jobspec.go line 205)
**Source:** `internal/controller/reporter_jobspec.go` line 205
**Apply to:** tide-import Job's two PVC volume mounts
```go
SubPath: fmt.Sprintf("%s/workspace", project.UID)
```

---

## No Analog Found

All files have close analogs. No entries.

---

## Metadata

**Analog search scope:** `api/v1alpha2/`, `internal/controller/`, `cmd/tide-reporter/`, `cmd/manager/`, `images/tide-reporter/`, `charts/tide/`
**Files scanned:** 12 source files read directly
**Pattern extraction date:** 2026-06-18
