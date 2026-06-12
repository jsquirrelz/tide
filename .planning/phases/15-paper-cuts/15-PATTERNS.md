# Phase 15: Paper Cuts - Pattern Map

**Mapped:** 2026-06-12
**Files analyzed:** 12 new/modified files
**Analogs found:** 12 / 12

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/owner/label.go` (NEW) | utility | transform | `internal/owner/owner.go` | exact (same package, same metadata-seam role) |
| `internal/reporter/materialize.go` | service | CRUD | self + `internal/owner/owner.go` | exact |
| `internal/controller/milestone_controller.go` | controller | CRUD | `internal/controller/phase_controller.go` | exact |
| `internal/controller/phase_controller.go` | controller | CRUD | self (AwaitingApproval block already fixed) | exact |
| `internal/controller/plan_controller.go` | controller | CRUD | self (reconcileWaveMaterialization) | exact |
| `internal/webhook/v1alpha1/plan_webhook.go` | middleware | request-response | self | exact |
| `cmd/tide/artifact_get_run.go` | utility | file-I/O | `cmd/tide/tail.go` | exact (same pod-log-stream architecture) |
| `cmd/dashboard/api/waves.go` (NEW) | service | event-driven | `cmd/dashboard/api/informer_bridge.go` + `cmd/dashboard/api/projects.go` | role-match |
| `dashboard/web/src/components/StatusBadge.tsx` | component | transform | self | exact |
| `dashboard/web/src/components/PlanningDAGView.tsx` | component | event-driven | self | exact |
| `dashboard/web/src/components/ProjectPicker.tsx` | component | request-response | self | exact |
| `dashboard/web/src/components/RunningWavesView.tsx` (NEW) | component | event-driven | `dashboard/web/src/components/PlanningDAGView.tsx` | role-match |
| `dashboard/web/src/App.tsx` | component | event-driven | self | exact |

---

## Pattern Assignments

### `internal/owner/label.go` (NEW) — utility, transform

**Analog:** `internal/owner/owner.go` (lines 1-64)

**Package/imports pattern** (owner.go lines 1-38):
```go
/*
Copyright 2026 TIDE Authors.
...Apache 2.0 header...
*/

// Package owner provides a helper for setting controller-style owner references
// with same-namespace enforcement.
package owner

