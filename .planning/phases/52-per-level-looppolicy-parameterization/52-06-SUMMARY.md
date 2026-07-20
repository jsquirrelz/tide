---
phase: 52-per-level-looppolicy-parameterization
plan: 06
subsystem: infra
tags: [kubernetes, controller-runtime, verification-loop, gate-policy, cli]

# Dependency graph
requires:
  - phase: 52-per-level-looppolicy-parameterization (plan 02)
    provides: LoopPolicy.Level field + LoopLevel enum (api/v1alpha3/loop_types.go)
  - phase: 52-per-level-looppolicy-parameterization (plan 04)
    provides: ResolveLoopPolicy / ResolveVerificationSpec resolver (dispatch_helpers.go)
provides:
  - shared exhaustVerifyLoop D-08 branch point (level_status.go) — the one place onExhaustion differentiates requireApproval (park) from escalate (project-wide halt)
  - Task loop migrated onto ResolveLoopPolicy (repairOrHalt, haltVerify) — SC3 static guard fully armed, zero raw Spec.Verification.MaxIterations/.OnExhaustion reads left in task_controller.go
  - post-approval sentinel preventing executor resurrection after an operator resumes a parked verify-exhausted Task
  - findAwaitingProject at the front of the tide approve discovery chain (Project -> Milestone -> Phase -> Plan -> Task)
affects: [52-07, 52-08, 52-09, 52-10, phase-53]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "D-08 branch point: exhaustVerifyLoop is the ONE function every level's verify-loop exhaustion consults — self-contained mutate-then-patch cycle via patchLevelStatus, callers must invoke it before any of their own Status mutations or the patch base silently drops them"
    - "Verify-exhaustion AwaitingApproval park is re-evaluated every reconcile (LoopStatus.ExitReason-keyed), distinct from the Spec.Gates.Task gate-policy park — required because checkReadinessGates' own approve-annotation consumption is gated behind an explicit gate policy most tasks never set"

key-files:
  created: []
  modified:
    - internal/controller/level_status.go
    - internal/controller/task_controller.go
    - internal/controller/dispatch_helpers_loop_policy_test.go
    - internal/controller/task_verify_loop_test.go
    - internal/controller/task_verify_dispatch_test.go
    - cmd/tide/approve.go
    - cmd/tide/approve_test.go

key-decisions:
  - "exhaustVerifyLoop performs its own mutate-then-patch cycle (mirrors consumeApproveAndResume/patchTaskAwaitingApproval's self-contained shape); haltVerify calls it FIRST, then does its own SEPARATE follow-up patch for the caller-specific ConditionFailed reason + LoopStatus/CompletedAt — two sequential patches, not one, to avoid the DeepCopy-based patch base silently swallowing earlier in-memory mutations"
  - "finishVerifierTerminal (the Task-level completion/failure metric) is skipped on the requireApproval leg of haltVerify — the Task hasn't reached its real terminal yet (parked, not done); the post-approval sentinel's markVerifiedSucceeded fires it exactly once when the loop genuinely closes, avoiding double-counting"
  - "A NEW gateChecks Step 1b re-checks the approve-task annotation every reconcile for LoopStatus.ExitReason-carrying AwaitingApproval parks — not explicitly named in the plan's action text but structurally required: without it, checkReadinessGates' gate-policy-only annotation consumption (gated behind Spec.Gates.Task==approve/pause, which PolicyAuto — the common default — skips entirely) would let dispatch silently resume on the very next reconcile regardless of operator approval, making onExhaustion:requireApproval a complete no-op"
  - "ConditionFailed is never stamped on the requireApproval leg (only the WaveOrLevelPaused/ReasonVerifyExhausted park condition) — a merely-parked Task carrying ConditionFailed=True would contradict the park state"
  - "5 pre-existing Phase-51 test fixtures (task_verify_loop_test.go) had onExhaustion:\"requireApproval\" as an unread placeholder value; pinned to \"escalate\" explicitly so their VerifyHalted/ConditionVerifyHalt assertions keep testing the escalate leg now that the field is genuinely honored"

patterns-established:
  - "Pattern: shared D-08 branch point — a single exhaustVerifyLoop(ctx, c, project, obj, conditions, phasePtr, level, policy, completedAt, message) function every level's verify-loop-exhaustion call site invokes; per-value onExhaustion differentiation lives ONLY there"

requirements-completed: [ESC-01]

# Metrics
duration: 48min
completed: 2026-07-20
---

