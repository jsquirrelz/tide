# Phase 26: Multi-Milestone Drive + Spec Conformance — Research

**Researched:** 2026-06-17
**Domain:** Kubernetes controller-runtime, Go, TIDE orchestrator (multi-milestone drive, spec conformance, dashboard frontend)
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** `project_planner.tmpl` emits ALL milestones up-front, each with `Milestone.dependsOn`, forming the Milestone DAG. Today it emits exactly one milestone (line 16 of the template).
- **D-02:** Plan-ALL-milestones-then-execute-globally. All milestones fully planned (down to Tasks) before ANY execution dispatch (Phase 24 EXEC-01). One global Execution DAG spans all milestones.
- **D-03:** `Milestone.dependsOn` is PLANNING-ORDER + gate-descent ONLY — contributes ZERO execution edges. Remove `depgraph.go §6d` (line 258); keep §6a/6b/6c. Add a README/spec note clarifying the Milestone edge is a planning-DAG edge.
- **D-04:** No new schema for MS-03. `Project.Spec.Gates` already delivers approve-every-milestone. MS-03 is a conformance test only.
- **D-05:** Milestone gate = PLANNING-time hold (not an execution-time output review). MS-03 test asserts N planning holds.
- **D-06:** SPEC-01 is an envtest with REAL CRDs (full stack), not a unit test. Apply 2-milestone hierarchy, let real reconcilers assemble + derive, assert Wave CRs / global wave-index labels equal `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`.
- **D-07:** Dashboard render of the SPEC-01 fixture REPLACES BOTH README mermaid diagrams. Global execution-DAG view (not per-plan). dagre `rankdir: LR`. Screenshots are committed image assets.
- **D-08:** OQ-3 proper fix — distinguish zero-member wave (display-marked Running) from wave with real in-flight Running tasks; add prune guard. Must keep CR-01 `PruneShrink` regression test green.
- **D-09:** WR-02 — add event predicate to `globalDependentsMapper`'s Task watch. Fire only on status-phase transitions or `dependsOn` changes.

### Claude's Discretion

- Exact `project_planner.tmpl` decomposition prompt wording.
- Whether to add a fast in-memory `deriveGlobalWaves` unit guard alongside the required envtest.
- Global execution-DAG dashboard view data source/shape (extend `ExecutionDAGView` vs new component).
- Exact README spec-text edits for Milestone-edge reinterpretation (D-03).
- Keeping all Makefile verification targets green; locked `{project,phase,plan,wave}` metric label set unchanged.

### Deferred Ideas (OUT OF SCOPE)

- Per-Milestone gate override field (`Milestone.Spec.Gates`).
- Per-scope conservative `FailureProfile` granularity.
- Execution-time milestone boundary gate ("slack tide").
- Revisiting Plan/Phase coarse fan-out (§6b/6c).
- OpenAI/Codex subagent backend + dogfood run #2.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| MS-01 | A Project drives MULTIPLE Milestones end-to-end via the Milestone DAG — planning emits a milestone DAG and all milestones' Tasks join the single global Execution DAG. | D-01: template change + idempotency guard widening; D-02: global execution pass unchanged |
| MS-02 | Cross-milestone global waves — a Task in one Milestone may share a global wave with a Task in another; cross-milestone task dependencies expressible and honored. | D-03: §6d removal is the key enabling change; §6a–6c remain for task-level cross-scope edges |
| MS-03 | Milestone-level gate policy composes across the Milestone DAG — approve-every-milestone works for N milestones; full-auto and full-supervised remain expressible. | D-04/D-05: conformance test only; no code change needed; existing gates.EvaluatePolicy + milestone_controller gate-descent already compose |
| SPEC-01 | README execution-DAG worked example encoded as executable test producing `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`; README and implementation agree; README mermaid diagrams replaced with dashboard screenshots. | D-06: envtest pattern already exists; D-07: new global execution-DAG component needed |

</phase_requirements>

---

## Summary

Phase 26 closes the v1.0.2 Spring Tide milestone by exercising the global Execution DAG (built in Phases 23–25) across multiple Milestones. The work breaks into four concrete areas:

**Area 1 — Template + idempotency (D-01, MS-01/MS-02):** `project_planner.tmpl` today emits exactly one Milestone at line 16 with a hardcoded single-child shape. The template must be rewritten to emit N Milestone child-CRDs (one per milestone in the decomposition), each with a `dependsOn` list wiring the Milestone DAG. The `project_controller.go` idempotency guard (line 961–972) currently bails out on `>= 1 child Milestone`; this must widen to "bail only if the planner Job already completed" or use `out.ChildCount` as the guard, so N > 1 milestones are created normally. The golden file and byte-count ratchet must be updated in the same commit.

**Area 2 — §6d removal (D-03, MS-02):** `depgraph.go` lines 258–283 implement Milestone-level all-to-all fan-out. Removing this block makes `Milestone.dependsOn` a planning-DAG-only edge. No call sites or tests assert §6d-specific behavior beyond the `buildGlobalEdges` function itself. The `resolveScope` function (used by §6d) is shared with §6a–6c and remains unchanged. A README spec note and REQUIREMENTS.md annotation (DEPS-02 reinterpretation) complete this item.

**Area 3 — SPEC-01 envtest + dashboard screenshots (D-06, D-07, SPEC-01):** An envtest conformance test must apply the literal README α…θ fixture as a 2-Milestone hierarchy (Milestone A: α,β,γ,δ,ε; Milestone B: ζ,η,θ), let the real reconcilers derive global Waves, and assert `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`. The cross-milestone edge `γ→η` (Task in Milestone A depending on Task in Milestone B) is the load-bearing cross-scope edge. A global execution-DAG dashboard component must be added so the SPEC-01 fixture can be screenshotted on the `kind-tide-dogfood` cluster.

**Area 4 — Carried-in debt (D-08, D-09):** The wave-aggregator prune guard (OQ-3) and the `globalDependentsMapper` event predicate (WR-02) are bounded fixes that land in one plan alongside the SPEC-01 work.

