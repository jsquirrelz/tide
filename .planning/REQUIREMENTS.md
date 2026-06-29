# Requirements: TIDE v1.0.6 — Adoption-Path Correctness & Dispatch Safety

**Defined:** 2026-06-28
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.

**Milestone framing:** A corrective patch closing the four code-level defects (D1–D4) dogfood run #2b surfaced on the v1.0.5 import/adoption path. All four are confirmed must-ship — none are deferrable without leaving run #2b's failure modes in production (research SUMMARY.md §Expected Features). No new dependencies, no new CRDs, no new persistence surface: every fix is a narrow seam edit on existing controller code.

## v1 Requirements

Requirements for the v1.0.6 patch release. Each maps to exactly one roadmap phase.

### Adoption Lifecycle & Cost Rollup (D1 + D2)

The single lifecycle seam in `reconcileProjectPlannerDispatch` — D2 (lifecycle advance) is a prerequisite for D1 (budget rollup); they ship together. This closes the "spent blind" failure that undermines the budget-safety guarantee of the entire v1.0.x line.

- [x] **ADOPT-01**: An adopted/imported Project advances from `Initialized` to `Running` after `ImportComplete=True`, without dispatching a project-planner Job. *(D2)*
- [x] **ADOPT-02**: An adopted Project accrues `costSpentCents` and token usage as its milestone/phase/plan planners complete (the budget rollup fires under the adoption path, not only the normal lifecycle). *(D1)*
- [x] **ADOPT-03**: The metered `budget.absoluteCapCents` halt enforces on an adopted Project — the budget gate evaluates once the Project is `Running` and halts the fan-out when spend crosses the cap. *(D1)*
- [x] **ADOPT-04**: Budget rollup is exactly-once at every level across halt→resume and reporter-Job TTL-GC — no double-counting after a reporter Job is garbage-collected (extend the Phase-27 `RolledUpUID` durable-marker pattern to milestone/phase levels if absent; preserve the D-11 project-planner suppression exactly). *(D1 idempotency)*
- [x] **ADOPT-05**: The normal (non-import) Project lifecycle is unchanged, and the suppressed project-planner is never re-dispatched on an informer-cache miss after a manager restart (durable adoption sentinel in `.status`, not a live List alone). *(D2 no-regression)*

### Dispatch Concurrency Cap (D3)

A per-level max-in-flight cap that prevents the cascade from OOM'ing a single node. The fix shape (pool `Release` semantics vs a live `client.List` count-check) is a design fork to be resolved at this phase's discuss/plan step before implementation; the requirements state the behavior, not the mechanism.

- [x] **CONCUR-01**: In-flight planner Jobs are bounded by the configurable `plannerConcurrency` cap at steady state (counting running pods, not merely concurrent Job-creation calls), so the planning cascade cannot exceed the cap regardless of how many reconcile rounds fire.
- [x] **CONCUR-02**: The default `plannerConcurrency` is reduced from 16 to a value safe for a single-node cluster (canonical value resolved in planning), documented in `charts/tide/values.yaml`.
- [x] **CONCUR-03**: Planner and executor pools remain separately sized — the cap does not unify the two pools (spec contract), and the executor pool is unchanged.
- [x] **CONCUR-04**: Dispatches deferred by the cap are observable (log line at minimum) and never silently truncate a wave — excess dispatches park/requeue rather than being dropped, and the chart documents that the cap must be sized at least as wide as the widest expected wave.

### Planner Failure Semantics (D4)

A childless-success guard at the phase and milestone levels, mirroring the plan-level guard Phase 30 already shipped — so a failed planner cannot falsely succeed its parent and corrupt the planning DAG.

- [x] **PLANFAIL-01**: A phase whose planner exits non-zero with zero children (`envReadOK && exitCode != 0 && childCount == 0`) is marked `Failed`, not `Succeeded`.
- [x] **PLANFAIL-02**: A milestone whose planner exits non-zero with zero children is marked `Failed`, not `Succeeded` (same guard, shared `isPlannerFailure` helper across levels).
- [ ] **PLANFAIL-03**: A genuine leaf (planner exits zero with zero children) still `Succeeds` — no regression; the fail-check is ordered before the succeed-check.
- [ ] **PLANFAIL-04**: A falsely-failed parent is recoverable via the existing `tide resume --retry-failed` verb — the guard patches a permanent `Failed` condition rather than returning a Go error (no controller-side retry storm, no auto-retry).

## v2 Requirements

Deferred to a future release — acknowledged by the research as non-blocking for v1.0.6.

### Observability & Ergonomics

- **OBS-01**: Prometheus pool-saturation gauge for deferred planner dispatches (logging is sufficient for v1.0.6).
- **OBS-02**: Dashboard "Adopted" badge distinguishing imported vs freshly-planned nodes.
- **CONCUR-F1**: Per-`Project` concurrency override CRD field (the chart-level cap is sufficient for v1.0.6).

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| OpenAI/Codex subagent backend | The deferred run-#2-completion milestone; v1.0.6 adds no provider — it only hardens the adoption path the run will use. |
| Dogfood run #2c relaunch | An operational follow-on, not code in this patch; requires multi-node / ≥16 GiB infrastructure that single-node kind cannot provide (research SUMMARY.md). |
| Automatic planner retry with backoff | Operator-driven `tide resume --retry-failed` is the sanctioned recovery posture; auto-retry risks storms (P-D4a) and contradicts the established v1.0.1 recovery-verb decision. |
| External queue / Kueue / Volcano for D3 | Adds an external dependency; the in-process pool + count-check is sufficient and keeps CRD-`.status`-only + no-new-dependency intact. |
| `MaxConcurrentReconciles` as the dispatch cap | Bounds reconcile goroutines, not in-flight Jobs — the wrong lever for D3 (must stay strictly greater than `plannerConcurrency`). |
| Cached wave schedule / declared waves | Spec contract — waves stay re-derived from the DAG + completed-set; the cap parks dispatch, it does not persist a schedule. |

## Traceability

Which phases cover which requirements. Populated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| ADOPT-01 | Phase 31 | Complete |
| ADOPT-02 | Phase 31 | Complete |
| ADOPT-03 | Phase 31 | Complete |
| ADOPT-04 | Phase 31 | Complete |
| ADOPT-05 | Phase 31 | Complete |
| CONCUR-01 | Phase 32 | Complete |
| CONCUR-02 | Phase 32 | Complete |
| CONCUR-03 | Phase 32 | Complete |
| CONCUR-04 | Phase 32 | Complete |
| PLANFAIL-01 | Phase 33 | Complete |
| PLANFAIL-02 | Phase 33 | Complete |
| PLANFAIL-03 | Phase 33 | Pending |
| PLANFAIL-04 | Phase 33 | Pending |

**Coverage:**
- v1 requirements: 13 total
- Mapped to phases: 13 (100%)
- Unmapped: 0

---
*Requirements defined: 2026-06-28*
*Last updated: 2026-06-28 — traceability table populated after roadmap creation (Phase 31: ADOPT-01..05, Phase 32: CONCUR-01..04, Phase 33: PLANFAIL-01..04)*
