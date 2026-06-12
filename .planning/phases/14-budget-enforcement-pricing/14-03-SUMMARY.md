---
phase: 14-budget-enforcement-pricing
plan: "03"
subsystem: controller
tags: [budget, reservation, envtest, ginkgo, kubernetes, controller-runtime]

# Dependency graph
requires:
  - phase: 14-budget-enforcement-pricing
    provides: ReservationStore + checkBudgetBlocked + setBudgetBlockedIfNeeded from 14-02
provides:
  - EstimatedCostCents field on BuildOptions with tideproject.k8s/estimated-cost Job label
  - TaskReconciler budget gate rewrite — cap detection in task loop, not ProjectReconciler
  - Reserve/Settle/Release wired at Job create / terminal state / finalizer cleanup
  - 30s requeue on budget-parked tasks (cap-raise recovery)
  - Ginkgo envtest regression for run-1 BudgetBlocked silence bug
affects:
  - task-controller
  - budget
  - podjob

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cap detection in TaskReconciler (not ProjectReconciler) — avoids GenerationChangedPredicate blind spot on status-only patches"
    - "Reserve-on-create / Settle-on-terminal / Release-on-finalizer lifecycle for ReservationStore"
    - "30s requeueAfter on budget-parked tasks to enable cap-raise recovery without external event"
    - "Separate Describe blocks per scenario for Ginkgo test isolation (unique names prevent 409 Conflict from finalizer delay)"

key-files:
  created:
    - internal/controller/budget_blocked_regression_test.go
  modified:
    - internal/dispatch/podjob/jobspec.go
    - internal/dispatch/podjob/jobspec_test.go
    - internal/controller/task_controller.go

key-decisions:
  - "Cap detection lives in TaskReconciler at two call sites: dispatch gate (pre-Job) and after RollUpUsage (post-terminal)"
  - "BudgetBlocked gate uses 30s RequeueAfter so parked tasks wake up on cap-raise without requiring external event"
  - "Regression test uses 4 separate Describe blocks with unique resource names — prevents 409 from finalizer-delayed deletion across Ginkgo-randomized spec order"
  - "dispatch_image_test.go whitespace drift (trailing space on SigningKey field) was pre-existing from gofmt; restored via git checkout before each Task 2/3 stage"

patterns-established:
  - "Pattern: D-05 reservation lifecycle — Reserve after Job.Create succeeds, Settle at handleJobCompletion entry, Release in finalizer cleanup callback"
  - "Pattern: bidirectional setBudgetBlockedIfNeeded — stamps True on cap breach, clears to False on cap-raise, called at both dispatch gate and post-terminal"

requirements-completed: []

# Metrics
duration: 75min
completed: 2026-06-12
---

# Phase 14 Plan 03: Budget Gate Rewrite + Reservation Wiring Summary

**TaskReconciler cap detection (fixes run-1 silence), D-05 reserve/settle lifecycle, and Ginkgo envtest regression covering BudgetBlocked trip, dispatch park, reservation headroom, and cap-raise recovery**

## Performance

- **Duration:** ~75 min
- **Started:** 2026-06-12T23:00:00Z
- **Completed:** 2026-06-12T00:16:53Z
- **Tasks:** 3
- **Files modified:** 4 (2 modified, 1 created, 1 modified with tests)

## Accomplishments

- Added `EstimatedCostCents int64` to `BuildOptions` and wired `tideproject.k8s/estimated-cost` label onto executor Jobs for restart rederivation via `RederiveReservations`
- Rewrote TaskReconciler Step 4 budget gate: `setBudgetBlockedIfNeeded` now called at the dispatch guard (pre-Job) and after `RollUpUsage` (post-terminal), fixing the run-1 silence bug where `GenerationChangedPredicate` never re-enqueued on status-only spend patches
- Wired Reserve/Settle/Release at all lifecycle points: reserve after successful `r.Create`, settle at `handleJobCompletion` entry, release in finalizer cleanup callback
- Added `HasHeadroom` gate before Job creation to block wave-wide overshoot when `spent + reserved + estimate >= cap`
- Added 30s `RequeueAfter` on budget-parked tasks so cap-raise recovery fires without an external event
- Wrote 4-scenario envtest regression: cap trips `BudgetBlocked=True`, blocked task parks (no Job, no Fail, 30s requeue), reservation headroom gates second dispatch, cap-raise clears condition to False

## Task Commits

Each task was committed atomically:

