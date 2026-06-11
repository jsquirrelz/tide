---
phase: 12-gate-semantics-reject-resume
verified: 2026-06-11T19:40:00Z
status: gaps_found
score: 3/5 must-haves verified
overrides_applied: 0
gaps:
  - truth: "Approving a gated level with incomplete children records gate passage but never advances it to Succeeded (GATE-01) — at ALL gated levels, including Plan (Project.Spec.Gates.Plan is a first-class config field)"
    status: partial
    reason: "Delivered for Milestone and Phase (verified: milestone_gates_test ChildCount=5 → Running+ApprovedByUser; phase has full parity). NOT delivered for Plan. CR-01 independently confirmed: the plan-level approve gate hook lives only in handlePlannerJobCompletion (plan_controller.go:506-519), reachable only while the Plan has zero Tasks. reconcilePlannerDispatch (line 211-219) early-exits to dispatched=false the instant any Task with planRef exists, routing the next reconcile to reconcileWaveMaterialization and bypassing handlePlannerJobCompletion entirely. The ChildCount gate (line 491-498) requeues while observed<expected; the moment Tasks materialize (observed>=expected possible) line 217 trips first. So for any Plan with ChildCount>0 (every real planner output), patchPlanAwaitingApproval never fires, Status.Phase stays Running, and executor Tasks dispatch with zero approval. The Task-side descent hold (checkParentApproval kind=Plan, dispatch_helpers.go:288-293) only triggers on plan.Status.Phase==AwaitingApproval, which is never set — dead code for this scenario."
    artifacts:
      - path: "internal/controller/plan_controller.go"
        issue: "Plan approve gate hook at :506-519 is structurally unreachable for ChildCount>0; the early task-exists exit at :217-219 routes around handlePlannerJobCompletion once Tasks materialize"
      - path: "internal/controller/plan_gates_test.go"
        issue: "Test 6a (line 118-155) passes only via the leaf path: driveToJobCompletion (:110-113) sets EnvelopeOut with no ChildCount (defaults 0) and the PlanReconciler sets no ReporterImage, so no Tasks materialize. No regression test exercises ChildCount>0 + reporter-materialized Tasks at the Plan gate."
    missing:
      - "Move the plan-level gate-policy check (EvaluatePolicy/CheckApprove → patchPlanAwaitingApproval) to BEFORE the ChildCount requeue in handlePlannerJobCompletion (mirror milestone/phase hook position), and make reconcileWaveMaterialization (or the tasks-exist early-exit at plan_controller.go:217) honor plan.Status.Phase==AwaitingApproval so the wave path cannot dispatch executors while parked."
      - "Add a plan-gate regression spec with ChildCount>0 plus reporter-materialized Tasks asserting zero executor Jobs while parked."
  - truth: "A level parked at AwaitingApproval blocks child dispatch — children materialize but reconcilers hold all Job dispatch until the parent is approved (GATE-04), at all gated levels"
    status: partial
    reason: "Verified for Milestone→Phase (envtest gates_test.go:78-165: five Phase children produce zero planner Jobs while Milestone parked). FAILS at the Plan→Task boundary as a consequence of CR-01: a Plan with gates.plan=approve never reaches AwaitingApproval when it authors Tasks, so checkParentApproval(kind=Plan) at task_controller.go:326 never holds and executor Tasks dispatch unreviewed. Additionally CR-02: PlanReconciler.reconcilePlannerDispatch has NO Status.Phase==AwaitingApproval early-return branch (confirmed: only AwaitingApproval references in the file are the descent-hold comment :244 and the patch helper :510/:625). Milestone (:216) and Phase (:201) both have it. A leaf Plan parked at AwaitingApproval falls through the Running check, re-enters dispatch, and is stomped back to Running at :366-377 with no annotation consumed — the finding-2 oscillation reproduced at Plan level."
    artifacts:
      - path: "internal/controller/plan_controller.go"
        issue: "No AwaitingApproval early-return in reconcilePlannerDispatch (CR-02); a parked leaf Plan un-parks itself with no operator action"
      - path: "internal/controller/dispatch_helpers.go"
        issue: "checkParentApproval(kind=Plan) at :288-293 is dead for non-leaf Plans because the Plan never enters AwaitingApproval (CR-01 downstream)"
    missing:
      - "Add the AwaitingApproval early-return branch at the top of reconcilePlannerDispatch (consume annotation → Running+ApprovedByUser → Requeue), mirroring milestone_controller.go:216-243."
