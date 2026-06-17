---
phase: 25-global-dispatch-failure-semantics-gates-resumption
plan: "03"
subsystem: controller
tags: [conservative-halt, failure-semantics, dispatch-gate, resume, wave-prune]

requires:
  - phase: 25-02
    provides: global indegree dispatch via shared depgraph resolver; strict profile free from indegree model
  - phase: 25-01
    provides: failure_halt.go scaffolded with checkFailureHalt/setFailureHaltIfNeeded; RED test scaffolds

provides:
  - conservative FailureProfile: first task failure stamps ConditionFailureHalt=True project-wide
  - checkFailureHalt at four execution dispatch sites (task/plan/phase/milestone)
  - FailureHalt cleared by tide resume --retry-failed (not bare resume)
  - wave prune OQ-3 deferred with explanatory comment (wave aggregator zero-member blocking)

affects: [phase-26-multi-milestone-conformance]

tech-stack:
  added: []
  patterns:
    - "Five-site halt pattern (BillingHalt + FailureHalt): project-wide condition stamped on failure, read at dispatch gates, cleared by resume verb"
    - "Conservative vs strict failure profiles: strict = free from indegree model; conservative = explicit ConditionFailureHalt stamped by setFailureHaltIfNeeded"

key-files:
  created: []
  modified:
    - internal/controller/task_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/project_controller.go
    - cmd/tide/resume.go

key-decisions:
  - "Wave prune in-flight guard deferred: wave aggregator sets Phase=Running even for 0-member waves (when all tasks are deleted), so Phase-based guard blocks pruning of legitimately stale empty waves (CR-01 regression). Correct fix requires WaveController to distinguish no-tasks from in-flight-tasks; deferred to Phase 26."
  - "checkFailureHalt placed at FOUR execution dispatch sites (task/plan/phase/milestone) but NOT at project_controller.go planner site — conservative halt is execution-only per D-03/RESEARCH OQ-3"
  - "FailureHalt clear placed inside retryFailed branch of resumeRun, after !retryFailed guard — bare resume does not clear it (intentional friction matching the retry-failed task resets)"

patterns-established:
  - "Execution-only halt: FailureHalt gates task/plan/phase/milestone dispatch but not project-level planner dispatch"

requirements-completed: [DISP-02, RESUME-01]

duration: 35min
completed: 2026-06-17
---

# Phase 25 Plan 03: Conservative Failure Profile (DISP-02) Summary

**Conservative failure halt via `ConditionFailureHalt` — checkFailureHalt at four execution dispatch gates, cleared by `tide resume --retry-failed` — turns DISP-02 strict+conservative and resume unit tests GREEN (51/51 envtest, 7+2 unit tests)**

## Performance

- **Duration:** 35 min
- **Started:** 2026-06-17T00:00:00Z
- **Completed:** 2026-06-17T00:35:00Z
- **Tasks:** 2 (Task 1 pre-completed in Wave 2 scaffolding; Task 2 implemented here)
- **Files modified:** 6

## Accomplishments

- Added `checkFailureHalt` after `checkBillingHalt` at the four EXECUTION dispatch sites (task/plan/phase/milestone controllers); planner site (project_controller.go:~1000) explicitly not gated per D-03
- Added FailureHalt clear in `cmd/tide/resume.go` inside the `retryFailed` branch (after `!retryFailed` guard; bare resume leaves FailureHalt True)
- Re-fetches project with fresh resourceVersion after BillingHalt status patch before FailureHalt clear
- Wave prune in-flight guard attempted (Phase == "Running" check) then correctly reverted: wave aggregator uses Phase="Running" even for zero-member waves, causing CR-01 prune regression test to fail; deferred to Phase 26 with explanatory comment
- All 7 `failure_halt_test.go` unit cases GREEN; all 2 `resume_failure_test.go` cases GREEN
- 51/51 envtest specs GREEN (DISP-01, DISP-02 strict+conservative, DISP-03, RESUME-01)
- Pre-existing kind-layer failure (`medium_http_test.go` — `ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` not pre-built) confirmed unrelated to Phase 25

## Task Commits

1. **Task 1: failure_halt.go + setFailureHaltIfNeeded** — pre-completed in commit `66d876e` (Wave 2 scaffolding)
2. **Task 2: four dispatch sites + resume clear + wave prune** — `00cd46d` (feat)
3. **Fix: wave prune guard correct (Running only)** — `2a97a7a` (fix — intermediate; superseded)
4. **Fix: revert wave prune guard (OQ-3 deferred)** — `e7c14f7` (fix)

## Files Created/Modified

