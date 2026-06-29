---
gsd_state_version: 1.0
milestone: v1.0.6
milestone_name: Adoption-Path Correctness & Dispatch Safety
status: Awaiting next milestone
stopped_at: Phase 33 context gathered
last_updated: "2026-06-29T19:47:46.596Z"
last_activity: 2026-06-29 — Milestone v1.0.6 completed and archived
progress:
  total_phases: 12
  completed_phases: 3
  total_plans: 8
  completed_plans: 8
  percent: 25
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-28)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Phase 33 — D4 — Planner Failure Semantics

## Current Position

Phase: Milestone v1.0.6 complete
Plan: —
Status: Awaiting next milestone
Last activity: 2026-06-29 — Milestone v1.0.6 completed and archived

## Performance Metrics

**Velocity (v1.0.5 reference):**

- Total plans completed across v1.0.1–v1.0.5: ~64+
- Phase 30: 3 plans, completed 2026-06-27

**v1.0.6 Phase Tracking:**

| Phase | Plans | Status |
|-------|-------|--------|
| 31. D2+D1 — Adoption Lifecycle Seam | TBD | Not started |
| 32. D3 — Dispatch Concurrency Cap | TBD | Not started |
| 33. D4 — Planner Failure Semantics | TBD | Not started |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

**v1.0.6 binding constraints (from REQUIREMENTS.md and PROJECT.md):**

- Persistence stays CRD-`.status`-only — no new fields beyond what is needed for idempotency markers; no external DB.
- Planner and executor pools remain separately sized — Phase 32 (D3) must not unify the two pools. The cross-pool analyzer (`make lint`) enforces this.
- Wave-boundary failure semantics are intact and must not be weakened by lifecycle-advance or concurrency-cap changes.
- The D3 cap parks/requeues excess dispatches — it never silently truncates a wave.
- D1 rollup is exactly-once under reporter-Job TTL-GC — the Phase-27 `PlannerRolledUpUID` durable-marker pattern must be extended to milestone/phase levels if not already present.
- D4 uses a permanent `patchPhaseFailed`/`patchMilestoneFailed` condition patch — not a Go error return — to avoid controller retry storms (P-D4a).
- `MaxConcurrentReconciles` is NOT the D3 lever — it bounds reconcile goroutines, not in-flight Jobs. Must stay strictly greater than `plannerConcurrency`.
- The dogfood run #2c relaunch is OUT OF SCOPE for this milestone; it requires multi-node or ≥16 GiB infrastructure.

**Phase 32 design fork (must resolve before implementation):**
Option A (1 researcher): pool `Release` fires on function return and already caps in-flight — chart default reduction alone is sufficient.
Option B (3 researchers, deeper code reads): `defer r.PlannerPool.Release()` fires milliseconds after `r.Create(job)`, not on Job terminal state; a live `client.List` count-check is required before pool acquire. Resolution: one `kubectl get jobs -l tideproject.k8s/role=planner` observation with `plannerConcurrency=2` and 5 Milestones closes the fork. No implementation plan for Phase 32 may be written until this is resolved.

### Roadmap Evolution

- v1.0.6 roadmap defined 2026-06-28: Phases 31–33, 13 requirements (ADOPT-01..05, CONCUR-01..04, PLANFAIL-01..04), 100% mapped.
- Phase numbering continues from v1.0.5 (Phase 30 was the last phase). Phase 31 is the first v1.0.6 phase.
- Phase 31 (D2+D1) is highest priority: D2 lifecycle advance is a prerequisite for D1 budget rollup; both are the "spent blind" headline safety failure.
- Phase 32 (D3) carries a mandatory design fork — no implementation plan may be written before the fork is resolved at the discuss/plan step.
- Phase 33 (D4) is independent of D1/D2/D3; it follows Phase 32 by severity ordering but has no dependency on D3's resolution.

### Pending Todos

- Resolve Phase 32 D3 design fork before writing implementation plans for Phase 32. Method: observe active Job count with `plannerConcurrency=2` on `kind-tide-dogfood` while 5+ Milestones are enqueued, or resolve via discuss step.
- During Phase 31 planning: grep all `budget.RollUpUsage` call sites for any `if project.Spec.ImportSource != nil { skip }` guards at milestone, phase, and plan controllers. Child-level rollup must be unconditional.
- During Phase 31 planning: verify whether `MilestoneRolledUpUID` / `PhaseRolledUpUID` idempotency markers exist at child levels (Phase-27 pattern). If absent, add them — this is the P-D1a/P-D1c double-count risk.
- During Phase 33 planning: verify whether `patchPhaseFailed` / `patchMilestoneFailed` helpers already exist. If absent, add by mirroring `patchPlanFailed` in `plan_controller.go:842`.

### Blockers/Concerns