import (
    "fmt"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
```

**Core helper pattern** (owner.go lines 50-64 — exact structure to copy):
```go
// EnsureOwnerRef sets a controller-style owner reference on child pointing
// to parent. The reference has Controller=true and BlockOwnerDeletion=true
// so the K8s garbage collector cascades parent deletion to child (CRD-02).
//
// Returns an error if:
//   - parent is nil
//   - child is nil
//   - parent and child are in different namespaces (Pitfall 23 prevention)
//
// On the cross-namespace failure path, the child is NOT mutated.
func EnsureOwnerRef(child, parent metav1.Object, scheme *runtime.Scheme) error {
    if parent == nil {
        return fmt.Errorf("owner: parent is nil")
    }
    // ... guard pattern ...
}
```

**New `StampProjectLabel` function to write** (mirrors the nil-guard + no-op pattern of EnsureOwnerRef):
```go
// StampProjectLabel stamps the canonical tideproject.k8s/project label on obj.
// Must be called at every child CR create site BEFORE c.Create().
// No-op if projectName is empty (fail-open: don't prevent creation).
func StampProjectLabel(obj metav1.Object, projectName string) {
    if projectName == "" {
        return
    }
    labels := obj.GetLabels()
    if labels == nil {
        labels = make(map[string]string)
    }
    labels["tideproject.k8s/project"] = projectName
    obj.SetLabels(labels)
}
```

**Label key constant** (from `cmd/tide/inspect_wave_run.go` lines 37-40 — exact constant name to reuse):
```go
const (
    labelProject   = "tideproject.k8s/project"
    labelWaveIndex = "tideproject.k8s/wave-index"
)
```
Note: define the constant in the `owner` package (or use the raw string — inspect_wave_run.go uses a local const because it is in `package main`).

---

### `internal/reporter/materialize.go` — service, CRUD (CUTS-01)

**Analog:** `internal/reporter/materialize.go` (the file is its own analog — patch only)

**Call site to add** (lines 252-263 — between `stampParentRef` call and `owner.EnsureOwnerRef` call):
```go
// After stampParentRef(obj, parent.GetName()) on line 252:
stampParentRef(obj, parent.GetName())

// ADD HERE:
owner.StampProjectLabel(obj, parent.GetLabels()["tideproject.k8s/project"])

if err := owner.EnsureOwnerRef(obj, parent, scheme); err != nil {
```

**projectName source:** `parent.GetLabels()["tideproject.k8s/project"]` — the reporter Job runs against a Project whose label should already be set. `StampProjectLabel` is a no-op on empty string so a missing parent label fails-open (Pitfall 1 in RESEARCH.md).

---

### `internal/controller/milestone_controller.go` + `internal/controller/phase_controller.go` — controller, CRUD (CUTS-01 backfill, D-03)

**Analog for backfill pattern:** `internal/controller/plan_controller.go` lines 1280-1299 (`stampTaskLabels` label-presence guard + MergeFrom patch)

**Backfill pattern to copy** (plan_controller.go lines 1282-1298):
```go
// Guard: only patch if label is MISSING (idempotent — avoids re-triggering reconcile)
if t.Labels["tideproject.k8s/wave-index"] == waveIndexStr &&
    (projectName == "" || t.Labels["tideproject.k8s/project"] == projectName) {
    continue
}
patch := client.MergeFrom(t.DeepCopy())
if t.Labels == nil {
    t.Labels = map[string]string{}
}
t.Labels["tideproject.k8s/project"] = projectName
if err := r.Patch(ctx, t, patch); err != nil {
    return fmt.Errorf("stamp task labels on %s: %w", t.Name, err)
}
```

**Adapted for milestone/phase reconcilers:**
```go
// In MilestoneReconciler/PhaseReconciler — backfill project label on observed child CRs
// (D-03: idempotent self-healing for existing unlabeled CRs)
if child.Labels[labelProject] == "" {
    projectName := resolveProjectLabelFromOwnerChain(ctx, r.Client, &child)
    if projectName != "" {
        patch := client.MergeFrom(child.DeepCopy())
        if child.Labels == nil {
            child.Labels = map[string]string{}
        }
        child.Labels[labelProject] = projectName
        if err := r.Patch(ctx, &child, patch); err != nil {
            return fmt.Errorf("backfill project label on %s: %w", child.Name, err)
        }
    }
}
```

**Project resolution helper pattern** (plan_controller.go lines 1303-1320 — `resolveProjectName`):
```go
func (r *PlanReconciler) resolveProjectName(ctx context.Context, plan *tideprojectv1alpha1.Plan) (string, error) {
    // Fast path: label stamped on this Plan.
    if name, ok := plan.Labels["tideproject.k8s/project"]; ok && name != "" {
        return name, nil
    }
    // Owner-ref chain walk: Plan→Phase→Milestone→Project (via Spec.PhaseRef).
    if project := r.resolveProjectForPlan(ctx, plan); project != nil {
        return project.Name, nil
    }
    return "", ErrParentUnresolved
}
```

---

### `internal/controller/phase_controller.go` (CUTS-03 regression test) — controller, CRUD

**Analog:** `internal/controller/phase_controller.go` lines 197-233 (already fixed AwaitingApproval early-return)

**Exact code the regression test must pin** (phase_controller.go lines 206-233):
```go
if ph.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(ph, "phase") {
        // Consume annotation (T-04-G2 one-shot).
        newAnno := gates.ConsumeApprove(ph, "phase")
        annoPatch := client.MergeFrom(ph.DeepCopy())
        ph.SetAnnotations(newAnno)
        if err := r.Patch(ctx, ph, annoPatch); err != nil {
            return ctrl.Result{}, err
        }
        // Return to Running + record ApprovedByUser condition (D-04).
        statusPatch := client.MergeFrom(ph.DeepCopy())
        ph.Status.Phase = "Running"
        meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
            Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
            Status:             metav1.ConditionFalse,
            Reason:             tideprojectv1alpha1.ReasonApprovedByUser,
            Message:            "Phase approved; children will dispatch",
            LastTransitionTime: metav1.Now(),
        })
        if err := r.Status().Patch(ctx, ph, statusPatch); err != nil {
            return ctrl.Result{}, err
        }
        return ctrl.Result{Requeue: true}, nil
    }
    return ctrl.Result{}, nil  // no requeue — stay parked
}
```
The regression test must assert: Phase at AwaitingApproval + no approve annotation → Status.Phase stays `AwaitingApproval` across 3 reconcile cycles (the `return ctrl.Result{}, nil` on line 232 is the invariant — no requeue = no re-entry).

---

### `internal/controller/plan_controller.go` (CUTS-07 dispatch gate) — controller, CRUD

**Analog:** Dispatch gate stack, plan_controller.go lines 292-330 (AwaitingApproval/Rejected/BillingHalt/BudgetBlocked gates)

**Gate ordering pattern** (plan_controller.go lines 292-330):
```go
// D-02 descent hold
if held, hErr := checkParentApproval(...); held {
    return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
}
// D-05 reject hold
earlyProject := r.resolveProjectForPlan(ctx, plan)
if earlyProject != nil && gates.CheckRejected(earlyProject) {
    res, err := r.patchPlanRejected(ctx, plan, gates.RejectedReason(earlyProject))
    return res, true, err
}
// BillingHalt hold
if checkBillingHalt(earlyProject) { ... }
// BudgetBlocked hold
if checkBudgetBlocked(earlyProject) && !budget.IsBypassed(...) { ... }
```

**Park function pattern to replicate** for `patchPlanFileTouchMismatch` (plan_controller.go lines 701-714):
```go
func (r *PlanReconciler) patchPlanRejected(ctx context.Context, plan *tideprojectv1alpha1.Plan, reason string) (ctrl.Result, error) {
    patch := client.MergeFrom(plan.DeepCopy())
    meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
        Status:             metav1.ConditionTrue,
        Reason:             tideprojectv1alpha1.ReasonRejectedByUser,
        Message:            fmt.Sprintf("Rejected: %s", reason),
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, plan, patch); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}
```

**New `patchPlanFileTouchMismatch` adapts this pattern:** sets `plan.Status.ValidationState = "FileTouchMismatch"` AND a condition with `Reason: "FileTouchMismatch"`, `Message: summariseMismatches(mismatches)`. Returns `ctrl.Result{}` (no requeue — next Task event triggers re-entry, matching how `patchPlanRejected` requeues only for annotation polling; file-touch park re-checks when Tasks update).

**Gate insertion point** in `reconcileWaveMaterialization` (plan_controller.go lines 918-955 — after the ValidationState check, before `dag.ComputeWaves`):
```go
// CUTS-07 insertion point: plan_controller.go line ~918
// After: if plan.Status.ValidationState != "Validated" { ... }
// Before: nodes, edges := tasksToDAGLocal(taskList.Items)
if len(taskList.Items) > 0 {
    project := r.resolveProjectForPlan(ctx, plan)
    mode := webhookv1alpha1.ResolveFileTouchMode(plan, project, r.DefaultFileTouchMode)
    mismatches := webhookv1alpha1.ComputeFileTouchMismatches(taskList.Items)
    if len(mismatches) > 0 && mode == "strict" {
        return r.patchPlanFileTouchMismatch(ctx, plan, mismatches)
    }
}
```

**Export `computeFileTouchMismatches`** in `internal/webhook/v1alpha1/plan_webhook.go` (line 247): rename from `computeFileTouchMismatches` to `ComputeFileTouchMismatches`. Same for `summariseMismatches` → `SummariseMismatches`. No functional changes.

---

### `internal/webhook/v1alpha1/plan_webhook.go` (CUTS-07/D-08 mode resolution fix) — middleware, request-response

**Analog:** self (lines 164-175, the nil-project mode resolution that must be upgraded)

**Current code** (plan_webhook.go line 174):
```go
// Phase 2 trade-off per RESEARCH.md Open Question #1: the webhook does NOT walk
// owner refs to find the Project (would add 3 Gets per validate).
mode := ResolveFileTouchMode(plan, nil, v.DefaultFileTouchMode)
```

**After fix** (D-08 — real project resolution, nil fallback preserved):
```go
// D-08: resolve the actual Project so mode precedence uses project.Spec.FileTouchMode,
// not just the annotation or helm default. 3 Gets per validate-accept (acceptable).
project := resolveProjectForWebhook(ctx, v.Client, plan)
mode := ResolveFileTouchMode(plan, project, v.DefaultFileTouchMode)
```
`resolveProjectForWebhook` follows the same owner-ref chain walk as `resolveProjectForPlan` in plan_controller.go:1316 but uses a non-receiver form (webhook has `v.Client`, not `r.Client`).

---

### `cmd/tide/artifact_get_run.go` — utility, file-I/O (CUTS-04)

**Analog:** `cmd/tide/tail.go` (full file — inspector pod follows the same kubernetes.NewForConfig + GetLogs architecture)

**Imports pattern** (tail.go lines 38-53):
```go
import (
    "context"
    "fmt"
    "io"
    "strings"

    "github.com/spf13/cobra"
    corev1 "k8s.io/api/core/v1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/kubernetes"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)
