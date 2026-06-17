# Phase 26: Multi-Milestone Drive + Spec Conformance - Context

**Gathered:** 2026-06-17
**Status:** Ready for planning

<domain>
## Phase Boundary

The v1.0.2 Spring Tide closer. Make a **single Project drive multiple Milestones end-to-end** over the global Execution DAG that Phases 23–25 built, and **pin the README worked example as an executable + visual conformance test**.

Concretely:
- **MS-01:** Planning emits a **Milestone DAG** (`Milestone.dependsOn`, schema-present since Phase 23 but never exercised) and every milestone's Tasks join the ONE global Execution DAG. Today the project planner emits **exactly one** Milestone child (`internal/subagent/common/templates/project_planner.tmpl:16`) — this phase gives a Project a real path to N milestones.
- **MS-02:** A Task in one Milestone can **share a global wave** with a Task in another (the literal README example — ζ from Milestone B in Wave 1); cross-milestone task dependencies are expressible and honored.
- **MS-03:** Milestone-level gate policy composes across the Milestone DAG (approve-every-milestone for N milestones; full-auto and full-supervised stay expressible).
- **SPEC-01:** The README execution-DAG worked example (tasks α…θ; edges `α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ`; schedule `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`) is encoded as an executable test producing the documented global wave schedule, **and the README diagrams are replaced by real dashboard renders** so README and implementation agree visually.

**Plus carried-in Phase-25 debt** (deferred, non-blocking — now in scope): OQ-3 wave-prune in-flight guard (proper aggregator fix) and WR-02 watch predicate.

**Explicitly NOT this phase:** the global derivation engine (Phase 24, done); global dispatch / failure semantics / gates-as-holds / resumption (Phase 25, done); per-milestone gate *override* fields (deferred — see Deferred Ideas); OpenAI backend + dogfood run #2 (next milestone).

</domain>

<decisions>
## Implementation Decisions

> Every decision is constrained by locked direction — README "two distinct DAGs"; Phase 24 EXEC-01 ("once project planning completes, before any execution dispatch"); Phase 25 D-03 (M/P/P gates are planning-DAG holds, the Task gate is the sole execution hold); resumption = global indegree + completed-set only, no cached schedule; cycles refused not recovered; gates per-Project, never baked into the controller.

### Multi-milestone drive model (MS-01, MS-02)

- **D-01: The project planner emits ALL milestones up-front, each with `Milestone.dependsOn`, forming the Milestone DAG.** Change `project_planner.tmpl` to decompose the whole Project outcome into N Milestone child-CRDs at once (today it emits exactly one — `project_planner.tmpl:16`), wiring `dependsOn` between them. Matches the README mental model (the whole tree is planned before execution) and Phase 24 EXEC-01.
  - **Rejected — milestone-succession chain** (emit one, author the next on completion): conflicts with cross-milestone shared waves — ζ (Milestone B) could never be in Wave 1 if Milestone B isn't authored until Milestone A finishes. Breaks the README example.
  - **Rejected — consume-only** (planner stays single-milestone, drive = execute hand-authored Milestone CRDs): smaller, but doesn't make TIDE self-decompose into milestones, which is the MS-01 goal ("a Project drives multiple Milestones").

- **D-02: Plan-ALL-milestones-then-execute-globally.** All milestones are fully planned (down to Tasks) before ANY execution dispatch (Phase 24 EXEC-01). Then ONE global Execution DAG spans all milestones and waves derive across milestone boundaries. There is **no per-milestone execution pass**.

