---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
plan: 05
subsystem: controller
tags: [dispatch-gates, refactoring, budget-halt, billing-halt, failure-halt, import-gate, controller-runtime]

# Dependency graph
requires:
  - phase: 41-04
    provides: LevelPhase* status constants and the internal/controller literal sweep (checkDispatchHolds reads tideprojectv1alpha3.LevelPhaseAwaitingApproval, not string literals)
provides:
  - Shared checkDispatchHolds gate-chain helper in dispatch_helpers.go (Billing->Failure->Budget->Import, 30s/30s/30s/5s)
  - MilestoneReconciler, PhaseReconciler, PlanReconciler dispatching project-scoped holds through one implementation
  - Documented, non-migrated divergence at TaskReconciler's gate chain (comment + follow-up todo)
affects: [refactoring-review phase closeout, any future work touching dispatch-hold ordering/requeue intervals]

# Tech tracking
tech-stack:
  added: []
  patterns: ["shared cross-reconciler gate helper co-located in dispatch_helpers.go (mirrors checkParentApproval's existing shape)"]

key-files:
  created:
    - .planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md
  modified:
    - internal/controller/dispatch_helpers.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go

key-decisions:
  - "checkDispatchHolds logs uniformly with (level, objName, \"project\", project.Name) per the plan's <interfaces> contract, even though pre-refactor Phase/Plan sites did not log a \"project\" key at all (only Milestone did) -- see Deviations."
  - "Task's chain is documented, not migrated -- Import stays SECOND at that site, preserving the phase's non-breaking boundary."

requirements-completed: [REFAC-07]

# Metrics
duration: ~20min (commit-to-commit span; excludes plan/pattern-map reading time)
completed: 2026-07-12
---

# Phase 41 Plan 05: Extract shared dispatch-holds gate chain Summary

**One shared `checkDispatchHolds` helper now carries the Billing->Failure->Budget->Import project-scoped hold chain for Milestone, Phase, and Plan; Task's structurally divergent Import-second order is documented (comment + todo), not silently normalized.**

## Performance

- **Tasks:** 3/3 completed
- **Files modified:** 5 (4 controller files + 1 new todo)
- **Commits:** 3 task commits (`96dd23b`, `ba715b0`, `7e9bca1`)

## Accomplishments
- `checkDispatchHolds(ctx, project, level, objName) (held bool, result ctrl.Result)` landed in `dispatch_helpers.go`, mirroring `checkParentApproval`'s existing shared-gate shape in the same file.
- MilestoneReconciler, PhaseReconciler, and PlanReconciler all now call the one helper instead of four inline checks apiece; the `earlyProject` fetch, `gates.CheckRejected` → `patch*Rejected`, and (for Phase/Plan) `checkParentApproval` stay unchanged at each call site, in the same position relative to pool acquire (Pitfall 2 preserved).
- TaskReconciler's inline chain is untouched; a new comment block at the head of its gate chain cross-references `checkDispatchHolds` and the follow-up todo, explaining exactly how its order diverges (Import second, plus a task-only reservation-headroom hold).
- New candidate-finding todo (`2026-07-12-task-dispatch-gate-order-divergence.md`) captures the divergence and a two-option fix fork for a future phase, cross-linking the sibling `project-dispatch-missing-failurehalt-gate.md` finding already in the pending queue.

## Task Commits

1. **Task 1: Author checkDispatchHolds and migrate MilestoneReconciler** - `96dd23b` (feat)
2. **Task 2: Migrate PhaseReconciler** - `ba715b0` (feat)
3. **Task 3: Migrate PlanReconciler; document the Task-site divergence** - `7e9bca1` (feat)

## Files Created/Modified
- `internal/controller/dispatch_helpers.go` - new `checkDispatchHolds` helper (project-scoped Billing/Failure/Budget/Import chain); added `time`, `k8s.io/apimachinery/pkg/api/meta`, `ctrl "sigs.k8s.io/controller-runtime"`, and `internal/budget` imports
- `internal/controller/milestone_controller.go` - four inline hold checks replaced with one `checkDispatchHolds` call; `earlyProject` fetch + Reject gate unchanged
- `internal/controller/phase_controller.go` - same migration; `checkParentApproval` stays first at the call site
- `internal/controller/plan_controller.go` - same migration, adapted to the 3-tuple `(ctrl.Result, bool, error)` return shape this reconciler's helper method uses
- `internal/controller/task_controller.go` - no logic change; added a cross-reference comment at the head of the gate chain documenting the Import-position divergence
- `.planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md` - new candidate-finding todo

## Decisions Made
- **Uniform "project" log key across all three migrated sites.** Pre-refactor, only Milestone's inline chain logged `"project", ms.Spec.ProjectRef`; Phase and Plan's inline chains logged only their own object name with no project reference at all. The plan's `<interfaces>` contract specified a single log shape (`level, objName, "project", project.Name`) for the shared helper, and since `project.Name` equals the value Milestone logged (the fetch keys on that exact name), this was implemented literally as written. Net effect: Phase and Plan gain a `"project"` key-value pair in these four log lines that they didn't previously emit. The message TEXT — the load-bearing grep target per CLAUDE.md and the plan's must_haves — is unchanged at all three sites; only a KV metadata field was added at two of them. No test in the repo asserts on the specific KV shape (verified via grep across `*_test.go`), so this is a non-breaking observability improvement, not a behavior change to gate logic, ordering, or requeue timing.
- **Task's chain stays inline per the plan's resolved scope boundary.** Confirmed at HEAD: Task's order is Reject -> ParentApproval(5s) -> Import(5s, SECOND) -> Billing(30s) -> Failure(30s) -> Budget(30s) -> reservation-headroom(30s, task-only), diverging from the planner tier's Reject -> Billing -> Failure -> Budget -> Import(LAST). Documented, not normalized.

