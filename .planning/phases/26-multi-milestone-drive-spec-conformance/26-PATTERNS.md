# Phase 26: Multi-Milestone Drive + Spec Conformance — Pattern Map

**Mapped:** 2026-06-17
**Files analyzed:** 12 new/modified files
**Analogs found:** 12 / 12

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/subagent/common/templates/project_planner.tmpl` | config/template | transform | self (current single-milestone form) | exact — widen, don't replace |
| `internal/eval/testdata/goldie/project_planner.golden` | test fixture | transform | `internal/eval/testdata/goldie/` sibling goldens | exact |
| `internal/eval/testdata/ratchets/project_planner.txt` | config | transform | self | exact |
| `internal/controller/depgraph.go` | utility | transform (graph) | self (§6a/6b/6c blocks being kept) | exact — deletion inside existing file |
| `internal/controller/project_controller.go` | controller | CRUD + event-driven | self (idempotency guard ~961, prune loop ~1644) | exact — targeted edits |
| `internal/controller/wave_controller.go` | controller | event-driven | self (aggregator ~163–184) | exact — targeted edit |
| `internal/controller/task_controller.go` | controller | event-driven | self + `predicate.AnnotationChangedPredicate` usage (~1740) | exact — add `WithPredicates` |
| `test/integration/envtest/spec_conformance_test.go` | test | CRUD + event-driven | `test/integration/envtest/global_wave_derivation_test.go` | exact — new file, same package, same helpers |
| `cmd/dashboard/api/execution_dag.go` | service (HTTP handler) | request-response | `cmd/dashboard/api/plans.go` | exact — same handler shape, different list filter |
| `dashboard/web/src/components/GlobalExecutionDAGView.tsx` | component | event-driven / SSE | `dashboard/web/src/components/ExecutionDAGView.tsx` | exact — project-scope variant |
| `dashboard/web/src/components/EmptyState.tsx` | component | request-response | self | exact — add two variants |
| `dashboard/web/src/App.tsx` | component | event-driven | self (~line 347–369) | exact — add third right-pane state |

---

## Pattern Assignments

### `internal/subagent/common/templates/project_planner.tmpl`
**Role:** config/template  **Data flow:** transform  
**Analog:** self (current file)

**Current single-milestone constraint** (lines 15–16):
```
  - Do NOT decompose into Phases here — that is the milestone planner's job,
    dispatched downstream once this Milestone exists. The project planner's
    sole structural output is exactly one Milestone child-CRD.
```

**Current HOW-TO-EMIT block** (lines 18–42) — exact shape that must be extended:
```
HOW TO EMIT THE CHILD CRD (REQUIRED — this is how the orchestrator picks it up):
Use your Write tool to create exactly ONE file under your children/ directory.

The file MUST be a single JSON object with this exact shape:

  {
    "kind": "Milestone",
    "name": "milestone-01-<slug>",
    "spec": { "projectRef": "<this project's name>" }
  }

- "kind" MUST be the string "Milestone".
- "name" is the metadata.name the orchestrator assigns; keep it unique and
  DNS-safe (lowercase, hyphenated).
...
Write ONLY into the children/ directory shown above; files written elsewhere
are ignored. The orchestrator reads every *.json there into typed child CRDs.
Markdown (MILESTONE.md) is the human-review surface; the JSON file is the
machine contract — both must be produced.
```

**Target multi-milestone pattern** (D-01; Opus 4.x literal-instruction guidance from CLAUDE.md requires explicit scope):
```
HOW TO EMIT MILESTONE CHILD CRDs (REQUIRED):
Use your Write tool to create ONE file per milestone under your children/ directory.

Each file MUST be a single JSON object with this exact shape:

  {
    "kind": "Milestone",
    "name": "milestone-01-<slug>",
    "spec": {
      "projectRef": "<this project's name>",
      "dependsOn": []   // empty for the FIRST milestone; list predecessor names for later milestones
    }
  }

Emit one Milestone child-CRD per milestone in the DAG, each with its `dependsOn`
wired to its predecessors. A project with two milestones produces two files:
  children/milestone-01-foundation.json   (no dependsOn)
  children/milestone-02-surface.json      (dependsOn: ["milestone-01-foundation"])

