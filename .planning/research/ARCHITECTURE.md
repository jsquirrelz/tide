# Architecture Research

**Domain:** Kubernetes-native AI orchestrator (operator + dispatched Job pods + dashboard) running a 5-level hierarchical paradigm (Project → Milestone → Phase → Plan → Task → Wave) over two distinct DAGs (Planning + Execution), with Kahn-layered topological sort as the wave scheduler.
**Researched:** 2026-05-12
**Confidence:** HIGH on K8s operator patterns and CRD ownership/cascade; MEDIUM on dashboard log-streaming topology (multiple credible patterns exist, recommendation below is opinionated but not universal); MEDIUM on artifact-PVC layout (real numbers depend on cluster storage class — flagged for phase-time decision).

---

## Standard Architecture

### System Overview

The system is a single in-cluster control plane (one TIDE Deployment per cluster install, namespace-per-Project tenancy), several CRD Kinds tracking the 5-level hierarchy, ephemeral Job pods spawned per dispatched subagent, a shared per-Project PVC carrying artifacts, and a separate dashboard Deployment that proxies the K8s API for live state.

```
┌─────────────────────────────────────────────────────────────────────┐
│  Human / CI                                                          │
│  ┌─────────────┐         ┌──────────────────────────┐                │
│  │  tide CLI   │         │  Browser → Dashboard UI  │                │
│  └──────┬──────┘         └─────────────┬────────────┘                │
└─────────┼─────────────────────────────-┼─────────────────────────────┘
          │ kubectl apply                │ HTTPS + WebSocket
          │ (Project CRD, Secret)        │
          ▼                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                       K8s API Server (etcd)                          │
│  Project / Milestone / Phase / Plan / Task / Wave CRDs               │
│  + Jobs, Pods, Secrets, PVCs (built-ins)                             │
└──────────────┬──────────────────────────────┬───────────────────────-┘
               │ watches / status patches     │ watches / log proxy
               │                              │
               ▼                              ▼
┌──────────────────────────────────┐  ┌──────────────────────────────┐
│  TIDE Orchestrator Deployment    │  │  TIDE Dashboard Deployment   │
│  (one per cluster install)       │  │  (one per cluster install)   │
│                                  │  │                              │
│  ┌──────────────────────────┐    │  │  ┌──────────────────────┐    │
│  │ controller-runtime       │    │  │  │ Go HTTP server       │    │
│  │ Manager                  │    │  │  │  - GET /projects     │    │
│  │ ┌────────────────────┐   │    │  │  │  - GET /dag/...      │    │
│  │ │ ProjectController  │   │    │  │  │  - WS  /logs/:pod    │    │
│  │ │ MilestoneCtrl      │   │    │  │  │  (uses client-go to  │    │
│  │ │ PhaseCtrl          │   │    │  │  │   GET pods/log       │    │
│  │ │ PlanCtrl           │   │    │  │  │   ?follow=true)      │    │
│  │ │ TaskCtrl           │   │    │  │  └──────────────────────┘    │
│  │ │ WaveCtrl           │   │    │  │  ┌──────────────────────┐    │
│  │ └────────────────────┘   │    │  │  │ Static SPA assets    │    │
│  │ ┌────────────────────┐   │    │  │  │ (Mermaid + DAG view) │    │
│  │ │ planner-pool       │   │    │  │  └──────────────────────┘    │
│  │ │ semaphore (in-mem) │   │    │  └──────────────────────────────┘
│  │ │ executor-pool      │   │
│  │ │ semaphore (in-mem) │   │
│  │ └────────────────────┘   │
│  │ ┌────────────────────┐   │
│  │ │ pkg/dag (shared    │   │
│  │ │  Kahn-layered)     │   │
│  │ └────────────────────┘   │
│  │ ┌────────────────────┐   │
│  │ │ pkg/dispatch       │   │
│  │ │  (Subagent iface)  │   │
│  │ │  PodJobBackend     │   │
│  │ └────────────────────┘   │
│  │ ┌────────────────────┐   │
│  │ │ pkg/git push       │   │
│  │ │  (level-boundary)  │   │
│  │ └────────────────────┘   │
│  └──────────────────────────┘    │
└──────────────────────┬───────────┘
                       │ creates / watches
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Project namespace (e.g. tide-proj-acme)                             │
│                                                                      │
│  ┌──────────────────────────────┐  ┌──────────────────────────────┐  │
│  │ Planner Job pods             │  │ Executor Job pods            │  │
│  │ (rising-tide subagents)      │  │ (per-task subagents)         │  │
│  │  image: tide-subagent:vN     │  │  image: tide-subagent:vN     │  │
│  │  args: --role=planner        │  │  args: --role=executor       │  │
│  │        --level=milestone|... │  │        --plan=... --task=... │  │
│  │  env:  ANTHROPIC_API_KEY     │  │  env:  ANTHROPIC_API_KEY     │  │
│  │        (from project Secret) │  │        (from project Secret) │  │
│  │  mounts:                     │  │  mounts:                     │  │
│  │    /workspace ← Project PVC  │  │    /workspace ← Project PVC  │  │
│  │    /result    ← emptyDir     │  │    /result    ← emptyDir     │  │
│  └──────────────────────────────┘  └──────────────────────────────┘  │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  PersistentVolumeClaim (RWX) — one per Project              │    │
│  │  /workspace/repo/         ← cloned target repo              │    │
│  │  /workspace/artifacts/    ← MILESTONE.md, PLAN.md, diffs    │    │
│  │  /workspace/envelopes/    ← per-task result envelopes       │    │
│  └─────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Implementation |
|-----------|----------------|----------------|
| **Project controller** | Top-of-funnel reconciler. Validates Project spec, ensures namespace + PVC + git clone, decodes Secrets, creates first Milestone (or next ready Milestone per the milestone DAG), tracks overall run lifecycle, owns level-gate policy resolution. | `internal/controller/project_controller.go`, watches `Project`, owns `Milestone` |
| **Milestone controller** | Drives planner dispatch to author `MILESTONE.md` (or detect already-authored), then materializes Phase CRDs from the milestone artifact's declared phase DAG. | `internal/controller/milestone_controller.go`, watches `Milestone`, owns `Phase` |
| **Phase controller** | Same shape as Milestone: dispatch a planner to author the phase brief, then materialize Plan CRDs from the brief's plan list. Honors phase-level dependency edges before dispatch. | `internal/controller/phase_controller.go`, watches `Phase`, owns `Plan` |
| **Plan controller** | Dispatch a planner to author `PLAN.md`, validate the declared task DAG (cycle check), then materialize Task CRDs and a Wave CRD computed by Kahn-layered. | `internal/controller/plan_controller.go`, watches `Plan`, owns `Task` and `Wave` |
| **Wave controller** | Executes one wave of one Plan. Maintains the indegree map in-memory keyed by `(plan-namespace/plan-name, wave-index)`, dispatches executor Jobs for the current wave's tasks (respecting executor-pool semaphore), waits for the wave's tasks to terminate, then either advances to the next wave or surfaces wave-failure per strict-profile contract. | `internal/controller/wave_controller.go`, watches `Wave` and child `Task` status |
| **Task controller** | Translates a Task CRD into a Job + Pod, watches pod terminal state, reads the result envelope off the PVC, stamps Task.status (succeeded / failed / exit code / envelope digest), surfaces logs link. | `internal/controller/task_controller.go`, watches `Task`, owns `Job` |
| **`pkg/dag`** | Pure Go, no K8s deps. `ComputeWaves(nodes, edges) ([]Wave, error)` plus cycle detection. Used twice — same function called with PlanningDAG inputs and ExecutionDAG inputs. | Go package, table-driven tests, ~200 LoC |
| **`pkg/dispatch`** | Subagent interface (`Dispatch(ctx, SubagentSpec) (Handle, error)`, `Status(ctx, Handle) (Result, error)`). v1 has one concrete impl: `PodJobBackend` that creates a Job, watches it, parses the envelope from the PVC. | Go package, mockable interface |
| **`pkg/git`** | Clone-into-PVC at Project init, fetch/push at level boundaries, signing config, generic git-host abstraction (no GitHub-specific code). | Go package wrapping `go-git` or shelling to `git` |
| **Subagent image** | Single container image, role + level passed via flags. Embeds Claude Code (or Anthropic SDK) + tide-specific harness that reads context from `/workspace`, writes envelope to `/result/envelope.json`, exits with status code matching the result. | `cmd/subagent/`, multi-stage Dockerfile |
| **Dashboard** | Separate Deployment. Read-only HTTP API + static SPA. Uses client-go to: list CRDs (renders DAGs), proxy pod logs via `corev1.Pods().GetLogs(...).Stream()` over WebSocket to the browser. Service-account-scoped to read-only access in TIDE namespaces. | `cmd/dashboard/`, separate binary, separate ServiceAccount |
| **`tide` CLI** | Thin wrapper over `client-go`. Apply Project YAML, tail run (= follow CRD status across the hierarchy), inspect a wave (= print task DAG with statuses), resume (= no-op CRD touch that re-triggers reconcile). No local cache; reads cluster live. | `cmd/tide/`, cobra commands |

---

## Recommended Project Structure

```
.
├── cmd/
│   ├── manager/             # main.go — orchestrator Deployment entry point
│   │   └── main.go          #   registers all 6 controllers on one Manager
│   ├── dashboard/           # main.go — dashboard Deployment entry point
│   │   └── main.go          #   HTTP + WebSocket server, client-go reader
│   ├── subagent/            # main.go — subagent image entry point
│   │   └── main.go          #   args: --role={planner,executor} --level=...
│   └── tide/                # main.go — CLI binary
│       └── main.go          #   cobra root, subcommands
├── api/
│   └── v1alpha1/            # CRD type definitions (kubebuilder convention)
│       ├── project_types.go
│       ├── milestone_types.go
│       ├── phase_types.go
│       ├── plan_types.go
│       ├── task_types.go
│       ├── wave_types.go
│       ├── groupversion_info.go
│       └── zz_generated_deepcopy.go
├── internal/
│   ├── controller/          # one reconciler file per CRD Kind
│   │   ├── project_controller.go
│   │   ├── milestone_controller.go
│   │   ├── phase_controller.go
│   │   ├── plan_controller.go
│   │   ├── task_controller.go
│   │   └── wave_controller.go
│   ├── pool/                # parallelism budgets (semaphores)
│   │   ├── planner_pool.go
│   │   └── executor_pool.go
│   ├── git/                 # git remote operations
│   └── pvc/                 # PVC layout helpers (paths, envelope I/O)
├── pkg/                     # public-ish, importable, K8s-agnostic
│   ├── dag/                 # Kahn-layered, cycle detection
│   │   ├── kahn.go
│   │   └── kahn_test.go
│   ├── dispatch/            # Subagent interface + PodJobBackend impl
│   │   ├── interface.go
│   │   ├── pod_job_backend.go
│   │   └── envelope.go
│   ├── artifacts/           # artifact path conventions on PVC
│   └── otel/                # OpenInference span helpers
├── config/                  # kubebuilder-generated manifests
│   ├── crd/
│   ├── rbac/
│   ├── manager/
│   ├── dashboard/
│   └── samples/
├── charts/
│   └── tide/                # Helm chart (CRDs + Deployments + RBAC)
├── docs/
│   ├── architecture.md      # this file's eventual public sibling
│   └── crd-reference.md     # generated from api/v1alpha1
├── hack/                    # codegen, mocks, e2e setup
└── test/
    └── e2e/                 # kind-based self-hosting smoke test
