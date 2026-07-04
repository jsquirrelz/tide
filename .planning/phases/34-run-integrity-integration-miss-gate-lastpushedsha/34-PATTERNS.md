# Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA - Pattern Map

**Mapped:** 2026-07-04
**Files analyzed:** 12 modified + 1 new
**Analogs found:** 13 / 13 (all changes land inside or beside a proven in-repo pattern)

Note: this phase is almost entirely modification-in-place — the "analog" for most files is an existing pattern *in the same file or a sibling*. Line numbers verified in-session against the current worktree.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog (pattern source) | Match Quality |
|---|---|---|---|---|
| `internal/controller/plan_controller.go` (modify: loop, retry, conflict→Failed) | controller/reconciler | event-driven (Job status → status patch) | #13b machine in `project_controller.go:668–884` | exact (same repo, same shape) |
| `internal/controller/boundary_push.go` (modify: cumulative set, D-02 gate, labels) | controller helper | request-response (List → Create Job) | `plannerInFlightCount` (`dispatch_helpers.go:304–329`) + existing `triggerBoundaryPush` | exact |
| `internal/controller/project_controller.go` (modify: SHA stamp, condition arms, envelope-reader generalization) | controller/reconciler | event-driven | its own success/failure arms `:734–823` | exact |
| `internal/controller/push_helpers.go` (modify: Job labels) | controller helper | config/build | planner-Job labels consumed at `dispatch_helpers.go:307` | exact |
| `pkg/git/integrate.go` (modify: merge-abort hygiene, typed conflict error) | utility (git CLI wrapper) | file-I/O / exec | its own merge loop `:94–107` | exact |
| `cmd/tide-push/main.go` (modify: flock, verify step, conflict classification, wave-success envelope) | Job binary | batch / exec | `classifyPushError` `:710–734`, `writePushEnvelope` `:662–703` | exact |
| `cmd/tide/resume.go` (modify: attempts reset + condition clear) | CLI command | request-response (K8s patches) | its own `resumeRun` annotation/status-patch sequence `:70–125` | exact |
| `api/v1alpha2/shared_types.go` (modify: new condition/reason consts) | API types | — | Phase 11/33 reason blocks `:197–212` | exact |
| `api/v1alpha2/plan_types.go` + `api/v1alpha1/plan_types.go` (modify: wave-integration attempts status, if chosen) | API types | — | `BoundaryPushStatus` (`project_types.go:289+` v1alpha2, `:271+` v1alpha1) + `IntegratedThroughWave` dual-version parity | exact |
| `internal/metrics/registry.go` (modify: integration-outcome counter) | config/metrics | — | `PushJobsTotal` `:173–179` | exact |
| `cmd/tide-push/main_test.go` (modify: verify/conflict unit cases) | test | request-response (in-process `run(cfg)` seam) | existing tests in same file | exact |
| `test/integration/kind/integration_miss_test.go` (NEW) | test (kind Layer B) | event-driven E2E | `medium_http_test.go` (hermetic git server) + `wave_test.go` (fixtures) + `chaos_resume_test.go:367+` (PVC-inspection Job) | exact composite |
| envtest specs in `internal/controller/*_test.go` (modify/extend) | test (Layer A) | event-driven | existing boundary-push envtest specs | exact |

## Pattern Assignments

### `internal/controller/boundary_push.go` — cumulative set (D-03/D-07) + single-flight gate (D-02)

**Analog A — deterministic-name idempotent dispatch (keep, already present):** `triggerBoundaryPush` (boundary_push.go:93–129):
```go
pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
var existing batchv1.Job
getErr := c.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &existing)
if getErr == nil {
    return nil // already created — boundary push already in flight
}
...
if cErr := c.Create(ctx, pushJob); cErr != nil {
    if !apierrors.IsAlreadyExists(cErr) {
        return fmt.Errorf("create push job: %w", cErr)
    }
    // AlreadyExists: idempotent success — the D-B5 serialization race.
}
```