deferred:
  - truth: "tide approve <project> / tide resume --retry-failed discover gated/Failed Milestone/Phase/Plan CRs in a production (reporter-materialized) run (CLI half of GATE-03 / RESUME-01)"
    addressed_in: "Phase 15"
    evidence: "REQUIREMENTS.md CUTS-01 (Phase 15): 'Reporter-created Milestone/Phase CRs carry the tideproject.k8s/project label so tide approve discovers gated levels (finding 6: zero labels → no level awaiting approval despite a parked CR)'. CR-03's CLI label-discovery gap is the exact CUTS-01 scope; deferral confirmed by orchestrator note (planner discretion)."
human_verification: []
---

# Phase 12: Gate Semantics + Reject/Resume Verification Report

**Phase Goal:** Gate passage and reject/resume recovery are correct — the approve gate sits at descent (review the authored artifact before children spend), approval never jumps a level past its children, reject parks instead of fail-marking, and `tide resume` is the one sanctioned recovery verb.
**Verified:** 2026-06-11T19:40:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | GATE-01: approving a gated level with incomplete children → Running+ApprovedByUser, never Succeeded (at all gated levels) | ✗ FAILED | Milestone/Phase VERIFIED (milestone_gates_test:231-354 ChildCount=5 → Running not Succeeded; phase parity). **Plan FAILED** — CR-01: gate hook (plan_controller.go:506-519) unreachable for ChildCount>0; tasks-exist early-exit (:217-219) routes around handlePlannerJobCompletion |
| 2 | GATE-04: parked level blocks child dispatch — zero child Jobs until approval (at all gated levels) | ✗ FAILED | Milestone→Phase VERIFIED (envtest gates_test.go:78-165, zero planner Jobs while parked). **Plan→Task FAILED** — CR-01 (Plan never enters AwaitingApproval) + CR-02 (no AwaitingApproval early-return in PlanReconciler; parked leaf Plan oscillates back to Running) |
| 3 | GATE-02: gates.md documents approve-at-descent; old "advances the level to Succeeded" and `Approved` phase-value text gone | ✓ VERIFIED | grep for the bug text returns nothing; docs/gates.md:30,40,88 document park-at-descent + children-gated succession |
| 4 | RESUME-01: reject parks (not Failed); resume lifts; resume --retry-failed resets Failed → re-dispatch → ResumedByUser | ✓ VERIFIED (reconciler half) | All 4 reject sites call patch*Rejected (park, RejectedByUser condition, no Phase=Failed) — milestone:301/447, phase:290/440, plan:262/503, task:314. resume --retry-failed resets Status.Phase via Status().Patch + ResumedByUser (resume.go:94-150). Production CLI discovery of upper-level CRs deferred to Phase 15 (see Deferred) |
| 5 | GATE-03: approve against a Failed level prints actionable error pointing at resume --retry-failed | ✓ VERIFIED (logic) | approve.go:152-164 findFailedLevel guard fires before the AwaitingApproval search and errors with "use 'tide resume %s --retry-failed' to recover". Same Phase-15 production-discovery caveat as SC-4 |

**Score:** 3/5 truths verified

### Deferred Items

