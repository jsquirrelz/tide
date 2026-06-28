---
phase: 31-d2-d1-adoption-lifecycle-seam
plan: "03"
subsystem: controller
tags: [kubernetes, controller-runtime, budget-rollup, idempotency, adoption, envtest]

# Dependency graph
requires:
  - plan: "31-01"
    provides: "MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID scalar markers on child .status structs (D-03 / D-03a)"
provides:
  - "MilestoneRolledUpUID-gated RollUpUsage in milestone_controller.go handleJobCompletion"
  - "PhaseRolledUpUID-gated RollUpUsage in phase_controller.go handleJobCompletion"
  - "PlanRolledUpUID-gated RollUpUsage in plan_controller.go handlePlannerJobCompletion (D-03a new)"
  - "Envtest proving ADOPT-02 (accrual) and ADOPT-04 (exactly-once across TTL-GC) at all three child levels"
affects:
  - budget correctness for adopted Projects (ADOPT-04)
  - CostSpentCents integrity across halt/resume cycles at milestone/phase/plan levels

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Durable per-level rollup marker gates RollUpUsage: compute levelJobName locally in handleJobCompletion via fmt.Sprintf, check marker != levelJobName before calling budget.RollUpUsage, stamp marker after nil-error rollup via Status().Patch(MergeFrom) — non-fatal on patch error (D-08)"
    - "ReporterImage='' test shortcut: spawnReporterIfNeeded returns (true, nil) so isFirstCompletion=true unconditionally without requiring a PVC — clean TTL-GC simulation for ADOPT-04 double-count prevention tests"

key-files:
  created:
    - internal/controller/child_rollup_idempotency_test.go
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go

key-decisions:
  - "Local jobName variable: each handleJobCompletion computes its own levelJobName (milestoneJobName/phaseJobName/planJobName) — the outer reconcilePlannerDispatch jobName is not in scope inside the handler function. Same fmt.Sprintf shape ensures consistency."
  - "D-03a confirmed new: grep confirmed plan_controller.go had zero RolledUpUID occurrences before this plan; PlanRolledUpUID is an entirely new addition."
  - "ImportSource audit: the three importSource checks at milestone L370 / phase L368 / plan L374 are dispatch-hold checks (park until ImportComplete=True), NOT skip-around-rollup guards — correctly untouched per plan requirement."
  - "ADOPT-04 test approach: ReporterImage empty → isFirstCompletion=true on every call (no PVC needed), marker is the SOLE guard — exactly the TTL-GC condition the plan specified."

patterns-established:
  - "handleJobCompletion-local levelJobName: handlers compute their own job name strings, not reading from outer scope, for correctness across call contexts"
  - "Consistently(2s, 200ms) for no-double-count assertions: stronger than Eventually for budget invariants"

requirements-completed: [ADOPT-02, ADOPT-04]

# Metrics
duration: 60min
completed: 2026-06-28T21:59:25Z
---

# Phase 31 Plan 03: D1 Child-Level Rollup Idempotency — Durable Per-Level Markers

**Exactly-once child budget rollup gated on durable MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID markers (D-03/D-03a), proven by three Ginkgo envtest specs (ADOPT-02 accrual + ADOPT-04 no double-count across TTL-GC)**

## Performance

- **Duration:** ~60 min
- **Started:** 2026-06-28T~21:00Z
- **Completed:** 2026-06-28T21:59:25Z
- **Tasks:** 2
- **Files modified:** 3 controllers + 1 new test file = 4 files

## Accomplishments

- **Task 1 — Marker-gated rollup in all three child controllers:**
  - `milestone_controller.go` `handleJobCompletion`: wraps `budget.RollUpUsage` in `ms.Status.MilestoneRolledUpUID != milestoneJobName` guard; stamps marker after successful rollup via `Status().Patch(client.MergeFrom(ms.DeepCopy()))` (non-fatal on patch error per D-08)
  - `phase_controller.go` `handleJobCompletion`: same pattern with `ph.Status.PhaseRolledUpUID` and `phaseJobName`
  - `plan_controller.go` `handlePlannerJobCompletion`: same pattern with `plan.Status.PlanRolledUpUID` and `planJobName` — D-03a new addition (confirmed no prior marker via grep before implementation)
  - ImportSource audit confirmed: three `Spec.ImportSource != nil` checks in child controllers are dispatch-hold checks (Phase 28 IMPORT-01 — park before pool acquire until import completes), NOT rollup skip guards — unconditional rollup preserved per plan requirement
  - No new `Status().Update` sites (D-09 preserved); `values.yaml` untouched (D-11); project-level `Budget.PlannerRolledUpUID` untouched (D-10)

