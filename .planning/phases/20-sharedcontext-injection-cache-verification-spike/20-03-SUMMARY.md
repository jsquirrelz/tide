---
phase: 20-sharedcontext-injection-cache-verification-spike
plan: "03"
subsystem: controller
tags: [shared-context, cache, envelope, materializer, dispatch]

# Dependency graph
requires:
  - phase: 20-sharedcontext-injection-cache-verification-spike
    plan: "01"
    provides: SharedContext struct fields on EnvelopeIn, EnvelopeOut, ChildCRDSpec, and all four CRD Spec types
provides:
  - BuildPlannerEnvelope with sharedContext param stamping EnvelopeIn.SharedContext at every planner level (D-07)
  - All five BuildPlannerEnvelope call sites updated to pass parent CRD Spec.SharedContext
  - MaterializeChildCRDs stamps Spec.SharedContext byte-identically onto Milestone, Phase, Plan, and Task children (D-05)
  - maxSharedContextBytes (64 KiB) size cap enforced before Create (T-20-03-01 etcd DoS guard)
  - Executor-omit lock proven by TestBuildEnvelopeInExecutorIgnoresSharedContext (CACHE-02)
  - Sibling byte-identity proven by TestBuildPlannerEnvelopeSharedContext
affects:
  - "phase 20 plan 04 (cache spike)"
  - "phase 20 plan 02 (template interpolation)"
  - "phase 21 observability"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Additive omitempty sharedContext param as final arg to BuildPlannerEnvelope (D-07 uniform stamp)"
    - "Per-Kind SharedContext stamp in MaterializeChildCRDs mirroring existing PromptPath=SourcePath pattern"
    - "Pre-flight size cap in allowlist loop (fail-closed, consistent with T-308 Kind allowlist guard)"
    - "TDD RED/GREEN per task: failing tests committed before implementation"

key-files:
  created: []
  modified:
    - internal/controller/dispatch_helpers.go
    - internal/controller/dispatch_helpers_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller_test.go
    - internal/reporter/materialize.go
    - internal/reporter/materialize_test.go

key-decisions:
  - "Project level passes empty string for sharedContext (ProjectSpec has no SharedContext field; project is the DAG root with no parent to propagate from)"
  - "Size cap enforced in pre-flight loop alongside Kind allowlist (before Create, fail-closed) rather than after-stamp per D-05 requirement"
  - "maxSharedContextBytes = 64 KiB -- 93x below etcd 1.5 MiB limit; well above realistic 300-700 byte curated blobs"

patterns-established:
  - "Additive sharedContext string final param pattern: BuildPlannerEnvelope(... helmDefaults ProviderDefaults, sharedContext string)"
  - "Per-Kind stamp after Unmarshal: X.Spec.SharedContext = child.SharedContext (mirrors tk.Spec.PromptPath = child.SourcePath)"
  - "Executor-omit lock test: assert buildEnvelopeIn output SharedContext == empty string regardless of task.Spec.SharedContext"

requirements-completed: [CACHE-02, CACHE-04]

# Metrics
duration: 25min
completed: 2026-06-15
---

# Phase 20 Plan 03: SharedContext Injection Summary

**CACHE-02/04 carry path complete: BuildPlannerEnvelope stamps parent-curated SharedContext byte-identically at all planner levels; MaterializeChildCRDs propagates it onto all four child CRD kinds; executor-omit lock and etcd size cap proven by tests.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-06-15T00:00:00Z
- **Completed:** 2026-06-15
- **Tasks:** 2 (each with RED+GREEN TDD commits)
- **Files modified:** 9

## Accomplishments

- `BuildPlannerEnvelope` gains `sharedContext string` as final parameter and stamps it into `EnvelopeIn.SharedContext` uniformly (D-07), covering milestone/phase/plan/project planner levels
- All five call sites updated: milestone, phase, plan controllers pass their CRD's `Spec.SharedContext`; project passes `""` (root level); both test fixtures pass `""`
- `MaterializeChildCRDs` stamps `Spec.SharedContext = child.SharedContext` on Milestone, Phase, Plan, and Task branches (4 stamps); Wave branch unchanged (no SharedContext field on WaveSpec)
- `maxSharedContextBytes = 64 KiB` size cap in pre-flight loop rejects oversized LLM-authored blobs before any `Create` (T-20-03-01 etcd DoS guard)
- Six new tests all green: sibling byte-identity, omitempty suppression, executor-omit lock, multi-kind identity, size cap rejection, PromptPath no-regression

## Task Commits

Each task was committed atomically with TDD RED then GREEN:

1. **Task 1 RED: SharedContext param + executor-omit tests** - `e940d67` (test)
2. **Task 1 GREEN: BuildPlannerEnvelope + 5 call sites** - `6d03059` (feat)
3. **Task 2 RED: Materializer SharedContext stamp + size cap tests** - `cbe5590` (test)
4. **Task 2 GREEN: MaterializeChildCRDs stamp + maxSharedContextBytes** - `4833afb` (feat)

## Files Created/Modified

- `internal/controller/dispatch_helpers.go` - Added `sharedContext string` param to `BuildPlannerEnvelope`; stamps `SharedContext: sharedContext` into envIn
- `internal/controller/dispatch_helpers_test.go` - `TestBuildPlannerEnvelopeSharedContext`, `TestBuildPlannerEnvelopeSharedContextEmpty` (2 existing test call sites updated to pass `""`)
- `internal/controller/milestone_controller.go` - Pass `ms.Spec.SharedContext`
- `internal/controller/phase_controller.go` - Pass `ph.Spec.SharedContext`
- `internal/controller/plan_controller.go` - Pass `plan.Spec.SharedContext`
- `internal/controller/project_controller.go` - Pass `""` (root; no parent SharedContext)
- `internal/controller/task_controller_test.go` - `TestBuildEnvelopeInExecutorIgnoresSharedContext`
- `internal/reporter/materialize.go` - `maxSharedContextBytes` const + pre-flight size cap + 4 `Spec.SharedContext = child.SharedContext` stamps
- `internal/reporter/materialize_test.go` - Identity, size-cap, and no-regression tests

## Decisions Made

- **Project level passes empty string**: `ProjectSpec` has no `SharedContext` field (only added on Milestone/Phase/Plan/Task specs in Plan 01). The project IS the DAG root -- there is no parent above it to propagate SharedContext from. Passing `""` is correct and consistent with test fixture behavior.
- **Size cap in pre-flight loop**: The `maxSharedContextBytes` check is placed in the same loop as the Kind allowlist (before any Create), ensuring fail-closed behavior consistent with T-308. An oversized blob aborts the whole batch.
- **`maxSharedContextBytes = 64 KiB`**: Conservative ceiling. Realistic curated blobs are ~300-700 bytes; 64 KiB is 93x below etcd's 1.5 MiB hard limit, leaving ample headroom while enforcing a clear bound.

## Deviations from Plan

**1. [Rule 1 - Bug] Project level passes empty string not `project.Spec.SharedContext`**
- **Found during:** Task 1 (call site update)
- **Issue:** `ProjectSpec` does not have a `SharedContext` field (Plan 01 only added it on Milestone/Phase/Plan/Task specs). Compiling `project.Spec.SharedContext` fails with `undefined`.
- **Fix:** Pass `""` for the project level with a comment explaining root-level semantics. This is correct behavior -- the project planner is never a sibling in a wave, so it never receives a parent-curated SharedContext.
- **Files modified:** `internal/controller/project_controller.go`
- **Committed in:** `6d03059` (Task 1 GREEN commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - bug: compile error at project call site)
**Impact on plan:** Fix is correct and necessary. No scope creep.

## Issues Encountered

None beyond the deviation above.

## Known Stubs

None. All stamps are fully wired.

## Threat Flags

None. All changes are additive fields on existing in-cluster structs. No new network endpoints, auth paths, or file access patterns were introduced. T-20-03-01 (etcd DoS guard) and T-20-03-02 (executor-omit lock) were both implemented and proven by tests.

## Next Phase Readiness

- CACHE-02 carry path is complete: parent blob -> ChildCRDSpec -> child Spec -> planner EnvelopeIn, byte-identical across siblings, executor path empty
- CACHE-04: one curated blob flows through the single uniform stamp path
- Planner templates (Plan 02, parallel Wave 2) can now reference `{{.SharedContext}}` from the `EnvelopeIn` struct -- the field is populated
- Cache verification spike (Plan 04) can proceed with the full carry path in place

## Self-Check: PASSED

- `internal/controller/dispatch_helpers.go` — FOUND
- `internal/reporter/materialize.go` — FOUND
- `.planning/phases/20-sharedcontext-injection-cache-verification-spike/20-03-SUMMARY.md` — FOUND
- Commit `e940d67` (test RED task 1) — FOUND
- Commit `6d03059` (feat GREEN task 1) — FOUND
- Commit `cbe5590` (test RED task 2) — FOUND
- Commit `4833afb` (feat GREEN task 2) — FOUND

---
*Phase: 20-sharedcontext-injection-cache-verification-spike*
*Completed: 2026-06-15*
