# Phase 13: Dispatch Image Resolution + Provider Halt - Pattern Map

**Mapped:** 2026-06-11
**Files analyzed:** 11 new/modified files
**Analogs found:** 11 / 11

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/controller/dispatch_helpers.go` | utility | request-response | `internal/controller/dispatch_helpers.go` (ResolveProvider) | exact — add parallel function |
| `internal/controller/milestone_controller.go` | controller | request-response | `internal/controller/phase_controller.go` :341-344 | exact |
| `internal/controller/phase_controller.go` | controller | request-response | `internal/controller/plan_controller.go` :373-376 | exact |
| `internal/controller/plan_controller.go` | controller | request-response | `internal/controller/milestone_controller.go` :380-389 | exact |
| `internal/controller/project_controller.go` | controller | request-response | `internal/controller/milestone_controller.go` :380-389 | exact |
| `internal/controller/task_controller.go` | controller | request-response | `internal/controller/task_controller.go` :619-628 (Deps.SubagentImage) | exact |
| `internal/credproxy/server.go` | middleware | request-response | `internal/credproxy/server.go` (Director pattern) | exact — add ModifyResponse |
| `api/v1alpha1/shared_types.go` | model | — | `api/v1alpha1/shared_types.go` (ConditionBudgetExceeded block) | exact |
| `cmd/tide/resume.go` | utility | request-response | `cmd/tide/resume.go` (resumeRun / ConsumeReject patch) | exact — extend existing function |
| `charts/tide/templates/deployment.yaml` | config | — | `charts/tide/templates/deployment.yaml` :45 (CLAUDE_SUBAGENT_IMAGE env) | exact |
| `test/integration/kind/suite_test.go` | test | — | `test/integration/kind/suite_test.go` :469-506 (helmControllerArgs) | exact |

---

## Pattern Assignments

### `internal/controller/dispatch_helpers.go` — new `resolveImage` function

**Analog:** `internal/controller/dispatch_helpers.go` — `ResolveProvider` (lines 138-183)

**Imports pattern** (lines 39-56 — no new imports needed; same package):
```go
import (
    tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)
```
`resolveImage` lives in the same `controller` package as `ResolveProvider`; no import changes.

**Core pattern — ResolveProvider (lines 138-183) to mirror exactly:**
```go
func ResolveProvider(project *tideprojectv1alpha1.Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec {
    var levelCfg *tideprojectv1alpha1.LevelConfig
    if project != nil {
        switch level {
        case "milestone":
            levelCfg = project.Spec.Subagent.Levels.Milestone
        case "phase":
            levelCfg = project.Spec.Subagent.Levels.Phase
        case "plan":
            levelCfg = project.Spec.Subagent.Levels.Plan
        case "task":
            levelCfg = project.Spec.Subagent.Levels.Task
        }
    }
    // Resolve Model — switch{} over three precedence levels
    var model string
    switch {
    case levelCfg != nil && levelCfg.Model != "":
        model = levelCfg.Model
    case project != nil && project.Spec.Subagent.Model != "":
        model = project.Spec.Subagent.Model
    default:
        if helmDefaults.Models != nil {
            model = helmDefaults.Models[level]
        }
    }
    ...
}
```

**New function — resolveImage (mirrors the same switch structure for Image):**
```go
// resolveImage walks Project.Spec.Subagent precedence chain for the given
// level, returning the resolved subagent container image reference.
//
//   Levels.<level>.Image → Spec.Subagent.Image → helmDefaults.Image → ""
//
// An empty return means no image was configured; callers must surface this
// as a config error rather than dispatching a Job with an empty image field.
func resolveImage(project *tideprojectv1alpha1.Project, level string, helmDefaults ProviderDefaults) string {
    var levelCfg *tideprojectv1alpha1.LevelConfig
    if project != nil {
        switch level {
        case "milestone":
            levelCfg = project.Spec.Subagent.Levels.Milestone
        case "phase":
            levelCfg = project.Spec.Subagent.Levels.Phase
        case "plan":
            levelCfg = project.Spec.Subagent.Levels.Plan
        case "task":
            levelCfg = project.Spec.Subagent.Levels.Task
        }
    }
    switch {
    case levelCfg != nil && levelCfg.Image != "":
        return levelCfg.Image
    case project != nil && project.Spec.Subagent.Image != "":
        return project.Spec.Subagent.Image
    default:
        return helmDefaults.Image
    }
}
```

**Also add — checkBillingHalt helper (mirrors checkParentApproval, lines 254-296):**
```go
// checkParentApproval (lines 271-296) — exact shape to copy for checkBillingHalt:
func checkParentApproval(ctx context.Context, c client.Client, ns, parentName, parentKind string) (bool, error) {
    if parentName == "" {
        return false, nil
    }
    switch parentKind {
    case "Milestone":
        var ms tideprojectv1alpha1.Milestone
        if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ms); err != nil {
            return false, client.IgnoreNotFound(err)
        }
        return ms.Status.Phase == "AwaitingApproval", nil
    ...
    }
    return false, nil
}

