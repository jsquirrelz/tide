---
phase: 52-per-level-looppolicy-parameterization
plan: 07
subsystem: infra
tags: [kubernetes, controller-runtime, verification-loop, plan-check, gate-policy]

# Dependency graph
requires:
  - phase: 52-per-level-looppolicy-parameterization (plan 02)
    provides: level-generic VerifierJobName(level, parentUID, attempt) + JobKindVerifier reading opts.ParentObj
  - phase: 52-per-level-looppolicy-parameterization (plan 03)
    provides: plan_verifier.tmpl goal-backward rubric + the D-09-pinned PlanGoal/Children render-data contract
  - phase: 52-per-level-looppolicy-parameterization (plan 04)
    provides: ResolveLoopPolicy / ResolveVerificationSpec resolver + PlannerReconcilerDeps.VerifierImage/Reservations/ReserveEstimateCents
  - phase: 52-per-level-looppolicy-parameterization (plan 05)
    provides: BuildOptions.WorktreeCheckoutImage/WorktreeBranch — the level-verify worktree-checkout init container
  - phase: 52-per-level-looppolicy-parameterization (plan 06)
    provides: shared exhaustVerifyLoop D-08 branch point (level_status.go)
provides:
  - "Plan Verifying sub-state (D-03): a Locked-contract Plan holds child Task dispatch until an independent plan-check verifier approves"
  - "checkParentApproval Verifying OR-clause — the sole, structural D-03 hold mechanism (no new call site)"
  - "checkPlanVerifyingState / dispatchPlanVerifier / buildPlanVerifierEnvelopeIn / handlePlanVerifierCompletion / markPlanVerifiedApproved / exhaustPlanVerifyLoop — the plan-check dispatch/consume state machine, riding every D-10 rail (cap-before-reserve, ReservationStore, fail-closed ClassifyVerdict, EVALUATOR sibling span, worktree-checkout init container)"
  - "A second, reachable Verifying-entry site inside reconcileWaveMaterialization closing a real reachability gap in handlePlannerJobCompletion's own ChildCount-gated entry (see Deviations)"
affects: [52-09, 52-10, 52-11]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Plan-level verifier state machine mirrors Task's checkVerifyingState/dispatchVerifier/handleVerifierCompletion function-for-function, one level up — same D-10 rails, no re-implementation"
    - "planVerifierRenderData (embeds pkgdispatch.EnvelopeIn + PlanGoal + Children) — the production render-data struct for plan_verifier.tmpl's D-09-pinned contract; PlanGoal sources from Project.Spec.OutcomePrompt (outcomePromptOf), the only authored goal text anywhere in the schema"
    - "Conservative intermediate: REPAIRABLE and an APPROVED-with-deterministic-gate-failure verdict both route through the shared exhaustVerifyLoop (never markPlanVerifiedApproved) via a single marked seam — 52-09 replaces that leg with repairOrHaltPlan's findings-seeded re-plan"

key-files:
  created:
    - internal/controller/plan_verify_dispatch_test.go
  modified:
    - internal/controller/plan_controller.go
    - internal/controller/dispatch_helpers.go

key-decisions:
  - "PlanGoal (plan_verifier.tmpl's render field) sources from Project.Spec.OutcomePrompt via the existing outcomePromptOf() helper — Plan has no authored goal/prompt text field of its own in the schema; this is the same value the planner's own dispatch already renders (Claude's Discretion, no locked alternative exists)"
  - "Children (plan_verifier.tmpl's bounded child-Task summary) reads Task.Spec.FilesTouched (not DeclaredOutputPaths) for the 'Files' field — matches the rubric's 'file-touch declarations' dimension-2 language exactly"
  - "attempt = int(plan.Status.LoopStatus.Iteration) + 1 for the plan-check verifier Job name and dispatch identity — the D-06 quality counter doubles as the verifier attempt number (RESEARCH Open Question 3's resolved answer); this plan never increments Iteration (52-09 owns that)"
  - "handlePlanVerifierCompletion collapses REPAIRABLE, an APPROVED verdict a deterministic gate-command failure dominates, and BLOCKED into ONE shared exhaustPlanVerifyLoop call — a single literal '52-09 replaces this with repairOrHaltPlan' marker pins the future re-plan seam (kept to exactly one occurrence per the plan's own acceptance criterion)"
  - "WorktreeCheckoutImage/WorktreeBranch are set unconditionally on every plan-check dispatch (sourced from r.Deps.TidePushImage/project.Status.Git.BranchName) — buildWorktreeCheckoutContainer's own gate (both fields non-empty) is the off-switch when either is unset, so no extra conditional is needed at the call site"

