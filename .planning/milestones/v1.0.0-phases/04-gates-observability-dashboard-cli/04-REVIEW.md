---
phase: 04-gates-observability-dashboard-cli
reviewed: 2026-05-19T00:00:00Z
depth: standard
files_reviewed: 81
files_reviewed_list:
  - api/v1alpha1/project_types.go
  - api/v1alpha1/shared_types.go
  - cmd/dashboard/api/events_sse.go
  - cmd/dashboard/api/informer_bridge.go
  - cmd/dashboard/api/logs_sse.go
  - cmd/dashboard/api/projects.go
  - cmd/dashboard/embed/embed.go
  - cmd/dashboard/hub/pubsub.go
  - cmd/dashboard/main.go
  - cmd/dashboard/router.go
  - cmd/manager/main.go
  - cmd/tide-lint/main.go
  - cmd/tide-push/main.go
  - cmd/tide/apply.go
  - cmd/tide/approve.go
  - cmd/tide/artifact_get_run.go
  - cmd/tide/artifact_get.go
  - cmd/tide/cancel.go
  - cmd/tide/describe_budget_run.go
  - cmd/tide/describe_budget.go
  - cmd/tide/inspect_wave_run.go
  - cmd/tide/inspect_wave.go
  - cmd/tide/main.go
  - cmd/tide/reject.go
  - cmd/tide/resume.go
  - cmd/tide/root_flags.go
  - cmd/tide/runners.go
  - cmd/tide/subcommands.go
  - cmd/tide/tail.go
  - cmd/tide/watch.go
  - dashboard/web/src/App.tsx
  - dashboard/web/src/components/AppShell.tsx
  - dashboard/web/src/components/ClipboardCopyAction.tsx
  - dashboard/web/src/components/ConnectionStatusIndicator.tsx
  - dashboard/web/src/components/EmptyState.tsx
  - dashboard/web/src/components/ErrorState.tsx
  - dashboard/web/src/components/ExecutionDAGView.tsx
  - dashboard/web/src/components/Header.tsx
  - dashboard/web/src/components/LoadingState.tsx
  - dashboard/web/src/components/MilestoneNode.tsx
  - dashboard/web/src/components/NodeClickContext.tsx
  - dashboard/web/src/components/PhaseNode.tsx
  - dashboard/web/src/components/PlanningDAGView.tsx
  - dashboard/web/src/components/PlanNode.tsx
  - dashboard/web/src/components/PodLogStreamer.tsx
  - dashboard/web/src/components/ProjectNode.tsx
  - dashboard/web/src/components/ProjectPicker.tsx
  - dashboard/web/src/components/StatusBadge.tsx
  - dashboard/web/src/components/TaskDetailDrawer.tsx
  - dashboard/web/src/components/TaskNode.tsx
  - dashboard/web/src/components/TideNodeShell.tsx
  - dashboard/web/src/components/Toast.tsx
  - dashboard/web/src/components/ToastContainer.tsx
  - dashboard/web/src/components/WaveBackground.tsx
  - dashboard/web/src/lib/ansi.ts
  - dashboard/web/src/lib/api.ts
  - dashboard/web/src/lib/clsx.ts
  - dashboard/web/src/lib/layout.ts
  - dashboard/web/src/lib/sse.ts
  - dashboard/web/src/lib/toast-copy.ts
  - internal/controller/boundary_push.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/push_helpers.go
  - internal/controller/task_controller.go
  - internal/controller/wave_controller.go
  - internal/gates/annotation.go
  - internal/gates/boundary.go
  - internal/gates/doc.go
  - internal/gates/policy.go
  - internal/metrics/doc.go
  - internal/metrics/registry.go
  - internal/otelinit/doc.go
  - internal/otelinit/provider.go
  - pkg/otelai/attrs.go
  - pkg/otelai/doc.go
  - tools/analyzers/metriccardinality/analyzer.go
  - tools/analyzers/metriccardinality/doc.go
findings:
  critical: 5
  warning: 15
  info: 0
  total: 20
status: resolved
---

# Phase 4: Code Review Report

**Reviewed:** 2026-05-19
**Depth:** standard
**Files Reviewed:** 81
**Status:** issues_found

## Summary

Phase 4 shipped a large surface area (gates package, OTel/metrics primitives, W-2 mid-stack boundary push, ten-verb CLI, full React dashboard). Most of the per-file implementation work is well-shaped and consistent with the stated invariants (no `WithSampler`, no `"task"` label literal, zero `dangerouslySetInnerHTML`, MergeFrom-based status patches, defer-Close on streaming handlers).