- **Task 2 — Envtest for ADOPT-02 + ADOPT-04:**
  - `internal/controller/child_rollup_idempotency_test.go`: three `Describe` blocks (milestone / phase / plan), each with a combined ADOPT-02+04 `It` spec
  - ADOPT-02: `handleJobCompletion` (nil Job, ReporterImage="") with stubbed Usage raises `Project.Status.Budget.CostSpentCents` and `TokensSpent` by exact stubbed amounts; level marker is set to `tide-<level>-<uid>-1`
  - ADOPT-04: second call with marker already set → `Consistently(2s, 200ms)` asserts `CostSpentCents` unchanged (ReporterImage="" keeps `isFirstCompletion=true`, marker is the SOLE guard)
  - 3/3 ChildRollup specs pass; full controller suite (158 specs) passes with no regressions

## Task Commits

1. **Task 1: Gate child-level rollup on durable level-specific markers** — `312c9e9` (feat)
2. **Task 2: Envtest ADOPT-02/04 + fix local jobName vars** — `c43e65d` (feat)

*Note: Task 2 commit includes corrections to the Task 1 controller changes — the initial commit used `jobName` which is scoped to `reconcilePlannerDispatch` (outer function) and not available inside `handleJobCompletion`. The fix introduces level-specific local variables (`milestoneJobName`/`phaseJobName`/`planJobName`) computed from the child's UID using the same `fmt.Sprintf` pattern.*

## Files Created/Modified

- `internal/controller/milestone_controller.go` — `handleJobCompletion`: `milestoneJobName` local + `MilestoneRolledUpUID != milestoneJobName` gate + marker stamp after successful rollup
- `internal/controller/phase_controller.go` — `handleJobCompletion`: `phaseJobName` local + `PhaseRolledUpUID != phaseJobName` gate + marker stamp
- `internal/controller/plan_controller.go` — `handlePlannerJobCompletion`: `planJobName` local + `PlanRolledUpUID != planJobName` gate + marker stamp (D-03a new)
- `internal/controller/child_rollup_idempotency_test.go` — new: 3 Ginkgo Describe blocks proving ADOPT-02 accrual and ADOPT-04 idempotency at all three child levels

## Decisions Made

- **Local levelJobName in handleJobCompletion:** The outer `reconcilePlannerDispatch` function computes `jobName` at the top and passes it to `handleJobCompletion` indirectly (via the job name lookup), but the variable itself is not in scope inside the handler. The fix is to compute the job name locally using the same `fmt.Sprintf("tide-<level>-%s-1", <level>.UID)` pattern — identical semantics, correct scoping.
- **ReporterImage="" in tests:** `spawnReporterIfNeeded` returns `(true, nil)` when `reporterImage == ""`, making `isFirstCompletion=true` on every call. This avoids needing a PVC in the test and cleanly simulates the TTL-GC scenario (reporter Job absent → isFirstCompletion flips back to true).
- **`Consistently` for ADOPT-04:** Used `Consistently(2s, 200ms)` instead of just asserting once — budget writes land asynchronously via Status().Patch and a passing single-shot assertion could race with a delayed second write.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] handleJobCompletion cannot access outer reconcilePlannerDispatch's `jobName`**
- **Found during:** Task 1 implementation
- **Issue:** The plan's action described gating on `jobName` inside `handleJobCompletion`, but `jobName` is declared in `reconcilePlannerDispatch` (the dispatch function that calls `handleJobCompletion`). Go's lexical scoping means it is not accessible inside the handler.
- **Fix:** Introduced `milestoneJobName`, `phaseJobName`, and `planJobName` locals in each handler using the identical `fmt.Sprintf("tide-<level>-%s-1", <level>.UID)` pattern — same semantics, correct scope. Included in Task 2 commit.
- **Files modified:** `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`
- **Commit:** `c43e65d`

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced. The only new surface is the additional `Status().Patch` call per level (marker stamp), which is a well-established idempotent CRD status write pattern already present at 17+ sites in `project_controller.go`. T-31-07 (double-count) is mitigated by the durable markers as planned.

## Self-Check: PASSED

- `internal/controller/milestone_controller.go` — `MilestoneRolledUpUID` present (4 occurrences: comment, gate, assignment, log)
- `internal/controller/phase_controller.go` — `PhaseRolledUpUID` present (4 occurrences)
- `internal/controller/plan_controller.go` — `PlanRolledUpUID` present (4 occurrences, >= 2 required)
- No generic `PlannerRolledUpUID` in any child controller (grep returns no files)
- `Status().Update` count: milestone=4, phase=4, plan=2 (unchanged from baseline)
- `ImportSource` audit: 3 occurrences, all in dispatch-hold context (not rollup), none removed
- Task 1 commit `312c9e9` exists in git log
- Task 2 commit `c43e65d` exists in git log
- `go build ./api/... ./internal/... ./cmd/manager/...` exits 0
- 3/3 ChildRollup envtest specs pass; full 158-spec suite passes

---
*Phase: 31-d2-d1-adoption-lifecycle-seam*
*Completed: 2026-06-28*
