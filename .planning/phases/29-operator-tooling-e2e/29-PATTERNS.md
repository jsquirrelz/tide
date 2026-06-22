# Phase 29: Operator Tooling + E2E - Pattern Map

**Mapped:** 2026-06-22
**Files analyzed:** 9 new/modified files
**Analogs found:** 9 / 9

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/tide/export_envelopes.go` | controller (CLI verb) | request-response | `cmd/tide/artifact_get.go` | exact |
| `cmd/tide/export_envelopes_run.go` | service (pod I/O) | file-I/O (streaming read) | `cmd/tide/artifact_get_run.go` | exact |
| `cmd/tide/export_envelopes_test.go` | test | request-response | `cmd/tide/artifact_get_run_test.go` | exact |
| `cmd/tide/import_envelopes.go` | controller (CLI verb) | request-response | `cmd/tide/artifact_get.go` | exact |
| `cmd/tide/import_envelopes_run.go` | service (pod I/O) | file-I/O (streaming write + K8s API) | `cmd/tide/artifact_get_run.go` (inverted) | role-match |
| `cmd/tide/import_envelopes_test.go` | test | request-response | `cmd/tide/artifact_get_run_test.go` | role-match |
| `cmd/tide/subcommands.go` | config (wiring) | — | `cmd/tide/subcommands.go` (self) | exact (modify) |
| `pkg/bundle/` (bundle.go + seed.go) | utility | transform | `cmd/tide-import/main.go` (types) | role-match |
| `test/integration/kind/import_resume_test.go` | test | event-driven (kind) | `test/integration/kind/bare_project_test.go` + `chaos_resume_test.go` | role-match |

---

## Pattern Assignments

---

### `cmd/tide/export_envelopes.go` (CLI verb constructor, request-response)

**Analog:** `cmd/tide/artifact_get.go` (lines 1–91)

**Imports pattern** (`artifact_get.go` lines 19–26):
```go
import (
    "context"
    "fmt"
    "time"

    "github.com/spf13/cobra"
    "k8s.io/client-go/kubernetes"
)
```

**Cobra constructor pattern** (`artifact_get.go` lines 35–60):
```go
func newArtifactGetCmd() *cobra.Command {
    var timeout time.Duration
    var pvcName string
    c := &cobra.Command{
        Use:   "artifact-get <namespace>/<project>/<path>",
        Short: "Fetch a PVC artifact via an inspector pod",
        Long:  "...",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            return runArtifactGet(cmd, args, timeout, pvcName)
        },
    }
    c.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "...")
    c.Flags().StringVar(&pvcName, "pvc", "tide-projects", "...")
    return c
}
```

**RunE adapter pattern** (`artifact_get.go` lines 65–91):
```go
func runArtifactGet(cmd *cobra.Command, args []string, timeout time.Duration, pvcName string) error {
    k, err := K8sClient()
    if err != nil { return err }
    cfg, err := RESTConfig()
    if err != nil { return err }
    cs, err := kubernetes.NewForConfig(cfg)
    if err != nil { return fmt.Errorf("build kubernetes clientset: %w", err) }

    ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
    defer cancel()

    return artifactGetRun(ctx, k, cs, args[0], pvcName, cmd.OutOrStdout(), cmd.ErrOrStderr())
}
```

**New flags for `export-envelopes`** (beyond what `artifact-get` uses):
- `--output <path>` (default `<project>.tgz`) — output bundle path
- `--dir` (bool) — unpack to directory instead of tgz
- `--pvc <name>` (default `tide-projects`) — same as artifact-get
- `--timeout <duration>` (default `5m`) — same as artifact-get

**RBAC comment block** (`artifact_get.go` lines 28–34):
```go
// Required RBAC in the target namespace:
//   - pods: create, get, delete
//   - pods/log: get      ← export (read path, same as artifact-get)
//   - pods/exec: create  ← import (write path, new verb for loader pod)
```

---

### `cmd/tide/export_envelopes_run.go` (inspector-pod implementation, file-I/O read)

**Analog:** `cmd/tide/artifact_get_run.go` (entire file, 323 lines)

**Func-var seam pattern** (`artifact_get_run.go` line 57):
```go
// inspectorPodRunner creates and streams the inspector pod. Function var so
// tests can inject without a live apiserver (mirrors tail.go's tailStreamer).
var inspectorPodRunner = defaultInspectorPodRunner
```
Export replicates this as:
```go
var exportInspectorPodRunner = defaultExportInspectorPodRunner
```

**Project UID resolution** (`artifact_get_run.go` lines 135–147):
```go
var proj tidev1alpha2.Project
if err := k.Get(ctx, types.NamespacedName{Namespace: ns, Name: projName}, &proj); err != nil {
    return fmt.Errorf("get project %s/%s: %w", ns, projName, err)
}
projectUID := string(proj.UID)
if projectUID == "" {
    return fmt.Errorf("project %s/%s has no UID — cannot resolve PVC subPath", ns, projName)
}
```
Export also queries live Milestone/Phase/Plan CRs (List calls) to build seed manifest entries. Use the same `k.Get` / `k.List` pattern with `tidev1alpha2.MilestoneList`, `PhaseList`, `PlanList`.

**Pod spec pattern — ReadOnly mount** (`artifact_get_run.go` lines 176–236):
```go
podSpec := &corev1.Pod{
    ObjectMeta: metav1.ObjectMeta{
        Name:      podName,
        Namespace: ns,
        Labels: map[string]string{
            "app.kubernetes.io/component":  "tide-inspector",
            "app.kubernetes.io/managed-by": "tide-cli",
        },
    },
    Spec: corev1.PodSpec{
        RestartPolicy: corev1.RestartPolicyNever,
        Containers: []corev1.Container{{
            Name:  "inspector",
            Image: "busybox:1.36",
            Command: []string{"sh", "-c",
                // artifact-get: cat "$ARTIFACT_PATH"
                // export:       tar czf - -C /workspace envelopes/ artifacts/
                `tar czf - -C /workspace envelopes/ artifacts/`,
            },
            VolumeMounts: []corev1.VolumeMount{{
                Name:      "workspace",
                MountPath: "/workspace",
                SubPath:   projectUID,  // <PVC>/<projectUID>/workspace/
                ReadOnly:  true,
            }},
        }},
        Volumes: []corev1.Volume{{
            Name: "workspace",
            VolumeSource: corev1.VolumeSource{
                PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                    ClaimName: pvcName,
                    ReadOnly:  true,
                },
            },
        }},
    },
}
```

**Key difference from artifact-get:** the export command changes:
- Pod command: `tar czf - -C /workspace envelopes/ artifacts/` (not `cat`)
- No `ARTIFACT_PATH` env var (no user-controlled path)
- No stability poll loop needed (tarring a directory is atomic from the pod's view)
- SubPath is `projectUID` (not `projectUID + "/" + something`) — the PVC subPath `<projectUID>` maps `/workspace` to `<PVC>/<projectUID>/`; tar then reaches `envelopes/` and `artifacts/` inside that

**Pod create + deferred delete** (`artifact_get_run.go` lines 238–246):
```go
if _, err := cs.CoreV1().Pods(ns).Create(ctx, podSpec, metav1.CreateOptions{}); err != nil {
    return fmt.Errorf("create inspector pod: %w", err)
}
// T-15-09: defer Delete with context.Background()
defer func() {
    _ = cs.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
}()
```

**waitForPodRunning helper** (`artifact_get_run.go` lines 287–311): copy verbatim — identical for export pod.

**Log stream for tgz bytes** (`artifact_get_run.go` lines 258–282):
```go
req := cs.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
    Follow:    true,
    Container: "inspector",
})
stream, err := req.Stream(ctx)
// ...
go func() {
    <-ctx.Done()
    _ = stream.Close()
}()
if _, err := io.Copy(out, stream); err != nil && ctx.Err() == nil {
    return fmt.Errorf("read artifact stream: %w", err)
}
```
Export writes the tgz stream to a local file or `io.Writer` — same `io.Copy` pattern.

**randSuffix helper** (`artifact_get_run.go` lines 314–322): copy verbatim.

**childCount repair pattern** (new in export — D-16):
After reading each `out.json` off the PVC (via the tgz stream + extraction to temp dir), before writing into the bundle:
```go
var env pkgdispatch.EnvelopeOut
if err := json.Unmarshal(outData, &env); err != nil { ... }
// D-16: legacy salvage envelopes predate ChildCount field (plan 09-08).
// Repair: stamp childCount = len(childCRDs) when absent/0 but children exist.
if env.ChildCount == 0 && len(env.ChildCRDs) > 0 {
    fmt.Fprintf(errOut, "repaired legacy childCount for envelope %s\n", env.TaskUID)
    env.ChildCount = len(env.ChildCRDs)
    outData, _ = json.Marshal(env)
}
```
Source type: `pkg/dispatch/envelope.go:224` `ChildCount int \`json:"childCount,omitempty"\``

