---
phase: 13-dispatch-image-resolution-provider-halt
plan: "01"
subsystem: controller
tags: [go, controller-runtime, kubebuilder, dispatch, image-resolution, podjob]

# Dependency graph
requires:
  - phase: 03-planner-dispatch
    provides: ResolveProvider precedence chain + ProviderDefaults type that resolveImage mirrors
  - phase: 09-reporter-architecture
    provides: HelmProviderDefaults wiring on reconcilers (TaskReconcilerDeps.HelmProviderDefaults)
provides:
  - resolveImage(project, level, helmDefaults) — three-tier image precedence chain
  - All six controller dispatch sites consume resolveImage instead of r.SubagentImage
  - main.go flag-overrides-env compatibility shim for pre-13-03 chart installs
  - PodJobBackend.Run() inline image precedence walk (mirrors resolveImage)
  - Envtest regression tests asserting pinned images land in created Job specs
affects:
  - 13-02-PLAN (DISPATCH-02 chart flag removal — builds on this wiring)
  - 13-03-PLAN (drops --subagent-image chart flag; shim becomes inert)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "resolveImage pure function pattern: mirrors ResolveProvider structure exactly (same levelCfg switch, same three-case precedence switch)"
    - "Dead field comment convention: SubagentImage fields marked 'dead since Phase 13 — resolveImage owns resolution; retained for legacy test wiring, ignored at dispatch'"
    - "Flag-overrides-env compatibility shim: post-parse helmProviderDefaults.Image = subagentImage if non-empty"

key-files:
  created:
    - internal/controller/dispatch_image_test.go
  modified:
    - internal/controller/dispatch_helpers.go
    - internal/controller/dispatch_helpers_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go
    - internal/dispatch/podjob/backend.go
    - cmd/manager/main.go
    - internal/controller/task_controller_test.go
    - internal/controller/plan_planner_test.go
    - internal/controller/planner_job_absent_test.go
    - internal/controller/milestone_controller_test.go
    - internal/controller/phase_controller_test.go
    - internal/controller/phase_gates_test.go
    - internal/controller/plan_gates_test.go
    - internal/controller/plan_controller_test.go
    - internal/controller/milestone_gates_test.go
    - internal/controller/boundary_push_test.go
    - internal/controller/plan_wave_integration_test.go

key-decisions:
  - "D-03 confirmed: No CRD changes needed — Spec.Subagent.Image and LevelConfig.Image already existed; this plan is pure controller wiring"
  - "resolveImage is unexported (lowercase): all consumers are in package controller; PodJobBackend gets an inline walk, not the function"
  - "SubagentImage fields on reconcilers marked dead, not removed: retained for legacy test wiring until 13-03 cleanup"
  - "Flag-overrides-env shim applied post-flag.Parse() so the unchanged chart (still passing --subagent-image) dispatches stub via helmDefault tier"
  - "PodJobBackend.SubagentImage now receives helmProviderDefaults.Image post-shim, not raw subagentImage, for consistency"

patterns-established:
  - "resolveImage mirrors ResolveProvider exactly: same four-case levelCfg switch (milestone/phase/plan/task), same three-case precedence switch"
  - "Level 'project' has no case in the switch by design (CRD has no Levels.Project); falls through to Spec.Subagent.Image"
  - "Nil project guard: returns helmDefaults.Image without panic"

requirements-completed: [DISPATCH-01]

# Metrics
duration: 45min
completed: 2026-06-11
---

# Phase 13 Plan 01: Dispatch Image Resolution Summary

**`resolveImage` precedence chain (Levels.<level>.Image -> Spec.Subagent.Image -> helmDefault) wired at all six controller dispatch sites, closing the v1.0 stub-image bug with envtest regression coverage and a pre-13-03 compatibility shim**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-06-11T17:15:00Z
- **Completed:** 2026-06-11T17:59:15Z
- **Tasks:** 2
- **Files modified:** 20

## Accomplishments

- `resolveImage(project, level, helmDefaults) string` implemented in dispatch_helpers.go, mirroring ResolveProvider's exact structure — same four-case levelCfg switch, same three-case precedence walk
- All six dispatch sites (milestone/phase/plan/project/task createDispatchJob/task ensureJob) now call `resolveImage` instead of reading `r.SubagentImage` directly; reconciler SubagentImage fields marked dead with comment
- main.go flag-overrides-env shim: `--subagent-image` (still passed by unchanged chart) overrides `helmProviderDefaults.Image` post-parse so Layer B kind installs continue dispatching the stub until 13-03 drops the chart flag
- PodJobBackend.Run() gets an inline task-level precedence walk (mirrors resolveImage; fixture-only backend stays consistent with the chain)
- Six unit tests (TestResolveImage_*) green; two envtest regression specs assert pinned images land in created Job containers