// checkBillingHalt — same nil-guard shape, reads Project.Status.Conditions:
func checkBillingHalt(project *tideprojectv1alpha1.Project) bool {
    if project == nil {
        return false
    }
    for _, c := range project.Status.Conditions {
        if c.Type == tideprojectv1alpha1.ConditionBillingHalt &&
            c.Status == metav1.ConditionTrue {
            return true
        }
    }
    return false
}
```

---

### Five dispatch sites — `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `project_controller.go`, `task_controller.go`

**Analog — current broken 2-line pattern at each site:**

milestone_controller.go (lines 380-389):
```go
opts := podjob.BuildOptions{
    ...
    SubagentImage:  r.SubagentImage,
    ...
}
if opts.SubagentImage == "" {
    opts.SubagentImage = r.HelmProviderDefaults.Image
}
```

phase_controller.go (lines 341-344):
```go
subagentImage := r.SubagentImage
if subagentImage == "" {
    subagentImage = r.HelmProviderDefaults.Image
}
opts := podjob.BuildOptions{..., SubagentImage: subagentImage, ...}
```

plan_controller.go (lines 373-376): identical to phase_controller.go pattern.

project_controller.go (lines 982-985):
```go
subagentImg := r.SubagentImage
if subagentImg == "" {
    subagentImg = r.HelmProviderDefaults.Image
}
opts := podjob.BuildOptions{..., SubagentImage: subagentImg, ...}
```

task_controller.go (line 628):
```go
opts := podjob.BuildOptions{
    ...
    SubagentImage:  r.Deps.SubagentImage,
    ...
}
// No fallback at the task site — SubagentImage was pre-resolved in main.go
```

**Replacement pattern for all five sites:**
```go
// Replace the 2-line fallback (or the single r.Deps.SubagentImage at task) with:
opts := podjob.BuildOptions{
    ...
    SubagentImage: resolveImage(project, "<level>", r.HelmProviderDefaults),
    // task site uses r.Deps.HelmProviderDefaults instead:
    // SubagentImage: resolveImage(project, "task", r.Deps.HelmProviderDefaults),
    ...
}
```

**BillingHalt dispatch gate — insert after CheckRejected, before pool acquire:**

Copy from task_controller.go BudgetExceeded gate (lines 334-348):
```go
// Step 4: Budget gate (lines 334-348) — EXACT position model for BillingHalt gate:
if project.Status.Phase == "BudgetExceeded" && !budget.IsBypassed(project, time.Now()) {
    patch := client.MergeFrom(task.DeepCopy())
    meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
        Type:               "BudgetBlocked",
        Status:             metav1.ConditionTrue,
        Reason:             tideprojectv1alpha1.ConditionBudgetExceeded,
        Message:            "Project budget cap exceeded; task dispatch halted",
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, task, patch); err != nil {
        return taskGateResult{}, err
    }
    return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
}
```

BillingHalt gate at same position (park with 30s requeue, not empty result):
```go
if checkBillingHalt(project) {
    // D-05: park — do not fail; requeue to re-check after credits are refilled.
    return <haltResult>, nil  // ctrl.Result{RequeueAfter: 30 * time.Second}
}
```

---

### `internal/credproxy/server.go` — add ModifyResponse

**Analog:** `internal/credproxy/server.go` — Director hook pattern (lines 125-136)

**Imports to add** (lines 19-27 currently):
```go
import (
    "bytes"
    "io"
    // net/http and net/http/httputil already imported
    ...
)
```

