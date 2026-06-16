# Phase 23: Schema Migration + Cross-Scope Dependency Model - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-16
**Phase:** 23-schema-migration-cross-scope-dependency-model
**Areas discussed:** Dependency reference format, Wave shape, Migration mechanics, Interface/coarse-dependency model, Refinement ownership

---

## Cross-scope dependency reference format

| Option | Description | Selected |
|--------|-------------|----------|
| Flat Task CR names | `[]string` bare Task CR names; drop same-Plan restriction; works because namespace is shared and names unique | ✓ |
| Structured qualified refs | Typed `{milestone,phase,plan,task}` ref objects | |
| Stable logical interface IDs | Label/annotation IDs decoupled from CR names | |

**User's choice:** Flat Task CR names — later generalized (see Refinement area) to flat structural IDs that may name any hierarchy level.
**Notes:** User flagged that flat-name vs. a separate interface-id namespace "does feel like two competing reference systems" and explicitly chose to use only the flat-name system, dropping interface IDs.

---

## Wave shape (SCHEMA-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Re-owned Wave CR | Wave CR re-owned to Project (`projectRef` + global monotonic `waveIndex`) | ✓ |
| Derived status view (no Wave CR) | Retire Wave CR; global waves as a derived read-only projection | |
| Need a deeper look | Discuss tradeoffs first | |

**User's choice:** Re-owned Wave CR.
**Notes:** Keeps a concrete object for dashboard/CLI to list; avoids putting the global index in `.status` where it could trip `verify-no-aggregates`. Direction (Plan→Project) was already locked in PROJECT.md.

---

## Migration mechanics (SCHEMA-03)

| Option | Description | Selected |
|--------|-------------|----------|
| v1alpha2 + reject old, drop webhook | New version served-only; controller fail-closed rejects old-shape objects; retire conversion-webhook scaffolding | ✓ |
| Mutate v1alpha1 + fail-closed guard | Keep version string, change shape, guard halts old objects | |
| Clean break + reinstall (initial framing) | Bump version, document reinstall, fail-closed | ✓ (refined into option 1) |

**User's choice:** Clean break + reinstall, with mechanics pinned to: v1alpha2 served-only, old objects rejected loudly (`RequiresReinstall`-style condition), conversion webhook removed.
**Notes:** Justified by pre-GA reality (only dogfood clusters exist). "No silent corruption" satisfied by loud fail-closed rejection rather than a conversion webhook.

---

## Interface / coarse-dependency model (DEPS-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Fan-in/fan-out expansion | Coarse edge = every dependent task waits on every depended task | (folded in as assembler fallback) |
| Declared interface points (`provides`/`requires`) | Coarse deps resolve to named interface outputs | rejected |
| Barrier nodes | Synthetic scope-completion barrier nodes | |
| Task provides + scope requires | Recommended producer/consumer split | rejected |
| Scope exposes + scope requires | Interface declared at scope level | rejected |

**User's choice:** None of the interface-id options. User chose to drop the separate interface-id namespace entirely and unify on flat structural IDs with **progressive refinement** — a `dependsOn` reference is authored at the coarsest known level (Milestone) and narrowed as planning descends (→ Phase → Plan → Task). Coarse refs still resolve via assembler fan-in/fan-out (DEPS-02 satisfied without a `provides`/`exposes` field).
**Notes:** This collapses DEPS-01 (task→task) and DEPS-02 (coarse interface) into one mechanism: a `dependsOn` entry that may name any hierarchy node. Temporal-authoring concern (higher planner can't name a not-yet-existing task) is resolved by naming the coarsest *existing* node and refining later.

---

## Refinement ownership

| Option | Description | Selected |
|--------|-------------|----------|
| Mutate-in-place | `dependsOn` rewritten on the same CRD as planning descends; many writers, racy | rejected |
| Author-per-level + resolver | Each scope authors its own coarse `dependsOn`; resolution composes the fine edges | ✓ |

**User's choice:** Author-per-level. Resolution happens at **per-plan / per-task granularity** (user's explicit correction — *not* a single global traversal). Resolution intended **in-memory** (not written back), with the final write-back-vs-in-memory mechanic deferred to Phase 24's engine.
**Notes:** User clarified depth-first planning means all tasks exist by resolution time; resolution then runs per-unit and composes, enabling O(V+E) incremental re-derivation (EXEC-04). Mutate-in-place rejected for its multi-writer / ResourceVersion-conflict hazards.

---

## Claude's Discretion

- Exact `v1alpha2` field names, kubebuilder markers, CEL constraints, printer columns, deepcopy regeneration.
- Status-condition type/reason strings for old-object rejection and cycle rejection.
- Keeping `verify-no-aggregates` / `verify-dag-imports` guards green through the schema change.
- Exact cycle-rejection validation surface (where the condition lands, when it fires) — global-scope, validation-time, node-surfacing, recovery-free is fixed; the rest is research/planner territory.

## Deferred Ideas

- Global Kahn derivation/assembly engine + bidirectional global wave index — Phase 24.
- Global dispatch / wave-boundary failure / gates-as-holds / minimal resumption — Phase 25.
- Multi-milestone drive + cross-milestone global waves + README conformance test — Phase 26.
- Write-back vs. in-memory resolution final lock — Phase 24.
- Planner-prompt discipline for correct dependency refinement — when cross-scope planners are built.