```

### Structure Rationale

- **`api/v1alpha1/`** — Kubebuilder convention; alpha is honest for v1 of a paradigm-driven CRD set (we expect to learn from self-hosting and revise). One file per Kind keeps generated DeepCopy localized.
- **`internal/controller/`** — Kubebuilder go/v4 layout. `internal/` prevents external imports of controller internals; controller is singular per project convention. One file per Kind cleanly maps to the [recommended one-controller-per-Kind pattern](https://book.kubebuilder.io/reference/good-practices) — the alternative (one mega-orchestrator controller) is explicitly called out as a smell.
- **`pkg/dag/`** — The two-DAG property collapses into one Go package consumed twice. `pkg/` (not `internal/`) because this is the most reusable, paradigm-pure piece — eventually other tooling (CLI dag-lint, dashboard rendering, tests) will import it. No K8s types in the signatures: takes `[]Node, []Edge` returns `[]Wave`. This is the right boundary for "two DAGs, same algorithm."
- **`pkg/dispatch/`** — Public interface for the Subagent contract. Pluggability-from-day-one demands this be importable. v2 sidecar/streaming backends drop in here.
- **`cmd/{manager,dashboard,subagent,tide}/`** — Four binaries, four entry points. Single Go module, single `go build` matrix. The orchestrator Manager registers all six controllers; the dashboard is a separate binary with a separate ServiceAccount (different RBAC surface).
- **`charts/tide/`** — Helm chart is the v1 distribution unit. CRDs + ServiceAccounts + Deployments + RBAC + default `tide-system` namespace in one apply.

---

## Architectural Patterns

### Pattern 1: One Reconciler Per CRD Kind (Multiple Reconcilers, One Manager, One Deployment)

**What:** Six separate Reconciler implementations (`ProjectReconciler`, `MilestoneReconciler`, `PhaseReconciler`, `PlanReconciler`, `WaveReconciler`, `TaskReconciler`) all registered on one `controller-runtime.Manager` running inside one orchestrator Deployment. Each watches its own CRD Kind plus owned children. No "single mega-orchestrator controller" that reconciles all levels.

**When to use:** This is the controller-runtime [good-practice default](https://book.kubebuilder.io/reference/good-practices) and it lines up cleanly with the 5-level paradigm — each level reconciler does one thing.

**Trade-offs:**
- **Pro:** Independent rate-limiting and concurrency per Kind (`MaxConcurrentReconciles` is per-controller). High-fanout levels (Task) can crank the worker count without affecting low-fanout levels (Milestone).
- **Pro:** Sub-Single-Responsibility errors are local; a panic in `WaveReconciler` doesn't take down `ProjectReconciler`.
- **Pro:** Tests are small and per-level.
- **Con:** Six controllers share state for cross-level concerns (the parallelism semaphores, the DAG cache). That shared state is in the Manager process, accessed via injected pointers — fine, but it's the one place where "single controller" feels tempting. Resist.

**Example:**
```go
// cmd/manager/main.go
mgr, _ := ctrl.NewManager(cfg, ctrl.Options{...})