"kind" MUST be the string "Milestone" for EVERY file.
Write ONLY into the children/ directory — files written elsewhere are ignored.
The orchestrator reads every *.json there into typed child CRDs.
Produce one MILESTONE.md per milestone (or a combined MILESTONES.md) as the
human-review surface; the JSON files are the machine contract.
```

**Ratchet file** `internal/eval/testdata/ratchets/project_planner.txt` currently contains `2193`. After the template edit, re-run `go test ./internal/eval/... -update` and replace this number with the new byte count. The eval test fails on any divergence (grow OR shrink) — intentional update gate.

**Golden file** `internal/eval/testdata/goldie/project_planner.golden` — regenerated by the same `-update` run. Contains the rendered template text verbatim.

---

### `internal/controller/depgraph.go` — §6d removal
**Role:** utility  **Data flow:** graph transform  
**Analog:** self (§6a/6b/6c blocks at lines ~200–256)

**§6c block kept as structural reference** (lines 239–256) — the §6d block immediately below it must be deleted entirely:
```go
// 6c. Phase-level DependsOn fan-out (§6c — KEEP):
for i := range phases {
    ph := &phases[i]
    for _, dep := range ph.Spec.DependsOn {
        fromTasks := resolver.resolveScope(dep)
        var toTasks []string
        for planName, phaseName := range resolver.planToPhase {
            if phaseName == ph.Name {
                toTasks = append(toTasks, resolver.tasksByPlan[planName]...)
            }
        }
        for _, from := range fromTasks {
            for _, to := range toTasks {
                addEdge(from, to)
            }
        }
    }
}
```

**§6d block — REMOVE entirely** (lines 258–283):
```go
// 6d. Milestone-level DependsOn fan-out: all tasks in THIS milestone depend
// on all tasks in the referenced scope.
for i := range ms {
    m := &ms[i]
    for _, dep := range m.Spec.DependsOn {
        fromTasks := resolver.resolveScope(dep)
        var toTasks []string
        for phaseName, msName := range resolver.phaseToMS {
            if msName == m.Name {
                for planName, ph2 := range resolver.planToPhase {
                    if ph2 == phaseName {
                        toTasks = append(toTasks, resolver.tasksByPlan[planName]...)
                    }
                }
            }
        }
        for _, from := range fromTasks {
            for _, to := range toTasks {
                addEdge(from, to)
            }
        }
    }
}
```
After deletion `return edges` (line 283) becomes the last statement of `buildGlobalEdges`. No call sites depend on §6d behavior — verified: `depgraph_test.go` exercises §6a–6c only.

---

### `internal/controller/project_controller.go` — idempotency guard + prune guard
**Role:** controller  **Data flow:** CRUD + event-driven  
**Analog:** self — two targeted edits at lines 961–972 and 1644–1656

**Current idempotency guard** (lines 955–972) — bails on ANY owned Milestone, must be widened to N-milestone-safe:
```go
// Step 1b: Idempotency guard — skip dispatch when the Project already owns
// >=1 Milestone. Once the label fix makes the envelope round-trip succeed,
// Projects that already have a pre-applied Milestone (push-lease, chaos-resume,
// wave-test fixtures) would otherwise author a spurious extra Milestone.
// This mirrors BoundaryDetected's ownership check without the all-Succeeded
// requirement — we just need to know children exist.
{
    var existingMilestones tidev1alpha2.MilestoneList
    if lErr := r.List(ctx, &existingMilestones, client.InNamespace(project.Namespace)); lErr != nil {
        return ctrl.Result{}, fmt.Errorf("idempotency: list milestones: %w", lErr)
    }
    for i := range existingMilestones.Items {
        if metav1.IsControlledBy(&existingMilestones.Items[i], project) {
            // Project already has at least one owned Milestone — planner already ran.
            return ctrl.Result{}, nil
        }
    }
}
```

**Target idempotency pattern** (D-01; gate on Job existence, not milestone count):
The Job name `tide-project-<uid>-1` is the stable idempotency signal (Job is created once; presence means planner already dispatched). The safest replacement: check whether `tide-project-<uid>-1` Job already exists instead of counting owned Milestones. Pattern from the existing `jobName` assignment at line 974:
```go
jobName := fmt.Sprintf("tide-project-%s-1", project.UID)
// Idempotency: if the planner Job already exists, planner already ran.
var existingJob batchv1.Job
if err := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: jobName}, &existingJob); err == nil {
    return ctrl.Result{}, nil // planner already dispatched
} else if !apierrors.IsNotFound(err) {
    return ctrl.Result{}, fmt.Errorf("idempotency: get planner job: %w", err)
}
```

**Current prune guard** (lines 1631–1657) — OQ-3 fix target:
```go
for i := range allWaves.Items {
    w := &allWaves.Items[i]
    if w.Spec.ProjectRef == project.Name && w.Spec.WaveIndex >= len(globalWaves) {
        // NOTE (Phase 25 OQ-3 deferred): an in-flight guard here would protect
        // Waves with running Jobs from premature deletion. Deferred: the wave
        // aggregator sets Phase="Running" even for 0-member waves (when all tasks
        // are deleted), so a Phase-based guard would block pruning of legitimately
        // stale empty waves (CR-01 regression). A correct guard requires the
        // WaveController to distinguish "no tasks assigned" from "tasks in-flight";
        // that wave-controller refactor is out of scope for Phase 25.
        if delErr := r.Delete(ctx, w); delErr != nil && !apierrors.IsNotFound(delErr) {
            return fmt.Errorf("prune wave %s: %w", w.Name, delErr)
        }
        logger.Info("pruned stale global wave", "wave", w.Name, ...)
    }
}
```

**Target prune pattern** (D-08; uses `wave.Status.TaskRefs` populated by WaveReconciler):
```go
for i := range allWaves.Items {
    w := &allWaves.Items[i]
    if w.Spec.ProjectRef == project.Name && w.Spec.WaveIndex >= len(globalWaves) {
        // OQ-3 fix: only prune if zero members OR already Succeeded.
        // Zero-member: TaskRefs is empty (aggregator found no matching tasks).
        // Succeeded: all tasks completed.
        if len(w.Status.TaskRefs) == 0 || w.Status.Phase == "Succeeded" {
            if delErr := r.Delete(ctx, w); delErr != nil && !apierrors.IsNotFound(delErr) {
                return fmt.Errorf("prune wave %s: %w", w.Name, delErr)
            }
            logger.Info("pruned stale global wave", "wave", w.Name, "waveIndex", w.Spec.WaveIndex, "currentWaveCount", len(globalWaves))
        } else {
            logger.V(1).Info("skipping prune of in-flight wave", "wave", w.Name,
                "phase", w.Status.Phase, "memberCount", len(w.Status.TaskRefs))
        }
    }
}
```

---

### `internal/controller/wave_controller.go` — OQ-3 aggregator fix
**Role:** controller  **Data flow:** event-driven  
**Analog:** self (aggregator switch at lines 174–185)

**Current aggregator phase logic** (lines 174–184) — sets `"Running"` for zero-member and real-in-flight alike:
```go
var phase, message string
switch {
case allSucceeded && len(members) > 0:
    phase = "Succeeded"
    message = fmt.Sprintf("All %d member task(s) succeeded", len(members))
case failedTask != "":
    phase = "Failed"
    message = fmt.Sprintf("Member task %q failed", failedTask)
default:
    phase = "Running"
    message = fmt.Sprintf("%d member task(s); awaiting completion", len(members))
}
```

**Target pattern** (D-08; add `"ZeroMembers"` phase for the empty case so the prune guard can distinguish it from a wave with real in-flight tasks):
```go
var phase, message string
switch {
case len(members) == 0:
    phase = "ZeroMembers"
    message = "No tasks assigned to this wave"
case allSucceeded:
    phase = "Succeeded"
    message = fmt.Sprintf("All %d member task(s) succeeded", len(members))
case failedTask != "":
    phase = "Failed"
    message = fmt.Sprintf("Member task %q failed", failedTask)
default:
    phase = "Running"
    message = fmt.Sprintf("%d member task(s); awaiting completion", len(members))
}
```
The prune guard then checks `len(w.Status.TaskRefs) == 0 || w.Status.Phase == "Succeeded"` (TaskRefs is empty iff Phase is "ZeroMembers", so either condition works). The CR-01 PruneShrink test (`global_wave_derivation_test.go:413`) must remain green — the fix makes zero-member waves prunable, which is what PruneShrink requires.

---

### `internal/controller/task_controller.go` — WR-02 watch predicate
**Role:** controller  **Data flow:** event-driven  
**Analog:** self — `predicate.AnnotationChangedPredicate{}` usage at line 1740, and the `Watches(...).WithPredicates(...)` call at line 1748

**Current `Watches` call without predicate** (lines 1744–1747):
```go
Watches(
    &tideprojectv1alpha2.Task{},
    handler.EnqueueRequestsFromMapFunc(r.globalDependentsMapper),
).
```

**Existing `WithPredicates` call pattern** (lines 1748–1754 — copy this pattern for the WR-02 fix):
```go
Watches(
    &tideprojectv1alpha2.Task{},
    handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
        return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
    }),
    builder.WithPredicates(annotationOnly),
).
```

**Target WR-02 predicate** (D-09; define before `SetupWithManager`, use `slices.Equal` from stdlib `slices` Go 1.21+):
```go
statusPhaseOrDepsChanged := predicate.Funcs{
    UpdateFunc: func(e event.UpdateEvent) bool {
        oldT, ok1 := e.ObjectOld.(*tideprojectv1alpha2.Task)
        newT, ok2 := e.ObjectNew.(*tideprojectv1alpha2.Task)
        if !ok1 || !ok2 {
            return true // conservative: let untyped events through
        }
        return oldT.Status.Phase != newT.Status.Phase ||
            !slices.Equal(oldT.Spec.DependsOn, newT.Spec.DependsOn)
    },
    CreateFunc:  func(event.CreateEvent) bool { return true },
    DeleteFunc:  func(event.DeleteEvent) bool { return true },
    GenericFunc: func(event.GenericEvent) bool { return false },
}
```

Then wire it:
```go
Watches(
    &tideprojectv1alpha2.Task{},
    handler.EnqueueRequestsFromMapFunc(r.globalDependentsMapper),
    builder.WithPredicates(statusPhaseOrDepsChanged), // WR-02
).
```

**Imports already present:** `event` → `sigs.k8s.io/controller-runtime/pkg/event`; `predicate` → `sigs.k8s.io/controller-runtime/pkg/predicate`. Add `"slices"` to the stdlib import group if not already present.

---

### `test/integration/envtest/spec_conformance_test.go` (new file)
**Role:** test  **Data flow:** CRUD + event-driven (full-stack envtest)  
**Analog:** `test/integration/envtest/global_wave_derivation_test.go` — copy the exact package declaration, import block, helper pattern, BeforeEach/AfterEach structure, and `assertWaveExists` polling pattern

**Package + import block** (lines 1–32 of analog):
```go
package envtest_integration

