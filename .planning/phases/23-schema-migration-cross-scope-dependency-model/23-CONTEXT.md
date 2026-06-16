# Phase 23: Schema Migration + Cross-Scope Dependency Model - Context

**Gathered:** 2026-06-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Re-shape the CRD surface so **Wave ownership/derivation lives at Project scope** (a global, monotonic wave index) and **Tasks can declare dependencies across plan/phase/milestone boundaries** — all reconciled into one global Execution DAG that **rejects cycles at validation**, shipped behind a **breaking-but-explicit migration path** that never silently corrupts an in-flight Project.

This phase delivers the **schema + dependency-reference model only**. The actual global Kahn derivation/assembly **engine is Phase 24**; global dispatch/failure/gates/resume is Phase 25; multi-milestone drive is Phase 26. Phase 23's job is to make the schema *permit* the global model without precluding any Phase 24+ decision.

**Requirements owned by this phase (authoritative mapping in REQUIREMENTS.md):** SCHEMA-01, SCHEMA-02, SCHEMA-03, DEPS-01, DEPS-02, DEPS-03.

⚠ **ROADMAP discrepancy to ignore:** the Phase 23 ROADMAP header line lists "Requirements: EXEC-01..04." That is **stale** — the requirement→phase table maps EXEC-01..04 to **Phase 24** (the derivation engine). Scope this phase to SCHEMA-* + DEPS-* per the table.

</domain>

<decisions>
## Implementation Decisions

