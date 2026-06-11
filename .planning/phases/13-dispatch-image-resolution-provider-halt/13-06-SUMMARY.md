---
phase: 13-dispatch-image-resolution-provider-halt
plan: "06"
subsystem: testing
tags: [envtest, ginkgo, billing-halt, helm, kind-fixtures, promptPath]

# Dependency graph
requires:
  - phase: 13-dispatch-image-resolution-provider-halt
    provides: "billing_halt.go + checkBillingHalt + per-level planner dispatch-entry hold logic"
provides:
  - "Non-vacuous BillingHalt hold regression tests for all four planner levels (Milestone/Phase/Plan/Project)"
  - "promptPath on three-task-wave.yaml, chaos-resume-three-task.yaml, output_test.go inline Task"
  - "helm required guard on subagent.defaults.image with contract test"
affects:
  - 13-dispatch-image-resolution-provider-halt
  - 13-07

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Direct dispatch-function call pattern for Project-level tests (avoids PVC/init-Job lifecycle)"
    - "Eventually-based pre-condition pump replaces fixed reconcile count for envtest stability"

key-files:
  created: []
  modified:
    - internal/controller/billing_halt_regression_test.go
    - test/integration/kind/testdata/three-task-wave.yaml
    - test/integration/kind/testdata/chaos-resume-three-task.yaml
    - test/integration/kind/output_test.go
    - charts/tide/templates/deployment.yaml
    - test/integration/kind/projects_pvc_test.go

key-decisions:
  - "Project-level control spec calls reconcileProjectPlannerDispatch directly (same package) to bypass the init-Job/PVC lifecycle; the full r.Reconcile path requires a Bound PVC and a completed init Job before reaching reconcileProjectPlannerDispatch, making a Job-creation assertion infeasible without extensive fixture setup"
  - "Control-spec pre-condition pump uses Eventually instead of fixed for-range-3 loop; fixed count was racy because reconciler step counts vary with cache-sync timing (Milestone needs 2 reconciles on fast cache, 1 extra on slow cache)"
  - "Helm required guard uses sprig required function at the $img assignment point; fail-fast at render time is cheaper than runtime InvalidImageName from the garbage value ':<appVersion>' that empty-string assignment produced"

patterns-established:
  - "Dispatcher-wired reconciler helpers (newBH*Reconciler) document both gates (r.Dispatcher != nil + SigningKey) in comments for future test authors"
  - "assertPlannerJobForParent uses tideproject.k8s/<level>-uid label (not parent-uid) matching the jobspec label written by podjob.BuildJobSpec"

requirements-completed: []

# Metrics
duration: 99min
completed: 2026-06-11
---

# Phase 13 Plan 06: Test-debt and WR gap closure Summary

**De-vacuated BillingHalt planner hold specs (WR-01), added promptPath to kind fixtures so admission passes (WR-02), and added helm required guard with contract test so empty subagent.defaults.image fails at render time with a named error (WR-04)**

## Performance

- **Duration:** ~99 min
- **Started:** 2026-06-11T18:30:00Z
- **Completed:** 2026-06-11T20:09:17Z
- **Tasks:** 2 (plus 1 flakiness-fix commit)
- **Files modified:** 6

## Accomplishments

- All four BillingHalt planner hold specs now exercise the actual checkBillingHalt code path (Dispatcher wired); deleting checkBillingHalt fails the suite (HALT-01 regression honesty)
- Per-level halt-cleared control specs prove dispatch proceeds after BillingHalt is cleared; Project level uses direct reconcileProjectPlannerDispatch call to avoid init-Job/PVC lifecycle complexity
- kind fixtures (three-task-wave.yaml, chaos-resume-three-task.yaml, output_test.go inline Task) now include spec.promptPath — Required + MinLength=1 since b612fce; admission was rejecting all three tasks
- helm template --set subagent.defaults.image= now fails render with named error rather than silently producing ":<appVersion>" garbage (InvalidImageName at runtime)
- TestHelmDeploymentTemplateEmptyImageFailsRender contract test added to projects_pvc_test.go

## Task Commits

1. **Task 1: De-vacuate BillingHalt hold specs (WR-01)** - `b603673` (test)
2. **Task 1 flakiness fix: Eventually-based pre-condition pump** - `b952d75` (fix)
3. **Task 2: promptPath fixtures + helm required guard (WR-02/WR-04)** - `edb4361` (fix)