import (
    "context"
    "fmt"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)
```

**Distinct project name** — use `"spec-conformance-project"` (NOT `"global-wave-test-project"`) to avoid state collision with the existing suite.

**Helper — `createSimpleMilestoneWithDeps`** (extend the existing `createSimpleMilestone` at lines 55–71):
```go
func createSimpleMilestoneWithDeps(ctx context.Context, name, projectRef string, deps []string) {
    ms := &tideprojectv1alpha2.Milestone{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name,
            Namespace: globalWaveNamespace,
        },
        Spec: tideprojectv1alpha2.MilestoneSpec{
            ProjectRef: projectRef,
            DependsOn:  deps,
        },
    }
    Expect(k8sClient.Create(ctx, ms)).To(Succeed())
    Eventually(func() error {
        return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Milestone{})
    }, "5s", "100ms").Should(Succeed())
}
```

**`makeGlobalWaveTask` with project label** (lines 83–110 of analog — reuse unchanged, but stamp `"spec-conformance-project"` on task labels):
```go
// makeSpecConformanceTask is makeGlobalWaveTask with the spec-conformance project label.
func makeSpecConformanceTask(ctx context.Context, name, planRef string, dependsOn []string) *tideprojectv1alpha2.Task {
    labels := map[string]string{
        "tideproject.k8s/project": "spec-conformance-project",
    }
    task := &tideprojectv1alpha2.Task{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name,
            Namespace: globalWaveNamespace,
            Labels:    labels,
        },
        Spec: tideprojectv1alpha2.TaskSpec{
            PlanRef:             planRef,
            PromptPath:          "envelopes/test/children/" + name + ".json",
            DependsOn:           dependsOn,
            FilesTouched:        []string{name + ".go"},
            DeclaredOutputPaths: []string{name + ".go"},
        },
    }
    Expect(k8sClient.Create(ctx, task)).To(Succeed())
    Eventually(func() error {
        return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Task{})
    }, "5s", "100ms").Should(Succeed())
    time.Sleep(50 * time.Millisecond) // allow indexer to propagate
    return task
}
```

**`assertWaveExists` pattern** (lines 112–122 of analog — reuse unchanged):
```go
func assertWaveExists(ctx context.Context, projectName string, waveIdx int) {
    Eventually(func() error {
        wave := &tideprojectv1alpha2.Wave{}
        return k8sClient.Get(ctx, client.ObjectKey{
            Name:      fmt.Sprintf("tide-wave-%s-%d", projectName, waveIdx),
            Namespace: globalWaveNamespace,
        }, wave)
    }, "30s", "500ms").Should(Succeed(), "Wave CR tide-wave-%s-%d should exist", projectName, waveIdx)
}
```

**Wave membership assertion helper** (new, needed for SPEC-01 beyond existence):
```go
func assertWaveMembership(ctx context.Context, projectName string, waveIdx int, expectedTasks []string) {
    Eventually(func() error {
        wave := &tideprojectv1alpha2.Wave{}
        if err := k8sClient.Get(ctx, client.ObjectKey{
            Name:      fmt.Sprintf("tide-wave-%s-%d", projectName, waveIdx),
            Namespace: globalWaveNamespace,
        }, wave); err != nil {
            return err
        }
        for _, expected := range expectedTasks {
            found := false
            for _, ref := range wave.Status.TaskRefs {
                if ref == expected {
                    found = true
                    break
                }
            }
            if !found {
                return fmt.Errorf("task %q not in wave %d TaskRefs %v", expected, waveIdx, wave.Status.TaskRefs)
            }
        }
        return nil
    }, "30s", "500ms").Should(Succeed())
}
```

**BeforeEach pattern** (lines 131–147 of analog — copy exactly, change project name):
```go
BeforeEach(func() {
    makeBoundPVC(ctx, "tide-projects", globalWaveNamespace)
    project := &tideprojectv1alpha2.Project{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "spec-conformance-project",
            Namespace: globalWaveNamespace,
        },
        Spec: tideprojectv1alpha2.ProjectSpec{
            SchemaRevision: "v1alpha2",
            TargetRepo:     "https://github.com/example/spec-conformance.git",
        },
    }
    if err := k8sClient.Create(ctx, project); err != nil {
        Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
    }
})
```

**AfterEach cleanup order** (lines 149–186 of analog — copy exactly): Waves → Tasks → Plans → Phases → Milestones → Projects → PVCs. This order prevents foreign-key-like constraint violations during cleanup.

**SPEC-01 fixture topology** (from RESEARCH.md §D-06):
```
Milestone A (ms-spec-a): Phase A.1 [Plan A.1.1: sc-alpha, sc-beta; Plan A.1.2: sc-gamma], Phase A.2 [Plan A.2.1: sc-delta, sc-epsilon]
Milestone B (ms-spec-b, DependsOn: ["ms-spec-a"]): Phase B.1 [Plan B.1.1: sc-zeta; Plan B.1.2: sc-eta, sc-theta]