```

**Clientset construction pattern** (tail.go lines 239-243 in `newTailCmd`):
```go
cfg, err := RESTConfig()
if err != nil {
    return err
}
cs, err := kubernetes.NewForConfig(cfg)
if err != nil {
    return fmt.Errorf("build kubernetes clientset: %w", err)
}
```

**Log-stream pattern** (tail.go lines 131-160 — `defaultTailStreamer`):
```go
req := cs.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
    Follow:    true,
    Container: container,
})
stream, err := req.Stream(ctx)
if err != nil {
    return fmt.Errorf("open log stream for pod/%s: %w", pod, err)
}
defer func() { _ = stream.Close() }()

go func() {
    <-ctx.Done()
    _ = stream.Close()
}()

if _, err := io.Copy(out, stream); err != nil && ctx.Err() == nil {
    return fmt.Errorf("read log stream: %w", err)
}
```

**Inspector pod command pattern** (from RESEARCH.md — the wait-then-cat shell loop):
```go
// Pod spec command — blocks until file exists then cats it (D-11 race-free wait)
command: []string{"sh", "-c",
    fmt.Sprintf("until [ -f /workspace/%s ]; do sleep 1; done; cat /workspace/%s", path, path),
}
```

**Defer delete pattern** (analogous to tail.go deferred stream.Close):
```go
if _, err := cs.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
    return fmt.Errorf("create inspector pod: %w", err)
}
defer func() {
    _ = cs.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
}()
```

**Function-var testable seam pattern** (tail.go lines 66-71):
```go
// tailPodPicker resolves the active Pod + container for a Task.
// Function var so tests can inject without a live apiserver.
var tailPodPicker = defaultTailPodPicker
var tailStreamer = defaultTailStreamer
```
Apply same pattern to `artifactGetRun`: extract `var inspectorPodRunner = defaultInspectorPodRunner` so tests can inject a fake without a live cluster.

**PVC mount note** (RESEARCH.md Pitfall 4): use `subPath = project UID` (not project name) when mounting the PVC. The project UID is retrieved via `k.Get(ctx, client.ObjectKey{...}, &project)` before pod creation.

---

### `cmd/dashboard/api/waves.go` (NEW) — service, event-driven (CUTS-06)

**Analog 1:** `cmd/dashboard/api/informer_bridge.go` lines 103-158 (Task event handling + publish pattern)
**Analog 2:** `cmd/tide/inspect_wave_run.go` lines 57-136 (wave grouping logic)
**Analog 3:** `cmd/dashboard/api/projects.go` lines 40-160 (handler struct + writeJSON pattern)

**Handler struct pattern** (projects.go lines 44-48):
```go
type ProjectsHandler struct {
    Client client.Client
    Log    logr.Logger
}
```

**Task list + label grouping pattern** (inspect_wave_run.go lines 73-101):
```go
var list tidev1alpha1.TaskList
if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
    return fmt.Errorf("list tasks in %s: %w", ns, err)
}

