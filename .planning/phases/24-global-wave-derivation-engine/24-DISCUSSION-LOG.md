# Phase 24: Global Wave Derivation Engine - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-16
**Phase:** 24-global-wave-derivation-engine
**Mode:** `--auto` — every area auto-resolved to the recommended option (no interactive prompts). Choices below are logged for audit.
**Areas discussed:** Assembler location & planning-complete trigger, Coarse-ref fan-out resolution, Wave CR ownership & lifecycle, Bidirectional global wave index, Re-derivation triggers & no-cache idempotency

---

## Assembler location & planning-complete trigger (EXEC-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Extend `ProjectReconciler` | Reuse the global-scope reconciler that already lists all Tasks + runs the cycle gate; derivation is the next stage | ✓ |
| New dedicated scheduler controller | Separate controller owning derivation | |

**Auto choice:** Extend `ProjectReconciler` (recommended). Ordering contract locked: assemble → cycle-check → derive → dispatch. Exact planning-complete signal left to research/planner.
**Notes:** ProjectReconciler already owns `assembleProjectDepGraph` + `checkGlobalCycleGate`; a second controller would duplicate Task listing and add a second Wave writer. Per-plan `materializeWaves` is removed so there is exactly one Wave writer (D-03).

---

## Coarse-ref fan-out resolution (EXEC-01/02; completes Phase 23 D-06)

| Option | Description | Selected |
|--------|-------------|----------|
| In-memory fan-out at assembly | Expand Plan/Phase/Milestone refs to task-level fan-in via labels; never written back | ✓ |
| Write resolved edges back to CRDs | Persist resolved task edges | |

**Auto choice:** In-memory fan-out (recommended) — locks Phase 23 D-05's deferred mechanic.
**Notes:** Write-back is forbidden by the locked "no cached schedule / re-derive" constraint and `verify-no-aggregates`. Not a genuine fork — constraints leave only the in-memory option. Un-refined coarse refs fan out conservatively (D-06).

---

## Wave CR ownership & re-derivation lifecycle (EXEC-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Project-owned `tide-wave-<project>-<globalIndex>`, reconciled set with prune | One Wave per global wave, prune stale extras on re-derivation | ✓ |
| Keep per-plan Wave keying | Plan-owned `tide-wave-<plan.UID>-<i>` | |

**Auto choice:** Project-owned global Waves with set reconciliation + prune (recommended).
**Notes:** v1alpha2 `WaveSpec{ProjectRef, WaveIndex}` already supports this. Exactly-once-on-Create metric semantics preserved; prune mechanic is discretion, the "persisted set == current derivation" invariant is locked.

---

## Bidirectional global wave index (EXEC-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Label-indexed (re-point existing contract to global index) | `tideproject.k8s/wave-index` per Task + label-selector for wave→tasks | ✓ |
| `Task.status.wave` field / Project-level wave→tasks map | Status field or aggregate map | |

**Auto choice:** Label-indexed, re-pointed to the global index (recommended).
**Notes:** The per-plan `stampTaskLabels` contract already exists and is read by Wave/Task reconcilers; re-point to global. A Project-level wave→tasks map would be exactly the cached aggregate `verify-no-aggregates` forbids. A per-Task scalar label is per-object, guard stays green. Restores README:54 invariant Project-wide.

---

## Re-derivation triggers & no-cache idempotency (EXEC-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Watch Task add/complete; recompute whole schedule each reconcile, no cache | Idempotent O(V+E) re-derivation from DAG + completed-set | ✓ |
| Cache schedule, incremental patching | Store derived schedule, mutate deltas | |

**Auto choice:** Recompute-from-scratch each reconcile, nothing cached (recommended).
**Notes:** Matches locked resumption model (indegree map + completed-set only) and feeds Phase 25 dispatch without new persisted state. `pkg/dag.ComputeWaves` reused unchanged; `pkg/dag` stays k8s-free (verify-dag-imports).

---

## Claude's Discretion

- Exact planning-complete signal (Project condition vs. derived "all leaf Plans materialized").
- Delete vs. neutralize the per-plan `materializeWaves`/`stampTaskLabels` paths and the refactor mechanics.
- Wave-CR prune mechanic (delete vs. tombstone); requeue/backoff on transient List failures.
- Label keys/values, status-condition type/reason strings, printer columns.
- How fan-out resolves a scope ref to its task set (membership label) and dedup of overlapping coarse+fine edges.
- Keeping `verify-no-aggregates` / `verify-no-sqlite-dep` / `verify-dag-imports` green.

## Deferred Ideas

- Global dispatch / failure semantics / gates-as-holds / minimal resumption — Phase 25.
- Multi-milestone drive + cross-milestone shared waves + README conformance test — Phase 26.
- Planner-prompt discipline for correct dependency refinement — when cross-scope planners are built.
- `cache-f1-direct-sdk-cross-pod-caching.md` todo — reviewed, not folded (off-domain, superseded Ebb Tide scope).
