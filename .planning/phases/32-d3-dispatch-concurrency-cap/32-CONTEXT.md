# Phase 32: D3 — Dispatch Concurrency Cap - Context

**Gathered:** 2026-06-28
**Status:** Ready for planning

<domain>
## Phase Boundary

Bound in-flight planner Jobs at steady state by a configurable cap (`plannerConcurrency`) so the planning cascade cannot OOM a single-node cluster. Excess dispatches park/requeue rather than being silently dropped; planner and executor pools remain separately sized (spec contract); the default cap is lowered from `16` to a single-node-safe value documented in `charts/tide/values.yaml`.

**In scope:** CONCUR-01..04 — the in-flight cap mechanism, the reduced default, pool-separation preservation, and observable deferral. All `values.yaml` / pool / planner-dispatch changes for the milestone live here.

**Out of scope (other phases):** D2/D1 adoption seam (Phase 31, complete), D4 planner failure semantics (Phase 33). No new CRDs, no new dependencies, no new persistence surface (milestone constraint).
</domain>

<decisions>
## Implementation Decisions

### Mandatory design fork — RESOLVED BY CODE (load-bearing)
- **D-01 (Option B is locked):** The cap must be implemented as a **live `client.List` in-flight count-check before pool acquire at each planner dispatch site, returning `ctrl.Result{RequeueAfter}` when the running-planner count ≥ cap.** Option A (just lower `plannerConcurrency` in `values.yaml`, no controller change) is **incorrect** and does not satisfy CONCUR-01.

  **Evidence (direct code read, not the kubectl experiment the ROADMAP proposed):** In every planner controller the dispatch flow is `PlannerPool.Acquire` → `defer PlannerPool.Release()` → build envelope → `r.Create(ctx, job)` → `r.Status().Patch(Phase=Running)` → `return ctrl.Result{}, nil`. `r.Create` writes the Job object and returns immediately; the planner pod runs **asynchronously** afterward. There is **no blocking wait** for the pod to reach terminal state, so `defer Release()` fires on the reconcile return — milliseconds after `Create`. The semaphore therefore caps *concurrent Job-creation reconciles* (a near-instantaneous window), **not in-flight running pods**. This is exactly the Option B reading from 3 of 4 research subagents. The fork is resolved; **no kubectl observation is required before planning.**

  Sites (all identical shape): `milestone_controller.go:381-385,482,503`, `phase_controller.go:379-383`, `plan_controller.go:385-389`, `project_controller.go:1178-1182`.

### Architectural facts to design against (discovered during scout — treat as locked constraints)
- **D-02 (single shared planner pool today):** `cmd/manager/main.go:343` creates **one** `plannerPool := pool.New(cfg.PlannerConcurrency, "planner")` instance, passed to all four planner controllers (project/milestone/phase/plan — `main.go:445,475,501,...`). The cap today is therefore **global across planner levels**, not per-level. Any in-flight count gate inherits this topology unless deliberately changed (see RQ-3).
- **D-03 (no slot leak — preserve the existing ordering invariant):** Phase 31 D-06 and the comment at `project_controller.go:1090,1152` already enforce "park/return BEFORE `PlannerPool.Acquire` — acquiring then parking leaks a slot." The new List-count gate MUST sit **before** any pool acquire and `return RequeueAfter` without acquiring. Do not regress this.
- **D-04 (return shape):** Deferred dispatches return `ctrl.Result{RequeueAfter: ...}, nil` — never a Go `err` (mirrors `failure_halt.go` / `billing_halt.go` / the existing import-hold at `milestone_controller.go:375`, which already uses `RequeueAfter: 5 * time.Second`). Never silently drop/truncate a wave (CONCUR-04).
- **D-05 (pools stay separate — CONCUR-03):** The executor pool (`executorConcurrency`) is untouched; the cross-pool analyzer (`tools/analyzers/crosspool`, run by `make lint`) must stay green. The cap does not unify the two pools.

### Claude's Discretion — delegated to research/planning (user chose "Decide in planning" on all four)
The user explicitly delegated the four implementation sub-decisions below to the planner. Each carries a starting-point analysis; the planner should confirm against actual reconcile/List cost and existing conventions, not treat them as locked.