# Phase 52 Plan 06: Shared exhaustVerifyLoop + Task Migration + Post-Approval Sentinel Summary

**Task loop's `onExhaustion: requireApproval` now genuinely parks at AwaitingApproval instead of freezing the whole project — via one shared `exhaustVerifyLoop` branch point in `level_status.go`, a `gateChecks` re-check that re-evaluates the park's approve annotation every reconcile, and a post-approval sentinel that routes a resumed Task straight to `markVerifiedSucceeded` instead of resurrecting the executor.**

## Performance

- **Duration:** ~48 min
- **Completed:** 2026-07-20
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Closed Phase 51's declared-but-uniform `onExhaustion` gap: `requireApproval` and `escalate` now genuinely diverge, differentiated in exactly one place (`exhaustVerifyLoop`) that every terminal non-APPROVED verify exit consults uniformly (BLOCKED, unreadable envelope, anti-gaming escalation, MaxIterations exhaustion)
- Migrated `repairOrHalt`'s raw `task.Spec.Verification.MaxIterations` read onto `ResolveLoopPolicy` — the SC3 static guard (`TestNoDirectVerificationPolicyReads`) now covers the whole `internal/controller` package with zero exclusions beyond `dispatch_helpers.go` itself
- Added the structurally-necessary re-check that makes the `requireApproval` park actually hold across reconciles (it is triggered by loop exhaustion, not the `Spec.Gates.Task` gate-policy machinery that `checkReadinessGates` already re-evaluates)
- Added the post-approval sentinel (T-52-15 threat mitigation): an approved exhausted loop closes via `markVerifiedSucceeded`, never resurrecting the executor — proven live with an unchanged Job count across the approve-resume transition
- Extended `tide approve`'s discovery chain with `findAwaitingProject`, inserted first (Project is the hierarchy root)

## Task Commits

Each task was committed atomically:

1. **Task 1: Shared exhaustVerifyLoop + Task migration + post-approval sentinel** - `53712cdf` (feat)
2. **Task 2: findAwaitingProject + envtest coverage for both exhaustion arms** - `47ac8d43` (feat)

_Note: Task 1's commit also carries the deviation fixture fix to `task_verify_loop_test.go` (5 pre-existing specs), since those tests would otherwise fail against the newly-armed SC3 guard's own behavior-change bar._

## Files Created/Modified

- `internal/controller/level_status.go` - Adds `exhaustVerifyLoop`, the D-08 branch point
- `internal/controller/task_controller.go` - `repairOrHalt` migrated onto `ResolveLoopPolicy`; `haltVerify` delegates its terminal patch to `exhaustVerifyLoop`; `gateChecks` gains Step 1b (verify-exhaustion park re-check) and the Running-phase post-approval sentinel
- `internal/controller/dispatch_helpers_loop_policy_test.go` - SC3 guard's `task_controller.go` exclusion removed
- `internal/controller/task_verify_loop_test.go` - 5 pre-existing fixtures pinned `onExhaustion: "escalate"` explicitly (deviation, see below)
- `internal/controller/task_verify_dispatch_test.go` - 3 new specs proving both `onExhaustion` arms at Task level
- `cmd/tide/approve.go` - `findAwaitingProject` added, inserted first in `approveLevel`'s chain
- `cmd/tide/approve_test.go` - `TestApproveLevelDiscoversAwaitingProjectFirst`

## Decisions Made