---

### `cmd/tide/export_envelopes_test.go` (unit test, func-var seam injection)

**Analog:** `cmd/tide/artifact_get_run_test.go` (entire file)

**testScheme helper** (`cmd/tide/inspect_wave_test.go` lines 36–42):
```go
func testScheme(t *testing.T) *runtime.Scheme {
    t.Helper()
    s := runtime.NewScheme()
    utilruntime.Must(clientgoscheme.AddToScheme(s))
    utilruntime.Must(tidev1alpha2.AddToScheme(s))
    return s
}
```
Already declared in the package — do not re-declare. Import from within `package main`.

**fakeRunner pattern** (`artifact_get_run_test.go` lines 67–103):
```go
type fakeRunner struct {
    createCalls atomic.Int32
    deleteCalls atomic.Int32
    streamBytes []byte
    errToReturn error
    runFn func(ctx context.Context, cs kubernetes.Interface, ns, projectUID, artifactPath, pvcName string, out, errOut io.Writer) error
}

func (f *fakeRunner) run(...) error {
    f.createCalls.Add(1)
    defer f.deleteCalls.Add(1)
    if f.runFn != nil { return f.runFn(...) }
    if f.errToReturn != nil { return f.errToReturn }
    _, _ = out.Write(f.streamBytes)
    return nil
}

func injectRunner(f *fakeRunner) func() {
    orig := inspectorPodRunner
    inspectorPodRunner = f.run
    return func() { inspectorPodRunner = orig }
}
```
Export test declares an equivalent `fakeExportRunner` with matching signature for `exportInspectorPodRunner`. The `injectRunner` helper is NOT re-usable across files because the target seam var differs — declare `injectExportRunner` separately.