1. **Task 1: EstimatedCostCents field + estimated-cost Job label** - `91b6c32` (feat)
2. **Task 2: TaskReconciler gate rewrite + reserve/settle wiring** - `fed408e` (feat)
3. **Task 3: run-1 BudgetBlocked regression envtest** - `ecfff9e` (test)

## Files Created/Modified

- `internal/dispatch/podjob/jobspec.go` - Added `EstimatedCostCents int64` to `BuildOptions`; stamps `tideproject.k8s/estimated-cost` label on executor Jobs when non-zero
- `internal/dispatch/podjob/jobspec_test.go` - Two new table entries: label present when non-zero, absent when zero
- `internal/controller/task_controller.go` - Added `Reservations`/`ReserveEstimateCents`/`PricingOverridesJSON` to `TaskReconcilerDeps`; rewrote Step 4 gate with `setBudgetBlockedIfNeeded` + `HasHeadroom`; wired Reserve/Settle/Release at Job create/terminal/finalizer
- `internal/controller/budget_blocked_regression_test.go` - 4-Describe regression suite covering run-1 silence bug, dispatch park behavior, reservation headroom, cap-raise recovery

## Decisions Made

- **Cap detection in TaskReconciler, not ProjectReconciler**: `RollUpUsage` patches only `Status.Budget.CostSpentCents` (status subresource), which does NOT increment `metadata.generation`. The `ProjectReconciler` uses `GenerationChangedPredicate` so `handleBudgetGate` never re-ran after spend updates. Moving detection to `TaskReconciler` (which already reconciles every task event) fixes this without changing the ProjectReconciler's watch predicate.
- **30s RequeueAfter on parked tasks**: Previous code returned `ctrl.Result{}` (empty, no requeue), meaning a task parked on budget would only wake if an external event triggered re-reconcile. 30s polling ensures cap-raise recovery within one check interval.
- **Separate Describe blocks per test scenario**: Ginkgo randomizes spec order. Sharing resource names across `It` blocks within one `Describe` causes 409 AlreadyExists in `BeforeEach` when a finalizer-protected task from the prior spec hasn't been deleted yet. Unique names per `Describe` eliminate the race.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Restored pre-existing gofmt drift in dispatch_image_test.go**
- **Found during:** Task 2 pre-commit staging check
- **Issue:** `internal/controller/dispatch_image_test.go` had a trailing-space alignment diff on the `SigningKey` field — a whitespace-only change that `gofmt` applied but was not part of this plan's work
- **Fix:** `git checkout -- internal/controller/dispatch_image_test.go` before staging Task 2 and again before staging Task 3
- **Files modified:** `internal/controller/dispatch_image_test.go` (restored, not committed)
- **Verification:** `git status --short` showed clean after restore
- **Committed in:** N/A (pre-existing file left at original state)

**2. [Rule 1 - Bug] Fixed 409 AlreadyExists in test isolation (Task 3)**
- **Found during:** Task 3 initial test run
- **Issue:** First version of regression test used a single `Describe` block with 3 `It` specs sharing `projName="bb-run1-proj-1"` and `taskName="bb-run1-task-1"`. Ginkgo ran specs in sequence; the finalizer on the first task prevented immediate deletion, causing `BeforeEach` of the second spec to receive HTTP 409 on the same name.
- **Fix:** Split into 4 separate `Describe` blocks, each with unique project/task names per scenario
- **Files modified:** `internal/controller/budget_blocked_regression_test.go`
- **Verification:** `make test` passed 131 specs 0 failures
- **Committed in:** `ecfff9e` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (1 pre-existing drift restore, 1 test isolation bug)
**Impact on plan:** Both fixes necessary for correctness. No scope creep.

## Issues Encountered

- **`go test` without `KUBEBUILDER_ASSETS`**: Running `go test ./internal/controller/ -run Test -short` directly failed — the worktree doesn't have its own `bin/k8s/` envtest binaries. Must use `make test` from the main repo path which sets `KUBEBUILDER_ASSETS` from `bin/k8s/`. Documented for future reference; `make test` is the canonical test command.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Budget gate is fully wired; run-1 silence bug is fixed and regression-tested
- `ReservationStore` lifecycle (Reserve/Settle/Release) is complete
- `TIDE_PRICING_OVERRIDES_JSON` transport from 14-02 is plumbed through `BuildOptions.PricingOverridesJSON` to the executor Job env
- Ready for phase 14 wave 3 work (pricing override parsing in executor, acceptance testing)

---
*Phase: 14-budget-enforcement-pricing*
*Completed: 2026-06-12*