**Analog B — D-02 List gate template:** `plannerInFlightCount` (dispatch_helpers.go:304–329) — copy this shape for a `gitWriterInFlightCount`:
```go
func plannerInFlightCount(ctx context.Context, c client.Client, watchNamespace string) (int, error) {
    var jobs batchv1.JobList
    opts := []client.ListOption{
        client.MatchingLabels{"tideproject.k8s/role": "planner"},
    }
    if watchNamespace != "" {
        opts = append(opts, client.InNamespace(watchNamespace))
    }
    if err := c.List(ctx, &jobs, opts...); err != nil {
        return 0, err
    }
    n := 0
    for i := range jobs.Items {
        if jobs.Items[i].DeletionTimestamp != nil {
            continue // deleting Jobs don't hold a slot
        }
        if !isJobTerminal(&jobs.Items[i]) {
            n++
        }
    }
    return n, nil
}
```
Key adaptations: label `tideproject.k8s/role: git-writer` + `tideproject.k8s/project: <name>` (labels must be ADDED in `buildPushJob` — push/wave Jobs carry none today); Pitfall 7 — exclude the Job the caller itself owns/observes (match on name) or the boundary-push retry loop deadlocks on itself.

**Analog C — cumulative Succeeded-branch set:** compose the existing plan-level collection (boundary_push.go:215–222) with a live List (label verified: `tideproject.k8s/project`, see fixtures_test.go:106):
```go
// existing per-caller shape being replaced (boundary_push.go:216-221):
for _, t := range taskItems {
    if t.Status.Phase == "Succeeded" {
        branches = append(branches, pkggit.TaskBranchName(string(t.UID)))
    }
}
```
Move this inside `triggerBoundaryPush`/`dispatchBoundaryPush` as a `client.List` of Task CRs (RESEARCH Pattern 1 gives the full helper; `sort.Strings(branches)` for deterministic args across retries).

---

### `internal/controller/plan_controller.go` — loop extension (INTEG-01) + bounded retry (D-04) + conflict→Plan Failed (D-10)

**The skip to close** (plan_controller.go:1192–1198):
```go
// Iterate each wave boundary k → k+1. Skip the last wave (no k+1 to gate on).
for k := 0; k < len(layers)-1; k++ {
    res, handled, err := r.reconcileWaveBoundary(ctx, plan, project, taskByName, layers, k)
    if handled || err != nil {
        return res, err
    }
}
```
Change bound to `k < len(layers)`; the no-git short-circuit at :1007 keeps stub projects unblocked; note Pitfall 6 (Plan=Succeeded now waits for final integration — `BoundaryDetected`/`patchPlanSucceeded` at :1208–1214 runs after the loop).

**The failure arm D-04 replaces** (plan_controller.go:1031–1037):
```go
if integJob.Status.Failed > 0 {
    // Permanently failed (BackoffLimit exhausted) → terminal Plan failure.
    res, err := r.patchPlanFailed(ctx, plan,
        tideprojectv1alpha2.ReasonWaveIntegrationFailed,
        fmt.Sprintf("wave %d integration job %s failed (BackoffLimit exhausted)", waveNum, integJobName))
    return res, true, err
}
```
Replace with: read envelope reason (generalized `readPushEnvelope`) → `merge-conflict` → `patchPlanFailed` naming both branches (D-10); else bounded retry copying the #13b arm pattern below.

**Retry template — the #13b machine** (project_controller.go, copy these three pieces):