## Files Created/Modified

- `internal/controller/billing_halt_regression_test.go` - Wired Dispatcher into all four newBH*Reconciler helpers; added assertPlannerJobForParent helper and per-level halt-cleared control specs with unique object names; replaced for-range-3 pump with Eventually for stability
- `test/integration/kind/testdata/three-task-wave.yaml` - Added promptPath to alpha/beta/gamma task specs
- `test/integration/kind/testdata/chaos-resume-three-task.yaml` - Added promptPath to alpha-chaos/beta-chaos/gamma-chaos task specs
- `test/integration/kind/output_test.go` - Added promptPath to inline Task YAML
- `charts/tide/templates/deployment.yaml` - Added required guard on subagent.defaults.image assignment
- `test/integration/kind/projects_pvc_test.go` - Added TestHelmDeploymentTemplateEmptyImageFailsRender with helm template subprocess execution

## Decisions Made

- Project-level control spec calls `reconcileProjectPlannerDispatch` directly (same-package access). The full `r.Reconcile` path goes through `reconcileProjectPhase2` which requires a Bound PVC + completed init Job before reaching `reconcileProjectPlannerDispatch`. Without a PVC, the reconciler returns 30s for the wrong reason (PVC not found, not billing halt) making the hold spec vacuous in a different way. Direct call bypasses all of this cleanly.
- `Eventually`-based pre-condition pump replaces `for range 3`. The fixed count raced against the cache: on fast cache the Milestone finalizer+ownerRef setup takes 2 reconciles (reconcile 1 -> finalizer, reconcile 2 -> ownerRef+dispatch), leaving reconcile 3 to go through the dispatch body a second time. But on slow or re-ordered cache, the third reconcile could return 0s due to Ginkgo randomized test ordering causing shared-state issues in the default namespace. `Eventually` retries until the reconciler actually returns 30s, which is the correct spec for "billing halt is reached."

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed flaky control-spec pre-condition pump**
- **Found during:** Task 1 (WR-01)
- **Issue:** `for range 3` reconcile pump was racy: ~2 in 5 runs produced 0s instead of 30s for the pre-condition assertion. Root cause: Ginkgo randomizes spec order; when Phase/Milestone control specs run after other billing-halt specs, the idempotency guard in reconcilePlannerDispatch can fire from shared envtest state. Also, fixed reconcile counts assume deterministic cache timing.
- **Fix:** Replaced all four control-spec pre-condition pumps with `Eventually(func(g Gomega) { ... g.Expect(result.RequeueAfter).To(Equal(30s)) }, 5s, 100ms).Should(Succeed())`
- **Files modified:** internal/controller/billing_halt_regression_test.go
- **Commit:** b952d75

---

**Total deviations:** 1 auto-fixed (Rule 1 - Bug)
**Impact on plan:** Essential for test reliability; the flakiness would block CI. No scope creep.

## Issues Encountered

- The Project-level hold spec was vacuously passing because `initJobRequeueAfterNoPVC = 30 * time.Second` coincidentally matches the billing halt requeue interval; the test returned 30s for the wrong reason (PVC not found in envtest default namespace). Resolved by calling `reconcileProjectPlannerDispatch` directly.

## Known Stubs

None - no stub/placeholder values introduced.

## Next Phase Readiness

- 13-07 (heavy gate) can now apply the kind fixtures without admission rejection; the three-task-wave and chaos-resume fixtures were previously failing at apply time
- HALT-01 regression coverage is now honest: the billing halt check IS reached by all four planner-level specs

## Self-Check: PASSED

- `b603673` exists in git log: FOUND
- `b952d75` exists in git log: FOUND
- `edb4361` exists in git log: FOUND
- `billing_halt_regression_test.go` modified: FOUND
- `three-task-wave.yaml` has promptPath: FOUND
- `chaos-resume-three-task.yaml` has promptPath: FOUND
- `deployment.yaml` has required guard: FOUND
- `projects_pvc_test.go` has TestHelmDeploymentTemplateEmptyImageFailsRender: FOUND

---
*Phase: 13-dispatch-image-resolution-provider-halt*
*Completed: 2026-06-11*