plannerPool := pool.New(cfg.PlannerBudget)   // separately-sized
executorPool := pool.New(cfg.ExecutorBudget) // separately-sized

(&ProjectReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr)
(&MilestoneReconciler{Client: mgr.GetClient(), PlannerPool: plannerPool}).SetupWithManager(mgr)
(&PhaseReconciler{Client: mgr.GetClient(), PlannerPool: plannerPool}).SetupWithManager(mgr)
(&PlanReconciler{Client: mgr.GetClient(), PlannerPool: plannerPool}).SetupWithManager(mgr)
(&WaveReconciler{Client: mgr.GetClient(), ExecutorPool: executorPool}).SetupWithManager(mgr)
(&TaskReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr)
mgr.Start(ctx)
```

### Pattern 2: One CRD Per Level — Owner-Reference Cascade, Plan Validates Before Spawning

**What:** Six distinct CRD Kinds — `Project`, `Milestone`, `Phase`, `Plan`, `Task`, `Wave`. Each child sets `metadata.ownerReferences[0]` to its parent with `blockOwnerDeletion: true, controller: true`. Deleting a `Project` cascades all the way down via [Kubernetes default background cascading deletion](https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/).

**When to use:** Always, for this paradigm. The alternative — embedding sub-resources as inline structs inside a parent CRD — was rejected because:

1. **etcd 1.5 MiB hard ceiling per object.** A real project's full hierarchy (one Project containing 10 Milestones × 5 Phases × 3 Plans × 30 Tasks = 4,500 Task records) embedded inline blows the limit. Splitting per Kind keeps each object small. [See etcd system limits.](https://etcd.io/docs/v3.6/dev-guide/limit/)
2. **Watches and rate-limiting are per-Kind.** You want different reconcile concurrency for Task (high fanout, cheap reconcile) vs Project (low fanout, expensive reconcile with git push). Inline embedding collapses both into one watch.
3. **Dashboard rendering is faster with `kubectl get plans -l project=foo` than walking nested structs.**
4. **Label-selector queries** (label every child with `tide.io/project`, `tide.io/milestone`, `tide.io/phase`, `tide.io/plan`) make ad-hoc inspection by humans and the dashboard tractable.

`Wave` deserves its own Kind (not embedded in `Plan`) because:
- Wave status (which wave is running, which tasks failed) is what the dashboard polls most.
- Resumption reads `Wave.status.completedTaskRefs` and re-derives indegrees against `pkg/dag` — having Wave as its own object means the cache rebuild is one GET, not a parse of a large Plan.

**Trade-offs:**
- **Pro:** Each object stays well under the etcd limit; the largest object is `Plan` carrying the declared task DAG (a few hundred edges max per Plan).
- **Pro:** Native cascade delete handles cleanup of in-progress runs.
- **Pro:** RBAC can scope per Kind (e.g., grant viewers read-only on Project + Wave but not Task).
- **Con:** Six CRD installs in the Helm chart. Routine but adds Helm template surface area.
- **Con:** Reconcile chain has more hops (Project → Milestone create → Milestone reconciles → Phase create → ...). Mitigated by event-driven watches: each parent's `status.phase=Authored` fires the child's create immediately.

**Example:**
```go
// internal/controller/plan_controller.go (excerpt)
func (r *PlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var plan tidev1alpha1.Plan
    if err := r.Get(ctx, req.NamespacedName, &plan); err != nil { ... }

    // 1. Validate declared task DAG (cycle detection at validation time, per spec).
    nodes, edges := tasksFromPlan(plan.Status.PlanDoc) // parsed from PLAN.md
    waves, err := dag.ComputeWaves(nodes, edges)
    if err != nil {
        plan.Status.Phase = "InvalidDAG"
        plan.Status.Conditions = appendCond(plan.Status.Conditions, "Cycle", err.Error())
        return ctrl.Result{}, r.Status().Update(ctx, &plan)
    }

    // 2. Materialize Task CRDs with owner ref to this Plan.
    for _, n := range nodes {
        task := &tidev1alpha1.Task{
            ObjectMeta: metav1.ObjectMeta{
                Name:      taskName(plan.Name, n.ID),
                Namespace: plan.Namespace,
                Labels: map[string]string{
                    "tide.io/project":   plan.Labels["tide.io/project"],
                    "tide.io/milestone": plan.Labels["tide.io/milestone"],
                    "tide.io/phase":     plan.Labels["tide.io/phase"],
                    "tide.io/plan":      plan.Name,
                },
                OwnerReferences: []metav1.OwnerReference{
                    *metav1.NewControllerRef(&plan, tidev1alpha1.GroupVersion.WithKind("Plan")),
                },
            },
            Spec: tidev1alpha1.TaskSpec{...},
        }
        _ = r.Create(ctx, task)
    }

    // 3. Materialize a Wave CRD that references this Plan's wave schedule.
    wave := &tidev1alpha1.Wave{...} // similar owner-ref + labels
    return ctrl.Result{}, r.Create(ctx, wave)
}
```

### Pattern 3: Two-DAG, One Algorithm — `pkg/dag` Used Twice

**What:** A single Go package `pkg/dag` exports `ComputeWaves(nodes []Node, edges []Edge) ([]Wave, error)`. K8s-agnostic, generic node IDs. Both the **planning DAG** (called when a Milestone needs to spawn parallel phase planner subagents) and the **execution DAG** (called when a Plan needs to compute Task waves) consume the same function.

**When to use:** Any time the spec's two-DAG distinction shows up. The boundary is per-call: the caller decides which DAG semantics by what they pass in.

**Trade-offs:**
- **Pro:** One source of truth for cycle detection, monotonicity, complexity bounds. One set of tests.
- **Pro:** Forces caller-side type discipline — Planning DAG callers and Execution DAG callers wrap `pkg/dag` outputs in their respective domain types (`PlanningWave`, `ExecutionWave`) rather than passing the raw `[]Wave` around. Keeps the two DAGs typed apart at the API level even though they share an algorithm.
- **Con:** Some temptation to add execution-specific concerns (artifact paths, model selectors) into `Node`. Resist — keep `Node` as `{ID string, Meta map[string]string}` and have callers project domain data through the Meta map or via a parallel map keyed by ID.

**Example:**
```go
// pkg/dag/kahn.go
package dag

