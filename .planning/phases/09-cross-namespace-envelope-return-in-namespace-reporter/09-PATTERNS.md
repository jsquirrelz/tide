# Phase 9: Cross-namespace envelope return (in-namespace reporter) - Pattern Map

**Mapped:** 2026-06-08
**Files analyzed:** 13 (5 new, 8 modified)
**Analogs found:** 13 / 13

> **Execution model is LOCKED to Option C** (CONTEXT.md line 46-49: Manager-spawned reader Job). RESEARCH.md *recommended* B-main (second main container) but the user resolved the SA-sharing fork (Pitfall 2 / Open Q1 / ASSUMPTION A2) in favor of **a separate short-lived reader Job in the project namespace with its own `tide-reporter` SA**. Where RESEARCH.md and CONTEXT.md diverge, this map follows CONTEXT.md. The "second main container + sentinel file" mechanics are NOT in scope — the reader Job runs *after* the dispatch Job completes (Manager-spawned on Job-completion watch), reads `out.json` from the namespace PVC, materializes children, exits.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/tide-reporter/main.go` (NEW) | cmd-main / reporter | file-I/O → K8s-API | `cmd/tide-push/main.go` (cmd-main shape) + `cmd/manager/main.go` (in-cluster client) | role-match |
| `internal/reporter/materialize.go` (NEW, lifted) | utility / materializer | transform → CRUD | `internal/controller/dispatch_helpers.go` MaterializeChildCRDs + childrenAlreadyMaterialized + childKindAllowlist | exact (move) |
| `internal/controller/reporter_jobspec.go` (NEW) | jobspec builder | request-response | `internal/controller/push_helpers.go` buildCloneJob/buildPushJob | exact |
| reader-Job spawn in `handle*JobCompletion` (MODIFIED) | controller | event-driven | `project_controller.go` clone/push Job spawn + `handleProjectJobCompletion` | exact |
| `charts/tide/templates/reporter-rbac.yaml` (NEW) | config / RBAC | n/a | `charts/tide/templates/push-rbac.yaml` + `per-namespace-rolebinding.yaml` | exact |
| `examples/projects/medium/per-namespace-resources.yaml` (MODIFIED) | config / RBAC | n/a | the `tide-push` SA+Role+RoleBinding block already in that file | exact |
| `internal/controller/{project,milestone,phase,plan}_controller.go` handle*JobCompletion (MODIFIED) | controller | event-driven | `project_controller.go:790-829` handleProjectJobCompletion | exact |
| `internal/subagent/anthropic/pricing.go` (NEW) | utility / price-table | transform | `internal/subagent/anthropic/stream_parser.go` (token assembly) | role-match |
| `internal/subagent/anthropic/subagent.go` (MODIFIED) | service / runner | transform | self (assembly point at :269-276) | exact |
| `internal/dispatch/podjob/backend.go` PodStatusEnvelopeReader (MODIFIED) | reader | request-response | self (:151-178) | exact |
| `internal/controller/task_controller.go` buildEnvelopeIn (MODIFIED) | controller | request-response | self (:1043-1090) + backend.go ReadPrompt traversal defense | exact |
| `cmd/claude-subagent/main.go` + `cmd/stub-subagent/main.go` (MODIFIED) | cmd-main | file-I/O | self (termination-write at claude:122-133, stub:204-210) | exact |
| `pkg/dispatch/envelope.go` TerminationStub (NEW, resurrect) | model / DTO | n/a | EnvelopeOut/Usage struct (:130-249) | role-match |

---

## Pattern Assignments

### `cmd/tide-reporter/main.go` (NEW — cmd-main / reporter)

**Analogs:** `cmd/tide-push/main.go` (cmd-main flag/run/exit shape) + `cmd/manager/main.go` (in-cluster controller-runtime client wiring).

**cmd-main shape — copy from `cmd/tide-push/main.go:133-201`:**
```go
func main() {
    fs := flag.NewFlagSet("tide-reporter", flag.ExitOnError)
    // reader Job needs: --workspace, --project-uid, --parent-uid, --parent-name,
    //                   --parent-kind, --parent-namespace, --task-uid (envelope key)
    workspace := fs.String("workspace", "/workspace", "workspace root")
    // ... one fs.String per arg, mirroring tide-push's flag block ...
    if err := fs.Parse(os.Args[1:]); err != nil {
        fmt.Fprintf(os.Stderr, "tide-reporter: flag parse: %v\n", err)
        os.Exit(2)
    }
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer cancel()
    os.Exit(run(ctx, cfg, os.Stdout, os.Stderr))   // testable run() seam — tide-push pattern
}
```
The `cfg`-struct-by-value + `run(ctx, cfg, stdout, stderr) int` testable seam is the project's cmd-main idiom (tide-push:79-201, claude-subagent:79-98). Reuse it verbatim so `main_test.go` drives `run()` without `os.Args`.

**In-cluster K8s client — copy the scheme+config wiring from `cmd/manager/main.go` (scheme registration at :32-34, 42):**
```go
scheme := runtime.NewScheme()
utilruntime.Must(clientgoscheme.AddToScheme(scheme))
utilruntime.Must(tidev1alpha1.AddToScheme(scheme))   // tide CRDs
cfg, err := config.GetConfig()                        // sigs.k8s.io/controller-runtime/pkg/client/config — honors in-cluster SA token
c, err := client.New(cfg, client.Options{Scheme: scheme})
```
RESEARCH §B-main line 104 specifies exactly this (`config.GetConfig()` + `client.New`). The reporter Job's pod runs as `tide-reporter` SA so the auto-mounted token carries the create+get verbs.

**Reading out.json — copy `FilesystemEnvelopeReader.ReadOut` from `backend.go:94-105` (pure filesystem, no K8s deps, import-safe from cmd):**
```go
path := filepath.Join(workspaceRoot, "envelopes", taskUID, "out.json")  // reader is IN the project ns; mount has subPath {uid}/workspace at /workspace
data, err := os.ReadFile(path)
var out pkgdispatch.EnvelopeOut
_ = json.Unmarshal(data, &out)
```
Note divergence: the reader Job mounts the PVC with `subPath: {project-uid}/workspace` at `/workspace` (same as subagent/push Jobs), so the path is `/workspace/envelopes/<taskUID>/out.json` — NOT the Manager's `/workspaces/{uid}/workspace/...` layout. This is the whole point of #11/#12: the read is now SAME-namespace.

**Parent ownerRef resolution — Get the parent by name to obtain its live UID, then materialize.** Pass `--parent-name/-namespace/-kind` as args (Manager knows these at spawn time, per RESEARCH line 106). The reader does `c.Get(ctx, key, &parentObj)` then passes the typed parent into the lifted `MaterializeChildCRDs` (which calls `owner.EnsureOwnerRef` internally).

**Exit-code map — mirror `cmd/tide-push/main.go:109-120` const block** (0 success, 2 invariant/bad-args, etc.) so the Manager's reader-Job completion watch can classify failure.

> **Divergence from tide-push:** tide-push writes a result envelope to `/dev/termination-log` AND the PVC. The reporter Job does NOT need to carry usage/git status — that still rides the *dispatch* Job's termination message (CONTEXT line 49). The reporter's only output is the created CRs; its termination message (if any) is just a tiny success/failure marker for the Manager's reader-Job watch.

---

### `internal/reporter/materialize.go` (NEW — utility, lifted from controller)

**Analog (move target):** `internal/controller/dispatch_helpers.go:80-86, 236-377`.

Per RESEARCH §Materialization Relocation + Open Q4: `MaterializeChildCRDs`, `childKindAllowlist`, and `childrenAlreadyMaterialized` move into a package `cmd/tide-reporter` can import. **`internal/reporter` is the recommended home** (cmd may import internal; the moved code needs only `api/v1alpha1` + `internal/owner` + `pkg/dispatch` — all import-safe, no `internal/controller` back-edge).

**Move `MaterializeChildCRDs` verbatim — `dispatch_helpers.go:304-377`.** The Kind-allowlist pre-flight (:309-313), the typed-unmarshal switch (:315-360), `obj.SetNamespace(parent.GetNamespace())` (:363), `owner.EnsureOwnerRef(obj, parent, scheme)` (:365), and the `IsAlreadyExists`-is-success guard (:369-374) all transfer unchanged. The Task-prompt wiring **moves with it** (:341-349):
```go
case "Task":
    tk := &tideprojectv1alpha1.Task{}
    json.Unmarshal(child.Spec.Raw, &tk.Spec)
    tk.Spec.PromptPath = child.SourcePath   // defect #10b stamp — stays at the create-site
    obj = tk
