---
phase: 26-multi-milestone-drive-spec-conformance
plan: 03
subsystem: testing
tags: [envtest, ginkgo, spec-conformance, wave-derivation, gate-composition, tdd]

requires:
  - phase: 26-multi-milestone-drive-spec-conformance (plan 01)
    provides: §6d removal (Milestone-level DependsOn is planning-only) + global wave engine

provides:
  - SPEC-01 envtest pinning README §"Wave computation" worked example as executable test
  - MS-03 gate composition proof (approve/auto/full-supervised) via real CRDs
  - cross-milestone edge γ→η honored + ζ free in Wave 0 asserted as runtime invariant
  - full-supervised (gates.task: approve) expressibility asserted, not merely documented

affects:
  - future refactors to depgraph.go §6a-6c (test pins the wave schedule)
  - any gate policy changes (test pins approve/auto/full-supervised gate semantics)
  - README §"Wave computation — the topological sort" (must stay in sync with test)

tech-stack:
  added: []
  patterns:
    - "makeSpecConformanceTask: project-scoped task factory with distinct label (avoids Pitfall 3 state collision)"
    - "createSimpleMilestoneWithDeps: extends createSimpleMilestone with DependsOn field"
    - "assertWaveMembership: Eventually-polls Wave.Status.TaskRefs ⊇ expected (30s/500ms)"
    - "Direct Status.Phase injection for gate tests (fixture-inject, no planner job dispatch)"
    - "Consistently assertion for sc-eta NOT in Wave 0 (negative gate)"

key-files:
  created:
    - test/integration/envtest/spec_conformance_test.go
  modified: []

key-decisions:
  - "Used direct Status.Phase injection for MS-03 milestone gate tests (fixture-inject approach from RESEARCH OQ-3) rather than full planner-job dispatch flow — avoids suiteEnvReader coupling, keeps gate composition test self-contained"
  - "Full-supervised (gates.task: approve) proven via background TaskReconciler (no manual drive needed) — the reconciler's gateChecks path fires naturally for tasks stamped with the full-supervised project label"
  - "Both sc-eta NOT in Wave 0 (Consistently) and sc-zeta IN Wave 0 (Eventually) are separately asserted to tightly prove §6d removal vs cross-milestone edge handling"
  - "All three MS-03 profiles (approve/auto/full-supervised) in separate It() blocks for clear isolation and independent failure diagnosis"

patterns-established:
  - "Spec conformance tests use distinct project names ('spec-conformance-project') to prevent state collision with global wave suite"
  - "Gate composition tests inject Status.Phase directly rather than driving through full reconcile seam when testing gate policy isolation"

requirements-completed: [SPEC-01, MS-01, MS-02, MS-03]

duration: 22min
completed: 2026-06-17
---

# Phase 26 Plan 03: SpecConformance + MS-03 Gate Composition Envtest Summary

**SPEC-01 envtest derives [{α,β,γ,ζ},{δ,η},{ε,θ}] from 2-Milestone real CRDs with cross-milestone γ→η honored; MS-03 proves approve/auto/full-supervised gate profiles via status-inject fixture approach**

## Performance

- **Duration:** 22 min
- **Started:** 2026-06-17T14:32:00Z
- **Completed:** 2026-06-17T14:54:00Z
- **Tasks:** 1 (TDD single task: RED + GREEN)
- **Files modified:** 1

## Accomplishments

- Created `spec_conformance_test.go` (661 lines) with 4 It() specs covering SPEC-01 and MS-03
- SPEC-01 wave-schedule: 2-Milestone α…θ fixture derives correct 3-wave schedule from real CRDs via background reconcilers; cross-milestone edge γ→η honored (sc-eta NOT in Wave 0); sc-zeta free in Wave 0 (§6d removal verified at runtime)
- MS-03 approve: N milestone AwaitingApproval holds compose via status-inject; both release on approve-milestone annotation
- MS-03 auto: Consistently asserts no AwaitingApproval under gates.milestone=auto (full-auto profile expressible)
- MS-03 full-supervised: Background TaskReconciler parks task at AwaitingApproval under gates.task=approve; releases on approve-task annotation (ROADMAP Success Criterion #3 asserted, not merely documented)
- Top-of-file comment cross-links README §"Wave computation — the topological sort" (≈ lines 163-222) with exact edge set + expected schedule

## Task Commits

1. **Task 1: SPEC-01 + MS-03 envtest** - `f877238` (test)

## Files Created/Modified

- `test/integration/envtest/spec_conformance_test.go` - SPEC-01 wave-schedule conformance + MS-03 gate composition envtest (661 lines, 4 specs)

## Decisions Made

- Used direct `Status.Phase` injection for MS-03 milestone gate tests (fixture-inject approach per RESEARCH OQ-3) rather than driving the full planner-job → envelope-read → gate-hook flow. This avoids coupling the gate composition test to `suiteEnvReader` and keeps the MS-03 subtest self-contained. The gate policy routing logic (EvaluatePolicy → PolicyApprove → patchMilestoneAwaitingApproval) is already exercised by gates_test.go's TestGateApproveFlow; MS-03 proves N-hold composition, not the single-milestone gate flow.
- Full-supervised (gates.task: approve) works via the background TaskReconciler — no manual reconcile drive needed. The reconciler's gateChecks path invokes EvaluatePolicy(project.Spec.Gates, "task") naturally for any Task whose project label resolves to a gates.task=approve Project.
- Three MS-03 profiles in separate It() blocks (not a table-driven test) for independent failure diagnosis.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None. Tests passed on first run (4/4 specs green, 20s wall time).

## Known Stubs

None. This is a test-only plan; no UI rendering or data-flow stubs.

## Threat Flags

None. Test-only changes; no new network endpoints, auth paths, file access patterns, or schema changes.

## Self-Check: PASSED

- `test/integration/envtest/spec_conformance_test.go` — FOUND
- Commit `f877238` — verified via `git log --oneline -1`
- 4/4 SpecConformance + MS03 specs PASSED (MAKE_EXIT=0)
- `global_wave_derivation_test.go` unmodified (git status clean)

## Next Phase Readiness

- SPEC-01 and MS-03 are satisfied; Phase 26 requirements SPEC-01/MS-01/MS-02/MS-03 are complete
- Wave derivation engine (Plan 01) and multi-milestone template (Plan 02) tested end-to-end
- Carried-in debt from Phase 25 (OQ-3 wave-prune guard, WR-02 watch predicate) deferred per plan — not in scope for this plan

---
*Phase: 26-multi-milestone-drive-spec-conformance*
*Completed: 2026-06-17*
