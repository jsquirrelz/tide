# Requirements: TIDE v1.0.2 — Spring Tide (Global Execution DAG)

**Defined:** 2026-06-16
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.

**Milestone goal:** Re-architect execution so waves are derived from ONE global Execution DAG spanning the entire Project (all milestones/phases/plans), assembled after planning completes — making the Topologically-Indexed paradigm real. This is a **severe corrective patch**: v1.0.0/v1.0.1 shipped a per-plan-waves execution layer that never implemented the spec's global Execution DAG (`Plan` has no deps, `Task.dependsOn` is plan-local per D-F1, waves are per-plan via `materializeWaves`, and there is no global indegree map). Supersedes the unreleased Ebb Tide scope (archived: `milestones/v1.0.2-ebb-REQUIREMENTS.md`).

**Binding constraints (apply to every requirement below):**
- The spec is the contract: README "two distinct DAGs" + the execution-graph worked example (waves spanning plans/phases/**milestones**, cross-cutting edges) is the literal target. Update the spec first only where a real implementation pressure forces a paradigm change.
- Preserve the wave-boundary failure contract EXACTLY (spec §"Failure handling at wave boundaries") at global scope.
- Resumption state stays minimal: ONE global indegree map + completed-task set — nothing more (no cached schedule; re-derive O(V+E)).
- Cycles are bugs, not runtime conditions — reject a cyclic global DAG at validation, surface involved nodes; no recovery features.
- Breaking CRD changes ship a migration/conversion path; no silent corruption of in-flight Projects.
- Keep human-gate policy out of the controller — gates compose as *holds* over the global scheduler; approve-every-milestone, fully-autonomous, and fully-supervised all remain expressible.

## Milestone v1.0.2 Requirements

Requirements for this milestone. Each maps to exactly one roadmap phase.

### Global Execution DAG (EXEC)

- [x] **EXEC-01**: The orchestrator assembles ONE global Execution DAG of all Tasks across all Milestones/Phases/Plans in a Project, once project planning completes, before any execution dispatch.
- [x] **EXEC-02**: Waves are derived by layered Kahn over the GLOBAL task DAG; wave indices are global (a single monotonic schedule), not per-plan.
- [x] **EXEC-03**: The global wave index is queryable both directions — given any Task you resolve its global wave; given any wave you list its Tasks (restores the README:54 namesake invariant).
- [x] **EXEC-04**: Waves re-derive on every task add/complete in O(V+E) with no cached schedule (PERSIST-03), spanning the whole Project.

### Cross-Scope Dependencies (DEPS)

- [x] **DEPS-01**: A Task can declare dependencies on Tasks in other Plans, Phases, AND Milestones (retire plan-local D-F1) via qualified references resolved into the global DAG.
- [x] **DEPS-02**: Plan-, Phase-, and Milestone-level interface dependency declarations are reconciled into the global task DAG (coarse interface edges resolve to / coexist with task-level edges). **Phase 26 D-03 reinterpretation:** coarse fan-out for Plan (§6b) and Phase (§6c) scope is retained; Milestone-level all-to-all fan-out (§6d) was removed as too coarse a coupling unit — `Milestone.dependsOn` is a planning-DAG edge only (zero execution edges). DEPS-02 remains Complete (Phase 23); the fan-out scope is now §6b/§6c rather than §6b/§6c/§6d.
- [x] **DEPS-03**: A cyclic global Execution DAG is rejected at validation time with involved nodes surfaced — across plan/phase/milestone boundaries; no runtime cycle recovery.

### Global Dispatch & Failure Semantics (DISP)

- [x] **DISP-01**: A Task dispatches only when ALL its global dependencies are complete (global indegree 0 vs the completed-task set), regardless of authoring Plan/Phase/Milestone.
- [x] **DISP-02**: Wave-boundary failure semantics hold EXACTLY at global scope — failed task → independent siblings in the same global wave continue; global dependents never dispatch; non-dependents dispatch in strict / halt in conservative.
- [x] **DISP-03**: Gates (milestone/phase/plan/task approve) compose with the global scheduler as *holds* — a gate withholds a globally-ready Task until approved; approval releases it without bypassing dependency readiness or human-gate-policy configurability.

### Multiple Milestones (MS)

- [ ] **MS-01**: A Project drives MULTIPLE Milestones end-to-end via the Milestone DAG (`Milestone.dependsOn`, schema-present today, never exercised) — planning emits a milestone DAG and all milestones' Tasks join the single global Execution DAG.
- [ ] **MS-02**: Cross-milestone global waves — a Task in one Milestone may share a global wave with a Task in another (the literal README execution example); cross-milestone task dependencies are expressible and honored.
- [ ] **MS-03**: Milestone-level gate policy composes across the Milestone DAG (approve-every-milestone works for N milestones; full-auto and full-supervised remain expressible).

### Minimal Resumption (RESUME)

- [x] **RESUME-01**: An orchestrator restart re-derives the entire Project execution schedule from the global indegree map + completed-task set alone — no other persisted execution state.

### CRD Migration — the breaking surface (SCHEMA)

- [x] **SCHEMA-01**: Wave derivation/ownership moves from Plan to the global (Project) scope; the Wave CR (or its replacement derived status view) carries a global wave index.
- [x] **SCHEMA-02**: The `wave` telemetry label is resemanticized to the global wave; the locked metric label set `{project,phase,plan,wave}` is kept (the `task` label stays forbidden per the metriccardinality analyzer).
- [x] **SCHEMA-03**: Breaking CRD changes ship with a documented migration/conversion path and version bump; in-flight Projects are not silently corrupted.

### Spec Conformance (SPEC)

- [ ] **SPEC-01**: The README execution-DAG section and the implementation agree — the README cross-plan/cross-phase/cross-milestone worked example is encoded as an executable test producing the documented global wave schedule.

### Folded-in Fixes (FIX)

- [x] **FIX-01**: The dashboard image build embeds the current SPA (regenerate `cmd/dashboard/embed/dist` in the image/release path, or gate staleness in CI) so published images can never ship a bundle older than source — verified against the Telemetry tab rendering. (Root cause from dogfood run #2: v1.0.0/v1.0.1 dashboard images froze the embedded bundle at commit `6d7a28f`, pre-telemetry.)

## Future Requirements

Deferred to later milestones:
- **OpenAI/Codex subagent backend + dogfood run #2** — gated on this milestone landing a correct execution layer (was the "vNext" milestone).
- **Polyglot subagent runtimes (LangGraph)** — backlog, unchanged ([framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md)).

## Out of Scope

- Re-deriving cost/pricing/metrics correctness — already correct; only the `wave` label semantics change.
- Cycle "recovery" features — cyclic DAGs are rejected, not repaired.
- Caching the global schedule — re-derivation is intentional and cheap (O(V+E)).
- Heterogeneous wave-internal sub-scheduling (CPM/HEFT) — out; layered Kahn stays.

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| FIX-01 | Phase 22 | Complete |
| SCHEMA-01 | Phase 23 | Complete |
| SCHEMA-02 | Phase 23 | Complete |
| SCHEMA-03 | Phase 23 | Complete |
| DEPS-01 | Phase 23 | Complete |
| DEPS-02 | Phase 23 | Complete |
| DEPS-03 | Phase 23 | Complete |
| EXEC-01 | Phase 24 | Complete |
| EXEC-02 | Phase 24 | Complete |
| EXEC-03 | Phase 24 | Complete |
| EXEC-04 | Phase 24 | Complete |
| DISP-01 | Phase 25 | Complete |
| DISP-02 | Phase 25 | Complete |
| DISP-03 | Phase 25 | Complete |
| RESUME-01 | Phase 25 | Complete |
| MS-01 | Phase 26 | Pending |
| MS-02 | Phase 26 | Pending |
| MS-03 | Phase 26 | Pending |
| SPEC-01 | Phase 26 | Pending |

**Coverage:** 19/19 v1.0.2 Spring Tide requirements mapped to exactly one phase. No orphans, no duplicates.