**Test body pattern** (`artifact_get_run_test.go` lines 111–150):
```go
func TestExportEnvelopesFoo(t *testing.T) {
    fr := &fakeExportRunner{streamBytes: makeFakeTgzBytes(t)}
    restore := injectExportRunner(fr)
    defer restore()

    proj := makeProjectForExport("my-project", "default")
    c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
    cs := fakeclientset.NewSimpleClientset()

    var stdout, stderr bytes.Buffer
    err := exportEnvelopesRun(context.Background(), c, cs, "default/my-project",
        "tide-projects", "/tmp/out.tgz", false, &stdout, &stderr)
    // assertions...
}
```

**Key tests to author:**
1. Flag parsing: `cobra.Command.Execute()` with `buildRootForTest()` to verify flag wiring
2. childCount repair: inject fake tgz with ChildCount=0/len(ChildCRDs)>0; assert repaired bytes written
3. Seed manifest generation: inject fake CRs with known UIDs; assert FQName + OldUID + sha256 in manifest
4. Offline dry-run cycle: inject bundle with cycle edges; assert `CycleError` returned with `InvolvedNodes`
5. Timeout: verify non-zero exit + pod delete called (same as `TestArtifactGetTimeout`)

---

### `cmd/tide/import_envelopes.go` (CLI verb constructor, request-response)

**Analog:** `cmd/tide/artifact_get.go` (lines 1–91) — same cobra constructor pattern.

**New flags for `import-envelopes`** beyond artifact-get:
- `--namespace <ns>` (target namespace for ConfigMap + loader pod)
- `--dry-run` (bool) — offline validation only, no cluster writes
- `--output json` — machine-readable dry-run report (inherits `--output/-o` from root)
- `--pvc <name>` (default `tide-projects`)
- `--timeout <duration>` (default `5m`)

**Args:** `cobra.ExactArgs(1)` — positional arg is the bundle path (`bundle.tgz` or directory).

