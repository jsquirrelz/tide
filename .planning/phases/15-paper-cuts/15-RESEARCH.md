# Phase 15: Paper Cuts — Research

**Researched:** 2026-06-12
**Domain:** Go controller-runtime reconciler gates, Kubernetes Pod API, React/TypeScript dashboard, CLI pod-log streaming
**Confidence:** HIGH (all findings verified against codebase source)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Label fix + approve discovery (CUTS-01)**
- D-01: Universal label stamping — shared helper stamps `tideproject.k8s/project` on every child CR at every create site, including `internal/reporter/materialize.go` (currently zero labels) AND all reconciler create sites.
- D-02: `tide approve` discovery unchanged — keeps existing label-filter shape; no OwnerRef fallback.
- D-03: Reconciler backfill — reconcilers patch the missing project label onto observed unlabeled CRs, deriving project from OwnerRef chain. Idempotent, self-healing.
- D-04: Project label only — no broader role/level label parity for reporter-created CRs.

**File-touch enforcement (CUTS-07)**
- Root cause confirmed: `computeFileTouchMismatches` in the webhook early-returns when zero Tasks are visible at Plan admission (Pitfall B, plan_webhook.go:139-147). Reporter flow always creates Tasks AFTER Plan, so the check never ran.
- D-05: PlanReconciler dispatch gate is the authoritative seat — re-runs the mismatch check once all Tasks are materialized, BEFORE wave derivation/dispatch. Sets the dormant `ValidationState=FileTouchMismatch` value (plan_types.go:45).
- D-06: Strict mismatch parks, never fails. No Jobs dispatch; Plan parks with `ValidationState=FileTouchMismatch` + condition. Consistent with Phase 12 D-05 park-not-fail.
- D-07: Planner prompt patch — sibling tasks in a wave must not share files; declare dependsOn or split the work.
- D-08: Webhook stays AND gets real mode resolution — admission check remains as early layer, upgraded to resolve actual project file-touch mode (currently passes nil project at line 174).

**artifact-get execution (CUTS-04)**
- D-09: Bare inspector Pod + log stream. CLI creates short-lived inspector Pod mounting the per-project PVC, streams container logs.
- D-10: Raw bytes to stdout, status to stderr. Pipeable.
- D-11: Wait for artifact READINESS — never partial reads. Completeness-detection mechanism is planner/researcher discretion, must be race-free.
- D-12: Plain error on genuinely missing path after wait window exhausted.

**Cross-plan wave view (CUTS-06)**
- D-13: Right-pane default — aggregate all-running-waves view replaces "Select a plan" empty state.
- D-14: Rich wave cards — per running wave: plan name, wave index, running/total count, task chips reusing StatusBadge.
- D-15: Server-side aggregate + SSE — manager API derives running-waves aggregate via label-selector queries over Tasks, delivered over existing SSE channel.
- D-16: Click-through navigates — clicking a wave card selects that plan; right pane swaps to ExecutionDAGView.

