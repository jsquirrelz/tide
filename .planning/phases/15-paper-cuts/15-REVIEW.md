---
phase: 15-paper-cuts
reviewed: 2026-06-12T00:00:00Z
depth: standard
files_reviewed: 43
files_reviewed_list:
  - cmd/dashboard/api/events_sse.go
  - cmd/dashboard/api/events_sse_test.go
  - cmd/dashboard/api/informer_bridge.go
  - cmd/dashboard/api/informer_bridge_test.go
  - cmd/dashboard/api/waves.go
  - cmd/dashboard/api/waves_test.go
  - cmd/dashboard/router.go
  - cmd/manager/main.go
  - cmd/tide-push/main_test.go
  - cmd/tide/approve_test.go
  - cmd/tide/artifact_get.go
  - cmd/tide/artifact_get_run.go
  - cmd/tide/artifact_get_run_test.go
  - cmd/tide/describe_budget_test.go
  - cmd/tide/runners.go
  - dashboard/web/src/App.test.tsx
  - dashboard/web/src/App.tsx
  - dashboard/web/src/components/EmptyState.tsx
  - dashboard/web/src/components/PlanningDAGView.tsx
  - dashboard/web/src/components/ProjectPicker.test.tsx
  - dashboard/web/src/components/ProjectPicker.tsx
  - dashboard/web/src/components/RunningWavesView.tsx
  - dashboard/web/src/components/StatusBadge.test.tsx
  - dashboard/web/src/components/StatusBadge.tsx
  - dashboard/web/src/components/__tests__/RunningWavesView.test.tsx
  - dashboard/web/src/components/__tests__/dag-views.test.tsx
  - dashboard/web/src/lib/sse.ts
  - internal/controller/file_touch_gate_test.go
  - internal/controller/milestone_controller.go
  - internal/controller/milestone_controller_test.go
  - internal/controller/phase_controller.go
  - internal/controller/phase_controller_test.go
  - internal/controller/plan_controller.go
  - internal/controller/plan_webhook_test.go
  - internal/gates/boundary.go
  - internal/owner/label.go
  - internal/owner/label_test.go
  - internal/reporter/materialize.go
  - internal/reporter/materialize_test.go
  - internal/subagent/common/prompt_templates_test.go
  - internal/subagent/common/templates/plan_planner.tmpl
  - internal/webhook/v1alpha1/plan_webhook.go
findings:
  critical: 1
  warning: 7
  info: 7
  total: 15
status: issues_found
---

# Phase 15: Code Review Report

**Reviewed:** 2026-06-12
**Depth:** standard
**Files Reviewed:** 43
**Status:** issues_found

## Summary

Reviewed the Phase 15 paper-cuts surface: the running-waves SSE aggregate (waves.go, events_sse.go, informer_bridge.go, RunningWavesView/App), the real `tide artifact-get` inspector-pod implementation, the file-touch dispatch gate, the project-label create-site stamp + backfill (CUTS-01 D-01/D-03), and the supporting tests. The security-sensitive surfaces are mostly solid: artifact-path validation rejects traversal and shell metacharacters and delivers the path via env var (never interpolated into `sh -c`); the SSE handler unsubscribes on every exit path; MaterializeChildCRDs enforces the Kind allowlist before any API call.

One Critical finding: the Plan controller permanently fails a Plan on a *transient* envelope read error — the exact failure class milestone/phase controllers were already fixed to treat as non-fatal (Phase 12 Pitfall 1), so the parity gap wedges a run terminally. Warnings cluster around: a hot poll loop in artifact-get, a startup pre-charge that runs against an unstarted informer cache, a D-01 label-stamp no-op at the Project→Milestone edge that re-opens a (now transient) finding-6 window, inconsistent ParentUnresolved condition polarity, and several SSE namespace/empty-project scoping inconsistencies on the dashboard side.

## Critical Issues

### CR-01: Transient envelope read error permanently fails a Plan (parity gap with milestone/phase controllers)

