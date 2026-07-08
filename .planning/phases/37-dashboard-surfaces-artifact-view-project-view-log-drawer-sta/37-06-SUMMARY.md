---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 06
subsystem: controller
tags: [go, controller, tide-push, artifacts, staging, dash-02, single-flight]

# Dependency graph
requires:
  - phase: 34-push-internals
    provides: "single-flight tide-push-<uid> Job, verify-in-push, lastPushedSHA stamp, clean-tree skip"
  - phase: 36-agent-identity
    provides: "resolveAgentIdentity() env-sourced TIDE agent author identity on push Jobs"
  - plan: 37-02
    provides: "--stage-envelopes <uid>:<destPrefix> CSV contract on cmd/tide-push; .tide/planning/<kind>/<name>/ layout"
provides:
  - "PushOptions.StageEnvelopes + buildPushJob rendering --stage-envelopes=<CSV>"
  - "collectStageEnvelopes: cumulative planner-completed <uid>:<kind>/<name> map, sorted by kind then name"
  - "triggerArtifactPush: single-flight artifact-stage push on the tide-push-<uid> Job"
  - "Boundary pushes now also carry the cumulative map (R-05, one writer class)"
  - "Trigger wired at all four levels + milestone/phase/plan parked-arm retries (Pitfall 8)"
affects: [37-09, dashboard-artifact-view, DASH-02]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Conservative planner-materialized predicate (AwaitingApproval/Succeeded/Complete) — over-inclusion poisons the whole cumulative push (37-02 D-03 loud fail), under-inclusion self-heals"
    - "Park-arm-only artifact trigger so it never preempts the boundary push on the shared deterministic Job name (single-flight collision avoidance)"
    - "Parked-arm RequeueAfter 30s retry until a push lands (AwaitingApproval early-return cannot swallow the trigger)"

key-files:
  created:
    - internal/controller/artifact_push.go
    - internal/controller/artifact_push_test.go
  modified:
    - internal/controller/push_helpers.go
    - internal/controller/boundary_push.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go

key-decisions:
  - "plannerMaterialized excludes the ambiguous 'Running' phase — the landed level enum reuses 'Running' for BOTH planner-executing AND children-dispatching, so it cannot prove *.md presence. Only AwaitingApproval/Succeeded (m/ph/plan) and Complete (project) qualify. This diverges from the plan's assumed 'Planning-class' phase (no such phase landed); the CONTRACT it holds is over-safe inclusion."
  - "Artifact trigger placed in the gate-PARK arm only (before patchXAwaitingApproval), NOT before the whole gate branch. Placing it before the branch preempted the boundary push — both share the deterministic tide-push-<uid> Job name (R-05 single-flight), so the artifact push won, dropping the boundary's D-B2 commit message + task-branch integration. Park-arm placement satisfies D-01 (artifacts before approve gate) while the succeed path's boundary push, which now also carries the cumulative map, stages artifacts in the auto-gate case."
  - "Per-reconciler thin wrappers folded into direct triggerArtifactPush calls at the wiring sites — the acceptance grep is literal 'triggerArtifactPush' (would not match 'maybeTriggerArtifactPush'), and unused wrappers trip the unused linter."
  - "Project included via child-Milestone existence OR Complete — any materialized Milestone proves the project planner authored MILESTONE.md, so the project envelope's *.md is guaranteed present; the project has no approve gate (D-02) so early inclusion is a pure fidelity win."
  - "DASH-02 left Pending — 37-06 is the controller write-path trigger only; the end-to-end truth (artifacts on a real remote) is 37-09's kind-suite lock. Marking it Complete now would be a false claim (same reasoning as 37-02)."

patterns-established:
  - "Every push — boundary or artifact-triggered — carries the full cumulative map; a busy Job name loses nothing (single-flight no-op) because the next push self-heals"

requirements-completed: []  # DASH-02 write-path trigger implemented; requirement NOT complete until 37-09 e2e lock

# Metrics
duration: 27min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 06: Controller Artifact-Push Trigger Summary