**Already-fixed cuts (verify, don't rebuild)**
- CUTS-02: Commit 8f0b99b (Phase 11) made boundary push skip empty commit on clean tree. Tests at cmd/tide-push/main_test.go:996-1045. Deliverable: verify run-1 symptom path fully covered; add test only if gap found.
- CUTS-03: Commit abf177c (Phase 12-01) added AwaitingApproval early-return (phase_controller.go:197-206). Deliverable: verify convergence; pin with regression test if not already covered.

### Claude's Discretion

- CUTS-05 chip fix shape: map Project `Complete` to Succeeded-equivalent styling per UI-SPEC status vocabulary.
- Shared label-stamping helper placement (internal/owner is a candidate).
- artifact-get timeout flag default and RBAC for inspector-pod creation.
- Regression-test vehicle per cut (envtest vs kind Layer B vs Vitest).
- Whether `inspect_wave` CLI machinery shares anything with the new running-waves aggregate endpoint.

### Deferred Ideas (OUT OF SCOPE)

- Wave-view naming via the tide metaphor (e.g., "currents").
- Artifact browsing/listing UX (`tide artifact-ls` or dashboard artifact browser).
- OwnerRef fallback discovery in `tide approve`.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CUTS-01 | Reporter-created Milestone/Phase CRs carry `tideproject.k8s/project` label so `tide approve` discovers gated levels | materialize.go confirmed zero label stamps; approve.go uses label-filter at :304-319; owner package is right home for shared helper |
| CUTS-02 | Boundary push no-ops cleanly on a clean tree | Fixed at cmd/tide-push/main.go:488-496; test at main_test.go:996-1045 covers the boundary path; gap: `tide push` CLI command surface (not `tide-push` binary) may need checking |
| CUTS-03 | Phase CRs stop oscillating AwaitingApproval↔Running | Fixed at phase_controller.go:197-206 AwaitingApproval early-return; regression test needed |
| CUTS-04 | `tide artifact-get` runs inspector pod for real instead of dry-run printing spec | Stub confirmed at artifact_get_run.go:65-86; PVC is "tide-projects"; kubernetes.NewForConfig pattern exists in tail.go for log streaming |
| CUTS-05 | Dashboard project-node status chip maps CR status `Complete` correctly | Bug confirmed: KNOWN array in PlanningDAGView.tsx:61-72 has 10 values but no "Complete"; coerce() falls through to "Pending" |
| CUTS-06 | Dashboard offers cross-plan "all running waves right now" view | wave-index label confirmed on Tasks; inspect_wave_run.go is reusable pattern; SSE hub pattern established in events_sse.go |
| CUTS-07 | Sibling tasks in one wave cannot both declare same file under strict fileTouchMode | computeFileTouchMismatches confirmed correct and exportable; Pitfall B at plan_webhook.go:139-147 confirmed; ValidationState=FileTouchMismatch enum value present but never set |
</phase_requirements>

---

## Summary

Phase 15 closes seven run-1 regressions. Two (CUTS-02, CUTS-03) are already fixed on main and require only regression test confirmation. The remaining five are new implementation: a universal project-label stamping helper (CUTS-01), an inspector Pod for artifact streaming (CUTS-04), a one-line status union extension (CUTS-05), a running-waves dashboard view over SSE (CUTS-06), and a PlanReconciler dispatch gate for file-touch conflict enforcement (CUTS-07).

The codebase has all necessary infrastructure. `computeFileTouchMismatches` in plan_webhook.go is correct and reusable; it only needs to be exported/relocated and called from the reconciler. The `tideproject.k8s/wave-index` label is already stamped on Tasks by `stampTaskLabels`, making server-side wave aggregation straightforward. The `inspect_wave_run.go` list-by-label pattern in cmd/tide is directly reusable for the SSE aggregate. The `tail.go` log-streaming architecture using `kubernetes.NewForConfig` + `CoreV1().Pods().GetLogs()` is the template for the artifact-get inspector pod pattern.

**Primary recommendation:** Plan each cut as a self-contained sub-unit with its regression test. Order: CUTS-02/03 verification first (no new code, quick), CUTS-05 (trivial UI line), CUTS-01 (systemic label fix), CUTS-07 (reconciler gate), CUTS-04 (inspector pod), CUTS-06 (SSE view).

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Project label stamping (CUTS-01) | API/Backend (reporter + reconcilers) | — | Labels are CRD metadata; reporter is the create-site |
| Label backfill for existing CRs (CUTS-01) | API/Backend (reconcilers) | — | Reconcilers observe and patch CRD metadata |
| `tide approve` discovery (CUTS-01) | CLI (cmd/tide) | — | Already uses label-filter; no change needed |
| `tide push` clean-tree behavior (CUTS-02) | CLI (cmd/tide-push) | — | Push binary owns git operations |
| Phase status flapping (CUTS-03) | API/Backend (PhaseReconciler) | — | Controller reconcile loop owns status transitions |
| artifact-get inspector Pod (CUTS-04) | CLI (cmd/tide) | K8s (core/v1 Pod) | CLI creates pod and streams logs from cluster |
| Dashboard status chip `Complete` (CUTS-05) | Frontend (React) | — | Status union in PlanningDAGView.tsx coerce() |
| Running-waves aggregate (CUTS-06) | API/Backend (dashboard) | Frontend (React) | Server derives via label queries per spec; client renders |
| File-touch dispatch gate (CUTS-07) | API/Backend (PlanReconciler) | Admission Webhook (existing) | Reconciler is authoritative for reporter flow |
| Planner prompt patch (CUTS-07) | Template (internal/subagent/common) | — | plan_planner.tmpl is the prompt content |

---

## Standard Stack

No new external packages are introduced by this phase. All implementation uses existing project dependencies.

### Core (already in go.mod)
| Library | Version | Purpose | Role in Phase 15 |
|---------|---------|---------|-----------------|
| sigs.k8s.io/controller-runtime | v0.24.x | Reconciler, client, SSE plumbing | Reconciler dispatch gate (CUTS-07), backfill patch (CUTS-01) |
| k8s.io/client-go/kubernetes | (controller-runtime pinned) | CoreV1 Pod API, log streaming | Inspector pod create/delete/log stream (CUTS-04) |
| k8s.io/api/core/v1 | (controller-runtime pinned) | Pod spec types | Inspector pod spec (CUTS-04) |
| go-chi/chi/v5 | v5.x | Router — new SSE endpoint | Running-waves aggregate route (CUTS-06) |
| React 18 + TypeScript | 18.x | Dashboard frontend | StatusBadge fix (CUTS-05), wave view (CUTS-06) |
| @xyflow/react | v12 | React Flow nodes | Wave cards reuse existing node primitives |

### No New Package Installs
This phase does not install any new packages. All capabilities are satisfied by existing dependencies already in go.mod and package.json.

---

## Package Legitimacy Audit

> No new packages are introduced. This section is not applicable.

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

---

## Architecture Patterns

### System Architecture Diagram

```
CUTS-01 Label Path:
  reporter Job → MaterializeChildCRDs → [NEW: stampProjectLabel helper] → Create CRs with label
  ReconcilerA (milestone/phase) → observe child with empty label → [NEW: backfill patch] → patch label from OwnerRef chain
  CLI: tide approve → findAwaiting* → label-filter (tideproject.k8s/project=<proj>) → finds CR first call ✓

CUTS-04 artifact-get:
  CLI: tide artifact-get ns/proj/path
    → parseArtifactRef (existing)
    → create inspector Pod (busybox, PVC mount at /workspace, subPath=proj-UID)
    → wait for Pod Running (poll or watch)
    → [WAIT LOOP]: file exists? → yes → stream logs → stdout
    → delete Pod (defer)

CUTS-06 Running-waves view:
  Manager SSE hub ← Task informer events (kind=Task)
    new SSE event type: "running_waves"
    handler: List Tasks in namespace → filter by wave-index label → group by (planRef, wave-index) → count Running → emit JSON
  Browser EventSource → wave cards render ← decode "running_waves" event
    click card → setSelectedPlan(card.planRef) → right pane = ExecutionDAGView

CUTS-07 File-touch gate:
  PlanReconciler.reconcileWaveMaterialization:
    tasks = List(planRef = plan.Name)
    if len(tasks) == 0 → return (tasks not materialized yet)
    mismatches = computeFileTouchMismatches(tasks)  [exported from webhook pkg]
    mode = ResolveFileTouchMode(plan, project, r.DefaultFileTouchMode)
    if mismatches && mode == "strict" → park Plan (ValidationState=FileTouchMismatch + condition)
    else → proceed with wave derivation
```

### Recommended Project Structure (no new directories)

```
internal/
├── reporter/
│   └── materialize.go          # add stampProjectLabel call in MaterializeChildCRDs
├── owner/
│   └── label.go (NEW)          # OR: labels.go — shared StampProjectLabel helper
├── controller/
│   ├── plan_controller.go      # CUTS-07: add file-touch gate in reconcileWaveMaterialization
│   ├── milestone_controller.go # CUTS-01: add backfill in reconcile loop
│   └── phase_controller.go     # CUTS-01: add backfill in reconcile loop
├── webhook/v1alpha1/
│   └── plan_webhook.go         # CUTS-07/D-08: real mode resolution (nil → project)
cmd/
├── tide/
│   ├── artifact_get_run.go     # CUTS-04: replace dry-run with real inspector pod + log stream
│   └── runners.go              # CUTS-04: update runArtifactGet to call real impl
cmd/dashboard/
├── api/
│   └── waves.go (NEW)          # CUTS-06: running-waves aggregate handler
└── router.go                   # CUTS-06: register new route
dashboard/web/src/
├── components/
│   ├── PlanningDAGView.tsx      # CUTS-05: add "Complete" to KNOWN array
│   └── RunningWavesView.tsx (NEW) # CUTS-06: aggregate wave card UI
internal/subagent/common/templates/
└── plan_planner.tmpl            # CUTS-07/D-07: add sibling-file overlap guidance
```

### Pattern 1: Universal Label Stamping Helper (CUTS-01)

`internal/owner/label.go` (or `internal/owner/owner.go` extended) — the `internal/owner` package already owns the "authoritative child metadata" seam (EnsureOwnerRef). StampProjectLabel belongs next to it.

```go
// Source: codebase pattern — internal/owner/owner.go + internal/controller/plan_controller.go:1292-1294

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

**Where it must be called (CUTS-01 audit):**

| Call site | Currently stamps label? | projectName source |
|-----------|------------------------|-------------------|
| `internal/reporter/materialize.go` MaterializeChildCRDs | NO — confirmed zero label ops | parent.GetLabels()["tideproject.k8s/project"] or traverse OwnerRef chain |
| `internal/controller/milestone_controller.go` (creates Phase via reporter Job, not directly) | N/A — reporter Job does the create | — |
| `internal/controller/phase_controller.go` (creates Plan via reporter Job, not directly) | N/A — reporter Job does the create | — |
| `internal/controller/plan_controller.go` stampTaskLabels | YES — stamps on Tasks | resolved projectName |

The gap is exclusively in `MaterializeChildCRDs` — the reporter's create path. Reconciler create sites (push Jobs, Wave CRs) don't need the project label. The backfill (D-03) handles any CRs already in the cluster without the label.

**Backfill pattern (D-03):** MilestoneReconciler and PhaseReconciler check incoming CRs, patch missing label from OwnerRef chain. The OwnerRef chain is already set by EnsureOwnerRef; traversing it requires a Get per level (max 2 Gets: Phase → Milestone → Project). PlanReconciler already has `resolveProjectName` which does exactly this walk. A shared `resolveProjectLabelFromOwnerChain(ctx, client, obj)` function can be used across all three reconcilers.

### Pattern 2: CUTS-07 Dispatch Gate Slot (verified in plan_controller.go)

The file-touch gate belongs in `reconcileWaveMaterialization`, AFTER all Tasks are confirmed visible, BEFORE `dag.ComputeWaves`. The existing dispatch-entry gate stack in `reconcilePlannerDispatch` (AwaitingApproval → CheckRejected → BillingHalt → BudgetBlocked) is the correct model for gate ordering, but the file-touch gate operates at WAVE MATERIALIZATION time, not at planner dispatch time. It fires when Tasks already exist (the tasks-exist path in `reconcilePlannerDispatch` returns `dispatched=false`, routing to `reconcileWaveMaterialization`).

Insertion point in `reconcileWaveMaterialization` (plan_controller.go):

```go
// Source: internal/controller/plan_controller.go — before stampTaskLabels

// [NEW: CUTS-07 file-touch gate]
// computeFileTouchMismatches is exported from internal/webhook/v1alpha1 (or relocated).
// ResolveFileTouchMode walks annotation > resolved-cache > project.Spec > helm default.
project := r.resolveProjectForPlan(ctx, plan)
mode := v1alpha1webhook.ResolveFileTouchMode(plan, project, r.DefaultFileTouchMode)
mismatches := v1alpha1webhook.ComputeFileTouchMismatches(taskList.Items)
if len(mismatches) > 0 && mode == "strict" {
    return r.patchPlanFileTouchMismatch(ctx, plan, mismatches)
}
```

`patchPlanFileTouchMismatch` patches:
- `plan.Status.ValidationState = "FileTouchMismatch"` (plan_types.go:45 — dormant value brought alive)
- `meta.SetStatusCondition(&plan.Status.Conditions, ...)` with Reason=FileTouchMismatch, Message naming both tasks and shared path (from `summariseMismatches`)

The park means no `reconcileWaveBoundary` runs, no `stampTaskLabels`, no Jobs dispatch.

**Export/relocation decision for `computeFileTouchMismatches`:** The function is currently unexported in `internal/webhook/v1alpha1/plan_webhook.go`. Options:
1. Export it in-place (`ComputeFileTouchMismatches`) — simplest.
2. Move to a shared `internal/filetouchcheck` package — cleaner separation.

Option 1 is simpler and the planner should choose it unless CLAUDE.md convention favors extraction.

### Pattern 3: Inspector Pod + Log Stream (CUTS-04)

`tail.go` in cmd/tide is the established pattern for pod log streaming. The inspector pod implementation follows the same `kubernetes.NewForConfig` + `CoreV1().Pods(ns)` architecture:

```go
// Source: cmd/tide/tail.go (kubernetes.NewForConfig + GetLogs pattern)

func artifactGetRun(ctx context.Context, cfg *rest.Config, ns, project, path string, waitTimeout time.Duration, out, errOut io.Writer) error {
    cs, err := kubernetes.NewForConfig(cfg)
    // ...
    pod := buildInspectorPod(ns, project, pvcName, path, podName)
    if _, err := cs.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
        return fmt.Errorf("create inspector pod: %w", err)
    }
    defer cs.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
    // Wait for pod to reach Running/Succeeded state (wait for artifact)
    // Stream logs → out (raw bytes)
}
```

**PVC name resolution:** The stub hardcodes `tide-projects`. The real implementation should accept the PVC name as a flag (defaulting to `tide-projects`) matching the chart-provisioned PVC. `cmd/manager/main.go:382` shows `PVCName: "tide-projects"` as the default. No per-project separate PVC — the single shared RWX PVC uses per-Project subPaths.

**Wait-for-readiness mechanism (D-11):** The key constraint is "never partial reads — the authoring Job must have completed writing." The race-free completeness detection is: poll until the authoring Job (or whatever wrote the artifact) has `Status.Phase=Succeeded` or the file appears with stable size. The simplest implementation is: watch the Pod until Succeeded (container exited 0), which means `cat /workspace/artifacts/PATH` completed. If the authoring Job is still running, the cat command will block at the write end — but that is incorrect (we want to WAIT, not read partial). The planner must choose: either (a) check the authoring Job's Succeeded status before creating the inspector pod, or (b) use file-stability polling inside the inspector pod command (e.g., `while [ ! -f /workspace/path ]; do sleep 1; done; cat /workspace/path`). Option (b) is simpler from the CLI's perspective and is race-free.

**RBAC:** The `tide` CLI binary runs as an operator (kubectl-equivalent). It needs `pods: create/get/delete` and `pods/log: get` in the target namespace. There are currently no kubebuilder:rbac annotations on CLI commands (they're operator-side), so this is purely a documentation note. If TIDE's operator role needs to grant these verbs, the chart's ClusterRole would need updating — but that is a chart change. The CLI uses the operator's kubeconfig, so RBAC enforcement is on the cluster side.

### Pattern 4: SSE Running-Waves (CUTS-06)

The running-waves aggregate endpoint follows the existing events_sse.go SSE pattern exactly. A new SSE event type `running_waves` rides the existing project-scoped SSE channel at `/api/v1/projects/{name}/events`.

**Server-side derivation (D-15):** The informer_bridge.go already publishes events for every Task change to the project hub. The `ProjectsHandler` or a new `WavesHandler` can query Tasks by label on each SSE publish. The spec constraint is: "derive via label-selector queries, never store aggregates in CRD status." This means the handler lists Tasks matching:
- `tideproject.k8s/project = <projectName>`
- `status.phase = Running` (or client-side filter — informer cache supports client.MatchingLabels but not field selectors on custom resources without an index)

The wave-index label is already stamped. The aggregate shape:

```json
{
  "type": "running_waves",
  "waves": [
    {
      "planRef": "plan-02-implement-feature",
      "waveIndex": 1,
      "tasks": [
        {"name": "task-01", "status": "Running"},
        {"name": "task-02", "status": "Succeeded"}
      ]
    }
  ]
}
```

The `inspectWaveRun` function in `cmd/tide/inspect_wave_run.go` performs exactly this grouping (lines 77-101). The SSE handler can share this logic via a `computeRunningWaves(ctx, client, ns, projectName)` helper.

**Frontend (CUTS-06 + D-13/D-16):** Replace the empty-state `div` in App.tsx at line 217-229 with a `RunningWavesView` component. This component:
- Subscribes to the SSE channel (already established for the project)
- Renders wave cards per wave in the `running_waves` event
- On wave card click: calls `setSelectedPlan(card.planRef)` (existing callback pattern in App.tsx `onPlanClick`)

**`inspect_wave` sharing:** The `inspectWaveRun` function is reusable as-is for the wave aggregation logic if extracted to a shared helper. This is cleaner than duplicating the list-then-group logic in the SSE handler.

### Pattern 5: StatusBadge "Complete" Fix (CUTS-05)

The bug is in PlanningDAGView.tsx at the `KNOWN` constant (line 61-72) and the `coerce()` function (line 73-77). `"Complete"` is not in KNOWN, so `coerce("Complete")` returns `"Pending"`. The fix:

1. Add `"Complete"` to the `StatusValue` union in StatusBadge.tsx (the 10-value union becomes 11).
2. Add the `Complete` row to `STATUS_TABLE` in StatusBadge.tsx — use `CircleCheck` icon and `var(--color-status-success)` matching `Succeeded` (same terminal-success presentation, different label).
3. Add `"Complete"` to the `KNOWN` array in PlanningDAGView.tsx.
4. Add `"Complete"` to the same `KNOWN`/`coerce` equivalent in ProjectPicker.tsx (`coerceStatus` at line 43).

**Why "Complete" ≠ "Succeeded":** The Project CRD uses `PhaseComplete = "Complete"` (project_types.go:392) as the terminal success value, while all child-level CRDs use `"Succeeded"`. The dashboard must handle BOTH. StatusBadge needs `"Complete"` as a valid entry pointing to success styling.

### Anti-Patterns to Avoid

- **Setting ValidationState without a condition:** The park for file-touch must set BOTH `ValidationState=FileTouchMismatch` AND a `Conditions` entry. The existing `ReasonNoProjectLabel` paths in plan_controller.go:1017 and task_controller.go:318 set condition-only (no ValidationState); the file-touch park sets ValidationState + condition per plan_types.go schema.
- **Creating a new SSE route for running-waves:** D-15 says the aggregate rides the existing SSE channel. Do NOT add a new `/api/v1/projects/{name}/waves` route. The running-waves event is an additional event type delivered over the existing `/events` channel.
- **Accepting wave list as CRD input (CLAUDE.md anti-pattern):** The running-waves aggregate is a READ surface derived at request time, not stored in CRD status. Make sure no `status.runningWaves` or similar field is added to any CRD.
- **Calling computeFileTouchMismatches from inside AwaitingApproval hold:** The gate runs in `reconcileWaveMaterialization`, which is only reached when `dispatched=false`. The AwaitingApproval hold returns `dispatched=true` (plan_controller.go:247-248), so the file-touch gate never fires while the Plan is parked — correct behavior.
- **Blocking the reconcile on Pod wait (CUTS-04):** The inspector pod creation is a CLI operation, not a reconciler operation. The CLI may use a polling loop with a context deadline; the reconciler is not involved.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Pod log streaming | Custom HTTP connection to API server | `kubernetes.NewForConfig` + `CoreV1().Pods().GetLogs()` with Follow:true | tail.go already demonstrates the correct pattern |
| Pod create/delete lifecycle | Custom state machine | Standard `Create` + `defer Delete` + watch for terminal phase | Standard K8s pattern; tested in kind harness |
| File overlap detection | New algorithm | `computeFileTouchMismatches` (existing in plan_webhook.go) | Already correct, handles Pitfall G (dir-prefix vs exact-path), handles canonical ordering |
| Project traversal from child | Custom walk | `resolveProjectName` (existing in plan_controller.go) or `resolveProjectForPlan` | Already implements OwnerRef chain walk with caching |
| Status-value coercion | New type-switch | `coerce()` function (PlanningDAGView.tsx) — just add "Complete" to KNOWN | One-line fix; adding a new coerce() would be duplication |
| Wave aggregation | New grouping algorithm | `inspectWaveRun` grouping logic (inspect_wave_run.go:77-101) | Already groups tasks by wave-index label |

---

## CUTS-02 Verification (Already Fixed)

**Confirmed fixed by reading source:** `cmd/tide-push/main.go` at lines 488-496 implements `worktreeClean()` — if clean AND no artifacts to stage, it skips the commit and still pushes. The comment at line 486 explicitly says: "In BOTH cases attempting pkggit.Commit fails with 'cannot create empty commit: clean working tree' and the push never fires — the medium-DoD boundary-push defect."

**Test coverage at main_test.go:1001-1045:** `TestRunPushBoundaryCleanTreePushesIntegratedBranch` covers:
- Level-boundary push with no artifacts and no integrate flags
- Working tree is clean after a prior wave-integration
- Must NOT attempt empty commit
- Must STILL push the run branch to the remote

**CUTS-02 deliverable:** Verify that the success criterion's "tide push" phrasing maps to the `cmd/tide-push` binary (it does — `tide push` dispatches a `tide-push` Job which runs this binary). The run-1 symptom was `cannot create empty commit` — the test asserts the branch lands on the remote even from a clean tree. Coverage is complete. Deliverable = confirm the test passes and close CUTS-02 with test evidence; no new code needed.

**Gap check for `tide push` CLI surface:** `cmd/tide/` has no `push` subcommand — `tide push` in the docs refers to running the `tide-push` binary via a Kubernetes Job (the boundary push trigger in plan_controller.go). The "tide push" in the success criterion refers to the end-to-end behavior, not a CLI command. No gap.

---

## CUTS-03 Verification (Already Fixed)

**Confirmed fixed by reading source:** `phase_controller.go` at lines 197-206 adds an AwaitingApproval early-return at the TOP of `reconcilePlannerDispatch`:
```go
// Step 1a: AwaitingApproval early-return (D-01 parity with milestone_controller.go).
// Stops the finding-2 oscillation where a Phase parked at AwaitingApproval would
// fall through to the idempotency guard and re-enter the planner dispatch body on
// every reconcile
if ph.Status.Phase == "AwaitingApproval" {
    if gates.CheckApprove(ph, "phase") {
        // ... consume annotation, return to Running, Requeue
    }
    return ctrl.Result{}, nil  // no requeue — stay parked
}
```

The comment explicitly names "finding-2 oscillation." The fix prevents the AwaitingApproval→Running transition on every reconcile loop by returning early with no requeue when no approve annotation is present.

**CUTS-03 deliverable:** Verify that the existing Phase 12 envtest specs (test/integration/envtest/) cover this path. If no spec asserts "AwaitingApproval Phase without approve annotation does NOT transition to Running on requeue," add one. The regression test should: create a Phase at AwaitingApproval, trigger 3 reconcile cycles without approve annotation, assert Status.Phase remains AwaitingApproval throughout.

---

## Common Pitfalls

### Pitfall 1: Reporter Flow Label Gap
**What goes wrong:** Reporter creates Milestone/Phase CRs. `MaterializeChildCRDs` in reporter/materialize.go calls `obj.SetName`, `obj.SetNamespace`, `stampParentRef`, `owner.EnsureOwnerRef`, then `c.Create` — no label stamping anywhere. The created CR has no labels.
**Why it happens:** `MaterializeChildCRDs` was designed for structural correctness (ownerRef, parentRef); labels were an afterthought.
**How to avoid:** Call `StampProjectLabel(obj, projectName)` BEFORE `c.Create`. The projectName is available in the reporter context (the reporter Job runs against a specific project).
**Warning signs:** `tide approve <project>` returns "no level awaiting approval" despite `kubectl get milestones` showing AwaitingApproval.

### Pitfall 2: backfill reconcile loop risk
**What goes wrong:** If the backfill patch triggers a reconcile that checks the label, which triggers another patch, infinite loop.
**Why it happens:** `r.Patch` on a CR triggers the controller's watch.
**How to avoid:** Guard the backfill with a nil-check: only patch if the label is MISSING. After the patch the label is present; the next reconcile will skip. Standard idempotent pattern.

### Pitfall 3: computeFileTouchMismatches called before all Tasks visible
**What goes wrong:** Reconciler calls the check when only some Tasks have been materialized. False negative — mismatches not detected.
**Why it happens:** Reporter creates Tasks asynchronously. The reconcile cycle may fire between creates.
**How to avoid:** The gate only fires when `len(taskList.Items) > 0` AND the task count matches the plan's declared scope. In practice: if the gate fires and mismatches are 0 but more Tasks are expected, wait — the next Task creation will trigger another reconcile. The park is idempotent; false negatives surface on the next cycle.

### Pitfall 4: Inspector pod PVC mount path
**What goes wrong:** The stub hardcodes `claimName: tide-projects` and `mountPath: /workspace`. The real per-Project subPath is `<projectUID>` not `<projectName>`. Accessing `/workspace/artifacts/PLAN.md` without the correct subPath mounts the root of the PVC, and the path won't match.
**Why it happens:** The stub was illustrative, not correct.
**How to avoid:** Use `volumeMounts[0].subPath = project UID` when mounting. The project UID is resolved via a Get on the Project CR. The artifact path inside the pod should then be `/workspace/<relative-path>` where the relative path is exactly what `parseArtifactRef` returns.
**Warning signs:** Pod runs but `cat` exits non-zero with file not found.

### Pitfall 5: "Complete" NOT in KNOWN array — the source of CUTS-05
**What goes wrong:** `coerce("Complete")` returns `"Pending"` because `KNOWN` is exhaustive. Any new status value added to the Go API but not to `KNOWN` silently maps to Pending.
**Why it happens:** TypeScript union types + the runtime KNOWN guard together require manual synchronization with the Go API vocabulary. They are currently out of sync for "Complete".
**How to avoid:** Add "Complete" to both `StatusValue` union AND `KNOWN` AND `STATUS_TABLE` AND `ProjectPicker.coerceStatus`. The UI-SPEC is the contract — check it when any new Phase value is added to the Go API.

### Pitfall 6: SSE running-waves not a new route (D-15)
**What goes wrong:** Planner creates a new `/api/v1/projects/{name}/waves` route instead of piggybacking on the existing events channel.
**Why it happens:** New feature → assume new endpoint.
**How to avoid:** D-15 explicitly says "delivers over the existing SSE channel." The informer_bridge already fires on Task changes; the hub already delivers events to the project's SSE subscribers. A new event type `running_waves` in the existing event JSON is correct. No new route registration in router.go.

---

## Code Examples

### CUTS-01: Label stamping in MaterializeChildCRDs

```go
// Source: internal/reporter/materialize.go (derived from boundary.go comment + plan_controller.go:1292)
// After EnsureOwnerRef and before c.Create, add:
owner.StampProjectLabel(obj, parent.GetLabels()["tideproject.k8s/project"])
// The parent (Project, Milestone, Phase, Plan) carries the label; if not yet stamped
// on the parent (bootstrap case), fall back to resolving up the OwnerRef chain.
```

### CUTS-05: StatusValue "Complete" addition

```typescript
// Source: dashboard/web/src/components/StatusBadge.tsx
// Change:
export type StatusValue =
  | "Pending" | "Dispatching" | "Running" | "AwaitingApproval"
  | "Paused" | "Succeeded" | "Failed" | "PushLeaseFailed"
  | "PushLeakBlocked" | "Rejected";