1. Cap guard, re-derived from status so it survives restarts (:700–718):
```go
if project.Status.BoundaryPush.Attempts >= maxBoundaryPushAttempts {
    if c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha2.ConditionBoundaryPushed); c == nil ||
        c.Reason != tidev1alpha2.ReasonPushFailed {
        // set condition once, emit Event once, bump metric once
    }
    return ctrl.Result{}, nil
}
```
2. Attempts increment on dispatch (:866–873):
```go
now := metav1.Now()
patch := client.MergeFrom(project.DeepCopy())
project.Status.BoundaryPush.Attempts++
project.Status.BoundaryPush.LastAttemptTime = &now
project.Status.BoundaryPush.LastError = lastErr
if err := r.Status().Patch(ctx, project, patch); err != nil { ... }
...
return ctrl.Result{RequeueAfter: boundaryPushRequeue(project.Status.BoundaryPush.Attempts)}, nil
```
3. Background-propagation delete before re-dispatch (:895–904) — Foreground never clears under envtest and wedges the deterministic name:
```go
policy := metav1.DeletePropagationBackground
if err := r.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &policy}); err != nil {
    if apierrors.IsNotFound(err) {
        return nil
    }
    return fmt.Errorf("delete failed boundary push job %s: %w", job.Name, err)
}
```

---

### `internal/controller/project_controller.go` — LastPushedSHA stamp (D-14) + integration-incomplete arms (D-08/D-09/D-11)

**Success arm where the stamp goes** (:734–748 — same arm that sets BoundaryPushed=True, per CONTEXT specifics):
```go
if isJobSucceeded(&existingPush) {
    patch := client.MergeFrom(project.DeepCopy())
    project.Status.Git.LeaseFailureCount = 0
    project.Status.BoundaryPush.LastError = ""
    if err := r.Status().Patch(ctx, project, patch); err != nil { ... }
    if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionTrue,
        tidev1alpha2.ReasonPushed, ...); err != nil { ... }
    tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "success").Inc()
    ...
}
```
Add: `env, ok := r.readPushEnvelope(...)`; if ok, `project.Status.Git.LastPushedSHA = env.HeadSHA` in the same merge patch. Pitfall 4: do NOT block the condition on envelope readability — log + metric on stamp-skip.

**Failure-arm switch to extend** (:760–823) — new `case` entries for the miss/conflict reasons mirror the existing sticky-condition arm exactly (:781–798, `lease-rejected`):
```go
case "lease-rejected":
    patch := client.MergeFrom(project.DeepCopy())
    project.Status.Phase = tidev1alpha2.PhasePushLeaseFailed
    project.Status.Git.LeaseFailureCount++
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:               tidev1alpha2.ConditionPushLeaseFailed,
        Status:             metav1.ConditionTrue,
        Reason:             "LeaseRejected",
        Message:            fmt.Sprintf("Push Job %s rejected by --force-with-lease", pushJobName),
        LastTransitionTime: metav1.Now(),
    })
    project.Status.BoundaryPush.LastError = "lease-rejected"
    if err := r.Status().Patch(ctx, project, patch); err != nil { ... }
    tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "lease").Inc()
    return ctrl.Result{}, nil
```
D-08 difference: an `integration-incomplete` reason routes through the DEFAULT (bounded-retry) arm until the cap, and the sticky condition parks only at cap exhaustion (:700–718 arm). A `merge-conflict` reason parks immediately (conditional-arm shape above). Sticky-arm re-entry guards at :688–695 (`FindStatusCondition` early-return) must gain a matching guard for the new condition.

**Envelope reader to generalize** (:85–109 — currently a `ProjectReconciler` method; promote to package-level func so `PlanReconciler` can classify wave-Job failures):
```go
var pods corev1.PodList
if err := r.List(ctx, &pods,
    client.InNamespace(namespace),
    client.MatchingLabels{"job-name": pushJobName},
); err != nil { return pushResultEnvelope{}, false }
...
term := pod.Status.ContainerStatuses[0].State.Terminated
if term == nil || term.Message == "" { return pushResultEnvelope{}, false }
var env pushResultEnvelope
if err := json.Unmarshal([]byte(term.Message), &env); err != nil { return pushResultEnvelope{}, false }
```
Termination-log surface is per-pod and collision-free (Pitfall 2 — do NOT read the shared PVC envelope path keyed by project UID).

---

### `internal/controller/push_helpers.go` — Job labels for the D-02 gate