**RunE adapter** (mirrors `runArtifactGet`):
```go
func runImportEnvelopes(cmd *cobra.Command, args []string, ns string, dryRun bool, timeout time.Duration, pvcName string) error {
    if dryRun {
        // offline — no K8s client needed
        return importEnvelopesDryRun(args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
    }
    k, err := K8sClient()
    if err != nil { return err }
    cfg, err := RESTConfig()
    if err != nil { return err }
    cs, err := kubernetes.NewForConfig(cfg)
    if err != nil { return fmt.Errorf("build kubernetes clientset: %w", err) }

    ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
    defer cancel()

    return importEnvelopesRun(ctx, k, cs, cfg, args[0], ns, pvcName, cmd.OutOrStdout(), cmd.ErrOrStderr())
}
```

---

### `cmd/tide/import_envelopes_run.go` (loader pod + dry-run, file-I/O write)

**Analog:** `cmd/tide/artifact_get_run.go` — the loader pod INVERTS the inspector pod. Read-direction (log stream) becomes write-direction (remotecommand/SPDY exec).

**Func-var seam** (mirrors `artifact_get_run.go` line 57):
```go
var loaderPodRunner = defaultLoaderPodRunner
```

**Loader pod spec — key differences from inspector pod:**

| Property | Inspector (export) | Loader (import) |
|----------|--------------------|-----------------|
| PVC ReadOnly | `true` | `false` |
| SubPath | `projectUID` | `oldProjectUID + "/workspace"` |
| Command | `tar czf - -C /workspace envelopes/ artifacts/` | `tar xzf - -C /workspace` |
| `stdin: true` | no | `yes` (`Container.Stdin = true`) |
| Streaming | `GetLogs` → `io.Copy(out, stream)` | `remotecommand.SPDY` → `io.Copy(pod-stdin, tgzReader)` |
| RBAC | `pods/log: get` | `pods/exec: create` |

**Loader pod template** (new code, adapts `artifact_get_run.go` pod spec):
```go
podSpec := &corev1.Pod{
    ObjectMeta: metav1.ObjectMeta{
        Name:      podName,
        Namespace: ns,
        Labels: map[string]string{
            "app.kubernetes.io/component":  "tide-loader",
            "app.kubernetes.io/managed-by": "tide-cli",
        },
    },
    Spec: corev1.PodSpec{
        RestartPolicy: corev1.RestartPolicyNever,
        Containers: []corev1.Container{{
            Name:  "loader",
            Image: "busybox:1.36",
            Command: []string{"tar", "xzf", "-", "-C", "/workspace"},
            Stdin:   true,
            VolumeMounts: []corev1.VolumeMount{{
                Name:      "workspace",
                MountPath: "/workspace",
                SubPath:   oldProjectUID + "/workspace",  // <PVC>/<oldUID>/workspace/
                ReadOnly:  false,
            }},
        }},
        Volumes: []corev1.Volume{{
            Name: "workspace",
            VolumeSource: corev1.VolumeSource{
                PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                    ClaimName: pvcName,
                    ReadOnly:  false,
                },
            },
        }},
    },
}
```

**SPDY exec stream for write direction** (uses `k8s.io/client-go/tools/remotecommand` — already in go.mod):
```go
// After waitForPodRunning (same helper from artifact_get_run.go):
execURL := cfg.Host + fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/exec", ns, podName)
req := cs.CoreV1().RESTClient().Post().
    Resource("pods").Name(podName).Namespace(ns).
    SubResource("exec").
    VersionedParams(&corev1.PodExecOptions{
        Container: "loader",
        Command:   []string{"tar", "xzf", "-", "-C", "/workspace"},
        Stdin:     true,
        Stdout:    true,
        Stderr:    true,
    }, runtime.NewParameterCodec(scheme))
exec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
if err != nil { return fmt.Errorf("create SPDY executor: %w", err) }
if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
    Stdin:  tgzReader,
    Stdout: errOut,
    Stderr: errOut,
}); err != nil { return fmt.Errorf("stream tgz to loader pod: %w", err) }
```
**Import:** `"k8s.io/client-go/tools/remotecommand"` — already a transitive dep, no go.mod change.

