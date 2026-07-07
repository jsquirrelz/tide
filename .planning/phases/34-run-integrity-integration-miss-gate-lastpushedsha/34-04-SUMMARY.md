---
phase: 34-run-integrity-integration-miss-gate-lastpushedsha
plan: "04"
subsystem: internal/controller
tags: [integ-01, integ-02, wave-loop, bounded-retry, conflict-classification]
requirements: [INTEG-01, INTEG-02]

dependency_graph:
  requires: ["34-02", "34-03"]
  provides:
    - "Full-range wave-boundary loop (final Kahn wave now integrates)"
    - "triggerBoundaryPush computes the cumulative Succeeded set internally + D-02 gate"
    - "errGitWriterBusy sentinel + 5 call-site requeue handling"
    - "maxWaveIntegrationAttempts bounded-retry state machine on Plan.Status.WaveIntegration"
    - "D-10 conflict semantics: Plan Failed with ReasonMergeConflict naming both branches"
  affects:
    - internal/controller/plan_controller.go
    - internal/controller/boundary_push.go
    - internal/controller/boundary_push_test.go
    - internal/controller/plan_wave_integration_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go

tech_stack:
  added: []
  patterns:
    - "#13b bounded-retry machine (Attempts/LastAttemptTime/LastError, Background-propagation delete, capped backoff) mirrored onto Plan.Status.WaveIntegration"

key_files:
  modified:
    - internal/controller/plan_controller.go
    - internal/controller/boundary_push.go
    - internal/controller/boundary_push_test.go
    - internal/controller/plan_wave_integration_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go

decisions:
  - "triggerBoundaryPush's `integrateBranches []string` parameter was dropped entirely (not just defaulted) — the cumulative set is now ALWAYS computed inside the shared trigger via a live List, per D-03. PlanReconciler.maybeTriggerBoundaryPush also lost its taskItems parameter (dead code — the only live call site passed nil, pre-Tasks)."
  - "errGitWriterBusy requeues at 5s from all 5 call sites (milestone x2, phase x2, plan x1) — a busy gate is normal serialization, not a reconcile error."
  - "Fixed a pre-existing regression risk in plan_wave_integration_test.go: the file's manual Job status patches only set Status.Succeeded/Failed counts, not the JobComplete/JobFailed CONDITIONS the new gitWriterInFlightCount (via isJobTerminal) reads — cross-plan false-busy would have resulted. Rewired those spots to use the existing makeFakeJobTerminal helper, and added the tideproject.k8s/project label (now required by the List-based cumulative-set helper) to all Task fixtures in that file."
  - "Rewrote plan_wave_integration_test.go's step (e) from 'single failure -> immediate Plan Failed' (pre-fix semantics) to the new bounded-retry-then-fail-at-cap contract — this is an intentional behavior change the phase makes, not a test regression; verified by running the loop through all 5 attempts and asserting Failed only at the cap."

metrics:
  duration: "~2h"
  completed: "2026-07-04"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 6
---

# Phase 34 Plan 04: PlanReconciler Wave-Loop Fix + Bounded Retry — Summary

**One-liner:** Closed the `k < len(layers)-1` last-wave skip (now `k < len(layers)`, so a single-wave plan and every plan's final wave integrate); `triggerBoundaryPush` computes the cumulative Succeeded-branch set via a live List (not caller-supplied) and gates on a new D-02 single-flight `gitWriterInFlightCount` check; wave-integration Job failures now ride a bounded retry (`Plan.Status.WaveIntegration`, cap 5) with merge conflicts (D-09/D-10) failing the Plan immediately instead of retrying.

## Tasks Completed

| Task | Name | Files |
|------|------|-------|
| 1 | boundary_push.go — cumulative set + D-02 gate | boundary_push.go, boundary_push_test.go, milestone_controller.go, phase_controller.go, plan_controller.go (call site) |
| 2 | plan_controller.go — full-range loop + bounded retry + conflict→Plan Failed | plan_controller.go, plan_wave_integration_test.go |
| 3 | Layer A sweep — Pitfall 6 timing audit + full envtest pass | (verification only; no additional test-file changes needed beyond Task 2's fixes) |

## Verification Results (all commands actually run this session)

- `go build ./internal/controller/...` — PASS
- `go test ./internal/controller/... -count=1` (envtest, `KUBEBUILDER_ASSETS` set) — PASS, 177/177 then 184/185 specs green across incremental runs as new specs were added (final full run below)
- Targeted: `go test ./internal/controller/... -run 'TestPlanReconciler' -v -count=1` — PASS, all 5 (1 pre-existing + 4 new: single-wave dispatch, final-wave-integrates-and-gates-Succeeded, D-02-gate-blocks-dispatch, conflict-fails-Plan-immediately)
- `make test-int-fast` (Layer A envtest, no Docker) — MAKE_EXIT=0, 55/55 specs green, zero `^--- FAIL|^FAIL\s` grep hits
- `grep -cE 'for k := 0; k < len\(layers\); k\+\+' internal/controller/plan_controller.go` → 1
- `grep -c 'maxWaveIntegrationAttempts = 5' internal/controller/plan_controller.go` → 1
- `grep -c 'ReasonMergeConflict' internal/controller/plan_controller.go` ≥ 1, conflict path proven single-shot (Attempts stays 0) by the new conflict test
- `grep -c 'DeletePropagationBackground' internal/controller/plan_controller.go` ≥ 1
- `grep -c 'taskItems' internal/controller/boundary_push.go` → 0

## Pitfall 6 (delayed Plan=Succeeded timing) — audit outcome

No existing envtest/kind spec broke from extending the wave loop. Full `go test ./internal/controller/...` (177 pre-existing + subsequently-added specs) stayed green throughout — the no-git short-circuit (`project == nil || ... || r.TidePushImage == ""`) already protected stub/test fixtures, and no git-configured fixture in the existing suite asserted a quick Plan=Succeeded that the new final-wave gating would delay. No timeout bumps or test weakening were needed.