// Group by (planRef, waveIndex) — mirrors inspect_wave_run.go grouping
for i := range list.Items {
    tk := &list.Items[i]
    if tk.Labels[labelProject] != projectName {
        continue
    }
    waveStr := tk.Labels[labelWaveIndex]
    wave, err := strconv.Atoi(waveStr)
    if err != nil {
        continue  // not yet stamped — skip
    }
    // ... append to group map keyed by (planRef, waveIndex)
}
```

**SSE event publication pattern** (informer_bridge.go lines 140-144):
```go
h.Publish(projectKey, hub.Event{
    Type: typePrefix + "." + verb,   // for waves: "waves.snapshot"
    JSON: json.RawMessage(buf),
})
```

**writeJSON helper** (projects.go — used throughout api package):
```go
writeJSON(w, http.StatusOK, payload)
```

**Running-wave filter** (from RESEARCH.md — a wave is "running" iff ≥1 task has phase ∈ {Running, Dispatching}):
```go
func isRunningPhase(phase string) bool {
    return phase == "Running" || phase == "Dispatching"
}
```

**JSON response shape** (from 15-RESEARCH.md Pattern 4):
```json
{
  "waves": [
    {
      "planName": "plan-02-implement-feature",
      "waveIndex": 1,
      "tasks": [
        {"name": "task-01", "status": "Running"},
        {"name": "task-02", "status": "Succeeded"}
      ]
    }
  ]
}
```

**SSE emit via bridge augmentation** (informer_bridge.go lines 147-158): on each Task `AddFunc`/`UpdateFunc`, re-derive the running-waves aggregate for the project and publish a `waves.snapshot` event. This piggybacks on the existing Task informer handler in `newKindHandler` — no new informer, just an additional `h.Publish` call in the `task.update` path.

**Route registration** (router.go lines 163-178 — no new route needed; `waves.snapshot` is a new event type over the existing `/events` SSE channel per D-15/Pitfall 6 in RESEARCH.md):
```go
// DO NOT add: r.Get("/projects/{name}/waves", ...)
// The waves.snapshot event type rides the existing events route.
```

---

### `dashboard/web/src/components/StatusBadge.tsx` — component, transform (CUTS-05)

**Analog:** self (full file, lines 1-187)

**StatusValue union** (lines 28-38 — add `"Complete"` after `"Succeeded"`):
```typescript
export type StatusValue =
  | "Pending"
  | "Dispatching"
  | "Running"
  | "AwaitingApproval"
  | "Paused"
  | "Succeeded"
  | "Complete"      // ADD: Project terminal-success (PhaseComplete, project_types.go:392)
  | "Failed"
  | "PushLeaseFailed"
  | "PushLeakBlocked"
  | "Rejected";