**Dry-run path** (offline, no pods, no cluster writes — D-07):
```go
func importEnvelopesDryRun(bundlePath string, out, errOut io.Writer) error {
    // 1. Unpack bundle to temp dir (or use directly if --dir).
    // 2. Read seed-manifest.json.
    // 3. For each seedEntry: read out.json, ValidateAPIVersionKind, check len(ChildCRDs)==ChildCount, verify sha256.
    // 4. Build dag.Edge slice from seedEntry.DependsOn.
    // 5. dag.ComputeWaves(nodes, edges) — returns *CycleError on cycle.
    // 6. Print table: level | name | verdict | reason.
    // Return non-nil error only on hard cycle (D-09: cycle rejects entire import).
}
```

**ValidateAPIVersionKind call** (`pkg/dispatch/envelope.go` lines 400–415):
```go
if err := dispatch.ValidateAPIVersionKind(env.APIVersion, env.Kind, dispatch.KindTaskEnvelopeOut); err != nil {
    row.Verdict = "re-plan"
    row.Reason = fmt.Sprintf("schema mismatch: %v", err)
    continue
}
```

**ComputeWaves for cycle detection** (`pkg/dag/kahn.go` lines 46–97):
```go
waves, err := dag.ComputeWaves(nodes, edges)
if err != nil {
    var cycleErr *dag.CycleError
    if errors.As(err, &cycleErr) {
        // D-09: hard-reject entire import
        fmt.Fprintf(out, "CYCLE DETECTED — import would fail\nInvolved nodes: %v\n", cycleErr.InvolvedNodes)
        return fmt.Errorf("cyclic DAG: %w", err)
    }
    return err
}
```

**ConfigMap creation for live import** (uses `cs.CoreV1().ConfigMaps(ns).Create()`):
```go
cm := &corev1.ConfigMap{
    ObjectMeta: metav1.ObjectMeta{
        Name:      seedConfigMapName,
        Namespace: ns,
    },
    Data: map[string]string{
        "manifest": string(seedManifestJSON),
    },
}
if _, err := cs.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
    if !apierrors.IsAlreadyExists(err) {  // AlreadyExists = idempotent re-run
        return fmt.Errorf("create seed ConfigMap: %w", err)
    }
}
```
Pattern for `apierrors.IsAlreadyExists` is used throughout `internal/controller/import_controller.go`.

---

### `cmd/tide/import_envelopes_test.go` (unit test)

**Analog:** `cmd/tide/artifact_get_run_test.go`

**fakeLoaderRunner pattern** (mirrors fakeRunner, but signature includes `restCfg`, `tgzReader`):
```go
type fakeLoaderRunner struct {
    createCalls  atomic.Int32
    capturedData []byte  // bytes the fake "received"
    errToReturn  error
}
```

**Key tests to author:**
1. Dry-run table output: inject bundle with known envelopes; assert tabular output rows
2. Dry-run cycle detection: inject seed with cycle edges; assert error + `InvolvedNodes` printed
3. Dry-run sha256 mismatch: inject bundle with corrupted out.json; assert verdict = "re-plan", reason = "checksum mismatch"
4. Live mode ConfigMap creation: fake `cs.CoreV1().ConfigMaps(ns)` via `fakeclientset.NewSimpleClientset()`; assert ConfigMap created with `"manifest"` key
5. Idempotent re-run: ConfigMap already exists → `IsAlreadyExists` swallowed, no error

---

### `cmd/tide/subcommands.go` (register new verbs — modify existing file)

**File:** `cmd/tide/subcommands.go` (lines 28–41)

**Current pattern** (lines 28–41):
```go
func registerSubcommands(root *cobra.Command) {
    root.AddCommand(newApplyCmd())
    root.AddCommand(newWatchCmd())
    root.AddCommand(newInspectWaveCmd())
    root.AddCommand(newArtifactGetCmd())
    root.AddCommand(newDescribeBudgetCmd())
    root.AddCommand(newApproveCmd())
    root.AddCommand(newRejectCmd())
    root.AddCommand(newCancelCmd())
    root.AddCommand(newResumeCmd())
    root.AddCommand(newTailCmd())
}
```

**Change:** Add two lines in the same style:
```go
    root.AddCommand(newExportEnvelopesCmd())
    root.AddCommand(newImportEnvelopesCmd())
```
No structural changes to the file — pure append.

---

### `pkg/bundle/` (bundle.go + seed.go — new package)

**Analog:** `cmd/tide-import/main.go` (rekeyEntry type shape) + `internal/controller/import_controller.go` (seedEntry/seedManifest structs lines 94–130)

