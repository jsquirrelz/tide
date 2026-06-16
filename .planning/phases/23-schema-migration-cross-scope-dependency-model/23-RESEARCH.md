# Phase 23: Schema Migration + Cross-Scope Dependency Model — Research

**Researched:** 2026-06-16
**Domain:** kubebuilder CRD versioning, controller-runtime admission webhooks, K8s multi-version CRD migration, cross-scope dependency schema design
**Confidence:** HIGH (codebase read directly; kubebuilder/controller-runtime confirmed via Context7 + official markers)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** One reference system — flat structural IDs. No interface-id namespace, no provides/exposes field. A `dependsOn` entry is a flat string naming a hierarchy node (Milestone/Phase/Plan/Task), resolved within the single project namespace (all CRs share one namespace per namespace-per-project model, so refs never cross namespaces). The earlier "declared interface points / `provides` / `exposes`" idea is explicitly dropped.
- **D-02:** `dependsOn []string` lives on every level, targets may name any level. Generalize the existing `Milestone.dependsOn` / `Phase.dependsOn` (currently sibling-only), **add `dependsOn` to `Plan`** (it has none today), and **broaden `Task.dependsOn` past plan-local** (retire D-F1's same-Plan restriction). An entry may target a node at any level.
- **D-03:** Progressive refinement — coarse first, narrowed as planning descends. MB requires MA → MB requires MA-P3 → MB requires MA-P3-PB → MB requires MA-P3-PB-task-07.
- **D-04:** Refinement model = author-per-level + per-unit resolution (NOT scattered mutate-in-place). Each scope authors its own `dependsOn` at the granularity it knows and never reaches into another CRD to rewrite it mid-planning.
- **D-05:** Resolution intended in-memory (final mechanic locked in Phase 24). Phase 23 must only shape the schema so in-memory resolution is *possible*. Authored coarse `dependsOn` is the only persisted truth; resolved task edges are derivation.
- **D-06:** Assembler (Phase 24) collapses DEPS-01 + DEPS-02 into one mechanism. A Task-targeting entry = direct edge; a scope-targeting entry = fan-in/fan-out over that scope's tasks at assembly time.
- **D-07:** Keep a Wave CR, re-owned Plan→Project. Replace `WaveSpec.PlanRef` with a Project ref; `WaveSpec.WaveIndex` becomes the global, monotonic wave index. One Wave CR per global wave.
- **D-08:** `wave` telemetry label resemanticized to global wave index. Keep locked metric label set `{project, phase, plan, wave}`; `task` label stays forbidden.
- **D-09:** Clean break — v1alpha2-only served, reject old-shape objects, drop conversion webhook. Introduce `v1alpha2` with new shape, remove `v1alpha1` from served versions. Controller fail-closed rejects any surviving old-shape object with a clear status condition (e.g. `reason: RequiresReinstall`). Retire the no-op conversion-webhook scaffolding (`api/v1alpha1/plan_conversion.go`, Hub(), wave_webhook.go, plan_webhook.go, webhook manifests). Documented reinstall + version bump.
- **D-10:** Cycle rejection across plan/phase/milestone boundaries, controller-side (CEL can't express all-paths), validation-time, involved nodes surfaced via status condition, reuse `pkg/dag` CycleError, no runtime recovery.

### Claude's Discretion

- Exact `v1alpha2` field names, kubebuilder markers, CEL constraints, printer columns, and deepcopy/regeneration mechanics.
- The precise status-condition type/reason strings for old-object rejection and cycle rejection.
- How `make verify-no-aggregates` / `verify-dag-imports` guards are kept green through the schema change (and whether any guard grep-pattern needs updating for the global wave index — must stay forbidden as a *cached schedule*, permitted as a *Wave CR spec*).

### Deferred Ideas (OUT OF SCOPE)

- Global Kahn derivation / assembly engine + bidirectional global wave index — Phase 24.
- Global dispatch off one indegree map, wave-boundary failure semantics at global scope, gates-as-holds, minimal resumption — Phase 25.
- Multi-milestone drive via the Milestone DAG + cross-milestone global waves + milestone gate policy + README conformance test — Phase 26.
- Write-back vs. in-memory resolution final lock — deferred to Phase 24's engine work (D-05).
- Planner-prompt discipline for correct dependency refinement — lands when the cross-scope-aware planners are built.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SCHEMA-01 | Wave derivation/ownership moves from Plan to global (Project) scope; Wave CR carries a global wave index | WaveSpec.PlanRef → ProjectRef; WaveSpec.WaveIndex = global monotonic index; naming scheme changes from `tide-wave-<plan.UID>-<i>` to `tide-wave-<project.UID>-<N>` |
| SCHEMA-02 | `wave` telemetry label resemanticized to global wave; locked `{project, phase, plan, wave}` kept; `task` stays forbidden | `internal/metrics/registry.go` label set is unchanged in arity; `wave` value source changes from per-plan layer index to global WaveIndex |
| SCHEMA-03 | Breaking CRD changes ship with documented migration/conversion path and version bump; in-flight Projects not silently corrupted | v1alpha2 introduction mechanics, Hub() removal, `+kubebuilder:unservedversion` or full deletion of v1alpha1, reinstall doc |
| DEPS-01 | Task can declare deps on Tasks in other Plans/Phases/Milestones (retire D-F1) via qualified refs resolved into global DAG | `TaskSpec.DependsOn` comment/constraint update to remove plan-local restriction; `PlanRef` field stays for ownership, DependsOn becomes unrestricted flat name |
| DEPS-02 | Plan-, Phase-, Milestone-level interface dependency declarations reconciled into global task DAG (coarse deps → fan-in/fan-out) | Add `dependsOn []string` to PlanSpec; generalize Phase/Milestone DependsOn comments to say "any level" not "sibling-only" |
| DEPS-03 | Cyclic global Execution DAG rejected at validation time with involved nodes surfaced across plan/phase/milestone boundaries | Global cycle gate in Project reconciler, reusing `pkg/dag.CycleError`, surfacing as `ProjectStatus.Conditions` entry |
</phase_requirements>

---

## Summary

Phase 23 is a pure schema + reference-model + migration + cycle-validation surface phase. The codebase today ships one served+stored API version (`v1alpha1`) for six Kinds (Project, Milestone, Phase, Plan, Task, Wave). The migration introduces `v1alpha2` as the sole served-and-stored version, removes v1alpha1 from the serving matrix entirely, deletes the no-op conversion webhook scaffolding, and re-shapes the schemas for `dependsOn` breadth and `WaveSpec` ownership. No new execution logic ships in this phase; the schema must only make the Phase 24 global Kahn engine *possible*.

The two most surgical concerns are: (1) the kubebuilder mechanics of introducing v1alpha2 without a conversion webhook (D-09 is a clean break, not a live migration), and (2) the cycle rejection surface (D-10), which must assemble the full Project dep graph and call `pkg/dag.ComputeWaves` without building the Phase-24 dispatch engine.

**Primary recommendation:** Introduce `api/v1alpha2/` as a new package mirroring the current v1alpha1 directory layout, add `+kubebuilder:storageversion` only on v1alpha2 types, mark all v1alpha1 types with `+kubebuilder:unservedversion`, delete the `Hub()`/conversion scaffolding and the v1alpha1 admission webhook registrations, then regenerate CRDs. The controller guard for old-shape objects is a reconciler-head check in the Project reconciler (not a webhook) that sets `ConditionParentUnresolved` with `reason: RequiresReinstall` and returns `reconcile.TerminalError`.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| v1alpha2 type definitions | `api/v1alpha2/` package | — | kubebuilder convention: one package per version |
| deepcopy generation | `make generate` (controller-gen) | — | Automatic from `+kubebuilder:object:generate=true` |
| CRD YAML regeneration | `make manifests` (controller-gen) | Helm chart via helmify | CRDs live in `config/crd/bases/`; chart consumes them |
| Old-object fail-closed guard | Project reconciler head | — | Needs cache-backed client to inspect object shape; reconciler sees all projects on restart |
| Cross-scope cycle detection (DEPS-03) | Project reconciler | `pkg/dag.ComputeWaves` | Must assemble all Tasks across all Milestones/Phases/Plans; reconciler has cache-backed client; CEL can't do all-paths |
| `dependsOn` field validation (format) | CEL on CRD (`x-kubernetes-validations`) | — | MinLength, self-reference rejection are expressible in CEL |
| Wave CR ownership | Project scope (WaveSpec.ProjectRef) | — | Phase 24 creates Wave CRs; Phase 23 only changes the spec field |
| Metric label source | `internal/metrics/registry.go` + callers | Wave CR `Spec.WaveIndex` | Label arity unchanged; value source of `wave` changes to global index |
| Conversion webhook scaffolding | DELETE | — | D-09 clean break; no runtime conversion needed |
| Migration documentation | `docs/migration/` or Helm chart README | release notes | Must document reinstall steps + version bump |

---

## Standard Stack

### Core (unchanged from project stack)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| controller-gen | kubebuilder v4.14.0 bundled | Generates CRD YAML + deepcopy from markers | CLAUDE.md pinned |
| kubebuilder | v4.14.0 | Marker vocabulary, scaffold conventions | CLAUDE.md pinned |
| controller-runtime | v0.24.x | `ctrl.NewWebhookManagedBy`, `reconcile.TerminalError`, field indexers | CLAUDE.md pinned |
| pkg/dag (internal) | current | `ComputeWaves`, `CycleError` reuse for DEPS-03 | Import firewall: must stay k8s-free |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `k8s.io/apimachinery/pkg/api/meta` | via controller-runtime | `meta.SetStatusCondition` for `RequiresReinstall` condition | Old-object rejection in reconciler |
| `sigs.k8s.io/controller-runtime/pkg/reconcile` | v0.24.x | `reconcile.TerminalError` for permanent old-object rejection | Prevents requeue storm on old-shape CRs |

**No new external packages are introduced in Phase 23.** All tools are already in `go.mod`.

---

## Package Legitimacy Audit

No new packages are installed in Phase 23. This section is not applicable.

---

## Architecture Patterns

### System Architecture Diagram

```
[kubectl apply v1alpha2 CRD]
       |
       v
[API server: only v1alpha2 served/stored]
       |
       v
[Project Reconciler — head guard]
       |-- spec has required v1alpha2 fields? → YES → proceed normally
       |-- NO (old v1alpha1 shape in etcd) → SetStatusCondition(RequiresReinstall)
                                              reconcile.TerminalError → no requeue
       |
       v
[Project Reconciler — dep graph assembly (DEPS-03)]
  list all Milestones → list all Phases → list all Plans → list all Tasks
       |
       v
  build (nodes=Task.Names, edges=from all DependsOn at all levels)
       |
       v
  pkg/dag.ComputeWaves(nodes, edges)
       |-- CycleError → SetStatusCondition(CycleDetected, involvedNodes)
       |-- OK → proceed (no schedule stored — Phase 24 consumes result)
```

### Recommended Project Structure

```
api/
├── v1alpha1/                      # KEPT in tree (unserved); types carry +kubebuilder:unservedversion
│   ├── groupversion_info.go       # unchanged
│   ├── *_types.go                 # all unchanged EXCEPT wave, task, plan get +unservedversion on Kind marker
│   ├── plan_conversion.go         # DELETE (Hub() no-op stub)
│   └── zz_generated.deepcopy.go  # regenerated (same types)
└── v1alpha2/                      # NEW package
    ├── groupversion_info.go       # Group=tideproject.k8s, Version=v1alpha2
    ├── wave_types.go              # WaveSpec: ProjectRef string, WaveIndex int (SCHEMA-01)
    ├── task_types.go              # TaskSpec.DependsOn: broadened (DEPS-01, D-F1 retired)
    ├── plan_types.go              # PlanSpec.DependsOn added (DEPS-02)
    ├── phase_types.go             # PhaseSpec.DependsOn: "any level" not "sibling-only"
    ├── milestone_types.go         # MilestoneSpec.DependsOn: "any level" not "sibling-only"
    ├── project_types.go           # ProjectStatus gets CycleDetected condition support
    ├── shared_types.go            # ReasonRequiresReinstall, ReasonCycleDetectedGlobal constants
    └── zz_generated.deepcopy.go  # generated

internal/
├── webhook/
│   └── v1alpha1/
│       ├── plan_webhook.go        # UPDATE: now references v1alpha2 types; OR keep as v1alpha1 for backward compat
│       ├── wave_webhook.go        # RETIRE (or re-register for v1alpha2 Wave)
│       └── project_webhook.go    # KEEP, update import to v1alpha2
└── controller/
    ├── plan_controller.go         # UPDATE: import v1alpha2, stub-out materializeWaves (Phase 24 replaces)
    ├── wave_controller.go         # UPDATE: Spec.PlanRef → Spec.ProjectRef
    └── project_controller.go     # UPDATE: add dep-graph assembly + cycle gate (DEPS-03)

cmd/manager/main.go               # UPDATE: register v1alpha2 scheme, drop v1alpha1 conversion wiring
config/
└── crd/bases/                    # regenerated by make manifests
```

### Pattern 1: v1alpha2 Introduction — Clean Break (D-09)

**What:** Introduce `api/v1alpha2/` with re-shaped types. Mark ALL v1alpha1 types with `+kubebuilder:unservedversion`. Mark v1alpha2 types as `+kubebuilder:storageversion`. Delete `plan_conversion.go` (Hub() stub). Regenerate CRDs.

**Key marker mechanics:**
- `+kubebuilder:storageversion` on ONE type in `api/v1alpha2/` per Kind marks it as the storage version.
- `+kubebuilder:unservedversion` on v1alpha1 types causes controller-gen to emit `served: false` in the CRD spec for those versions but keeps the version entry present (so etcd migration tooling can see it).
- With only one served version and no conversion webhook wired, the CRD `spec.conversion.strategy` becomes `None` (the default when no webhook is configured), which is correct for D-09.

**Critical sequencing:**

```
1. Create api/v1alpha2/ package with all six Kinds + new field shapes
2. Add +kubebuilder:storageversion to v1alpha2 plan_types.go (ONLY plan needs it if Plan
   is the "hub" for conversion purposes — but since D-09 removes conversion entirely,
   EACH kind independently needs +kubebuilder:storageversion on its v1alpha2 form)
3. Add +kubebuilder:unservedversion to each Kind's root marker in api/v1alpha1/*_types.go
4. Delete api/v1alpha1/plan_conversion.go (removes Hub() — no longer any conversion)
5. Register v1alpha2.AddToScheme in cmd/manager/main.go
6. Remove v1alpha1 webhook path registrations for plan + wave (they referenced v1alpha1 types)
7. Run: make generate manifests
8. Verify: make verify-no-aggregates (grep still targets api/v1alpha1 AND api/v1alpha2)
9. Verify: no CRD emits conversion.strategy=Webhook
```

**Source:** `+kubebuilder:unservedversion` marker documented at [https://book.kubebuilder.io/reference/markers/crd.html] `[CITED: book.kubebuilder.io]`

**Example (v1alpha2 wave_types.go):**
```go
// api/v1alpha2/wave_types.go

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion                    // ← NEW: this is now the storage version
// +kubebuilder:resource:scope=Namespaced
// ...
type Wave struct { ... }
```

**Example (v1alpha1 wave_types.go — after adding unservedversion):**
```go
// api/v1alpha1/wave_types.go

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:unservedversion                   // ← ADD: makes v1alpha1 served:false
// +kubebuilder:resource:scope=Namespaced
// ...
type Wave struct { ... }
```

**Current state (confirmed from codebase read):**
- `plan_types.go` has `+kubebuilder:storageversion` on v1alpha1 `Plan` — this must move to v1alpha2 `Plan` and be REMOVED from v1alpha1.
- `plan_conversion.go` has only `func (*Plan) Hub() {}` — trivially deleted.
- Wave and Task do NOT have `+kubebuilder:storageversion` today on v1alpha1 — they will get it on v1alpha2.

**What make manifests regenerates:**
- `config/crd/bases/tideproject.k8s_*.yaml` — CRD specs with versions list, served/storage flags, schema validation.
- `api/v1alpha2/zz_generated.deepcopy.go` — after `make generate`.

### Pattern 2: WaveSpec Re-Ownership (SCHEMA-01, D-07)

**Current shape (v1alpha1):**
```go
type WaveSpec struct {
    PlanRef   string `json:"planRef"`   // owning Plan name
    WaveIndex int    `json:"waveIndex"` // 0-indexed per-plan layer
}
```

**New shape (v1alpha2):**
```go
type WaveSpec struct {
    // ProjectRef is the name of the owning Project (same namespace).
    // +kubebuilder:validation:MinLength=1
    ProjectRef string `json:"projectRef"`

    // WaveIndex is the global, monotonic 0-indexed wave position across the
    // entire Project's execution DAG. Derived by pkg/dag.ComputeWaves over
    // all Tasks in all Milestones/Phases/Plans; never cached in status.
    // +kubebuilder:validation:Minimum=0
    WaveIndex int `json:"waveIndex"`
}
```

**Impact on Wave naming:** `tide-wave-<plan.UID>-<i>` → `tide-wave-<project.UID>-<N>` where N is the global index. The Phase-24 engine writes this; Phase 23 only defines the spec shape.

**verify-no-aggregates guard impact:** The grep pattern is `Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag` (confirmed from Makefile line 529). `WaveIndex` is NOT in the denylist — it is a spec field on the Wave CR, not a cached aggregate in status. The guard stays green with no changes needed to the Makefile pattern. [VERIFIED: codebase read]

**wave_controller.go impact:** The `wave_controller.go` uses `wave.Spec.PlanRef` to list Tasks via the `taskPlanRefIndexKey` field index. After the schema change:
- `wave.Spec.PlanRef` disappears; the wave controller must list by `wave.Spec.ProjectRef` + a different field index (or a label selector).
- Phase 23 must stub or update the wave controller enough that it compiles against v1alpha2. Full rewire of Task dispatch is Phase 24.

### Pattern 3: dependsOn on Every Level (DEPS-01, DEPS-02, D-01, D-02)

**Changes per level in v1alpha2:**

| Level | Current `dependsOn` | v1alpha2 change |
|-------|---------------------|-----------------|
| Milestone | `[]string` sibling-only (comment says "sibling Milestone names in same Project") | Update comment: "any level node (Milestone/Phase/Plan/Task) in this Project" |
| Phase | `[]string` sibling-only (comment says "sibling Phase names in same Milestone") | Update comment: "any level node in this Project" |
| Plan | NONE | Add `DependsOn []string` field |
| Task | `[]string` plan-local per D-F1 (comment says "sibling Task names in the same Plan") | Update comment: "any Task, Plan, Phase, or Milestone name in this Project (D-F1 retired)" |

**CEL validation worth adding (Claude's discretion):**

For a `dependsOn` string entry, the only structural CEL constraint worth enforcing at admission time is MinLength. Self-reference rejection ("task X cannot depend on itself") requires knowing the object's own name at validation time — CEL `self.name` is available via `x-kubernetes-validations` on the object root. A minimal CEL rule:

```yaml
x-kubernetes-validations:
  - rule: "!self.spec.dependsOn.exists(d, d == self.metadata.name)"
    message: "dependsOn must not contain the object's own name"
```

This is expressible in CEL and cheap; include it. Cross-object cycle detection (D-10) remains controller-side per CLAUDE.md convention.

**No change to existing field indexing:** The `taskPlanRefIndexKey` (`.spec.planRef`) index is used to list Tasks owned by a Plan. `TaskSpec.PlanRef` remains in v1alpha2 for ownership — it is NOT a dep-resolution field. `TaskSpec.DependsOn` is the dep-declaration field and is not indexed (Phase 24's assembler iterates all Tasks and builds the dep graph in memory, consistent with D-05 in-memory resolution).

### Pattern 4: Old-Object Fail-Closed Guard (D-09, SCHEMA-03)

**The problem:** After the CRD upgrade to v1alpha2-only storage, any Project that was persisted in etcd as v1alpha1 will either (a) fail to read (if v1alpha1 is fully absent from the CRD spec) or (b) read as a partially-valid v1alpha2 object (if v1alpha1 is `served:false` but etcd still has it under the old storage version key).

**Concrete detection approach:** The simplest fail-closed guard is checking for a now-required field that was absent in v1alpha1. Given D-09 is a "reinstall", the guard need not be elaborate — it checks whether the object satisfies v1alpha2 invariants.

Option A (recommended): **Schema-gated required field.** Add a field to ProjectSpec in v1alpha2 that is `+kubebuilder:validation:Required` (or use CEL `required`) and was absent in v1alpha1. On a reinstall, the operator re-applies the YAML with the new field populated. An old-etcd object missing the field will fail admission when updated. For the *reconciler* read path, the object may still be fetched (etcd has the raw bytes) — the reconciler guard checks for this field being empty and surfaces the condition.

Option B: **Version annotation check.** The K8s API server stamps `metadata.resourceVersion` but NOT the API version in `metadata`. However, the controller can check for the presence of a v1alpha2-specific required field.

**Recommended implementation (controller head guard):**

```go
// In ProjectReconciler.Reconcile, immediately after Get:
if !isV2Shape(&project) {
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:               "Ready",
        Status:             metav1.ConditionFalse,
        Reason:             "RequiresReinstall",
        Message:            "Project was created with v1alpha1 schema; reinstall required: kubectl delete project <name> && kubectl apply -f <project.yaml>",
        LastTransitionTime: metav1.Now(),
    })
    _ = r.Status().Update(ctx, &project)
    return ctrl.Result{}, reconcile.TerminalError(
        fmt.Errorf("project %s/%s requires reinstall (old schema)", project.Namespace, project.Name),
    )
}
```

**`isV2Shape` detection:** The cleanest v2 marker is a new required field in `ProjectSpec` that was not present in v1alpha1. Alternatively, a `SchemaVersion string` field set to `"v1alpha2"` by a defaulting webhook. The planner must decide; both patterns are viable.

**`reconcile.TerminalError` semantics (controller-runtime v0.24.x):** Wraps an error to prevent the controller from requeuing the request — it is a permanent failure signal. [VERIFIED: Context7 controller-runtime docs]

### Pattern 5: Global Cycle Rejection (DEPS-03, D-10)

**Where it runs:** The Project reconciler is the correct seat. It is the only reconciler that (a) has access to all child Milestones/Phases/Plans/Tasks in the project namespace and (b) is reconciled on changes to any of them via owner-ref watch chains.

**Existing per-plan cycle detection for comparison:** `internal/webhook/v1alpha1/plan_webhook.go` calls `pkg/dag.ComputeWaves(nodes, edges)` over Tasks in a single Plan (lines 150–165). The pattern to replicate at global scope is identical but the input set spans the entire Project.

**Minimal Phase-23 surface (NOT Phase 24):** Phase 23's cycle gate is a *validation-time* check, not a dispatch engine. It assembles the dep graph, calls `ComputeWaves`, and if a `CycleError` is returned, surfaces it. It does NOT store the schedule (that would trip `verify-no-aggregates`) and does NOT dispatch any Tasks.

```go
// In ProjectReconciler — after verifying v2 shape, before any dispatch
func (r *ProjectReconciler) checkGlobalCycleGate(ctx context.Context, project *tideprojectv2alpha1.Project) (bool, error) {
    nodes, edges, err := r.assembleProjectDepGraph(ctx, project)
    if err != nil {
        return false, err // transient; requeue
    }
    if _, err := dag.ComputeWaves(nodes, edges); err != nil {
        var cyc *dag.CycleError
        if errors.As(err, &cyc) {
            meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
                Type:    "CycleDetected",
                Status:  metav1.ConditionTrue,
                Reason:  "GlobalCycleDetected",
                Message: fmt.Sprintf("cyclic global Execution DAG involving: %v", cyc.InvolvedNodes),
                // ...
            })
            // Do NOT use TerminalError here — a plan edit may fix the cycle; requeue on changes
            return true, nil
        }
        return false, err
    }
    return false, nil
}
```

**assembleProjectDepGraph** iterates:
1. List all Milestones with `client.InNamespace(project.Namespace)` + label `tideproject.k8s/project=<name>`
2. List all Phases same way
3. List all Plans same way
4. List all Tasks same way
5. Build `nodes = {task.Name for all tasks}` (task-granularity is the global DAG node)
6. Build edges:
   - For each Task: edges from `task.Spec.DependsOn` entries that NAME another Task (DEPS-01 direct edges)
   - For each Task: entries naming a Plan/Phase/Milestone are fan-out in Phase 24; at Phase-23 time they can be treated as "pending resolution" and ignored for cycle detection (no edge = conservative; cycle detection does not produce false positives for unresolved coarse refs)
   - Phase-24 NOTE: the full fan-out of scope deps is Phase 24 territory; Phase 23's cycle gate only needs to catch cycles in the concrete task-level edges.

**Why NOT a webhook:** The global cycle gate requires listing all Tasks across all Plans in the namespace — this is a multi-object list on the admitting webhook's request path and makes the webhook stateful-and-slow. Controller-side is correct per CLAUDE.md ("CEL CRD validation, not admission webhooks — except for cycle detection if CEL can't express all-paths"). The reconciler fires on any Project change, which is when it matters. [VERIFIED: CLAUDE.md line "CEL CRD validation, not admission webhooks"]

### Anti-Patterns to Avoid

- **Storing wave layers in ProjectStatus:** The `verify-no-aggregates` guard (`Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag`) must stay green. The global wave schedule is NEVER written to any `*_types.go` status field. Phase 23 writes only the `WaveSpec.WaveIndex` (a spec field on the Wave CR), which is not in the denylist.
- **Leaving v1alpha1 conversion webhook alive:** Deleting `plan_conversion.go` (Hub() stub) without also removing the conversion webhook manifests from `config/webhook/manifests.yaml` and removing `SetupPlanWebhookWithManager` / `SetupWaveWebhookWithManager` would leave the binary wired to a webhook path that no longer needs a conversion handler. Clean up all four sites (Go file, webhook manifest, service YAML, kustomization references).
- **Cross-namespace dependsOn refs:** The namespace-per-project model means all CRs for a Project share one namespace. `dependsOn` entries are resolved within that namespace — no cross-namespace machinery needed or permitted.
- **Adding `pkg/dag` imports to controller package WITHOUT going through the existing pattern:** `pkg/dag` already has an import firewall (`verify-dag-imports` — no k8s.io imports). The controller already imports `pkg/dag` (confirmed in `plan_controller.go` line 54). No new pattern needed.
- **Treating the Hub() removal as version-independent:** `Hub()` on the v1alpha1 `Plan` type is required for controller-runtime's multi-version webhook machinery. Removing it WITHOUT removing v1alpha1 from the scheme registration would cause a compile or runtime panic. The correct order is: remove Hub(), also remove v1alpha1 webhook path registrations, re-run make generate.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| deepcopy methods for v1alpha2 types | Manual `DeepCopy` / `DeepCopyObject` | `make generate` (controller-gen) | Controller-gen emits correct, test-covered deepcopy from `+kubebuilder:object:generate=true` marker |
| CRD YAML with served/storage version flags | Manually editing `config/crd/bases/*.yaml` | `make manifests` (controller-gen) | Marker-driven; manual edits are overwritten on next `make manifests` run |
| Cycle detection algorithm | Custom graph traversal | `pkg/dag.ComputeWaves` + `CycleError` | Already in tree, import-firewalled, tested, deterministic |
| Status condition API | Custom condition fields | `k8s.io/apimachinery/pkg/api/meta.SetStatusCondition` + `metav1.Condition` | K8s convention; already used uniformly across all six Kinds |
| Old-object detection via annotation inspection | Scanning annotation history | Checking presence/value of a v1alpha2-required field | Annotations are mutable; field presence is structural |

**Key insight:** Every piece of tooling for this phase already exists in the repo. Phase 23 is configuration and schema surgery, not new algorithm work.

---

## Runtime State Inventory

> Phase 23 is a CRD schema migration for a pre-GA system with only dogfood clusters. Included because it involves a breaking change to persisted objects.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | Dogfood cluster `kind-tide-dogfood` has Project/Milestone/Phase/Plan/Task/Wave CRs persisted under v1alpha1 storage version | Reinstall: delete namespace or delete all CRs, then re-apply under v1alpha2. No data-migration tooling needed (pre-GA, dogfood only). |
| Live service config | `kind-tide-dogfood` has tide-system namespace with tide-manager deployment + conversion webhook service | After CRD upgrade: delete old webhook service + ValidatingWebhookConfiguration entries for plan/wave webhooks; redeploy with updated manager binary |
| OS-registered state | None — no OS-level registrations for dogfood cluster CRDs | None |
| Secrets/env vars | `~/.tide/anthropic.key` referenced for tide-secrets; key is external to CRD schema and unaffected by schema rename | None |
| Build artifacts | `config/crd/bases/*.yaml` will be regenerated by `make manifests` after schema change | Run `make manifests generate` as part of plan wave 1 |

**Nothing found in category "OS-registered state"** — verified by inspection (no cron jobs, launchd plists, or systemd units embed CRD version strings).

---

## Common Pitfalls

### Pitfall 1: Leaving StorageVersion on v1alpha1 Plan While Adding v1alpha2

**What goes wrong:** If `+kubebuilder:storageversion` stays on `api/v1alpha1/plan_types.go` AND is added to `api/v1alpha2/plan_types.go`, controller-gen will emit an error or produce an invalid CRD (only one version may be the storage version per kind).

**Why it happens:** v1alpha1 `plan_types.go` line 71 already has `+kubebuilder:storageversion`. It is the only Kind currently marked as storage version. It must be REMOVED from v1alpha1 when added to v1alpha2.

**How to avoid:** In the same commit that adds `+kubebuilder:storageversion` to each v1alpha2 type, remove it from the corresponding v1alpha1 type. Never have both markers simultaneously.

**Warning signs:** `make manifests` exits with an error about multiple storage versions for the same Kind. [VERIFIED: codebase read — plan_types.go:71]

### Pitfall 2: Webhook Manifests Referencing Removed v1alpha1 Paths

**What goes wrong:** `config/webhook/manifests.yaml` and the webhook service continue to reference v1alpha1 resources and admission webhook paths. After removing the v1alpha1 webhook Go code, the binary no longer serves those paths, causing the admission webhook to return connection-refused, which (with `failurePolicy=fail`) blocks all Plan and Wave Creates.

**Why it happens:** The removal of `SetupPlanWebhookWithManager` and `SetupWaveWebhookWithManager` from `cmd/manager/main.go` removes the HTTP handler registration, but the `ValidatingWebhookConfiguration` resource in the cluster still routes Plan/Wave admission requests to the now-absent path.

**How to avoid:** In the same plan wave as the Go code removal, delete or update the webhook manifests. Verify with `kubectl get validatingwebhookconfigurations` that no stale webhook points to the manager. [VERIFIED: codebase read — webhook/manifests.yaml exists]

### Pitfall 3: verify-no-aggregates Grep Targets Only v1alpha1

**What goes wrong:** The Makefile target `verify-no-aggregates` greps `api/v1alpha1/*_types.go` (line 529 confirmed). If v1alpha2 types introduce any `Schedule`, `Waves []`, `IndegreeMap`, etc., the guard won't catch it.

**Why it happens:** The grep path is hardcoded to `api/v1alpha1/`. Phase 23 adds `api/v1alpha2/` but the guard doesn't cover it.

**How to avoid:** Expand the grep to `api/**/*_types.go` or add `api/v1alpha2/*_types.go` to the target. Best done in the same wave as the v1alpha2 type introduction. [VERIFIED: Makefile line 529 — `grep -nE '...' api/v1alpha1/*_types.go`]

### Pitfall 4: WaveController Uses Spec.PlanRef for Task Listing

**What goes wrong:** `wave_controller.go` line 152 does `client.MatchingFields{taskPlanRefIndexKey: wave.Spec.PlanRef}` to list Tasks that belong to a Wave. After `WaveSpec.PlanRef` is renamed to `WaveSpec.ProjectRef`, this line fails to compile.

**Why it happens:** WaveController's Task-listing strategy assumes Wave ↔ Plan is the association. In v1alpha2, Wave is owned by Project; Tasks are not directly indexed by Project in the wave controller (the global dispatch is Phase 24's concern).

**How to avoid:** Phase 23 must stub or update `wave_controller.go` to compile against v1alpha2 WaveSpec. The Phase-23 stub can list Tasks by `wave.Spec.ProjectRef` with a new `taskProjectRefIndexKey` field index, or the Task-listing logic can be temporarily removed/commented and replaced in Phase 24. The plan must explicitly address this. [VERIFIED: wave_controller.go line 152]

### Pitfall 5: Hub() Removal Without Scheme Deregistration

**What goes wrong:** `plan_conversion.go` defines `func (*Plan) Hub() {}`. Removing this file without also removing `tideprojectv1alpha1.AddToScheme(scheme)` from `cmd/manager/main.go` is fine — the Hub marker is only needed by conversion webhook machinery. However, if both `AddToScheme` calls (v1alpha1 AND v1alpha2) are kept, the scheme will have two versions of the same Kind registered, which IS correct for kubebuilder multi-version. The pitfall is removing v1alpha1 from the scheme prematurely when old objects might still be in etcd. For D-09 (clean break / reinstall), removing v1alpha1 from the scheme is the correct end state — but it must happen AFTER the cluster is confirmed clean.

**How to avoid:** Migration ordering: (1) upgrade CRD to v1alpha2-only served+stored, (2) reinstall (delete all old CRs, re-apply with v1alpha2 shape), (3) then remove v1alpha1 AddToScheme from main.go. In practice for a pre-GA dogfood cluster, steps can be collapsed into one — the plan just needs to order them correctly in the wave structure.

### Pitfall 6: TaskSpec.DependsOn Broadening Breaks Existing Plan Webhook Cycle Detection

**What goes wrong:** The existing `plan_webhook.go::validate` builds the DAG from Tasks in a single Plan. With cross-scope `dependsOn` entries (a Task pointing to a Task in another Plan), `pkg/dag.ComputeWaves` will return `fmt.Errorf("edge references unknown node: %s", e.From)` — a non-cycle error — when the `From` node name is not in the per-Plan `nodes` set.

**Why it happens:** `ComputeWaves` validates that edge endpoints are in the nodes set. The per-Plan webhook only passes `nodes = {task.Name for tasks in THIS plan}`. A cross-scope dep on a task in another plan will be an unknown node.

**How to avoid:** The Phase-23 plan webhook for v1alpha2 must either (a) skip validation of cross-scope deps (return a warning, not a rejection) or (b) assemble the full project-scope dep set. Option (a) is correct for Phase 23: the per-Plan cycle gate validates within-plan deps only; the global gate (Project reconciler, DEPS-03) covers cross-scope cycles. The webhook must strip cross-scope dep names from the per-Plan DAG before calling `ComputeWaves`. [VERIFIED: plan_webhook.go lines 150–165, ComputeWaves line 56–61 in kahn.go]

---

## Code Examples

### v1alpha2 WaveSpec (SCHEMA-01)

```go
// api/v1alpha2/wave_types.go
// +kubebuilder:object:generate=true
// +groupName=tideproject.k8s
package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// WaveSpec carries the global-scope wave identity per D-07.
type WaveSpec struct {
    // ProjectRef is the name of the owning Project (same namespace).
    // +kubebuilder:validation:MinLength=1
    ProjectRef string `json:"projectRef"`

    // WaveIndex is the global monotonic 0-indexed wave position derived by
    // pkg/dag.ComputeWaves over the entire Project's task DAG. Never cached
    // in status (PERSIST-03 / verify-no-aggregates).
    // +kubebuilder:validation:Minimum=0
    WaveIndex int `json:"waveIndex"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Index",type=integer,JSONPath=".spec.waveIndex"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type Wave struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitzero"`
    Spec              WaveSpec   `json:"spec"`
    Status            WaveStatus `json:"status,omitzero"`
}
```

### v1alpha2 TaskSpec.DependsOn Broadening (DEPS-01)

```go
// api/v1alpha2/task_types.go
type TaskSpec struct {
    // PlanRef is the name of the owning Plan (same namespace). Used for
    // ownership and Task listing; NOT a dep constraint.
    // +kubebuilder:validation:MinLength=1
    PlanRef string `json:"planRef"`

    // DependsOn lists the names of Tasks (in any Plan/Phase/Milestone of
    // this Project) that must complete before this Task may dispatch.
    // D-F1 (plan-local restriction) is retired — entries may target Tasks
    // in sibling Plans, sibling Phases, or sibling Milestones.
    // Resolved in-memory at assembly time (D-05); coarse scope refs
    // (naming a Plan/Phase/Milestone rather than a Task) are fan-out
    // expanded by the assembler (Phase 24, DEPS-02).
    // +optional
    // +kubebuilder:validation:XValidation:rule="!self.exists(d, d == '')",message="dependsOn entries must not be empty strings"
    DependsOn []string `json:"dependsOn,omitempty"`

    // ... rest of TaskSpec fields unchanged from v1alpha1 ...
}
```

### v1alpha2 PlanSpec.DependsOn Addition (DEPS-02)

```go
// api/v1alpha2/plan_types.go
type PlanSpec struct {
    // PhaseRef is the name of the owning Phase (same namespace).
    // +kubebuilder:validation:MinLength=1
    PhaseRef string `json:"phaseRef"`

    // DependsOn lists hierarchy nodes (Task/Plan/Phase/Milestone names)
    // in this Project that this Plan's Tasks implicitly depend on.
    // Used at assembly time (Phase 24) for fan-out: a dep on a Plan
    // means "all Tasks in that Plan must complete before any Task in
    // this Plan may dispatch." Progressive refinement (D-03) may
    // narrow this to a specific Task-level dep as planning descends.
    // +optional
    DependsOn []string `json:"dependsOn,omitempty"`

    // SharedContext unchanged from v1alpha1
    // +optional
    SharedContext string `json:"sharedContext,omitempty"`
}
```

### Old-Object Fail-Closed Guard (D-09)

```go
// internal/controller/project_controller.go — head of Reconcile, after Get
const (
    ConditionRequiresReinstall = "RequiresReinstall"
    ReasonRequiresReinstall    = "RequiresReinstall"
)

func isV2Shape(project *tidev1alpha2.Project) bool {
    // v1alpha2 ProjectSpec has no required flag field beyond TargetRepo.
    // Use the CRD admission schema to reject old objects at apply time.
    // The reconciler guard is belt-and-suspenders: if an old etcd object
    // slips through, TargetRepo being empty (v1alpha1 schema may not have
    // had it required) is a signal. Alternatively use a new required
    // v1alpha2-only field.
    //
    // Simplest: check for a CEL-validated marker field added only to v1alpha2.
    return project.Spec.TargetRepo != "" // TargetRepo is required in v1alpha1 too
    // → Better: add a new required field `APIVersion string` defaulted to "v1alpha2"
    //   by a mutating webhook; its absence signals old-shape. Planner decides.
}
```

NOTE: The exact old-object detection strategy (required field vs. annotation check) is marked Claude's Discretion. The planner should choose a concrete discriminator field.

### Global Cycle Gate (DEPS-03)

```go
// internal/controller/project_controller.go
func (r *ProjectReconciler) assembleProjectDepGraph(
    ctx context.Context,
    project *tidev1alpha2.Project,
) ([]dag.NodeID, []dag.Edge, error) {
    // List all Tasks in the project namespace with project label.
    var taskList tidev1alpha2.TaskList
    if err := r.List(ctx, &taskList,
        client.InNamespace(project.Namespace),
        client.MatchingLabels{"tideproject.k8s/project": project.Name},
    ); err != nil {
        return nil, nil, fmt.Errorf("list tasks: %w", err)
    }

    taskNames := make(map[string]struct{}, len(taskList.Items))
    for i := range taskList.Items {
        taskNames[taskList.Items[i].Name] = struct{}{}
    }

    nodes := make([]dag.NodeID, 0, len(taskList.Items))
    var edges []dag.Edge

    for i := range taskList.Items {
        t := &taskList.Items[i]
        nodes = append(nodes, t.Name)
        for _, dep := range t.Spec.DependsOn {
            // Phase 23: only wire edges for task-level deps (dep is a known task name).
            // Coarse scope deps (dep names a Plan/Phase/Milestone) are fan-out
            // in Phase 24; skip here to avoid "unknown node" errors from ComputeWaves.
            if _, isTask := taskNames[dep]; isTask {
                edges = append(edges, dag.Edge{From: dep, To: t.Name})
            }
        }
    }
    return nodes, edges, nil
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Per-plan wave derivation (`materializeWaves`, `tide-wave-<plan.UID>-<i>`) | Global wave derivation at Project scope (`tide-wave-<project.UID>-<N>`) | Phase 23 (schema only), Phase 24 (engine) | Wave CRs are globally comparable; metric `wave` label now carries global index |
| `Task.DependsOn` = plan-local (D-F1) | `Task.DependsOn` = any node in project namespace | Phase 23 | Unlocks cross-milestone task sequencing |
| `Plan` has no `dependsOn` | `Plan.DependsOn []string` at plan-level | Phase 23 | Enables coarse interface deps between plans at planning time |
| Conversion webhook Hub() on v1alpha1 Plan | No conversion webhook; clean break; v1alpha2-only | Phase 23 | Simpler CRD spec; no webhook overhead |
| Per-plan cycle detection (plan webhook) | Per-plan PLUS global cycle detection (Project reconciler) | Phase 23 | Catches cross-boundary cycles at validation time |

**Deprecated/outdated after Phase 23:**
- `WaveSpec.PlanRef`: replaced by `WaveSpec.ProjectRef`
- `TaskSpec.DependsOn` plan-local restriction (D-F1 comment): retired
- `+kubebuilder:storageversion` on v1alpha1 Plan: moved to v1alpha2
- `api/v1alpha1/plan_conversion.go` (Hub() stub): deleted
- `internal/webhook/v1alpha1/wave_webhook.go` `SetupWaveWebhookWithManager`: removed from main.go (the webhook itself may be repurposed for v1alpha2 Wave if D-B1 client-apply rejection is carried forward)
- Per-plan wave naming `tide-wave-<UID>-<i>`: replaced with `tide-wave-<project.UID>-<N>` naming

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `+kubebuilder:unservedversion` on v1alpha1 types causes `served: false` in CRD spec without requiring a conversion webhook for the `None` strategy. | Standard Stack / Pattern 1 | If wrong, k8s API server requires a conversion webhook even for unserved versions → plan must add no-op webhook or fully delete v1alpha1 type entries |
| A2 | Removing v1alpha1 from served versions and having only v1alpha2 as `served+stored` is sufficient for D-09 "clean break" — etcd objects stored under v1alpha1 storage version key will be silently inaccessible without a conversion webhook running | Standard Stack / Pattern 1 | If wrong, old etcd objects are auto-converted on read → no "fail-closed rejection" visible to the reconciler, undermining the guard |
| A3 | The `taskPlanRefIndexKey` field index on `TaskSpec.PlanRef` does NOT need to change in Phase 23 — Task ownership by Plan is preserved in v1alpha2 even as DependsOn is broadened | Architecture / Pitfall 4 | If wrong, wave controller needs a new index, and Phase 23 plan must add one |
| A4 | The `verify-no-aggregates` Makefile grep only covers `api/v1alpha1/*_types.go` — does not auto-extend to `api/v1alpha2/` | Common Pitfalls / Pitfall 3 | If wrong (i.e., glob covers both), no Makefile change needed for the guard — but confirmed single-version glob means the planner MUST add v1alpha2 to the guard |

---

## Open Questions

1. **Exact old-object discriminator for isV2Shape**
   - What we know: v1alpha1 `ProjectSpec.TargetRepo` was already `+kubebuilder:validation:MinLength=1` so it is required in both versions.
   - What's unclear: There is no field present in v1alpha2 but absent in v1alpha1 without adding one.
   - Recommendation: Add a marker field `SchemaRevision string` to v1alpha2 `ProjectSpec` with a CEL rule requiring its presence and defaulting to `"v1alpha2"` via a mutating webhook, OR rely solely on CRD admission schema (v1alpha1 objects fail CRD admission when applied after the upgrade, so the controller guard is only for objects already in etcd). The simplest position: trust the CRD admission schema as the primary gate; the controller guard is belt-and-suspenders for etcd-stranded objects.

2. **Wave webhook D-B1 client-apply rejection for v1alpha2**
   - What we know: `wave_webhook.go` enforces D-B1 (only WaveReconciler may create Waves). After the schema change, the webhook must reference v1alpha2.Wave.
   - What's unclear: Whether the webhook is repurposed (re-registered for v1alpha2) or deleted as part of the webhook cleanup sweep.
   - Recommendation: Re-register the webhook for v1alpha2 Wave (same D-B1 logic, updated import); don't delete it. D-B1 remains a valid invariant. Phase 23 plan should include this as an explicit task.

3. **Fan-out of coarse scope deps in cycle gate**
   - What we know: Phase 23's `assembleProjectDepGraph` skips coarse refs (those naming Plans/Phases/Milestones rather than Tasks). This means the cycle gate can't detect cycles involving scope-level deps until Phase 24 implements fan-out.
   - What's unclear: Whether Phase 23 should partially implement scope-level dep expansion for cycle detection.
   - Recommendation: Skip scope deps in Phase 23's cycle gate (they are conservative — adding them later can only ADD edges, never remove them, so the Phase-23 gate is never more permissive than the final Phase-24 gate). Document the limitation.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| controller-gen (make generate) | v1alpha2 deepcopy + CRD regen | check with `which controller-gen` | kubebuilder v4.14.0 bundled | `go install sigs.k8s.io/controller-tools/cmd/controller-gen` |
| Go 1.26 | Compilation | check with `go version` | 1.26.x | None (project requires Go 1.26) |
| kind-tide-dogfood cluster | Migration smoke test | `kind get clusters` | current | Use envtest for unit-level tests |
| make | Build + verify targets | standard tool | any | Run underlying grep/go commands manually |

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (integration), stdlib `testing` (unit) |
| Config file | none — `go test ./...` or `make test-int` |
| Quick run command | `go test ./api/v1alpha2/... ./internal/controller/... -count=1` |
| Full suite command | `make test-int` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SCHEMA-01 | WaveSpec.ProjectRef replaces PlanRef; WaveIndex is global | unit | `go test ./api/v1alpha2/... -run TestWaveSpec` | Wave 0 |
| SCHEMA-02 | Metric `wave` label emits global WaveIndex, not per-plan | unit | `go test ./internal/metrics/... -run TestWaveLabel` | Wave 0 |
| SCHEMA-03 | Old v1alpha1-shape object rejected with RequiresReinstall condition | envtest | `go test ./internal/controller/... -run TestOldShapeRejection` | Wave 0 |
| DEPS-01 | Task.DependsOn accepts cross-scope names; plan-local restriction absent | unit | `go test ./api/v1alpha2/... -run TestTaskDependsOn` | Wave 0 |
| DEPS-02 | PlanSpec.DependsOn field present and validates | unit | `go test ./api/v1alpha2/... -run TestPlanDependsOn` | Wave 0 |
| DEPS-03 | Cross-scope cycle detected and surfaced as ProjectStatus condition | envtest | `go test ./internal/controller/... -run TestGlobalCycleDetection` | Wave 0 |
| SCHEMA-03 | Migration doc exists with reinstall steps | manual | N/A | Wave 0 |
| (guard) | verify-no-aggregates passes on api/v1alpha2 | CI make target | `make verify-no-aggregates` (after Makefile update to include v1alpha2) | Makefile update needed |
| (guard) | verify-dag-imports passes | CI make target | `make verify-dag-imports` | No change needed |

### Sampling Rate
- **Per task commit:** `go test ./api/v1alpha2/... ./internal/controller/... -count=1`
- **Per wave merge:** `make test-int` (Ginkgo suite + go tests)
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `api/v1alpha2/` package — all type files + groupversion_info.go
- [ ] `api/v1alpha2/zz_generated.deepcopy.go` — generated
- [ ] `internal/controller/project_controller_cycle_test.go` — covers DEPS-03
- [ ] `internal/controller/project_controller_v2_guard_test.go` — covers SCHEMA-03 old-object rejection
- [ ] Update `Makefile` `verify-no-aggregates` to include `api/v1alpha2/*_types.go`

---

## Security Domain

> ASVS check: Phase 23 changes CRD schema and admission logic. No auth/session/crypto changes; input validation changes for `dependsOn` strings.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | N/A |
| V3 Session Management | no | N/A |
| V4 Access Control | no | N/A |
| V5 Input Validation | yes | CEL `x-kubernetes-validations` on `dependsOn` entries (MinLength, no empty string, no self-reference) |
| V6 Cryptography | no | N/A |

### Known Threat Patterns for CRD Schema Changes

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Malformed `dependsOn` string causing controller infinite loop | Tampering | CEL validation at admission rejects empty strings; controller cycle gate uses `pkg/dag.ComputeWaves` which is bounded O(V+E) |
| Cross-scope dep injection pointing to a non-existent node | Tampering | `pkg/dag.ComputeWaves` returns error on unknown edge endpoint; controller treats as transient error + requeue |
| Old-schema object bypassing the fail-closed guard via direct etcd write | Elevation | TerminalError prevents reconciler action; status condition remains set; operator must explicitly reinstall |

---

## Sources

### Primary (HIGH confidence)
- Codebase direct read: `api/v1alpha1/*.go`, `internal/controller/plan_controller.go`, `internal/controller/wave_controller.go`, `internal/metrics/registry.go`, `Makefile` lines 472–546, `api/v1alpha1/shared_types.go`, `pkg/dag/kahn.go`, `pkg/dag/errors.go`
- Context7 `/websites/book_kubebuilder_io` — `+kubebuilder:unservedversion`, `+kubebuilder:storageversion`, conversion webhook markers
- Context7 `/kubernetes-sigs/controller-runtime` — `reconcile.TerminalError`, `ctrl.NewWebhookManagedBy`, `meta.SetStatusCondition` pattern
- CONTEXT.md decisions D-01 through D-10 (locked)

### Secondary (MEDIUM confidence)
- [Kubebuilder book: CRD markers reference](https://book.kubebuilder.io/reference/markers/crd.html) — `+kubebuilder:unservedversion` documented
- [Kubebuilder book: migration manual process](https://book.kubebuilder.io/migration/manual-process.html) — multi-version webhook setup

### Tertiary (LOW confidence)
- WebSearch result summaries on CRD clean-break migration patterns — confirmed by kubebuilder markers reference above; not relied upon alone

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries already in go.mod; no new packages; marker mechanics confirmed via Context7
- Schema field changes: HIGH — all current fields read from codebase directly; changes are additive or replacement on confirmed existing shapes
- v1alpha2 introduction mechanics: MEDIUM — `+kubebuilder:unservedversion` is confirmed documented; exact etcd behavior for unserved versions under clean-break (A1, A2 assumptions) is LOW and flagged
- Old-object guard: MEDIUM — `reconcile.TerminalError` confirmed; discriminator field choice is open question
- Cycle rejection: HIGH — `pkg/dag.ComputeWaves` pattern already in use in plan_webhook.go; global assembly is straightforward extension
- Wave ownership change: HIGH — current code read; impact on wave_controller.go identified precisely

**Research date:** 2026-06-16
**Valid until:** 2026-07-16 (stable Go/kubebuilder stack; no fast-moving dependencies)