```

**STATUS_TABLE row to add** (after `Succeeded` row, lines 93-99 — exact same shape):
```typescript
Complete: {
  icon: CircleCheckBig,         // UI-SPEC C1: distinct icon from Succeeded (CircleCheck) per color-blindness rule
  iconName: "CircleCheckBig",
  label: "Complete",
  colorVar: "var(--color-status-success)",
  srDescription: "Complete — all milestones succeeded",
},
```

**New import to add** (line 4 — after `CircleCheck`):
```typescript
import {
  ...
  CircleCheck,
  CircleCheckBig,   // ADD for Complete badge
  ...
} from "lucide-react";
```

**data-testid follows from render path** (lines 174-175 — no change needed; `data-testid={`status-badge-${status}`}` already uses the status string, so `data-testid="status-badge-Complete"` is automatic).

---

### `dashboard/web/src/components/PlanningDAGView.tsx` — component, event-driven (CUTS-05)

**Analog:** self (lines 61-77)

**KNOWN array** (lines 61-72 — add `"Complete"`):
```typescript
const KNOWN: readonly StatusValue[] = [
  "Pending",
  "Dispatching",
  "Running",
  "AwaitingApproval",
  "Paused",
  "Succeeded",
  "Complete",    // ADD: prevents coerce("Complete") → "Pending" (run-1 finding-9b)
  "Failed",
  "PushLeaseFailed",
  "PushLeakBlocked",
  "Rejected",
];
```

**coerce function** (lines 73-77 — no change needed; it reads from `KNOWN` dynamically):
```typescript
function coerce(phase: string): StatusValue {
  return (KNOWN as readonly string[]).includes(phase)
    ? (phase as StatusValue)
    : "Pending";
}
```

**Optional consolidation** (UI-SPEC C2 recommendation): export `KNOWN_STATUS_VALUES` from `StatusBadge.tsx` derived from `Object.keys(STATUS_TABLE)` and replace KNOWN in PlanningDAGView.tsx and KNOWN_STATUSES in ProjectPicker.tsx with an import. Eliminates the silent-drift bug class permanently.

---

### `dashboard/web/src/components/ProjectPicker.tsx` — component, request-response (CUTS-05)

**Analog:** self (lines 30-47)

**KNOWN_STATUSES array** (lines 30-41 — add `"Complete"`, same as PlanningDAGView):
```typescript
const KNOWN_STATUSES: readonly StatusValue[] = [
  "Pending",
  "Dispatching",
  "Running",
  "AwaitingApproval",
  "Paused",
  "Succeeded",
  "Complete",    // ADD: second coerce site (RESEARCH A2 — verified)
  "Failed",
  "PushLeaseFailed",
  "PushLeakBlocked",
  "Rejected",
] as const;
```

---

### `dashboard/web/src/components/RunningWavesView.tsx` (NEW) — component, event-driven (CUTS-06)

**Analog:** `dashboard/web/src/components/PlanningDAGView.tsx` (full file — same SSE subscription + snapshot-replace pattern)

**Imports pattern** (PlanningDAGView.tsx lines 1-24):
```typescript
import { useCallback, useEffect, useState } from "react";
import { Waves } from "lucide-react";         // wave-card kind icon (UI-SPEC)
import StatusBadge from "./StatusBadge";
import { useSSEStream } from "../lib/sse";
import { projectEventsURL } from "../lib/tasks";
import type { StatusValue } from "./StatusBadge";
import { clsx } from "../lib/clsx";
```

**Props interface** (from 15-UI-SPEC C3):
```typescript
export type RunningWaveTask = { name: string; status: string };
export type RunningWave = {
  planName: string;
  waveIndex: number;
  tasks: RunningWaveTask[];
};
type Props = {
  projectName: string;
  onPlanClick: (planName: string) => void;
  initialSnapshot?: RunningWave[];   // tests bypass SSE, mirrors PlanningDAGView.initialData
};
```

**SSE subscription pattern** (PlanningDAGView.tsx — `useSSEStream` + named event type):
```typescript
// PlanningDAGView listens for planning-level events on the existing channel.
// RunningWavesView listens for waves.snapshot on the SAME channel URL.
const sseURL = projectEventsURL(projectName);
useEffect(() => {
    // subscribe to "waves.snapshot" named event (NOT the generic onmessage)
    // See UI-SPEC C3: "waves.snapshot must be added to SSE_PROJECT_EVENT_TYPES
    // in dashboard/web/src/lib/sse.ts"
    ...
}, [projectName]);
```

**Snapshot-replace state** (PlanningDAGView's pattern of replacing full state on each SSE event):
```typescript
const [waves, setWaves] = useState<RunningWave[] | null>(initialSnapshot ?? null);
// On waves.snapshot event: setWaves(parsed.waves)  — full replace, no merge
```

**Loading / empty states** (from 15-UI-SPEC C3 — mirrors EmptyState component pattern):
```typescript
if (waves === null) return <Loader2 className="animate-spin" />;   // L2 pane-loading
if (waves.length === 0) return <EmptyState variant="no-running-waves" />;
```

**Card chrome** (from 15-UI-SPEC C3 — mirrors TideNodeShell two-row pattern):
```typescript
<div
  role="button"
  tabIndex={0}
  data-testid={`wave-card-${planName}-${waveIndex}`}
  data-plan={planName}
  data-wave-index={waveIndex}
  aria-label={`plan ${planName}, wave ${waveIndex}, ${running} of ${total} tasks running`}
  className="rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] cursor-pointer hover:bg-[var(--color-surface-overlay)]"
  onClick={() => onPlanClick(planName)}
  onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onPlanClick(planName); }}
