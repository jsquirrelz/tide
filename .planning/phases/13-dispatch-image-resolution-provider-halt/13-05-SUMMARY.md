---
phase: 13-dispatch-image-resolution-provider-halt
plan: "05"
subsystem: controller/credproxy/cmd
tags: [cr01, wr03, halt-01, dispatch-01, billing, nil-guard, time-fence, tdd]
dependency_graph:
  requires: [13-01, 13-02, 13-04]
  provides: [DISPATCH-01-milestone-guard, HALT-01-straggler-fence, WR-03]
  affects: [milestone_controller, phase_controller, plan_controller, project_controller, task_controller, credproxy, cmd/tide]
tech_stack:
  added: []
  patterns: [tdd-red-green, nil-guard-mirroring-cascade-7, time-fence-fail-closed, metadata-annotation-separate-from-status-patch]
key_files:
  created: []
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/dispatch_image_test.go
    - internal/controller/billing_halt.go
    - internal/controller/billing_halt_test.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go
    - api/v1alpha1/shared_types.go
    - cmd/tide/resume.go
    - cmd/tide/resume_test.go
    - internal/credproxy/server.go
    - internal/credproxy/server_test.go
decisions:
  - "jobStart threaded via completedJob parameter on all 5 handleJobCompletion signatures (renamed _ to completedJob); nil job (TTL/GC case) uses zero time which is fail-closed"
  - "resume.go stamps AnnotationBillingResumedAt with a second Get+MergePatch after the status patch — separate patch because metadata and status are different subresources"
  - "credproxy synthetic body reworded to remove 'credit balance'; real 400 still passes through byte-identical as the true billing evidence source"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-11"
  tasks_completed: 3
  files_modified: 13
---

# Phase 13 Plan 05: CR-01 and WR-03 Gap Closure Summary

Closes two code-defect blockers confirmed by 13-VERIFICATION.md: CR-01 (nil-project panic in milestone reconcilePlannerDispatch) and WR-03 (BillingHalt re-stamped by pre-resume stragglers via the credproxy latch body).

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 RED | CR-01 nil-project envtest spec (panics) | 9a93a3a | dispatch_image_test.go |
| 1 GREEN | CR-01 nil-project guard in milestone dispatch | 35e779a | milestone_controller.go |
| 2 RED | WR-03 time-fence + resume annotation (compile fail) | 3632a7a | billing_halt_test.go, resume_test.go |
| 2 GREEN | WR-03 billing-resumed-at annotation + jobStart guard | 984c886 | shared_types.go, billing_halt.go, 5 controllers, resume.go |
| 3 RED | WR-03 credproxy synthetic body classifier assertion | ade767f | server_test.go |
| 3 GREEN | WR-03 credproxy latch body drops classifier substring | a722716 | server.go |

## What Was Built

### CR-01: Nil-project guard in milestone reconcilePlannerDispatch

Inserted a nil guard immediately after Step 4's project Get block in `reconcilePlannerDispatch`. Mirrors the `plan_controller.go` cascade-7 guard shape:
- `spec.projectRef == ""`: refuse dispatch without requeueing (CRD MinLength=1, near-unreachable)
- project nil (transient Get failure / Project deleted): `return ctrl.Result{RequeueAfter: 1 * time.Second}`

The existing `:370` (`project.Spec.ProviderSecretRef`) and `:394` (`string(project.UID)`) derefs are safe under the guard. The PlannerPool Release defer covers the early returns.

### WR-03: Resume time-fence

`AnnotationBillingResumedAt = "tideproject.k8s/billing-resumed-at"` added to `shared_types.go`.

`setBillingHaltIfNeeded` signature changed to include `jobStart time.Time`. Time fence: if the project carries `AnnotationBillingResumedAt`, jobStart is non-zero, and `jobStart.Before(resumedAt)` → return nil (stale pre-resume evidence). Fail-closed: zero jobStart or unparseable annotation → stamp halt.

All 5 handleJobCompletion sites renamed `_ *batchv1.Job` to `completedJob *batchv1.Job`; `jobStart = completedJob.CreationTimestamp.Time` where non-nil.

`tide resume`: after clearing BillingHalt via status patch, performs a second Get then metadata MergePatch stamping `billing-resumed-at = time.Now().UTC().Format(time.RFC3339)`. Output message extended to mention the straggler fence.

### WR-03 defense-in-depth: credproxy latch body

Synthetic short-circuit body reworded to `"TIDE billing halt is active (cached at credproxy); upstream not contacted. Run tide resume after refilling credits."` — no "credit balance" substring. Added a detailed comment explaining why: the real first 400 body leads stderr (EnvelopeOut.Reason is built from the head of stderr), so the manufactured-evidence channel is only the restarted-container case, which is now closed.

## Test Coverage

- 1 envtest Ginkgo spec (CR-01 nil-project no-panic, RequeueAfter confirmed)
- 5 unit tests: setBillingHaltIfNeeded jobStart guard (pre-resume no-stamp, post-resume stamps, no-annotation stamps, zero fail-closed, unparseable fail-closed)
- 2 unit tests: resumeRun stamps AnnotationBillingResumedAt only when BillingHalt was cleared
- 1 unit test: credproxy synthetic body does not contain "credit balance" (explicit negative assertion)

## Verification

```
grep -n "setBillingHaltIfNeeded(ctx, r.Client, project, out.Reason," internal/controller/*.go | grep -cv _test
# → 5
```

All `go test ./internal/controller/ ./cmd/tide/ ./internal/credproxy/ ./api/...` green.

## Deviations from Plan

None. Plan executed exactly as written.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced beyond what the plan's threat model covers (T-13-G1 through T-13-G4 all mitigated as specified).

## Known Stubs

None. All wiring is complete.

## Self-Check: PASSED

Files exist:
- internal/controller/milestone_controller.go — CR-01 guard present
- internal/controller/billing_halt.go — jobStart time.Time parameter present
- api/v1alpha1/shared_types.go — AnnotationBillingResumedAt present
- cmd/tide/resume.go — billing-resumed-at stamp present
- internal/credproxy/server.go — latch body reworded (no "credit balance")

Commits verified:
- 9a93a3a (RED CR-01), 35e779a (GREEN CR-01), 3632a7a (RED WR-03), 984c886 (GREEN WR-03), ade767f (RED Task 3), a722716 (GREEN Task 3)
