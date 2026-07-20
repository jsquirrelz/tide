---
phase: 52-per-level-looppolicy-parameterization
plan: 09
subsystem: infra
tags: [kubernetes, controller-runtime, verification-loop, plan-check, re-plan, gate-policy]

# Dependency graph
requires:
  - phase: 52-per-level-looppolicy-parameterization (plan 07)
    provides: Plan Verifying sub-state + plan-check dispatch/consume state machine (checkPlanVerifyingState/dispatchPlanVerifier/handlePlanVerifierCompletion) with the single marked `// 52-09 replaces this` seam
provides:
  - "repairOrHaltPlan (D-04/D-05 decision tree): severity-weighted stall check first, then the MaxIterations boundary — neither burns an iteration — then dispatchPlanRepair"
  - "severityScore/replanStalled — pure, plain-Go-tested D-05 stall-detection primitives (high-severity findings weighted 10x over raw findings count, strictly-decreasing requirement)"
  - "dispatchPlanRepair (D-04 re-plan): stamps the bounded replan-findings annotation, deletes the rejected attempt's child Tasks (RESEARCH Pitfall 3), bumps LoopStatus.Iteration (D-06's quality-re-plan counter), clears Phase off Verifying"
  - "Attempt-aware planner dispatch: reconcilePlannerDispatch's jobName/attempt and handlePlannerJobCompletion's planJobName now derive from LoopStatus.Iteration+1 instead of a hardcoded attempt-1, enabling a genuine second (and further) planner attempt"
  - "RepairFindings wiring: the replan-findings annotation decodes into EnvelopeIn.RepairFindings at planner dispatch time (consumed by the 52-03-pinned plan_planner.tmpl block) and clears once the fresh attempt materializes"
affects: [52-10, 52-11]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Plan-level re-plan loop mirrors Task's repairOrHalt/dispatchRepairAttempt shape one level up, with ONE new piece: D-05 stall detection ordered BEFORE the MaxIterations boundary check"
    - "Bounded single-current-iteration findings transport via a Plan annotation (tideproject.k8s/replan-findings) — gates-annotation precedent, set/decode/clear lifecycle, never .status (LOOP-03)"
    - "LoopStatus.Iteration doubles as the planner-attempt identity (no separate infra-retry counter needed — Plan's planner dispatch had none to preserve, unlike Task)"

key-files:
  created:
    - internal/controller/plan_verify_loop_test.go
  modified:
    - internal/controller/plan_controller.go
    - internal/controller/plan_verify_dispatch_test.go

key-decisions:
  - "severityScore/replanStalled take a *EvaluationSummary (not a value type) for 'no previous evaluation' — nil maps directly to plan.Status.LoopStatus.LastEvaluation's own pointer type and 'never stalled', avoiding a zero-value sentinel ambiguity"
  - "The D-05 severity weighting formula is high×10 + findingsCount (score(3,1)=13) — Claude's Discretion per D-05's only structural requirement (strictly-decreasing), matches the plan's own worked example"
  - "RepairFinding.Summary sources from Finding.Evidence (falling back to SuggestedFix) — RepairFinding has no Dimension/Evidence/SuggestedFix fields of its own by design (pkg/dispatch/envelope.go's own doc comment: it is the compact planner-prompt summary, not the verifier's wire format)"
  - "exhaustPlanVerifyLoop's existing 4-arg signature (message-only) is reused UNCHANGED for both of repairOrHaltPlan's exhaustion legs (stall + MaxIterations boundary) — both route through ExitEscalated/ReasonVerifyExhausted with a distinct message string, per the plan's own 'route to exhaustVerifyLoop, same as 52-07's path' instruction; no new ExitReason value was needed or added (this plan touches no api/v1alpha3 files)"
  - "planJobName inside handlePlannerJobCompletion (used for the PlanRolledUpUID exactly-once budget-rollup marker) was ALSO fixed to the Iteration-derived formula, even though only reconcilePlannerDispatch's two sites were named in the plan's own acceptance grep — the SAME hardcoded '-1' literal exists there and the plan's own acceptance criterion (`grep -n \"attempt := 1\\|tide-plan-%s-1\"` = 0) requires it to be gone regardless"

patterns-established:
  - "Bounded annotation findings transport (T-52-29 DoS mitigation): cap 10 entries, hard-byte-cut each Summary to 300 bytes, 'present iff informative' contract (empty verdict clears rather than stamps an empty array)"

requirements-completed: [ESC-01]

# Metrics
duration: 48min
completed: 2026-07-20
---

# Phase 52 Plan 09: Plan-Check Re-Plan Loop (D-04/D-05/D-06) Summary

