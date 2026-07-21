---
phase: 53-chart-config-dashboard-provenance-surfacing
plan: 10
subsystem: infra
tags: [kubernetes, controller-runtime, git, findings, verify-loop, task-loop]

# Dependency graph
requires:
  - phase: 53-chart-config-dashboard-provenance-surfacing (plan 53-03)
    provides: taskFindingsStageable predicate + collectStageEnvelopes task-kind entries
  - phase: 53-chart-config-dashboard-provenance-surfacing (plan 53-06)
    provides: verification enablement gates in task_controller.go
provides:
  - "Task verdict-final findings-push trigger (maybeTriggerTaskFindingsPush): stages + pushes a Task's verifier findings the moment it reaches VerifyHalted/AwaitingApproval(loop-exhausted)/Succeeded, even while ConditionVerifyHalt freezes all dispatch project-wide"
  - "triggerArtifactPush ensure-entry union (ensureTaskEntries) so a just-patched Task rides the push even when the informer cache hasn't observed the status patch yet"
  - "TaskReconcilerDeps.TidePushImage wired from the same tidePushImage local ProjectReconciler already uses"
affects: [53-08 findings-view, 53-09, dashboard-provenance]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Verdict-final push trigger: a git-write side effect fired from three seams (haltVerify, markVerifiedSucceeded, VerifyHalted terminal short-circuit) that deliberately sits AFTER all dispatch-hold gates so it is never blocked by checkVerifyHalt/checkDispatchHolds"
    - "Ensure-entry union on a List-derived cumulative map: a variadic escape hatch (triggerArtifactPush's `ensure ...*Task` param) that guarantees a just-mutated object's own entry is present even when the List backing the map raced the mutation"

key-files:
  created:
    - internal/controller/task_findings_push_test.go
  modified:
    - internal/controller/task_controller.go
    - internal/controller/artifact_push.go
    - internal/controller/artifact_push_test.go
    - cmd/manager/main.go

key-decisions:
  - "maybeTriggerTaskFindingsPush returns (carried bool, err error) rather than a ctrl.Result directly — only the VerifyHalted terminal short-circuit arm needs retry semantics (5s RequeueAfter while !carried); the two verdict-final seams (haltVerify, markVerifiedSucceeded) are fire-and-forget since the terminal arm's own re-reconcile is the backstop"
  - "ensureTaskEntries lives in artifact_push.go beside collectStageEnvelopes/stageEntry rather than task_controller.go — keeps the staging-map entry format in one file so collectStageEnvelopes and the ensure union can never diverge on shape"
  - "No new push mechanism: the trigger reuses the deterministic tide-push-<project.UID> Job name, the single-flight Get-before-Create guard, and the stagedEnvelopesAnnotation carried-entry check that ProjectReconciler's isStaleArtifactPush already consumes"

requirements-completed: [OBS-04]

# Metrics
duration: 12min
completed: 2026-07-21
---

# Phase 53 Plan 10: Task Verdict-Final Findings-Push Trigger Summary

**A VerifyHalted/AwaitingApproval/Succeeded Task now stages and pushes its verifier findings through the existing tide-push machinery at the exact verdict-final transition — even while a project-wide VerifyHalt freezes all dispatch — closing the plan-check BLOCKER that left findings unreachable until `tide resume`.**

## Performance

- **Duration:** ~12 min (base commit 01:37:18 → last task commit 01:49:08)
- **Tasks:** 2
- **Files modified:** 5 (1 created, 4 modified)

