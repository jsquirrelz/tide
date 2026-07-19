---
phase: 51-the-task-loop
plan: 05
subsystem: infra
tags: [controller-runtime, dispatch-holds, condition-vocabulary, k8s-conditions, reconciler-refactor]

# Dependency graph
requires:
  - phase: 51-the-task-loop
    provides: "ConditionVerifyHalt/ReasonVerifyExhausted/AnnotationVerifyResumedAt vocabulary (51-01) and failure_halt.go's checkFailureHalt/setFailureHaltIfNeeded clone source"
provides:
  - "verify_halt.go: checkVerifyHalt/setVerifyHaltIfNeeded, a file-for-file clone of failure_halt.go with the Phase-25 CR-02 resume time-fence, diverging only in trigger (no FailureProfile gate — exhaustion trigger lives at the Plan 07 call site)"
  - "checkVerifyHalt wired into checkDispatchHolds (planner tier, uniform order Billing->Failure->Verify->Budget->Import) AND TaskReconciler.gateChecks (task tier)"
  - "TaskReconciler.gateChecks migrated onto checkDispatchHolds — closes the Task Import-checked-second order divergence; task-only BUDGET-03 headroom hold and the legacy BudgetExceeded phase fallback preserved as post-delegation checks"
  - "ProjectReconciler's planner dispatch chain gains checkFailureHalt + checkVerifyHalt — closes the missing-FailureHalt-gate todo; the Project planner no longer spends under a conservative halt or a VerifyHalt"
  - "co_occurring_holds_test.go: order-pinning + distinct-halt-class (ESC-03) + headroom-survives-migration regression coverage"
affects: [51-06, 51-07, 51-08, 52-plan-project-verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Third dispatch-hold clone in the BillingHalt -> FailureHalt -> VerifyHalt lineage, with a documented single divergence axis (trigger condition) called out in the header comment"
    - "Shared dispatch-hold chain (checkDispatchHolds) now has 4 callers (Milestone/Phase/Plan directly, Task via gateChecks); tier-only extras applied by the caller AFTER delegating to the shared chain rather than parameterized into it"

key-files:
  created:
    - internal/controller/verify_halt.go
    - internal/controller/verify_halt_test.go
    - internal/controller/co_occurring_holds_test.go
  modified:
    - internal/controller/dispatch_helpers.go
    - internal/controller/task_controller.go
    - internal/controller/project_controller.go

key-decisions:
  - "setVerifyHaltIfNeeded has NO FailureProfile-style gate inside the helper — unlike setFailureHaltIfNeeded, the exhaustion trigger condition lives entirely at the (Plan 07) call site, matching 51-07-PLAN.md's documented interface signature setVerifyHaltIfNeeded(ctx,c,project,taskCompletedAt)"
  - "The task-only BUDGET-03 reservation-headroom hold and the legacy pre-Phase-14 BudgetExceeded phase fallback are applied by gateChecks AFTER delegating to checkDispatchHolds, not folded into the shared chain — checkDispatchHolds has no planner-tier counterpart for either"
  - "No VerifyHalt-at-terminal hook was added to gateChecks' Step 1 terminal short-circuit in this plan — the real exhaustion trigger call site is the verifier-completion branch Plan 07 adds, a different code path than the Failed-phase terminal short-circuit FailureHalt already occupies"

requirements-completed: [ESC-02, ESC-03]

# Metrics
duration: 17min
completed: 2026-07-19
---

# Phase 51 Plan 05: Unify Dispatch-Hold Chains + VerifyHalt Summary

**ConditionVerifyHalt lands as a third-generation halt class (clone of failure_halt.go, CR-02 time-fence preserved) gating both the planner and task dispatch tiers, and the migration closes two carried-forward W-2 todos: Task's gateChecks now delegates to the shared checkDispatchHolds chain instead of checking Import second, and the Project planner chain gains checkFailureHalt/checkVerifyHalt so it stops spending under a conservative halt.**

## Performance

- **Duration:** ~17 min (09:13 -> 09:30 local)
- **Started:** 2026-07-19T13:13:49Z
- **Completed:** 2026-07-19T13:30:32Z
- **Tasks:** 2/2 completed
- **Files modified:** 6 (3 created, 3 modified)

## Accomplishments
- `verify_halt.go` clones `failure_halt.go`'s `checkFailureHalt`/`setFailureHaltIfNeeded` file-for-file as `checkVerifyHalt`/`setVerifyHaltIfNeeded`, keeping the Phase-25 CR-02 resume time-fence verbatim; the single deliberate divergence is the trigger — `setVerifyHaltIfNeeded` has no `FailureProfile` gate, because the loop-exhaustion trigger belongs at the Plan 07 call site, not inside this helper (confirmed against 51-07-PLAN.md's documented interface signature)
- `checkDispatchHolds` (the shared planner-tier chain) gains a `checkVerifyHalt` arm inserted adjacent to `checkFailureHalt`, producing the uniform order Billing(30s) -> Failure(30s) -> Verify(30s) -> Budget(30s) -> Import(5s)
- `TaskReconciler.gateChecks` now delegates Billing/Failure/Verify/Budget/Import to `checkDispatchHolds` instead of its own inline chain that checked Import SECOND — closing `.planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md`. The task-only BUDGET-03 reservation-headroom hold and the legacy pre-Phase-14 `BudgetExceeded` phase fallback (neither has a planner-tier counterpart) are preserved as checks applied immediately after the delegated call
- `ProjectReconciler`'s planner-dispatch hold block gains `checkFailureHalt` + `checkVerifyHalt` (previously had neither) — closing `.planning/todos/pending/2026-07-12-project-dispatch-missing-failurehalt-gate.md`. The Project planner no longer keeps spending on project-planner Jobs while every child level is parked under a conservative halt
- `co_occurring_holds_test.go` adds 10 new plain-Go regression tests (genuinely executed by `-run 'CoOccurring|VerifyHalt|DispatchHolds|Headroom'`, not vacuous against the shared Ginkgo suite): order-pinning across the four `checkDispatchHolds` callers, the concrete Task Billing-vs-Import co-occurring regression, headroom-exhausted-still-parks, legacy-phase-fallback-still-parks, conservative-FailureHalt/VerifyHalt now holding the Project chain, and the ESC-03 distinct-halt-class structural proof (VerifyHalt never stamps FailureHalt, never touches `Project.Status.Phase`, never touches a sibling Task)
- Full `internal/controller` envtest suite (all Ginkgo specs + plain Go tests) stays green after the migration; `make lint` clean