**Primary recommendation:** Structure the phase as 3 plans: Plan 1 = §6d removal + template rewrite + golden/ratchet update + README note; Plan 2 = SPEC-01 envtest (RED→GREEN); Plan 3 = MS-03 conformance test + dashboard global execution-DAG view + screenshots + OQ-3/WR-02 debt.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Multi-milestone planning emission | Project Planner subagent (template) | ProjectReconciler (child CRD materialization) | The planner template is the authoring surface; the controller materializes child CRDs from the JSON output |
| Milestone execution fan-out removal | Controller (depgraph.go) | — | Pure graph-edge concern; §6d is the only site |
| Global wave derivation (already done) | ProjectReconciler | WaveReconciler (display) | Phases 23–24 built this; Phase 26 exercises it across milestones |
| Gate policy composition (N milestones) | MilestoneReconciler | gates package | Already works; MS-03 is a conformance test |
| SPEC-01 conformance test | Envtest suite (test/integration/envtest/) | — | Real CRDs, real reconcilers; extends existing suite |
| Global execution-DAG dashboard view | Dashboard frontend (React) | Dashboard API (Go) | New component; data available via Wave CR labels |
| Wave prune guard (OQ-3) | ProjectReconciler.deriveGlobalWaves | WaveReconciler.reconcileObservational | prune at project_controller:1644; zero-member detection at wave_controller:163–184 |
| Watch predicate (WR-02) | TaskReconciler.SetupWithManager | — | Line 1746: the globalDependentsMapper Watches call needs `builder.WithPredicates(...)` |

---

## Standard Stack

No new external packages. All work is internal changes to existing Go + React code.

| Component | File / Package | Current State |
|-----------|----------------|---------------|
| project planner template | `internal/subagent/common/templates/project_planner.tmpl` | Emits exactly 1 Milestone; must emit N |
| project planner golden | `internal/eval/testdata/goldie/project_planner.golden` | Matches single-milestone template; update in same commit |
| project planner ratchet | `internal/eval/testdata/ratchets/project_planner.txt` | Single value `2193`; must update |
| depgraph fan-out | `internal/controller/depgraph.go:258–283` (§6d) | Remove |
| project controller idempotency | `internal/controller/project_controller.go:961–972` | Widen from `>=1 milestone` to N-milestone-safe guard |
| wave prune | `internal/controller/project_controller.go:1631–1657` | Add in-flight guard using WaveReconciler's new member-count field |
| wave aggregator | `internal/controller/wave_controller.go:163–184` | Add `ZeroMemberRunning` distinction |
| task watch predicate | `internal/controller/task_controller.go:1744–1746` | Add `builder.WithPredicates(statusPhaseChangedOrDependsOnChanged)` |
| SPEC-01 envtest | `test/integration/envtest/global_wave_derivation_test.go` | Extend with 2-milestone conformance test |
| MS-03 envtest | `test/integration/envtest/gates_test.go` (or new file) | New test: N planning holds compose |
| global execution-DAG component | `dashboard/web/src/components/` (new or extended) | New component consuming Wave CRs via project label |
| dashboard API — project tasks endpoint | `cmd/dashboard/api/projects.go` or new handler | New: list all Tasks for a project with waveIndex |
| README mermaid diagrams | `README.md` lines 91–159 | Replace both mermaid blocks with committed screenshots |

## Package Legitimacy Audit

No external packages are installed in this phase. All changes are in-repo code.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| (none) | — | — | — | — | — | — |

---

## Architecture Patterns

### System Architecture Diagram

```
project_planner.tmpl (N-milestone emit)
        │  writes N *.json into children/
        ▼
tide-reporter Job (reads out.json, creates Milestone CRDs via K8s API)
        │
        ▼
ProjectReconciler.reconcileProjectPlannerDispatch (idempotency guard)
        │  out.ChildCount == N  →  waits for N Milestones to appear
        ▼
MilestoneReconciler × N  ──(gates.PolicyApprove)──►  AwaitingApproval × N
        │  each approved in Milestone-DAG order
        ▼
ProjectReconciler.assembleProjectDepGraph
        │  buildScopeResolver + buildGlobalEdges (§6a/6b/6c only, §6d removed)
        │  Task cross-scope edges cross milestone boundaries (γ→η)
        ▼
ProjectReconciler.deriveGlobalWaves
        │  layered Kahn → [{α,β,γ,ζ}, {δ,η}, {ε,θ}]
        │  creates Wave CRs: tide-wave-<project>-0/1/2
        │  stamps tideproject.k8s/wave-index labels on Tasks
        ▼
WaveReconciler.reconcileObservational
        │  aggregates Task statuses → Wave.Status.Phase
        │  [OQ-3 fix] zero-member vs real-in-flight distinction
        ▼
Dashboard API: GET /api/v1/projects/{name}/execution-dag (new)
        │  lists Tasks by owner.LabelProject + waveIndex from Wave.Status.TaskRefs
        ▼
GlobalExecutionDAGView (new React component)
        │  dagre LR layout, wave bands, cross-wave smoothstep edges
        ▼
Screenshots committed → README mermaid diagrams replaced
```

### Recommended Project Structure

No structural changes. New files:
```
test/integration/envtest/
├── spec_conformance_test.go    # SPEC-01 + MS-03 conformance tests (new)
dashboard/web/src/components/
├── GlobalExecutionDAGView.tsx  # New global execution-DAG component (D-07)
cmd/dashboard/api/
├── execution_dag.go            # New handler for GET /api/v1/projects/{name}/execution-dag
docs/screenshots/               # Committed screenshots replacing README mermaid
├── planning-dag.png
└── execution-dag.png
```

---

## Key Facts per Decision

### D-01: project_planner.tmpl — emit N milestones

**Current template (lines 1–52), key constraints:**