## Task Commits

TDD RED-GREEN cycle for each task:

1. **Task 1: TestResolveImage specs (RED)** - `a1d52f4` (test)
2. **Task 1: resolveImage implementation (GREEN)** - `5a18cb0` (feat)
3. **Task 2: envtest regression specs (RED)** - `1d560ce` (test)
4. **Task 2: six dispatch sites wired + shim (GREEN)** - `0827768` (feat)

## Files Created/Modified

- `internal/controller/dispatch_helpers.go` - Added `resolveImage` function (lines 254-286)
- `internal/controller/dispatch_helpers_test.go` - Six TestResolveImage_* unit tests
- `internal/controller/dispatch_image_test.go` - NEW: envtest regression for DISPATCH-01
- `internal/controller/milestone_controller.go` - resolveImage("milestone") wired; SubagentImage field dead-commented
- `internal/controller/phase_controller.go` - resolveImage("phase") wired; subagentImage local-var+fallback removed
- `internal/controller/plan_controller.go` - resolveImage("plan") wired; subagentImage local-var+fallback removed
- `internal/controller/project_controller.go` - resolveImage("project") wired; Step 8 block replaced
- `internal/controller/task_controller.go` - resolveImage("task") wired at createDispatchJob + ensureJob; Deps.SubagentImage dead-commented
- `internal/dispatch/podjob/backend.go` - Inline task-level precedence walk in Run() before BuildOptions
- `cmd/manager/main.go` - Flag help text updated; post-parse shim; PodJobBackend.SubagentImage -> helmProviderDefaults.Image
- 10 test files - HelmProviderDefaults.Image = testSubagentImage wired to all reconciler test constructions

## Decisions Made

- resolveImage is unexported (package-local) — all six dispatch sites are in `package controller`; PodJobBackend is in `podjob` package and uses an inline walk per plan specification
- Dead SubagentImage fields retained (not removed) — plan 13-03 will clean them up after chart flag is dropped; pre-13-03 test wiring may still set the field for historical clarity
- Compatibility shim fires post-flag.Parse() so `subagentImage` variable (populated by `--subagent-image` flag) can override `helmProviderDefaults.Image`; CRD fields beat the shim via resolveImage

## Deviations from Plan

None — plan executed exactly as written.

## TDD Gate Compliance

RED gate: `a1d52f4` (test), `1d560ce` (test) — two RED commits preceding their respective GREEN commits  
GREEN gate: `5a18cb0` (feat), `0827768` (feat) — both GREEN commits pass all tests  
REFACTOR: Not required — implementations were clean on first pass

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: T-13-02 (mitigated) | dispatch_helpers.go | Image ref is passed via typed Container.Image (no shell interpolation); byte-identical pass-through verified by envtest regression |
| threat_flag: T-13-03 (accepted) | dispatch_helpers.go | Empty resolveImage return (no flag/env/CRD) → Job with empty image; kubelet fails pod with ImagePullBackOff — observable, not silent |

## Issues Encountered

None. Plan interfaces table (line-number anchors for all six dispatch sites) was accurate. No pre-existing test failures encountered.

## Next Phase Readiness

- Plan 13-02 (DISPATCH-02): drops `--subagent-image` from chart deployment.yaml, adds `subagent.defaults.image` helm value, updates kind test harness to opt-in stub via `--set subagent.defaults.image=...stub`; builds on helmProviderDefaults.Image channel this plan established
- Plan 13-03 (HALT-01): billing halt via credproxy + reconciler backstop; uses same `r.HelmProviderDefaults` field this plan wired on all reconcilers

## Self-Check: PASSED

- internal/controller/dispatch_helpers.go: FOUND
- internal/controller/dispatch_image_test.go: FOUND
- internal/controller/dispatch_helpers_test.go: FOUND
- All controller files: FOUND
- Commits a1d52f4, 5a18cb0, 1d560ce, 0827768: FOUND
- resolveImage dispatch sites: 6 (expected 6)
- func resolveImage count: 1 (expected 1)
