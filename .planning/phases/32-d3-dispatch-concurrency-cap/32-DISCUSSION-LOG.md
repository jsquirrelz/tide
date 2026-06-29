# Phase 32: D3 — Dispatch Concurrency Cap - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-28
**Phase:** 32-d3-dispatch-concurrency-cap
**Areas discussed:** Mandatory design fork (resolved by code), Cap mechanism, Default value, Cap scope, Observability

---

## Mandatory design fork (Option A vs B)

Resolved by direct code read before discussion — not put to the user as a choice, because it is a code fact, not a preference. `defer r.PlannerPool.Release()` fires on reconcile return (ms after `r.Create`), so the semaphore caps concurrent Job-creation reconciles, not in-flight running pods. **Option B locked** (live `client.List` count-check + `RequeueAfter`). The ROADMAP's kubectl experiment is no longer a planning prerequisite.

---

## Cap mechanism (semaphore's role under Option B)

| Option | Description | Selected |
|--------|-------------|----------|
| Replace pool with List-gate | List-count becomes sole in-flight cap | |
| Keep both (List-gate primary) | Semaphore as secondary thundering-herd guard | |
| Decide in planning | Capture as research question (RQ-1) | ✓ |

**User's choice:** Decide in planning.
**Notes:** Delegated to planner; tradeoff is single-source-of-truth vs per-dispatch List cost.

---

## Default value (CONCUR-02)

| Option | Description | Selected |
|--------|-------------|----------|
| 4 | Research suggestion; single-node-comfortable | |
| 2 | Conservative floor | |
| Decide in planning | Planner picks vs measured per-pod footprint (RQ-2) | ✓ |

**User's choice:** Decide in planning.
**Notes:** Must move binary default (`config.go:117`) and chart default (`values.yaml`) together; remove `16`.

---

## Cap scope/topology

| Option | Description | Selected |
|--------|-------------|----------|
| Global planner cap | One cap across all four levels (matches today's shared pool) | |
| Per-level cap | Independent caps per level | |
| Decide in planning | Capture as research question (RQ-3) | ✓ |

**User's choice:** Decide in planning.
**Notes:** Lean global to match the single shared `plannerPool`; also resolve List selector + namespace scope.

---

## Observability (CONCUR-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Log line only | Minimum requirement | |
| Log + Prometheus metric | Dashboard-visible deferred-dispatch/in-flight metric | |
| Decide in planning | Planner weighs cardinality analyzer + metric conventions (RQ-4) | ✓ |

**User's choice:** Decide in planning.

---

## Claude's Discretion

All four implementation sub-decisions (RQ-1..RQ-4) were explicitly delegated to the planner. The load-bearing fork was resolved by Claude from code evidence and locked as Option B.

## Deferred Ideas

- Per-level planner caps (future, only if independent tuning is needed).
- Dashboard "stalled wave" visualization beyond a metric (v2 observability).