patterns-established:
  - "Level-verify entry-transition symmetry: a contract's activation test (GateCommand != \"\" && Phase == Locked) is evaluated identically at both the ChildCount==0 (handlePlannerJobCompletion) and ChildCount>0 (reconcileWaveMaterialization) materialization-complete signals, mutually exclusive by construction (the second only fires when Phase is still Running, which the first already moved off of when it fires)"

requirements-completed: [ESC-01]

# Metrics
duration: 65min
completed: 2026-07-20
---

# Phase 52 Plan 07: Plan-Check Dispatch/Consume (D-03/D-10) Summary

**The plan-check half of the Task loop's generalization: a Locked-contract Plan enters `Verifying` after its child Tasks materialize, dispatches an independent plan-check verifier riding every Phase-51 D-10 safety rail, and structurally holds all child Task dispatch via a one-line `checkParentApproval` extension until the verdict is APPROVED — closing a real reachability bug along the way where the pre-existing "materialization complete" signal could never fire for any Plan with actual children.**

## Performance

- **Duration:** ~65 min
- **Tasks:** 3 (2 combined into one commit — see Deviations sequencing note)
- **Files modified:** 3 (1 created, 2 modified)

## Accomplishments

- `checkParentApproval`'s `case "Plan":` arm gained a `LevelPhaseVerifying` OR-clause (dispatch_helpers.go) — the SOLE hold mechanism for D-03; no new call site needed since Task's `gateChecks` already calls it.
- `handlePlannerJobCompletion` transitions a Locked-contract Plan to `Verifying` (instead of clearing to `""`) once `ResolveVerificationSpec` resolves an active contract — byte-for-byte preserved behavior for the no-contract case.
- The full plan-check state machine — `checkPlanVerifyingState` / `dispatchPlanVerifier` / `buildPlanVerifierEnvelopeIn` / `handlePlanVerifierCompletion` / `markPlanVerifiedApproved` / `exhaustPlanVerifyLoop` — mirrors Task's verifier loop one level up: ESC-04 cap-before-reserve, the shared `ReservationStore`, fail-closed `ClassifyVerdict`, the `EVALUATOR` sibling span (`tide.dispatch.plan.verify`), and the level-verify worktree-checkout init container (`WorktreeCheckoutImage`/`WorktreeBranch`).
- `Reconcile()` and `reconcilePlannerDispatch` both route a `Verifying` Plan away from normal dispatch/wave processing — Reconcile's own top-level routing drives the verify state machine forward; `reconcilePlannerDispatch`'s defensive early-return guards the crash window a future re-plan (52-09) will open.
- **Root-caused and fixed a genuine reachability bug** (see Deviations): the ChildCount-gated transition inside `handlePlannerJobCompletion` can only ever fire for a genuine leaf Plan (`ChildCount==0`) — for any real Plan with children, `reconcilePlannerDispatch`'s own tasks-exist early-return permanently blocks re-entry into `handlePlannerJobCompletion` the moment the first child Task becomes visible. Added a second, reachable entry point inside `reconcileWaveMaterialization`.
- 6 Ginkgo specs (`Describe("PlanCheck", ...)`) pin the hold, dispatch, ESC-04 rails (cap-defer + reserve/settle), fail-closed exhaustion, and APPROVED-unblocks-dispatch behaviors end-to-end against a real envtest apiserver.

## Task Commits

Each task was committed atomically:

1. **Task 1+2: Verifying entry + hold + plan-check dispatch/consume (D-03/D-10)** - `3b273133` (feat)
2. **Task 3: Ginkgo PlanCheck specs + Rule 2 reachability-gap fix** - `d070b625` (test)

