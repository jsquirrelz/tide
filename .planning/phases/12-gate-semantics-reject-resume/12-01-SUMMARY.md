---
phase: 12-gate-semantics-reject-resume
plan: "01"
subsystem: gate-semantics
tags: [gates, milestone-controller, phase-controller, tdd, regression]
dependency_graph:
  requires: []
  provides: [ReasonApprovedByUser, milestone-approve-routing, phase-approve-routing, GATE-01, GATE-02]
  affects: [milestone_controller, phase_controller, gates_test, gates_docs]
tech_stack:
  added: []
  patterns: [D-04 Running+ApprovedByUser condition, alreadyApproved sentinel, envReaderPresent Pitfall-1 guard]
key_files:
  created: []
  modified:
    - api/v1alpha1/shared_types.go
    - internal/controller/milestone_controller.go
    - internal/controller/milestone_gates_test.go
    - internal/controller/phase_controller.go
    - internal/controller/phase_gates_test.go
    - test/integration/envtest/gates_test.go
    - docs/gates.md
decisions:
  - "D-03 invariant enforced: patchMilestoneSucceeded/patchPhaseSucceeded only reachable from handleJobCompletion ChildCount gate, never from AwaitingApproval branch"
  - "D-04 shape: approval transitions to Running+ApprovedByUser (no new Status.Phase enum); Succeeded fires only via ChildCount-gated succession"
  - "envReaderPresent sentinel (Pitfall-1): distinguishes nil-reader (unit-test) from read-error (transient); guards leaf-succeed on error path"
  - "alreadyApproved check prevents re-parking a level that already has ApprovedByUser or ResumedByUser condition"
metrics:
  duration: "~90 minutes (continued from prior session)"
  completed_date: "2026-06-11"
  tasks_completed: 3
  tasks_total: 3
  files_changed: 7
---

# Phase 12 Plan 01: Gate Semantics Reject/Resume — Approval Routing Fix Summary

**One-liner:** Approval of a gated Milestone/Phase now transitions `Running+ApprovedByUser` via D-04 two-step; Succeeded fires only via ChildCount-gated succession (D-03), never from the AwaitingApproval branch — run-1 finding-7 trust-killer closed.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Milestone controller finding-7 fix + GATE-01 regression spec | e5b67b6 | api/v1alpha1/shared_types.go, milestone_controller.go, milestone_gates_test.go |
| 2 | Phase controller parity — AwaitingApproval early-return + alreadyApproved guard | abf177c | phase_controller.go, phase_gates_test.go, test/integration/envtest/gates_test.go |
| 3 | Rewrite docs/gates.md for approve-at-descent semantics (GATE-02) | 148a68b | docs/gates.md |

## What Was Built

### Task 1: Milestone controller finding-7 fix

**Root cause** (run-1 finding-7): The `AwaitingApproval` branch in `reconcilePlannerDispatch` (milestone_controller.go) called `handleJobCompletion` when the planner Job was TTL'd, which in turn called `patchMilestoneSucceeded` — bypassing the ChildCount-gated succession check entirely. Approving a Milestone with 5 running Phases set it to `Succeeded` in the same reconcile burst.

**Fix**: Rewrote the AwaitingApproval branch. When `gates.CheckApprove` is true:
1. Consume annotation (one-shot, T-04-G2 purity) via `gates.ConsumeApprove` + metadata patch
2. Status patch: `Status.Phase=Running` + `WaveOrLevelPaused{ConditionFalse, ReasonApprovedByUser}`
3. Return `{Requeue: true}` — the Running branch re-enters `handleJobCompletion` which owns ChildCount-gated succession (D-03 invariant restored)

Added `alreadyApproved` check in `handleJobCompletion` gate-policy hook: if `WaveOrLevelPaused` condition has `Status=False` and `Reason` in `{ApprovedByUser, ResumedByUser}`, skip the park — don't re-park an already-approved level.

Added `envReaderPresent` sentinel (Pitfall-1): when reader is non-nil but `ReadOut` returns an error, guard the `!hasChildPhases` leaf-succeed path in the nil-EnvReader fallback. Transient envelope read failure requeues instead of firing `patchMilestoneSucceeded` past incomplete children.

Added `ReasonApprovedByUser = "ApprovedByUser"` constant to `api/v1alpha1/shared_types.go`.