However, the wiring at `cmd/manager/main.go` leaves a load-bearing cluster of fields unset: `Dispatcher` is never assigned on `MilestoneReconciler` or `PhaseReconciler`, and `TidePushImage` is never assigned on `MilestoneReconciler`, `PhaseReconciler`, or `PlanReconciler`. In combination these mean the W-2 mid-stack boundary push — the headline deliverable for this phase's W-1/W-2 closeout — does not fire in production at any level above Project. The `gates.BoundaryDetected` shared seam documented as the W-2 trigger point is also never called by any reconciler; pushes fire on planner-job-terminal, not on all-children-Succeeded, which misaligns the commit-message semantics from the spec.

On the dashboard, a click-routing bug in the shared `TideNodeShell` causes every Planning-DAG node click (Project, Milestone, Phase) to fire `onPlanClick(name)` regardless of kind, and the underlying `useSSEStream` hook accumulates `MessageEvent` references without a cap — a guaranteed memory leak on any long-running dashboard tab.

Fifteen WARNING items cover misleading user copy, missing edge-case validation, swallowed errors, and defense-in-depth suggestions.

## Critical Issues

### CR-01: Mid-stack boundary push never fires — `Dispatcher` not wired on MilestoneReconciler / PhaseReconciler

**File:** `cmd/manager/main.go:287-308`
**Issue:** `MilestoneReconciler` (line 287) and `PhaseReconciler` (line 298) are constructed without the `Dispatcher` field. Only `PlanReconciler` (line 315) and `TaskReconciler` (line 344) get `Dispatcher: dispatcher`. In `milestone_controller.go:144` and `phase_controller.go:136` the body is gated on `if r.Dispatcher != nil { return r.reconcilePlannerDispatch(ctx, &milestone) }`. With `Dispatcher` always nil, `reconcilePlannerDispatch` is never invoked → `handleJobCompletion` never runs → `maybeTriggerBoundaryPush` never fires → gate-policy hooks never fire at the milestone/phase level. The entire Phase 4 W-2 design at milestone and phase levels is dead code in the running binary.
**Fix:**
```go
if err := (&controller.MilestoneReconciler{
    Client:                  mgr.GetClient(),
    Scheme:                  mgr.GetScheme(),
    MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Milestone,
    PlannerPool:             plannerPool,
    WatchNamespace:          watchNamespace,
    HelmProviderDefaults:    helmProviderDefaults,
    Dispatcher:              dispatcher, // <-- ADD
    TidePushImage:           tidePushImage, // <-- ADD (see CR-02)
    EnvReader:               envReader, // <-- ADD (handleJobCompletion needs it)
}).SetupWithManager(mgr); err != nil { ... }
// Same for PhaseReconciler.
```

### CR-02: `TidePushImage` not wired on Milestone/Phase/Plan reconcilers — boundary push silently skipped

**File:** `cmd/manager/main.go:287-320`
**Issue:** `MilestoneReconciler`, `PhaseReconciler`, and `PlanReconciler` each carry a `TidePushImage string` field used by `triggerBoundaryPush` in `internal/controller/boundary_push.go`. The wiring in main.go sets it only on `ProjectReconciler` (line 282). In `boundary_push.go:76-83` the empty-image branch is silent at V(1):
```go
if tidePushImage == "" {
    logger.V(1).Info("skipping boundary push: TidePushImage not configured", ...)
    return nil
}
```
Even if CR-01 were fixed, the call would no-op without an operator-visible log at default verbosity.
**Fix:** Add `TidePushImage: tidePushImage` to the Milestone, Phase, and Plan reconciler structs in `cmd/manager/main.go`. Consider promoting the "skip" log from V(1) to `Info` (or to a Warning Event on the Project) so silent disablement is operator-visible.

### CR-03: `gates.BoundaryDetected` shared seam is never called by any reconciler — push fires on planner-job-completion, not on all-children-Succeeded

