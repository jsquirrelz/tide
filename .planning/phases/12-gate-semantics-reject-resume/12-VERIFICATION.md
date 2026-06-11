---
phase: 12-gate-semantics-reject-resume
verified: 2026-06-11T20:15:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 3/5
  gaps_closed:
    - "GATE-01 at the Plan level: approving a gated Plan with ChildCount>0 records passage but never advances to Succeeded — the gate hook was moved before the ChildCount requeue so it fires for non-leaf Plans (CR-01)"
    - "GATE-04 at the Plan→Task boundary: a parked Plan blocks executor dispatch — the AwaitingApproval early-return suppresses the wave path while parked, and the parked Plan no longer self-unparks (CR-01 wave-path half + CR-02 oscillation)"
  gaps_remaining: []
  regressions: []
deferred:
  - truth: "tide approve <project> / tide resume --retry-failed discover gated/Failed Milestone/Phase/Plan CRs in a production (reporter-materialized) run (CLI half of GATE-03 / RESUME-01)"
    addressed_in: "Phase 15"
    evidence: "REQUIREMENTS.md CUTS-01 (Phase 15): reporter-created Milestone/Phase CRs carry the tideproject.k8s/project label so tide approve discovers gated levels. CR-03 CLI label-discovery gap == CUTS-01 scope; deferral confirmed in prior verification."
human_verification: []
---

# Phase 12: Gate Semantics + Reject/Resume Verification Report

**Phase Goal:** Gate passage and reject/resume recovery are correct — the approve gate sits at descent (review the authored artifact before children spend), approval never jumps a level past its children, reject parks instead of fail-marking, and `tide resume` is the one sanctioned recovery verb.
**Verified:** 2026-06-11T20:15:00Z
**Status:** passed
**Re-verification:** Yes — after gap closure (gap plan 12-05, RED fb31aad / GREEN c021c3b)

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | GATE-01: approving a gated level with incomplete children → Running+ApprovedByUser, never Succeeded (at ALL gated levels including Plan) | ✓ VERIFIED | Milestone/Phase already clean. **Plan now CLOSED** — gate hook relocated to plan_controller.go:529-582, fires BEFORE `expected := out.ChildCount` (:591). Test 6d (plan_gates_test.go:339-480) drives ChildCount=2 planner completion → asserts `Status.Phase=="AwaitingApproval"` + ConditionWaveOrLevelPaused True/ReasonAwaitingApproval; approve → Running + ApprovedByUser, never direct Succeeded (children-gated succession at :584-594 intact) |
| 2 | GATE-04: parked level blocks child dispatch — zero child Jobs until approval (at all gated levels) | ✓ VERIFIED | Milestone→Phase already clean. **Plan→Task now CLOSED** — AwaitingApproval early-return (plan_controller.go:215-244) sits BEFORE the tasks-exist List (:248-251) and returns `dispatched=true`, suppressing reconcileWaveMaterialization while parked. Test 6d Step 5 (:391-436) materializes 2 Tasks, drives the TaskReconciler, asserts both Tasks stay Phase="" (held), zero Jobs carry the `tideproject.k8s/task-uid` label of either Task, and zero Wave CRs name the plan; Step 7 (:463-479) proves the executor Job appears once the Plan is approved (hold lifts). checkParentApproval(kind=Plan) is now live, not dead |
| 3 | GATE-02: gates.md documents approve-at-descent; old "advances the level to Succeeded" / `Approved` phase-value text gone | ✓ VERIFIED | Unchanged since prior verification — docs/gates.md documents park-at-descent + children-gated succession; bug text absent |
| 4 | RESUME-01: reject parks (not Failed); resume lifts; resume --retry-failed resets Failed → re-dispatch → ResumedByUser | ✓ VERIFIED (reconciler half) | Unchanged. All 4 reject sites call patch*Rejected (park, RejectedByUser, no Phase=Failed); plan reject short-circuit now hoisted to handlePlannerJobCompletion top (:444-446, milestone parity) — still parks. Production CLI discovery of upper-level CRs deferred to Phase 15 |
| 5 | GATE-03: approve against a Failed level prints actionable error pointing at resume --retry-failed | ✓ VERIFIED (logic) | Unchanged — approve.go findFailedLevel guard errors with "use 'tide resume %s --retry-failed' to recover". Same Phase-15 production-discovery caveat as SC-4 |

**Score:** 5/5 truths verified

### Deferred Items

