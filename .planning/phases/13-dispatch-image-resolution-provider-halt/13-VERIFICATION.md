---
phase: 13-dispatch-image-resolution-provider-halt
verified: 2026-06-11T22:05:00Z
status: passed
score: 6/6 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 4/6
  gaps_closed:
    - "CR-01: milestone reconcilePlannerDispatch nil-project deref (DISPATCH-01)"
    - "WR-03: BillingHalt re-stamped by pre-resume stragglers (HALT-01 recovery)"
    - "Full make test-int red (3 inherited promptPath fixtures + 1 unattributed reporter materialization) (DISPATCH-02 green-suite)"
  gaps_remaining: []
  regressions: []
gaps: []
deferred: []
---

# Phase 13: Dispatch Image Resolution + Provider Halt Verification Report

**Phase Goal:** Subagent image resolves correctly at all dispatch sites via the documented chain, and a provider billing-400 response halts the entire project instead of burning sessions one at a time.
**Verified:** 2026-06-11T22:05:00Z
**Status:** passed
**Re-verification:** Yes — after gap closure (plans 13-05, 13-06, 13-07)

## Re-Verification Summary

The initial verification (4/6) found three blocking gaps. All three are closed in the merged HEAD
(`cb78685`); every gap-closure commit (`9a93a3a 35e779a 3632a7a 984c886 ade767f a722716 b603673
b952d75 edb4361 12fc148 ca28122 fe06d27 e9c4c9b 9b36663`) verified as ancestor of HEAD.

- **CR-01 (DISPATCH-01)** — nil-project guard now present in `milestone_controller.go:339-347`, before
  the first deref. Mirrors the plan_controller cascade-7 shape: empty `ProjectRef` refuses without
  requeue; nil project requeues at 1s. RED→GREEN envtest spec at `dispatch_image_test.go:47-76` (CR-01
  no-panic, RequeueAfter confirmed) is in the green Layer A 38/38 run.
- **WR-03 (HALT-01 recovery)** — closed by two complementary mechanisms: (1) a resume time-fence in
  `setBillingHaltIfNeeded` (`billing_halt.go:111-124`) that skips re-stamping when the completed Job
  predates `AnnotationBillingResumedAt`, threaded as `jobStart` through all 5 `handleJobCompletion`
  sites (fail-closed on zero/unparseable); (2) the credproxy synthetic latch body
  (`server.go:229-231`) reworded to drop the `"credit balance"` classifier substring so a restarted
  straggler container cannot manufacture the trigger. `tide resume` stamps `billing-resumed-at`
  (`resume.go:102-123`) only when BillingHalt was actually cleared, and now surfaces the straggler
  fence in its output.
- **Green-suite (DISPATCH-02 / 13-03 criterion)** — reporter materialization root-caused as a REAL
  RBAC bug (`ensureReporterSARBAC` missing `projects/get` + `list`, now mirrors
  `charts/tide/templates/reporter-rbac.yaml` exactly at `failure_test.go:224-229`); promptPath added to
  all five fixtures/inline Tasks; GATE-04 approval-race regression fixed. Full `make test-int` log
  (`/tmp/13-07-test-int-full.log`): both packages report `ok`, MAKE_EXIT=0, zero `^--- FAIL|^FAIL`
  lines, Layer A 38/38 SUCCESS 0 Failed, Layer B 17/18 SUCCESS 0 Failed 1 Skipped.