## Deviations from Plan

### Notes (not auto-fixes — no code behavior changed)

**1. Acceptance-criteria grep count assumed 4 pre-existing sites; a 5th (Project) exists.**
- **Found during:** Task 3 verification
- **Issue:** Task 3's acceptance criteria asserts `grep -rc 'dispatch held: project billing halt' internal/controller/*.go` (non-test) totals exactly 2, "down from 4." Actual count after all three migrations is **3** (helper in `dispatch_helpers.go` + Task's own inline chain + `project_controller.go`'s own separate inline chain). `project_controller.go` has its own project-scoped dispatch-hold chain (confirmed present since at least commit `8ffd1f9`, wave 3/41-04 — pre-existing, untouched by this plan) that neither the seed, RESEARCH.md Pitfall 1, nor this plan's `<files>` scope ever counted. It is not in this plan's migration scope (only Milestone/Phase/Plan/Task were ever in scope) and is already tracked as a separate finding: `.planning/todos/pending/2026-07-12-project-dispatch-missing-failurehalt-gate.md` (filed during this same plan-check cycle, source-dated 2026-07-12, noting Project's chain also lacks a `checkFailureHalt` call entirely).
- **Action taken:** None — confirmed via `git log -1 -- internal/controller/project_controller.go` and a prior-commit content check that this is pre-existing and out of this plan's scope. No fix applied; verified the true reduction this plan delivers is 3 sites (Milestone+Phase+Plan's shared inline chain, now one call each into the helper) collapsing to 1 shared implementation, Task's inline chain unchanged, Project's separate inline chain unchanged.
- **Files modified:** None (verification-only finding)
- **Verification:** `grep -rn 'dispatch held: project billing halt' internal/controller/*.go | grep -v _test` shows exactly `dispatch_helpers.go`, `project_controller.go`, `task_controller.go` — 3 total, matching the corrected count.

**2. gofmt import reordering in dispatch_helpers.go (Task 3 commit)**
- **Found during:** `make test-int-fast`'s `fmt` prerequisite target
- **Issue:** `go fmt` reordered the `k8s.io/apimachinery/pkg/api/errors` / `k8s.io/apimachinery/pkg/api/meta` import lines alphabetically after Task 1's edit left them in a non-canonical order.
- **Fix:** Included the gofmt diff in Task 3's commit (no logic change, import ordering only).
- **Files modified:** `internal/controller/dispatch_helpers.go`
- **Committed in:** `7e9bca1`

---

**Total deviations:** 0 code-behavior changes; 1 documentation note (acceptance-criteria arithmetic gap, pre-existing 5th site) + 1 gofmt auto-fix (import ordering).
**Impact on plan:** None on scope or behavior invariance. The must_haves' actual load-bearing claims (gate order, requeue intervals, log message text byte-identity) all verified true.

## Verification

- `go build ./internal/...` — clean after every task (whole-repo `go build ./...` fails on a pre-existing, unrelated `cmd/tide-demo-init` embed-pattern gap, logged in this phase's `deferred-items.md` by plan 41-03; confirmed unrelated via `git log`/prior-commit archaeology, out of this plan's scope).
- `go vet ./internal/...` — clean.
- `gofmt -l internal/controller/*.go` — no output (all formatted).
- `go test ./internal/controller/... -run 'Gates|Halt|Budget|Import' -count=1 -v` — 45/45 PASS, 0 FAIL, after each of the three tasks.
- `make test-int-fast` (Task 3 close) — `MAKE_EXIT=0`; Ginkgo `Ran 56 of 56 Specs in 66.758 seconds` / `SUCCESS! 56 Passed | 0 Failed | 0 Pending | 0 Skipped`; no `--- FAIL` or `^FAIL\s` lines in the log.
- `grep -c 'func checkDispatchHolds' internal/controller/dispatch_helpers.go` = 1.
- `grep -c 'dispatch held: project billing halt' internal/controller/{milestone,phase,plan}_controller.go` = 0 each (moved into the helper).
- `grep -c 'checkDispatchHolds(' internal/controller/{phase,plan}_controller.go` = 1 each.
- `grep -c 'checkDispatchHolds' internal/controller/task_controller.go` = 2 (comment references only); `grep -cE 'if held, res := checkDispatchHolds' internal/controller/task_controller.go` = 0 (no call).
- `test -f .planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md` succeeds.

## Issues Encountered
None blocking. See Deviations for the two non-blocking notes.

## Next Phase Readiness
- Seed item 7 closed for the planner tier (Milestone/Phase/Plan); the next project-scoped gate addition is now a one-file change in `dispatch_helpers.go` instead of three.
- Two follow-up findings now queued in `.planning/todos/pending/` for a future phase to resolve together: Task's Import-position divergence (this plan) and Project's missing `checkFailureHalt` gate (sibling finding, same plan-check cycle) — both touch the same "unify the fifth/sixth dispatch chain" decision space.
- Phase 41 has one plan remaining per its wave structure before milestone v1.0.7 closeout review.

---
*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Completed: 2026-07-12*

## Self-Check: PASSED

All 5 modified/created controller and todo files verified present on disk; all 4 commit hashes (`96dd23b`, `ba715b0`, `7e9bca1`, `e6d3b19`) verified present in `git log --oneline --all`.