**Files:** `internal/gates/boundary.go`, `internal/controller/milestone_controller.go:307`, `internal/controller/phase_controller.go:266`, `internal/controller/plan_controller.go:345`
**Issue:** `internal/gates/doc.go:13-15` advertises `BoundaryDetected(ctx, c, parent, childKind)` as "the shared seam between the gate-policy code path AND the W-2 mid-stack push trigger — both call BoundaryDetected on the same children." `grep -rn "BoundaryDetected" internal/controller/` confirms only the test files call it. Each reconciler's `handleJobCompletion` calls `maybeTriggerBoundaryPush` unconditionally the moment its OWN planner Job is terminal. That is the wrong semantic boundary: per CONTEXT.md D-W2 "after observing all children Succeeded, before marking the level Succeeded, dispatch a tide-push Job." The current code pushes when the children have just been MATERIALIZED, not when they have SUCCEEDED — so the commit message `"tide: milestone X authored"` lands before any phase work has occurred, and gate `approve` parks the milestone at `AwaitingApproval` on the scaffold rather than on the completed work.
**Fix:** Gate the push trigger in `handleJobCompletion` on `BoundaryDetected`:
```go
detected, err := gates.BoundaryDetected(ctx, r.Client, ms, "Phase")
if err != nil { return ctrl.Result{}, err }
if !detected { return r.patchMilestoneSucceeded(ctx, ms) } // children not done yet
if err := r.maybeTriggerBoundaryPush(ctx, ms, project); err != nil { ... }
```
Repeat for Phase (childKind="Plan") and Plan (childKind="Task"). Re-evaluate the gate-policy hook position too — it should fire on the children-Succeeded boundary, not on planner completion.

### CR-04: Dashboard Planning DAG fires `onPlanClick` for every node kind — wrong-kind clicks pollute right pane

**Files:** `dashboard/web/src/components/TideNodeShell.tsx:85-87,121`, `dashboard/web/src/components/PlanningDAGView.tsx:237`
**Issue:** `PlanningDAGView.tsx:237` sets `<NodeClickContext.Provider value={onPlanClick}>`. Every node renderer (`ProjectNode`, `MilestoneNode`, `PhaseNode`, `PlanNode`) wraps `TideNodeShell`, which unconditionally calls `useNodeClick()(name)` on click regardless of `kind`. So clicking a `ProjectNode` or `MilestoneNode` calls `setSelectedPlan("my-project")` or `setSelectedPlan("ms-alpha")` — the right pane then renders `ExecutionDAGView` with a `planName` that has no matching Plan and silently displays nothing (or worse, the wrong plan if names collide).
**Fix:** Make the click affordance kind-aware. Cheapest fix is a `clickable?: boolean` prop on `TideNodeShell` that suppresses `onClick`/`onKeyDown`/`role="button"` for non-clickable kinds; pass `clickable={kind === "plan"}` from PlanningDAG node renderers (and `clickable={kind === "task"}` from ExecutionDAG TaskNode). Alternatively split into two context callbacks (one per pane).

### CR-05: `useSSEStream` accumulates `MessageEvent` references unboundedly — memory leak on every dashboard SSE connection

**File:** `dashboard/web/src/lib/sse.ts:86-99`
**Issue:**
```js
es.onmessage = (e: MessageEvent) => {
  ...
  const nextEvents = resultRef.current.events.concat(e);
  resultRef.current = { events: nextEvents, ... };
}
```
`resultRef.current.events` is appended on every message with no cap. `useTaskLog` caps the *derived* lines at 5000 (line 194), but the raw `events` array grows forever as long as the EventSource is open. Each `MessageEvent` holds DOM references; a project dashboard tab open for a workday on a chatty project will grow into hundreds of MB. This is exactly the T-04-D-eventsource-leak / Pitfall 22 scenario the design was supposed to mitigate.
**Fix:** Cap `events` inside `useSSEStream` (e.g. `MAX_EVENTS = 1000`, slice from end on overflow), or restructure to fire a callback per event without retaining the `MessageEvent` array at all (consumers maintain their own derived buffers — `useTaskLog` already does this; the underlying buffer is redundant). Add a unit test that drives N events and asserts the retained array never exceeds the cap.

## Warnings

### WR-01: Dashboard SSE handler subscribes to any project name without verifying existence or RBAC

**File:** `cmd/dashboard/api/events_sse.go:140-153`
**Issue:** `EventsHandler.ServeHTTP` extracts `name := chi.URLParam(r, "name")` and immediately calls `h.Hub.Subscribe(projectName, lastEventID)` without a `c.Get(ctx, ObjectKey{name})` existence check or per-request SubjectAccessReview. The dashboard SA has cluster-wide get/list/watch on Projects (D-D2), so the human operator cannot escalate beyond what the dashboard SA can see — but any browser that reaches the dashboard can subscribe to ANY project's event stream, including projects in namespaces they may have no business viewing. This is consistent with D-D2's stated trust model ("Browser NEVER talks to apiserver directly") but should be documented and probe-detection-hardened.
**Fix:** At minimum, do `c.Get(ctx, ObjectKey{Namespace: ns, Name: projectName}, &project)` before subscribing so probe traffic / typos don't open SSE connections that stream nothing. Document the trust model on the SSE endpoint and recommend operators front the dashboard with an OIDC reverse proxy when used in shared clusters.