_Note: Task 1 and Task 2 landed in one commit — see Deviations sequencing note (mirrors 52-04/52-06/51-07 precedent in this same phase)._

## Files Created/Modified

- `internal/controller/dispatch_helpers.go` - `checkParentApproval`'s `case "Plan":` arm gains the `LevelPhaseVerifying` OR-clause
- `internal/controller/plan_controller.go` - Verifying entry (2 sites: `handlePlannerJobCompletion` for `ChildCount==0`, `reconcileWaveMaterialization` for `ChildCount>0`), `reconcilePlannerDispatch`'s defensive Verifying early-return, `Reconcile()`'s Verifying routing to `checkPlanVerifyingState`, the full plan-check dispatch/consume state machine (`checkPlanVerifyingState`/`dispatchPlanVerifier`/`buildPlanVerifierEnvelopeIn`/`synthesizeNoPlanEnvelopeOut`/`emitPlanEvaluatorSpan`/`settlePlanVerifierSpend`/`applyPlanLoopStatus`/`markPlanVerifiedApproved`/`exhaustPlanVerifyLoop`/`handlePlanVerifierCompletion`)
- `internal/controller/plan_verify_dispatch_test.go` - new: `Describe("PlanCheck", ...)` with 6 specs (hold+dispatch, off-switch, cap-defer, reserve/settle, fail-closed, APPROVED-unblocks)

## Decisions Made