- **D-03: `Milestone.dependsOn` is PLANNING-ORDER + gate-descent ONLY — it contributes ZERO execution edges.** The Milestone DAG governs authoring order (plan A's tree before B's) and gate descent; it does **not** serialize execution. Cross-milestone *execution* coupling comes only from explicit task/plan/phase-level `dependsOn` that happen to cross milestones (the README's `γ→η`). This reproduces the README exactly (ζ free in Wave 1) and keeps the planning and execution DAGs distinct — the whole point of Spring Tide.
  - **Implementation:** **Remove `depgraph.go §6d`** (the Milestone-level execution fan-out, currently "all tasks in THIS milestone depend on all tasks in the referenced scope", `internal/controller/depgraph.go:258`). **Keep §6a (task), §6b (plan), §6c (phase) fan-out.**
  - **DEPS-02 reconciliation:** DEPS-02 (Phase 23, "Plan-, Phase-, and Milestone-level interface deps reconciled into the global task DAG") is **reinterpreted** — coarse interface fan-out stays for Plan + Phase scope; Milestone is too coarse a unit for all-to-all fan-out to ever be the intended coupling. Record this reinterpretation and add a **README/spec note** clarifying that the Milestone edge is a planning-DAG edge. (`SPEC-01` requires README ↔ implementation agreement, so the spec text must reflect this.)
  - **Worked contrast (why):** Backend milestone {a1=schema, a2=tests}, Frontend milestone {b1=scaffold, b2=wire-to-API}, real coupling only `b2→a1`. Planning-order-only → waves `[{a1,a2,b1},{b2}]` (scaffold runs in parallel). Milestone fan-out → `[{a1,a2},{b1,b2}]` (scaffold needlessly serialized). The fan-out is a foot-gun that destroys MS-02's cross-milestone wave sharing.
  - **Rejected — keep §6d** (Option B): contradicts the README, makes any `Milestone.dependsOn` silently freeze all cross-milestone execution.
  - **Rejected — also revisit §6b/6c plan/phase fan-out**: out of scope; would re-open the Phase 23/24 DEPS-02 design. Plan/phase fan-out stays as-shipped.

### Per-milestone gate policy (MS-03)

- **D-04: Project-level `Project.Spec.Gates` already delivers approve-every-milestone — NO new schema.** With all milestones authored up-front (D-01) and approve-at-descent (Phase 12), `gates.milestone: approve` fires once per milestone — each milestone's planning hold withholds *authoring its phases* until approved. N milestones → N holds, in milestone-DAG order, automatically. `full-auto` = `gates.milestone: auto`; `full-supervised` = `gates.task: approve`. MS-03 becomes a **conformance test** proving N holds compose, not a code feature.
  - **Rejected — per-Milestone gate override field** (`Milestone.Spec.Gates` overriding the Project default): more expressive ("approve the risky milestone, auto the rest") but not required by MS-03's wording; defers to a future phase. See Deferred Ideas.

- **D-05: Milestone gate = PLANNING-time hold, confirmed (not an execution-time output review).** Because all milestones' tasks interleave in one global wave schedule (D-02), there is no execution-time milestone boundary — you cannot review milestone A's *executed code* before milestone B's code is written. `approve-every-milestone` reviews each milestone's **scope/plan** before its phases are authored. Consistent with Phase 25 D-03 and the execution-boundary gate the user declined in Phase 25. The MS-03 test asserts N **planning** holds released by the approve-milestone annotation, then one global execution pass.

### SPEC-01 conformance test + visual deliverable

- **D-06: SPEC-01 is an envtest with REAL CRDs (full stack), not a unit test.** Apply the actual 2-milestone hierarchy (Project + 2 Milestones with `dependsOn` + phases + plans + 8 Tasks α…θ carrying cross-scope `dependsOn`, **including the cross-milestone edge γ→η**), let the real reconcilers assemble + derive, and assert the derived Project-scoped Wave CRs / global wave-index labels equal `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`. Proves MS-01/MS-02 + EXEC end-to-end, not just the algorithm.
  - **Textual conformance:** the test fixture is pinned to the README §"Wave computation" example; assert the literal schedule and cross-link the test from the README so the worked example and the test cannot drift.
  - **Rejected — unit-only** (build in-memory graph, call `deriveGlobalWaves`): doesn't exercise cross-scope ref resolution, multi-milestone assembly, or CRD wiring; weaker proof the shipped orchestrator agrees with the README. (A fast unit guard MAY be added alongside at the planner's discretion, but envtest is the required surface.)