## Accomplishments
- `maybeTriggerTaskFindingsPush` (task_controller.go): the verdict-final trigger, edge-gated on the `stagedEnvelopesAnnotation` carried-entry check, reusing `taskFindingsStageable` (53-03) as its eligibility predicate so trigger and collector never diverge.
- `triggerArtifactPush` gained an optional variadic ensure-entry union (`ensureTaskEntries`) so a just-patched Task's own `<uid>:task/<name>` entry rides the push even when the informer cache backing `collectStageEnvelopes`' List hasn't observed the status patch yet — the exact race that would otherwise leave a frozen VerifyHalted project stuck.
- Wired at all three verdict-final seams: `haltVerify` (covers both the escalate→VerifyHalted and requireApproval→AwaitingApproval legs in one call), `markVerifiedSucceeded`, and the `VerifyHalted` terminal short-circuit arm (5s `RequeueAfter` retry until carried, then steady state — mirrors the `Failed` branch's `setFailureHaltIfNeeded`-at-terminal precedent immediately above it).
- `TaskReconcilerDeps.TidePushImage` wired in `cmd/manager/main.go` from the same `tidePushImage` local `ProjectReconciler`/`plannerDeps` already receive — no second image source.
- Proving test suite (`task_findings_push_test.go`, 4 subtests) pins the blocker proof (push fires while `ConditionVerifyHalt=True`), the no-churn edge gate, the busy-race retry, and the T-53-25 nil-evaluation poison guard.

## Task Commits

Each task was committed atomically:

1. **Task 1: Verdict-final findings-push trigger** - `eb26ff8f` (feat)
2. **Task 2: Proving tests — push fires while ConditionVerifyHalt is True** - `c2108a64` (test)

_No separate plan-metadata commit — this SUMMARY is committed in worktree mode per the parallel-executor protocol (STATE.md/ROADMAP.md excluded; orchestrator owns those after merge)._

## Files Created/Modified
- `internal/controller/artifact_push.go` - `stageEntry` extraction + `ensureTaskEntries` union; `triggerArtifactPush` gained the variadic `ensure ...*Task` param
- `internal/controller/task_controller.go` - `TaskReconcilerDeps.TidePushImage` field, `maybeTriggerTaskFindingsPush` helper, three call sites (haltVerify, markVerifiedSucceeded, VerifyHalted terminal arm)
- `internal/controller/artifact_push_test.go` - 3 new tests covering the ensure-entry union (add-missing, dedup, no-ensure-callers-unaffected)
- `internal/controller/task_findings_push_test.go` - new file, 4 proving tests (a)-(d)
- `cmd/manager/main.go` - `TidePushImage: tidePushImage` added to the `TaskReconcilerDeps` literal

## Decisions Made
- The trigger's return shape is `(carried bool, err error)`, not a `ctrl.Result` — only the VerifyHalted terminal short-circuit arm converts `!carried` into a 5s `RequeueAfter`; the two verdict-final seams are non-fatal fire-and-forget (log and continue), matching `setVerifyHaltIfNeeded`'s tolerated-error posture.
- `stageEntry`/`ensureTaskEntries` live in `artifact_push.go` (not `task_controller.go`) so the staging-map entry format has exactly one source of truth shared by `collectStageEnvelopes` and the new ensure-union path.
- No new push mechanism: same deterministic `tide-push-<project.UID>` Job name, same single-flight Get-before-Create guard, same `stagedEnvelopesAnnotation` the `ProjectReconciler`'s `isStaleArtifactPush` already reads — one writer class (R-05), per the plan's binding design constraints.

## Deviations from Plan

None — plan executed exactly as written. The four existing planner-tier `triggerArtifactPush` call sites (milestone×2, phase×2, plan×2, project×1) pass no `ensure` args and are unaffected (verified via `TestArtifactPush_*` regression + a dedicated `TestArtifactPush_EnsureEntryUnion_NoEnsureArgsUnaffected` test).

## Issues Encountered

None during implementation. Full-package `go test ./internal/controller/... -count=1` cannot run the Ginkgo `TestControllers` suite in this worktree — `envtest`'s etcd binary is absent (`/usr/local/kubebuilder/bin/etcd: no such file or directory`), a pre-existing environment limitation unrelated to this plan's changes (confirmed: zero envtest/Ginkgo files touched by this diff). The plan's own-entry-point unit tests are the required proof per the parallel-execution briefing, and both filtered runs are green:
- `go test ./internal/controller/... -run TestArtifactPush -count=1` — 6 tests, all PASS (3 pre-existing + 3 new ensure-entry-union tests)
- `go test ./internal/controller/... -run TestTaskFindingsPush -count=1 -v` — 4 subtests (a)-(d), all PASS

`make lint` was not run — `golangci-lint` is not installed in this worktree's PATH and the `lint` Makefile target additionally depends on `demo-fixture`, which requires generated/gitignored assets absent from a fresh worktree (the same known limitation the parallel-execution briefing calls out for whole-repo `go build ./...`). `gofmt -l` on all touched files reports no issues; `go vet ./internal/controller/...` is clean; `go build ./internal/... ./cmd/manager/... ./pkg/...` is clean.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The 53-08 "View findings" disclosure now has real data to render for its primary scenario (a VerifyHalted, project-frozen Task) without requiring `tide resume` first — the plan-check BLOCKER this plan existed to close is resolved at the root.
- No architectural follow-ups identified. The `LastEvaluation-as-presence-proxy` surfaced contradiction documented in `artifact_push.go`'s `taskFindingsStageable` doc comment (a recorded `LastEvaluation` does not yet imply `findings.json` landed on the PVC — the verifier never writes one) remains open and out of this plan's declared file scope; flagged there for whichever plan wires the verifier-side `findings.json` writer.

---
*Phase: 53-chart-config-dashboard-provenance-surfacing*
*Completed: 2026-07-21*