Task-level edges (§6a only — §6d removed):
  sc-alpha → sc-delta
  sc-beta  → sc-delta
  sc-delta → sc-epsilon
  sc-gamma → sc-eta     ← cross-milestone load-bearing edge
  sc-zeta  → sc-eta
  sc-eta   → sc-theta

Expected waves: [{sc-alpha,sc-beta,sc-gamma,sc-zeta}, {sc-delta,sc-eta}, {sc-epsilon,sc-theta}]
```

**PruneShrink regression test** (lines 413–454) — must stay green after OQ-3 fix. The fix makes zero-member waves prunable, which is exactly what PruneShrink tests. No change to this test.

---

### `cmd/dashboard/api/execution_dag.go` (new file)
**Role:** service (HTTP handler)  **Data flow:** request-response  
**Analog:** `cmd/dashboard/api/plans.go` — copy the handler struct, ServeHTTP signature, `planTaskCard` reuse, `waveByTask` construction, sort, and `writeJSON` call

**File header + package** (lines 1–28 of analog):
```go
/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 ...
*/

// execution_dag.go — GET /api/v1/projects/{name}/execution-dag.
//
// Surfaces the global execution DAG for a Project: all Tasks across all
// Milestones with their waveIndex, status, and dependsOn. Used by the
// GlobalExecutionDAGView dashboard component.
//
// DASH-05 zero-mutation contract: this handler is HTTP GET only.
package api
```

**Handler struct** (copy `PlansHandler` shape from lines 44–48):
```go
type ExecutionDAGHandler struct {
    Client client.Client
    Log    logr.Logger
}
```

**Response types** (reuse `planTaskCard` from plans.go; new wrapper):
```go
// projectExecutionDAGResponse is the JSON shape for GET /api/v1/projects/{name}/execution-dag.
// Reuses planTaskCard (same field names the frontend's ExecutionTaskData expects).
type projectExecutionDAGResponse struct {
    ProjectName string         `json:"projectName"`
    Tasks       []planTaskCard `json:"tasks"`
}
```

**Handler body pattern** (copy the waveByTask construction from plans.go lines 121–158):
```go
func (h *ExecutionDAGHandler) Get(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    name := chi.URLParam(r, "name")
    namespace := r.URL.Query().Get("namespace")
    if namespace == "" {
        namespace = "default"
    }

    // List ALL Tasks for this project via the project label.
    var tasks tidev1alpha2.TaskList
    if err := h.Client.List(ctx, &tasks,
        client.InNamespace(namespace),
        client.MatchingLabels{owner.LabelProject: name},
    ); err != nil {
        h.Log.Error(err, "list tasks failed", "project", name)
        writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list tasks: %s", err.Error()))
        return
    }

    // Waves — build waveByTask map from Wave CRs (same pattern as plans.go:121-127).
    var waves tidev1alpha2.WaveList
    if err := h.Client.List(ctx, &waves,
        client.InNamespace(namespace),
        client.MatchingLabels{owner.LabelProject: name},
    ); err != nil {
        h.Log.Error(err, "list waves failed", "project", name)
    }
    waveByTask := make(map[string]int, len(tasks.Items))
    for i := range waves.Items {
        wv := &waves.Items[i]
        for _, tref := range wv.Status.TaskRefs {
            waveByTask[tref] = wv.Spec.WaveIndex
        }
    }

    // Build cards (copy plans.go:129-159).
    cards := make([]planTaskCard, 0, len(tasks.Items))
    for i := range tasks.Items {
        tk := &tasks.Items[i]
        phase := tk.Status.Phase
        if phase == "" {
            phase = "Pending"
        }
        deps := tk.Spec.DependsOn
        if deps == nil {
            deps = []string{}
        }
        cards = append(cards, planTaskCard{
            Name:      tk.Name,
            Phase:     phase,
            WaveIndex: waveByTask[tk.Name],
            Attempt:   tk.Status.Attempt,
            DependsOn: deps,
        })
    }
    sort.Slice(cards, func(i, j int) bool {
        if cards[i].WaveIndex != cards[j].WaveIndex {
            return cards[i].WaveIndex < cards[j].WaveIndex
        }
        return cards[i].Name < cards[j].Name
    })

    writeJSON(w, http.StatusOK, projectExecutionDAGResponse{
        ProjectName: name,
        Tasks:       cards,
    })
}
```

**Router registration** — add to `router.go` inside the `/api/v1` group (lines 166–181), following the `plansHandler` pattern:
```go
execDagHandler := &dashboardapi.ExecutionDAGHandler{
    Client: deps.Client,
    Log:    deps.Log,
}
// inside r.Route("/api/v1", ...):
r.Get("/projects/{name}/execution-dag", execDagHandler.Get)
```
The route table comment block at lines 88–101 must be updated to include this new route. DASH-05 TestZeroMutationRoutes walks the route tree — GET registration satisfies it.

---

### `dashboard/web/src/components/GlobalExecutionDAGView.tsx` (new file)
**Role:** component  **Data flow:** event-driven (SSE-driven data)  
**Analog:** `dashboard/web/src/components/ExecutionDAGView.tsx` — copy the entire file structure, rename `planName`/`ExecutionPlanData` references to `projectName`/`ProjectExecutionDAGData`

**Props interface** (replaces `ExecutionDAGViewProps` and `ExecutionPlanData` — reuses `ExecutionTaskData` from the analog):
```typescript
// ExecutionTaskData is imported from ./ExecutionDAGView — reuse the type directly.
import type { ExecutionTaskData } from "./ExecutionDAGView";