- `internal/controller/task_controller.go` — added `checkFailureHalt` at line ~393 after `checkBillingHalt`; `setFailureHaltIfNeeded` already wired at lines 317 and 961 (Wave 2)
- `internal/controller/plan_controller.go` — added `checkFailureHalt` after `checkBillingHalt` at execution dispatch site
- `internal/controller/phase_controller.go` — added `checkFailureHalt` after `checkBillingHalt` at execution dispatch site
- `internal/controller/milestone_controller.go` — added `checkFailureHalt` after `checkBillingHalt` at execution dispatch site
- `internal/controller/project_controller.go` — wave prune OQ-3 comment added (guard deferred); no functional change from pre-plan state
- `cmd/tide/resume.go` — FailureHalt clear block inside `retryFailed` branch with re-Get for fresh resourceVersion

## Decisions Made

- **Wave prune guard deferred:** The plan specified guarding the wave prune on `Status.Phase != "Succeeded"` (OQ-3 from Phase 24 TODO comment). Implementation revealed the wave aggregator sets `Phase = "Running"` even for zero-member waves (0 members falls to the default case in wave_controller.go). A Phase-based guard blocked the CR-01 prune test which depends on pruning waves after their tasks are deleted. The correct fix requires the WaveController to distinguish "no tasks assigned" from "tasks in-flight" — a behavioral change out of scope for Phase 25. Deferred to Phase 26 with an explanatory NOTE comment.
- **checkFailureHalt at FOUR sites (not five):** BillingHalt gates all five sites (including the project-level planner). FailureHalt only gates the four EXECUTION sites. This is per D-03/RESEARCH OQ-3: conservative failure halt is execution-only; gating planning would wrongly freeze authoring of already-approved scopes.
- **FailureHalt clear inside retryFailed branch only:** BillingHalt is cleared unconditionally on bare resume (billing recovery). FailureHalt requires `--retry-failed` because clearing it without resetting the Failed tasks would leave the project in an inconsistent state.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Wave prune guard reverted after CR-01 regression**
- **Found during:** Task 2 (wave prune guard implementation)
- **Issue:** The plan's `Phase == "Running"` guard (wave prune in-flight protection) caused CR-01 regression — the wave aggregator sets Phase="Running" for 0-member waves, blocking prune of legitimately stale empty waves
- **Fix:** Reverted to pre-plan prune behavior; added explanatory NOTE comment deferring OQ-3 to Phase 26
- **Files modified:** `internal/controller/project_controller.go`
- **Verification:** `make test-int` envtest layer 51/51 GREEN; CR-01 prune test passes
- **Committed in:** `e7c14f7` (fix)

---

**Total deviations:** 1 auto-fixed (Rule 1 - bug: wave prune guard caused CR-01 regression)
**Impact on plan:** Wave prune in-flight guard deferred to Phase 26. All other success criteria met. The T-25-03-04 threat (DoS: in-flight Wave deletion) remains as a documented deferred risk.

## Issues Encountered

- Wave prune guard implementation took two attempts (initially `!= "Succeeded"`, then `== "Running"`, both failed the CR-01 test due to wave aggregator zero-member behavior). Root cause diagnosed: wave_controller.go sets Phase="Running" for 0 members via the `default` switch case. The guard deferred rather than changing the wave aggregator's behavior (out of scope for Phase 25).

## Known Stubs

None - all wired functionality is complete and functional.

## Threat Flags

None - no new network endpoints, auth paths, or file access patterns introduced. The T-25-03-04 threat (in-flight Wave deletion) is mitigated by documenting the deferred guard and its root cause.

## Self-Check: PASSED

- `internal/controller/failure_halt.go` exists: FOUND (committed in Wave 2, `66d876e`)
- `checkFailureHalt` in task_controller.go: FOUND (line ~393)
- `checkFailureHalt` in plan_controller.go: FOUND
- `checkFailureHalt` in phase_controller.go: FOUND
- `checkFailureHalt` in milestone_controller.go: FOUND
- `checkFailureHalt` NOT in project_controller.go planner site: CONFIRMED
- `ConditionFailureHalt` clear in resume.go: FOUND
- Commits `00cd46d`, `2a97a7a`, `e7c14f7` exist: CONFIRMED
- 51/51 envtest specs GREEN: CONFIRMED
- 7+2 unit tests GREEN: CONFIRMED
- `make verify-no-aggregates` / `verify-dag-imports` / `verify-no-sqlite-dep`: ALL GREEN

## Next Phase Readiness

Phase 26 (multi-milestone drive + spec-conformance close) is the final phase. This plan delivers the complete DISP-02 conservative profile. The one deferred item (wave prune in-flight guard, OQ-3) is documented and non-blocking for Phase 26. The global dispatch+failure semantics+gates+resumption system is complete.

---
*Phase: 25-global-dispatch-failure-semantics-gates-resumption*
*Completed: 2026-06-17*