1. Line 16: `"The project planner's sole structural output is exactly one Milestone child-CRD."` — this sentence must become `"emit one Milestone child-CRD **per milestone** in the project's decomposition."` [VERIFIED: read file]
2. Line 23: The HOW-TO-EMIT block specifies JSON shape: `{"kind": "Milestone", "name": "milestone-01-<slug>", "spec": {"projectRef": "<this project's name>"}}`. For multi-milestone, the spec must include `"dependsOn": ["<earlier-milestone-name>"]` for non-root milestones. `MilestoneSpec.DependsOn` is `[]string json:"dependsOn,omitempty"` at `api/v1alpha2/milestone_types.go:38`. [VERIFIED: read file]
3. The template must instruct the planner to write **multiple** JSON files (one per milestone) rather than exactly one. The controller reads every `*.json` in `children/` — so N files → N Milestones. [VERIFIED: template comment line 18–19, project_controller.go reporter path]
4. For N-milestone templates, the Opus 4.x literal-instruction guidance (CLAUDE.md) requires explicit scope: "emit one Milestone child-CRD **per milestone in the DAG**, each with its `dependsOn`" — state this broadly, not just for the first item.
5. The `{{/* WHY: two-artifact contract */ -}}` comment specifies "both must be produced" (Markdown + JSON). For N milestones, the contract becomes: one MILESTONE.md per milestone OR one combined MILESTONES.md + N JSON files. Planner discretion; the JSON files are machine-required.

**Golden + ratchet update:**
- `internal/eval/testdata/goldie/project_planner.golden`: the golden matches the current template text verbatim. After the template change, re-run `go test ./internal/eval/... -update` (or equivalent) to regenerate. [VERIFIED: read file]
- `internal/eval/testdata/ratchets/project_planner.txt`: contains `2193` (byte count). After the template grows, update this number. The eval test uses a frozen byte-count ratchet — any divergence (grow OR shrink) fails, forcing an intentional update. [VERIFIED: read file]

**Idempotency guard (project_controller.go:961–972):** [VERIFIED: read file]

```go
// Current guard (lines 961–972): bail if any owned Milestone exists
var existingMilestones tidev1alpha2.MilestoneList
if lErr := r.List(...); lErr != nil { ... }
for i := range existingMilestones.Items {
    if metav1.IsControlledBy(&existingMilestones.Items[i], project) {
        return ctrl.Result{}, nil  // ← bails immediately on first milestone
    }
}
```

This guard must change so it bails only if `out.ChildCount` milestones already exist (the planner has fully completed), not on the first milestone seen. The `handleProjectJobCompletion` path already gates on `out.ChildCount` for the reporter materialization window (lines 1200–1208). The same logic applies here:

```go
// Proposed: bail only if N == expected milestones already exist
// (or if the planner Job has already fired — jobName already exists)
```

The safest approach: check whether `tide-project-<uid>-1` Job already exists as the idempotency signal (Job is created once; presence means planner already dispatched). This is equivalent to what the current single-milestone path does once the Job is created.

---

### D-03: §6d removal from depgraph.go

**Exact location:** `internal/controller/depgraph.go` lines 258–283. [VERIFIED: read file]

```go
// §6d — lines 258–283 (REMOVE THIS BLOCK):
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
            for _, to := range toTasks { addEdge(from, to) }
        }
    }
}
```

**Call sites:** `buildGlobalEdges` is called from:
1. `assembleProjectDepGraph` in `project_controller.go` (the main path)
2. Possibly tests. [VERIFIED: grep shows `depgraph_test.go` contains a WR-04 test for coarse-ref collision — that test exercises §6a/6b/6c resolveScope, not §6d specifically]

**No other callers depend on §6d behavior.** The `ms` parameter is passed through `buildGlobalEdges`; removing the §6d block means milestones' `DependsOn` entries produce no execution edges. The `resolveScope` function itself still handles Milestone names as a valid scope level (for §6a–6c resolution of Task/Plan/Phase names that happen to be Milestone names) — that behavior is unchanged.

**Test impact:** Search for `m.Spec.DependsOn` or Milestone-level DependsOn in tests:
- `depgraph_test.go` tests §6a–6c (WR-04 collision test, plan-fan-out test). No test explicitly asserts §6d all-to-all Milestone fan-out behavior.
- The SPEC-01 envtest uses cross-scope task-level `dependsOn` (γ depends on ζ by task name), which routes through §6a. §6d is explicitly NOT needed for this test.

**README note required (D-03):** Edit `README.md` §"Two distinct DAGs" (≈ lines 80–85) and the Abstract visualization section to clarify that `Milestone.dependsOn` is a planning-DAG edge (authoring order + gate-descent) NOT an execution edge. The execution graph cross-milestone coupling (ζ free in Wave 1) is already depicted correctly in the mermaid — the note explains WHY ζ is not blocked by MA→MB.

---

### D-06: SPEC-01 conformance envtest

**Existing test infrastructure** (all facts verified):

- **Test package:** `test/integration/envtest/` — `package envtest_integration`
- **Suite entry point:** `TestIntegrationEnvtest` in `suite_test.go:164`
- **CRD directory:** `config/crd/bases` (three levels up from test dir)
- **Manager setup:** `newPhase2ReconcilersForTest(mgr)` in `suite_test.go:234` — registers all 6 reconcilers including `ProjectReconciler`, `MilestoneReconciler`, `WaveReconciler`, `TaskReconciler`
- **Global project variable:** `globalWaveTestProject = "global-wave-test-project"` in `global_wave_derivation_test.go:35`
- **Helper functions available:**
  - `makeGlobalWaveTask(ctx, name, planRef, dependsOn, files)` — creates Task with `owner.LabelProject` stamp
  - `createSimplePlan(ctx, name)` — creates minimal Plan
  - `createSimplePhase(ctx, name, milestoneRef)` — creates minimal Phase (defined in global_wave_derivation_test.go:39)
  - `createSimpleMilestone(ctx, name, projectRef)` — creates minimal Milestone with ProjectRef (defined at line 56)
  - `assertWaveExists(ctx, projectName, waveIdx)` — Eventually-polls for Wave CR existence (line 113)
  - `makeBoundPVC(ctx, name, ns)` — from indegree_test.go; needed for the project to reconcile

**README worked example fixture mapping:** [VERIFIED: README.md lines 163–222]

```
Tasks: α, β, γ, δ, ε, ζ, η, θ
Edges: α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ
Expected schedule: [{α,β,γ,ζ}, {δ,η}, {ε,θ}]
```