**Why a separate pkg rather than re-importing internal/controller:**
`cmd/tide/` is a binary that MUST NOT import `internal/controller/` (the controller package pulls in full controller-runtime + envtest dependencies). `pkg/bundle/` is importable from both `cmd/tide/` and test code.

**BundleEntry — superset of seedEntry with sha256** (`seed.go`):
```go
// BundleEntry mirrors internal/controller.seedEntry but adds SHA256 for dry-run
// integrity validation (D-04). The ImportController's json.Unmarshal ignores
// the extra field (Go's encoding/json drops unknown fields by default — verified
// import_controller.go:311 uses standard json.Unmarshal without DisallowUnknownFields).
type BundleEntry struct {
    Name         string   `json:"name"`
    FQName       string   `json:"fqName"`
    OldUID       string   `json:"oldUID"`
    DependsOn    []string `json:"dependsOn,omitempty"`
    Status       string   `json:"status,omitempty"`
    PhaseRef     string   `json:"phaseRef,omitempty"`
    MilestoneRef string   `json:"milestoneRef,omitempty"`
    ProjectRef   string   `json:"projectRef,omitempty"`
    // SHA256 is the hex-encoded sha256 of the envelope's out.json bytes.
    // Not consumed by ImportController (unknown field, silently ignored).
    // Read by dry-run validation (D-04/D-07).
    SHA256 string `json:"sha256,omitempty"`
}

// BundleManifest is the top-level structure of seed-manifest.json in the bundle.
// Mirrors internal/controller.seedManifest exactly for the Milestones/Phases/Plans
// arrays; the ImportController parses only those three keys.
type BundleManifest struct {
    Milestones []BundleEntry `json:"milestones"`
    Phases     []BundleEntry `json:"phases"`
    Plans      []BundleEntry `json:"plans"`
}
```

**FQName construction convention** (from `import_controller.go` line 99 comment):
- Milestone: `<milestoneName>` (single-component; ProjectRef is separate field)
- Phase: `<milestoneName>/<phaseName>`
- Plan: `<milestoneName>/<phaseName>/<planName>`

**sha256 computation** (uses `crypto/sha256` stdlib, already used in `internal/credproxy/token.go`):
```go
import "crypto/sha256"

func computeEnvelopeSHA256(outJSONBytes []byte) string {
    sum := sha256.Sum256(outJSONBytes)
    return fmt.Sprintf("%x", sum)
}
```

**Bundle tgz structure** (must match salvage fixture — verified in RESEARCH.md):
```
<bundle>.tgz root:
  project.yaml          (operator applies this after import-envelopes stages)
  milestones.yaml
  phases.yaml
  plans.yaml
  seed-manifest.json    (BundleManifest JSON — key "manifest" value for ConfigMap Data)
  SEED-OUTLINE.md       (human-readable tree)
  pvc-envelopes.tgz     (nested: envelopes/<oldUID>/out.json + children/ + in.json + events.jsonl)
```

**pvc-envelopes.tgz internal layout** (workspace content, NOT workspace/-prefixed):
```
pvc-envelopes.tgz root:
  envelopes/
    <oldTaskUID>/
      in.json
      out.json          (childCount-repaired per D-16)
      children/
        <childName>.json
      events.jsonl
  artifacts/
    ...
```
Loader pod: `tar xzf - -C /workspace` with PVC mount at `subPath=<oldUID>/workspace`.

---

### `test/integration/kind/import_resume_test.go` (kind E2E test)

**Analog:** `test/integration/kind/bare_project_test.go` (structure) + `test/integration/kind/chaos_resume_test.go` (JobList label filter, exec.LookPath pattern)

**Package and import pattern** (`bare_project_test.go` lines 17–57):
```go
package kind_integration

import (
    "fmt"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    batchv1 "k8s.io/api/batch/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)
```
Add `"os/exec"` for `exec.LookPath("tide")`.

**testing.Short() guard** (`suite_test.go` lines 119–121):
```go
// Suite-level guard already in TestIntegrationKind:
if testing.Short() {
    t.Skip("skipping kind integration tests in short mode")
}
```
For the heavy within-suite specs (D-12), add a spec-level guard:
```go
var _ = Describe("Import resume E2E", Label("kind", "long"), func() {
    BeforeEach(func() {
        skipIfCRDsOnlyMode()
        if testing.Short() {
            Skip("Skipping long import-resume test in short mode")
        }
        // Verify tide binary is available
        if _, err := exec.LookPath("tide"); err != nil {
            Skip("tide binary not in PATH; build with `make build-cli` first")
        }
    })
```