**Current ObjectMeta has NO labels** (:205–209):
```go
job := &batchv1.Job{
    ObjectMeta: metav1.ObjectMeta{
        Name:      fmt.Sprintf("tide-push-%s", project.UID),
        Namespace: project.Namespace,
    },
    ...
```
Add `Labels: map[string]string{"tideproject.k8s/role": "git-writer", "tideproject.k8s/project": project.Name}` — matching the label key style consumed by `plannerInFlightCount` (`tideproject.k8s/role: planner`) and the existing project label (`tideproject.k8s/project`, fixtures_test.go:106). Args pattern for any new flag mirrors `--integrate-task-branches` (:158–162, CSV via `strings.Join`).

---

### `cmd/tide-push/main.go` — flock + verify (D-06) + conflict classification (D-09) + wave-success envelope

**Insertion point** — verify goes after the integrate block, before worktree open/commit/push (:359–380). The integration-only success early-exit at :374–379 currently writes NO envelope (Pitfall 3) — add a `writePushEnvelope(cfg, "", exitSuccess, ...)` there.

**Envelope error-path pattern to copy** (:340–353 — envelope first, stderr second, return typed exit code):
```go
if cfg.Branch == "" {
    writePushEnvelope(cfg, "", exitInvariant, "invalid-branch")
    fmt.Fprintf(stderr, "tide-push: push mode requires --branch\n")
    return exitInvariant
}
```

**Classification pattern to mirror for merge conflicts** (`classifyPushError`, :710–734 — conservative lowercase string matching, comment explains why):
```go
msg := strings.ToLower(err.Error())
switch {
case strings.Contains(msg, "stale info") ||
    strings.Contains(msg, "non-fast-forward") ||
    strings.Contains(msg, "force-with-lease"):
    return exitLeaseFailed, "lease-rejected"
...
}
return exitGenericFail, "push-failed"
```
Conflict markers (verified in scratch repo, git 2.54): `CONFLICT (` / `Automatic merge failed`. Split today's undifferentiated `"integration-failed"` reason at :362.

**Envelope write shape** (:662–671) — the struct the controller unmarshals; `HeadSHA` is what D-14 stamps; success write at :571 is `writePushEnvelope(cfg, newHash.String(), exitSuccess, "")`:
```go
pr := pushResult{
    APIVersion: envelopeAPIVersion,
    Kind:       envelopeKind,
    ProjectUID: cfg.ProjectUID,
    Branch:     cfg.Branch,
    HeadSHA:    headSHA,
    ExitCode:   exit,
    Reason:     reason,
}
```
Termination-message cap is 4096 bytes — truncate the missing-branch list (first 10 + count).

**flock:** no in-repo analog (genuinely new). Use RESEARCH Pattern 3 verbatim (`golang.org/x/sys/unix.Flock`, lockfile inside `repo.git`, no unlock — kernel releases on exit). Route any new stderr/envelope text through `redactPAT` (main.go:774).

**Verify predicate:** RESEARCH Pattern 4 verbatim (`git merge-base --is-ancestor <br> <runBranch>` per branch in the integration worktree; `exec.ExitError.ExitCode() == 1` = miss, other errors = infra). Exec style matches `integrate.go:102` (`CombinedOutput`, error wraps output).

---

### `pkg/git/integrate.go` — merge-abort hygiene (Pitfall 1)

**The merge loop with no abort** (:94–107):
```go
for _, taskBranch := range taskBranches {
    msg := fmt.Sprintf("tide: integrate %s", taskBranch)
    args := []string{
        "-c", "user.name=" + botName,
        "-c", "user.email=" + botEmail,
        "-C", integrationDir,
        "merge", "--no-ff", taskBranch, "-m", msg,
    }
    out, err := exec.Command("git", args...).CombinedOutput()
    if err != nil {
        return fmt.Errorf("IntegrateTaskBranches: merge %s → %s: %w: %s",
            taskBranch, runBranch, err, string(out))
    }
}
```
On failure: run `git -C <integrationDir> merge --abort` before returning (lingering `MERGE_HEAD` on the shared PVC breaks every retry differently); consider defensive `merge --abort || true` at start. Error text already carries both branch names — surface them for D-10's condition. Tolerant-error style for the abort: mirror the "already checked out"/"already exists" string-tolerance at :74–81. Do NOT touch the bot-identity block (:85–92 — Phase 36 lands there).