>
  {/* header row */}
  <div className="flex items-center gap-2 px-3 py-2">
    <Waves size={14} aria-hidden className="text-[var(--color-text-muted)]" />
    <span className="min-w-0 flex-1 truncate" style={{ fontFamily: "var(--font-mono)", fontSize: "13px", fontWeight: 600 }} title={planName}>
      {planName}
    </span>
    <span className="shrink-0" style={{ fontFamily: "var(--font-mono)", fontSize: "12px", fontWeight: 600, color: "var(--color-text-muted)" }}>
      WAVE {waveIndex} · {running}/{total} running
    </span>
  </div>
  {/* divider */}
  <hr className="border-[var(--color-border-subtle)]" />
  {/* chip row */}
  <div className="flex flex-wrap gap-2 px-3 py-2" aria-hidden="true">
    {tasks.map(t => (
      <span key={t.name} title={t.name} data-testid="wave-card-chip">
        <StatusBadge status={coerce(t.status)} />
      </span>
    ))}
  </div>
</div>
```

**coerce function** (PlanningDAGView.tsx lines 73-77 — copy verbatim or import the exported KNOWN_STATUS_VALUES if consolidation is done):
```typescript
function coerce(phase: string): StatusValue {
  return (KNOWN as readonly string[]).includes(phase) ? (phase as StatusValue) : "Pending";
}
```

**SSE_PROJECT_EVENT_TYPES integration pitfall** (from 15-UI-SPEC C3): add `"waves.snapshot"` to the event listener registration in `dashboard/web/src/lib/sse.ts` — named SSE events not in that list never reach `onMessage`.

---

### `dashboard/web/src/App.tsx` — component, event-driven (CUTS-06 + D-13)

**Analog:** self (lines 200-235)

**Right-pane empty-state replacement** (lines 217-229 — replace the div):
```typescript
// BEFORE:
{selectedPlan ? (
  <ExecutionDAGView ... />
) : (
  <div className="flex h-full items-center justify-center text-[var(--color-text-muted)]"
       style={{ fontFamily: "var(--font-mono)", fontSize: "13px" }}>
    Select a plan to view its execution DAG
  </div>
)}

