---
phase: 40-deprecate-v1alpha1-api
plan: 04
subsystem: api
tags: [go, kubebuilder, controller-runtime, ginkgo, envtest, subagent-dispatch]

# Dependency graph
requires:
  - phase: 40-03
    provides: all five dispatch controllers (project/milestone/phase/plan/task) compiling and running against api/v1alpha3
provides:
  - levelOverrideKey(level) — the D-02 subagent.levels semantic-rename mapping, wired into ResolveProvider and resolveImage
  - Resolved-model structured logging at all five dispatch sites (T-40-12 observability closure)
  - envtest coverage pinning per-level model resolution + unchanged dispatch identity across all five dispatch surfaces
affects: [40-05, 40-06, 40-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Override-key remap lives entirely inside the resolver (ResolveProvider/resolveImage), never at call sites — dispatch identity (envelope Level, Job labels, template selection) and the override-key semantics are deliberately decoupled so a semantic rename never risks the wrong subagent template firing"
    - "envtest per-level model-resolution proof: decode a dispatch Job's envelope-writer ENVELOPE_IN_B64 env var to inspect the resolved EnvelopeIn (Provider.Model, Level) — the only place the resolved model is observable on the Job itself"

key-files:
  created: []
  modified:
    - internal/controller/dispatch_helpers.go (levelOverrideKey + ResolveProvider/resolveImage rewire)
    - internal/controller/dispatch_helpers_test.go (3 pre-existing unit tests updated to the post-rename level->slot mapping)
    - test/integration/envtest/planner_dispatch_test.go (new envtest spec driving all 5 dispatch surfaces)
    - internal/controller/project_controller.go (resolved-model log at project-level dispatch)
    - internal/controller/milestone_controller.go (resolved-model log at milestone-level dispatch)
    - internal/controller/phase_controller.go (resolved-model log at phase-level dispatch)
    - internal/controller/plan_controller.go (resolved-model log at plan-level dispatch)
    - internal/controller/task_controller.go (resolved-model log at task executor dispatch)
    - internal/dispatch/podjob/jobspec.go (BuildOptions.Level doc comment lists all 5 legal values)

key-decisions:
  - "Followed the plan's DESIGN NOTE over 40-PATTERNS.md's superseded 'call-site literal shift' sketch: dispatch level strings (envelope Level, Job labels, template-selection switch) are permanently unchanged — only the override-key derivation inside ResolveProvider/resolveImage shifts. Zero edits to any controller's BuildPlannerEnvelope/Level/resolveImage call-site literals across both tasks."
  - "Task 2's per-dispatch-site log recomputes the resolved model rather than threading it through: the 4 planner sites capture BuildPlannerEnvelope's previously-discarded EnvelopeIn return value (already computed, free); the task executor site recomputes via ResolveProvider (mirrors the pre-existing resolveImage recompute-at-Job-creation pattern at the same site)."

patterns-established:
  - "levelOverrideKey(level string) string is the single source of truth for the D-02 mapping; both ResolveProvider and resolveImage derive `key := levelOverrideKey(level)` once and switch on `key`, not `level`, for their Levels.<slot> lookup"

requirements-completed: [CRANK-04]

# Metrics
duration: ~31min
completed: 2026-07-11
---

# Phase 40 Plan 04: Subagent Levels Semantic Rename (D-02) Summary

**`levelOverrideKey` remaps each of the 5 dispatch levels to its `Subagent.Levels` override slot per the folded todo's DECIDED table (project→Levels.Milestone, milestone→Levels.Phase, phase/plan→Levels.Plan, task→Levels.Task), closing the first-external-run bug where MILESTONE.md dispatch silently fell back to the top-level model — plus resolved-model logging at all five dispatch sites.**

## Performance

- **Duration:** ~31 min
- **Started:** 2026-07-11T20:48:30-04:00 (base commit)
- **Completed:** 2026-07-11T21:19:37-04:00
- **Tasks:** 2 completed
- **Files modified:** 9 (2 in Task 1 code + 1 test file; 6 in Task 2)

## Accomplishments

- `levelOverrideKey(level string) string` implements the todo's DECIDED mapping table verbatim, wired into both `ResolveProvider` and `resolveImage` via a single `key := levelOverrideKey(level)` — the four Levels.X switch cases themselves are untouched, only the value switched on shifts from `level` to `key`.
- The structural bug the rename fixes is now provably dead: pre-fix, `ResolveProvider(project, "project", ...)` matched no switch case (levelCfg stayed nil) and fell through to `Spec.Subagent.Model`/`""`; post-fix it resolves `Levels.Milestone` — the exact MILESTONE.md-runs-on-the-wrong-model bug from the first external run.
- Dispatch identity is provably unchanged: zero edits to any of the five controllers' call sites (`git diff --stat` on all five confirmed empty for Task 1; Task 2 only adds log lines using the already-passed level literal). The envelope `Level` field, the `tideproject.k8s/level` Job label, and the subagent template-selection switch all still see `"project"|"milestone"|"phase"|"plan"|"task"` exactly as before.
- New envtest spec (`test/integration/envtest/planner_dispatch_test.go`) drives a full Project→Milestone→Phase→Plan→Task hierarchy with four distinct per-level models, decoding each dispatched Job's envelope-writer `ENVELOPE_IN_B64` to assert the resolved `Provider.Model` and pin `Level`/Job-label identity — RED (`Expected: "" to equal: "model-milestone-md"`) confirmed against the pre-fix resolver, GREEN after the `levelOverrideKey` wire-up.
- Every dispatch now logs `"resolved subagent dispatch"` (level/model/image fields) at Info level — closing the folded todo's observability gap ("today the resolved model appears nowhere in pod spec, events, or conditions — only inside the PVC envelope").
- `internal/dispatch/podjob/jobspec.go`'s `BuildOptions.Level` doc comment now documents all 5 legal values including `"project"` (previously undocumented, now explained as predating this phase rather than left looking out-of-spec).

## Task Commits

1. **Task 1: levelOverrideKey mapping in ResolveProvider/resolveImage, test-first** - `3cee1b9` (feat)
2. **Task 2: Resolved-model dispatch logging + Level doc alignment** - `05fe8ba` (feat)

_No plan-metadata commit — this worktree agent does not update STATE.md/ROADMAP.md; the orchestrator commits those after the wave merges._

## Files Created/Modified

- `internal/controller/dispatch_helpers.go` - `levelOverrideKey` added; `ResolveProvider`/`resolveImage` switch on the mapped key; doc comments rewritten to describe the new resolution chain
- `internal/controller/dispatch_helpers_test.go` - 3 pre-existing unit tests (`TestResolveProviderPerLevelWins`, `TestResolveProviderHelmDefaultFallback`, `TestResolveProviderParamsMerge`) updated from the pre-rename level→slot assumption to the post-rename mapping; 1 test's stale comment corrected
- `test/integration/envtest/planner_dispatch_test.go` - new `Describe` block + `decodeEnvelopeFromJob` helper covering all 5 dispatch surfaces
- `internal/controller/{project,milestone,phase,plan,task}_controller.go` - one structured `"resolved subagent dispatch"` Info log per dispatch site; the 4 planner sites capture the previously-discarded `EnvelopeIn` return value from `BuildPlannerEnvelope`
- `internal/dispatch/podjob/jobspec.go` - `BuildOptions.Level` doc comment lists all 5 legal values, cross-references `levelOverrideKey`

## Decisions Made

- **Followed the plan's own DESIGN NOTE over 40-PATTERNS.md's "call-site literal shift" sketch.** 40-PATTERNS.md (authored during research, before the plan's ASK-FIRST resolution) shows the four controllers' level literals shifting (e.g. `"project"` → `"milestone"`); 40-04-PLAN.md explicitly rejects that shape because `BuildPlannerEnvelope` uses the SAME `level` argument for both `EnvelopeIn.Level` (template selection) and `ResolveProvider` (model resolution) — shifting the literal would hand the PLAN.md dispatch and the task-DAG dispatch the same `Level: "plan"`, breaking template selection for one of them. I implemented per the PLAN.md interfaces table (dispatch level strings unchanged forever; only the internal override-key derivation shifts), not per 40-PATTERNS.md's superseded sketch.
- **Task 2's model value is recomputed/captured, not threaded as a new parameter.** The 4 planner-dispatch sites already call `BuildPlannerEnvelope` and previously discarded its `EnvelopeIn` return value (`_, envInJSON, err := ...`); capturing it costs nothing. The task executor site's `createDispatchJob` never had the `EnvelopeIn` in scope (it's built earlier in `buildEnvelopeIn`/`prepareDispatch` and only the marshaled bytes flow through `taskDispatchSpec`), so it recomputes via `ResolveProvider(project, "task", ...)` — mirroring the pre-existing `resolveImage(project, "task", ...)` recompute already present at that exact call site.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] 3 pre-existing `dispatch_helpers_test.go` unit tests encoded the pre-rename level→slot mapping**
- **Found during:** Task 1 (`make test` after wiring `levelOverrideKey` into `ResolveProvider`)
- **Issue:** `TestResolveProviderPerLevelWins`, `TestResolveProviderHelmDefaultFallback`, and `TestResolveProviderParamsMerge` called `ResolveProvider`/checked `helmDefaults.Models` keyed by the OLD (pre-D-02) level→slot assumption (e.g. dispatch level `"milestone"` directly reading `Levels.Milestone`). Post-rename, `"milestone"` reads `Levels.Phase` instead — these 3 tests failed with the fixture's per-level override/helm-default now landing in the wrong (unset) slot.
- **Fix:** Updated each fixture to set the field/helm-default-key that dispatch level actually resolves post-rename (`"milestone"`→`Levels.Phase`/`"phase"` key; `"phase"`→`Levels.Plan`/`"plan"` key), preserving each test's original intent (precedence-chain behavior) while asserting the correct post-rename slot. Also corrected `TestResolveImage_ProjectLevel_NoLevelTier`'s stale comment (the "project" level DOES now have a level-config case — `Levels.Milestone` — the test still passes only because that fixture leaves `Levels.Milestone` unset).
- **Files modified:** `internal/controller/dispatch_helpers_test.go`
- **Verification:** `go test ./internal/controller/... -short` — all 204 specs green (was 3 failures before the fix); `make test` exits 0.
- **Committed in:** `3cee1b9` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — pre-existing tests updated to match the deliberate, plan-mandated behavior change)
**Impact on plan:** Necessary consequence of implementing Task 1's core change; no scope creep — the plan's own acceptance criterion (`make test` exits 0) required it.

## Issues Encountered

- **Driving the project-level dispatch surface in envtest required bypassing init-Job/PVC ceremony**, since envtest has no real kubelet to complete the init Pod. Resolved by reusing the existing `boundary_push_test.go` pattern (`makeBoundPVC` + directly status-patching `Status.Git.BranchName`) rather than inventing a new fixture shape — confirmed via reading `reconcileProjectPhase2`/`reconcilePhase3Lifecycle` that this is the same bypass an existing, passing test already relies on.
- **The suite-registered (background) `ProjectReconciler` carries no `SigningKey`** (by design — per `boundary_push_test.go`'s comment, so it can't race `leak_blocked_test.go`'s hand-built push Jobs), so it never reaches project-level planner dispatch. An explicit `ProjectReconciler` instance (mirroring this file's existing explicit-`MilestoneReconciler` pattern) was required for Test 1; the other four levels' suite-registered reconcilers do carry `SigningKey` and could have been used directly, but explicit instances were used throughout for consistency and determinism (matches `gates_test.go`'s established `newXReconcilerForGateIT` convention).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `levelOverrideKey` is the sole diff surface for the D-02 mapping; a future adjustment to the operator ladder (e.g. splitting the D-11 `plan`/`phase` collapse back into two keys) is a one-function change with existing envtest + unit coverage to pin the new mapping against.
- Dispatch identity (envelope `Level`, Job labels, subagent template selection) is untouched and pinned by Test 6 of the new envtest spec — Plans 40-05/40-06 (package removal, docs/samples sweep) can proceed without re-verifying this surface.
- All 40-04 acceptance criteria met: `planner_dispatch` envtest green (56 specs, full suite); `make test` green; zero diffs in the forbidden-touch files (`internal/subagent/`, `cmd/stub-subagent/`, `internal/gates/`, `internal/controller/push_helpers.go`).

---
*Phase: 40-deprecate-v1alpha1-api*
*Completed: 2026-07-11*

## Self-Check: PASSED

- Verified all 9 created/modified files exist on disk (dispatch_helpers.go, dispatch_helpers_test.go, planner_dispatch_test.go, the 5 controller files, jobspec.go).
- Verified both commit hashes (`3cee1b9`, `05fe8ba`) exist in `git log --oneline --all`.