## Task Commits

Each task was committed atomically (TDD RED/GREEN split per task):

1. **Task 1: verify_halt.go — clone failure_halt.go with resume time-fence** - `bc2a4107` (test, RED) + `3471ba91` (feat, GREEN)
2. **Task 2: Unify the dispatch-hold chains** - `7b9fa3b4` (test, RED) + `a2466004` (feat, GREEN)

RED for Task 2 was confirmed by temporarily stashing the three production-file edits and re-running the new test suite: `TestCheckDispatchHolds_VerifyBeforeImport` and `TestReconcileProjectPlannerDispatch_VerifyHalt_Holds` failed genuinely (wrong `RequeueAfter`/no hold) against pre-migration code, then passed after restoring the implementation.

## Files Created/Modified
- `internal/controller/verify_halt.go` - `checkVerifyHalt`/`setVerifyHaltIfNeeded`, clone of `failure_halt.go` with the CR-02 time-fence, no FailureProfile gate
- `internal/controller/verify_halt_test.go` - unit tests: nil-safety, no-FailureProfile-gate divergence, idempotency, time-fence (stale/fresh/unparseable)
- `internal/controller/dispatch_helpers.go` - `checkDispatchHolds` gains the `checkVerifyHalt` arm; doc comment updated to reflect Task's new delegation and the reversed Project-chain note
- `internal/controller/task_controller.go` - `gateChecks` migrated onto `checkDispatchHolds`; task-only headroom + legacy-phase checks kept post-delegation
- `internal/controller/project_controller.go` - planner dispatch block gains `checkFailureHalt` + `checkVerifyHalt`
- `internal/controller/co_occurring_holds_test.go` - order-pinning, co-occurring-holds, distinct-class, and headroom-survival regression tests

## Decisions Made
- `setVerifyHaltIfNeeded` carries no `FailureProfile`-style gate — the exhaustion trigger lives entirely at the future Plan 07 call site (`haltVerify`), matching the 4-argument signature `setVerifyHaltIfNeeded(ctx,c,project,taskCompletedAt)` documented in 51-07-PLAN.md's interfaces section.
- The task-only BUDGET-03 headroom hold and the legacy `BudgetExceeded` phase fallback are NOT folded into `checkDispatchHolds` — they stay task-tier-only checks applied by `gateChecks` immediately after delegating to the shared chain, since `checkDispatchHolds` has no planner-tier counterpart for either.
- No VerifyHalt-at-terminal hook was added to `gateChecks`' Step 1 terminal short-circuit (where `FailureHalt`'s CR-02 hook lives). The real verify-exhaustion trigger fires from the verifier-completion branch Plan 06/07 add — a different code path — so fabricating a hook here with no real trigger would be speculative, out of this plan's stated scope ("the exhaustion TRIGGER... lands in Plan 07").

## Deviations from Plan

None - plan executed as written. One self-correction during execution (not a deviation from the plan's intent, a test-design fix caught during self-review): three of the new regression test names (`TestGateChecks_BillingWinsOverImport_ClosesOrderDivergenceTodo`, `TestGateChecks_LegacyBudgetExceededPhase_StillParks`, `TestReconcileProjectPlannerDispatch_ConservativeFailureHalt_NowHolds`) did not contain any of the plan's mandated verify-filter substrings (`CoOccurring|VerifyHalt|DispatchHolds|Headroom`), so the plan's own acceptance command would have silently skipped them. Renamed to `TestCoOccurringHolds_*` prefixes before committing so all 10 new tests genuinely execute under the mandated filter — caught via the "verify genuinely" discipline (critical_reminders), not left to a later verifier pass.

## Issues Encountered

None beyond the test-naming self-correction above (documented in Deviations, not a blocking issue).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `checkVerifyHalt`/`setVerifyHaltIfNeeded` and the uniform hold chain are in place for Plan 06 (verifier dispatch, forward half) and Plan 07 (verdict consumption, exhaustion trigger call site — the actual `setVerifyHaltIfNeeded` caller).
- No blockers. `internal/controller` envtest suite green (both the shared Ginkgo entry point and the plain-Go tests), `make lint` clean, `go vet ./...` clean.
- Downstream plans reusing the `-run <Name>` pattern against `internal/controller`'s shared `TestControllers` Ginkgo suite should verify their chosen test names actually contain the filter substring being run — a real name/filter mismatch silently skips tests with exit 0, distinct from (but related to) the previously-documented Ginkgo-vacuous-match issue.

---
*Phase: 51-the-task-loop*
*Completed: 2026-07-19*