**2-Milestone decomposition for the conformance test:**
- **Milestone A** (`ms-spec-a`): Phase A.1 (Plan A.1.1: α,β; Plan A.1.2: γ), Phase A.2 (Plan A.2.1: δ,ε)
  - task-level edges: α→δ, β→δ, δ→ε
- **Milestone B** (`ms-spec-b`): Phase B.1 (Plan B.1.1: ζ; Plan B.1.2: η,θ)
  - task-level edges: γ→η (CROSS-MILESTONE: γ is in Milestone A, η in Milestone B), ζ→η, η→θ
- `ms-spec-b.Spec.DependsOn = ["ms-spec-a"]` — planning order only (D-03)

**What the test must assert:**
1. Wave 0 exists (`tide-wave-<project>-0`) with TaskRefs ∋ {α,β,γ,ζ}
2. Wave 1 exists with TaskRefs ∋ {δ,η}
3. Wave 2 exists with TaskRefs ∋ {ε,θ}
4. The cross-milestone edge γ→η is honored (η is NOT in Wave 0)
5. ζ is in Wave 0 despite Milestone B depending on Milestone A (Milestone edge is NOT an execution edge — §6d removed)

**Key constraint from existing test pattern:** After creating all CRDs, use `Eventually(...)` with a 30-second timeout and 500ms poll interval (established by `PruneShrink` test). The manager runs in a goroutine and reconciles asynchronously.

**AfterEach cleanup pattern (from global_wave_derivation_test.go:149–186):** Must clean up Waves, Tasks, Plans, Phases, Milestones, Projects, PVCs in that order.

**New SPEC-01 test file:** Create `test/integration/envtest/spec_conformance_test.go`. Use a distinct project name (e.g., `spec-conformance-project`) to avoid state collision with the `global-wave-test-project` used by existing tests.

---

### D-04/D-05: MS-03 gate conformance test

**How the gate already works** (verified by reading milestone_controller.go:596–645 and gates/policy.go):

1. `gates.EvaluatePolicy(project.Spec.Gates, "milestone")` returns `PolicyApprove` (the default from `DefaultGates()`).
2. In `handleJobCompletion` (~line 604), if `policy == PolicyApprove`, the reconciler checks `gates.CheckApprove(ms, "milestone")` (annotation `tideproject.k8s/approve-milestone: true`).
3. If not approved: calls `patchMilestoneAwaitingApproval` — sets `Status.Phase = "AwaitingApproval"`.
4. If approved: `gates.ConsumeApprove(ms, "milestone")` removes the annotation; sets `Status.Phase = "Running"` with `ConditionWaveOrLevelPaused = False/ApprovedByUser`.
5. For N milestones, each milestone independently goes through this path. The Milestone DAG (planning order via `dependsOn`) ensures Milestone B's phases are NOT authored until Milestone A is done — but the gate fires per-milestone in gate-descent.

**MS-03 conformance test:** Create a 2-milestone Project with `gates.milestone: approve`. Assert:
1. Milestone A reaches `AwaitingApproval`.
2. After annotating Milestone A with `tideproject.k8s/approve-milestone: true`, Milestone A proceeds to plan its phases.
3. Milestone B ALSO reaches `AwaitingApproval` (N holds).
4. After annotating Milestone B with `tideproject.k8s/approve-milestone: true`, Milestone B proceeds.
5. `gates.milestone: auto` — both milestones proceed without holds.

This test likely belongs in `test/integration/envtest/spec_conformance_test.go` alongside SPEC-01.

---

### D-08: OQ-3 — wave prune in-flight guard

**Root cause (verified by reading project_controller.go:1631–1657 and wave_controller.go:163–184):**

The prune loop at `project_controller.go:1644` checks `w.Spec.WaveIndex >= len(globalWaves)` and calls Delete. The problem: during re-derivation, a Wave that still has Running tasks might get pruned. The naive guard `skip if Wave.Status.Phase != "Succeeded"` BREAKS the CR-01 PruneShrink test because `wave_controller.go:183` sets `Phase = "Running"` for any wave with `len(members) == 0` (zero-member case) or with pending/running tasks. A zero-member wave (all tasks deleted) should be prunable but is currently indistinguishable from a wave with real in-flight tasks.

**PruneShrink test location:** `test/integration/envtest/global_wave_derivation_test.go:413–453`. It MUST remain green. [VERIFIED: read file]

**The fix has two parts:**

**Part 1 — WaveReconciler aggregator:**  Add a new `Wave.Status` boolean or phase value `"ZeroMembers"` to distinguish "wave exists but has zero assigned tasks" from "wave exists and has Running tasks." Alternatively, add a field `MemberCount int` to `WaveStatus`. The planner can then guard on `memberCount == 0 || phase == "Succeeded"`.

OR (simpler, no schema change): Expose the zero-member state via a distinct reason on the `Reconciling` condition, and let the prune guard check `len(wave.Status.TaskRefs) == 0 || wave.Status.Phase == "Succeeded"`.

**Part 2 — prune guard in project_controller.go:1644:**
```go
// Before deleting, check if the wave has real in-flight members
if w.Spec.WaveIndex >= len(globalWaves) {
    // Safe to prune only if: zero members OR already Succeeded
    if len(w.Status.TaskRefs) == 0 || w.Status.Phase == "Succeeded" {
        if delErr := r.Delete(ctx, w); ...
    }
    // Otherwise: wave has real Running tasks — skip prune, will be revisited
}
```

**Why `wave.Status.TaskRefs` is reliable:** The WaveReconciler populates `TaskRefs` from the label-indexed Task list (wave_controller.go:155). A zero-member wave has no Tasks matching its labels → `TaskRefs` is empty → `len(w.Status.TaskRefs) == 0` is true. A wave with real Running tasks has `TaskRefs` populated and `Phase = "Running"`. This avoids the zero-member false-positive.

**Note:** `wave.Status.TaskRefs` and `wave.Status.Phase` are written by WaveReconciler; the prune check in ProjectReconciler reads a potentially stale cached version. To be safe, the guard should use the `allWaves` list already fetched at line 1636, which is fresh from the informer cache.

---

### D-09: WR-02 — watch predicate for globalDependentsMapper

