---
phase: 53-chart-config-dashboard-provenance-surfacing
plan: 03
subsystem: api
tags: [findings-staging, artifact-push, dashboard-api, verify-loop, envtest-hygiene]

# Dependency graph
requires:
  - phase: 51
    provides: "Task loop LoopStatus/LastEvaluation (applyLoopStatus), LevelPhaseVerifyHalted/VerifyHalted verdict-final phases"
  - phase: 49
    provides: "tide-push kind==task findings.json staging consumer (cmd/tide-push/main.go), collectStageEnvelopes' cumulative <uid>:<kind>/<name> staging map"
provides:
  - "taskFindingsStageable(t) predicate: verdict-final (VerifyHalted/Succeeded/AwaitingApproval) AND LastEvaluation-recorded Task eligibility, shareable by plan 53-10's push trigger"
  - "collectStageEnvelopes now emits task-kind entries alongside milestone/phase/plan/project"
  - "GET /api/v1/nodes/task/{name}/artifacts admitted to the artifacts endpoint's closed allowlist"
affects: ["53-10", "53-08"]

# Tech tracking
tech-stack:
  added: []
  patterns: ["shared eligibility predicate as a named helper for reuse across a collector loop and a future push-trigger call site"]

key-files:
  created: []
  modified:
    - internal/controller/artifact_push.go
    - internal/controller/artifact_push_test.go
    - cmd/dashboard/api/artifacts.go
    - cmd/dashboard/api/artifacts_test.go
    - internal/controller/task_controller_test.go
    - internal/controller/wave_controller_test.go
    - internal/controller/plan_controller_test.go
    - internal/controller/boundary_push_test.go
    - internal/controller/dispatch_image_test.go
    - internal/controller/task_dispatch_traceparent_test.go

key-decisions:
  - "taskFindingsStageable does NOT reuse plannerMaterialized — Task's verify-loop phase vocabulary doesn't fit the planner-completion vocabulary, and reusing it would admit a pre-verify Succeeded Task with no verdict"
  - "LastEvaluation != nil is the presence-proxy guard on EVERY verdict-final arm (including VerifyHalted) — a VerifyHalted Task whose verifier crashed before producing a verdict has nil LastEvaluation and is excluded, preventing tide-push's fail-closed missing-findings.json guard from poisoning the whole cumulative push"
  - "Surfaced (not fixed, out of file scope): cmd/tide-langgraph-verifier/verifier/__main__.py never writes findings.json today (only out.json + termination-log) — LastEvaluation is the tightest predicate expressible from Task.Status alone, but does not yet guarantee findings.json presence on disk; flagged for whichever plan adds the verifier-side writer"
  - "Root-caused (not worked around) a test-suite regression Task 1 exposed: several Task cleanup helpers deleted a finalizer-guarded Task without clearing the finalizer first, leaving it listable across specs sharing the shared envtest 'default' namespace; fixed at the single shared cleanupTask helper (68 call sites) plus 5 other latent instances of the same pattern"

requirements-completed: [OBS-04]

# Metrics
duration: 40min
completed: 2026-07-21
---

# Phase 53 Plan 03: Task Findings Staging + Allowlist Admission Summary

**collectStageEnvelopes now stages verdict-final Task findings.json onto the run branch (evaluation-guarded predicate), and the dashboard artifacts endpoint admits kind=task — closing both Finding-10 gaps with unit coverage, plus a root-cause fix for a latent test-suite Task-leak bug the new collector exposed.**

## Performance

- **Duration:** ~40 min
- **Tasks:** 2 (plus 1 deviation fix)
- **Files modified:** 10 (4 declared + 6 test-hygiene fix)

## Accomplishments

- `taskFindingsStageable(t)`: a shared, named predicate (verdict-final phase AND `LastEvaluation != nil`) that gates a Task's inclusion in the cumulative artifact-push map — poison-proof against tide-push's fail-closed missing-`findings.json` guard, and designed to be reused verbatim by plan 53-10's push trigger.
- `collectStageEnvelopes` lists Tasks in the Project's namespace and emits `entry{"task", name, uid}` for every eligible Task, sorted after `project` in the existing (kind, name) order.
- `cmd/dashboard/api/artifacts.go`'s `artifactKinds` closed allowlist now admits `"task"`; the 400 error message names it.
- Root-caused a real test-suite regression the new Task-listing surfaced: cleanup helpers across several `_test.go` files deleted `Task` objects without clearing `taskFinalizer` first, leaking Terminating-but-still-listed Tasks across specs sharing the shared envtest `default` namespace. Fixed at the shared `cleanupTask` helper (68 call sites) plus 5 other latent call sites with the identical bug pattern.