type Node struct { ID string }
type Edge struct { From, To string }
type Wave []string

func ComputeWaves(nodes []Node, edges []Edge) ([]Wave, error) {
    indegree := make(map[string]int, len(nodes))
    for _, n := range nodes { indegree[n.ID] = 0 }
    for _, e := range edges { indegree[e.To]++ }

    succ := successorIndex(edges)
    waves := []Wave{}
    remaining := nodeIDs(nodes)

    for len(remaining) > 0 {
        var current Wave
        for id := range remaining {
            if indegree[id] == 0 { current = append(current, id) }
        }
        if len(current) == 0 { return nil, ErrCycle }
        sort.Strings(current) // deterministic wave order
        waves = append(waves, current)
        for _, id := range current {
            delete(remaining, id)
            for _, s := range succ[id] { indegree[s]-- }
        }
    }
    return waves, nil
}
```

```go
// internal/controller/milestone_controller.go uses it for planning:
phaseNodes, phaseEdges := planningNodesFromMilestone(&milestone)
phaseWaves, _ := dag.ComputeWaves(phaseNodes, phaseEdges)
plannerPool.DispatchWave(ctx, phaseWaves[0]) // wave 0 = phases with no deps

// internal/controller/wave_controller.go uses it for execution:
taskNodes, taskEdges := executionNodesFromPlan(&plan)
taskWaves, _ := dag.ComputeWaves(taskNodes, taskEdges)
executorPool.DispatchWave(ctx, taskWaves[wave.Status.Index])
```

### Pattern 4: Two Parallelism Budgets as In-Memory Semaphores + CRD Status Annotation

**What:** Two `chan struct{}` semaphores in the orchestrator process — `plannerPool` (default 16) and `executorPool` (default 4) — sized independently from a ConfigMap. Reconcilers acquire before dispatching a Job, release on Pod terminal phase. Capacity is **not** stored in a CRD; it's a Deployment config value (Helm value).

**When to use:** Always. The spec is explicit that planner and executor pools are sized separately. Burning a CRD for "running count" is overkill — it's process state.

**Trade-offs:**
- **Pro:** Trivial to implement (buffered channel). Zero etcd writes for capacity tracking. Restart-safe because on controller restart, the reconciler reads which Pods are currently Running and pre-charges the semaphore.
- **Pro:** Independent budgets give the operator the two knobs the spec demands without inventing a scheduling subsystem.
- **Con:** Multi-replica orchestrator (HA) means coordinating semaphore state. **For v1, run single-replica with leader election** ([standard controller-runtime pattern](https://book.kubebuilder.io/reference/good-practices)). HA-multi-replica with shared budget tracking is a v2 problem; one human watching one run does not need it.
- **Con:** On controller crash, in-flight Jobs are owned by K8s, not the controller. Restart pre-charges the semaphore from `kubectl get jobs -l tide.io/role=planner --field-selector=status.active=1` — this read is what makes resumption work without persisting the semaphore.

**Example:**
```go
// internal/pool/pool.go
type Pool struct {
    sem chan struct{}
    name string
}
func New(size int, name string) *Pool {
    return &Pool{ sem: make(chan struct{}, size), name: name }
}
func (p *Pool) Acquire(ctx context.Context) error {
    select {
    case p.sem <- struct{}{}: return nil
    case <-ctx.Done(): return ctx.Err()
    }
}
func (p *Pool) Release() { <-p.sem }

// Pre-charge on startup
func (p *Pool) PreCharge(ctx context.Context, c client.Client, role string) error {
    var jobs batchv1.JobList
    _ = c.List(ctx, &jobs, client.MatchingLabels{"tide.io/role": role})
    for _, j := range jobs.Items {
        if j.Status.Active > 0 { p.sem <- struct{}{} }
    }
    return nil
}
```

### Pattern 5: One Subagent Image, Role/Level Flags, Tools Mounted

**What:** A single container image (`tide-subagent:vX.Y.Z`) used for every dispatched Job. Behavior is selected at runtime by flags:
- `--role={planner,executor}`
- `--level={milestone,phase,plan,task}` (planner only — selects system prompt)
- `--task-ref` / `--plan-ref` / etc. (executor — the CRD it's executing against)

Tools (git CLI, language runtimes, kubectl-for-CRD-introspection if needed) are baked into the image rather than mounted, because v1 runs against the dogfood repo (Go) and the toolset is small. v2 can support per-Project tool overlays via init containers if needed.

**When to use:** Always for v1. Two-image (planner vs executor) would double the image surface area for no benefit — the divergence is prompt + tool-allowance, not binary code.

**Trade-offs:**
- **Pro:** One image to build, scan, sign. One Dockerfile.
- **Pro:** Easy to A/B-test prompt changes by changing the flag handler, not by rebuilding two images.
- **Pro:** When v2 adds a streaming subagent backend, it slots in behind the same `pkg/dispatch.Subagent` interface; the image contract doesn't change.
- **Con:** Larger single image than two purpose-built ones. Acceptable — base it on a slim distroless + Claude Code binary.
- **Con:** Role/level coupling lives in flag handling, not Go types. Mitigated by parsing flags into a typed `RunSpec` at startup and dispatching on the type, not on the string flag, in subsequent code.

### Pattern 6: One PVC Per Project, ReadWriteMany (RWX)

**What:** Each `Project` CRD's controller provisions one PVC, named `tide-pvc-<project>`, claimed `ReadWriteMany`. All subagent Pods for that Project mount it at `/workspace`. Layout:

```
/workspace/
├── repo/                       # cloned target repo, branch checkout per milestone
├── artifacts/
│   ├── M-001/                  # MILESTONE.md, per-phase briefs
│   │   ├── MILESTONE.md
│   │   ├── P-001/              # phase
│   │   │   ├── BRIEF.md
│   │   │   ├── L-001/PLAN.md   # plan
│   │   │   └── L-002/PLAN.md
│   │   └── P-002/...
│   └── M-002/...
└── envelopes/
    └── T-<task-uid>.json       # per-task result envelopes