**Current wiring (verified, task_controller.go:1744–1746):**
```go
Watches(
    &tideprojectv1alpha2.Task{},
    handler.EnqueueRequestsFromMapFunc(r.globalDependentsMapper),
    // WR-02: no predicate — fires on every Task event including no-op resourceVersion bumps
),
```

**The fix:** Add `builder.WithPredicates(predicate)` to this Watches call. The predicate should pass Update events only when:
1. `Status.Phase` changed (predecessor completed/failed → dependents must re-evaluate), OR
2. `Spec.DependsOn` changed (new edge declared → possibly new dependency readiness needed).

**Pattern from existing code:** `predicate.AnnotationChangedPredicate{}` is used on the self-Watches call (line 1753). For status-phase changes, there is no built-in `StatusChangedPredicate` in controller-runtime — a custom `predicate.Funcs` or `predicate.NewPredicateFuncs` must be used.

**controller-runtime predicate pattern:**
```go
statusPhaseOrDepsChanged := predicate.Funcs{
    UpdateFunc: func(e event.UpdateEvent) bool {
        oldTask, ok1 := e.ObjectOld.(*tideprojectv1alpha2.Task)
        newTask, ok2 := e.ObjectNew.(*tideprojectv1alpha2.Task)
        if !ok1 || !ok2 {
            return true // conservative: let untyped events through
        }
        // Phase transition or DependsOn change
        return oldTask.Status.Phase != newTask.Status.Phase ||
               !slices.Equal(oldTask.Spec.DependsOn, newTask.Spec.DependsOn)
    },
    CreateFunc:  func(event.CreateEvent) bool { return true },
    DeleteFunc:  func(event.DeleteEvent) bool { return true },
    GenericFunc: func(event.GenericEvent) bool { return false },
}
```

**Import note:** `event` package is `sigs.k8s.io/controller-runtime/pkg/event`; `predicate` is `sigs.k8s.io/controller-runtime/pkg/predicate`. Both are already imported. `slices.Equal` from Go stdlib `slices` package (Go 1.21+; project uses Go 1.26 per CLAUDE.md).

**Risk of being too restrictive:** If `status.Phase` is set by the task reconciler as part of the same object update that sets other fields, the predicate must fire on the transition. The current pattern (taskController sets `Status.Phase` via `Status().Patch`) guarantees a separate status update event from spec changes, so the predicate is sound.

---

### D-07: global execution-DAG dashboard component

**Current ExecutionDAGView is per-Plan** (verified, `ExecutionDAGView.tsx`):
- Props: `planName: string, plan: ExecutionPlanData | null, onTaskClick`
- `ExecutionPlanData` shape: `{planName, tasks: ExecutionTaskData[], activeDispatchWave?}`
- `ExecutionTaskData`: `{name, status, waveIndex, attempt, dependsOn}`
- Layout: `applyDagreLayout(nodes, edges, "LR")` — already LR

**Data available for global view:**
- `GET /api/v1/projects/{name}` already returns milestones/phases/plans but NOT tasks.
- Wave CRs carry `Spec.ProjectRef` and `Spec.WaveIndex`; `Status.TaskRefs` is the task membership list.
- Tasks carry `tideproject.k8s/wave-index` and `tideproject.k8s/project` labels.
- A new API endpoint `GET /api/v1/projects/{name}/execution-dag` is needed to serve all Tasks for a project with their waveIndex, status, and dependsOn.

**New API endpoint shape** (following `plans.go` pattern):
```go
type projectExecutionDAGResponse struct {
    ProjectName string              `json:"projectName"`
    Tasks       []planTaskCard      `json:"tasks"`
    // planTaskCard reused: {name, phase, waveIndex, attempt, dependsOn}
}
```
Implementation: `List(Tasks, MatchingLabels{owner.LabelProject: projectName})` + build waveByTask map from Wave CRs (same as `plans.go:121–127`).

**GlobalExecutionDAGView component design:**
- Props: `projectName: string, project: ProjectExecutionDAGData | null, onTaskClick`
- Data type mirrors `ExecutionPlanData` but scoped to project (all tasks across all milestones).
- Reuse `ExecutionDAGViewInner` logic — the build/layout/band machinery is identical; only the data source changes.
- Option A (simpler): copy the inner logic, rename props, replace planName with projectName.
- Option B: factor `ExecutionDAGViewInner` into a generic `TaskDAGView` that accepts `{name, tasks}` and is composed by both per-plan and global views. Claude's discretion.

**Staleness / verify-dashboard-freshness:**
- After adding `GlobalExecutionDAGView.tsx` and the new API route, run `make dashboard-frontend` (rebuilds `dashboard/web/dist` then copies to `cmd/dashboard/embed/dist`).
- The `verify-dashboard-freshness` gate (`Makefile:284–290`) runs `diff -rq dashboard/web/dist cmd/dashboard/embed/dist` — must pass before merging.
- The dashboard change requires regenerating `cmd/dashboard/embed/dist` IN THE SAME COMMIT. [VERIFIED: Makefile read]

**App.tsx wiring (line 359–369):** Current: `selectedPlan ? <ExecutionDAGView .../> : <RunningWavesView .../>`. New: add a third view state or a tab in the UI for the global execution-DAG. For the screenshot deliverable, a minimal approach is a new route/tab that renders `GlobalExecutionDAGView` for the selected project.

**Screenshot workflow:**
1. Apply SPEC-01 fixture to `kind-tide-dogfood` cluster.
2. Navigate dashboard to the new global execution-DAG view.
3. Screenshot both: `PlanningDAGView` (for the planning containment graph) and `GlobalExecutionDAGView` (for the execution wave graph).
4. Commit screenshots to `docs/screenshots/` (or `README-assets/`).
5. Edit `README.md` to replace lines 91–159 (both mermaid blocks) with image references.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Wave member count in prune guard | Custom Task list scan in prune loop | `wave.Status.TaskRefs` already populated by WaveReconciler | Re-listing Tasks in the prune path adds an extra API call; TaskRefs is already maintained |
| Event predicate for status changes | Polling-based reconcile check | `predicate.Funcs{UpdateFunc: ...}` with `builder.WithPredicates` | controller-runtime predicate pattern; already used for AnnotationChangedPredicate in the same file |
| Dagre layout for new dashboard view | Hand-computed x/y positions | `applyDagreLayout(nodes, edges, "LR")` from `dashboard/web/src/lib/layout.ts` | Already used by both ExecutionDAGView and PlanningDAGView; LR is already implemented |
| JSON child-CRD parsing | Custom parser | Existing reporter Job + K8s API path | The reporter materializes children; no template-level format change needed for `dependsOn` field |