- **Phase 32 design fork (BLOCKER for Phase 32 implementation):** The D3 fix shape diverges across research subagents. Option A (chart default only) vs Option B (live `client.List` count-check). Must be resolved before any Phase 32 implementation plan is authored. This is encoded as a mandatory gate in the ROADMAP.md Phase 32 detail section.
- **Phase 32 default value TBD:** ARCHITECTURE.md recommends `plannerConcurrency: 3`; STACK.md and FEATURES.md recommend `4`. Canonical value to be resolved and documented at Phase 32 discuss/plan step.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260615-gos | Explore CLI-as-strategy + second SDK/LangGraph strategy — semi-scoped backlog milestone | 2026-06-15 | d43f402 | [260615-gos-explore-abstracting-cli-to-be-a-single-s](./quick/260615-gos-explore-abstracting-cli-to-be-a-single-s/) |
| 260617-qqh | Fix project-controller planner-completion ordering so reporter spawns + planner cost rolls up (dogfood run #2 root cause) | 2026-06-17 | 2a5e0dc | [260617-qqh-fix-project-controller-planner-completio](./quick/260617-qqh-fix-project-controller-planner-completio/) |
| 260625-k1q | v1.0.4 patch: publish tide-import image + chart-image release-matrix guardrail | 2026-06-25 | fd86a79 | [260625-k1q-v1-0-4-patch-publish-tide-import-image-c](./quick/260625-k1q-v1-0-4-patch-publish-tide-import-image-c/) |
| 260625-txr | Dogfood run #2 setup artifacts — v1alpha2 skeleton CRs + project.yaml + runbook | 2026-06-26 | 49b7b0e | [260625-txr-dogfood-run-2-setup-artifacts-v1alpha2-s](./quick/260625-txr-dogfood-run-2-setup-artifacts-v1alpha2-s/) |

## Deferred Items

Items acknowledged and deferred at v1.0.1 milestone close on 2026-06-13:

| Category | Item | Status |
|----------|------|--------|
| quick_task | 260521-ccz-push-lease-cascade-9-recipe | missing |
| quick_task | 260521-eoz-phase-03-cascade-10-filter-pillar-4-list | missing |
| quick_task | 260521-f8x-phase-03-cascade-7-gate-plan-planner-dis | missing |
| quick_task | 260521-gmm-phase-03-cascade-11-pvcprewarmpod-helper | missing |
| quick_task | 260521-hk4-phase-03-cascade-12-patchjobtofailed-mus | missing |
| quick_task | 260521-jz0-phase-03-cascade-13-idempotency-guard-in | missing |
| quick_task | 260526-w11-phase-5-closeout-polish-roadmap-16-16-17 | missing |
| quick_task | 260530-h2h-boot-04-acceptance-v1-cert-manager-prere | missing |
| quick_task | 260530-hrc-open-phase-6-v1-0-image-publish-pipeline | missing |
| quick_task | 260531-oek-fix-cascade-12-chart-template-dispatch-i | missing |
| quick_task | 260610-vcp-audit-codebase-against-k8s-helm-best-pra | missing |
| quick_task | 260610-x3d-draft-the-three-tide-on-tide-dogfood-pro | missing |
| quick_task | 260611-3o9-planning-dag-lr-orientation | unknown |
| quick_task | 260611-439-podjob-caps-floor-bump | unknown |
| quick_task | 260611-cz8-salvage-branch-merge-prep-4-review-fixes | missing |

All v1.0.0-era quick-task records. Work landed; artifact status fields never flipped. Non-blocking administrative debt.

**v1.0.6 deferred to v2 (per REQUIREMENTS.md):**

- OBS-01: Prometheus pool-saturation gauge for deferred planner dispatches (logging sufficient for v1.0.6)
- OBS-02: Dashboard "Adopted" badge distinguishing imported vs freshly-planned nodes
- CONCUR-F1: Per-Project concurrency override CRD field (chart-level cap is sufficient for v1.0.6)

## Session Continuity

Last session: 2026-06-29T13:13:12.047Z
Stopped at: Phase 33 context gathered
Resume file: None

## Operator Next Steps

- Start the next milestone with /gsd-new-milestone

## Deferred Items

Items acknowledged and deferred at v1.0.6 milestone close (2026-06-29). All are stale historical capture-log entries, not active work:

| Category | Count | Notes |
|----------|-------|-------|
| quick_tasks | 20 | Phase-02/03 cascade recipes + Phase 5/6 closeout notes + assorted (260521–260625 era), long resolved; never cleaned from the capture log |
| todos | 1 | historical |
| uat_gaps | 1 | partial-status historical entry |

v1.0.6 tech-debt carried to v1.0.7 (from milestone audit): (W1) retrofit project-level `PlannerRolledUpUID` stamp to the hardened `RetryOnConflict` pattern; (W2) chart `configmap.yaml` `plannerConcurrency | default 16` → `default 4`; plus the deeper "controller envtest suite outgrew the unit tier — move heavy specs to TEST-02" item surfaced during the release (raised the TEST-01 budget to 300s as a stopgap).