// To:
export type StatusValue =
  | "Pending" | "Dispatching" | "Running" | "AwaitingApproval"
  | "Paused" | "Succeeded" | "Complete" | "Failed" | "PushLeaseFailed"
  | "PushLeakBlocked" | "Rejected";

// And add to STATUS_TABLE:
Complete: {
  icon: CircleCheck,
  iconName: "CircleCheck",
  label: "Complete",
  colorVar: "var(--color-status-success)",
  srDescription: "Complete — all milestones succeeded",
},

// And in PlanningDAGView.tsx KNOWN array, add "Complete".
// And in ProjectPicker.tsx coerceStatus KNOWN array, add "Complete".
```

### CUTS-07: File-touch gate in reconcileWaveMaterialization

```go
// Source: internal/webhook/v1alpha1/plan_webhook.go computeFileTouchMismatches + ResolveFileTouchMode
// (to be exported: ComputeFileTouchMismatches)
// Insertion point: after listing taskList, before dag.ComputeWaves call

if len(taskList.Items) > 0 {
    project := r.resolveProjectForPlan(ctx, plan)
    mode := webhookv1alpha1.ResolveFileTouchMode(plan, project, r.DefaultFileTouchMode)
    mismatches := webhookv1alpha1.ComputeFileTouchMismatches(taskList.Items)
    if len(mismatches) > 0 && mode == "strict" {
        return r.patchPlanFileTouchMismatch(ctx, plan, mismatches)
    }
}
```

### CUTS-04: Inspector pod completeness wait

```go
// Source: derived from D-11 + tail.go pattern
// Use a shell command in the inspector pod that waits for the file:
command: []string{"sh", "-c",
    fmt.Sprintf("until [ -f /workspace/%s ]; do sleep 1; done; cat /workspace/%s", path, path),
}
// This is race-free: the sh loop retries until the file exists, then cats it atomically.
// The log stream (Follow:true) then delivers the file content to stdout.
```

---

## Runtime State Inventory

> This phase contains no rename/refactor/migration. The label-stamping fix (CUTS-01) is a new-create fix + reconciler backfill; it does not rename any existing field. Omit this section.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | All Go changes | ✓ | (project-standard) | — |
| controller-runtime v0.24.x | CUTS-01, CUTS-03, CUTS-07 | ✓ | in go.mod | — |
| k8s.io/client-go | CUTS-04 inspector pod | ✓ | controller-runtime-pinned | — |
| vitest + @testing-library/react | CUTS-05, CUTS-06 Vitest tests | ✓ | dashboard/web/package.json | — |
| envtest | CUTS-01, CUTS-03, CUTS-07 envtest specs | ✓ | Makefile setup-envtest | — |
| kind cluster | CUTS-02, CUTS-03 Layer B verification | ✓ (tide cluster from run-1) | per STATE.md constraint | — |

**Missing dependencies with no fallback:** none.

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework (Go) | Ginkgo v2.28 + Gomega (envtest) + plain go test (kind Layer B) |
| Framework (TS) | Vitest + @testing-library/react |
| Config file | dashboard/web/vitest.config.ts |
| Quick run command (Go) | `go test ./internal/... ./cmd/...` |
| Full suite command | `make test-int` |
| Dashboard test command | `cd dashboard/web && npm run test` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CUTS-01 | Reporter-created Milestone/Phase carries `tideproject.k8s/project` label; `tide approve` finds it on first call | envtest (reporter) + unit (approve) | `go test ./internal/reporter/... ./cmd/tide/...` | ❌ Wave 0 — new test |
| CUTS-01 | Backfill: reconciler patches missing label on observed unlabeled CR | envtest | `go test ./internal/controller/...` | ❌ Wave 0 — new test |
| CUTS-02 | Boundary push clean tree exits 0, pushes integrated branch | go test (cmd/tide-push) | `go test ./cmd/tide-push/...` | ✅ main_test.go:1001-1045 |
| CUTS-03 | Phase at AwaitingApproval does NOT transition to Running without approve annotation across multiple reconcile cycles | envtest | `go test ./internal/controller/...` | ❌ Wave 0 — new test (if not already in Phase 12 specs) |
| CUTS-04 | `tide artifact-get` creates inspector pod, streams content to stdout | unit + kind (if inspector pod needs live cluster) | `go test ./cmd/tide/...` | ❌ Wave 0 — new test |
| CUTS-05 | Dashboard renders "Complete" (not "Pending") for Project with status `Complete` | Vitest | `cd dashboard/web && npm run test` | ❌ Wave 0 — new test |
| CUTS-06 | Running-waves SSE event aggregates Tasks by wave-index label | unit (handler) + Vitest (component) | `go test ./cmd/dashboard/... && cd dashboard/web && npm run test` | ❌ Wave 0 — new test |
| CUTS-07 | Reporter-flow Plan with sibling Tasks sharing a file + strict mode parks with FileTouchMismatch | envtest | `go test ./internal/controller/...` | ❌ Wave 0 — new test |
| CUTS-07 | Webhook early-returns when zero Tasks (Pitfall B preserved); reconciler gate is authoritative | unit (webhook) | `go test ./internal/webhook/...` | Partial (Pitfall B already tested; gate is new) |

**Regression test rule (milestone-wide):** Each test must assert the run-1 symptom string or behavior, not just green-path correctness.

### Wave 0 Gaps
- [ ] `internal/reporter/materialize_test.go` — assert label present after MaterializeChildCRDs; covers CUTS-01 create-site
- [ ] `internal/controller/milestone_controller_test.go` or `phase_controller_test.go` — assert backfill patches label on observed unlabeled CR; covers CUTS-01 backfill
- [ ] `cmd/tide/approve_test.go` — add integration test: reporter-created CR without label is found by approveLevel; covers CUTS-01 end-to-end
- [ ] `internal/controller/phase_controller_test.go` — AwaitingApproval stays parked across 3 reconcile cycles without approve annotation; covers CUTS-03
- [ ] `cmd/tide/artifact_get_test.go` — assert real inspector pod create/log stream path (fake or kind); covers CUTS-04
- [ ] `dashboard/web/src/components/__tests__/dag-views.test.tsx` (or nodes.test.tsx) — assert project node with phase="Complete" renders status-badge-Complete not status-badge-Pending; covers CUTS-05
- [ ] `cmd/dashboard/api/waves_test.go` (new) — assert running-waves handler returns correct wave groups from label-indexed Tasks; covers CUTS-06
- [ ] `dashboard/web/src/components/__tests__/RunningWavesView.test.tsx` (new) — assert wave cards render from SSE event; covers CUTS-06 UI
- [ ] `internal/controller/plan_controller_test.go` — reporter-flow Plan with sibling Tasks sharing strict file parks with ValidationState=FileTouchMismatch; covers CUTS-07

### Sampling Rate
- **Per task commit:** `go test ./... -short` and `cd dashboard/web && npm run test`
- **Per wave merge:** `make test` (envtest suite)
- **Phase gate:** `make test-int` full suite green before `/gsd:verify-work`

---

## Security Domain

> `security_enforcement` not explicitly false in config.json — section required.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No — no new auth surfaces | — |
| V3 Session Management | No | — |
| V4 Access Control | Yes (CUTS-04 inspector pod) | CLI uses operator kubeconfig; pod runs in operator's RBAC context |
| V5 Input Validation | Yes (CUTS-04 artifact ref; CUTS-07 path comparison) | `parseArtifactRef` validates ref; `computeFileTouchMismatches` compares exact strings |
| V6 Cryptography | No | — |

### Known Threat Patterns for This Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal in artifact ref (CUTS-04) | Tampering | `parseArtifactRef` rejects empty path components; inspector pod command is `cat /workspace/<path>` — no shell expansion needed beyond the fixed mount path |
| Label injection via reporter LLM output (CUTS-01) | Tampering | `StampProjectLabel` overwrites from authoritative parent; LLM-authored labels on child CRDs are ignored (same pattern as `stampParentRef`) |
| Inspector pod persisting after error (CUTS-04) | Availability | `defer cs.CoreV1().Pods(ns).Delete(...)` in all exit paths |
| False wave aggregate from stale informer cache (CUTS-06) | Information Disclosure | Cache-backed reads are bounded by informer sync; no stale data worse than brief SSE delay |
| FileTouchMismatch park bypassable via webhook nil-project (CUTS-07/D-08) | Tampering | D-08 upgrades webhook to real mode resolution; reconciler gate is authoritative regardless |

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `computeFileTouchMismatches` webhook-only | Reconciler gate (authoritative) + webhook (early layer) | Phase 15 | Reporter flow now enforced |
| `artifactGetDryRun` dry-run stub | Real inspector pod + log stream | Phase 15 | `tide artifact-get` produces actual content |
| `"Complete"` unmapped in StatusValue | `"Complete"` added to union + STATUS_TABLE | Phase 15 | Project terminal state renders correctly |
| Reporter creates CRs with zero labels | Universal label stamping + backfill | Phase 15 | `tide approve` discovers gated levels on first call |

**Deprecated/outdated:**
- `artifactGetDryRun` (artifact_get_run.go:65-86): replaced by real inspector pod implementation. The function itself can be removed or renamed to a test helper.
- The "tide-projects" hardcode in the stub is illustrative, not wrong — the real PVC is "tide-projects".

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The run-1 kind cluster `tide` with run-1 CRs is still live for Layer B regression tests | Validation Architecture | Kind Layer B tests would need a fresh repro setup; regression shape still testable in envtest |
| A2 | The "Complete" status value for Project is not displayed anywhere else in the dashboard (no other coerce() call site that could also be wrong) | CUTS-05 | If other components also coerce Project phase, they'd also be wrong; research found ProjectPicker.tsx has a second coerceStatus that also needs updating |

**Note on A2:** Research confirmed ProjectPicker.tsx has `coerceStatus` at line 43 — this is a second coerce call site that also needs "Complete" added. This is not an assumption; it's a verified finding. The planner must include ProjectPicker.tsx in the CUTS-05 task scope.

---

## Open Questions

1. **artifact-get wait timeout default**
   - What we know: D-11 says "wait rather than erroring"; D-12 says "plain error after wait window exhausted"
   - What's unclear: what the default timeout value should be (30s? 5m? configurable?)
   - Recommendation: Claude's Discretion per CONTEXT.md. 5 minutes with a `--timeout` flag defaulting to 5m is reasonable for a human-supervised operator tool. The planner should pick this.

2. **Inspector pod: watch vs poll for readiness**
   - What we know: D-11 requires race-free completeness; the shell-loop pattern (`until [ -f path ]; do sleep 1; done; cat`) is race-free
   - What's unclear: whether a watch (using `CoreV1().Pods(ns).Watch()`) is more efficient than poll
   - Recommendation: Use the shell-loop approach in the inspector pod command — it is simpler, avoids a second goroutine for the watch, and is proven in the existing `tail.go` architecture.

3. **SSE running-waves: new event type vs new endpoint**
   - What we know: D-15 explicitly says "existing SSE channel"
   - What's unclear: whether the informer_bridge already delivers enough context for the new event type, or whether the bridge needs augmentation
   - Recommendation: Inspect `informer_bridge.go` closely during planning. If the bridge emits per-Task events, the EventsHandler can aggregate on receive. If not, a TTL-based re-query on Task events is the right pattern.

---

## Sources

### Primary (HIGH confidence)
- Codebase direct reads: `internal/reporter/materialize.go`, `cmd/tide/approve.go`, `internal/webhook/v1alpha1/plan_webhook.go`, `internal/controller/plan_controller.go`, `internal/controller/phase_controller.go`, `api/v1alpha1/plan_types.go`, `api/v1alpha1/project_types.go`, `dashboard/web/src/components/StatusBadge.tsx`, `dashboard/web/src/components/PlanningDAGView.tsx`, `dashboard/web/src/components/ProjectNode.tsx`, `cmd/dashboard/router.go`, `cmd/dashboard/api/projects.go`, `cmd/dashboard/api/events_sse.go`, `cmd/tide/inspect_wave_run.go`, `cmd/tide/tail.go`, `cmd/tide/root_flags.go`, `cmd/tide-push/main.go`, `cmd/tide-push/main_test.go`, `internal/owner/owner.go`, `internal/subagent/common/templates/plan_planner.tmpl`, `internal/gates/boundary.go`, `internal/webhook/v1alpha1/strict_mode.go`

### Secondary (MEDIUM confidence)
- CONTEXT.md D-01 through D-16 (discuss-phase output with scout findings)
- REQUIREMENTS.md CUTS-01 through CUTS-07
- STATE.md accumulated constraints (kind cluster, chart contract)

### Tertiary (LOW confidence)
- None — all claims in this document were verified against codebase source.

---

## Metadata

**Confidence breakdown:**
- Standard Stack: HIGH — all packages confirmed in go.mod/package.json
- Architecture: HIGH — all patterns verified against existing codebase code
- Pitfalls: HIGH — sourced from reading actual code paths, not hypothetical
- Already-fixed verification (CUTS-02, CUTS-03): HIGH — confirmed by reading commit artifacts

**Research date:** 2026-06-12
**Valid until:** 2026-07-12 (30 days — stable Go/React stack)
