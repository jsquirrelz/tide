# Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption - Context

**Gathered:** 2026-06-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Make execution **dispatch readiness**, the **wave-boundary failure contract**, **gate holds**, and **restart resumption** all operate over the ONE global Execution DAG Phase 24 built — not per-plan. This is the layer that *uses* Phase 24's global wave derivation to actually run work correctly at global scope.

Concretely, this phase retires the **plan-local** dispatch machinery that still ships today:
- `task_controller.go::computeIndegree` + `listSiblingTasks` resolve `DependsOn` only against same-`PlanRef` siblings — the literal D-F1 restriction this milestone exists to retire. They must resolve against the **global** edge set (with Phase 24's coarse-ref fan-out) and the **global** completed-task set.
- The wave-boundary failure contract (spec §"Failure handling at wave boundaries") must hold **exactly at global scope**: failed task → independent siblings in the same global wave continue; global dependents never dispatch; non-dependents dispatch in **strict** / halt in **conservative**.
- Gates compose with the global scheduler as **holds**.
- An orchestrator restart re-derives the entire schedule from the global indegree map + completed-task set alone.

**Requirements owned by this phase (authoritative mapping in REQUIREMENTS.md):** DISP-01, DISP-02, DISP-03, RESUME-01.

**Explicitly NOT this phase:** the global derivation *engine* itself (Phase 24, done); multi-milestone drive via the Milestone DAG + cross-milestone shared waves + per-milestone gate policy + README conformance test (Phase 26, MS-01..03 + SPEC-01).

</domain>

<decisions>
## Implementation Decisions

> Every decision below is constrained by already-locked direction — PROJECT.md "no cached schedule / re-derive"; PERSIST-03 / `verify-no-aggregates`; resumption = global indegree + completed-set only; cycles refused not recovered; gates configurable per-Project never baked in; `tide resume --retry-failed` is the one sanctioned recovery verb (Phase 12/17, D-07). The discussion surfaced two simplifications that *shrink* this phase rather than grow it (see D-03, D-02).

### Global dispatch readiness (DISP-01)
- **D-01: TaskReconciler re-derives its own global readiness each reconcile — nothing derived is persisted.** A Task resolves its own global `DependsOn` (fanning coarse Milestone/Phase/Plan refs out to their member Tasks via the project/phase/plan/milestone labels, per Phase 24 D-04/D-06) against the completed-task set and dispatches when global indegree is 0 — regardless of which Plan/Phase/Milestone authored the dependency. The coarse-ref fan-out resolution is factored into a **shared helper** so the dispatch decision and Phase 24's wave derivation can never disagree about what an edge means.
  - **Why this and not a stamped signal:** the two governing constraints — RESUME-01 ("no *other* persisted execution state") and PERSIST-03 — both point the same way: re-derive, persist nothing. RESUME-01 then falls out *for free* (restart runs the identical compute). TaskReconciler stays the sole authority for its own dispatch — no two-writer staleness hop where a task is ready-in-truth but not-yet-stamped.
  - **Rejected:** ProjectReconciler stamping a per-task `dispatchable`/`blocked-by` label (viable, centralizes compute, but reintroduces a *derived persisted signal* — the softer version of the cached-derivation smell that motivated Spring Tide — and splits the dispatch decision across two controllers). Also rejected hard: wave-gated dispatch ("dispatch wave N once all of wave <N is Succeeded" — breaks the strict failure contract by blocking independent later-wave work behind an unrelated earlier failure), and a persisted Project-level indegree map (`IndegreeMap` is literally a `verify-no-aggregates` grep token).
  - **The readiness test is per-dependency (global indegree 0), never per-wave-completion.** This is load-bearing for DISP-02: strict profile requires a later-wave non-dependent to still dispatch when an earlier task failed.

### Failure semantics / strict vs conservative profile (DISP-02)
- **D-02: `Project.Spec.FailureProfile` enum `{strict|conservative}`, default `strict`.** Per-Project (matches the gates-per-Project pattern, the "conservative becomes a per-Project setting" key decision, and CEL-enum validation). Per-milestone overrides are a Phase 26 concern; out of scope now.
- **D-02a: Strict profile is (almost) free from the indegree model.** With D-01 in place, "dependents never dispatch" falls out (a Failed task is never `Succeeded`, so dependents' indegree never reaches 0) and "independent siblings continue" falls out (independent tasks have their own indegree). Strict needs nothing beyond the readiness model + treating `Failed` as permanently-not-`Succeeded`.
- **D-02b: Conservative profile = a project-wide failure-halt condition mirroring `BillingHalt`.** On the first task failure under `conservative`, set a `ConditionFailureHalt` on the Project that all dispatch sites already-gate-check (exactly as `checkBillingHalt` gates all five dispatch sites today). In-flight Jobs finish (you can't un-dispatch a running Job; same-wave siblings drain per the spec). The operator clears it via `tide resume --retry-failed` (the one sanctioned recovery verb). Dependents were already blocked by indegree, so halting them too is harmless and is the *point* of conservative ("something broke, freeze and let a human look").
  - **Rejected:** per-scope (milestone/phase) halt — the spec says non-dependents *project-wide*, so scoped halt is extra complexity for no spec-required benefit (revisit per-milestone in Phase 26). Pure per-task "is anything failed AND am I a non-dependent" computation — stateful, messy, no reuse.

### Gate-as-hold composition (DISP-03)
- **D-03: Milestone/Phase/Plan gates are planning-DAG holds and need NO execution-time re-check; the Task gate is the sole execution hold.** A user observation reshaped this: with approve-at-descent (Phase 12), a Milestone/Phase/Plan gate withholds *authoring* that scope's children — so an un-approved scope's Tasks **never get authored** and can't be in the global Execution DAG at all. Execution can only ever run already-approved-and-planned scopes; a "globally-ready Task whose milestone gate is still pending" is **structurally impossible**. Their composition with the scheduler is automatic and structural (un-approved scope ⇒ no tasks ⇒ nothing to hold).
- **D-03a: The Task gate is inherently an execution hold and composes with global indegree.** A Task is a leaf — nothing to author below it; its "descent" *is* its execution. So `gates.task: approve` must withhold a globally-ready Task at dispatch. `task_controller.go` already parks a ready Task at `AwaitingApproval` until `tideproject.k8s/approve-task: "true"` arrives; Phase 25 composes that with the **global** indegree-0 check:
  ```
  dispatch(task) iff globalIndegree==0 AND task-gate approved AND no billing/failure halt
  ```
  This keeps **fully-supervised** expressible (spec: fully-autonomous / fully-supervised must be as expressible as approve-every-milestone; DISP-03 explicitly lists "task approve"). It is opt-in (default `auto`).
- **D-03b: A held (`AwaitingApproval`) Task blocks its dependents for free.** No extra mechanism: indegree counts a dependency satisfied only when `Succeeded`; `AwaitingApproval` ≠ `Succeeded`, so dependents wait via the same indegree math, transitively. Non-dependents correctly keep running (an unrelated pending approval must not freeze independent work).
  - **Rejected:** dropping the task gate entirely (Option 1 — simpler, but removes fully-supervised expressiveness and narrows DISP-03's explicit "task approve"). Also dropped: my earlier over-engineered "per-task ancestry gate-closure at execution" for upper levels (refuted by D-03) and the optional project-level "approve the assembled DAG before execution" boundary gate (the user did not want the extra checkpoint).

### Minimal resumption (RESUME-01)
- **D-04: Resumption falls out of D-01 — Phase 25 adds a regression test, not new persistence.** On restart: (1) completed-task set = Task CRD `.status.Phase == Succeeded` (Task CRDs survive in etcd; the set is *read*, never stored as an aggregate); (2) the global indegree map is re-derived O(V+E) from authored `dependsOn` + completed-set, nothing cached (`verify-no-aggregates` stays green); (3) in-flight `Running` Tasks re-adopt via the deterministic Job name `podjob.JobName(task.UID, task.Status.Attempt)` (existing `checkRunningState`); (4) halt conditions (`BillingHalt`, new `FailureHalt`) persist as Project `.status` — safety signals, *not* schedule state — survive restart and clear via `tide resume --retry-failed`. RESUME-01's "no *other* persisted execution state" targets the derived **schedule**, not per-object status or the authored DAG.

### Claude's Discretion
- **D-01 mechanic:** list-all-project-tasks-and-filter vs label-select-each-dep's-scope (per-dependency-scoped resolution, O(deps×scope) instead of O(V+E)) — both satisfy D-01; pick the efficient one. The shared-helper package boundary (where the fan-out resolver lives, reused by ProjectReconciler + TaskReconciler) is research/planner territory.
- **Watch/field-index wiring** so a completing/held Task re-enqueues its *global* dependents (today's sibling watch is plan-local) — real mechanics, not a user decision.
- **Condition/reason vocabulary:** the exact `FailureHalt` condition type + reason strings (mirror the `BillingHalt` vocabulary in `api/v1alpha2/shared_types.go`), the `FailureProfile` CEL enum markers/printer columns.
- Keeping `make verify-no-aggregates` / `verify-no-sqlite-dep` / `verify-dag-imports` green; the locked `{project,phase,plan,wave}` metric label set unchanged.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spec (load-bearing — the implementation must conform)
- `README.md` §"Failure handling at wave boundaries" — the **exact** contract DISP-02 must preserve at global scope (failed task → siblings continue; dependents in later waves never dispatch; non-dependents dispatch in strict / halt in conservative; resumption state = indegree map + completed-task set, nothing more).
- `README.md` §"The acronym" (≈ lines 50–58, the **README:54 namesake invariant**) and §"Execution DAG" / Kahn worked example — the global wave model dispatch runs against. (Phase 26 SPEC-01 pins the cross-plan/phase/milestone worked example as an executable test.)

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` — authoritative requirement→phase table. **Phase 25 owns DISP-01, DISP-02, DISP-03, RESUME-01.** Read the DISP / RESUME sections; the adjacent MS-* belong to Phase 26 (this phase must not preclude them).
- `.planning/ROADMAP.md` §"Phase 25" and the Phase 24→25→26 chain — phase boundaries.
- `.planning/PROJECT.md` §"Current Milestone: v1.0.2 Spring Tide" — locked milestone decisions (global dispatch off one indegree map + completed-set; wave-boundary failure contract preserved at global scope; resumption minimal; cycles refused not recovered; gates per-Project). Also §"Key Decisions" rows: approve-at-descent (Phase 12), provider billing-400 project-wide halt (Phase 13, the pattern `FailureHalt` mirrors), reserve/settle budget rederivable on restart (Phase 14), `tide resume --retry-failed` as the one recovery verb (D-07).

### Phase 23/24 hand-off (this phase consumes their schema + engine)
- `.planning/phases/24-global-wave-derivation-engine/24-CONTEXT.md` — D-04/D-06 (coarse-ref fan-out resolution the readiness helper reuses), D-05 (in-memory only, never written back), D-09 (per-Task `wave-index`/`project` label index — per-object scalar, not an aggregate), D-10 (ProjectReconciler re-derives O(V+E) on task add/complete, no cached schedule).
- `.planning/phases/23-schema-migration-cross-scope-dependency-model/23-CONTEXT.md` — D-02 (`dependsOn` on every level, any-level targets; D-F1 retired), D-06 (assembler collapses DEPS-01+02 via fan-out).

### Current code being extended/replaced
- `internal/controller/task_controller.go` — `computeIndegree` (~1198) + `listSiblingTasks` (~1183, **plan-local `PlanRef` filter — must go global**, D-01); `checkReadinessGates` (~417, where indegree + task-gate compose, D-01/D-03a); `checkRunningState` (~483, in-flight Job re-adoption for RESUME-01, D-04); the dispatch gate ladder in `gateChecks` (~297) where `checkBillingHalt` gates dispatch (the slot `FailureHalt` joins, D-02b).
- `internal/controller/billing_halt.go` + the `checkBillingHalt(project)` gate + `ConditionBillingHalt`/reason vocabulary in `api/v1alpha2/shared_types.go` (~199–222) — the **pattern** `ConditionFailureHalt` mirrors (D-02b).
- `internal/gates/` — `policy.go` (`EvaluatePolicy`, `DefaultGates`), `annotation.go` (`CheckApprove`/`ConsumeApprove`), `doc.go` — the per-level gate machinery; Phase 25 composes the **task** level with global indegree (D-03a) and leaves M/P/P as planning holds (D-03).
- `api/v1alpha2/project_types.go` — `Gates` struct (~53) and where `FailureProfile` enum field lands (~Spec, D-02).
- `internal/controller/project_controller.go` — `assembleProjectDepGraph` + fan-out (Phase 24) is the shared resolution the readiness helper factors out / reuses (D-01).
- `pkg/dag/kahn.go` — global indegree/Kahn reused unchanged; keep k8s-free (`verify-dag-imports`).
- `cmd/` `tide resume --retry-failed` — composes unchanged; clears halts + resets `Failed`→`Pending` (D-02b, D-04).

### Guards that must stay green
- `Makefile`: `verify-no-aggregates` (no `Schedule`/`Waves[]`/`IndegreeMap` in `api/v1alpha*/*_types.go` — the indegree map stays re-derived, never a `.status` field), `verify-no-sqlite-dep`, `verify-dag-imports`.
- `internal/metrics/registry.go` — locked `{project,phase,plan,wave}` label set unchanged; `task` label stays forbidden (cardinality analyzer).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`checkBillingHalt` project-wide halt pattern** (gates all five dispatch sites, persisted as a Project condition, cleared by `tide resume`) — `ConditionFailureHalt` reuses these exact rails (D-02b).
- **The indegree model itself** — `computeIndegree`'s `!= "Succeeded"` test already gives both strict-profile dependent-blocking *and* held-task dependent-blocking for free (D-02a, D-03b). The only change is widening its sibling scope from plan-local to global with fan-out (D-01).
- **`checkRunningState` + deterministic `podjob.JobName(UID, Attempt)`** — in-flight Job re-adoption on restart already works; RESUME-01 needs no new code here (D-04).
- **`internal/gates` per-level policy/annotation machinery** — the task-level gate already parks at `AwaitingApproval`; Phase 25 just composes it with global indegree (D-03a).
- **Phase 24 `assembleProjectDepGraph` fan-out** — the coarse-ref→task-set resolution the readiness helper shares (D-01).

### Established Patterns
- **No cached schedule / re-derive (PERSIST-03)** — `verify-no-aggregates` forbids `Schedule`/`Waves[]`/`IndegreeMap` in api types; indegree stays re-derived per reconcile, halts live as per-object status conditions only (D-01, D-04).
- **Namespace-per-project tenancy** — all of a Project's Tasks share one namespace; global fan-out scope→task resolution is a same-namespace label query, no cross-namespace machinery.
- **Idempotent reconcile** — recompute readiness from scratch each pass; this is what makes restart a no-op (D-01, D-04).
- **Gate policy read from `Project.Spec.Gates`, never baked into the controller** — `FailureProfile` follows the same per-Project-config rule (D-02, D-03).

### Integration Points
- `listSiblingTasks` plan-local `PlanRef` filter → global resolution with fan-out (D-01) — the single most important change; needs a watch/field-index so a completing/held Task re-enqueues its *global* dependents (today's watch is plan-local).
- `FailureHalt` condition joins the dispatch gate ladder alongside `checkBillingHalt` (D-02b).
- `FailureProfile` enum field added to `Project.Spec` with CEL validation (D-02).
- `tide resume --retry-failed` clears `FailureHalt` in addition to `BillingHalt` (D-02b, D-04).

</code_context>

<specifics>
## Specific Ideas

- Conservative-halt UX mirrors billing-halt exactly: first `Failed` task under `conservative` → project freezes new dispatch, in-flight drains, `tide resume --retry-failed` clears. Same operator muscle memory as a credit dry-out.
- Strict is the default profile (matches the PROJECT.md "Strict-by-default failure profile" key decision).
- Fully-supervised run = `gates.task: approve` — a human approves every code-writing dispatch; the held task blocks its dependents automatically, non-dependents keep flowing.
- The dispatch predicate to encode/verify literally: `dispatch(task) iff globalIndegree(task)==0 AND task-gate approved AND NOT billingHalt AND NOT (conservative ∧ failureHalt)`.

</specifics>

<deferred>
## Deferred Ideas

- **Multi-milestone drive via the Milestone DAG + cross-milestone shared waves + per-milestone gate policy + README execution-DAG conformance test** — Phase 26 (MS-01..03, SPEC-01). Phase 25 must not preclude per-milestone `FailureProfile`/gate policy, but does not implement it.
- **Optional project-level "approve the fully-assembled global DAG before execution spends" boundary gate** (the "slack tide" execution checkpoint) — surfaced and **declined** by the user for this phase. Recorded in case a future supervised-run need revives it; it composes cleanly on top of the D-03 model (a single project-level execution-release condition) without disturbing the per-task dispatch predicate.
- **Per-scope (milestone/phase) conservative halt granularity** — considered, rejected for Phase 25 (spec says non-dependents project-wide). A natural Phase 26 extension once per-milestone policy exists.
- **Direct-SDK cross-pod prompt caching (CACHE-F1)** — unrelated Ebb-Tide-era follow-up; not this phase.

None of the discussion strayed outside the four phase requirements; the two simplifications (D-02a free-from-indegree, D-03 no-upper-level-execution-gate) *narrowed* scope rather than expanding it.

</deferred>

---

*Phase: 25-global-dispatch-failure-semantics-gates-resumption*
*Context gathered: 2026-06-16*