---

## Common Pitfalls

### Pitfall 1: Idempotency guard bails too early for N milestones

**What goes wrong:** The current guard at `project_controller.go:961–972` returns `ctrl.Result{}, nil` as soon as it finds any owned Milestone. For N > 1 milestones, the planner Job emits N children; the reporter creates them incrementally. If the reconciler fires between the 1st and Nth child being created, it will bail out thinking "planner already ran."

**Why it happens:** The guard was designed for single-milestone emission where one Milestone = "planner completed."

**How to avoid:** Gate on `out.ChildCount` (from the reporter's tiny-status read) rather than `>= 1 milestone`. If the planner Job doesn't exist yet (not yet dispatched), the guard is a no-op. If the Job already completed, check whether `countChildMilestones() >= out.ChildCount`.

**Warning signs:** In envtest, you'll see only M < N milestones created and no further planner dispatch.

### Pitfall 2: §6d removal breaks an existing test

**What goes wrong:** A test might directly or indirectly rely on Milestone-level all-to-all fan-out.

**How to avoid:** After removing §6d, run `make test` and `make test-int`. The `depgraph_test.go` WR-04 test exercises `buildGlobalEdges` — verify it only tests task/plan/phase resolution, not milestone fan-out. If a test breaks, the fix is to express the dependency at task level instead.

**Warning signs:** `depgraph_test.go` failures mentioning Milestone edges.

### Pitfall 3: SPEC-01 test uses same project as existing wave tests

**What goes wrong:** `globalWaveTestProject = "global-wave-test-project"` is shared by all tests in `global_wave_derivation_test.go`. Adding SPEC-01 to that file with the same project name causes state collision between test `It` blocks.

**How to avoid:** Use a distinct project name (e.g., `spec-conformance-project`) in the new test file, with its own BeforeEach/AfterEach cleanup.

### Pitfall 4: Cross-milestone edge γ→η uses task names that conflict

**What goes wrong:** The conformance fixture uses names like `sc-gamma`, `sc-eta`. The `DependsOn` entry on η must match the exact Task name of γ: `DependsOn: []string{"sc-gamma"}`. If the task names used in the fixture don't match, the edge is silently dropped (resolveScope returns empty for unresolved refs).

**How to avoid:** Keep fixture names consistent and verify `resolveScope` debug output. The `collisions` map in `scopeResolver` surfaces unexpected ambiguities.

### Pitfall 5: Wave.Status.TaskRefs may be stale in prune guard

**What goes wrong:** The prune loop reads `allWaves` from the informer cache, which may have stale `Status.TaskRefs`. A wave with in-flight tasks might show empty TaskRefs if the WaveReconciler hasn't yet reconciled.

**How to avoid:** The prune only fires during ProjectReconciler reconciliation triggered by Task/Wave events. If WaveReconciler has not run yet, the TaskRefs will be empty — which means the guard will treat the wave as zero-member and allow deletion. This is conservatively WRONG in the unlikely case where a wave was just created (before the aggregator ran). To prevent this, add a secondary check: if the wave's `CreationTimestamp` is very recent (< 5s), skip pruning it regardless of TaskRefs.

### Pitfall 6: Dashboard embed not regenerated before commit

**What goes wrong:** Adding `GlobalExecutionDAGView.tsx` changes the SPA bundle. If `make dashboard-frontend` is not run before committing, `verify-dashboard-freshness` fails in CI.

**How to avoid:** The plan must include a step to run `make dashboard-frontend` and commit the updated `cmd/dashboard/embed/dist/`. This is load-bearing per the Phase 22 FIX-01 gate.

---

## Code Examples

### Project planner template — N-milestone emission pattern

```
// Source: project_planner.tmpl (current), modified for N milestones

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
```

### §6d block — exact lines to remove (depgraph.go:258–283)

```go
// Source: internal/controller/depgraph.go:258-283
// REMOVE this entire §6d block (D-03):

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

### SPEC-01 envtest fixture structure (reference)

```go
// Source: test/integration/envtest/global_wave_derivation_test.go (existing pattern)
// New: 2-milestone hierarchy for SPEC-01

// Milestone A: α,β (Plan A.1.1), γ (Plan A.1.2), δ,ε (Plan A.2.1)
// Milestone B: ζ (Plan B.1.1), η,θ (Plan B.1.2)
// dependsOn edges (task-level only, §6d removed):
//   α→δ, β→δ (§6a: task DependsOn)
//   γ→η       (§6a: cross-milestone task DependsOn — THE load-bearing edge)
//   ζ→η       (§6a: task DependsOn)
//   δ→ε       (§6a: task DependsOn)
//   η→θ       (§6a: task DependsOn)
// ms-spec-b.DependsOn = ["ms-spec-a"]  (planning order only, NOT execution edge)

createSimpleMilestone(ctx, "ms-spec-a", "spec-conformance-project")
createSimpleMilestone(ctx, "ms-spec-b", "spec-conformance-project")
// wire ms-spec-b.DependsOn post-create via Patch (or use a helper that accepts dependsOn)

// Wave assertions:
assertWaveMembership(ctx, "spec-conformance-project", 0, []string{"sc-alpha","sc-beta","sc-gamma","sc-zeta"})
assertWaveMembership(ctx, "spec-conformance-project", 1, []string{"sc-delta","sc-eta"})
assertWaveMembership(ctx, "spec-conformance-project", 2, []string{"sc-epsilon","sc-theta"})
```

### WR-02 predicate pattern (task_controller.go SetupWithManager)

```go
// Source: internal/controller/task_controller.go:1741-1758 (existing), with WR-02 fix
// Add predicate to the globalDependentsMapper Watches call:

statusPhaseOrDepsChanged := predicate.Funcs{
    UpdateFunc: func(e event.UpdateEvent) bool {
        oldT, ok1 := e.ObjectOld.(*tideprojectv1alpha2.Task)
        newT, ok2 := e.ObjectNew.(*tideprojectv1alpha2.Task)
        if !ok1 || !ok2 {
            return true
        }
        return oldT.Status.Phase != newT.Status.Phase ||
            !slices.Equal(oldT.Spec.DependsOn, newT.Spec.DependsOn)
    },
    CreateFunc:  func(event.CreateEvent) bool { return true },
    DeleteFunc:  func(event.DeleteEvent) bool { return true },
    GenericFunc: func(event.GenericEvent) bool { return false },
}

return ctrl.NewControllerManagedBy(mgr).
    For(&tideprojectv1alpha2.Task{}).
    Owns(&batchv1.Job{}).
    Watches(
        &tideprojectv1alpha2.Task{},
        handler.EnqueueRequestsFromMapFunc(r.globalDependentsMapper),
        builder.WithPredicates(statusPhaseOrDepsChanged),  // WR-02
    ).
    // ... rest unchanged
```

### OQ-3 prune guard fix (project_controller.go:1644)

```go
// Source: internal/controller/project_controller.go:1642-1657 (current)
// Replace the naive prune with an in-flight guard:

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
            logger.Info("pruned stale global wave", ...)
        } else {
            logger.V(1).Info("skipping prune of in-flight wave", "wave", w.Name,
                "phase", w.Status.Phase, "memberCount", len(w.Status.TaskRefs))
        }
    }
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact for Phase 26 |
|--------------|------------------|--------------|---------------------|
| Per-plan Wave CRs (`tide-wave-<plan.UID>-<i>`) | Project-scoped Wave CRs (`tide-wave-<project>-<N>`) | Phase 23/24 | SPEC-01 asserts the global naming scheme |
| Plan-local `Task.dependsOn` (D-F1) | Any-level cross-scope `dependsOn` | Phase 23 | γ→η cross-milestone edge is now expressible |
| Single-milestone project planner | N-milestone planner (this phase) | Phase 26 | Core D-01 change |
| Milestone execution fan-out (§6d) | No execution fan-out at milestone level | Phase 26 (D-03) | Enables MS-02: ζ free in Wave 1 |