export type ProjectExecutionDAGData = {
  projectName: string;
  tasks: ExecutionTaskData[];
  activeDispatchWave?: number;
};

export type GlobalExecutionDAGViewProps = {
  projectName: string;
  project: ProjectExecutionDAGData | null;
  onTaskClick: (taskName: string) => void;
};
```

**Constants** (lines 63–76 of analog — copy unchanged):
```typescript
const PADDING = 24;
const TASK_WIDTH = 260;
const TASK_HEIGHT = 64;
const EDGE_STROKE = "var(--color-border-strong)";
const EDGE_STYLE = { stroke: EDGE_STROKE, strokeWidth: 1.5 } as const;
const EDGE_MARKER = {
  type: MarkerType.ArrowClosed,
  color: EDGE_STROKE,
  width: 16,
  height: 16,
} as const;
```

**`buildExecutionGraph` function** (lines 78–113 of analog — copy, replace `plan: ExecutionPlanData` with `project: ProjectExecutionDAGData`, replace `plan.tasks` with `project.tasks`):
```typescript
function buildExecutionGraph(project: ProjectExecutionDAGData): {
  nodes: Node[];
  edges: Edge[];
  waveMap: Map<string, number>;
} {
  // same body as analog, using project.tasks
}
```

**`annotateEdges`, `computeBands`, `executionNodeTypes`** (lines 116–213 of analog) — copy unchanged, they are pure functions with no plan/project coupling.

**`GlobalExecutionDAGViewInner` function** (analog: `ExecutionDAGViewInner` lines 215–347):
```typescript
function GlobalExecutionDAGViewInner({
  projectName: _projectName,
  project,
  onTaskClick,
}: GlobalExecutionDAGViewProps) {
  void _projectName; // SSE seam will key off projectName
  // ... copy all state hooks, useEffect blocks unchanged ...
  // Change: plan → project, plan?.tasks → project?.tasks,
  //         plan?.activeDispatchWave → project?.activeDispatchWave
  // data-testid: "global-execution-dag-view" (distinguish from per-plan)
}
```

**View states** (from UI-SPEC §View States):
- `project === null` → centered `Loader2` spinner (copy RunningWavesView pattern)
- `project.tasks.length === 0` → `<EmptyState variant="global-dag-no-tasks" />`
- fetch error → `<EmptyState variant="global-dag-fetch-error" />`
- populated → ReactFlow canvas

**Outer wrapper** (lines 349–355 of analog — copy exactly, rename):
```typescript
export default function GlobalExecutionDAGView(props: GlobalExecutionDAGViewProps) {
  return (
    <ReactFlowProvider>
      <GlobalExecutionDAGViewInner {...props} />
    </ReactFlowProvider>
  );
}
```

---

### `dashboard/web/src/components/EmptyState.tsx` — two new variants
**Role:** component  **Data flow:** request-response  
**Analog:** self (existing `"no-running-waves"` variant at lines 146+ — copy the CenteredCard + h2 + p structure)

**Type union extension** (line 21, after `"no-running-waves"`):
```typescript
export type EmptyStateVariant =
  | "no-projects"
  | "awaiting-first-milestone"
  | "plan-accepted-no-tasks"
  | "no-running-waves"
  | "global-dag-no-tasks"
  | "global-dag-fetch-error";