**At every planner completion the level controller now stages the cumulative `<uid>:<kind>/<name>` planner-artifact map onto the run branch via the same single-flight `tide-push-<uid>` Job as boundary pushes — fired in the gate-park arm (before approve, D-01) with a 30s parked-arm retry, and additionally attached to every boundary push (R-05), so planning artifacts land in git before approve gates without a second writer class.**

## Performance

- **Duration:** ~27 min
- **Started:** 2026-07-08T09:48:18Z
- **Completed:** 2026-07-08T10:15:00Z
- **Tasks:** 2
- **Files created:** 2 · **Files modified:** 6

## Accomplishments

- `PushOptions.StageEnvelopes []string`; `buildPushJob` renders a single `--stage-envelopes=<CSV>` arg when non-empty (parsed by cmd/tide-push, 37-02).
- `collectStageEnvelopes` lists the project's Milestone/Phase/Plan CRs (namespace-scoped, mirroring `assembleProjectDepGraph`), filters to planner-materialized levels, adds the Project itself when it has materialized children or is Complete, and emits a `(kind, name)`-sorted `<uid>:<kind>/<name>` list so byte-identical restages are clean-tree no-ops.
- `triggerArtifactPush` mirrors `triggerBoundaryPush`'s guard chain (nil/git-less → nil; empty image → Info skip; empty run branch → skip for the parked retry; empty map → skip) and single-flight (deterministic `tide-push-<uid>` Get → exists → no-op), builds the push with the resolved agent identity + an artifact-stage commit message, and increments `PushJobsTotal{outcome="artifact-stage"}`.
- Boundary pushes (`triggerBoundaryPush`) now also attach `collectStageEnvelopes` output (best-effort, non-fatal on collection error) — one writer class, every push cumulative (R-05).
- Wired at all four levels: milestone/phase/plan trigger in the gate-park arm before `patchXAwaitingApproval`, plus a 30s `RequeueAfter` parked-arm retry in `reconcilePlannerDispatch`; the project triggers at planner completion (no gate → no parked arm).

## Task Commits

TDD RED→GREEN followed in-process; one commit per task to keep the tree buildable.

1. **Task 1: cumulative map + triggerArtifactPush + boundary augmentation** — `f2641ea` (feat)
2. **Task 2: wire four completion sites + parked-arm retries** — `2b7510a` (feat)

## Files Created/Modified

- `internal/controller/artifact_push.go` (NEW) — `plannerMaterialized`, `collectStageEnvelopes`, `buildArtifactStageMessage`, `triggerArtifactPush`.
- `internal/controller/artifact_push_test.go` (NEW) — cumulative+deterministic map; Job args carry `--stage-envelopes`/`--branch`/artifact-stage message; single-flight no-op; guard chain (nil/git-less/empty-image/no-branch/empty-map); parked-milestone trigger+30s requeue; Pitfall 8 busy-Job requeue regression guard.
- `internal/controller/push_helpers.go` — `PushOptions.StageEnvelopes` + render.
- `internal/controller/boundary_push.go` — attach cumulative map to boundary pushes (R-05).
- `internal/controller/{milestone,phase,plan}_controller.go` — park-arm trigger + parked-arm retry.
- `internal/controller/project_controller.go` — planner-completion trigger.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Completion-site trigger preempted the boundary push**
- **Found during:** Task 2 (full-package envtest — 4 `boundary_push_test.go` specs failed).
- **Issue:** The plan directs "invoke the trigger immediately after the reporter spawn and BEFORE any gate-park return." Placed there, the artifact push and the boundary push (same reconcile, in the auto/succeed path) collide on the shared deterministic `tide-push-<uid>` Job name (R-05 single-flight). The artifact push fired first, so the boundary push no-op'd and its D-B2 commit message (`tide: milestone <name> authored`) — and, at plan boundaries, the `--integrate-task-branches` merge — were dropped. Envtest caught `--commit-message=tide: stage planning artifacts (milestone)` where the boundary message was asserted.
- **Fix:** Relocated the milestone/phase/plan trigger into the gate-PARK arm only (immediately before `patchXAwaitingApproval`), still "before the gate-park return" per D-01, so it fires only when no boundary push follows in the same reconcile. The auto/succeed path's boundary push — which now also carries the cumulative map — stages artifacts there. Parked-arm 30s retries continue to cover the pre-approval window.
- **Files modified:** milestone_controller.go, phase_controller.go, plan_controller.go.
- **Verification:** full `go test ./internal/controller/` green (was 4 FAIL / 164 pass → all pass, ~87s).
- **Committed in:** 2b7510a (Task 2 commit).