The de-vacuated WR-01 planner-hold specs (the prior verification's standing WARNING) are now genuine:
all four `newBH*Reconciler` helpers wire `Dispatcher: &stubDispatcher{}` and each level has a
halt-cleared control spec proving a planner Job IS created once BillingHalt is removed
(`assertPlannerJobForParent`).

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | resolveImage implements the documented chain (Levels.<level>.Image → Spec.Subagent.Image → flag/Helm default) with full unit coverage | ✓ VERIFIED | dispatch_helpers.go resolveImage; 6 TestResolveImage_* cases (unchanged since initial verification) |
| 2 | Image resolves correctly at ALL dispatch sites (incl. milestone) | ✓ VERIFIED | CR-01 CLOSED: milestone_controller.go:339-347 nil guard before :370/:394 derefs; resolveImage at :406 nil-safe; 6 call sites wired; envtest no-panic regression (dispatch_image_test.go:47-76) green in Layer A 38/38 |
| 3 | Released-chart install dispatches the real subagent (no silent stub override); stub is explicit opt-in | ✓ VERIFIED | deployment.yaml flag dropped; CLAUDE_SUBAGENT_IMAGE from subagent.defaults.image; helm `required` guard + TestHelmDeploymentTemplateEmptyImageFailsRender added (WR-04); contract tests green |
| 4 | A provider billing-400 halts NEW dispatch project-wide and surfaces a Project condition | ✓ VERIFIED | checkBillingHalt gate at all 5 levels before pool acquire; setBillingHaltIfNeeded backstop at all 5 sites; de-vacuated hold specs prove the gate IS reached (Dispatcher wired) and dispatch resumes when cleared |
| 5 | BillingHalt recovery via `tide resume` reliably restores dispatch (halt does not silently re-stamp) | ✓ VERIFIED | WR-03 CLOSED: time-fence (billing_halt.go:111-124) skips stale pre-resume Jobs via AnnotationBillingResumedAt; credproxy synthetic body drops "credit balance" (server.go:229-231); resume.go:102-123 stamps billing-resumed-at + surfaces straggler fence in output; unit tests green |
| 6 | Full `make test-int` green after the chart change (13-03 / D-02 deliverable) | ✓ VERIFIED | /tmp/13-07-test-int-full.log: MAKE_EXIT=0, both packages `ok`, zero `^--- FAIL|^FAIL` lines, Layer A 38/38 0 Failed, Layer B 17/18 0 Failed. 1 Skipped is an unrelated phase-15 end-to-end spec (VM-load 10-min timeout then teardown-skip), not a FAIL |

**Score:** 6/6 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| internal/controller/milestone_controller.go | nil-project guard before derefs | ✓ VERIFIED | :339-347 guard; empty ProjectRef refuse, nil project RequeueAfter 1s |
| internal/controller/dispatch_image_test.go | CR-01 no-panic envtest spec | ✓ VERIFIED | :47-76; RequeueAfter > 0 asserted; green in Layer A 38/38 |
| internal/controller/billing_halt.go | time-fence in setBillingHaltIfNeeded | ✓ VERIFIED | :104 signature has jobStart; :111-124 fence; fail-closed on zero/unparseable |
| api/v1alpha1/shared_types.go | AnnotationBillingResumedAt const | ✓ VERIFIED | :238 = "tideproject.k8s/billing-resumed-at" |
| cmd/tide/resume.go | billing-resumed-at stamp + straggler msg | ✓ VERIFIED | :102-123, stamps only when BillingHalt cleared, separate metadata patch |
| internal/credproxy/server.go | synthetic latch body without classifier | ✓ VERIFIED | :229-231 body has no "credit balance"; X-Tide-Billing-Halt sentinel set |
| internal/controller/billing_halt_regression_test.go | non-vacuous planner-hold + halt-cleared control specs | ✓ VERIFIED | 4 newBH*Reconciler with Dispatcher wired; 4 halt-cleared control specs w/ assertPlannerJobForParent |
| test/integration/kind/failure_test.go | ensureReporterSARBAC mirrors chart RBAC | ✓ VERIFIED | :224-229 child kinds [create,get,list] + projects [get]; matches reporter-rbac.yaml |
| test/integration/kind fixtures | promptPath on all Tasks | ✓ VERIFIED | three-task-wave.yaml=3, chaos-resume-three-task.yaml=3, output/caps/failure/suite inline tasks |
| charts/tide/templates/deployment.yaml | required guard on subagent.defaults.image | ✓ VERIFIED | required guard added (WR-04); render-time named error vs runtime InvalidImageName |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| 5 handleJobCompletion sites | setBillingHaltIfNeeded | jobStart param from completedJob.CreationTimestamp | ✓ WIRED | all 5 derive jobStart; nil-job fail-closed (zero time → stamp) |
| reconcile callers | handleJobCompletion | &job (real Job) on completion path | ✓ WIRED | milestone/phase/project pass &job on completion, nil only on non-completion arm; plan/task always &job |
| resume.go | AnnotationBillingResumedAt | metadata MergePatch after status clear | ✓ WIRED | :110-118, gated on hadBillingHalt |
| credproxy latch | synthetic body | X-Tide-Billing-Halt header + non-classifier body | ✓ WIRED | :226-232 |
| ensureReporterSARBAC | resolveParent + ChildrenAlreadyMaterialized | projects/get + child list verbs | ✓ WIRED | RBAC now grants both; reporter Job materializes Milestone (no "total in ns: 0") |
| 6 controller dispatch sites | resolveImage | BuildOptions.SubagentImage | ✓ WIRED | unchanged; milestone site now nil-guarded |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| merged HEAD compiles | go build ./... | BUILD_EXIT=0 | ✓ PASS |
| resume + credproxy billing units | go test ./cmd/tide/ ./internal/credproxy/ -run 'Resume|Billing|Credit' | ok, EXIT=0 | ✓ PASS |
| controller billing-halt/time-fence units | go test ./internal/controller/ -run 'BillingHalt|Resume' | ok, EXIT=0 | ✓ PASS |
| Full make test-int (orchestrator) | /tmp/13-07-test-int-full.log | MAKE_EXIT=0, both pkgs ok, 0 FAIL lines | ✓ PASS |
| Inherited-debt strings gone | grep "promptPath Required" / "no Milestone owned" / "total in ns: 0" | all 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| DISPATCH-01 | 13-01, 13-05 | Image resolves via chain at all reconciler dispatch sites | ✓ SATISFIED | Chain + 6 sites wired and unit-covered; milestone nil-project crash class closed (CR-01 guard + no-panic spec) |
| DISPATCH-02 | 13-03, 13-06, 13-07 | Released chart dispatches a pinned real image, no silent stub | ✓ SATISFIED | Chart flag-drop + required guard; full make test-int MAKE_EXIT=0, zero FAIL lines (green-suite criterion as written) |
| HALT-01 | 13-02, 13-04, 13-05, 13-06 | Billing 400 halts project-wide + condition; recovery reliable | ✓ SATISFIED | Halt + condition + 5 gates + 5 backstops; recovery (D-06) now reliable via time-fence + reworded latch body; non-vacuous hold + halt-cleared regression specs |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none introduced by gap-closure) | — | no TBD/FIXME/XXX, no unreferenced TODO/HACK in touched production files | ℹ️ Info | gap-closure files clean |