**GATE-01 regression spec**: creates 5 child Phases with OwnerReferences, drives planner Job to completion, approves — asserts `Status.Phase==Running` + `ReasonApprovedByUser` condition. Then status-patches all 5 Phase children to `Succeeded` — reconcile — asserts `Succeeded`. Updated Test 2 for the new two-step flow.

### Task 2: Phase controller parity

Mirrored all Task 1 changes to `phase_controller.go`:

- Added `AwaitingApproval` early-return branch in `reconcilePlannerDispatch` (after `Succeeded||Failed` short-circuit, before `jobName` construction) — fixes finding-2 oscillation root cause where Phase re-entered the planner dispatch body on every reconcile while parked
- Added `alreadyApproved` check in `handleJobCompletion` gate-policy hook
- Added `envReaderPresent` sentinel + guard in fallback leaf-succeed path
- Fixed compile error: `envReaderPresent` guard (`} else if envReaderPresent {`) was missing from fallback path; added with `RequeueAfter: 5s` return (parity with milestone pattern)

Updated tests:
- `phase_gates_test.go` Test 5b: asserts annotation consumed + `Phase != AwaitingApproval` first, then `Succeeded`
- `phase_gates_test.go` oscillation regression spec: 3 consecutive reconciles of unapproved `AwaitingApproval` Phase leave status unchanged, zero new planner Jobs created
- `test/integration/envtest/gates_test.go` TestGateApproveFlow: description updated; intermediate assertion checks `Phase != AwaitingApproval` + annotation consumed before terminal `Succeeded` assertion

All 36 envtest specs pass; `make test-int-fast` MAKE_EXIT=0, zero FAIL lines.

### Task 3: docs/gates.md rewrite (GATE-02)

Rewrote four sections:

1. **"End-to-end approve flow"**: step 2 explains children materialize (D-02) but dispatch is held while parked; step 5 removes the bug-encoding sentence "advances the level to `Succeeded`" and documents the D-04 two-step (consume annotation → `Running+ApprovedByUser` → ChildCount-gated `Succeeded`)
2. **"What's coming"**: removed the `Approved` phase-value sketch (superseded by D-04); replaced with the actual `Running` → `AwaitingApproval` → `Running (+ApprovedByUser)` → `Succeeded` transition
3. **Verb table**: `tide reject` updated to "parks the Project" (no `Failed` write); `tide resume` updated to include `--retry-failed` flag and its semantics
4. **"Reject flow"**: rewritten for D-05 park-not-fail; added "Recovering Failed levels" subsection documenting `tide resume --retry-failed` and raw kubectl recipe for legacy run-1 CRs pre-dating the project label (Phase 15 CUTS-01)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] envReaderPresent declared but not used in phase_controller.go**
- **Found during:** Task 2 GREEN (compile error)
- **Issue:** `envReaderPresent := r.EnvReader != nil` was added but the fallback guard `} else if envReaderPresent {` was missing — parity with milestone_controller.go was incomplete
- **Fix:** Added the `} else if envReaderPresent {` branch with `RequeueAfter: 5s` return in the nil-EnvReader fallback, matching the milestone pattern exactly
- **Files modified:** `internal/controller/phase_controller.go`
- **Commit:** abf177c (included in Task 2 commit)

## Known Stubs

None — no stub patterns or placeholder text introduced.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries. The only schema addition is a constant (`ReasonApprovedByUser`) with no new CRD enum value.

## Self-Check: PASSED

Files verified present:
- `api/v1alpha1/shared_types.go` — contains `ReasonApprovedByUser`
- `internal/controller/milestone_controller.go` — AwaitingApproval branch rewired
- `internal/controller/phase_controller.go` — AwaitingApproval branch added
- `internal/controller/milestone_gates_test.go` — GATE-01 spec present
- `internal/controller/phase_gates_test.go` — oscillation regression spec present
- `test/integration/envtest/gates_test.go` — TestGateApproveFlow updated
- `docs/gates.md` — "advances the level to" removed, `Approved` sketch removed

Commits verified in git log: e5b67b6 (Task 1), abf177c (Task 2), 148a68b (Task 3).

Test run: `make test-int-fast` MAKE_EXIT=0, 36/36 Ginkgo specs passed, zero `--- FAIL` lines.
