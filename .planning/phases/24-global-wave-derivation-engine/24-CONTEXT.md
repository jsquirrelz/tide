# Phase 24: Global Wave Derivation Engine - Context

**Gathered:** 2026-06-16
**Status:** Ready for planning
**Mode:** `--auto` (decisions auto-selected to the recommended option, grounded in the spec, Phase 23's locked schema decisions, and existing code; logged below for audit)

<domain>
## Phase Boundary

Build the **engine** that, once project planning completes and before any execution dispatch, assembles **ONE global Execution DAG** of every Task across all Milestones/Phases/Plans in a Project and derives a **single monotonic global wave schedule** by layered Kahn — queryable both directions (task→wave, wave→tasks) and re-derived in O(V+E) on every task add/complete with **no cached schedule**.

This phase delivers the **derivation/assembly engine only**. It consumes the v1alpha2 schema and cross-scope dependency model that Phase 23 already shipped (Wave re-owned Plan→Project, `dependsOn []string` on every level targeting any level, conservative task-only cycle gate in `ProjectReconciler`). It does **not** build global *dispatch*, failure semantics, gates, or resumption (Phase 25), nor multi-milestone drive (Phase 26).

The concrete deliverable: replace the per-plan `materializeWaves` (`tide-wave-<plan.UID>-<i>`, owned by Plan) with a **Project-scoped global derivation** that produces `tide-wave-<project>-<globalIndex>` Waves owned by the Project, and turn Phase 23's conservative task-only edge assembly into the full fan-out reconciliation (D-06) over the whole Project.

**Requirements owned by this phase (authoritative mapping in REQUIREMENTS.md):** EXEC-01, EXEC-02, EXEC-03, EXEC-04.

⚠ **ROADMAP discrepancy already noted in Phase 23:** the Phase 23 ROADMAP header line lists "Requirements: EXEC-01..04" — that is stale; EXEC-* belongs to **this** phase per the REQUIREMENTS.md table. Phase 23 owned SCHEMA-* + DEPS-*.

</domain>

<decisions>
## Implementation Decisions

> All five gray areas were auto-selected and resolved to the recommended option. Every decision is constrained by already-locked direction (PROJECT.md "no cached schedule / re-derive"; Phase 23 D-05/D-06/D-07; the README:54 namesake invariant) and by existing code — none introduces a divergent architecture. The exact mechanics (field names, watch wiring, prune semantics) remain research/planner territory and are flagged under Claude's Discretion.

### Assembler location & "planning-complete" trigger (EXEC-01)
- **D-01: The global assembler lives in `ProjectReconciler`.** It is the only global-scope reconciler, it already lists every Task by project label and already runs the cross-scope cycle gate (`assembleProjectDepGraph` + `checkGlobalCycleGate`, `internal/controller/project_controller.go`). Global wave derivation is the natural next stage in that same reconcile after the cycle gate passes. Rejected: a new dedicated "scheduler" controller (duplicates the Task listing + cycle assembly the Project reconciler already owns; adds a second writer to Wave CRs).
- **D-02: Derivation runs after a planning-complete signal AND the cycle gate passes, before any dispatch.** EXEC-01 is explicit about ordering ("once project planning completes, before any execution dispatch"). The *exact* planning-complete signal is Claude's discretion (candidates: a Project status condition such as `PlanningComplete`, or "all expected leaf Plans materialized and no Plan/Phase/Milestone still authoring"), but the **ordering contract is locked**: assemble → cycle-check → derive waves → (only then) dispatch. Progressive planning means the engine must tolerate being invoked while the DAG is still growing — see D-09 (re-derivation is idempotent and cheap, so re-running mid-planning is safe and correct, it just produces an interim schedule).
- **D-03: Per-plan `materializeWaves` is removed, not left alongside.** `PlanReconciler.materializeWaves` (`tide-wave-<plan.UID>-<i>`, Plan-owned) and its per-plan `stampTaskLabels` global-index source are superseded. The per-plan path must be deleted (or fully neutralized) so there is exactly one writer of Wave CRs and one source of the `wave` metric label — leaving both live would re-create the per-plan-waves defect this milestone exists to fix.

### Coarse-ref fan-out resolution (EXEC-01/EXEC-02, completes Phase 23 D-06)
- **D-04: Extend `assembleProjectDepGraph` from task-only edges to full fan-out.** Phase 23 deliberately emitted only task→task edges and *skipped* coarse scope refs (`project_controller.go:1465-1478`, "RESEARCH OQ#3 — Phase 24 fan-out will add those edges"). This phase implements that fan-out: a `dependsOn` entry naming a **Plan/Phase/Milestone** expands to **fan-in over every Task in that scope** (resolved via the project/phase/plan/milestone labels already stamped on Tasks, same namespace). A `dependsOn` naming a **Task** stays a direct task→task edge. This collapses DEPS-01 + DEPS-02 into one assembly mechanism exactly as Phase 23 D-06 specified.
- **D-05: Resolution is in-memory only — never written back to CRDs.** This locks the item Phase 23 D-05 deferred to "the Phase 24 engine decision." Authored coarse `dependsOn` is the only persisted truth; resolved task-level edges and the derived wave index are computed at assembly time and never persisted as edges. This is **forced** by the locked "no cached schedule / re-derive, don't store derived state" constraint (PROJECT.md; PERSIST-03; `verify-no-aggregates`) — write-back would itself be a cached-derivation that the guards forbid. It also makes incremental re-planning correct for free (re-resolve from current state). Not a genuine fork: the constraints leave only this option.
- **D-06: Coarse refs left un-refined fan out conservatively.** A ref never narrowed past its scope pulls in *all* that scope's tasks (conservative over-serialization, never an incorrect/missing edge). Correct *narrowing* (refining `MA` → `MA-P3-task-07`) is a planner-correctness responsibility that lands when the cross-scope-aware planners are built — **not** this engine phase. The engine's contract is only: fan out anything still coarse; never invent an edge that isn't implied by an authored `dependsOn`.

### Wave CR ownership & re-derivation lifecycle (EXEC-02)
- **D-07: One Wave CR per global wave, named `tide-wave-<project>-<globalIndex>`, owned by the Project.** Replaces the per-plan `tide-wave-<plan.UID>-<i>` scheme. `WaveSpec.ProjectRef` + global monotonic `WaveSpec.WaveIndex` are already the v1alpha2 shape (`api/v1alpha2/wave_types.go`); the engine fills them at global scope. Owner-ref → Project with `BlockOwnerDeletion: true` (established cascade pattern).
- **D-08: Wave-CR set is reconciled (create missing / update membership / prune extras) on every derivation.** When re-derivation yields fewer waves than exist (e.g., a dependency was removed and the schedule compressed), stale high-index Wave CRs must be pruned so the persisted Wave set always equals the current derivation. The `tide_waves_dispatched_total{project,phase,plan,wave}` metric keeps its exactly-once-on-Create semantics (D-08/CR-02 from the per-plan path) — re-derivation replays must not double-count. Pruning mechanics (delete vs. mark) are Claude's discretion; the invariant — *persisted Wave set == current derivation, no orphans* — is locked.

### Bidirectional global wave index (EXEC-03)
- **D-09: Keep the established label-indexed mechanism, re-pointed to the global index.** The existing per-plan contract already stamps `tideproject.k8s/wave-index=<N>` + `tideproject.k8s/project=<name>` on each Task (`plan_controller.go::stampTaskLabels`); WaveReconciler/TaskReconciler already read it. Phase 24 keeps this exact contract but the stamped index becomes the **global** wave index and the stamping moves to the Project-scoped derivation. Then: **task→wave** = read the Task's wave-index label; **wave→tasks** = label-selector list (`wave-index=<N>` ∧ `project=<name>`). This restores the README:54 namesake invariant Project-wide. A single per-Task scalar label is per-object, not an aggregate, so `verify-no-aggregates` stays green (it forbids `Schedule`/`Waves[]`/`IndegreeMap` in api types, not a scalar label). Rejected: a `Task.status.wave` field or a Project-level wave→tasks map (the latter is exactly the cached aggregate the guard forbids).

### Re-derivation triggers & no-cache idempotency (EXEC-04)
- **D-10: `ProjectReconciler` watches Task add/complete and recomputes the whole schedule from scratch each reconcile.** Re-derivation = re-list Tasks → re-assemble edges (with fan-out) → `pkg/dag.ComputeWaves` → reconcile Wave CRs + re-stamp labels. O(V+E) per the spec; nothing about the schedule is cached in `.status`. The completed-task set + the authored DAG are the only inputs — matching the locked resumption model (Phase 25 will dispatch off the same re-derivation). The reconcile must be idempotent: identical inputs → identical Wave set + labels, no churn, no metric double-count.
- **D-11: `pkg/dag.ComputeWaves` + `CycleError` are reused unchanged.** The pure-Go layered Kahn (`pkg/dag/kahn.go:46`) already produces `[][]NodeID` layers = waves and the `CycleError` shape Phase 23 wired into the cycle gate. The engine feeds it the *global* node/edge set; `pkg/dag` stays k8s-free (the `verify-dag-imports` firewall must remain green). No new scheduling algorithm — the spec explicitly rejects CPM/HEFT.

### Claude's Discretion
- The exact **planning-complete signal** (Project status condition vs. derived "all leaf Plans materialized" check) — only the *ordering contract* (D-02) is locked.
- Whether to **delete vs. fully neutralize** the per-plan `materializeWaves`/`stampTaskLabels` paths (D-03) and the precise refactor mechanics.
- The Wave-CR **prune mechanic** (delete extras vs. tombstone) and exact requeue/backoff on transient List failures (D-08).
- Exact **label keys/values** if any need adjusting for the global index, status-condition type/reason strings for derivation state, and printer columns.
- How fan-out resolves a scope ref to its task set (which label encodes phase/plan/milestone membership) and any dedup of overlapping coarse+fine edges (D-04).
- Keeping `make verify-no-aggregates` / `verify-no-sqlite-dep` / `verify-dag-imports` green through the change.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spec (load-bearing — the implementation must conform)
- `README.md` §"The acronym" (lines ~50–58, esp. the **README:54 namesake invariant** — "given any task, you know its wave; given any wave, you know its tasks") and §"Execution DAG" / "Wave computation" Kahn worked example. This is the conformance target EXEC-03 restores Project-wide. (Phase 26 SPEC-01 pins the cross-plan/phase/milestone worked example as an executable test.)

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` — authoritative requirement→phase table. **Phase 24 owns EXEC-01/02/03/04** (DEPS-* completed in Phase 23; DISP-*/RESUME-* are Phase 25). Read the EXEC section and the adjacent DISP/RESUME sections for the downstream contract Phase 25 builds on this engine.
- `.planning/ROADMAP.md` §"Phase 24" and the Phase 24→25→26 chain — phase boundaries. ⚠ Phase 24 header's requirements line is correct; the *Phase 23* header's "EXEC-01..04" line is the stale one.
- `.planning/PROJECT.md` §"Current Milestone: v1.0.2 Spring Tide" — locked milestone decisions (global wave derivation, **no cached schedule / re-derive**, resumption = indegree map + completed-set, cycles refused not recovered, wave-boundary failure contract preserved at global scope).

### Phase 23 hand-off (this phase consumes its schema + completes its TODOs)
- `.planning/phases/23-schema-migration-cross-scope-dependency-model/23-CONTEXT.md` — D-05 (in-memory resolution, deferred to here), D-06 (assembler collapses DEPS-01+02 via fan-out), D-07 (Wave re-owned Plan→Project), D-08 (`wave` label = global index), D-10 (cycle gate). The "Deferred Ideas" list explicitly routes the global engine + write-back-vs-in-memory lock to this phase.

### Current code being extended/replaced
- `internal/controller/project_controller.go` — `assembleProjectDepGraph` (lines ~1432–1480, conservative task-only edges — **extend to fan-out**, D-04) and `checkGlobalCycleGate` (~1482+, derivation runs after this passes, D-02); `checkSchemaRevisionGuard` (RequiresReinstall fail-closed, ~1410).
- `internal/controller/plan_controller.go` — `materializeWaves` (~1347, `tide-wave-<plan.UID>-<i>`, Plan-owned — **superseded**, D-03) and `stampTaskLabels` (~1421, the `wave-index`/`project` label contract to re-point global, D-09).
- `internal/controller/wave_controller.go` — Phase-24 TODOs (lines ~104, 134, 236, 248) for re-wiring Wave→Task association off the global wave index (ProjectRef-scoped).
- `api/v1alpha2/wave_types.go` — `WaveSpec{ProjectRef, WaveIndex}` (global monotonic index; the engine fills these).
- `pkg/dag/kahn.go` (`ComputeWaves`, line 46) + `pkg/dag/errors.go` (`CycleError`) — reused unchanged (D-11); keep k8s-free.

### Guards that must stay green
- `Makefile` targets `verify-no-aggregates` (greps `api/v1alpha*/*_types.go` for `Schedule`/`Waves[]`/`IndegreeMap` — the global index stays a Wave CR spec field + per-Task label, never a cached aggregate), `verify-no-sqlite-dep`, `verify-dag-imports` (`tools/analyzers/dagimports/`).
- `internal/metrics/registry.go` — locked `{project,phase,plan,wave}` label set; `wave` now sourced from the global index (Phase 23 D-08).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/dag.ComputeWaves` / `CycleError` — layered Kahn already returns waves-as-layers; feed it the global node/edge set. No algorithm work needed (D-11).
- `ProjectReconciler.assembleProjectDepGraph` + `checkGlobalCycleGate` — Phase 23 already built the global Task listing + cycle assembly with conservative task-only edges; Phase 24 extends the edge step to fan-out and appends the wave-derivation stage (D-01, D-04).
- `stampTaskLabels` + the `tideproject.k8s/wave-index` / `tideproject.k8s/project` label contract — the bidirectional index mechanism already exists per-plan; re-point it to the global index (D-09).
- Wave create/idempotency + exactly-once metric pattern in `materializeWaves` (AlreadyExists-as-success, increment only on Create) — port the semantics into the global derivation, add prune (D-08).

### Established Patterns
- **Namespace-per-project tenancy** — all of a Project's Tasks share one namespace; fan-out scope→task resolution is a same-namespace label query, no cross-namespace machinery.
- **No cached schedule / re-derive (PERSIST-03)** — `verify-no-aggregates` forbids `Schedule`/`Waves[]`/`IndegreeMap` in api types. Global index lives as Wave CR spec + per-Task scalar label only (D-05, D-09).
- **Owner-ref cascade `BlockOwnerDeletion: true`**, Spec/Status separation, small CRDs.
- **Idempotent reconcile** — recompute-from-scratch each pass; AlreadyExists is success; metric increments exactly once on Create (D-10).

### Integration Points
- Wave creation moves `PlanReconciler` → `ProjectReconciler` (the one global writer of Wave CRs).
- `wave` metric label value source changes to the global index (`internal/metrics/registry.go`).
- `WaveReconciler` Wave→Task mapper re-wires off the global index (ProjectRef-scoped) — the four `wave_controller.go` Phase-24 TODOs.
- Phase 25 dispatch consumes this engine's re-derivation (global indegree 0 vs completed-set) — keep the derivation pure/recomputable so Phase 25 reads it without new persisted state.

</code_context>

<specifics>
## Specific Ideas

- Progressive-refinement worked example (carried from Phase 23, the canonical illustration the engine's fan-out must honor): `MB requires MA` → `MB requires MA-P3` → `MB requires MA-P3-PB` → `MB requires MA-P3-PB-task-07`. Un-refined coarse refs fan out to the whole named scope (D-06); refined refs narrow to the named node.
- Wave naming moves from `tide-wave-<plan.UID>-<i>` to `tide-wave-<project>-<globalIndex>` (illustrative; exact format is discretion, but it must be Project-scoped + global-monotonic, not Plan-keyed).

</specifics>

<deferred>
## Deferred Ideas

- **Global dispatch off one indegree map, wave-boundary failure semantics at global scope, gates-as-holds, minimal resumption** — Phase 25 (DISP-01..03, RESUME-01). This engine produces the schedule Phase 25 dispatches from; it does not dispatch.
- **Multi-milestone drive via the Milestone DAG + cross-milestone shared waves + per-milestone gate policy + README conformance test** — Phase 26 (MS-01..03, SPEC-01).
- **Planner-prompt discipline for correct dependency refinement** (avoid incorrect narrowing → missing edge) — lands when the cross-scope-aware planners are built, not in this engine phase (D-06).

### Reviewed Todos (not folded)
- `cache-f1-direct-sdk-cross-pod-caching.md` (todo.match score 0.6) — **reviewed, not folded.** Off-domain: it concerns direct-SDK cross-pod *prompt caching* from the superseded Ebb Tide scope; it matched only on weak keyword coincidence ("status/phase/cross") and has nothing to do with global wave derivation. Belongs to the deferred OpenAI/cost work, not this phase.

</deferred>

---

*Phase: 24-global-wave-derivation-engine*
*Context gathered: 2026-06-16*