```

**Move `childrenAlreadyMaterialized` verbatim — `dispatch_helpers.go:236-287`.** The cascade-9/10/11 lesson is load-bearing and translates intact: the guard matches by **spec-parent-ref** (`Spec.ProjectRef`/`MilestoneRef`/`PhaseRef`/`PlanRef`), with `metav1.IsControlledBy` as fallback — NOT ownerRef, because the reporter sets specRef synchronously at create time while ownerRef is set async by the child's reconciler. Pitfall 3 (RESEARCH line 259): this guard MUST live at the create-site, so it goes with `MaterializeChildCRDs` into the reporter — leaving a copy in the Manager would double-author on re-dispatch.

**`owner.EnsureOwnerRef` is import-safe** — confirmed `internal/owner/owner.go:50` takes `(child, parent metav1.Object, scheme *runtime.Scheme)` and only imports `metav1` + `runtime` + `controllerutil`. Same-namespace is enforced (:57-61) — sound because the reader Job runs in the parent's namespace.

> **Divergence:** the existing `dispatch_helpers_test.go` tests for these three functions move to `internal/reporter` (RESEARCH Wave-0 gap: "move existing dispatch_helpers tests"). The runner-side mirror `childKindAllowlist` in `anthropic/subagent.go:334` STAYS (defense-in-depth — RESEARCH line 178).

---

### `internal/controller/reporter_jobspec.go` (NEW — jobspec builder)

**Analog:** `internal/controller/push_helpers.go:259-319` (`buildCloneJob`) — the closest match: a short-lived, single-container, PVC-mounting, deterministic-named utility Job the controller spawns in the *project* namespace.

**Copy the builder skeleton from `buildCloneJob`:**
```go
func buildReporterJob(parent metav1.Object, project *tidev1alpha1.Project, pvcName, taskUID string, opts ReporterOptions, scheme *runtime.Scheme) *batchv1.Job {
    args := []string{
        "--workspace=/workspace",
        "--project-uid=" + string(project.UID),
        "--task-uid=" + taskUID,                       // keys the out.json path
        "--parent-name=" + parent.GetName(),
        "--parent-namespace=" + parent.GetNamespace(),
        "--parent-kind=" + <Kind>,
    }
    job := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("tide-reporter-%s-%d", parentUID, attempt),  // deterministic; AlreadyExists = idempotent
            Namespace: project.Namespace,                                        // PROJECT namespace (push_helpers:191)
        },
        Spec: batchv1.JobSpec{
            BackoffLimit:            ptr(int32(2)),        // push uses 2; reporter create is idempotent so retry-safe
            TTLSecondsAfterFinished: ptr(int32(300)),     // push_helpers:195
            Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
                RestartPolicy:      corev1.RestartPolicyNever,   // push_helpers:198
                ServiceAccountName: "tide-reporter",             // NEW SA (NOT tide-push, NOT tide-subagent)
                Volumes:            []corev1.Volume{ /* PVC, subPath {uid}/workspace */ },
                Containers:         []corev1.Container{ /* image=opts.ReporterImage, Args, VolumeMounts */ },
            }},
        },
    }
    _ = owner.EnsureOwnerRef(job, parent, scheme)   // push_helpers:316 — same-namespace, cascade-delete
    return job
}
```

**PVC subPath mount — copy `push_helpers.go:157-163` / `buildCloneJob` :302-308 exactly:**
```go
VolumeMounts: []corev1.VolumeMount{{
    Name: "project-workspace", MountPath: "/workspace",
    SubPath: fmt.Sprintf("%s/workspace", project.UID),   // SAME subPath the subagent wrote out.json into
}},
```
This is the load-bearing fix: the reader Job mounts the *same* per-project subPath the dispatch Job used, so it reads the same `out.json`. No cross-namespace read.

> **Divergences from buildCloneJob:** (1) NO `EnvFrom` git-creds Secret (reporter needs no PAT). (2) SA is `tide-reporter`, not `tide-push`. (3) The reporter image is a NEW Helm value `images.tideReporter.repository:tag` (mirror how `opts.TidePushImage` threads through). (4) The security context should mirror the hardened subagent/credproxy containers (`jobspec.go:368-375`: RunAsNonRoot, drop ALL caps) — buildCloneJob omits these, but the reporter is a trusted writer and should set them.

---

### Manager change: spawn the reader Job on dispatch-Job completion (MODIFIED controllers)

**Analog:** `project_controller.go` already spawns clone/push Jobs and watches `Owns(&batchv1.Job{})` (:1028). The completion handler `handleProjectJobCompletion:790-829` is the exact site to edit.

**Current handler (`project_controller.go:790-829`) reads + materializes inline:**
```go
envOut, err = r.EnvReader.ReadOut(ctx, string(project.UID), string(project.UID))   // ← cross-ns PVC read (BROKEN #12)
...
if !already {
    MaterializeChildCRDs(ctx, r.Client, r.Scheme, project, envOut.ChildCRDs)        // ← moves to reporter
}
```

**New shape (Option C):** on dispatch-Job completion, the handler (1) reads the **tiny status** from the dispatch Job's termination message via the updated `PodStatusEnvelopeReader` for usage/git/exitCode/reason, (2) **spawns the reader Job** via `buildReporterJob` + `client.Create` (AlreadyExists = idempotent, exactly like the clone/push spawn at the existing dispatch sites), and (3) returns — children arrive via the existing `Owns(&Milestone{})` / `Owns(&Phase{})` / etc. watch (:1029) which re-enqueues the parent. **Drop the `MaterializeChildCRDs` + `childrenAlreadyMaterialized` calls from all four handlers** (they now live in the reporter).

**Per-handler edit list** (RESEARCH §Materialization Relocation line 186): `project_controller.go:handleProjectJobCompletion` (~:790), and the matching handlers in `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`. Each keeps its `Owns(child)` watch wiring and its `checkComplete`/roll-up logic — only the read+materialize block changes to read-tiny-status + spawn-reader.

> **Divergence / new watch:** RESEARCH §Option C line 133 notes "a new dispatch site + its own completion watch." The Manager now watches *two* Job kinds per level boundary — the dispatch Job AND the reader Job. The reader-Job completion is only needed for failure surfacing (reporter exit≠0 → log/condition); the children themselves are observed via the CRD `Owns()` watch. Keep `Owns(&batchv1.Job{})` (already present) and discriminate dispatch-vs-reporter Jobs by a label (e.g. `tideproject.k8s/role: reporter`).

---

### `charts/tide/templates/reporter-rbac.yaml` (NEW — RBAC config)

**Analogs:** `charts/tide/templates/push-rbac.yaml` (the SA+Role+RoleBinding triple in `.Release.Namespace`) + `per-namespace-rolebinding.yaml` (the `range .Values.projectNamespaces` per-namespace fan-out).

**Copy the three-doc structure from `push-rbac.yaml:1-61`**, swapping the rule:
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tide-reporter
  namespace: {{ .Release.Namespace }}
  labels: {{- include "tide.labels" . | nindent 4 }}
automountServiceAccountToken: true
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: tide-reporter
  namespace: {{ .Release.Namespace }}
  labels: {{- include "tide.labels" . | nindent 4 }}
rules:
  # Least-privilege: create child CRs + get parent for ownerRef resolution. (RESEARCH §Per-Namespace RBAC line 147-160)
  - apiGroups: ["tideproject.k8s"]
    resources: ["milestones", "phases", "plans", "tasks", "waves"]
    verbs: ["create", "get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding   # bind tide-reporter SA → tide-reporter Role (push-rbac.yaml:46-61 shape)
```

