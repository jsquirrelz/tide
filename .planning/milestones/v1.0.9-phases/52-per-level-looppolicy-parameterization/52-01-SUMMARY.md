---
phase: 52-per-level-looppolicy-parameterization
plan: 01
subsystem: api
tags: [crd-schema, kubebuilder, cel-validation, loop-contract, controller-gen, helm]

# Dependency graph
requires:
  - phase: 51-the-task-loop
    provides: VerificationSpec standalone type (task_types.go), LoopPolicy/LoopStatus shared types (Phase 49), the CEL immutable-once-Locked rule, the LevelPhase* Task-only vocabulary and TestLoopStatus_NoForbiddenFields guard this plan extends
provides:
  - LoopLevel enum type + 5 constants (task/plan/phase/milestone/project) + LoopPolicy.Level field
  - PlanSpec.Verification (VerificationSpec re-embed) + PlanStatus.LoopStatus (own iteration counter, distinct from WaveIntegration.Attempts)
  - ProjectSpec.Verification (new VerificationDefaults struct — 5 optional *VerificationSpec slots mirroring Gates) with the Pitfall-4 Draft-authoring convention documented
  - LoopStatus embedded on PhaseStatus/MilestoneStatus/ProjectStatus (LastEvaluation + ExitReason surface for maxIterations:0 levels)
  - Generalized LevelPhaseVerifying/LevelPhaseVerifyHalted doc comments + VerifyHalt "what SETS it" prose covering the new Plan/Phase/Milestone/Project call sites
  - Regenerated zz_generated.deepcopy.go, config/crd/bases/*.yaml, and charts/tide-crds/ for plans/phases/milestones/projects
  - TestLoopStatus_NoForbiddenFields-style compile-time guard (var block) proving all four new LoopStatus embedding sites use the guarded type
affects: [52-02, 52-03, 52-04, 52-05, 52-06, 52-07, 52-08, 52-09, 52-10, 52-11]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Type re-embedding for cross-level contracts: VerificationSpec re-embedded verbatim on PlanSpec/ProjectSpec so its type-attached CEL rule travels with zero shape changes (D-01)"
    - "Per-level defaults map mirroring an existing precedent (VerificationDefaults mirrors Gates' 5-field per-level shape exactly, but with pointer fields so absence is distinguishable from an empty struct)"
    - "Compile-time struct-literal/var-assertion guards for cross-cutting invariants (LOOP-03 no-history now covers 4 new embedding sites via `var _ LoopStatus = XStatus{}.LoopStatus`)"

key-files:
  created: []
  modified:
    - api/v1alpha3/loop_types.go
    - api/v1alpha3/loop_types_test.go
    - api/v1alpha3/plan_types.go
    - api/v1alpha3/project_types.go
    - api/v1alpha3/phase_types.go
    - api/v1alpha3/milestone_types.go
    - api/v1alpha3/shared_types.go
    - api/v1alpha3/zz_generated.deepcopy.go
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_projects.yaml
    - charts/tide-crds/templates/plan-crd.yaml
    - charts/tide-crds/templates/phase-crd.yaml
    - charts/tide-crds/templates/milestone-crd.yaml
    - charts/tide-crds/templates/project-crd.yaml

key-decisions:
  - "LoopPolicy.Level is stamped by the resolver (ResolveLoopPolicy, a later plan), never authored directly — the field exists here as the schema surface SC3's resolver will populate"
  - "VerificationDefaults uses pointer *VerificationSpec fields (unlike Gates' value strings) so an absent per-level default is distinguishable from an explicitly empty VerificationSpec{}"
  - "Removed the redundant TestLoopStatus_EmbeddingSites runtime reflect.TypeOf test after golangci-lint's modernize check flagged it as simplifiable — the check was vacuous since Go's static typing already proves field types at compile time; the package-level var assertions are the correct (and sole) enforcement mechanism"

patterns-established:
  - "Cross-level VerificationSpec re-embedding: any future level needing a verification contract re-embeds the same standalone type rather than declaring a parallel shape"
  - "LoopStatus embedding sites for maxIterations:0 levels only populate LastEvaluation + ExitReason in practice — documented uniformly on Phase/Milestone/Project status doc comments"

requirements-completed: [ESC-01]

# Metrics
duration: 13min
completed: 2026-07-20
---

# Phase 52 Plan 01: Per-Level Verification Schema Fields Summary

**Generalized Phase 51's Task-only VerificationSpec/LoopStatus/LoopPolicy machinery onto Plan and Project (via a new per-level VerificationDefaults map), added the LoopLevel enum that SC3's future resolver keys off, and regenerated deepcopy/CRD/Helm artifacts across four Kinds.**

## Performance

- **Duration:** 13 min
- **Started:** 2026-07-20T05:35:00Z (approx)
- **Completed:** 2026-07-20T05:47:27Z
- **Tasks:** 2 completed
- **Files modified:** 16

## Accomplishments
- `LoopLevel` enum (`task|plan|phase|milestone|project`) + `LoopPolicy.Level` field — the schema surface a later plan's single `ResolveLoopPolicy` resolver will key off instead of switching on CRD kind
- `PlanSpec.Verification` (the exact `VerificationSpec` type from `task_types.go`, zero shape changes, CEL immutability travels automatically) and `PlanStatus.LoopStatus` (own iteration counter, doc-distinguished from `WaveIntegrationStatus.Attempts` and the planner Job's own attempt identity)
- `ProjectSpec.Verification` — a new `VerificationDefaults` struct with five optional `*VerificationSpec` pointer fields (task/plan/phase/milestone/project), mirroring `Gates`' per-level shape; doc comment carries the Pitfall-4 warning (author at `Phase: "Draft"`, never `"Locked"`) for Phase 53's chart-default authoring
- `LoopStatus` embedded on `PhaseStatus`, `MilestoneStatus`, and `ProjectStatus` (in addition to the already-existing `TaskStatus`/new `PlanStatus` sites) — doc-commented that `maxIterations:0` levels populate only `LastEvaluation` + `ExitReason` in practice
- Generalized the `LevelPhaseVerifying`/`LevelPhaseVerifyHalted` doc comments and the `ConditionVerifyHalt` "what SETS it" prose to name the new Plan/Phase/Milestone/Project call sites — zero "Task-only" residue remains, and no sibling constant family was minted
- Regenerated `zz_generated.deepcopy.go`, the four affected `config/crd/bases/*.yaml` files, and `charts/tide-crds/templates/*.yaml` (chart-reproducibility pre-commit gate required this in the same commit as the CRD-base regeneration)
- Extended the LOOP-03 no-history guard: `var _ LoopStatus = PlanStatus{}.LoopStatus` (and the Phase/Milestone/Project equivalents) proves each new embedding site uses the guarded type itself, not a locally-widened variant

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the per-level verification schema fields (D-01/D-02/D-06 + level-status embeddings)** - `d3899b05` (feat)
2. **Task 2: Regenerate deepcopy + CRD manifests and extend the no-history guard** - `64face90` (feat)
3. **Fix: drop redundant TestLoopStatus_EmbeddingSites (make lint)** - `d4e2b810` (fix, part of Task 2's verification)

**Plan metadata:** (this commit — SUMMARY.md)

## Files Created/Modified
- `api/v1alpha3/loop_types.go` - `LoopLevel` enum + 5 constants; `LoopPolicy.Level` field
- `api/v1alpha3/loop_types_test.go` - compile-time `var _ LoopStatus = ...` assertions for the 4 new embedding sites
- `api/v1alpha3/plan_types.go` - `PlanSpec.Verification`; `PlanStatus.LoopStatus`
- `api/v1alpha3/project_types.go` - `VerificationDefaults` struct; `ProjectSpec.Verification`; `ProjectStatus.LoopStatus`
- `api/v1alpha3/phase_types.go` - `PhaseStatus.LoopStatus`
- `api/v1alpha3/milestone_types.go` - `MilestoneStatus.LoopStatus`
- `api/v1alpha3/shared_types.go` - generalized `LevelPhaseVerifying`/`LevelPhaseVerifyHalted` + `ConditionVerifyHalt` doc comments
- `api/v1alpha3/zz_generated.deepcopy.go` - regenerated (`make generate`)
- `config/crd/bases/tideproject.k8s_{plans,phases,milestones,projects}.yaml` - regenerated (`make manifests`)
- `charts/tide-crds/templates/{plan,phase,milestone,project}-crd.yaml` - regenerated (`make helm`, required by the chart-reproducibility pre-commit hook)

## Decisions Made
- `LoopPolicy.Level` is stamped by the resolver, never authored directly — matches D-02's literal instruction
- `VerificationDefaults` uses pointer fields (unlike `Gates`' value strings) so absence is distinguishable from an empty `VerificationSpec{}`, per the plan's explicit instruction
- Dropped the redundant `TestLoopStatus_EmbeddingSites` runtime test (see Deviations below) — the compile-time `var` block alone satisfies both the plan's acceptance criteria and `make lint`

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Regenerated `charts/tide-crds/` to match the new CRD manifests**
- **Found during:** Task 2 (committing the regenerated CRD manifests)
- **Issue:** The pre-commit `chart-reproducibility` hook failed — `charts/tide-crds/templates/*.yaml` (helmify-generated from `config/crd/bases/`) had drifted from the freshly regenerated CRD-base YAMLs, since Task 2's action only specified `make generate` + `make manifests`, not `make helm`
- **Fix:** Ran `make helm` and staged the four regenerated chart template files in the same commit
- **Files modified:** `charts/tide-crds/templates/{plan,phase,milestone,project}-crd.yaml`
- **Verification:** Pre-commit hook re-ran and passed ("chart reproducibility (make helm + diff) ... Passed")
- **Committed in:** `64face90` (Task 2 commit)

**2. [Rule 1 - Bug] Removed a lint-flagged, actually-vacuous runtime test**
- **Found during:** Task 2's `make lint` verification pass
- **Issue:** `golangci-lint`'s `modernize` linter flagged three `reflect.TypeOf(...)` calls in the newly-added `TestLoopStatus_EmbeddingSites` as simplifiable to `reflect.TypeFor[T]()`. Applying that mechanical fix would have made the assertions compare a hardcoded type against itself — the "runtime" check was never actually exercising anything beyond what Go's static type system (and the package-level `var` assertions already added) proves at compile time.
- **Fix:** Removed `TestLoopStatus_EmbeddingSites` entirely, keeping the compile-time `var _ LoopStatus = XStatus{}.LoopStatus` block as the sole (and correct) LOOP-03 embedding-site guard — exactly the pattern the plan's action text originally suggested (`var _ LoopStatus = PlanStatus{}.LoopStatus` per status struct)
- **Files modified:** `api/v1alpha3/loop_types_test.go`
- **Verification:** `make lint` → `0 issues`; `go test ./api/v1alpha3/... -run TestLoopStatus` still green; grep count for the four status struct names in the guard file still ≥ 4 (6 after removal)
- **Committed in:** `d4e2b810`

---

**Total deviations:** 2 auto-fixed (1 blocking chart-regen, 1 bug/lint cleanup)
**Impact on plan:** Both auto-fixes were required for the plan's own stated verification (`make manifests` + a clean commit) and `make lint` to actually pass. No scope creep — no controller/resolver/dispatch logic was touched, staying within Task 1/2's `api/v1alpha3` + generated-artifact boundary.

## Issues Encountered
- `go build ./...` fails on a pre-existing, unrelated `cmd/tide-demo-init` embed-fixture error (`pattern all:fixture: no matching files found`) — confirmed present before this plan's changes via a scoped before/after check. Out of scope per the deviation rules' scope boundary (a gitignored fixture-materialization step, not something Task 1/2 touch). `go build ./api/...`, `go build ./internal/...`, `go build ./pkg/...`, and `go build ./cmd/manager/...` all build clean.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Every downstream 52-* plan (resolver, plan-check loop, level-verify dispatch/escalation, per-level verifier templates) now compiles against a complete, doc-comment-complete schema surface: `LoopLevel`, `LoopPolicy.Level`, `PlanSpec.Verification`, `PlanStatus.LoopStatus`, `ProjectSpec.Verification` (`VerificationDefaults`), and `LoopStatus` on Phase/Milestone/Project statuses
- `make manifests`/`make helm` CRD-YAML diffs landed as expected this phase (unlike Phase 49); no webhook or controller logic was touched, so no runtime behavior changed — this plan is additive schema only
- No blockers for 52-02 onward

---
*Phase: 52-per-level-looppolicy-parameterization*
*Completed: 2026-07-20*

## Self-Check: PASSED

- FOUND: .planning/phases/52-per-level-looppolicy-parameterization/52-01-SUMMARY.md
- FOUND: d3899b05
- FOUND: 64face90
- FOUND: d4e2b810