**File:** `internal/controller/plan_controller.go:488-504`
**Issue:** In `handlePlannerJobCompletion`, an `EnvReader.ReadOut` error patches `plan.Status.Phase = "Failed"` with `Reason=EnvelopeReadFailed`. `Failed` is a terminal short-circuit (`plan_controller.go:272`), so the Plan never re-enters dispatch — one transiently unreadable termination message (Pod GC'd, informer lag, apiserver hiccup) wedges the Plan and its entire subtree permanently. The milestone and phase controllers were explicitly fixed for this exact class (Phase 12 Pitfall 1: `milestone_controller.go:534-542`, `phase_controller.go:465-473` log the error as non-fatal and defer to children-based succession). The Plan controller was never given the same fix, breaking the codebase's own stated doctrine ("envelope transiently absent" must not be terminal).
**Fix:** Mirror the milestone/phase Pitfall-1 handling: on `readErr != nil`, log non-fatally and requeue (or fall back to the task-children-based path) instead of patching Failed:
```go
out, readErr = r.EnvReader.ReadOut(ctx, projectUID, string(plan.UID))
if readErr != nil {
    logger.Error(readErr, "plan planner envelope tiny-status read failed (non-fatal); requeueing", "plan", plan.Name)
    return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}
```
If a permanent-failure escape hatch is needed, bound it (N consecutive read failures or Job-Failed condition) before patching Failed.

## Warnings

### WR-01: `waitForPodRunning` is a hot poll loop with no sleep

**File:** `cmd/tide/artifact_get_run.go:296-320`
**Issue:** The loop issues `Pods(ns).Get` back-to-back with **no delay** between iterations. The trailing `select { case <-ctx.Done(): ... default: }` is non-blocking — the comment "Still Pending — yield and retry" describes a yield that does not exist. The loop is only throttled by client-go's default rate limiter (5 QPS), so it burns a CPU/network spin against the apiserver for up to the full `--timeout` (default 5m) while the pod is Pending (e.g., image pull, PVC attach).
**Fix:** Replace the second non-blocking select with a real wait:
```go
select {
case <-ctx.Done():
    return fmt.Errorf("timed out waiting for inspector pod %s to start", podName)
case <-time.After(time.Second):
}
```
(or use `k8s.io/apimachinery/pkg/util/wait.PollUntilContextCancel`).

### WR-02: Pool pre-charge runs against the unstarted informer cache — POOL-02 never works and boot may stall 30s

**File:** `cmd/manager/main.go:325-332`
**Issue:** `plannerPool.PreCharge` / `executorPool.PreCharge` are called synchronously **before** `mgr.Start(signalCtx)` using `mgr.GetClient()`, which reads through the informer cache. The cache has not started yet, so the `List` in `pool.PreCharge` (internal/pool/pool.go:94) blocks on cache sync until the 30s `preChargeTimeout` expires, then fails. Result: POOL-02 pre-charge silently never functions (pools restart empty, allowing oversubscription after a manager restart) and every boot can be delayed up to 30s. The code itself acknowledges the correct pattern two pages later — step 9 registers `budget.PreCharge` as a Manager Runnable precisely because "the cache-backed client is warm by this point."
**Fix:** Move both pool pre-charges into the same `mgr.Add(ctrlmgr.RunnableFunc(...))` pattern used for `budget.PreCharge` at step 9, or pass `mgr.GetAPIReader()` (uncached) if pre-start execution is required.

### WR-03: D-01 project-label stamp is a silent no-op for Milestones created under a Project parent

**File:** `internal/reporter/materialize.go:254-257` (interacts with `internal/controller/project_controller.go`)
**Issue:** `MaterializeChildCRDs` stamps the child's project label from `parent.GetLabels()[owner.LabelProject]`. A `Project` CR never carries the `tideproject.k8s/project` label on itself (nothing stamps it — verified by grep across `internal/controller/project_controller.go`), so for the Project→Milestone edge the stamp is always a no-op and reporter-created Milestones are still born **unlabeled** — the exact CUTS-01 finding-6 shape this phase claims to fix at the create-site. The system now depends entirely on the D-03 reconciler backfill, leaving a window between reporter-create and first Milestone reconcile in which `tide approve` label-filtered discovery misses a parked Milestone (the finding-6 symptom, now transient instead of permanent). The unit test `TestMaterializeChildCRDsStampsProjectLabel` only exercises a *Milestone* parent that already carries the label, so the gap is untested.
**Fix:** Special-case the Project parent — the project name is the parent's own name:
```go
projectName := parent.GetLabels()[owner.LabelProject]
if _, isProject := parent.(*tideprojectv1alpha1.Project); isProject {
    projectName = parent.GetName()
}
owner.StampProjectLabel(obj, projectName)
```
Add a test row with a `*Project` parent.

### WR-04: `ConditionParentUnresolved` has opposite Status polarity across controllers

**File:** `internal/controller/milestone_controller.go:831-837`, `internal/controller/phase_controller.go:760-766`, `internal/controller/plan_controller.go:1091-1098`
**Issue:** Milestone and Phase controllers surface an unresolved parent with `Status: metav1.ConditionFalse` on `ConditionParentUnresolved`; the Plan controller sets the same condition Type with `Status: metav1.ConditionTrue` (Reason `NoProjectLabel`). Any operator tooling, dashboard render, or `kubectl wait --for=condition=ParentUnresolved` consuming this condition gets opposite meanings depending on the level — a correctness bug in the observable API surface.
**Fix:** Pick one polarity (True = "parent IS unresolved" matches the Type name) and apply it in all three controllers; update any condition consumers.

### WR-05: Transient empty `projectName` opens a doomed EventSource (`/dev/null/no-project`) and fires a garbage `fetchProject("")`

**File:** `dashboard/web/src/App.tsx:213-217, 271-274`; `dashboard/web/src/components/RunningWavesView.tsx:221-223`; `dashboard/web/src/components/PlanningDAGView.tsx:238-251`; `dashboard/web/src/lib/tasks.ts:80-83`
**Issue:** On the first render after projects load, `selectedProject` is still `null` (the auto-default effect runs post-commit), so App renders both panes with `projectName=""`. Two consequences: (1) `RunningWavesView` calls `projectEventsURL("")`, which returns the *truthy* synthetic URL `"/dev/null/no-project"` — `useSSEStream` only skips construction on an **empty** url, so a real `EventSource` is opened against a bogus path, errors, and enters the exponential reconnect loop until the re-render replaces the url. `PlanningDAGView` guards this case (`projectName ? projectEventsURL(...) : ""`); `RunningWavesView` does not — an inconsistency in the same codebase. (2) `PlanningDAGView.runFetch` calls `fetchProject("")` unguarded, producing a request to `/api/v1/projects/` whose response either rejects (404) or parses as the list payload, in which case `buildPlanningGraph` dereferences `detail.milestones.length` on `undefined` and throws inside a `void`-swallowed promise.
**Fix:** In `RunningWavesView`, mirror the PlanningDAGView guard: `useSSEStream(hasInitialRef.current || !projectName ? "" : projectEventsURL(projectName), ...)`. In `PlanningDAGView.runFetch`, early-return when `!projectName && !initialData`.

### WR-06: Initial `waves.snapshot` is cluster-scoped while bridge-published snapshots are namespace-scoped

**File:** `cmd/dashboard/api/events_sse.go:217-219`; `cmd/dashboard/api/informer_bridge.go:152-153`; `cmd/dashboard/api/waves.go:94-98`
**Issue:** The on-subscribe snapshot calls `computeRunningWaves(ctx, reader, namespace, projectName)` with `namespace` from the query string — and the frontend never sends one (`projectEventsURL` has no namespace param), so `client.InNamespace("")` lists Tasks across **all namespaces**. Every subsequent bridge-published snapshot is scoped to the changed Task's single namespace. For same-name projects in different namespaces (a supported scenario — the picker renders `namespace/name` identities), the pane initially shows the union of both projects' waves, then flip-flops to whichever single namespace last had a Task event. Same-name cross-namespace task data also leaks into the wrong project's view.
**Fix:** Resolve the project's namespace server-side (the WR-01 existence check at events_sse.go:186-194 already locates the Project — capture its namespace) and pass it to both `computeRunningWaves` call sites, or thread the selected project's namespace from the frontend (`App.tsx` already computes `selectedNamespace`).

### WR-07: `RunningWavesView` shows the previous project's waves after a project switch

**File:** `dashboard/web/src/components/RunningWavesView.tsx:210-245`
**Issue:** `waves` state is never reset when `projectName` changes. On switching projects the component keeps rendering the old project's wave cards until the new SSE stream delivers a snapshot; if the new subscription fails (project 404s, network error → reconnect backoff up to 30s), stale wrong-project data persists indefinitely with no loading indication. `useTaskLog` in the same codebase resets its buffer on `taskName` change — this component skips the equivalent reset.
**Fix:** Reset to the spinner state on project change:
```tsx
useEffect(() => {
  if (!hasInitialRef.current) setWaves(null);
}, [projectName]);
```

## Info

### IN-01: Deferred inspector-pod Delete has no timeout

**File:** `cmd/tide/artifact_get_run.go:253-255`
**Issue:** The cleanup `Delete` uses bare `context.Background()` — if the apiserver hangs, the CLI blocks indefinitely at exit instead of exiting after the user's `--timeout`.
**Fix:** `ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)` around the Delete.

### IN-02: SPA fallback returns 500 for non-clean paths instead of serving the fallback

**File:** `cmd/dashboard/router.go:253-261`
**Issue:** `fs.Stat` returns `fs.ErrInvalid` (not `ErrNotExist`) for paths containing `..` or other non-`fs.ValidPath` shapes, so the handler answers 500 "failed to stat asset" rather than the index fallback / 404. Not a traversal risk (embed.FS rejects it), just wrong status semantics for probe traffic.
**Fix:** Treat `errors.Is(err, fs.ErrInvalid)` the same as not-found.

### IN-03: JSON error bodies served as text/plain with Go-style escaping

**File:** `cmd/dashboard/api/events_sse.go:170, 191`
**Issue:** `http.Error` with a hand-built JSON string sets `Content-Type: text/plain`; `%q` uses Go escape sequences (e.g. `\x80`) that are not valid JSON for non-ASCII project names. Clients parsing the error body as JSON can fail.
**Fix:** Write the header + `json.Marshal`'d body explicitly, or use a shared `writeJSONError` helper.

### IN-04: Project-label key duplicated as string literals across four packages

**File:** `cmd/dashboard/api/waves.go:52-55`, `internal/controller/plan_controller.go:832, 1362-1372`, `cmd/tide/inspect_wave_run.go:38-39`, `internal/webhook/v1alpha1/plan_webhook.go:132`
**Issue:** `owner.LabelProject` is the canonical constant (created this phase), yet `"tideproject.k8s/project"` remains a raw literal in plan_controller.go and the CLI/dashboard copies; the webhook hardcodes `".spec.planRef"` instead of sharing `taskPlanRefIndexKey`. Drift in any one site silently breaks label-filtered discovery.
**Fix:** Import `owner.LabelProject` where the import graph allows (internal/controller, internal/webhook); for cmd packages add a single shared constants file or a `pkg/`-level export.

### IN-05: Plan controller re-implements reporter-spawn inline instead of using the shared helper

**File:** `internal/controller/plan_controller.go:511-536`
**Issue:** Milestone and Phase controllers call `spawnReporterIfNeeded` (dispatch_helpers.go); the Plan controller duplicates the get/create/AlreadyExists logic inline, so future fixes to the helper (e.g. T-09-13 hardening) will miss this site.
**Fix:** Call `spawnReporterIfNeeded(ctx, r.Client, r.Scheme, plan, project, "Plan", r.ReporterImage)`.

### IN-06: Dead `SubagentImage` plumbing still wired through main.go and four reconcilers

**File:** `cmd/manager/main.go:407, 438, 464, 489, 532`; `internal/controller/milestone_controller.go:84-85` (and phase/plan equivalents)
**Issue:** The field is documented "dead since Phase 13 — resolveImage owns resolution; retained for legacy test wiring, ignored at dispatch" yet is still assigned in production wiring, inviting the false belief that it does something.
**Fix:** Drop the assignments from main.go and schedule field removal once legacy tests migrate to `HelmProviderDefaults.Image`.

### IN-07: Wave-key formatting in test helper breaks for wave index > 9

**File:** `cmd/dashboard/api/waves_test.go:103`
**Issue:** `string(rune('0'+w.WaveIndex))` produces `:` for index 10, `;` for 11, etc. Current fixtures only use 0/1, but any future fixture with a two-digit index silently builds wrong map keys and the assertions stop checking what they claim (test-reliability latent bug).
**Fix:** `fmt.Sprintf("%s/%d", w.PlanName, w.WaveIndex)`.

---

_Reviewed: 2026-06-12_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