**Namespace + PVC + signing-key setup** (`bare_project_test.go` lines 64–76):
```go
BeforeEach(func() {
    skipIfCRDsOnlyMode()
    createNamespace(importResumeNS)
    ensureSigningKeySecret(importResumeNS)
    // pvcPrewarmPod is called inside createNamespace
})
AfterEach(func() {
    deleteNamespace(importResumeNS)
})
```

**Invoking the real tide binary** (D-10, pattern from `suite_test.go` `exec.CommandContext` convention):
```go
tideBin, _ := exec.LookPath("tide")
bundlePath := filepath.Join(GinkgoT().TempDir(), "test-bundle.tgz")

// Export
exportCmd := exec.CommandContext(ctx, tideBin,
    "--kubeconfig", kubeconfigPath,
    "export-envelopes", importResumeNS+"/"+projectName,
    "--output", bundlePath,
)
out, err := exportCmd.CombinedOutput()
Expect(err).NotTo(HaveOccurred(), "tide export-envelopes: %s", out)

// Import
importCmd := exec.CommandContext(ctx, tideBin,
    "--kubeconfig", kubeconfigPath,
    "import-envelopes", bundlePath,
    "--namespace", targetNS,
)
out, err = importCmd.CombinedOutput()
Expect(err).NotTo(HaveOccurred(), "tide import-envelopes: %s", out)
```

**JobList label filter assertion** (`chaos_resume_test.go` lines 222–234):
```go
jobs := &batchv1.JobList{}
Expect(k8sClient.List(ctx, jobs,
    client.InNamespace(ns),
    client.MatchingLabels{
        "tideproject.k8s/role":  "planner",
        "tideproject.k8s/level": "milestone",
    },
)).To(Succeed())
Expect(jobs.Items).To(BeEmpty(),
    "no milestone planner Jobs should be dispatched for imported levels")
```
D-17: Repeat for `level: phase`. Do NOT assert for `level: plan` (plan planners legitimately re-run).

**$0 re-paid planning cost assertion** (`api/v1alpha2/project_types.go:270` — `CostSpentCents int64`):
```go
var project tideprojectv1alpha2.Project
Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: projectName}, &project)).To(Succeed())
Expect(project.Status.Budget.CostSpentCents).To(Equal(int64(0)),
    "imported envelopes must not re-pay planning cost (D-14)")
```
Assert this BEFORE plan-level planner Jobs can dispatch (i.e., immediately after import completes and before the wave controller advances to plan level).

**applyFile helper** (`suite_test.go` lines 1042–1050):
```go
func applyFile(path string) error {
    cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
        "apply", "-f", path, "--timeout=30s")
    out, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("kubectl apply -f %s failed: %w\n%s", path, err, out)
    }
    return nil
}
```
Import test uses `applyFile` to apply the bundle's `project.yaml` after `import-envelopes` stages it.

**skipIfCRDsOnlyMode pattern** (referenced across multiple test files; defined in `suite_test.go`): call it in every `BeforeEach` as the first guard.

**Makefile: tide binary build** (current `Makefile` line 263):
```makefile
go build -o bin/tide ./cmd/tide
```
This is NOT currently in `test-int-kind-prep` (line 155). The planner must add:
```makefile
go build -o bin/tide ./cmd/tide
```
to `test-int-kind-prep`, or have the test check `os.Getenv("TIDE_BINARY")` as an override path (same pattern as `SKIP_KIND_TESTS` env var in `suite_test.go:116`).

**Small fixture for drain-to-Succeeded tier** (D-11 tier a):
```
test/integration/kind/testdata/import-small-fixture/
  projects.yaml
  milestones.yaml
  phases.yaml
  plans.yaml
  seed-manifest.json   (BundleManifest with 1 project/1 ms/1 phase/2 plans)
  pvc-envelopes.tgz   (stub-generated envelopes with correct childCount)
```
Minimum: 1 Project → 1 Milestone → 1 Phase → 2 Plans (no Tasks — Tasks materialize from plan-level children). Stub subagent handles planner + executor roles.

---

## Shared Patterns