**Deprecated/outdated:**
- `materializeWaves` per-plan function: removed in Phase 24. Wave derivation is global.
- `tide-wave-<plan.UID>-<i>` naming: replaced by `tide-wave-<project>-<N>` since Phase 24.

---

## README Facts (for D-07 screenshot replacement)

**Both mermaid blocks are at lines 91–159** (verified by reading README.md:80–160):

1. **Planning graph mermaid** — lines 91–129 (flowchart TB, nested subgraphs MA/MB, `MA --> MB` edge)
2. **Execution graph mermaid** — lines 133–159 (flowchart LR, Wave 1/2/3 subgraphs, cross-wave edges)

Both will be replaced with committed image references. The image files need to be committed before the README edit. Suggested paths: `docs/screenshots/planning-dag.png` and `docs/screenshots/execution-dag.png` (or inline at repo root in `assets/`).

**README "two distinct DAGs" section** that needs the §6d reinterpretation note: lines 80–85 (the paragraph: "Two distinct DAGs run through this: Planning DAG — ... Execution DAG — ...") and the "Two-DAG application" section (~lines 233–239). Add a note: "The Milestone DAG governs planning order and gate-descent; a Milestone-level `dependsOn` entry is a planning edge, not an execution edge. Execution coupling across milestones is expressed via task-level `dependsOn` across milestone boundaries."

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `wave.Status.TaskRefs` is reliably populated by WaveReconciler before the prune guard runs. | OQ-3 fix | Prune guard may have a narrow race window where a new wave shows empty TaskRefs; add CreationTimestamp fence as secondary guard | 
| A2 | `go test ./internal/eval/... -update` regenerates the golden file; the ratchet is a plain byte count in `ratchets/project_planner.txt`. | D-01 | If the update mechanism differs, the ratchet update step needs a different command |
| A3 | No existing test explicitly asserts §6d Milestone fan-out behavior (i.e., `make test` passes after §6d removal). | D-03 | If a test fails, it signals an undocumented dependency on Milestone all-to-all execution edges that needs to be diagnosed |
| A4 | The `kind-tide-dogfood` cluster has the SPEC-01 fixture applied before screenshots are taken (live cluster step per D-07). | D-07 | If the cluster is unavailable, screenshots cannot be captured; plan must include cluster prep steps |
| A5 | The new dashboard API endpoint `GET /api/v1/projects/{name}/execution-dag` can be wired into the existing chi router in `cmd/dashboard/`. | D-07 | If the router setup is more constrained (e.g., handlers registered in main.go with specific wiring), the plan step must account for that |

---

## Open Questions

1. **MilestoneSpec.DependsOn patching in the SPEC-01 fixture**
   - What we know: `createSimpleMilestone` (global_wave_derivation_test.go:56) creates a Milestone with only `ProjectRef`. It does NOT accept a `dependsOn` parameter.
   - What's unclear: Should a new `createSimpleMilestoneWithDeps(ctx, name, projectRef, deps)` helper be added, or should the SPEC-01 test Patch the Milestone after creation to set DependsOn?
   - Recommendation: Add a `createSimpleMilestoneWithDeps` helper following the existing pattern; it's cleaner and reusable for MS-03.

2. **Screenshot tooling for D-07**
   - What we know: The kind-tide-dogfood cluster exists; the dashboard is deployed.
   - What's unclear: How are screenshots captured for CI reproducibility? Manual capture with browser dev tools is fine for the initial commit, but not automatable.
   - Recommendation: Screenshots are committed static assets (as accepted in D-07). The plan step is: apply fixture → open browser → capture → commit. A comment in the README near the images notes "generated with TIDE v1.0.2 on kind-tide-dogfood."