**Current Director hook pattern (lines 124-136) — ModifyResponse follows the same rp.* assignment shape:**
```go
rp := httputil.NewSingleHostReverseProxy(upstream)
origDirector := rp.Director
rp.Director = func(req *http.Request) {
    origDirector(req)
    req.Host = upstream.Host
    req.Header.Set("Authorization", "Bearer "+p.RealAPIKey)
    req.Header.Set("x-api-key", p.RealAPIKey)
}
```

**New ModifyResponse assignment — add immediately after rp.Director block, before the http.HandlerFunc return:**
```go
// Add after rp.Director assignment (line 136), before "return http.HandlerFunc...":
rp.ModifyResponse = func(resp *http.Response) error {
    if resp.StatusCode != http.StatusBadRequest {
        return nil
    }
    body, err := io.ReadAll(resp.Body)
    resp.Body.Close()
    if err != nil {
        resp.Body = io.NopCloser(bytes.NewReader(nil))
        return nil
    }
    resp.Body = io.NopCloser(bytes.NewReader(body)) // MUST restore before return
    if isCreditExhaustion(body) {
        // Pass-through: Claude Code receives the 400 and exits non-zero.
        // The reconciler backstop reads "credit balance" from stderr/Reason.
        // Log here for operator visibility in pod logs.
        log.Printf("billing-halt: Anthropic credit-exhaustion 400 detected at credproxy %s", // stdlib log — credproxy must NOT import controller-runtime (providerfirewall boundary)
            "statusCode", resp.StatusCode)
    }
    return nil
}

// isCreditExhaustion returns true for Anthropic HTTP 400 + "credit balance" body.
// Conservative: substring match on lowercased body, not exact message text.
func isCreditExhaustion(body []byte) bool {
    return strings.Contains(strings.ToLower(string(body)), "credit balance")
}
```

Note: `logf` is already imported via `logf "sigs.k8s.io/controller-runtime/pkg/log"` in the controller package, but credproxy must not import controller-runtime. Use `log.Println` or a Logger field on Proxy — check what logging credproxy currently uses. If no Logger field exists, add one or use `fmt.Fprintf(os.Stderr, ...)` as the fallback.

---

### `api/v1alpha1/shared_types.go` — add BillingHalt constants

**Analog:** Phase 2 condition vocab block (lines 45-72), specifically:
```go
// ConditionBudgetExceeded — Project absolute cost cap has been hit
// (Plan 10 sets; TaskReconciler halts on this condition).
ConditionBudgetExceeded = "BudgetExceeded"

// ReasonCapHit — a Task or Project cap (tokens, iterations, wall-clock) was
// reached; TaskReconciler marks the Task failed with this reason.
ReasonCapHit = "CapHit"
```

**New constants — follow same comment density and naming:**
```go
// Phase 13 condition + reason vocabulary — provider billing halt (HALT-01).
// A provider credit-exhaustion 400 halts all new dispatch project-wide until
// the operator refills credits and runs `tide resume`. BillingHalt is set on
// the Project status by any reconciler that classifies the billing-failure
// class from a failed Job envelope.
const (
    // ConditionBillingHalt — provider returned a credit-exhaustion 400;
    // new dispatch is halted project-wide until the operator refills credits
    // and runs `tide resume`. Phase 13 HALT-01.
    ConditionBillingHalt = "BillingHalt"

    // ReasonCreditBalanceTooLow — Anthropic API returned HTTP 400 with
    // "credit balance" in the error body. Set on Project by the reconciler
    // billing classifier.
    ReasonCreditBalanceTooLow = "CreditBalanceTooLow"
)
```

---

### `cmd/tide/resume.go` — extend resumeRun to clear BillingHalt

**Analog:** lines 78-82 — annotation clear via MergeFrom + Patch:
```go
patch := client.MergeFrom(proj.DeepCopy())
proj.SetAnnotations(gates.ConsumeReject(&proj))
if err := c.Patch(ctx, &proj, patch); err != nil {
    return fmt.Errorf("patch project: %w", err)
}
```

**New BillingHalt clear — add immediately after the annotation Patch, before the `if !retryFailed` check:**
```go
// Clear BillingHalt unconditionally (D-06: operator chose recovery by invoking resume).
patch2 := client.MergeFrom(proj.DeepCopy())
meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha1.ConditionBillingHalt)
if err := c.Status().Patch(ctx, &proj, patch2); err != nil {
    return fmt.Errorf("patch status (clear BillingHalt): %w", err)
}
```