```

**New switch cases** (add after `"no-running-waves"` case, following the `h2`+`p` CenteredCard pattern):
```typescript
case "global-dag-no-tasks":
  return (
    <CenteredCard>
      <h2
        style={{ marginTop: "48px", fontSize: "18px", fontWeight: 600,
                 color: "var(--color-text-primary)" }}
      >
        No tasks in global DAG
      </h2>
      <p
        style={{ marginTop: "16px", fontSize: "14px",
                 color: "var(--color-text-muted)", maxWidth: "440px" }}
      >
        Wave derivation has not run yet — planning may still be in progress.
      </p>
    </CenteredCard>
  );
case "global-dag-fetch-error":
  return (
    <CenteredCard>
      <h2
        style={{ marginTop: "48px", fontSize: "18px", fontWeight: 600,
                 color: "var(--color-text-primary)" }}
      >
        Could not load global DAG
      </h2>
      <p
        style={{ marginTop: "16px", fontSize: "14px",
                 color: "var(--color-text-muted)", maxWidth: "440px" }}
      >
        The execution-dag endpoint returned an error. Check the dashboard API logs.
      </p>
    </CenteredCard>
  );
```

---

### `dashboard/web/src/App.tsx` — third right-pane state
**Role:** component  **Data flow:** event-driven  
**Analog:** self — the existing `selectedPlan ? <ExecutionDAGView .../> : <RunningWavesView .../>` conditional at lines 358–369, and the `PaneHeader` with `action` slot at lines 74–87, and the "All waves" button at lines 340–351

**State addition** (add alongside `selectedPlan` state):
```typescript
const [showGlobalDAG, setShowGlobalDAG] = useState(false);
```

**"Global DAG" button** (add to EXECUTION pane header `action` slot, copying the "All waves" button pattern at lines 331–351):
```typescript
action={
  showGlobalDAG ? (
    <button
      onClick={() => setShowGlobalDAG(false)}
      style={{ /* same inline style as "All waves" button */ }}
    >
      All waves
    </button>
  ) : (
    <button
      onClick={() => { setSelectedPlan(null); setShowGlobalDAG(true); }}
      style={{ /* same inline style */ }}
    >
      Global DAG
    </button>
  )
}
```

**Pane label** — when `showGlobalDAG === true`, change PaneHeader label from `"EXECUTION"` to `"GLOBAL EXECUTION DAG"` (12px mono 600 muted, matching other PaneHeader labels per UI-SPEC).

**Three-way conditional** (replaces lines 358–369 of analog):
```typescript
{selectedPlan ? (
  <ExecutionDAGView
    planName={selectedPlan}
    plan={executionPlan}
    onTaskClick={onTaskClick}
  />
) : showGlobalDAG ? (
  <GlobalExecutionDAGView
    projectName={selectedProject ?? ""}
    project={globalExecutionDAG}
    onTaskClick={onTaskClick}
  />
) : (
  <RunningWavesView
    projectName={selectedProject ?? ""}
    onPlanClick={onPlanClick}
  />
)}
```

**Data fetch hook** — add `globalExecutionDAG` state and a `fetchProjectExecutionDAG(projectName)` call (copy `fetchPlan` hook pattern, change URL to `/api/v1/projects/${name}/execution-dag`, map `tasks[].phase` to `status` as `ExecutionTaskData.status`).

---

## Shared Patterns

### Error handling in Go handlers
**Source:** `cmd/dashboard/api/plans.go` lines 89–96  
**Apply to:** `cmd/dashboard/api/execution_dag.go`
```go
if err := h.Client.Get(ctx, ...); err != nil {
    if apierrors.IsNotFound(err) {
        writeError(w, http.StatusNotFound, fmt.Sprintf("... not found", name))
        return
    }
    h.Log.Error(err, "get ... failed", "name", name, "namespace", namespace)
    writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get ...: %s", err.Error()))
    return
}
```

### `writeJSON` response helper
**Source:** `cmd/dashboard/api/plans.go` line 173  
**Apply to:** `cmd/dashboard/api/execution_dag.go`
```go
writeJSON(w, http.StatusOK, responseStruct{...})
```

### controller-runtime `predicate.Funcs` pattern
**Source:** `internal/controller/task_controller.go` line 1740 (`predicate.AnnotationChangedPredicate{}`)  
**Apply to:** `internal/controller/task_controller.go` WR-02 fix — `predicate.Funcs{UpdateFunc: ..., CreateFunc: ..., DeleteFunc: ..., GenericFunc: ...}`

### Envtest `Eventually` polling
**Source:** `test/integration/envtest/global_wave_derivation_test.go` lines 115–122  
**Apply to:** `test/integration/envtest/spec_conformance_test.go` — use 30s timeout / 500ms poll for Wave assertions; 5s / 100ms for CRD creation confirmation.

### Dashboard embed freshness gate
**Source:** `Makefile` target `verify-dashboard-freshness` (lines 283–292) — runs `diff -rq dashboard/web/dist cmd/dashboard/embed/dist`  
**Apply to:** Every commit that touches `dashboard/web/src/` MUST run `make dashboard-frontend` and commit the updated `cmd/dashboard/embed/dist/` in the SAME commit. This is load-bearing per Phase 22 FIX-01.

---

## No Analog Found

All files in Phase 26 have close analogs. No entries in this section.

---

## Metadata

**Analog search scope:** `internal/controller/`, `test/integration/envtest/`, `cmd/dashboard/api/`, `dashboard/web/src/components/`, `internal/subagent/common/templates/`  
**Files scanned:** 14 source files read directly  
**Pattern extraction date:** 2026-06-17