| # | Item | Addressed In | Evidence |
| --- | --- | --- | --- |
| 1 | CLI discovery of reporter-materialized Milestone/Phase/Plan CRs by `tide approve` / `tide resume --retry-failed` (CR-03) | Phase 15 | REQUIREMENTS.md CUTS-01: reporter stamps `tideproject.k8s/project` label so approve discovers gated levels |

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `internal/controller/plan_controller.go` | Plan approve gate before ChildCount requeue + AwaitingApproval early-return before tasks-exist exit + reject park | ✓ VERIFIED | Edit 1: AwaitingApproval branch :215-244 (before List :248). Edit 2: CheckRejected :444 (before ReadOut :464). Edit 3: EvaluatePolicy :537 with alreadyApproved guard :544-550, parks at :554 (before `expected` :591). ReasonApprovedByUser at 3 sites (:230, :547, :571) |
| `internal/controller/plan_gates_test.go` | Test 6d (ChildCount>0 park + zero executor Jobs + zero Wave CRs + approve lifts) and Test 6e (no oscillation) | ✓ VERIFIED | Test 6d :314-481 exercises the non-leaf path (ChildCount=2, materialized Tasks, gates.task=auto so the PARENT hold is isolated). Test 6e :483-543 loops 3 reconciles asserting AwaitingApproval persists. `driveToJobCompletion` parameterized with `childCount int` (:102), call sites updated |
| `internal/controller/milestone_controller.go` | AwaitingApproval branch → Running (the analog) | ✓ VERIFIED | Unchanged; the parity source |
| `internal/controller/phase_controller.go` | AwaitingApproval early-return + already-approved | ✓ VERIFIED | Unchanged |
| `internal/controller/task_controller.go` | patchTaskRejected + descent hold (now live vs Plan parent) | ✓ VERIFIED | checkParentApproval(kind=Plan) now engages because the Plan actually enters AwaitingApproval (Test 6d Step 5 proves the hold) |
| `internal/controller/dispatch_helpers.go` | checkParentApproval shared helper | ✓ VERIFIED | Covers Milestone/Phase/Plan; functions correctly now that every gated level reaches AwaitingApproval |
| `docs/gates.md` | Approve-at-descent docs (GATE-02) | ✓ VERIFIED | Unchanged |
| `cmd/tide/resume.go` | --retry-failed status reset + ResumedByUser | ✓ VERIFIED (logic) | Unchanged; production upper-level discovery deferred (CR-03) |
| `cmd/tide/approve.go` | findFailedLevel D-07 guard | ✓ VERIFIED (logic) | Unchanged; same CR-03 caveat |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| plan gate-policy hook | patchPlanAwaitingApproval | EvaluatePolicy → CheckApprove, moved before ChildCount requeue | ✓ WIRED | Hook now reachable for ChildCount>0 (:537 < :591); Test 6d primary assertion passes |
| Plan AwaitingApproval branch | consume + Running+ApprovedByUser | early-return before tasks-exist List, dispatched=true | ✓ WIRED | :215-244; suppresses wave path while parked, consumes annotation on approve |
| reconcilePlannerDispatch (parked) | reconcileWaveMaterialization suppressed | dispatched=true return | ✓ WIRED | A parked Plan with materialized Tasks no longer routes to the wave path (Test 6d Step 4 + Step 5: zero Wave CRs, zero executor Jobs) |
| task_controller checkParentApproval(kind=Plan) | plan.Status.Phase==AwaitingApproval | Plan now actually parks | ✓ WIRED | Task descent hold engages (Test 6d Step 5: both Tasks held at Phase="") and lifts on approval (Step 7: executor Job created) |
| reject sites (4) | patch*Rejected park helpers | call-site replacement | ✓ WIRED | All 4 still park; plan reject hoisted to handler top (:444) — parity, still patchPlanRejected |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Package builds | go build ./... | exit 0 (orchestrator-established, post-merge) | ✓ PASS |
| make test | make test | MAKE_EXIT=0, zero FAIL lines (orchestrator-established, /tmp/gap-post-merge-test.log) | ✓ PASS |
| RED specs failed pre-fix | git diff fb31aad~1 fb31aad -- plan_controller.go | empty (controller untouched in RED commit; specs ran against pre-fix code) | ✓ PASS |
| GREEN contains the fix | git show --stat c021c3b -- plan_controller.go | 101 insertions / 21 deletions in plan_controller.go | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| GATE-01 | 12-01, 12-05 | Approval records passage, Succeeded only after children | ✓ SATISFIED | Plan-level gate hook reachable for ChildCount>0; Test 6d proves park-then-Running, never direct Succeeded |
| GATE-02 | 12-01 | gates.md approve-at-descent rewrite | ✓ SATISFIED | docs/gates.md rewritten, bug text gone |
| GATE-03 | 12-02, 12-04 | approve refuses Failed → resume --retry-failed pointer | ✓ SATISFIED (logic); production discovery deferred | approve.go guard; upper-level CR discovery → Phase 15 CUTS-01 |
| GATE-04 | 12-03, 12-05 | Parked level blocks child dispatch | ✓ SATISFIED | Plan→Task closed: zero executor Jobs + zero Wave CRs while parked (Test 6d), hold lifts on approval |
| RESUME-01 | 12-02, 12-04 | reject parks; resume --retry-failed recovers Failed | ✓ SATISFIED (reconciler); CLI discovery deferred | Reject parks at all 4 levels; --retry-failed resets + re-dispatch; upper-level CR discovery → Phase 15 |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| internal/controller/dispatch_helpers.go | 278-293 | checkParentApproval fails open on parent NotFound (WR-01) | ⚠️ Warning | Lagging Plan informer fail-opens the Task hold; informer-lag-bounded; tracked in docs/audit backlog (out of gap scope per 12-05 threat model T-12gc-04) |
| internal/controller/phase_controller.go | 424 | Reject short-circuit fires after reporter-Job spawn (WR-10, phase instance) | ⚠️ Warning | A rejected Project still creates new reporter Jobs at the phase level (plan instance fixed in 12-05; phase left out of gap scope) |
| cmd/tide/approve.go | 152-164 | D-07 guard blocks all approvals project-wide on any Failed level (WR-06) | ⚠️ Warning | Contradicts strict failure profile; --wave path skips the guard; tracked, not a Phase-12 goal blocker |