Note: `meta` is already imported as `"k8s.io/apimachinery/pkg/api/meta"` at line 46 of resume.go. `meta.RemoveStatusCondition` is the symmetric pair to `meta.SetStatusCondition` used throughout.

---

### `charts/tide/templates/deployment.yaml` — drop `--subagent-image` arg

**Change:** Remove line 30 entirely:
```yaml
# LINE 30 — DELETE this line:
- --subagent-image={{ .Values.images.stubSubagent.repository }}:{{ .Values.images.stubSubagent.tag | default .Chart.AppVersion }}
```

**Surviving channel** (line 45 — no change, becomes sole image-default path):
```yaml
- name: CLAUDE_SUBAGENT_IMAGE
  value: "{{ .Values.images.claudeSubagent.repository }}:{{ .Values.images.claudeSubagent.tag | default .Chart.AppVersion }}"
```

**Add comment above the CLAUDE_SUBAGENT_IMAGE env block explaining the new resolution chain:**
```yaml
# Subagent image resolution chain (Phase 13 DISPATCH-01/02):
#   Project.Spec.Subagent.Levels.<level>.Image   ← per-level CRD override
#   Project.Spec.Subagent.Image                  ← per-Project CRD override
#   CLAUDE_SUBAGENT_IMAGE env (this line)         ← Helm chart default
# To use the stub subagent (tests/CI), set:
#   subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:...
# The images.stubSubagent.* values keys are retained for reference but are
# no longer injected as a --subagent-image flag.
```

---

### `test/integration/kind/suite_test.go` — stub opt-in in helmControllerArgs

**Analog:** helmControllerArgs function (lines 469-506):
```go
func helmControllerArgs(chartDir string, rolloutNonce string) []string {
    return []string{
        "upgrade", "--install", "tide", chartDir,
        ...
        "--set", "images.stubSubagent.tag=test",
        ...
    }
}
```

**Change:** Add `--set subagent.defaults.image=...stub...` to override the helm default image with the stub for test installs:
```go
// Add to helmControllerArgs return slice — replaces the implicit --subagent-image flag:
"--set", fmt.Sprintf("subagent.defaults.image=%s:%s",
    "ghcr.io/jsquirrelz/tide-stub-subagent", "test"),
```

The existing `"--set", "images.stubSubagent.tag=test"` line can stay for chart rendering completeness but is no longer the dispatch-driver.

---

### `test/integration/kind/projects_pvc_test.go` — new chart contract assertions

**Analog:** existing tests in `test/integration/kind/projects_pvc_test.go` (lines 48-60) — read YAML fixture, assert field presence:
```go
func TestThreeTaskWaveFixtureIncludesProjectsPVC(t *testing.T) {
    docs := readKindYAMLDocs(t, filepath.Join("testdata", "three-task-wave.yaml"))
    var pvc *kindYAMLDoc
    for i := range docs {
        doc := &docs[i]
        if doc.Kind == "PersistentVolumeClaim" && doc.Metadata.Name == "tide-projects" {
            pvc = doc
            break
        }
    }
    ...
}
```

**New tests — read rendered deployment.yaml directly from chartDir:**
```go
// TestHelmDeploymentTemplateDropsSubagentImageFlag reads the rendered
// deployment.yaml and asserts --subagent-image= does NOT appear in args.
func TestHelmDeploymentTemplateDropsSubagentImageFlag(t *testing.T) {
    deployYAML := renderDeploymentTemplate(t) // helm template or read fixture
    if bytes.Contains(deployYAML, []byte("--subagent-image=")) {
        t.Error("deployment.yaml must not contain --subagent-image= (Phase 13 DISPATCH-02)")
    }
}

// TestHelmDeploymentTemplateHasCLAUDE_SUBAGENT_IMAGE asserts the surviving
// image-default channel is present.
func TestHelmDeploymentTemplateHasCLAUDE_SUBAGENT_IMAGE(t *testing.T) {
    deployYAML := renderDeploymentTemplate(t)
    if !bytes.Contains(deployYAML, []byte("CLAUDE_SUBAGENT_IMAGE")) {
        t.Error("deployment.yaml must contain CLAUDE_SUBAGENT_IMAGE env var (Phase 13 DISPATCH-02)")
    }
}
```