## Task Commits

1. **Task 1: collectStageEnvelopes emits task-kind entries for verdict-final tasks with a recorded evaluation** - `20b6fd52` (feat)
2. **Task 2: Admit kind=task to the artifacts endpoint's closed allowlist** - `7bf3e5dc` (feat)
3. **Deviation: stop Task test fixtures leaking a finalizer-guarded object across specs** - `4ddde943` (fix)

## Files Created/Modified

- `internal/controller/artifact_push.go` - `taskFindingsStageable` predicate + Task list/filter loop in `collectStageEnvelopes`
- `internal/controller/artifact_push_test.go` - `TestCollectStageEnvelopes` with 6 subtests (a)-(f) covering the predicate's phase×evaluation matrix
- `cmd/dashboard/api/artifacts.go` - `artifactKinds["task"] = true` + updated 400 message
- `cmd/dashboard/api/artifacts_test.go` - kind=task admission (absent + available states), traversal-shaped kind still 400s, 400 body names task
- `internal/controller/task_controller_test.go` - `cleanupTask` now clears `Finalizers` before `Delete`
- `internal/controller/wave_controller_test.go`, `plan_controller_test.go` - delegate Task cleanup to the fixed `cleanupTask` instead of duplicating the unguarded Get+Delete
- `internal/controller/boundary_push_test.go`, `dispatch_image_test.go`, `task_dispatch_traceparent_test.go` - fixed the same unguarded-delete pattern at their own local cleanup sites

## Decisions Made