**Replaced 52-07's single marked conservative seam with the real re-plan decision tree: a REPAIRABLE plan-check verdict deletes the rejected attempt's child Tasks and re-dispatches a findings-seeded planner attempt (exactly one at default `maxIterations:1`), while a severity-weighted stall check halts a non-improving re-plan early even when `maxIterations` would allow another attempt — proven live in envtest, including a genuine second planner Job dispatch and its own Verifying/plan-check cycle.**

## Performance

- **Duration:** ~48 min
- **Started:** 2026-07-20T04:47:26-04:00 (base commit)
- **Completed:** 2026-07-20T05:35:26-04:00
- **Tasks:** 2
- **Files modified:** 3 (1 created, 2 modified)

## Accomplishments

- `severityScore`/`replanStalled` — the D-05 severity-weighted stall-detection primitives (high-severity findings weighted 10x, strictly-decreasing requirement), pinned by plain-Go subtests covering every named behavior case (including the "no previous evaluation, Iteration 0, never stalled" edge).
- `repairOrHaltPlan` replaces 52-07's marked seam: stall check first, then the `MaxIterations` boundary (`plan.Status.LoopStatus.Iteration >= policy.MaxIterations`), then `dispatchPlanRepair` — neither check consumes an iteration.
- `dispatchPlanRepair` implements the full D-04 re-plan: stamps the bounded `tideproject.k8s/replan-findings` annotation, deletes every child Task owned by the Plan (unblocking `reconcilePlannerDispatch`'s tasks-exist early-return — RESEARCH Pitfall 3), bumps `LoopStatus.Iteration`, and clears `Phase` off `Verifying` back to `""`.
- `reconcilePlannerDispatch`'s planner-attempt identity (both the `jobName` Get and the dispatch-tail `attempt` var) and `handlePlannerJobCompletion`'s `planJobName` now derive from `int(plan.Status.LoopStatus.Iteration) + 1` instead of a hardcoded attempt-1 — a genuine second (and further) planner Job can now dispatch.
- The bounded findings block decodes into `EnvelopeIn.RepairFindings` at planner dispatch time (consumed by the 52-03-pinned `plan_planner.tmpl {{range .RepairFindings}}` block) and clears once the fresh attempt's planner Job completes (the consumption point).
- 4 new Ginkgo specs (`Describe("RePlan", ...)`) pin: exactly one re-plan at default `maxIterations:1` with the exact Job-name progression `tide-plan-<uid>-1` → `-2` and no `-3`; the rejected attempt's child Task is deleted outright (never merely un-dispatched — T-52-27); severity-weighted stall detection halting early at `maxIterations:2` without consuming the remaining iteration; and an improving-score control proving the loop proceeds and consumes the remaining iteration when it should.

## Task Commits

Each task was committed atomically:

1. **Task 1: repairOrHaltPlan + dispatchPlanRepair + attempt-aware planner dispatch** - `ddfe29ac` (feat)
2. **Task 2: Ginkgo RePlan specs — one re-plan, stale-task invariant, stall at maxIterations:2** - `905f9db4` (test)

## Files Created/Modified

- `internal/controller/plan_controller.go` - `severityScore`/`replanStalled`/`truncateReplanString`/`boundedRepairFindings` (pure functions), `setReplanFindingsAnnotation`/`decodeReplanFindings`/`clearReplanFindingsAnnotation` (annotation lifecycle), `repairOrHaltPlan`/`dispatchPlanRepair`, `reconcilePlannerDispatch`'s attempt-aware `jobName`/`attempt`/RepairFindings injection, `handlePlannerJobCompletion`'s attempt-aware `planJobName` + annotation-clear call, `handlePlanVerifierCompletion`'s REPAIRABLE/APPROVED-deterministic-failure legs routed through `repairOrHaltPlan`
- `internal/controller/plan_verify_loop_test.go` - new: `TestSeverityScore`/`TestReplanStalled` plain-Go subtests (mirrors `task_verify_loop_test.go`'s pure-function-test-file precedent, Pitfall 5)
- `internal/controller/plan_verify_dispatch_test.go` - new `Describe("RePlan", ...)` with 4 specs, `driveThroughPlanRepairCycle`/`completePlanPlannerJobAttempt`/`waitForJobTerminalCacheSync` helpers

## Decisions Made

See `key-decisions` in frontmatter. Summary: `severityScore`/`replanStalled` take a pointer `*EvaluationSummary` (matching `LoopStatus.LastEvaluation`'s own field type) rather than the value-type signature PATTERNS.md sketched, so "no previous evaluation" maps directly to `nil` without a zero-value sentinel; the D-05 weighting is `high×10 + findingsCount`; `RepairFinding.Summary` sources from `Finding.Evidence` (falling back to `SuggestedFix`) since `RepairFinding` deliberately carries no `Dimension`/`Evidence` fields of its own; `exhaustPlanVerifyLoop`'s existing signature is reused unchanged for both new exhaustion legs (message differs, `ExitEscalated`/`ReasonVerifyExhausted` stays uniform) — no schema change, no new `ExitReason` value, consistent with this plan's `files_modified` scope (controller + tests only).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `handlePlannerJobCompletion`'s `planJobName` was also hardcoded to attempt-1, silently breaking the exactly-once budget-rollup marker for any re-planned completion**
- **Found during:** Task 1, implementing the attempt-aware dispatch sites
- **Issue:** The plan's own action text named "BOTH hardcoded attempt-1 sites" as `reconcilePlannerDispatch`'s `attempt := 1` and the `tide-plan-%s-1` Sprintf — but the literal `tide-plan-%s-1` Sprintf pattern actually appears at TWO call sites in the file (`reconcilePlannerDispatch`'s `jobName`, and `handlePlannerJobCompletion`'s `planJobName`, used for the `PlanRolledUpUID` exactly-once budget-rollup marker comparison). Left unfixed, a re-planned (attempt≥2) planner completion would always compute `planJobName == "tide-plan-<uid>-1"`, which never matches the real attempt-2+ Job that just completed — silently short-circuiting `budget.RollUpUsage` for every re-planned attempt's spend.
- **Fix:** Replaced with the same `fmt.Sprintf("tide-plan-%s-%d", plan.UID, int(plan.Status.LoopStatus.Iteration)+1)` formula used at the other two attempt-aware sites.
- **Files modified:** `internal/controller/plan_controller.go`
- **Verification:** The plan's own acceptance grep (`grep -n "attempt := 1\|tide-plan-%s-1" internal/controller/plan_controller.go` = 0) now passes; full envtest suite (270/270) green including the pre-existing budget-rollup coverage.
- **Committed in:** `ddfe29ac` (Task 1 commit)

**2. [Rule 1 - Bug] A direct-client/cached-client cache-sync race flaked Job-completion-then-reconcile sequences under load (~1 in a handful of runs)**
- **Found during:** Task 2, running the new RePlan specs concurrently with other heavy processes (full-suite background run + golangci-lint build) — reproduced live in a full unfiltered-suite run, not just a focused re-run
- **Issue:** `completePlanVerifierJob`/`completePlanPlannerJobAttempt` patch a Job's terminal status via the DIRECT `k8sClient`, then the test immediately calls `r.Reconcile(...)`, whose reconciler reads the SAME Job via the manager's CACHED `mgrClient` (`Client: mgrClient` in `newVerifyDispatchPlanReconciler`). Under system load the informer cache can lag the direct-client write by enough to make `isJobTerminal` return false on the very next reconcile — observed live as a re-planned attempt's second REPAIRABLE verdict never being consumed (Plan stuck at `Verifying` instead of exhausting to `VerifyHalted`), and separately as two PRE-EXISTING 52-07 `PlanCheck` specs each flaking on a different assertion under the same load.
- **Fix:** Added `waitForJobTerminalCacheSync` (Eventually-polls `mgrClient.Get` + `isJobTerminal` before returning) and wired it into `completePlanVerifierJob`/`completePlanPlannerJobAttempt` — hardens every existing and new caller of these two shared helpers, not just the new RePlan specs.
- **Files modified:** `internal/controller/plan_verify_dispatch_test.go`
- **Verification:** 4 concurrent full re-runs of `--ginkgo.focus='RePlan|PlanCheck'` under artificial load, all green; a subsequent full unfiltered suite run (270/270) also green.
- **Committed in:** `905f9db4` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — a correctness bug and a reproducible test-infrastructure race)
**Impact on plan:** Both fixes were required for the plan's own acceptance criteria (grep=0) and this project's explicit NO-FLAKE-TOLERANCE bar to hold for real. No scope creep — neither fix touches functionality outside this plan's declared files.

## Issues Encountered

None beyond the two deviations above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- SC1 is fully live: the plan-check loop has its own counter (`LoopStatus.Iteration`, D-06), defaults to exactly one findings-seeded re-plan (D-04), and halts early on a non-improving re-plan via severity-weighted stall detection (D-05) — every claim pinned by an envtest spec against a real envtest apiserver.
- `go build ./...`, `go vet ./internal/controller/...`, `gofmt -l` (clean), `bin/golangci-lint run ./internal/controller/...` (0 issues), and the full unfiltered `internal/controller` envtest suite (270/270) are all clean at this plan's HEAD.
- Plan 52-10/52-11 (Phase/Milestone/Project level-verify, per 52-08's own `affects` list) are unaffected by this plan's scope — this plan touched only `plan_controller.go` and its own test files.
- No blockers.

---
*Phase: 52-per-level-looppolicy-parameterization*
*Completed: 2026-07-20*

## Self-Check: PASSED

All 3 modified/created source files and both task commit hashes (`ddfe29ac`, `905f9db4`) verified present on disk / in `git log --oneline --all`.