### Human Verification Required

None — the closed gaps are statically observable in the reconcile flow and exercised by envtest specs (Test 6d/6e); no runtime/visual verification needed. The two warnings are tracked backlog items, not goal blockers.

### Gaps Summary

No gaps. Both previously-partial truths are now delivered at the Plan level by gap plan 12-05:

- **GATE-01 (Plan) — CLOSED:** The gate-policy hook (`gates.EvaluatePolicy(project.Spec.Gates, "plan")`) was relocated to `plan_controller.go:529-582`, ahead of the `expected := out.ChildCount` requeue at :591, with an `alreadyApproved` guard so a consumed annotation does not re-park. For a Plan reporting ChildCount=2, the hook fires and `patchPlanAwaitingApproval` parks the level before any executor Task dispatches. Test 6d's primary assertion (`Status.Phase=="AwaitingApproval"` after ChildCount>0 completion) passes; succession to Succeeded still happens only via children completing.
- **GATE-04 (Plan→Task) + CR-02 — CLOSED:** An `AwaitingApproval` early-return was added at the very top of `reconcilePlannerDispatch` (:215-244), before the tasks-exist List (:248), returning `dispatched=true` so `reconcileWaveMaterialization` cannot run while parked. Test 6d proves zero executor Jobs (no `tideproject.k8s/task-uid`-labeled Job) and zero Wave CRs naming the plan while parked, both Tasks held at Phase="", and the executor Job appearing once approved. Test 6e proves a parked leaf Plan stays parked across 3 reconciles with no annotation consumed (no Running stomp).

TDD discipline verified independently of the SUMMARY: RED commit `fb31aad` touches only `plan_gates_test.go` (empty controller diff vs its parent — the specs ran against the pre-fix controller), and GREEN commit `c021c3b` carries the 101-insertion controller fix. Both on `main`. The orchestrator-established post-merge evidence (`go build ./...` exit 0; `make test` MAKE_EXIT=0, zero FAIL lines) confirms no regression in the milestone/phase gates, reject-park, boundary-push, or envtest suites.

**Deferred (not a Phase-12 gap):** CR-03's CLI label-discovery gap — `tide approve` / `tide resume --retry-failed` cannot find reporter-materialized upper-level CRs in production because nothing stamps `tideproject.k8s/project` on Milestones/Phases/Plans — remains scoped to Phase 15 CUTS-01. The reconciler-half logic of RESUME-01/GATE-03 is correct and tested.

The phase goal is achieved: the approve gate sits at descent at all three controller levels, approval never jumps a level past its children (children-gated succession intact), reject parks instead of fail-marking at all four levels, and `tide resume` is the sanctioned recovery verb.

---

_Verified: 2026-06-11T20:15:00Z_
_Verifier: Claude (gsd-verifier)_