| # | Item | Addressed In | Evidence |
| --- | --- | --- | --- |
| 1 | CLI discovery of reporter-materialized Milestone/Phase/Plan CRs by `tide approve` / `tide resume --retry-failed` (CR-03) | Phase 15 | REQUIREMENTS.md CUTS-01: reporter stamps `tideproject.k8s/project` label so approve discovers gated levels |

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `api/v1alpha1/shared_types.go` | ReasonApprovedByUser constant | ✓ VERIFIED | Used in milestone/phase approve branches |
| `internal/controller/milestone_controller.go` | AwaitingApproval branch → Running (never direct Succeeded) | ✓ VERIFIED | :216-243; gate hook :511-525 parks via patchMilestoneAwaitingApproval; Succeeded only via ChildCount succession |
| `internal/controller/phase_controller.go` | AwaitingApproval early-return + already-approved check | ✓ VERIFIED | :201-228 early-return; reject hold + park helper present |
| `internal/controller/plan_controller.go` | Plan approve gate + AwaitingApproval early-return + reject park | ⚠️ PARTIAL | patchPlanRejected/patchPlanAwaitingApproval exist and reject parks correctly, BUT approve gate unreachable for ChildCount>0 (CR-01) and AwaitingApproval early-return absent (CR-02) |
| `internal/controller/task_controller.go` | patchTaskRejected + descent hold | ✓ VERIFIED (own scope) | Reject parks (:314); descent hold present but dead vs Plan parent (CR-01 downstream) |
| `internal/controller/dispatch_helpers.go` | checkParentApproval shared helper | ✓ VERIFIED | :271-296 covers Milestone/Phase/Plan; functions correctly where parent actually enters AwaitingApproval |
| `docs/gates.md` | Approve-at-descent docs (GATE-02) | ✓ VERIFIED | Bug text removed; descent semantics documented |
| `cmd/tide/resume.go` | --retry-failed status reset + ResumedByUser | ✓ VERIFIED (logic) | retryFailedLevels resets Phase via Status().Patch over 4 kinds; production upper-level discovery deferred (CR-03) |
| `cmd/tide/approve.go` | findFailedLevel D-07 guard | ✓ VERIFIED (logic) | :155-164; same CR-03 discovery caveat |
| `internal/controller/milestone_gates_test.go` | GATE-01 finding-7 regression (ChildCount=5) | ✓ VERIFIED | :231-354 asserts Running not Succeeded; Succeeded only after 5 children complete |
| `test/integration/envtest/gates_test.go` | GATE-04 descent-hold envtest | ✓ VERIFIED | :78-165 five Phase children, zero planner Jobs while Milestone parked |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| milestone AwaitingApproval branch | ChildCount-gated succession | Running transition + Requeue | ✓ WIRED | :240 Requeue → :248 Running branch → handleJobCompletion |
| reject sites (4) | patch*Rejected park helpers | call-site replacement | ✓ WIRED | All 4 confirmed (milestone/phase/plan/task); patch*Failed no longer reached from reject |
| status reset (phase='') | planner re-dispatch | terminal short-circuit gates only Succeeded\|\|Failed | ✓ WIRED | plan_controller.go:222 empty phase re-enters dispatch |
| plan gate-policy hook | patchPlanAwaitingApproval | EvaluatePolicy → CheckApprove | ✗ NOT_WIRED | Hook present (:506-519) but unreachable for ChildCount>0 — early task-exists exit (:217) routes around it (CR-01) |
| Plan AwaitingApproval | consume + Running+ApprovedByUser | early-return branch | ✗ NOT_WIRED | No AwaitingApproval branch in reconcilePlannerDispatch (CR-02) |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| GATE-01 | 12-01 | Approval records passage, Succeeded only after children | ✗ BLOCKED (Plan level) | Milestone/Phase delivered; Plan gate structurally bypassed (CR-01) |
| GATE-02 | 12-01 | gates.md approve-at-descent rewrite | ✓ SATISFIED | docs/gates.md rewritten, bug text gone |
| GATE-03 | 12-02, 12-04 | approve refuses Failed → resume --retry-failed pointer | ✓ SATISFIED (logic); ⚠️ production discovery deferred | approve.go:155-164; upper-level CR discovery → Phase 15 CUTS-01 |
| GATE-04 | 12-03 | Parked level blocks child dispatch | ✗ BLOCKED (Plan→Task) | Milestone→Phase delivered; Plan→Task dead via CR-01/CR-02 |
| RESUME-01 | 12-02, 12-04 | reject parks; resume --retry-failed recovers Failed | ✓ SATISFIED (reconciler); ⚠️ CLI discovery deferred | Reject parks at all 4 levels; --retry-failed resets + re-dispatch; upper-level CR discovery → Phase 15 |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| internal/controller/plan_controller.go | 506-519 | Gate hook present but unreachable for ChildCount>0 (dead path in production) | 🛑 Blocker | Operator setting gates.plan=approve gets unreviewed executor spend — run-1 finding-1 class, one level down |
| internal/controller/plan_controller.go | 217-219 / 244-255 | checkParentApproval(kind=Plan) hold dead because Plan never enters AwaitingApproval | 🛑 Blocker | Task descent hold never engages for Plan parent |
| internal/controller/dispatch_helpers.go | 278-293 | checkParentApproval fails open on parent NotFound (WR-01) | ⚠️ Warning | Lagging Plan informer fail-opens the Task hold; zero-spend-during-review guarantee weakened |
| internal/controller/phase_controller.go / plan_controller.go | 424 / 503 | Reject short-circuit fires after reporter-Job spawn (WR-10) | ⚠️ Warning | A rejected Project still creates new reporter Jobs (parity drift vs milestone ordering) |
| cmd/tide/approve.go | 152-164 | D-07 guard blocks all approvals project-wide on any Failed level (WR-06) | ⚠️ Warning | Contradicts strict failure profile; --wave path skips the guard entirely |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Package builds | go build ./... | exit 0 (orchestrator-established, /tmp/wave*-post-merge-test.log) | ✓ PASS |
| make test | make test | MAKE_EXIT=0, zero FAIL lines (orchestrator-established) | ✓ PASS |
| No plan-gate ChildCount>0 test exists | grep ReporterImage/ChildCount plan_gates_test.go | none — confirms CR-01 leaf-only coverage | ✗ FAIL (coverage gap) |