Look at `TestHelmDeploymentTemplateRendersManagerPodAnnotations` in the same package for the `renderDeploymentTemplate` helper pattern (helm template command + output parse).

---

### `internal/controller/dispatch_helpers_test.go` — new resolveImage + checkBillingHalt tests

**Analog:** `internal/controller/dispatch_helpers_test.go` — TestResolveProviderPerLevelWins (lines 27-48):
```go
func TestResolveProviderPerLevelWins(t *testing.T) {
    project := &tideprojectv1alpha1.Project{
        Spec: tideprojectv1alpha1.ProjectSpec{
            Subagent: tideprojectv1alpha1.SubagentConfig{
                Model: "claude-sonnet-4-6",
                Levels: tideprojectv1alpha1.LevelOverrides{
                    Milestone: &tideprojectv1alpha1.LevelConfig{Model: "claude-opus-4-7"},
                },
            },
        },
    }
    defaults := ProviderDefaults{Models: map[string]string{"milestone": "claude-haiku-4-5"}}
    spec := ResolveProvider(project, "milestone", defaults)
    if spec.Model != "claude-opus-4-7" {
        t.Errorf("Model = %q, want %q", spec.Model, "claude-opus-4-7")
    }
}
```

**New tests — same structure, replace Model with Image:**
```go
func TestResolveImage_LevelWins(t *testing.T) {
    project := &tideprojectv1alpha1.Project{
        Spec: tideprojectv1alpha1.ProjectSpec{
            Subagent: tideprojectv1alpha1.SubagentConfig{
                Image: "ghcr.io/project-default",
                Levels: tideprojectv1alpha1.LevelOverrides{
                    Plan: &tideprojectv1alpha1.LevelConfig{Image: "ghcr.io/level-override"},
                },
            },
        },
    }
    defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
    if got := resolveImage(project, "plan", defaults); got != "ghcr.io/level-override" {
        t.Errorf("resolveImage = %q, want level override", got)
    }
}

func TestResolveImage_ProjectDefaultWinsOverHelm(t *testing.T) { ... }
func TestResolveImage_HelmDefaultFallback(t *testing.T) { ... }
func TestResolveImage_NilProject_ReturnsHelmDefault(t *testing.T) { ... }
```

---

### `cmd/tide/resume_test.go` — BillingHalt clear test case

**Analog:** TestResumeClearsRejectAnnotation (lines 40-54):
```go
func TestResumeClearsRejectAnnotation(t *testing.T) {
    p := makeRejectedProject("my-project", "stopped")
    c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()
    if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
        t.Fatalf("resumeRun: %v", err)
    }
    var got tidev1alpha1.Project
    if err := c.Get(context.Background(), ..., &got); err != nil { ... }
    if v, ok := got.Annotations["tideproject.k8s/reject"]; ok {
        t.Errorf("expected reject annotation cleared; still present %q", v)
    }
}
```

**New test — same fake.Client + resumeRun pattern:**
```go
func makeBillingHaltedProject(name string) *tidev1alpha1.Project {
    p := &tidev1alpha1.Project{
        ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
        Spec:       tidev1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
    }
    meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
        Type:               tidev1alpha1.ConditionBillingHalt,
        Status:             metav1.ConditionTrue,
        Reason:             tidev1alpha1.ReasonCreditBalanceTooLow,
        LastTransitionTime: metav1.Now(),
    })
    return p
}

func TestResumeClearsBillingHalt(t *testing.T) {
    p := makeBillingHaltedProject("halt-project")
    c := fake.NewClientBuilder().WithScheme(testScheme(t)).
        WithObjects(p).WithStatusSubresource(p).Build()
    if err := resumeRun(context.Background(), c, "default", "halt-project", false, nil); err != nil {
        t.Fatalf("resumeRun: %v", err)
    }
    var got tidev1alpha1.Project
    _ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "halt-project"}, &got)
    for _, cond := range got.Status.Conditions {
        if cond.Type == tidev1alpha1.ConditionBillingHalt && cond.Status == metav1.ConditionTrue {
            t.Error("BillingHalt condition should be cleared after resume")
        }
    }
}
```

---