**Per-namespace fan-out — mirror `per-namespace-rolebinding.yaml:19-37`'s `{{- range $ns := .Values.projectNamespaces }}`** so the SA+Role+RoleBinding render in each opted-in project namespace (the reporter Job runs in the project namespace, so the SA must exist there — same reason push-rbac documents at :15-23).

> **Divergence from push-rbac:** push grants `secrets get` (apiGroup `""`); reporter grants `create,get` on the five tide CRD Kinds (apiGroup `tideproject.k8s`). NO list/watch/update/delete/patch, NO `projects` (human-applied), NO secrets, NO core resources (RESEARCH line 162). **SOT discipline (CLAUDE.md chart-is-fixed-contract):** author the source under the chart's source tree; `hack/helm/augment-tide-chart.sh` mirrors it into `charts/tide/templates/` — do NOT hand-edit the rendered template.

---

### `examples/projects/medium/per-namespace-resources.yaml` (MODIFIED — RBAC)

**Analog:** the `tide-push` SA+Role+RoleBinding block already in this file (`per-namespace-resources.yaml:71-116`).

Add a parallel `tide-reporter` SA+Role+RoleBinding block in `tide-sample-medium`, copying the structure of the existing `tide-push` block (lines 71-116) but with the CRD-create rule from `reporter-rbac.yaml` above. This file already mirrors `tide-push` "because the controller's Jobs run in the project namespace" (file header :10-16) — the reporter Job is the same case. Use the same label set (`app.kubernetes.io/managed-by: tide-sample`, `tideproject.k8s/sample: medium`) as the existing blocks.

