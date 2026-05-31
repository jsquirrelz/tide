---
phase: "07"
plan: "05"
subsystem: controller
tags: [project-controller, dispatch, planner-job, milestone-authoring, d-a2]
dependency_graph:
  requires: ["07-03", "07-04"]
  provides: ["07-06"]
  affects: ["internal/controller/project_controller.go", "cmd/manager/main.go"]
tech_stack:
  added: []
  patterns:
    - "reconcileProjectPlannerDispatch mirrors milestone_controller.go:reconcilePlannerDispatch"
    - "SigningKey guard: len(r.SigningKey)==0 → skip dispatch (tolerate test mode)"
    - "checkProjectComplete calls gates.BoundaryDetected(project, Milestone)"
    - "handleProjectJobCompletion materializes Milestone via MaterializeChildCRDs, no gate (D-02)"
key_files:
  created: []
  modified:
    - internal/controller/project_controller.go
    - cmd/manager/main.go
decisions:
  - "SigningKey guard in reconcileProjectPlannerDispatch: when len(r.SigningKey)==0 skip dispatch so existing tests without dispatch fields stay green"
  - "No gate at Project->Milestone boundary per D-02: handleProjectJobCompletion auto-proceeds"
  - "Project is its own parent in BuildPlannerEnvelope call: level=project, parent=project, project=project"
  - "Cascade-13 idempotency guard preserved: Phase=Complete/Running short-circuits in reconcileProjectPlannerDispatch"
metrics:
  duration: "12min"
  completed: "2026-05-31"
  tasks_completed: 2
  files_modified: 2
---

# Phase 7 Plan 5: ProjectReconciler D-A2 5th Dispatch Site Summary

ProjectReconciler is now the 5th D-A2 dispatch site. A bare Project advances through Initialized → Running (planner Job dispatched) → completes via checkProjectComplete when owned Milestones reach Succeeded. This is the heart of cascade-7 closure.

## What Was Built

**Task 1 — project_controller.go:**

- Added 5 new struct fields to `ProjectReconciler`: `EnvReader podjob.EnvelopeReader`, `SigningKey []byte`, `SubagentImage string`, `CredproxyImage string`, `HelmProviderDefaults ProviderDefaults`
- Added RBAC marker: `// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones,verbs=get;list;watch;create`
- Added `checkProjectComplete(ctx, project)`: calls `gates.BoundaryDetected(project, "Milestone")`, patches `Status.Phase=Complete` + `ConditionSucceeded=True` when all owned Milestones are Succeeded
- Added `reconcileProjectPlannerDispatch(ctx, project)`: mirrors `milestone_controller.go:reconcilePlannerDispatch` — terminal short-circuit (Complete/InitFailed), Running check with job terminal detection, PlannerPool acquire, envelope build, token mint, job create (`tide-project-<uid>-1`), patch Phase=Running + AuthoringPlanner=True
- Added `handleProjectJobCompletion(ctx, project, job)`: reads EnvelopeOut via EnvReader (tolerates nil), calls MaterializeChildCRDs to create the Milestone CR, no gate per D-02
- Inserted `checkProjectComplete` + `reconcileProjectPlannerDispatch` at the top of `reconcilePhase3Lifecycle`, before branch-name init (Step 0 and Step 0b)
- New imports: `internal/credproxy`, `internal/dispatch/podjob`, `internal/gates`, `pkg/dispatch`

**Task 2 — cmd/manager/main.go:**

- Added 5 fields to `ProjectReconciler` registration: `EnvReader: envReader`, `SigningKey: signingKey`, `SubagentImage: subagentImage`, `CredproxyImage: credproxyImage`, `HelmProviderDefaults: helmProviderDefaults` — same variables already computed above for MilestoneReconciler

## Verification Results

```
go vet ./...               → clean
go build ./...             → clean
go test ./internal/controller/... → ok (93/93 specs pass)
go test ./cmd/...          → ok (all packages pass)
```

Verification checks:
- `grep -c reconcileProjectPlannerDispatch project_controller.go` → 1
- `grep -c handleProjectJobCompletion project_controller.go` → 1
- `grep -c checkProjectComplete project_controller.go` → 5 (definition + 4 call/reference sites)
- `grep -c "tide-project-.*-1" project_controller.go` → 2
- `grep -c "BoundaryDetected.*Milestone" project_controller.go` → 2
- `grep -c "EnvReader.*envReader" cmd/manager/main.go` → 6 (multiple wiring sites across reconcilers)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] SigningKey nil-guard in reconcileProjectPlannerDispatch**
- **Found during:** Task 1, running `go test ./internal/controller/...`
- **Issue:** 4 existing tests failed with `credproxy: signingKey must not be empty` — tests that exercise clone/push lifecycle set `Dispatcher: &stubDispatcher{}` but do not wire SigningKey. The new dispatch code runs inside `reconcilePhase3Lifecycle` which those tests reach via Phase=Initialized.
- **Fix:** Added `if len(r.SigningKey) == 0 { return ctrl.Result{}, nil }` guard at the top of `reconcileProjectPlannerDispatch`. This matches the `r.EnvReader != nil` toleration pattern in `handleProjectJobCompletion` and the `r.Dispatcher != nil` guard in `reconcilePlannerDispatch` (milestone controller) — test setups that don't configure dispatch are unaffected.
- **Files modified:** `internal/controller/project_controller.go`
- **Cascade-13 compatibility verified:** Phase=Complete correctly short-circuits in the terminal switch before the SigningKey guard is reached, so the no-revert test passes.

## Known Stubs

None — this plan wires production code. The project-level planner Job dispatched by `reconcileProjectPlannerDispatch` will execute whatever image is in `SubagentImage`/`HelmProviderDefaults.Image`. The stub subagent (Plan 07-04) provides the `$0` path.

## Threat Flags

No new trust boundaries beyond what is in the plan's threat model. The T-308 allowlist in `MaterializeChildCRDs` already covers Milestone. RBAC marker added per T-07-05-03 (subagent pods have zero K8s verbs; ProjectReconciler creates Milestones via its own SA).

## Self-Check: PASSED
- `internal/controller/project_controller.go` exists with all 3 new methods
- `cmd/manager/main.go` wires all 5 fields
- Commits `281e799` (Task 1) and `021911d` (Task 2) verified in git log
