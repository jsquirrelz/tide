# Phase 24: Global Wave Derivation Engine — Research

**Researched:** 2026-06-16
**Domain:** controller-runtime reconciler extension (Go + sigs.k8s.io/controller-runtime), layered Kahn topology, Kubernetes CRD label-indexed bidirectional index
**Confidence:** HIGH (all findings verified directly from codebase; no external lookups required — this is a pure internal implementation phase)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Global assembler lives in `ProjectReconciler`. It is the only global-scope reconciler, already lists every Task by project label and runs the cross-scope cycle gate. Global wave derivation is the natural next stage in the same reconcile after the cycle gate passes.
- **D-02:** Derivation runs after a planning-complete signal AND the cycle gate passes, before any dispatch. Ordering contract is locked: assemble → cycle-check → derive waves → (only then) dispatch. Progressive planning: engine tolerates being invoked while the DAG is still growing (re-derivation is idempotent and cheap).
- **D-03:** Per-plan `materializeWaves` is REMOVED, not left alongside. `PlanReconciler.materializeWaves` and `stampTaskLabels` are superseded. The per-plan path must be deleted or fully neutralized — exactly one writer of Wave CRs and one source of the `wave` metric label.
- **D-04:** Extend `assembleProjectDepGraph` from task-only edges to FULL FAN-OUT. A `dependsOn` naming a Plan/Phase/Milestone expands to fan-in over every Task in that scope (resolved via spec.planRef / spec.phaseRef / spec.milestoneRef hierarchy, same namespace). A `dependsOn` naming a Task stays a direct task→task edge.
- **D-05:** Resolution is IN-MEMORY ONLY — never written back to CRDs. Authored coarse `dependsOn` is the only persisted truth. Forced by PERSIST-03 / `verify-no-aggregates`.
- **D-06:** Coarse refs left un-refined fan out conservatively (all scope's tasks). Correct narrowing is planner-correctness responsibility — not this engine phase.
- **D-07:** One Wave CR per global wave, named `tide-wave-<project>-<globalIndex>`, owned by Project. `WaveSpec{ProjectRef, WaveIndex}` already defined in v1alpha2 (`api/v1alpha2/wave_types.go`). Owner-ref → Project with `BlockOwnerDeletion: true`.
- **D-08:** Wave-CR set is reconciled (create/update/prune stale extras) on every derivation. `tide_waves_dispatched_total` keeps exactly-once-on-Create semantics. Prune mechanic is Claude's discretion; invariant — persisted Wave set == current derivation, no orphans — is locked.
- **D-09:** Keep established label-indexed mechanism, re-pointed to global index. Per-Task scalar label `tideproject.k8s/wave-index=<N>` + `tideproject.k8s/project=<name>`. task→wave = read label; wave→tasks = label selector. NO Project-level aggregate map (verify-no-aggregates forbids it).
- **D-10:** `ProjectReconciler` watches Task add/complete; recomputes whole schedule from scratch each reconcile, O(V+E). Nothing cached.
- **D-11:** Reuse `pkg/dag.ComputeWaves` + `CycleError` unchanged. Keep `pkg/dag` k8s-free (verify-dag-imports firewall).

### Claude's Discretion

- The exact **planning-complete signal** (Project status condition vs. derived "all leaf Plans materialized" check).
- Whether to **delete vs. fully neutralize** per-plan `materializeWaves`/`stampTaskLabels` and the precise refactor mechanics.
- Wave-CR **prune mechanic** (delete extras vs. tombstone) and exact requeue/backoff on transient List failures.
- Exact **label keys/values** if any need adjusting for the global index, status-condition type/reason strings, and printer columns.
- How fan-out resolves a scope ref to its task set (which field encodes phase/plan/milestone membership) and any dedup of overlapping coarse+fine edges.
- Keeping `make verify-no-aggregates` / `verify-no-sqlite-dep` / `verify-dag-imports` green through the change.

### Deferred Ideas (OUT OF SCOPE)

- Global dispatch off one indegree map, wave-boundary failure semantics at global scope, gates-as-holds, minimal resumption — Phase 25 (DISP-01..03, RESUME-01).
- Multi-milestone drive via the Milestone DAG + cross-milestone shared waves + per-milestone gate policy + README conformance test — Phase 26 (MS-01..03, SPEC-01).
- Planner-prompt discipline for correct dependency refinement — not this engine phase.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| EXEC-01 | Orchestrator assembles ONE global Execution DAG of all Tasks across all Milestones/Phases/Plans, once project planning completes, before any execution dispatch. | `assembleProjectDepGraph` is the extension point; fan-out resolution mechanism verified (see §Fan-Out Resolution below). |
| EXEC-02 | Waves derived by layered Kahn over GLOBAL task DAG; wave indices are global (single monotonic schedule), not per-plan. | `pkg/dag.ComputeWaves` unchanged; Wave naming moves from `tide-wave-<plan.UID>-<i>` to `tide-wave-<project>-<globalIndex>`; `WaveSpec` already carries `ProjectRef + WaveIndex`. |
| EXEC-03 | Global wave index is queryable both directions — given any Task you resolve its global wave; given any wave you list its Tasks (README:54 namesake invariant). | Bidirectional index via `tideproject.k8s/wave-index` + `tideproject.k8s/project` labels already stamped by `stampTaskLabels`; re-pointed to global index. `WaveReconciler` TODO items (lines 104, 134, 236, 248) close. |
| EXEC-04 | Waves re-derive on every task add/complete in O(V+E) with no cached schedule, spanning the whole Project. | `ProjectReconciler` already watches Task via `taskToProject` mapper; re-derivation on every reconcile is the pattern. `verify-no-aggregates` guard enforced. |
</phase_requirements>

---

## Summary

Phase 24 builds the global wave derivation engine that makes the TIDE acronym's "Indexed" property real at Project scope. The schema (Phase 23) is complete: Wave CRs now carry `ProjectRef + WaveIndex` (global), every CRD level has `dependsOn []string` targeting any-level nodes, and v1alpha2 is the only served version across all controllers.

The engine consists of four concrete changes to existing code: (1) extend `assembleProjectDepGraph` to fan-out coarse scope refs (Plan/Phase/Milestone names in `dependsOn`) into task-level edges using the `spec.planRef` / `spec.phaseRef` / `spec.milestoneRef` hierarchy — in-memory, never persisted; (2) after the cycle gate passes, call `pkg/dag.ComputeWaves` on the assembled global node/edge set and reconcile Wave CRs at `tide-wave-<project>-<globalIndex>` scope owned by the Project; (3) re-stamp every Task with its global `tideproject.k8s/wave-index`, replacing the per-plan index value; (4) remove (or neutralize) `PlanReconciler.materializeWaves` + `stampTaskLabels` so there is exactly one Wave CR writer. Four Phase-24 TODOs in `WaveReconciler` must be closed to re-wire Wave→Task association off the global index.

The planning-complete trigger is discretionary but research reveals the current code has no `PlanningComplete` Project condition. The recommended signal is a derived check: "all Plans that are owned by this Project have `ValidationState == Validated`" (meaning their planner subagent has completed and child Tasks exist). Because D-02 locks progressive-planning tolerance (re-derivation mid-planning is safe and idempotent), the simplest correct approach is to run global derivation on every reconcile that passes the cycle gate — no separate `PlanningComplete` condition is needed for correctness, though one may be added for observability.

**Primary recommendation:** Extend `ProjectReconciler.assembleProjectDepGraph` to perform fan-out over coarse scope refs (using spec.planRef / spec.phaseRef / spec.milestoneRef resolution in-memory), then derive global Waves by calling `ComputeWaves` on the global node/edge set in the same reconcile after `checkGlobalCycleGate` passes. Reconcile the Wave CR set (create/prune) and re-stamp Task `wave-index` labels. Remove the per-plan `materializeWaves` path entirely. Close the four WaveReconciler TODOs for ProjectRef-scoped Task listing.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Global DAG assembly (fan-out) | `ProjectReconciler` | — | Only global-scope reconciler; already lists all Tasks by project label (D-01). |
| Layered Kahn computation | `pkg/dag.ComputeWaves` | — | Pure-Go, k8s-free, already reused by cycle gate; no changes needed (D-11). |
| Wave CR lifecycle (create/prune) | `ProjectReconciler` | — | Single writer of global Wave CRs (D-07, D-08). PlanReconciler exits this role. |
| Task wave-index label stamping | `ProjectReconciler` | — | Moves from `stampTaskLabels` in PlanReconciler; becomes global (D-09). |
| Wave→Task bidirectional query | `WaveReconciler` (label selector) | — | Re-wired off `tideproject.k8s/wave-index` + `tideproject.k8s/project` label filter (closes 4 TODOs). |
| Scope-ref→Task fan-out resolution | In-memory inside `assembleProjectDepGraph` | — | Never written back (D-05); uses spec.planRef / spec.phaseRef / spec.milestoneRef fields. |
| Planning-complete detection | `ProjectReconciler` (derived check) | — | Derived "all Plans Validated" check or run-every-reconcile approach; no separate condition needed for correctness. |

---

## Standard Stack

This phase introduces no new external dependencies. All tools are already present.

### Core (unchanged — already in use)

| Library | Purpose | Source |
|---------|---------|--------|
| `sigs.k8s.io/controller-runtime` | Reconciler, client, watches, predicates, owner refs | Already in `go.mod`; current project version |
| `pkg/dag.ComputeWaves` (internal) | Layered Kahn returning `[][]NodeID`; reused unchanged (D-11) | `pkg/dag/kahn.go` |
| `pkg/dag.CycleError` (internal) | Cycle detection shape; reused unchanged | `pkg/dag/errors.go` |
| `internal/owner.StampProjectLabel` | Stamp `tideproject.k8s/project` on child CRs | `internal/owner/label.go` |
| `internal/metrics.WavesDispatchedTotal` | Exactly-once Wave dispatch counter | `internal/metrics/registry.go` |

### Package Legitimacy Audit

> No new external packages are installed in this phase. Audit is N/A.

| Package | Registry | Disposition |
|---------|----------|-------------|
| (none new) | — | N/A — phase extends existing code only |

---

## Architecture Patterns

### System Architecture Diagram

```
ProjectReconciler.Reconcile(Project)
  │
  ├─► checkSchemaRevisionGuard          [EXISTING — fail-closed gate]
  │
  ├─► checkGlobalCycleGate              [EXISTING — conservative task-only edges; Phase 24 extends the assembler it calls]
  │       │
  │       └─► assembleProjectDepGraph   [EXTEND — fan-out coarse refs to task edges]
  │                 │
  │                 ├─ List Tasks by project label (already done)
  │                 ├─ For each Task.DependsOn entry:
  │                 │    • If name ∈ taskNames → task→task edge (existing)
  │                 │    • If name ∈ planNames → fan-out: all Tasks with spec.planRef==name (NEW)
  │                 │    • If name ∈ phaseNames → fan-out: all Tasks transitively under that Phase (NEW)
  │                 │    • If name ∈ milestoneNames → fan-out: all Tasks transitively (NEW)
  │                 └─ Return (nodes, edges)  [in-memory, never persisted]
  │
  ├─► [CYCLE GATE PASSES]
  │
  ├─► deriveGlobalWaves (NEW)           [EXEC-01, EXEC-02]
  │       │
  │       ├─ pkg/dag.ComputeWaves(nodes, edges) → [][]NodeID (global wave layers)
  │       ├─ reconcileWaveCRSet:
  │       │    for i, layer := range waves:
  │       │      waveName = fmt.Sprintf("tide-wave-%s-%d", project.Name, i)
  │       │      Get or Create Wave CR with ProjectRef=project.Name, WaveIndex=i
  │       │      Create: set ownerRef→Project, increment WavesDispatchedTotal exactly once
  │       │    prune: delete Wave CRs with index >= len(waves)
  │       └─ stampGlobalTaskLabels: patch tideproject.k8s/wave-index on each Task (EXEC-03)
  │
  └─► (Phase 25 dispatch reads global wave index from Task labels)


PlanReconciler                          [D-03: REMOVE materializeWaves + stampTaskLabels]
  └─ reconcileWaveMaterialization → returns early (no-op stub) OR removed entirely

WaveReconciler.reconcileObservational   [Close 4 Phase-24 TODOs]
  └─ List Tasks by label:
       tideproject.k8s/wave-index == wave.Spec.WaveIndex
       AND tideproject.k8s/project == wave.Spec.ProjectRef
     (already implemented at wave_controller.go:154 — just remove the TODO comment)
```

### Recommended Project Structure (changes only)

```
internal/controller/
├── project_controller.go         # Extend assembleProjectDepGraph + add deriveGlobalWaves
├── plan_controller.go            # Remove/neutralize materializeWaves + stampTaskLabels
└── wave_controller.go            # Close 4 Phase-24 TODOs (lines ~104, 134, 236, 248)
test/integration/envtest/
└── global_wave_derivation_test.go  # New: multi-plan cross-scope global wave test (Wave 0 gap)
```

---

## Open Question Resolution

Research resolved all six open questions from the CONTEXT.md. Findings below.

### OQ-1: What signals "planning complete"?

**Finding:** There is no existing `PlanningComplete` Project status condition. The project reconcile calls `reconcilePhase3Lifecycle` → `reconcileProjectPlannerDispatch` which dispatches the project-level planner Job; planner Jobs produce Milestones which in turn produce Phases, Plans, and Tasks. The planning chain is hierarchical and progressive.

**Key insight:** A Plan signals "my planner is done and Tasks exist" via `ValidationState=Validated` (set by `PlanReconciler.reconcileWaveMaterialization` at step 1019 — if validation state is not `Validated` or `FileTouchMismatch`, no wave work happens). A Plan is "leaf" when it has been validated and has no child Plans.

**Recommended signal for D-02 (Claude's Discretion):** Run global derivation on every reconcile that passes the cycle gate. D-02 explicitly permits this: "Progressive planning means the engine must tolerate being invoked while the DAG is still growing — re-derivation is idempotent and cheap, so re-running mid-planning is safe and correct." This avoids the complexity of defining and maintaining a `PlanningComplete` condition. The only concern is efficiency: listing all Tasks, Plans, Phases, and Milestones on every Project reconcile adds API server load. For Phase 24, this is acceptable — Phase 25 (dispatch) can refine with a condition-gated check if needed.

**Optional (observability only):** Add a `GlobalScheduleReady` Project condition set to `True` when at least one Wave CR exists — useful for operators to observe that derivation has run.

### OQ-2: How do membership labels work for fan-out scope resolution?

**Finding:** There is NO `tideproject.k8s/phase` or `tideproject.k8s/milestone` label on Tasks. The membership hierarchy is encoded via CRD spec fields:
- `Task.Spec.PlanRef` (string) — which Plan owns this Task
- `Plan.Spec.PhaseRef` (string) — which Phase owns this Plan
- `Phase.Spec.MilestoneRef` (string) — which Milestone owns this Phase
- `Milestone.Spec.ProjectRef` (string) — which Project owns this Milestone

There is a registered field indexer `".spec.planRef"` on Tasks (`task_controller.go:1509`), which enables `client.MatchingFields{taskPlanRefIndexKey: planName}` for listing Tasks by Plan.

**For fan-out resolution (D-04), the assembler needs:**
1. **dep names a Plan** → `r.List(ctx, &taskList, client.InNamespace(ns), client.MatchingFields{taskPlanRefIndexKey: planName})`
2. **dep names a Phase** → list all Plans where `spec.phaseRef == phaseName`, then list all Tasks for each plan (two hops). Alternatively: in-memory pass over the already-listed Task set filtered by `task.Spec.PlanRef ∈ {plans that belong to this Phase}`.
3. **dep names a Milestone** → three-hop: Milestone → Phases → Plans → Tasks. Again, in-memory filtering over the already-listed Tasks is cheaper than multiple List calls.

**Recommended approach:** In `assembleProjectDepGraph`, extend to also List all Plans, Phases, and Milestones in the namespace (one extra List per type per reconcile). Build in-memory maps:
```go
planToPhase  map[string]string  // plan.Name → plan.Spec.PhaseRef
phaseToMS    map[string]string  // phase.Name → phase.Spec.MilestoneRef
taskByPlan   map[string][]string // plan.Name → []taskName (already have from task list)
```
Fan-out of `dep` is then:
- `dep ∈ planNames` → all tasks with `spec.planRef == dep`
- `dep ∈ phaseNames` → all tasks whose `spec.planRef ∈ plans where plan.Spec.PhaseRef == dep`
- `dep ∈ milestoneNames` → all tasks whose plan's phaseRef's phase's milestoneRef == dep

This is O(V) map lookups per edge — stays O(V+E) overall. [VERIFIED: codebase — task_types.go, plan_types.go, phase_types.go, milestone_types.go]

**De-duplication:** A dep set may contain both `plan-foo` (coarse) and a specific task-in-plan-foo (fine). Fan-out of `plan-foo` covers all tasks in `plan-foo`, including the fine one. The result will have duplicate edges (fine direct edge + fan-out covering the same Task). `ComputeWaves` handles duplicate edges naturally (indegree increments per edge; duplicates just add extra increments that always resolve together). However, for clean semantics, deduplicate edges before calling `ComputeWaves` using a `map[string]struct{}` keyed on `From+To`.

### OQ-3: How is `ProjectReconciler` currently wired to watch Tasks?

**Finding (VERIFIED: project_controller.go:1572):**
```go
Watches(&tidev1alpha2.Task{}, handler.EnqueueRequestsFromMapFunc(r.taskToProject))
```
This Watch is ALREADY wired at `SetupWithManager`. The `taskToProject` mapper reads `tideproject.k8s/project` from the Task's labels and returns a reconcile request for the owning Project. This Watch was added in Phase 23 (WR-02) to re-run `checkGlobalCycleGate` when a Task's `DependsOn` changes.

**For D-10:** The Watch is already in place. Global derivation on Task add/complete is additive — the reconcile is already triggered. No new Watch setup is needed. The new `deriveGlobalWaves` function runs in the same reconcile after the cycle gate passes.

### OQ-4: Idempotency + exactly-once metric mechanics from `materializeWaves`

**Finding (VERIFIED: plan_controller.go:1339–1412):** The existing pattern:
1. `Get` existing Wave by name → if `IsNotFound`, proceed to Create.
2. On Create success → increment `WavesDispatchedTotal` exactly once.
3. On `AlreadyExists` from Create (watch-lag race) → idempotent success; do NOT increment.
4. On Get success (reconcile replay) → ensure owner ref only; do NOT increment.

**Port pattern for global derivation:**
```go
for i, layer := range globalWaves {
    waveName := fmt.Sprintf("tide-wave-%s-%d", project.Name, i)
    wave := &Wave{..., Spec: WaveSpec{ProjectRef: project.Name, WaveIndex: i}}
    var existing Wave
    if err := r.Get(ctx, key(waveName), &existing); err != nil {
        if !IsNotFound(err) { return err }
        if err := owner.EnsureOwnerRef(wave, project, r.Scheme); err != nil { return err }
        if err := r.Create(ctx, wave); err != nil {
            if !IsAlreadyExists(err) { return err }
            // AlreadyExists: idempotent; no metric increment
        } else {
            metrics.WavesDispatchedTotal.WithLabelValues(project.Name, "", "").Inc()
            // Note: phase/plan are "" for global waves — WavesDispatchedTotal has {project,phase,plan}
            // labels. For global waves, phase and plan are empty (or a sentinel "global").
        }
    } else {
        // Wave exists — ensure owner ref if needed (reconcile replay)
    }
}
```

**Metric label concern:** `WavesDispatchedTotal` has labels `{project, phase, plan}`. For global waves, `phase` and `plan` are not meaningful (waves span many plans). Options: (a) emit with `phase="global"` / `plan="global"` sentinels, or (b) leave them empty strings. Option (a) is safer (avoids empty label values — Pitfall 4 / "never emit an empty label value" per plan_controller.go:1355). The planner must choose a concrete sentinel. [ASSUMED] — the CLAUDE.md sentinel rule says prefer "unknown" but "global" is more meaningful here. The planner should pick one and document it.

**Prune mechanic:** Delete Wave CRs with WaveIndex >= len(globalWaves). Safe because Wave CRs with higher indices are stale artifacts. List all Waves for this Project (label filter: `tideproject.k8s/project == project.Name`), then delete those with `Spec.WaveIndex >= len(globalWaves)`. Deletion triggers the Wave finalizer which is bounded (finalizerCleanupTimeout). [VERIFIED: wave_controller.go:90-101 shows the bounded finalizer pattern]

**Note on `PlanReconciler.Owns(&tidev1alpha2.Wave{})` (line 1498 of plan_controller.go):** After per-plan materializeWaves is removed, `PlanReconciler` must also remove `Owns(&tidev1alpha2.Wave{})` from its `SetupWithManager`. If not removed, the reconciler will spuriously re-reconcile when the Project-owned Wave CRs are created. [VERIFIED: plan_controller.go:1498]

### OQ-5: Integration test patterns for multi-plan cross-scope fixtures

**Finding (VERIFIED: test/integration/envtest/):** Existing test helpers:
- `createSimplePlan(ctx, name)` — creates a minimal Plan with `SchemaRevision: v1alpha2`.
- `makeTask(ctx, name, planRef, dependsOn, files)` — creates a Task with the project label pre-stamped.
- `makeTaskWithWaveLabel(ctx, name, planRef, dependsOn, files, waveIndex)` — creates a Task with wave-index label set.

The `indegree_test.go` patterns show a "create Project + Plans + Tasks, wait for WaveReconciler to update Wave status" test shape. A new `global_wave_derivation_test.go` should use this same shape with Tasks spanning multiple Plans.

**The README:54 conformance test shape (for EXEC-01/02/03):**
```
Create Project with Tasks α,β,γ (plan-A), δ,ε (plan-B), ζ,η,θ (plan-C)
Set DependsOn: α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ
Expected global waves: [{α,β,γ,ζ}, {δ,η}, {ε,θ}]
Assert: wave-0 Tasks have wave-index label == "0"
Assert: wave-1 Tasks have wave-index label == "1"
Assert: wave-2 Tasks have wave-index label == "2"
Assert: Wave CRs tide-wave-<project>-0/1/2 exist with correct WaveIndex
```

### OQ-6: `verify-no-aggregates` — what exactly does it forbid?

**Finding (VERIFIED: Makefile:527-535):**
```bash
grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag' api/v1alpha1/*_types.go api/v1alpha2/*_types.go
```
The grep patterns are: `Schedule`, `Waves []`, `IndegreeMap`, `CachedDag`, `DerivedDag`.

**What is permitted by this guard:**
- `WaveSpec.WaveIndex int` (scalar integer) — OK, not an aggregate.
- Per-Task scalar label `tideproject.k8s/wave-index` — label, not an api type field.
- `WaveStatus.TaskRefs []string` — already exists; it's an observation, not a derived schedule (identical names as `Schedule` patterns would catch `Waves []WaveSpec` but NOT `TaskRefs []string`).

**What is forbidden:**
- Any field named `Schedule`, `Waves []` (slice of wave objects), `IndegreeMap`, `CachedDag`, `DerivedDag` in `api/v1alpha1/*_types.go` or `api/v1alpha2/*_types.go`.

**Phase 24 impact:** Zero. The engine does not add any such fields. The global index is a Wave CR spec field (single scalar `WaveIndex int`) + per-Task label. Both pass the guard. [VERIFIED: Makefile:527-535]

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Layered topological sort | Custom Kahn | `pkg/dag.ComputeWaves` | Already correct, tested, O(V+E), deterministic (lexicographic sort), cycle-detects for free. |
| Owner-ref lifecycle | Manual GVK lookup | `owner.EnsureOwnerRef` + `controllerutil.SetControllerReference` (same package) | Established cascade pattern with BlockOwnerDeletion. |
| Wave→Task label query | Indexer + custom filter | `client.MatchingLabels{"tideproject.k8s/wave-index": idx, owner.LabelProject: proj}` | Already works in `wave_controller.go:154-158` (verified). |
| Cycle detection | Custom DFS | Reuse `checkGlobalCycleGate` / `CycleError` | Already integrated into ProjectReconciler; Phase 24 just extends the assembler it calls. |
| Idempotent Wave creation | Conditional logic | `IsAlreadyExists(err)` == success pattern (already in `materializeWaves`) | Established pattern; prevents metric double-count. |
| Task label patching | Update vs Patch | `client.MergeFrom(t.DeepCopy())` + `r.Patch(ctx, t, patch)` | Established pattern in `stampTaskLabels`; avoids version conflicts. |

---

## Common Pitfalls

### Pitfall 1: Leaving `PlanReconciler.Owns(&Wave{})` after removing `materializeWaves`

**What goes wrong:** `PlanReconciler` currently declares `Owns(&tidev1alpha2.Wave{})` in `SetupWithManager` (line 1498). If `materializeWaves` is removed but this `Owns()` is not, the PlanReconciler will re-reconcile on every Wave create/update event triggered by the ProjectReconciler, causing spurious reconciles and potentially triggering Wave GC (controller-owned resources can be garbage-collected when their owner is deleted, and the Plan owns nothing — but the real risk is enqueuing Plan reconciles for every global Wave operation).
**How to avoid:** Remove `Owns(&tidev1alpha2.Wave{})` from `PlanReconciler.SetupWithManager` in the same plan that removes `materializeWaves`.
**Warning signs:** Unexpected PlanReconciler log entries when Project-owned Waves are created.

### Pitfall 2: Fan-out edge de-duplication before `ComputeWaves`

**What goes wrong:** If a `dependsOn` has both `plan-A` (coarse) and `task-specific-in-plan-A` (fine), the assembler generates both a direct edge AND a fan-out edge from `plan-A` that covers `task-specific-in-plan-A`. `ComputeWaves` does NOT deduplicate edges — it increments indegree for each edge, meaning the same `From→To` pair appearing twice would increment indegree by 2 and prevent the target from reaching indegree=0 unless BOTH predecessor sides are subtracted. This is incorrect.
**How to avoid:** Deduplicate edges before calling `ComputeWaves`: use a `map[from+to]struct{}` set. Only include each `(From, To)` pair once.
**Warning signs:** Tasks that should be in wave 0 are stuck at indegree > 0 despite having no real dependencies.

### Pitfall 3: WavesDispatchedTotal with empty phase/plan labels

**What goes wrong:** `WavesDispatchedTotal` has label arity `{project, phase, plan}`. For global waves, phase and plan are not meaningful. Emitting empty label values triggers "never emit an empty label value" (Pitfall 4 / plan_controller.go:1355 commentary). Cardinality analyzer enforcement may catch it.
**How to avoid:** Use a non-empty sentinel for phase and plan labels when counting global waves. Options: `"global"` or derive the count from the per-plan label path as `""` with a known-safe exception. Choose once and document. The cardinality analyzer (`metriccardinality` custom analyzer) enforces the `task` label is forbidden; it does not forbid non-empty values for `phase`/`plan`. [ASSUMED - the cardinality analyzer behavior for empty labels is inferred from code commentary, not verified by direct inspection of the analyzer source]
**Warning signs:** Missing metric samples in Prometheus; cardinality analyzer lint failures.

### Pitfall 4: Scope-ref resolution depends on field indexers that may not be registered in test environments

**What goes wrong:** The fan-out assembler will need to `r.List(ctx, &taskList, client.MatchingFields{taskPlanRefIndexKey: planName})`. The `taskPlanRefIndexKey` field indexer is registered in `TaskReconciler.SetupWithManager`, which runs only when the full manager is started. In unit tests or envtest suites that register controllers selectively, the indexer may be absent, causing List to fail silently (returns empty results instead of error).
**How to avoid:** Either (a) register the field indexer from the `ProjectReconciler.SetupWithManager` as well (both reconcilers can register the same indexer — controller-runtime deduplicates on the same key+type), or (b) use label-based listing as fallback for the fan-out resolution, or (c) ensure integration tests register TaskReconciler. [ASSUMED - deduplication behavior of registering the same field indexer from two controllers. Verify against controller-runtime source if this is the chosen approach]
**Warning signs:** Fan-out silently returns zero tasks for a plan dep; waves are erroneously derived with fewer tasks than expected.

### Pitfall 5: Wave prune race — list-then-delete with concurrent ProjectReconciler

**What goes wrong:** When the global derivation runs and pruning reduces the Wave count (e.g., from 3 to 2), the prune step deletes wave-index-2. If the ProjectReconciler is re-triggered between derivation and prune (e.g., a Task label stamp triggers a Task watch event), the new reconcile may re-create wave-index-2 momentarily, then the original prune deletes it, then re-derivation re-creates it — causing flapping.
**How to avoid:** The idempotency of the whole derivation loop prevents permanent damage (next reconcile produces the correct Wave set). The flap is bounded. To minimize it: complete all Wave CR mutations (create + prune) before returning from the reconcile function (atomic in terms of the reconcile cycle). The controller-runtime WorkQueue deduplicates reconcile requests, so rapid re-enqueueing resolves to one reconcile pass.
**Warning signs:** Wave CRs flapping in/out in logs; `WavesDispatchedTotal` incrementing unexpectedly.

### Pitfall 6: `PlanReconciler` still Owns Waves after migration — wave GC

**What goes wrong (related to Pitfall 1):** If `PlanReconciler` still has `Owns(&Wave{})` AND the Wave's ownerRef points to Project (set by the new global derivation), controller-runtime may see a Wave owned by Project but watched by PlanReconciler and trigger unexpected behavior. More critically: if any stale per-plan Wave CRs (from before the migration) have ownerRef pointing to a deleted Plan, they will be GC'd by Kubernetes as orphaned children — this is correct behavior but may be confusing in logs.
**How to avoid:** When removing `materializeWaves`, also delete existing per-plan Wave CRs (those named `tide-wave-<plan.UID>-<i>`) or let them be GC'd when their owning Plans are deleted. Document that old Wave CRs are transitional and will GC on Plan deletion.

### Pitfall 7: `checkGlobalCycleGate` discards waves computed during cycle check

**What goes wrong:** `checkGlobalCycleGate` currently calls `assembleProjectDepGraph` + `dag.ComputeWaves` but **discards** the result ("deliberately DISCARDED — the gate only validates", line 1504-1505). Phase 24 extends `assembleProjectDepGraph` with fan-out — which means the cycle gate now calls a more expensive version (extra List calls for Plans/Phases/Milestones). After the gate passes, `deriveGlobalWaves` calls `assembleProjectDepGraph` AGAIN and runs `ComputeWaves` AGAIN, doubling the List calls.
**How to avoid:** Refactor to call `assembleProjectDepGraph` once in the ProjectReconciler, pass (nodes, edges) to both `checkGlobalCycleGate` and `deriveGlobalWaves`. The gate checks for cycles; if it passes, waves are derived from the same (nodes, edges) — no re-assembly needed. This halves the API server load per reconcile.
**Warning signs:** Double the expected number of List API calls in integration test logs.

---

## Code Examples

### Pattern 1: Extended `assembleProjectDepGraph` with fan-out

```go
// Source: internal/controller/project_controller.go (to be extended, Phase 24)
func (r *ProjectReconciler) assembleProjectDepGraph(
    ctx context.Context,
    project *tidev1alpha2.Project,
) (nodes []dag.NodeID, edges []dag.Edge, err error) {
    // 1. List all Tasks in the project namespace.
    var taskList tidev1alpha2.TaskList
    if err := r.List(ctx, &taskList,
        client.InNamespace(project.Namespace),
        client.MatchingLabels{owner.LabelProject: project.Name},
    ); err != nil {
        return nil, nil, fmt.Errorf("list tasks: %w", err)
    }

    // 2. List Plans, Phases, Milestones for scope resolution (fan-out D-04).
    var planList tidev1alpha2.PlanList
    var phaseList tidev1alpha2.PhaseList
    var msList tidev1alpha2.MilestoneList
    if err := r.List(ctx, &planList, client.InNamespace(project.Namespace)); err != nil {
        return nil, nil, fmt.Errorf("list plans: %w", err)
    }
    if err := r.List(ctx, &phaseList, client.InNamespace(project.Namespace)); err != nil {
        return nil, nil, fmt.Errorf("list phases: %w", err)
    }
    if err := r.List(ctx, &msList, client.InNamespace(project.Namespace)); err != nil {
        return nil, nil, fmt.Errorf("list milestones: %w", err)
    }

    // 3. Build in-memory resolution maps.
    taskNames := make(map[string]struct{}, len(taskList.Items))
    tasksByPlan := make(map[string][]string) // planRef → []taskName
    planToPhase := make(map[string]string)   // plan.Name → phase.Name
    phaseToMS := make(map[string]string)     // phase.Name → milestone.Name

    for i := range taskList.Items {
        t := &taskList.Items[i]
        taskNames[t.Name] = struct{}{}
        tasksByPlan[t.Spec.PlanRef] = append(tasksByPlan[t.Spec.PlanRef], t.Name)
    }
    for i := range planList.Items {
        p := &planList.Items[i]
        planToPhase[p.Name] = p.Spec.PhaseRef
    }
    for i := range phaseList.Items {
        ph := &phaseList.Items[i]
        phaseToMS[ph.Name] = ph.Spec.MilestoneRef
    }

    // 4. Resolve scope → tasks helper (in-memory fan-out).
    tasksForScope := func(scopeName string) []string {
        // Direct task match.
        if _, isTask := taskNames[scopeName]; isTask {
            return []string{scopeName}
        }
        // Plan match: all tasks with spec.planRef == scopeName.
        if tasks, ok := tasksByPlan[scopeName]; ok {
            return tasks
        }
        // Phase match: all tasks in all plans belonging to this phase.
        var result []string
        for planName, phaseName := range planToPhase {
            if phaseName == scopeName {
                result = append(result, tasksByPlan[planName]...)
            }
        }
        if len(result) > 0 {
            return result
        }
        // Milestone match: all tasks in all phases in this milestone.
        for phaseName, msName := range phaseToMS {
            if msName == scopeName {
                for planName, phaseName2 := range planToPhase {
                    if phaseName2 == phaseName {
                        result = append(result, tasksByPlan[planName]...)
                    }
                }
            }
        }
        return result // empty if unresolved — skip (conservative)
    }

    // 5. Build nodes and deduplicated edges.
    nodes = make([]dag.NodeID, 0, len(taskList.Items))
    for i := range taskList.Items {
        nodes = append(nodes, taskList.Items[i].Name)
    }

    edgeSet := make(map[string]struct{})
    for i := range taskList.Items {
        t := &taskList.Items[i]
        for _, dep := range t.Spec.DependsOn {
            for _, from := range tasksForScope(dep) {
                key := from + "→" + t.Name
                if _, dup := edgeSet[key]; !dup {
                    edgeSet[key] = struct{}{}
                    edges = append(edges, dag.Edge{From: from, To: t.Name})
                }
            }
        }
    }
    // Also fan-out plan/phase/milestone-level DependsOn declared on Plans/Phases/Milestones.
    // [See full pattern in Architecture Patterns section — Phase/Milestone level deps omitted for brevity]
    return nodes, edges, nil
}
```

**Note on fan-out for Plan-level `dependsOn`:** The above handles `Task.DependsOn` fan-out. Plans, Phases, and Milestones also carry `DependsOn []string` (added in Phase 23). The assembler must also iterate `planList.Items[i].Spec.DependsOn`, interpreting each as "all Tasks in THIS plan depend on all tasks in the referenced scope." The same `tasksForScope` helper applies — for each task in the owning Plan, add edges from every task in the referenced scope to that task. [VERIFIED: plan_types.go:40 — PlanSpec.DependsOn is present; phase_types.go:36 — PhaseSpec.DependsOn; milestone_types.go:37 — MilestoneSpec.DependsOn]

### Pattern 2: Global Wave CR reconciliation (create/prune) with idempotent metric

```go
// Source: To be added to project_controller.go (Phase 24)
func (r *ProjectReconciler) deriveAndReconcileWaves(
    ctx context.Context,
    project *tidev1alpha2.Project,
    nodes []dag.NodeID,
    edges []dag.Edge,
) error {
    globalWaves, err := dag.ComputeWaves(nodes, edges)
    if err != nil {
        // Cycle: already handled by checkGlobalCycleGate upstream.
        return fmt.Errorf("ComputeWaves: %w", err)
    }

    // Create/update Wave CRs for each layer.
    for i := range globalWaves {
        waveName := fmt.Sprintf("tide-wave-%s-%d", project.Name, i)
        wave := &tidev1alpha2.Wave{
            ObjectMeta: metav1.ObjectMeta{Name: waveName, Namespace: project.Namespace},
            Spec:       tidev1alpha2.WaveSpec{ProjectRef: project.Name, WaveIndex: i},
        }
        var existing tidev1alpha2.Wave
        if getErr := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: waveName}, &existing); getErr != nil {
            if client.IgnoreNotFound(getErr) != nil {
                return fmt.Errorf("get wave %s: %w", waveName, getErr)
            }
            // Wave does not exist — create.
            if ownerErr := owner.EnsureOwnerRef(wave, project, r.Scheme); ownerErr != nil {
                return fmt.Errorf("owner ref wave %s: %w", waveName, ownerErr)
            }
            if createErr := r.Create(ctx, wave); createErr != nil {
                if !apierrors.IsAlreadyExists(createErr) {
                    return fmt.Errorf("create wave %s: %w", waveName, createErr)
                }
                // AlreadyExists (watch-lag race): idempotent success; no metric increment.
            } else {
                // Exactly-once increment on Create success.
                tidemetrics.WavesDispatchedTotal.WithLabelValues(project.Name, "global", "global").Inc()
            }
        }
        // If Get succeeded: Wave exists; owner ref already set by Create; skip.
    }

    // Prune stale Wave CRs (wave count decreased).
    var allWaves tidev1alpha2.WaveList
    if listErr := r.List(ctx, &allWaves,
        client.InNamespace(project.Namespace),
        client.MatchingLabels{owner.LabelProject: project.Name},
    ); listErr != nil {
        return fmt.Errorf("list waves for prune: %w", listErr)
    }
    for i := range allWaves.Items {
        w := &allWaves.Items[i]
        if w.Spec.ProjectRef == project.Name && w.Spec.WaveIndex >= len(globalWaves) {
            if delErr := r.Delete(ctx, w); delErr != nil && !apierrors.IsNotFound(delErr) {
                return fmt.Errorf("prune wave %s: %w", w.Name, delErr)
            }
        }
    }

    // Stamp global wave-index labels on Tasks.
    return r.stampGlobalTaskLabels(ctx, nodes, globalWaves, project.Name)
}
```

**Source of pattern:** `materializeWaves` at `plan_controller.go:1339-1412` — directly ported with global naming and prune step added. [VERIFIED]

### Pattern 3: Global Task label stamping

```go
// Source: Adapted from plan_controller.go:1421 stampTaskLabels (Phase 24)
func (r *ProjectReconciler) stampGlobalTaskLabels(
    ctx context.Context,
    allTaskNames []dag.NodeID,
    globalWaves [][]dag.NodeID,
    projectName string,
) error {
    // Build name → global wave index map.
    taskWave := make(map[string]int, len(allTaskNames))
    for waveIdx, wave := range globalWaves {
        for _, name := range wave {
            taskWave[name] = waveIdx
        }
    }

    // Fetch and patch each Task (only if label needs updating).
    for _, taskName := range allTaskNames {
        waveIdx, ok := taskWave[taskName]
        if !ok {
            continue
        }
        waveIndexStr := fmt.Sprintf("%d", waveIdx)

        var t tidev1alpha2.Task
        if err := r.Get(ctx, client.ObjectKey{Name: taskName, Namespace: /* project ns */}, &t); err != nil {
            return fmt.Errorf("get task %s: %w", taskName, err)
        }
        if t.Labels["tideproject.k8s/wave-index"] == waveIndexStr &&
            t.Labels[owner.LabelProject] == projectName {
            continue // already correct; skip patch
        }
        patch := client.MergeFrom(t.DeepCopy())
        if t.Labels == nil {
            t.Labels = make(map[string]string)
        }
        t.Labels["tideproject.k8s/wave-index"] = waveIndexStr
        t.Labels[owner.LabelProject] = projectName
        if err := r.Patch(ctx, &t, patch); err != nil {
            return fmt.Errorf("stamp wave label on task %s: %w", taskName, err)
        }
    }
    return nil
}
```

**Note:** `stampTaskLabels` in `plan_controller.go:1421` uses the already-fetched task slice. For the global path, tasks must be fetched individually (or kept from the assembler list). The assembler already has all Task objects in `taskList.Items` — pass them through to avoid redundant Gets.

### Pattern 4: WaveReconciler Phase-24 TODOs (all four, to close)

The four TODOs in `wave_controller.go` have already been partially addressed in the current code:

**TODO at line 104 (owner ref lookup):**
```go
// TODO(phase-24): in v1alpha2 Wave carries ProjectRef not PlanRef; the per-plan
// materializeWaves stub sets owner-ref at create time. Phase 24 will re-own Wave under
// Project; this step will resolve ProjectRef → Project and set the owner ref.
```
Fix: Remove the TODO comment. The global `deriveAndReconcileWaves` sets the owner ref via `EnsureOwnerRef(wave, project, r.Scheme)` at create time. No action needed in WaveReconciler for new Waves. For completeness, add a fallback: if `wave.Spec.ProjectRef != ""` and no owner ref set, fetch the Project and set it. [VERIFIED: wave_controller.go:104]

**TODO at line 134 (Wave→Task listing for observational roll-up):**
```go
// TODO(phase-24): re-wire Wave→Task association off the global wave index;
// ProjectRef-scoped listing lands with the global scheduler (Phase 24).
```
Fix: The stub already implements the correct approach: list Tasks by `tideproject.k8s/wave-index=<WaveIndex>` AND `tideproject.k8s/project=<ProjectRef>` (wave_controller.go:154-158). With global indices, this query is now correct — no two global Waves share an index within a project. Remove the TODO comment. [VERIFIED: wave_controller.go:134-158]

**TODO at line 236 (task-to-wave mapper):**
```go
// TODO(phase-24): re-wire the mapper off the global wave index.
```
Fix: Replace the "list all Waves in namespace" approach with "read the Task's `tideproject.k8s/wave-index` label, find the Wave named `tide-wave-<projectRef>-<waveIndex>`". This is an O(1) lookup instead of listing all Waves. [VERIFIED: wave_controller.go:236-263]

**TODO at line 248 (same mapper function body):**
```go
// TODO(phase-24): associate Wave→Task via global wave index (ProjectRef-scoped).
```
Addressed by the mapper fix above. [VERIFIED: wave_controller.go:248]

---

## State of the Art

| Old Approach | Current Approach (Phase 24 target) | Changed In | Impact |
|--------------|-----------------------------------|------------|--------|
| Per-plan `tide-wave-<plan.UID>-<N>` Waves owned by Plan | Global `tide-wave-<project>-<N>` Waves owned by Project | Phase 24 | README:54 namesake invariant holds Project-wide |
| Task-only edges in `assembleProjectDepGraph` | Full fan-out of coarse scope refs (Plan/Phase/Milestone → all member Tasks) | Phase 24 | Cross-scope dependencies expressible and honored |
| Waves derived once-per-plan by PlanReconciler | Waves re-derived every reconcile by ProjectReconciler, O(V+E), no cache | Phase 24 | PERSIST-03 compliance; resumption-safe |
| Wave→Task via Plan field indexer (per-plan) | Wave→Task via `tideproject.k8s/wave-index` + `tideproject.k8s/project` labels (global) | Phase 24 (closes 4 TODOs) | Correct bidirectional index at Project scope |

**Deprecated/removed in Phase 24:**
- `PlanReconciler.materializeWaves` — superseded by `ProjectReconciler.deriveAndReconcileWaves`
- `PlanReconciler.stampTaskLabels` — superseded by `ProjectReconciler.stampGlobalTaskLabels`
- `PlanReconciler.Owns(&Wave{})` — ownership moves to ProjectReconciler

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Using `"global"` as the sentinel for `phase` and `plan` labels in `WavesDispatchedTotal` is safe and does not conflict with cardinality analyzer rules | Pitfall 3 / Code Example 2 | Cardinality analyzer lint failure; planner must choose a different sentinel |
| A2 | controller-runtime deduplicates registration of the same field indexer (`".spec.planRef"`) from two different SetupWithManager calls (TaskReconciler and ProjectReconciler) rather than panicking | Pitfall 4 | If not deduplicated, ProjectReconciler.SetupWithManager panics on startup; alternative: use in-memory-only resolution with a full List without field indexer |
| A3 | The "cardinality analyzer" (`metriccardinality`) custom analyzer (`tools/analyzers/metriccardinality/`) does not flag empty or `"global"` string label values, only the specific forbidden label `task` | Pitfall 3 | Lint failure; inspect analyzer source to confirm |

**Low-risk assumptions (training knowledge, stable Go/controller-runtime behavior):**
- `apierrors.IsAlreadyExists` correctly identifies the HTTP 409 Conflict response from the K8s API server — used extensively throughout the codebase and confirmed correct.
- `client.MergeFrom(t.DeepCopy())` + `r.Patch(ctx, &t, patch)` produces a JSON merge patch that only includes changed fields.

---

## Open Questions

1. **Where does fan-out of Plan-level / Phase-level / Milestone-level `DependsOn` fire?**
   - What we know: Tasks, Plans, Phases, and Milestones all carry `DependsOn []string` in v1alpha2. Phase 24 D-04 says fan-out applies to all `dependsOn` entries. The code example above shows fan-out for `Task.DependsOn`.
   - What's unclear: When `Plan.DependsOn = ["other-plan"]`, the semantic is "all Tasks in THIS plan depend on all Tasks in other-plan." The assembler must iterate over Plans (and Phases, Milestones) and generate cross-task edges representing those coarse-level deps.
   - Recommendation: The planner must explicitly scope the fan-out to cover all four `dependsOn` carrier types (Task / Plan / Phase / Milestone), not just `Task.DependsOn`. A missing carrier type would silently drop legitimate cross-scope dependencies.

2. **Should `checkGlobalCycleGate` be refactored to share the assembled (nodes, edges) with `deriveGlobalWaves`?**
   - What we know: Currently `checkGlobalCycleGate` assembles and discards; `deriveGlobalWaves` will re-assemble. Double assembly = double List calls per reconcile.
   - What's unclear: Whether the increased per-reconcile API overhead is acceptable (depends on Project scale — number of Tasks/Plans).
   - Recommendation: Refactor to assemble once, pass (nodes, edges) to both gate and derivation. This is a clean refactor with no semantic change. The planner should include this as a task.

3. **What is the exact Wave prune strategy when some Waves are being dispatched by Phase 25?**
   - What we know: Phase 24 delivers the engine only; Phase 25 delivers dispatch. During Phase 24, pruning is safe because no dispatch uses global Wave CRs yet.
   - What's unclear: Whether Phase 25 dispatch adds any constraint on pruning (e.g., "don't prune a Wave CR that has dispatched Tasks"). This is a Phase 25 concern, but the prune mechanic chosen in Phase 24 should be designed to be extendable.
   - Recommendation: For Phase 24, delete-on-prune is correct and safe. Flag in a code comment that Phase 25 should gate prune on Wave.Status.Phase == "Succeeded" to avoid pruning an in-flight Wave.

---

## Environment Availability

> This phase is purely internal Go code changes. No new external tools required.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | Build | Assumed present (project toolchain) | 1.26 | — |
| envtest | Integration tests | ✓ (existing test suite) | Existing version | — |
| `make test-int` | Verification gate | ✓ | — | — |
| kind cluster | kind-layer integration tests | Assumed available per project setup | — | Run envtest-only tests |

**Missing dependencies with no fallback:** None.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2 + Gomega (envtest suite) |
| Config file | `test/integration/envtest/suite_test.go` |
| Quick run command | `go test ./test/integration/envtest/... -run TestGlobalWaveDerivation -v` |
| Full suite command | `make test-int` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| EXEC-01 | ProjectReconciler assembles global DAG of Tasks from multiple Plans | integration (envtest) | `go test ./test/integration/envtest/... -run GlobalDag -v` | ❌ Wave 0 |
| EXEC-02 | Global wave indices (not per-plan) assigned; Wave CRs named `tide-wave-<project>-<N>` | integration (envtest) | `go test ./test/integration/envtest/... -run GlobalWaveIndex -v` | ❌ Wave 0 |
| EXEC-03 | README:54 bidirectional: task→wave label present; wave→tasks label selector works | integration (envtest) | `go test ./test/integration/envtest/... -run BidirectionalIndex -v` | ❌ Wave 0 |
| EXEC-04 | Adding a Task re-derives waves; no cached schedule in status | integration (envtest) | `go test ./test/integration/envtest/... -run WaveRederivation -v` | ❌ Wave 0 |
| verify guards | `make verify-no-aggregates`, `verify-dag-imports`, `verify-no-sqlite-dep` | static analysis | `make verify-no-aggregates verify-dag-imports verify-no-sqlite-dep` | ✓ (Makefile targets exist) |

### Sampling Rate

- **Per task commit:** `go test ./pkg/dag/... ./internal/controller/... -count=1 -timeout 60s`
- **Per wave merge:** `make test-int` (full envtest suite)
- **Phase gate:** `make test-int` green + `make verify-no-aggregates verify-dag-imports` green

### Wave 0 Gaps

- [ ] `test/integration/envtest/global_wave_derivation_test.go` — covers EXEC-01 through EXEC-04 using multi-plan fixtures conforming to the README worked example (tasks α…θ across Plans, assert wave-0/1/2 labels and Wave CRs).
- [ ] Shared fixtures: extend `createSimplePlan` to accept a `phaseRef` argument (or create `createSimplePhase` helper) so cross-phase tests can declare hierarchy.

---

## Security Domain

> No new authentication, authorization, or input validation surface is introduced. Phase 24 is a reconciler extension with no new CRD fields, no new webhooks, no new HTTP endpoints, and no user-controlled fan-out input beyond what Phase 23 already admitted via `dependsOn []string` with CEL constraints.

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | Partial | CEL constraints on `dependsOn` entries (MinLength=1, no empty strings) already in v1alpha2 API types; no new fields. |
| V4 Access Control | No | Single-namespace tenancy unchanged; no cross-namespace label queries added. |
| V6 Cryptography | No | No new crypto surface. |

**Threat: Adversarial `dependsOn` cycle via coarse scope ref** — e.g., Plan A has `dependsOn: [plan-B]` and Plan B has `dependsOn: [plan-A]`. Fan-out expands these to task-level edges forming a cycle. `checkGlobalCycleGate` (now running BEFORE derivation) catches this and surfaces `CycleDetected` on the Project. No dispatch occurs. [VERIFIED: project_controller.go:1493-1527]

---

## Sources

### Primary (HIGH confidence — verified directly from codebase)

- `internal/controller/project_controller.go` — `assembleProjectDepGraph` (lines 1432-1479), `checkGlobalCycleGate` (1482-1528), `taskToProject` watch mapper (1530-1543), `SetupWithManager` (1548-1577)
- `internal/controller/plan_controller.go` — `materializeWaves` (1339-1412), `stampTaskLabels` (1415-1455), `reconcileWaveMaterialization` (1009-1120), `SetupWithManager` (1488-1512)
- `internal/controller/wave_controller.go` — Phase-24 TODOs at lines 104, 134, 236, 248; `reconcileObservational` (130-230); `taskToWaveMapper` (232-263)
- `pkg/dag/kahn.go` — `ComputeWaves` function (lines 46-97); O(V+E), lexicographic sort, cycle detection
- `pkg/dag/errors.go` — `CycleError` shape
- `api/v1alpha2/wave_types.go` — `WaveSpec{ProjectRef, WaveIndex}` (global monotonic shape)
- `api/v1alpha2/task_types.go` — `TaskSpec.DependsOn []string` (any-level targets)
- `api/v1alpha2/plan_types.go` — `PlanSpec.DependsOn []string` (any-level targets) + `PlanSpec.PhaseRef`
- `api/v1alpha2/phase_types.go` — `PhaseSpec.DependsOn []string` + `PhaseSpec.MilestoneRef`
- `api/v1alpha2/milestone_types.go` — `MilestoneSpec.DependsOn []string` + `MilestoneSpec.ProjectRef`
- `api/v1alpha2/shared_types.go` — condition/reason vocabulary
- `internal/metrics/registry.go` — `WavesDispatchedTotal` label arity `{project, phase, plan}`
- `internal/owner/label.go` — `LabelProject = "tideproject.k8s/project"`, `StampProjectLabel`
- `Makefile` — `verify-no-aggregates` (lines 526-535), `verify-dag-imports` (472-480), `verify-no-sqlite-dep` (537-545)
- `test/integration/envtest/indegree_test.go` — existing multi-task test helpers (`createSimplePlan`, `makeTask`, `makeTaskWithWaveLabel`)
- `README.md` — lines 50-58 (README:54 namesake invariant), lines 162-220 (execution graph + Kahn worked example)
- `.planning/phases/24-global-wave-derivation-engine/24-CONTEXT.md` — locked decisions D-01 through D-11

### Secondary (MEDIUM confidence)

- `internal/controller/task_controller.go:1509` — `taskPlanRefIndexKey` field indexer registration (confirms `client.MatchingFields{".spec.planRef": planName}` works for Task listing by Plan)
- `internal/gates/boundary.go` — `BoundaryDetected` (planning-complete analog at Milestone level; shows pattern for "all children Succeeded" check)
- `.planning/REQUIREMENTS.md` — EXEC-01..04 ownership traceability

---

## Metadata

**Confidence breakdown:**
- Standard stack (internal only): HIGH — all verified from codebase
- Architecture patterns: HIGH — derived directly from existing code patterns in `materializeWaves` + `stampTaskLabels`
- Pitfalls: HIGH (Pitfalls 1-4, 7) / MEDIUM (Pitfalls 5-6) — Pitfall 3/4 have one ASSUMED flag on controller-runtime internal behavior
- Fan-out resolution: HIGH — spec.planRef/phaseRef/milestoneRef fields verified in all CRD types
- Planning-complete signal: MEDIUM — no existing condition for this; recommendation derived from D-02 ("tolerant of mid-planning invocation") which makes the question moot for correctness

**Research date:** 2026-06-16
**Valid until:** Stable indefinitely (no external dependencies; codebase is the source of truth)