---

### `internal/subagent/anthropic/pricing.go` (NEW) + `subagent.go` (MODIFIED — cost #6)

**Root cause (RESEARCH §Cost Surfacing line 222):** `ParseStream` (`stream_parser.go:99-107`) populates `usage.InputTokens/OutputTokens/CacheReadTokens/CacheCreationTokens` but NEVER sets `EstimatedCostCents`. `subagent.go:269-276` assembles `EnvelopeOut{Usage: usage}` without computing cost → `budget.RollUpUsage` (`tally.go:51`) adds zero. Only the STUB hardcodes a cost (`stub-subagent/main.go:394` = 1).

**NEW `pricing.go` — per-model price table** (no analog in repo; closest sibling is `stream_parser.go`'s Usage-field-mapping role). Shape per RESEARCH line 227:
```go
type modelPrice struct{ inputCentsPerMTok, outputCentsPerMTok, cacheReadCentsPerMTok, cacheWriteCentsPerMTok int64 }
var priceTable = map[string]modelPrice{
    // Claude Haiku (medium sample): $1/M in, $5/M out, cacheWrite 1.25× in, cacheRead 0.1×in.
    // cents/MTok: input=100, output=500, cacheWrite=125, cacheRead=10. (RESEARCH line 231 — CONFIRM exact model ID + prices, ASSUMPTION A3)
}
func estimatedCostCents(model string, u pkgdispatch.Usage) int64 { /* ceil(sum of tokens×price/1e6); fail-loud or conservative default on table miss — Pitfall 4 */ }
```
**Key the table on the exact `in.Provider.Model` string** the medium sample resolves (Pitfall 4, RESEARCH line 265). Confirm against `examples/projects/medium/project.yaml` `subagent.model` + chart default.

**`subagent.go` edit — at the assembly point (`subagent.go:269-276`)**, after `ParseStream` returns `usage`, set the cost before building EnvelopeOut:
```go
usage, resultText, parseErr := ParseStream(stdout, eventsFile)
...
usage.EstimatedCostCents = estimatedCostCents(in.Provider.Model, usage)   // NEW — provider-specific, behind the Subagent interface (CLAUDE.md anti-pattern)
out := pkgdispatch.EnvelopeOut{ ..., Usage: usage, ... }
```
The price table lives in `internal/subagent/anthropic/` (CLAUDE.md: all Anthropic-specific code behind the `Subagent` interface — `subagent.go:17-37` package doc). `RollUpUsage` stays provider-agnostic and just accumulates `usage.EstimatedCostCents` (`tally.go:51`, unchanged).

> **Stub:** `stub-subagent` already sets `EstimatedCostCents: 1` (`main.go:394`) — keep it; the stub does not use the anthropic price table.

---

### `internal/dispatch/podjob/backend.go` PodStatusEnvelopeReader (MODIFIED — tiny-status reader)

**Analog:** self, `backend.go:151-178`.

`PodStatusEnvelopeReader.ReadOut` scans `ContainerStatuses` for `ContainerNameSubagent` (`backend.go:159`). Under V1 the subagent stops writing the termination message (reporter handles childCRDs; the *dispatch* Job's tiny status is what the Manager reads). **Change the container-name match to a new `ContainerNameReporter` constant... NO — per Option C the dispatch Job's tiny status still comes from the SUBAGENT container.** Re-read CONTEXT line 49: "The dispatch Job's termination message still carries the tiny status (usage/git/exitCode/reason)." So the subagent container KEEPS writing the tiny status to its termination message; only the *childCRDs* are dropped from it. The reader continues matching `ContainerNameSubagent` (`backend.go:159`) — **no container-name change needed under Option C** (unlike RESEARCH's B-main which had the reporter write it).

**What changes:** the subagent shims write a **tiny `TerminationStub`** (usage/git/exitCode/reason) instead of the full `EnvelopeOut` (which currently overflows 4KB with childCRDs — #11). The reader deserializes the tiny stub. Remove the `FilesystemEnvelopeReader` fallback for the cross-namespace planner path (`backend.go:174-176`) — it never worked cross-namespace (#12 root cause); keep it only for same-namespace local-test setups (RESEARCH line 200).

> **Divergence:** the verbose `result` + childCRDs are NO longer in the termination message — they stay on the namespace PVC (`out.json`, read by the reporter Job). The tiny stub is a strict subset of `EnvelopeOut` and deserializes into it with `ChildCRDs`/`Result` empty.

---

### `pkg/dispatch/envelope.go` TerminationStub (NEW — resurrect from #11)

**Analog:** the `EnvelopeOut`/`Usage` structs (`envelope.go:130-249`) + the reverted #11 `TerminationStub` (debug doc lines 712-737 — the writer half was sound; only the PVC-authoritative-read PREMISE was wrong).

Re-introduce a small `TerminationStub` type carrying only `Usage` + `Git.HeadSHA` + `ExitCode` + `Reason` (NO `ChildCRDs`, NO `Result`), plus `NewTerminationStub(out EnvelopeOut) TerminationStub` and a `TestNewTerminationStub_StaysSmall` (<4KB) test (RESEARCH line 199, Wave-0 gap line 337). Field names/tags mirror the `EnvelopeOut` subset: `Usage` (`envelope.go:219`), `Git.HeadSHA` (`:193`), `ExitCode`, `Reason`.

---

### `cmd/claude-subagent/main.go` + `cmd/stub-subagent/main.go` (MODIFIED — tiny status)

**Analog:** self — both write the FULL envelope to `/dev/termination-log` today (`claude-subagent/main.go:122-133` `writeEnvelope`; `stub-subagent/main.go:204-210` `writeTerminationMessage`).

**Change both `writeEnvelope` shims to marshal a `TerminationStub` (not the full `EnvelopeOut`) to the termination-log path:**
```go
// claude-subagent/main.go:126-131 currently:
data, _ := json.Marshal(out)                          // ← full envelope, overflows 4KB with childCRDs
_ = os.WriteFile(terminationLogPath, data, 0o644)
// becomes:
stub := pkgdispatch.NewTerminationStub(out)
data, _ := json.Marshal(stub)
_ = os.WriteFile(terminationLogPath, data, 0o644)
```
The full `out.json` write to the PVC (`harness.WriteEnvelopeOut` at claude:123, `writeEnvelope` at stub:189-202) is UNCHANGED — `out.json` on the PVC remains the audit artifact + the reporter Job's input (it carries `ChildCRDs`).

> **Stub-compat action item (RESEARCH §Stub Compatibility line 241):** confirm the stub sets `SourcePath` on its Task children (`dispatchPlannerSuccess:329-333` does NOT currently set `SourcePath` — only the real runner does at `anthropic/subagent.go:427`). The reporter copies `child.SourcePath → tk.Spec.PromptPath` (MinLength=1), so a missing `SourcePath` on stub Task children would fail `Create`. Add `SourcePath` to the stub's Task `ChildCRDSpec`, OR have the stub write the prompt artifact the executor reads. (Note: Option C does NOT require the sentinel-file write that RESEARCH's B-main needed.)

---

### `internal/controller/task_controller.go` buildEnvelopeIn (MODIFIED — #10b in-pod prompt read)

**Analog:** self (`task_controller.go:1043-1090`) + the path-traversal-defended `ReadPrompt` logic in `backend.go:107-141`.

**Current (broken cross-ns) — `task_controller.go:1064`:**
```go
prompt, err := r.Deps.PromptReader.ReadPrompt(ctx, string(project.UID), task.Spec.PromptPath)  // ← Manager reads ITS PVC — wrong namespace (#10b)
...
envIn := pkgdispatch.EnvelopeIn{ ..., Prompt: prompt, ... }
```

**Fix (RESEARCH §Prompt Read In-Pod line 204, recommend option 1):** the Manager STOPS reading the prompt. Remove the `r.Deps.PromptReader.ReadPrompt` call from `buildEnvelopeIn`; pass `PromptPath` (workspace-relative) on `EnvelopeIn` instead of `Prompt`. The IN-POD harness reads the prompt from its OWN namespace PVC.

**Move the traversal-defended read into the in-pod harness** — copy `FilesystemEnvelopeReader.ReadPrompt` (`backend.go:112-141`: empty-check, abs-reject, `..`-reject, `Clean`, base-prefix check, `childPromptFile.Spec.Prompt` decode) into the subagent harness / `anthropic.Run`. It is pure filesystem (no K8s deps), import-safe into cmd. The executor template still renders `{{.Prompt}}` — now from the in-pod-read content. Add `PromptPath` to `pkgdispatch.EnvelopeIn` (the CRD field `TaskSpec.PromptPath` MinLength=1 is unchanged — only WHO reads it moves, RESEARCH line 218).

> **Divergence:** `r.Deps.PromptReader` can be dropped from `TaskReconciler.Deps` once `buildEnvelopeIn` no longer calls it (Pitfall 5: removing the call is the fix; leaving it leaves the cross-ns bug live). The symmetric planner-level `BuildPlannerEnvelope` (`dispatch_helpers.go:181`) already passes `Prompt` directly (outcome prompt, not a PVC artifact) — no change there.

---

## Shared Patterns

### In-cluster controller-runtime client (cmd binary)
**Source:** `cmd/manager/main.go:32-34, 42` (scheme registration) + RESEARCH line 104 (`config.GetConfig()` + `client.New`).
**Apply to:** `cmd/tide-reporter/main.go`.
```go
scheme := runtime.NewScheme()
utilruntime.Must(clientgoscheme.AddToScheme(scheme))
utilruntime.Must(tidev1alpha1.AddToScheme(scheme))
cfg, _ := config.GetConfig()
c, _ := client.New(cfg, client.Options{Scheme: scheme})
```

### cmd-main testable seam
**Source:** `cmd/tide-push/main.go:133-201`, `cmd/claude-subagent/main.go:49-98`.
**Apply to:** `cmd/tide-reporter/main.go`.
`main()` parses flags into a by-value cfg struct + `signal.NotifyContext(SIGTERM,SIGINT)`, then `os.Exit(run(ctx, cfg, stdout, stderr))`. Tests drive `run()` directly.

### Short-lived utility Job in the project namespace
**Source:** `internal/controller/push_helpers.go:259-319` (buildCloneJob).
**Apply to:** `reporter_jobspec.go`.
Deterministic name (AlreadyExists = idempotent serialization), `Namespace: project.Namespace`, `RestartPolicy: Never`, `TTLSecondsAfterFinished: 300`, PVC `subPath: {project.UID}/workspace`, `owner.EnsureOwnerRef(job, parent, scheme)` for cascade-delete.

### Spec-ref idempotency guard at the create-site (cascade-9/10/11)
**Source:** `dispatch_helpers.go:236-287` (childrenAlreadyMaterialized).
**Apply to:** `internal/reporter/materialize.go` — the guard MOVES with `MaterializeChildCRDs` (Pitfall 3). Match by `Spec.{Project,Milestone,Phase,Plan}Ref` (synchronous, race-free) with `metav1.IsControlledBy` fallback (async). Never guard by ownerRef alone.

### Kind allowlist (T-308) — defense in depth
**Source:** `dispatch_helpers.go:80-86` (controller) + `anthropic/subagent.go:334-340` (runner mirror).
**Apply to:** the controller copy MOVES to the reporter; the runner mirror STAYS. CEL admission also guards regardless of creator (no change).

### Path-traversal defense on PromptPath
**Source:** `backend.go:112-141` (ReadPrompt) + `anthropic/subagent.go:358-431` (readChildCRDs EvalSymlinks/Lstat defense).
**Apply to:** the in-pod prompt read (#10b). abs-reject, `..`-reject, `filepath.Clean`, base-prefix containment check.

### Owner ref (same-namespace, cascade-delete)
**Source:** `internal/owner/owner.go:50-64` (EnsureOwnerRef).
**Apply to:** reporter materializer (child→parent) AND reporter Job spec (Job→parent). Import-safe; enforces same-namespace (Pitfall 23) — sound because the reader Job and the children all live in the parent's namespace.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | — | — | Every Phase-9 file has a close analog. The reader-Job binary is a composite of cmd-main (tide-push), in-cluster client (manager), filesystem read (FilesystemEnvelopeReader), and materialization (dispatch_helpers) — all existing. The price table is novel in shape but trivial; its assembly site (subagent.go:269) and field set (Usage:219-249) are established. |

## Metadata

**Analog search scope:** `cmd/` (tide-push, claude-subagent, stub-subagent, manager), `internal/controller/` (dispatch_helpers, push_helpers, task/project/milestone/phase/plan controllers), `internal/dispatch/podjob/` (backend, jobspec), `internal/subagent/anthropic/` (subagent, stream_parser), `internal/budget/` (tally), `internal/owner/`, `pkg/dispatch/` (envelope), `charts/tide/templates/` (push-rbac, serviceaccount-subagent, per-namespace-rolebinding), `examples/projects/medium/`.
**Files scanned:** 16 read in full + 4 grep-targeted.
**Pattern extraction date:** 2026-06-08
**Execution model:** Option C (Manager-spawned reader Job) — LOCKED in CONTEXT.md. RESEARCH.md's B-main mechanics (second main container, sentinel file) are explicitly NOT followed.