### WR-02: `tide cancel` advertises PVC cleanup that the finalizer doesn't perform

**Files:** `cmd/tide/cancel.go:69,132`, `internal/controller/project_controller.go:178-184`
**Issue:** `cancel.go:69` prints `"PVC cleanup runs via finalizer."` and dry-run output (line 132) says `"PVC cleanup: via existing finalizer (CTRL-05, Phase 1)"`. In `project_controller.go:179-183` the finalizer cleanup callback is a no-op log line — it does NOT `rm -rf` the per-Project subPath `<project.UID>/workspace` on the shared `tide-projects` PVC. Over time the shared PVC fills with orphan workspace data from deleted Projects.
**Fix:** Either (a) implement workspace cleanup in the finalizer (dispatch a busybox `rm -rf` Job with the same `subPath` mount as the init Job), or (b) correct the CLI copy to honestly state that the per-Project subdirectory is NOT cleaned automatically and operators must remove it manually (or via an external sweep Job).

### WR-03: Approval-annotation removal relies on undocumented JSON merge patch behavior

**Files:** `internal/controller/milestone_controller.go:294-300`, `phase_controller.go:256-262`, `plan_controller.go:333-339,614-622`, `task_controller.go:292-298`
**Issue:** Pattern at every call site:
```go
newAnno := gates.ConsumeApprove(ms, "milestone")
patch := client.MergeFrom(ms.DeepCopy())
ms.SetAnnotations(newAnno)
if err := r.Patch(ctx, ms, patch); err != nil { ... }
```
This works for CRDs because controller-runtime's `MergeFrom` emits an RFC 7396 JSON merge patch and for the `metadata.annotations` field the diff sends the entire (new) annotations map, replacing the prior value and dropping the approve key. But this depends on patch-shape implementation details: a future controller-runtime refactor that diffs annotation sub-paths instead of replacing the whole map could silently regress, and any caller that switches to `client.StrategicMergeFrom` would NOT remove the key (strategic merge patch treats map elements as merge keys).
**Fix:** Add an envtest-level unit test that asserts the approve annotation is actually removed from the apiserver after `Consume*Approve` + `Patch` (the existing `cap_test.go::TestConsumeBypass` only verifies the in-memory map, not the apiserver-side patch).

### WR-04: `joinCSV` reimplements `strings.Join` with O(n²) concatenation

**File:** `internal/controller/push_helpers.go:371-382`
**Issue:**
```go
func joinCSV(paths []string) string {
    out := ""
    for i, p := range paths {
        if i > 0 { out += "," }
        out += p
    }
    return out
}
```
Called once per boundary push so not a perf issue, but a Go-stdlib hygiene smell.
**Fix:** `return strings.Join(paths, ",")` and delete the helper.

### WR-05: `MAX_RING_LINES` cap is per-consumer, not enforced at the underlying SSE stream

**File:** `dashboard/web/src/lib/sse.ts:47,179-208`
**Issue:** Layered defense gap with CR-05. Even if CR-05 is fixed, any new `useSSEStream` consumer added by future plans without their own cap will leak. The hook should be the boundary that prevents unbounded growth.
**Fix:** Once CR-05 is addressed, add a regression test that mounts a non-`useTaskLog` consumer and drives unbounded events to assert the cap holds.

### WR-06: `tide cancel --dry-run` silently swallows List errors

**File:** `cmd/tide/cancel.go:88-129`
**Issue:** Each kind block is `if err := c.List(ctx, &list, ...); err == nil { ...iterate... }`. RBAC denial / apiserver timeout produces zero printed children for that kind — the operator sees a misleading deletion scope without knowing the listing failed.
**Fix:** Surface the error: `if err := c.List(...); err != nil { fmt.Fprintf(errOut, "warning: list %s failed: %v\n", kind, err); continue }`.

### WR-07: `tide approve --wave` regex accepts `..` in plan-name component

**File:** `cmd/tide/approve.go:46`
**Issue:** `^[a-z0-9.-]+/\d+$` accepts e.g. `..-../1` — the apiserver will reject the Get with an invalid-name error, but the CLI message is the more confusing apiserver string instead of the friendly local regex error.
**Fix:** Tighten to a DNS-1123 regex or call `validation.IsDNS1123Label(planName)` from `k8s.io/apimachinery/pkg/util/validation` after splitting.