- **RQ-1 (semaphore's fate under Option B):** Does the List-count gate **replace** the in-process `PlannerPool` semaphore on the planner path, or do both coexist (List-gate = steady-state cap, semaphore = cheap thundering-herd guard on concurrent Creates)? Tradeoff: single-source-of-truth simplicity vs a List on every capped dispatch. *Lean: resolve against measured List cost; if the gate already runs every reconcile, a redundant semaphore may be dead weight — but removing it touches `main.go` wiring + `task_controller` shares the pattern.*
- **RQ-2 (default value, CONCUR-02):** Canonical replacement for `16`. *Starting point: `4` (research suggestion) for a single-node kind cluster — each planner pod is subagent + credproxy sidecar; leave headroom for the executor pool + system pods. `2` is the conservative floor. Current default is set at `internal/config/config.go:117` (`resolveField("plannerConcurrency", ..., 16, ...)`) AND must be reflected in `charts/tide/values.yaml` with the `16` removed (Success Criterion 2).* Confirm the binary default and the chart default move together (chart is the FIXED contract — binary catches up to chart).
- **RQ-3 (cap scope/topology):** Keep the **global** shared-pool cap (count all `role=planner` running Jobs against one cap — matches D-02) or move to **per-level** caps. *Lean: global, to match today's architecture and the milestone's "no new surface" constraint; per-level is a larger refactor with more tuning surface and is not required by CONCUR-01..04.* Also resolve the List **selector + namespace scope** (label `tideproject.k8s/role=planner`; all-namespaces vs watched-namespace) and its per-reconcile cost.
- **RQ-4 (observability depth, CONCUR-04):** Log-line-only (minimum) vs log + Prometheus metric (deferred-dispatch counter or in-flight gauge, visible on the dashboard). *Constraint: a metric must pass `tools/analyzers/metriccardinality` and follow existing `internal/metrics` conventions; the milestone forbids new persistence but a metric is not persistence.* Confirm whether a stalled wave needs dashboard visibility or logs suffice for v1.0.6.

### Carried-in hardening debt (folded into this phase per ROADMAP)
- **D-06:** Phase 31's code review (`31-REVIEW.md`) hardening items WR-01/WR-02/WR-03/WR-04 were folded into Phase 32 scope (see ROADMAP §"Phase 32 → Carried-in debt"). These are **independent** of the D3 cap work and touch the rollup/suppression paths in the controllers; the planner should scope them as their own plan(s) so they don't entangle the cap change. Primary: WR-02/03 — wrap the `*RolledUpUID` marker stamp in `RetryOnConflict` + re-fetch, mirroring `budget.RollUpUsage`.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 32: D3 — Dispatch Concurrency Cap" — goal, the (now-resolved) mandatory design fork, success criteria, and the carried-in hardening debt block.
- `.planning/REQUIREMENTS.md` §"Dispatch Concurrency Cap (D3)" — CONCUR-01..04 verbatim.
- `.planning/phases/31-.../31-REVIEW.md` — WR-01/02/03/04 hardening items (carried in via D-06).
- `.planning/phases/31-.../31-CONTEXT.md` §D-06 — the existing "no slot leak: park before Acquire" ordering invariant this phase must preserve.

### Load-bearing code
- `cmd/manager/main.go:343,445,475,501` — single shared `plannerPool` wiring across all four planner controllers (the global-cap topology).
- `internal/controller/{project,milestone,phase,plan}_controller.go` — the four identical Acquire → defer Release → Create → return dispatch sites (project at ~1178; children at ~379-389).
- `internal/pool/` — the `pool.Pool` semaphore (`Acquire`/`Release`/`New`).
- `internal/config/config.go:45,76,117` — `PlannerConcurrency` field + the `16` default (`resolveField`).
- `charts/tide/values.yaml` — the FIXED chart contract; the default-value change lands here (binary catches up to chart, never reverse).
- `internal/controller/milestone_controller.go:375` — existing `RequeueAfter: 5 * time.Second` park pattern to mirror.
- `tools/analyzers/crosspool`, `tools/analyzers/metriccardinality` — `make lint` gates the cap must not break (pool separation; metric cardinality if RQ-4 adds a metric).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/pool.Pool` — existing planner-pool semaphore; reuse or supplement per RQ-1.
- Existing `RequeueAfter` park pattern (`milestone_controller.go:375`, `failure_halt.go`, `billing_halt.go`) — the exact return shape for a capped/deferred dispatch (RequeueAfter + nil err).
- `tideproject.k8s/role=planner` Job label — the selector for counting in-flight planner Jobs via `client.List`.

### Established Patterns
- All four planner dispatch sites share one shape (Acquire → defer Release → Create → return). A single helper (e.g. `plannerInFlightAtCap(ctx) (bool, error)`) called identically at all four sites keeps the change DRY and analyzer-clean.
- "Park before Acquire" no-slot-leak ordering (Phase 31 D-06) — the gate goes ahead of the pool acquire.

### Integration Points
- The List-count gate inserts at each `Step 3: Acquire plannerPool` site, before `Acquire`.
- The default-value change spans `internal/config/config.go` (binary default) and `charts/tide/values.yaml` (chart contract) together.

</code_context>

<specifics>
## Specific Ideas

The mandatory design fork was resolved by reading the code rather than running the proposed `kubectl plannerConcurrency=2 + 5 Milestones` experiment — the `defer Release()`-fires-on-reconcile-return behavior is unambiguous in source. The planner may still use that kubectl observation as a verification gate (confirm ≤ N planner Jobs Running with the gate in place), but it is no longer a prerequisite to writing the plan.

</specifics>

<deferred>
## Deferred Ideas

- Per-level planner caps (RQ-3 alternative) — only if a future need to tune levels independently arises; out of scope for v1.0.6's single-node-safety goal.
- Dashboard "stalled wave" visualization beyond a deferred-dispatch metric — v2 observability, not required by CONCUR-04.

</deferred>

---

*Phase: 32-d3-dispatch-concurrency-cap*
*Context gathered: 2026-06-28*