See `key-decisions` in frontmatter. Summary: `exhaustVerifyLoop` is a self-contained mutate-then-patch primitive (like `consumeApproveAndResume`); `haltVerify` calls it first, then does its own follow-up patch for `ConditionFailed`/`LoopStatus`/`CompletedAt` (skipped entirely on the `requireApproval` leg, since a park should never also carry `ConditionFailed=True`); a new `gateChecks` re-check step (not spelled out in the plan's action text, but structurally required) makes the park actually hold.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical Functionality] Added gateChecks Step 1b — the verify-exhaustion park re-check**
- **Found during:** Task 1 (implementing exhaustVerifyLoop + haltVerify migration)
- **Issue:** The plan's action text only specifies the post-approval sentinel (Running phase + non-empty ExitReason -> markVerifiedSucceeded). Tracing the full reconcile path revealed that a Task parked at AwaitingApproval via the NEW `exhaustVerifyLoop` requireApproval branch would never actually be *held* there: `gateChecks` has no phase-based short-circuit for AwaitingApproval, and `checkReadinessGates`' own approve-annotation consumption is gated behind `Spec.Gates.Task`/project default equal to `"approve"`/`"pause"` (`gates.PolicyApprove`/`PolicyPause`) — the common default is `PolicyAuto`, which skips that whole check. Without a re-check, dispatch would silently resume on the very next reconcile regardless of operator approval, making `onExhaustion: requireApproval` a complete no-op.
- **Fix:** Added a new `gateChecks` Step 1b: when `Phase == AwaitingApproval && LoopStatus.ExitReason != ""` (a signal that only a verify-loop park sets, never a gate-policy park), re-check `gates.CheckApprove(task, "task")` every reconcile; if absent, re-park (no dispatch); if present, resume via `consumeApproveAndResume` (the same two-step every other AwaitingApproval park already uses).
- **Files modified:** internal/controller/task_controller.go
- **Verification:** New envtest spec "requireApproval parks at AwaitingApproval (no project halt), then Succeeds on tide approve with no executor re-dispatch" proves the full park -> approve -> resume -> sentinel -> Succeeded flow, with the Job count asserted unchanged.
- **Committed in:** 53712cdf (Task 1 commit)

**2. [Rule 1 - Bug] Fixed 5 pre-existing Phase-51 test fixtures that would fail under the newly-armed onExhaustion differentiation**
- **Found during:** Task 1 verification (`--ginkgo.focus='Verif'` run)
- **Issue:** `task_verify_loop_test.go`'s Phase-51-era specs set `OnExhaustion: "requireApproval"` on their `VerificationSpec` fixtures as an unread placeholder (the field was declared but never consulted in Phase 51). Now that `haltVerify` genuinely differentiates on `policy.EscalationPolicy`, those specs' assertions of `LevelPhaseVerifyHalted` + project-wide `ConditionVerifyHalt=True` broke — the fixture's `requireApproval` value now correctly routes to a park instead.
- **Fix:** Pinned `OnExhaustion: "escalate"` explicitly on the 5 affected fixtures (an unreadable-envelope halt, a BLOCKED-verdict halt, a MaxIterations-exhaustion halt, the ESC-03 distinct-halt-class spec, and the anti-gaming true-positive escalation) so they keep exercising the escalate leg they were written to test. The requireApproval leg gets its own dedicated coverage in the new Task 2 specs.
- **Files modified:** internal/controller/task_verify_loop_test.go (not in the plan's `files_modified` list, but required to keep the plan's own "existing Phase-51 specs pass unmodified" acceptance bar green)
- **Verification:** Full `internal/controller` envtest suite green (260/260 specs) both before and after this fix's surrounding changes.
- **Committed in:** 53712cdf (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 missing critical functionality, 1 bug/regression fix)
**Impact on plan:** Both were necessary for the plan's own stated correctness bar (a functioning requireApproval park; a green existing-suite regression bar). No scope creep — both are narrowly targeted at the D-08 behavior this plan introduces.

## Issues Encountered

- A flaky first pass on the new "requireApproval -> approve -> Succeeded" envtest spec: `reconcileWithRetry`'s fixed-count reconcile loop raced the controller-runtime informer cache's propagation lag between the approve-annotation patch and the subsequent Reconcile call reading it. Fixed by replacing the fixed-count retry with `Eventually`-wrapped reconcile-and-assert loops (mirroring this file's own `waitForJobTerminalInCache` idiom) at both the annotation-visibility wait and the two-step (Running -> Succeeded) transition. Confirmed non-flaky across 4 repeated runs plus 2 full unfiltered-suite runs.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Task loop's `onExhaustion` differentiation is complete and proven both live (envtest) and structurally (the D-08 branch point every future level-verify dispatch site will reuse).
- 52-07/52-08/52-09/52-10 (Plan-check and Phase/Milestone/Project level-verify dispatch, per PATTERNS.md) can now call the SAME `exhaustVerifyLoop` for their own exhaustion branches — no new differentiation logic needed, only new call sites plus `ResolveLoopPolicy`'s already-generalized per-level defaults.
- `cmd/tide/approve.go`'s discovery chain is now complete at the Project level ahead of any future project-level gate-policy or level-verify dispatch work landing in later plans.
- No blockers.

---
*Phase: 52-per-level-looppolicy-parameterization*
*Completed: 2026-07-20*

## Self-Check: PASSED

All 7 modified source/test files and both task commit hashes (`53712cdf`, `47ac8d43`) verified present on disk / in `git log --oneline --all`.