### kubeconfig chain (stateless CLI)
**Source:** `cmd/tide/root_flags.go` lines 40–109
**Apply to:** `export_envelopes.go`, `import_envelopes.go`
```go
var configFlags = genericclioptions.NewConfigFlags(true)

func RESTConfig() (*rest.Config, error) {
    cfg, err := configFlags.ToRESTConfig()
    if err != nil { return nil, fmt.Errorf("resolve kubeconfig: %w", err) }
    return cfg, nil
}

func K8sClient() (client.Client, error) {
    cfg, err := RESTConfig()
    if err != nil { return nil, err }
    c, err := client.New(cfg, client.Options{Scheme: scheme})
    if err != nil { return nil, fmt.Errorf("build k8s client: %w", err) }
    return c, nil
}
```
Both new verbs call `K8sClient()` + `RESTConfig()` + `kubernetes.NewForConfig(cfg)` exactly as `runArtifactGet` does.

### Deferred pod delete (T-15-09)
**Source:** `cmd/tide/artifact_get_run.go` lines 244–246
**Apply to:** `export_envelopes_run.go` (inspector pod), `import_envelopes_run.go` (loader pod)
```go
defer func() {
    _ = cs.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
}()
```
Always `context.Background()` (not `ctx`) so cleanup fires even after timeout/cancel.

### Ctx-cancel watcher (log stream close)
**Source:** `cmd/tide/artifact_get_run.go` lines 268–272
**Apply to:** `export_envelopes_run.go` (tgz stream read-out)
```go
go func() {
    <-ctx.Done()
    _ = stream.Close()
}()
```

### AlreadyExists idempotency
**Source:** `internal/controller/import_controller.go` (multiple sites)
**Apply to:** `import_envelopes_run.go` (ConfigMap creation)
```go
if !apierrors.IsAlreadyExists(err) {
    return fmt.Errorf("create seed ConfigMap: %w", err)
}
```

### Path traversal defense
**Source:** `cmd/tide/artifact_get_run.go` lines 96–115 (`validateArtifactPath`)
         + `cmd/tide-import/main.go` lines 272–289 (`containedJoin`)
**Apply to:** `pkg/bundle/bundle.go` tgz extraction (zip-slip defense)
```go
// Before writing any extracted file:
cleanPath := filepath.Clean(hdr.Name)
if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
    return fmt.Errorf("tgz entry %q rejected: path traversal", hdr.Name)
}
destPath := filepath.Join(destDir, cleanPath)
if !strings.HasPrefix(destPath, destDir+string(os.PathSeparator)) {
    return fmt.Errorf("tgz entry %q resolves outside dest dir (zip-slip)", hdr.Name)
}
```

### stderr-for-status, stdout-for-data (D-10)
**Source:** `cmd/tide/artifact_get_run.go` lines 238, 257
**Apply to:** `export_envelopes_run.go`, `import_envelopes_run.go`
- All status/progress messages: `fmt.Fprintf(errOut, "...")`
- Dry-run table output: `fmt.Fprintf(out, "...")` (stdout — machine-parseable)
- tgz bytes: written to local file, NOT stdout

### Kind test namespace lifecycle
**Source:** `test/integration/kind/bare_project_test.go` lines 64–80
**Apply to:** `import_resume_test.go`
```go
BeforeEach(func() {
    skipIfCRDsOnlyMode()
    createNamespace(importResumeNS)
    ensureSigningKeySecret(importResumeNS)
})
AfterEach(func() {
    deleteNamespace(importResumeNS)
    if CurrentSpecReport().Failed() {
        // dump logs (optional, follow bare_project_test.go AfterEach pattern)
    }
})
```

### randSuffix helper
**Source:** `cmd/tide/artifact_get_run.go` lines 314–322
**Apply to:** `export_envelopes_run.go`, `import_envelopes_run.go` — pod name generation. The helper is already in the package; do not re-declare.

---

## No Analog Found

All files have strong analogs. No entries in this section.

---

## Metadata

**Analog search scope:** `cmd/tide/`, `cmd/tide-import/`, `pkg/dispatch/`, `pkg/dag/`, `internal/controller/import_controller.go`, `api/v1alpha2/import_types.go`, `test/integration/kind/`
**Files scanned:** 14 source files read directly
**Pattern extraction date:** 2026-06-22
