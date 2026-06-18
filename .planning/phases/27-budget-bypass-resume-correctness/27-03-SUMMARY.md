---
phase: 27-budget-bypass-resume-correctness
plan: 03
subsystem: controller
tags: [budget, rollup, idempotency, envtest, tdd, bypass, resume]

# Dependency graph
requires:
  - phase: 27-01
    provides: PlannerRolledUpUID field in api/v1alpha2 BudgetStatus
  - phase: 27-02
    provides: bypass resume target fix (PhaseRunning) + CloneComplete guard

provides:
  - PlannerRolledUpUID-gated rollup in handleProjectJobCompletion (BYPASS-03)
  - BYPASS-05 TTL-GC companion envtest scenario proving rollup-once on nil-Job path

affects:
  - phase 27-04 (final verification wave)
  - any future controller changes touching handleProjectJobCompletion or budget rollup

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Durable idempotency marker pattern: construct deterministic key from project.UID, check before operation, set only on success, never clear"
    - "TDD RED/GREEN: write failing envtest specs first, implement guard, confirm 55/55 green"

key-files:
  created: []
  modified:
    - internal/controller/project_controller.go
    - internal/controller/project_planner_completion_test.go

key-decisions:
  - "PlannerRolledUpUID check lives inside the existing isFirstCompletion && envReadOK block — the marker is an additional inner guard, not a replacement for isFirstCompletion"
  - "plannerJobName constructed inside handleProjectJobCompletion as fmt.Sprintf('tide-project-%s-1', project.UID) — mirrors the reconcileProjectPlannerDispatch construction site, not passed as parameter"
  - "On rollup error: marker left unset so next reconcile retries (Pitfall 2 from RESEARCH)"
  - "T-27-03-01: plannerJobName is a construction-site invariant derived from project.UID, not external input — documented in code comment"
  - "Never clear PlannerRolledUpUID on bypass: marker must survive halt→resume per D-03"

patterns-established:
  - "Idempotency marker: check != expected before operation, set only on success — applies to any non-fatal operation that must run at most once per deterministic key"
  - "Non-fatal patch failure: logger.Error(pErr, '... (non-fatal)', ...) on Status().Patch for markers — let next reconcile retry"

requirements-completed: [BYPASS-03, BYPASS-05]

# Metrics
duration: 20min
completed: 2026-06-18
---

# Phase 27 Plan 03: Budget Bypass Resume Correctness — Rollup-Once Guard Summary

**PlannerRolledUpUID-gated rollup in handleProjectJobCompletion prevents double-counting planning cost on halt→resume when the reporter Job has TTL-GC'd; BYPASS-05 TTL-GC companion envtest proves the nil-Job path rolls up exactly once**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-06-18T15:40:00Z
- **Completed:** 2026-06-18T15:56:00Z
- **Tasks:** 2 (TDD RED + GREEN across both tasks)
- **Files modified:** 2

## Accomplishments

- Replaced the reporter-Job-existence `isFirstCompletion` double-count signal with a durable `PlannerRolledUpUID` idempotency marker in `handleProjectJobCompletion` (BYPASS-03)
- Added BYPASS-03 double-count spec: two consecutive `handleProjectJobCompletion(ctx, proj, nil)` calls produce `CostSpentCents == plannerCostCents` (not 2×) and `PlannerRolledUpUID == tide-project-<uid>-1`
- Added BYPASS-05 TTL-GC companion spec: single nil-Job call spawns reporter and rolls up cost, proving the ordering fix holds on the planner-Job-absent path (mirrors QQH-01 present-Job assertion)
- All 55/55 Layer A envtest specs green (`make test-int-fast` SUCCESS)

## Task Commits

Each task was committed atomically:

1. **Task 2 (TDD RED): BYPASS-03 double-count spec + BYPASS-05 TTL-GC companion** - `1971a07` (test)
2. **Task 1 (TDD GREEN): PlannerRolledUpUID rollup-once guard** - `51c9b20` (feat)

Note: TDD RED was committed first (Task 2 test-only), then GREEN implementation (Task 1 feat) — the test file landed before the implementation per TDD protocol.

## Files Created/Modified

- `internal/controller/project_controller.go` — `handleProjectJobCompletion`: replaced bare `budget.RollUpUsage` call with `PlannerRolledUpUID != plannerJobName` guard; patches marker on rollup success, never clears it
- `internal/controller/project_planner_completion_test.go` — Added `Describe("BYPASS-03 / BYPASS-05 rollup-once across halt+GC")` block with two specs: double-count guard and TTL-GC companion

## Decisions Made

- `plannerJobName` is constructed inside `handleProjectJobCompletion` as `fmt.Sprintf("tide-project-%s-1", project.UID)` — not passed as a parameter from `reconcileProjectPlannerDispatch`. This keeps the function self-contained and avoids API surface churn; the key is a pure function of `project.UID` at both sites.
- Marker check is an additional inner guard inside `isFirstCompletion && envReadOK`, not a replacement. This preserves the semantic meaning of `isFirstCompletion` (reporter Job spawn signal) while adding the durable double-count protection.
- T-27-03-01 mitigation documented in inline comment: the `tide-project-<uid>-1` shape is a construction-site invariant, not external input.

## Deviations from Plan

None — plan executed exactly as written. Both tasks followed TDD RED/GREEN protocol. No blocking issues encountered.

## Issues Encountered

- **Claude temp filesystem full (99/99 task symlinks)**: the `/private/tmp/claude-501/.../tasks` directory was at capacity (99 symlinks) before execution began. Removed one stale symlink from a prior session to restore bash command capability. No code impact.
- **envtest binary path**: `go test` in the worktree failed with `etcd: no such file or directory` at `/usr/local/kubebuilder/bin/etcd`. Set `KUBEBUILDER_ASSETS` to `/Users/justinsearles/Projects/tide/bin/k8s/1.33.0-darwin-amd64` (main repo's pre-downloaded binaries). `make test-int-fast` (which sets `KUBEBUILDER_ASSETS` via `setup-envtest`) ran cleanly.

## Known Stubs

None — no stub patterns introduced. The `PlannerRolledUpUID` field is set from live controller state (`project.UID`), not hardcoded.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes introduced. `PlannerRolledUpUID` is controller-written from `project.UID` (construction-site invariant, T-27-03-01 mitigated inline).

## Self-Check

Files verified present:
- [x] `internal/controller/project_controller.go` — modified, contains `PlannerRolledUpUID` at 2 sites
- [x] `internal/controller/project_planner_completion_test.go` — modified, contains new BYPASS-03/BYPASS-05 Describe block

Commits verified:
- [x] `1971a07` — test(27-03): add failing BYPASS-03/BYPASS-05 specs (RED)
- [x] `51c9b20` — feat(27-03): PlannerRolledUpUID rollup-once guard in handleProjectJobCompletion

Test results:
- [x] 55/55 specs green (`make test-int-fast SUCCESS!`)
- [x] BYPASS-03 double-count spec: `CostSpentCents == plannerCostCents` after two nil-Job calls
- [x] BYPASS-05 TTL-GC companion: reporter spawns + cost rolls up on nil-Job path
- [x] QQH-01 existing specs: 2/2 still pass

## Next Phase Readiness

- Wave 3 complete; plan 27-03 delivers BYPASS-03 + BYPASS-05 (rollup-once guard + TTL-GC companion)
- Wave 4 (27-04) is the final verification wave for Phase 27

---
*Phase: 27-budget-bypass-resume-correctness*
*Completed: 2026-06-18*