- **D-07: The dashboard render of the SPEC-01 fixture REPLACES BOTH README mermaid diagrams.** Planning graph ← `PlanningDAGView` (already renders M→P→P→T containment, dagre). Execution graph ← a **global** execution-DAG view. Screenshots replace both mermaid blocks; **edges flow LEFT-TO-RIGHT** (dagre `rankdir: LR`) to match the README execution-graph orientation; the visuals must represent the same concept as the current diagrams.
  - **Scope consequence (flagged):** today `ExecutionDAGView` is **per-Plan** (`dashboard/web/src/App.tsx:359`, takes `planName`/`plan`); `RunningWavesView` is a wave aggregate. Spring Tide globalized waves but the dashboard is still plan-scoped. Phase 26 must **extend/add a global execution-DAG view** that renders the whole-Project α…θ wave DAG before it can be screenshotted. This adds **frontend scope** to Phase 26 (consider whether `/gsd:ui-phase 26` is warranted; deliverable is constrained — "match the README concept").
  - **Staleness tradeoff (accepted):** static screenshots don't re-render like inline mermaid and need committing as image assets. Accepted because the point is to prove the implementation produces the documented picture (visual conformance).
  - **Execution detail for the planner:** screenshots require a running cluster + dashboard with the fixture applied (the durable `kind-tide-dogfood` cluster exists). This is a live-cluster step, not pure CI.

### Carried-in Phase-25 debt

- **D-08: OQ-3 — proper fix.** Fix the wave aggregator to distinguish a **zero-member wave** (display-marked `Running`) from a **wave with real in-flight (`Running`) tasks**, then add the prune guard that blocks pruning only waves with actual in-flight tasks. Must keep the pre-existing CR-01 `PruneShrink` regression test green (the naive `skip if Wave.Status.Phase != "Succeeded"` guard broke it in Phase 25, commits `2a97a7a`→`e7c14f7`). Wave CRs are display artifacts (`computeGlobalIndegree` reads Task `.status` only), so this is correctness-of-display, not of dispatch — but fixed at root.
  - **Rejected — leave as display-only / wontfix:** zero code, but transient wave-display flicker during re-derivation persists; user chose the root fix.

- **D-09: WR-02 — add the watch predicate.** Give `globalDependentsMapper`'s Task watch an event predicate so it fires only on status-phase transitions or `dependsOn` changes (ignore no-op / resourceVersion-only updates). Bounded perf fix; pairs naturally with the OQ-3 aggregator work in the same plan. The mapper is idempotent today — this removes wasteful full-re-derivation churn.