```

**When to use:** v1 default. **Confidence: MEDIUM** — exact RWX driver is a deploy-time choice; recommend Helm chart leaves `storageClassName` empty and lets the cluster operator pick (EFS in AWS, Filestore in GCP, Azure Files in Azure, NFS or Longhorn on-prem). [Multiple drivers support RWX](https://kubernetes-csi.github.io/docs/drivers.html); kind dev clusters can use [`csi-driver-nfs`](https://github.com/kubernetes-csi/csi-driver-nfs) for the self-hosting demo.

**Why one-PVC-per-Project, not per-Milestone:**
- Cross-milestone artifact reads happen (later milestones reference earlier milestone docs).
- Provisioning a new PVC on every Milestone transition is slow and storage-class-dependent.
- Cleanup is by Project cascade delete — one PVC to garbage-collect.

**Trade-offs:**
- **Pro:** Simple mental model. One filesystem per project.
- **Pro:** Concurrent reads from many Pods in the same wave are exactly what RWX is for.
- **Con:** Concurrent writes between sibling task Pods in the same wave to **the same file** are a correctness hazard. The Subagent contract avoids this by partitioning writes: each task writes only its envelope (`/workspace/envelopes/T-<uid>.json`) and its declared-touched files under `/workspace/repo/`. The plan-validation step rejects task DAGs with overlapping file-touch sets (already required by the spec; this is just the storage-layer reason for it).
- **Con:** RWX-supporting CSI is more constrained than RWO. Document supported drivers; soft-fail with a clear error if the cluster's default class is RWO-only.

### Pattern 7: Git Push at Level Boundaries, From the Orchestrator

**What:** When a Milestone, Phase, or Plan reaches `status.Phase = Authored` and its artifact is on the PVC, the orchestrator (not the subagent pod) runs the git push. The orchestrator process has a small `pkg/git` package that:
1. Reads the git remote URL and creds from the Project's referenced Secret.
2. Mounts (read-only via a short-lived helper Job, or in-process if `go-git` is used) the relevant slice of `/workspace/repo` and `/workspace/artifacts`.
3. Commits with a structured message: `tide: milestone M-001 authored` etc.
4. Pushes to the remote branch.
5. On success, stamps the level's `status.gitCommit` field.

**Why orchestrator and not a sidecar in the final task Pod:**
- Subagent pods are dispatched per task, not per level. "The final task Pod of a level" is not well-defined in a parallel wave — you'd need to nominate one, which is a synchronization headache.
- Credentials are per-Project; the orchestrator already has Secret-reading RBAC. Giving every task Pod the git push cred enlarges the blast radius significantly.
- Push is at most a few times per minute. The orchestrator can do it on a goroutine without slowing reconciles.

**Trade-offs:**
- **Pro:** One cred handler in one process. Easier to rotate, audit, log.
- **Pro:** Task pods stay narrow-scoped (edit files, write envelope, exit) — they don't need network egress to the git host.
- **Pro:** Failed pushes (auth, network) don't fail the wave; orchestrator retries with backoff and surfaces the failure in `Project.status.conditions`.
- **Con:** Orchestrator does I/O beyond reconcile (git network ops). Mitigate with timeouts and a separate goroutine; don't block the reconcile loop.

### Pattern 8: Dashboard As Separate Deployment, Direct K8s API Proxy

**What:** Dashboard is its own Deployment (`tide-dashboard`) with its own ServiceAccount scoped to read-only verbs on tide CRDs and `pods/log` across project namespaces. Frontend is a static SPA bundled into the binary. Backend is a thin Go HTTP server that:

- `GET /api/projects` → `client.List(&ProjectList{})`
- `GET /api/projects/:p/dag/planning` → reads Milestones+Phases+Plans, renders DAG JSON
- `GET /api/projects/:p/dag/execution` → reads Tasks+Waves, renders DAG JSON
- `WS /api/pods/:ns/:pod/logs` → calls `clientset.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{Follow: true}).Stream(ctx)`, copies bytes to WebSocket

**Why separate from the orchestrator:**
- Different scaling profile (orchestrator is HA-singleton with leader election; dashboard is many-replica behind a Service).
- Different RBAC posture (read-only for dashboard; write for orchestrator).
- Compromise of dashboard (XSS in someone's commit message, for instance) does not give write access to CRDs.
- Different release cadence eventually (dashboard UI iterates faster than controller logic).

**Why NOT route logs through the orchestrator:**
- The orchestrator is a controller-runtime Manager — its goroutines are reconciliation workers, not HTTP proxies. Adding a log-streaming HTTP server to it would require careful lifecycle plumbing.
- The K8s API server already proxies `pods/log` natively with [WebSocket support as of v1.31](https://kubernetes.io/blog/2024/08/20/websockets-transition/). The dashboard hits this directly via client-go.

**Trade-offs:**
- **Pro:** Clean separation of concerns. Two Deployments, two ServiceAccounts, two RBAC scopes.
- **Pro:** Dashboard can be horizontally scaled if many viewers; orchestrator stays singleton.
- **Pro:** Logs go directly browser → dashboard pod → apiserver → kubelet → container. No detour through the orchestrator.
- **Con:** Two Helm sub-charts to maintain. Minor.
- **Con:** Dashboard needs cluster-aware service-account auth in-cluster vs kubeconfig out-of-cluster (for dev). Standard `client-go` boilerplate.

### Pattern 9: CLI As Thin Client-Go Wrapper

**What:** `tide` is a cobra-based CLI with a small set of commands:
- `tide project apply -f project.yaml` → `kubectl apply` equivalent for Project CRD
- `tide project tail <name>` → watches `Project`, `Milestone`, `Phase`, `Plan`, `Wave`, `Task` events; renders a live tree like `kubectl get` but tree-shaped
- `tide wave inspect <plan>` → prints the wave schedule and per-task status
- `tide resume <project>` → `kubectl annotate project <p> tide.io/resume=$(date +%s)` (the annotation change re-triggers reconcile; resumption logic lives in the controller, not the CLI)
- `tide artifact get <ref>` → reads artifact off the PVC via a short-lived pod (`kubectl debug`-style) or via a dedicated read-only Service exposed by the dashboard

No local cache. All state is read from the cluster on every command. This matches the "artifacts as source of truth, CRDs as cache" principle — adding a CLI-local cache would invert the hierarchy.

**Trade-offs:**
- **Pro:** Stateless CLI is trivially correct. No invalidation logic.
- **Pro:** Whatever `kubectl` users can do, `tide` users can do — and vice versa. The CLI is a UX layer, not a privilege layer.
- **Con:** Slightly more API server load (no caching). At v1 scale (one human, one cluster) this is irrelevant.

---

## Data Flow

### CRD Apply → Reconcile → Dispatch → Artifact

```
[Human / CI: tide project apply project.yaml]
    │
    ▼