### Contract-preserving divergences from the plan's assumed shape

- **No "Planning-class" phase exists in the landed enum.** The plan's predicate assumed a distinct planning phase to exclude; the landed level enum is `{"", Running, AwaitingApproval, Succeeded, Failed}` and reuses `Running` for both planner-executing and children-dispatching. `plannerMaterialized` therefore excludes `Running` entirely and admits only `AwaitingApproval`/`Succeeded` (m/ph/plan) and `Complete` (project). This is over-safe by design: a still-planning level has no `*.md`, and 37-02's staging fails the ENTIRE push loud on a missing `*.md` — over-inclusion poisons every level's artifacts, whereas under-inclusion self-heals on the next push.
- **Per-reconciler wrappers folded into direct calls** — the acceptance grep is literal `triggerArtifactPush` (case-sensitive; would not match `maybeTriggerArtifactPush`), and keeping unused wrappers trips the `unused` linter.

**Total deviations:** 1 auto-fixed bug + 2 contract-preserving shape adaptations (recorded per the plan's PRECONDITION clause).

## Requirements

- **DASH-02 left Pending.** 37-06 lands the controller write-path trigger (the other half of 37-02's `--stage-envelopes` mechanism). The end-to-end truth — artifacts actually materialized on a real remote — is 37-09's kind-suite lock. Marking DASH-02 Complete now would be a false claim, so REQUIREMENTS.md is unchanged (same discipline as 37-02).

## Threat Flags

None beyond the plan's threat model. T-37-06-01 (CR-name→path traversal) is mitigated upstream: names are DNS-1123 at admission and 37-02's parser validates fail-closed. T-37-06-02 (parked-requeue churn) is bounded by single-flight no-ops + clean-tree empty-commit skips. T-37-06-03 (second push writer breaking force-with-lease) is prevented by construction — zero new push mechanisms; StageEnvelopes rides the existing Job class.

## Self-Check

- `internal/controller/artifact_push.go` — FOUND (created)
- `internal/controller/artifact_push_test.go` — FOUND (created, 6 test funcs)
- `internal/controller/push_helpers.go` — FOUND (modified)
- `internal/controller/boundary_push.go` — FOUND (modified)
- Commit `f2641ea` — FOUND
- Commit `2b7510a` — FOUND
- `go build ./...` — OK
- `go vet ./cmd/... ./internal/...` — OK
- `gofmt -l internal/controller/*.go` — clean
- `bin/golangci-lint run internal/controller/` — 0 issues
- `go test ./internal/controller/ -run TestArtifactPush` — PASS (6 funcs)
- `go test ./internal/controller/` — PASS (full package, envtest, ~87s)
- Acceptance greps: `StageEnvelopes` in push_helpers.go = 4 (>=2); `collectStageEnvelopes` in boundary_push.go = 2 (>=1); `triggerArtifactPush` per controller file = project 1 / milestone 2 / phase 2 / plan 2 (>=1 each); parked-arm `RequeueAfter: 30 * time.Second` present in milestone + phase.

## Self-Check: PASSED

## Next Phase Readiness

- The write path from planner completion to run branch is live: boundary + artifact pushes both stage the cumulative `.tide/planning/<kind>/<name>/` layout via one single-flight Job. 37-09 can now assert artifacts land end-to-end on a real remote; DASH-02 flips Complete when that lock passes.

---
*Phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta*
*Completed: 2026-07-08*