- **Predicate design, not reuse:** `taskFindingsStageable` is deliberately separate from `plannerMaterialized` — Task's verify-loop phase vocabulary (`VerifyHalted`/`Succeeded`/`AwaitingApproval`, gated by `LastEvaluation`) is semantically distinct from the four planner-completion kinds' vocabulary, and collapsing them would silently admit a pre-verify `Succeeded` Task with no verdict at all.
- **uid→srcDir verified, not assumed:** confirmed `entry{"task", t.Name, string(t.UID)}` resolves to the exact directory the verifier writes into. `dispatchVerifier` (task_controller.go:2164) builds the verifier Job with `podjob.BuildOptions.Task = task`, and `BuildJobSpec`'s `JobKindVerifier` branch derives the envelope subPath from `opts.Task.UID` (internal/dispatch/podjob/jobspec.go); tide-push resolves `srcDir = filepath.Join(cfg.Workspace, "envelopes", es.UID)` (cmd/tide-push/main.go's `stageEnvelopeArtifacts`). Both sides key off the Task's own UID — no fix needed.
- **LastEvaluation-as-presence-proxy — surfaced, not silently accepted:** `applyLoopStatus` (task_controller.go:2505-2530) sets `LastEvaluation` iff `out.Verdict != nil`, and is called unconditionally on every `haltVerify` leg, so a verifier envelope that was genuinely unreadable (`VerifierEnvelopeUnreadable`/`VerifierVerdictMissing`) correctly leaves `LastEvaluation` nil and is excluded. However, `cmd/tide-langgraph-verifier/verifier/__main__.py:211-225` (the verifier's own write path) currently writes only `out.json` (`write_envelope_out`) and the termination-log stub — **it never writes `findings.json` anywhere**. This means a recorded `LastEvaluation` does not yet *guarantee* `findings.json` landed on the PVC; it only proves a verdict was parsed. The controller cannot check file presence directly (no PVC mount), so this predicate is the tightest check expressible from `Task.Status` alone. Documented in `taskFindingsStageable`'s doc comment and here for whichever plan (53-10 or a follow-up) wires the verifier-side `findings.json` writer — until that lands, a `tide-push` carrying a task-kind entry will still hard-fail on the missing file regardless of how tightly this predicate is shaped.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Task cleanup helpers leaked finalizer-guarded objects across envtest specs**
- **Found during:** Verification (full `go test ./internal/controller/... ./cmd/dashboard/api/...` run after Task 1+2)
- **Issue:** `ProjectReconciler bp13b Test 9` (project_boundary_push_test.go) failed under the full 280-spec suite but passed in isolation. Git-archaeology (temporarily reverting `artifact_push.go` to the pre-Task-1 commit and re-running the full suite) proved the pre-existing tree is 280/280 green — the failure is caused by Task 1's new Task-listing in `collectStageEnvelopes`. Root cause: `cleanupTask` (and 2 other duplicated cleanup helpers, plus 3 more local closures) deleted a `Task` object without first clearing `taskFinalizer` ("tideproject.k8s/task-cleanup", added by a real `TaskReconciler.Reconcile` pass). A `Delete` on a finalizer-guarded object only sets `deletionTimestamp` — the object stays listable (Terminating, its last-observed `Status.Phase`/`LoopStatus.LastEvaluation` intact) until something processes the finalizer removal, which none of these specs' cleanup call sites did. Because the whole envtest suite shares one `default` namespace across all 280 specs with no per-spec isolation, a leaked verdict-final Task from an earlier spec (e.g. `task_verify_loop_test.go`'s VerifyHalted/Succeeded fixtures) polluted Test 9's `collectStageEnvelopes` call for an unrelated Project.
- **Fix:** `cleanupTask` (task_controller_test.go, 68 call sites across 9 files) now clears `Finalizers` before `Delete`, mirroring the idiom 6 other files in the suite already used correctly (`git_writer_test.go`, `reporter_spawn_idempotency_test.go`, `span_emission_test.go`, `task_traceonly_reporter_test.go`, `file_touch_gate_test.go`, and one closure in `boundary_push_test.go`). `cleanupWave`/`cleanupPlanFixture` now delegate their Task-deletion loop to `cleanupTask` instead of duplicating the unguarded logic. Fixed the same pattern at 3 more local call sites (`boundary_push_test.go`'s other closure, `dispatch_image_test.go`, `task_dispatch_traceparent_test.go`) found via a targeted grep for the identical anti-pattern. Left `verification_immutability_test.go`'s bare `Delete` calls untouched after confirming those Tasks are created via direct `k8sClient.Create` (never reconciled), so no finalizer is ever added there — no leak risk.
- **Files modified:** internal/controller/task_controller_test.go, wave_controller_test.go, plan_controller_test.go, boundary_push_test.go, dispatch_image_test.go, task_dispatch_traceparent_test.go
- **Verification:** Full suite (`go test ./internal/controller/... ./cmd/dashboard/api/... -count=1`) green with 2 different Ginkgo random seeds (default + `-ginkgo.seed=999`) after the fix; `make lint` clean (0 issues).
- **Committed in:** `4ddde943`

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** The fix was necessary for plan-declared verification (`go test ./internal/controller/... ./cmd/dashboard/api/... -count=1 green`) to genuinely pass — not scope creep, since the failure is a direct regression Task 1's own correct code exposed. No production code was touched by the fix; all 6 modified files are test-only.

## Issues Encountered

None beyond the deviation documented above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `taskFindingsStageable` is ready for plan 53-10 to consume verbatim at its Task verdict-final push trigger — same predicate, no re-derivation needed.
- The `findings.json`-writer gap in `cmd/tide-langgraph-verifier/verifier/__main__.py` is a real, verified blocker to the FULL end-to-end "findings browsable through gitfetch" claim (D-07/OBS-04) — a task-kind entry will hard-fail tide-push's cumulative push today because nothing writes `findings.json`. This must be resolved (likely alongside or before 53-10's trigger lands) or explicitly deferred with an owner.
- Dashboard-side `kind=task` requests now succeed end-to-end against the gitfetch path once a real `findings.json` exists on the run branch — verified with a synthetic fixture in `TestArtifactsTaskFindingsAvailable`.

---
*Phase: 53-chart-config-dashboard-provenance-surfacing*
*Completed: 2026-07-21*