---

### `api/v1alpha2/shared_types.go` (+ plan/project types, both API versions)

**Reason-block style to copy** (:197–204):
```go
// Phase 11 condition + reason vocabulary — per-wave integration failure.
const (
    // ReasonWaveIntegrationFailed — a per-wave integration Job (BackoffLimit
    // exhausted) failed before wave k+1 could be dispatched. Plan is marked
    // terminal Failed; subsequent reconcile cycles see Phase=="Failed" and
    // exit early without requeueing.
    ReasonWaveIntegrationFailed = "WaveIntegrationFailed"
)
```
New consts follow PascalCase (`ConditionIntegrationIncomplete`, `ReasonIntegrationIncomplete`, `ReasonMergeConflict` — naming is discretionary) with a `// Phase 34 condition + reason vocabulary — ...` header. If a wave-integration attempts field lands on `Plan.Status`, mirror `BoundaryPushStatus` (Attempts/LastAttemptTime/LastError — `project_types.go:289+` v1alpha2, `:271+` v1alpha1) in BOTH versions with conversion parity, like `IntegratedThroughWave` (v1alpha2 plan_types.go:74–80 / v1alpha1 :61–67).

---

### `internal/metrics/registry.go` — integration-outcome counter (D-12)

**Analog** (:173–179):
```go
PushJobsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_push_jobs_total",
        Help: "Count of terminal push Jobs, with outcome ∈ {success, leak, lease, auth, internal} (Phase 4 D-O2).",
    },
    []string{"project", "outcome"},
)
```
Register the new `tide_integration_outcomes_total{project, outcome}` beside it; extend the seed-and-assert test in `registry_test.go`.

---

### `cmd/tide/resume.go` — attempts reset + condition clear (D-13)

**Analog — the BillingHalt clear sequence in `resumeRun`** (:91–119), showing the load-bearing re-fetch discipline (annotation patch → re-Get → status patch → re-Get → metadata patch; annotations and status are different subresources with different resourceVersion windows):
```go
if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
    return fmt.Errorf("re-get project for BillingHalt clear: %w", err)
}
haltCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha2.ConditionBillingHalt)
if haltCond != nil {
    patch2 := client.MergeFrom(proj.DeepCopy())
    meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha2.ConditionBillingHalt)
    if err := c.Status().Patch(ctx, &proj, patch2); err != nil { ... }
    // then a SEPARATE metadata patch for the annotation stamp
}
```
D-13 pins the annotation mechanism: precedent is `bypassPushLeaseAnnotation = "tideproject.k8s/bypass-push-lease"` (project_controller.go:125–128) — CLI sets the annotation, controller consumes it and resets `Status.BoundaryPush.Attempts` + clears the sticky condition.

---

### `test/integration/kind/integration_miss_test.go` (NEW) — INTEG-05

Composite of three existing analogs in the same package:

1. **Hermetic git remote:** `medium_http_test.go` — git-http-server Deployment + Service + demo-remote-init Job, anonymous push (`http.receivepack=true`), in-cluster `http://` repo URL. Reuse the fixture stack in a dedicated namespace.
2. **Typed fixtures:** `fixtures_test.go` / `wave_test.go` — `newStubProject(... withGit(repo, secret))`, `newStubPlan`, `newStubTask(... withTaskDependsOn(...))`; stub-subagent `success` mode writes a real file per task branch, so a dropped merge is observable as missing content. Branch names resolvable via `pkggit.TaskBranchName(string(task.UID))` → `tide/wt-<uid>`.
3. **PVC-inspection assertion Job:** `chaos_resume_test.go:367–430` — inline Job mounting the `tide-projects` PVC at `subPath: <project.UID>/workspace`, running a shell script, result via exit code/termination message:
```go
writerJob.Spec.Template.Spec = corev1.PodSpec{
    RestartPolicy:      corev1.RestartPolicyNever,
    ServiceAccountName: "tide-subagent",
    SecurityContext:    &corev1.PodSecurityContext{FSGroup: &fsGroup},
    Volumes: []corev1.Volume{{
        Name: "project-workspace",
        VolumeSource: corev1.VolumeSource{
            PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                ClaimName: "tide-projects",
            },
        },
    }},
    Containers: []corev1.Container{{
        Name:    "release",
        Image:   "busybox:1.36",
        Command: []string{"sh", "-c", signalScript},
        ...
    }},
}
```
Adapt: image must be git-capable — use the tide-push image (already built/loaded by `make test-int-kind-prep` via the `loadRequiredImage` pattern); script runs `git -C /workspace/repo.git merge-base --is-ancestor <task-branch> <run-branch>`. Two cases per CONTEXT: single-wave degenerate (cheapest RED) and 2-parallel-task final wave (observed shape, also asserts `lastPushedSHA` set + `BoundaryPushed=True`).

## Shared Patterns

### Deterministic Job names as serialization keys
**Source:** `boundary_push.go:93` (`tide-push-<project.UID>`), `:164` (`tide-push-wave-<plan.UID>-<k>`)
**Apply to:** every Job-creating change. Get-then-Create, AlreadyExists = idempotent success. The D-02 gate LAYERS ON this; it does not replace it.

### Envelope-first error returns in tide-push
**Source:** main.go:340–353, 550–572
**Apply to:** every new exit path in tide-push (verify miss, conflict, flock failure): `writePushEnvelope(cfg, sha, exitCode, reason)` → `fmt.Fprintf(stderr, ...)` (PAT-redacted) → `return exitCode`. New exit codes follow the map style at main.go:36–46.

### Status-patch discipline in controllers
**Source:** project_controller.go:735–740, 866–873
**Apply to:** all controller status writes: `patch := client.MergeFrom(obj.DeepCopy())` → mutate → `r.Status().Patch(ctx, obj, patch)`; conditions via `meta.SetStatusCondition`/`FindStatusCondition`; metric increment + `logger.Info` with structured keys in the same arm.

### Sticky-condition re-entry guard
**Source:** project_controller.go:682–695 (leak/lease guards) and the once-only cap arm :700–702
**Apply to:** the new `integration-incomplete` and merge-conflict park arms — guard on `FindStatusCondition` before re-processing, or the Complete fast-path re-increments counters in a loop.

## No Analog Found

| File/Behavior | Role | Reason | Fallback |
|---|---|---|---|
| flock in tide-push | Job binary syscall | No flock usage anywhere in repo (verified by research grep) | RESEARCH Pattern 3 code (x/sys/unix, already in go.sum) |
| `merge-base --is-ancestor` verify | Job binary git exec | New predicate | RESEARCH Pattern 4 code; exec style from integrate.go:102 |

## Assumptions

- Headless run: analog selection follows RESEARCH.md's already-verified pattern citations rather than a fresh whole-repo sweep; every excerpt above was re-read from the current worktree, so line numbers are current as of 2026-07-04.
- Treated the wave-integration attempts counter as a Plan.Status field mirroring `BoundaryPushStatus` (RESEARCH Open Question 1's recommendation) for classification purposes; planner may still choose the annotation alternative.

## Metadata

**Analog search scope:** `internal/controller/`, `pkg/git/`, `cmd/tide-push/`, `cmd/tide/`, `api/v1alpha{1,2}/`, `internal/metrics/`, `test/integration/kind/`
**Files scanned:** 10 read in targeted ranges this session (RESEARCH.md verified the rest in its own session)
**Pattern extraction date:** 2026-07-04