### Cross-scope dependency reference model (DEPS-01, DEPS-02)
- **D-01: One reference system — flat structural IDs. No interface-id namespace.** A `dependsOn` entry is a flat string naming a hierarchy node (Milestone / Phase / Plan / Task), resolved within the single project namespace (all CRs share one namespace per the namespace-per-project model, so refs never cross namespaces). The earlier "declared interface points / `provides` / `exposes`" idea is **explicitly dropped** — it created two competing reference systems (structural IDs *and* semantic interface IDs). Do **not** reintroduce a `provides`/`exposes`/interface-id field.
- **D-02: `dependsOn []string` lives on every level, targets may name any level.** Generalize the existing `Milestone.dependsOn` / `Phase.dependsOn` (currently sibling-only), **add `dependsOn` to `Plan`** (it has none today), and **broaden `Task.dependsOn` past plan-local** (retire D-F1's same-Plan restriction). An entry may target a node at any level (a Task may depend on a Plan; a Milestone's dep may be refined down to a Task).
- **D-03: Progressive refinement — coarse first, narrowed as planning descends.** A dependency is authored at the coarsest known granularity and sharpened as deeper structure materializes: `MB → MA`, then `MB → MA-P3`, then `MB → MA-P3-PB`, then `MB → MA-P3-PB-task-07`. This is what lets a higher-level planner declare a dependency *before* the producing tasks exist.
- **D-04: Refinement model = author-per-level + per-unit resolution (NOT scattered mutate-in-place).** Each scope authors its own `dependsOn` at the granularity it knows and **never reaches into another CRD to rewrite it mid-planning** (rejected "mutate-in-place" online refinement: racy ResourceVersion conflicts, multi-writer ordering hazards). Resolution from coarse refs to concrete Task edges happens at **per-plan / per-task granularity** (the user's clarification — *not* one monolithic global sweep), which composes into the global DAG and supports O(V+E) re-derivation on each task add/complete (EXEC-04, Phase 24).
- **D-05: Resolution intended in-memory (final mechanic locked in Phase 24).** Lean: the per-unit resolution that turns coarse refs into task edges runs **in-memory at assembly time and is NOT written back** into the CRDs — authored coarse `dependsOn` is the only persisted truth; resolved task edges are derivation (same category as the wave index, which PERSIST-03 / `verify-no-aggregates` forbid caching). This keeps incremental re-planning correct for free (re-resolve from current state) and is consistent with "re-derive, don't store derived state." **The write-back-vs-in-memory mechanic is a Phase 24 engine decision** — Phase 23 must only shape the schema so in-memory resolution is *possible* (it is: `dependsOn` holds authored intent; nothing in the schema requires written-back edges).
- **D-06: Assembler reconciliation (Phase 24) collapses DEPS-01 + DEPS-02 into one mechanism.** A `dependsOn` entry naming a **Task** → a direct task→task edge (DEPS-01). A `dependsOn` entry still naming a **scope** (Milestone/Phase/Plan) at assembly time → expands **fan-in/fan-out over that scope's tasks** (DEPS-02 "reconciled into the global task DAG"). Coarse refs that were never refined still resolve correctly — they just pull in more tasks (conservative over-serialization, never an incorrect edge).
- **Correctness note for downstream planner-prompt work (NOT this phase):** refinement *narrows* an edge (`MA` = "all MA tasks" → `task-07` = "just task-07"), which is a relaxation that is correct only if the narrowed target is genuinely the sole producer. Correct refinement is a **planner-correctness responsibility**; the Phase 23 schema only has to permit any-level refs + the assembler has to fan-out anything left coarse. Planner-prompt discipline lands when those planners are built, not here.

### Wave representation (SCHEMA-01, SCHEMA-02)
- **D-07: Keep a Wave CR, re-owned Plan → Project.** Replace `WaveSpec.PlanRef` with a Project ref; `WaveSpec.WaveIndex` becomes the **global, monotonic** wave index (not the per-plan `tide-wave-<plan.UID>-<i>` scheme). One Wave CR per global wave. Chosen over the "derived status view / no Wave CR" alternative so the dashboard/CLI keep a concrete object to list and so the global index does not have to live in a `.status` field that risks tripping the `verify-no-aggregates` guard. (Direction "Wave ownership moves Plan→Project" was already locked in PROJECT.md; this picks the concrete shape.)
- **D-08: `wave` telemetry label resemanticized to the global wave; label set unchanged.** Keep the locked metric label set `{project, phase, plan, wave}` (internal/metrics/registry.go); the `wave` label now carries the **global** wave index. The `task` label stays **forbidden** (metric-cardinality analyzer). Mechanical/locked — no design choice here.

### Migration / versioning (SCHEMA-03)
- **D-09: Clean break — v1alpha2-only served, reject old-shape objects, drop the conversion webhook.** Introduce `v1alpha2` with the new shape and **remove `v1alpha1` from the served versions**. The controller **fail-closed rejects** any surviving old-shape object with a clear status condition (e.g. `reason: RequiresReinstall`) — it never silently runs on stale data, satisfying SCHEMA-03's "no silent corruption" literally. **Retire the no-op conversion-webhook scaffolding** (`api/v1alpha1/plan_conversion.go`, the `Hub()`/`ConvertTo`/`ConvertFrom` stubs, webhook wiring). Justified by pre-GA reality: only dogfood clusters exist today; a full conversion webhook is overkill when a documented reinstall + loud rejection covers every real in-flight object. Bump version in release notes as a breaking CRD change requiring reinstall.

### Cycle rejection (DEPS-03)
- **D-10: Cycle rejection across plan/phase/milestone boundaries, controller-side, at validation time, involved nodes surfaced.** CEL `x-kubernetes-validations` cannot express all-paths cycle detection, so this is controller/validation logic (per the project's CEL-except-cycle-detection convention), reusing `pkg/dag`'s `CycleError` shape. Surfaced via a Project (or validation) status condition naming the involved nodes. **No runtime cycle recovery** — refuse and surface. Exact surface (where the condition lands, when validation fires) is research/planner territory; the decision here is only *that* it is global-scope, validation-time, node-surfacing, and recovery-free.

### Claude's Discretion
- Exact `v1alpha2` field names, kubebuilder markers, CEL constraints, printer columns, and deepcopy/regeneration mechanics.
- The precise status-condition type/reason strings for old-object rejection and cycle rejection.
- How `make verify-no-aggregates` / `verify-dag-imports` guards are kept green through the schema change (and whether any guard grep-pattern needs updating for the global wave index — must stay forbidden as a *cached schedule*, permitted as a *Wave CR spec*).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spec (load-bearing — the implementation must conform)
- `README.md` §"Execution DAG" / Kahn worked example (≈ lines 78–241) and the **README:54 namesake invariant** ("given any task you know its wave; given any wave you know its tasks") — the cross-plan/cross-phase/cross-milestone worked example is the conformance target. Phase 23 must not preclude it; SPEC-01 (Phase 26) encodes it as an executable test.

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` — **authoritative** requirement→phase mapping table (Phase 23 owns SCHEMA-01/02/03 + DEPS-01/02/03; EXEC-* is Phase 24). Read the SCHEMA, DEPS, EXEC, DISP, MS, RESUME, SPEC sections for the full v1.0.2 Spring Tide contract.
- `.planning/ROADMAP.md` §"Phase 23" (line ~80) and Phases 24–26 — phase boundaries and dependency chain. ⚠ Phase 23 header's "Requirements: EXEC-01..04" line is stale; trust the REQUIREMENTS.md table.
- `.planning/PROJECT.md` §"Current Milestone: v1.0.2 Spring Tide" — locked decisions (Wave Plan→Project, no cached schedule, resumption = indegree + completed-set, cycles refused not recovered).

### Current CRD schema being migrated
- `api/v1alpha1/wave_types.go` — `WaveSpec{PlanRef, WaveIndex}` (the per-plan ownership being re-owned to Project; SCHEMA-01).
- `api/v1alpha1/task_types.go` — `TaskSpec.DependsOn []string` (plan-local D-F1, to be broadened; DEPS-01).
- `api/v1alpha1/plan_types.go` — `PlanSpec` has **no** `dependsOn` today (to be added; DEPS-02).
- `api/v1alpha1/phase_types.go`, `api/v1alpha1/milestone_types.go` — existing sibling-only `DependsOn` (to be generalized to any-level targets).
- `api/v1alpha1/project_types.go` — Project scope (new Wave owner; namespace-per-project tenancy).

### Migration surface (to be removed)
- `api/v1alpha1/plan_conversion.go`, `internal/webhook/v1alpha1/wave_webhook.go`, `internal/webhook/v1alpha1/plan_webhook.go` — the no-op conversion-webhook + admission scaffolding being retired (D-09).

### DAG algorithm + guards (Phase 24 will consume; Phase 23 must keep green)
- `pkg/dag/kahn.go`, `pkg/dag/errors.go` — `ComputeWaves` + `CycleError` (cycle-detection reuse for DEPS-03; layered Kahn for Phase 24).
- `internal/controller/plan_controller.go` — current per-plan `materializeWaves` (`tide-wave-<UID>-<i>` naming) being superseded by global derivation in Phase 24.
- `internal/metrics/registry.go` — locked `{project,phase,plan,wave}` label set; `wave` resemanticized (D-08, SCHEMA-02).
- `Makefile` targets `verify-no-aggregates` (line ~526), `verify-no-sqlite-dep` (~537), `verify-dag-imports` (~472) — guards that must stay green through the schema change.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/dag` (`ComputeWaves`, `CycleError`) — pure-Go layered Kahn + cycle shape; cycle detection (DEPS-03) reuses `CycleError`. Has an import firewall (`verify-dag-imports`); keep it k8s-free.
- Conversion-webhook scaffolding already exists (firing as no-ops) — but D-09 **removes** it rather than wiring it. Note its presence so removal is complete (admission + conversion + manifests + suite registration).
- `make verify-no-aggregates` greps `api/v1alpha1/*_types.go` for `Schedule`/`Waves[]`/`IndegreeMap` — the global wave index must remain a **Wave CR spec field**, never a cached aggregate in `.status`, to stay green.

### Established Patterns
- **Namespace-per-project tenancy** — every reconciler is `WatchNamespace`-scoped; all of a Project's Milestones/Phases/Plans/Tasks share one namespace. Cross-scope `dependsOn` refs resolve within that single namespace (no cross-namespace ref machinery needed).
- **Spec/Status separation + small CRDs** (PERSIST-02 / Pitfall 4) — keep new `dependsOn` and the global wave index in Spec; status stays minimal. Owner-ref cascade with `BlockOwnerDeletion: true`.
- **CEL inline validation, except cycle detection** — structural constraints (MinLength, enums) go in CEL markers; all-paths cycle detection is controller-side (D-10).

### Integration Points
- Wave creation moves from `internal/controller/plan_controller.go::materializeWaves` (per-plan) to Project-scope ownership; Phase 23 reshapes the **schema/ownership**, Phase 24 builds the global derivation that creates the re-owned Wave CRs.
- `internal/metrics/registry.go` label emission — `wave` value source changes to the global index (D-08).

</code_context>

<specifics>
## Specific Ideas

- Progressive-refinement worked example the user gave (record verbatim as the canonical illustration): `MB requires MA` → (phases planned) `MB requires MA-P3` → (plans written) `MB requires MA-P3-PB` → (tasks established) `MB requires MA-P3-PB-task-07`.
- Resolution is **per-plan / per-task**, explicitly *not* a single global traversal — this granularity is what makes O(V+E) incremental re-derivation (EXEC-04) natural.
- Old-object rejection condition reason floated as `RequiresReinstall` (illustrative; exact string is Claude's discretion).

</specifics>

<deferred>
## Deferred Ideas

- **Global Kahn derivation / assembly engine + bidirectional global wave index** — Phase 24 (EXEC-01..04). Phase 23 only shapes the schema for it.
- **Global dispatch off one indegree map, wave-boundary failure semantics at global scope, gates-as-holds, minimal resumption** — Phase 25 (DISP-01..03, RESUME-01).
- **Multi-milestone drive via the Milestone DAG + cross-milestone global waves + milestone gate policy + README conformance test** — Phase 26 (MS-01..03, SPEC-01).
- **Write-back vs. in-memory resolution final lock** — deferred to Phase 24's engine work (D-05); Phase 23 only ensures the schema permits in-memory resolution.
- **Planner-prompt discipline for correct dependency refinement** (avoid incorrect narrowing → missing edge) — lands when the cross-scope-aware planners are built, not in this schema phase.

</deferred>

---

*Phase: 23-schema-migration-cross-scope-dependency-model*
*Context gathered: 2026-06-16*