See `key-decisions` in frontmatter. Summary: `PlanGoal` sources from `Project.Spec.OutcomePrompt` (no Plan-level goal field exists); `Children`'s `Files` field is `FilesTouched` (matches the rubric's own "file-touch declarations" language); the plan-check attempt number is `LoopStatus.Iteration + 1` (Open Question 3's resolved answer, no separate counter minted); REPAIRABLE and a deterministic-failure-dominated APPROVED verdict share ONE conservative exhaustion path with a single, exactly-once "52-09 replaces this" marker (kept to one occurrence to satisfy the plan's own acceptance-criteria grep).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical Functionality] The ChildCount-gated Verifying transition is structurally unreachable for any Plan with real children — added a second, reachable entry point**
- **Found during:** Task 3, writing spec (a) (a contracted Plan enters Verifying after materialization)
- **Issue:** `handlePlannerJobCompletion` is reached ONLY from `reconcilePlannerDispatch`'s "Running" branch, which is itself reached ONLY when the top-of-function tasks-exist check finds ZERO Tasks. Once even ONE child Task becomes visible (the reporter's async materialization), every subsequent reconcile is permanently routed away from `handlePlannerJobCompletion` by that same tasks-exist early-return (`dispatched=false` → `reconcileWaveMaterialization` instead). So the Task-1-designed transition point — gated on `handlePlannerJobCompletion`'s own `observed >= expected` ChildCount check — can only ever be satisfied for `ChildCount==0` (a genuine leaf Plan, where the check is skipped entirely), since materialization is asynchronous and can never complete within the SAME reconcile that first found zero Tasks. For any real Plan with children, the Verifying transition would simply never fire, silently defeating D-03's entire purpose (children would dispatch through the ordinary wave path, completely unheld). Live-reproduced against the envtest apiserver: `plan.Status.Phase` stayed `"Running"` indefinitely across repeated reconciles once a child Task existed.
- **Fix:** Added a second check at the top of `reconcileWaveMaterialization` (immediately after its own Task `List` call, before wave/file-touch processing) — `if plan.Status.Phase == Running && len(taskList.Items) > 0`, re-resolves the contract via `ResolveVerificationSpec`, and performs the identical transition. `reconcileWaveMaterialization` is genuinely re-entered on every reconcile once `ValidationState=="Validated"` (stamped unconditionally by `handlePlannerJobCompletion`'s FIRST — and only — call, before its own ChildCount requeue), so this is the actually-reachable "materialization has happened" seam for the common case. The two sites are mutually exclusive by construction: the new site's `Phase == Running` guard is false once the original site has already fired (for `ChildCount==0`), and vice versa.
- **Files modified:** `internal/controller/plan_controller.go`
- **Verification:** New spec (a) proves the transition + Job dispatch + child-Task hold live against envtest; the full unfiltered `internal/controller` suite (266 specs, including every pre-existing `ChildCount>0` no-contract Plan/wave test) stayed green before and after, confirming byte-for-byte preservation of the no-contract path.
- **Committed in:** `d070b625` (Task 3 commit)

**2. [Rule 1 - Bug] A field-indexer cache-sync race flaked the new specs (~1 in 6 runs)**
- **Found during:** Task 3, stress-testing the new specs for the project's NO-FLAKE-TOLERANCE rule
- **Issue:** `makeVerifyChildTask`'s `waitForCacheSync` only confirms the new Task is visible via a plain Get-by-name; the reconcile path immediately after queries via the SEPARATE `.spec.planRef` field indexer (`taskPlanRefIndexKey`), which can lag the primary object-store sync by a beat under load. The second reconcile in the test helper occasionally ran before the indexer caught up, undercounting Tasks and skipping the Verifying transition for that pass.
- **Fix:** Added an explicit `Eventually`/`EventuallyWithOffset` wait on the SAME indexed `List` call the production code path uses, before firing the next reconcile, in both `driveToPlanVerifying` and spec (c)'s inline flow.
- **Files modified:** `internal/controller/plan_verify_dispatch_test.go`
- **Verification:** 12 consecutive full runs of the `PlanCheck` focus, all green (previously flaked roughly 1-in-6 across two stress runs of 8 and 4 iterations).
- **Committed in:** `d070b625` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (1 Rule 2 correctness gap, 1 Rule 1 test flake)
**Impact on plan:** No scope creep — both fixes were required for the plan's own D-03 invariant to hold for real (non-leaf) Plans and for the new specs to meet this project's explicit no-flake-tolerance bar. All acceptance-criteria greps from the plan's Task 1/2 blocks pass exactly as specified (`LevelPhaseVerifying` ≥ 3 sites in plan_controller.go / 1 site in dispatch_helpers.go; `verifierInFlightCount` before Reserve; one `ClassifyVerdict` three-tier switch; `52-09 replaces` = 1; `WorktreeCheckoutImage` = 1).

**Sequencing note (not a Rule 1-4 deviation, no user decision needed):** Task 1 (Verifying entry + hold) and Task 2 (dispatch/consume state machine) landed in one commit (`3b273133`) rather than two, because Task 1's own acceptance criteria require `Reconcile()` to route a Verifying Plan to `checkPlanVerifyingState` — a Task 2 function. Splitting them would have required either a non-compiling intermediate commit or deferring the routing call past Task 1's own stated scope. Mirrors 52-04/52-06/51-07's identical precedent in this same phase (documented there as a genuine two-way call dependency, not an ordering violation). Both tasks' distinct scopes are still cleanly separable in the diff/commit body.

## Issues Encountered

None beyond the two deviations above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The plan-check loop's dispatch/consume/hold/rails are fully live and envtest-proven. 52-09 can now replace the single marked seam (`grep -n "52-09 replaces" internal/controller/plan_controller.go`) with `repairOrHaltPlan`'s findings-seeded re-plan (stall detection, delete-then-recreate child-Task reconciliation, the attempt-aware planner Job counter) without touching anything else in this plan's state machine — `handlePlanVerifierCompletion`'s REPAIRABLE and deterministic-failure-dominated-APPROVED cases are the sole two call sites that change.
- `ResolveLoopPolicy`/`ResolveVerificationSpec`, the D-10 rails, and the worktree-checkout init container (all from prior 52-* plans) are proven consumable by a non-Task dispatch site — 52-08/52-10's Phase/Milestone/Project level-verify dispatch sites can follow the exact same shape.
- `go build ./...`, `go vet`, `make lint` (0 issues), and the full unfiltered `internal/controller` envtest suite (266/266) are all clean at this plan's HEAD.
- No blockers.

---
*Phase: 52-per-level-looppolicy-parameterization*
*Completed: 2026-07-20*

## Self-Check: PASSED

All 3 modified/created source files and both task commit hashes (`3b273133`, `d070b625`) verified present on disk / in `git log --oneline --all`.