### Human Verification Required

None — the gaps are statically observable in the reconcile flow; no runtime/visual verification needed.

### Gaps Summary

Phase 12 delivered the gate-semantics rewrite cleanly for the **Milestone and Phase** levels and the **reject-parks-not-fail** contract at all four levels — the run-killer (finding-7) and reject recovery (finding-9a) are genuinely closed for those surfaces, with passing regression tests. GATE-02 (docs) is fully done.

However, the phase goal is stated universally ("approval never jumps **a level** past its children"; GATE-01/GATE-04 say "a level"/"levels") and `Project.Spec.Gates.Plan` is a first-class operator-configurable field. Two structural defects in the **Plan** reconciler mean GATE-01 and GATE-04 are NOT delivered there:

- **CR-01 (BLOCKER):** The plan-level approve gate hook lives only in `handlePlannerJobCompletion`, which is unreachable once a Plan authors Tasks (every real planner output). `reconcilePlannerDispatch` early-exits to the wave path the moment a Task with `planRef` exists, so `patchPlanAwaitingApproval` never fires and executor Tasks dispatch with zero approval — the exact finding-1 failure class, reproduced one level down. Independently confirmed by tracing the reconcile flow and the leaf-only Test 6a fixture (ChildCount=0, no ReporterImage).
- **CR-02 (BLOCKER):** `reconcilePlannerDispatch` lacks the `AwaitingApproval` early-return that Milestone and Phase both have, so a parked leaf Plan is stomped back to Running with no annotation consumed (finding-2 oscillation at Plan level).

These two together block the goal for the Plan level. The fix is mechanical and well-scoped (move the gate hook before the ChildCount requeue; have the wave path honor `AwaitingApproval`; add the early-return branch; add a ChildCount>0 regression spec) — see `missing` in frontmatter.

**Deferred (not a Phase-12 gap):** CR-03's CLI label-discovery gap — `tide approve`/`tide resume --retry-failed` cannot find reporter-materialized upper-level CRs in production because nothing stamps `tideproject.k8s/project` on Milestones/Phases/Plans — is explicitly scoped to Phase 15 CUTS-01. The reconciler-half logic of RESUME-01/GATE-03 is correct and tested; only the production end-to-end CLI discovery awaits the Phase-15 label stamp.

---

_Verified: 2026-06-11T19:40:00Z_
_Verifier: Claude (gsd-verifier)_