[K8s API: stores Project CRD]
    │
    ▼ (watch fires)
[ProjectReconciler]
    ├─ creates Namespace tide-proj-<name> (if missing)
    ├─ creates PVC tide-pvc-<name> (RWX)
    ├─ clones target repo into /workspace/repo via init Job
    ├─ resolves gate policy from project.spec.gates
    └─ creates first Milestone CRD (M-001) with owner-ref to Project
        │
        ▼ (watch fires)
        [MilestoneReconciler]
            ├─ acquires plannerPool semaphore
            ├─ creates Job tide-plan-M-001 (subagent image, --role=planner --level=milestone)
            │     └─ Pod mounts PVC, writes /workspace/artifacts/M-001/MILESTONE.md + envelope.json, exits 0
            ├─ on Job success: reads envelope, sets Milestone.status.phase = Authored
            ├─ releases plannerPool semaphore
            ├─ ORCHESTRATOR runs git commit + push for MILESTONE.md (pkg/git)
            ├─ parses MILESTONE.md for phase DAG declarations
            ├─ computes planning waves via pkg/dag.ComputeWaves(phases, phaseDeps)
            ├─ checks gate policy (auto-pass vs human-approve)
            └─ creates Phase CRDs for first planning wave with owner-refs
                │
                ▼ (watch fires, per Phase)
                [PhaseReconciler] (same shape — phase brief planner, then materializes Plans)
                    │
                    ▼
                    [PlanReconciler]
                        ├─ dispatches plan-author planner subagent
                        ├─ on author success: validates declared task DAG via pkg/dag (cycle check)
                        ├─ on validation success: creates Task CRDs (one per declared task)
                        └─ creates a Wave CRD seeded with status.index = 0
                            │
                            ▼ (watch fires)
                            [WaveReconciler]
                                ├─ loads task DAG from sibling Task CRDs
                                ├─ computes execution waves via pkg/dag.ComputeWaves(tasks, taskDeps)
                                ├─ for each task in wave[Wave.status.index]:
                                │   - acquires executorPool semaphore
                                │   - patches Task.spec.dispatch = true (triggers TaskReconciler)
                                ▼
                                [TaskReconciler]
                                    ├─ creates Job tide-exec-T-<uid> (subagent image, --role=executor)
                                    │     └─ Pod mounts PVC, edits /workspace/repo/<declared files>,
                                    │       writes /workspace/envelopes/T-<uid>.json, exits 0 or non-zero
                                    ├─ on Job terminal: reads envelope, stamps Task.status.{phase, envelope, exitCode}
                                    └─ releases executorPool semaphore
                                ↑
                                │ (Task status watch fires WaveReconciler)
                                [WaveReconciler] (per task status update)
                                    ├─ decrements remaining-in-wave counter
                                    ├─ when wave complete:
                                    │   - if all tasks succeeded → Wave.status.index++; reconcile again for next wave
                                    │   - if any task failed → strict-profile contract:
                                    │       siblings continue (already dispatched), dependents never dispatch
                                    │       (their indegree never reaches 0)
                                    │   - non-dependents in next wave dispatch if strict; halt if conservative
                                    └─ when final wave complete:
                                        - mark Plan.status.phase = Complete
                                        - ORCHESTRATOR runs git commit + push for diffs
                                        - PhaseReconciler sees all Plans Complete → marks Phase.status.phase = Complete
                                        - MilestoneReconciler sees all Phases Complete → marks Milestone Complete
                                        - ProjectReconciler creates next Milestone (if any) or marks Project Complete
```

### Resumption Flow (Controller Restart)

On orchestrator pod restart (crash, rollout, node drain), the new controller process starts with empty memory. Resumption is **derived, not persisted**:

```
1. Manager starts; controllers register watches.
2. Each watch triggers an initial reconcile for every existing CRD of its Kind.
3. WaveReconciler reconciles each in-progress Wave:
   a. Loads sibling Task CRDs (status.phase tells which completed)
   b. Re-derives execution waves via pkg/dag.ComputeWaves(tasks, deps)
   c. Re-computes indegree map; subtracts completed tasks' outgoing edges
   d. For tasks in current wave with status.phase=Running:
      - These are in-flight Jobs. Pre-charges executorPool semaphore.
      - Continues watching; does NOT recreate.
   e. For tasks in current wave with status.phase=Pending:
      - Dispatches normally (creates Job).
   f. For wave that was complete before restart but next wave not started:
      - Advances Wave.status.index; reconcile loop fires next wave.
4. Persistent state read from cluster only:
   - Task.status.phase (completed-task set)
   - Plan.status.taskDAG (the edges, re-parsed from PLAN.md if absent)
   - Job/Pod status (in-flight detection)