3. **Whether MS-03 gate test needs a full planner job or can use fixture-injected Milestones**
   - What we know: The existing `gates_test.go` in `test/integration/envtest/` likely tests gate flow by applying fixtures directly without dispatching a planner Job.
   - What's unclear: Does MS-03 need the planner to emit 2 milestones, or can the test create 2 Milestones manually (matching what the planner would produce)?
   - Recommendation: Create the Milestones manually (using `createSimpleMilestoneWithDeps`) — this is what all existing gate tests do. The planner emission (D-01) is tested separately by the golden/ratchet eval.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | controller code | Check `go version` | per go.mod | — |
| envtest binaries | SPEC-01 / MS-03 tests | bin/k8s/ populated by `make setup-envtest` | K8s 1.33+ | `make setup-envtest` |
| kind-tide-dogfood cluster | D-07 screenshots | Durable cluster (per MEMORY.md) | kind v0.31 | Rebuild with `kind create cluster --name tide-dogfood` |
| Node 22 + npm | dashboard-frontend build | Implied by Phase 22 CI wiring | Node 22 | — |
| `make dashboard-frontend` | verify-dashboard-freshness | Target exists in Makefile | — | — |

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (envtest); standard `testing` (unit tests) |
| Config file | `test/integration/envtest/suite_test.go` (TestIntegrationEnvtest entry point) |
| Quick run command | `make test-int-fast` |
| Full suite command | `make test-int` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| MS-01 | Project with N milestones produces N owned Milestone CRDs after planner emission | envtest (full stack) | `make test-int-fast` | ❌ Wave 0 |
| MS-02 | Cross-milestone task edge (γ→η) honored; ζ free in Wave 0 | envtest (SPEC-01 conformance) | `make test-int-fast` | ❌ Wave 0 |
| MS-03 | N milestone-level `AwaitingApproval` holds compose; approve-all works; auto passes without holds | envtest (gate flow) | `make test-int-fast` | ❌ Wave 0 |
| SPEC-01 | README α…θ fixture derives exactly `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]` Wave CRs | envtest (conformance) | `make test-int-fast` | ❌ Wave 0 |
| OQ-3 | PruneShrink regression test stays green after in-flight guard added | envtest (CR-01 regression) | `make test-int-fast` | ✅ `global_wave_derivation_test.go:413` |
| WR-02 | globalDependentsMapper fires only on phase/dependsOn changes | unit test | `go test ./internal/controller/... -run TestGlobalDependentsMapper` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test ./internal/controller/... ./internal/eval/... -count=1`
- **Per wave merge:** `make test-int-fast`
- **Phase gate:** Full `make test-int` green + `make verify-dashboard-freshness` + `make lint` before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `test/integration/envtest/spec_conformance_test.go` — covers MS-01, MS-02, MS-03, SPEC-01
- [ ] Unit test for WR-02 predicate (`task_controller_extracted_test.go` or new file)
- [ ] No framework install needed — envtest already set up

---

## Security Domain

This phase contains no new authentication, authorization, secret handling, or input validation paths. All changes are to internal controller logic, template wording, and dashboard rendering. Security domain: N/A for this phase's scope.

---

## Sources

### Primary (HIGH confidence)
- Codebase: `internal/controller/depgraph.go` — §6d exact lines 258–283 verified
- Codebase: `internal/controller/project_controller.go` — idempotency guard lines 961–972, prune lines 1631–1657, handleProjectJobCompletion lines 1100–1216 verified
- Codebase: `internal/controller/wave_controller.go` — aggregator lines 130–226, SetupWithManager lines 263–288 verified
- Codebase: `internal/controller/task_controller.go` — globalDependentsMapper lines 1497–1586, SetupWithManager lines 1706–1759 verified
- Codebase: `internal/controller/milestone_controller.go` — gate-descent lines 596–645 verified
- Codebase: `internal/gates/policy.go` + `annotation.go` — EvaluatePolicy, CheckApprove, ConsumeApprove verified
- Codebase: `internal/subagent/common/templates/project_planner.tmpl` — single-milestone emission structure verified
- Codebase: `internal/eval/testdata/goldie/project_planner.golden` + `ratchets/project_planner.txt` — byte count 2193 verified
- Codebase: `api/v1alpha2/milestone_types.go` — `DependsOn []string`, self-cycle CEL guard verified
- Codebase: `test/integration/envtest/global_wave_derivation_test.go` — PruneShrink test lines 413–453, README fixture test lines 191–216, helper functions verified
- Codebase: `test/integration/envtest/suite_test.go` — full envtest setup, `newPhase2ReconcilersForTest` verified
- Codebase: `dashboard/web/src/components/ExecutionDAGView.tsx` — per-plan data shape, `applyDagreLayout(nodes, edges, "LR")` usage verified
- Codebase: `cmd/dashboard/api/plans.go` — waveByTask construction pattern verified
- Codebase: `README.md` lines 80–222 — both mermaid diagrams (91–159) and worked example (163–222) verified
- Codebase: `Makefile` — `verify-dashboard-freshness` target lines 283–292, `dashboard-frontend` lines 278–281 verified

### Secondary (MEDIUM confidence)
- `26-CONTEXT.md` — locked decisions D-01..D-09 (authoritative user decisions)
- `.planning/REQUIREMENTS.md` — MS-01/02/03, SPEC-01 requirement descriptions
- `.planning/ROADMAP.md` — Phase 26 success criteria

---

## Metadata

**Confidence breakdown:**
- D-01 template mechanics: HIGH — template and golden read directly; exact line numbers verified
- D-03 §6d removal: HIGH — exact code block read; no test asserts §6d behavior found
- D-06 SPEC-01 envtest: HIGH — existing test infrastructure fully read; helper functions confirmed
- D-07 dashboard: MEDIUM-HIGH — ExecutionDAGView structure confirmed; new API endpoint shape inferred from plans.go pattern (no existing global endpoint verified)
- D-08 OQ-3 fix: HIGH — prune code + wave aggregator + PruneShrink test all read; fix approach verified against code
- D-09 WR-02 fix: HIGH — SetupWithManager wiring confirmed; predicate.Funcs pattern verified against existing usage in codebase
- MS-03 gate conformance: HIGH — gate-descent code fully read; DefaultGates() and EvaluatePolicy confirmed

**Research date:** 2026-06-17
**Valid until:** 2026-07-17 (stable; no fast-moving external dependencies)
