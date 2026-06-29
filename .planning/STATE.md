---
gsd_state_version: 1.0
milestone: v1.0.7
milestone_name: Flood Tide — TIDE-on-TIDE Self-Hosting Proof
status: planning
last_updated: "2026-06-29T20:39:48.135Z"
last_activity: 2026-06-29
progress:
  total_phases: 6
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-29)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Phase 34 — Pre-flight Tech-Debt Hardening

## Current Position

Phase: 34 of 39 (Pre-flight Tech-Debt Hardening) — first v1.0.7 phase
Plan: — (ready to plan)
Status: Ready to plan Phase 34
Last activity: 2026-06-29 — v1.0.7 roadmap created (Phases 34–39, 16 reqs, 100% mapped)

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity (reference):**

- Total plans completed across v1.0.1–v1.0.6: ~80+
- v1.0.6 (Phases 31–33): 8 plans, 12 tasks, shipped 2026-06-29

**v1.0.7 Phase Tracking:**

| Phase | Plans | Status |
|-------|-------|--------|
| 34. Pre-flight Tech-Debt Hardening | TBD | Not started |
| 35. Infra + Fresh v1.0.7 Deploy | TBD | Not started |
| 36. Salvaged-Tree Import + Dry-Run + Tuning | TBD | Not started |
| 37. Launch + Operate Run #2 to Completion | TBD | Not started |
| 38. Output Review + Extraction | TBD | Not started |
| 39. Release v1.0.7 | TBD | Not started |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

**v1.0.7 binding constraints (from REQUIREMENTS.md and PROJECT.md):**

- This is an operations/dogfood milestone: the human operates TIDE; **TIDE builds the entire OpenAI backend** — no hand-written backend code. The backend is TIDE's *output*, reviewed not merged (rework is the follow-up "extend TIDE" milestone).
- Single-node OOM safety comes from the D3 concurrency cap + low effective `plannerConcurrency` (PREFLIGHT-01 default = 4), NOT from large RAM — 16GB is explicitly too much; the node is sized *slightly* above ~8GiB.
- The hard `$100 absoluteCapCents` (10000 cents) gate must halt the run cleanly; relaunch/resume only on explicit human approval of more spend (RUN-02 — no blind spend).
- Orchestrator defects that surface during the run are root-fixed in-repo with a symptom-reproducing test and the run relaunched/resumed — not worked around (RUN-04, the v1.0.6 D1–D4 pattern).
- Persistence stays CRD-`.status`-only; resumption stays minimal/re-derivable. No new CRD schema this milestone.
- v1.0.7 ships the two PREFLIGHT tech-debt fixes as real release artifacts (RELEASE-01).

### Roadmap Evolution

- v1.0.7 roadmap created 2026-06-29: Phases 34–39, 16 requirements (PREFLIGHT/INFRA/IMPORT/RUN/REVIEW/RELEASE-01), 100% mapped.
- Phase numbering continues from v1.0.6 (Phase 33 was the last phase). Phase 34 is the first v1.0.7 phase.
- The phase chain is forced sequential: each phase's deliverable is the next's prerequisite (harden → deploy → import → operate → review → release). 34 → 35 → 36 → 37 → 38 → 39.
- Phase 34 (PREFLIGHT) must land before launch — it protects single-node OOM safety (configmap default) and $100-cap cost accuracy (project-level rollup hardening); it is also part of what RELEASE-01 ships.
- RUN-04 (root-fix surfacing defects) lives inside Phase 37 as an expected iterative operate activity, not a separate phase.

### Pending Todos

- Phase 34 planning: locate the project-level rollup marker (`PlannerRolledUpUID` / equivalent) in `project_controller.go` and confirm whether it already uses the milestone/phase `RetryOnConflict` + re-fetch pattern (v1.0.6 carried this in as W1). If best-effort, harden it.
- Phase 34 planning: render the chart with defaults and confirm `plannerConcurrency` configmap value; the v1.0.6 chart-comment softening held the value at 4 but the configmap `default 16` may still be present (W2).
- Phase 35: document the kind node memory ceiling; the durable real key lives at `~/.tide/anthropic.key` (outside the repo, survives teardowns) and the in-cluster `tide-secrets` is recreated at full-deploy.
- Phase 36: salvage tree is `salvage-20260618` (3 Milestones / 15 Phases); use `tide import-envelopes` (+ `--dry-run` for the cost projection); set `absoluteCapCents=10000`.

### Blockers/Concerns

- The prior `kind-tide-dogfood` cluster is v1alpha1-only / pre-Spring-Tide and its stored Project would orphan on a no-conversion CRD upgrade — Phase 35 stands up a *fresh* cluster rather than reusing it (per the v1.0.2 spec-shot lesson).
- `make test-int` has a pre-existing kind `medium_http` fixture flake (MAKE_EXIT=2) unrelated to v1.0.7 work — do not treat it as a v1.0.7 regression unless a v1.0.7 commit touches `test/integration/kind/`.

## Deferred Items

Carried forward at v1.0.6 close (2026-06-29), now scoped:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| tech_debt | Project-level `PlannerRolledUpUID` hardening (W1) | Folded into Phase 34 (PREFLIGHT-02) | v1.0.6 |
| tech_debt | Chart configmap `plannerConcurrency default 16→4` (W2) | Folded into Phase 34 (PREFLIGHT-01) | v1.0.6 |
| tech_debt | Controller-envtest-suite tier split | Deferred (DEBT-01) — not load-bearing for run #2 | v1.0.6 |
| stale artifacts | 20 historical quick-tasks + 1 todo + 1 uat_gap | Non-blocking administrative debt | v1.0.6 |

## Session Continuity

Last session: 2026-06-29 — v1.0.7 roadmap created
Stopped at: ROADMAP.md + REQUIREMENTS.md traceability + STATE.md written for v1.0.7 (Phases 34–39)
Resume file: None

## Operator Next Steps

- Plan Phase 34 with `/gsd:plan-phase 34` (Pre-flight Tech-Debt Hardening — PREFLIGHT-01/02)