### `internal/credproxy/server_test.go` — billing 400 ModifyResponse test

**Analog:** TestProxyHandler_RejectsBadBearerWith401 (lines 53-79) — fake upstream, httptest.NewServer, assert response:
```go
func buildTestProxy(t *testing.T, signingKey []byte, taskUID string) (*Proxy, *http.Request) {
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    ...
}
```

**New test — fake upstream returns billing 400, assert body passes through unchanged and isCreditExhaustion classifies correctly:**
```go
func TestIsCreditExhaustion_Classifies400Body(t *testing.T) {
    billingBody := `{"type":"error","error":{"type":"invalid_request_error",` +
        `"message":"Your credit balance is too low to access the Anthropic API."}}`
    if !isCreditExhaustion([]byte(billingBody)) {
        t.Error("expected isCreditExhaustion=true for credit balance body")
    }
    // Unrelated 400 must not match:
    if isCreditExhaustion([]byte(`{"type":"error","error":{"message":"invalid model"}}`)) {
        t.Error("expected isCreditExhaustion=false for non-billing 400")
    }
}

func TestModifyResponse_BillingBodyPassesThroughUnchanged(t *testing.T) {
    // Build a fake upstream that returns HTTP 400 with credit balance body.
    billingBody := `{"error":{"message":"Your credit balance is too low..."}}`
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        _, _ = w.Write([]byte(billingBody))
    }))
    defer upstream.Close()
    // ... wire a Proxy against upstream, make a valid signed request,
    // assert response body == billingBody (body not consumed/lost by ModifyResponse).
}
```

---

## Shared Patterns

### Condition management (SetStatusCondition / RemoveStatusCondition)

**Source:** Throughout `internal/controller/project_controller.go`, `cmd/tide/resume.go` (lines 96-102), `internal/controller/task_controller.go` (lines 337-344)
**Apply to:** `dispatch_helpers.go` (checkBillingHalt setter), `cmd/tide/resume.go` (BillingHalt clear), `api/v1alpha1/shared_types.go` (new constants)
```go
// Set pattern (all reconcilers):
patch := client.MergeFrom(obj.DeepCopy())
meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionXxx,
    Status:             metav1.ConditionTrue,
    Reason:             tideprojectv1alpha1.ReasonXxx,
    Message:            "human-readable text for kubectl describe",
    LastTransitionTime: metav1.Now(),
})
_ = r.Client.Status().Patch(ctx, obj, patch)

// Clear pattern (resume.go lines 78-82 shape):
patch2 := client.MergeFrom(obj.DeepCopy())
meta.RemoveStatusCondition(&obj.Status.Conditions, tidev1alpha1.ConditionBillingHalt)
_ = c.Status().Patch(ctx, obj, patch2)
```

### Park-not-fail requeue pattern

**Source:** `internal/controller/task_controller.go` lines 326-332 (checkParentApproval hold — 5s requeue)
**Apply to:** All five reconciler BillingHalt dispatch gates
```go
// Park: return halt without changing Status.Phase, requeue after 30s.
// (BillingHalt uses 30s; ApprovalHold uses 5s — billing dry-out resolves
// on operator action, not a transient informer lag.)
return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
```

### MergeFrom + status patch

**Source:** `cmd/tide/resume.go` lines 78-81; `internal/controller/task_controller.go` lines 336-347
**Apply to:** Every new status write
```go
patch := client.MergeFrom(obj.DeepCopy())
// mutate obj...
if err := c.Status().Patch(ctx, obj, patch); err != nil {
    return fmt.Errorf("patch status: %w", err)
}
```

### Provider firewall boundary

**Source:** `tools/analyzers/providerfirewall/analyzer.go` (excludes `internal/credproxy/`)
**Apply to:** `isCreditExhaustion` in `internal/credproxy/server.go`; billing string classification in reconcilers (`strings.Contains(lower, "credit balance")`) is pure string ops — no SDK import, legal in controller package.

---

## No Analog Found

All files have close analogs. No entries.

---

## Metadata

**Analog search scope:** `internal/controller/`, `internal/credproxy/`, `api/v1alpha1/`, `cmd/tide/`, `charts/tide/templates/`, `test/integration/kind/`, `cmd/manager/`
**Files scanned:** 16 source files read directly
**Pattern extraction date:** 2026-06-11