// AFTER (D-13):
{selectedPlan ? (
  <ExecutionDAGView ... />
) : (
  <RunningWavesView
    projectName={selectedProject ?? ""}
    onPlanClick={onPlanClick}
  />
)}
```

**PaneHeader return affordance** (D-13 return button — `PaneHeader` gains optional `action` prop):
```typescript
// In the EXECUTION pane header (when selectedPlan !== null):
<PaneHeader label="EXECUTION" action={
  selectedPlan ? (
    <button
      data-testid="execution-pane-all-waves"
      aria-label="Show all running waves"
      onClick={() => { setSelectedPlan(null); /* clear hash */ }}
      style={{ fontFamily: "var(--font-mono)", fontSize: "12px", fontWeight: 600, color: "var(--color-text-muted)" }}
    >
      All waves
    </button>
  ) : undefined
} />
```

---

## Shared Patterns

### Condition + ValidationState park (CUTS-07, CUTS-01 backfill)
**Source:** `internal/controller/plan_controller.go` lines 701-714 (`patchPlanRejected`)
**Apply to:** `patchPlanFileTouchMismatch` in plan_controller.go
```go
// Park pattern: DeepCopy → MergeFrom → SetStatusCondition → Status().Patch
patch := client.MergeFrom(plan.DeepCopy())
meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
    Status:             metav1.ConditionTrue,
    Reason:             "FileTouchMismatch",   // or existing Reason constant
    Message:            summariseMismatches(mismatches),
    LastTransitionTime: metav1.Now(),
})
// Also set ValidationState (distinct from patchPlanRejected which does not):
plan.Status.ValidationState = "FileTouchMismatch"
if err := r.Status().Patch(ctx, plan, patch); err != nil {
    return ctrl.Result{}, err
}
return ctrl.Result{}, nil   // no requeue; next Task event triggers re-entry
```

### Label-only patch (CUTS-01 backfill)
**Source:** `internal/controller/plan_controller.go` lines 1286-1298 (`stampTaskLabels`)
**Apply to:** milestone_controller.go and phase_controller.go backfill paths
```go
// Idempotent metadata patch: only when label is missing
patch := client.MergeFrom(obj.DeepCopy())
if obj.GetLabels() == nil {
    obj.SetLabels(map[string]string{})
}
obj.GetLabels()["tideproject.k8s/project"] = projectName
if err := r.Patch(ctx, obj, patch); err != nil {
    return fmt.Errorf("backfill project label on %s: %w", obj.GetName(), err)
}
```

### SSE named event type (CUTS-06)
**Source:** `cmd/dashboard/api/informer_bridge.go` lines 140-144 + `cmd/dashboard/api/events_sse.go` lines 217-229
**Apply to:** `cmd/dashboard/api/waves.go` (new file)
```go
// Server side: publish waves.snapshot
h.Publish(projectKey, hub.Event{
    Type: "waves.snapshot",
    JSON: json.RawMessage(buf),
})

// Client side (sse.ts): register addEventListener("waves.snapshot", ...)
// See UI-SPEC C3: SSE_PROJECT_EVENT_TYPES must include "waves.snapshot"
```

### Apache 2.0 file header
**Source:** Every Go file in the repo (e.g., `internal/owner/owner.go` lines 1-15)
**Apply to:** All new Go files (`internal/owner/label.go`, `cmd/dashboard/api/waves.go`)
```go
/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
...
*/
```

---

## No Analog Found

All files in Phase 15 have close analogs. No entries here.

---

## Metadata

**Analog search scope:** `internal/owner/`, `internal/reporter/`, `internal/controller/`, `internal/webhook/v1alpha1/`, `cmd/tide/`, `cmd/dashboard/api/`, `cmd/dashboard/hub/`, `dashboard/web/src/components/`, `dashboard/web/src/App.tsx`
**Files scanned:** 22
**Pattern extraction date:** 2026-06-12
