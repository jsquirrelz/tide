# Phase 26: Multi-Milestone Drive + Spec Conformance - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-17
**Phase:** 26-multi-milestone-drive-spec-conformance
**Areas discussed:** Multi-milestone drive model, Per-milestone gate policy, SPEC-01 conformance test, Carried-in Phase-25 debt

---

## Multi-milestone drive model — milestone authoring (MS-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Project planner emits all milestones up-front | Decompose outcome into N Milestone child-CRDs with `dependsOn`; one planning pass then global execution | ✓ |
| Milestone-succession chain | Emit one milestone; author the next on completion; sequential | |
| Consume hand-authored Milestone CRDs | Planner stays single-milestone; drive = execute pre-applied CRDs | |

**User's choice:** Project planner emits all milestones up-front.
**Notes:** Matches README (whole tree planned before execution) + Phase 24 EXEC-01.

## Multi-milestone drive model — milestone edge semantics (MS-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Planning-order only — no execution fan-out | `Milestone.dependsOn` governs authoring order/gate descent only; cross-milestone execution via explicit task edges | ✓ |
| Interface dep — fans out to member tasks | MA→MB makes all of B depend on all of A; breaks README | |
| You decide / discuss | — | |

**User's choice:** Planning-order only — no execution fan-out.
**Notes:** Confirmed by observing `depgraph.go:258 §6d` currently fans milestone deps into execution edges.

## Multi-milestone drive model — DEPS-02 reconciliation

| Option | Description | Selected |
|--------|-------------|----------|
| Remove §6d — planning-order only (Option A) | Delete milestone execution fan-out; keep plan/phase; reinterpret DEPS-02 + README note | ✓ |
| Keep §6d (Option B) | Milestone fans out; fixture avoids `Milestone.dependsOn`; accepts foot-gun | |
| Remove §6d AND revisit §6b/6c | Treat all coarse fan-out as suspect; re-opens Phase 23/24 | |

**User's choice:** Remove §6d — planning-order only (Option A). (User requested a contrived worked example with pros/cons before deciding; provided — Backend/Frontend milestones showing scaffold-parallelism difference — then chose A.)
**Notes:** Preserves "two distinct DAGs" and MS-02 cross-milestone wave sharing; milestone too coarse for all-to-all.

---

## Per-milestone gate policy (MS-03) — mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Project-level Gates already suffice | `gates.milestone:approve` fires once per milestone via approve-at-descent; MS-03 = conformance test | ✓ |
| Add per-Milestone gate override field | `Milestone.Spec.Gates` overrides project default; more expressive; schema change | |
| You decide / discuss | — | |

**User's choice:** Project-level Gates already suffice.
**Notes:** full-auto = `gates.milestone:auto`; full-supervised = `gates.task:approve`.

## Per-milestone gate policy (MS-03) — gate timing

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — planning-time hold only | Approve scope before phases authored; no execution-time milestone review | ✓ |
| Execution-time milestone review too | Pause execution at milestone boundaries; conflicts with cross-milestone waves | |
| You decide / discuss | — | |

**User's choice:** Yes — planning-time hold only.
**Notes:** Consistent with Phase 25 D-03 and the previously declined execution-boundary gate.

---

## SPEC-01 conformance test — surface

| Option | Description | Selected |
|--------|-------------|----------|
| envtest with real CRDs (full stack) | Apply 2-milestone α…θ hierarchy incl. γ→η; assert global waves | ✓ |
| Unit test against derivation engine | In-memory graph → `deriveGlobalWaves`; algorithm-only | |
| Both layers | Unit guard + envtest | |

**User's choice:** envtest with real CRDs — **plus** a new directive: screenshot the dashboard render of the fixture and replace the README flowcharts with it (edges left-to-right / ReactFlow LR; visuals represent the same concept).
**Notes:** Visual conformance added to SPEC-01.

## SPEC-01 conformance test — which README diagrams to replace

| Option | Description | Selected |
|--------|-------------|----------|
| Both diagrams | Planning ← PlanningDAGView; Execution ← new global execution-DAG view | ✓ |
| Execution graph only | Replace just the wave-schedule mermaid | |
| You decide / discuss | — | |

**User's choice:** Both diagrams.
**Notes:** Observed that today's `ExecutionDAGView` is per-Plan — Phase 26 must build/extend a global execution-DAG view before screenshotting. Adds frontend scope.

---

## Carried-in Phase-25 debt — OQ-3 wave-prune guard

| Option | Description | Selected |
|--------|-------------|----------|
| Proper fix — distinguish zero-member from real-Running | Fix the wave aggregator + add prune guard; keep CR-01 PruneShrink green | ✓ |
| Leave as display-only artifact | wontfix; dispatch unaffected; accept display flicker | |
| You decide / discuss | — | |

**User's choice:** Proper fix.
**Notes:** Root fix in the wave aggregator; Wave CRs are display-only but fixed correctly.

## Carried-in Phase-25 debt — WR-02 watch predicate

| Option | Description | Selected |
|--------|-------------|----------|
| Add the predicate — fire only on meaningful changes | Event predicate for status-phase / dependsOn changes | ✓ |
| Defer WR-02 again | Punt the perf-only fix | |
| You decide / discuss | — | |

**User's choice:** Add the predicate.
**Notes:** Pairs with OQ-3 aggregator work in the same plan.

---

## Claude's Discretion

- `project_planner.tmpl` N-milestone decomposition prompt wording (Opus-4.x literal-instruction scope).
- Whether to add a fast in-memory `deriveGlobalWaves` unit guard alongside the required envtest.
- Global execution-DAG dashboard view's data source/shape and dagre LR wiring.
- Exact README spec-text edits for the milestone-edge reinterpretation.

## Deferred Ideas

- Per-Milestone gate override field (`Milestone.Spec.Gates`).
- Per-scope (milestone/phase) conservative `FailureProfile` granularity.
- Execution-time milestone boundary gate (re-declined).
- Revisiting Plan/Phase coarse fan-out (§6b/6c) as foot-guns.
- OpenAI/Codex subagent backend + dogfood run #2 (next milestone).