### Claude's Discretion
- Exact `project_planner.tmpl` decomposition prompt wording (how the planner is instructed to split an outcome into N milestones + wire `dependsOn`) — template-authoring territory; honor Opus-4.x literal-instruction guidance (state "emit one Milestone child-CRD **per milestone in the DAG**, each with its `dependsOn`" explicitly).
- Whether to add a fast in-memory `deriveGlobalWaves` unit guard alongside the required envtest (D-06).
- The global execution-DAG dashboard view's data source/shape (extend `ExecutionDAGView` vs `RunningWavesView` vs new component) and the dagre LR layout wiring — research/UI-spec territory; `applyDagreLayout(nodes, edges, "LR")` already exists (`PlanningDAGView.tsx:298`).
- Exact README spec-text edits for the Milestone-edge reinterpretation (D-03) — must land so SPEC-01's "README and implementation agree" holds.
- Keeping `make verify-no-aggregates` / `verify-no-sqlite-dep` / `verify-dag-imports` / `verify-dashboard-freshness` green; the locked `{project,phase,plan,wave}` metric label set unchanged.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spec (load-bearing — the implementation must conform; SPEC-01 demands agreement)
- `README.md` §"Abstract visualization" (≈ lines 87–161) — the **two mermaid diagrams** (planning containment graph + execution wave graph) that D-07 replaces with dashboard screenshots; note MA→MB is a planning edge while ζ (Milestone B) is in execution Wave 1.
- `README.md` §"Wave computation — the topological sort" (≈ lines 163–222) — the **exact worked example** SPEC-01 pins: tasks α…θ, edges `α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ`, indegree table, schedule `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`.
- `README.md` §"two distinct DAGs" (Planning DAG vs Execution DAG, ≈ lines 80–85, 237, 270) — the distinction D-03 preserves (Milestone edge = planning DAG only). **This section must be edited** to clarify the milestone-edge reinterpretation.
- `README.md` §"Failure handling at wave boundaries" — unchanged contract; Phase 26 must not disturb it.

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` — authoritative requirement→phase table. **Phase 26 owns MS-01, MS-02, MS-03, SPEC-01.** Note the DEPS-02 reinterpretation (D-03) touches a requirement marked Complete under Phase 23 — record the reinterpretation, don't silently diverge.
- `.planning/ROADMAP.md` §"Phase 26" + "Carried-in debt from Phase 25" — phase boundary and the OQ-3 / WR-02 debt this phase folds in.
- `.planning/PROJECT.md` §"Current Milestone: v1.0.2 Spring Tide" — locked milestone decisions (global dispatch off one indegree map + completed-set; no cached schedule; cycles refused; gates per-Project).

### Prior-phase hand-offs (this phase builds on their schema + engine)
- `.planning/phases/25-global-dispatch-failure-semantics-gates-resumption/25-CONTEXT.md` — D-03 (M/P/P gates are planning-DAG holds; Task gate sole execution hold — D-05 here confirms it), the declined execution-boundary gate (Deferred), per-milestone `FailureProfile`/gate as the explicit Phase-26 extension.
- `.planning/phases/24-global-wave-derivation-engine/24-CONTEXT.md` — global wave derivation (`deriveGlobalWaves`, Project-scoped Wave CRs `tide-wave-<project>-<N>`, global wave-index label, O(V+E) re-derive, no cached schedule). The SPEC-01 envtest asserts against this.
- `.planning/phases/23-schema-migration-cross-scope-dependency-model/23-CONTEXT.md` — `dependsOn` on every level / any-level targets (D-F1 retired); the assembler collapsing DEPS-01+02 via fan-out (the §6d this phase removes).

### Current code being extended/replaced
- `internal/controller/depgraph.go` — **§6d (~line 258) milestone fan-out → REMOVE** (D-03); keep §6a/6b/6c; `scopeResolver`/`resolveScope` shared with indegree + wave derivation.
- `internal/controller/project_controller.go` — `assembleProjectDepGraph` (~264) and the multi-milestone authoring / boundary path (`countChildMilestones` ~891, `BoundaryDetected` ~907, the up-front Milestone child creation ~956–962 that today guards a single milestone — D-01 widens this).
- `internal/subagent/common/templates/project_planner.tmpl` — **emits exactly one Milestone today (line 16) → emit N with `dependsOn`** (D-01). Update goldens `internal/eval/testdata/goldie/project_planner.golden` and ratchet `internal/eval/testdata/ratchets/project_planner.txt` in the same commit.
- `internal/controller/wave_controller.go` + the wave aggregator — OQ-3 zero-member-vs-real-Running distinction + prune guard (D-08); preserve CR-01 `PruneShrink`.
- `internal/controller/task_controller.go` — `globalDependentsMapper` watch needs an event predicate (D-09, WR-02).
- `internal/gates/` (`policy.go` `EvaluatePolicy`/`DefaultGates`, `annotation.go` `CheckApprove`/`ConsumeApprove`) + `milestone_controller.go` gate-descent — MS-03 conformance composes over these unchanged (D-04).
- `api/v1alpha2/milestone_types.go` — `DependsOn` (~line 38), the any-level self-cycle CEL guard (~69) the Milestone DAG uses.
- Dashboard: `dashboard/web/src/components/ExecutionDAGView.tsx` (per-plan today), `RunningWavesView.tsx`, `PlanningDAGView.tsx` (LR dagre, `applyDagreLayout(...,"LR")` ~298), `dashboard/web/src/lib/layout.ts` — D-07 global execution view + screenshots.

### Guards that must stay green
- `Makefile`: `verify-no-aggregates`, `verify-no-sqlite-dep`, `verify-dag-imports`, `verify-dashboard-freshness` (Phase 22 — the dashboard view change must keep the embed fresh).
- `internal/metrics/registry.go` — locked `{project,phase,plan,wave}` label set; `task` label forbidden (cardinality analyzer).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Approve-at-descent gate machinery** (`internal/gates`, milestone/phase/plan reconcilers) — MS-03 (D-04) composes N milestone holds over this unchanged; no new gate code.
- **`deriveGlobalWaves` + Project-scoped Wave CRs + global wave-index label** (Phase 24) — the SPEC-01 envtest (D-06) asserts directly against these.
- **`depgraph.go` shared `scopeResolver`/fan-out** — §6a/6b/6c stay; §6d removed (D-03). The shared resolver guarantees dispatch + wave derivation can't disagree.
- **`PlanningDAGView` containment render + `applyDagreLayout(nodes, edges, "LR")`** (`PlanningDAGView.tsx:298`) — the planning-graph screenshot (D-07) reuses this; the LR helper already exists for the execution-graph orientation.
- **CR-01 `PruneShrink` regression test** — the green guardrail the OQ-3 fix (D-08) must not break.

### Established Patterns
- **No cached schedule / re-derive O(V+E)** (`verify-no-aggregates`, PERSIST-03) — the SPEC-01 test and any new view read derived state; nothing new persisted.
- **Two distinct DAGs** (planning vs execution) — D-03 enforces this at the milestone boundary; the foundational invariant of the whole milestone.
- **Gate policy read from `Project.Spec.Gates`, never baked in** — MS-03 (D-04) honors it; no per-milestone override added.
- **Wave CRs are display artifacts** (`computeGlobalIndegree` reads Task `.status` only) — frames OQ-3 (D-08) as display-correctness, decoupled from dispatch.

### Integration Points
- `project_planner.tmpl` single-milestone emission → N-milestone DAG emission (D-01) — the single most important behavioral change; ripples to goldens/ratchets and the project_controller authoring guard.
- `depgraph.go §6d` removal (D-03) — drops milestone→task execution fan-out; verify no other call site depends on it.
- New global execution-DAG dashboard view (D-07) — connects to the Project-scoped Wave CRs / global wave labels; must keep `verify-dashboard-freshness` green (regenerate `cmd/dashboard/embed/dist`).
- `globalDependentsMapper` predicate (D-09) + wave-aggregator prune guard (D-08) — both in the reconcile/watch layer; can land in one plan.

</code_context>

<specifics>
## Specific Ideas

- The README diagrams should become **real dashboard screenshots** of the SPEC-01 fixture — the implementation literally renders the documented picture. Edges left-to-right (dagre LR) to match the README execution-graph concept.
- The SPEC-01 fixture is the canonical README example verbatim: α…θ, edges `α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ`, asserting `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]`, with `γ→η` as the load-bearing cross-milestone edge.
- "Approve every milestone" = the human reviews each milestone's **scope/plan** at descent; there is no execution-time milestone checkpoint (D-05).

</specifics>

<deferred>
## Deferred Ideas

- **Per-Milestone gate override field** (`Milestone.Spec.Gates` overriding the Project default — "approve the risky milestone, auto the rest") — considered, not required by MS-03; natural future extension once a real need appears.
- **Per-scope (milestone/phase) conservative `FailureProfile` granularity** — flagged in Phase 25 as a Phase-26 candidate; not pulled in (MS-03 needs only gate-policy composition, and the spec halts non-dependents project-wide). A later extension once per-milestone policy exists.
- **Execution-time milestone boundary gate** (the declined "slack tide" project-level execution-release checkpoint, Phase 25 Deferred) — re-declined here (D-05); it conflicts with cross-milestone shared waves. Composes cleanly on top if a supervised-run need ever revives it.
- **Revisiting Plan/Phase coarse fan-out (§6b/6c) as foot-guns** — out of scope; would re-open DEPS-02. Only milestone fan-out (§6d) is removed.
- **OpenAI/Codex subagent backend + dogfood run #2** — next milestone, gated on Spring Tide landing.

None of the discussion strayed outside the four phase requirements (MS-01/02/03, SPEC-01) plus the explicitly carried-in debt (OQ-3, WR-02). The one scope addition — replacing the README mermaid with dashboard screenshots (D-07) — is in direct service of SPEC-01 ("README and implementation agree").

</deferred>

---

*Phase: 26-multi-milestone-drive-spec-conformance*
*Context gathered: 2026-06-17*