### WR-08: `parseLastEventID` swallows oversized values, causing replay starvation

**File:** `cmd/dashboard/api/events_sse.go:202-211`
**Issue:** A browser (malicious or buggy) sending `Last-Event-ID: 99999999999` causes `Hub.Subscribe` to iterate the replay buffer and match nothing (since `ev.ID > lastEventID` is always false), then sit silent until a new Publish arrives. The operator sees a "live" SSE pipe that never delivers events.
**Fix:** Cap `lastEventID` at `h.nextID[project]` inside `Hub.Subscribe` so oversized values fall back to "no replay" semantics instead of "permanent silence."

### WR-09: `redactPAT` misses URL-encoded PAT forms

**File:** `cmd/tide-push/main.go:513-518`
**Issue:** `strings.ReplaceAll(msg, pat, "<redacted>")` only redacts the exact-substring form. If go-git logs the auth URL with the PAT percent-encoded (e.g., `%2B` for `+`), the PAT leaks via error messages.
**Fix:** Additionally redact `url.QueryEscape(pat)` and `url.PathEscape(pat)`.

### WR-10: `cmd/dashboard/api/projects.go::List` is O(Projects × Milestones) per request

**File:** `cmd/dashboard/api/projects.go:117-131`
**Issue:** `countActiveMilestones` does a full namespace MilestoneList per Project. Out of scope per the brief (performance is v1-deferred), but flagging for follow-up.
**Fix:** Hoist a single `MilestoneList` outside the loop and group by `ProjectRef` once.

### WR-11: No startup-time HMAC self-test between Manager and credproxy

**File:** `cmd/manager/main.go:86-96`
**Issue:** `TIDE_SIGNING_KEY` is read once via env at startup. If the Manager and the credproxy sidecar drift to different Secrets (e.g., chart misconfiguration), HMAC validation will fail at the first dispatch — operator gets "auth" errors with no clear root cause.
**Fix:** At Manager startup, sign a probe token with the key, hand it to credproxy via a known endpoint, and assert validation succeeds before the manager enters `Start`. Fast-fail on mismatch beats slow-fail per-task.

### WR-12: `pickContainer` returns a name that may not exist in the pod's container statuses

**File:** `cmd/tide/tail.go:167-181`
**Issue:** Iterates `p.Spec.Containers` and returns first non-credproxy/non-init match without verifying the container actually started (`p.Status.ContainerStatuses`). If a future refactor renames the executor container, this still returns a name; `req.Stream(ctx)` then fails post-header-flush with "container not in pod."
**Fix:** Cross-check against `p.Status.ContainerStatuses[*].Name` before returning, surface "container X not found in pod Y; available: [...]" if missing.

### WR-13: `WaveBackground.failedCount` prop is dead code — failed-band UI signal never displays

**Files:** `dashboard/web/src/components/ExecutionDAGView.tsx:248-256`, `WaveBackground.tsx:29,39`
**Issue:** `WaveBackground` accepts `failedCount?: number` (default 0) and drives "failed band" styling from it. `ExecutionDAGView` constructs each `<WaveBackground>` without passing the prop, so every wave renders as non-failed regardless of member task status. The UI-SPEC §6 failed-band signal is therefore never displayed.
**Fix:** In `computeBands`, compute `failedCount` from member tasks (count tasks whose status is in the failed family). Pass `failedCount={b.failedCount}` to `<WaveBackground>`.

### WR-14: `handleBudgetGate` annotation removal shares the WR-03 risk

**File:** `internal/controller/project_controller.go:577-585`
**Issue:** Same `client.MergeFrom` + new-annotations-map pattern as the Phase 4 gate-policy code. Outside Phase 4 scope but worth fixing in the same pass.
**Fix:** Add the same envtest unit test recommended in WR-03 for the budget bypass path.

### WR-15: `metriccardinality` analyzer only catches string literals, not identifiers / constants

**File:** `tools/analyzers/metriccardinality/analyzer.go:62-74`
**Issue:** Analyzer only flags `*ast.BasicLit` of kind `STRING`. A caller writing `const taskLabel = "task"; prometheus.NewCounterVec(..., []string{taskLabel})` produces an `*ast.Ident` for `taskLabel`, which the analyzer silently passes. Known limitation of pattern-matching analyzers.
**Fix:** Document the escape hatch in `doc.go`. Optionally extend the analyzer with `go/types` to resolve const declarations to their string values (significant rework — judge cost/benefit).

---

_Reviewed: 2026-05-19_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
