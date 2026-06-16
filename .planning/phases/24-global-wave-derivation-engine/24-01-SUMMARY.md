---
phase: 24-global-wave-derivation-engine
plan: "01"
subsystem: test
tags: [tdd, envtest, global-wave, exec-01, exec-02, exec-03, exec-04]
dependency_graph:
  requires: []
  provides: [global-wave-derivation-test-contract]
  affects: [test/integration/envtest/]
tech_stack:
  added: []
  patterns: [ginkgo-v2, gomega, envtest, createSimplePhase, createSimpleMilestone]
key_files:
  created:
    - test/integration/envtest/global_wave_derivation_test.go
  modified: []
decisions:
  - "Reuse makeTask/makeTaskWithWaveLabel/createSimplePlan from indegree_test.go unchanged — no duplication"
  - "assertWaveExists helper uses 30s/500ms Eventually timeout following indegree_test.go style"
  - "AfterEach deletes Waves first, then Tasks/Plans/Phases/Milestones/Projects/PVCs to prevent cross-It contamination"
  - "Cross-scope case uses Plan.DependsOn=[cm-plan-a] coarse ref to exercise fan-out assertion"
metrics:
  duration: "~8 minutes"
  completed: "2026-06-16"
  tasks_completed: 1
  files_created: 1
  files_modified: 0
---

# Phase 24 Plan 01: Global Wave Derivation Test Scaffold (RED) Summary

Wave 0 RED test scaffold — envtest contract file encoding the README worked example and EXEC-01..04 assertions, confirmed RED on current main before any engine implementation.

## What Was Built

`test/integration/envtest/global_wave_derivation_test.go` (414 lines, package `envtest_integration`):

- Two new cross-scope fixture helpers: `createSimplePhase(ctx, name, milestoneRef)` and `createSimpleMilestone(ctx, name, projectRef)` — follow the `createSimplePlan` Create+Eventually-Get shape from `indegree_test.go`.
- `assertWaveExists(ctx, projectName, waveIdx)` — Wave CR name assertion helper using 30s/500ms Eventually.
- Top-level `Describe("Global Wave Derivation", ...)` with four nested Describe/Context blocks whose names contain the substrings `GlobalDag`, `GlobalWaveIndex`, `BidirectionalIndex`, `WaveRederivation` (VALIDATION.md `-run` selectors).
- README:54 worked-example fixture: tasks α,β,γ in plan-A; δ,ε in plan-B; ζ,η,θ in plan-C; edges α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ; expected global waves [{α,β,γ,ζ},{δ,η},{ε,θ}].
- Cross-phase/cross-milestone coarse-ref fan-out case using `createSimplePhase`/`createSimpleMilestone` plus `Plan.DependsOn=[cm-plan-a]` to exercise Plan-level coarse dep fan-out.

## RED Test Evidence (Confirmed on Current Main)

```
[FAILED] Timed out after 30.000s.
Wave CR tide-wave-global-wave-test-project-0 should exist
    waves.tideproject.k8s "tide-wave-global-wave-test-project-0" not found
```

Run command: `go test ./test/integration/envtest/... -v -timeout 55s -count=1 -- -ginkgo.focus="GlobalWaveIndex"`

The contract is real and unmet — global derivation engine does not yet exist. Plans 02/03 will turn this GREEN.

## Acceptance Criteria Verified

| Check | Result |
|-------|--------|
| File exists, package `envtest_integration` | PASS |
| `go vet ./test/integration/envtest/...` exits 0 | PASS |
| `grep -c 'func makeTask' global_wave_derivation_test.go` returns 0 | PASS (0 — reused from indegree_test.go) |
| Four VALIDATION.md selector substrings in non-comment lines | PASS (4) |
| `grep -c 'func createSimplePhase\|func createSimpleMilestone'` returns 2 | PASS |
| `grep -c 'tide-wave-'` returns ≥ 1 | PASS (16) |
| File ≥ 120 lines | PASS (414 lines) |
| Runs RED on current main | PASS — `tide-wave-global-wave-test-project-0 not found` |

## Commits

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Cross-scope fixture helpers + global-derivation test file (RED) | 937d847 | test/integration/envtest/global_wave_derivation_test.go |

## Deviations from Plan

None — plan executed exactly as written. The `assertWaveExists` helper was added inline in the test file rather than as a named standalone function in a separate helpers file; both approaches are equivalent since all helpers in the package are visible across files.

## Known Stubs

None. The test file contains no stubs — all assertions are real and intentionally unmet on current main (RED by design).

## Threat Flags

None. Test-only code; no new production trust boundary introduced.

## Self-Check: PASSED

- File `test/integration/envtest/global_wave_derivation_test.go` exists: FOUND
- Commit 937d847 exists: FOUND
- `go vet ./test/integration/envtest/...`: exits 0
- RED test evidence captured above