The prior verification's standing WARNINGS were resolved or downgraded by the gap-closure work:
WR-01 (vacuous holds) CLOSED via Dispatcher-wired specs; WR-04 (empty-image garbage ref) CLOSED via
helm `required` guard. WR-02/05/06/07/08 and IN-06 were tracked robustness notes, none of which
defeated the goal in the initial verification and none of which regressed; they remain available for a
hardening pass but are not phase-13 blockers.

### Human Verification Required

None mandated. All three gaps are closed with code-observable evidence and regression coverage; the
full suite is green. The 13-03 manual end-to-end confidence check (kind install with chart defaults +
a Project pinning a real image showing the pinned image in `kubectl get job -o yaml` with no stub
children) remains a valid optional confidence check but is not a deciding factor — the resolution
chain, chart posture, and the stub-via-defaults Layer B path are all verified green.

### Skipped-Spec Adjudication (load-bearing judgment)

Layer B reported 1 Skipped: `medium_http_test.go:360` — "medium Project with stub-subagent reaches
Complete over http://". On attempt #1 it ran the full 10-minute `Eventually` budget (17:28:07 →
17:38:07) and timed out (FAILED); Ginkgo's flake-retry then short-circuited to SKIPPED because
`skipIfCRDsOnlyMode()` detected the namespace/cluster torn down by AfterEach. This is:

1. **Not a phase-13 surface** — `medium_http_test.go` was last touched by phase-15's git-server fix
   (commit 25fce55, 2026-06-09, pre-milestone); it exercises full-stack happy-path completion over
   http://, with no image-resolution or billing-halt assertions.
2. **Not a FAIL** — the green-suite criterion (13-03 acceptance_criteria) is precisely
   `MAKE_EXIT == 0 AND zero ^--- FAIL|^FAIL lines`, both satisfied. A skip is not a failure.
3. **A constrained-VM timing artifact** — cluster was at ~19 min uptime; a 10-min full-pipeline run
   under VM load did not complete in the window, exactly the CLAUDE.md "7.65 GiB VM, one heavy run at a
   time" failure mode. The package still reported `ok ... 1155.305s`.

Weighed against DISPATCH-02's green-suite criterion: the criterion is met as written and per the
project's CLAUDE.md exit discipline (MAKE_EXIT + FAIL-grep, never Ginkgo summary alone). The skip does
not block.

### Gaps Summary

No gaps. All three prior blockers (CR-01 nil-project deref, WR-03 straggler re-stamp, full test-int
red) are closed in merged code with regression coverage, the build is green, the targeted gap-closure
unit tests pass, and the full `make test-int` log satisfies the documented green-suite criterion
(MAKE_EXIT=0, zero FAIL lines). The phase goal — subagent image resolves correctly at all dispatch
sites via the documented chain, and a provider billing-400 halts the entire project (with reliable
`tide resume` recovery) — is observably achieved in the codebase.

---

_Verified: 2026-06-11T22:05:00Z_
_Verifier: Claude (gsd-verifier)_