5. NO state read from in-memory cache. NO state read from external DB.
```

This is exactly the spec's promise: "Resumption state is minimal: indegree map + completed-task set." Both are derivable in O(V+E) from CRD reads.

**Crucially:** If the orchestrator pod is gone for hours, the in-flight subagent Pods keep running independently. K8s owns their lifecycle. On orchestrator return, it reads their status and either harvests results (if they finished) or waits for them (if still running). The orchestrator is not on the data path of a running task.

---

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| **One project, one human watching (v1 target)** | Single-replica orchestrator with leader election. In-memory semaphores. CRD-status-only persistence. PVC-per-project. This is the design. |
| **Multiple concurrent projects, one cluster** | Same architecture. Namespace-per-project gives isolation. Orchestrator handles many Projects in the same Manager; semaphores are per-Project (keyed map) or global (a Project's burst doesn't starve others — flag as v1.1 tuning decision). |
| **Large projects (>1000 tasks per Plan)** | Plan CRD body (declared task DAG) might approach etcd limits. Mitigation: keep the *full* DAG in the artifact (`PLAN.md` on PVC) and store a *digest* + edge count in CRD spec. Tasks are already separate Kinds so they're fine. |
| **HA orchestrator (multi-replica)** | Defer to v2. Requires either (a) leader-election with passive replicas, or (b) sharded reconcile by namespace. (b) is cleaner but adds operational complexity not justified at v1 scale. |
| **Dashboard fan-out (many viewers)** | Dashboard is a separate Deployment with horizontal scaling. Behind a Service. Each replica is stateless. Logs WebSocket connections are sticky to the replica that initiated them. |

### Scaling Priorities

1. **First bottleneck: subagent API rate limits, not K8s.** Anthropic API rate limits, LLM token costs. Solve by sizing `plannerPool` and `executorPool` conservatively. The K8s control plane is not the binding constraint at any scale this paradigm reaches.
2. **Second bottleneck: PVC RWX driver throughput.** EFS has IOPS modes; NFS has throughput ceilings. Many concurrent task pods reading the same artifact files can hit them. Mitigation: minimize per-task PVC reads (pass artifact content via env or args when small; mount only when large).
3. **Third bottleneck: etcd write volume.** Many tasks → many `Task.status` patches per second. controller-runtime client batches these well; status subresource separates them from spec writes (which would otherwise contend with `kubectl apply` cycles). Monitor; unlikely to be hit at v1 scale.

---

## Anti-Patterns

### Anti-Pattern 1: Single Mega-Orchestrator Controller

**What people do:** Write one `OrchestratorReconciler` that reconciles `Project` and walks the entire hierarchy from there — creating Milestones, Phases, Plans, Tasks, Waves in nested loops.

**Why it's wrong:**
- Violates single-responsibility — controller-runtime's [good-practice guidance](https://book.kubebuilder.io/reference/good-practices) explicitly flags this.
- One reconcile call ends up doing minutes of work; controller workqueue stalls; rate-limiting becomes coarse.
- Errors deep in the tree (a failed plan author) are surfaced at the wrong level (Project shows "broken" instead of Plan).
- Testing the controller requires mocking the whole hierarchy.

**Do this instead:** Six reconcilers, one per Kind. Each reconciles **only its own object** plus enqueues children on status transitions. The Project controller's job is to ensure a Milestone exists with the right spec — it does NOT walk Milestones.

### Anti-Pattern 2: Persisting the Wave Schedule in CRD Status

**What people do:** On wave computation, write the full `[]Wave` (each wave's task list) into `Wave.status.schedule`. On restart, read it back to avoid recomputing.

**Why it's wrong:**
- Re-derivation is O(V+E) and the spec explicitly calls out that this is cheap and intentional.
- A stale stored schedule (Plan was edited between writes) leads to dispatching against the wrong wave.
- Spec is explicit: "resumption state is minimal: indegree map + completed-task set." Adding a stored schedule expands the resumption surface area.

**Do this instead:** Store `Wave.status.index` (which wave we're on) and read `Task.status.phase` for the completed-task set. Re-derive waves on every reconcile.

### Anti-Pattern 3: Embedding Sub-Resources Inline in Project CRD

**What people do:** Define a giant `ProjectSpec` containing nested `[]Milestone`, each containing `[]Phase`, each containing `[]Plan`, etc.

**Why it's wrong:**
- etcd 1.5 MiB ceiling per object kills any non-trivial project.
- Watches fire on the whole Project object for any nested change — no granular concurrency.
- Cascade delete is automatic but updates fight (every Phase status change is a write to the Project object, contending with every other Phase).

**Do this instead:** Six CRD Kinds, owner-references, labels for query indexing.

### Anti-Pattern 4: Streaming Logs Through the Orchestrator

**What people do:** Add an HTTP server to the orchestrator that proxies pod logs to the dashboard.

**Why it's wrong:**
- Mixes responsibilities (reconcile loop + HTTP proxy in one process).
- Two release cadences in one binary.
- One CVE in either takes both down.
- The K8s apiserver already does this via `pods/log` — there's no value-add.

**Do this instead:** Separate Dashboard Deployment with read-only RBAC. Browser → Dashboard pod → apiserver via client-go.

### Anti-Pattern 5: Two Subagent Images (Planner + Executor)

**What people do:** Build `tide-planner` and `tide-executor` as separate images.

**Why it's wrong:**
- Duplicates Dockerfile, build pipeline, scanning, signing.
- Tool drift between the two (planner gets a newer Claude Code than executor).
- The actual difference is system prompt and tool allowance, not binary code.

**Do this instead:** One image, role flag, runtime branching. Two-image only if/when binary code genuinely diverges (it won't for v1).

### Anti-Pattern 6: Sidecar Git-Push in the Final Task Pod

**What people do:** Add a sidecar to each task pod that runs git commit/push on pod exit.

**Why it's wrong:**
- "Final task of a level" is ill-defined in a parallel wave — you'd need to nominate one, adding synchronization.
- Multiplies cred surface (every task pod gets git creds).
- N pushes per wave for one logical commit.

**Do this instead:** Orchestrator pushes at level boundaries. One cred, one process, one commit per level.

### Anti-Pattern 7: Wave As Inline Field of Plan

**What people do:** Skip the `Wave` CRD, embed wave state directly in `Plan.status.currentWaveIndex` and `Plan.status.completedTasks`.

**Why it's wrong:**
- Conflates two different reconcile concerns (Plan authoring vs Wave execution). They have different watch surfaces (Plan watches its planner Job; Wave watches its Task children).
- Dashboard polling for "what's running now" is a query against Wave specifically — having it as its own Kind makes the query cheap.
- Failure surfacing — `Wave.status.phase = Failed` with `Wave.status.failedTaskRef` is cleaner than overloading Plan status with both authoring and execution concerns.

**Do this instead:** Wave is its own Kind. One Wave per Plan (Wave.spec.planRef → Plan). Wave reconciler is the execution engine.

---

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| **LLM provider (Anthropic v1, others later)** | Subagent image makes API calls directly from inside the dispatched Pod, reading `ANTHROPIC_API_KEY` from a Secret env var. Orchestrator does NOT proxy LLM calls. | Keeps orchestrator off the LLM data path. Failures surface as Job exit codes + envelope content. Pluggability via `pkg/dispatch.Subagent` interface means a future "in-orchestrator LLM client" backend can land without changing CRDs. |
| **Git remote (host-agnostic)** | Orchestrator's `pkg/git` connects to the remote at level boundaries using creds from the Project's Secret. Generic SSH/HTTPS — no GitHub/GitLab-specific code in v1. | Use `go-git` for portability or shell out to `git` (depending on protocol coverage needs — `go-git` has known SSH gaps). [Document this decision at implementation phase.] |
| **OpenTelemetry collector** | Both orchestrator and subagent containers export OTel spans/metrics via OTLP. Subagent uses OpenInference span conventions for LLM calls. Endpoint configured via Helm value. | OpenInference attribute set is what makes Phoenix/LangSmith/Arize queries work without custom dashboards. |
| **Prometheus** | Orchestrator exposes `/metrics` on a sidecar port; controller-runtime's built-in metrics (`workqueue_depth`, `reconcile_total`, `reconcile_errors_total`) plus custom metrics (`tide_waves_dispatched_total`, `tide_tasks_completed_total`, `tide_dispatch_latency_seconds`). | Standard ServiceMonitor in Helm chart. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| **Orchestrator ↔ K8s API** | client-go via controller-runtime cache. Watches + status patches. | Standard. Cache freshness is the only concern; controller-runtime handles it. |
| **Orchestrator ↔ Subagent Pod** | Dispatch: K8s Job creation. Result: envelope file at `/workspace/envelopes/<task-uid>.json` + exit code. | This is the contract. Spec is locked. |
| **Orchestrator ↔ Git remote** | HTTPS/SSH via `pkg/git`, creds from Secret. Async goroutine, retries with backoff. | Out-of-band from reconcile loop. Push failure does not block reconcile; it surfaces in Project status. |
| **Dashboard ↔ K8s API** | client-go, read-only RBAC. CRD lists for state, pods/log for streaming. | Separate ServiceAccount. Apiserver proxies log traffic to kubelet directly. |
| **Dashboard ↔ Browser** | HTTPS for state, WebSocket for logs. | WebSocket is the native log streaming protocol [as of K8s v1.31](https://kubernetes.io/blog/2024/08/20/websockets-transition/). |
| **CLI ↔ K8s API** | client-go via kubeconfig (out-of-cluster). | No CLI-side state. Reads cluster live. |
| **`pkg/dag` ↔ everything** | Pure function. No dependencies on anything K8s-related. | Imported by Plan controller (validation), Phase controller (planning waves), Wave controller (execution waves), and the dashboard (DAG rendering). |
| **`pkg/dispatch` ↔ controllers** | Controllers call `dispatch.Submit(ctx, spec)`; receive a Handle; poll/watch for completion. | One interface, multiple potential backends (v1: PodJobBackend; v2: in-process streaming or remote-worker). |

---

## Build Order Implications

A bootstrap-able sequence that lets each step be validated before adding the next:

1. **`pkg/dag`** — Pure Go, no K8s. Unit-testable in isolation. Build first. Includes Kahn-layered, cycle detection, table-driven tests for the spec's worked example. Confidence-building artifact for the whole project.

2. **CRD types in `api/v1alpha1/`** — Kubebuilder scaffold. All six Kinds defined, OpenAPI validation, no controller logic. Verifiable by `kubectl apply` of sample manifests and reading them back. Owner-reference wiring is purely declarative at this stage.

3. **Subagent image (minimal)** — Build a stub image that takes the flags, mocks the LLM call (writes a canned envelope), exits 0. Lets the dispatch path be tested end-to-end without any LLM tokens being spent.

4. **`pkg/dispatch.PodJobBackend`** — Concrete implementation against the stub image. Tested by spinning up a kind cluster, applying a Job, reading the envelope.

5. **Innermost controller pair: `TaskReconciler` + `WaveReconciler`** — Hardcode a Plan with a fixed task DAG (skip the planner-authored case). Validate: applying a Plan-with-tasks YAML produces a wave-by-wave Job dispatch, envelope harvesting, status transitions, strict-profile failure handling. **This is the dogfood-critical pair**; everything above is plumbing around it.

6. **`PlanReconciler`** — Stub the planner subagent dispatch (just writes a canned PLAN.md to PVC). Validates the materialize-Tasks-and-Wave step. Adds plan-time DAG validation via `pkg/dag`.

7. **Real subagent image (Claude-backed)** — Replace the stub. The dispatch contract is unchanged; only the image content changes. This decouples LLM-time learning from K8s-time learning.

8. **`PhaseReconciler`, `MilestoneReconciler`, `ProjectReconciler`** — Up-stack in order. Each adds one level of dispatch + materialization. The reconciler shape is repetitive (the spec's "same shape per level" property is reflected in code).

9. **`pkg/git` push at level boundaries** — Layer on top once levels produce artifacts. Test against a local bare repo before pointing at GitHub.

10. **Dashboard** — Last orchestrator-adjacent component. Has the most surface area (UI, WebSocket, DAG rendering) but depends on nothing the orchestrator doesn't already produce. Can be developed in parallel with steps 5-9 against canned CRD fixtures.

11. **`tide` CLI** — Thin layer; mostly a UX improvement over `kubectl`. Last because nothing else depends on it.

12. **Helm chart** — Packages all of the above. Built incrementally throughout, finalized when the self-hosting demo passes.

13. **Self-hosting MVP** — TIDE in kind drives its own next milestone on this repo. v1 ships.

**Critical-path insight:** Steps 1-5 deliver a *minimum viable orchestrator* that dispatches against a manually-authored Plan. Steps 6-9 layer on the up-stack planning. **Steps 1-5 should be a discrete first milestone**, because they prove the most novel part of the system (the wave-dispatch engine) before investing in any of the up-stack scaffolding.

---

## Sources

- [Kubebuilder Good Practices — one controller per Kind](https://book.kubebuilder.io/reference/good-practices)
- [Kubebuilder Quick Start (current project structure)](https://book.kubebuilder.io/quick-start)
- [Operator SDK common recommendations](https://sdk.operatorframework.io/docs/best-practices/common-recommendation/)
- [Argo Workflows architecture](https://argo-workflows.readthedocs.io/en/latest/architecture/)
- [argoproj/argo-workflows docs/architecture.md](https://github.com/argoproj/argo-workflows/blob/main/docs/architecture.md)
- [Tekton controller-logic developer docs](https://github.com/tektoncd/pipeline/blob/main/docs/developers/controller-logic.md)
- [Tekton PipelineRuns](https://tekton.dev/docs/pipelines/pipelineruns/)
- [Crossplane composite resources](https://docs.crossplane.io/latest/composition/composite-resources/)
- [Kubernetes cascading deletion docs](https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/)
- [Using Finalizers to Control Deletion (kubernetes.io blog)](https://kubernetes.io/blog/2021/05/14/using-finalizers-to-control-deletion/)
- [Kubernetes 1.31: SPDY → WebSockets for streaming](https://kubernetes.io/blog/2024/08/20/websockets-transition/)
- [etcd system limits — 1.5 MiB default request size](https://etcd.io/docs/v3.6/dev-guide/limit/)
- [Resolving CRD size limit issues](https://dev.to/ibraheemcisse/resolving-crd-size-limit-issues-with-keda-on-kubernetes-2930)
- [Kubernetes CSI Drivers — RWX-capable list](https://kubernetes-csi.github.io/docs/drivers.html)
- [aws-efs-csi-driver multi-pod example](https://github.com/kubernetes-sigs/aws-efs-csi-driver/blob/master/examples/kubernetes/multiple_pods/README.md)
- [Longhorn RWX volumes](https://longhorn.io/docs/1.11.1/nodes-and-volumes/volumes/rwx-volumes/)
- [controller-runtime MaxConcurrentReconciles discussion](https://github.com/kubernetes-sigs/controller-runtime/issues/1841)
- [Kubernetes Jobs — fine parallel processing with a work queue](https://kubernetes.io/docs/tasks/job/fine-parallel-processing-work-queue/)

---
*Architecture research for: Kubernetes-native AI orchestrator running the TIDE 5-level paradigm*
*Researched: 2026-05-12*
